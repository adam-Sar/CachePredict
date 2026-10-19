/**
 * Precog Browser SDK: Zero-Latency Client-Side Predictive Caching
 * 
 * 1. Tracks client navigation & API requests.
 * 2. Runs Precog Dense Matrix Predictor in WebAssembly (or lightweight fallback).
 * 3. Pre-warms speculative API responses directly into Browser CacheStorage (caches.open).
 * 4. Injects W3C Speculation Rules for instant 0ms page navigations.
 * 5. Intercepts fetch() to serve cached responses with 0ms network latency.
 */
(function(window) {
  'use strict';

  const CACHE_NAME = 'precog-cache-v1';
  let wasmLoaded = false;
  let precogCache = null;

  // Initialize CacheStorage
  if ('caches' in window) {
    caches.open(CACHE_NAME).then(c => { precogCache = c; });
  }

  // Lazy-load the Go WASM predictor (runtime glue + module served by the backend).
  // Silently falls back to the lightweight JS tracker if unavailable.
  let wasmLoading = false;
  async function loadWasmPredictor() {
    if (window.PrecogWasm || wasmLoading || typeof WebAssembly === 'undefined') return;
    wasmLoading = true;
    try {
      if (!window.Go) {
        await new Promise((resolve, reject) => {
          const s = document.createElement('script');
          s.src = '/precog/wasm_exec.js';
          s.onload = resolve;
          s.onerror = reject;
          document.head.appendChild(s);
        });
      }
      if (!window.Go) return;
      const go = new Go();
      const result = await WebAssembly.instantiateStreaming(fetch('/precog/precog.wasm'), go.importObject);
      await go.run(result.instance);
    } catch (err) {
      // WASM unavailable (not deployed or instantiation failed); JS fallback continues
    }
  }
  loadWasmPredictor();

  // Lightweight in-browser transition tracker fallback before Wasm loads
  const fallbackTransitions = new Map();
  let lastRoute = null;

  function recordTransition(from, to) {
    if (!from || !to) return;
    if (!fallbackTransitions.has(from)) {
      fallbackTransitions.set(from, new Map());
    }
    const row = fallbackTransitions.get(from);
    row.set(to, (row.get(to) || 0) + 1);
  }

  function predictFallback(from, topK = 3) {
    const row = fallbackTransitions.get(from);
    if (!row) return [];
    return Array.from(row.entries())
      .sort((a, b) => b[1] - a[1])
      .slice(0, topK)
      .map(([url]) => url);
  }

  // Record an API call or navigation
  function record(method, path) {
    const current = path.split('?')[0];
    if (window.PrecogWasm && window.PrecogWasm.record) {
      window.PrecogWasm.record(method, path);
    } else if (lastRoute) {
      recordTransition(lastRoute, current);
    }
    lastRoute = current;
    triggerSpeculativePrefetch();
  }

  // Get predictions
  function getPredictions(topK = 3) {
    if (window.PrecogWasm && window.PrecogWasm.predict) {
      try {
        const raw = window.PrecogWasm.predict(topK);
        const preds = JSON.parse(raw);
        return preds.map(p => {
          const parts = p.Call.split(' ');
          return parts.length > 1 ? parts[1] : p.Call;
        });
      } catch (e) {
        // fallback
      }
    }
    return lastRoute ? predictFallback(lastRoute, topK) : [];
  }

  // Speculatively prefetch predicted URLs into Browser CacheStorage
  async function triggerSpeculativePrefetch() {
    const predictedUrls = getPredictions(3);
    if (!predictedUrls || predictedUrls.length === 0) return;

    // 1. Update W3C Speculation Rules in DOM
    injectSpeculationRules(predictedUrls);

    // 2. Background fetch into Browser CacheStorage
    if (!precogCache) return;

    for (const url of predictedUrls) {
      if (!url.startsWith('/') && !url.startsWith('http')) continue;
      // Skip unhydrated route placeholders
      if (url.includes(':id') || url.includes(':uuid') || url.includes(':hex')) continue;
      try {
        const has = await precogCache.match(url);
        if (!has) {
          // Use originalFetch to bypass interceptor and avoid overhead or recursive recording
          const resp = await originalFetch(url, {
            headers: { 'X-Precog-Speculative': 'true' },
            priority: 'low',
            credentials: 'same-origin'
          });
          if (resp && resp.ok) {
            const cc = (resp.headers.get('Cache-Control') || '').toLowerCase();
            // Strictly enforce RFC 9111 cacheability in browser CacheStorage
            if (!cc.includes('no-store') && !cc.includes('private') && !resp.headers.has('Set-Cookie')) {
              await precogCache.put(url, resp.clone());
            }
          }
        }
      } catch (err) {
        // Non-blocking prefetch failure ignored
      }
    }
  }

  // Dynamically inject or update W3C Speculation Rules tag
  function injectSpeculationRules(urls) {
    if (!HTMLScriptElement.supports || !HTMLScriptElement.supports('speculationrules')) {
      return;
    }
    const validUrls = urls.filter(u => !u.includes(':id') && !u.includes(':uuid') && !u.includes(':hex'));
    if (validUrls.length === 0) return;

    let script = document.getElementById('precog-speculation-rules');
    if (!script) {
      script = document.createElement('script');
      script.id = 'precog-speculation-rules';
      script.type = 'speculationrules';
      document.head.appendChild(script);
    }
    script.textContent = JSON.stringify({
      prefetch: [
        {
          source: 'list',
          urls: validUrls,
          eagerness: 'moderate'
        }
      ]
    });
  }

  // Intercept window.fetch to serve from browser CacheStorage with 0ms latency
  const originalFetch = window.fetch;
  window.fetch = async function(resource, init) {
    const method = (init && init.method) ? init.method.toUpperCase() : 'GET';
    const url = typeof resource === 'string' ? resource : (resource && resource.url ? resource.url : '');

    // Check if this request is an internal speculative prefetch
    const isSpec = init && init.headers && (
      (init.headers['X-Precog-Speculative'] === 'true') ||
      (typeof init.headers.get === 'function' && init.headers.get('X-Precog-Speculative') === 'true')
    );

    // Only record real user interactions for training; never record speculative background fetches
    if (url && !isSpec) {
      record(method, url);
    }

    // Only serve GET requests from cache
    if (method === 'GET' && precogCache && url) {
      const cached = await precogCache.match(resource);
      if (cached) {
        // Evict one-time speculative hit
        precogCache.delete(resource);

        // Construct response with X-Cache: HIT
        const newHeaders = new Headers(cached.headers);
        newHeaders.set('X-Cache', 'HIT');
        newHeaders.set('X-Precog', 'browser-cache');
        const blob = await cached.blob();
        return new Response(blob, {
          status: cached.status,
          statusText: cached.statusText,
          headers: newHeaders
        });
      }
    }

    const resp = await originalFetch.apply(this, arguments);

    // If this was an explicit speculative prefetch, save into CacheStorage for instant retrieval
    // strictly only when response is public/cacheable and has no Set-Cookie
    if (isSpec && precogCache && method === 'GET' && resp && resp.ok && url) {
      const cc = (resp.headers.get('Cache-Control') || '').toLowerCase();
      if (!cc.includes('no-store') && !cc.includes('private') && !resp.headers.has('Set-Cookie')) {
        try {
          await precogCache.put(resource, resp.clone());
        } catch (err) {}
      }
    }

    return resp;
  };

  // Hover-intent listener with 65ms debounce to avoid triggering on cursor pass-through
  let hoverTimer = null;
  let hoveredAnchor = null;

  document.addEventListener('mouseover', (e) => {
    const a = e.target.closest('a');
    if (!a || !a.href || a.origin !== window.location.origin) {
      if (hoverTimer) { clearTimeout(hoverTimer); hoverTimer = null; }
      hoveredAnchor = null;
      return;
    }
    // Respect opt-out attributes and nofollow links
    if (a.hasAttribute('data-precog-skip') || a.getAttribute('rel') === 'nofollow') {
      return;
    }
    if (a === hoveredAnchor) return;
    if (hoverTimer) clearTimeout(hoverTimer);

    hoveredAnchor = a;
    hoverTimer = setTimeout(() => {
      record('GET', a.pathname + a.search);
      hoverTimer = null;
    }, 65);
  }, { passive: true });

  document.addEventListener('mouseout', (e) => {
    const a = e.target.closest('a');
    if (a && a === hoveredAnchor) {
      if (hoverTimer) { clearTimeout(hoverTimer); hoverTimer = null; }
      hoveredAnchor = null;
    }
  }, { passive: true });

  window.addEventListener('popstate', () => {
    record('GET', window.location.pathname + window.location.search);
  });

  // Export public Precog API on window
  window.Precog = {
    record,
    predict: getPredictions,
    clearCache: async () => {
      if (precogCache) {
        await caches.delete(CACHE_NAME);
        precogCache = await caches.open(CACHE_NAME);
      }
    },
    // reset clears all navigation history, fallback transitions, and CacheStorage (useful on user logout)
    reset: async () => {
      lastRoute = null;
      fallbackTransitions.clear();
      if (precogCache) {
        await caches.delete(CACHE_NAME);
        precogCache = await caches.open(CACHE_NAME);
      }
    }
  };

  // Record initial page load
  record('GET', window.location.pathname + window.location.search);

})(typeof window !== 'undefined' ? window : this);

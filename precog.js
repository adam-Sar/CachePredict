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
      try {
        const has = await precogCache.match(url);
        if (!has) {
          const resp = await fetch(url, {
            headers: { 'X-Precog-Speculative': 'true' },
            priority: 'low'
          });
          if (resp && resp.ok) {
            await precogCache.put(url, resp);
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
          urls: urls,
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

    // Record interaction for real-time model training
    if (url) {
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
    const isSpec = init && init.headers && (
      (init.headers['X-Precog-Speculative'] === 'true') ||
      (typeof init.headers.get === 'function' && init.headers.get('X-Precog-Speculative') === 'true')
    );
    if (isSpec && precogCache && method === 'GET' && resp && resp.ok && url) {
      try {
        await precogCache.put(resource, resp.clone());
      } catch (err) {}
    }

    return resp;
  };

  // Intercept link clicks / hovers for predictive navigation
  document.addEventListener('mouseover', (e) => {
    const a = e.target.closest('a');
    if (a && a.href && a.origin === window.location.origin) {
      record('GET', a.pathname + a.search);
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
      if (precogCache) await caches.delete(CACHE_NAME);
    }
  };

  // Record initial page load
  record('GET', window.location.pathname + window.location.search);

})(typeof window !== 'undefined' ? window : this);

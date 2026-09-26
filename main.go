package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/dgraph-io/ristretto"
	"github.com/joho/godotenv"
	"github.com/labstack/echo/v5"
)

func main() {
	_ = godotenv.Load() // .env is optional; real env wins if set

	cfg, err := LoadConfig()
	if err != nil {
		log.Fatal(err)
	}
	store, err := NewStore(cfg.SupabaseURL, cfg.SupabaseKey)
	if err != nil {
		log.Fatal(err)
	}
	predictor, err := NewPredictor(cfg.ONNXModelPath, cfg.TokenizerPath)
	if err != nil {
		log.Fatal(err)
	}
	defer predictor.Close()
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: int64(cfg.CacheMaxEntries) * 10,
		MaxCost:     int64(cfg.CacheMaxEntries) * 64,
		BufferItems: 64,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer cache.Close()
	sessions := NewSessionStore(cfg.SessionMaxHist, cfg.SessionMaxCount)
	cart := NewCartStore()
	wishlist := NewWishlistStore()

	registry := NewPrefetchRegistry()
	registry.Register("GET", "/products", func(ctx context.Context, query string) ([]byte, error) {
		vals, _ := url.ParseQuery(query)
		if idStr := vals.Get("id"); idStr != "" {
			id, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil {
				return nil, err
			}
			p, err := store.GetProduct(ctx, id)
			if err != nil {
				return nil, err
			}
			return json.Marshal(map[string]ProductWithImageURL{"product": p.WithImageURL(cfg.ImageBaseURL)})
		}
		products, err := store.ListProducts(ctx)
		if err != nil {
			return nil, err
		}
		output := make([]ProductWithImageURL, 0, len(products))
		for _, p := range products {
			output = append(output, p.WithImageURL(cfg.ImageBaseURL))
		}
		return json.Marshal(map[string]any{"products": output, "count": len(output)})
	})
	registry.Register("GET", "/products/filter", func(ctx context.Context, query string) ([]byte, error) {
		return json.Marshal(map[string]any{"filters": map[string]any{}})
	})
	registry.Register("GET", "/home", func(ctx context.Context, query string) ([]byte, error) {
		return json.Marshal(map[string]any{
			"hero":    "New season. Cached.",
			"featured": []any{},
			"sections": []map[string]string{},
		})
	})
	registry.Register("GET", "/search", func(ctx context.Context, query string) ([]byte, error) {
		vals, _ := url.ParseQuery(query)
		return json.Marshal(map[string]any{
			"query":    vals.Get("q"),
			"products": []any{},
			"count":    0,
		})
	})
	registry.Register("GET", "/category", func(ctx context.Context, query string) ([]byte, error) {
		vals, _ := url.ParseQuery(query)
		return json.Marshal(map[string]any{
			"category": vals.Get("type"),
			"products": []any{},
			"count":    0,
		})
	})
	registry.Register("GET", "/reviews", func(ctx context.Context, query string) ([]byte, error) {
		vals, _ := url.ParseQuery(query)
		return json.Marshal(map[string]any{
			"product_id":      vals.Get("product_id"),
			"average_rating":  4.6,
			"review_count":    0,
			"reviews":         []any{},
			"would_recommend": 0.92,
		})
	})
	registry.Register("GET", "/cart", func(ctx context.Context, query string) ([]byte, error) {
		return json.Marshal(map[string]any{"items": []any{}, "total": 0, "count": 0})
	})

	e := echo.New()
	e.GET("/api/activity", RecentHandler)
	e.Use(PrefetchMiddleware(predictor, cache, registry, sessions, cfg.CacheTTL, cfg.CookieSecure))
	e.GET("/products", ListProductsHandler(store, cfg.ImageBaseURL))
	e.GET("/products/filter", FilterProductsPageHandler())
	e.POST("/products/filter", FilterProductsHandler(store, cfg.ImageBaseURL))
	e.GET("/home", HomeHandler(store, cfg.ImageBaseURL))
	e.GET("/search", SearchHandler(store, cfg.ImageBaseURL))
	e.GET("/category", CategoryHandler(store, cfg.ImageBaseURL))
	e.GET("/reviews", ReviewsHandler)
	e.GET("/cart", GetCartHandler(cart, store, cfg.ImageBaseURL))
	e.POST("/cart", AddToCartHandler(cart))
	e.GET("/checkout", CheckoutHandler(cart, store, cfg.ImageBaseURL))
	e.POST("/payment", PaymentHandler(cart))
	e.GET("/wishlist", GetWishlistHandler(wishlist, store, cfg.ImageBaseURL))
	e.POST("/wishlist", AddWishlistItem(wishlist))
	e.GET("/healthz", HealthHandler)

	// Run the server in a goroutine so we can intercept shutdown signals
	// and let in-flight requests drain via Echo's graceful shutdown.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	sc := echo.StartConfig{
		Address:         cfg.Port,
		GracefulTimeout: cfg.ShutdownTimeout,
		OnShutdownError: func(err error) {
			log.Printf("shutdown: %v", err)
		},
	}
	if err := sc.Start(ctx, e); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

package main

import (
	"context"
	"encoding/json"
	"log"

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
	if err != nil { log.Fatal(err) }
	defer cache.Close()
	sessions := NewSessionStore(8)

	registry := NewPrefetchRegistry()
	registry.Register("GET", "/products", func(ctx context.Context) ([]byte, error) {
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
	registry.Register("GET", "/products/filter", func(ctx context.Context) ([]byte, error) {
		return json.Marshal(map[string]any{"filters": map[string]any{}})
	})

	e := echo.New()
	e.Use(PrefetchMiddleware(predictor, cache, registry, sessions, cfg.CacheTTL))
	e.GET("/products",         ListProductsHandler(store, cfg.ImageBaseURL))
	e.GET("/products/filter",  FilterProductsPageHandler(store))
	e.POST("/products/filter", FilterProductsHandler(store, cfg.ImageBaseURL))
	e.GET("/healthz",HealthHandler)
	if err := e.Start(cfg.Port); err != nil {
		log.Fatal(err)
	}
}
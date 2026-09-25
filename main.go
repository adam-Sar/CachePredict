package main

import (
	"log"

	"github.com/dgraph-io/ristretto"
	"github.com/labstack/echo/v5"
)

func main() {


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
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: int64(cfg.CacheMaxEntries) * 10,
		MaxCost:     int64(cfg.CacheMaxEntries) * 64,
		BufferItems: 64,
	})
	if err != nil { log.Fatal(err) }
	defer cache.Close()
	sessions := NewSessionStore(8)
	e := echo.New()
	e.Use(PrefetchMiddleware(predictor, cache, sessions, cfg.CacheTTL))
	e.GET("/products",         ListProductsHandler(store, cfg.ImageBaseURL))
	e.GET("/products/filter",  FilterProductsPageHandler(store))
	e.POST("/products/filter", FilterProductsHandler(store, cfg.ImageBaseURL))
	e.GET("/healthz",HealthHandler)
	e.Start(cfg.Port)
}
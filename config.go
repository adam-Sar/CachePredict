package main

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	SupabaseURL     string
	SupabaseKey     string
	ImageBaseURL    string
	ONNXModelPath   string
	TokenizerPath   string
	CacheTTL        time.Duration
	CacheMaxEntries int
	Port            string
	SessionMaxHist  int
	SessionMaxCount int
	CookieSecure    bool
	ShutdownTimeout time.Duration
}

func LoadConfig() (*Config, error) {
	cfg := &Config{
		SupabaseURL:     os.Getenv("SUPABASE_URL"),
		SupabaseKey:     os.Getenv("SUPABASE_KEY"),
		ImageBaseURL:    os.Getenv("IMAGE_BASE_URL"),
		ONNXModelPath:   getenv("ONNX_MODEL_PATH", "cache_predict_model.onnx"),
		TokenizerPath:   getenv("TOKENIZER_PATH", "tokenizer.json"),
		CacheTTL:        parseCacheTTL(getenv("CACHE_TTL", "60s")),
		CacheMaxEntries: parseInt(getenv("CACHE_MAX", "500"), 500),
		Port:            getenv("PORT", ":1234"),
		SessionMaxHist:  parseInt(getenv("SESSION_MAX_HIST", "8"), 8),
		SessionMaxCount: parseInt(getenv("SESSION_MAX_COUNT", "10000"), 10000),
		CookieSecure:    parseBool(getenv("COOKIE_SECURE", "false")),
		ShutdownTimeout: parseCacheTTL(getenv("SHUTDOWN_TIMEOUT", "10s")),
	}
	if cfg.SupabaseURL == "" {
		return nil, errors.New("SUPABASE_URL is required")
	}
	if cfg.SupabaseKey == "" {
		return nil, errors.New("SUPABASE_KEY is required")
	}
	if cfg.ImageBaseURL == "" {
		return nil, errors.New("IMAGE_BASE_URL is required")
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseCacheTTL(s string) time.Duration {
	if d, err := time.ParseDuration(s); err == nil {
		return d
	}
	if n, err := strconv.Atoi(s); err == nil {
		return time.Duration(n) * time.Second
	}
	return 60 * time.Second
}

func parseInt(s string, fallback int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return fallback
}

func parseBool(s string) bool {
	switch strings.ToLower(s) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

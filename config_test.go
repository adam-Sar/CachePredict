package main

import (
	"bufio"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLoadConfig(t *testing.T) {
	cleanup := loadEnvFile(t, ".env")
	defer cleanup()

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.SupabaseURL != "https://wodxueleyrgukeewrxve.supabase.co" {
		t.Errorf("SupabaseURL = %q", cfg.SupabaseURL)
	}
	if cfg.SupabaseKey == "" {
		t.Error("SupabaseKey is empty")
	}
	if cfg.ImageBaseURL != "d3qtqd30ynvals.cloudfront.net" {
		t.Errorf("ImageBaseURL = %q", cfg.ImageBaseURL)
	}
	if cfg.ONNXModelPath != "cache_predict_model.onnx" {
		t.Errorf("ONNXModelPath = %q", cfg.ONNXModelPath)
	}
	if cfg.TokenizerPath != "tokenizer.json" {
		t.Errorf("TokenizerPath = %q", cfg.TokenizerPath)
	}
	if cfg.CacheTTL != 60*time.Second {
		t.Errorf("CacheTTL = %v, want 60s", cfg.CacheTTL)
	}
	if cfg.CacheMaxEntries != 500 {
		t.Errorf("CacheMaxEntries = %d", cfg.CacheMaxEntries)
	}
	if cfg.Port != ":1234" {
		t.Errorf("Port = %q", cfg.Port)
	}
}

func TestLoadConfigMissingRequired(t *testing.T) {
	os.Unsetenv("SUPABASE_URL")
	os.Unsetenv("SUPABASE_KEY")
	os.Unsetenv("IMAGE_BASE_URL")

	if _, err := LoadConfig(); err == nil {
		t.Error("expected error when required vars missing")
	}
}

func loadEnvFile(t *testing.T, path string) func() {
	f, err := os.Open(path)
	if err != nil {
		t.Skipf("env file not found: %s", path)
		return func() {}
	}
	defer f.Close()

	var keys []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		os.Setenv(key, val)
		keys = append(keys, key)
	}
	return func() {
		for _, k := range keys {
			os.Unsetenv(k)
		}
	}
}
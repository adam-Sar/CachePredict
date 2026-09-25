package main

import (
	"bufio"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func loadEnv(t *testing.T) func() {
	f, err := os.Open(".env")
	if err != nil {
		t.Skipf(".env not found: %v", err)
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

func newTestStore(t *testing.T) *Store {
	cleanup := loadEnv(t)
	t.Cleanup(cleanup)
	store, err := NewStore(os.Getenv("SUPABASE_URL"), os.Getenv("SUPABASE_KEY"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

func TestStoreListProducts(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	products, err := store.ListProducts(ctx)
	if err != nil {
		t.Fatalf("ListProducts: %v", err)
	}
	if len(products) != 16 {
		t.Errorf("ListProducts returned %d products, want 16", len(products))
	}
	for i := 1; i < len(products); i++ {
		if products[i].ID < products[i-1].ID {
			t.Errorf("products not sorted by id at index %d: %d < %d", i, products[i].ID, products[i-1].ID)
		}
	}
	if products[0].ImagePath == "" || products[0].Name == "" || products[0].Price == 0 {
		t.Errorf("first product missing fields: %+v", products[0])
	}
}

func TestStoreGetProduct(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	p, err := store.GetProduct(ctx, 44)
	if err != nil {
		t.Fatalf("GetProduct(44): %v", err)
	}
	if p.ID != 44 {
		t.Errorf("id = %d, want 44", p.ID)
	}
	if p.Name != "A-Line Midi Skirt" {
		t.Errorf("name = %q, want %q", p.Name, "A-Line Midi Skirt")
	}
	if p.ImagePath != "Women/44.jpg" {
		t.Errorf("image_path = %q, want %q", p.ImagePath, "Women/44.jpg")
	}
}

func TestStoreGetProductNotFound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_, err := store.GetProduct(ctx, 999999)
	if !errors.Is(err, ErrProductNotFound) {
		t.Errorf("expected ErrProductNotFound, got %v", err)
	}
}

func TestStoreFilterProducts(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	t.Run("by name substring", func(t *testing.T) {
		skirt, err := store.FilterProducts(ctx, FilterOptions{NameSubstr: "skirt"})
		if err != nil {
			t.Fatalf("FilterProducts: %v", err)
		}
		if len(skirt) != 3 {
			t.Errorf("got %d skirts, want 3 (got: %+v)", len(skirt), skirt)
		}
	})

	t.Run("by price range", func(t *testing.T) {
		min, max := 100.0, 200.0
		mid, err := store.FilterProducts(ctx, FilterOptions{MinPrice: &min, MaxPrice: &max})
		if err != nil {
			t.Fatalf("FilterProducts: %v", err)
		}
		if len(mid) == 0 {
			t.Error("expected at least one product in $100-$200 range")
		}
		for _, p := range mid {
			if p.Price < min || p.Price > max {
				t.Errorf("%s at $%.2f outside [$%.2f, $%.2f]", p.Name, p.Price, min, max)
			}
		}
	})

	t.Run("with limit", func(t *testing.T) {
		limited, err := store.FilterProducts(ctx, FilterOptions{Limit: 2})
		if err != nil {
			t.Fatalf("FilterProducts: %v", err)
		}
		if len(limited) != 2 {
			t.Errorf("got %d, want 2", len(limited))
		}
	})
}

func TestProductWithImageURL(t *testing.T) {
	p := Product{ID: 44, Name: "A-Line Midi Skirt", ImagePath: "Women/44.jpg"}

	tests := []struct {
		name string
		base string
		want string
	}{
		{"no protocol", "d3qtqd30ynvals.cloudfront.net", "https://d3qtqd30ynvals.cloudfront.net/Women/44.jpg"},
		{"with https", "https://cdn.example.com", "https://cdn.example.com/Women/44.jpg"},
		{"with trailing slash", "cdn.example.com/", "https://cdn.example.com/Women/44.jpg"},
		{"with http", "http://localhost:8080", "http://localhost:8080/Women/44.jpg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.WithImageURL(tt.base).ImageURL
			if got != tt.want {
				t.Errorf("WithImageURL(%q).ImageURL = %q, want %q", tt.base, got, tt.want)
			}
		})
	}
}

func TestNewStoreValidation(t *testing.T) {
	if _, err := NewStore("", "key"); err == nil {
		t.Error("expected error for empty baseURL")
	}
	if _, err := NewStore("https://x.supabase.co", ""); err == nil {
		t.Error("expected error for empty apiKey")
	}
}

func TestBuildFilterQuery(t *testing.T) {
	min, max := 100.0, 200.0
	tests := []struct {
		name string
		opts FilterOptions
		want string
	}{
		{"empty", FilterOptions{}, "select=*&order=id"},
		{"name substring", FilterOptions{NameSubstr: "skirt"}, "select=*&order=id&name=ilike.*skirt*"},
		{"name with special chars", FilterOptions{NameSubstr: "a&b=c"}, "select=*&order=id&name=ilike.*a%26b%3Dc*"},
		{"min price", FilterOptions{MinPrice: &min}, "select=*&order=id&price=gte.100"},
		{"max price", FilterOptions{MaxPrice: &max}, "select=*&order=id&price=lte.200"},
		{"price range", FilterOptions{MinPrice: &min, MaxPrice: &max}, "select=*&order=id&price=gte.100&price=lte.200"},
		{"limit only", FilterOptions{Limit: 5}, "select=*&order=id&limit=5"},
		{
			"all combined",
			FilterOptions{NameSubstr: "skirt", MinPrice: &min, MaxPrice: &max, Limit: 2},
			"select=*&order=id&name=ilike.*skirt*&price=gte.100&price=lte.200&limit=2",
		},
		{"limit zero ignored", FilterOptions{Limit: 0}, "select=*&order=id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildFilterQuery(tt.opts)
			if got != tt.want {
				t.Errorf("buildFilterQuery = %q, want %q", got, tt.want)
			}
		})
	}
}

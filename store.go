package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Product struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	ImagePath   string  `json:"image_path"`
	Price       float64 `json:"price"`
	Description string  `json:"description"`
}

type ProductWithImageURL struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	ImagePath   string  `json:"image_path"`
	ImageURL    string  `json:"image_url"`
	Price       float64 `json:"price"`
	Description string  `json:"description"`
}

func (p Product) WithImageURL(base string) ProductWithImageURL {
	base = strings.TrimRight(base, "/")
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "https://" + base
	}
	return ProductWithImageURL{
		ID:          p.ID,
		Name:        p.Name,
		ImagePath:   p.ImagePath,
		ImageURL:    base + "/" + p.ImagePath,
		Price:       p.Price,
		Description: p.Description,
	}
}

type FilterOptions struct {
	NameSubstr string   `json:"name_substr"`
	MinPrice   *float64 `json:"min_price,omitempty"`
	MaxPrice   *float64 `json:"max_price,omitempty"`
	Limit      int      `json:"limit,omitempty"`
}

var ErrProductNotFound = errors.New("product not found")

type Store struct {
	apiBase string
	apiKey  string
	http    *http.Client
}

func NewStore(baseURL, apiKey string) (*Store, error) {
	if baseURL == "" {
		return nil, errors.New("baseURL is required")
	}
	if apiKey == "" {
		return nil, errors.New("apiKey is required")
	}
	return &Store{
		apiBase: strings.TrimRight(baseURL, "/") + "/rest/v1",
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (s *Store) doRequest(ctx context.Context, method, path, query string, out any) error {
	u := s.apiBase + path
	if query != "" {
		u += "?" + query
	}
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("apikey", s.apiKey)
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return ErrProductNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("supabase %d %s: %s", resp.StatusCode, http.StatusText(resp.StatusCode), string(body))
	}

	if out != nil && len(body) > 0 {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("decode: %w", err)
		}
	}
	return nil
}

func (s *Store) ListProducts(ctx context.Context) ([]Product, error) {
	var result []Product
	if err := s.doRequest(ctx, http.MethodGet, "/products", "select=*&order=id", &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) GetProduct(ctx context.Context, id int64) (*Product, error) {
	var result []Product
	q := fmt.Sprintf("id=eq.%d&select=*", id)
	if err := s.doRequest(ctx, http.MethodGet, "/products", q, &result); err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, ErrProductNotFound
	}
	return &result[0], nil
}

func (s *Store) FilterProducts(ctx context.Context, opts FilterOptions) ([]Product, error) {
	var result []Product
	if err := s.doRequest(ctx, http.MethodGet, "/products", buildFilterQuery(opts), &result); err != nil {
		return nil, err
	}
	return result, nil
}

// SearchProducts does a free-text ilike search over name and description.
func (s *Store) SearchProducts(ctx context.Context, query string) ([]Product, error) {
	if query == "" {
		return s.ListProducts(ctx)
	}
	var result []Product
	q := url.QueryEscape(query)
	filter := fmt.Sprintf("select=*&order=id&or=(name.ilike.*%s*,description.ilike.*%s*)", q, q)
	if err := s.doRequest(ctx, http.MethodGet, "/products", filter, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// ListByCategory filters products whose description mentions category. Real
// catalogues would have a category_id column; for the demo this is good enough.
func (s *Store) ListByCategory(ctx context.Context, category string) ([]Product, error) {
	if category == "" {
		return s.ListProducts(ctx)
	}
	var result []Product
	q := url.QueryEscape(category)
	filter := fmt.Sprintf("select=*&order=id&description=ilike.*%s*", q)
	if err := s.doRequest(ctx, http.MethodGet, "/products", filter, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// buildFilterQuery assembles a PostgREST query string that pushes filtering,
// price range, and limit down to Supabase instead of doing them client-side.
// It is the canonical place to translate FilterOptions into the request URL.
func buildFilterQuery(opts FilterOptions) string {
	var parts []string
	parts = append(parts, "select=*", "order=id")
	if opts.NameSubstr != "" {
		// ilike.*X* is case-insensitive substring match. Escape the user
		// input so '%', '_', '&', etc. don't break out of the wildcard.
		parts = append(parts, "name=ilike.*"+url.QueryEscape(opts.NameSubstr)+"*")
	}
	if opts.MinPrice != nil {
		parts = append(parts, "price=gte."+strconv.FormatFloat(*opts.MinPrice, 'f', -1, 64))
	}
	if opts.MaxPrice != nil {
		parts = append(parts, "price=lte."+strconv.FormatFloat(*opts.MaxPrice, 'f', -1, 64))
	}
	if opts.Limit > 0 {
		parts = append(parts, "limit="+strconv.Itoa(opts.Limit))
	}
	return strings.Join(parts, "&")
}

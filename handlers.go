package main

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v5"
)

// ListProductsHandler returns GET /products. With ?id=N it returns a single
// product wrapped as {"product": {...}} (404 if absent, 400 on bad id);
// otherwise it returns {"products": [...], "count": N}.
func ListProductsHandler(store *Store, imageBase string) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if idStr := c.QueryParam("id"); idStr != "" {
			id, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil {
				return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
			}
			p, err := store.GetProduct(c.Request().Context(), id)
			if err != nil {
				if errors.Is(err, ErrProductNotFound) {
					return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
				}
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
			}
			return c.JSON(http.StatusOK, map[string]ProductWithImageURL{
				"product": p.WithImageURL(imageBase),
			})
		}

		products, err := store.ListProducts(c.Request().Context())
		if err != nil {
			return c.JSON(500, map[string]string{"error": err.Error()})
		}
		output := make([]ProductWithImageURL, 0, len(products))
		for _, prod := range products {
			output = append(output, prod.WithImageURL(imageBase))
		}
		return c.JSON(http.StatusOK, map[string]any{
			"products": output,
			"count":    len(output),
		})
	}
}

// FilterProductsHandler returns POST /products/filter. Body fields:
// name_substr, min_price?, max_price?, limit?. Responds with
// {"products": [...], "count": N}; 400 on a bad body, 500 on store error.
func FilterProductsHandler(store *Store, imageBase string) echo.HandlerFunc {
	return func(c *echo.Context) error {
		opts := FilterOptions{}
		if err := c.Bind(&opts); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		products, err := store.FilterProducts(c.Request().Context(), opts)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		output := make([]ProductWithImageURL, 0, len(products))
		for _, prod := range products {
			output = append(output, prod.WithImageURL(imageBase))
		}
		return c.JSON(http.StatusOK, map[string]any{"products": output, "count": len(output)})
	}
}

// FilterProductsPageHandler returns GET /products/filter — a placeholder
// response with the available filter fields. No body is read.
func FilterProductsPageHandler() echo.HandlerFunc {
	return func(c *echo.Context) error {
		return c.JSON(http.StatusOK, map[string]any{"filters": map[string]any{}})
	}
}

// HealthHandler handles GET /healthz. Returns {"status": "ok"}.
func HealthHandler(c *echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func ensureSID(c *echo.Context) string {
	sid := sessionIDFromRequest(c)
	if sid == "" {
		sid = NewSessionID()
		writeSessionCookie(c, sid, false)
	}
	return sid
}

// HomeHandler handles GET /home. Returns a small featured-products set plus
// section labels so the demo home page has real content.
func HomeHandler(store *Store, imageBase string) echo.HandlerFunc {
	return func(c *echo.Context) error {
		products, err := store.ListProducts(c.Request().Context())
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		featured := products
		if len(featured) > 4 {
			featured = featured[:4]
		}
		out := make([]ProductWithImageURL, 0, len(featured))
		for _, p := range featured {
			out = append(out, p.WithImageURL(imageBase))
		}
		return c.JSON(http.StatusOK, map[string]any{
			"hero":    "New season. Cached.",
			"featured": out,
			"sections": []map[string]string{
				{"id": "trending", "title": "Trending now"},
				{"id": "new", "title": "New arrivals"},
				{"id": "bestsellers", "title": "Best sellers"},
			},
		})
	}
}

// SearchHandler handles GET /search?q=...
func SearchHandler(store *Store, imageBase string) echo.HandlerFunc {
	return func(c *echo.Context) error {
		q := c.QueryParam("q")
		products, err := store.SearchProducts(c.Request().Context(), q)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		out := make([]ProductWithImageURL, 0, len(products))
		for _, p := range products {
			out = append(out, p.WithImageURL(imageBase))
		}
		return c.JSON(http.StatusOK, map[string]any{
			"query":    q,
			"products": out,
			"count":    len(out),
		})
	}
}

// CategoryHandler handles GET /category?type=...
func CategoryHandler(store *Store, imageBase string) echo.HandlerFunc {
	return func(c *echo.Context) error {
		t := c.QueryParam("type")
		products, err := store.ListByCategory(c.Request().Context(), t)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		out := make([]ProductWithImageURL, 0, len(products))
		for _, p := range products {
			out = append(out, p.WithImageURL(imageBase))
		}
		return c.JSON(http.StatusOK, map[string]any{
			"category": t,
			"products": out,
			"count":    len(out),
		})
	}
}

// ReviewsHandler handles GET /reviews?product_id=N. Real reviews would come
// from a reviews table; for the demo we return an empty list with summary
// metadata so the UI has something to render.
func ReviewsHandler(c *echo.Context) error {
	pid := c.QueryParam("product_id")
	return c.JSON(http.StatusOK, map[string]any{
		"product_id":      pid,
		"average_rating":  4.6,
		"review_count":    0,
		"reviews":         []map[string]any{},
		"would_recommend": 0.92,
	})
}

// GetCartHandler returns the caller's cart hydrated with product info and total.
func GetCartHandler(cart *CartStore, store *Store, imageBase string) echo.HandlerFunc {
	return func(c *echo.Context) error {
		sid := ensureSID(c)
		items := cart.List(sid)
		if len(items) == 0 {
			return c.JSON(http.StatusOK, map[string]any{"items": []any{}, "total": 0, "count": 0})
		}
		ids := make([]int64, len(items))
		for i, it := range items {
			ids[i] = it.ProductID
		}
		prods, err := store.GetProducts(c.Request().Context(), ids)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		type hydrated struct {
			ProductID int64   `json:"product_id"`
			Name      string  `json:"name"`
			Price     float64 `json:"price"`
			ImageURL  string  `json:"image_url"`
			Quantity  int     `json:"quantity"`
		}
		out := make([]hydrated, 0, len(items))
		var total float64
		for _, it := range items {
			p, ok := prods[it.ProductID]
			if !ok {
				continue
			}
			pp := p.WithImageURL(imageBase)
			out = append(out, hydrated{
				ProductID: it.ProductID,
				Name:      pp.Name,
				Price:     pp.Price,
				ImageURL:  pp.ImageURL,
				Quantity:  it.Quantity,
			})
			total += pp.Price * float64(it.Quantity)
		}
		return c.JSON(http.StatusOK, map[string]any{
			"items": out,
			"total": total,
			"count": len(out),
		})
	}
}

// AddToCartHandler handles POST /cart with body {product_id, quantity?}.
func AddToCartHandler(cart *CartStore) echo.HandlerFunc {
	return func(c *echo.Context) error {
		sid := ensureSID(c)
		var body struct {
			ProductID int64 `json:"product_id"`
			Quantity  int   `json:"quantity"`
		}
		if err := c.Bind(&body); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		if body.ProductID == 0 {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "product_id required"})
		}
		cart.Add(sid, body.ProductID, body.Quantity)
		return c.JSON(http.StatusOK, map[string]string{"status": "added"})
	}
}

// CheckoutHandler handles GET /checkout. Returns line items, subtotal, shipping, total.
func CheckoutHandler(cart *CartStore, store *Store, imageBase string) echo.HandlerFunc {
	return func(c *echo.Context) error {
		sid := ensureSID(c)
		items := cart.List(sid)
		type line struct {
			Name     string  `json:"name"`
			Price    float64 `json:"price"`
			Quantity int     `json:"quantity"`
			Subtotal float64 `json:"subtotal"`
		}
		if len(items) == 0 {
			return c.JSON(http.StatusOK, map[string]any{
				"lines": []any{}, "subtotal": 0, "shipping": 0, "total": 0, "currency": "USD",
			})
		}
		ids := make([]int64, len(items))
		for i, it := range items {
			ids[i] = it.ProductID
		}
		prods, err := store.GetProducts(c.Request().Context(), ids)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		lines := make([]line, 0, len(items))
		var subtotal float64
		for _, it := range items {
			p, ok := prods[it.ProductID]
			if !ok {
				continue
			}
			pp := p.WithImageURL(imageBase)
			sub := pp.Price * float64(it.Quantity)
			subtotal += sub
			lines = append(lines, line{
				Name:     pp.Name,
				Price:    pp.Price,
				Quantity: it.Quantity,
				Subtotal: sub,
			})
		}
		shipping := 0.0
		if subtotal > 0 && subtotal < 50 {
			shipping = 5.99
		}
		return c.JSON(http.StatusOK, map[string]any{
			"lines":    lines,
			"subtotal": subtotal,
			"shipping": shipping,
			"total":    subtotal + shipping,
			"currency": "USD",
		})
	}
}

// PaymentHandler handles POST /payment. Body: {amount, method}. Clears the cart
// on confirmed payment.
func PaymentHandler(cart *CartStore) echo.HandlerFunc {
	return func(c *echo.Context) error {
		sid := ensureSID(c)
		var body struct {
			Amount float64 `json:"amount"`
			Method string  `json:"method"`
		}
		if err := c.Bind(&body); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		cart.Clear(sid)
		return c.JSON(http.StatusOK, map[string]any{
			"status":   "confirmed",
			"order_id": fmt.Sprintf("ORD-%d", time.Now().Unix()),
			"amount":   body.Amount,
			"method":   body.Method,
		})
	}
}

// GetWishlistHandler returns the caller's wishlist.
func GetWishlistHandler(wish *WishlistStore, store *Store, imageBase string) echo.HandlerFunc {
	return func(c *echo.Context) error {
		sid := ensureSID(c)
		items := wish.List(sid)
		if len(items) == 0 {
			return c.JSON(http.StatusOK, map[string]any{"items": []any{}, "count": 0})
		}
		ids := make([]int64, len(items))
		for i, it := range items {
			ids[i] = it.ProductID
		}
		prods, err := store.GetProducts(c.Request().Context(), ids)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		type hydrated struct {
			ProductID int64   `json:"product_id"`
			Name      string  `json:"name"`
			Price     float64 `json:"price"`
			ImageURL  string  `json:"image_url"`
		}
		out := make([]hydrated, 0, len(items))
		for _, it := range items {
			p, ok := prods[it.ProductID]
			if !ok {
				continue
			}
			pp := p.WithImageURL(imageBase)
			out = append(out, hydrated{
				ProductID: it.ProductID,
				Name:      pp.Name,
				Price:     pp.Price,
				ImageURL:  pp.ImageURL,
			})
		}
		return c.JSON(http.StatusOK, map[string]any{
			"items": out,
			"count": len(out),
		})
	}
}

// AddWishlistItem handles POST /wishlist?product_id=N (also accepts body).
func AddWishlistItem(wish *WishlistStore) echo.HandlerFunc {
	return func(c *echo.Context) error {
		sid := ensureSID(c)
		var pid int64
		if v := c.QueryParam("product_id"); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid product_id"})
			}
			pid = n
		}
		var body struct {
			ProductID int64 `json:"product_id"`
		}
		_ = c.Bind(&body)
		if body.ProductID != 0 {
			pid = body.ProductID
		}
		if pid == 0 {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "product_id required"})
		}
		wish.Add(sid, pid)
		return c.JSON(http.StatusOK, map[string]string{"status": "added"})
	}
}

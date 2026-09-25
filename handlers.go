package main

import (
	"errors"
	"net/http"
	"strconv"

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

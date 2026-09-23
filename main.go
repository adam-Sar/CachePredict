package main

import (
	//"fmt"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"net/http"
)

type Product struct {
	ID *int `query:"id"` //*int so that if none was entered it'll be nil not 0
}

type Filter struct {
	Category string `json:"category"`
	Price    string `json:"price"`
	Gender   string `json:"gender"`
}



func main() {
	e := echo.New()
	e.Use(middleware.RequestLogger())
	e.Use(middleware.Recover())

	e.GET("/products", func(c *echo.Context) error {
		var product Product
		if err := c.Bind(&product); err != nil {
			return err
		}

		if product.ID == nil {
			return c.String(http.StatusOK,"products page") //enter products page
		}
		return c.JSON(http.StatusOK, map[string]int{"id": *product.ID}) //go to this product
	})

	e.GET("/products/filter", func(c *echo.Context) error {
		var filter Filter
		if err := echo.BindBody(c, &filter); err != nil {
			return err
		}

		if filter.Category == "" && filter.Gender == "" && filter.Price == "" {
			return c.String(http.StatusOK, "filter page")// enter filter dropdown
		}

		return c.JSON(http.StatusOK, map[string]string{"cat": filter.Category, "gender": filter.Gender, "price": filter.Price})// filter the products based on the filters and go back to products main page

	})
	e.Start(":1234")
}

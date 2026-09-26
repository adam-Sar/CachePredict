package main

import (
	"sync"
	"time"
)

// CartItem is one line of a session's shopping cart. Quantity is per-line.
type CartItem struct {
	ProductID int64 `json:"product_id"`
	Quantity  int   `json:"quantity"`
	AddedAt   int64 `json:"added_at"`
}

// WishlistItem is one product ID the caller has saved for later.
type WishlistItem struct {
	ProductID int64 `json:"product_id"`
	AddedAt   int64 `json:"added_at"`
}

type CartStore struct {
	mu    sync.RWMutex
	carts map[string][]CartItem
}

func NewCartStore() *CartStore {
	return &CartStore{carts: make(map[string][]CartItem)}
}

func (c *CartStore) List(sid string) []CartItem {
	c.mu.RLock()
	defer c.mu.RUnlock()
	src := c.carts[sid]
	out := make([]CartItem, len(src))
	copy(out, src)
	return out
}

func (c *CartStore) Add(sid string, productID int64, qty int) {
	if qty <= 0 {
		qty = 1
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	items := c.carts[sid]
	for i := range items {
		if items[i].ProductID == productID {
			items[i].Quantity += qty
			c.carts[sid] = items
			return
		}
	}
	items = append(items, CartItem{
		ProductID: productID,
		Quantity:  qty,
		AddedAt:   time.Now().Unix(),
	})
	c.carts[sid] = items
}

func (c *CartStore) Remove(sid string, productID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	src := c.carts[sid]
	out := src[:0]
	for _, it := range src {
		if it.ProductID != productID {
			out = append(out, it)
		}
	}
	c.carts[sid] = out
}

func (c *CartStore) Clear(sid string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.carts, sid)
}

type WishlistStore struct {
	mu        sync.RWMutex
	wishlists map[string][]WishlistItem
}

func NewWishlistStore() *WishlistStore {
	return &WishlistStore{wishlists: make(map[string][]WishlistItem)}
}

func (w *WishlistStore) List(sid string) []WishlistItem {
	w.mu.RLock()
	defer w.mu.RUnlock()
	src := w.wishlists[sid]
	out := make([]WishlistItem, len(src))
	copy(out, src)
	return out
}

func (w *WishlistStore) Add(sid string, productID int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	items := w.wishlists[sid]
	for _, it := range items {
		if it.ProductID == productID {
			return
		}
	}
	w.wishlists[sid] = append(items, WishlistItem{
		ProductID: productID,
		AddedAt:   time.Now().Unix(),
	})
}

func (w *WishlistStore) Remove(sid string, productID int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	src := w.wishlists[sid]
	out := src[:0]
	for _, it := range src {
		if it.ProductID != productID {
			out = append(out, it)
		}
	}
	w.wishlists[sid] = out
}
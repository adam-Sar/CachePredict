package precog_test

import (
	"fmt"
	"math"
	"sync"
	"testing"

	"github.com/adam-Sar/precog"
)

func TestDenseMatrixPredictorBasic(t *testing.T) {
	p := precog.NewDenseMatrixPredictor(&precog.DensePredictorConfig{
		MaxRoutes: 128,
	})

	session := "user_sess_1"

	// Simulate 8 transitions from /catalog to /item/42, and 2 transitions from /catalog to /cart
	for i := 0; i < 8; i++ {
		p.Record(session, "GET", "/catalog", "")
		p.Record(session, "GET", "/item/42", "")
	}
	for i := 0; i < 2; i++ {
		p.Record(session, "GET", "/catalog", "")
		p.Record(session, "GET", "/cart", "")
	}

	// Now session is at /catalog
	p.Record(session, "GET", "/catalog", "")

	preds := p.Predict(session, 2)
	if len(preds) != 2 {
		t.Fatalf("expected 2 predictions, got %d", len(preds))
	}

	// Top prediction should be /item/:id with probability 0.8
	if preds[0].Pattern != "GET /item/:id" {
		t.Errorf("expected top prediction GET /item/:id, got %s", preds[0].Pattern)
	}
	if math.Abs(preds[0].Probability-0.8) > 0.01 {
		t.Errorf("expected probability ~0.8, got %f", preds[0].Probability)
	}

	// Second prediction should be /cart with probability 0.2
	if preds[1].Pattern != "GET /cart" {
		t.Errorf("expected second prediction GET /cart, got %s", preds[1].Pattern)
	}
	if math.Abs(preds[1].Probability-0.2) > 0.01 {
		t.Errorf("expected probability ~0.2, got %f", preds[1].Probability)
	}
}

func TestDenseMatrixPredictorHighConcurrency(t *testing.T) {
	p := precog.NewDenseMatrixPredictor(&precog.DensePredictorConfig{
		MaxRoutes: 256,
	})

	var wg sync.WaitGroup
	workers := 64
	sessionsPerWorker := 10

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for s := 0; s < sessionsPerWorker; s++ {
				sid := fmt.Sprintf("session_%d_%d", workerID, s)
				p.Record(sid, "GET", "/search", "q=shoes")
				p.Record(sid, "GET", "/products/100", "")
				p.Record(sid, "POST", "/cart", "")
				p.Record(sid, "GET", "/checkout", "")

				preds := p.Predict(sid, 3)
				if len(preds) > 0 && preds[0].Probability < 0 {
					t.Errorf("invalid probability %f", preds[0].Probability)
				}
			}
		}(w)
	}

	wg.Wait()
}

func TestDenseMatrixPredictorUUIDAndHexHydration(t *testing.T) {
	p := precog.NewDenseMatrixPredictor(&precog.DensePredictorConfig{
		MaxRoutes: 128,
	})

	uuid := "123e4567-e89b-12d3-a456-426614174000"
	p.Record("sess_uuid", "GET", "/orders/"+uuid, "")
	p.Record("sess_uuid", "GET", "/orders/"+uuid+"/receipt", "")
	p.Record("sess_uuid", "GET", "/orders/"+uuid, "")

	preds := p.Predict("sess_uuid", 1)
	if len(preds) == 0 {
		t.Fatalf("expected predictions")
	}
	expectedCall := "GET /orders/" + uuid + "/receipt"
	if preds[0].Call != expectedCall {
		t.Errorf("expected hydrated UUID call %s, got %s", expectedCall, preds[0].Call)
	}

	hexID := "507f1f77bcf86cd799439011"
	p.Record("sess_hex", "GET", "/items/"+hexID, "")
	p.Record("sess_hex", "GET", "/items/"+hexID+"/details", "")
	p.Record("sess_hex", "GET", "/items/"+hexID, "")

	predsHex := p.Predict("sess_hex", 1)
	if len(predsHex) == 0 {
		t.Fatalf("expected predictions for hex")
	}
	expectedHexCall := "GET /items/" + hexID + "/details"
	if predsHex[0].Call != expectedHexCall {
		t.Errorf("expected hydrated Hex call %s, got %s", expectedHexCall, predsHex[0].Call)
	}
}

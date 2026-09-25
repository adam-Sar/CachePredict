package main

import (
	"sync"
	"testing"
)

func TestSessionStoreAddCall(t *testing.T) {
	store := NewSessionStore(8, 0)

	store.AddCall("alice", "GET /products")
	store.AddCall("alice", "GET /products?id=44")
	store.AddCall("alice", "GET /cart?product_id=44")

	got := store.Snapshot("alice")
	want := []string{"GET /products", "GET /products?id=44", "GET /cart?product_id=44"}
	if len(got) != len(want) {
		t.Fatalf("got %d calls, want %d", len(got), len(want))
	}
	for i, c := range want {
		if got[i] != c {
			t.Errorf("call %d = %q, want %q", i, got[i], c)
		}
	}
}

func TestSessionStoreTrimMaxHist(t *testing.T) {
	store := NewSessionStore(3, 0)

	for i := 0; i < 7; i++ {
		store.AddCall("bob", "call-"+string(rune('a'+i)))
	}

	got := store.Snapshot("bob")
	if len(got) != 3 {
		t.Fatalf("history length = %d, want 3 (got: %v)", len(got), got)
	}
	if got[0] != "call-e" || got[1] != "call-f" || got[2] != "call-g" {
		t.Errorf("trimmed wrong calls: %v", got)
	}
}

func TestSessionStoreGetOrCreateReuse(t *testing.T) {
	store := NewSessionStore(8, 0)

	a := store.GetOrCreate("alice")
	a.History = append(a.History, "manual push")

	b := store.GetOrCreate("alice")
	if a != b {
		t.Errorf("GetOrCreate returned different Session instances for same id")
	}
	if len(b.History) != 1 || b.History[0] != "manual push" {
		t.Errorf("history lost across GetOrCreate: %v", b.History)
	}
}

func TestSessionStoreIsolation(t *testing.T) {
	store := NewSessionStore(8, 0)

	store.AddCall("alice", "A1")
	store.AddCall("bob", "B1")
	store.AddCall("alice", "A2")

	aliceHist := store.Snapshot("alice")
	bobHist := store.Snapshot("bob")

	if len(aliceHist) != 2 || aliceHist[0] != "A1" || aliceHist[1] != "A2" {
		t.Errorf("alice history wrong: %v", aliceHist)
	}
	if len(bobHist) != 1 || bobHist[0] != "B1" {
		t.Errorf("bob history wrong: %v", bobHist)
	}
	if store.Count() != 2 {
		t.Errorf("Count = %d, want 2", store.Count())
	}
}

func TestSessionStoreSnapshotMissing(t *testing.T) {
	store := NewSessionStore(8, 0)
	if got := store.Snapshot("nobody"); got != nil {
		t.Errorf("Snapshot of missing id = %v, want nil", got)
	}
}

func TestSessionStoreConcurrent(t *testing.T) {
	store := NewSessionStore(100, 0)
	const goroutines = 50
	const callsEach = 20

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			for i := 0; i < callsEach; i++ {
				store.AddCall(id, "call")
			}
		}("session-" + string(rune('a'+g%26)))
	}
	wg.Wait()

	if store.Count() == 0 {
		t.Error("expected some sessions created")
	}
}

func TestSessionStoreLRUEviction(t *testing.T) {
	store := NewSessionStore(8, 2)

	store.AddCall("a", "A1")
	store.AddCall("b", "B1")
	store.AddCall("c", "C1") // should evict "a"

	if store.Count() != 2 {
		t.Errorf("Count = %d, want 2", store.Count())
	}
	if store.Snapshot("a") != nil {
		t.Error("expected 'a' to be evicted")
	}
	if store.Snapshot("b")[0] != "B1" {
		t.Error("expected 'b' to survive")
	}
	if store.Snapshot("c")[0] != "C1" {
		t.Error("expected 'c' to survive")
	}
}

func TestSessionStoreLRUTouchRefreshesOrder(t *testing.T) {
	store := NewSessionStore(8, 2)

	store.AddCall("a", "A1")
	store.AddCall("b", "B1")

	// Touch 'a' so 'b' is now the LRU.
	store.Snapshot("a")

	store.AddCall("c", "C1") // should evict 'b', not 'a'

	if store.Snapshot("a") == nil {
		t.Error("'a' should survive after being touched")
	}
	if store.Snapshot("b") != nil {
		t.Error("'b' should have been evicted as LRU")
	}
	if store.Snapshot("c") == nil {
		t.Error("'c' should survive")
	}
}

func TestNewSessionID(t *testing.T) {
	seen := make(map[string]bool, 1000)
	for i := 0; i < 1000; i++ {
		id := NewSessionID()
		if len(id) != 32 {
			t.Errorf("id length = %d, want 32", len(id))
		}
		if seen[id] {
			t.Errorf("duplicate session id: %q", id)
		}
		seen[id] = true
	}
}

func TestSnapshotIsCopy(t *testing.T) {
	store := NewSessionStore(8, 0)
	store.AddCall("alice", "A")

	snap := store.Snapshot("alice")
	snap[0] = "MUTATED"

	if store.Snapshot("alice")[0] != "A" {
		t.Error("Snapshot shares underlying slice with Session.History")
	}
}
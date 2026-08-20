package server

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLoginFailureLimiterBlocksAndResets(t *testing.T) {
	limiter := newLoginFailureLimiter(3, time.Minute, time.Minute)
	const key = "203.0.113.1\x00admin"
	for i := 0; i < 2; i++ {
		if !limiter.Allowed(key) {
			t.Fatalf("attempt %d blocked too early", i+1)
		}
		limiter.Failure(key)
	}
	limiter.Failure(key)
	if limiter.Allowed(key) {
		t.Fatal("threshold failures should block login")
	}
	limiter.Success(key)
	if !limiter.Allowed(key) {
		t.Fatal("success should clear failure history")
	}
}

func TestWebSocketConnectionQuota(t *testing.T) {
	s := &Server{wsByIP: map[string]int{}}
	for i := 0; i < maxWebSocketConnectionsPerIP; i++ {
		if !s.acquireWebSocket("203.0.113.1") {
			t.Fatalf("connection %d rejected too early", i+1)
		}
	}
	if s.acquireWebSocket("203.0.113.1") {
		t.Fatal("per-IP websocket quota should reject excess connection")
	}
	s.releaseWebSocket("203.0.113.1")
	if !s.acquireWebSocket("203.0.113.1") {
		t.Fatal("released websocket slot should be reusable")
	}
}

func TestDuplicatePlannedNodePreservesToken(t *testing.T) {
	factories := map[string]func(*testing.T) dataStore{
		"json": func(t *testing.T) dataStore {
			store, err := NewStore(filepath.Join(t.TempDir(), "server.json"))
			if err != nil {
				t.Fatal(err)
			}
			return store
		},
		"sqlite": func(t *testing.T) dataStore {
			store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "server.db"), "")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			return store
		},
	}
	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			store := factory(t)
			const nodeID = "n_duplicate_safe"
			const token = "existing-plaintext-token"
			if err := store.AddPlannedNode(nodeID, 10); err != nil {
				t.Fatal(err)
			}
			if err := store.SetNodeToken(nodeID, token, hashToken(token), 10); err != nil {
				t.Fatal(err)
			}
			if err := store.AddPlannedNode(nodeID, 10); err != nil {
				t.Fatal(err)
			}
			got, ok := store.GetNodeToken(nodeID)
			if !ok || got != token || !store.ValidNodeToken(nodeID, hashToken(token)) {
				t.Fatalf("duplicate add changed token: got=%q ok=%v", got, ok)
			}
		})
	}
}

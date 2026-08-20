package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestMarketEnabledDefaultsOnAndPersists(t *testing.T) {
	tests := []struct {
		name string
		make func(t *testing.T) dataStore
	}{
		{name: "json", make: func(t *testing.T) dataStore {
			store, err := NewStore(filepath.Join(t.TempDir(), "server.json"))
			if err != nil {
				t.Fatal(err)
			}
			return store
		}},
		{name: "sqlite", make: func(t *testing.T) dataStore {
			store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "server.db"), "")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			return store
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := tt.make(t)
			if !store.GetSettings().MarketEnabled {
				t.Fatal("market should be enabled by default")
			}
			settings := store.GetSettings()
			settings.MarketEnabled = false
			if err := store.UpdateSettings(settings); err != nil {
				t.Fatal(err)
			}
			if store.GetSettings().MarketEnabled {
				t.Fatal("market disabled setting was not persisted")
			}
		})
	}
}

func TestDisabledMarketRejectsPublicEndpoints(t *testing.T) {
	s := newTestServer(t)
	settings := s.store.GetSettings()
	settings.MarketEnabled = false
	if err := s.store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		handler http.HandlerFunc
	}{
		{name: "listings", handler: s.handleMarketListings},
		{name: "categories", handler: s.handleMarketCategories},
		{name: "captcha", handler: s.handleMarketCaptcha},
		{name: "ads", handler: s.handleMarketAds},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "https://monitor.example.com/api/market", nil)
			resp := httptest.NewRecorder()
			test.handler(resp, req)
			if resp.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d body = %s", resp.Code, resp.Body.String())
			}
		})
	}

	hash, err := hashOwnerPassword("account-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.CreateOwner("account@example.com", hash); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"email": "account@example.com", "password": "account-password"})
	loginReq := httptest.NewRequest(http.MethodPost, "https://monitor.example.com/api/account/login", bytes.NewReader(body))
	loginReq.Host = "monitor.example.com"
	loginResp := httptest.NewRecorder()
	s.handleOwnerLogin(loginResp, loginReq)
	if loginResp.Code != http.StatusOK {
		t.Fatalf("account login should remain available when market is disabled: status=%d body=%s", loginResp.Code, loginResp.Body.String())
	}
}

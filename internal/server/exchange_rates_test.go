package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestExchangeRateFetchCacheAndManualMode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":"success","base_code":"USD","time_last_update_unix":1784540000,"rates":{"CNY":7.25,"HKD":7.8,"EUR":0.86,"JPY":149.1}}`))
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "exchange-rate.json")
	service := &ExchangeRateService{
		path: path, endpoint: server.URL, client: &http.Client{Timeout: time.Second},
	}
	snapshot := service.Snapshot(defaultSettings())
	if snapshot.Rate != 7.25 || snapshot.Rates["HKD"] != 7.8 || snapshot.Stale {
		t.Fatalf("automatic snapshot = %#v", snapshot)
	}

	loaded := &ExchangeRateService{path: path, endpoint: "http://127.0.0.1:1", client: &http.Client{Timeout: time.Millisecond}}
	loaded.load()
	cached := loaded.Snapshot(defaultSettings())
	if cached.Rate != 7.25 {
		t.Fatalf("cached snapshot = %#v", cached)
	}

	settings := defaultSettings()
	settings.ExchangeRateMode = "manual"
	settings.ManualUSDCNY = 7.33
	manual := service.Snapshot(settings)
	if manual.Rate != 7.33 || manual.Source != "manual" || manual.Stale {
		t.Fatalf("manual snapshot = %#v", manual)
	}
}

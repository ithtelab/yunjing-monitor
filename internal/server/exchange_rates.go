package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const defaultExchangeRateURL = "https://open.er-api.com/v6/latest/USD"

type ExchangeRateSnapshot struct {
	Base      string             `json:"base"`
	Quote     string             `json:"quote"`
	Rate      float64            `json:"rate"`
	Rates     map[string]float64 `json:"rates,omitempty"`
	UpdatedAt int64              `json:"updated_at"`
	Source    string             `json:"source"`
	Stale     bool               `json:"stale"`
	TimeZone  string             `json:"time_zone,omitempty"`
}

type ExchangeRateService struct {
	mu          sync.Mutex
	path        string
	endpoint    string
	client      *http.Client
	cache       ExchangeRateSnapshot
	lastAttempt time.Time
}

func NewExchangeRateService(dataPath string) *ExchangeRateService {
	service := &ExchangeRateService{
		path:     filepath.Join(filepath.Dir(dataPath), "exchange-rate.json"),
		endpoint: defaultExchangeRateURL,
		client:   &http.Client{Timeout: 5 * time.Second},
	}
	service.load()
	return service
}

func (s *ExchangeRateService) Snapshot(settings Settings) ExchangeRateSnapshot {
	settings = normalizeSettings(settings)
	if settings.ExchangeRateMode == "manual" {
		return manualExchangeRate(settings.ManualUSDCNY, false, "manual")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if validExchangeSnapshot(s.cache) && now.Sub(time.Unix(s.cache.UpdatedAt, 0)) < 6*time.Hour {
		return s.cache
	}
	if !s.lastAttempt.IsZero() && now.Sub(s.lastAttempt) < 10*time.Minute {
		return s.fallback(settings.ManualUSDCNY)
	}
	s.lastAttempt = now
	snapshot, err := s.fetch()
	if err != nil {
		return s.fallback(settings.ManualUSDCNY)
	}
	s.cache = snapshot
	_ = s.save(snapshot)
	return snapshot
}

func (s *ExchangeRateService) fallback(rate float64) ExchangeRateSnapshot {
	if validExchangeSnapshot(s.cache) {
		out := s.cache
		out.Stale = true
		return out
	}
	return manualExchangeRate(rate, true, "fallback")
}

func (s *ExchangeRateService) fetch() (ExchangeRateSnapshot, error) {
	request, err := http.NewRequest(http.MethodGet, s.endpoint, nil)
	if err != nil {
		return ExchangeRateSnapshot{}, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "vps-monitor/exchange-rate")
	response, err := s.client.Do(request)
	if err != nil {
		return ExchangeRateSnapshot{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ExchangeRateSnapshot{}, fmt.Errorf("exchange rate returned HTTP %d", response.StatusCode)
	}
	var payload struct {
		Result    string             `json:"result"`
		UpdatedAt int64              `json:"time_last_update_unix"`
		BaseCode  string             `json:"base_code"`
		Rates     map[string]float64 `json:"rates"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return ExchangeRateSnapshot{}, err
	}
	if payload.Result != "success" || strings.ToUpper(payload.BaseCode) != "USD" || payload.Rates["CNY"] < 1 || payload.Rates["CNY"] > 20 {
		return ExchangeRateSnapshot{}, errors.New("invalid exchange rate response")
	}
	rates := map[string]float64{"USD": 1, "CNY": payload.Rates["CNY"]}
	for _, currency := range []string{"HKD", "EUR", "JPY"} {
		if value := payload.Rates[currency]; value > 0 {
			rates[currency] = value
		}
	}
	if payload.UpdatedAt <= 0 {
		payload.UpdatedAt = time.Now().Unix()
	}
	return ExchangeRateSnapshot{
		Base: "USD", Quote: "CNY", Rate: rates["CNY"], Rates: rates,
		UpdatedAt: payload.UpdatedAt, Source: "open.er-api.com",
	}, nil
}

func manualExchangeRate(rate float64, stale bool, source string) ExchangeRateSnapshot {
	if rate < 1 || rate > 20 {
		rate = 7.2
	}
	return ExchangeRateSnapshot{
		Base: "USD", Quote: "CNY", Rate: rate,
		Rates:     map[string]float64{"USD": 1, "CNY": rate},
		UpdatedAt: time.Now().Unix(), Source: source, Stale: stale,
	}
}

func validExchangeSnapshot(snapshot ExchangeRateSnapshot) bool {
	return snapshot.Base == "USD" && snapshot.Quote == "CNY" && snapshot.Rate >= 1 && snapshot.Rate <= 20 && snapshot.UpdatedAt > 0
}

func (s *ExchangeRateService) load() {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var snapshot ExchangeRateSnapshot
	if json.Unmarshal(raw, &snapshot) == nil && validExchangeSnapshot(snapshot) {
		s.cache = snapshot
	}
}

func (s *ExchangeRateService) save(snapshot ExchangeRateSnapshot) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), "exchange-rate-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.path)
}

func (s *Server) handleSiteExchangeRate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	settings := s.store.GetSettings()
	snapshot := s.exchangeRates.Snapshot(settings)
	snapshot.TimeZone = settings.TimeZone
	writeJSON(w, snapshot)
}

package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var marketReportCategories = map[string]bool{
	"fraud": true, "inaccurate": true, "unreachable": true, "prohibited": true, "other": true,
}

func (s *Server) handleMarketReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.marketEnabled(w) {
		return
	}
	if s.features == nil {
		http.Error(w, "market reports unavailable", http.StatusServiceUnavailable)
		return
	}
	ip := requestClientIP(r)
	if s.marketReportLimiter != nil && !s.marketReportLimiter.Allow(ip) {
		http.Error(w, "too many reports", http.StatusTooManyRequests)
		return
	}
	defer r.Body.Close()
	var req struct {
		ListingNodeID string `json:"listing_node_id"`
		Category      string `json:"category"`
		Message       string `json:"message"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	req.ListingNodeID = strings.TrimSpace(req.ListingNodeID)
	req.Category = strings.ToLower(strings.TrimSpace(req.Category))
	req.Message = cleanFeatureMessage(req.Message, 1000)
	listing, exists := s.store.GetMarketListing(req.ListingNodeID)
	if !exists || !listing.ForSale || listing.ModerationStatus != "approved" {
		http.Error(w, "listing not found", http.StatusNotFound)
		return
	}
	if !marketReportCategories[req.Category] || len([]rune(req.Message)) < 10 {
		http.Error(w, "valid category and report details are required", http.StatusBadRequest)
		return
	}
	now := time.Now().Unix()
	reporterHash := privateIdentifierHash(s.cfg.AuthSecret, "market-report", ip)
	s.features.mu.Lock()
	for _, item := range s.features.data.MarketReports {
		if item.ListingNodeID == req.ListingNodeID && item.ReporterHash == reporterHash && now-item.CreatedAt < 24*60*60 {
			s.features.mu.Unlock()
			http.Error(w, "report already received", http.StatusConflict)
			return
		}
	}
	id, err := newFeatureID("report")
	if err == nil {
		item := MarketReport{ID: id, ListingNodeID: req.ListingNodeID, Category: req.Category, Message: req.Message, ReporterHash: reporterHash, Status: "pending", CreatedAt: now, UpdatedAt: now}
		s.features.data.MarketReports = append(s.features.data.MarketReports, item)
		err = s.features.saveLocked()
	}
	s.features.mu.Unlock()
	if err != nil {
		http.Error(w, "save report", http.StatusInternalServerError)
		return
	}
	go s.dispatchPlatformEvent("market.report.created", map[string]any{"owner_id": listing.OwnerID, "report_id": id, "node_id": req.ListingNodeID, "category": req.Category, "created_at": now})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]any{"ok": true, "id": id})
}

func (s *Server) handleOwnerMarketAppeals(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.ownerAuthorized(r)
	if !ok {
		http.Error(w, "owner login required", http.StatusUnauthorized)
		return
	}
	if s.features == nil {
		http.Error(w, "market appeals unavailable", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		data := s.features.Snapshot()
		reports := make([]MarketReport, 0)
		ownedReports := map[string]bool{}
		for _, report := range data.MarketReports {
			listing, exists := s.store.GetMarketListing(report.ListingNodeID)
			if exists && listing.OwnerID == owner.ID {
				report.ReporterHash = ""
				reports = append(reports, report)
				ownedReports[report.ID] = true
			}
		}
		appeals := make([]MarketAppeal, 0)
		for _, appeal := range data.MarketAppeals {
			if appeal.OwnerID == owner.ID && ownedReports[appeal.ReportID] {
				appeals = append(appeals, appeal)
			}
		}
		writeJSON(w, map[string]any{"reports": reports, "appeals": appeals})
	case http.MethodPost:
		if !s.store.GetSettings().MarketEnabled {
			http.Error(w, "market is currently disabled", http.StatusServiceUnavailable)
			return
		}
		if !s.validAdminOrigin(r) {
			http.Error(w, "invalid request origin", http.StatusForbidden)
			return
		}
		defer r.Body.Close()
		var req struct {
			ReportID string `json:"report_id"`
			Message  string `json:"message"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		req.ReportID = strings.TrimSpace(req.ReportID)
		req.Message = cleanFeatureMessage(req.Message, 1000)
		if !validateFeatureID(req.ReportID) || len([]rune(req.Message)) < 10 {
			http.Error(w, "valid report and appeal details are required", http.StatusBadRequest)
			return
		}
		now := time.Now().Unix()
		s.features.mu.Lock()
		report, exists := findMarketReport(s.features.data.MarketReports, req.ReportID)
		if exists {
			listing, listingExists := s.store.GetMarketListing(report.ListingNodeID)
			exists = listingExists && listing.OwnerID == owner.ID
		}
		if !exists {
			s.features.mu.Unlock()
			http.Error(w, "report not found", http.StatusNotFound)
			return
		}
		for _, item := range s.features.data.MarketAppeals {
			if item.ReportID == req.ReportID && item.OwnerID == owner.ID && item.Status == "pending" {
				s.features.mu.Unlock()
				http.Error(w, "appeal already pending", http.StatusConflict)
				return
			}
		}
		id, err := newFeatureID("appeal")
		if err == nil {
			s.features.data.MarketAppeals = append(s.features.data.MarketAppeals, MarketAppeal{ID: id, ReportID: req.ReportID, OwnerID: owner.ID, Message: req.Message, Status: "pending", CreatedAt: now, UpdatedAt: now})
			err = s.features.saveLocked()
		}
		s.features.mu.Unlock()
		if err != nil {
			http.Error(w, "save appeal", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, map[string]any{"ok": true, "id": id})
	default:
		methodNotAllowed(w)
	}
}

func cleanFeatureMessage(value string, maxRunes int) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	value = strings.ReplaceAll(value, "\x00", "")
	runes := []rune(value)
	if len(runes) > maxRunes {
		value = string(runes[:maxRunes])
	}
	return value
}

func privateIdentifierHash(secret, purpose, value string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(purpose + "\x00" + strings.TrimSpace(value)))
	return hex.EncodeToString(mac.Sum(nil))
}

func findMarketReport(items []MarketReport, id string) (MarketReport, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return MarketReport{}, false
}

func upsertMarketReport(items []MarketReport, value MarketReport) ([]MarketReport, error) {
	for i := range items {
		if items[i].ID == value.ID {
			items[i] = value
			return items, nil
		}
	}
	return items, fmt.Errorf("report not found")
}

func upsertMarketAppeal(items []MarketAppeal, value MarketAppeal) ([]MarketAppeal, error) {
	for i := range items {
		if items[i].ID == value.ID {
			items[i] = value
			return items, nil
		}
	}
	return items, fmt.Errorf("appeal not found")
}

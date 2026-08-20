package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

func (s *Server) handleVisitorStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, s.visitorStats.Snapshot(time.Now()))
}
func (s *Server) handleVisitorVisit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if s.store.GetSettings().VisitorStatsHidden {
		writeJSON(w, s.visitorStats.Snapshot(time.Now()))
		return
	}
	if s.visitorLimiter != nil && !s.visitorLimiter.Allow(requestClientIP(r)) {
		http.Error(w, "too many visit events", http.StatusTooManyRequests)
		return
	}
	var req struct {
		VisitorID string `json:"visitor_id"`
		EventID   string `json:"event_id"`
		PageView  bool   `json:"page_view"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req); err != nil {
		http.Error(w, "invalid visit", http.StatusBadRequest)
		return
	}
	req.VisitorID = strings.TrimSpace(req.VisitorID)
	req.EventID = strings.TrimSpace(req.EventID)
	if !validVisitorToken(req.VisitorID) || req.EventID != "" && !validVisitorToken(req.EventID) {
		http.Error(w, "invalid visitor identity", http.StatusBadRequest)
		return
	}
	if req.PageView && req.EventID == "" {
		http.Error(w, "page view event_id is required", http.StatusBadRequest)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, s.visitorStats.Record(req.VisitorID, req.EventID, requestClientIP(r), req.PageView, time.Now()))
}
func validVisitorToken(value string) bool {
	if len(value) < 16 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

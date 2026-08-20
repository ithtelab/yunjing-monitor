package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"
)

func (s *Server) handleAdminAlerts(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		http.Error(w, "admin login required", http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodGet:
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		writeJSON(w, map[string]any{"settings": s.alerts.Settings(), "events": s.alerts.Events(limit)})
	case http.MethodPost:
		if !s.validAdminOrigin(r) {
			http.Error(w, "invalid request origin", http.StatusForbidden)
			return
		}
		var settings AlertSettings
		if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&settings); err != nil {
			http.Error(w, "invalid alert settings", http.StatusBadRequest)
			return
		}
		if err := s.alerts.UpdateSettings(settings); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "settings": s.alerts.Settings()})
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleAdminAlertTest(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		http.Error(w, "admin login required", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.validAdminOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return
	}
	settings := s.alerts.Settings()
	event := AlertEvent{ID: "test", NodeID: "test", NodeName: "通知测试", Kind: "test", Status: "firing", Message: "云镜监控通知通道测试", CreatedAt: time.Now().Unix()}
	if err := s.alerts.notify(event, settings); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

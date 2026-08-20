package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type serviceMonitorSummary struct {
	Checks       int     `json:"checks"`
	Successes    int     `json:"successes"`
	Availability float64 `json:"availability_percent"`
	AvgLatencyMS int64   `json:"average_latency_ms"`
}

type serviceMonitorView struct {
	ServiceMonitor
	Summary serviceMonitorSummary `json:"summary_24h"`
}

func (s *Server) serviceMonitorViews(ownerID string, all bool) []serviceMonitorView {
	monitors := s.store.ListServiceMonitors(ownerID, all)
	out := make([]serviceMonitorView, 0, len(monitors))
	for _, monitor := range monitors {
		state, _ := s.store.GetServiceMonitorState(monitor.ID)
		if state.MonitorID == "" {
			state.MonitorID = monitor.ID
			state.Status = "unknown"
		}
		monitor.State = state
		out = append(out, serviceMonitorView{ServiceMonitor: monitor, Summary: summarizeServiceMonitorResults(s.store.ListServiceMonitorResults(monitor.ID, 1000), time.Now().Add(-24*time.Hour).Unix())})
	}
	return out
}

func summarizeServiceMonitorResults(results []ServiceMonitorResult, since int64) serviceMonitorSummary {
	var summary serviceMonitorSummary
	var latencyTotal int64
	for _, result := range results {
		if result.CheckedAt < since {
			continue
		}
		summary.Checks++
		if result.Success {
			summary.Successes++
		}
		latencyTotal += result.LatencyMS
	}
	if summary.Checks > 0 {
		summary.Availability = float64(summary.Successes) * 100 / float64(summary.Checks)
		summary.AvgLatencyMS = latencyTotal / int64(summary.Checks)
	}
	return summary
}

func (s *Server) handleAdminServiceMonitors(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		http.Error(w, "admin login required", http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, s.serviceMonitorViews("", true))
	case http.MethodPost:
		if !s.validAdminOrigin(r) {
			http.Error(w, "invalid request origin", http.StatusForbidden)
			return
		}
		monitor, err := decodeServiceMonitorRequest(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		monitor.OwnerID = strings.TrimSpace(monitor.OwnerID)
		if monitor.OwnerID != "" {
			if _, ok := s.store.GetOwnerByID(monitor.OwnerID); !ok {
				http.Error(w, "owner not found", http.StatusBadRequest)
				return
			}
		}
		s.saveServiceMonitor(w, monitor, "", true)
	case http.MethodDelete:
		if !s.validAdminOrigin(r) {
			http.Error(w, "invalid request origin", http.StatusForbidden)
			return
		}
		s.deleteServiceMonitor(w, r, "", true)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleAdminServiceMonitorResults(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if !s.adminAuthorized(r) {
		http.Error(w, "admin login required", http.StatusUnauthorized)
		return
	}
	s.writeServiceMonitorResults(w, r, "", true)
}

func (s *Server) handleAccountServiceMonitors(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.ownerAuthorized(r)
	if !ok {
		http.Error(w, "account login required", http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, s.serviceMonitorViews(owner.ID, false))
	case http.MethodPost:
		if !s.validAdminOrigin(r) {
			http.Error(w, "invalid request origin", http.StatusForbidden)
			return
		}
		monitor, err := decodeServiceMonitorRequest(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		monitor.OwnerID = owner.ID
		s.saveServiceMonitor(w, monitor, owner.ID, false)
	case http.MethodDelete:
		if !s.validAdminOrigin(r) {
			http.Error(w, "invalid request origin", http.StatusForbidden)
			return
		}
		s.deleteServiceMonitor(w, r, owner.ID, false)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleAccountServiceMonitorResults(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	owner, ok := s.ownerAuthorized(r)
	if !ok {
		http.Error(w, "account login required", http.StatusUnauthorized)
		return
	}
	s.writeServiceMonitorResults(w, r, owner.ID, false)
}

func decodeServiceMonitorRequest(r *http.Request) (ServiceMonitor, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		return ServiceMonitor{}, err
	}
	var monitor ServiceMonitor
	if err := json.Unmarshal(raw, &monitor); err != nil {
		return ServiceMonitor{}, err
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	if monitor.ID == "" {
		if _, present := fields["enabled"]; !present {
			monitor.Enabled = true
		}
	}
	return monitor, nil
}

func (s *Server) saveServiceMonitor(w http.ResponseWriter, monitor ServiceMonitor, ownerID string, all bool) {
	now := time.Now().Unix()
	creating := strings.TrimSpace(monitor.ID) == ""
	var persistedState ServiceMonitorState
	if creating {
		limit := 50
		if all {
			limit = 1000
		}
		if len(s.store.ListServiceMonitors(ownerID, all)) >= limit {
			http.Error(w, "service monitor limit reached", http.StatusConflict)
			return
		}
		id, err := newFeatureID("svc")
		if err != nil {
			http.Error(w, "create monitor id", http.StatusInternalServerError)
			return
		}
		monitor.ID, monitor.CreatedAt = id, now
	} else {
		existing, ok := s.store.GetServiceMonitor(monitor.ID)
		if !ok || !all && existing.OwnerID != ownerID {
			http.Error(w, "service monitor not found", http.StatusNotFound)
			return
		}
		monitor.CreatedAt = existing.CreatedAt
		monitor.LastRunAt = existing.LastRunAt
		monitor.NextRunAt = existing.NextRunAt
		persistedState, _ = s.store.GetServiceMonitorState(monitor.ID)
		if !all {
			monitor.OwnerID = ownerID
		}
	}
	monitor.State = ServiceMonitorState{}
	monitor.UpdatedAt = now
	if monitor.NextRunAt == 0 || !monitor.Enabled {
		monitor.NextRunAt = now
	}
	normalized, err := normalizeServiceMonitorCore(monitor)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.validateServiceMonitorProbePoints(normalized.ProbePointIDs); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if normalized.NodeID != "" {
		planned, ok := s.store.GetPlannedNode(normalized.NodeID)
		if !ok || !all && planned.OwnerID != ownerID || all && normalized.OwnerID != "" && planned.OwnerID != normalized.OwnerID {
			http.Error(w, "monitor node is not owned by the selected account", http.StatusForbidden)
			return
		}
	}
	if err := s.store.SaveServiceMonitor(normalized); err != nil {
		http.Error(w, "persist service monitor", http.StatusInternalServerError)
		return
	}
	status := http.StatusOK
	if creating {
		status = http.StatusCreated
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if persistedState.MonitorID == "" {
		persistedState = ServiceMonitorState{MonitorID: normalized.ID, Status: "unknown"}
	}
	normalized.State = persistedState
	writeJSON(w, normalized)
}

func (s *Server) validateServiceMonitorProbePoints(ids []string) error {
	if s.features == nil {
		if len(ids) == 1 && ids[0] == "probe_local" {
			return nil
		}
		return errors.New("remote probe points are unavailable")
	}
	s.features.mu.RLock()
	defer s.features.mu.RUnlock()
	for _, id := range ids {
		point, ok := findProbePoint(s.features.data.ProbePoints, id)
		if !ok || !point.Enabled {
			return errors.New("probe point not found or disabled")
		}
	}
	return nil
}

func (s *Server) deleteServiceMonitor(w http.ResponseWriter, r *http.Request, ownerID string, all bool) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if !validNodeID(id) {
		http.Error(w, "invalid monitor id", http.StatusBadRequest)
		return
	}
	monitor, ok := s.store.GetServiceMonitor(id)
	if !ok || !all && monitor.OwnerID != ownerID {
		http.Error(w, "service monitor not found", http.StatusNotFound)
		return
	}
	if err := s.store.DeleteServiceMonitor(id); err != nil {
		http.Error(w, "delete service monitor", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) writeServiceMonitorResults(w http.ResponseWriter, r *http.Request, ownerID string, all bool) {
	id := strings.TrimSpace(r.URL.Query().Get("monitor_id"))
	if !validNodeID(id) {
		http.Error(w, "invalid monitor id", http.StatusBadRequest)
		return
	}
	monitor, ok := s.store.GetServiceMonitor(id)
	if !ok || !all && monitor.OwnerID != ownerID {
		http.Error(w, "service monitor not found", http.StatusNotFound)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	writeJSON(w, s.store.ListServiceMonitorResults(id, limit))
}

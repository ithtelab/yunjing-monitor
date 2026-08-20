package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
)

func auditForRequest(r *http.Request, actorType, actorID, action, objectType, objectID, summary string) AuditEvent {
	return AuditEvent{ActorType: actorType, ActorID: actorID, Action: action, ObjectType: objectType, ObjectID: objectID, IP: requestClientIP(r), Summary: summary}
}

type recycleSummary struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	NodeID    string `json:"node_id"`
	DeletedAt int64  `json:"deleted_at"`
	DeletedBy string `json:"deleted_by"`
	Summary   string `json:"summary"`
}

func (s *Server) handleAdminRecycleBin(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		http.Error(w, "admin login required", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	records := s.store.ListRecycleRecords()
	out := make([]recycleSummary, 0, len(records))
	for _, record := range records {
		out = append(out, recycleSummary{ID: record.ID, Kind: record.Kind, NodeID: record.NodeID, DeletedAt: record.DeletedAt, DeletedBy: record.DeletedBy, Summary: record.Summary})
	}
	writeJSON(w, out)
}

func decodeRecycleID(r *http.Request) (string, error) {
	var req struct {
		ID string `json:"id"`
	}
	err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req)
	return req.ID, err
}

func (s *Server) handleAdminRecycleRestore(w http.ResponseWriter, r *http.Request) {
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
	id, err := decodeRecycleID(r)
	if err != nil || id == "" {
		http.Error(w, "invalid recycle id", http.StatusBadRequest)
		return
	}
	if err := s.store.RestoreRecycleRecord(id, auditForRequest(r, "admin", "admin", "recycle.restore", "recycle_record", id, "Restored recycle record")); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	s.cache.MarkDirty()
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleAdminRecyclePurge(w http.ResponseWriter, r *http.Request) {
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
	id, err := decodeRecycleID(r)
	if err != nil || id == "" {
		http.Error(w, "invalid recycle id", http.StatusBadRequest)
		return
	}
	if err := s.store.PurgeRecycleRecord(id, auditForRequest(r, "admin", "admin", "recycle.purge", "recycle_record", id, "Permanently deleted recycle record")); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleAdminAudit(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		http.Error(w, "admin login required", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	writeJSON(w, s.store.ListAuditEvents(limit))
}

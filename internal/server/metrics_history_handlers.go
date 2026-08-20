package server

import (
	"net/http"
	"strings"
	"time"
)

func (s *Server) handleMetricsHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	nodeID := strings.TrimSpace(r.URL.Query().Get("node_id"))
	if !validNodeID(nodeID) {
		http.Error(w, "invalid node_id", http.StatusBadRequest)
		return
	}
	if !s.nodeIsPublic(nodeID) {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}
	duration, err := historyRange(strings.TrimSpace(r.URL.Query().Get("range")))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	exists := false
	for _, node := range s.store.AdminNodes(s.cfg.OfflineWait) {
		if node.NodeID == nodeID {
			exists = true
			break
		}
	}
	if !exists {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]any{
		"node_id": nodeID,
		"range":   strings.TrimSpace(r.URL.Query().Get("range")),
		"samples": s.metricsHistory.Query(nodeID, time.Now().Add(-duration).Unix()),
	})
}

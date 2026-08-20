package server

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

func decodeLimitedJSON(r *http.Request, value any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 256<<10))
	return decoder.Decode(value)
}

func (s *Server) handleAccountServices(w http.ResponseWriter, r *http.Request) {
	s.handleAccountServiceMonitors(w, r)
}

func (s *Server) handleAdminServices(w http.ResponseWriter, r *http.Request) {
	s.handleAdminServiceMonitors(w, r)
}

/* Legacy platform-file service monitor handlers retained temporarily for data migration reference.
func (s *Server) handleServicesForActor(w http.ResponseWriter, r *http.Request, ownerID string, admin bool) {
	if s.features == nil {
		http.Error(w, "service monitoring unavailable", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.features.mu.RLock()
		monitors := make([]ServiceMonitor, 0)
		summaries := map[string]serviceMonitorSummary{}
		for _, monitor := range s.features.data.ServiceMonitors {
			if admin || monitor.OwnerID == ownerID {
				monitors = append(monitors, monitor)
				summaries[monitor.ID] = serviceSummary(s.features.data.ServiceCheckResults, monitor.ID, time.Now().Add(-24*time.Hour).Unix())
			}
		}
		s.features.mu.RUnlock()
		sort.Slice(monitors, func(i, j int) bool { return monitors[i].CreatedAt > monitors[j].CreatedAt })
		writeJSON(w, map[string]any{"monitors": monitors, "summaries": summaries})
	case http.MethodPost:
		if !s.validAdminOrigin(r) {
			http.Error(w, "invalid request origin", http.StatusForbidden)
			return
		}
		var payload serviceMonitorPayload
		if err := decodeLimitedJSON(r, &payload); err != nil {
			http.Error(w, "invalid service monitor request", http.StatusBadRequest)
			return
		}
		action := strings.ToLower(strings.TrimSpace(payload.Action))
		if action == "" {
			action = "save"
		}
		switch action {
		case "save":
			item, err := normalizeServiceMonitor(payload.Monitor)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if !admin {
				item.OwnerID = ownerID
				if item.NodeID == "" || !s.ownerOwnsNode(ownerID, item.NodeID) {
					http.Error(w, "monitor must belong to one of your nodes", http.StatusForbidden)
					return
				}
			} else if item.OwnerID != "" {
				planned, exists := s.store.GetPlannedNode(item.NodeID)
				if !exists || planned.OwnerID != item.OwnerID {
					http.Error(w, "monitor owner does not own node", http.StatusBadRequest)
					return
				}
			}
			now := time.Now().Unix()
			s.features.mu.Lock()
			if item.ID == "" {
				item.ID, err = newFeatureID("monitor")
				item.CreatedAt, item.State.Status = now, "unknown"
			} else {
				current, found := findServiceMonitor(s.features.data.ServiceMonitors, item.ID)
				if !found || !admin && current.OwnerID != ownerID {
					s.features.mu.Unlock()
					http.Error(w, "monitor not found", http.StatusNotFound)
					return
				}
				item.CreatedAt, item.State = current.CreatedAt, current.State
				if !admin {
					item.OwnerID = ownerID
				}
			}
			item.UpdatedAt = now
			if err == nil {
				s.features.data.ServiceMonitors = upsertServiceMonitor(s.features.data.ServiceMonitors, item)
				err = s.features.saveLocked()
			}
			s.features.mu.Unlock()
			if err != nil {
				http.Error(w, "save service monitor failed", http.StatusInternalServerError)
				return
			}
			writeJSON(w, map[string]any{"ok": true, "monitor": item})
		case "delete":
			id := strings.TrimSpace(payload.ID)
			s.features.mu.Lock()
			found := false
			out := s.features.data.ServiceMonitors[:0]
			for _, item := range s.features.data.ServiceMonitors {
				if item.ID == id && (admin || item.OwnerID == ownerID) {
					found = true
					continue
				}
				out = append(out, item)
			}
			s.features.data.ServiceMonitors = out
			if found {
				results := s.features.data.ServiceCheckResults[:0]
				for _, result := range s.features.data.ServiceCheckResults {
					if result.MonitorID != id {
						results = append(results, result)
					}
				}
				s.features.data.ServiceCheckResults = results
				_ = s.features.saveLocked()
			}
			s.features.mu.Unlock()
			if !found {
				http.Error(w, "monitor not found", http.StatusNotFound)
				return
			}
			writeJSON(w, map[string]bool{"ok": true})
		case "check":
			id := strings.TrimSpace(payload.ID)
			s.features.mu.Lock()
			monitor, found := findServiceMonitor(s.features.data.ServiceMonitors, id)
			if found && !admin && monitor.OwnerID != ownerID {
				found = false
			}
			if found && !s.features.serviceMonitorRunning[id] {
				s.features.serviceMonitorRunning[id] = true
			} else if found {
				s.features.mu.Unlock()
				http.Error(w, "monitor check already running", http.StatusConflict)
				return
			}
			s.features.mu.Unlock()
			if !found {
				http.Error(w, "monitor not found", http.StatusNotFound)
				return
			}
			go s.runOneLocalServiceMonitor(monitor)
			writeJSON(w, map[string]string{"status": "queued"})
		default:
			http.Error(w, "unsupported service monitor action", http.StatusBadRequest)
		}
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) runOneLocalServiceMonitor(monitor ServiceMonitor) {
	serviceProbeSemaphore <- struct{}{}
	defer func() { <-serviceProbeSemaphore }()
	ctx, cancel := contextWithMonitorTimeout(monitor)
	result := runServiceProbe(ctx, monitor, "probe_local")
	cancel()
	s.recordServiceCheck(monitor.ID, result)
}

func contextWithMonitorTimeout(monitor ServiceMonitor) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), time.Duration(monitor.TimeoutSeconds)*time.Second)
}

func findServiceMonitor(items []ServiceMonitor, id string) (ServiceMonitor, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return ServiceMonitor{}, false
}

func upsertServiceMonitor(items []ServiceMonitor, value ServiceMonitor) []ServiceMonitor {
	for index := range items {
		if items[index].ID == value.ID {
			items[index] = value
			return items
		}
	}
	return append(items, value)
}

func (s *Server) handleAccountServiceHistory(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.ownerAuthorized(r)
	if !ok {
		http.Error(w, "owner login required", http.StatusUnauthorized)
		return
	}
	s.handleServiceHistory(w, r, owner.ID, false)
}

func (s *Server) handleAdminServiceHistory(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		http.Error(w, "admin login required", http.StatusUnauthorized)
		return
	}
	s.handleServiceHistory(w, r, "", true)
}

func (s *Server) handleServiceHistory(w http.ResponseWriter, r *http.Request, ownerID string, admin bool) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	duration, err := serviceHistoryRange(r.URL.Query().Get("range"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	monitorID := strings.TrimSpace(r.URL.Query().Get("monitor_id"))
	s.features.mu.RLock()
	monitor, found := findServiceMonitor(s.features.data.ServiceMonitors, monitorID)
	if !found || !admin && monitor.OwnerID != ownerID {
		s.features.mu.RUnlock()
		http.Error(w, "monitor not found", http.StatusNotFound)
		return
	}
	since := time.Now().Add(-duration).Unix()
	items := make([]ServiceCheckResult, 0)
	for _, result := range s.features.data.ServiceCheckResults {
		if result.MonitorID == monitorID && result.CheckedAt >= since {
			items = append(items, result)
		}
	}
	s.features.mu.RUnlock()
	writeJSON(w, map[string]any{"monitor": monitor, "summary": serviceSummary(items, monitorID, since), "results": sortedServiceResults(items)})
}
*/

func (s *Server) handleAccountServiceHistory(w http.ResponseWriter, r *http.Request) {
	s.handleAccountServiceMonitorResults(w, r)
}

func (s *Server) handleAdminServiceHistory(w http.ResponseWriter, r *http.Request) {
	s.handleAdminServiceMonitorResults(w, r)
}

func (s *Server) handleAccountNodeGroups(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.ownerAuthorized(r)
	if !ok {
		http.Error(w, "owner login required", http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodGet {
		s.features.mu.RLock()
		items := make([]OwnerNodeGroup, 0)
		for _, item := range s.features.data.OwnerNodeGroups {
			if item.OwnerID == owner.ID {
				items = append(items, item)
			}
		}
		s.features.mu.RUnlock()
		writeJSON(w, items)
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
	var req struct {
		Action string         `json:"action"`
		ID     string         `json:"id"`
		Group  OwnerNodeGroup `json:"group"`
	}
	if err := decodeLimitedJSON(r, &req); err != nil {
		http.Error(w, "invalid node group request", http.StatusBadRequest)
		return
	}
	if req.Action == "delete" {
		s.features.mu.Lock()
		out := s.features.data.OwnerNodeGroups[:0]
		for _, item := range s.features.data.OwnerNodeGroups {
			if item.ID != req.ID || item.OwnerID != owner.ID {
				out = append(out, item)
			}
		}
		s.features.data.OwnerNodeGroups = out
		_ = s.features.saveLocked()
		s.features.mu.Unlock()
		writeJSON(w, map[string]bool{"ok": true})
		return
	}
	item := req.Group
	item.OwnerID = owner.ID
	item.Name = cleanFeatureText(item.Name, 64)
	item.Color = normalizeGroupColor(item.Color)
	item.NodeIDs = cleanFeatureList(item.NodeIDs, 200, 80)
	if item.Name == "" {
		http.Error(w, "group name is required", http.StatusBadRequest)
		return
	}
	for _, nodeID := range item.NodeIDs {
		if !s.ownerOwnsNode(owner.ID, nodeID) {
			http.Error(w, "node group contains a node owned by another account", http.StatusForbidden)
			return
		}
	}
	now := time.Now().Unix()
	s.features.mu.Lock()
	if item.ID == "" {
		item.ID, _ = newFeatureID("group")
		item.CreatedAt = now
	} else if current, found := findOwnerNodeGroup(s.features.data.OwnerNodeGroups, item.ID, owner.ID); found {
		item.CreatedAt = current.CreatedAt
	} else {
		s.features.mu.Unlock()
		http.Error(w, "node group not found", http.StatusNotFound)
		return
	}
	item.UpdatedAt = now
	s.features.data.OwnerNodeGroups = upsertOwnerNodeGroup(s.features.data.OwnerNodeGroups, item)
	_ = s.features.saveLocked()
	s.features.mu.Unlock()
	writeJSON(w, map[string]any{"ok": true, "group": item})
}

func normalizeGroupColor(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != 7 || value[0] != '#' {
		return "#165dff"
	}
	for _, character := range value[1:] {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return "#165dff"
		}
	}
	return value
}

func findOwnerNodeGroup(items []OwnerNodeGroup, id, ownerID string) (OwnerNodeGroup, bool) {
	for _, item := range items {
		if item.ID == id && item.OwnerID == ownerID {
			return item, true
		}
	}
	return OwnerNodeGroup{}, false
}

func upsertOwnerNodeGroup(items []OwnerNodeGroup, value OwnerNodeGroup) []OwnerNodeGroup {
	for index := range items {
		if items[index].ID == value.ID && items[index].OwnerID == value.OwnerID {
			items[index] = value
			return items
		}
	}
	return append(items, value)
}

func (s *Server) handleAccountOverviewPreference(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.ownerAuthorized(r)
	if !ok {
		http.Error(w, "owner login required", http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodGet {
		s.features.mu.RLock()
		item, exists := s.features.data.OwnerOverviewPreferences[owner.ID]
		s.features.mu.RUnlock()
		if !exists {
			item = defaultOverviewPreference(owner.ID)
		}
		writeJSON(w, item)
		return
	}
	if r.Method != http.MethodPost || !s.validAdminOrigin(r) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
		} else {
			http.Error(w, "invalid request origin", http.StatusForbidden)
		}
		return
	}
	var item OwnerOverviewPreference
	if err := decodeLimitedJSON(r, &item); err != nil {
		http.Error(w, "invalid overview preference", http.StatusBadRequest)
		return
	}
	item = normalizeOverviewPreference(owner.ID, item)
	if item.GroupID != "" {
		s.features.mu.RLock()
		_, found := findOwnerNodeGroup(s.features.data.OwnerNodeGroups, item.GroupID, owner.ID)
		s.features.mu.RUnlock()
		if !found {
			http.Error(w, "node group not found", http.StatusBadRequest)
			return
		}
	}
	s.features.mu.Lock()
	s.features.data.OwnerOverviewPreferences[owner.ID] = item
	_ = s.features.saveLocked()
	s.features.mu.Unlock()
	writeJSON(w, map[string]any{"ok": true, "preference": item})
}

func defaultOverviewPreference(ownerID string) OwnerOverviewPreference {
	return OwnerOverviewPreference{OwnerID: ownerID, View: "cards", SortBy: "name", SortOrder: "asc"}
}

func normalizeOverviewPreference(ownerID string, item OwnerOverviewPreference) OwnerOverviewPreference {
	item.OwnerID = ownerID
	if !listContains([]string{"cards", "compact", "table"}, item.View) {
		item.View = "cards"
	}
	if !listContains([]string{"name", "status", "region", "due_date", "traffic"}, item.SortBy) {
		item.SortBy = "name"
	}
	if item.SortOrder != "desc" {
		item.SortOrder = "asc"
	}
	item.GroupID = cleanFeatureText(item.GroupID, 80)
	item.Statuses = cleanFeatureList(item.Statuses, 3, 16)
	item.UpdatedAt = time.Now().Unix()
	return item
}

func (s *Server) handleAccountNodeEvents(w http.ResponseWriter, r *http.Request) {
	s.handleNodeEventsForActor(w, r, false)
}

func (s *Server) handleAdminNodeEvents(w http.ResponseWriter, r *http.Request) {
	s.handleNodeEventsForActor(w, r, true)
}

func (s *Server) handleNodeEventsForActor(w http.ResponseWriter, r *http.Request, admin bool) {
	ownerID := ""
	if admin {
		if !s.adminAuthorized(r) {
			http.Error(w, "admin login required", http.StatusUnauthorized)
			return
		}
	} else {
		owner, ok := s.ownerAuthorized(r)
		if !ok {
			http.Error(w, "owner login required", http.StatusUnauthorized)
			return
		}
		ownerID = owner.ID
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	s.features.mu.RLock()
	items := make([]NodeOperationalEvent, 0)
	for index := len(s.features.data.NodeOperationalEvents) - 1; index >= 0 && len(items) < 200; index-- {
		item := s.features.data.NodeOperationalEvents[index]
		if admin || item.OwnerID == ownerID {
			items = append(items, item)
		}
	}
	s.features.mu.RUnlock()
	writeJSON(w, items)
}

func (s *Server) handleAccountTrafficPolicies(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.ownerAuthorized(r)
	if !ok {
		http.Error(w, "owner login required", http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodGet {
		hosts := map[string]AkileHost{}
		for _, host := range s.store.AkileHosts() {
			hosts[host.Host.Name] = host
		}
		s.features.mu.RLock()
		items := make([]map[string]any, 0)
		for _, nodeID := range s.store.ListOwnerNodeIDs(owner.ID) {
			policy := s.features.data.TrafficAlertPolicies[nodeID]
			host := hosts[nodeID]
			items = append(items, map[string]any{"node_id": nodeID, "policy": policy, "used_bytes": host.State.CycleNetInTransfer + host.State.CycleNetOutTransfer, "period_start": host.State.TrafficPeriodStart, "next_reset": host.State.TrafficNextReset, "reset_day": host.State.TrafficResetDay})
		}
		s.features.mu.RUnlock()
		writeJSON(w, items)
		return
	}
	if r.Method != http.MethodPost || !s.validAdminOrigin(r) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
		} else {
			http.Error(w, "invalid request origin", http.StatusForbidden)
		}
		return
	}
	var item TrafficAlertPolicy
	if err := decodeLimitedJSON(r, &item); err != nil {
		http.Error(w, "invalid traffic policy", http.StatusBadRequest)
		return
	}
	item.NodeID = strings.TrimSpace(item.NodeID)
	if !s.ownerOwnsNode(owner.ID, item.NodeID) {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}
	if item.QuotaBytes < 1<<20 || item.QuotaBytes > 1<<60 || item.ThresholdPct < 1 || item.ThresholdPct > 100 {
		http.Error(w, "traffic quota or threshold is invalid", http.StatusBadRequest)
		return
	}
	item.OwnerID, item.UpdatedAt = owner.ID, time.Now().Unix()
	s.features.mu.Lock()
	current := s.features.data.TrafficAlertPolicies[item.NodeID]
	item.AlertActive = current.AlertActive
	s.features.data.TrafficAlertPolicies[item.NodeID] = item
	_ = s.features.saveLocked()
	s.features.mu.Unlock()
	writeJSON(w, map[string]any{"ok": true, "policy": item})
}

func (s *Server) handleAdminTrafficPolicies(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		http.Error(w, "admin login required", http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodGet {
		hosts := map[string]AkileHost{}
		for _, host := range s.store.AkileHosts() {
			hosts[host.Host.Name] = host
		}
		s.features.mu.RLock()
		items := make([]map[string]any, 0, len(s.features.data.TrafficAlertPolicies))
		for nodeID, policy := range s.features.data.TrafficAlertPolicies {
			host := hosts[nodeID]
			items = append(items, map[string]any{"node_id": nodeID, "policy": policy, "used_bytes": host.State.CycleNetInTransfer + host.State.CycleNetOutTransfer, "period_start": host.State.TrafficPeriodStart, "next_reset": host.State.TrafficNextReset, "reset_day": host.State.TrafficResetDay})
		}
		s.features.mu.RUnlock()
		writeJSON(w, items)
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
	var item TrafficAlertPolicy
	if err := decodeLimitedJSON(r, &item); err != nil {
		http.Error(w, "invalid traffic policy", http.StatusBadRequest)
		return
	}
	item.NodeID = strings.TrimSpace(item.NodeID)
	planned, exists := s.store.GetPlannedNode(item.NodeID)
	if !exists {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}
	if item.QuotaBytes < 1<<20 || item.QuotaBytes > 1<<60 || item.ThresholdPct < 1 || item.ThresholdPct > 100 {
		http.Error(w, "traffic quota or threshold is invalid", http.StatusBadRequest)
		return
	}
	item.OwnerID, item.UpdatedAt = planned.OwnerID, time.Now().Unix()
	s.features.mu.Lock()
	item.AlertActive = s.features.data.TrafficAlertPolicies[item.NodeID].AlertActive
	s.features.data.TrafficAlertPolicies[item.NodeID] = item
	err := s.features.saveLocked()
	s.features.mu.Unlock()
	if err != nil {
		http.Error(w, "save traffic policy failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "policy": item})
}

func (s *Server) handleAdminProbePoints(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		http.Error(w, "admin login required", http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodGet {
		s.features.mu.RLock()
		items := append([]ProbePoint(nil), s.features.data.ProbePoints...)
		s.features.mu.RUnlock()
		writeJSON(w, items)
		return
	}
	if r.Method != http.MethodPost || !s.validAdminOrigin(r) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
		} else {
			http.Error(w, "invalid request origin", http.StatusForbidden)
		}
		return
	}
	var req struct {
		Action string     `json:"action"`
		ID     string     `json:"id"`
		Point  ProbePoint `json:"probe_point"`
	}
	if err := decodeLimitedJSON(r, &req); err != nil {
		http.Error(w, "invalid probe point request", http.StatusBadRequest)
		return
	}
	if req.Action == "delete" {
		if req.ID == "probe_local" {
			http.Error(w, "local probe point cannot be deleted", http.StatusConflict)
			return
		}
		s.features.mu.Lock()
		out := s.features.data.ProbePoints[:0]
		for _, item := range s.features.data.ProbePoints {
			if item.ID != req.ID {
				out = append(out, item)
			}
		}
		s.features.data.ProbePoints = out
		_ = s.features.saveLocked()
		s.features.mu.Unlock()
		writeJSON(w, map[string]bool{"ok": true})
		return
	}
	item := req.Point
	item.Name, item.Region = cleanFeatureText(item.Name, 64), cleanFeatureText(item.Region, 64)
	item.NodeID = strings.TrimSpace(item.NodeID)
	if item.Name == "" || item.Region == "" || item.NodeID == "" {
		http.Error(w, "remote probe point requires name, region and node_id", http.StatusBadRequest)
		return
	}
	if _, exists := s.store.GetPlannedNode(item.NodeID); !exists {
		http.Error(w, "probe node not found", http.StatusBadRequest)
		return
	}
	item.Mode = "agent"
	now := time.Now().Unix()
	s.features.mu.Lock()
	if item.ID == "" {
		item.ID, _ = newFeatureID("probe")
		item.CreatedAt = now
	} else if current, exists := findProbePoint(s.features.data.ProbePoints, item.ID); exists {
		item.CreatedAt, item.LastSeenAt = current.CreatedAt, current.LastSeenAt
	} else {
		s.features.mu.Unlock()
		http.Error(w, "probe point not found", http.StatusNotFound)
		return
	}
	item.UpdatedAt = now
	s.features.data.ProbePoints = upsertProbePoint(s.features.data.ProbePoints, item)
	_ = s.features.saveLocked()
	s.features.mu.Unlock()
	writeJSON(w, map[string]any{"ok": true, "probe_point": item})
}

func findProbePoint(items []ProbePoint, id string) (ProbePoint, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return ProbePoint{}, false
}

func upsertProbePoint(items []ProbePoint, value ProbePoint) []ProbePoint {
	for index := range items {
		if items[index].ID == value.ID {
			items[index] = value
			return items
		}
	}
	return append(items, value)
}

func (s *Server) handleAgentProbeAssignments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if !s.agentAuthorized(r) {
		http.Error(w, "missing agent identity", http.StatusUnauthorized)
		return
	}
	nodeID := strings.TrimSpace(r.Header.Get("X-Node-ID"))
	s.features.mu.Lock()
	points := make([]ProbePoint, 0)
	pointIDs := map[string]bool{}
	for index := range s.features.data.ProbePoints {
		point := &s.features.data.ProbePoints[index]
		if point.Enabled && point.Mode == "agent" && point.NodeID == nodeID {
			point.LastSeenAt = time.Now().Unix()
			points = append(points, *point)
			pointIDs[point.ID] = true
		}
	}
	monitors := make([]ServiceMonitor, 0)
	for _, monitor := range s.store.ListServiceMonitors("", true) {
		if !monitor.Enabled {
			continue
		}
		for _, pointID := range monitor.ProbePointIDs {
			if pointIDs[pointID] {
				monitors = append(monitors, monitor)
				break
			}
		}
	}
	if len(points) > 0 {
		_ = s.features.saveLocked()
	}
	s.features.mu.Unlock()
	writeJSON(w, map[string]any{"probe_points": points, "monitors": monitors, "server_time": time.Now().Unix()})
}

func (s *Server) handleAgentProbeResults(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.agentAuthorized(r) {
		http.Error(w, "missing agent identity", http.StatusUnauthorized)
		return
	}
	nodeID := strings.TrimSpace(r.Header.Get("X-Node-ID"))
	var req struct {
		ProbePointID string                 `json:"probe_point_id"`
		Results      []ServiceMonitorResult `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 512<<10)).Decode(&req); err != nil || len(req.Results) > 100 {
		http.Error(w, "invalid probe result payload", http.StatusBadRequest)
		return
	}
	s.features.mu.RLock()
	point, found := findProbePoint(s.features.data.ProbePoints, req.ProbePointID)
	s.features.mu.RUnlock()
	if !found || !point.Enabled || point.Mode != "agent" || point.NodeID != nodeID {
		http.Error(w, "probe point is not assigned to this agent", http.StatusForbidden)
		return
	}
	now := time.Now().Unix()
	accepted := 0
	for _, result := range req.Results {
		monitor, ok := s.store.GetServiceMonitor(strings.TrimSpace(result.MonitorID))
		if !ok || !monitor.Enabled || !listContains(monitor.ProbePointIDs, point.ID) || !validAgentProbeResult(result, now) {
			continue
		}
		result.ID = ""
		result.ProbePointID, result.CheckedAt = point.ID, now
		result.Error = cleanFeatureText(result.Error, 240)
		if s.serviceMonitors == nil || s.serviceMonitors.RecordResult(monitor, result) != nil {
			continue
		}
		accepted++
	}
	writeJSON(w, map[string]any{"ok": true, "accepted": accepted})
}

func validAgentProbeResult(result ServiceMonitorResult, now int64) bool {
	if result.LatencyMS < 0 || result.LatencyMS > 30000 || result.StatusCode < 0 || result.StatusCode > 599 {
		return false
	}
	if result.ResolvedIP != "" {
		ip := net.ParseIP(strings.TrimSpace(result.ResolvedIP))
		if ip == nil || !publicNotificationIP(ip) {
			return false
		}
	}
	if result.TLSFingerprint != "" {
		if len(result.TLSFingerprint) != 64 {
			return false
		}
		if _, err := hex.DecodeString(result.TLSFingerprint); err != nil {
			return false
		}
	}
	if result.TLSNotAfter != 0 && (result.TLSNotAfter < now-365*86400 || result.TLSNotAfter > now+10*365*86400) {
		return false
	}
	return true
}

func (s *Server) recordAgentIP(nodeID, address string) {
	ip := net.ParseIP(strings.TrimSpace(address))
	if s.features == nil || ip == nil || !ip.IsGlobalUnicast() {
		return
	}
	planned, _ := s.store.GetPlannedNode(nodeID)
	now := time.Now().Unix()
	var event *NodeOperationalEvent
	s.features.mu.Lock()
	record := s.features.data.NodeIPRecords[nodeID]
	if record.CurrentIP != "" && record.CurrentIP != ip.String() {
		item := NodeOperationalEvent{OwnerID: planned.OwnerID, NodeID: nodeID, Kind: "ip.changed", Status: "changed", Message: fmt.Sprintf("IP changed from %s to %s", record.CurrentIP, ip.String()), CreatedAt: now}
		item.ID, _ = newFeatureID("event")
		event = &item
		s.features.data.NodeOperationalEvents = append(s.features.data.NodeOperationalEvents, item)
		record.PreviousIP, record.ChangedAt = record.CurrentIP, now
	}
	record.NodeID, record.OwnerID, record.CurrentIP, record.UpdatedAt = nodeID, planned.OwnerID, ip.String(), now
	s.features.data.NodeIPRecords[nodeID] = record
	if len(s.features.data.NodeOperationalEvents) > 2000 {
		s.features.data.NodeOperationalEvents = append([]NodeOperationalEvent(nil), s.features.data.NodeOperationalEvents[len(s.features.data.NodeOperationalEvents)-2000:]...)
	}
	_ = s.features.saveLocked()
	s.features.mu.Unlock()
	if event != nil {
		go s.dispatchPlatformEvent("alert.firing", map[string]any{"owner_id": event.OwnerID, "node_id": nodeID, "kind": event.Kind, "status": event.Status, "message": event.Message, "created_at": now})
	}
}

func (s *Server) evaluateTrafficPolicies() {
	if s.features == nil {
		return
	}
	hosts := map[string]AkileHost{}
	for _, host := range s.store.AkileHosts() {
		hosts[host.Host.Name] = host
	}
	events := make([]map[string]any, 0)
	s.features.mu.Lock()
	changed := false
	for nodeID, policy := range s.features.data.TrafficAlertPolicies {
		if !policy.Enabled || policy.QuotaBytes == 0 {
			continue
		}
		host := hosts[nodeID]
		used := host.State.CycleNetInTransfer + host.State.CycleNetOutTransfer
		active := used*100 >= policy.QuotaBytes*uint64(policy.ThresholdPct)
		if active == policy.AlertActive {
			continue
		}
		policy.AlertActive, policy.UpdatedAt = active, time.Now().Unix()
		s.features.data.TrafficAlertPolicies[nodeID] = policy
		changed = true
		status, message := "recovered", "Traffic usage returned below the configured threshold"
		if active {
			status, message = "firing", fmt.Sprintf("Traffic usage reached %d%% of the configured quota", policy.ThresholdPct)
		}
		item := NodeOperationalEvent{OwnerID: policy.OwnerID, NodeID: nodeID, Kind: "traffic.threshold", Status: status, Message: message, CreatedAt: time.Now().Unix()}
		item.ID, _ = newFeatureID("event")
		s.features.data.NodeOperationalEvents = append(s.features.data.NodeOperationalEvents, item)
		events = append(events, map[string]any{"owner_id": policy.OwnerID, "node_id": nodeID, "kind": item.Kind, "status": status, "message": message, "used_bytes": used, "quota_bytes": policy.QuotaBytes, "created_at": item.CreatedAt})
	}
	if changed {
		_ = s.features.saveLocked()
	}
	s.features.mu.Unlock()
	for _, event := range events {
		name := "alert.firing"
		if event["status"] == "recovered" {
			name = "alert.recovered"
		}
		go s.dispatchPlatformEvent(name, event)
	}
}

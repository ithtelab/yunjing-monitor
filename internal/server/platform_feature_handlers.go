package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type platformAdminRequest struct {
	Action        string              `json:"action"`
	ID            string              `json:"id"`
	NodeProfile   NodeFeatureProfile  `json:"node_profile"`
	SavedView     SavedNodeView       `json:"saved_view"`
	Maintenance   MaintenanceWindow   `json:"maintenance"`
	StatusPage    PublicStatusPage    `json:"status_page"`
	Incident      StatusIncident      `json:"incident"`
	APIKey        APIKeyRecord        `json:"api_key"`
	Webhook       WebhookIntegration  `json:"webhook"`
	WebhookSecret string              `json:"webhook_secret"`
	Rollout       AgentUpgradeRollout `json:"rollout"`
	SellerTrust   SellerTrustProfile  `json:"seller_trust"`
	MarketReport  MarketReport        `json:"market_report"`
	MarketAppeal  MarketAppeal        `json:"market_appeal"`
	SetupKey      string              `json:"setup_key"`
	SetupDone     bool                `json:"setup_done"`
}

func (s *Server) handleAdminPlatform(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		http.Error(w, "admin login required", http.StatusUnauthorized)
		return
	}
	if s.features == nil {
		http.Error(w, "platform features unavailable", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, s.platformSnapshot())
	case http.MethodPost:
		if !s.validAdminOrigin(r) {
			http.Error(w, "invalid request origin", http.StatusForbidden)
			return
		}
		var req platformAdminRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		result, err := s.applyPlatformAction(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.dispatchPlatformAdminNotification(req)
		writeJSON(w, result)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) dispatchPlatformAdminNotification(req platformAdminRequest) {
	switch strings.TrimSpace(req.Action) {
	case "resolve_market_report":
		report, ok := findMarketReport(s.features.Snapshot().MarketReports, req.MarketReport.ID)
		if !ok {
			return
		}
		listing, ok := s.store.GetMarketListing(report.ListingNodeID)
		if !ok {
			return
		}
		go s.dispatchPlatformEvent("market.report.resolved", map[string]any{"owner_id": listing.OwnerID, "report_id": report.ID, "node_id": report.ListingNodeID, "status": report.Status, "resolution": report.Resolution})
	case "resolve_market_appeal":
		for _, appeal := range s.features.Snapshot().MarketAppeals {
			if appeal.ID != req.MarketAppeal.ID {
				continue
			}
			go s.dispatchPlatformEvent("market.appeal.resolved", map[string]any{"owner_id": appeal.OwnerID, "appeal_id": appeal.ID, "report_id": appeal.ReportID, "status": appeal.Status, "resolution": appeal.Resolution})
			return
		}
	}
}

func (s *Server) platformSnapshot() map[string]any {
	data := s.features.Snapshot()
	nodes := s.store.AdminNodes(s.cfg.OfflineWait)
	versions := map[string]int{}
	for _, node := range nodes {
		version := strings.TrimSpace(node.AgentVersion)
		if version == "" {
			version = "unknown"
		}
		versions[version]++
	}
	backupTrust := map[string]any{"enabled": s.backupReady() == nil, "count": 0, "latest": nil, "last_drill": nil}
	if backups, err := s.listBackups(); err == nil {
		backupTrust["count"] = len(backups)
		if len(backups) > 0 {
			backupTrust["latest"] = backups[0]
		}
	}
	if len(data.BackupDrills) > 0 {
		backupTrust["last_drill"] = data.BackupDrills[len(data.BackupDrills)-1]
	}
	return map[string]any{
		"data":           data,
		"agent_versions": versions,
		"backup_trust":   backupTrust,
		"price_analysis": s.marketPriceAnalysis(),
		"setup":          s.setupChecklist(data),
	}
}

func (s *Server) applyPlatformAction(req platformAdminRequest) (map[string]any, error) {
	now := time.Now().Unix()
	if req.Action == "run_backup_drill" {
		return s.runBackupDrill()
	}
	h := s.features
	h.mu.Lock()
	defer h.mu.Unlock()
	result := map[string]any{"ok": true}
	switch strings.TrimSpace(req.Action) {
	case "save_node_profile":
		item := req.NodeProfile
		if !validNodeID(item.NodeID) {
			return nil, fmt.Errorf("invalid node_id")
		}
		item.Group = cleanFeatureText(item.Group, 48)
		item.Tags = cleanFeatureList(item.Tags, 16, 32)
		item.Notes = cleanFeatureText(item.Notes, 300)
		h.data.NodeProfiles[item.NodeID] = item
	case "save_view":
		item := req.SavedView
		if item.ID == "" {
			var err error
			item.ID, err = newFeatureID("view")
			if err != nil {
				return nil, err
			}
			item.CreatedAt = now
		}
		if !validateFeatureID(item.ID) || cleanFeatureText(item.Name, 64) == "" {
			return nil, fmt.Errorf("view name is required")
		}
		item.Name = cleanFeatureText(item.Name, 64)
		item.NodeIDs = cleanFeatureList(item.NodeIDs, 200, 80)
		item.Groups = cleanFeatureList(item.Groups, 30, 48)
		item.Tags = cleanFeatureList(item.Tags, 30, 32)
		item.UpdatedAt = now
		h.data.SavedViews = upsertSavedView(h.data.SavedViews, item)
		result["id"] = item.ID
	case "delete_view":
		h.data.SavedViews = deleteSavedView(h.data.SavedViews, req.ID)
	case "save_maintenance":
		item := req.Maintenance
		if item.ID == "" {
			item.ID, _ = newFeatureID("maint")
			item.CreatedAt = now
		}
		item.Name = cleanFeatureText(item.Name, 80)
		item.NodeIDs = cleanFeatureList(item.NodeIDs, 200, 80)
		item.Tags = cleanFeatureList(item.Tags, 30, 32)
		if item.Name == "" || item.StartAt <= 0 || item.EndAt <= item.StartAt {
			return nil, fmt.Errorf("valid maintenance name and time range are required")
		}
		h.data.Maintenance = upsertMaintenance(h.data.Maintenance, item)
		result["id"] = item.ID
	case "delete_maintenance":
		h.data.Maintenance = deleteMaintenance(h.data.Maintenance, req.ID)
	case "save_status_page":
		item := req.StatusPage
		if item.ID == "" {
			item.ID, _ = newFeatureID("status")
			item.CreatedAt = now
		}
		item.Slug = strings.ToLower(strings.TrimSpace(item.Slug))
		item.Name = cleanFeatureText(item.Name, 80)
		item.Description = cleanFeatureText(item.Description, 300)
		item.NodeIDs = cleanFeatureList(item.NodeIDs, 200, 80)
		item.ServiceIDs = cleanFeatureList(item.ServiceIDs, 200, 80)
		item.Tags = cleanFeatureList(item.Tags, 30, 32)
		item.UpdatedAt = now
		if item.Name == "" || !platformSlugPattern.MatchString(item.Slug) || statusSlugUsed(h.data.StatusPages, item.ID, item.Slug) {
			return nil, fmt.Errorf("status page name and unique slug are required")
		}
		h.data.StatusPages = upsertStatusPage(h.data.StatusPages, item)
		result["id"] = item.ID
	case "delete_status_page":
		h.data.StatusPages = deleteStatusPage(h.data.StatusPages, req.ID)
	case "save_incident":
		item := req.Incident
		if item.ID == "" {
			item.ID, _ = newFeatureID("incident")
		}
		item.Title = cleanFeatureText(item.Title, 100)
		item.Message = cleanFeatureText(item.Message, 500)
		switch item.Status {
		case "identified", "monitoring":
			item.ResolvedAt = 0
		case "resolved":
			if item.ResolvedAt == 0 {
				item.ResolvedAt = now
			}
		default:
			item.Status = "investigating"
			item.ResolvedAt = 0
		}
		if item.StartedAt == 0 {
			item.StartedAt = now
		}
		if item.Title == "" || !statusPageExists(h.data.StatusPages, item.StatusPageID) {
			return nil, fmt.Errorf("valid status page and incident title are required")
		}
		h.data.Incidents = upsertIncident(h.data.Incidents, item)
		result["id"] = item.ID
	case "create_api_key":
		item := req.APIKey
		item.ID, _ = newFeatureID("key")
		token, prefix, hash, err := newAPIKey()
		if err != nil {
			return nil, err
		}
		item.Name = cleanFeatureText(item.Name, 64)
		item.Scopes = allowedAPIScopes(item.Scopes)
		if item.Name == "" || len(item.Scopes) == 0 || item.ExpiresAt < 0 {
			return nil, fmt.Errorf("API key name and scope are required")
		}
		item.Prefix, item.KeyHash, item.Enabled, item.CreatedAt, item.LastUsedAt = prefix, hash, true, now, 0
		h.data.APIKeys = append(h.data.APIKeys, item)
		result["id"], result["token"] = item.ID, token
	case "revoke_api_key":
		for i := range h.data.APIKeys {
			if h.data.APIKeys[i].ID == req.ID {
				h.data.APIKeys[i].Enabled = false
			}
		}
	case "save_webhook":
		item := req.Webhook
		if item.ID == "" {
			item.ID, _ = newFeatureID("hook")
			item.CreatedAt = now
		}
		current, _ := findWebhook(h.data.Webhooks, item.ID)
		var err error
		item, req.WebhookSecret, err = validateWebhookIntegration(item, req.WebhookSecret, current)
		if err != nil {
			return nil, err
		}
		item.UpdatedAt = now
		if req.WebhookSecret != "" {
			ciphertext, err := h.cipher.encrypt(req.WebhookSecret)
			if err != nil {
				return nil, err
			}
			item.SecretCipher = ciphertext
		} else if current, ok := findWebhook(h.data.Webhooks, item.ID); ok {
			item.SecretCipher = current.SecretCipher
		}
		h.data.Webhooks = upsertWebhook(h.data.Webhooks, item)
		result["id"] = item.ID
	case "delete_webhook":
		h.data.Webhooks = deleteWebhook(h.data.Webhooks, req.ID)
	case "save_rollout":
		item := req.Rollout
		if item.ID == "" {
			item.ID, _ = newFeatureID("rollout")
			item.CreatedAt = now
			item.NodeStates = map[string]string{}
		}
		item.Name = cleanFeatureText(item.Name, 80)
		item.TargetVersion = cleanFeatureText(item.TargetVersion, 32)
		item.RollbackVersion = cleanFeatureText(item.RollbackVersion, 32)
		item.NodeIDs = cleanFeatureList(item.NodeIDs, 500, 80)
		item.Tags = cleanFeatureList(item.Tags, 30, 32)
		item.CanaryPercent = clampInt(item.CanaryPercent, 1, 100)
		var err error
		item.Artifacts, err = normalizeUpgradeArtifacts(item.Artifacts)
		if err != nil {
			return nil, err
		}
		item.RollbackArtifacts, err = normalizeUpgradeArtifacts(item.RollbackArtifacts)
		if err != nil {
			return nil, err
		}
		if item.State != "paused" && item.State != "completed" && item.State != "rollback" {
			item.State = "active"
		}
		item.UpdatedAt = now
		if item.Name == "" || item.TargetVersion == "" || item.State == "rollback" && (item.RollbackVersion == "" || len(item.RollbackArtifacts) == 0) {
			return nil, fmt.Errorf("rollout name and target version are required")
		}
		h.data.UpgradeRollouts = upsertRollout(h.data.UpgradeRollouts, item)
		result["id"] = item.ID
	case "save_seller_trust":
		item := req.SellerTrust
		if _, ok := s.store.GetOwnerByID(item.OwnerID); !ok {
			return nil, fmt.Errorf("owner not found")
		}
		item.Level = strings.ToLower(cleanFeatureText(item.Level, 16))
		if item.Level != "trusted" && item.Level != "watch" {
			item.Level = "standard"
		}
		item.Note = cleanFeatureText(item.Note, 300)
		item.Violations = clampInt(item.Violations, 0, 999)
		item.UpdatedAt = now
		h.data.SellerTrust[item.OwnerID] = item
	case "resolve_market_report":
		item, ok := findMarketReport(h.data.MarketReports, req.MarketReport.ID)
		if !ok {
			return nil, fmt.Errorf("report not found")
		}
		status := strings.ToLower(strings.TrimSpace(req.MarketReport.Status))
		if status != "resolved" && status != "rejected" {
			return nil, fmt.Errorf("report resolution must be resolved or rejected")
		}
		item.Status = status
		item.Resolution = cleanFeatureMessage(req.MarketReport.Resolution, 500)
		item.ResolvedAt, item.UpdatedAt = now, now
		var err error
		h.data.MarketReports, err = upsertMarketReport(h.data.MarketReports, item)
		if err != nil {
			return nil, err
		}
	case "resolve_market_appeal":
		var item *MarketAppeal
		for i := range h.data.MarketAppeals {
			if h.data.MarketAppeals[i].ID == req.MarketAppeal.ID {
				item = &h.data.MarketAppeals[i]
				break
			}
		}
		if item == nil {
			return nil, fmt.Errorf("appeal not found")
		}
		status := strings.ToLower(strings.TrimSpace(req.MarketAppeal.Status))
		if status != "accepted" && status != "rejected" {
			return nil, fmt.Errorf("appeal resolution must be accepted or rejected")
		}
		item.Status = status
		item.Resolution = cleanFeatureMessage(req.MarketAppeal.Resolution, 500)
		item.ResolvedAt, item.UpdatedAt = now, now
	case "save_setup":
		key := cleanFeatureText(req.SetupKey, 48)
		if key == "" {
			return nil, fmt.Errorf("setup key is required")
		}
		h.data.SetupCompleted[key] = req.SetupDone
	default:
		return nil, fmt.Errorf("unknown platform action")
	}
	if err := h.saveLocked(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Server) runBackupDrill() (map[string]any, error) {
	start := time.Now()
	record := BackupDrillRecord{Status: "failed", CreatedAt: start.Unix()}
	record.ID, _ = newFeatureID("drill")
	backups, err := s.listBackups()
	if err == nil && len(backups) == 0 {
		err = fmt.Errorf("no backup available")
	}
	if err == nil {
		record.BackupID = backups[0].ID
		_, err = s.readAndVerifyBackup(backups[0].ID)
	}
	if err == nil {
		record.Status, record.Message = "passed", "Encrypted package, manifest and file hashes verified"
	} else {
		record.Message = err.Error()
	}
	record.Duration = time.Since(start).Milliseconds()
	s.features.mu.Lock()
	s.features.data.BackupDrills = append(s.features.data.BackupDrills, record)
	if len(s.features.data.BackupDrills) > 100 {
		s.features.data.BackupDrills = s.features.data.BackupDrills[len(s.features.data.BackupDrills)-100:]
	}
	saveErr := s.features.saveLocked()
	s.features.mu.Unlock()
	if saveErr != nil {
		return nil, saveErr
	}
	if err != nil {
		return map[string]any{"ok": false, "record": record}, nil
	}
	return map[string]any{"ok": true, "record": record}, nil
}

func (s *Server) maybeRunScheduledBackupDrill() {
	if s.features == nil || s.backupReady() != nil {
		return
	}
	snapshot := s.features.Snapshot()
	if len(snapshot.BackupDrills) > 0 && time.Since(time.Unix(snapshot.BackupDrills[len(snapshot.BackupDrills)-1].CreatedAt, 0)) < 7*24*time.Hour {
		return
	}
	backups, err := s.listBackups()
	if err != nil || len(backups) == 0 || !s.backupMu.TryLock() {
		return
	}
	defer s.backupMu.Unlock()
	result, err := s.runBackupDrill()
	if err != nil {
		s.dispatchPlatformEvent("backup.failed", map[string]any{"reason": "verification_drill", "error": cleanFeatureText(err.Error(), 240), "created_at": time.Now().Unix()})
		return
	}
	if ok, _ := result["ok"].(bool); !ok {
		s.dispatchPlatformEvent("backup.failed", map[string]any{"reason": "verification_drill", "record": result["record"], "created_at": time.Now().Unix()})
	}
}

func (s *Server) marketPriceAnalysis() []map[string]any {
	groups := map[string][]float64{}
	rates := manualExchangeRate(s.store.GetSettings().ManualUSDCNY, true, "fallback")
	if s.exchangeRates != nil {
		rates = s.exchangeRates.Snapshot(s.store.GetSettings())
	}
	for _, listing := range s.buildMarketViews(s.store.ListMarketListings(true), false) {
		monthly := monthlyPriceInCurrency(listing.PriceAmount, listing.PriceCurrency, listing.BillingCycle, "CNY", rates)
		if monthly <= 0 {
			continue
		}
		region := listing.Region
		if region == "" {
			region = listing.RegionCode
		}
		if region == "" {
			region = "Other"
		}
		groups[region] = append(groups[region], monthly)
	}
	out := make([]map[string]any, 0, len(groups))
	for region, values := range groups {
		sort.Float64s(values)
		total := 0.0
		for _, value := range values {
			total += value
		}
		median := values[len(values)/2]
		if len(values)%2 == 0 {
			median = (values[len(values)/2-1] + values[len(values)/2]) / 2
		}
		out = append(out, map[string]any{"region": region, "count": len(values), "currency": "CNY", "cycle": "monthly", "min": values[0], "average": total / float64(len(values)), "median": median, "max": values[len(values)-1]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["region"].(string) < out[j]["region"].(string) })
	return out
}

func (s *Server) setupChecklist(data PlatformFeatureData) []map[string]any {
	settings := s.store.GetSettings()
	return []map[string]any{
		{"key": "public_url", "label": "Public URL / HTTPS", "done": strings.HasPrefix(strings.ToLower(s.cfg.PublicURL), "https://")},
		{"key": "backup", "label": "Encrypted backup", "done": s.backupReady() == nil},
		{"key": "notification", "label": "Alert notification", "done": s.alerts != nil && (s.alerts.Settings().WebhookURL != "" || s.alerts.Settings().TokenConfigured)},
		{"key": "status_page", "label": "Public status page", "done": len(data.StatusPages) > 0},
		{"key": "site", "label": "Site identity", "done": strings.TrimSpace(settings.SiteName) != ""},
	}
}

func (s *Server) handleAdminDiagnostics(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		http.Error(w, "admin login required", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	info := map[string]any{"generated_at": time.Now().UTC().Format(time.RFC3339), "version": buildVersion, "commit": buildCommit, "build_time": buildTime, "store_driver": s.cfg.StoreDriver, "nodes": len(s.store.AdminNodes(s.cfg.OfflineWait)), "features": s.diagnosticPlatformSummary(), "health": map[string]any{"store": s.store != nil, "sessions": s.sessions != nil, "frontend": hasStaticBuild()}, "paths": map[string]string{"data_file": filepath.Base(s.cfg.DataPath), "backup_dir": filepath.Base(s.cfg.BackupDir)}}
	w.Header().Set("Content-Disposition", "attachment; filename=monitor-diagnostics.json")
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, info)
}

func (s *Server) diagnosticPlatformSummary() map[string]any {
	data := s.features.Snapshot()
	versions := map[string]int{}
	for _, node := range s.store.AdminNodes(s.cfg.OfflineWait) {
		version := cleanFeatureText(node.AgentVersion, 32)
		if version == "" {
			version = "unknown"
		}
		versions[version]++
	}
	return map[string]any{
		"counts": map[string]int{
			"node_profiles": len(data.NodeProfiles), "saved_views": len(data.SavedViews),
			"maintenance": len(data.Maintenance), "status_pages": len(data.StatusPages),
			"incidents": len(data.Incidents), "api_keys": len(data.APIKeys),
			"webhooks": len(data.Webhooks), "webhook_deliveries": len(data.WebhookDeliveries),
			"upgrade_rollouts": len(data.UpgradeRollouts), "market_reports": len(data.MarketReports),
			"market_appeals": len(data.MarketAppeals), "subscriptions": len(data.Subscriptions),
		},
		"agent_versions": versions,
		"setup":          s.setupChecklist(data),
		"updated_at":     data.UpdatedAt,
	}
}

func (s *Server) handlePublicStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	slug := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/status/"), "/")
	page, incidents, ok := s.features.StatusPage(slug)
	if !ok {
		http.Error(w, "status page not found", http.StatusNotFound)
		return
	}
	nodes := s.statusPageNodes(page)
	overall := "operational"
	for _, node := range nodes {
		if !node.Online {
			overall = "degraded"
			break
		}
	}
	for _, incident := range incidents {
		if incident.Status != "resolved" {
			overall = "incident"
			break
		}
	}
	w.Header().Set("Cache-Control", "public, max-age=15")
	services := s.statusPageServices(page)
	for _, service := range services {
		if service.Status == "down" {
			overall = "degraded"
			break
		}
	}
	writeJSON(w, map[string]any{"page": page, "overall": overall, "nodes": nodes, "services": services, "incidents": incidents, "updated_at": time.Now().Unix()})
}

type publicServiceStatus struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Kind        string  `json:"kind"`
	Status      string  `json:"status"`
	LastRunAt   int64   `json:"last_run_at,omitempty"`
	LastSuccess int64   `json:"last_success_at,omitempty"`
	LatencyMS   int64   `json:"latency_ms,omitempty"`
	UptimePct   float64 `json:"uptime_percent"`
}

func (s *Server) statusPageServices(page PublicStatusPage) []publicServiceStatus {
	if len(page.ServiceIDs) == 0 {
		return []publicServiceStatus{}
	}
	out := make([]publicServiceStatus, 0, len(page.ServiceIDs))
	for _, service := range s.store.ListServiceMonitors("", true) {
		if !listContains(page.ServiceIDs, service.ID) {
			continue
		}
		state, _ := s.store.GetServiceMonitorState(service.ID)
		status := state.Status
		if status == "" {
			status = "unknown"
		}
		results := s.store.ListServiceMonitorResults(service.ID, 1000)
		var successes int
		var latencyTotal int64
		for _, result := range results {
			if result.Success {
				successes++
			}
			latencyTotal += result.LatencyMS
		}
		latency, uptime := int64(0), float64(0)
		if len(results) > 0 {
			latency = latencyTotal / int64(len(results))
			uptime = float64(successes) * 100 / float64(len(results))
		}
		out = append(out, publicServiceStatus{ID: service.ID, Name: service.Name, Kind: service.Kind, Status: status, LastRunAt: service.LastRunAt, LastSuccess: state.LastSuccessAt, LatencyMS: latency, UptimePct: uptime})
	}
	return out
}

type publicStatusNode struct {
	NodeID      string  `json:"node_id"`
	DisplayName string  `json:"display_name"`
	Region      string  `json:"region,omitempty"`
	Online      bool    `json:"online"`
	LastSeen    int64   `json:"last_seen"`
	CPU         float64 `json:"cpu,omitempty"`
	Memory      float64 `json:"memory,omitempty"`
}

func (s *Server) statusPageNodes(page PublicStatusPage) []publicStatusNode {
	hosts := map[string]AkileHost{}
	for _, host := range s.store.AkileHosts() {
		hosts[host.Host.Name] = host
	}
	out := []publicStatusNode{}
	for _, node := range s.store.AdminNodes(s.cfg.OfflineWait) {
		if !s.nodeIsPublic(node.NodeID) {
			continue
		}
		if len(page.NodeIDs) > 0 || len(page.Tags) > 0 {
			selected := listContains(page.NodeIDs, node.NodeID)
			if !selected && len(page.Tags) > 0 {
				selected = s.features.NodeMatches(node.NodeID, page.Tags)
			}
			if !selected {
				continue
			}
		}
		item := publicStatusNode{NodeID: node.NodeID, DisplayName: node.DisplayName, Region: node.Region, Online: node.Online, LastSeen: node.LastSeen}
		if page.ShowMetrics {
			host := hosts[node.NodeID]
			item.CPU = host.State.CPU
			item.Memory = percent(host.State.MemUsed, host.Host.MemTotal)
		}
		out = append(out, item)
	}
	return out
}

func (s *Server) apiKeyAuthorized(r *http.Request, scope string) bool {
	token := strings.TrimSpace(r.Header.Get("X-API-Key"))
	if token == "" {
		token = strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	}
	_, ok := s.features.APIKey(token, scope)
	return ok
}

func (s *Server) handlePublicAPINodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if !s.apiKeyAuthorized(r, "nodes:read") {
		http.Error(w, "valid API key required", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(s.cachedHostsJSON())
}

func (s *Server) handlePublicAPIStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if !s.apiKeyAuthorized(r, "status:read") {
		http.Error(w, "valid API key required", http.StatusUnauthorized)
		return
	}
	writeJSON(w, s.operationsOverview())
}

func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, openAPISpec())
}

func openAPISpec() map[string]any {
	return map[string]any{"openapi": "3.1.0", "info": map[string]any{"title": "Yunjing Monitor API", "version": buildVersion}, "components": map[string]any{"securitySchemes": map[string]any{"ApiKey": map[string]any{"type": "apiKey", "in": "header", "name": "X-API-Key"}}}, "paths": map[string]any{"/api/v1/nodes": map[string]any{"get": map[string]any{"summary": "List redacted node metrics", "security": []any{map[string]any{"ApiKey": []string{}}}}}, "/api/v1/status": map[string]any{"get": map[string]any{"summary": "Operations status", "security": []any{map[string]any{"ApiKey": []string{}}}}}, "/api/status/{slug}": map[string]any{"get": map[string]any{"summary": "Public status page"}}}}
}

func (s *Server) handleOwnerSubscriptions(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.ownerAuthorized(r)
	if !ok {
		http.Error(w, "owner login required", http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodGet:
		data := s.features.Snapshot()
		items := []MarketSubscription{}
		for _, item := range data.Subscriptions {
			if item.OwnerID == owner.ID {
				items = append(items, item)
			}
		}
		writeJSON(w, items)
	case http.MethodPost:
		if !s.store.GetSettings().MarketEnabled {
			http.Error(w, "market is currently disabled", http.StatusServiceUnavailable)
			return
		}
		if !s.validAdminOrigin(r) {
			http.Error(w, "invalid request origin", http.StatusForbidden)
			return
		}
		var req struct {
			Action       string             `json:"action"`
			ID           string             `json:"id"`
			Subscription MarketSubscription `json:"subscription"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req) != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		s.features.mu.Lock()
		if req.Action == "delete" {
			out := s.features.data.Subscriptions[:0]
			for _, item := range s.features.data.Subscriptions {
				if item.ID != req.ID || item.OwnerID != owner.ID {
					out = append(out, item)
				}
			}
			s.features.data.Subscriptions = out
		} else {
			item := req.Subscription
			if item.ID == "" {
				item.ID, _ = newFeatureID("subscription")
				item.CreatedAt = time.Now().Unix()
			} else if current, ok := findOwnerSubscription(s.features.data.Subscriptions, item.ID, owner.ID); ok {
				item.CreatedAt = current.CreatedAt
				item.MatchedNodeIDs = current.MatchedNodeIDs
				item.MatchCount = current.MatchCount
				item.LastMatchedAt = current.LastMatchedAt
			} else {
				s.features.mu.Unlock()
				http.Error(w, "subscription not found", http.StatusNotFound)
				return
			}
			item.OwnerID = owner.ID
			item.Name = cleanFeatureText(item.Name, 64)
			item.Regions = cleanFeatureList(item.Regions, 20, 32)
			item.Tags = cleanFeatureList(item.Tags, 20, 32)
			item.Currency = strings.ToUpper(cleanFeatureText(item.Currency, 8))
			if !listContains([]string{"CNY", "USD", "HKD", "EUR", "JPY"}, item.Currency) {
				item.Currency = "CNY"
			}
			item.UpdatedAt = time.Now().Unix()
			if item.Name == "" || !validateFeatureID(item.ID) || item.MaxPrice < 0 || item.MaxPrice > 1e9 || item.MinMemory > 16<<40 {
				s.features.mu.Unlock()
				http.Error(w, "valid subscription name is required", http.StatusBadRequest)
				return
			}
			s.features.data.Subscriptions = upsertSubscription(s.features.data.Subscriptions, item)
		}
		err := s.features.saveLocked()
		s.features.mu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	default:
		methodNotAllowed(w)
	}
}

func findOwnerSubscription(items []MarketSubscription, id, ownerID string) (MarketSubscription, bool) {
	for _, item := range items {
		if item.ID == id && item.OwnerID == ownerID {
			return item, true
		}
	}
	return MarketSubscription{}, false
}

func allowedAPIScopes(values []string) []string {
	allowed := map[string]bool{"nodes:read": true, "status:read": true, "*": true}
	out := []string{}
	for _, value := range cleanFeatureList(values, 5, 32) {
		if allowed[value] {
			out = append(out, value)
		}
	}
	return out
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func statusSlugUsed(items []PublicStatusPage, id, slug string) bool {
	for _, item := range items {
		if item.ID != id && item.Slug == slug {
			return true
		}
	}
	return false
}

func statusPageExists(items []PublicStatusPage, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}
func findWebhook(items []WebhookIntegration, id string) (WebhookIntegration, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return WebhookIntegration{}, false
}
func upsertSavedView(items []SavedNodeView, value SavedNodeView) []SavedNodeView {
	for i := range items {
		if items[i].ID == value.ID {
			items[i] = value
			return items
		}
	}
	return append(items, value)
}
func deleteSavedView(items []SavedNodeView, id string) []SavedNodeView {
	out := items[:0]
	for _, item := range items {
		if item.ID != id {
			out = append(out, item)
		}
	}
	return out
}
func upsertMaintenance(items []MaintenanceWindow, value MaintenanceWindow) []MaintenanceWindow {
	for i := range items {
		if items[i].ID == value.ID {
			items[i] = value
			return items
		}
	}
	return append(items, value)
}
func deleteMaintenance(items []MaintenanceWindow, id string) []MaintenanceWindow {
	out := items[:0]
	for _, item := range items {
		if item.ID != id {
			out = append(out, item)
		}
	}
	return out
}
func upsertStatusPage(items []PublicStatusPage, value PublicStatusPage) []PublicStatusPage {
	for i := range items {
		if items[i].ID == value.ID {
			items[i] = value
			return items
		}
	}
	return append(items, value)
}
func deleteStatusPage(items []PublicStatusPage, id string) []PublicStatusPage {
	out := items[:0]
	for _, item := range items {
		if item.ID != id {
			out = append(out, item)
		}
	}
	return out
}
func upsertIncident(items []StatusIncident, value StatusIncident) []StatusIncident {
	for i := range items {
		if items[i].ID == value.ID {
			items[i] = value
			return items
		}
	}
	return append(items, value)
}
func upsertWebhook(items []WebhookIntegration, value WebhookIntegration) []WebhookIntegration {
	for i := range items {
		if items[i].ID == value.ID {
			items[i] = value
			return items
		}
	}
	return append(items, value)
}
func deleteWebhook(items []WebhookIntegration, id string) []WebhookIntegration {
	out := items[:0]
	for _, item := range items {
		if item.ID != id {
			out = append(out, item)
		}
	}
	return out
}
func upsertRollout(items []AgentUpgradeRollout, value AgentUpgradeRollout) []AgentUpgradeRollout {
	for i := range items {
		if items[i].ID == value.ID {
			if value.NodeStates == nil {
				value.NodeStates = items[i].NodeStates
			}
			items[i] = value
			return items
		}
	}
	return append(items, value)
}
func upsertSubscription(items []MarketSubscription, value MarketSubscription) []MarketSubscription {
	for i := range items {
		if items[i].ID == value.ID && items[i].OwnerID == value.OwnerID {
			items[i] = value
			return items
		}
	}
	return append(items, value)
}

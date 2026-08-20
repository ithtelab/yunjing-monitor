package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func (s *Server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.validAdminOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return
	}
	var req struct {
		Username   string `json:"username"`
		Password   string `json:"password"`
		RememberMe bool   `json:"remember_me"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	limitIPKey := loginLimitKey(requestClientIP(r), "")
	limitIdentityKey := loginLimitKey(requestClientIP(r), req.Username)
	if !s.adminLoginLimiter.Allowed(limitIPKey) || !s.adminLoginLimiter.Allowed(limitIdentityKey) {
		http.Error(w, "too many login attempts", http.StatusTooManyRequests)
		return
	}
	if !constantEqual(req.Username, s.cfg.AdminUser) || !constantEqual(req.Password, s.cfg.AdminPass) {
		s.adminLoginLimiter.Failure(limitIPKey)
		s.adminLoginLimiter.Failure(limitIdentityKey)
		time.Sleep(300 * time.Millisecond)
		http.Error(w, "invalid admin credentials", http.StatusUnauthorized)
		return
	}
	s.adminLoginLimiter.Success(limitIPKey)
	s.adminLoginLimiter.Success(limitIdentityKey)
	ttl := defaultSessionTTL
	if req.RememberMe {
		ttl = rememberedSessionTTL
	}
	token, err := s.sessions.CreateForDuration(SessionKindAdmin, "admin", ttl)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, adminCookie(r, token, ttl, req.RememberMe))
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.validAdminOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return
	}
	if cookie, err := r.Cookie("monitor_admin"); err == nil {
		s.sessions.Delete(cookie.Value)
	}
	c := adminCookie(r, "", -time.Hour, true)
	http.SetCookie(w, c)
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleAdminMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, map[string]bool{"authenticated": s.adminAuthorized(r)})
}

func (s *Server) handleAdminNodes(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		http.Error(w, "admin login required", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet && !s.validAdminOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, s.store.AdminNodes(s.cfg.OfflineWait))
	case http.MethodPost:
		var req struct {
			NodeID      string `json:"node_id"`
			DisplayName string `json:"display_name"`
			Region      string `json:"region"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		req.NodeID = strings.TrimSpace(req.NodeID)
		req.DisplayName = strings.TrimSpace(req.DisplayName)
		req.Region = strings.TrimSpace(req.Region)
		// Prefer display name as human label; auto-generate node_id when omitted.
		if req.NodeID == "" {
			id, err := newNodeID()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			req.NodeID = id
		}
		if !validNodeID(req.NodeID) {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
			return
		}
		if req.DisplayName == "" {
			req.DisplayName = req.NodeID
		}
		displayName, region, regionCode := resolveNodeMeta(req.DisplayName, req.Region, req.NodeID)
		if err := s.store.AddPlannedNode(req.NodeID, s.cfg.MaxNodes); err != nil {
			http.Error(w, err.Error(), http.StatusTooManyRequests)
			return
		}
		if err := s.store.SetNodeMeta(req.NodeID, displayName, region, regionCode); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.cache.MarkDirty()
		writeJSON(w, map[string]any{
			"ok":           true,
			"node_id":      req.NodeID,
			"display_name": displayName,
			"region":       region,
			"region_code":  regionCode,
		})
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleAdminNodesExport(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		http.Error(w, "admin login required", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename=monitor-nodes.json")
	writeJSON(w, s.store.ExportNodes())
}

func (s *Server) handleAdminNodesImport(w http.ResponseWriter, r *http.Request) {
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
	defer r.Body.Close()
	var backup NodeBackup
	if err := json.NewDecoder(io.LimitReader(r.Body, 10<<20)).Decode(&backup); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	imported, err := s.store.ImportNodes(backup, s.cfg.MaxNodes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.cache.MarkDirty()
	writeJSON(w, map[string]int{"imported": imported})
}

func (s *Server) handleAdminInstallCommand(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		http.Error(w, "admin login required", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	base, err := s.externalBase(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !s.validAdminOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return
	}
	nodeID := strings.TrimSpace(r.URL.Query().Get("node_id"))
	platform := strings.TrimSpace(r.URL.Query().Get("platform"))
	reset := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("reset")), "1") ||
		strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("reset")), "true")
	if !validNodeID(nodeID) {
		http.Error(w, "invalid node_id", http.StatusBadRequest)
		return
	}
	linuxUninstall := fmt.Sprintf("curl -fsSL %s/uninstall/agent-linux.sh | sudo sh", base)
	windowsUninstall := fmt.Sprintf("powershell -ExecutionPolicy Bypass -Command \"[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12; iwr %s/uninstall/agent-windows.ps1 -UseBasicParsing | iex\"", base)
	// 卸载命令不包含 token，直接返回，避免无谓轮换节点 token 导致在线 Agent 掉线
	if platform == "linux-uninstall" {
		writeJSON(w, map[string]string{"command": linuxUninstall})
		return
	}
	if platform == "windows-uninstall" {
		writeJSON(w, map[string]string{"command": windowsUninstall})
		return
	}

	token, rotated, err := s.issueOrReuseNodeToken(nodeID, reset)
	if err != nil {
		if strings.Contains(err.Error(), "legacy") {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	linux := fmt.Sprintf("curl -fsSL %s/install/agent-linux.sh | sudo sh -s -- --server %s --token %s --node-id %s", base, base, shellQuote(token), shellQuote(nodeID))
	windows := fmt.Sprintf("powershell -ExecutionPolicy Bypass -Command \"[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12; iwr %s/install/agent-windows.ps1 -UseBasicParsing | iex; Install-VpsAgent -Server '%s' -Token '%s' -NodeId '%s'\"", base, base, psQuote(token), psQuote(nodeID))
	payload := map[string]any{
		"linux":             linux,
		"windows":           windows,
		"linux_uninstall":   linuxUninstall,
		"windows_uninstall": windowsUninstall,
		"rotated":           rotated,
		"node_id":           nodeID,
	}
	if platform == "linux" {
		writeJSON(w, map[string]any{"command": linux, "rotated": rotated, "node_id": nodeID})
		return
	}
	if platform == "windows" {
		writeJSON(w, map[string]any{"command": windows, "rotated": rotated, "node_id": nodeID})
		return
	}
	writeJSON(w, payload)
}

// issueOrReuseNodeToken returns an existing plaintext token when possible.
// reset=true always issues a new token. Legacy nodes that only store a hash
// require reset=true (cannot re-show the original install command).
func (s *Server) issueOrReuseNodeToken(nodeID string, reset bool) (token string, rotated bool, err error) {
	if !reset {
		if existing, ok := s.store.GetNodeToken(nodeID); ok {
			return existing, false, nil
		}
		// Has hash but no plaintext → cannot re-view; force explicit reset.
		nodes := s.store.AdminNodes(s.cfg.OfflineWait)
		for _, n := range nodes {
			if n.NodeID == nodeID && n.HasToken && !n.CanViewToken {
				return "", false, fmt.Errorf("legacy token cannot be re-viewed; pass reset=1 to rotate")
			}
		}
	}
	token, err = newAgentToken()
	if err != nil {
		return "", false, err
	}
	if err := s.store.SetNodeToken(nodeID, token, hashToken(token), s.cfg.MaxNodes); err != nil {
		return "", false, err
	}
	return token, true, nil
}

func (s *Server) handleAdminSettings(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		http.Error(w, "admin login required", http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, s.store.GetSettings())
	case http.MethodPost:
		if !s.validAdminOrigin(r) {
			http.Error(w, "invalid request origin", http.StatusForbidden)
			return
		}
		var raw map[string]json.RawMessage
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&raw); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		payload, err := json.Marshal(raw)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var req Settings
		if err := json.Unmarshal(payload, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		req.SiteName = strings.TrimSpace(req.SiteName)
		req.FooterText = strings.TrimSpace(req.FooterText)
		req.FriendLinksTitle = strings.TrimSpace(req.FriendLinksTitle)
		currentSettings := s.store.GetSettings()
		if _, present := raw["market_enabled"]; !present {
			req.MarketEnabled = currentSettings.MarketEnabled
		}
		if _, present := raw["user_registration_disabled"]; !present {
			req.UserRegistrationDisabled = currentSettings.UserRegistrationDisabled
		}
		if _, present := raw["user_node_apply_disabled"]; !present {
			req.UserNodeApplyDisabled = currentSettings.UserNodeApplyDisabled
		}
		if _, present := raw["user_node_limit"]; !present {
			req.UserNodeLimit = currentSettings.UserNodeLimit
		}
		if strings.TrimSpace(req.TimeZone) == "" {
			req.TimeZone = currentSettings.TimeZone
		}
		if strings.TrimSpace(req.ExchangeRateMode) == "" {
			req.ExchangeRateMode = currentSettings.ExchangeRateMode
		}
		if req.ManualUSDCNY == 0 {
			req.ManualUSDCNY = currentSettings.ManualUSDCNY
		}
		req.TimeZone = strings.TrimSpace(req.TimeZone)
		req.ExchangeRateMode = strings.ToLower(strings.TrimSpace(req.ExchangeRateMode))
		if req.SiteName == "" || len([]rune(req.SiteName)) > 64 || len([]rune(req.FooterText)) > 160 || len([]rune(req.FriendLinksTitle)) > 40 {
			http.Error(w, "invalid site_name", http.StatusBadRequest)
			return
		}
		if len(req.VisitorStatsItems) > 3 || len(req.VisitorStatsItems) > 0 && len(normalizeVisitorStatsItems(req.VisitorStatsItems)) != len(req.VisitorStatsItems) {
			http.Error(w, "select 1-3 valid visitor statistics", http.StatusBadRequest)
			return
		}
		if !validSiteTimeZone(req.TimeZone) || req.ExchangeRateMode != "auto" && req.ExchangeRateMode != "manual" || req.ManualUSDCNY < 1 || req.ManualUSDCNY > 20 || req.UserNodeLimit < 1 || req.UserNodeLimit > 100 {
			http.Error(w, "invalid time zone or exchange rate settings", http.StatusBadRequest)
			return
		}
		req = normalizeSettings(req)
		if err := s.store.UpdateSettings(req); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	default:
		methodNotAllowed(w)
	}
}

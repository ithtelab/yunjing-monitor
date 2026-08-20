package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"vps-agent/internal/agent"
	serverapp "vps-agent/internal/server/application"
)

type Server struct {
	cfg                     Config
	store                   serverapp.Store
	http                    *http.Server
	sessions                *SessionStore
	cache                   *ResponseCache
	captcha                 *CaptchaStore
	submitLimiter           *ipRateLimiter
	captchaLimiter          *ipRateLimiter
	adStatLimiter           *ipRateLimiter
	adGlobalLimiter         *ipRateLimiter
	visitorLimiter          *ipRateLimiter
	adminLoginLimiter       *loginFailureLimiter
	ownerLoginLimiter       *loginFailureLimiter
	marketReportLimiter     *ipRateLimiter
	notificationLimiter     *ipRateLimiter
	notificationPollLimiter *ipRateLimiter
	visitorStats            *VisitorStatsStore
	updates                 *UpdateService
	exchangeRates           *ExchangeRateService
	metricsHistory          *MetricsHistoryStore
	alerts                  *AlertService
	serviceMonitors         *ServiceMonitorService
	features                *PlatformFeatureHub
	backgroundOnce          sync.Once
	backgroundStop          chan struct{}
	backgroundDone          chan struct{}
	backupMu                sync.Mutex
	wsMu                    sync.Mutex
	wsConnections           int
	wsByIP                  map[string]int
}

func New(cfg Config) (*Server, error) {
	var err error
	cfg, err = normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}
	store, err := newStoreBackend(cfg)
	if err != nil {
		return nil, err
	}
	sessions, err := NewPersistentSessionStore(sessionStorePath(cfg.DataPath), cfg.AuthSecret)
	if err != nil {
		return nil, err
	}
	alerts, err := NewAlertService(alertDataPath(cfg.DataPath), cfg.AuthSecret)
	if err != nil {
		return nil, err
	}
	features, err := NewPlatformFeatureHub(platformFeaturePath(cfg.DataPath), cfg.AuthSecret)
	if err != nil {
		return nil, err
	}
	s := &Server{
		cfg:            cfg,
		store:          store,
		sessions:       sessions,
		cache:          NewResponseCache(),
		captcha:        NewCaptchaStore(),
		submitLimiter:  newIPRateLimiter(30 * time.Second),
		captchaLimiter: newIPRateLimiter(2 * time.Second),
		adStatLimiter:  newIPRateLimiter(time.Minute),
		// Analytics are intentionally best-effort. A single process-wide gate
		// prevents distributed IPs from forcing a full JSON rewrite per request.
		adGlobalLimiter:         newIPRateLimiter(100 * time.Millisecond),
		visitorLimiter:          newIPRateLimiter(time.Second),
		adminLoginLimiter:       newLoginFailureLimiter(5, 5*time.Minute, 15*time.Minute),
		ownerLoginLimiter:       newLoginFailureLimiter(5, 5*time.Minute, 15*time.Minute),
		marketReportLimiter:     newIPRateLimiter(2 * time.Minute),
		notificationLimiter:     newIPRateLimiter(time.Minute),
		notificationPollLimiter: newIPRateLimiter(time.Second),
		visitorStats:            NewVisitorStatsStore(visitorStatsPath(cfg.DataPath), cfg.AuthSecret),
		updates:                 NewUpdateService(cfg),
		exchangeRates:           NewExchangeRateService(cfg.DataPath),
		metricsHistory:          NewMetricsHistoryStore(metricsHistoryPath(cfg.DataPath)),
		alerts:                  alerts,
		features:                features,
		backgroundStop:          make(chan struct{}),
		backgroundDone:          make(chan struct{}),
		wsByIP:                  map[string]int{},
	}
	s.serviceMonitors = NewServiceMonitorService(store, alerts)
	alerts.SetEventHook(func(event AlertEvent) {
		name := "alert.firing"
		if event.Status != "firing" {
			name = "alert.recovered"
		}
		payload := map[string]any{
			"id": event.ID, "node_id": event.NodeID, "node_name": event.NodeName,
			"kind": event.Kind, "status": event.Status, "message": event.Message,
			"created_at": event.CreatedAt,
		}
		if event.OwnerID != "" {
			payload["owner_id"] = event.OwnerID
		} else if planned, ok := s.store.GetPlannedNode(event.NodeID); ok {
			payload["owner_id"] = planned.OwnerID
		}
		s.dispatchPlatformEvent(name, payload)
	})
	// Official release notes are embedded. Migration removes old duplicated
	// official HTML while preserving administrator-authored content.
	_ = s.ensureSiteContentMigrations()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/agent/ping", s.handleAgentPing)
	mux.HandleFunc("/api/agent/report", s.handleAgentReport)
	mux.HandleFunc("/api/agent/upgrade-manifest", s.handleAgentUpgradeManifest)
	mux.HandleFunc("/api/agent/upgrade-result", s.handleAgentUpgradeResult)
	mux.HandleFunc("/api/agent/probe-assignments", s.handleAgentProbeAssignments)
	mux.HandleFunc("/api/agent/probe-results", s.handleAgentProbeResults)
	mux.HandleFunc("/api/admin/login", s.handleAdminLogin)
	mux.HandleFunc("/api/admin/logout", s.handleAdminLogout)
	mux.HandleFunc("/api/admin/me", s.handleAdminMe)
	mux.HandleFunc("/api/admin/settings", s.handleAdminSettings)
	mux.HandleFunc("/api/admin/nodes", s.handleAdminNodes)
	mux.HandleFunc("/api/admin/nodes/export", s.handleAdminNodesExport)
	mux.HandleFunc("/api/admin/nodes/import", s.handleAdminNodesImport)
	mux.HandleFunc("/api/admin/install-command", s.handleAdminInstallCommand)
	mux.HandleFunc("/api/admin/market/listings", s.handleAdminMarketListings)
	mux.HandleFunc("/api/admin/market/pin", s.handleAdminMarketPin)
	mux.HandleFunc("/api/admin/market/delete", s.handleAdminMarketDelete)
	mux.HandleFunc("/api/admin/market/moderate", s.handleAdminMarketModerate)
	mux.HandleFunc("/api/admin/market/owners", s.handleAdminMarketOwners)
	mux.HandleFunc("/api/admin/market/owners/status", s.handleAdminMarketOwnerStatus)
	mux.HandleFunc("/api/admin/market/owners/reset-password", s.handleAdminMarketOwnerResetPassword)
	mux.HandleFunc("/api/admin/site/content", s.handleAdminSiteContent)
	mux.HandleFunc("/api/admin/release-notes", s.handleAdminReleaseNotes)
	mux.HandleFunc("/api/admin/backups", s.handleAdminBackups)
	mux.HandleFunc("/api/admin/backups/", s.handleAdminBackups)
	mux.HandleFunc("/api/admin/operations", s.handleAdminOperations)
	mux.HandleFunc("/api/admin/alerts", s.handleAdminAlerts)
	mux.HandleFunc("/api/admin/alerts/test", s.handleAdminAlertTest)
	mux.HandleFunc("/api/admin/service-monitors", s.handleAdminServiceMonitors)
	mux.HandleFunc("/api/admin/service-monitors/results", s.handleAdminServiceMonitorResults)
	mux.HandleFunc("/api/admin/friend-links", s.handleAdminFriendLinks)
	mux.HandleFunc("/api/admin/friend-links/save", s.handleAdminFriendLinkSave)
	mux.HandleFunc("/api/admin/friend-links/delete", s.handleAdminFriendLinkDelete)
	mux.HandleFunc("/api/admin/friend-links/reorder", s.handleAdminFriendLinkReorder)
	mux.HandleFunc("/api/admin/ads", s.handleAdminAds)
	mux.HandleFunc("/api/admin/ads/save", s.handleAdminAdSave)
	mux.HandleFunc("/api/admin/ads/delete", s.handleAdminAdDelete)
	mux.HandleFunc("/api/admin/ads/upload", s.handleAdminAdUpload)
	mux.HandleFunc("/api/admin/ads/settings", s.handleAdminAdLayout)
	mux.HandleFunc("/api/admin/update", s.handleAdminUpdate)
	mux.HandleFunc("/api/admin/update/apply", s.handleAdminUpdateApply)
	mux.HandleFunc("/api/admin/recycle-bin", s.handleAdminRecycleBin)
	mux.HandleFunc("/api/admin/recycle-bin/restore", s.handleAdminRecycleRestore)
	mux.HandleFunc("/api/admin/recycle-bin/purge", s.handleAdminRecyclePurge)
	mux.HandleFunc("/api/admin/audit", s.handleAdminAudit)
	mux.HandleFunc("/api/admin/platform", s.handleAdminPlatform)
	mux.HandleFunc("/api/admin/notifications", s.handleAdminNotifications)
	mux.HandleFunc("/api/admin/notifications/test", s.handleAdminNotificationTest)
	mux.HandleFunc("/api/admin/notifications/retry", s.handleAdminNotificationRetry)
	mux.HandleFunc("/api/admin/services", s.handleAdminServices)
	mux.HandleFunc("/api/admin/services/history", s.handleAdminServiceHistory)
	mux.HandleFunc("/api/admin/probe-points", s.handleAdminProbePoints)
	mux.HandleFunc("/api/admin/node-events", s.handleAdminNodeEvents)
	mux.HandleFunc("/api/admin/traffic-policies", s.handleAdminTrafficPolicies)
	mux.HandleFunc("/api/admin/diagnostics", s.handleAdminDiagnostics)
	mux.HandleFunc("/api/admin/openapi.json", s.handleOpenAPI)
	mux.HandleFunc("/api/market/listings", s.handleMarketListings)
	mux.HandleFunc("/api/market/categories", s.handleMarketCategories)
	mux.HandleFunc("/api/market/submit", s.handleMarketSubmit)
	mux.HandleFunc("/api/market/captcha", s.handleMarketCaptcha)
	mux.HandleFunc("/api/market/ads", s.handleMarketAds)
	mux.HandleFunc("/api/market/ads/impression", s.handleMarketAdImpression)
	mux.HandleFunc("/api/market/reports", s.handleMarketReport)
	mux.HandleFunc("/r/ad/", s.handleAdvertisementRedirect)
	mux.HandleFunc("/ads/", s.handleAdImage)
	mux.HandleFunc("/api/owner/login", s.handleOwnerLogin)
	mux.HandleFunc("/api/owner/logout", s.handleOwnerLogout)
	mux.HandleFunc("/api/owner/me", s.handleOwnerMe)
	mux.HandleFunc("/api/owner/nodes", s.handleOwnerNodes)
	mux.HandleFunc("/api/owner/nodes/toggle", s.handleOwnerNodesToggle)
	mux.HandleFunc("/api/owner/nodes/reset-token", s.handleOwnerNodesResetToken)
	mux.HandleFunc("/api/owner/nodes/update", s.handleOwnerNodesUpdate)
	mux.HandleFunc("/api/owner/nodes/delete", s.handleOwnerNodesDelete)
	mux.HandleFunc("/api/owner/subscriptions", s.handleOwnerSubscriptions)
	mux.HandleFunc("/api/owner/service-monitors", s.handleAccountServiceMonitors)
	mux.HandleFunc("/api/owner/service-monitors/results", s.handleAccountServiceMonitorResults)
	mux.HandleFunc("/api/owner/market-appeals", s.handleOwnerMarketAppeals)
	mux.HandleFunc("/api/account/captcha", s.handleAccountCaptcha)
	mux.HandleFunc("/api/account/register", s.handleAccountRegister)
	mux.HandleFunc("/api/account/login", s.handleOwnerLogin)
	mux.HandleFunc("/api/account/logout", s.handleOwnerLogout)
	mux.HandleFunc("/api/account/me", s.handleOwnerMe)
	mux.HandleFunc("/api/account/nodes", s.handleOwnerNodes)
	mux.HandleFunc("/api/account/nodes/privacy", s.handleAccountNodePrivacy)
	mux.HandleFunc("/api/account/nodes/listing", s.handleAccountNodeListing)
	mux.HandleFunc("/api/account/nodes/history", s.handleAccountNodeHistory)
	mux.HandleFunc("/api/account/nodes/toggle", s.handleOwnerNodesToggle)
	mux.HandleFunc("/api/account/nodes/reset-token", s.handleOwnerNodesResetToken)
	mux.HandleFunc("/api/account/nodes/update", s.handleOwnerNodesUpdate)
	mux.HandleFunc("/api/account/nodes/delete", s.handleOwnerNodesDelete)
	mux.HandleFunc("/api/account/subscriptions", s.handleOwnerSubscriptions)
	mux.HandleFunc("/api/account/service-monitors", s.handleAccountServiceMonitors)
	mux.HandleFunc("/api/account/service-monitors/results", s.handleAccountServiceMonitorResults)
	mux.HandleFunc("/api/account/market-appeals", s.handleOwnerMarketAppeals)
	mux.HandleFunc("/api/account/notifications", s.handleAccountNotifications)
	mux.HandleFunc("/api/account/notifications/bind/start", s.handleAccountNotificationBindStart)
	mux.HandleFunc("/api/account/notifications/bind/status", s.handleAccountNotificationBindStatus)
	mux.HandleFunc("/api/account/notifications/preferences", s.handleAccountNotificationPreferences)
	mux.HandleFunc("/api/account/notifications/test", s.handleAccountNotificationTest)
	mux.HandleFunc("/api/account/notifications/unbind", s.handleAccountNotificationUnbind)
	mux.HandleFunc("/api/account/notifications/deliveries", s.handleAccountNotificationDeliveries)
	mux.HandleFunc("/api/account/orders", s.handleAccountOrders)
	mux.HandleFunc("/api/account/orders/action", s.handleAccountOrderAction)
	mux.HandleFunc("/api/account/services", s.handleAccountServices)
	mux.HandleFunc("/api/account/services/history", s.handleAccountServiceHistory)
	mux.HandleFunc("/api/account/node-groups", s.handleAccountNodeGroups)
	mux.HandleFunc("/api/account/overview-preferences", s.handleAccountOverviewPreference)
	mux.HandleFunc("/api/account/node-events", s.handleAccountNodeEvents)
	mux.HandleFunc("/api/account/traffic-policies", s.handleAccountTrafficPolicies)
	mux.HandleFunc("/api/status/", s.handlePublicStatus)
	mux.HandleFunc("/api/v1/nodes", s.handlePublicAPINodes)
	mux.HandleFunc("/api/v1/status", s.handlePublicAPIStatus)
	mux.HandleFunc("/openapi.json", s.handleOpenAPI)
	mux.HandleFunc("/install/agent-linux.sh", s.handleAgentLinuxInstaller)
	mux.HandleFunc("/install/agent-windows.ps1", s.handleAgentWindowsInstaller)
	mux.HandleFunc("/uninstall/agent-linux.sh", s.handleAgentLinuxUninstaller)
	mux.HandleFunc("/uninstall/agent-windows.ps1", s.handleAgentWindowsUninstaller)
	mux.HandleFunc("/download/", s.handleDownload)
	mux.HandleFunc("/admin", s.handleAdminPage)
	mux.HandleFunc("/admin/", s.handleAdminPage)
	mux.HandleFunc("/config.json", s.handleConfig)
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/info", s.handleInfo)
	mux.HandleFunc("/delete", s.handleDelete)
	mux.HandleFunc("/api/nodes", s.handleNodes)
	mux.HandleFunc("/api/nodes/history", s.handleMetricsHistory)
	mux.HandleFunc("/api/health/live", s.handleHealthLive)
	mux.HandleFunc("/api/health/ready", s.handleHealthReady)
	mux.HandleFunc("/api/site/content", s.handleSiteContent)
	mux.HandleFunc("/api/site/release-notes", s.handleReleaseNotes)
	mux.HandleFunc("/api/site/footer", s.handleSiteFooter)
	mux.HandleFunc("/api/site/visitor-stats", s.handleVisitorStats)
	mux.HandleFunc("/api/site/visit", s.handleVisitorVisit)
	mux.HandleFunc("/api/site/version", s.handleSiteVersion)
	mux.HandleFunc("/api/site/exchange-rate", s.handleSiteExchangeRate)
	mux.HandleFunc("/", s.handleStatic)
	s.http = &http.Server{
		Addr:           cfg.Addr,
		Handler:        withCORS(mux, cfg.CORSOrigins),
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   15 * time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 16 << 10,
	}
	return s, nil
}

func (s *Server) ListenAndServe() error {
	s.startBackgroundServices()
	return s.http.ListenAndServe()
}

func (s *Server) startBackgroundServices() {
	s.backgroundOnce.Do(func() {
		if s.backgroundStop == nil {
			s.backgroundStop = make(chan struct{})
		}
		if s.backgroundDone == nil {
			s.backgroundDone = make(chan struct{})
		}
		go func() {
			defer close(s.backgroundDone)
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					if s.serviceMonitors != nil {
						go s.serviceMonitors.RunDue(context.Background())
					}
					if s.alerts != nil {
						nodes := s.store.AdminNodes(s.cfg.OfflineWait)
						if s.features != nil {
							active := nodes[:0]
							for _, node := range nodes {
								if !s.features.MaintenanceActive(node.NodeID, time.Now().Unix()) {
									active = append(active, node)
								}
							}
							nodes = active
						}
						s.alerts.Evaluate(nodes, s.store.AkileHosts())
					}
					go s.maybeCreateScheduledBackup()
					go s.maybeRunScheduledBackupDrill()
					go s.processShowDocNotifications()
					go s.expireMarketOrderIntents()
					go s.evaluateTrafficPolicies()
				case <-s.backgroundStop:
					if s.metricsHistory != nil {
						_ = s.metricsHistory.Save()
					}
					if s.alerts != nil {
						_ = s.alerts.Save()
					}
					return
				}
			}
		}()
	})
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.startBackgroundServices()
	select {
	case <-s.backgroundStop:
	default:
		close(s.backgroundStop)
	}
	select {
	case <-s.backgroundDone:
	case <-ctx.Done():
	}
	return s.http.Shutdown(ctx)
}

func (s *Server) handleAgentPing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if !s.agentAuthorized(r) {
		http.Error(w, "missing agent identity", http.StatusUnauthorized)
		return
	}
	writeJSON(w, map[string]string{"ok": "true"})
}

func (s *Server) handleAgentReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.agentAuthorized(r) {
		http.Error(w, "missing agent identity", http.StatusUnauthorized)
		return
	}
	defer r.Body.Close()
	var metrics agent.Metrics
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&metrics); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	metrics.NodeID = strings.TrimSpace(r.Header.Get("X-Node-ID"))
	if !validNodeID(metrics.NodeID) {
		http.Error(w, "invalid node_id", http.StatusBadRequest)
		return
	}
	metrics.Timestamp = time.Now().Unix()
	if err := s.store.UpsertReport(metrics, s.cfg.MaxNodes); err != nil {
		http.Error(w, err.Error(), http.StatusTooManyRequests)
		return
	}
	if s.metricsHistory != nil {
		s.metricsHistory.Add(metrics)
	}
	if s.features != nil {
		s.recordAgentIP(metrics.NodeID, requestClientIP(r))
		completions, err := s.features.ReconcileAgentVersion(metrics.NodeID, metrics.AgentVersion)
		if err != nil {
			http.Error(w, "persist upgrade completion", http.StatusInternalServerError)
			return
		}
		for _, completion := range completions {
			payload := map[string]any{
				"rollout_id": completion.RolloutID, "node_id": metrics.NodeID,
				"action": completion.Action, "status": "installed",
				"version": completion.Version, "inferred_from_agent_report": true,
			}
			if planned, ok := s.store.GetPlannedNode(metrics.NodeID); ok {
				payload["owner_id"] = planned.OwnerID
			}
			go s.dispatchPlatformEvent("agent.upgrade.result", payload)
		}
	}
	if s.alerts != nil {
		s.alerts.Evaluate(s.store.AdminNodes(s.cfg.OfflineWait), s.store.AkileHosts())
	}
	s.cache.MarkDirty()
	writeJSON(w, map[string]string{"ok": "true"})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	base, err := s.requestBase(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	settings := s.store.GetSettings()
	writeJSON(w, map[string]string{
		"socket":                 socketURL(base),
		"apiURL":                 base,
		"siteName":               s.store.SiteName(),
		"offlineWait":            fmt.Sprintf("%.0f", s.cfg.OfflineWait.Seconds()),
		"landingEnabled":         fmt.Sprintf("%t", settings.LandingEnabled),
		"marketEnabled":          fmt.Sprintf("%t", settings.MarketEnabled),
		"registrationEnabled":    fmt.Sprintf("%t", !settings.UserRegistrationDisabled),
		"selfServiceNodeEnabled": fmt.Sprintf("%t", !settings.UserNodeApplyDisabled),
		"userNodeLimit":          fmt.Sprintf("%d", settings.UserNodeLimit),
	})
}

func (s *Server) marketEnabled(w http.ResponseWriter) bool {
	if s.store.GetSettings().MarketEnabled {
		return true
	}
	http.Error(w, "market is currently disabled", http.StatusServiceUnavailable)
	return false
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		infos := s.store.InfoList()
		public := make([]HostInfo, 0, len(infos))
		for _, info := range infos {
			if s.nodeIsPublic(info.Name) {
				public = append(public, info)
			}
		}
		writeJSON(w, public)
	case http.MethodPost:
		if !s.adminAuthorized(r) {
			http.Error(w, "admin login required", http.StatusUnauthorized)
			return
		}
		if !s.validAdminOrigin(r) {
			http.Error(w, "invalid request origin", http.StatusForbidden)
			return
		}
		var req HostInfo
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		if !validNodeID(req.Name) {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
			return
		}
		req.DisplayName = strings.TrimSpace(req.DisplayName)
		req.Region = strings.TrimSpace(req.Region)
		price, amount, currency, billingCycle, billingErr := normalizeBilling(req.Price, req.Cycle, req.PriceAmount, req.PriceCurrency, req.BillingCycle, false)
		if billingErr != nil || len([]rune(price)) > 64 {
			http.Error(w, "invalid billing information", http.StatusBadRequest)
			return
		}
		req.Price, req.PriceAmount, req.PriceCurrency, req.BillingCycle = price, amount, currency, billingCycle
		if req.Cycle == "" && billingCycle != "" {
			req.Cycle = legacyCycleLabel(billingCycle)
		}
		displayName, region, regionCode := resolveNodeMeta(req.DisplayName, req.Region, req.Name)
		req.DisplayName = displayName
		req.Region = region
		req.RegionCode = regionCode
		if err := s.store.UpsertInfo(req); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Keep planned meta in sync when editing from the admin form.
		_ = s.store.SetNodeMeta(req.Name, displayName, region, regionCode)
		s.cache.MarkDirty()
		writeJSON(w, map[string]string{"ok": "true"})
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.adminAuthorized(r) {
		http.Error(w, "admin login required", http.StatusUnauthorized)
		return
	}
	if !s.validAdminOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !validNodeID(req.Name) {
		http.Error(w, "invalid node_id", http.StatusBadRequest)
		return
	}
	if _, err := s.store.TrashNode(req.Name, auditForRequest(r, "admin", "admin", "node.trash", "node", req.Name, "Moved node to recycle bin")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.metricsHistory.Delete(req.Name)
	s.cache.MarkDirty()
	writeJSON(w, map[string]string{"ok": "true"})
}

func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	s.writeCachedHosts(w)
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	ip := requestClientIP(r)
	if !s.acquireWebSocket(ip) {
		http.Error(w, "too many websocket connections", http.StatusTooManyRequests)
		return
	}
	defer s.releaseWebSocket(ip)
	conn, rw, err := upgradeWebSocket(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer conn.Close()
	if err := writeWSBytes(rw, s.cachedHostsJSON()); err != nil {
		return
	}
	for {
		_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		_, err := readWS(conn)
		if err != nil {
			return
		}
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := writeWSBytes(rw, s.cachedHostsJSON()); err != nil {
			return
		}
	}
}

func (s *Server) cachedHostsJSON() []byte {
	return s.cache.Get(func() []byte {
		hosts := s.publicHosts()
		if !s.cfg.PublicMonitorDetails {
			hosts = redactPublicHosts(hosts)
		}
		data, err := json.Marshal(hosts)
		if err != nil {
			return []byte("[]")
		}
		return data
	})
}

func (s *Server) nodeIsPublic(nodeID string) bool {
	planned, ok := s.store.GetPlannedNode(strings.TrimSpace(nodeID))
	return !ok || !planned.Private
}

func (s *Server) publicHosts() []AkileHost {
	hosts := s.store.AkileHosts()
	out := make([]AkileHost, 0, len(hosts))
	for _, host := range hosts {
		if s.nodeIsPublic(host.Host.Name) {
			out = append(out, host)
		}
	}
	return out
}

func (s *Server) writeCachedHosts(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(s.cachedHostsJSON())
}

func (s *Server) requestBase(r *http.Request) (string, error) {
	if s.cfg.PublicURL != "" {
		return strings.TrimRight(s.cfg.PublicURL, "/"), nil
	}
	host, err := normalizeExternalHost(r.Host)
	if err != nil {
		return "", err
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + host, nil
}

func (s *Server) externalBase(r *http.Request) (string, error) {
	return s.requestBase(r)
}

// normalizeExternalHost accepts only the authority form emitted by a real HTTP
// server: a DNS name or IP address, with an optional numeric TCP port. The
// returned value is safe to interpolate into generated installer URLs.
func normalizeExternalHost(raw string) (string, error) {
	if raw == "" || strings.TrimSpace(raw) != raw || len(raw) > 271 {
		return "", fmt.Errorf("invalid request host")
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f || strings.ContainsRune("/\\@?#", r) {
			return "", fmt.Errorf("invalid request host")
		}
	}
	host, port, bracketed := raw, "", false
	if strings.HasPrefix(raw, "[") {
		end := strings.IndexByte(raw, ']')
		if end < 0 || net.ParseIP(raw[1:end]) == nil {
			return "", fmt.Errorf("invalid request host")
		}
		host, bracketed = raw[1:end], true
		if raw[end+1:] != "" {
			if !strings.HasPrefix(raw[end+1:], ":") {
				return "", fmt.Errorf("invalid request host")
			}
			port = raw[end+2:]
			if port == "" {
				return "", fmt.Errorf("invalid request host")
			}
		}
	} else if strings.Count(raw, ":") > 1 {
		if net.ParseIP(raw) == nil {
			return "", fmt.Errorf("invalid request host")
		}
		bracketed = true
	} else if strings.Count(raw, ":") == 1 {
		var ok bool
		host, port, ok = strings.Cut(raw, ":")
		if !ok || host == "" || port == "" {
			return "", fmt.Errorf("invalid request host")
		}
	}
	if port != "" {
		if len(port) > 5 {
			return "", fmt.Errorf("invalid request host")
		}
		for _, r := range port {
			if r < '0' || r > '9' {
				return "", fmt.Errorf("invalid request host")
			}
		}
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", fmt.Errorf("invalid request host")
		}
	}
	if !bracketed {
		if ip := net.ParseIP(host); ip == nil {
			if !validDNSHost(host) {
				return "", fmt.Errorf("invalid request host")
			}
		}
	}
	if bracketed {
		return "[" + host + "]" + func() string {
			if port == "" {
				return ""
			}
			return ":" + port
		}(), nil
	}
	if port != "" {
		return host + ":" + port, nil
	}
	return host, nil
}

func validDNSHost(host string) bool {
	if len(host) == 0 || len(host) > 253 || strings.HasSuffix(host, ".") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}
	return true
}

func redactPublicHosts(hosts []serverapp.AkileHost) []serverapp.AkileHost {
	for i := range hosts {
		hosts[i].Host.Hostname = ""
		hosts[i].Host.PlatformVersion = ""
		hosts[i].Host.Kernel = ""
		hosts[i].Host.CPUModel = ""
		hosts[i].Host.GPUs = []string{}
		hosts[i].Host.Virtualization = ""
		// Mount paths and filesystem types can disclose tenant layout and device names.
		hosts[i].State.Disks = []agent.Disk{}
	}
	return hosts
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func psQuote(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

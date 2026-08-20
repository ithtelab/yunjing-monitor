package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const platformTestSecret = "platform-test-secret-with-enough-entropy"

func newPlatformTestServer(t *testing.T) (*Server, *Store) {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "server.json"), platformTestSecret)
	if err != nil {
		t.Fatal(err)
	}
	features, err := NewPlatformFeatureHub(filepath.Join(dir, "platform-features.json"), platformTestSecret)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		cfg: Config{
			AuthSecret: platformTestSecret, DataPath: filepath.Join(dir, "server.json"),
			BackupDir: filepath.Join(dir, "backups"), PublicURL: "https://monitor.example.com",
			OfflineWait: time.Minute, MaxNodes: 20,
		},
		store: store, sessions: NewSessionStore(), cache: NewResponseCache(), features: features,
		marketReportLimiter: newIPRateLimiter(2 * time.Minute),
	}
	return s, store
}

func TestMarketDisabledBlocksOwnerMarketWrites(t *testing.T) {
	s, store := newPlatformTestServer(t)
	hash, err := hashOwnerPassword("password123")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := store.CreateOwner("paused-market@example.com", hash)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateOwnerNode(owner.ID, PlannedNode{NodeID: "paused-node", Private: true}, HostInfo{}, "paused-token", accountAudit(owner.ID, "node.create", "paused-node"), 20, 3); err != nil {
		t.Fatal(err)
	}
	token, err := s.sessions.CreateFor(SessionKindOwner, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	cookie := &http.Cookie{Name: "monitor_owner", Value: token}
	settings := store.GetSettings()
	settings.MarketEnabled = false
	if err := store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}

	listingUpdate := accountRequest(http.MethodPost, "https://monitor.example.com/api/account/nodes/update", map[string]any{"node_id": "paused-node", "contact": "tg:@paused"}, cookie)
	listingResp := httptest.NewRecorder()
	s.handleOwnerNodesUpdate(listingResp, listingUpdate)
	if listingResp.Code != http.StatusServiceUnavailable {
		t.Fatalf("listing update status=%d body=%s", listingResp.Code, listingResp.Body.String())
	}
	metadataUpdate := accountRequest(http.MethodPost, "https://monitor.example.com/api/account/nodes/update", map[string]any{"node_id": "paused-node", "display_name": "still manageable"}, cookie)
	metadataResp := httptest.NewRecorder()
	s.handleOwnerNodesUpdate(metadataResp, metadataUpdate)
	if metadataResp.Code != http.StatusOK {
		t.Fatalf("metadata update status=%d body=%s", metadataResp.Code, metadataResp.Body.String())
	}
	subscription := accountRequest(http.MethodPost, "https://monitor.example.com/api/account/subscriptions", map[string]any{"action": "save", "subscription": map[string]any{"name": "paused"}}, cookie)
	subscriptionResp := httptest.NewRecorder()
	s.handleOwnerSubscriptions(subscriptionResp, subscription)
	if subscriptionResp.Code != http.StatusServiceUnavailable {
		t.Fatalf("subscription status=%d body=%s", subscriptionResp.Code, subscriptionResp.Body.String())
	}
	appeal := accountRequest(http.MethodPost, "https://monitor.example.com/api/account/market-appeals", map[string]any{"report_id": "report_paused", "message": "paused market appeal"}, cookie)
	appealResp := httptest.NewRecorder()
	s.handleOwnerMarketAppeals(appealResp, appeal)
	if appealResp.Code != http.StatusServiceUnavailable {
		t.Fatalf("appeal status=%d body=%s", appealResp.Code, appealResp.Body.String())
	}
}

func TestPlatformFeaturePersistenceAndSnapshotRedaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "platform-features.json")
	hub, err := NewPlatformFeatureHub(path, platformTestSecret)
	if err != nil {
		t.Fatal(err)
	}
	secret := "webhook-signing-secret-value"
	ciphertext, err := hub.cipher.encrypt(secret)
	if err != nil {
		t.Fatal(err)
	}
	hub.data.APIKeys = []APIKeyRecord{{ID: "key_1", KeyHash: apiKeyHash("mpk-secret")}}
	hub.data.Webhooks = []WebhookIntegration{{ID: "hook_1", SecretCipher: ciphertext}}
	hub.data.MarketReports = []MarketReport{{ID: "report_1", ReporterHash: "private-reporter-fingerprint"}}
	if err := hub.saveLocked(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) {
		t.Fatal("webhook secret was stored in plaintext")
	}
	reloaded, err := NewPlatformFeatureHub(path, platformTestSecret)
	if err != nil {
		t.Fatal(err)
	}
	snapshotRaw, _ := json.Marshal(reloaded.Snapshot())
	for _, forbidden := range []string{apiKeyHash("mpk-secret"), ciphertext, "private-reporter-fingerprint"} {
		if strings.Contains(string(snapshotRaw), forbidden) {
			t.Fatalf("snapshot leaked %q", forbidden)
		}
	}
}

func TestPlatformAPIKeyScopeAndExpiry(t *testing.T) {
	hub, err := NewPlatformFeatureHub(filepath.Join(t.TempDir(), "features.json"), platformTestSecret)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	hub.data.APIKeys = []APIKeyRecord{
		{ID: "valid", KeyHash: apiKeyHash("valid-token"), Scopes: []string{"nodes:read"}, Enabled: true, ExpiresAt: now + 60},
		{ID: "expired", KeyHash: apiKeyHash("expired-token"), Scopes: []string{"*"}, Enabled: true, ExpiresAt: now - 1},
	}
	if _, ok := hub.APIKey("valid-token", "nodes:read"); !ok {
		t.Fatal("valid scoped key was rejected")
	}
	if _, ok := hub.APIKey("valid-token", "status:read"); ok {
		t.Fatal("key authorized an ungranted scope")
	}
	if _, ok := hub.APIKey("expired-token", "nodes:read"); ok {
		t.Fatal("expired key was accepted")
	}
}

func TestPlatformMaintenanceAndStatusFiltering(t *testing.T) {
	hub, err := NewPlatformFeatureHub(filepath.Join(t.TempDir(), "features.json"), platformTestSecret)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	hub.data.NodeProfiles["node_tagged"] = NodeFeatureProfile{NodeID: "node_tagged", Tags: []string{"database"}}
	hub.data.Maintenance = []MaintenanceWindow{
		{ID: "active", Tags: []string{"database"}, StartAt: now - 10, EndAt: now + 10, MuteAlerts: true},
		{ID: "expired", NodeIDs: []string{"node_other"}, StartAt: now - 20, EndAt: now - 10, MuteAlerts: true},
	}
	if !hub.MaintenanceActive("node_tagged", now) || hub.MaintenanceActive("node_other", now) {
		t.Fatal("maintenance targeting or time window mismatch")
	}
	hub.data.StatusPages = []PublicStatusPage{
		{ID: "enabled", Slug: "public-status", Enabled: true},
		{ID: "disabled", Slug: "private-status", Enabled: false},
	}
	hub.data.Incidents = []StatusIncident{{ID: "old", StatusPageID: "enabled", StartedAt: 1}, {ID: "new", StatusPageID: "enabled", StartedAt: 2}, {ID: "hidden", StatusPageID: "disabled", StartedAt: 3}}
	_, incidents, ok := hub.StatusPage("public-status")
	if !ok || len(incidents) != 2 || incidents[0].ID != "new" {
		t.Fatalf("unexpected public status result: ok=%v incidents=%#v", ok, incidents)
	}
	if _, _, ok := hub.StatusPage("private-status"); ok {
		t.Fatal("disabled status page was exposed")
	}
}

func TestAdminDiagnosticsRedactsSecretsAndAbsolutePaths(t *testing.T) {
	s, _ := newPlatformTestServer(t)
	apiHash := apiKeyHash("diagnostic-api-key")
	hookSecret := "diagnostic-webhook-secret"
	ciphertext, _ := s.features.cipher.encrypt(hookSecret)
	s.features.data.APIKeys = []APIKeyRecord{{ID: "key_diag", KeyHash: apiHash}}
	s.features.data.Webhooks = []WebhookIntegration{{ID: "hook_diag", SecretCipher: ciphertext}}
	s.features.data.MarketReports = []MarketReport{{ID: "report_diag", ReporterHash: "reporter-private-hash"}}
	token, err := s.sessions.Create()
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "https://monitor.example.com/api/admin/diagnostics", nil)
	req.AddCookie(&http.Cookie{Name: "monitor_admin", Value: token})
	resp := httptest.NewRecorder()
	s.handleAdminDiagnostics(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("diagnostics status=%d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	for _, forbidden := range []string{apiHash, ciphertext, hookSecret, "reporter-private-hash", filepath.Dir(s.cfg.DataPath), platformTestSecret} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("diagnostics leaked %q", forbidden)
		}
	}
}

func TestAgentUpgradeEligibilityManifestAndResult(t *testing.T) {
	previousVersion := buildVersion
	buildVersion = "v2.0.0"
	t.Cleanup(func() { buildVersion = previousVersion })
	s, store := newPlatformTestServer(t)
	const nodeID = "node_upgrade_1"
	const token = "agent-upgrade-token"
	if err := store.SetNodeToken(nodeID, token, hashToken(token), 20); err != nil {
		t.Fatal(err)
	}
	s.features.data.NodeProfiles[nodeID] = NodeFeatureProfile{NodeID: nodeID, Tags: []string{"canary"}}
	s.features.data.UpgradeRollouts = []AgentUpgradeRollout{{
		ID: "rollout_test", Name: "Agent 2", TargetVersion: "v2.0.0", Tags: []string{"canary"},
		CanaryPercent: 100, State: "active", NodeStates: map[string]string{},
		Artifacts: []AgentUpgradeArtifact{{
			OS: "linux", Arch: "amd64", URL: "https://downloads.example.com/vps-agent-linux-amd64", SHA256: strings.Repeat("a", 64),
		}},
	}}

	req := httptest.NewRequest(http.MethodGet, "https://monitor.example.com/api/agent/upgrade-manifest?os=linux&arch=amd64", nil)
	req.Header.Set("X-Node-ID", nodeID)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	s.handleAgentUpgradeManifest(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("manifest status=%d body=%s", resp.Code, resp.Body.String())
	}
	var manifest AgentUpgradeManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Action != "upgrade" || manifest.Version != "v2.0.0" || !validSHA256(manifest.SHA256) || manifest.Signature != signAgentUpgradeManifest(manifest, token) {
		t.Fatalf("invalid manifest: %#v", manifest)
	}
	if manifest.Signature == signAgentUpgradeManifest(manifest, "different-node-token") {
		t.Fatal("manifest signature was not bound to the node token")
	}
	if eligible1, eligible2 := rolloutCanaryEligible("stable", nodeID, 25), rolloutCanaryEligible("stable", nodeID, 25); eligible1 != eligible2 {
		t.Fatal("canary selection is not stable")
	}

	badResultBody := `{"rollout_id":"rollout_test","action":"upgrade","status":"installed","version":"v2.0.0","os":"linux","arch":"amd64","sha256":"` + strings.Repeat("0", 64) + `"}`
	badResultReq := httptest.NewRequest(http.MethodPost, "https://monitor.example.com/api/agent/upgrade-result", strings.NewReader(badResultBody))
	badResultReq.Header.Set("X-Node-ID", nodeID)
	badResultReq.Header.Set("Authorization", "Bearer "+token)
	badResultResp := httptest.NewRecorder()
	s.handleAgentUpgradeResult(badResultResp, badResultReq)
	if badResultResp.Code != http.StatusConflict {
		t.Fatalf("mismatched artifact result status=%d", badResultResp.Code)
	}

	resultBody := `{"rollout_id":"rollout_test","action":"upgrade","status":"installed","version":"v2.0.0","os":"linux","arch":"amd64","sha256":"` + manifest.SHA256 + `"}`
	resultReq := httptest.NewRequest(http.MethodPost, "https://monitor.example.com/api/agent/upgrade-result", strings.NewReader(resultBody))
	resultReq.Header.Set("X-Node-ID", nodeID)
	resultReq.Header.Set("Authorization", "Bearer "+token)
	resultResp := httptest.NewRecorder()
	s.handleAgentUpgradeResult(resultResp, resultReq)
	if resultResp.Code != http.StatusOK {
		t.Fatalf("result status=%d body=%s", resultResp.Code, resultResp.Body.String())
	}
	if got := s.features.Snapshot().UpgradeRollouts[0].NodeStates[nodeID]; got != "installed" {
		t.Fatalf("node rollout state=%q", got)
	}

	s.features.mu.Lock()
	s.features.data.UpgradeRollouts[0].State = "paused"
	s.features.mu.Unlock()
	pausedReq := httptest.NewRequest(http.MethodGet, "https://monitor.example.com/api/agent/upgrade-manifest?os=linux&arch=amd64", nil)
	pausedReq.Header.Set("X-Node-ID", nodeID)
	pausedReq.Header.Set("Authorization", "Bearer "+token)
	pausedResp := httptest.NewRecorder()
	s.handleAgentUpgradeManifest(pausedResp, pausedReq)
	if pausedResp.Code != http.StatusNoContent {
		t.Fatalf("paused rollout returned %d", pausedResp.Code)
	}

	s.features.mu.Lock()
	s.features.data.UpgradeRollouts[0].State = "rollback"
	s.features.data.UpgradeRollouts[0].RollbackVersion = "v1.9.0"
	s.features.data.UpgradeRollouts[0].RollbackArtifacts = []AgentUpgradeArtifact{{OS: "linux", Arch: "amd64", URL: "https://example.com/vps-agent-v1.9.0", SHA256: strings.Repeat("a", 64)}}
	s.features.mu.Unlock()
	rollbackReq := httptest.NewRequest(http.MethodGet, "https://monitor.example.com/api/agent/upgrade-manifest?os=linux&arch=amd64", nil)
	rollbackReq.Header.Set("X-Node-ID", nodeID)
	rollbackReq.Header.Set("Authorization", "Bearer "+token)
	rollbackResp := httptest.NewRecorder()
	s.handleAgentUpgradeManifest(rollbackResp, rollbackReq)
	if rollbackResp.Code != http.StatusOK {
		t.Fatalf("rollback manifest status=%d body=%s", rollbackResp.Code, rollbackResp.Body.String())
	}
	var rollbackManifest AgentUpgradeManifest
	if err := json.NewDecoder(rollbackResp.Body).Decode(&rollbackManifest); err != nil {
		t.Fatal(err)
	}
	if rollbackManifest.Action != "rollback" || rollbackManifest.Version != "v1.9.0" || rollbackManifest.Signature != signAgentUpgradeManifest(rollbackManifest, token) {
		t.Fatalf("invalid rollback manifest: %#v", rollbackManifest)
	}
}

func TestAgentReportReconcilesUpgradeAndLegacyRollbackCompletion(t *testing.T) {
	s, store := newPlatformTestServer(t)
	const nodeID = "node_reconcile"
	const token = "reconcile-token"
	if err := store.SetNodeToken(nodeID, token, hashToken(token), 20); err != nil {
		t.Fatal(err)
	}
	s.features.data.UpgradeRollouts = []AgentUpgradeRollout{
		{ID: "rollout_upgrade", TargetVersion: "v2.0.0", CanaryPercent: 100, State: "paused", NodeStates: map[string]string{nodeID: "downloading"}},
		{ID: "rollout_rollback", TargetVersion: "v3.0.0", RollbackVersion: "v1.5.0", CanaryPercent: 100, State: "rollback", NodeStates: map[string]string{nodeID: "rollback_downloading"}},
	}
	reportReq := httptest.NewRequest(http.MethodPost, "https://monitor.example.com/api/agent/report", strings.NewReader(`{"agent_version":"1.5.0"}`))
	reportReq.Header.Set("X-Node-ID", nodeID)
	reportReq.Header.Set("Authorization", "Bearer "+token)
	reportResp := httptest.NewRecorder()
	s.handleAgentReport(reportResp, reportReq)
	if reportResp.Code != http.StatusOK {
		t.Fatalf("legacy rollback report status=%d body=%s", reportResp.Code, reportResp.Body.String())
	}
	snapshot := s.features.Snapshot()
	if got := snapshot.UpgradeRollouts[1].NodeStates[nodeID]; got != "rollback_installed" {
		t.Fatalf("legacy rollback state=%q", got)
	}
	if got := snapshot.UpgradeRollouts[0].NodeStates[nodeID]; got != "downloading" {
		t.Fatalf("mismatched upgrade was changed to %q", got)
	}

	completions, err := s.features.ReconcileAgentVersion(nodeID, "v2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(completions) != 1 || completions[0].Action != "upgrade" {
		t.Fatalf("unexpected upgrade completions: %#v", completions)
	}
	reloaded, err := NewPlatformFeatureHub(s.features.path, platformTestSecret)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Snapshot().UpgradeRollouts[0].NodeStates[nodeID]; got != "installed" {
		t.Fatalf("reconciled upgrade state was not persisted: %q", got)
	}
}

func TestWebhookValidationSignatureDeliveryAndPrivateTarget(t *testing.T) {
	if _, _, err := validateWebhookIntegration(WebhookIntegration{Name: "hook", URL: "https://example.com/hook", Events: []string{"unsupported"}}, "long-enough-secret", WebhookIntegration{}); err == nil {
		t.Fatal("unsupported webhook event was accepted")
	}
	if _, _, err := validateWebhookIntegration(WebhookIntegration{Name: "hook", URL: "https://127.0.0.1/hook", Events: []string{"agent.upgrade.result"}}, "long-enough-secret", WebhookIntegration{}); err == nil {
		t.Fatal("private webhook target was accepted")
	}
	if _, err := safeWebhookDialContext(context.Background(), "tcp", "127.0.0.1:443"); err == nil {
		t.Fatal("safe webhook dialer accepted loopback")
	}

	s, _ := newPlatformTestServer(t)
	secret := "webhook-test-signing-secret"
	ciphertext, _ := s.features.cipher.encrypt(secret)
	requestSeen := make(chan struct{}, 1)
	var receivedBody []byte
	var receivedSignature, receivedTimestamp string
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody, _ = io.ReadAll(r.Body)
		receivedSignature = r.Header.Get("X-Monitor-Signature")
		receivedTimestamp = r.Header.Get("X-Monitor-Timestamp")
		requestSeen <- struct{}{}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	s.features.webhookClient = target.Client()
	hook := WebhookIntegration{ID: "hook_test", URL: target.URL, SecretCipher: ciphertext, Events: []string{"agent.upgrade.result"}, Enabled: true}
	s.features.data.Webhooks = []WebhookIntegration{hook}
	s.dispatchPlatformEvent("agent.upgrade.result", map[string]string{"status": "installed"})
	select {
	case <-requestSeen:
	case <-time.After(time.Second):
		t.Fatal("webhook request was not delivered")
	}
	wantSignature := "sha256=" + platformWebhookSignature(receivedTimestamp, receivedBody, secret)
	if !hmac.Equal([]byte(receivedSignature), []byte(wantSignature)) {
		t.Fatalf("signature=%q want=%q", receivedSignature, wantSignature)
	}
	deliveries := s.features.Snapshot().WebhookDeliveries
	if len(deliveries) != 1 || deliveries[0].Status != "delivered" || deliveries[0].HTTPStatus != http.StatusNoContent {
		t.Fatalf("unexpected delivery record: %#v", deliveries)
	}
}

func TestMarketReportValidationDeduplicationAndAppeal(t *testing.T) {
	s, store := newPlatformTestServer(t)
	s.marketReportLimiter = nil
	store.Listings["listing_1"] = MarketListing{NodeID: "listing_1", OwnerID: "owner_1", ForSale: true, ModerationStatus: "approved"}
	store.Owners["owner_1"] = Owner{ID: "owner_1", Email: "owner@example.com"}
	body := `{"listing_node_id":"listing_1","category":"inaccurate","message":"The advertised configuration does not match the delivered server."}`
	req := httptest.NewRequest(http.MethodPost, "https://monitor.example.com/api/market/reports", strings.NewReader(body))
	req.RemoteAddr = "1.1.1.1:1234"
	resp := httptest.NewRecorder()
	s.handleMarketReport(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("report status=%d body=%s", resp.Code, resp.Body.String())
	}
	duplicateReq := httptest.NewRequest(http.MethodPost, "https://monitor.example.com/api/market/reports", strings.NewReader(body))
	duplicateReq.RemoteAddr = "1.1.1.1:9999"
	duplicateResp := httptest.NewRecorder()
	s.handleMarketReport(duplicateResp, duplicateReq)
	if duplicateResp.Code != http.StatusConflict {
		t.Fatalf("duplicate report status=%d", duplicateResp.Code)
	}
	report := s.features.data.MarketReports[0]
	if report.ReporterHash == "" || strings.Contains(report.ReporterHash, "1.1.1.1") {
		t.Fatalf("reporter fingerprint is not private: %q", report.ReporterHash)
	}

	token, err := s.sessions.CreateFor(SessionKindOwner, "owner_1")
	if err != nil {
		t.Fatal(err)
	}
	appealBody := `{"report_id":"` + report.ID + `","message":"Please review the attached server order and original listing details."}`
	appealReq := httptest.NewRequest(http.MethodPost, "https://monitor.example.com/api/owner/market-appeals", strings.NewReader(appealBody))
	appealReq.AddCookie(&http.Cookie{Name: "monitor_owner", Value: token})
	appealResp := httptest.NewRecorder()
	s.handleOwnerMarketAppeals(appealResp, appealReq)
	if appealResp.Code != http.StatusCreated || len(s.features.Snapshot().MarketAppeals) != 1 {
		t.Fatalf("appeal status=%d body=%s", appealResp.Code, appealResp.Body.String())
	}
	appeal := s.features.Snapshot().MarketAppeals[0]
	if _, err := s.applyPlatformAction(platformAdminRequest{Action: "resolve_market_report", MarketReport: MarketReport{ID: report.ID, Status: "resolved", Resolution: "Listing evidence confirmed."}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.applyPlatformAction(platformAdminRequest{Action: "resolve_market_appeal", MarketAppeal: MarketAppeal{ID: appeal.ID, Status: "accepted", Resolution: "Appeal evidence accepted."}}); err != nil {
		t.Fatal(err)
	}
	snapshot := s.features.Snapshot()
	if snapshot.MarketReports[0].Status != "resolved" || snapshot.MarketAppeals[0].Status != "accepted" {
		t.Fatalf("admin report workflow was not persisted: reports=%#v appeals=%#v", snapshot.MarketReports, snapshot.MarketAppeals)
	}
}

func TestPlatformSignaturePrimitive(t *testing.T) {
	body := []byte(`{"event":"agent.upgrade.result"}`)
	got := platformWebhookSignature("1700000000", body, "secret")
	mac := hmac.New(sha256.New, []byte("secret"))
	_, _ = mac.Write([]byte("1700000000."))
	_, _ = mac.Write(body)
	if got != hex.EncodeToString(mac.Sum(nil)) {
		t.Fatalf("signature mismatch: %s", got)
	}
}

func TestPlatformRoutesRegistered(t *testing.T) {
	dir := t.TempDir()
	s, err := New(Config{
		Addr: "127.0.0.1:0", AuthSecret: platformTestSecret,
		AdminPass: "platform-admin-password-long", DataPath: filepath.Join(dir, "server.json"),
		BackupDir: filepath.Join(dir, "backups"), PublicURL: "https://monitor.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodGet, "/api/agent/upgrade-manifest?os=linux&arch=amd64", http.StatusUnauthorized},
		{http.MethodPost, "/api/agent/upgrade-result", http.StatusUnauthorized},
		{http.MethodGet, "/api/market/reports", http.StatusMethodNotAllowed},
		{http.MethodGet, "/api/owner/market-appeals", http.StatusUnauthorized},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(tt.method, "https://monitor.example.com"+tt.path, nil)
		resp := httptest.NewRecorder()
		s.http.Handler.ServeHTTP(resp, req)
		if resp.Code != tt.want {
			t.Fatalf("%s %s status=%d want=%d", tt.method, tt.path, resp.Code, tt.want)
		}
	}
}

func TestMarketTrustAndSubscriptionMatchArePublicAndDeduplicated(t *testing.T) {
	s, store := newPlatformTestServer(t)
	seller, err := store.CreateOwner("seller@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	buyer, err := store.CreateOwner("buyer@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	const nodeID = "node_market_match"
	if err := store.AddPlannedNode(nodeID, 20); err != nil {
		t.Fatal(err)
	}
	if err := store.SetNodeMeta(nodeID, "Hong Kong Value", "Hong Kong", "HK"); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateMarketListing(MarketListing{NodeID: nodeID, OwnerID: seller.ID, ForSale: true, ModerationStatus: "approved", PriceAmount: 12, PriceCurrency: "USD", BillingCycle: "annual"}); err != nil {
		t.Fatal(err)
	}
	s.features.mu.Lock()
	s.features.data.SellerTrust[seller.ID] = SellerTrustProfile{OwnerID: seller.ID, Verified: true, Level: "trusted", Note: "verified seller"}
	s.features.data.Subscriptions = []MarketSubscription{{ID: "subscription_test", OwnerID: buyer.ID, Name: "HK budget", Regions: []string{"HK"}, MaxPrice: 10, Currency: "CNY", Enabled: true}}
	s.features.mu.Unlock()
	s.notifyMarketApproval(nodeID)
	s.notifyMarketApproval(nodeID)
	snapshot := s.features.Snapshot()
	if got := snapshot.Subscriptions[0]; got.MatchCount != 1 || len(got.MatchedNodeIDs) != 1 || got.LastMatchedAt == 0 {
		t.Fatalf("subscription was not matched exactly once: %#v", got)
	}
	views := s.buildMarketViews(store.ListMarketListings(true), false)
	if len(views) != 1 || views[0].SellerTrust == nil || !views[0].SellerTrust.Verified || views[0].OwnerID != "" {
		t.Fatalf("public seller trust view is invalid: %#v", views)
	}
	rates := manualExchangeRate(7.2, false, "test")
	if got := monthlyPriceInCurrency(12, "USD", "annual", "CNY", rates); got != 7.2 {
		t.Fatalf("monthly conversion=%v want=7.2", got)
	}
}

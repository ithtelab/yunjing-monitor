package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const platformFeatureVersion = 4

var platformSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,47}$`)

type NodeFeatureProfile struct {
	NodeID string   `json:"node_id"`
	Group  string   `json:"group,omitempty"`
	Tags   []string `json:"tags,omitempty"`
	Notes  string   `json:"notes,omitempty"`
}

type SavedNodeView struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	NodeIDs   []string `json:"node_ids,omitempty"`
	Groups    []string `json:"groups,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	CreatedAt int64    `json:"created_at"`
	UpdatedAt int64    `json:"updated_at"`
}

type MaintenanceWindow struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	NodeIDs    []string `json:"node_ids,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	StartAt    int64    `json:"start_at"`
	EndAt      int64    `json:"end_at"`
	MuteAlerts bool     `json:"mute_alerts"`
	CreatedAt  int64    `json:"created_at"`
}

type PublicStatusPage struct {
	ID          string   `json:"id"`
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Enabled     bool     `json:"enabled"`
	NodeIDs     []string `json:"node_ids,omitempty"`
	ServiceIDs  []string `json:"service_ids,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	ShowMetrics bool     `json:"show_metrics"`
	CreatedAt   int64    `json:"created_at"`
	UpdatedAt   int64    `json:"updated_at"`
}

type ProbePoint struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Region     string `json:"region"`
	Mode       string `json:"mode"`
	NodeID     string `json:"node_id,omitempty"`
	Enabled    bool   `json:"enabled"`
	LastSeenAt int64  `json:"last_seen_at,omitempty"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
}

type OwnerNodeGroup struct {
	ID        string   `json:"id"`
	OwnerID   string   `json:"owner_id"`
	Name      string   `json:"name"`
	Color     string   `json:"color,omitempty"`
	NodeIDs   []string `json:"node_ids,omitempty"`
	CreatedAt int64    `json:"created_at"`
	UpdatedAt int64    `json:"updated_at"`
}

type OwnerOverviewPreference struct {
	OwnerID   string   `json:"owner_id"`
	View      string   `json:"view"`
	SortBy    string   `json:"sort_by"`
	SortOrder string   `json:"sort_order"`
	GroupID   string   `json:"group_id,omitempty"`
	Statuses  []string `json:"statuses,omitempty"`
	UpdatedAt int64    `json:"updated_at"`
}

type NodeIPRecord struct {
	NodeID     string `json:"node_id"`
	OwnerID    string `json:"owner_id,omitempty"`
	CurrentIP  string `json:"current_ip"`
	PreviousIP string `json:"previous_ip,omitempty"`
	ChangedAt  int64  `json:"changed_at,omitempty"`
	UpdatedAt  int64  `json:"updated_at"`
}

type NodeOperationalEvent struct {
	ID        string `json:"id"`
	OwnerID   string `json:"owner_id,omitempty"`
	NodeID    string `json:"node_id"`
	Kind      string `json:"kind"`
	Status    string `json:"status"`
	Message   string `json:"message"`
	CreatedAt int64  `json:"created_at"`
}

type TrafficAlertPolicy struct {
	NodeID       string `json:"node_id"`
	OwnerID      string `json:"owner_id,omitempty"`
	Enabled      bool   `json:"enabled"`
	QuotaBytes   uint64 `json:"quota_bytes"`
	ThresholdPct int    `json:"threshold_percent"`
	AlertActive  bool   `json:"alert_active"`
	UpdatedAt    int64  `json:"updated_at"`
}

type StatusIncident struct {
	ID           string `json:"id"`
	StatusPageID string `json:"status_page_id"`
	Title        string `json:"title"`
	Message      string `json:"message,omitempty"`
	Status       string `json:"status"`
	StartedAt    int64  `json:"started_at"`
	ResolvedAt   int64  `json:"resolved_at,omitempty"`
}

type APIKeyRecord struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Prefix     string   `json:"prefix"`
	KeyHash    string   `json:"key_hash"`
	Scopes     []string `json:"scopes"`
	Enabled    bool     `json:"enabled"`
	CreatedAt  int64    `json:"created_at"`
	ExpiresAt  int64    `json:"expires_at,omitempty"`
	LastUsedAt int64    `json:"last_used_at,omitempty"`
}

type WebhookIntegration struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	URL          string   `json:"url"`
	SecretCipher string   `json:"secret_cipher,omitempty"`
	Events       []string `json:"events"`
	Enabled      bool     `json:"enabled"`
	CreatedAt    int64    `json:"created_at"`
	UpdatedAt    int64    `json:"updated_at"`
}

type WebhookDelivery struct {
	ID         string `json:"id"`
	WebhookID  string `json:"webhook_id"`
	Event      string `json:"event"`
	Status     string `json:"status"`
	HTTPStatus int    `json:"http_status,omitempty"`
	Error      string `json:"error,omitempty"`
	CreatedAt  int64  `json:"created_at"`
	Duration   int64  `json:"duration_ms"`
}

type AgentUpgradeArtifact struct {
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

type AgentUpgradeRollout struct {
	ID                string                 `json:"id"`
	Name              string                 `json:"name"`
	TargetVersion     string                 `json:"target_version"`
	Artifacts         []AgentUpgradeArtifact `json:"artifacts,omitempty"`
	RollbackVersion   string                 `json:"rollback_version,omitempty"`
	RollbackArtifacts []AgentUpgradeArtifact `json:"rollback_artifacts,omitempty"`
	NodeIDs           []string               `json:"node_ids,omitempty"`
	Tags              []string               `json:"tags,omitempty"`
	CanaryPercent     int                    `json:"canary_percent"`
	State             string                 `json:"state"`
	CreatedAt         int64                  `json:"created_at"`
	UpdatedAt         int64                  `json:"updated_at"`
	NodeStates        map[string]string      `json:"node_states,omitempty"`
}

type SellerTrustProfile struct {
	OwnerID    string `json:"owner_id"`
	Verified   bool   `json:"verified"`
	Level      string `json:"level"`
	Note       string `json:"note,omitempty"`
	Violations int    `json:"violations"`
	UpdatedAt  int64  `json:"updated_at"`
}

type MarketSubscription struct {
	ID             string   `json:"id"`
	OwnerID        string   `json:"owner_id"`
	Name           string   `json:"name"`
	Regions        []string `json:"regions,omitempty"`
	Tags           []string `json:"tags,omitempty"`
	MaxPrice       float64  `json:"max_price,omitempty"`
	Currency       string   `json:"currency,omitempty"`
	MinMemory      uint64   `json:"min_memory,omitempty"`
	Enabled        bool     `json:"enabled"`
	MatchedNodeIDs []string `json:"matched_node_ids,omitempty"`
	MatchCount     int      `json:"match_count,omitempty"`
	LastMatchedAt  int64    `json:"last_matched_at,omitempty"`
	CreatedAt      int64    `json:"created_at"`
	UpdatedAt      int64    `json:"updated_at"`
}

type BackupDrillRecord struct {
	ID        string `json:"id"`
	BackupID  string `json:"backup_id,omitempty"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
	CreatedAt int64  `json:"created_at"`
	Duration  int64  `json:"duration_ms"`
}

type MarketReport struct {
	ID            string `json:"id"`
	ListingNodeID string `json:"listing_node_id"`
	Category      string `json:"category"`
	Message       string `json:"message"`
	ReporterHash  string `json:"reporter_hash,omitempty"`
	Status        string `json:"status"`
	Resolution    string `json:"resolution,omitempty"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
	ResolvedAt    int64  `json:"resolved_at,omitempty"`
}

type MarketAppeal struct {
	ID         string `json:"id"`
	ReportID   string `json:"report_id"`
	OwnerID    string `json:"owner_id"`
	Message    string `json:"message"`
	Status     string `json:"status"`
	Resolution string `json:"resolution,omitempty"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
	ResolvedAt int64  `json:"resolved_at,omitempty"`
}

type ShowDocSettings struct {
	Enabled          bool     `json:"enabled"`
	RetryLimit       int      `json:"retry_limit"`
	AdminTokenCipher string   `json:"admin_token_cipher,omitempty"`
	AdminTokenSuffix string   `json:"admin_token_suffix,omitempty"`
	AdminEvents      []string `json:"admin_events,omitempty"`
}

type NotificationBinding struct {
	OwnerID     string `json:"owner_id"`
	Provider    string `json:"provider"`
	TokenCipher string `json:"token_cipher"`
	TokenSuffix string `json:"token_suffix"`
	Status      string `json:"status"`
	BoundAt     int64  `json:"bound_at"`
	LastSuccess int64  `json:"last_success_at,omitempty"`
	LastError   string `json:"last_error,omitempty"`
}

type NotificationPreference struct {
	OwnerID      string   `json:"owner_id"`
	Enabled      bool     `json:"enabled"`
	Events       []string `json:"events,omitempty"`
	NodeIDs      []string `json:"node_ids,omitempty"`
	QuietStart   string   `json:"quiet_start,omitempty"`
	QuietEnd     string   `json:"quiet_end,omitempty"`
	TimeZone     string   `json:"time_zone,omitempty"`
	DeliveryMode string   `json:"delivery_mode"`
	UpdatedAt    int64    `json:"updated_at"`
}

type NotificationBindSession struct {
	ID         string `json:"id"`
	OwnerID    string `json:"owner_id"`
	QRScene    string `json:"qr_scene"`
	QRCodeURL  string `json:"qr_code_url"`
	ExpiresAt  int64  `json:"expires_at"`
	ConsumedAt int64  `json:"consumed_at,omitempty"`
}

type NotificationDelivery struct {
	ID             string `json:"id"`
	OwnerID        string `json:"owner_id,omitempty"`
	Event          string `json:"event"`
	Title          string `json:"title"`
	Content        string `json:"content"`
	Status         string `json:"status"`
	Attempts       int    `json:"attempts"`
	NextAttemptAt  int64  `json:"next_attempt_at,omitempty"`
	IdempotencyKey string `json:"idempotency_key"`
	HTTPStatus     int    `json:"http_status,omitempty"`
	Error          string `json:"error,omitempty"`
	CreatedAt      int64  `json:"created_at"`
	DeliveredAt    int64  `json:"delivered_at,omitempty"`
}

type MarketOrderIntent struct {
	ID            string `json:"id"`
	ListingNodeID string `json:"listing_node_id"`
	BuyerOwnerID  string `json:"buyer_owner_id"`
	SellerOwnerID string `json:"seller_owner_id"`
	BuyerContact  string `json:"buyer_contact"`
	Message       string `json:"message,omitempty"`
	Status        string `json:"status"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
	ExpiresAt     int64  `json:"expires_at"`
}

type PlatformFeatureData struct {
	Version                  int                                `json:"version"`
	NodeProfiles             map[string]NodeFeatureProfile      `json:"node_profiles"`
	SavedViews               []SavedNodeView                    `json:"saved_views"`
	Maintenance              []MaintenanceWindow                `json:"maintenance"`
	StatusPages              []PublicStatusPage                 `json:"status_pages"`
	Incidents                []StatusIncident                   `json:"incidents"`
	APIKeys                  []APIKeyRecord                     `json:"api_keys"`
	Webhooks                 []WebhookIntegration               `json:"webhooks"`
	WebhookDeliveries        []WebhookDelivery                  `json:"webhook_deliveries"`
	UpgradeRollouts          []AgentUpgradeRollout              `json:"upgrade_rollouts"`
	SellerTrust              map[string]SellerTrustProfile      `json:"seller_trust"`
	Subscriptions            []MarketSubscription               `json:"subscriptions"`
	BackupDrills             []BackupDrillRecord                `json:"backup_drills"`
	MarketReports            []MarketReport                     `json:"market_reports"`
	MarketAppeals            []MarketAppeal                     `json:"market_appeals"`
	ShowDocSettings          ShowDocSettings                    `json:"showdoc_settings"`
	NotificationBindings     map[string]NotificationBinding     `json:"notification_bindings"`
	NotificationPreferences  map[string]NotificationPreference  `json:"notification_preferences"`
	NotificationBindSessions []NotificationBindSession          `json:"notification_bind_sessions"`
	NotificationDeliveries   []NotificationDelivery             `json:"notification_deliveries"`
	MarketOrderIntents       []MarketOrderIntent                `json:"market_order_intents"`
	ProbePoints              []ProbePoint                       `json:"probe_points"`
	OwnerNodeGroups          []OwnerNodeGroup                   `json:"owner_node_groups"`
	OwnerOverviewPreferences map[string]OwnerOverviewPreference `json:"owner_overview_preferences"`
	NodeIPRecords            map[string]NodeIPRecord            `json:"node_ip_records"`
	NodeOperationalEvents    []NodeOperationalEvent             `json:"node_operational_events"`
	TrafficAlertPolicies     map[string]TrafficAlertPolicy      `json:"traffic_alert_policies"`
	SetupCompleted           map[string]bool                    `json:"setup_completed"`
	UpdatedAt                int64                              `json:"updated_at"`
}

type PlatformFeatureHub struct {
	mu                     sync.RWMutex
	path                   string
	cipher                 *tokenCipher
	webhookClient          *http.Client
	notificationProcessing bool
	data                   PlatformFeatureData
}

func NewPlatformFeatureHub(path, secret string) (*PlatformFeatureHub, error) {
	cipher, err := newTokenCipher(secret)
	if err != nil {
		return nil, err
	}
	hub := &PlatformFeatureHub{path: path, cipher: cipher, webhookClient: newSafeWebhookClient(), data: defaultPlatformFeatureData()}
	if err := hub.load(); err != nil {
		return nil, err
	}
	return hub, nil
}

func defaultPlatformFeatureData() PlatformFeatureData {
	return PlatformFeatureData{
		Version:                  platformFeatureVersion,
		NodeProfiles:             map[string]NodeFeatureProfile{},
		SellerTrust:              map[string]SellerTrustProfile{},
		ShowDocSettings:          defaultShowDocSettings(),
		NotificationBindings:     map[string]NotificationBinding{},
		NotificationPreferences:  map[string]NotificationPreference{},
		OwnerOverviewPreferences: map[string]OwnerOverviewPreference{},
		NodeIPRecords:            map[string]NodeIPRecord{},
		TrafficAlertPolicies:     map[string]TrafficAlertPolicy{},
		ProbePoints:              []ProbePoint{{ID: "probe_local", Name: "Local", Region: "local", Mode: "local", Enabled: true, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix()}},
		SetupCompleted:           map[string]bool{},
	}
}

func platformFeaturePath(dataPath string) string {
	return filepath.Join(filepath.Dir(dataPath), "platform-features.json")
}

func (h *PlatformFeatureHub) load() error {
	raw, err := os.ReadFile(h.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var data PlatformFeatureData
	if err := json.Unmarshal(raw, &data); err != nil {
		return fmt.Errorf("decode platform features: %w", err)
	}
	if data.Version > platformFeatureVersion {
		return fmt.Errorf("platform features version %d is newer than supported version %d", data.Version, platformFeatureVersion)
	}
	if data.NodeProfiles == nil {
		data.NodeProfiles = map[string]NodeFeatureProfile{}
	}
	if data.SellerTrust == nil {
		data.SellerTrust = map[string]SellerTrustProfile{}
	}
	if data.SetupCompleted == nil {
		data.SetupCompleted = map[string]bool{}
	}
	if data.NotificationBindings == nil {
		data.NotificationBindings = map[string]NotificationBinding{}
	}
	if data.NotificationPreferences == nil {
		data.NotificationPreferences = map[string]NotificationPreference{}
	}
	if data.OwnerOverviewPreferences == nil {
		data.OwnerOverviewPreferences = map[string]OwnerOverviewPreference{}
	}
	if data.NodeIPRecords == nil {
		data.NodeIPRecords = map[string]NodeIPRecord{}
	}
	if data.TrafficAlertPolicies == nil {
		data.TrafficAlertPolicies = map[string]TrafficAlertPolicy{}
	}
	if data.Version < 4 && len(data.ProbePoints) == 0 {
		now := time.Now().Unix()
		data.ProbePoints = []ProbePoint{{ID: "probe_local", Name: "Local", Region: "local", Mode: "local", Enabled: true, CreatedAt: now, UpdatedAt: now}}
	}
	if data.Version < 3 {
		data.ShowDocSettings = defaultShowDocSettings()
	} else {
		data.ShowDocSettings = normalizeShowDocSettings(data.ShowDocSettings)
	}
	now := time.Now().Unix()
	activeSessions := data.NotificationBindSessions[:0]
	for _, item := range data.NotificationBindSessions {
		if item.ExpiresAt > now && item.ConsumedAt == 0 {
			activeSessions = append(activeSessions, item)
		}
	}
	data.NotificationBindSessions = activeSessions
	data.Version = platformFeatureVersion
	h.data = data
	return nil
}

func (h *PlatformFeatureHub) saveLocked() error {
	h.data.Version = platformFeatureVersion
	h.data.UpdatedAt = time.Now().Unix()
	return writeJSONAtomic(h.path, h.data, 0o600)
}

func (h *PlatformFeatureHub) Snapshot() PlatformFeatureData {
	h.mu.RLock()
	defer h.mu.RUnlock()
	raw, _ := json.Marshal(h.data)
	var out PlatformFeatureData
	_ = json.Unmarshal(raw, &out)
	for i := range out.APIKeys {
		out.APIKeys[i].KeyHash = ""
	}
	for i := range out.Webhooks {
		out.Webhooks[i].SecretCipher = ""
	}
	for i := range out.MarketReports {
		out.MarketReports[i].ReporterHash = ""
	}
	out.ShowDocSettings.AdminTokenCipher = ""
	for ownerID, binding := range out.NotificationBindings {
		binding.TokenCipher = ""
		out.NotificationBindings[ownerID] = binding
	}
	for i := range out.NotificationBindSessions {
		out.NotificationBindSessions[i].QRScene = ""
	}
	return out
}

func cleanFeatureText(value string, max int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	runes := []rune(value)
	if len(runes) > max {
		value = string(runes[:max])
	}
	return value
}

func cleanFeatureList(values []string, maxItems, maxLength int) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = cleanFeatureText(value, maxLength)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
		if len(out) >= maxItems {
			break
		}
	}
	sort.Strings(out)
	return out
}

func newFeatureID(prefix string) (string, error) {
	buf := make([]byte, 9)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(buf), nil
}

func newAPIKey() (string, string, string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", "", err
	}
	token := "mpk_" + base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(token))
	return token, token[:12], hex.EncodeToString(sum[:]), nil
}

func apiKeyHash(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func (h *PlatformFeatureHub) APIKey(token, scope string) (APIKeyRecord, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	hash, now := apiKeyHash(token), time.Now().Unix()
	for i := range h.data.APIKeys {
		item := &h.data.APIKeys[i]
		if item.Enabled && constantEqual(item.KeyHash, hash) && (item.ExpiresAt == 0 || item.ExpiresAt > now) && listContains(item.Scopes, scope) {
			if now-item.LastUsedAt >= 60 {
				item.LastUsedAt = now
				_ = h.saveLocked()
			}
			out := *item
			out.KeyHash = ""
			return out, true
		}
	}
	return APIKeyRecord{}, false
}

func listContains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value || candidate == "*" {
			return true
		}
	}
	return false
}

func (h *PlatformFeatureHub) NodeMatches(nodeID string, tags []string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	profile := h.data.NodeProfiles[nodeID]
	if len(tags) == 0 {
		return true
	}
	for _, tag := range tags {
		if listContains(profile.Tags, tag) {
			return true
		}
	}
	return false
}

func (h *PlatformFeatureHub) MaintenanceActive(nodeID string, now int64) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	profile := h.data.NodeProfiles[nodeID]
	for _, item := range h.data.Maintenance {
		if !item.MuteAlerts || now < item.StartAt || now >= item.EndAt {
			continue
		}
		if len(item.NodeIDs) == 0 && len(item.Tags) == 0 || listContains(item.NodeIDs, nodeID) {
			return true
		}
		for _, tag := range item.Tags {
			if listContains(profile.Tags, tag) {
				return true
			}
		}
	}
	return false
}

func (h *PlatformFeatureHub) StatusPage(slug string) (PublicStatusPage, []StatusIncident, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, page := range h.data.StatusPages {
		if page.Enabled && page.Slug == slug {
			incidents := []StatusIncident{}
			for _, incident := range h.data.Incidents {
				if incident.StatusPageID == page.ID {
					incidents = append(incidents, incident)
				}
			}
			sort.Slice(incidents, func(i, j int) bool { return incidents[i].StartedAt > incidents[j].StartedAt })
			return page, incidents, true
		}
	}
	return PublicStatusPage{}, nil, false
}

func validateFeatureID(value string) bool {
	if value == "" || len(value) > 80 {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

package domain

import "vps-agent/internal/agent"

type Settings struct {
	SiteName                 string   `json:"site_name"`
	LandingEnabled           bool     `json:"landing_enabled"`
	MarketEnabled            bool     `json:"market_enabled"`
	FooterHidden             bool     `json:"footer_hidden"`
	FooterText               string   `json:"footer_text"`
	FriendLinksTitle         string   `json:"friend_links_title"`
	VisitorStatsHidden       bool     `json:"visitor_stats_hidden"`
	VisitorStatsItems        []string `json:"visitor_stats_items"`
	TimeZone                 string   `json:"time_zone"`
	ExchangeRateMode         string   `json:"exchange_rate_mode"`
	ManualUSDCNY             float64  `json:"manual_usd_cny"`
	UserRegistrationDisabled bool     `json:"user_registration_disabled"`
	UserNodeApplyDisabled    bool     `json:"user_node_apply_disabled"`
	UserNodeLimit            int      `json:"user_node_limit"`
}

type ServiceMonitor struct {
	ID                     string              `json:"id"`
	OwnerID                string              `json:"owner_id,omitempty"`
	NodeID                 string              `json:"node_id,omitempty"`
	Name                   string              `json:"name"`
	Kind                   string              `json:"kind"`
	Type                   string              `json:"type,omitempty"`
	Target                 string              `json:"target"`
	Port                   int                 `json:"port,omitempty"`
	Enabled                bool                `json:"enabled"`
	IntervalSeconds        int                 `json:"interval_seconds"`
	TimeoutSeconds         int                 `json:"timeout_seconds"`
	FailureThreshold       int                 `json:"failure_threshold"`
	FailureDurationSeconds int                 `json:"failure_duration_seconds"`
	ExpectedStatus         int                 `json:"expected_status,omitempty"`
	ExpectedKeyword        string              `json:"expected_keyword,omitempty"`
	SSLWarnDays            int                 `json:"ssl_warn_days,omitempty"`
	TLSWarningDays         int                 `json:"tls_warning_days,omitempty"`
	ProbePointIDs          []string            `json:"probe_point_ids,omitempty"`
	State                  ServiceMonitorState `json:"state"`
	CreatedAt              int64               `json:"created_at"`
	UpdatedAt              int64               `json:"updated_at"`
	LastRunAt              int64               `json:"last_run_at,omitempty"`
	NextRunAt              int64               `json:"next_run_at,omitempty"`
}

type ServiceMonitorState struct {
	MonitorID             string `json:"monitor_id"`
	Status                string `json:"status"`
	ConsecutiveFailures   int    `json:"consecutive_failures"`
	FirstFailureAt        int64  `json:"first_failure_at,omitempty"`
	LastSuccessAt         int64  `json:"last_success_at,omitempty"`
	LastFailureAt         int64  `json:"last_failure_at,omitempty"`
	LastAlertAt           int64  `json:"last_alert_at,omitempty"`
	LastIP                string `json:"last_ip,omitempty"`
	LastTLSFingerprint    string `json:"last_tls_fingerprint,omitempty"`
	LastTLSNotAfter       int64  `json:"last_tls_not_after,omitempty"`
	TLSExpiryAlertedFor   int64  `json:"tls_expiry_alerted_for,omitempty"`
	LastCertificateChange int64  `json:"last_certificate_change,omitempty"`
	LastCheckAt           int64  `json:"last_check_at,omitempty"`
	LastChangedAt         int64  `json:"last_changed_at,omitempty"`
	LastLatencyMS         int64  `json:"last_latency_ms,omitempty"`
	LastError             string `json:"last_error,omitempty"`
	CertificateExpiresAt  int64  `json:"certificate_expires_at,omitempty"`
}

type ServiceMonitorResult struct {
	ID             string `json:"id"`
	MonitorID      string `json:"monitor_id"`
	OwnerID        string `json:"owner_id,omitempty"`
	ProbePointID   string `json:"probe_point_id,omitempty"`
	Success        bool   `json:"success"`
	LatencyMS      int64  `json:"latency_ms"`
	StatusCode     int    `json:"status_code,omitempty"`
	Error          string `json:"error,omitempty"`
	ResolvedIP     string `json:"resolved_ip,omitempty"`
	TLSFingerprint string `json:"tls_fingerprint,omitempty"`
	TLSNotAfter    int64  `json:"tls_not_after,omitempty"`
	CheckedAt      int64  `json:"checked_at"`
}

type FriendLink struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	URL       string `json:"url"`
	Enabled   bool   `json:"enabled"`
	NewTab    bool   `json:"new_tab"`
	SortOrder int    `json:"sort_order"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

type PlannedNode struct {
	NodeID      string `json:"node_id"`
	OwnerID     string `json:"owner_id,omitempty"`
	Private     bool   `json:"private"`
	CreatedAt   int64  `json:"created_at"`
	TokenHash   string `json:"token_hash,omitempty"`
	Token       string `json:"token,omitempty"` // AES-GCM ciphertext; decrypted only for an authenticated admin command
	DisplayName string `json:"display_name,omitempty"`
	Region      string `json:"region,omitempty"`
	RegionCode  string `json:"region_code,omitempty"`
}

type AdminNode struct {
	NodeID         string   `json:"node_id"`
	OwnerID        string   `json:"owner_id,omitempty"`
	Private        bool     `json:"private"`
	DisplayName    string   `json:"display_name,omitempty"`
	Region         string   `json:"region,omitempty"`
	RegionCode     string   `json:"region_code,omitempty"`
	Online         bool     `json:"online"`
	LastSeen       int64    `json:"last_seen"`
	CreatedAt      int64    `json:"created_at"`
	HasToken       bool     `json:"has_token"`
	CanViewToken   bool     `json:"can_view_token"` // false for legacy nodes that only have hash
	AgentVersion   string   `json:"agent_version,omitempty"`
	AgentStartedAt int64    `json:"agent_started_at,omitempty"`
	AgentOS        string   `json:"agent_os,omitempty"`
	AgentArch      string   `json:"agent_arch,omitempty"`
	Info           HostInfo `json:"info"`
}

type NodeBackup struct {
	Version    int                `json:"version"`
	ExportedAt int64              `json:"exported_at"`
	Nodes      []NodeBackupRecord `json:"nodes"`
}

type NodeBackupRecord struct {
	NodeID      string   `json:"node_id"`
	OwnerID     string   `json:"owner_id,omitempty"`
	Private     bool     `json:"private"`
	CreatedAt   int64    `json:"created_at"`
	TokenHash   string   `json:"token_hash,omitempty"`
	Token       string   `json:"token,omitempty"` // legacy import only; exports leave this empty
	DisplayName string   `json:"display_name,omitempty"`
	Region      string   `json:"region,omitempty"`
	RegionCode  string   `json:"region_code,omitempty"`
	Info        HostInfo `json:"info"`
}

type HostInfo struct {
	Name            string  `json:"name"`
	DisplayName     string  `json:"display_name,omitempty"`
	Region          string  `json:"region,omitempty"`
	RegionCode      string  `json:"region_code,omitempty"`
	DueTime         int64   `json:"due_time"`
	BuyURL          string  `json:"buy_url"`
	Seller          string  `json:"seller"`
	Price           string  `json:"price"`
	Cycle           string  `json:"cycle"`
	PriceAmount     float64 `json:"price_amount,omitempty"`
	PriceCurrency   string  `json:"price_currency,omitempty"`
	BillingCycle    string  `json:"billing_cycle,omitempty"`
	Bandwidth       string  `json:"bandwidth"`
	Traffic         string  `json:"traffic"`
	TrafficResetDay int     `json:"traffic_reset_day"`
	Show            bool    `json:"show_purchase_info"`
	AuthSecret      string  `json:"auth_secret,omitempty"`
}

type TrafficStat struct {
	ResetDay    int    `json:"reset_day"`
	PeriodStart int64  `json:"period_start"`
	NextReset   int64  `json:"next_reset"`
	LastRxBytes uint64 `json:"last_rx_bytes"`
	LastTxBytes uint64 `json:"last_tx_bytes"`
	RxTotal     uint64 `json:"rx_total"`
	TxTotal     uint64 `json:"tx_total"`
	UpdatedAt   int64  `json:"updated_at"`
}

// Owner is a third-party marketplace account (email + bcrypt password).
type Owner struct {
	ID           string `json:"id"`
	Email        string `json:"email"`
	PasswordHash string `json:"password_hash,omitempty"`
	CreatedAt    int64  `json:"created_at"`
	LastLogin    int64  `json:"last_login"`
	Disabled     bool   `json:"disabled"`
}

// MarketListing is a for-sale entry tied to a planned node and owner.
// DisplayName / Region live on PlannedNode / HostInfo and are joined at read time.
type MarketListing struct {
	NodeID           string  `json:"node_id"`
	OwnerID          string  `json:"owner_id"`
	ForSale          bool    `json:"for_sale"`
	ListingType      string  `json:"listing_type"` // rent | sale | transfer
	Contact          string  `json:"contact"`
	Description      string  `json:"description"`
	Specs            string  `json:"specs"`
	Price            string  `json:"price"`
	PriceAmount      float64 `json:"price_amount,omitempty"`
	PriceCurrency    string  `json:"price_currency,omitempty"`
	BillingCycle     string  `json:"billing_cycle,omitempty"`
	Pinned           bool    `json:"pinned"`
	CreatedAt        int64   `json:"created_at"`
	UpdatedAt        int64   `json:"updated_at"`
	ModerationStatus string  `json:"moderation_status"`
	RejectionReason  string  `json:"rejection_reason,omitempty"`
}

// MarketCategory is a derived region bucket for the public market UI.
type MarketCategory struct {
	ID        string `json:"id"`   // region_code, e.g. "HK"
	Name      string `json:"name"` // region display name
	NodeCount int    `json:"node_count"`
}

// MarketListingView is the public/owner-facing listing with node metadata joined in.
type MarketListingView struct {
	NodeID           string           `json:"node_id"`
	OwnerID          string           `json:"owner_id,omitempty"`
	DisplayName      string           `json:"display_name"`
	Region           string           `json:"region"`
	RegionCode       string           `json:"region_code"`
	ForSale          bool             `json:"for_sale"`
	ListingType      string           `json:"listing_type"`
	Contact          string           `json:"contact"`
	Description      string           `json:"description"`
	Specs            string           `json:"specs"`
	Price            string           `json:"price"`
	PriceAmount      float64          `json:"price_amount,omitempty"`
	PriceCurrency    string           `json:"price_currency,omitempty"`
	BillingCycle     string           `json:"billing_cycle,omitempty"`
	Pinned           bool             `json:"pinned"`
	Online           bool             `json:"online"`
	LastSeen         int64            `json:"last_seen"`
	CreatedAt        int64            `json:"created_at"`
	UpdatedAt        int64            `json:"updated_at"`
	DueTime          int64            `json:"due_time"`
	ModerationStatus string           `json:"moderation_status"`
	RejectionReason  string           `json:"rejection_reason,omitempty"`
	SellerTrust      *SellerTrustView `json:"seller_trust,omitempty"`
	// Optional live host facts when agent has reported.
	LogicalCores   int    `json:"logical_cores,omitempty"`
	MemTotal       uint64 `json:"mem_total,omitempty"`
	DiskTotal      uint64 `json:"disk_total,omitempty"`
	NetInSpeed     uint64 `json:"net_in_speed,omitempty"`
	NetOutSpeed    uint64 `json:"net_out_speed,omitempty"`
	NetInTransfer  uint64 `json:"net_in_transfer,omitempty"`
	NetOutTransfer uint64 `json:"net_out_transfer,omitempty"`
}

type SellerTrustView struct {
	Verified   bool   `json:"verified"`
	Level      string `json:"level"`
	Note       string `json:"note,omitempty"`
	Violations int    `json:"violations,omitempty"`
}

// MarketListingUpdate is a partial update for owner-editable sales fields.
type MarketListingUpdate struct {
	ListingType   *string  `json:"listing_type,omitempty"`
	Contact       *string  `json:"contact,omitempty"`
	Description   *string  `json:"description,omitempty"`
	Specs         *string  `json:"specs,omitempty"`
	Price         *string  `json:"price,omitempty"`
	PriceAmount   *float64 `json:"price_amount,omitempty"`
	PriceCurrency *string  `json:"price_currency,omitempty"`
	BillingCycle  *string  `json:"billing_cycle,omitempty"`
	ForSale       *bool    `json:"for_sale,omitempty"`
}

type NodeBillingUpdate struct {
	Price         string  `json:"price"`
	LegacyCycle   string  `json:"legacy_cycle"`
	PriceAmount   float64 `json:"price_amount"`
	PriceCurrency string  `json:"price_currency"`
	BillingCycle  string  `json:"billing_cycle"`
}

// AuditEvent is an append-only record of a security- or data-relevant operation.
type AuditEvent struct {
	ID         string `json:"id"`
	ActorType  string `json:"actor_type"`
	ActorID    string `json:"actor_id"`
	Action     string `json:"action"`
	ObjectType string `json:"object_type"`
	ObjectID   string `json:"object_id"`
	Timestamp  int64  `json:"timestamp"`
	IP         string `json:"ip"`
	Summary    string `json:"summary"`
}

// MarketSubmission is committed atomically so a failed first listing cannot
// leave an owner, node, token, or listing behind.
type MarketSubmission struct {
	Owner          Owner         `json:"owner"`
	NewOwner       bool          `json:"new_owner"`
	Node           PlannedNode   `json:"node"`
	Info           HostInfo      `json:"info"`
	Listing        MarketListing `json:"listing"`
	AgentToken     string        `json:"-"`
	OwnerNodeLimit int           `json:"-"`
	Audit          AuditEvent    `json:"audit"`
}

// OwnerNodeMutation groups every owner-editable projection of a node into one commit.
type OwnerNodeMutation struct {
	NodeID      string              `json:"node_id"`
	Listing     MarketListingUpdate `json:"listing"`
	DisplayName *string             `json:"display_name,omitempty"`
	Region      *string             `json:"region,omitempty"`
	RegionCode  *string             `json:"region_code,omitempty"`
	DueTime     *int64              `json:"due_time,omitempty"`
	Billing     *NodeBillingUpdate  `json:"billing,omitempty"`
	Audit       AuditEvent          `json:"audit"`
}

type OwnerAdminMutation struct {
	OwnerID      string     `json:"owner_id"`
	Disabled     *bool      `json:"disabled,omitempty"`
	PasswordHash *string    `json:"-"`
	Audit        AuditEvent `json:"audit"`
}

type ListingModerationMutation struct {
	NodeID          string     `json:"node_id"`
	Status          string     `json:"status"`
	RejectionReason string     `json:"rejection_reason,omitempty"`
	Audit           AuditEvent `json:"audit"`
}

type RecycledNode struct {
	Planned PlannedNode    `json:"planned"`
	Info    *HostInfo      `json:"info,omitempty"`
	Report  *agent.Metrics `json:"report,omitempty"`
	Traffic *TrafficStat   `json:"traffic,omitempty"`
	Listing *MarketListing `json:"listing,omitempty"`
}

// RecycleRecord payloads are persisted, but admin handlers expose summaries only.
type RecycleRecord struct {
	ID        string         `json:"id"`
	Kind      string         `json:"kind"`
	NodeID    string         `json:"node_id"`
	DeletedAt int64          `json:"deleted_at"`
	DeletedBy string         `json:"deleted_by"`
	Summary   string         `json:"summary"`
	Node      *RecycledNode  `json:"node,omitempty"`
	Listing   *MarketListing `json:"listing,omitempty"`
}

// Advertisement is an operator-managed native ad shown only in the marketplace.
// StartAt and EndAt are Unix timestamps in UTC; zero means no boundary.
type Advertisement struct {
	ID           string `json:"id"`
	Brand        string `json:"brand"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	ImageURL     string `json:"image_url"`
	TargetURL    string `json:"target_url"`
	ButtonText   string `json:"button_text"`
	Enabled      bool   `json:"enabled"`
	Pinned       bool   `json:"pinned"`
	Recommended  bool   `json:"recommended"`
	Priority     int    `json:"priority"`
	PositionMode string `json:"position_mode"` // auto | after | start | end | exclusive
	DesktopAfter int    `json:"desktop_after"`
	MobileAfter  int    `json:"mobile_after"`
	StartAt      int64  `json:"start_at"`
	EndAt        int64  `json:"end_at"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
	Impressions  int64  `json:"impressions"`
	Clicks       int64  `json:"clicks"`
}

type AdLayoutSettings struct {
	DesktopInterval  int    `json:"desktop_interval"`
	MobileInterval   int    `json:"mobile_interval"`
	MaxAds           int    `json:"max_ads"` // 0 means unlimited
	MinServerGap     int    `json:"min_server_gap"`
	AllowConsecutive bool   `json:"allow_consecutive"`
	ConflictStrategy string `json:"conflict_strategy"` // shift | rotate | stack
	RotationMode     string `json:"rotation_mode"`     // fixed | rotate
}

type MarketAdsResponse struct {
	Ads      []Advertisement  `json:"ads"`
	Settings AdLayoutSettings `json:"settings"`
}

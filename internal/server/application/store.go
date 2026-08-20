package application

import (
	"time"

	"vps-agent/internal/agent"
	"vps-agent/internal/server/domain"
)

type Store interface {
	SiteName() string
	GetSettings() domain.Settings
	UpdateSettings(domain.Settings) error
	UpsertReport(agent.Metrics, int) error
	AddPlannedNode(string, int) error
	GetPlannedNode(string) (domain.PlannedNode, bool)
	ListOwnerNodeIDs(string) []string
	CreateOwnerNode(ownerID string, node domain.PlannedNode, info domain.HostInfo, agentToken string, audit domain.AuditEvent, maxNodes, ownerLimit int) error
	SetNodePrivacy(ownerID, nodeID string, private bool, audit domain.AuditEvent) error
	// SetNodeToken stores an encrypted token (for admin re-view) and its hash (for agent auth).
	SetNodeToken(nodeID, token, tokenHash string, maxNodes int) error
	// GetNodeToken decrypts and returns the token for an authenticated admin command.
	GetNodeToken(nodeID string) (token string, ok bool)
	// SetNodeMeta updates display name / region without touching the token.
	SetNodeMeta(nodeID, displayName, region, regionCode string) error
	// GetNodeMeta returns planned-node display metadata.
	GetNodeMeta(nodeID string) (displayName, region, regionCode string, ok bool)
	ValidNodeToken(string, string) bool
	UpsertInfo(domain.HostInfo) error
	SetNodeDueTime(nodeID string, dueTime int64) error
	SetNodeBilling(nodeID, price, legacyCycle string, amount float64, currency, billingCycle string) error
	Delete(string) error
	InfoList() []domain.HostInfo
	AkileHosts() []AkileHost
	AdminNodes(time.Duration) []domain.AdminNode
	ExportNodes() domain.NodeBackup
	ImportNodes(domain.NodeBackup, int) (int, error)

	// Marketplace owners
	CreateOwner(email, passwordHash string) (domain.Owner, error)
	AuthenticateOwner(email, password string) (domain.Owner, error)
	GetOwnerByID(id string) (domain.Owner, bool)
	TouchOwnerLogin(id string) error
	ListOwners(query string, limit, offset int) ([]domain.Owner, int)
	UpdateOwnerAdmin(domain.OwnerAdminMutation) error

	// Marketplace listings
	CreateMarketListing(listing domain.MarketListing) error
	CreateOwnerListing(ownerID string, listing domain.MarketListing, audit domain.AuditEvent) error
	UpdateMarketListing(nodeID string, update domain.MarketListingUpdate) error
	GetMarketListing(nodeID string) (domain.MarketListing, bool)
	ListMarketListings(forSaleOnly bool) []domain.MarketListing
	ListOwnerListings(ownerID string) []domain.MarketListing
	SetListingForSale(nodeID string, forSale bool) error
	SetListingPinned(nodeID string, pinned bool) error
	DeleteMarketListing(nodeID string) error
	CreateMarketSubmission(domain.MarketSubmission, int) error
	UpdateOwnerNode(ownerID string, mutation domain.OwnerNodeMutation) error
	ModerateMarketListing(domain.ListingModerationMutation) error

	// Recycle bin and append-only operation audit.
	TrashNode(nodeID string, audit domain.AuditEvent) (domain.RecycleRecord, error)
	TrashMarketListing(nodeID string, audit domain.AuditEvent) (domain.RecycleRecord, error)
	ListRecycleRecords() []domain.RecycleRecord
	RestoreRecycleRecord(id string, audit domain.AuditEvent) error
	PurgeRecycleRecord(id string, audit domain.AuditEvent) error
	AppendAudit(domain.AuditEvent) error
	ListAuditEvents(limit int) []domain.AuditEvent

	// Service availability monitors and their durable check state/history.
	ListServiceMonitors(ownerID string, all bool) []domain.ServiceMonitor
	GetServiceMonitor(id string) (domain.ServiceMonitor, bool)
	SaveServiceMonitor(domain.ServiceMonitor) error
	DeleteServiceMonitor(id string) error
	GetServiceMonitorState(id string) (domain.ServiceMonitorState, bool)
	SaveServiceMonitorCheck(domain.ServiceMonitor, domain.ServiceMonitorState, domain.ServiceMonitorResult) error
	ListServiceMonitorResults(monitorID string, limit int) []domain.ServiceMonitorResult

	// Marketplace advertisements
	ListAdvertisements(includeInactive bool, now time.Time) []domain.Advertisement
	GetAdvertisement(id string) (domain.Advertisement, bool)
	SaveAdvertisement(domain.Advertisement) (domain.Advertisement, error)
	DeleteAdvertisement(id string) error
	IncrementAdvertisementStat(id string, click bool) error
	GetAdLayoutSettings() domain.AdLayoutSettings
	UpdateAdLayoutSettings(domain.AdLayoutSettings) error

	// Site footer friend links
	ListFriendLinks(includeDisabled bool) []domain.FriendLink
	GetFriendLink(id string) (domain.FriendLink, bool)
	SaveFriendLink(domain.FriendLink) (domain.FriendLink, error)
	DeleteFriendLink(id string) error
	ReorderFriendLinks(ids []string) error
}

type AkileHost struct {
	Host      AkileHostMeta  `json:"Host"`
	State     AkileHostState `json:"State"`
	TimeStamp int64          `json:"TimeStamp"`
}

type AkileHostMeta struct {
	Name            string   `json:"Name"`
	DisplayName     string   `json:"DisplayName,omitempty"`
	Region          string   `json:"Region,omitempty"`
	RegionCode      string   `json:"RegionCode,omitempty"`
	Hostname        string   `json:"Hostname"`
	Platform        string   `json:"Platform"`
	PlatformVersion string   `json:"PlatformVersion"`
	Kernel          string   `json:"Kernel"`
	Arch            string   `json:"Arch"`
	Virtualization  string   `json:"Virtualization"`
	CPU             []int    `json:"CPU"`
	CPUModel        string   `json:"CPUModel"`
	GPUs            []string `json:"GPUs"`
	PhysicalCores   int      `json:"PhysicalCores"`
	LogicalCores    int      `json:"LogicalCores"`
	MemTotal        uint64   `json:"MemTotal"`
	SwapTotal       uint64   `json:"SwapTotal"`
}

type AkileHostState struct {
	CPU                 float64      `json:"CPU"`
	MemUsed             uint64       `json:"MemUsed"`
	SwapUsed            uint64       `json:"SwapUsed"`
	DiskUsed            uint64       `json:"DiskUsed"`
	DiskTotal           uint64       `json:"DiskTotal"`
	Disks               []agent.Disk `json:"Disks"`
	NetInTransfer       uint64       `json:"NetInTransfer"`
	NetOutTransfer      uint64       `json:"NetOutTransfer"`
	NetInSpeed          uint64       `json:"NetInSpeed"`
	NetOutSpeed         uint64       `json:"NetOutSpeed"`
	DiskReadSpeed       uint64       `json:"DiskReadSpeed"`
	DiskWriteSpeed      uint64       `json:"DiskWriteSpeed"`
	TCP                 int          `json:"TCP"`
	UDP                 int          `json:"UDP"`
	Processes           int          `json:"Processes"`
	Load1               float64      `json:"Load1"`
	Load5               float64      `json:"Load5"`
	Load15              float64      `json:"Load15"`
	Uptime              uint64       `json:"Uptime"`
	CycleNetInTransfer  uint64       `json:"CycleNetInTransfer"`
	CycleNetOutTransfer uint64       `json:"CycleNetOutTransfer"`
	TrafficResetDay     int          `json:"TrafficResetDay"`
	TrafficPeriodStart  int64        `json:"TrafficPeriodStart"`
	TrafficNextReset    int64        `json:"TrafficNextReset"`
}

package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"vps-agent/internal/agent"
)

type Store struct {
	mu                    sync.RWMutex
	path                  string
	Reports               map[string]agent.Metrics          `json:"reports"`
	Infos                 map[string]HostInfo               `json:"infos"`
	Planned               map[string]PlannedNode            `json:"planned"`
	Settings              Settings                          `json:"settings"`
	Traffic               map[string]TrafficStat            `json:"traffic"`
	Owners                map[string]Owner                  `json:"owners"`
	Listings              map[string]MarketListing          `json:"listings"`
	Ads                   map[string]Advertisement          `json:"advertisements"`
	AdLayout              AdLayoutSettings                  `json:"ad_layout"`
	Links                 map[string]FriendLink             `json:"friend_links"`
	Recycle               map[string]RecycleRecord          `json:"recycle_bin,omitempty"`
	Audit                 []AuditEvent                      `json:"audit_log,omitempty"`
	ServiceMonitors       map[string]ServiceMonitor         `json:"service_monitors,omitempty"`
	ServiceMonitorStates  map[string]ServiceMonitorState    `json:"service_monitor_states,omitempty"`
	ServiceMonitorResults map[string][]ServiceMonitorResult `json:"service_monitor_results,omitempty"`

	lastTrafficSave time.Time    `json:"-"`
	tokenCipher     *tokenCipher `json:"-"`
}

func NewStore(path string, tokenSecret ...string) (*Store, error) {
	secret := "monitor-party-test-token-secret"
	if len(tokenSecret) > 0 {
		secret = tokenSecret[0]
	}
	cipher, err := newTokenCipher(secret)
	if err != nil {
		return nil, err
	}
	s := &Store{
		path:                  path,
		Reports:               map[string]agent.Metrics{},
		Infos:                 map[string]HostInfo{},
		Planned:               map[string]PlannedNode{},
		Settings:              defaultSettings(),
		Traffic:               map[string]TrafficStat{},
		Owners:                map[string]Owner{},
		Listings:              map[string]MarketListing{},
		Ads:                   map[string]Advertisement{},
		AdLayout:              defaultAdLayoutSettings(),
		Links:                 map[string]FriendLink{},
		Recycle:               map[string]RecycleRecord{},
		Audit:                 []AuditEvent{},
		ServiceMonitors:       map[string]ServiceMonitor{},
		ServiceMonitorStates:  map[string]ServiceMonitorState{},
		ServiceMonitorResults: map[string][]ServiceMonitorResult{},
		tokenCipher:           cipher,
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, s); err != nil {
			return nil, err
		}
	}
	if s.Reports == nil {
		s.Reports = map[string]agent.Metrics{}
	}
	if s.Infos == nil {
		s.Infos = map[string]HostInfo{}
	}
	if s.Planned == nil {
		s.Planned = map[string]PlannedNode{}
	}
	if s.Traffic == nil {
		s.Traffic = map[string]TrafficStat{}
	}
	if s.Owners == nil {
		s.Owners = map[string]Owner{}
	}
	if s.Listings == nil {
		s.Listings = map[string]MarketListing{}
	}
	if s.Ads == nil {
		s.Ads = map[string]Advertisement{}
	}
	s.AdLayout = normalizeAdLayoutSettings(s.AdLayout)
	if s.Links == nil {
		s.Links = map[string]FriendLink{}
	}
	if s.Recycle == nil {
		s.Recycle = map[string]RecycleRecord{}
	}
	if s.ServiceMonitors == nil {
		s.ServiceMonitors = map[string]ServiceMonitor{}
	}
	if s.ServiceMonitorStates == nil {
		s.ServiceMonitorStates = map[string]ServiceMonitorState{}
	}
	if s.ServiceMonitorResults == nil {
		s.ServiceMonitorResults = map[string][]ServiceMonitorResult{}
	}
	s.Settings = normalizeSettings(s.Settings)
	if s.Settings.SiteName == "" {
		s.Settings.SiteName = "云镜监控"
	}
	migrated := false
	for nodeID, planned := range s.Planned {
		encrypted, changed, err := s.tokenCipher.encryptStoredValue(planned.Token)
		if err != nil {
			return nil, fmt.Errorf("migrate token for node %q: %w", nodeID, err)
		}
		if changed {
			planned.Token = encrypted
			s.Planned[nodeID] = planned
			migrated = true
		}
	}
	for nodeID, listing := range s.Listings {
		status := normalizeModerationStatus(listing.ModerationStatus)
		if status != listing.ModerationStatus {
			listing.ModerationStatus = status
			s.Listings[nodeID] = listing
			migrated = true
		}
		planned, exists := s.Planned[nodeID]
		if strings.TrimSpace(listing.OwnerID) != "" && (!exists || strings.TrimSpace(planned.OwnerID) == "") {
			planned.NodeID = nodeID
			planned.OwnerID = strings.TrimSpace(listing.OwnerID)
			if planned.CreatedAt == 0 {
				planned.CreatedAt = listing.CreatedAt
				if planned.CreatedAt == 0 {
					planned.CreatedAt = time.Now().Unix()
				}
			}
			s.Planned[nodeID] = planned
			migrated = true
		}
	}
	if migrated {
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

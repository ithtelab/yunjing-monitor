package server

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"vps-agent/internal/agent"
	serverapp "vps-agent/internal/server/application"
	serverdomain "vps-agent/internal/server/domain"
)

func (s *Store) UpsertReport(metrics agent.Metrics, maxNodes int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.Reports[metrics.NodeID]; !exists && len(s.Reports) >= maxNodes {
		return fmt.Errorf("max nodes reached")
	}
	s.Reports[metrics.NodeID] = metrics
	if _, ok := s.Planned[metrics.NodeID]; !ok {
		s.Planned[metrics.NodeID] = PlannedNode{NodeID: metrics.NodeID, CreatedAt: time.Now().Unix()}
	}
	return s.updateTrafficLocked(metrics)
}

func (s *Store) UpsertInfo(info HostInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	info.AuthSecret = ""
	info.TrafficResetDay = serverdomain.NormalizeTrafficResetDay(info.TrafficResetDay)
	if planned, ok := s.Planned[info.Name]; ok {
		if info.DisplayName == "" {
			info.DisplayName = planned.DisplayName
		} else {
			planned.DisplayName = strings.TrimSpace(info.DisplayName)
		}
		if info.Region == "" {
			info.Region = planned.Region
		} else {
			planned.Region = strings.TrimSpace(info.Region)
		}
		if info.RegionCode == "" {
			info.RegionCode = planned.RegionCode
		} else {
			planned.RegionCode = strings.TrimSpace(info.RegionCode)
		}
		s.Planned[info.Name] = planned
	}
	s.Infos[info.Name] = info
	s.syncTrafficResetDayLocked(info.Name, info.TrafficResetDay)
	return s.saveLocked()
}

func (s *Store) SetNodeDueTime(nodeID string, dueTime int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.Planned[nodeID]; !ok {
		return fmt.Errorf("node not found")
	}
	info := s.Infos[nodeID]
	info.Name = nodeID
	info.DueTime = dueTime
	if info.TrafficResetDay == 0 {
		info.TrafficResetDay = serverdomain.NormalizeTrafficResetDay(0)
	}
	s.Infos[nodeID] = info
	return s.saveLocked()
}

func (s *Store) SetNodeBilling(nodeID, price, legacyCycle string, amount float64, currency, billingCycle string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.Planned[nodeID]; !ok {
		return fmt.Errorf("node not found")
	}
	info := s.Infos[nodeID]
	info.Name = nodeID
	info.Price = price
	info.Cycle = legacyCycle
	info.PriceAmount = amount
	info.PriceCurrency = currency
	info.BillingCycle = billingCycle
	if info.TrafficResetDay == 0 {
		info.TrafficResetDay = serverdomain.NormalizeTrafficResetDay(0)
	}
	s.Infos[nodeID] = info
	return s.saveLocked()
}

func (s *Store) InfoList() []HostInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]HostInfo, 0, len(s.Infos))
	for _, info := range s.Infos {
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *Store) AkileHosts() []AkileHost {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]AkileHost, 0, len(s.Planned)+len(s.Reports))
	for _, m := range s.Reports {
		host := serverapp.ToAkileHost(m, s.Traffic[m.NodeID])
		displayName, region, regionCode := nodeMetaLocked(s, m.NodeID)
		serverapp.ApplyHostMeta(&host, displayName, region, regionCode)
		out = append(out, host)
	}
	for name := range s.Planned {
		if _, ok := s.Reports[name]; ok {
			continue
		}
		host := serverapp.OfflineAkileHost(name)
		displayName, region, regionCode := nodeMetaLocked(s, name)
		serverapp.ApplyHostMeta(&host, displayName, region, regionCode)
		out = append(out, host)
	}
	sort.Slice(out, func(i, j int) bool {
		left := out[i].Host.DisplayName
		if left == "" {
			left = out[i].Host.Name
		}
		right := out[j].Host.DisplayName
		if right == "" {
			right = out[j].Host.Name
		}
		return left < right
	})
	return out
}

func nodeMetaLocked(s *Store, nodeID string) (displayName, region, regionCode string) {
	if planned, ok := s.Planned[nodeID]; ok {
		displayName, region, regionCode = planned.DisplayName, planned.Region, planned.RegionCode
	}
	if info, ok := s.Infos[nodeID]; ok {
		if displayName == "" {
			displayName = info.DisplayName
		}
		if region == "" {
			region = info.Region
		}
		if regionCode == "" {
			regionCode = info.RegionCode
		}
	}
	return displayName, region, regionCode
}

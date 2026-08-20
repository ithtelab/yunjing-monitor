package server

import (
	"fmt"
	"sort"
	"strings"
	"time"

	serverdomain "vps-agent/internal/server/domain"
)

func (s *Store) AddPlannedNode(nodeID string, maxNodes int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.Planned[nodeID]; exists {
		return nil
	}
	if len(s.Planned) >= maxNodes {
		return fmt.Errorf("max nodes reached")
	}
	s.Planned[nodeID] = PlannedNode{NodeID: nodeID, CreatedAt: time.Now().Unix()}
	return s.saveLocked()
}

func (s *Store) SetNodeToken(nodeID, token, tokenHash string, maxNodes int) error {
	encrypted, err := s.tokenCipher.encrypt(token)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.Planned[nodeID]; !exists && len(s.Planned) >= maxNodes {
		return fmt.Errorf("max nodes reached")
	}
	planned := s.Planned[nodeID]
	planned.NodeID = nodeID
	if planned.CreatedAt == 0 {
		planned.CreatedAt = time.Now().Unix()
	}
	planned.Token = encrypted
	planned.TokenHash = tokenHash
	s.Planned[nodeID] = planned
	return s.saveLocked()
}

func (s *Store) GetNodeToken(nodeID string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	planned, ok := s.Planned[nodeID]
	if !ok || strings.TrimSpace(planned.Token) == "" {
		return "", false
	}
	token, err := s.tokenCipher.decrypt(planned.Token)
	if err != nil {
		return "", false
	}
	return token, true
}

func (s *Store) SetNodeMeta(nodeID, displayName, region, regionCode string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	planned, ok := s.Planned[nodeID]
	if !ok {
		planned = PlannedNode{NodeID: nodeID, CreatedAt: time.Now().Unix()}
	}
	planned.NodeID = nodeID
	planned.DisplayName = strings.TrimSpace(displayName)
	planned.Region = strings.TrimSpace(region)
	planned.RegionCode = strings.TrimSpace(regionCode)
	s.Planned[nodeID] = planned

	info := s.Infos[nodeID]
	info.Name = nodeID
	info.DisplayName = planned.DisplayName
	info.Region = planned.Region
	info.RegionCode = planned.RegionCode
	s.Infos[nodeID] = info
	return s.saveLocked()
}

func (s *Store) GetNodeMeta(nodeID string) (displayName, region, regionCode string, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	planned, ok := s.Planned[nodeID]
	if !ok {
		if info, hasInfo := s.Infos[nodeID]; hasInfo {
			return info.DisplayName, info.Region, info.RegionCode, true
		}
		return "", "", "", false
	}
	displayName = planned.DisplayName
	region = planned.Region
	regionCode = planned.RegionCode
	if displayName == "" || region == "" {
		if info, hasInfo := s.Infos[nodeID]; hasInfo {
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
	}
	return displayName, region, regionCode, true
}

func (s *Store) ValidNodeToken(nodeID, tokenHash string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	planned, ok := s.Planned[nodeID]
	if !ok || planned.TokenHash == "" || tokenHash == "" {
		return false
	}
	return constantEqual(planned.TokenHash, tokenHash)
}

func (s *Store) Delete(name string) error {
	_, err := s.TrashNode(name, AuditEvent{ActorType: "system", ActorID: "compat", Action: "node.trash", ObjectType: "node", ObjectID: name, Summary: "Node moved to recycle bin"})
	return err
}

func (s *Store) AdminNodes(offlineWait time.Duration) []AdminNode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now().Unix()
	threshold := int64(offlineWait.Seconds())
	seen := map[string]bool{}
	out := make([]AdminNode, 0, len(s.Planned)+len(s.Reports))
	for name, planned := range s.Planned {
		report, hasReport := s.Reports[name]
		lastSeen := int64(0)
		online := false
		if hasReport {
			lastSeen = report.Timestamp
			online = report.Timestamp > 0 && now-report.Timestamp <= threshold
		}
		displayName, region, regionCode := planned.DisplayName, planned.Region, planned.RegionCode
		if info, ok := s.Infos[name]; ok {
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
		out = append(out, AdminNode{
			NodeID:         name,
			OwnerID:        planned.OwnerID,
			Private:        planned.Private,
			DisplayName:    displayName,
			Region:         region,
			RegionCode:     regionCode,
			Online:         online,
			LastSeen:       lastSeen,
			CreatedAt:      planned.CreatedAt,
			HasToken:       planned.TokenHash != "",
			CanViewToken:   func() bool { _, err := s.tokenCipher.decrypt(planned.Token); return err == nil }(),
			AgentVersion:   report.AgentVersion,
			AgentStartedAt: report.AgentStartedAt,
			AgentOS:        report.OS,
			AgentArch:      report.Arch,
			Info:           s.Infos[name],
		})
		seen[name] = true
	}
	for name, report := range s.Reports {
		if seen[name] {
			continue
		}
		online := report.Timestamp > 0 && now-report.Timestamp <= threshold
		info := s.Infos[name]
		out = append(out, AdminNode{
			NodeID:         name,
			DisplayName:    info.DisplayName,
			Region:         info.Region,
			RegionCode:     info.RegionCode,
			Online:         online,
			LastSeen:       report.Timestamp,
			AgentVersion:   report.AgentVersion,
			AgentStartedAt: report.AgentStartedAt,
			AgentOS:        report.OS,
			AgentArch:      report.Arch,
			Info:           info,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NodeID < out[j].NodeID })
	return out
}

func (s *Store) ExportNodes() NodeBackup {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := map[string]bool{}
	for name := range s.Planned {
		names[name] = true
	}
	for name := range s.Infos {
		names[name] = true
	}
	for name := range s.Reports {
		names[name] = true
	}
	out := NodeBackup{Version: 3, ExportedAt: time.Now().Unix(), Nodes: make([]NodeBackupRecord, 0, len(names))}
	for name := range names {
		planned := s.Planned[name]
		info := s.Infos[name]
		info.AuthSecret = ""
		info.TrafficResetDay = serverdomain.NormalizeTrafficResetDay(info.TrafficResetDay)
		displayName := planned.DisplayName
		region := planned.Region
		regionCode := planned.RegionCode
		if displayName == "" {
			displayName = info.DisplayName
		}
		if region == "" {
			region = info.Region
		}
		if regionCode == "" {
			regionCode = info.RegionCode
		}
		out.Nodes = append(out.Nodes, NodeBackupRecord{
			NodeID:      name,
			OwnerID:     planned.OwnerID,
			Private:     planned.Private,
			CreatedAt:   planned.CreatedAt,
			TokenHash:   planned.TokenHash,
			DisplayName: displayName,
			Region:      region,
			RegionCode:  regionCode,
			Info:        info,
		})
	}
	sort.Slice(out.Nodes, func(i, j int) bool { return out.Nodes[i].NodeID < out.Nodes[j].NodeID })
	return out
}

func (s *Store) ImportNodes(backup NodeBackup, maxNodes int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if backup.Version == 0 {
		backup.Version = 1
	}
	if backup.Version != 1 && backup.Version != 2 && backup.Version != 3 {
		return 0, fmt.Errorf("unsupported backup version")
	}
	imported := 0
	now := time.Now().Unix()
	for _, record := range backup.Nodes {
		nodeID := strings.TrimSpace(record.NodeID)
		if nodeID == "" && record.Info.Name != "" {
			nodeID = strings.TrimSpace(record.Info.Name)
		}
		if !validNodeID(nodeID) {
			return imported, fmt.Errorf("invalid node_id: %s", nodeID)
		}
		if _, exists := s.Planned[nodeID]; !exists && len(s.Planned) >= maxNodes {
			return imported, fmt.Errorf("max nodes reached")
		}
		planned := s.Planned[nodeID]
		planned.NodeID = nodeID
		if backup.Version >= 3 {
			planned.OwnerID = strings.TrimSpace(record.OwnerID)
			planned.Private = record.Private
		}
		if record.CreatedAt > 0 {
			planned.CreatedAt = record.CreatedAt
		} else if planned.CreatedAt == 0 {
			planned.CreatedAt = now
		}
		if record.TokenHash != "" {
			planned.TokenHash = strings.TrimSpace(record.TokenHash)
		}
		if record.Token != "" {
			if strings.HasPrefix(strings.TrimSpace(record.Token), encryptedTokenPrefix) {
				return imported, fmt.Errorf("encrypted token cannot be imported; rotate this node token")
			}
			encrypted, encryptErr := s.tokenCipher.encrypt(strings.TrimSpace(record.Token))
			if encryptErr != nil {
				return imported, encryptErr
			}
			planned.Token = encrypted
			if planned.TokenHash == "" {
				planned.TokenHash = hashToken(strings.TrimSpace(record.Token))
			}
		}
		if record.DisplayName != "" {
			planned.DisplayName = strings.TrimSpace(record.DisplayName)
		}
		if record.Region != "" {
			planned.Region = strings.TrimSpace(record.Region)
		}
		if record.RegionCode != "" {
			planned.RegionCode = strings.TrimSpace(record.RegionCode)
		}
		s.Planned[nodeID] = planned
		info := record.Info
		info.Name = nodeID
		info.AuthSecret = ""
		if planned.DisplayName != "" {
			info.DisplayName = planned.DisplayName
		}
		if planned.Region != "" {
			info.Region = planned.Region
		}
		if planned.RegionCode != "" {
			info.RegionCode = planned.RegionCode
		}
		info.TrafficResetDay = serverdomain.NormalizeTrafficResetDay(info.TrafficResetDay)
		s.Infos[nodeID] = info
		s.syncTrafficResetDayLocked(nodeID, info.TrafficResetDay)
		imported++
	}
	if imported > 0 {
		if err := s.saveLocked(); err != nil {
			return imported, err
		}
	}
	return imported, nil
}

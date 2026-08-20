package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	serverdomain "vps-agent/internal/server/domain"
)

func (s *SQLiteStore) AddPlannedNode(nodeID string, maxNodes int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	exists, err := plannedExistsTx(tx, nodeID)
	if err != nil {
		return err
	}
	if exists {
		return tx.Commit()
	}
	count, err := countRowsTx(tx, "planned_nodes")
	if err != nil {
		return err
	}
	if count >= maxNodes {
		return fmt.Errorf("max nodes reached")
	}
	if err := upsertPlannedTx(tx, PlannedNode{NodeID: nodeID, CreatedAt: time.Now().Unix()}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) SetNodeToken(nodeID, token, tokenHash string, maxNodes int) error {
	encrypted, err := s.tokenCipher.encrypt(token)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	exists, err := plannedExistsTx(tx, nodeID)
	if err != nil {
		return err
	}
	if !exists {
		count, err := countRowsTx(tx, "planned_nodes")
		if err != nil {
			return err
		}
		if count >= maxNodes {
			return fmt.Errorf("max nodes reached")
		}
	}
	planned, _, err := getPlannedTx(tx, nodeID)
	if err != nil {
		return err
	}
	if planned.CreatedAt == 0 {
		planned.CreatedAt = time.Now().Unix()
	}
	planned.NodeID = nodeID
	planned.Token = encrypted
	planned.TokenHash = tokenHash
	if err := upsertPlannedTx(tx, planned); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) GetNodeToken(nodeID string) (string, bool) {
	planned, err := s.loadPlannedByID(nodeID)
	if err != nil || strings.TrimSpace(planned.Token) == "" {
		return "", false
	}
	token, err := s.tokenCipher.decrypt(planned.Token)
	if err != nil {
		return "", false
	}
	return token, true
}

func (s *SQLiteStore) SetNodeMeta(nodeID, displayName, region, regionCode string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	planned, exists, err := getPlannedTx(tx, nodeID)
	if err != nil {
		return err
	}
	if !exists {
		planned = PlannedNode{NodeID: nodeID, CreatedAt: time.Now().Unix()}
	}
	planned.NodeID = nodeID
	planned.DisplayName = strings.TrimSpace(displayName)
	planned.Region = strings.TrimSpace(region)
	planned.RegionCode = strings.TrimSpace(regionCode)
	if err := upsertPlannedTx(tx, planned); err != nil {
		return err
	}
	info, hasInfo, err := getInfoTx(tx, nodeID)
	if err != nil {
		return err
	}
	if !hasInfo {
		info = HostInfo{Name: nodeID}
	}
	info.Name = nodeID
	info.DisplayName = planned.DisplayName
	info.Region = planned.Region
	info.RegionCode = planned.RegionCode
	if err := upsertInfoTx(tx, info); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) GetNodeMeta(nodeID string) (displayName, region, regionCode string, ok bool) {
	planned, err := s.loadPlannedByID(nodeID)
	if err == nil {
		displayName, region, regionCode = planned.DisplayName, planned.Region, planned.RegionCode
		ok = true
	}
	info, err := s.loadInfoByID(nodeID)
	if err == nil {
		ok = true
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
	return displayName, region, regionCode, ok
}

func (s *SQLiteStore) ValidNodeToken(nodeID, tokenHash string) bool {
	var stored string
	err := s.db.QueryRow(`SELECT token_hash FROM planned_nodes WHERE node_id = ?`, nodeID).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) || stored == "" || tokenHash == "" {
		return false
	}
	if err != nil {
		log.Printf("sqlite token read failed: %v", err)
		return false
	}
	return constantEqual(stored, tokenHash)
}

func (s *SQLiteStore) Delete(name string) error {
	_, err := s.TrashNode(name, AuditEvent{ActorType: "system", ActorID: "compat", Action: "node.trash", ObjectType: "node", ObjectID: name, Summary: "Node moved to recycle bin"})
	return err
}

func (s *SQLiteStore) AdminNodes(offlineWait time.Duration) []AdminNode {
	planned, err := s.loadPlanned()
	if err != nil {
		log.Printf("sqlite planned read failed: %v", err)
		return nil
	}
	reports, err := s.loadReports()
	if err != nil {
		log.Printf("sqlite reports read failed: %v", err)
		return nil
	}
	infos, err := s.loadInfos()
	if err != nil {
		log.Printf("sqlite infos read failed: %v", err)
		return nil
	}
	now := time.Now().Unix()
	threshold := int64(offlineWait.Seconds())
	seen := map[string]bool{}
	out := make([]AdminNode, 0, len(planned)+len(reports))
	for name, plannedNode := range planned {
		report, hasReport := reports[name]
		lastSeen := int64(0)
		online := false
		if hasReport {
			lastSeen = report.Timestamp
			online = report.Timestamp > 0 && now-report.Timestamp <= threshold
		}
		displayName, region, regionCode := plannedNode.DisplayName, plannedNode.Region, plannedNode.RegionCode
		if info, ok := infos[name]; ok {
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
			OwnerID:        plannedNode.OwnerID,
			Private:        plannedNode.Private,
			DisplayName:    displayName,
			Region:         region,
			RegionCode:     regionCode,
			Online:         online,
			LastSeen:       lastSeen,
			CreatedAt:      plannedNode.CreatedAt,
			HasToken:       plannedNode.TokenHash != "",
			CanViewToken:   func() bool { _, err := s.tokenCipher.decrypt(plannedNode.Token); return err == nil }(),
			AgentVersion:   report.AgentVersion,
			AgentStartedAt: report.AgentStartedAt,
			AgentOS:        report.OS,
			AgentArch:      report.Arch,
			Info:           infos[name],
		})
		seen[name] = true
	}
	for name, report := range reports {
		if seen[name] {
			continue
		}
		online := report.Timestamp > 0 && now-report.Timestamp <= threshold
		info := infos[name]
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

func (s *SQLiteStore) ExportNodes() NodeBackup {
	planned, err := s.loadPlanned()
	if err != nil {
		log.Printf("sqlite planned export failed: %v", err)
		return NodeBackup{Version: 3, ExportedAt: time.Now().Unix()}
	}
	infos, err := s.loadInfos()
	if err != nil {
		log.Printf("sqlite infos export failed: %v", err)
		return NodeBackup{Version: 3, ExportedAt: time.Now().Unix()}
	}
	reports, err := s.loadReports()
	if err != nil {
		log.Printf("sqlite reports export failed: %v", err)
		return NodeBackup{Version: 3, ExportedAt: time.Now().Unix()}
	}
	names := map[string]bool{}
	for name := range planned {
		names[name] = true
	}
	for name := range infos {
		names[name] = true
	}
	for name := range reports {
		names[name] = true
	}
	out := NodeBackup{Version: 3, ExportedAt: time.Now().Unix(), Nodes: make([]NodeBackupRecord, 0, len(names))}
	for name := range names {
		info := infos[name]
		info.AuthSecret = ""
		info.TrafficResetDay = serverdomain.NormalizeTrafficResetDay(info.TrafficResetDay)
		displayName := planned[name].DisplayName
		region := planned[name].Region
		regionCode := planned[name].RegionCode
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
			OwnerID:     planned[name].OwnerID,
			Private:     planned[name].Private,
			CreatedAt:   planned[name].CreatedAt,
			TokenHash:   planned[name].TokenHash,
			DisplayName: displayName,
			Region:      region,
			RegionCode:  regionCode,
			Info:        info,
		})
	}
	sort.Slice(out.Nodes, func(i, j int) bool { return out.Nodes[i].NodeID < out.Nodes[j].NodeID })
	return out
}

func (s *SQLiteStore) ImportNodes(backup NodeBackup, maxNodes int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if backup.Version == 0 {
		backup.Version = 1
	}
	if backup.Version != 1 && backup.Version != 2 && backup.Version != 3 {
		return 0, fmt.Errorf("unsupported backup version")
	}
	imported := 0
	now := time.Now().Unix()
	plannedCount, err := countRowsTx(tx, "planned_nodes")
	if err != nil {
		return 0, err
	}
	for _, record := range backup.Nodes {
		nodeID := strings.TrimSpace(record.NodeID)
		if nodeID == "" && record.Info.Name != "" {
			nodeID = strings.TrimSpace(record.Info.Name)
		}
		if !validNodeID(nodeID) {
			return imported, fmt.Errorf("invalid node_id: %s", nodeID)
		}
		planned, exists, err := getPlannedTx(tx, nodeID)
		if err != nil {
			return imported, err
		}
		if !exists {
			if plannedCount >= maxNodes {
				return imported, fmt.Errorf("max nodes reached")
			}
			plannedCount++
		}
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
			planned.Token, err = s.tokenCipher.encrypt(strings.TrimSpace(record.Token))
			if err != nil {
				return imported, err
			}
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
		if err := upsertPlannedTx(tx, planned); err != nil {
			return imported, err
		}
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
		if err := upsertInfoTx(tx, info); err != nil {
			return imported, err
		}
		if err := syncTrafficResetDayTx(tx, nodeID, info.TrafficResetDay, time.Now()); err != nil {
			return imported, err
		}
		imported++
	}
	if err := tx.Commit(); err != nil {
		return imported, err
	}
	return imported, nil
}

func (s *SQLiteStore) loadPlanned() (map[string]PlannedNode, error) {
	rows, err := s.db.Query(`SELECT node_id, COALESCE(owner_id,''), private, created_at, token_hash, COALESCE(token,''), COALESCE(display_name,''), COALESCE(region,''), COALESCE(region_code,'') FROM planned_nodes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]PlannedNode{}
	for rows.Next() {
		var planned PlannedNode
		var private int
		if err := rows.Scan(&planned.NodeID, &planned.OwnerID, &private, &planned.CreatedAt, &planned.TokenHash, &planned.Token, &planned.DisplayName, &planned.Region, &planned.RegionCode); err != nil {
			return nil, err
		}
		planned.Private = private != 0
		out[planned.NodeID] = planned
	}
	return out, rows.Err()
}

func (s *SQLiteStore) loadPlannedByID(nodeID string) (PlannedNode, error) {
	var planned PlannedNode
	var private int
	err := s.db.QueryRow(`SELECT node_id, COALESCE(owner_id,''), private, created_at, token_hash, COALESCE(token,''), COALESCE(display_name,''), COALESCE(region,''), COALESCE(region_code,'') FROM planned_nodes WHERE node_id = ?`, nodeID).
		Scan(&planned.NodeID, &planned.OwnerID, &private, &planned.CreatedAt, &planned.TokenHash, &planned.Token, &planned.DisplayName, &planned.Region, &planned.RegionCode)
	planned.Private = private != 0
	return planned, err
}

func (s *SQLiteStore) loadInfoByID(nodeID string) (HostInfo, error) {
	var payload string
	if err := s.db.QueryRow(`SELECT info_json FROM host_infos WHERE node_id = ?`, nodeID).Scan(&payload); err != nil {
		return HostInfo{}, err
	}
	var info HostInfo
	if err := json.Unmarshal([]byte(payload), &info); err != nil {
		return HostInfo{}, err
	}
	info.Name = nodeID
	return info, nil
}

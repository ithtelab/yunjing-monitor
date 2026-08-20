package server

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (s *SQLiteStore) GetPlannedNode(nodeID string) (PlannedNode, bool) {
	planned, err := s.loadPlannedByID(strings.TrimSpace(nodeID))
	return planned, err == nil
}

func (s *SQLiteStore) ListOwnerNodeIDs(ownerID string) []string {
	rows, err := s.db.Query(`SELECT node_id FROM planned_nodes WHERE owner_id = ? ORDER BY node_id`, strings.TrimSpace(ownerID))
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := make([]string, 0)
	for rows.Next() {
		var nodeID string
		if rows.Scan(&nodeID) == nil {
			out = append(out, nodeID)
		}
	}
	sort.Strings(out)
	return out
}

func (s *SQLiteStore) CreateOwnerNode(ownerID string, node PlannedNode, info HostInfo, agentToken string, audit AuditEvent, maxNodes, ownerLimit int) error {
	ownerID, node.NodeID, agentToken = strings.TrimSpace(ownerID), strings.TrimSpace(node.NodeID), strings.TrimSpace(agentToken)
	if ownerID == "" || !validNodeID(node.NodeID) || agentToken == "" {
		return fmt.Errorf("valid owner, node and agent token are required")
	}
	encrypted, err := s.tokenCipher.encrypt(agentToken)
	if err != nil {
		return err
	}
	if ownerLimit <= 0 {
		ownerLimit = 3
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var ownerExists int
	if err := tx.QueryRow(`SELECT 1 FROM owners WHERE id = ?`, ownerID).Scan(&ownerExists); errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("owner not found")
	} else if err != nil {
		return err
	}
	if exists, err := plannedExistsTx(tx, node.NodeID); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("node already exists")
	}
	count, err := countRowsTx(tx, "planned_nodes")
	if err != nil {
		return err
	}
	if count >= maxNodes {
		return fmt.Errorf("max nodes reached")
	}
	var ownerNodes int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM planned_nodes WHERE owner_id = ?`, ownerID).Scan(&ownerNodes); err != nil {
		return err
	}
	if ownerNodes >= ownerLimit {
		return fmt.Errorf("owner node limit reached")
	}
	node.OwnerID = ownerID
	node.Token, node.TokenHash = encrypted, hashToken(agentToken)
	if node.CreatedAt == 0 {
		node.CreatedAt = time.Now().Unix()
	}
	info.Name, info.AuthSecret = node.NodeID, ""
	if node.DisplayName != "" {
		info.DisplayName = node.DisplayName
	}
	if node.Region != "" {
		info.Region = node.Region
	}
	if node.RegionCode != "" {
		info.RegionCode = node.RegionCode
	}
	if err := upsertPlannedTx(tx, node); err != nil {
		return err
	}
	if err := upsertInfoTx(tx, info); err != nil {
		return err
	}
	if err := insertAuditTx(tx, audit); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) SetNodePrivacy(ownerID, nodeID string, private bool, audit AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE planned_nodes SET private = ? WHERE node_id = ? AND owner_id = ?`, boolToInt(private), strings.TrimSpace(nodeID), strings.TrimSpace(ownerID))
	if err != nil {
		return err
	}
	if count, _ := res.RowsAffected(); count == 0 {
		return fmt.Errorf("node not found")
	}
	if err := insertAuditTx(tx, audit); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) CreateOwnerListing(ownerID string, listing MarketListing, audit AuditEvent) error {
	ownerID, listing.NodeID = strings.TrimSpace(ownerID), strings.TrimSpace(listing.NodeID)
	if ownerID == "" || !validNodeID(listing.NodeID) {
		return fmt.Errorf("valid owner and node are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	planned, exists, err := getPlannedTx(tx, listing.NodeID)
	if err != nil {
		return err
	}
	if !exists || planned.OwnerID != ownerID {
		return fmt.Errorf("node not found")
	}
	if _, exists, err := getListingTx(tx, listing.NodeID); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("listing already exists")
	}
	now := time.Now().Unix()
	listing.OwnerID = ownerID
	if listing.CreatedAt == 0 {
		listing.CreatedAt = now
	}
	listing.UpdatedAt = now
	listing.ModerationStatus = normalizeModerationStatus(listing.ModerationStatus)
	if err := upsertListingTx(tx, listing); err != nil {
		return err
	}
	if err := insertAuditTx(tx, audit); err != nil {
		return err
	}
	return tx.Commit()
}

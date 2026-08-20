package server

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

func (s *Store) GetPlannedNode(nodeID string) (PlannedNode, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	planned, ok := s.Planned[strings.TrimSpace(nodeID)]
	return planned, ok
}

func (s *Store) ListOwnerNodeIDs(ownerID string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ownerID = strings.TrimSpace(ownerID)
	out := make([]string, 0)
	for nodeID, planned := range s.Planned {
		if planned.OwnerID == ownerID {
			out = append(out, nodeID)
		}
	}
	sort.Strings(out)
	return out
}

func (s *Store) CreateOwnerNode(ownerID string, node PlannedNode, info HostInfo, agentToken string, audit AuditEvent, maxNodes, ownerLimit int) error {
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
	snapshot, err := s.snapshotLocked()
	if err != nil {
		return err
	}
	if _, ok := s.Owners[ownerID]; !ok {
		return fmt.Errorf("owner not found")
	}
	if _, exists := s.Planned[node.NodeID]; exists {
		return fmt.Errorf("node already exists")
	}
	if len(s.Planned) >= maxNodes {
		return fmt.Errorf("max nodes reached")
	}
	ownerNodes := 0
	for _, planned := range s.Planned {
		if planned.OwnerID == ownerID {
			ownerNodes++
		}
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
	s.Planned[node.NodeID], s.Infos[node.NodeID] = node, info
	if err := s.appendAuditLocked(audit); err != nil {
		s.restoreSnapshotLocked(snapshot)
		return err
	}
	return s.commitLocked(snapshot)
}

func (s *Store) SetNodePrivacy(ownerID, nodeID string, private bool, audit AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, err := s.snapshotLocked()
	if err != nil {
		return err
	}
	planned, ok := s.Planned[strings.TrimSpace(nodeID)]
	if !ok || planned.OwnerID != strings.TrimSpace(ownerID) {
		return fmt.Errorf("node not found")
	}
	planned.Private = private
	s.Planned[planned.NodeID] = planned
	if err := s.appendAuditLocked(audit); err != nil {
		s.restoreSnapshotLocked(snapshot)
		return err
	}
	return s.commitLocked(snapshot)
}

func (s *Store) CreateOwnerListing(ownerID string, listing MarketListing, audit AuditEvent) error {
	ownerID, listing.NodeID = strings.TrimSpace(ownerID), strings.TrimSpace(listing.NodeID)
	if ownerID == "" || !validNodeID(listing.NodeID) {
		return fmt.Errorf("valid owner and node are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, err := s.snapshotLocked()
	if err != nil {
		return err
	}
	planned, ok := s.Planned[listing.NodeID]
	if !ok || planned.OwnerID != ownerID {
		return fmt.Errorf("node not found")
	}
	if _, exists := s.Listings[listing.NodeID]; exists {
		return fmt.Errorf("listing already exists")
	}
	now := time.Now().Unix()
	listing.OwnerID = ownerID
	if listing.CreatedAt == 0 {
		listing.CreatedAt = now
	}
	listing.UpdatedAt = now
	listing.ModerationStatus = normalizeModerationStatus(listing.ModerationStatus)
	s.Listings[listing.NodeID] = listing
	if err := s.appendAuditLocked(audit); err != nil {
		s.restoreSnapshotLocked(snapshot)
		return err
	}
	return s.commitLocked(snapshot)
}

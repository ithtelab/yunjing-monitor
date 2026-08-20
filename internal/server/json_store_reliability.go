package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const maxAuditEvents = 20000

func newOperationID(prefix string) (string, error) {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func normalizeAudit(event AuditEvent) (AuditEvent, error) {
	if event.ID == "" {
		id, err := newOperationID("evt")
		if err != nil {
			return AuditEvent{}, err
		}
		event.ID = id
	}
	if event.Timestamp == 0 {
		event.Timestamp = time.Now().Unix()
	}
	event.ActorType = strings.TrimSpace(event.ActorType)
	event.ActorID = strings.TrimSpace(event.ActorID)
	event.Action = strings.TrimSpace(event.Action)
	event.ObjectType = strings.TrimSpace(event.ObjectType)
	event.ObjectID = strings.TrimSpace(event.ObjectID)
	event.IP = strings.TrimSpace(event.IP)
	event.Summary = strings.TrimSpace(event.Summary)
	if event.ActorType == "" || event.Action == "" || event.ObjectType == "" || event.ObjectID == "" {
		return AuditEvent{}, fmt.Errorf("invalid audit event")
	}
	return event, nil
}

func (s *Store) snapshotLocked() ([]byte, error) {
	return json.Marshal(s)
}

func (s *Store) restoreSnapshotLocked(raw []byte) {
	var restored Store
	if json.Unmarshal(raw, &restored) == nil {
		s.Reports = restored.Reports
		s.Infos = restored.Infos
		s.Planned = restored.Planned
		s.Settings = restored.Settings
		s.Traffic = restored.Traffic
		s.Owners = restored.Owners
		s.Listings = restored.Listings
		s.Ads = restored.Ads
		s.AdLayout = restored.AdLayout
		s.Links = restored.Links
		s.Recycle = restored.Recycle
		s.Audit = restored.Audit
		if s.Recycle == nil {
			s.Recycle = map[string]RecycleRecord{}
		}
		if s.Audit == nil {
			s.Audit = []AuditEvent{}
		}
	}
}

func (s *Store) commitLocked(snapshot []byte) error {
	if err := s.saveLocked(); err != nil {
		s.restoreSnapshotLocked(snapshot)
		return err
	}
	return nil
}

func (s *Store) appendAuditLocked(event AuditEvent) error {
	event, err := normalizeAudit(event)
	if err != nil {
		return err
	}
	s.Audit = append(s.Audit, event)
	if len(s.Audit) > maxAuditEvents {
		s.Audit = append([]AuditEvent(nil), s.Audit[len(s.Audit)-maxAuditEvents:]...)
	}
	return nil
}

func (s *Store) CreateMarketSubmission(sub MarketSubmission, maxNodes int) error {
	encrypted, err := s.tokenCipher.encrypt(sub.AgentToken)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, err := s.snapshotLocked()
	if err != nil {
		return err
	}
	if planned, exists := s.Planned[sub.Node.NodeID]; exists {
		if planned.OwnerID != "" && planned.OwnerID != sub.Owner.ID {
			return fmt.Errorf("node owner conflict")
		}
	} else if len(s.Planned) >= maxNodes {
		return fmt.Errorf("max nodes reached")
	}
	if _, exists := s.Listings[sub.Node.NodeID]; exists {
		return fmt.Errorf("listing already exists")
	}
	if sub.NewOwner {
		for _, owner := range s.Owners {
			if owner.Email == sub.Owner.Email {
				return fmt.Errorf("email already registered")
			}
		}
		if sub.Owner.ID == "" || sub.Owner.Email == "" || sub.Owner.PasswordHash == "" {
			return fmt.Errorf("invalid owner credentials")
		}
		s.Owners[sub.Owner.ID] = sub.Owner
	} else if _, ok := s.Owners[sub.Owner.ID]; !ok {
		return fmt.Errorf("owner not found")
	}
	if sub.OwnerNodeLimit > 0 {
		ownerNodes := 0
		for _, planned := range s.Planned {
			if planned.OwnerID == sub.Owner.ID {
				ownerNodes++
			}
		}
		if ownerNodes >= sub.OwnerNodeLimit {
			return fmt.Errorf("owner node limit reached")
		}
	}
	sub.Node.OwnerID = sub.Owner.ID
	sub.Node.Token = encrypted
	sub.Node.TokenHash = hashToken(sub.AgentToken)
	s.Planned[sub.Node.NodeID] = sub.Node
	sub.Info.Name = sub.Node.NodeID
	s.Infos[sub.Node.NodeID] = sub.Info
	sub.Listing.NodeID, sub.Listing.OwnerID = sub.Node.NodeID, sub.Owner.ID
	sub.Listing.ModerationStatus = normalizeModerationStatus(sub.Listing.ModerationStatus)
	s.Listings[sub.Node.NodeID] = sub.Listing
	if err := s.appendAuditLocked(sub.Audit); err != nil {
		s.restoreSnapshotLocked(snapshot)
		return err
	}
	return s.commitLocked(snapshot)
}

func (s *Store) UpdateOwnerNode(ownerID string, mutation OwnerNodeMutation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, err := s.snapshotLocked()
	if err != nil {
		return err
	}
	planned, ok := s.Planned[mutation.NodeID]
	if !ok || planned.OwnerID != ownerID {
		return fmt.Errorf("node not found")
	}
	if listing, exists := s.Listings[mutation.NodeID]; exists {
		if listing.OwnerID != ownerID {
			return fmt.Errorf("node not found")
		}
		applyMarketListingUpdate(&listing, mutation.Listing)
		if ownerNodeMutationRequiresReview(mutation) {
			listing.ModerationStatus = "pending"
			listing.RejectionReason = ""
		}
		listing.UpdatedAt = time.Now().Unix()
		s.Listings[mutation.NodeID] = listing
	}
	info := s.Infos[mutation.NodeID]
	info.Name = mutation.NodeID
	if mutation.DisplayName != nil {
		planned.DisplayName, info.DisplayName = *mutation.DisplayName, *mutation.DisplayName
	}
	if mutation.Region != nil {
		planned.Region, info.Region = *mutation.Region, *mutation.Region
	}
	if mutation.RegionCode != nil {
		planned.RegionCode, info.RegionCode = *mutation.RegionCode, *mutation.RegionCode
	}
	if mutation.DueTime != nil {
		info.DueTime = *mutation.DueTime
	}
	if mutation.Billing != nil {
		info.Price = mutation.Billing.Price
		info.Cycle = mutation.Billing.LegacyCycle
		info.PriceAmount = mutation.Billing.PriceAmount
		info.PriceCurrency = mutation.Billing.PriceCurrency
		info.BillingCycle = mutation.Billing.BillingCycle
	}
	s.Planned[mutation.NodeID], s.Infos[mutation.NodeID] = planned, info
	if err := s.appendAuditLocked(mutation.Audit); err != nil {
		s.restoreSnapshotLocked(snapshot)
		return err
	}
	return s.commitLocked(snapshot)
}

func (s *Store) TrashNode(nodeID string, audit AuditEvent) (RecycleRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, err := s.snapshotLocked()
	if err != nil {
		return RecycleRecord{}, err
	}
	planned, hasPlanned := s.Planned[nodeID]
	report, hasReport := s.Reports[nodeID]
	info, hasInfo := s.Infos[nodeID]
	traffic, hasTraffic := s.Traffic[nodeID]
	listing, hasListing := s.Listings[nodeID]
	if !hasPlanned && !hasReport && !hasInfo && !hasListing {
		return RecycleRecord{}, fmt.Errorf("node not found")
	}
	id, err := newOperationID("trash")
	if err != nil {
		return RecycleRecord{}, err
	}
	recycled := &RecycledNode{Planned: planned}
	if hasInfo {
		recycled.Info = &info
	}
	if hasReport {
		recycled.Report = &report
	}
	if hasTraffic {
		recycled.Traffic = &traffic
	}
	if hasListing {
		recycled.Listing = &listing
	}
	record := RecycleRecord{ID: id, Kind: "node", NodeID: nodeID, DeletedAt: time.Now().Unix(), DeletedBy: audit.ActorType + ":" + audit.ActorID, Summary: audit.Summary, Node: recycled}
	s.Recycle[id] = record
	delete(s.Reports, nodeID)
	delete(s.Planned, nodeID)
	delete(s.Infos, nodeID)
	delete(s.Traffic, nodeID)
	delete(s.Listings, nodeID)
	if err := s.appendAuditLocked(audit); err != nil {
		s.restoreSnapshotLocked(snapshot)
		return RecycleRecord{}, err
	}
	if err := s.commitLocked(snapshot); err != nil {
		return RecycleRecord{}, err
	}
	return record, nil
}

func (s *Store) TrashMarketListing(nodeID string, audit AuditEvent) (RecycleRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, err := s.snapshotLocked()
	if err != nil {
		return RecycleRecord{}, err
	}
	listing, ok := s.Listings[nodeID]
	if !ok {
		return RecycleRecord{}, fmt.Errorf("listing not found")
	}
	id, err := newOperationID("trash")
	if err != nil {
		return RecycleRecord{}, err
	}
	record := RecycleRecord{ID: id, Kind: "market_listing", NodeID: nodeID, DeletedAt: time.Now().Unix(), DeletedBy: audit.ActorType + ":" + audit.ActorID, Summary: audit.Summary, Listing: &listing}
	s.Recycle[id] = record
	delete(s.Listings, nodeID)
	if err := s.appendAuditLocked(audit); err != nil {
		s.restoreSnapshotLocked(snapshot)
		return RecycleRecord{}, err
	}
	if err := s.commitLocked(snapshot); err != nil {
		return RecycleRecord{}, err
	}
	return record, nil
}

func (s *Store) ListRecycleRecords() []RecycleRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]RecycleRecord, 0, len(s.Recycle))
	for _, record := range s.Recycle {
		out = append(out, record)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeletedAt > out[j].DeletedAt })
	return out
}

func (s *Store) RestoreRecycleRecord(id string, audit AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, err := s.snapshotLocked()
	if err != nil {
		return err
	}
	record, ok := s.Recycle[id]
	if !ok {
		return fmt.Errorf("recycle record not found")
	}
	if record.Kind == "node" && record.Node != nil {
		if _, ok := s.Planned[record.NodeID]; ok {
			return fmt.Errorf("node already exists")
		}
		if _, ok := s.Listings[record.NodeID]; ok {
			return fmt.Errorf("listing already exists")
		}
		s.Planned[record.NodeID] = record.Node.Planned
		if record.Node.Info != nil {
			s.Infos[record.NodeID] = *record.Node.Info
		}
		if record.Node.Report != nil {
			s.Reports[record.NodeID] = *record.Node.Report
		}
		if record.Node.Traffic != nil {
			s.Traffic[record.NodeID] = *record.Node.Traffic
		}
		if record.Node.Listing != nil {
			listing := *record.Node.Listing
			listing.ModerationStatus = normalizeModerationStatus(listing.ModerationStatus)
			s.Listings[record.NodeID] = listing
		}
	} else if record.Kind == "market_listing" && record.Listing != nil {
		if _, ok := s.Listings[record.NodeID]; ok {
			return fmt.Errorf("listing already exists")
		}
		if _, ok := s.Planned[record.NodeID]; !ok {
			return fmt.Errorf("node no longer exists")
		}
		listing := *record.Listing
		listing.ModerationStatus = normalizeModerationStatus(listing.ModerationStatus)
		s.Listings[record.NodeID] = listing
	} else {
		return fmt.Errorf("invalid recycle record")
	}
	delete(s.Recycle, id)
	if err := s.appendAuditLocked(audit); err != nil {
		s.restoreSnapshotLocked(snapshot)
		return err
	}
	return s.commitLocked(snapshot)
}

func (s *Store) PurgeRecycleRecord(id string, audit AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, err := s.snapshotLocked()
	if err != nil {
		return err
	}
	if _, ok := s.Recycle[id]; !ok {
		return fmt.Errorf("recycle record not found")
	}
	delete(s.Recycle, id)
	if err := s.appendAuditLocked(audit); err != nil {
		s.restoreSnapshotLocked(snapshot)
		return err
	}
	return s.commitLocked(snapshot)
}

func (s *Store) AppendAudit(event AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, err := s.snapshotLocked()
	if err != nil {
		return err
	}
	if err := s.appendAuditLocked(event); err != nil {
		return err
	}
	return s.commitLocked(snapshot)
}

func (s *Store) ListAuditEvents(limit int) []AuditEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	start := len(s.Audit) - limit
	if start < 0 {
		start = 0
	}
	out := append([]AuditEvent(nil), s.Audit[start:]...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Timestamp > out[j].Timestamp })
	return out
}

package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"vps-agent/internal/agent"
)

func insertAuditTx(tx *sql.Tx, event AuditEvent) error {
	event, err := normalizeAudit(event)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO audit_log(id, actor_type, actor_id, action, object_type, object_id, timestamp, ip, summary) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, event.ID, event.ActorType, event.ActorID, event.Action, event.ObjectType, event.ObjectID, event.Timestamp, event.IP, event.Summary)
	return err
}

func upsertListingTx(tx *sql.Tx, listing MarketListing) error {
	_, err := tx.Exec(`INSERT INTO market_listings(node_id, owner_id, for_sale, listing_type, contact, description, specs, price, price_amount, price_currency, billing_cycle, pinned, created_at, updated_at, moderation_status, rejection_reason)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET owner_id=excluded.owner_id, for_sale=excluded.for_sale, listing_type=excluded.listing_type, contact=excluded.contact, description=excluded.description, specs=excluded.specs, price=excluded.price, price_amount=excluded.price_amount, price_currency=excluded.price_currency, billing_cycle=excluded.billing_cycle, pinned=excluded.pinned, created_at=excluded.created_at, updated_at=excluded.updated_at, moderation_status=excluded.moderation_status, rejection_reason=excluded.rejection_reason`,
		listing.NodeID, listing.OwnerID, boolToInt(listing.ForSale), listing.ListingType, listing.Contact, listing.Description, listing.Specs, listing.Price, listing.PriceAmount, listing.PriceCurrency, listing.BillingCycle, boolToInt(listing.Pinned), listing.CreatedAt, listing.UpdatedAt, normalizeModerationStatus(listing.ModerationStatus), listing.RejectionReason)
	return err
}

func getListingTx(tx *sql.Tx, nodeID string) (MarketListing, bool, error) {
	var listing MarketListing
	var forSale, pinned int
	err := tx.QueryRow(`SELECT node_id, owner_id, for_sale, listing_type, contact, description, specs, price, price_amount, price_currency, billing_cycle, pinned, created_at, updated_at, moderation_status, rejection_reason FROM market_listings WHERE node_id = ?`, nodeID).Scan(&listing.NodeID, &listing.OwnerID, &forSale, &listing.ListingType, &listing.Contact, &listing.Description, &listing.Specs, &listing.Price, &listing.PriceAmount, &listing.PriceCurrency, &listing.BillingCycle, &pinned, &listing.CreatedAt, &listing.UpdatedAt, &listing.ModerationStatus, &listing.RejectionReason)
	if errors.Is(err, sql.ErrNoRows) {
		return MarketListing{}, false, nil
	}
	if err != nil {
		return MarketListing{}, false, err
	}
	listing.ForSale, listing.Pinned = forSale != 0, pinned != 0
	listing.ModerationStatus = normalizeModerationStatus(listing.ModerationStatus)
	return listing, true, nil
}

func (s *SQLiteStore) CreateMarketSubmission(sub MarketSubmission, maxNodes int) error {
	encrypted, err := s.tokenCipher.encrypt(sub.AgentToken)
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
	if sub.NewOwner {
		if sub.Owner.ID == "" || sub.Owner.Email == "" || sub.Owner.PasswordHash == "" {
			return fmt.Errorf("invalid owner credentials")
		}
		if _, err := tx.Exec(`INSERT INTO owners(id,email,password_hash,created_at,last_login,disabled) VALUES(?,?,?,?,?,?)`, sub.Owner.ID, sub.Owner.Email, sub.Owner.PasswordHash, sub.Owner.CreatedAt, sub.Owner.LastLogin, boolToInt(sub.Owner.Disabled)); err != nil {
			if containsUniqueError(err) {
				return fmt.Errorf("email already registered")
			}
			return err
		}
	} else {
		var found string
		if err := tx.QueryRow(`SELECT id FROM owners WHERE id=?`, sub.Owner.ID).Scan(&found); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("owner not found")
			}
			return err
		}
	}
	if sub.OwnerNodeLimit > 0 {
		var ownerNodes int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM planned_nodes WHERE owner_id = ?`, sub.Owner.ID).Scan(&ownerNodes); err != nil {
			return err
		}
		if ownerNodes >= sub.OwnerNodeLimit {
			return fmt.Errorf("owner node limit reached")
		}
	}
	exists, err := plannedExistsTx(tx, sub.Node.NodeID)
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
	} else {
		planned, _, err := getPlannedTx(tx, sub.Node.NodeID)
		if err != nil {
			return err
		}
		if planned.OwnerID != "" && planned.OwnerID != sub.Owner.ID {
			return fmt.Errorf("node owner conflict")
		}
	}
	if _, exists, err := getListingTx(tx, sub.Node.NodeID); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("listing already exists")
	}
	sub.Node.OwnerID = sub.Owner.ID
	sub.Node.Token = encrypted
	sub.Node.TokenHash = hashToken(sub.AgentToken)
	if err := upsertPlannedTx(tx, sub.Node); err != nil {
		return err
	}
	sub.Info.Name = sub.Node.NodeID
	if err := upsertInfoTx(tx, sub.Info); err != nil {
		return err
	}
	sub.Listing.NodeID, sub.Listing.OwnerID = sub.Node.NodeID, sub.Owner.ID
	sub.Listing.ModerationStatus = normalizeModerationStatus(sub.Listing.ModerationStatus)
	if err := upsertListingTx(tx, sub.Listing); err != nil {
		return err
	}
	if err := insertAuditTx(tx, sub.Audit); err != nil {
		return err
	}
	return tx.Commit()
}

func containsUniqueError(err error) bool {
	if err == nil {
		return false
	}
	text := err.Error()
	return stringsContainsFold(text, "unique") || stringsContainsFold(text, "constraint")
}

func stringsContainsFold(value, needle string) bool {
	for i := 0; i+len(needle) <= len(value); i++ {
		if equalFoldASCII(value[i:i+len(needle)], needle) {
			return true
		}
	}
	return false
}
func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		ac, bc := a[i], b[i]
		if ac >= 'A' && ac <= 'Z' {
			ac += 32
		}
		if bc >= 'A' && bc <= 'Z' {
			bc += 32
		}
		if ac != bc {
			return false
		}
	}
	return true
}

func (s *SQLiteStore) UpdateOwnerNode(ownerID string, mutation OwnerNodeMutation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	planned, ok, err := getPlannedTx(tx, mutation.NodeID)
	if err != nil {
		return err
	}
	if !ok || planned.OwnerID != ownerID {
		return fmt.Errorf("node not found")
	}
	listing, hasListing, err := getListingTx(tx, mutation.NodeID)
	if err != nil {
		return err
	}
	if hasListing {
		if listing.OwnerID != ownerID {
			return fmt.Errorf("node not found")
		}
		applyMarketListingUpdate(&listing, mutation.Listing)
		if ownerNodeMutationRequiresReview(mutation) {
			listing.ModerationStatus = "pending"
			listing.RejectionReason = ""
		}
		listing.UpdatedAt = time.Now().Unix()
		if err := upsertListingTx(tx, listing); err != nil {
			return err
		}
	}
	info, _, err := getInfoTx(tx, mutation.NodeID)
	if err != nil {
		return err
	}
	info.Name = mutation.NodeID
	if mutation.DisplayName != nil {
		planned.DisplayName = *mutation.DisplayName
		info.DisplayName = *mutation.DisplayName
	}
	if mutation.Region != nil {
		planned.Region = *mutation.Region
		info.Region = *mutation.Region
	}
	if mutation.RegionCode != nil {
		planned.RegionCode = *mutation.RegionCode
		info.RegionCode = *mutation.RegionCode
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
	if err := upsertPlannedTx(tx, planned); err != nil {
		return err
	}
	if err := upsertInfoTx(tx, info); err != nil {
		return err
	}
	if err := insertAuditTx(tx, mutation.Audit); err != nil {
		return err
	}
	return tx.Commit()
}

func loadNodeRecycleTx(tx *sql.Tx, nodeID string) (*RecycledNode, bool, error) {
	planned, hasPlanned, err := getPlannedTx(tx, nodeID)
	if err != nil {
		return nil, false, err
	}
	info, hasInfo, err := getInfoTx(tx, nodeID)
	if err != nil {
		return nil, false, err
	}
	listing, hasListing, err := getListingTx(tx, nodeID)
	if err != nil {
		return nil, false, err
	}
	var report agent.Metrics
	var raw string
	err = tx.QueryRow(`SELECT metrics_json FROM reports WHERE node_id=?`, nodeID).Scan(&raw)
	hasReport := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	if hasReport {
		if err := json.Unmarshal([]byte(raw), &report); err != nil {
			return nil, false, err
		}
	}
	traffic, hasTraffic, err := getTrafficTx(tx, nodeID)
	if err != nil {
		return nil, false, err
	}
	if !hasPlanned && !hasInfo && !hasReport && !hasListing {
		return nil, false, nil
	}
	node := &RecycledNode{Planned: planned}
	if hasInfo {
		node.Info = &info
	}
	if hasReport {
		node.Report = &report
	}
	if hasTraffic {
		node.Traffic = &traffic
	}
	if hasListing {
		node.Listing = &listing
	}
	return node, true, nil
}

func insertRecycleTx(tx *sql.Tx, record RecycleRecord) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO recycle_bin(id,kind,node_id,deleted_at,deleted_by,summary,payload_json) VALUES(?,?,?,?,?,?,?)`, record.ID, record.Kind, record.NodeID, record.DeletedAt, record.DeletedBy, record.Summary, string(raw))
	return err
}

func (s *SQLiteStore) TrashNode(nodeID string, audit AuditEvent) (RecycleRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return RecycleRecord{}, err
	}
	defer tx.Rollback()
	node, ok, err := loadNodeRecycleTx(tx, nodeID)
	if err != nil {
		return RecycleRecord{}, err
	}
	if !ok {
		return RecycleRecord{}, fmt.Errorf("node not found")
	}
	id, err := newOperationID("trash")
	if err != nil {
		return RecycleRecord{}, err
	}
	record := RecycleRecord{ID: id, Kind: "node", NodeID: nodeID, DeletedAt: time.Now().Unix(), DeletedBy: audit.ActorType + ":" + audit.ActorID, Summary: audit.Summary, Node: node}
	if err := insertRecycleTx(tx, record); err != nil {
		return RecycleRecord{}, err
	}
	for _, q := range []string{`DELETE FROM reports WHERE node_id=?`, `DELETE FROM planned_nodes WHERE node_id=?`, `DELETE FROM host_infos WHERE node_id=?`, `DELETE FROM traffic_stats WHERE node_id=?`, `DELETE FROM market_listings WHERE node_id=?`} {
		if _, err := tx.Exec(q, nodeID); err != nil {
			return RecycleRecord{}, err
		}
	}
	if err := insertAuditTx(tx, audit); err != nil {
		return RecycleRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return RecycleRecord{}, err
	}
	return record, nil
}

func (s *SQLiteStore) TrashMarketListing(nodeID string, audit AuditEvent) (RecycleRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return RecycleRecord{}, err
	}
	defer tx.Rollback()
	listing, ok, err := getListingTx(tx, nodeID)
	if err != nil {
		return RecycleRecord{}, err
	}
	if !ok {
		return RecycleRecord{}, fmt.Errorf("listing not found")
	}
	id, err := newOperationID("trash")
	if err != nil {
		return RecycleRecord{}, err
	}
	record := RecycleRecord{ID: id, Kind: "market_listing", NodeID: nodeID, DeletedAt: time.Now().Unix(), DeletedBy: audit.ActorType + ":" + audit.ActorID, Summary: audit.Summary, Listing: &listing}
	if err := insertRecycleTx(tx, record); err != nil {
		return RecycleRecord{}, err
	}
	if _, err := tx.Exec(`DELETE FROM market_listings WHERE node_id=?`, nodeID); err != nil {
		return RecycleRecord{}, err
	}
	if err := insertAuditTx(tx, audit); err != nil {
		return RecycleRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return RecycleRecord{}, err
	}
	return record, nil
}

func scanRecycle(rows *sql.Rows) []RecycleRecord {
	out := []RecycleRecord{}
	for rows.Next() {
		var raw string
		var ignored RecycleRecord
		if rows.Scan(&ignored.ID, &ignored.Kind, &ignored.NodeID, &ignored.DeletedAt, &ignored.DeletedBy, &ignored.Summary, &raw) != nil {
			continue
		}
		var record RecycleRecord
		if json.Unmarshal([]byte(raw), &record) == nil {
			out = append(out, record)
		}
	}
	return out
}
func (s *SQLiteStore) ListRecycleRecords() []RecycleRecord {
	rows, err := s.db.Query(`SELECT id,kind,node_id,deleted_at,deleted_by,summary,payload_json FROM recycle_bin ORDER BY deleted_at DESC`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	return scanRecycle(rows)
}

func loadRecycleTx(tx *sql.Tx, id string) (RecycleRecord, error) {
	var raw string
	err := tx.QueryRow(`SELECT payload_json FROM recycle_bin WHERE id=?`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return RecycleRecord{}, fmt.Errorf("recycle record not found")
	}
	if err != nil {
		return RecycleRecord{}, err
	}
	var record RecycleRecord
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return RecycleRecord{}, err
	}
	return record, nil
}

func (s *SQLiteStore) RestoreRecycleRecord(id string, audit AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	record, err := loadRecycleTx(tx, id)
	if err != nil {
		return err
	}
	if record.Kind == "node" && record.Node != nil {
		if exists, err := plannedExistsTx(tx, record.NodeID); err != nil {
			return err
		} else if exists {
			return fmt.Errorf("node already exists")
		}
		if _, exists, err := getListingTx(tx, record.NodeID); err != nil {
			return err
		} else if exists {
			return fmt.Errorf("listing already exists")
		}
		if err := upsertPlannedTx(tx, record.Node.Planned); err != nil {
			return err
		}
		if record.Node.Info != nil {
			if err := upsertInfoTx(tx, *record.Node.Info); err != nil {
				return err
			}
		}
		if record.Node.Report != nil {
			if err := upsertReportTx(tx, *record.Node.Report); err != nil {
				return err
			}
		}
		if record.Node.Traffic != nil {
			if err := upsertTrafficTx(tx, record.NodeID, *record.Node.Traffic); err != nil {
				return err
			}
		}
		if record.Node.Listing != nil {
			if err := upsertListingTx(tx, *record.Node.Listing); err != nil {
				return err
			}
		}
	} else if record.Kind == "market_listing" && record.Listing != nil {
		if exists, err := plannedExistsTx(tx, record.NodeID); err != nil {
			return err
		} else if !exists {
			return fmt.Errorf("node no longer exists")
		}
		if _, exists, err := getListingTx(tx, record.NodeID); err != nil {
			return err
		} else if exists {
			return fmt.Errorf("listing already exists")
		}
		if err := upsertListingTx(tx, *record.Listing); err != nil {
			return err
		}
	} else {
		return fmt.Errorf("invalid recycle record")
	}
	if _, err := tx.Exec(`DELETE FROM recycle_bin WHERE id=?`, id); err != nil {
		return err
	}
	if err := insertAuditTx(tx, audit); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *SQLiteStore) PurgeRecycleRecord(id string, audit AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM recycle_bin WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("recycle record not found")
	}
	if err := insertAuditTx(tx, audit); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *SQLiteStore) AppendAudit(event AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := insertAuditTx(tx, event); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *SQLiteStore) ListAuditEvents(limit int) []AuditEvent {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.db.Query(`SELECT id,actor_type,actor_id,action,object_type,object_id,timestamp,ip,summary FROM audit_log ORDER BY timestamp DESC,id DESC LIMIT ?`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []AuditEvent{}
	for rows.Next() {
		var e AuditEvent
		if rows.Scan(&e.ID, &e.ActorType, &e.ActorID, &e.Action, &e.ObjectType, &e.ObjectID, &e.Timestamp, &e.IP, &e.Summary) == nil {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Timestamp > out[j].Timestamp })
	return out
}

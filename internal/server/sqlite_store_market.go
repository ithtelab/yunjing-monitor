package server

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func (s *SQLiteStore) CreateOwner(email, passwordHash string) (Owner, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	email = normalizeOwnerEmail(email)
	if email == "" || passwordHash == "" {
		return Owner{}, fmt.Errorf("invalid owner credentials")
	}
	var existing string
	err := s.db.QueryRow(`SELECT id FROM owners WHERE email = ?`, email).Scan(&existing)
	if err == nil {
		return Owner{}, fmt.Errorf("email already registered")
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Owner{}, err
	}
	id, err := newOwnerID()
	if err != nil {
		return Owner{}, err
	}
	now := time.Now().Unix()
	owner := Owner{
		ID:           id,
		Email:        email,
		PasswordHash: passwordHash,
		CreatedAt:    now,
		LastLogin:    now,
	}
	if _, err := s.db.Exec(`
		INSERT INTO owners(id, email, password_hash, created_at, last_login)
		VALUES (?, ?, ?, ?, ?)
	`, owner.ID, owner.Email, owner.PasswordHash, owner.CreatedAt, owner.LastLogin); err != nil {
		return Owner{}, err
	}
	return owner, nil
}

func (s *SQLiteStore) AuthenticateOwner(email, password string) (Owner, error) {
	email = normalizeOwnerEmail(email)
	if email == "" || password == "" {
		return Owner{}, fmt.Errorf("invalid credentials")
	}
	var owner Owner
	var disabled int
	err := s.db.QueryRow(`
		SELECT id, email, password_hash, created_at, last_login, disabled FROM owners WHERE email = ?
	`, email).Scan(&owner.ID, &owner.Email, &owner.PasswordHash, &owner.CreatedAt, &owner.LastLogin, &disabled)
	if errors.Is(err, sql.ErrNoRows) {
		return Owner{}, fmt.Errorf("invalid credentials")
	}
	if err != nil {
		return Owner{}, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(owner.PasswordHash), []byte(password)); err != nil {
		return Owner{}, fmt.Errorf("invalid credentials")
	}
	owner.Disabled = disabled != 0
	if owner.Disabled {
		return Owner{}, fmt.Errorf("owner disabled")
	}
	owner.LastLogin = time.Now().Unix()
	if err := s.TouchOwnerLogin(owner.ID); err != nil {
		return Owner{}, err
	}
	return owner, nil
}

func (s *SQLiteStore) GetOwnerByID(id string) (Owner, bool) {
	var owner Owner
	var disabled int
	err := s.db.QueryRow(`
		SELECT id, email, password_hash, created_at, last_login, disabled FROM owners WHERE id = ?
	`, id).Scan(&owner.ID, &owner.Email, &owner.PasswordHash, &owner.CreatedAt, &owner.LastLogin, &disabled)
	if errors.Is(err, sql.ErrNoRows) {
		return Owner{}, false
	}
	if err != nil {
		return Owner{}, false
	}
	owner.Disabled = disabled != 0
	return owner, true
}

func (s *SQLiteStore) ListOwners(query string, limit, offset int) ([]Owner, int) {
	query = strings.ToLower(strings.TrimSpace(query))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	pattern := "%" + query + "%"
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM owners WHERE lower(email) LIKE ? OR lower(id) LIKE ?`, pattern, pattern).Scan(&total); err != nil {
		return nil, 0
	}
	rows, err := s.db.Query(`SELECT id,email,password_hash,created_at,last_login,disabled FROM owners WHERE lower(email) LIKE ? OR lower(id) LIKE ? ORDER BY created_at DESC,id ASC LIMIT ? OFFSET ?`, pattern, pattern, limit, offset)
	if err != nil {
		return nil, 0
	}
	defer rows.Close()
	out := []Owner{}
	for rows.Next() {
		var owner Owner
		var disabled int
		if rows.Scan(&owner.ID, &owner.Email, &owner.PasswordHash, &owner.CreatedAt, &owner.LastLogin, &disabled) == nil {
			owner.Disabled = disabled != 0
			out = append(out, owner)
		}
	}
	return out, total
}

func (s *SQLiteStore) UpdateOwnerAdmin(mutation OwnerAdminMutation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRow(`SELECT 1 FROM owners WHERE id=?`, mutation.OwnerID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("owner not found")
	} else if err != nil {
		return err
	}
	if mutation.Disabled != nil {
		if _, err := tx.Exec(`UPDATE owners SET disabled=? WHERE id=?`, boolToInt(*mutation.Disabled), mutation.OwnerID); err != nil {
			return err
		}
	}
	if mutation.PasswordHash != nil {
		if _, err := tx.Exec(`UPDATE owners SET password_hash=? WHERE id=?`, *mutation.PasswordHash, mutation.OwnerID); err != nil {
			return err
		}
	}
	if err := insertAuditTx(tx, mutation.Audit); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) TouchOwnerLogin(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`UPDATE owners SET last_login = ? WHERE id = ?`, time.Now().Unix(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("owner not found")
	}
	return nil
}

func (s *SQLiteStore) CreateMarketListing(listing MarketListing) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	listing.NodeID = strings.TrimSpace(listing.NodeID)
	listing.OwnerID = strings.TrimSpace(listing.OwnerID)
	if listing.NodeID == "" || listing.OwnerID == "" {
		return fmt.Errorf("node_id and owner_id required")
	}
	now := time.Now().Unix()
	if listing.CreatedAt == 0 {
		listing.CreatedAt = now
	}
	listing.UpdatedAt = now
	listing.ModerationStatus = normalizeModerationStatus(listing.ModerationStatus)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	planned, exists, err := getPlannedTx(tx, listing.NodeID)
	if err != nil {
		return err
	}
	if exists && planned.OwnerID != "" && planned.OwnerID != listing.OwnerID {
		return fmt.Errorf("node owner conflict")
	}
	planned.NodeID = listing.NodeID
	planned.OwnerID = listing.OwnerID
	if planned.CreatedAt == 0 {
		planned.CreatedAt = listing.CreatedAt
	}
	if err := upsertPlannedTx(tx, planned); err != nil {
		return err
	}
	_, err = tx.Exec(`
		INSERT INTO market_listings(
			node_id, owner_id, for_sale, listing_type, contact, description, specs, price, price_amount, price_currency, billing_cycle, pinned, created_at, updated_at, moderation_status, rejection_reason
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, listing.NodeID, listing.OwnerID, boolToInt(listing.ForSale), listing.ListingType, listing.Contact,
		listing.Description, listing.Specs, listing.Price, listing.PriceAmount, listing.PriceCurrency, listing.BillingCycle,
		boolToInt(listing.Pinned), listing.CreatedAt, listing.UpdatedAt, listing.ModerationStatus, listing.RejectionReason)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return fmt.Errorf("listing already exists")
		}
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) UpdateMarketListing(nodeID string, update MarketListingUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	listing, ok, err := s.loadListingByID(nodeID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("listing not found")
	}
	applyMarketListingUpdate(&listing, update)
	listing.UpdatedAt = time.Now().Unix()
	return s.upsertListing(listing)
}

func (s *SQLiteStore) GetMarketListing(nodeID string) (MarketListing, bool) {
	listing, ok, err := s.loadListingByID(nodeID)
	if err != nil || !ok {
		return MarketListing{}, false
	}
	return listing, true
}

func (s *SQLiteStore) ListMarketListings(forSaleOnly bool) []MarketListing {
	query := `
		SELECT node_id, owner_id, for_sale, listing_type, contact, description, specs, price, price_amount, price_currency, billing_cycle, pinned, created_at, updated_at, moderation_status, rejection_reason
		FROM market_listings
	`
	if forSaleOnly {
		query += ` WHERE for_sale = 1 AND moderation_status = 'approved'`
	}
	query += ` ORDER BY pinned DESC, updated_at DESC, node_id ASC`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil
	}
	defer rows.Close()
	return scanListings(rows)
}

func (s *SQLiteStore) ListOwnerListings(ownerID string) []MarketListing {
	rows, err := s.db.Query(`
		SELECT node_id, owner_id, for_sale, listing_type, contact, description, specs, price, price_amount, price_currency, billing_cycle, pinned, created_at, updated_at, moderation_status, rejection_reason
		FROM market_listings
		WHERE owner_id = ?
		ORDER BY pinned DESC, updated_at DESC, node_id ASC
	`, ownerID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	return scanListings(rows)
}

func (s *SQLiteStore) SetListingForSale(nodeID string, forSale bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`UPDATE market_listings SET for_sale = ?, updated_at = ? WHERE node_id = ?`, boolToInt(forSale), time.Now().Unix(), nodeID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("listing not found")
	}
	return nil
}

func (s *SQLiteStore) SetListingPinned(nodeID string, pinned bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`UPDATE market_listings SET pinned = ?, updated_at = ? WHERE node_id = ?`, boolToInt(pinned), time.Now().Unix(), nodeID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("listing not found")
	}
	return nil
}

func (s *SQLiteStore) ModerateMarketListing(mutation ListingModerationMutation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	status := normalizeModerationStatus(mutation.Status)
	reason := strings.TrimSpace(mutation.RejectionReason)
	if status != "rejected" {
		reason = ""
	}
	res, err := tx.Exec(`UPDATE market_listings SET moderation_status=?, rejection_reason=?, updated_at=? WHERE node_id=?`, status, reason, time.Now().Unix(), mutation.NodeID)
	if err != nil {
		return err
	}
	if count, _ := res.RowsAffected(); count == 0 {
		return fmt.Errorf("listing not found")
	}
	if err := insertAuditTx(tx, mutation.Audit); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) DeleteMarketListing(nodeID string) error {
	_, err := s.TrashMarketListing(nodeID, AuditEvent{ActorType: "system", ActorID: "compat", Action: "listing.trash", ObjectType: "market_listing", ObjectID: nodeID, Summary: "Listing moved to recycle bin"})
	return err
}

func (s *SQLiteStore) loadListingByID(nodeID string) (MarketListing, bool, error) {
	var listing MarketListing
	var forSale, pinned int
	err := s.db.QueryRow(`
		SELECT node_id, owner_id, for_sale, listing_type, contact, description, specs, price, price_amount, price_currency, billing_cycle, pinned, created_at, updated_at, moderation_status, rejection_reason
		FROM market_listings WHERE node_id = ?
	`, nodeID).Scan(
		&listing.NodeID, &listing.OwnerID, &forSale, &listing.ListingType, &listing.Contact,
		&listing.Description, &listing.Specs, &listing.Price, &listing.PriceAmount, &listing.PriceCurrency, &listing.BillingCycle,
		&pinned, &listing.CreatedAt, &listing.UpdatedAt, &listing.ModerationStatus, &listing.RejectionReason,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return MarketListing{}, false, nil
	}
	if err != nil {
		return MarketListing{}, false, err
	}
	listing.ForSale = forSale != 0
	listing.Pinned = pinned != 0
	listing.ModerationStatus = normalizeModerationStatus(listing.ModerationStatus)
	return listing, true, nil
}

func (s *SQLiteStore) upsertListing(listing MarketListing) error {
	_, err := s.db.Exec(`
		INSERT INTO market_listings(
			node_id, owner_id, for_sale, listing_type, contact, description, specs, price, price_amount, price_currency, billing_cycle, pinned, created_at, updated_at, moderation_status, rejection_reason
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET
			owner_id = excluded.owner_id,
			for_sale = excluded.for_sale,
			listing_type = excluded.listing_type,
			contact = excluded.contact,
			description = excluded.description,
			specs = excluded.specs,
			price = excluded.price,
			price_amount = excluded.price_amount,
			price_currency = excluded.price_currency,
			billing_cycle = excluded.billing_cycle,
			pinned = excluded.pinned,
			created_at = excluded.created_at,
			updated_at = excluded.updated_at,
			moderation_status = excluded.moderation_status,
			rejection_reason = excluded.rejection_reason
	`, listing.NodeID, listing.OwnerID, boolToInt(listing.ForSale), listing.ListingType, listing.Contact,
		listing.Description, listing.Specs, listing.Price, listing.PriceAmount, listing.PriceCurrency, listing.BillingCycle,
		boolToInt(listing.Pinned), listing.CreatedAt, listing.UpdatedAt, normalizeModerationStatus(listing.ModerationStatus), listing.RejectionReason)
	return err
}

func scanListings(rows *sql.Rows) []MarketListing {
	out := make([]MarketListing, 0)
	for rows.Next() {
		var listing MarketListing
		var forSale, pinned int
		if err := rows.Scan(
			&listing.NodeID, &listing.OwnerID, &forSale, &listing.ListingType, &listing.Contact,
			&listing.Description, &listing.Specs, &listing.Price, &listing.PriceAmount, &listing.PriceCurrency, &listing.BillingCycle,
			&pinned, &listing.CreatedAt, &listing.UpdatedAt, &listing.ModerationStatus, &listing.RejectionReason,
		); err != nil {
			continue
		}
		listing.ForSale = forSale != 0
		listing.Pinned = pinned != 0
		listing.ModerationStatus = normalizeModerationStatus(listing.ModerationStatus)
		out = append(out, listing)
	}
	return out
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

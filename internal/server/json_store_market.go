package server

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func (s *Store) CreateOwner(email, passwordHash string) (Owner, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	email = normalizeOwnerEmail(email)
	if email == "" || passwordHash == "" {
		return Owner{}, fmt.Errorf("invalid owner credentials")
	}
	if s.Owners == nil {
		s.Owners = map[string]Owner{}
	}
	for _, owner := range s.Owners {
		if owner.Email == email {
			return Owner{}, fmt.Errorf("email already registered")
		}
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
	s.Owners[id] = owner
	if err := s.saveLocked(); err != nil {
		return Owner{}, err
	}
	return owner, nil
}

func (s *Store) AuthenticateOwner(email, password string) (Owner, error) {
	email = normalizeOwnerEmail(email)
	if email == "" || password == "" {
		return Owner{}, fmt.Errorf("invalid credentials")
	}
	var found Owner
	var ok bool
	s.mu.RLock()
	for _, owner := range s.Owners {
		if owner.Email == email {
			found = owner
			ok = true
			break
		}
	}
	s.mu.RUnlock()
	if !ok {
		return Owner{}, fmt.Errorf("invalid credentials")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(found.PasswordHash), []byte(password)); err != nil {
		return Owner{}, fmt.Errorf("invalid credentials")
	}
	if found.Disabled {
		return Owner{}, fmt.Errorf("owner disabled")
	}
	if err := s.TouchOwnerLogin(found.ID); err != nil {
		return Owner{}, err
	}
	found.LastLogin = time.Now().Unix()
	return found, nil
}

func (s *Store) GetOwnerByID(id string) (Owner, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	owner, ok := s.Owners[id]
	return owner, ok
}

func (s *Store) ListOwners(query string, limit, offset int) ([]Owner, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	query = strings.ToLower(strings.TrimSpace(query))
	all := make([]Owner, 0, len(s.Owners))
	for _, owner := range s.Owners {
		if query != "" && !strings.Contains(strings.ToLower(owner.Email+" "+owner.ID), query) {
			continue
		}
		all = append(all, owner)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].CreatedAt != all[j].CreatedAt {
			return all[i].CreatedAt > all[j].CreatedAt
		}
		return all[i].ID < all[j].ID
	})
	total := len(all)
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset >= total {
		return []Owner{}, total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return append([]Owner(nil), all[offset:end]...), total
}

func (s *Store) UpdateOwnerAdmin(mutation OwnerAdminMutation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, err := s.snapshotLocked()
	if err != nil {
		return err
	}
	owner, ok := s.Owners[mutation.OwnerID]
	if !ok {
		return fmt.Errorf("owner not found")
	}
	if mutation.Disabled != nil {
		owner.Disabled = *mutation.Disabled
	}
	if mutation.PasswordHash != nil {
		owner.PasswordHash = *mutation.PasswordHash
	}
	s.Owners[mutation.OwnerID] = owner
	if err := s.appendAuditLocked(mutation.Audit); err != nil {
		s.restoreSnapshotLocked(snapshot)
		return err
	}
	return s.commitLocked(snapshot)
}

func (s *Store) TouchOwnerLogin(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	owner, ok := s.Owners[id]
	if !ok {
		return fmt.Errorf("owner not found")
	}
	owner.LastLogin = time.Now().Unix()
	s.Owners[id] = owner
	return s.saveLocked()
}

func (s *Store) CreateMarketListing(listing MarketListing) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	listing.NodeID = strings.TrimSpace(listing.NodeID)
	listing.OwnerID = strings.TrimSpace(listing.OwnerID)
	if listing.NodeID == "" || listing.OwnerID == "" {
		return fmt.Errorf("node_id and owner_id required")
	}
	if s.Listings == nil {
		s.Listings = map[string]MarketListing{}
	}
	if _, exists := s.Listings[listing.NodeID]; exists {
		return fmt.Errorf("listing already exists")
	}
	snapshot, err := s.snapshotLocked()
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	if listing.CreatedAt == 0 {
		listing.CreatedAt = now
	}
	listing.UpdatedAt = now
	listing.ModerationStatus = normalizeModerationStatus(listing.ModerationStatus)
	planned, exists := s.Planned[listing.NodeID]
	if exists && planned.OwnerID != "" && planned.OwnerID != listing.OwnerID {
		return fmt.Errorf("node owner conflict")
	}
	planned.NodeID = listing.NodeID
	planned.OwnerID = listing.OwnerID
	if planned.CreatedAt == 0 {
		planned.CreatedAt = listing.CreatedAt
	}
	s.Planned[listing.NodeID] = planned
	s.Listings[listing.NodeID] = listing
	return s.commitLocked(snapshot)
}

func (s *Store) UpdateMarketListing(nodeID string, update MarketListingUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	listing, ok := s.Listings[nodeID]
	if !ok {
		return fmt.Errorf("listing not found")
	}
	applyMarketListingUpdate(&listing, update)
	listing.UpdatedAt = time.Now().Unix()
	s.Listings[nodeID] = listing
	return s.saveLocked()
}

func (s *Store) GetMarketListing(nodeID string) (MarketListing, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	listing, ok := s.Listings[nodeID]
	listing.ModerationStatus = normalizeModerationStatus(listing.ModerationStatus)
	return listing, ok
}

func (s *Store) ListMarketListings(forSaleOnly bool) []MarketListing {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]MarketListing, 0, len(s.Listings))
	for _, listing := range s.Listings {
		listing.ModerationStatus = normalizeModerationStatus(listing.ModerationStatus)
		if forSaleOnly && (!listing.ForSale || listing.ModerationStatus != "approved") {
			continue
		}
		out = append(out, listing)
	}
	sortMarketListings(out)
	return out
}

func (s *Store) ModerateMarketListing(mutation ListingModerationMutation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, err := s.snapshotLocked()
	if err != nil {
		return err
	}
	listing, ok := s.Listings[mutation.NodeID]
	if !ok {
		return fmt.Errorf("listing not found")
	}
	listing.ModerationStatus = normalizeModerationStatus(mutation.Status)
	listing.RejectionReason = strings.TrimSpace(mutation.RejectionReason)
	if listing.ModerationStatus != "rejected" {
		listing.RejectionReason = ""
	}
	listing.UpdatedAt = time.Now().Unix()
	s.Listings[mutation.NodeID] = listing
	if err := s.appendAuditLocked(mutation.Audit); err != nil {
		s.restoreSnapshotLocked(snapshot)
		return err
	}
	return s.commitLocked(snapshot)
}

func normalizeModerationStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "pending":
		return "pending"
	case "rejected":
		return "rejected"
	default:
		return "approved"
	}
}

func (s *Store) ListOwnerListings(ownerID string) []MarketListing {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]MarketListing, 0)
	for _, listing := range s.Listings {
		if listing.OwnerID == ownerID {
			listing.ModerationStatus = normalizeModerationStatus(listing.ModerationStatus)
			out = append(out, listing)
		}
	}
	sortMarketListings(out)
	return out
}

func (s *Store) SetListingForSale(nodeID string, forSale bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	listing, ok := s.Listings[nodeID]
	if !ok {
		return fmt.Errorf("listing not found")
	}
	listing.ForSale = forSale
	listing.UpdatedAt = time.Now().Unix()
	s.Listings[nodeID] = listing
	return s.saveLocked()
}

func (s *Store) SetListingPinned(nodeID string, pinned bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	listing, ok := s.Listings[nodeID]
	if !ok {
		return fmt.Errorf("listing not found")
	}
	listing.Pinned = pinned
	listing.UpdatedAt = time.Now().Unix()
	s.Listings[nodeID] = listing
	return s.saveLocked()
}

func (s *Store) DeleteMarketListing(nodeID string) error {
	_, err := s.TrashMarketListing(nodeID, AuditEvent{ActorType: "system", ActorID: "compat", Action: "listing.trash", ObjectType: "market_listing", ObjectID: nodeID, Summary: "Listing moved to recycle bin"})
	return err
}

func applyMarketListingUpdate(listing *MarketListing, update MarketListingUpdate) {
	if update.ListingType != nil {
		listing.ListingType = strings.TrimSpace(*update.ListingType)
	}
	if update.Contact != nil {
		listing.Contact = strings.TrimSpace(*update.Contact)
	}
	if update.Description != nil {
		listing.Description = strings.TrimSpace(*update.Description)
	}
	if update.Specs != nil {
		listing.Specs = strings.TrimSpace(*update.Specs)
	}
	if update.Price != nil {
		listing.Price = strings.TrimSpace(*update.Price)
	}
	if update.PriceAmount != nil {
		listing.PriceAmount = *update.PriceAmount
	}
	if update.PriceCurrency != nil {
		listing.PriceCurrency = strings.TrimSpace(*update.PriceCurrency)
	}
	if update.BillingCycle != nil {
		listing.BillingCycle = strings.TrimSpace(*update.BillingCycle)
	}
	if update.ForSale != nil {
		listing.ForSale = *update.ForSale
	}
}

func ownerNodeMutationRequiresReview(mutation OwnerNodeMutation) bool {
	update := mutation.Listing
	return mutation.DisplayName != nil || mutation.Region != nil || mutation.DueTime != nil ||
		update.ListingType != nil || update.Contact != nil || update.Description != nil || update.Specs != nil ||
		update.Price != nil || update.PriceAmount != nil || update.PriceCurrency != nil || update.BillingCycle != nil
}

func sortMarketListings(out []MarketListing) {
	sort.Slice(out, func(i, j int) bool {
		if out[i].Pinned != out[j].Pinned {
			return out[i].Pinned
		}
		if out[i].UpdatedAt != out[j].UpdatedAt {
			return out[i].UpdatedAt > out[j].UpdatedAt
		}
		return out[i].NodeID < out[j].NodeID
	})
}

func normalizeOwnerEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func newOwnerID() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return "o_" + base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

func hashOwnerPassword(password string) (string, error) {
	sum, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(sum), nil
}

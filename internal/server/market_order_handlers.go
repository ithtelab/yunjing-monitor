package server

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

const marketOrderLifetime = 7 * 24 * time.Hour

func (s *Server) handleAccountOrders(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.ownerAuthorized(r)
	if !ok {
		http.Error(w, "account login required", http.StatusUnauthorized)
		return
	}
	if s.features == nil {
		http.Error(w, "market orders unavailable", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.expireMarketOrderIntents()
		s.features.mu.RLock()
		buying := make([]MarketOrderIntent, 0)
		selling := make([]MarketOrderIntent, 0)
		for _, item := range s.features.data.MarketOrderIntents {
			if item.BuyerOwnerID == owner.ID {
				buying = append(buying, item)
			}
			if item.SellerOwnerID == owner.ID {
				selling = append(selling, item)
			}
		}
		s.features.mu.RUnlock()
		sort.Slice(buying, func(i, j int) bool { return buying[i].UpdatedAt > buying[j].UpdatedAt })
		sort.Slice(selling, func(i, j int) bool { return selling[i].UpdatedAt > selling[j].UpdatedAt })
		writeJSON(w, map[string]any{"buying": buying, "selling": selling})
	case http.MethodPost:
		s.createMarketOrderIntent(w, r, owner)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) createMarketOrderIntent(w http.ResponseWriter, r *http.Request, buyer Owner) {
	if !s.store.GetSettings().MarketEnabled {
		http.Error(w, "market is currently disabled", http.StatusServiceUnavailable)
		return
	}
	if !s.validAdminOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return
	}
	defer r.Body.Close()
	var req struct {
		ListingNodeID string `json:"listing_node_id"`
		BuyerContact  string `json:"buyer_contact"`
		Message       string `json:"message"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 32<<10)).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	req.ListingNodeID = strings.TrimSpace(req.ListingNodeID)
	req.BuyerContact = cleanFeatureText(req.BuyerContact, 120)
	req.Message = cleanFeatureMessage(req.Message, 500)
	if !validNodeID(req.ListingNodeID) || len([]rune(req.BuyerContact)) < 3 {
		http.Error(w, "listing and buyer contact are required", http.StatusBadRequest)
		return
	}
	listing, ok := s.store.GetMarketListing(req.ListingNodeID)
	if !ok || !listing.ForSale || normalizeModerationStatus(listing.ModerationStatus) != "approved" {
		http.Error(w, "listing not found", http.StatusNotFound)
		return
	}
	if listing.OwnerID == buyer.ID {
		http.Error(w, "cannot create an order for your own listing", http.StatusConflict)
		return
	}
	now := time.Now().Unix()
	s.features.mu.Lock()
	activeCreated := 0
	for _, item := range s.features.data.MarketOrderIntents {
		if item.BuyerOwnerID == buyer.ID && now-item.CreatedAt < 60*60 {
			activeCreated++
		}
		if item.BuyerOwnerID == buyer.ID && item.ListingNodeID == req.ListingNodeID && (item.Status == "pending" || item.Status == "accepted") {
			s.features.mu.Unlock()
			http.Error(w, "an active order already exists for this listing", http.StatusConflict)
			return
		}
	}
	if activeCreated >= 5 {
		s.features.mu.Unlock()
		http.Error(w, "too many order requests", http.StatusTooManyRequests)
		return
	}
	id, err := newFeatureID("order")
	item := MarketOrderIntent{
		ID: id, ListingNodeID: req.ListingNodeID, BuyerOwnerID: buyer.ID, SellerOwnerID: listing.OwnerID,
		BuyerContact: req.BuyerContact, Message: req.Message, Status: "pending",
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now + int64(marketOrderLifetime/time.Second),
	}
	if err == nil {
		s.features.data.MarketOrderIntents = append(s.features.data.MarketOrderIntents, item)
		if len(s.features.data.MarketOrderIntents) > 2000 {
			s.features.data.MarketOrderIntents = append([]MarketOrderIntent(nil), s.features.data.MarketOrderIntents[len(s.features.data.MarketOrderIntents)-2000:]...)
		}
		err = s.features.saveLocked()
	}
	s.features.mu.Unlock()
	if err != nil {
		http.Error(w, "save market order", http.StatusInternalServerError)
		return
	}
	go s.dispatchPlatformEvent("market.order.created", marketOrderEvent(item, listing, item.SellerOwnerID, "seller"))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]any{"ok": true, "order": item})
}

func (s *Server) handleAccountOrderAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	owner, ok := s.ownerAuthorized(r)
	if !ok {
		http.Error(w, "account login required", http.StatusUnauthorized)
		return
	}
	if s.features == nil {
		http.Error(w, "market orders unavailable", http.StatusServiceUnavailable)
		return
	}
	if !s.validAdminOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return
	}
	defer r.Body.Close()
	var req struct {
		OrderID string `json:"order_id"`
		Action  string `json:"action"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req) != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	req.OrderID = strings.TrimSpace(req.OrderID)
	req.Action = strings.ToLower(strings.TrimSpace(req.Action))
	if !validateFeatureID(req.OrderID) {
		http.Error(w, "invalid order", http.StatusBadRequest)
		return
	}
	now := time.Now().Unix()
	s.features.mu.Lock()
	index := -1
	for i := range s.features.data.MarketOrderIntents {
		if s.features.data.MarketOrderIntents[i].ID == req.OrderID {
			index = i
			break
		}
	}
	if index < 0 {
		s.features.mu.Unlock()
		http.Error(w, "order not found", http.StatusNotFound)
		return
	}
	item := s.features.data.MarketOrderIntents[index]
	allowed := false
	switch req.Action {
	case "accept":
		allowed = owner.ID == item.SellerOwnerID && item.Status == "pending" && item.ExpiresAt > now
		if allowed {
			item.Status = "accepted"
		}
	case "complete":
		allowed = owner.ID == item.SellerOwnerID && item.Status == "accepted"
		if allowed {
			item.Status = "completed"
		}
	case "cancel":
		allowed = owner.ID == item.BuyerOwnerID && (item.Status == "pending" || item.Status == "accepted")
		if allowed {
			item.Status = "cancelled"
		}
	}
	if !allowed {
		s.features.mu.Unlock()
		http.Error(w, "order action is not allowed", http.StatusConflict)
		return
	}
	item.UpdatedAt = now
	s.features.data.MarketOrderIntents[index] = item
	err := s.features.saveLocked()
	s.features.mu.Unlock()
	if err != nil {
		http.Error(w, "save market order", http.StatusInternalServerError)
		return
	}
	listing, _ := s.store.GetMarketListing(item.ListingNodeID)
	event := "market.order." + item.Status
	recipient, role := item.BuyerOwnerID, "buyer"
	if item.Status == "cancelled" {
		recipient, role = item.SellerOwnerID, "seller"
	}
	go s.dispatchPlatformEvent(event, marketOrderEvent(item, listing, recipient, role))
	writeJSON(w, map[string]any{"ok": true, "order": item})
}

func (s *Server) expireMarketOrderIntents() {
	if s.features == nil {
		return
	}
	now := time.Now().Unix()
	expired := make([]MarketOrderIntent, 0)
	s.features.mu.Lock()
	for i := range s.features.data.MarketOrderIntents {
		item := &s.features.data.MarketOrderIntents[i]
		if (item.Status == "pending" || item.Status == "accepted") && item.ExpiresAt > 0 && item.ExpiresAt <= now {
			item.Status, item.UpdatedAt = "expired", now
			expired = append(expired, *item)
		}
	}
	if len(expired) > 0 {
		_ = s.features.saveLocked()
	}
	s.features.mu.Unlock()
	for _, item := range expired {
		listing, _ := s.store.GetMarketListing(item.ListingNodeID)
		go s.dispatchPlatformEvent("market.order.expired", marketOrderEvent(item, listing, item.BuyerOwnerID, "buyer"))
		go s.dispatchPlatformEvent("market.order.expired", marketOrderEvent(item, listing, item.SellerOwnerID, "seller"))
	}
}

func marketOrderEvent(item MarketOrderIntent, listing MarketListing, ownerID, role string) map[string]any {
	return map[string]any{
		"owner_id": ownerID, "recipient_role": role, "order_id": item.ID,
		"node_id": item.ListingNodeID, "message": item.Message, "status": item.Status,
		"buyer_contact": item.BuyerContact, "listing_type": listing.ListingType,
		"price": listing.Price, "created_at": item.CreatedAt, "expires_at": item.ExpiresAt,
	}
}

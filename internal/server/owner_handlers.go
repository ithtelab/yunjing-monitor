package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

func ownerCookie(r *http.Request, value string, maxAge time.Duration, persistent bool) *http.Cookie {
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	cookie := &http.Cookie{
		Name:     "monitor_owner",
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
	if maxAge < 0 {
		cookie.MaxAge = -1
		cookie.Expires = time.Unix(1, 0)
	} else if persistent {
		cookie.MaxAge = int(maxAge.Seconds())
		cookie.Expires = time.Now().Add(maxAge)
	}
	return cookie
}

func (s *Server) ownerAuthorized(r *http.Request) (Owner, bool) {
	cookie, err := r.Cookie("monitor_owner")
	if err != nil || cookie.Value == "" {
		return Owner{}, false
	}
	sess, ok := s.sessions.Get(cookie.Value)
	if !ok || sess.Kind != SessionKindOwner || sess.Subject == "" {
		return Owner{}, false
	}
	owner, ok := s.store.GetOwnerByID(sess.Subject)
	if !ok || owner.Disabled {
		return Owner{}, false
	}
	return owner, true
}

func (s *Server) handleOwnerLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.validAdminOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return
	}
	var req struct {
		Email      string `json:"email"`
		Password   string `json:"password"`
		RememberMe bool   `json:"remember_me"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	limitIPKey := loginLimitKey(requestClientIP(r), "")
	limitIdentityKey := loginLimitKey(requestClientIP(r), req.Email)
	if !s.ownerLoginLimiter.Allowed(limitIPKey) || !s.ownerLoginLimiter.Allowed(limitIdentityKey) {
		http.Error(w, "too many login attempts", http.StatusTooManyRequests)
		return
	}
	owner, err := s.store.AuthenticateOwner(req.Email, req.Password)
	if err != nil {
		s.ownerLoginLimiter.Failure(limitIPKey)
		s.ownerLoginLimiter.Failure(limitIdentityKey)
		time.Sleep(300 * time.Millisecond)
		if strings.Contains(err.Error(), "disabled") {
			http.Error(w, "owner account disabled", http.StatusForbidden)
		} else {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
		}
		return
	}
	s.ownerLoginLimiter.Success(limitIPKey)
	s.ownerLoginLimiter.Success(limitIdentityKey)
	ttl := defaultSessionTTL
	if req.RememberMe {
		ttl = rememberedSessionTTL
	}
	token, err := s.sessions.CreateForDuration(SessionKindOwner, owner.ID, ttl)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, ownerCookie(r, token, ttl, req.RememberMe))
	go s.dispatchPlatformEvent("account.login", map[string]any{"owner_id": owner.ID, "created_at": time.Now().Unix()})
	writeJSON(w, map[string]any{
		"ok":    true,
		"email": owner.Email,
		"id":    owner.ID,
	})
}

func (s *Server) handleOwnerLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.validAdminOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return
	}
	if cookie, err := r.Cookie("monitor_owner"); err == nil {
		s.sessions.Delete(cookie.Value)
	}
	http.SetCookie(w, ownerCookie(r, "", -time.Hour, true))
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleOwnerMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	owner, ok := s.ownerAuthorized(r)
	if !ok {
		writeJSON(w, map[string]any{"authenticated": false})
		return
	}
	settings := s.store.GetSettings()
	writeJSON(w, map[string]any{
		"authenticated":             true,
		"id":                        owner.ID,
		"email":                     owner.Email,
		"created_at":                owner.CreatedAt,
		"last_login":                owner.LastLogin,
		"market_enabled":            settings.MarketEnabled,
		"registration_enabled":      !settings.UserRegistrationDisabled,
		"self_service_node_enabled": !settings.UserNodeApplyDisabled,
		"user_node_limit":           settings.UserNodeLimit,
	})
}

func (s *Server) handleOwnerNodes(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.ownerAuthorized(r)
	if !ok {
		http.Error(w, "owner login required", http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.handleAccountNodesList(w, owner)
	case http.MethodPost:
		s.handleAccountNodeCreate(w, r, owner)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleOwnerNodesToggle(w http.ResponseWriter, r *http.Request) {
	if !s.marketEnabled(w) {
		return
	}
	owner, ok := s.ownerAuthorized(r)
	if !ok {
		http.Error(w, "owner login required", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.validAdminOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return
	}
	var req struct {
		NodeID  string `json:"node_id"`
		ForSale bool   `json:"for_sale"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !s.ownerOwnsNode(owner.ID, req.NodeID) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err := s.store.SetListingForSale(req.NodeID, req.ForSale); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = s.store.AppendAudit(auditForRequest(r, "owner", owner.ID, "listing.toggle", "market_listing", req.NodeID, "Changed marketplace visibility"))
	s.cache.MarkDirty()
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleOwnerNodesResetToken(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.ownerAuthorized(r)
	if !ok {
		http.Error(w, "owner login required", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.validAdminOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return
	}
	var req struct {
		NodeID string `json:"node_id"`
		Reset  bool   `json:"reset"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !s.ownerOwnsNode(owner.ID, req.NodeID) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	base, err := s.externalBase(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Default: re-show existing token when possible; reset=true rotates.
	token, rotated, err := s.issueOrReuseNodeToken(req.NodeID, req.Reset)
	if err != nil {
		if strings.Contains(err.Error(), "legacy") {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	linux, windows := buildInstallCommands(base, req.NodeID, token)
	if rotated {
		_ = s.store.AppendAudit(auditForRequest(r, "owner", owner.ID, "node.token.rotate", "node", req.NodeID, "Rotated Agent token"))
		go s.dispatchPlatformEvent("node.token.rotated", map[string]any{"owner_id": owner.ID, "node_id": req.NodeID, "created_at": time.Now().Unix()})
	}
	writeJSON(w, map[string]any{
		"ok":      true,
		"rotated": rotated,
		"node_id": req.NodeID,
		"linux":   linux,
		"windows": windows,
	})
}

func (s *Server) handleOwnerNodesUpdate(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.ownerAuthorized(r)
	if !ok {
		http.Error(w, "owner login required", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.validAdminOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return
	}
	var req struct {
		NodeID        string   `json:"node_id"`
		DisplayName   *string  `json:"display_name"`
		Region        *string  `json:"region"`
		ListingType   *string  `json:"listing_type"`
		Contact       *string  `json:"contact"`
		Description   *string  `json:"description"`
		Specs         *string  `json:"specs"`
		Price         *string  `json:"price"`
		PriceAmount   *float64 `json:"price_amount"`
		PriceCurrency *string  `json:"price_currency"`
		BillingCycle  *string  `json:"billing_cycle"`
		ForSale       *bool    `json:"for_sale"`
		DueDate       *string  `json:"due_date"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	listingWrite := req.ListingType != nil || req.Contact != nil || req.Description != nil || req.Specs != nil || req.Price != nil || req.PriceAmount != nil || req.PriceCurrency != nil || req.BillingCycle != nil || req.ForSale != nil
	if listingWrite && !s.store.GetSettings().MarketEnabled {
		http.Error(w, "market is currently disabled", http.StatusServiceUnavailable)
		return
	}
	if !s.ownerOwnsNode(owner.ID, req.NodeID) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	update := MarketListingUpdate{
		ListingType:   req.ListingType,
		Contact:       req.Contact,
		Description:   req.Description,
		Specs:         req.Specs,
		Price:         req.Price,
		PriceAmount:   req.PriceAmount,
		PriceCurrency: req.PriceCurrency,
		BillingCycle:  req.BillingCycle,
		ForSale:       req.ForSale,
	}
	if update.ListingType != nil {
		lt := normalizeListingType(*update.ListingType)
		if lt == "" {
			http.Error(w, "invalid listing_type", http.StatusBadRequest)
			return
		}
		update.ListingType = &lt
	}
	if update.Contact != nil {
		c := strings.TrimSpace(*update.Contact)
		if c == "" || utf8.RuneCountInString(c) > 120 {
			http.Error(w, "invalid contact", http.StatusBadRequest)
			return
		}
		update.Contact = &c
	}
	var normalizedBilling *MarketListing
	if update.Price != nil || update.PriceAmount != nil || update.PriceCurrency != nil || update.BillingCycle != nil {
		current, exists := s.store.GetMarketListing(req.NodeID)
		if !exists {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		price := current.Price
		amount := current.PriceAmount
		currency := current.PriceCurrency
		cycle := current.BillingCycle
		if update.Price != nil {
			price = *update.Price
		}
		if update.PriceAmount != nil {
			amount = *update.PriceAmount
		}
		if update.PriceCurrency != nil {
			currency = *update.PriceCurrency
		}
		if update.BillingCycle != nil {
			cycle = *update.BillingCycle
		}
		price, amount, currency, cycle, err := normalizeBilling(price, "", amount, currency, cycle, true)
		if err != nil || utf8.RuneCountInString(price) > 64 {
			http.Error(w, "invalid billing information", http.StatusBadRequest)
			return
		}
		update.Price, update.PriceAmount, update.PriceCurrency, update.BillingCycle = &price, &amount, &currency, &cycle
		current.Price, current.PriceAmount, current.PriceCurrency, current.BillingCycle = price, amount, currency, cycle
		normalizedBilling = &current
	}
	if update.Description != nil {
		d := sanitizePlain(*update.Description, 500)
		update.Description = &d
	}
	if update.Specs != nil {
		sp := sanitizePlain(*update.Specs, 200)
		update.Specs = &sp
	}
	var dueTime int64
	if req.DueDate != nil {
		var err error
		dueTime, err = parseOptionalDueDate(*req.DueDate)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	mutation := OwnerNodeMutation{NodeID: req.NodeID, Listing: update, Audit: auditForRequest(r, "owner", owner.ID, "listing.update", "market_listing", req.NodeID, "Updated listing and node metadata")}
	if normalizedBilling != nil {
		mutation.Billing = &NodeBillingUpdate{Price: normalizedBilling.Price, LegacyCycle: legacyCycleLabel(normalizedBilling.BillingCycle), PriceAmount: normalizedBilling.PriceAmount, PriceCurrency: normalizedBilling.PriceCurrency, BillingCycle: normalizedBilling.BillingCycle}
	}
	if req.DisplayName != nil || req.Region != nil {
		curName, curRegion, _, _ := s.store.GetNodeMeta(req.NodeID)
		if req.DisplayName != nil {
			curName = strings.TrimSpace(*req.DisplayName)
		}
		if req.Region != nil {
			curRegion = strings.TrimSpace(*req.Region)
		}
		displayName, region, regionCode := resolveNodeMeta(curName, curRegion, req.NodeID)
		mutation.DisplayName, mutation.Region, mutation.RegionCode = &displayName, &region, &regionCode
	}
	if req.DueDate != nil {
		mutation.DueTime = &dueTime
	}
	if err := s.store.UpdateOwnerNode(owner.ID, mutation); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.cache.MarkDirty()
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleOwnerNodesDelete(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.ownerAuthorized(r)
	if !ok {
		http.Error(w, "owner login required", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.validAdminOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return
	}
	var req struct {
		NodeID string `json:"node_id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !s.ownerOwnsNode(owner.ID, req.NodeID) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if _, err := s.store.TrashNode(req.NodeID, auditForRequest(r, "owner", owner.ID, "node.trash", "node", req.NodeID, "Owner moved node to recycle bin")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.cache.MarkDirty()
	go s.dispatchPlatformEvent("node.deleted", map[string]any{"owner_id": owner.ID, "node_id": req.NodeID, "created_at": time.Now().Unix()})
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) ownerOwnsNode(ownerID, nodeID string) bool {
	nodeID = strings.TrimSpace(nodeID)
	if !validNodeID(nodeID) {
		return false
	}
	planned, ok := s.store.GetPlannedNode(nodeID)
	return ok && planned.OwnerID == ownerID
}

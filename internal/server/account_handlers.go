package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

type accountNodeView struct {
	AdminNode
	Private    bool               `json:"private"`
	HasListing bool               `json:"has_listing"`
	Listing    *MarketListingView `json:"listing,omitempty"`
	Host       *AkileHost         `json:"host,omitempty"`
}

func (s *Server) handleAccountCaptcha(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if s.store.GetSettings().UserRegistrationDisabled {
		http.Error(w, "account registration disabled", http.StatusForbidden)
		return
	}
	if s.captchaLimiter != nil && !s.captchaLimiter.Allow(requestClientIP(r)) {
		http.Error(w, "too many captcha requests", http.StatusTooManyRequests)
		return
	}
	id, image, err := s.captcha.Issue()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"captcha_id": id, "captcha_image": image})
}

func (s *Server) handleAccountRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if s.store.GetSettings().UserRegistrationDisabled {
		http.Error(w, "account registration disabled", http.StatusForbidden)
		return
	}
	if !s.validAdminOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return
	}
	var req struct {
		Email           string `json:"email"`
		Password        string `json:"password"`
		PasswordConfirm string `json:"password_confirm"`
		RememberMe      bool   `json:"remember_me"`
		CaptchaID       string `json:"captcha_id"`
		CaptchaCode     string `json:"captcha_code"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if s.captcha == nil || !s.captcha.Verify(req.CaptchaID, req.CaptchaCode) {
		http.Error(w, "invalid captcha", http.StatusBadRequest)
		return
	}
	email := normalizeOwnerEmail(req.Email)
	if email == "" || !strings.Contains(email, "@") || utf8.RuneCountInString(email) > 120 {
		http.Error(w, "invalid email", http.StatusBadRequest)
		return
	}
	if len(req.Password) < 8 || len(req.Password) > 128 {
		http.Error(w, "password must be 8-128 characters", http.StatusBadRequest)
		return
	}
	if req.PasswordConfirm != req.Password {
		http.Error(w, "password confirmation mismatch", http.StatusBadRequest)
		return
	}
	if !s.allowMarketSubmit(requestClientIP(r)) {
		http.Error(w, "too many requests, try again later", http.StatusTooManyRequests)
		return
	}
	hash, err := hashOwnerPassword(req.Password)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	owner, err := s.store.CreateOwner(email, hash)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "registered") {
			http.Error(w, "email already registered", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
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
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]any{"ok": true, "id": owner.ID, "email": owner.Email})
}

func (s *Server) handleAccountNodesList(w http.ResponseWriter, owner Owner) {
	ids := s.store.ListOwnerNodeIDs(owner.ID)
	seenIDs := make(map[string]bool, len(ids))
	for _, nodeID := range ids {
		seenIDs[nodeID] = true
	}
	for _, listing := range s.store.ListOwnerListings(owner.ID) {
		if !seenIDs[listing.NodeID] {
			ids = append(ids, listing.NodeID)
			seenIDs[listing.NodeID] = true
		}
	}
	adminByID := make(map[string]AdminNode)
	for _, node := range s.store.AdminNodes(s.cfg.OfflineWait) {
		adminByID[node.NodeID] = node
	}
	hostByID := make(map[string]AkileHost)
	for _, host := range s.store.AkileHosts() {
		hostByID[host.Host.Name] = host
	}
	listingByID := make(map[string]MarketListingView)
	for _, view := range s.buildMarketViews(s.store.ListOwnerListings(owner.ID), true) {
		listingByID[view.NodeID] = view
	}
	out := make([]accountNodeView, 0, len(ids))
	for _, nodeID := range ids {
		planned, ok := s.store.GetPlannedNode(nodeID)
		if !ok {
			continue
		}
		admin := adminByID[nodeID]
		if admin.NodeID == "" {
			admin = AdminNode{NodeID: nodeID, DisplayName: planned.DisplayName, Region: planned.Region, RegionCode: planned.RegionCode, CreatedAt: planned.CreatedAt}
		}
		item := accountNodeView{AdminNode: admin, Private: planned.Private}
		if host, ok := hostByID[nodeID]; ok {
			copy := host
			item.Host = &copy
		}
		if listing, ok := listingByID[nodeID]; ok {
			copy := listing
			item.HasListing = true
			item.Listing = &copy
		}
		out = append(out, item)
	}
	writeJSON(w, out)
}

func (s *Server) handleAccountNodeCreate(w http.ResponseWriter, r *http.Request, owner Owner) {
	if !s.validAdminOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return
	}
	settings := s.store.GetSettings()
	if settings.UserNodeApplyDisabled {
		http.Error(w, "self-service node creation disabled", http.StatusForbidden)
		return
	}
	base, err := s.externalBase(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var req struct {
		DisplayName string `json:"display_name"`
		Region      string `json:"region"`
		DueDate     string `json:"due_date"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.Region = strings.TrimSpace(req.Region)
	if req.DisplayName == "" || utf8.RuneCountInString(req.DisplayName) > 64 || utf8.RuneCountInString(req.Region) > 32 {
		http.Error(w, "invalid node metadata", http.StatusBadRequest)
		return
	}
	dueTime, err := parseOptionalDueDate(req.DueDate)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	nodeID, err := newNodeID()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	displayName, region, regionCode := resolveNodeMeta(req.DisplayName, req.Region, nodeID)
	agentToken, err := newAgentToken()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	now := time.Now().Unix()
	planned := PlannedNode{NodeID: nodeID, OwnerID: owner.ID, Private: true, CreatedAt: now, TokenHash: hashToken(agentToken), DisplayName: displayName, Region: region, RegionCode: regionCode}
	info := HostInfo{Name: nodeID, DisplayName: displayName, Region: region, RegionCode: regionCode, DueTime: dueTime}
	audit := auditForRequest(r, "owner", owner.ID, "node.create", "node", nodeID, "Created private monitoring node")
	if err := s.store.CreateOwnerNode(owner.ID, planned, info, agentToken, audit, s.cfg.MaxNodes, settings.UserNodeLimit); err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(strings.ToLower(err.Error()), "limit") || strings.Contains(strings.ToLower(err.Error()), "max") {
			status = http.StatusTooManyRequests
		}
		http.Error(w, err.Error(), status)
		return
	}
	linux, windows := buildInstallCommands(base, nodeID, agentToken)
	s.cache.MarkDirty()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]any{"ok": true, "node_id": nodeID, "display_name": displayName, "region": region, "region_code": regionCode, "private": true, "linux": linux, "windows": windows})
}

func (s *Server) handleAccountNodePrivacy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	owner, ok := s.ownerAuthorized(r)
	if !ok {
		http.Error(w, "account login required", http.StatusUnauthorized)
		return
	}
	if !s.validAdminOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return
	}
	var req struct {
		NodeID  string `json:"node_id"`
		Private bool   `json:"private"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req) != nil || !s.ownerOwnsNode(owner.ID, req.NodeID) {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}
	audit := auditForRequest(r, "owner", owner.ID, "node.privacy", "node", req.NodeID, "Changed monitoring visibility")
	if err := s.store.SetNodePrivacy(owner.ID, req.NodeID, req.Private, audit); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.cache.MarkDirty()
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleAccountNodeListing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.marketEnabled(w) {
		return
	}
	owner, ok := s.ownerAuthorized(r)
	if !ok {
		http.Error(w, "account login required", http.StatusUnauthorized)
		return
	}
	if !s.validAdminOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return
	}
	var req struct {
		NodeID        string  `json:"node_id"`
		ListingType   string  `json:"listing_type"`
		Contact       string  `json:"contact"`
		Description   string  `json:"description"`
		Specs         string  `json:"specs"`
		Price         string  `json:"price"`
		PriceAmount   float64 `json:"price_amount"`
		PriceCurrency string  `json:"price_currency"`
		BillingCycle  string  `json:"billing_cycle"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	req.NodeID = strings.TrimSpace(req.NodeID)
	if !s.ownerOwnsNode(owner.ID, req.NodeID) {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}
	if _, exists := s.store.GetMarketListing(req.NodeID); exists {
		http.Error(w, "listing already exists", http.StatusConflict)
		return
	}
	listingType := normalizeListingType(req.ListingType)
	contact := strings.TrimSpace(req.Contact)
	if listingType == "" || contact == "" || utf8.RuneCountInString(contact) > 120 {
		http.Error(w, "invalid listing information", http.StatusBadRequest)
		return
	}
	price, amount, currency, cycle, err := normalizeBilling(req.Price, "", req.PriceAmount, req.PriceCurrency, req.BillingCycle, true)
	if err != nil || utf8.RuneCountInString(price) > 64 {
		http.Error(w, "invalid billing information", http.StatusBadRequest)
		return
	}
	now := time.Now().Unix()
	listing := MarketListing{NodeID: req.NodeID, OwnerID: owner.ID, ForSale: true, ListingType: listingType, Contact: contact, Description: sanitizePlain(req.Description, 500), Specs: sanitizePlain(req.Specs, 200), Price: price, PriceAmount: amount, PriceCurrency: currency, BillingCycle: cycle, CreatedAt: now, UpdatedAt: now, ModerationStatus: "pending"}
	audit := auditForRequest(r, "owner", owner.ID, "listing.create", "market_listing", req.NodeID, "Submitted owned monitoring node to marketplace")
	if err := s.store.CreateOwnerListing(owner.ID, listing, audit); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = s.store.SetNodeBilling(req.NodeID, price, legacyCycleLabel(cycle), amount, currency, cycle)
	s.cache.MarkDirty()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]any{"ok": true, "moderation_status": "pending"})
}

func (s *Server) handleAccountNodeHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	owner, ok := s.ownerAuthorized(r)
	if !ok {
		http.Error(w, "account login required", http.StatusUnauthorized)
		return
	}
	nodeID := strings.TrimSpace(r.URL.Query().Get("node_id"))
	if !s.ownerOwnsNode(owner.ID, nodeID) {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}
	rangeName := strings.TrimSpace(r.URL.Query().Get("range"))
	duration, err := historyRange(rangeName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]any{"node_id": nodeID, "range": rangeName, "samples": s.metricsHistory.Query(nodeID, time.Now().Add(-duration).Unix())})
}

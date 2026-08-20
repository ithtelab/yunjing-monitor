package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

func (s *Server) handleMarketCaptcha(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if !s.marketEnabled(w) {
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
	writeJSON(w, map[string]string{
		"captcha_id":    id,
		"captcha_image": image,
	})
}

func (s *Server) handleMarketListings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if !s.marketEnabled(w) {
		return
	}
	region := strings.TrimSpace(r.URL.Query().Get("region"))
	views := s.buildMarketViews(s.store.ListMarketListings(true), false)
	if region != "" && !strings.EqualFold(region, "all") {
		filtered := make([]MarketListingView, 0, len(views))
		for _, item := range views {
			if strings.EqualFold(item.RegionCode, region) || item.Region == region {
				filtered = append(filtered, item)
			}
		}
		views = filtered
	}
	writeJSON(w, views)
}

func (s *Server) handleMarketCategories(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if !s.marketEnabled(w) {
		return
	}
	views := s.buildMarketViews(s.store.ListMarketListings(true), false)
	counts := map[string]*MarketCategory{}
	for _, item := range views {
		code := strings.TrimSpace(item.RegionCode)
		name := strings.TrimSpace(item.Region)
		if code == "" {
			code = "OTHER"
		}
		if name == "" {
			name = code
		}
		cat, ok := counts[code]
		if !ok {
			cat = &MarketCategory{ID: code, Name: name}
			counts[code] = cat
		}
		cat.NodeCount++
	}
	out := make([]MarketCategory, 0, len(counts))
	for _, cat := range counts {
		out = append(out, *cat)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].NodeCount != out[j].NodeCount {
			return out[i].NodeCount > out[j].NodeCount
		}
		return out[i].ID < out[j].ID
	})
	writeJSON(w, out)
}

func (s *Server) handleMarketSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.marketEnabled(w) {
		return
	}
	settings := s.store.GetSettings()
	if settings.UserNodeApplyDisabled {
		http.Error(w, "self-service node creation disabled", http.StatusForbidden)
		return
	}
	if !s.validAdminOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return
	}
	base, err := s.externalBase(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var req struct {
		Email           string  `json:"email"`
		Password        string  `json:"password"`
		PasswordConfirm string  `json:"password_confirm"`
		DisplayName     string  `json:"display_name"`
		Region          string  `json:"region"`
		Specs           string  `json:"specs"`
		Price           string  `json:"price"`
		PriceAmount     float64 `json:"price_amount"`
		PriceCurrency   string  `json:"price_currency"`
		BillingCycle    string  `json:"billing_cycle"`
		ListingType     string  `json:"listing_type"`
		Contact         string  `json:"contact"`
		Description     string  `json:"description"`
		DueDate         string  `json:"due_date"`
		CaptchaID       string  `json:"captcha_id"`
		CaptchaCode     string  `json:"captcha_code"`
		// Existing owner login path (optional): if email already registered, password logs in.
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !s.captcha.Verify(req.CaptchaID, req.CaptchaCode) {
		http.Error(w, "invalid captcha", http.StatusBadRequest)
		return
	}
	email := normalizeOwnerEmail(req.Email)
	password := req.Password
	displayName := strings.TrimSpace(req.DisplayName)
	if email == "" || !strings.Contains(email, "@") || utf8.RuneCountInString(email) > 120 {
		http.Error(w, "invalid email", http.StatusBadRequest)
		return
	}
	if len(password) < 8 || len(password) > 128 {
		http.Error(w, "password must be 8-128 characters", http.StatusBadRequest)
		return
	}
	// Require explicit confirmation so API callers cannot skip the second password field.
	if req.PasswordConfirm != password {
		http.Error(w, "password confirmation mismatch", http.StatusBadRequest)
		return
	}
	if displayName == "" || utf8.RuneCountInString(displayName) > 64 {
		http.Error(w, "invalid display_name", http.StatusBadRequest)
		return
	}
	listingType := normalizeListingType(req.ListingType)
	if listingType == "" {
		http.Error(w, "invalid listing_type (rent|sale|transfer)", http.StatusBadRequest)
		return
	}
	contact := strings.TrimSpace(req.Contact)
	if contact == "" || utf8.RuneCountInString(contact) > 120 {
		http.Error(w, "invalid contact", http.StatusBadRequest)
		return
	}
	price, priceAmount, priceCurrency, billingCycle, err := normalizeBilling(
		req.Price, "", req.PriceAmount, req.PriceCurrency, req.BillingCycle, true,
	)
	if err != nil || utf8.RuneCountInString(price) > 64 {
		http.Error(w, "invalid billing information", http.StatusBadRequest)
		return
	}
	specs := sanitizePlain(req.Specs, 200)
	description := sanitizePlain(req.Description, 500)
	regionHint := strings.TrimSpace(req.Region)
	dueTime, err := parseOptionalDueDate(req.DueDate)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Rate-limit only after captcha + field validation so a typo does not burn the 30s window.
	if !s.allowMarketSubmit(requestClientIP(r)) {
		http.Error(w, "too many requests, try again later", http.StatusTooManyRequests)
		return
	}

	// Register or login owner.
	var owner Owner
	var isNew bool
	if existing, err := s.store.AuthenticateOwner(email, password); err == nil {
		owner = existing
	} else {
		if strings.Contains(err.Error(), "disabled") {
			http.Error(w, "owner account disabled", http.StatusForbidden)
			return
		}
		if settings.UserRegistrationDisabled {
			http.Error(w, "account registration disabled", http.StatusForbidden)
			return
		}
		hash, err := hashOwnerPassword(password)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		ownerID, err := newOwnerID()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		now := time.Now().Unix()
		owner = Owner{ID: ownerID, Email: email, PasswordHash: hash, CreatedAt: now, LastLogin: now}
		isNew = true
	}

	nodeID, err := newNodeID()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	displayName, region, regionCode := resolveNodeMeta(displayName, regionHint, nodeID)
	token, err := newAgentToken()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	now := time.Now().Unix()
	listing := MarketListing{
		NodeID:           nodeID,
		OwnerID:          owner.ID,
		ForSale:          true,
		ListingType:      listingType,
		Contact:          contact,
		Description:      description,
		Specs:            specs,
		Price:            price,
		PriceAmount:      priceAmount,
		PriceCurrency:    priceCurrency,
		BillingCycle:     billingCycle,
		Pinned:           false,
		CreatedAt:        now,
		UpdatedAt:        now,
		ModerationStatus: "pending",
	}
	info := HostInfo{Name: nodeID, DisplayName: displayName, Region: region, RegionCode: regionCode, DueTime: dueTime, Price: price, Cycle: legacyCycleLabel(billingCycle), PriceAmount: priceAmount, PriceCurrency: priceCurrency, BillingCycle: billingCycle}
	audit := auditForRequest(r, "owner", owner.ID, "market.submit", "market_listing", nodeID, "Created first marketplace listing")
	if err := s.store.CreateMarketSubmission(MarketSubmission{Owner: owner, NewOwner: isNew, Node: PlannedNode{NodeID: nodeID, OwnerID: owner.ID, CreatedAt: now, TokenHash: hashToken(token), DisplayName: displayName, Region: region, RegionCode: regionCode}, Info: info, Listing: listing, AgentToken: token, OwnerNodeLimit: settings.UserNodeLimit, Audit: audit}, s.cfg.MaxNodes); err != nil {
		if strings.Contains(err.Error(), "already registered") {
			http.Error(w, "email already registered, wrong password", http.StatusUnauthorized)
			return
		}
		if strings.Contains(err.Error(), "owner node limit") {
			http.Error(w, err.Error(), http.StatusTooManyRequests)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Auto-login owner session.
	sessToken, err := s.sessions.CreateFor(SessionKindOwner, owner.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, ownerCookie(r, sessToken, defaultSessionTTL, true))

	linux, windows := buildInstallCommands(base, nodeID, token)
	s.cache.MarkDirty()
	writeJSON(w, map[string]any{
		"ok":              true,
		"new_owner":       isNew,
		"owner_id":        owner.ID,
		"email":           owner.Email,
		"node_id":         nodeID,
		"display_name":    displayName,
		"region":          region,
		"region_code":     regionCode,
		"linux":           linux,
		"windows":         windows,
		"linux_install":   linux,
		"windows_install": windows,
	})
}

func (s *Server) handleAdminMarketListings(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		http.Error(w, "admin login required", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, s.buildMarketViews(s.store.ListMarketListings(false), true))
}

func (s *Server) handleAdminMarketPin(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		http.Error(w, "admin login required", http.StatusUnauthorized)
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
		Pinned bool   `json:"pinned"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !validNodeID(req.NodeID) {
		http.Error(w, "invalid node_id", http.StatusBadRequest)
		return
	}
	if err := s.store.SetListingPinned(req.NodeID, req.Pinned); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	_ = s.store.AppendAudit(auditForRequest(r, "admin", "admin", "listing.pin", "market_listing", req.NodeID, fmt.Sprintf("Set pinned=%t", req.Pinned)))
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleAdminMarketDelete(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		http.Error(w, "admin login required", http.StatusUnauthorized)
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
		NodeID     string `json:"node_id"`
		DeleteNode bool   `json:"delete_node"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !validNodeID(req.NodeID) {
		http.Error(w, "invalid node_id", http.StatusBadRequest)
		return
	}
	if req.DeleteNode {
		if _, err := s.store.TrashNode(req.NodeID, auditForRequest(r, "admin", "admin", "node.trash", "node", req.NodeID, "Moved node and listing to recycle bin")); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.cache.MarkDirty()
	} else {
		if _, err := s.store.TrashMarketListing(req.NodeID, auditForRequest(r, "admin", "admin", "listing.trash", "market_listing", req.NodeID, "Moved listing to recycle bin")); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) buildMarketViews(listings []MarketListing, includeOwner bool) []MarketListingView {
	threshold := int64(s.cfg.OfflineWait.Seconds())
	now := time.Now().Unix()
	// Build online/last-seen index from admin nodes (covers planned + reports).
	admin := s.store.AdminNodes(s.cfg.OfflineWait)
	status := map[string]AdminNode{}
	for _, n := range admin {
		status[n.NodeID] = n
	}
	// Live metrics for cores/mem/disk
	hosts := s.store.AkileHosts()
	hostByName := map[string]AkileHost{}
	for _, h := range hosts {
		hostByName[h.Host.Name] = h
	}
	trustByOwner := map[string]SellerTrustProfile{}
	if s.features != nil {
		trustByOwner = s.features.Snapshot().SellerTrust
	}

	out := make([]MarketListingView, 0, len(listings))
	for _, listing := range listings {
		price, priceAmount, priceCurrency, billingCycle, _ := normalizeBilling(
			listing.Price, "", listing.PriceAmount, listing.PriceCurrency, listing.BillingCycle, false,
		)
		view := MarketListingView{
			NodeID:           listing.NodeID,
			ForSale:          listing.ForSale,
			ListingType:      listing.ListingType,
			Contact:          listing.Contact,
			Description:      listing.Description,
			Specs:            listing.Specs,
			Price:            price,
			PriceAmount:      priceAmount,
			PriceCurrency:    priceCurrency,
			BillingCycle:     billingCycle,
			Pinned:           listing.Pinned,
			CreatedAt:        listing.CreatedAt,
			UpdatedAt:        listing.UpdatedAt,
			ModerationStatus: normalizeModerationStatus(listing.ModerationStatus),
			RejectionReason:  listing.RejectionReason,
		}
		if trust, ok := trustByOwner[listing.OwnerID]; ok && (trust.Verified || trust.Level != "standard" || trust.Violations > 0) {
			view.SellerTrust = &SellerTrustView{Verified: trust.Verified, Level: trust.Level, Note: trust.Note, Violations: trust.Violations}
		}
		if includeOwner {
			view.OwnerID = listing.OwnerID
		}
		if meta, ok := status[listing.NodeID]; ok {
			view.DisplayName = meta.DisplayName
			view.Region = meta.Region
			view.RegionCode = meta.RegionCode
			view.Online = meta.Online
			view.LastSeen = meta.LastSeen
			view.DueTime = meta.Info.DueTime
		} else {
			if dn, region, code, ok := s.store.GetNodeMeta(listing.NodeID); ok {
				view.DisplayName = dn
				view.Region = region
				view.RegionCode = code
			}
			view.DisplayName = normalizeDisplayName(view.DisplayName, listing.NodeID)
		}
		if h, ok := hostByName[listing.NodeID]; ok {
			view.LogicalCores = h.Host.LogicalCores
			view.MemTotal = h.Host.MemTotal
			view.DiskTotal = h.State.DiskTotal
			view.NetInSpeed = h.State.NetInSpeed
			view.NetOutSpeed = h.State.NetOutSpeed
			view.NetInTransfer = h.State.NetInTransfer
			view.NetOutTransfer = h.State.NetOutTransfer
			if view.LastSeen == 0 {
				view.LastSeen = h.TimeStamp
				view.Online = h.TimeStamp > 0 && now-h.TimeStamp <= threshold
			}
			if view.DisplayName == "" {
				view.DisplayName = h.Host.DisplayName
			}
			if view.Region == "" {
				view.Region = h.Host.Region
			}
			if view.RegionCode == "" {
				view.RegionCode = h.Host.RegionCode
			}
		}
		if view.DisplayName == "" {
			view.DisplayName = listing.NodeID
		}
		out = append(out, view)
	}
	// Keep store sort (pinned first), but re-apply for safety.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Pinned != out[j].Pinned {
			return out[i].Pinned
		}
		return out[i].UpdatedAt > out[j].UpdatedAt
	})
	return out
}

func parseOptionalDueDate(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value || parsed.Year() < 1970 || parsed.Year() > 9999 {
		return 0, fmt.Errorf("invalid due_date, expected YYYY-MM-DD")
	}
	return parsed.UnixMilli(), nil
}

func normalizeListingType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "rent", "出租":
		return "rent"
	case "sale", "出售":
		return "sale"
	case "transfer", "转让":
		return "transfer"
	default:
		return ""
	}
}

func sanitizePlain(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\x00", "")
	if maxRunes > 0 && utf8.RuneCountInString(value) > maxRunes {
		runes := []rune(value)
		value = string(runes[:maxRunes])
	}
	return value
}

func buildInstallCommands(base, nodeID, token string) (linux, windows string) {
	linux = fmt.Sprintf("curl -fsSL %s/install/agent-linux.sh | sudo sh -s -- --server %s --token %s --node-id %s", base, base, shellQuote(token), shellQuote(nodeID))
	windows = fmt.Sprintf("powershell -ExecutionPolicy Bypass -Command \"[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12; iwr %s/install/agent-windows.ps1 -UseBasicParsing | iex; Install-VpsAgent -Server '%s' -Token '%s' -NodeId '%s'\"", base, base, psQuote(token), psQuote(nodeID))
	return linux, windows
}

// clientIP returns the connecting peer address for rate limiting.
// Do not trust X-Forwarded-For / X-Real-IP here: those headers are client-spoofable
// unless a trusted reverse proxy strips them. Proxies should present the real client
// as RemoteAddr (or terminate TLS and pass a controlled header via middleware later).
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

// simple per-IP rate limiter for market submit (1 / 30s).
type ipRateLimiter struct {
	mu      sync.Mutex
	lastHit map[string]time.Time
	window  time.Duration
}

func newIPRateLimiter(window time.Duration) *ipRateLimiter {
	return &ipRateLimiter{lastHit: map[string]time.Time{}, window: window}
}

func (l *ipRateLimiter) Allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if _, exists := l.lastHit[ip]; !exists && len(l.lastHit) >= 8192 {
		for k, t := range l.lastHit {
			if now.Sub(t) > l.window*2 {
				delete(l.lastHit, k)
			}
		}
		if len(l.lastHit) >= 8192 {
			return false
		}
	}
	if t, ok := l.lastHit[ip]; ok && now.Sub(t) < l.window {
		return false
	}
	l.lastHit[ip] = now
	// opportunistic cleanup
	if len(l.lastHit) > 1000 {
		for k, t := range l.lastHit {
			if now.Sub(t) > l.window*2 {
				delete(l.lastHit, k)
			}
		}
	}
	return true
}

func (s *Server) allowMarketSubmit(ip string) bool {
	if s.submitLimiter == nil {
		return true
	}
	return s.submitLimiter.Allow(ip)
}

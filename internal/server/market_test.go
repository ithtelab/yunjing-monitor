package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newMarketTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	return &Server{
		cfg: Config{
			AuthSecret:  "strong-auth-secret-for-tests",
			AdminPass:   "strong-admin-password",
			DataPath:    filepath.Join(dir, "data.json"),
			OfflineWait: 10 * time.Second,
			MaxNodes:    2000,
			AdminUser:   "admin",
		},
		store:         store,
		sessions:      NewSessionStore(),
		cache:         NewResponseCache(),
		captcha:       NewCaptchaStore(),
		submitLimiter: newIPRateLimiter(time.Millisecond), // allow rapid tests
	}
}

func submitMarketFixture(t *testing.T, s *Server, email string) *httptest.ResponseRecorder {
	t.Helper()
	id, code := issueTestCaptcha(t, s.captcha)
	body := map[string]any{
		"email": email, "password": "password123", "password_confirm": "password123",
		"display_name": "fixture-node", "region": "HK", "specs": "2C2G",
		"price_amount": 10, "price_currency": "USD", "billing_cycle": "monthly",
		"listing_type": "rent", "contact": "tg:@fixture", "captcha_id": id, "captcha_code": code,
	}
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "https://monitor.example.com/api/market/submit", bytes.NewReader(payload))
	req.Host = "monitor.example.com"
	resp := httptest.NewRecorder()
	s.handleMarketSubmit(resp, req)
	return resp
}

func TestLegacyMarketSubmitRespectsAccountControls(t *testing.T) {
	t.Run("self service disabled", func(t *testing.T) {
		s := newMarketTestServer(t)
		settings := s.store.GetSettings()
		settings.UserNodeApplyDisabled = true
		if err := s.store.UpdateSettings(settings); err != nil {
			t.Fatal(err)
		}
		resp := submitMarketFixture(t, s, "disabled-node@example.com")
		if resp.Code != http.StatusForbidden {
			t.Fatalf("self-service disabled status=%d body=%s", resp.Code, resp.Body.String())
		}
	})

	t.Run("registration disabled", func(t *testing.T) {
		s := newMarketTestServer(t)
		settings := s.store.GetSettings()
		settings.UserRegistrationDisabled = true
		if err := s.store.UpdateSettings(settings); err != nil {
			t.Fatal(err)
		}
		resp := submitMarketFixture(t, s, "disabled-registration@example.com")
		if resp.Code != http.StatusForbidden {
			t.Fatalf("registration disabled status=%d body=%s", resp.Code, resp.Body.String())
		}
	})

	t.Run("owner node limit", func(t *testing.T) {
		s := newMarketTestServer(t)
		settings := s.store.GetSettings()
		settings.UserNodeLimit = 1
		if err := s.store.UpdateSettings(settings); err != nil {
			t.Fatal(err)
		}
		first := submitMarketFixture(t, s, "limited@example.com")
		if first.Code != http.StatusOK {
			t.Fatalf("first submit status=%d body=%s", first.Code, first.Body.String())
		}
		time.Sleep(2 * time.Millisecond)
		second := submitMarketFixture(t, s, "limited@example.com")
		if second.Code != http.StatusTooManyRequests {
			t.Fatalf("limited submit status=%d body=%s", second.Code, second.Body.String())
		}
	})
}

func TestMarketSubmitAndListings(t *testing.T) {
	s := newMarketTestServer(t)

	// Issue captcha and plant known answer.
	id, img, err := s.captcha.Issue()
	if err != nil || id == "" || img == "" {
		t.Fatalf("captcha issue failed: %v", err)
	}
	// Override captcha answer for deterministic verify.
	s.captcha.mu.Lock()
	entry := s.captcha.entries[id]
	entry.Code = "TEST1"
	s.captcha.entries[id] = entry
	s.captcha.mu.Unlock()

	body := map[string]any{
		"email":            "seller@example.com",
		"password":         "password123",
		"password_confirm": "password123",
		"display_name":     "香港-CN2-GIA",
		"specs":            "4C8G",
		"price":            "¥99/月",
		"listing_type":     "rent",
		"contact":          "tg:@seller",
		"description":      "test listing",
		"due_date":         "2027-08-09",
		"captcha_id":       id,
		"captcha_code":     "TEST1",
	}
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "https://monitor.example.com/api/market/submit", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "monitor.example.com"
	resp := httptest.NewRecorder()
	s.handleMarketSubmit(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("submit status=%d body=%s", resp.Code, resp.Body.String())
	}
	var submit map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &submit); err != nil {
		t.Fatal(err)
	}
	if submit["node_id"] == nil || submit["linux"] == nil {
		t.Fatalf("submit payload incomplete: %#v", submit)
	}
	nodeID, _ := submit["node_id"].(string)
	// owner cookie set
	if cookies := resp.Result().Cookies(); len(cookies) == 0 {
		t.Fatal("expected owner cookie")
	}

	listReq := httptest.NewRequest(http.MethodGet, "https://monitor.example.com/api/market/listings", nil)
	listResp := httptest.NewRecorder()
	s.handleMarketListings(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("listings status=%d", listResp.Code)
	}
	var listings []MarketListingView
	if err := json.Unmarshal(listResp.Body.Bytes(), &listings); err != nil {
		t.Fatal(err)
	}
	if len(listings) != 0 {
		t.Fatalf("pending listing was public: %#v", listings)
	}
	pending, ok := s.store.GetMarketListing(nodeID)
	if !ok || pending.ModerationStatus != "pending" {
		t.Fatalf("submitted listing moderation=%q ok=%v", pending.ModerationStatus, ok)
	}
	if err := s.store.ModerateMarketListing(ListingModerationMutation{NodeID: nodeID, Status: "approved", Audit: governanceAudit("listing.moderate", "market_listing", nodeID)}); err != nil {
		t.Fatal(err)
	}
	listResp = httptest.NewRecorder()
	s.handleMarketListings(listResp, listReq)
	if err := json.Unmarshal(listResp.Body.Bytes(), &listings); err != nil {
		t.Fatal(err)
	}
	if len(listings) != 1 {
		t.Fatalf("approved listings len=%d", len(listings))
	}
	if listings[0].DisplayName != "香港-CN2-GIA" {
		t.Fatalf("display name=%q", listings[0].DisplayName)
	}
	if listings[0].RegionCode != "HK" && listings[0].Region != "香港" {
		t.Fatalf("region not inferred: %+v", listings[0])
	}
	wantDue, _ := parseOptionalDueDate("2027-08-09")
	if listings[0].DueTime != wantDue {
		t.Fatalf("due time=%d want=%d", listings[0].DueTime, wantDue)
	}
	if listings[0].PriceAmount != 99 || listings[0].PriceCurrency != "CNY" || listings[0].BillingCycle != "monthly" {
		t.Fatalf("legacy billing was not normalized: %+v", listings[0])
	}

	catReq := httptest.NewRequest(http.MethodGet, "https://monitor.example.com/api/market/categories", nil)
	catResp := httptest.NewRecorder()
	s.handleMarketCategories(catResp, catReq)
	var cats []MarketCategory
	if err := json.Unmarshal(catResp.Body.Bytes(), &cats); err != nil {
		t.Fatal(err)
	}
	if len(cats) == 0 {
		t.Fatal("expected categories")
	}
}

func TestOwnerLoginAndToggle(t *testing.T) {
	s := newMarketTestServer(t)
	hash, err := hashOwnerPassword("password123")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := s.store.CreateOwner("owner@example.com", hash)
	if err != nil {
		t.Fatal(err)
	}
	nodeID := "n_testnode01"
	if err := s.store.AddPlannedNode(nodeID, 100); err != nil {
		t.Fatal(err)
	}
	_ = s.store.SetNodeMeta(nodeID, "日本-东京", "日本", "JP")
	if err := s.store.CreateMarketListing(MarketListing{
		NodeID: nodeID, OwnerID: owner.ID, ForSale: true, ListingType: "sale",
		Contact: "qq:1", Price: "100", CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}); err != nil {
		t.Fatal(err)
	}

	loginBody, _ := json.Marshal(map[string]any{"email": "owner@example.com", "password": "password123", "remember_me": true})
	loginReq := httptest.NewRequest(http.MethodPost, "https://monitor.example.com/api/owner/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginReq.Host = "monitor.example.com"
	loginResp := httptest.NewRecorder()
	s.handleOwnerLogin(loginResp, loginReq)
	if loginResp.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", loginResp.Code, loginResp.Body.String())
	}
	var ownerCookie *http.Cookie
	for _, c := range loginResp.Result().Cookies() {
		if c.Name == "monitor_owner" {
			ownerCookie = c
		}
	}
	if ownerCookie == nil {
		t.Fatal("missing owner cookie")
	}
	if ownerCookie.MaxAge != int(rememberedSessionTTL.Seconds()) {
		t.Fatalf("remembered owner cookie max age=%d", ownerCookie.MaxAge)
	}

	updateBody, _ := json.Marshal(map[string]any{
		"node_id": nodeID, "due_date": "2028-03-04", "price": "USD 0.50",
		"price_amount": 0.5, "price_currency": "USD", "billing_cycle": "monthly",
	})
	updateReq := httptest.NewRequest(http.MethodPost, "https://monitor.example.com/api/owner/nodes/update", bytes.NewReader(updateBody))
	updateReq.Header.Set("Content-Type", "application/json")
	updateReq.Host = "monitor.example.com"
	updateReq.AddCookie(ownerCookie)
	updateResp := httptest.NewRecorder()
	s.handleOwnerNodesUpdate(updateResp, updateReq)
	if updateResp.Code != http.StatusOK {
		t.Fatalf("update due date status=%d body=%s", updateResp.Code, updateResp.Body.String())
	}
	views := s.buildMarketViews(s.store.ListOwnerListings(owner.ID), true)
	wantDue, _ := parseOptionalDueDate("2028-03-04")
	if len(views) != 1 || views[0].DueTime != wantDue {
		t.Fatalf("owner due date was not saved: %+v", views)
	}
	if views[0].PriceAmount != 0.5 || views[0].PriceCurrency != "USD" || views[0].BillingCycle != "monthly" {
		t.Fatalf("owner billing was not saved: %+v", views[0])
	}
	infos := s.store.InfoList()
	if len(infos) != 1 || infos[0].PriceAmount != 0.5 || infos[0].BillingCycle != "monthly" {
		t.Fatalf("owner billing was not synchronized to host info: %+v", infos)
	}

	clearBody, _ := json.Marshal(map[string]any{"node_id": nodeID, "due_date": ""})
	clearReq := httptest.NewRequest(http.MethodPost, "https://monitor.example.com/api/owner/nodes/update", bytes.NewReader(clearBody))
	clearReq.Header.Set("Content-Type", "application/json")
	clearReq.Host = "monitor.example.com"
	clearReq.AddCookie(ownerCookie)
	clearResp := httptest.NewRecorder()
	s.handleOwnerNodesUpdate(clearResp, clearReq)
	if clearResp.Code != http.StatusOK {
		t.Fatalf("clear due date status=%d body=%s", clearResp.Code, clearResp.Body.String())
	}
	if got := s.buildMarketViews(s.store.ListOwnerListings(owner.ID), true)[0].DueTime; got != 0 {
		t.Fatalf("cleared due time=%d", got)
	}

	toggleBody, _ := json.Marshal(map[string]any{"node_id": nodeID, "for_sale": false})
	toggleReq := httptest.NewRequest(http.MethodPost, "https://monitor.example.com/api/owner/nodes/toggle", bytes.NewReader(toggleBody))
	toggleReq.Header.Set("Content-Type", "application/json")
	toggleReq.Host = "monitor.example.com"
	toggleReq.AddCookie(ownerCookie)
	toggleResp := httptest.NewRecorder()
	s.handleOwnerNodesToggle(toggleResp, toggleReq)
	if toggleResp.Code != http.StatusOK {
		t.Fatalf("toggle status=%d body=%s", toggleResp.Code, toggleResp.Body.String())
	}
	listing, ok := s.store.GetMarketListing(nodeID)
	if !ok || listing.ForSale {
		t.Fatalf("expected for_sale=false, got %+v", listing)
	}

	// Public listings should hide it.
	listReq := httptest.NewRequest(http.MethodGet, "https://monitor.example.com/api/market/listings", nil)
	listResp := httptest.NewRecorder()
	s.handleMarketListings(listResp, listReq)
	var listings []MarketListingView
	_ = json.Unmarshal(listResp.Body.Bytes(), &listings)
	if len(listings) != 0 {
		t.Fatalf("expected empty public listings, got %d", len(listings))
	}
}

func TestCaptchaOneTime(t *testing.T) {
	c := NewCaptchaStore()
	id, img, err := c.Issue()
	if err != nil || id == "" || !strings.HasPrefix(img, "data:image/png;base64,") {
		t.Fatalf("issue failed: %v id=%q", err, id)
	}
	c.mu.Lock()
	code := c.entries[id].Code
	c.mu.Unlock()
	if !c.Verify(id, code) {
		t.Fatal("first verify should pass")
	}
	if c.Verify(id, code) {
		t.Fatal("second verify should fail (one-time)")
	}
}

func TestCaptchaStoreCapacity(t *testing.T) {
	c := NewCaptchaStore()
	c.mu.Lock()
	for i := 0; i < maxCaptchaEntries; i++ {
		c.entries[fmt.Sprintf("captcha-%d", i)] = captchaEntry{Code: "TEST1", ExpiresAt: time.Now().Add(time.Minute)}
	}
	c.mu.Unlock()
	if _, _, err := c.Issue(); err == nil {
		t.Fatal("expected captcha capacity error")
	}
}

func TestParseOptionalDueDate(t *testing.T) {
	for _, value := range []string{"2026-02-30", "26-01-01", "1969-12-31", "10000-01-01"} {
		if _, err := parseOptionalDueDate(value); err == nil {
			t.Fatalf("parseOptionalDueDate(%q) unexpectedly succeeded", value)
		}
	}
	if got, err := parseOptionalDueDate(""); err != nil || got != 0 {
		t.Fatalf("empty due date = %d, %v", got, err)
	}
}

func TestJSONStoreMarketPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := hashOwnerPassword("password123")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := store.CreateOwner("a@b.com", hash)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateMarketListing(MarketListing{
		NodeID: "n_1", OwnerID: owner.ID, ForSale: true, ListingType: "rent",
		Contact: "x", Price: "1", CreatedAt: 1, UpdatedAt: 1,
	}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.GetOwnerByID(owner.ID); !ok {
		t.Fatal("owner not persisted")
	}
	if _, ok := reloaded.GetMarketListing("n_1"); !ok {
		t.Fatal("listing not persisted")
	}
}

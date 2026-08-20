package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func issueTestCaptcha(t *testing.T, captcha *CaptchaStore) (string, string) {
	t.Helper()
	id, _, err := captcha.Issue()
	if err != nil {
		t.Fatal(err)
	}
	captcha.mu.Lock()
	entry := captcha.entries[id]
	entry.Code = "ACCT1"
	captcha.entries[id] = entry
	captcha.mu.Unlock()
	return id, entry.Code
}

func accountRequest(method, target string, body any, cookie *http.Cookie) *http.Request {
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, target, bytes.NewReader(raw))
	req.Host = "monitor.example.com"
	if cookie != nil {
		req.AddCookie(cookie)
	}
	return req
}

func responseCookie(resp *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, cookie := range resp.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func TestAccountRegistrationAndPrivateNodeLifecycle(t *testing.T) {
	s := newMarketTestServer(t)
	s.metricsHistory = NewMetricsHistoryStore(filepath.Join(t.TempDir(), "metrics-history.json"))
	settings := s.store.GetSettings()
	settings.MarketEnabled = false
	settings.UserNodeLimit = 3
	if err := s.store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}

	captchaID, captchaCode := issueTestCaptcha(t, s.captcha)
	registerReq := accountRequest(http.MethodPost, "https://monitor.example.com/api/account/register", map[string]any{
		"email": "user@example.com", "password": "password123", "password_confirm": "password123",
		"captcha_id": captchaID, "captcha_code": captchaCode, "remember_me": true,
	}, nil)
	registerResp := httptest.NewRecorder()
	s.handleAccountRegister(registerResp, registerReq)
	if registerResp.Code != http.StatusCreated {
		t.Fatalf("register status=%d body=%s", registerResp.Code, registerResp.Body.String())
	}
	accountCookie := responseCookie(registerResp, "monitor_owner")
	if accountCookie == nil {
		t.Fatal("missing account session cookie")
	}
	if accountCookie.MaxAge != int(rememberedSessionTTL.Seconds()) {
		t.Fatalf("remembered account cookie max age=%d", accountCookie.MaxAge)
	}

	createReq := accountRequest(http.MethodPost, "https://monitor.example.com/api/account/nodes", map[string]any{
		"display_name": "私有测试节点", "region": "香港", "due_date": "2028-08-16",
	}, accountCookie)
	createResp := httptest.NewRecorder()
	s.handleOwnerNodes(createResp, createReq)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create node status=%d body=%s", createResp.Code, createResp.Body.String())
	}
	var created map[string]any
	decodeJSONResponse(t, createResp, &created)
	nodeID, _ := created["node_id"].(string)
	if !validNodeID(nodeID) || created["linux"] == "" || created["windows"] == "" {
		t.Fatalf("invalid create response: %#v", created)
	}
	planned, ok := s.store.GetPlannedNode(nodeID)
	if !ok || planned.OwnerID == "" || !planned.Private {
		t.Fatalf("planned node ownership/privacy not saved: %#v", planned)
	}

	listReq := accountRequest(http.MethodGet, "https://monitor.example.com/api/account/nodes", nil, accountCookie)
	listResp := httptest.NewRecorder()
	s.handleOwnerNodes(listResp, listReq)
	var nodes []accountNodeView
	decodeJSONResponse(t, listResp, &nodes)
	if len(nodes) != 1 || nodes[0].NodeID != nodeID || !nodes[0].Private || nodes[0].HasListing {
		t.Fatalf("account nodes=%#v", nodes)
	}

	if err := s.store.UpsertReport(sampleMetrics(nodeID, 1024, 2048), s.cfg.MaxNodes); err != nil {
		t.Fatal(err)
	}
	var publicHosts []AkileHost
	if err := json.Unmarshal(s.cachedHostsJSON(), &publicHosts); err != nil {
		t.Fatal(err)
	}
	if len(publicHosts) != 0 {
		t.Fatalf("private node leaked into public hosts: %#v", publicHosts)
	}
	infoReq := httptest.NewRequest(http.MethodGet, "https://monitor.example.com/info", nil)
	infoResp := httptest.NewRecorder()
	s.handleInfo(infoResp, infoReq)
	var publicInfos []HostInfo
	decodeJSONResponse(t, infoResp, &publicInfos)
	if len(publicInfos) != 0 {
		t.Fatalf("private node leaked into public info: %#v", publicInfos)
	}
	publicHistoryReq := httptest.NewRequest(http.MethodGet, "https://monitor.example.com/api/nodes/history?node_id="+nodeID+"&range=1h", nil)
	publicHistoryResp := httptest.NewRecorder()
	s.handleMetricsHistory(publicHistoryResp, publicHistoryReq)
	if publicHistoryResp.Code != http.StatusNotFound {
		t.Fatalf("public private-node history status=%d", publicHistoryResp.Code)
	}
	accountHistoryReq := accountRequest(http.MethodGet, "https://monitor.example.com/api/account/nodes/history?node_id="+nodeID+"&range=1h", nil, accountCookie)
	accountHistoryResp := httptest.NewRecorder()
	s.handleAccountNodeHistory(accountHistoryResp, accountHistoryReq)
	if accountHistoryResp.Code != http.StatusOK {
		t.Fatalf("account history status=%d body=%s", accountHistoryResp.Code, accountHistoryResp.Body.String())
	}

	listingReq := accountRequest(http.MethodPost, "https://monitor.example.com/api/account/nodes/listing", map[string]any{
		"node_id": nodeID, "listing_type": "sale", "contact": "TG @seller",
		"price_amount": 9.9, "price_currency": "USD", "billing_cycle": "monthly",
	}, accountCookie)
	listingResp := httptest.NewRecorder()
	s.handleAccountNodeListing(listingResp, listingReq)
	if listingResp.Code != http.StatusServiceUnavailable {
		t.Fatalf("listing should follow disabled market: status=%d body=%s", listingResp.Code, listingResp.Body.String())
	}

	privacyReq := accountRequest(http.MethodPost, "https://monitor.example.com/api/account/nodes/privacy", map[string]any{"node_id": nodeID, "private": false}, accountCookie)
	privacyResp := httptest.NewRecorder()
	s.handleAccountNodePrivacy(privacyResp, privacyReq)
	if privacyResp.Code != http.StatusOK {
		t.Fatalf("privacy status=%d body=%s", privacyResp.Code, privacyResp.Body.String())
	}
	var visible []AkileHost
	if err := json.Unmarshal(s.cachedHostsJSON(), &visible); err != nil {
		t.Fatal(err)
	}
	if len(visible) != 1 || visible[0].Host.Name != nodeID {
		t.Fatalf("public node missing after visibility change: %#v", visible)
	}
}

func TestOwnerOwnsNodeUsesPlannedOwnershipOnly(t *testing.T) {
	s := newMarketTestServer(t)
	store := s.store.(*Store)
	store.mu.Lock()
	store.Planned["ownership-node"] = PlannedNode{NodeID: "ownership-node", OwnerID: "planned-owner"}
	store.Listings["ownership-node"] = MarketListing{NodeID: "ownership-node", OwnerID: "listing-owner"}
	store.mu.Unlock()
	if s.ownerOwnsNode("listing-owner", "ownership-node") {
		t.Fatal("listing owner gained control of another owner's planned node")
	}
	if !s.ownerOwnsNode("planned-owner", "ownership-node") {
		t.Fatal("planned node owner lost node access")
	}
}

func TestAccountNodeOwnershipAndQuota(t *testing.T) {
	s := newMarketTestServer(t)
	hash, err := hashOwnerPassword("password123")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := s.store.CreateOwner("quota@example.com", hash)
	if err != nil {
		t.Fatal(err)
	}
	token, err := s.sessions.CreateFor(SessionKindOwner, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	cookie := &http.Cookie{Name: "monitor_owner", Value: token}
	settings := s.store.GetSettings()
	settings.UserNodeLimit = 1
	if err := s.store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		req := accountRequest(http.MethodPost, "https://monitor.example.com/api/account/nodes", map[string]any{"display_name": "quota node"}, cookie)
		resp := httptest.NewRecorder()
		s.handleOwnerNodes(resp, req)
		if index == 0 && resp.Code != http.StatusCreated {
			t.Fatalf("first create status=%d body=%s", resp.Code, resp.Body.String())
		}
		if index == 1 && resp.Code != http.StatusTooManyRequests {
			t.Fatalf("quota create status=%d body=%s", resp.Code, resp.Body.String())
		}
	}
}

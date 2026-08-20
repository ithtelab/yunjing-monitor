package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMarketOrderIntentLifecycleAndOwnership(t *testing.T) {
	s, store := newPlatformTestServer(t)
	hash, err := hashOwnerPassword("password123")
	if err != nil {
		t.Fatal(err)
	}
	seller, err := store.CreateOwner("seller-orders@example.com", hash)
	if err != nil {
		t.Fatal(err)
	}
	buyer, err := store.CreateOwner("buyer-orders@example.com", hash)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateOwnerNode(seller.ID, PlannedNode{NodeID: "order-node", Private: false}, HostInfo{}, "agent-token", accountAudit(seller.ID, "node.create", "order-node"), 20, 5); err != nil {
		t.Fatal(err)
	}
	listing := MarketListing{NodeID: "order-node", OwnerID: seller.ID, ForSale: true, ListingType: "sale", Price: "100 CNY", ModerationStatus: "approved"}
	if err := store.CreateOwnerListing(seller.ID, listing, accountAudit(seller.ID, "listing.create", "order-node")); err != nil {
		t.Fatal(err)
	}
	buyerToken, _ := s.sessions.CreateFor(SessionKindOwner, buyer.ID)
	sellerToken, _ := s.sessions.CreateFor(SessionKindOwner, seller.ID)
	buyerCookie := &http.Cookie{Name: "monitor_owner", Value: buyerToken}
	sellerCookie := &http.Cookie{Name: "monitor_owner", Value: sellerToken}

	create := accountRequest(http.MethodPost, "https://monitor.example.com/api/account/orders", map[string]any{
		"listing_node_id": "order-node", "buyer_contact": "tg:@buyer", "message": "I would like to buy this server.",
	}, buyerCookie)
	createResp := httptest.NewRecorder()
	s.handleAccountOrders(createResp, create)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createResp.Code, createResp.Body.String())
	}
	if len(s.features.data.MarketOrderIntents) != 1 || s.features.data.MarketOrderIntents[0].Status != "pending" {
		t.Fatalf("orders=%#v", s.features.data.MarketOrderIntents)
	}
	orderID := s.features.data.MarketOrderIntents[0].ID

	duplicate := accountRequest(http.MethodPost, "https://monitor.example.com/api/account/orders", map[string]any{
		"listing_node_id": "order-node", "buyer_contact": "tg:@buyer",
	}, buyerCookie)
	duplicateResp := httptest.NewRecorder()
	s.handleAccountOrders(duplicateResp, duplicate)
	if duplicateResp.Code != http.StatusConflict {
		t.Fatalf("duplicate status=%d", duplicateResp.Code)
	}

	buyerAccept := accountRequest(http.MethodPost, "https://monitor.example.com/api/account/orders/action", map[string]any{"order_id": orderID, "action": "accept"}, buyerCookie)
	buyerAcceptResp := httptest.NewRecorder()
	s.handleAccountOrderAction(buyerAcceptResp, buyerAccept)
	if buyerAcceptResp.Code != http.StatusConflict {
		t.Fatalf("buyer accept status=%d", buyerAcceptResp.Code)
	}

	sellerAccept := accountRequest(http.MethodPost, "https://monitor.example.com/api/account/orders/action", map[string]any{"order_id": orderID, "action": "accept"}, sellerCookie)
	sellerAcceptResp := httptest.NewRecorder()
	s.handleAccountOrderAction(sellerAcceptResp, sellerAccept)
	if sellerAcceptResp.Code != http.StatusOK || s.features.data.MarketOrderIntents[0].Status != "accepted" {
		t.Fatalf("seller accept status=%d order=%#v", sellerAcceptResp.Code, s.features.data.MarketOrderIntents[0])
	}

	buyerCancel := accountRequest(http.MethodPost, "https://monitor.example.com/api/account/orders/action", map[string]any{"order_id": orderID, "action": "cancel"}, buyerCookie)
	buyerCancelResp := httptest.NewRecorder()
	s.handleAccountOrderAction(buyerCancelResp, buyerCancel)
	if buyerCancelResp.Code != http.StatusOK || s.features.data.MarketOrderIntents[0].Status != "cancelled" {
		t.Fatalf("buyer cancel status=%d order=%#v", buyerCancelResp.Code, s.features.data.MarketOrderIntents[0])
	}

	list := accountRequest(http.MethodGet, "https://monitor.example.com/api/account/orders", nil, sellerCookie)
	listResp := httptest.NewRecorder()
	s.handleAccountOrders(listResp, list)
	if listResp.Code != http.StatusOK || !containsBody(listResp.Body.String(), orderID) {
		t.Fatalf("seller list status=%d body=%s", listResp.Code, listResp.Body.String())
	}
}

func containsBody(body, value string) bool {
	for i := 0; i+len(value) <= len(body); i++ {
		if body[i:i+len(value)] == value {
			return true
		}
	}
	return false
}

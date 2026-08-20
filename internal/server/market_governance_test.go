package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func governanceAudit(action, objectType, objectID string) AuditEvent {
	return AuditEvent{ActorType: "admin", ActorID: "admin", Action: action, ObjectType: objectType, ObjectID: objectID, Summary: action}
}

func TestMarketGovernanceAcrossStores(t *testing.T) {
	for _, tc := range reliabilityStores(t) {
		t.Run(tc.name, func(t *testing.T) {
			hash, err := hashOwnerPassword("old-password")
			if err != nil {
				t.Fatal(err)
			}
			sub := testSubmission("n_governance", "o_governance", "governance@example.com")
			sub.Owner.PasswordHash = hash
			if err := tc.store.CreateMarketSubmission(sub, 10); err != nil {
				t.Fatal(err)
			}
			if listing, _ := tc.store.GetMarketListing(sub.Node.NodeID); listing.ModerationStatus != "approved" {
				t.Fatalf("default moderation=%q", listing.ModerationStatus)
			}

			if err := tc.store.ModerateMarketListing(ListingModerationMutation{NodeID: sub.Node.NodeID, Status: "rejected", RejectionReason: "Missing proof", Audit: governanceAudit("listing.moderate", "market_listing", sub.Node.NodeID)}); err != nil {
				t.Fatal(err)
			}
			if public := tc.store.ListMarketListings(true); len(public) != 0 {
				t.Fatalf("rejected public listings=%d", len(public))
			}
			owned := tc.store.ListOwnerListings(sub.Owner.ID)
			if len(owned) != 1 || owned[0].ModerationStatus != "rejected" || owned[0].RejectionReason != "Missing proof" {
				t.Fatalf("owner listings=%#v", owned)
			}
			description := "Corrected listing"
			if err := tc.store.UpdateOwnerNode(sub.Owner.ID, OwnerNodeMutation{NodeID: sub.Node.NodeID, Listing: MarketListingUpdate{Description: &description}, Audit: governanceAudit("owner.node.update", "market_listing", sub.Node.NodeID)}); err != nil {
				t.Fatal(err)
			}
			if listing, _ := tc.store.GetMarketListing(sub.Node.NodeID); listing.ModerationStatus != "pending" || listing.RejectionReason != "" {
				t.Fatalf("corrected listing moderation=%q reason=%q", listing.ModerationStatus, listing.RejectionReason)
			}
			if public := tc.store.ListMarketListings(true); len(public) != 0 {
				t.Fatalf("pending corrected listings=%d", len(public))
			}
			if err := tc.store.ModerateMarketListing(ListingModerationMutation{NodeID: sub.Node.NodeID, Status: "approved", Audit: governanceAudit("listing.moderate", "market_listing", sub.Node.NodeID)}); err != nil {
				t.Fatal(err)
			}
			if public := tc.store.ListMarketListings(true); len(public) != 1 || public[0].RejectionReason != "" {
				t.Fatalf("approved listings=%#v", public)
			}
			contact := "updated-contact"
			if err := tc.store.UpdateOwnerNode(sub.Owner.ID, OwnerNodeMutation{NodeID: sub.Node.NodeID, Listing: MarketListingUpdate{Contact: &contact}, Audit: governanceAudit("owner.node.update", "market_listing", sub.Node.NodeID)}); err != nil {
				t.Fatal(err)
			}
			if listing, _ := tc.store.GetMarketListing(sub.Node.NodeID); listing.ModerationStatus != "pending" {
				t.Fatalf("approved listing edit kept moderation=%q", listing.ModerationStatus)
			}

			disabled := true
			if err := tc.store.UpdateOwnerAdmin(OwnerAdminMutation{OwnerID: sub.Owner.ID, Disabled: &disabled, Audit: governanceAudit("owner.status", "owner", sub.Owner.ID)}); err != nil {
				t.Fatal(err)
			}
			if _, err := tc.store.AuthenticateOwner(sub.Owner.Email, "old-password"); err == nil || !strings.Contains(err.Error(), "disabled") {
				t.Fatalf("disabled login err=%v", err)
			}
			owners, total := tc.store.ListOwners("governance", 10, 0)
			if total != 1 || len(owners) != 1 || !owners[0].Disabled {
				t.Fatalf("owners=%#v total=%d", owners, total)
			}
			disabled = false
			if err := tc.store.UpdateOwnerAdmin(OwnerAdminMutation{OwnerID: sub.Owner.ID, Disabled: &disabled, Audit: governanceAudit("owner.status", "owner", sub.Owner.ID)}); err != nil {
				t.Fatal(err)
			}
			newHash, err := hashOwnerPassword("new-password")
			if err != nil {
				t.Fatal(err)
			}
			if err := tc.store.UpdateOwnerAdmin(OwnerAdminMutation{OwnerID: sub.Owner.ID, PasswordHash: &newHash, Audit: governanceAudit("owner.password.reset", "owner", sub.Owner.ID)}); err != nil {
				t.Fatal(err)
			}
			if _, err := tc.store.AuthenticateOwner(sub.Owner.Email, "old-password"); err == nil {
				t.Fatal("old password still works")
			}
			if _, err := tc.store.AuthenticateOwner(sub.Owner.Email, "new-password"); err != nil {
				t.Fatalf("new password: %v", err)
			}
			if len(tc.store.ListAuditEvents(50)) < 6 {
				t.Fatalf("audit events=%d", len(tc.store.ListAuditEvents(50)))
			}
		})
	}
}

func TestMarketGovernanceAdminAPIAndDisabledSession(t *testing.T) {
	s := newMarketTestServer(t)
	hash, err := hashOwnerPassword("owner-password")
	if err != nil {
		t.Fatal(err)
	}
	sub := testSubmission("n_governance_api", "o_governance_api", "api-governance@example.com")
	sub.Owner.PasswordHash = hash
	if err := s.store.CreateMarketSubmission(sub, 10); err != nil {
		t.Fatal(err)
	}
	adminToken, err := s.sessions.Create()
	if err != nil {
		t.Fatal(err)
	}
	ownerToken, err := s.sessions.CreateFor(SessionKindOwner, sub.Owner.ID)
	if err != nil {
		t.Fatal(err)
	}

	moderate := adminRequestWithBody(http.MethodPost, "https://monitor.example.com/api/admin/market/moderate", adminToken, `{"node_id":"n_governance_api","status":"rejected","rejection_reason":"Needs verification"}`)
	moderateResp := httptest.NewRecorder()
	s.handleAdminMarketModerate(moderateResp, moderate)
	if moderateResp.Code != http.StatusOK {
		t.Fatalf("moderate status=%d body=%s", moderateResp.Code, moderateResp.Body.String())
	}
	if len(s.store.ListMarketListings(true)) != 0 {
		t.Fatal("rejected listing is public")
	}
	ownerNodes := httptest.NewRequest(http.MethodGet, "https://monitor.example.com/api/owner/nodes", nil)
	ownerNodes.AddCookie(&http.Cookie{Name: "monitor_owner", Value: ownerToken})
	ownerNodesResp := httptest.NewRecorder()
	s.handleOwnerNodes(ownerNodesResp, ownerNodes)
	if ownerNodesResp.Code != http.StatusOK || !strings.Contains(ownerNodesResp.Body.String(), `"moderation_status":"rejected"`) || !strings.Contains(ownerNodesResp.Body.String(), "Needs verification") {
		t.Fatalf("owner nodes status=%d body=%s", ownerNodesResp.Code, ownerNodesResp.Body.String())
	}

	list := httptest.NewRequest(http.MethodGet, "https://monitor.example.com/api/admin/market/owners?q=api-governance&page=1&page_size=10", nil)
	list.AddCookie(&http.Cookie{Name: "monitor_admin", Value: adminToken})
	listResp := httptest.NewRecorder()
	s.handleAdminMarketOwners(listResp, list)
	if listResp.Code != http.StatusOK || strings.Contains(listResp.Body.String(), "password_hash") || strings.Contains(listResp.Body.String(), hash) {
		t.Fatalf("owner list status=%d body=%s", listResp.Code, listResp.Body.String())
	}

	disable := adminRequestWithBody(http.MethodPost, "https://monitor.example.com/api/admin/market/owners/status", adminToken, `{"owner_id":"o_governance_api","disabled":true}`)
	disableResp := httptest.NewRecorder()
	s.handleAdminMarketOwnerStatus(disableResp, disable)
	if disableResp.Code != http.StatusOK {
		t.Fatalf("disable status=%d body=%s", disableResp.Code, disableResp.Body.String())
	}
	if s.sessions.Valid(ownerToken) {
		t.Fatal("disabled owner session was not revoked")
	}
	me := httptest.NewRequest(http.MethodGet, "https://monitor.example.com/api/owner/me", nil)
	me.AddCookie(&http.Cookie{Name: "monitor_owner", Value: ownerToken})
	meResp := httptest.NewRecorder()
	s.handleOwnerMe(meResp, me)
	if meResp.Code != http.StatusOK || !strings.Contains(meResp.Body.String(), `"authenticated":false`) {
		t.Fatalf("disabled me status=%d body=%s", meResp.Code, meResp.Body.String())
	}
	login := adminRequestWithBody(http.MethodPost, "https://monitor.example.com/api/owner/login", "", `{"email":"api-governance@example.com","password":"owner-password"}`)
	loginResp := httptest.NewRecorder()
	s.handleOwnerLogin(loginResp, login)
	if loginResp.Code != http.StatusForbidden {
		t.Fatalf("disabled login status=%d body=%s", loginResp.Code, loginResp.Body.String())
	}

	reset := adminRequestWithBody(http.MethodPost, "https://monitor.example.com/api/admin/market/owners/reset-password", adminToken, `{"owner_id":"o_governance_api","password":"short"}`)
	resetResp := httptest.NewRecorder()
	s.handleAdminMarketOwnerResetPassword(resetResp, reset)
	if resetResp.Code != http.StatusBadRequest {
		t.Fatalf("short reset status=%d", resetResp.Code)
	}
	if events := s.store.ListAuditEvents(20); len(events) < 3 {
		t.Fatalf("audit events=%d", len(events))
	}
}

func TestGovernanceFieldsMigrateJSONToSQLite(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "server.json")
	store, err := NewStore(jsonPath, "governance-migration-secret")
	if err != nil {
		t.Fatal(err)
	}
	hash, _ := hashOwnerPassword("owner-password")
	sub := testSubmission("n_governance_migrate", "o_governance_migrate", "migration-governance@example.com")
	sub.Owner.PasswordHash = hash
	if err := store.CreateMarketSubmission(sub, 10); err != nil {
		t.Fatal(err)
	}
	disabled := true
	if err := store.UpdateOwnerAdmin(OwnerAdminMutation{OwnerID: sub.Owner.ID, Disabled: &disabled, Audit: governanceAudit("owner.status", "owner", sub.Owner.ID)}); err != nil {
		t.Fatal(err)
	}
	if err := store.ModerateMarketListing(ListingModerationMutation{NodeID: sub.Node.NodeID, Status: "rejected", RejectionReason: "Migration reason", Audit: governanceAudit("listing.moderate", "market_listing", sub.Node.NodeID)}); err != nil {
		t.Fatal(err)
	}
	sqliteStore, err := NewSQLiteStore(filepath.Join(dir, "server.db"), jsonPath, "governance-migration-secret")
	if err != nil {
		t.Fatal(err)
	}
	defer sqliteStore.Close()
	owner, ok := sqliteStore.GetOwnerByID(sub.Owner.ID)
	if !ok || !owner.Disabled {
		t.Fatalf("owner=%#v ok=%v", owner, ok)
	}
	listing, ok := sqliteStore.GetMarketListing(sub.Node.NodeID)
	if !ok || listing.ModerationStatus != "rejected" || listing.RejectionReason != "Migration reason" {
		t.Fatalf("listing=%#v ok=%v", listing, ok)
	}
}

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func reliabilityStores(t *testing.T) []struct {
	name  string
	store dataStore
} {
	t.Helper()
	jsonStore, err := NewStore(filepath.Join(t.TempDir(), "server.json"), "reliability-test-secret")
	if err != nil {
		t.Fatal(err)
	}
	sqliteStore, err := NewSQLiteStore(filepath.Join(t.TempDir(), "server.db"), "", "reliability-test-secret")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqliteStore.Close() })
	return []struct {
		name  string
		store dataStore
	}{{"json", jsonStore}, {"sqlite", sqliteStore}}
}

func testSubmission(nodeID, ownerID, email string) MarketSubmission {
	now := time.Now().Unix()
	return MarketSubmission{
		Owner: Owner{ID: ownerID, Email: email, PasswordHash: "hash", CreatedAt: now, LastLogin: now}, NewOwner: true,
		Node:       PlannedNode{NodeID: nodeID, CreatedAt: now, TokenHash: hashToken("agent-token"), DisplayName: "Original", Region: "Hong Kong", RegionCode: "HK"},
		Info:       HostInfo{Name: nodeID, DisplayName: "Original", Region: "Hong Kong", RegionCode: "HK", Price: "USD 10", PriceAmount: 10, PriceCurrency: "USD", BillingCycle: "monthly"},
		Listing:    MarketListing{NodeID: nodeID, OwnerID: ownerID, ForSale: true, ListingType: "rent", Contact: "tg:test", Price: "USD 10", PriceAmount: 10, PriceCurrency: "USD", BillingCycle: "monthly", CreatedAt: now, UpdatedAt: now},
		AgentToken: "agent-token", Audit: AuditEvent{ActorType: "owner", ActorID: ownerID, Action: "market.submit", ObjectType: "market_listing", ObjectID: nodeID, IP: "127.0.0.1", Summary: "created"},
	}
}

func TestMarketReliabilityAtomicMutationRecycleAndAudit(t *testing.T) {
	for _, tc := range reliabilityStores(t) {
		t.Run(tc.name, func(t *testing.T) {
			sub := testSubmission("n_reliable", "o_reliable", "owner@example.com")
			if err := tc.store.CreateMarketSubmission(sub, 10); err != nil {
				t.Fatal(err)
			}
			if token, ok := tc.store.GetNodeToken(sub.Node.NodeID); !ok || token != "agent-token" {
				t.Fatalf("token=%q ok=%v", token, ok)
			}
			failed := testSubmission("n_overflow", "o_rollback", "rollback@example.com")
			if err := tc.store.CreateMarketSubmission(failed, 1); err == nil {
				t.Fatal("expected max node failure")
			}
			if _, ok := tc.store.GetOwnerByID("o_rollback"); ok {
				t.Fatal("failed atomic submission left owner behind")
			}
			invalidAudit := testSubmission("n_bad_audit", "o_bad_audit", "bad-audit@example.com")
			invalidAudit.Audit.Action = ""
			if err := tc.store.CreateMarketSubmission(invalidAudit, 10); err == nil {
				t.Fatal("expected invalid audit failure")
			}
			if _, ok := tc.store.GetOwnerByID("o_bad_audit"); ok {
				t.Fatal("audit failure left owner behind")
			}

			name, region, code := "Updated", "Singapore", "SG"
			due := int64(1893456000000)
			amount := 25.0
			price := "USD 25"
			currency := "USD"
			cycle := "monthly"
			mutation := OwnerNodeMutation{NodeID: sub.Node.NodeID, DisplayName: &name, Region: &region, RegionCode: &code, DueTime: &due, Billing: &NodeBillingUpdate{Price: price, LegacyCycle: "month", PriceAmount: amount, PriceCurrency: currency, BillingCycle: cycle}, Listing: MarketListingUpdate{Price: &price, PriceAmount: &amount, PriceCurrency: &currency, BillingCycle: &cycle}, Audit: AuditEvent{ActorType: "owner", ActorID: sub.Owner.ID, Action: "listing.update", ObjectType: "market_listing", ObjectID: sub.Node.NodeID, Summary: "updated"}}
			if err := tc.store.UpdateOwnerNode(sub.Owner.ID, mutation); err != nil {
				t.Fatal(err)
			}
			listing, ok := tc.store.GetMarketListing(sub.Node.NodeID)
			if !ok || listing.PriceAmount != 25 {
				t.Fatalf("listing=%#v ok=%v", listing, ok)
			}
			nodes := tc.store.AdminNodes(time.Minute)
			if len(nodes) != 1 || nodes[0].DisplayName != "Updated" || nodes[0].Info.DueTime != due {
				t.Fatalf("nodes=%#v", nodes)
			}

			audit := AuditEvent{ActorType: "admin", ActorID: "admin", Action: "node.trash", ObjectType: "node", ObjectID: sub.Node.NodeID, Summary: "trash"}
			record, err := tc.store.TrashNode(sub.Node.NodeID, audit)
			if err != nil {
				t.Fatal(err)
			}
			if record.Kind != "node" || len(tc.store.ListRecycleRecords()) != 1 {
				t.Fatalf("record=%#v", record)
			}
			if _, ok := tc.store.GetMarketListing(sub.Node.NodeID); ok {
				t.Fatal("listing remained active after trash")
			}
			if err := tc.store.RestoreRecycleRecord(record.ID, AuditEvent{ActorType: "admin", ActorID: "admin", Action: "recycle.restore", ObjectType: "recycle_record", ObjectID: record.ID, Summary: "restore"}); err != nil {
				t.Fatal(err)
			}
			if token, ok := tc.store.GetNodeToken(sub.Node.NodeID); !ok || token != "agent-token" {
				t.Fatalf("restored token=%q ok=%v", token, ok)
			}
			if len(tc.store.ListRecycleRecords()) != 0 {
				t.Fatal("restored record remained in recycle bin")
			}
			events := tc.store.ListAuditEvents(20)
			if len(events) < 4 {
				t.Fatalf("audit events=%d", len(events))
			}
		})
	}
}

func TestRecycleAdminAPIHidesPayloadAndRequiresOrigin(t *testing.T) {
	s := newMarketTestServer(t)
	sub := testSubmission("n_api", "o_api", "api@example.com")
	if err := s.store.CreateMarketSubmission(sub, 10); err != nil {
		t.Fatal(err)
	}
	record, err := s.store.TrashNode(sub.Node.NodeID, AuditEvent{ActorType: "admin", ActorID: "admin", Action: "node.trash", ObjectType: "node", ObjectID: sub.Node.NodeID, Summary: "trash"})
	if err != nil {
		t.Fatal(err)
	}
	token, err := s.sessions.Create()
	if err != nil {
		t.Fatal(err)
	}
	get := httptest.NewRequest(http.MethodGet, "https://monitor.example.com/api/admin/recycle-bin", nil)
	get.AddCookie(&http.Cookie{Name: "monitor_admin", Value: token})
	resp := httptest.NewRecorder()
	s.handleAdminRecycleBin(resp, get)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if json.Valid(resp.Body.Bytes()) == false {
		t.Fatal("invalid json")
	}
	var body []map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 1 || body[0]["node"] != nil || body[0]["listing"] != nil {
		t.Fatalf("payload leaked: %s", resp.Body.String())
	}

	bad := adminRequestWithBody(http.MethodPost, "https://monitor.example.com/api/admin/recycle-bin/restore", token, `{"id":"`+record.ID+`"}`)
	bad.Header.Set("Origin", "https://evil.example.com")
	badResp := httptest.NewRecorder()
	s.handleAdminRecycleRestore(badResp, bad)
	if badResp.Code != http.StatusForbidden {
		t.Fatalf("missing origin status=%d", badResp.Code)
	}
	good := adminRequestWithBody(http.MethodPost, "https://monitor.example.com/api/admin/recycle-bin/restore", token, `{"id":"`+record.ID+`"}`)
	goodResp := httptest.NewRecorder()
	s.handleAdminRecycleRestore(goodResp, good)
	if goodResp.Code != http.StatusOK {
		t.Fatalf("restore status=%d body=%s", goodResp.Code, goodResp.Body.String())
	}
}

func TestRecycleAndAuditMigrateFromJSONToSQLite(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "server.json")
	jsonStore, err := NewStore(jsonPath, "reliability-test-secret")
	if err != nil {
		t.Fatal(err)
	}
	sub := testSubmission("n_migrate_recycle", "o_migrate_recycle", "migrate@example.com")
	if err := jsonStore.CreateMarketSubmission(sub, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := jsonStore.TrashMarketListing(sub.Node.NodeID, AuditEvent{ActorType: "admin", ActorID: "admin", Action: "listing.trash", ObjectType: "market_listing", ObjectID: sub.Node.NodeID, Summary: "migrate"}); err != nil {
		t.Fatal(err)
	}
	sqliteStore, err := NewSQLiteStore(filepath.Join(dir, "server.db"), jsonPath, "reliability-test-secret")
	if err != nil {
		t.Fatal(err)
	}
	defer sqliteStore.Close()
	if len(sqliteStore.ListRecycleRecords()) != 1 {
		t.Fatalf("recycle records=%d", len(sqliteStore.ListRecycleRecords()))
	}
	if len(sqliteStore.ListAuditEvents(20)) < 2 {
		t.Fatalf("audit events=%d", len(sqliteStore.ListAuditEvents(20)))
	}
}

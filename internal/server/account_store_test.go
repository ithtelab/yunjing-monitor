package server

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func accountStoreBackends(t *testing.T) []struct {
	name  string
	store dataStore
} {
	t.Helper()
	jsonStore, err := NewStore(filepath.Join(t.TempDir(), "server.json"), "account-store-test-secret")
	if err != nil {
		t.Fatal(err)
	}
	sqliteStore, err := NewSQLiteStore(filepath.Join(t.TempDir(), "server.db"), "", "account-store-test-secret")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqliteStore.Close() })
	return []struct {
		name  string
		store dataStore
	}{{name: "json", store: jsonStore}, {name: "sqlite", store: sqliteStore}}
}

func accountAudit(actorID, action, objectID string) AuditEvent {
	return AuditEvent{
		ActorType:  "owner",
		ActorID:    actorID,
		Action:     action,
		ObjectType: "node",
		ObjectID:   objectID,
		Summary:    action,
	}
}

func TestAccountSettingsPersistAcrossStores(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "server.json")
		store, err := NewStore(path)
		if err != nil {
			t.Fatal(err)
		}
		settings := store.GetSettings()
		if settings.UserNodeLimit != 3 || settings.UserRegistrationDisabled || settings.UserNodeApplyDisabled {
			t.Fatalf("default settings = %#v", settings)
		}
		settings.UserRegistrationDisabled = true
		settings.UserNodeApplyDisabled = true
		settings.UserNodeLimit = 7
		if err := store.UpdateSettings(settings); err != nil {
			t.Fatal(err)
		}
		reloaded, err := NewStore(path)
		if err != nil {
			t.Fatal(err)
		}
		got := reloaded.GetSettings()
		if !got.UserRegistrationDisabled || !got.UserNodeApplyDisabled || got.UserNodeLimit != 7 {
			t.Fatalf("reloaded settings = %#v", got)
		}
	})

	t.Run("sqlite", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "server.db")
		store, err := NewSQLiteStore(path, "")
		if err != nil {
			t.Fatal(err)
		}
		settings := store.GetSettings()
		if settings.UserNodeLimit != 3 || settings.UserRegistrationDisabled || settings.UserNodeApplyDisabled {
			t.Fatalf("default settings = %#v", settings)
		}
		settings.UserRegistrationDisabled = true
		settings.UserNodeApplyDisabled = true
		settings.UserNodeLimit = 7
		if err := store.UpdateSettings(settings); err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		reloaded, err := NewSQLiteStore(path, "")
		if err != nil {
			t.Fatal(err)
		}
		defer reloaded.Close()
		got := reloaded.GetSettings()
		if !got.UserRegistrationDisabled || !got.UserNodeApplyDisabled || got.UserNodeLimit != 7 {
			t.Fatalf("reloaded settings = %#v", got)
		}
	})
}

func TestOwnerNodeLifecycleAcrossStores(t *testing.T) {
	for _, tc := range accountStoreBackends(t) {
		t.Run(tc.name, func(t *testing.T) {
			owner, err := tc.store.CreateOwner("owner@example.com", "password-hash")
			if err != nil {
				t.Fatal(err)
			}
			other, err := tc.store.CreateOwner("other@example.com", "password-hash")
			if err != nil {
				t.Fatal(err)
			}
			const nodeID = "owner-node-1"
			node := PlannedNode{NodeID: nodeID, Private: true, DisplayName: "Original", Region: "Hong Kong", RegionCode: "HK"}
			if err := tc.store.CreateOwnerNode(owner.ID, node, HostInfo{DueTime: 100}, "agent-token", accountAudit(owner.ID, "node.create", nodeID), 10, 1); err != nil {
				t.Fatal(err)
			}
			planned, ok := tc.store.GetPlannedNode(nodeID)
			if !ok || planned.OwnerID != owner.ID || !planned.Private || planned.TokenHash != hashToken("agent-token") {
				t.Fatalf("planned = %#v, ok = %v", planned, ok)
			}
			if token, ok := tc.store.GetNodeToken(nodeID); !ok || token != "agent-token" {
				t.Fatalf("token = %q, ok = %v", token, ok)
			}
			ids := tc.store.ListOwnerNodeIDs(owner.ID)
			if len(ids) != 1 || ids[0] != nodeID {
				t.Fatalf("owner node ids = %#v", ids)
			}
			adminNodes := tc.store.AdminNodes(time.Minute)
			if len(adminNodes) != 1 || adminNodes[0].OwnerID != owner.ID || !adminNodes[0].Private {
				t.Fatalf("admin nodes = %#v", adminNodes)
			}
			if err := tc.store.CreateOwnerNode(owner.ID, PlannedNode{NodeID: "owner-node-2"}, HostInfo{}, "second-token", accountAudit(owner.ID, "node.create", "owner-node-2"), 10, 1); err == nil {
				t.Fatal("expected owner node limit error")
			}

			name := "Updated without listing"
			due := int64(200)
			if err := tc.store.UpdateOwnerNode(owner.ID, OwnerNodeMutation{NodeID: nodeID, DisplayName: &name, DueTime: &due, Audit: accountAudit(owner.ID, "node.update", nodeID)}); err != nil {
				t.Fatal(err)
			}
			if err := tc.store.UpdateOwnerNode(other.ID, OwnerNodeMutation{NodeID: nodeID, DisplayName: &name, Audit: accountAudit(other.ID, "node.update", nodeID)}); err == nil {
				t.Fatal("expected cross-owner update rejection")
			}
			if err := tc.store.SetNodePrivacy(other.ID, nodeID, false, accountAudit(other.ID, "node.privacy", nodeID)); err == nil {
				t.Fatal("expected cross-owner privacy rejection")
			}
			if err := tc.store.SetNodePrivacy(owner.ID, nodeID, false, accountAudit(owner.ID, "node.privacy", nodeID)); err != nil {
				t.Fatal(err)
			}
			planned, _ = tc.store.GetPlannedNode(nodeID)
			if planned.Private || planned.DisplayName != name {
				t.Fatalf("updated planned = %#v", planned)
			}
			adminNodes = tc.store.AdminNodes(time.Minute)
			if len(adminNodes) != 1 || adminNodes[0].Info.DueTime != due {
				t.Fatalf("updated admin nodes = %#v", adminNodes)
			}

			listing := MarketListing{NodeID: nodeID, ForSale: true, ListingType: "sale", Contact: "tg:test"}
			if err := tc.store.CreateOwnerListing(other.ID, listing, accountAudit(other.ID, "listing.create", nodeID)); err == nil {
				t.Fatal("expected cross-owner listing rejection")
			}
			if err := tc.store.CreateOwnerListing(owner.ID, listing, accountAudit(owner.ID, "listing.create", nodeID)); err != nil {
				t.Fatal(err)
			}
			gotListing, ok := tc.store.GetMarketListing(nodeID)
			if !ok || gotListing.OwnerID != owner.ID {
				t.Fatalf("listing = %#v, ok = %v", gotListing, ok)
			}
			if err := tc.store.SetNodePrivacy(owner.ID, nodeID, true, accountAudit(owner.ID, "node.privacy", nodeID)); err != nil {
				t.Fatal(err)
			}
			record, err := tc.store.TrashNode(nodeID, accountAudit(owner.ID, "node.trash", nodeID))
			if err != nil {
				t.Fatal(err)
			}
			if err := tc.store.RestoreRecycleRecord(record.ID, accountAudit(owner.ID, "node.restore", record.ID)); err != nil {
				t.Fatal(err)
			}
			planned, ok = tc.store.GetPlannedNode(nodeID)
			if !ok || planned.OwnerID != owner.ID || !planned.Private {
				t.Fatalf("restored planned = %#v, ok = %v", planned, ok)
			}
		})
	}
}

func TestSubmissionAndDirectListingProtectNodeOwnership(t *testing.T) {
	for _, tc := range accountStoreBackends(t) {
		t.Run(tc.name, func(t *testing.T) {
			sub := testSubmission("submission-node", "submission-owner", "submission@example.com")
			sub.Node.OwnerID = ""
			sub.Node.TokenHash = ""
			if err := tc.store.CreateMarketSubmission(sub, 10); err != nil {
				t.Fatal(err)
			}
			planned, ok := tc.store.GetPlannedNode(sub.Node.NodeID)
			if !ok || planned.OwnerID != sub.Owner.ID || planned.TokenHash != hashToken(sub.AgentToken) {
				t.Fatalf("submission planned = %#v, ok = %v", planned, ok)
			}

			owner, err := tc.store.CreateOwner("direct@example.com", "password-hash")
			if err != nil {
				t.Fatal(err)
			}
			if err := tc.store.CreateOwnerNode(owner.ID, PlannedNode{NodeID: "direct-node"}, HostInfo{}, "direct-token", accountAudit(owner.ID, "node.create", "direct-node"), 10, 3); err != nil {
				t.Fatal(err)
			}
			if err := tc.store.CreateMarketListing(MarketListing{NodeID: "direct-node", OwnerID: "different-owner", ForSale: true}); err == nil {
				t.Fatal("expected direct listing owner conflict")
			}
			planned, _ = tc.store.GetPlannedNode("direct-node")
			if planned.OwnerID != owner.ID {
				t.Fatalf("owner changed after conflict: %#v", planned)
			}
			conflict := testSubmission("direct-node", "takeover-owner", "takeover@example.com")
			if err := tc.store.CreateMarketSubmission(conflict, 10); err == nil {
				t.Fatal("expected submission owner conflict")
			}
			if _, ok := tc.store.GetOwnerByID(conflict.Owner.ID); ok {
				t.Fatal("failed ownership takeover left owner behind")
			}

			if err := tc.store.CreateMarketListing(MarketListing{NodeID: "compat-node", OwnerID: "compat-owner", ForSale: true}); err != nil {
				t.Fatal(err)
			}
			planned, ok = tc.store.GetPlannedNode("compat-node")
			if !ok || planned.OwnerID != "compat-owner" || planned.Private {
				t.Fatalf("compat planned = %#v, ok = %v", planned, ok)
			}
		})
	}
}

func TestNodeBackupOwnerPrivacyCompatibilityAcrossStores(t *testing.T) {
	for _, tc := range accountStoreBackends(t) {
		t.Run(tc.name, func(t *testing.T) {
			owner, err := tc.store.CreateOwner("backup@example.com", "password-hash")
			if err != nil {
				t.Fatal(err)
			}
			if err := tc.store.CreateOwnerNode(owner.ID, PlannedNode{NodeID: "backup-node", Private: true}, HostInfo{}, "backup-token", accountAudit(owner.ID, "node.create", "backup-node"), 10, 3); err != nil {
				t.Fatal(err)
			}
			backup := tc.store.ExportNodes()
			if backup.Version != 3 || len(backup.Nodes) != 1 || backup.Nodes[0].OwnerID != owner.ID || !backup.Nodes[0].Private {
				t.Fatalf("v3 backup = %#v", backup)
			}
			record, err := tc.store.TrashNode("backup-node", accountAudit(owner.ID, "node.trash", "backup-node"))
			if err != nil || record.Node == nil {
				t.Fatalf("trash record = %#v, err = %v", record, err)
			}
			if _, err := tc.store.ImportNodes(backup, 10); err != nil {
				t.Fatal(err)
			}
			roundTrip, ok := tc.store.GetPlannedNode("backup-node")
			if !ok || roundTrip.OwnerID != owner.ID || !roundTrip.Private {
				t.Fatalf("v3 round trip = %#v, ok = %v", roundTrip, ok)
			}

			legacy := NodeBackup{Version: 2, Nodes: []NodeBackupRecord{{NodeID: "legacy-backup", OwnerID: "ignored-owner", Private: true}}}
			if _, err := tc.store.ImportNodes(legacy, 10); err != nil {
				t.Fatal(err)
			}
			planned, ok := tc.store.GetPlannedNode("legacy-backup")
			if !ok || planned.OwnerID != "" || planned.Private {
				t.Fatalf("legacy planned = %#v, ok = %v", planned, ok)
			}
		})
	}
}

func TestJSONLegacyListingBackfillsPlannedOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.json")
	raw := `{"listings":{"legacy-node":{"node_id":"legacy-node","owner_id":"legacy-owner","created_at":123}}}`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	planned, ok := store.GetPlannedNode("legacy-node")
	if !ok || planned.OwnerID != "legacy-owner" || planned.Private || planned.CreatedAt != 123 {
		t.Fatalf("backfilled planned = %#v, ok = %v", planned, ok)
	}
}

func TestSQLiteLegacySchemaAddsOwnerPrivacyAndBackfills(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE planned_nodes (node_id TEXT PRIMARY KEY, created_at INTEGER NOT NULL, token_hash TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE market_listings (node_id TEXT PRIMARY KEY, owner_id TEXT NOT NULL, for_sale INTEGER NOT NULL DEFAULT 1, listing_type TEXT NOT NULL DEFAULT '', contact TEXT NOT NULL DEFAULT '', description TEXT NOT NULL DEFAULT '', specs TEXT NOT NULL DEFAULT '', price TEXT NOT NULL DEFAULT '', pinned INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL)`,
		`INSERT INTO planned_nodes(node_id, created_at, token_hash) VALUES ('legacy-node', 123, '')`,
		`INSERT INTO market_listings(node_id, owner_id, created_at, updated_at) VALUES ('legacy-node', 'legacy-owner', 123, 123)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := NewSQLiteStore(path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	planned, ok := store.GetPlannedNode("legacy-node")
	if !ok || planned.OwnerID != "legacy-owner" || planned.Private {
		t.Fatalf("migrated planned = %#v, ok = %v", planned, ok)
	}
	var version int
	if err := store.db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version < 6 {
		t.Fatalf("schema version = %d", version)
	}
}

func TestJSONToSQLiteImportPreservesOwnerPrivacy(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "server.json")
	jsonStore, err := NewStore(jsonPath, "import-account-secret")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := jsonStore.CreateOwner("import@example.com", "password-hash")
	if err != nil {
		t.Fatal(err)
	}
	if err := jsonStore.CreateOwnerNode(owner.ID, PlannedNode{NodeID: "import-node", Private: true}, HostInfo{}, "import-token", accountAudit(owner.ID, "node.create", "import-node"), 10, 3); err != nil {
		t.Fatal(err)
	}
	sqliteStore, err := NewSQLiteStore(filepath.Join(dir, "server.db"), jsonPath, "import-account-secret")
	if err != nil {
		t.Fatal(err)
	}
	defer sqliteStore.Close()
	planned, ok := sqliteStore.GetPlannedNode("import-node")
	if !ok || planned.OwnerID != owner.ID || !planned.Private {
		t.Fatalf("imported planned = %#v, ok = %v", planned, ok)
	}
}

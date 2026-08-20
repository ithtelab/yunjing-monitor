package server

import (
	"path/filepath"
	"testing"
	"time"

	serverapp "vps-agent/internal/server/application"
)

func TestStructuredBillingPersistsAcrossStores(t *testing.T) {
	factories := map[string]func(*testing.T) serverapp.Store{
		"json": func(t *testing.T) serverapp.Store {
			store, err := NewStore(filepath.Join(t.TempDir(), "server.json"))
			if err != nil {
				t.Fatal(err)
			}
			return store
		},
		"sqlite": func(t *testing.T) serverapp.Store {
			store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "server.db"), "")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			return store
		},
	}

	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			store := factory(t)
			const nodeID = "US-billing-001"
			if err := store.AddPlannedNode(nodeID, 10); err != nil {
				t.Fatal(err)
			}
			if err := store.SetNodeBilling(nodeID, "USD 0.5", "月", 0.5, "USD", "monthly"); err != nil {
				t.Fatal(err)
			}
			if err := store.CreateMarketListing(MarketListing{
				NodeID: nodeID, OwnerID: "owner_1", ForSale: true, ListingType: "rent",
				Contact: "test", Price: "USD 0.5", PriceAmount: 0.5, PriceCurrency: "USD", BillingCycle: "monthly",
				CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
			}); err != nil {
				t.Fatal(err)
			}

			listing, ok := store.GetMarketListing(nodeID)
			if !ok || listing.PriceAmount != 0.5 || listing.PriceCurrency != "USD" || listing.BillingCycle != "monthly" {
				t.Fatalf("listing billing = %#v", listing)
			}
			nodes := store.AdminNodes(time.Minute)
			if len(nodes) != 1 || nodes[0].Info.PriceAmount != 0.5 || nodes[0].Info.BillingCycle != "monthly" {
				t.Fatalf("host billing = %#v", nodes)
			}
		})
	}
}

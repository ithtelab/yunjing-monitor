package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sampleFriendLink(label, url string) FriendLink {
	return FriendLink{Label: label, URL: url, Enabled: true, NewTab: true}
}

func TestFriendLinkJSONAndSQLitePersistence(t *testing.T) {
	factories := map[string]func() (dataStore, func()){
		"json": func() (dataStore, func()) {
			store, err := NewStore(filepath.Join(t.TempDir(), "server.json"))
			if err != nil {
				t.Fatal(err)
			}
			return store, func() {}
		},
		"sqlite": func() (dataStore, func()) {
			store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "server.db"), "")
			if err != nil {
				t.Fatal(err)
			}
			return store, func() { _ = store.Close() }
		},
	}
	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			store, closeFn := factory()
			defer closeFn()
			settings := Settings{SiteName: "Footer Test", FooterHidden: true, FooterText: "Custom copyright", FriendLinksTitle: "Partners", VisitorStatsHidden: true, VisitorStatsItems: []string{"today_views", "total_views"}}
			if err := store.UpdateSettings(settings); err != nil {
				t.Fatal(err)
			}
			gotSettings := store.GetSettings()
			if !gotSettings.FooterHidden || gotSettings.FooterText != settings.FooterText || len(gotSettings.VisitorStatsItems) != 2 {
				t.Fatalf("settings=%#v", gotSettings)
			}
			first, err := store.SaveFriendLink(sampleFriendLink("First", "https://first.example"))
			if err != nil {
				t.Fatal(err)
			}
			second, err := store.SaveFriendLink(sampleFriendLink("Second", "https://second.example"))
			if err != nil {
				t.Fatal(err)
			}
			if err := store.ReorderFriendLinks([]string{second.ID, first.ID}); err != nil {
				t.Fatal(err)
			}
			links := store.ListFriendLinks(false)
			if len(links) != 2 || links[0].ID != second.ID {
				t.Fatalf("links=%#v", links)
			}
			second.Enabled = false
			if _, err := store.SaveFriendLink(second); err != nil {
				t.Fatal(err)
			}
			if got := store.ListFriendLinks(false); len(got) != 1 || got[0].ID != first.ID {
				t.Fatalf("enabled links=%#v", got)
			}
		})
	}
}

func TestFriendLinkValidationAndPublicFooter(t *testing.T) {
	if _, err := validateFriendLink(sampleFriendLink("Bad", "javascript:alert(1)")); err == nil {
		t.Fatal("expected unsafe URL rejection")
	}
	s := newTestServer(t)
	enabled, err := s.store.SaveFriendLink(sampleFriendLink("Enabled", "https://enabled.example"))
	if err != nil {
		t.Fatal(err)
	}
	disabled := sampleFriendLink("Disabled", "https://disabled.example")
	disabled.Enabled = false
	if _, err := s.store.SaveFriendLink(disabled); err != nil {
		t.Fatal(err)
	}
	resp := httptest.NewRecorder()
	s.handleSiteFooter(resp, httptest.NewRequest(http.MethodGet, "/api/site/footer", nil))
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d", resp.Code)
	}
	var payload siteFooterResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Links) != 1 || payload.Links[0].Label != enabled.Label {
		t.Fatalf("footer=%#v", payload)
	}
}

func TestVisitorStatsDeduplicatesAndExpires(t *testing.T) {
	path := filepath.Join(t.TempDir(), "visitor-stats.json")
	store := NewVisitorStatsStore(path, "secret-value")
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))
	id1 := "visitor1234567890"
	id2 := "visitor0987654321"
	store.Record(id1, "event123456789012", "203.0.113.10", true, now)
	snap := store.Record(id1, "event123456789012", "203.0.113.10", true, now.Add(time.Second))
	if snap.Online != 1 || snap.TodayIPs != 1 || snap.TodayViews != 1 || snap.TotalViews != 1 {
		t.Fatalf("deduplicated snapshot=%#v", snap)
	}
	snap = store.Record(id2, "event987654321098", "203.0.113.10", true, now.Add(2*time.Second))
	if snap.Online != 2 || snap.TodayIPs != 1 || snap.TodayViews != 2 {
		t.Fatalf("second visitor snapshot=%#v", snap)
	}
	if snap = store.Snapshot(now.Add(4 * time.Minute)); snap.Online != 0 {
		t.Fatalf("expired online=%#v", snap)
	}
	if err := store.Flush(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "203.0.113.10") {
		t.Fatal("raw IP leaked to visitor stats file")
	}
	reloaded := NewVisitorStatsStore(path, "secret-value")
	persisted := reloaded.Snapshot(now)
	if persisted.TotalViews != 2 || persisted.TodayIPs != 1 {
		t.Fatalf("persisted=%#v", persisted)
	}
}

func TestVisitorStatsCapsOnlineVisitorsPerIP(t *testing.T) {
	store := NewVisitorStatsStore(filepath.Join(t.TempDir(), "visitor-stats.json"), "secret-value")
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))
	for i := 0; i < maxOnlinePerIP+5; i++ {
		store.Record(fmt.Sprintf("visitor%016d", i), "", "203.0.113.10", false, now)
	}
	if got := store.Snapshot(now).Online; got != maxOnlinePerIP {
		t.Fatalf("online visitors from one IP = %d, want %d", got, maxOnlinePerIP)
	}
	store.Record("visitor9999999999999999", "", "203.0.113.11", false, now)
	if got := store.Snapshot(now).Online; got != maxOnlinePerIP+1 {
		t.Fatalf("different IP should retain its own slot, got %d", got)
	}
}

func TestRequestClientIPOnlyTrustsProxyHeadersFromPrivatePeer(t *testing.T) {
	trusted := httptest.NewRequest(http.MethodPost, "/api/site/visit", nil)
	trusted.RemoteAddr = "172.18.0.1:1234"
	trusted.Header.Set("X-Forwarded-For", "198.51.100.8, 172.18.0.1")
	if got := requestClientIP(trusted); got != "198.51.100.8" {
		t.Fatalf("trusted proxy IP=%q", got)
	}
	untrusted := httptest.NewRequest(http.MethodPost, "/api/site/visit", nil)
	untrusted.RemoteAddr = "198.51.100.9:1234"
	untrusted.Header.Set("X-Forwarded-For", "192.0.2.7")
	if got := requestClientIP(untrusted); got != "198.51.100.9" {
		t.Fatalf("untrusted proxy IP=%q", got)
	}
}

func TestAdminFriendLinkRequiresSession(t *testing.T) {
	s := newTestServer(t)
	body := `{"label":"Docs","url":"https://docs.example","enabled":true,"new_tab":true}`
	resp := httptest.NewRecorder()
	s.handleAdminFriendLinkSave(resp, httptest.NewRequest(http.MethodPost, "/api/admin/friend-links/save", strings.NewReader(body)))
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", resp.Code)
	}
}

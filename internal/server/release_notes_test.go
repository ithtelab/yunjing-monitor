package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmbeddedAndCustomReleaseNotesRemainSeparate(t *testing.T) {
	dir := t.TempDir()
	s := newTestServer(t)
	s.cfg.DataPath = filepath.Join(dir, "server.json")
	custom := []ReleaseNote{{Date: "2026-08-15", Title: "自定义通知", Type: "feature", Items: []string{"保留的自定义内容"}}}
	if err := s.saveCustomReleaseNotes(custom); err != nil {
		t.Fatal(err)
	}
	response, err := s.releaseNotes()
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Official) == 0 || len(response.Custom) != 1 || response.Custom[0].Source != "custom" {
		t.Fatalf("unexpected release notes response: %#v", response)
	}
	if response.Official[0].Source != "official" {
		t.Fatalf("official source = %q", response.Official[0].Source)
	}
	custom[0].Version = response.Official[0].Version
	if err := s.saveCustomReleaseNotes(custom); err == nil {
		t.Fatal("custom note must not overwrite an official version")
	}
}

func TestLegacyContentEndpointRendersEmbeddedNotesWithoutPersistingThem(t *testing.T) {
	dir := t.TempDir()
	s := newTestServer(t)
	s.cfg.DataPath = filepath.Join(dir, "server.json")
	if err := s.saveSiteContent(siteContent{Announcement: "<p>notice</p>", Changelog: "<h3>custom legacy</h3>"}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/site/content", nil)
	resp := httptest.NewRecorder()
	s.handleSiteContent(resp, req)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), "v1.1.4") || !strings.Contains(resp.Body.String(), "custom legacy") {
		t.Fatalf("legacy response = %d %s", resp.Code, resp.Body.String())
	}
	raw := s.loadSiteContentRaw()
	if strings.Contains(raw.Changelog, "v1.1.4") {
		t.Fatal("official notes must not be persisted into content.json")
	}
}

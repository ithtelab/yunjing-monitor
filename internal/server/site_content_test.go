package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdminSiteContentReadWrite(t *testing.T) {
	dir := t.TempDir()
	dataPath := filepath.Join(dir, "server.json")
	s := newTestServer(t)
	s.cfg.DataPath = dataPath

	// Unauthenticated write rejected.
	unauth := httptest.NewRequest(http.MethodPost, "https://monitor.example.com/api/admin/site/content", strings.NewReader(`{"announcement":"x"}`))
	unauth.Host = "monitor.example.com"
	unauthResp := httptest.NewRecorder()
	s.handleAdminSiteContent(unauthResp, unauth)
	if unauthResp.Code != http.StatusUnauthorized {
		t.Fatalf("unauth status=%d", unauthResp.Code)
	}

	token, err := s.sessions.Create()
	if err != nil {
		t.Fatal(err)
	}

	// Empty file → empty content.
	getReq := authedAdminRequest(http.MethodGet, "https://monitor.example.com/api/admin/site/content", token)
	getResp := httptest.NewRecorder()
	s.handleAdminSiteContent(getResp, getReq)
	if getResp.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getResp.Code, getResp.Body.String())
	}
	var empty siteContent
	if err := json.Unmarshal(getResp.Body.Bytes(), &empty); err != nil {
		t.Fatal(err)
	}
	if empty.Announcement != "" || empty.Changelog != "" {
		t.Fatalf("expected empty content, got %+v", empty)
	}

	// Save.
	body := `{"announcement":"<p>hello</p>","changelog":"<h3>2026-07-19</h3><ul><li>item</li></ul>"}`
	postReq := adminRequestWithBody(http.MethodPost, "https://monitor.example.com/api/admin/site/content", token, body)
	postReq.Header.Set("Content-Type", "application/json")
	postResp := httptest.NewRecorder()
	s.handleAdminSiteContent(postResp, postReq)
	if postResp.Code != http.StatusOK {
		t.Fatalf("post status=%d body=%s", postResp.Code, postResp.Body.String())
	}

	// File written next to data path.
	raw, err := os.ReadFile(filepath.Join(dir, "content.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "hello") || !strings.Contains(string(raw), "2026-07-19") {
		t.Fatalf("content.json unexpected: %s", raw)
	}

	// Public GET reads the same file.
	pubReq := httptest.NewRequest(http.MethodGet, "https://monitor.example.com/api/site/content", nil)
	pubResp := httptest.NewRecorder()
	s.handleSiteContent(pubResp, pubReq)
	if pubResp.Code != http.StatusOK {
		t.Fatalf("public get status=%d", pubResp.Code)
	}
	var pub siteContent
	if err := json.Unmarshal(pubResp.Body.Bytes(), &pub); err != nil {
		t.Fatal(err)
	}
	if pub.Announcement != "<p>hello</p>" {
		t.Fatalf("announcement=%q", pub.Announcement)
	}
	if !strings.Contains(pub.Changelog, "2026-07-19") {
		t.Fatalf("changelog=%q", pub.Changelog)
	}
}

func TestSiteContentReleaseMigrationIsIdempotent(t *testing.T) {
	initial := siteContent{
		Announcement: `<a href="https://t.me/ithte">站长 TG @ithte</a><p>自定义公告</p>`,
		Changelog: officialChangelogHeading + `

<h3>2026-07-20 · v1.1.0 资产与市场升级</h3><ul><li>旧官方内容</li></ul>

<h3>2026-07-19 · 自定义记录</h3><ul><li>保留内容</li></ul>`,
	}

	migrated, changed := migrateSiteContent(initial)
	if !changed {
		t.Fatal("first migration should change content")
	}
	if strings.Contains(migrated.Announcement, `https://t.me/ithte"`) || strings.Contains(migrated.Announcement, "@ithte<") {
		t.Fatalf("old TG remains: %s", migrated.Announcement)
	}
	if !strings.Contains(migrated.Announcement, "https://t.me/ithte888") || !strings.Contains(migrated.Announcement, "@ithte888") {
		t.Fatalf("new TG missing: %s", migrated.Announcement)
	}
	if strings.Contains(migrated.Changelog, "v1.1.0 资产与市场升级") {
		t.Fatalf("embedded official entry should be removed from mutable content: %s", migrated.Changelog)
	}
	if !strings.Contains(migrated.Changelog, "自定义记录") || !strings.Contains(migrated.Changelog, "保留内容") {
		t.Fatalf("custom changelog was lost: %s", migrated.Changelog)
	}

	again, changed := migrateSiteContent(migrated)
	if changed || again != migrated {
		t.Fatal("second migration should be a no-op")
	}
}

func TestEnsureSiteContentMigrationPreservesMalformedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "content.json")
	if err := os.WriteFile(path, []byte(`{"changelog":`), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t)
	s.cfg.DataPath = filepath.Join(dir, "server.json")
	if err := s.ensureSiteContentMigrations(); err == nil {
		t.Fatal("malformed content should return an error")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"changelog":` {
		t.Fatalf("malformed content was overwritten: %s", raw)
	}
}

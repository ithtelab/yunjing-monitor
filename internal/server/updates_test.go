package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"v1.2.0", "v1.1.9", 1},
		{"1.2.0", "v1.2.0", 0},
		{"v2.0.0", "v10.0.0", -1},
		{"v1.0.0", "dev", 1},
		{"v1.0.0-rc.1", "v1.0.0", -1},
	}
	for _, test := range tests {
		got := versionCompare(test.a, test.b)
		if got != test.want {
			t.Fatalf("versionCompare(%q, %q) = %d, want %d", test.a, test.b, got, test.want)
		}
	}
}

func TestUpdateServiceFetchesPrivateReleaseAndCreatesSignedRequest(t *testing.T) {
	var authorization string
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		if r.URL.Path != "/repos/ithtelab/yunjing-monitor/releases/latest" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(githubRelease{TagName: "v1.3.0", Name: "Release 1.3", Body: "new update system", HTMLURL: "https://github.test/release", PublishedAt: "2026-07-20T00:00:00Z"})
	}))
	defer github.Close()

	dir := t.TempDir()
	service := NewUpdateService(Config{
		AuthSecret:    "test-update-secret",
		DataPath:      filepath.Join(dir, "server.json"),
		UpdateRepo:    "ithtelab/yunjing-monitor",
		GitHubToken:   "private-token",
		UpdateEnabled: true,
		UpdateAPIBase: github.URL,
	})
	previousVersion := buildVersion
	buildVersion = "v1.2.0"
	t.Cleanup(func() { buildVersion = previousVersion })

	info := service.snapshot(true, true)
	if authorization != "Bearer private-token" {
		t.Fatalf("authorization header = %q", authorization)
	}
	if !info.UpdateAvailable || info.Latest != "v1.3.0" || !info.UpdateEnabled {
		t.Fatalf("unexpected release info: %+v", info)
	}
	if err := service.createRequest(info.Latest); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "update-request.json"))
	if err != nil {
		t.Fatal(err)
	}
	var request map[string]string
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	payload := strings.Join([]string{request["repository"], request["version"], request["created_at"], request["nonce"]}, "\n")
	mac := hmac.New(sha256.New, []byte("test-update-secret"))
	_, _ = mac.Write([]byte(payload))
	if !hmac.Equal([]byte(request["signature"]), []byte(hex.EncodeToString(mac.Sum(nil)))) {
		t.Fatal("update request signature is invalid")
	}
	if status := service.readStatus(); status.State != "pending" || status.Version != "v1.3.0" {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestPublicVersionResponseHidesCheckErrorsAndUpdateCapability(t *testing.T) {
	service := NewUpdateService(Config{DataPath: filepath.Join(t.TempDir(), "server.json"), UpdateRepo: "ithtelab/yunjing-monitor", UpdateAPIBase: "http://127.0.0.1:1", UpdateEnabled: true})
	info := service.snapshot(true, false)
	if info.CheckError != "" || info.UpdateEnabled || info.Status != nil || info.Commit != "" || info.BuildTime != "" {
		t.Fatalf("public version response leaked admin fields: %+v", info)
	}
}

func TestAdminUpdateApplyRequiresPasswordConfirmation(t *testing.T) {
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(githubRelease{TagName: "v9.0.0", HTMLURL: "https://github.test/release"})
	}))
	defer github.Close()
	dir := t.TempDir()
	s := newTestServer(t)
	s.cfg.AuthSecret = "test-update-secret"
	s.cfg.AdminPass = "correct-password"
	s.cfg.UpdateEnabled = true
	s.cfg.UpdateRepo = "ithtelab/yunjing-monitor"
	s.updates = NewUpdateService(Config{AuthSecret: s.cfg.AuthSecret, AdminPass: s.cfg.AdminPass, DataPath: filepath.Join(dir, "server.json"), UpdateRepo: s.cfg.UpdateRepo, UpdateEnabled: true, UpdateAPIBase: github.URL})
	token, err := s.sessions.CreateFor(SessionKindAdmin, "admin")
	if err != nil {
		t.Fatal(err)
	}

	bad := adminRequestWithBody(http.MethodPost, "https://monitor.example.com/api/admin/update/apply", token, `{"password":"wrong"}`)
	bad.Header.Set("Content-Type", "application/json")
	badResponse := httptest.NewRecorder()
	s.handleAdminUpdateApply(badResponse, bad)
	if badResponse.Code != http.StatusUnauthorized {
		t.Fatalf("bad password status = %d", badResponse.Code)
	}

	good := adminRequestWithBody(http.MethodPost, "https://monitor.example.com/api/admin/update/apply", token, `{"password":"correct-password"}`)
	good.Header.Set("Content-Type", "application/json")
	goodResponse := httptest.NewRecorder()
	s.handleAdminUpdateApply(goodResponse, good)
	if goodResponse.Code != http.StatusOK {
		t.Fatalf("good password status = %d body=%s", goodResponse.Code, goodResponse.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "update-request.json")); err != nil {
		t.Fatalf("signed update request was not written: %v", err)
	}
}

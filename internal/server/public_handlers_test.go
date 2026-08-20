package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidDownloadName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{name: "vps-agent-linux-amd64", want: true},
		{name: "agent_1.2.3.exe", want: true},
		{name: "", want: false},
		{name: "../secret", want: false},
		{name: "bad name", want: false},
		{name: strings.Repeat("a", 129), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validDownloadName(tt.name); got != tt.want {
				t.Fatalf("validDownloadName(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestWindowsInstallerAndUninstallersRequireGetAndNoCache(t *testing.T) {
	s := newTestServer(t)

	windowsPostReq := httptest.NewRequest(http.MethodPost, "https://monitor.example.com/install/agent-windows.ps1", nil)
	windowsPostResp := httptest.NewRecorder()
	s.handleAgentWindowsInstaller(windowsPostResp, windowsPostReq)
	if windowsPostResp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("windows installer POST status = %d body = %s", windowsPostResp.Code, windowsPostResp.Body.String())
	}

	windowsGetReq := httptest.NewRequest(http.MethodGet, "https://monitor.example.com/install/agent-windows.ps1", nil)
	windowsGetReq.Host = "monitor.example.com"
	windowsGetResp := httptest.NewRecorder()
	s.handleAgentWindowsInstaller(windowsGetResp, windowsGetReq)
	if windowsGetResp.Code != http.StatusOK {
		t.Fatalf("windows installer GET status = %d body = %s", windowsGetResp.Code, windowsGetResp.Body.String())
	}
	if got := windowsGetResp.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("windows installer content type = %q", got)
	}
	if got := windowsGetResp.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("windows installer cache control = %q", got)
	}
	if !strings.Contains(windowsGetResp.Body.String(), "monitor.example.com") {
		t.Fatalf("windows installer body missing external base: %s", windowsGetResp.Body.String())
	}

	linuxUninstallReq := httptest.NewRequest(http.MethodGet, "https://monitor.example.com/uninstall/agent-linux.sh", nil)
	linuxUninstallResp := httptest.NewRecorder()
	s.handleAgentLinuxUninstaller(linuxUninstallResp, linuxUninstallReq)
	if linuxUninstallResp.Code != http.StatusOK {
		t.Fatalf("linux uninstaller status = %d body = %s", linuxUninstallResp.Code, linuxUninstallResp.Body.String())
	}
	if got := linuxUninstallResp.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("linux uninstaller cache control = %q", got)
	}

	windowsUninstallReq := httptest.NewRequest(http.MethodGet, "https://monitor.example.com/uninstall/agent-windows.ps1", nil)
	windowsUninstallResp := httptest.NewRecorder()
	s.handleAgentWindowsUninstaller(windowsUninstallResp, windowsUninstallReq)
	if windowsUninstallResp.Code != http.StatusOK {
		t.Fatalf("windows uninstaller status = %d body = %s", windowsUninstallResp.Code, windowsUninstallResp.Body.String())
	}
	if got := windowsUninstallResp.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("windows uninstaller cache control = %q", got)
	}
}

func TestExternalBaseUsesPublicURLSchemeWithoutForwardedProto(t *testing.T) {
	s := newTestServer(t)
	s.cfg.PublicURL = "https://monitor.example.com"

	tests := []struct {
		name  string
		host  string
		proto string
		tls   bool
		want  string
	}{
		{name: "configured url ignores request host", host: "monitor.example.com", want: "https://monitor.example.com"},
		{name: "configured url ignores forwarded proto", host: "other.example.com", proto: "https", want: "https://monitor.example.com"},
		{name: "configured url ignores tls and host", host: "127.0.0.1:3000", tls: true, want: "https://monitor.example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := "http://" + tt.host + "/api/admin/install-command"
			if tt.tls {
				target = "https://" + tt.host + "/api/admin/install-command"
			}
			req := httptest.NewRequest(http.MethodPost, target, nil)
			req.Host = tt.host
			if tt.proto != "" {
				req.Header.Set("X-Forwarded-Proto", tt.proto)
			}
			got, err := s.externalBase(req)
			if err != nil || got != tt.want {
				t.Fatalf("externalBase() = %q, err=%v, want %q", got, err, tt.want)
			}
		})
	}
}

func TestExternalBaseRejectsHostInjectionAndNormalizesValidHosts(t *testing.T) {
	s := newTestServer(t)
	s.cfg.PublicURL = ""
	tests := []struct {
		host string
		want string
		ok   bool
	}{
		{host: "monitor.example.com:8443", want: "http://monitor.example.com:8443", ok: true},
		{host: "[2001:db8::1]:443", want: "http://[2001:db8::1]:443", ok: true},
		{host: "2001:db8::1", want: "http://[2001:db8::1]", ok: true},
		{host: "evil.example/%0d%0aX-Injected: yes", ok: false},
		{host: "evil.example:0", ok: false},
		{host: "evil.example:65536", ok: false},
		{host: "-evil.example", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
			req.Host = tt.host
			got, err := s.externalBase(req)
			if tt.ok {
				if err != nil || got != tt.want {
					t.Fatalf("externalBase() = %q, err=%v, want %q", got, err, tt.want)
				}
			} else if err == nil {
				t.Fatalf("externalBase(%q) accepted as %q", tt.host, got)
			}
		})
	}
}

func TestPublicMonitorDetailsRedaction(t *testing.T) {
	s := newTestServer(t)
	const nodeID = "CN-public-redact"
	if err := s.store.UpsertReport(sampleMetrics(nodeID, 100, 200), 10); err != nil {
		t.Fatal(err)
	}
	var hidden []AkileHost
	if err := json.Unmarshal(s.cachedHostsJSON(), &hidden); err != nil {
		t.Fatal(err)
	}
	if len(hidden) != 1 {
		t.Fatalf("hidden hosts = %#v", hidden)
	}
	if hidden[0].Host.Hostname != "" || hidden[0].Host.Kernel != "" || hidden[0].Host.CPUModel != "" || len(hidden[0].State.Disks) != 0 {
		t.Fatalf("sensitive public fields were not redacted: %#v", hidden[0])
	}
	if hidden[0].State.DiskTotal != 2048 || hidden[0].State.NetInSpeed != 10 {
		t.Fatalf("core public metrics changed: %#v", hidden[0].State)
	}

	s.cfg.PublicMonitorDetails = true
	s.cache.MarkDirty()
	var full []AkileHost
	if err := json.Unmarshal(s.cachedHostsJSON(), &full); err != nil {
		t.Fatal(err)
	}
	if full[0].Host.Hostname != "test-host" || full[0].Host.Kernel != "test-kernel" || len(full[0].State.Disks) != 1 {
		t.Fatalf("full public details were not restored: %#v", full[0])
	}
}

func TestInstallerRejectsInvalidHostWithoutPublicURL(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "http://example.com/install/agent-linux.sh", nil)
	req.Host = "attacker.example/%0d%0aX-Test: injected"
	resp := httptest.NewRecorder()
	s.handleAgentLinuxInstaller(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("installer status = %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestAdminInstallCommandRejectsInvalidHostWithoutIssuingToken(t *testing.T) {
	s := newTestServer(t)
	adminSession, err := s.sessions.Create()
	if err != nil {
		t.Fatal(err)
	}
	const nodeID = "CN-invalid-host"
	if err := s.store.AddPlannedNode(nodeID, 10); err != nil {
		t.Fatal(err)
	}
	req := authedAdminRequest(http.MethodPost, "https://monitor.example.com/api/admin/install-command?node_id="+nodeID, adminSession)
	req.Host = "attacker.example:65536"
	resp := httptest.NewRecorder()
	s.handleAdminInstallCommand(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("admin install status = %d body=%s", resp.Code, resp.Body.String())
	}
	if _, ok := s.store.GetNodeToken(nodeID); ok {
		t.Fatal("invalid host request issued a node token")
	}
}

func TestAdminInstallCommandUninstallDoesNotRotateToken(t *testing.T) {
	s := newTestServer(t)
	token, err := s.sessions.Create()
	if err != nil {
		t.Fatal(err)
	}
	const nodeID = "CN-keep-001"
	const agentTokenHash = "existing-token-hash"
	if err := s.store.SetNodeToken(nodeID, "plaintext-token", agentTokenHash, s.cfg.MaxNodes); err != nil {
		t.Fatal(err)
	}

	for _, platform := range []string{"linux-uninstall", "windows-uninstall"} {
		req := authedAdminRequest(http.MethodPost, "https://monitor.example.com/api/admin/install-command?node_id="+nodeID+"&platform="+platform, token)
		req.Header.Set("Origin", "https://monitor.example.com")
		resp := httptest.NewRecorder()
		s.handleAdminInstallCommand(resp, req)
		if resp.Code != http.StatusOK {
			t.Fatalf("%s status = %d body = %s", platform, resp.Code, resp.Body.String())
		}
		if !strings.Contains(resp.Body.String(), "uninstall") {
			t.Fatalf("%s response missing uninstall command: %s", platform, resp.Body.String())
		}
		if !s.store.ValidNodeToken(nodeID, agentTokenHash) {
			t.Fatalf("%s request rotated the node token", platform)
		}
	}

	nodes := s.store.AdminNodes(s.cfg.OfflineWait)
	if len(nodes) != 1 || !nodes[0].HasToken {
		t.Fatalf("admin nodes = %#v, want single node with has_token", nodes)
	}

	// Default install-command reuses the existing plaintext token and must not rotate.
	req := authedAdminRequest(http.MethodPost, "https://monitor.example.com/api/admin/install-command?node_id="+nodeID, token)
	req.Header.Set("Origin", "https://monitor.example.com")
	resp := httptest.NewRecorder()
	s.handleAdminInstallCommand(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("default install-command status = %d body = %s", resp.Code, resp.Body.String())
	}
	if !s.store.ValidNodeToken(nodeID, agentTokenHash) {
		t.Fatal("default install-command should reuse the existing node token")
	}
	if !strings.Contains(resp.Body.String(), "plaintext-token") {
		t.Fatalf("default install-command should re-show the existing token: %s", resp.Body.String())
	}

	// Explicit reset=1 rotates the token.
	resetReq := authedAdminRequest(http.MethodPost, "https://monitor.example.com/api/admin/install-command?node_id="+nodeID+"&reset=1", token)
	resetReq.Header.Set("Origin", "https://monitor.example.com")
	resetResp := httptest.NewRecorder()
	s.handleAdminInstallCommand(resetResp, resetReq)
	if resetResp.Code != http.StatusOK {
		t.Fatalf("reset install-command status = %d body = %s", resetResp.Code, resetResp.Body.String())
	}
	if s.store.ValidNodeToken(nodeID, agentTokenHash) {
		t.Fatal("reset=1 install-command should rotate the node token")
	}
}

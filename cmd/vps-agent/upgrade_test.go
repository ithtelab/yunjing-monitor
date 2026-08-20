package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"vps-agent/internal/config"
)

func signedUpgradeManifest(cfg config.Config, now time.Time, body []byte) agentUpgradeManifest {
	sum := sha256.Sum256(body)
	manifest := agentUpgradeManifest{
		RolloutID: "rollout_test", NodeID: cfg.NodeID, Action: "upgrade", Version: "v9.0.0",
		OS: runtime.GOOS, Arch: agentUpgradeArch(), URL: "https://example.com/agent", SHA256: hex.EncodeToString(sum[:]),
		IssuedAt: now.Unix(), ExpiresAt: now.Add(10 * time.Minute).Unix(), SignatureAlgorithm: "hmac-sha256",
	}
	raw, _ := json.Marshal(manifest)
	mac := hmac.New(sha256.New, []byte(cfg.Token))
	_, _ = mac.Write(raw)
	manifest.Signature = hex.EncodeToString(mac.Sum(nil))
	return manifest
}

func TestVerifyAgentUpgradeManifestRejectsTamperAndExpiry(t *testing.T) {
	now := time.Now()
	cfg := config.Config{NodeID: "node_test", Token: "node-token-value"}
	manifest := signedUpgradeManifest(cfg, now, []byte("agent"))
	if err := verifyAgentUpgradeManifest(manifest, cfg, manifest.OS, manifest.Arch, now); err != nil {
		t.Fatal(err)
	}
	tampered := manifest
	tampered.Version = "v9.0.1"
	if err := verifyAgentUpgradeManifest(tampered, cfg, tampered.OS, tampered.Arch, now); err == nil {
		t.Fatal("tampered manifest was accepted")
	}
	expired := signedUpgradeManifest(cfg, now.Add(-20*time.Minute), []byte("agent"))
	if err := verifyAgentUpgradeManifest(expired, cfg, expired.OS, expired.Arch, now); err == nil {
		t.Fatal("expired manifest was accepted")
	}
}

func TestDownloadAgentUpgradeVerifiesSHA256(t *testing.T) {
	body := []byte("verified-agent-binary")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }))
	defer server.Close()
	sum := sha256.Sum256(body)
	manifest := agentUpgradeManifest{URL: server.URL, SHA256: hex.EncodeToString(sum[:])}
	data, err := downloadAgentUpgrade(context.Background(), server.Client(), manifest)
	if err != nil || string(data) != string(body) {
		t.Fatalf("download failed: %v", err)
	}
	manifest.SHA256 = hex.EncodeToString(make([]byte, sha256.Size))
	if _, err := downloadAgentUpgrade(context.Background(), server.Client(), manifest); err == nil {
		t.Fatal("artifact with wrong hash was accepted")
	}
}

func TestActivatedUpgradeResultUsesInstalledStatus(t *testing.T) {
	manifest := agentUpgradeManifest{RolloutID: "rollout_installed", Action: "rollback", Version: "v1.2.0", OS: "linux", Arch: "amd64", SHA256: strings.Repeat("a", 64)}
	result := activatedUpgradeResult(manifest)
	if result.Status != "installed" || result.Action != "rollback" || result.Version != manifest.Version || result.SHA256 != manifest.SHA256 {
		t.Fatalf("unexpected activated result: %#v", result)
	}
}

func TestReportPendingUpgradeRemovesReceiptOnlyAfterAccepted(t *testing.T) {
	var received agentUpgradeResult
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer node-token" || r.Header.Get("X-Node-ID") != "node_pending" {
			t.Errorf("missing agent authentication")
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("decode result: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	cfg := config.Config{Server: server.URL, NodeID: "node_pending", Token: "node-token"}
	configPath := filepath.Join(t.TempDir(), "agent.json")
	pending := agentUpgradeResult{RolloutID: "rollout_pending", Action: "upgrade", Status: "installed", Version: "v2.0.0", OS: runtime.GOOS, Arch: agentUpgradeArch(), SHA256: strings.Repeat("b", 64)}
	if err := writePendingUpgrade(configPath, pending); err != nil {
		t.Fatal(err)
	}
	if err := reportPendingUpgrade(context.Background(), server.Client(), cfg, configPath); err != nil {
		t.Fatal(err)
	}
	if received.Status != "installed" || received.RolloutID != pending.RolloutID {
		t.Fatalf("unexpected reported completion: %#v", received)
	}
	if _, err := os.Stat(pendingUpgradePath(configPath)); !os.IsNotExist(err) {
		t.Fatalf("pending receipt still exists: %v", err)
	}
}

func TestReportPendingUpgradeKeepsReceiptWhenServerRejects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "try again", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	cfg := config.Config{Server: server.URL, NodeID: "node_pending", Token: "node-token"}
	configPath := filepath.Join(t.TempDir(), "agent.json")
	if err := writePendingUpgrade(configPath, agentUpgradeResult{RolloutID: "rollout_pending", Action: "upgrade", Status: "installed"}); err != nil {
		t.Fatal(err)
	}
	if err := reportPendingUpgrade(context.Background(), server.Client(), cfg, configPath); err == nil {
		t.Fatal("server rejection was ignored")
	}
	if _, err := os.Stat(pendingUpgradePath(configPath)); err != nil {
		t.Fatalf("pending receipt was removed after rejection: %v", err)
	}
}

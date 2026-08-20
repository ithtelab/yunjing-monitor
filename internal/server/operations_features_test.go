package server

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vps-agent/internal/agent"
)

func TestMetricsHistoryDownsamplesAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics-history.json")
	history := NewMetricsHistoryStore(path)
	now := time.Now().Unix() / metricsHistoryBucket * metricsHistoryBucket
	history.Add(agent.Metrics{NodeID: "n_history", Timestamp: now, CPU: agent.CPU{UsagePercent: 10}, Memory: agent.Memory{Used: 20, Total: 100}})
	history.Add(agent.Metrics{NodeID: "n_history", Timestamp: now + 2, CPU: agent.CPU{UsagePercent: 30}, Memory: agent.Memory{Used: 40, Total: 100}})
	items := history.Query("n_history", now-1)
	if len(items) != 1 || items[0].CPUPercent != 30 || items[0].MemoryUsed != 40 {
		t.Fatalf("same-minute samples were not compacted: %#v", items)
	}
	if err := history.Save(); err != nil {
		t.Fatal(err)
	}
	reloaded := NewMetricsHistoryStore(path).Query("n_history", now-1)
	if len(reloaded) != 1 || reloaded[0].CPUPercent != 30 {
		t.Fatalf("persisted history mismatch: %#v", reloaded)
	}
}

func TestAlertTokenIsEncryptedAtRest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alerts.json")
	alerts, err := NewAlertService(path, "test-auth-secret-at-least-32-characters")
	if err != nil {
		t.Fatal(err)
	}
	settings := defaultAlertSettings()
	settings.Enabled = true
	settings.TelegramChatID = "123"
	settings.TelegramToken = "123456:telegram-secret-token"
	if err := alerts.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), settings.TelegramToken) {
		t.Fatal("telegram token was stored in plaintext")
	}
	reloaded, err := NewAlertService(path, "test-auth-secret-at-least-32-characters")
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Settings(); !got.TokenConfigured || got.TelegramToken != "" {
		t.Fatalf("unexpected public alert settings: %#v", got)
	}
}

func TestNotificationAndWebDAVURLValidation(t *testing.T) {
	for _, value := range []string{"http://example.com/hook", "https://localhost/hook", "https://127.0.0.1/hook", "https://10.0.0.1/hook"} {
		if err := validateNotificationURL(value); err == nil {
			t.Fatalf("expected notification URL rejection for %q", value)
		}
	}
	if err := validateNotificationURL("https://example.com/hook"); err != nil {
		t.Fatalf("public HTTPS notification URL rejected: %v", err)
	}
	if _, err := cleanBackupWebDAVURL("https://example.com/dav/backups"); err != nil {
		t.Fatalf("valid WebDAV URL rejected: %v", err)
	}
	if _, err := cleanBackupWebDAVURL("https://user:pass@example.com/dav"); err == nil {
		t.Fatal("WebDAV URL credentials should be rejected")
	}
}

func TestNotificationTargetRejectsReservedNetworks(t *testing.T) {
	for _, value := range []string{
		"100.64.0.1",
		"192.0.2.1",
		"198.18.0.1",
		"198.51.100.1",
		"203.0.113.1",
		"2001:db8::1",
	} {
		if publicNotificationIP(net.ParseIP(value)) {
			t.Fatalf("expected reserved notification target rejection for %s", value)
		}
	}
	for _, value := range []string{"1.1.1.1", "2606:4700:4700::1111"} {
		if !publicNotificationIP(net.ParseIP(value)) {
			t.Fatalf("expected public notification target acceptance for %s", value)
		}
	}
}

func TestUpdateHistoryNewestFirst(t *testing.T) {
	dir := t.TempDir()
	service := &UpdateService{cfg: Config{DataPath: filepath.Join(dir, "server.json")}}
	items := []UpdateStatus{{State: "success", Version: "v1.0.0", UpdatedAt: "2026-01-01T00:00:00Z"}, {State: "failed", Version: "v1.1.0", UpdatedAt: "2026-01-02T00:00:00Z"}}
	raw, _ := json.Marshal(items)
	if err := os.WriteFile(filepath.Join(dir, "update-history.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	got := service.readHistory()
	if len(got) != 2 || got[0].Version != "v1.1.0" || got[1].Version != "v1.0.0" {
		t.Fatalf("unexpected update history order: %#v", got)
	}
}

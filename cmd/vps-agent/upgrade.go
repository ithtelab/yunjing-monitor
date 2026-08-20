package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"vps-agent/internal/config"
)

const maxAgentUpgradeSize = 128 << 20

type agentUpgradeManifest struct {
	RolloutID          string `json:"rollout_id"`
	NodeID             string `json:"node_id"`
	Action             string `json:"action"`
	Version            string `json:"version"`
	OS                 string `json:"os"`
	Arch               string `json:"arch"`
	URL                string `json:"url"`
	SHA256             string `json:"sha256"`
	IssuedAt           int64  `json:"issued_at"`
	ExpiresAt          int64  `json:"expires_at"`
	SignatureAlgorithm string `json:"signature_algorithm"`
	Signature          string `json:"signature"`
}

type agentUpgradeResult struct {
	RolloutID string `json:"rollout_id"`
	Action    string `json:"action"`
	Status    string `json:"status"`
	Version   string `json:"version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	SHA256    string `json:"sha256,omitempty"`
	Error     string `json:"error,omitempty"`
}

func checkAgentUpgrade(ctx context.Context, cfg config.Config, configPath string) (bool, error) {
	client := agentUpgradeHTTPClient()
	if err := reportPendingUpgrade(ctx, client, cfg, configPath); err != nil {
		return false, err
	}
	osName, arch := runtime.GOOS, agentUpgradeArch()
	endpoint := strings.TrimRight(cfg.Server, "/") + "/api/agent/upgrade-manifest?os=" + url.QueryEscape(osName) + "&arch=" + url.QueryEscape(arch)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, err
	}
	setAgentUpgradeAuth(req, cfg)
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, upgradeResponseError(resp)
	}
	var manifest agentUpgradeManifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&manifest); err != nil {
		return false, fmt.Errorf("decode upgrade manifest: %w", err)
	}
	if err := verifyAgentUpgradeManifest(manifest, cfg, osName, arch, time.Now()); err != nil {
		return false, err
	}
	if manifest.Action == "upgrade" && normalizedAgentVersion(manifest.Version) == normalizedAgentVersion(version) {
		result := activatedUpgradeResult(manifest)
		return false, sendAgentUpgradeResult(ctx, client, cfg, result)
	}
	data, err := downloadAgentUpgrade(ctx, client, manifest)
	if err != nil {
		_ = sendAgentUpgradeResult(ctx, client, cfg, upgradeResult(manifest, "failed", err.Error()))
		return false, err
	}
	_ = sendAgentUpgradeResult(ctx, client, cfg, upgradeResult(manifest, "downloading", ""))
	// Only the replacement process reports installed; the staging process
	// reports downloading and exits after activation.
	pending := activatedUpgradeResult(manifest)
	if err := writePendingUpgrade(configPath, pending); err != nil {
		return false, err
	}
	if err := stageAgentUpgrade(data, pendingUpgradePath(configPath)); err != nil {
		_ = os.Remove(pendingUpgradePath(configPath))
		_ = sendAgentUpgradeResult(ctx, client, cfg, upgradeResult(manifest, "failed", err.Error()))
		return false, err
	}
	return true, nil
}

func agentUpgradeHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too many upgrade redirects")
			}
			if req.URL.Scheme != "https" && !isLoopbackUpgradeHost(req.URL.Hostname()) {
				return errors.New("upgrade redirect must use HTTPS")
			}
			return nil
		},
	}
}

func verifyAgentUpgradeManifest(value agentUpgradeManifest, cfg config.Config, osName, arch string, now time.Time) error {
	if value.RolloutID == "" || value.NodeID != cfg.NodeID || value.OS != osName || value.Arch != arch || (value.Action != "upgrade" && value.Action != "rollback") {
		return errors.New("upgrade manifest identity mismatch")
	}
	if value.Version == "" || value.SignatureAlgorithm != "hmac-sha256" || len(value.SHA256) != sha256.Size*2 {
		return errors.New("upgrade manifest is incomplete")
	}
	if _, err := hex.DecodeString(value.SHA256); err != nil {
		return errors.New("upgrade manifest has invalid SHA-256")
	}
	if value.IssuedAt > now.Add(time.Minute).Unix() || value.ExpiresAt <= now.Unix() || value.ExpiresAt-value.IssuedAt > int64(15*time.Minute/time.Second) {
		return errors.New("upgrade manifest is expired or has invalid lifetime")
	}
	u, err := url.Parse(value.URL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && !(u.Scheme == "http" && isLoopbackUpgradeHost(u.Hostname()))) {
		return errors.New("upgrade artifact URL must use HTTPS")
	}
	signature, err := hex.DecodeString(value.Signature)
	if err != nil || len(signature) != sha256.Size {
		return errors.New("upgrade manifest signature is invalid")
	}
	value.Signature = ""
	raw, _ := json.Marshal(value)
	mac := hmac.New(sha256.New, []byte(cfg.Token))
	_, _ = mac.Write(raw)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return errors.New("upgrade manifest signature mismatch")
	}
	return nil
}

func downloadAgentUpgrade(ctx context.Context, client *http.Client, manifest agentUpgradeManifest) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifest.URL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, upgradeResponseError(resp)
	}
	if resp.ContentLength > maxAgentUpgradeSize {
		return nil, errors.New("upgrade artifact is too large")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAgentUpgradeSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > maxAgentUpgradeSize {
		return nil, errors.New("upgrade artifact is empty or too large")
	}
	sum := sha256.Sum256(data)
	if !hmac.Equal([]byte(strings.ToLower(manifest.SHA256)), []byte(hex.EncodeToString(sum[:]))) {
		return nil, errors.New("upgrade artifact SHA-256 mismatch")
	}
	return data, nil
}

func stageAgentUpgrade(data []byte, pendingPath string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return err
	}
	stage := executable + ".upgrade"
	backup := executable + ".rollback"
	if err := os.WriteFile(stage, data, 0o755); err != nil {
		return fmt.Errorf("write staged agent: %w", err)
	}
	if file, err := os.OpenFile(stage, os.O_RDWR, 0); err == nil {
		_ = file.Sync()
		_ = file.Close()
	}
	if err := activateAgentUpgrade(executable, stage, backup, pendingPath); err != nil {
		_ = os.Remove(stage)
		return err
	}
	return nil
}

func reportPendingUpgrade(ctx context.Context, client *http.Client, cfg config.Config, configPath string) error {
	path := pendingUpgradePath(configPath)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var result agentUpgradeResult
	if err := json.Unmarshal(raw, &result); err != nil {
		_ = os.Remove(path)
		return errors.New("invalid pending upgrade result")
	}
	if err := sendAgentUpgradeResult(ctx, client, cfg, result); err != nil {
		return err
	}
	return os.Remove(path)
}

func sendAgentUpgradeResult(ctx context.Context, client *http.Client, cfg config.Config, result agentUpgradeResult) error {
	raw, _ := json.Marshal(result)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.Server, "/")+"/api/agent/upgrade-result", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	setAgentUpgradeAuth(req, cfg)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return upgradeResponseError(resp)
	}
	return nil
}

func writePendingUpgrade(configPath string, result agentUpgradeResult) error {
	raw, _ := json.Marshal(result)
	path := pendingUpgradePath(configPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func pendingUpgradePath(configPath string) string { return configPath + ".upgrade-result.json" }

func upgradeResult(manifest agentUpgradeManifest, status, message string) agentUpgradeResult {
	if len(message) > 500 {
		message = message[:500]
	}
	return agentUpgradeResult{RolloutID: manifest.RolloutID, Action: manifest.Action, Status: status, Version: manifest.Version, OS: manifest.OS, Arch: manifest.Arch, SHA256: manifest.SHA256, Error: message}
}

func activatedUpgradeResult(manifest agentUpgradeManifest) agentUpgradeResult {
	return upgradeResult(manifest, "installed", "")
}

func setAgentUpgradeAuth(req *http.Request, cfg config.Config) {
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("X-Node-ID", cfg.NodeID)
}

func agentUpgradeArch() string {
	if runtime.GOARCH == "arm" {
		return "armv7"
	}
	return runtime.GOARCH
}

func normalizedAgentVersion(value string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "v")
}

func isLoopbackUpgradeHost(host string) bool {
	host = strings.Trim(strings.ToLower(host), "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func upgradeResponseError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = resp.Status
	}
	return fmt.Errorf("upgrade server returned %s", message)
}

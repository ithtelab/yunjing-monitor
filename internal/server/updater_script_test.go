package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestUpdateHistoryReturnsNewestFifty(t *testing.T) {
	dir := t.TempDir()
	items := make([]UpdateStatus, 60)
	for i := range items {
		items[i] = UpdateStatus{State: "success", Version: "v1.2." + strconv.Itoa(i), UpdatedAt: strconv.Itoa(i)}
	}
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "update-history.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewUpdateService(Config{DataPath: filepath.Join(dir, "server.json")})
	history := service.readHistory()
	if len(history) != 50 {
		t.Fatalf("history length = %d, want 50", len(history))
	}
	if history[0].Version != "v1.2.59" || history[49].Version != "v1.2.10" {
		t.Fatalf("unexpected history order: first=%s last=%s", history[0].Version, history[49].Version)
	}
}

func TestUpdaterUsesWritableDockerConfigAndSpecificFailures(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "scripts", "install-updater-linux.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	for _, required := range []string{
		"RuntimeDirectory=monitor-updater",
		"Environment=HOME=/run/monitor-updater",
		"Environment=DOCKER_CONFIG=/run/monitor-updater/docker",
		"Container image build failed; previous application version restored",
		"Container start failed; previous application version restored",
		"Health check failed; previous application version restored",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("updater script missing %q", required)
		}
	}
	if strings.Contains(script, "docker compose build --no-cache monitor-party || ! docker compose up") {
		t.Fatal("build and start failures must be handled separately")
	}
}

func TestNativeInstallersPreserveBackupEncryptionKey(t *testing.T) {
	for _, name := range []string{"install-server-linux.sh", "install-server-interactive-linux.sh"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "scripts", name))
		if err != nil {
			t.Fatal(err)
		}
		script := string(raw)
		for _, required := range []string{
			"BACKUP_ENCRYPTION_KEY",
			"BACKUP_DIR=/var/lib/vps-monitor/backups",
			"BACKUP_INTERVAL=24h",
			"existing_env_value BACKUP_ENCRYPTION_KEY",
		} {
			if !strings.Contains(script, required) {
				t.Fatalf("%s missing %q", name, required)
			}
		}
	}
}

func TestUpdaterRequiresIndependentSignatureAndSelectsHostArchitecture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "scripts", "install-updater-linux.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	for _, required := range []string{
		"umask 077",
		"--public-key",
		"RELEASE_PUBLIC_KEY_FILE",
		"SHA256SUMS.sig",
		"openssl dgst -sha256 -verify",
		"x86_64|amd64) arch=amd64",
		"aarch64|arm64) arch=arm64",
		"armv7l|armv7) arch=armv7",
		"i386|i486|i586|i686) arch=386",
		"binary_target_rel=\"${SERVER_BINARY:-release/$binary_name}\"",
		"if [ -n \"${GITHUB_TOKEN:-}\" ]",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("updater script missing %q", required)
		}
	}
	if strings.Contains(script, "cp \"$tmp/vps-server-linux-amd64\"") {
		t.Fatal("updater must not hard-code the downloaded amd64 binary")
	}
	if strings.Contains(script, "${GITHUB_TOKEN:?") {
		t.Fatal("public repositories must be updateable without GITHUB_TOKEN")
	}
}

func TestDockerDataPermissionsFailClosed(t *testing.T) {
	for _, name := range []string{"entrypoint.sh", "install.sh"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "deploy", "docker", name))
		if err != nil {
			t.Fatal(err)
		}
		script := string(raw)
		if strings.Contains(script, "chmod -R 777") {
			t.Fatalf("%s must not make the data directory world-writable", name)
		}
		if !strings.Contains(script, "o-rwx") {
			t.Fatalf("%s must explicitly remove permissions for other users", name)
		}
	}
}

func TestWorkflowsPinThirdPartyActions(t *testing.T) {
	for _, name := range []string{"ci.yml", "release-assets.yml"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", name))
		if err != nil {
			t.Fatal(err)
		}
		workflow := string(raw)
		for _, line := range strings.Split(workflow, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "uses: actions/") {
				ref, ok := strings.CutPrefix(line, "uses: actions/")
				if !ok {
					continue
				}
				parts := strings.SplitN(ref, "@", 2)
				fields := []string(nil)
				if len(parts) == 2 {
					fields = strings.Fields(parts[1])
				}
				if len(fields) == 0 || len(fields[0]) != 40 {
					t.Fatalf("%s contains an unpinned action: %s", name, line)
				}
			}
		}
	}
}

func TestReleaseWorkflowSeparatesBuildFromPublishingCredentials(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release-assets.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(raw)
	for _, required := range []string{
		"required: true",
		"persist-credentials: false",
		"contents: read",
		"contents: write",
		"needs: build",
		"actions/upload-artifact@",
		"actions/download-artifact@",
	} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("release workflow missing %q", required)
		}
	}
	if strings.Contains(workflow, "default: v1.1.4") {
		t.Fatal("manual releases must require an explicit tag")
	}
}

package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newBackupTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	dataPath := filepath.Join(dir, "server.json")
	store, err := NewStore(dataPath, "backup-test-auth-secret-strong")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		cfg: Config{
			DataPath: dataPath, StoreDriver: "json", MaxNodes: 10,
			BackupDir:           filepath.Join(dir, "backups"),
			BackupEncryptionKey: "a-high-entropy-backup-test-key-with-32-bytes",
		},
		store: store,
		cache: NewResponseCache(), sessions: NewSessionStore(),
	}
	return s
}

func TestBackupManagementAPIRequiresAdminAndCreatesBackup(t *testing.T) {
	s := newBackupTestServer(t)
	if err := os.WriteFile(s.cfg.DataPath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	unauth := httptest.NewRequest(http.MethodGet, "/api/admin/backups", nil)
	unauthResp := httptest.NewRecorder()
	s.handleAdminBackups(unauthResp, unauth)
	if unauthResp.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", unauthResp.Code)
	}
	token, err := s.sessions.CreateForDuration(SessionKindAdmin, "admin", defaultSessionTTL)
	if err != nil {
		t.Fatal(err)
	}
	create := authedAdminRequest(http.MethodPost, "https://monitor.example.com/api/admin/backups", token)
	createResp := httptest.NewRecorder()
	s.handleAdminBackups(createResp, create)
	if createResp.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", createResp.Code, createResp.Body.String())
	}
	items, err := s.listBackups()
	if err != nil || len(items) != 1 {
		t.Fatalf("backup list = %#v, err=%v", items, err)
	}
}

func TestEncryptedBackupVerifyAndRestore(t *testing.T) {
	s := newBackupTestServer(t)
	root := filepath.Dir(s.cfg.DataPath)
	originalServer := []byte(`{"reports":{},"infos":{},"planned":{},"settings":{},"traffic":{},"owners":{"owner":{}},"listings":{},"advertisements":{},"friend_links":{}}`)
	if err := os.WriteFile(s.cfg.DataPath, originalServer, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "content.json"), []byte(`{"announcement":"important","changelog":"custom"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "visitor-stats.json"), []byte(`{"total":9}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "auth-sessions.json"), []byte(`{"session":"keep-current"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "ads"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ads", "original.png"), []byte("image-original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.saveCustomReleaseNotes([]ReleaseNote{{Date: "2026-08-15", Title: "custom", Type: "feature", Items: []string{"item"}}}); err != nil {
		t.Fatal(err)
	}

	metadata, err := s.createBackup("test")
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := os.ReadFile(filepath.Join(s.cfg.BackupDir, metadata.ID+".mpbackup"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte("important")) || bytes.Contains(encrypted, []byte("image-original")) {
		t.Fatal("encrypted backup leaked plaintext")
	}
	verified, err := s.readAndVerifyBackup(metadata.ID)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Manifest.ObjectCounts["owners"] != 1 || verified.Manifest.ObjectCounts["ad_images"] != 1 {
		t.Fatalf("object counts = %#v", verified.Manifest.ObjectCounts)
	}
	for _, file := range verified.Manifest.Files {
		if strings.Contains(file.Path, "auth-sessions") {
			t.Fatalf("session included: %s", file.Path)
		}
	}

	if err := os.WriteFile(filepath.Join(root, "content.json"), []byte(`{"announcement":"changed"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ads", "stale.png"), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := s.applyBackup(metadata.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result["restart_required"] != true || result["sessions_restored"] != false {
		t.Fatalf("restore result = %#v", result)
	}
	content, _ := os.ReadFile(filepath.Join(root, "content.json"))
	if !bytes.Contains(content, []byte("important")) {
		t.Fatalf("content not restored: %s", content)
	}
	if _, err := os.Stat(filepath.Join(root, "ads", "stale.png")); !os.IsNotExist(err) {
		t.Fatal("stale ad image was not removed")
	}
	session, _ := os.ReadFile(filepath.Join(root, "auth-sessions.json"))
	if !bytes.Contains(session, []byte("keep-current")) {
		t.Fatalf("session file changed: %s", session)
	}
}

func TestBackupAuthenticationAndPathValidation(t *testing.T) {
	s := newBackupTestServer(t)
	if err := os.WriteFile(s.cfg.DataPath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	metadata, err := s.createBackup("tamper")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.cfg.BackupDir, metadata.ID+".mpbackup")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 0xff
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.readAndVerifyBackup(metadata.ID); err == nil || !strings.Contains(err.Error(), "authentication") {
		t.Fatalf("tampered backup error = %v", err)
	}
	for _, path := range []string{"../server.json", "data/../../server.json", "/data/server.json", `data\\server.json`, "data/auth-sessions.json"} {
		if safeDataBackupPath(path) {
			t.Fatalf("unsafe path accepted: %q", path)
		}
	}
}

func TestBackupRequiresDedicatedStrongKey(t *testing.T) {
	s := newBackupTestServer(t)
	s.cfg.BackupEncryptionKey = ""
	if _, err := s.createBackup("disabled"); err == nil {
		t.Fatal("backup should require BACKUP_ENCRYPTION_KEY")
	}
}

func TestSQLiteBackupUsesConsistentDatabaseSnapshot(t *testing.T) {
	dir := t.TempDir()
	dataPath := filepath.Join(dir, "server.json")
	dbPath := filepath.Join(dir, "server.db")
	store, err := NewSQLiteStore(dbPath, dataPath, "sqlite-backup-test-auth-secret")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.AddPlannedNode("snapshot-node", 10); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		cfg: Config{
			DataPath: dataPath, DBPath: dbPath, StoreDriver: "sqlite", MaxNodes: 10,
			BackupDir:           filepath.Join(dir, "backups"),
			BackupEncryptionKey: "sqlite-high-entropy-backup-test-key-32-bytes",
		},
		store: store,
		cache: NewResponseCache(),
	}
	metadata, err := s.createBackup("sqlite-test")
	if err != nil {
		t.Fatal(err)
	}
	verified, err := s.readAndVerifyBackup(metadata.ID)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Manifest.ObjectCounts["planned_nodes"] != 1 {
		t.Fatalf("sqlite object counts = %#v", verified.Manifest.ObjectCounts)
	}
	foundDatabase := false
	for _, file := range verified.Manifest.Files {
		if file.Role == "database" && strings.HasSuffix(file.Path, "server.db") {
			foundDatabase = true
		}
	}
	if !foundDatabase {
		t.Fatalf("database snapshot missing: %#v", verified.Manifest.Files)
	}
}

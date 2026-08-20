package server

import (
	"archive/zip"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	backupFormatVersion = 1
	backupMagic         = "MPBACKUP1"
	maxBackupPlainSize  = int64(512 << 20)
	maxBackupFileSize   = int64(256 << 20)
)

type BackupFile struct {
	Path   string `json:"path"`
	Role   string `json:"role,omitempty"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type BackupConfig struct {
	StoreDriver  string `json:"store_driver"`
	DataFile     string `json:"data_file"`
	DatabaseFile string `json:"database_file,omitempty"`
	PublicURL    string `json:"public_url,omitempty"`
	MaxNodes     int    `json:"max_nodes"`
}

type BackupManifest struct {
	FormatVersion int            `json:"format_version"`
	CreatedAt     string         `json:"created_at"`
	AppVersion    string         `json:"app_version"`
	Reason        string         `json:"reason,omitempty"`
	Files         []BackupFile   `json:"files"`
	Directories   []string       `json:"directories,omitempty"`
	ObjectCounts  map[string]int `json:"object_counts"`
	Config        BackupConfig   `json:"config"`
}

type BackupMetadata struct {
	ID           string         `json:"id"`
	CreatedAt    string         `json:"created_at"`
	AppVersion   string         `json:"app_version"`
	Reason       string         `json:"reason,omitempty"`
	Size         int64          `json:"size"`
	SHA256       string         `json:"sha256"`
	ObjectCounts map[string]int `json:"object_counts"`
	RemoteState  string         `json:"remote_state,omitempty"`
	RemoteError  string         `json:"remote_error,omitempty"`
}

type verifiedBackup struct {
	Manifest BackupManifest
	Files    map[string][]byte
}

type backupReplacement struct {
	target string
	staged string
	old    string
	hadOld bool
}

func (s *Server) backupReady() error {
	if isWeakBackupSecret(s.cfg.BackupEncryptionKey) {
		return errors.New("backup is disabled: set BACKUP_ENCRYPTION_KEY to a high-entropy value of at least 32 characters")
	}
	dataRoot, _ := filepath.Abs(filepath.Dir(s.cfg.DataPath))
	backupRoot, _ := filepath.Abs(s.cfg.BackupDir)
	if samePath(dataRoot, backupRoot) || pathWithin(dataRoot, backupRoot) {
		return errors.New("BACKUP_DIR must not contain the application data directory")
	}
	if pathWithin(backupRoot, dataRoot) {
		rel, err := filepath.Rel(dataRoot, backupRoot)
		if err != nil || strings.Contains(rel, string(filepath.Separator)) {
			return errors.New("BACKUP_DIR inside the data directory must be a direct child, such as data/backups")
		}
	}
	return nil
}

func (s *Server) handleAdminBackups(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		http.Error(w, "admin login required", http.StatusUnauthorized)
		return
	}
	if err := s.backupReady(); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	base := "/api/admin/backups"
	action := strings.Trim(strings.TrimPrefix(r.URL.Path, base), "/")
	if action == "" {
		switch r.Method {
		case http.MethodGet:
			items, err := s.listBackups()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, map[string]any{"enabled": true, "backups": items})
		case http.MethodPost:
			if !s.validAdminOrigin(r) {
				http.Error(w, "invalid request origin", http.StatusForbidden)
				return
			}
			s.backupMu.Lock()
			metadata, err := s.createBackup("manual")
			s.backupMu.Unlock()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, metadata)
		default:
			methodNotAllowed(w)
		}
		return
	}

	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if !validBackupID(id) {
		http.Error(w, "invalid backup id", http.StatusBadRequest)
		return
	}
	switch action {
	case "download":
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		path, err := s.backupPath(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", id+".mpbackup"))
		http.ServeFile(w, r, path)
	case "verify", "restore/preview":
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		s.backupMu.Lock()
		verified, err := s.readAndVerifyBackup(id)
		s.backupMu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		if action == "verify" {
			writeJSON(w, map[string]any{"ok": true, "manifest": verified.Manifest})
			return
		}
		writeJSON(w, s.restorePreview(id, verified))
	case "restore/apply":
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		if !s.validAdminOrigin(r) {
			http.Error(w, "invalid request origin", http.StatusForbidden)
			return
		}
		defer r.Body.Close()
		var req struct {
			Confirm bool `json:"confirm"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil || !req.Confirm {
			http.Error(w, "restore requires {\"confirm\":true}", http.StatusBadRequest)
			return
		}
		s.backupMu.Lock()
		result, err := s.applyBackup(id)
		s.backupMu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		writeJSON(w, result)
	default:
		http.NotFound(w, r)
	}
}

func validBackupID(id string) bool {
	if len(id) < 20 || len(id) > 80 {
		return false
	}
	for _, r := range id {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func newBackupID() (string, error) {
	var random [6]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return "backup-" + time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(random[:]), nil
}

func (s *Server) backupPath(id string) (string, error) {
	if !validBackupID(id) {
		return "", errors.New("invalid backup id")
	}
	return filepath.Join(s.cfg.BackupDir, id+".mpbackup"), nil
}

func (s *Server) createBackup(reason string) (BackupMetadata, error) {
	if err := s.backupReady(); err != nil {
		return BackupMetadata{}, err
	}
	id, err := newBackupID()
	if err != nil {
		return BackupMetadata{}, err
	}
	files, directories, cleanup, err := s.collectBackupFiles()
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return BackupMetadata{}, err
	}
	manifest := s.buildBackupManifest(reason, files, directories)
	plain, err := encodeBackupArchive(manifest, files)
	if err != nil {
		return BackupMetadata{}, err
	}
	encrypted, err := encryptBackup(plain, s.cfg.BackupEncryptionKey)
	if err != nil {
		return BackupMetadata{}, err
	}
	if err := os.MkdirAll(s.cfg.BackupDir, 0o700); err != nil {
		return BackupMetadata{}, err
	}
	path, _ := s.backupPath(id)
	if err := writeBytesAtomic(path, encrypted, 0o600); err != nil {
		return BackupMetadata{}, err
	}
	sum := sha256.Sum256(encrypted)
	metadata := BackupMetadata{
		ID: id, CreatedAt: manifest.CreatedAt, AppVersion: manifest.AppVersion,
		Reason: reason, Size: int64(len(encrypted)), SHA256: hex.EncodeToString(sum[:]),
		ObjectCounts: manifest.ObjectCounts,
	}
	if err := writeJSONAtomic(filepath.Join(s.cfg.BackupDir, id+".json"), metadata, 0o600); err != nil {
		_ = os.Remove(path)
		return BackupMetadata{}, err
	}
	if s.cfg.BackupWebDAVURL != "" {
		metadata.RemoteState = "synced"
		if err := s.uploadBackupWebDAV(id, path); err != nil {
			metadata.RemoteState = "failed"
			metadata.RemoteError = err.Error()
		}
		_ = writeJSONAtomic(filepath.Join(s.cfg.BackupDir, id+".json"), metadata, 0o600)
	}
	return metadata, nil
}

func (s *Server) collectBackupFiles() (map[string][]byte, []string, func(), error) {
	dataRoot := filepath.Clean(filepath.Dir(s.cfg.DataPath))
	files := map[string][]byte{}
	directories := []string{}
	total := int64(0)
	var dbSnapshot string
	cleanup := func() {
		if dbSnapshot != "" {
			_ = os.Remove(dbSnapshot)
		}
	}

	dbPath := strings.TrimSpace(s.cfg.DBPath)
	if dbPath == "" {
		dbPath = defaultSQLitePath(s.cfg.DataPath)
	}
	if sqliteStore, ok := s.store.(*SQLiteStore); ok {
		tmp, err := os.CreateTemp("", "monitor-backup-*.db")
		if err != nil {
			return nil, nil, cleanup, err
		}
		dbSnapshot = tmp.Name()
		_ = tmp.Close()
		_ = os.Remove(dbSnapshot)
		sqliteStore.mu.Lock()
		_, _ = sqliteStore.db.Exec("PRAGMA wal_checkpoint(FULL)")
		_, err = sqliteStore.db.Exec("VACUUM INTO '" + strings.ReplaceAll(dbSnapshot, "'", "''") + "'")
		sqliteStore.mu.Unlock()
		if err != nil {
			cleanup()
			return nil, nil, func() {}, fmt.Errorf("create sqlite snapshot: %w", err)
		}
	}

	backupAbs, _ := filepath.Abs(s.cfg.BackupDir)
	dbAbs, _ := filepath.Abs(dbPath)
	err := filepath.WalkDir(dataRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		abs, _ := filepath.Abs(path)
		if entry.IsDir() && pathWithin(abs, backupAbs) {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(dataRoot, path)
		if err != nil || rel == "." {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			directories = append(directories, "data/"+rel)
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("backup refuses non-regular file %s", rel)
		}
		name := strings.ToLower(filepath.Base(path))
		if name == "auth-sessions.json" || strings.HasSuffix(name, ".tmp") || strings.HasSuffix(name, "-wal") || strings.HasSuffix(name, "-shm") {
			return nil
		}
		if dbSnapshot != "" && samePath(abs, dbAbs) {
			return nil
		}
		if info.Size() > maxBackupFileSize || total+info.Size() > maxBackupPlainSize {
			return errors.New("backup data exceeds configured size limit")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files["data/"+rel] = raw
		total += int64(len(raw))
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		cleanup()
		return nil, nil, func() {}, err
	}
	if dbSnapshot != "" {
		raw, err := os.ReadFile(dbSnapshot)
		if err != nil {
			cleanup()
			return nil, nil, func() {}, err
		}
		if int64(len(raw))+total > maxBackupPlainSize {
			cleanup()
			return nil, nil, func() {}, errors.New("backup data exceeds configured size limit")
		}
		rel, err := filepath.Rel(dataRoot, dbPath)
		archivePath := "database/" + filepath.Base(dbPath)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			archivePath = "data/" + filepath.ToSlash(rel)
		}
		files[archivePath] = raw
	}
	sort.Strings(directories)
	return files, directories, cleanup, nil
}

func pathWithin(path, parent string) bool {
	if path == "" || parent == "" {
		return false
	}
	rel, err := filepath.Rel(parent, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

func (s *Server) buildBackupManifest(reason string, payload map[string][]byte, directories []string) BackupManifest {
	files := make([]BackupFile, 0, len(payload))
	databaseArchivePath := "database/" + filepath.Base(s.databasePath())
	if rel, err := filepath.Rel(filepath.Dir(s.cfg.DataPath), s.databasePath()); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		databaseArchivePath = "data/" + filepath.ToSlash(rel)
	}
	for path, raw := range payload {
		sum := sha256.Sum256(raw)
		role := "data"
		if path == "data/"+filepath.ToSlash(filepath.Base(s.cfg.DataPath)) {
			role = "primary_data"
		}
		if _, sqlite := s.store.(*SQLiteStore); sqlite && path == databaseArchivePath {
			role = "database"
		}
		files = append(files, BackupFile{Path: path, Role: role, Size: int64(len(raw)), SHA256: hex.EncodeToString(sum[:])})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	driver := canonicalStoreDriver(s.cfg.StoreDriver)
	if driver == "" {
		if _, ok := s.store.(*SQLiteStore); ok {
			driver = "sqlite"
		} else {
			driver = "json"
		}
	}
	return BackupManifest{
		FormatVersion: backupFormatVersion,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		AppVersion:    buildVersion,
		Reason:        strings.TrimSpace(reason),
		Files:         files,
		Directories:   directories,
		ObjectCounts:  s.backupObjectCounts(payload),
		Config:        BackupConfig{StoreDriver: driver, DataFile: filepath.Base(s.cfg.DataPath), DatabaseFile: filepath.Base(s.databasePath()), PublicURL: s.cfg.PublicURL, MaxNodes: s.cfg.MaxNodes},
	}
}

func canonicalStoreDriver(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "sqlite", "sqlite3":
		return "sqlite"
	case "json", "file":
		return "json"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func (s *Server) databasePath() string {
	if strings.TrimSpace(s.cfg.DBPath) != "" {
		return s.cfg.DBPath
	}
	return defaultSQLitePath(s.cfg.DataPath)
}

func (s *Server) backupObjectCounts(payload map[string][]byte) map[string]int {
	counts := map[string]int{}
	dataKey := "data/" + filepath.ToSlash(filepath.Base(s.cfg.DataPath))
	if raw := payload[dataKey]; len(raw) > 0 {
		var root map[string]json.RawMessage
		if json.Unmarshal(raw, &root) == nil {
			for field, label := range map[string]string{"reports": "reports", "infos": "nodes", "planned": "planned_nodes", "owners": "owners", "listings": "market_listings", "advertisements": "ads", "friend_links": "friend_links", "recycle_bin": "recycle_bin"} {
				var values map[string]json.RawMessage
				if json.Unmarshal(root[field], &values) == nil {
					counts[label] = len(values)
				}
			}
			var audit []json.RawMessage
			if json.Unmarshal(root["audit_log"], &audit) == nil {
				counts["audit_log"] = len(audit)
			}
		}
	}
	for path, raw := range payload {
		if strings.HasSuffix(path, "/custom-release-notes.json") {
			var notes []ReleaseNote
			if json.Unmarshal(raw, &notes) == nil {
				counts["custom_release_notes"] = len(notes)
			}
		}
		if strings.HasPrefix(path, "data/ads/") {
			counts["ad_images"]++
		}
	}
	if _, ok := s.store.(*SQLiteStore); ok {
		for path, raw := range payload {
			if !strings.HasSuffix(path, ".db") {
				continue
			}
			tmp, err := os.CreateTemp("", "monitor-count-*.db")
			if err != nil {
				break
			}
			name := tmp.Name()
			_, _ = tmp.Write(raw)
			_ = tmp.Close()
			db, err := sql.Open("sqlite", name)
			if err == nil {
				for table, label := range map[string]string{"reports": "reports", "host_infos": "nodes", "planned_nodes": "planned_nodes", "owners": "owners", "market_listings": "market_listings", "advertisements": "ads", "friend_links": "friend_links", "recycle_bin": "recycle_bin", "audit_log": "audit_log"} {
					var count int
					if db.QueryRow("SELECT COUNT(*) FROM "+table).Scan(&count) == nil {
						counts[label] = count
					}
				}
				_ = db.Close()
			}
			_ = os.Remove(name)
			break
		}
	}
	return counts
}

func encodeBackupArchive(manifest BackupManifest, payload map[string][]byte) ([]byte, error) {
	var buffer bytes.Buffer
	zw := zip.NewWriter(&buffer)
	manifestRaw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	entries := map[string][]byte{"manifest.json": append(manifestRaw, '\n')}
	for path, raw := range payload {
		entries[path] = raw
	}
	paths := make([]string, 0, len(entries))
	for path := range entries {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		header := &zip.FileHeader{Name: path, Method: zip.Deflate}
		header.SetModTime(time.Unix(0, 0).UTC())
		header.SetMode(0o600)
		writer, err := zw.CreateHeader(header)
		if err != nil {
			return nil, err
		}
		if _, err := writer.Write(entries[path]); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	if int64(buffer.Len()) > maxBackupPlainSize {
		return nil, errors.New("backup archive exceeds configured size limit")
	}
	return buffer.Bytes(), nil
}

func encryptBackup(plain []byte, secret string) ([]byte, error) {
	key := sha256.Sum256([]byte("monitor-party-backup-v1\x00" + secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append([]byte(backupMagic), nonce...)
	out = gcm.Seal(out, nonce, plain, []byte(backupMagic))
	return out, nil
}

func decryptBackup(encrypted []byte, secret string) ([]byte, error) {
	if len(encrypted) < len(backupMagic)+12 || string(encrypted[:len(backupMagic)]) != backupMagic {
		return nil, errors.New("invalid backup header")
	}
	key := sha256.Sum256([]byte("monitor-party-backup-v1\x00" + secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(encrypted) < len(backupMagic)+gcm.NonceSize()+gcm.Overhead() {
		return nil, errors.New("truncated backup")
	}
	nonce := encrypted[len(backupMagic) : len(backupMagic)+gcm.NonceSize()]
	plain, err := gcm.Open(nil, nonce, encrypted[len(backupMagic)+gcm.NonceSize():], []byte(backupMagic))
	if err != nil {
		return nil, errors.New("backup authentication failed")
	}
	return plain, nil
}

func (s *Server) readAndVerifyBackup(id string) (verifiedBackup, error) {
	path, err := s.backupPath(id)
	if err != nil {
		return verifiedBackup{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return verifiedBackup{}, err
	}
	defer file.Close()
	encrypted, err := io.ReadAll(io.LimitReader(file, maxBackupPlainSize+32<<20))
	if err != nil {
		return verifiedBackup{}, err
	}
	if int64(len(encrypted)) > maxBackupPlainSize+16<<20 {
		return verifiedBackup{}, errors.New("encrypted backup exceeds configured size limit")
	}
	plain, err := decryptBackup(encrypted, s.cfg.BackupEncryptionKey)
	if err != nil {
		return verifiedBackup{}, err
	}
	reader, err := zip.NewReader(bytes.NewReader(plain), int64(len(plain)))
	if err != nil {
		return verifiedBackup{}, err
	}
	if len(reader.File) > 10000 {
		return verifiedBackup{}, errors.New("backup contains too many entries")
	}
	archive := map[string][]byte{}
	totalUncompressed := int64(0)
	for _, entry := range reader.File {
		name := filepath.ToSlash(entry.Name)
		if !safeArchivePath(name) || entry.Mode()&os.ModeSymlink != 0 || entry.UncompressedSize64 > uint64(maxBackupFileSize) {
			return verifiedBackup{}, fmt.Errorf("unsafe backup entry %q", name)
		}
		if _, exists := archive[name]; exists {
			return verifiedBackup{}, fmt.Errorf("duplicate backup entry %q", name)
		}
		stream, err := entry.Open()
		if err != nil {
			return verifiedBackup{}, err
		}
		raw, readErr := io.ReadAll(io.LimitReader(stream, maxBackupFileSize+1))
		_ = stream.Close()
		if readErr != nil || int64(len(raw)) > maxBackupFileSize {
			return verifiedBackup{}, errors.New("backup entry exceeds configured size limit")
		}
		totalUncompressed += int64(len(raw))
		if totalUncompressed > maxBackupPlainSize {
			return verifiedBackup{}, errors.New("expanded backup exceeds configured size limit")
		}
		archive[name] = raw
	}
	manifestRaw, ok := archive["manifest.json"]
	if !ok {
		return verifiedBackup{}, errors.New("backup manifest is missing")
	}
	delete(archive, "manifest.json")
	var manifest BackupManifest
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		return verifiedBackup{}, err
	}
	if manifest.FormatVersion != backupFormatVersion {
		return verifiedBackup{}, fmt.Errorf("unsupported backup format %d", manifest.FormatVersion)
	}
	if _, err := time.Parse(time.RFC3339, manifest.CreatedAt); err != nil {
		return verifiedBackup{}, errors.New("backup manifest has an invalid creation time")
	}
	if len(manifest.Files) != len(archive) {
		return verifiedBackup{}, errors.New("backup file list does not match archive")
	}
	seen := map[string]bool{}
	for _, file := range manifest.Files {
		if seen[file.Path] || !safeDataBackupPath(file.Path) || (file.Role != "" && file.Role != "data" && file.Role != "primary_data" && file.Role != "database") {
			return verifiedBackup{}, fmt.Errorf("invalid manifest path %q", file.Path)
		}
		seen[file.Path] = true
		raw, ok := archive[file.Path]
		if !ok || int64(len(raw)) != file.Size {
			return verifiedBackup{}, fmt.Errorf("backup file size mismatch for %q", file.Path)
		}
		sum := sha256.Sum256(raw)
		if !strings.EqualFold(hex.EncodeToString(sum[:]), file.SHA256) {
			return verifiedBackup{}, fmt.Errorf("backup checksum mismatch for %q", file.Path)
		}
	}
	for _, dir := range manifest.Directories {
		if !strings.HasPrefix(dir, "data/") || !safeArchivePath(dir) {
			return verifiedBackup{}, fmt.Errorf("invalid backup directory %q", dir)
		}
		if _, err := s.restoreDirectoryTarget(dir); err != nil {
			return verifiedBackup{}, err
		}
	}
	return verifiedBackup{Manifest: manifest, Files: archive}, nil
}

func (s *Server) restoreDirectoryTarget(archivePath string) (string, error) {
	if !strings.HasPrefix(archivePath, "data/") || !safeArchivePath(archivePath) {
		return "", errors.New("invalid restore directory")
	}
	dataRoot, _ := filepath.Abs(filepath.Dir(s.cfg.DataPath))
	target, _ := filepath.Abs(filepath.Join(dataRoot, filepath.FromSlash(strings.TrimPrefix(archivePath, "data/"))))
	if !pathWithin(target, dataRoot) || samePath(target, dataRoot) {
		return "", errors.New("restore directory escapes data directory")
	}
	backupRoot, _ := filepath.Abs(s.cfg.BackupDir)
	if samePath(target, backupRoot) || pathWithin(backupRoot, target) || pathWithin(target, backupRoot) {
		return "", errors.New("backup cannot replace BACKUP_DIR")
	}
	return target, nil
}

func safeArchivePath(name string) bool {
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") || strings.ContainsRune(name, '\x00') {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(name))
	return clean == name && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

func safeDataBackupPath(name string) bool {
	if !safeArchivePath(name) || (!strings.HasPrefix(name, "data/") && !strings.HasPrefix(name, "database/")) {
		return false
	}
	base := strings.ToLower(filepath.Base(name))
	return base != "auth-sessions.json" && !strings.HasSuffix(base, "-wal") && !strings.HasSuffix(base, "-shm")
}

func (s *Server) listBackups() ([]BackupMetadata, error) {
	entries, err := os.ReadDir(s.cfg.BackupDir)
	if errors.Is(err, os.ErrNotExist) {
		return []BackupMetadata{}, nil
	}
	if err != nil {
		return nil, err
	}
	items := []BackupMetadata{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		var metadata BackupMetadata
		raw, err := os.ReadFile(filepath.Join(s.cfg.BackupDir, entry.Name()))
		if err != nil || json.Unmarshal(raw, &metadata) != nil || !validBackupID(metadata.ID) {
			continue
		}
		if _, err := os.Stat(filepath.Join(s.cfg.BackupDir, metadata.ID+".mpbackup")); err != nil {
			continue
		}
		items = append(items, metadata)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt > items[j].CreatedAt })
	return items, nil
}

func (s *Server) restorePreview(id string, backup verifiedBackup) map[string]any {
	changes := make([]map[string]any, 0, len(backup.Manifest.Files))
	for _, file := range backup.Manifest.Files {
		target, err := s.restoreTarget(file)
		if err != nil {
			changes = append(changes, map[string]any{"path": file.Path, "action": "incompatible", "error": err.Error()})
			continue
		}
		action := "create"
		currentSize := int64(0)
		if info, err := os.Stat(target); err == nil {
			action, currentSize = "replace", info.Size()
		}
		changes = append(changes, map[string]any{"path": file.Path, "action": action, "current_size": currentSize, "backup_size": file.Size})
	}
	currentDriver := canonicalStoreDriver(s.cfg.StoreDriver)
	if currentDriver == "" {
		if _, ok := s.store.(*SQLiteStore); ok {
			currentDriver = "sqlite"
		} else {
			currentDriver = "json"
		}
	}
	driverMatches := canonicalStoreDriver(backup.Manifest.Config.StoreDriver) == currentDriver
	return map[string]any{"id": id, "verified": true, "compatible": driverMatches, "store_driver_matches": driverMatches, "manifest": backup.Manifest, "changes": changes, "sessions_restored": false, "restart_required": true}
}

func (s *Server) restoreTarget(file BackupFile) (string, error) {
	if file.Role == "primary_data" {
		if s.restoreTargetOverlapsBackupDir(s.cfg.DataPath) {
			return "", errors.New("data restore target overlaps BACKUP_DIR")
		}
		return s.cfg.DataPath, nil
	}
	if file.Role == "database" {
		target := s.databasePath()
		if s.restoreTargetOverlapsBackupDir(target) {
			return "", errors.New("database restore target overlaps BACKUP_DIR")
		}
		return target, nil
	}
	if !strings.HasPrefix(file.Path, "data/") {
		return "", errors.New("unsupported restore target")
	}
	rel := strings.TrimPrefix(file.Path, "data/")
	target := filepath.Join(filepath.Dir(s.cfg.DataPath), filepath.FromSlash(rel))
	rootAbs, _ := filepath.Abs(filepath.Dir(s.cfg.DataPath))
	targetAbs, _ := filepath.Abs(target)
	if !pathWithin(targetAbs, rootAbs) {
		return "", errors.New("restore target escapes data directory")
	}
	if s.restoreTargetOverlapsBackupDir(targetAbs) {
		return "", errors.New("restore target overlaps BACKUP_DIR")
	}
	return target, nil
}

func (s *Server) restoreTargetOverlapsBackupDir(target string) bool {
	targetAbs, _ := filepath.Abs(target)
	backupRoot, _ := filepath.Abs(s.cfg.BackupDir)
	return samePath(targetAbs, backupRoot) || pathWithin(targetAbs, backupRoot) || pathWithin(backupRoot, targetAbs)
}

func (s *Server) applyBackup(id string) (map[string]any, error) {
	backup, err := s.readAndVerifyBackup(id)
	if err != nil {
		return nil, err
	}
	if backup.Manifest.Config.StoreDriver != "" {
		current := canonicalStoreDriver(s.cfg.StoreDriver)
		if current == "" {
			if _, ok := s.store.(*SQLiteStore); ok {
				current = "sqlite"
			} else {
				current = "json"
			}
		}
		if current != canonicalStoreDriver(backup.Manifest.Config.StoreDriver) {
			return nil, fmt.Errorf("backup store driver %q does not match current driver %q", backup.Manifest.Config.StoreDriver, current)
		}
	}
	rollback, err := s.createBackup("pre-restore-" + id)
	if err != nil {
		return nil, fmt.Errorf("create pre-restore backup: %w", err)
	}
	stage, err := os.MkdirTemp(filepath.Dir(s.cfg.DataPath), ".monitor-restore-stage-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	rollbackDir, err := os.MkdirTemp(filepath.Dir(s.cfg.DataPath), ".monitor-restore-rollback-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(rollbackDir)

	dataRoot := filepath.Dir(s.cfg.DataPath)
	directoryRoots := map[string]bool{}
	for _, dir := range backup.Manifest.Directories {
		rel := strings.TrimPrefix(dir, "data/")
		first, _, _ := strings.Cut(rel, "/")
		if first != "" && first != "." {
			directoryRoots[first] = true
		}
	}
	// Recreate complete backed-up directories in staging. Swapping a directory
	// removes stale ad images that no longer exist in the selected backup.
	for root := range directoryRoots {
		if err := os.MkdirAll(filepath.Join(stage, "data", filepath.FromSlash(root)), 0o700); err != nil {
			return nil, err
		}
	}
	for path, raw := range backup.Files {
		if strings.HasPrefix(path, "data/") {
			rel := strings.TrimPrefix(path, "data/")
			stagedPath := filepath.Join(stage, "data", filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(stagedPath), 0o700); err != nil {
				return nil, err
			}
			if err := os.WriteFile(stagedPath, raw, 0o600); err != nil {
				return nil, err
			}
		}
	}

	replacements := make([]backupReplacement, 0, len(backup.Manifest.Files)+len(directoryRoots))
	seenTarget := map[string]bool{}
	for root := range directoryRoots {
		target := filepath.Join(dataRoot, filepath.FromSlash(root))
		staged := filepath.Join(stage, "data", filepath.FromSlash(root))
		old := filepath.Join(rollbackDir, "dir-"+base64.RawURLEncoding.EncodeToString([]byte(root)))
		replacements = append(replacements, backupReplacement{target: target, staged: staged, old: old})
		abs, _ := filepath.Abs(target)
		seenTarget[strings.ToLower(abs)] = true
	}
	for _, file := range backup.Manifest.Files {
		target, err := s.restoreTarget(file)
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(file.Path, "data/") {
			rel := strings.TrimPrefix(file.Path, "data/")
			first, _, _ := strings.Cut(rel, "/")
			if directoryRoots[first] {
				continue
			}
		}
		targetAbs, _ := filepath.Abs(target)
		if seenTarget[strings.ToLower(targetAbs)] {
			return nil, errors.New("backup maps multiple entries to one restore target")
		}
		seenTarget[strings.ToLower(targetAbs)] = true
		staged := filepath.Join(stage, "files", base64.RawURLEncoding.EncodeToString([]byte(file.Path)))
		if err := os.MkdirAll(filepath.Dir(staged), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(staged, backup.Files[file.Path], 0o600); err != nil {
			return nil, err
		}
		old := filepath.Join(rollbackDir, base64.RawURLEncoding.EncodeToString([]byte(file.Path)))
		replacements = append(replacements, backupReplacement{target: target, staged: staged, old: old})
	}

	completed := 0
	for i := range replacements {
		replacement := &replacements[i]
		if err := os.MkdirAll(filepath.Dir(replacement.target), 0o700); err != nil {
			rollbackReplacements(replacements[:completed])
			return nil, err
		}
		if _, err := os.Stat(replacement.target); err == nil {
			replacement.hadOld = true
			if err := os.Rename(replacement.target, replacement.old); err != nil {
				rollbackReplacements(replacements[:completed])
				return nil, err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			rollbackReplacements(replacements[:completed])
			return nil, err
		}
		if err := os.Rename(replacement.staged, replacement.target); err != nil {
			if replacement.hadOld {
				_ = os.Rename(replacement.old, replacement.target)
			}
			rollbackReplacements(replacements[:completed])
			return nil, err
		}
		completed++
	}
	s.cache.MarkDirty()
	return map[string]any{"ok": true, "restored_backup": id, "rollback_backup": rollback.ID, "files_restored": len(backup.Manifest.Files), "sessions_restored": false, "restart_required": true}, nil
}

func rollbackReplacements(replacements []backupReplacement) {
	for i := len(replacements) - 1; i >= 0; i-- {
		_ = os.RemoveAll(replacements[i].target)
		if replacements[i].hadOld {
			_ = os.Rename(replacements[i].old, replacements[i].target)
		}
	}
}

func writeBytesAtomic(path string, raw []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".backup-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

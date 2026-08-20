package server

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	serverapp "vps-agent/internal/server/application"
)

type Config struct {
	Addr                 string
	AuthSecret           string
	AdminUser            string
	AdminPass            string
	DataPath             string
	StoreDriver          string
	DBPath               string
	PublicURL            string
	PublicMonitorDetails bool
	CORSOrigins          []string
	OfflineWait          time.Duration
	MaxNodes             int
	UpdateRepo           string
	GitHubToken          string
	UpdateEnabled        bool
	UpdateAPIBase        string
	BackupEncryptionKey  string
	BackupDir            string
	BackupInterval       time.Duration
	BackupWebDAVURL      string
	BackupWebDAVUser     string
	BackupWebDAVPassword string
}

func normalizeConfig(cfg Config) (Config, error) {
	if isWeakSecret(cfg.AuthSecret) {
		return Config{}, errors.New("AUTH_SECRET must be at least 16 characters and not use a known placeholder")
	}
	if cfg.AdminUser == "" {
		cfg.AdminUser = "admin"
	}
	if isWeakSecret(cfg.AdminPass) {
		return Config{}, errors.New("ADMIN_PASS must be at least 16 characters and not use a known placeholder")
	}
	if cfg.PublicURL != "" {
		publicURL, err := cleanPublicURL(cfg.PublicURL)
		if err != nil {
			return Config{}, err
		}
		cfg.PublicURL = publicURL
	}
	if cfg.DataPath == "" {
		cfg.DataPath = "data/server.json"
	}
	if cfg.BackupEncryptionKey != "" && isWeakBackupSecret(cfg.BackupEncryptionKey) {
		return Config{}, errors.New("BACKUP_ENCRYPTION_KEY must be a high-entropy value of at least 32 characters")
	}
	if strings.TrimSpace(cfg.BackupDir) == "" {
		cfg.BackupDir = filepath.Join(filepath.Dir(cfg.DataPath), "backups")
	}
	if cfg.BackupInterval < 0 || (cfg.BackupInterval > 0 && cfg.BackupInterval < time.Hour) {
		return Config{}, errors.New("BACKUP_INTERVAL must be 0 or at least 1h")
	}
	if strings.TrimSpace(cfg.BackupWebDAVURL) != "" {
		cleaned, err := cleanBackupWebDAVURL(cfg.BackupWebDAVURL)
		if err != nil {
			return Config{}, err
		}
		cfg.BackupWebDAVURL = cleaned
	}
	if cfg.OfflineWait == 0 {
		cfg.OfflineWait = 10 * time.Second
	}
	if cfg.MaxNodes == 0 {
		cfg.MaxNodes = 2000
	}
	if strings.TrimSpace(cfg.UpdateRepo) == "" {
		cfg.UpdateRepo = "ithtelab/yunjing-monitor"
	}
	if !validUpdateRepo(cfg.UpdateRepo) {
		return Config{}, errors.New("UPDATE_REPOSITORY must use owner/repository format")
	}
	cfg.UpdateRepo = strings.TrimSpace(cfg.UpdateRepo)
	if strings.TrimSpace(cfg.UpdateAPIBase) == "" {
		cfg.UpdateAPIBase = "https://api.github.com"
	}
	cfg.UpdateAPIBase = strings.TrimRight(strings.TrimSpace(cfg.UpdateAPIBase), "/")
	if cfg.OfflineWait < time.Second {
		return Config{}, errors.New("OFFLINE_WAIT must be >= 1s")
	}
	if cfg.MaxNodes <= 0 {
		return Config{}, errors.New("MAX_NODES must be positive")
	}
	origins, err := cleanOriginList(cfg.CORSOrigins)
	if err != nil {
		return Config{}, err
	}
	cfg.CORSOrigins = origins
	return cfg, nil
}

func validUpdateRepo(value string) bool {
	parts := strings.Split(strings.TrimSpace(value), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	for _, part := range parts {
		for _, r := range part {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
				continue
			}
			return false
		}
	}
	return true
}

func newStoreBackend(cfg Config) (serverapp.Store, error) {
	driver := strings.ToLower(strings.TrimSpace(cfg.StoreDriver))
	if driver == "" && cfg.DBPath != "" {
		driver = "sqlite"
	}
	if driver == "" {
		driver = "json"
	}
	switch driver {
	case "json", "file":
		return NewStore(cfg.DataPath, cfg.AuthSecret)
	case "sqlite", "sqlite3":
		dbPath := strings.TrimSpace(cfg.DBPath)
		if dbPath == "" {
			dbPath = defaultSQLitePath(cfg.DataPath)
		}
		return NewSQLiteStore(dbPath, cfg.DataPath, cfg.AuthSecret)
	default:
		return nil, fmt.Errorf("unsupported STORE_DRIVER %q", cfg.StoreDriver)
	}
}

func defaultSQLitePath(dataPath string) string {
	if dataPath == "" {
		return "data/server.db"
	}
	ext := filepath.Ext(dataPath)
	if ext == "" {
		return dataPath + ".db"
	}
	return strings.TrimSuffix(dataPath, ext) + ".db"
}

func isWeakSecret(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 16 {
		return true
	}
	value = strings.ToLower(value)
	for _, weak := range []string{
		"change-me",
		"please-change-admin-password",
		"please-change-this-auth-secret-before-going-online",
	} {
		if value == weak {
			return true
		}
	}
	return false
}

func isWeakBackupSecret(value string) bool {
	if len(value) < 32 {
		return true
	}
	distinct := map[rune]bool{}
	for _, r := range value {
		distinct[r] = true
	}
	return len(distinct) < 10 || strings.Contains(strings.ToLower(value), "change-me")
}

func cleanPublicURL(value string) (string, error) {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return "", errors.New("PUBLIC_URL must be an absolute http or https URL")
	}
	host, err := normalizeExternalHost(u.Host)
	if err != nil {
		return "", errors.New("PUBLIC_URL must contain a valid host")
	}
	if u.Scheme != "https" && !strings.HasPrefix(u.Host, "127.0.0.1") && !strings.HasPrefix(u.Host, "localhost") {
		return "", errors.New("PUBLIC_URL must use https outside localhost")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.Host = host
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func cleanOriginList(values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if value == "*" {
			if !seen[value] {
				out = append(out, value)
				seen[value] = true
			}
			continue
		}
		origin, err := cleanOrigin(value)
		if err != nil {
			return nil, err
		}
		if !seen[origin] {
			out = append(out, origin)
			seen[origin] = true
		}
	}
	return out, nil
}

func cleanOrigin(value string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", fmt.Errorf("CORS_ORIGINS contains invalid origin %q", value)
	}
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/"), nil
}

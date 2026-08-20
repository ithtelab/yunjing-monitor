package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	serverdomain "vps-agent/internal/server/domain"

	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	mu          sync.Mutex
	db          *sql.DB
	path        string
	tokenCipher *tokenCipher
	tokenSecret string
}

func NewSQLiteStore(path, importJSONPath string, tokenSecret ...string) (*SQLiteStore, error) {
	secret := "monitor-party-test-token-secret"
	if len(tokenSecret) > 0 {
		secret = tokenSecret[0]
	}
	cipher, err := newTokenCipher(secret)
	if err != nil {
		return nil, err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("sqlite DB path is required")
	}
	if path != ":memory:" && !strings.HasPrefix(path, "file:") {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &SQLiteStore{db: db, path: path, tokenCipher: cipher, tokenSecret: secret}
	if err := s.configure(); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.initSchema(); err != nil {
		db.Close()
		return nil, err
	}
	empty, err := s.isEmpty()
	if err != nil {
		db.Close()
		return nil, err
	}
	if empty {
		if err := s.importJSONIfPresent(importJSONPath); err != nil {
			db.Close()
			return nil, err
		}
	}
	if err := s.ensureDefaultSettings(); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.migratePlaintextTokens(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SQLiteStore) migratePlaintextTokens() error {
	rows, err := s.db.Query(`SELECT node_id, token FROM planned_nodes WHERE token <> ''`)
	if err != nil {
		return err
	}
	type tokenRow struct{ nodeID, value string }
	var pending []tokenRow
	for rows.Next() {
		var row tokenRow
		if err := rows.Scan(&row.nodeID, &row.value); err != nil {
			_ = rows.Close()
			return err
		}
		if !strings.HasPrefix(row.value, encryptedTokenPrefix) {
			pending = append(pending, row)
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, row := range pending {
		encrypted, err := s.tokenCipher.encrypt(row.value)
		if err != nil {
			return fmt.Errorf("migrate token for node %q: %w", row.nodeID, err)
		}
		if _, err := s.db.Exec(`UPDATE planned_nodes SET token = ? WHERE node_id = ? AND token = ?`, encrypted, row.nodeID, row.value); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) configure() error {
	pragmas := []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
	}
	for _, query := range pragmas {
		if _, err := s.db.Exec(query); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) initSchema() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS planned_nodes (
			node_id TEXT PRIMARY KEY,
			owner_id TEXT NOT NULL DEFAULT '',
			private INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL,
			token_hash TEXT NOT NULL DEFAULT '',
			token TEXT NOT NULL DEFAULT '',
			display_name TEXT NOT NULL DEFAULT '',
			region TEXT NOT NULL DEFAULT '',
			region_code TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS host_infos (
			node_id TEXT PRIMARY KEY,
			info_json TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS reports (
			node_id TEXT PRIMARY KEY,
			ts INTEGER NOT NULL,
			metrics_json TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS traffic_stats (
			node_id TEXT PRIMARY KEY,
			reset_day INTEGER NOT NULL,
			period_start INTEGER NOT NULL,
			next_reset INTEGER NOT NULL,
			last_rx_bytes TEXT NOT NULL,
			last_tx_bytes TEXT NOT NULL,
			rx_total TEXT NOT NULL,
			tx_total TEXT NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES (1, strftime('%s', 'now'))`,
		`CREATE TABLE IF NOT EXISTS owners (
			id TEXT PRIMARY KEY,
			email TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			last_login INTEGER NOT NULL DEFAULT 0,
			disabled INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS market_listings (
			node_id TEXT PRIMARY KEY,
			owner_id TEXT NOT NULL,
			for_sale INTEGER NOT NULL DEFAULT 1,
			listing_type TEXT NOT NULL DEFAULT '',
			contact TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			specs TEXT NOT NULL DEFAULT '',
			price TEXT NOT NULL DEFAULT '',
			price_amount REAL NOT NULL DEFAULT 0,
			price_currency TEXT NOT NULL DEFAULT '',
			billing_cycle TEXT NOT NULL DEFAULT '',
			pinned INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			moderation_status TEXT NOT NULL DEFAULT 'approved',
			rejection_reason TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_market_listings_owner ON market_listings(owner_id)`,
		`CREATE INDEX IF NOT EXISTS idx_market_listings_for_sale ON market_listings(for_sale)`,
		`INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES (2, strftime('%s', 'now'))`,
		`CREATE TABLE IF NOT EXISTS advertisements (
			id TEXT PRIMARY KEY,
			brand TEXT NOT NULL DEFAULT '', title TEXT NOT NULL, description TEXT NOT NULL DEFAULT '',
			image_url TEXT NOT NULL, target_url TEXT NOT NULL, button_text TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1, pinned INTEGER NOT NULL DEFAULT 0,
			recommended INTEGER NOT NULL DEFAULT 0, priority INTEGER NOT NULL DEFAULT 0,
			position_mode TEXT NOT NULL DEFAULT 'auto', desktop_after INTEGER NOT NULL DEFAULT 0,
			mobile_after INTEGER NOT NULL DEFAULT 0, start_at INTEGER NOT NULL DEFAULT 0,
			end_at INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
			impressions INTEGER NOT NULL DEFAULT 0, clicks INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_advertisements_schedule ON advertisements(enabled, start_at, end_at)`,
		`INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES (3, strftime('%s', 'now'))`,
		`CREATE TABLE IF NOT EXISTS friend_links (
			id TEXT PRIMARY KEY, label TEXT NOT NULL, url TEXT NOT NULL UNIQUE,
			enabled INTEGER NOT NULL DEFAULT 1, new_tab INTEGER NOT NULL DEFAULT 1,
			sort_order INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_friend_links_sort ON friend_links(sort_order, created_at)`,
		`INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES (4, strftime('%s', 'now'))`,
		`CREATE TABLE IF NOT EXISTS recycle_bin (
			id TEXT PRIMARY KEY, kind TEXT NOT NULL, node_id TEXT NOT NULL,
			deleted_at INTEGER NOT NULL, deleted_by TEXT NOT NULL DEFAULT '',
			summary TEXT NOT NULL DEFAULT '', payload_json TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_recycle_bin_deleted_at ON recycle_bin(deleted_at DESC)`,
		`CREATE TABLE IF NOT EXISTS audit_log (
			id TEXT PRIMARY KEY, actor_type TEXT NOT NULL, actor_id TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL, object_type TEXT NOT NULL, object_id TEXT NOT NULL,
			timestamp INTEGER NOT NULL, ip TEXT NOT NULL DEFAULT '', summary TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_log_timestamp ON audit_log(timestamp DESC)`,
		`INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES (5, strftime('%s', 'now'))`,
		`CREATE TABLE IF NOT EXISTS service_monitors (
			id TEXT PRIMARY KEY, owner_id TEXT NOT NULL DEFAULT '', enabled INTEGER NOT NULL DEFAULT 1,
			next_run_at INTEGER NOT NULL DEFAULT 0, payload_json TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_service_monitors_owner ON service_monitors(owner_id)`,
		`CREATE INDEX IF NOT EXISTS idx_service_monitors_due ON service_monitors(enabled, next_run_at)`,
		`CREATE TABLE IF NOT EXISTS service_monitor_states (
			monitor_id TEXT PRIMARY KEY, payload_json TEXT NOT NULL,
			FOREIGN KEY(monitor_id) REFERENCES service_monitors(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS service_monitor_results (
			id TEXT PRIMARY KEY, monitor_id TEXT NOT NULL, owner_id TEXT NOT NULL DEFAULT '',
			checked_at INTEGER NOT NULL, payload_json TEXT NOT NULL,
			FOREIGN KEY(monitor_id) REFERENCES service_monitors(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_service_monitor_results_time ON service_monitor_results(monitor_id, checked_at DESC)`,
		`INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES (7, strftime('%s', 'now'))`,
	}
	for _, query := range statements {
		if _, err := s.db.Exec(query); err != nil {
			return err
		}
	}
	if err := s.migratePlannedNodeColumns(); err != nil {
		return err
	}
	if err := s.migrateOwnerColumns(); err != nil {
		return err
	}
	if err := s.migrateMarketListingColumns(); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_planned_nodes_owner ON planned_nodes(owner_id)`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`UPDATE planned_nodes SET owner_id = COALESCE((SELECT owner_id FROM market_listings WHERE market_listings.node_id = planned_nodes.node_id), '') WHERE owner_id = '' AND EXISTS (SELECT 1 FROM market_listings WHERE market_listings.node_id = planned_nodes.node_id)`); err != nil {
		return err
	}
	_, err := s.db.Exec(`INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES (6, strftime('%s', 'now'))`)
	return err
}

func (s *SQLiteStore) migrateOwnerColumns() error {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('owners') WHERE name = 'disabled'`).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		_, err := s.db.Exec(`ALTER TABLE owners ADD COLUMN disabled INTEGER NOT NULL DEFAULT 0`)
		return err
	}
	return nil
}

func (s *SQLiteStore) migratePlannedNodeColumns() error {
	columns := map[string]bool{}
	rows, err := s.db.Query(`PRAGMA table_info(planned_nodes)`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			_ = rows.Close()
			return err
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	alters := []struct {
		name string
		ddl  string
	}{
		{"token", `ALTER TABLE planned_nodes ADD COLUMN token TEXT NOT NULL DEFAULT ''`},
		{"display_name", `ALTER TABLE planned_nodes ADD COLUMN display_name TEXT NOT NULL DEFAULT ''`},
		{"region", `ALTER TABLE planned_nodes ADD COLUMN region TEXT NOT NULL DEFAULT ''`},
		{"region_code", `ALTER TABLE planned_nodes ADD COLUMN region_code TEXT NOT NULL DEFAULT ''`},
		{"owner_id", `ALTER TABLE planned_nodes ADD COLUMN owner_id TEXT NOT NULL DEFAULT ''`},
		{"private", `ALTER TABLE planned_nodes ADD COLUMN private INTEGER NOT NULL DEFAULT 0`},
	}
	for _, item := range alters {
		if columns[item.name] {
			continue
		}
		if _, err := s.db.Exec(item.ddl); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) migrateMarketListingColumns() error {
	columns := map[string]bool{}
	rows, err := s.db.Query(`PRAGMA table_info(market_listings)`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			_ = rows.Close()
			return err
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	alters := []struct{ name, ddl string }{
		{"price_amount", `ALTER TABLE market_listings ADD COLUMN price_amount REAL NOT NULL DEFAULT 0`},
		{"price_currency", `ALTER TABLE market_listings ADD COLUMN price_currency TEXT NOT NULL DEFAULT ''`},
		{"billing_cycle", `ALTER TABLE market_listings ADD COLUMN billing_cycle TEXT NOT NULL DEFAULT ''`},
		{"moderation_status", `ALTER TABLE market_listings ADD COLUMN moderation_status TEXT NOT NULL DEFAULT 'approved'`},
		{"rejection_reason", `ALTER TABLE market_listings ADD COLUMN rejection_reason TEXT NOT NULL DEFAULT ''`},
	}
	for _, item := range alters {
		if !columns[item.name] {
			if _, err := s.db.Exec(item.ddl); err != nil {
				return err
			}
		}
	}
	_, err = s.db.Exec(`UPDATE market_listings SET moderation_status = 'approved' WHERE moderation_status = '' OR moderation_status IS NULL`)
	return err
}

func (s *SQLiteStore) isEmpty() (bool, error) {
	for _, table := range []string{"settings", "planned_nodes", "host_infos", "reports", "traffic_stats", "service_monitors"} {
		count, err := countRows(s.db, table)
		if err != nil {
			return false, err
		}
		if count > 0 {
			return false, nil
		}
	}
	return true, nil
}

func (s *SQLiteStore) importJSONIfPresent(path string) error {
	path = strings.TrimSpace(path)
	if path == "" || path == s.path {
		return nil
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	jsonStore, err := NewStore(path, s.tokenSecret)
	if err != nil {
		return err
	}
	if !jsonStoreHasData(jsonStore) {
		return nil
	}
	return s.importJSONStore(jsonStore)
}

func jsonStoreHasData(store *Store) bool {
	return len(store.Reports) > 0 ||
		len(store.Infos) > 0 ||
		len(store.Planned) > 0 ||
		len(store.Traffic) > 0 ||
		len(store.Owners) > 0 ||
		len(store.Listings) > 0 ||
		len(store.Ads) > 0 ||
		len(store.Links) > 0 ||
		len(store.Recycle) > 0 ||
		len(store.Audit) > 0 ||
		len(store.ServiceMonitors) > 0 ||
		store.Settings.SiteName != "" && store.Settings.SiteName != "Monitor Party" && store.Settings.SiteName != "云镜监控"
}

func (s *SQLiteStore) importJSONStore(store *Store) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	settings := normalizeSettings(store.GetSettings())
	if err := upsertSettingTx(tx, "site_name", settings.SiteName); err != nil {
		return err
	}
	if err := upsertSettingTx(tx, "landing_enabled", strconv.FormatBool(settings.LandingEnabled)); err != nil {
		return err
	}
	if err := upsertSettingTx(tx, "market_enabled", strconv.FormatBool(settings.MarketEnabled)); err != nil {
		return err
	}
	settingsItems, err := json.Marshal(settings.VisitorStatsItems)
	if err != nil {
		return err
	}
	for key, value := range map[string]string{
		"footer_hidden": strconv.FormatBool(settings.FooterHidden), "footer_text": settings.FooterText,
		"friend_links_title": settings.FriendLinksTitle, "visitor_stats_hidden": strconv.FormatBool(settings.VisitorStatsHidden),
		"visitor_stats_items": string(settingsItems), "user_registration_disabled": strconv.FormatBool(settings.UserRegistrationDisabled),
		"user_node_apply_disabled": strconv.FormatBool(settings.UserNodeApplyDisabled), "user_node_limit": strconv.Itoa(settings.UserNodeLimit),
	} {
		if err := upsertSettingTx(tx, key, value); err != nil {
			return err
		}
	}
	plannedNodes := make(map[string]PlannedNode, len(store.Planned)+len(store.Listings))
	for nodeID, planned := range store.Planned {
		plannedNodes[nodeID] = planned
	}
	for nodeID, listing := range store.Listings {
		planned := plannedNodes[nodeID]
		if strings.TrimSpace(planned.OwnerID) == "" && strings.TrimSpace(listing.OwnerID) != "" {
			planned.NodeID = nodeID
			planned.OwnerID = strings.TrimSpace(listing.OwnerID)
			if planned.CreatedAt == 0 {
				planned.CreatedAt = listing.CreatedAt
			}
			plannedNodes[nodeID] = planned
		}
	}
	for _, planned := range plannedNodes {
		if err := upsertPlannedTx(tx, planned); err != nil {
			return err
		}
	}
	for _, info := range store.Infos {
		info.AuthSecret = ""
		info.TrafficResetDay = serverdomain.NormalizeTrafficResetDay(info.TrafficResetDay)
		if err := upsertInfoTx(tx, info); err != nil {
			return err
		}
	}
	for _, metrics := range store.Reports {
		if err := upsertReportTx(tx, metrics); err != nil {
			return err
		}
	}
	for nodeID, stat := range store.Traffic {
		if err := upsertTrafficTx(tx, nodeID, stat); err != nil {
			return err
		}
	}
	for _, owner := range store.Owners {
		if _, err := tx.Exec(`
			INSERT OR REPLACE INTO owners(id, email, password_hash, created_at, last_login, disabled)
			VALUES (?, ?, ?, ?, ?, ?)
		`, owner.ID, owner.Email, owner.PasswordHash, owner.CreatedAt, owner.LastLogin, boolToInt(owner.Disabled)); err != nil {
			return err
		}
	}
	for _, listing := range store.Listings {
		if _, err := tx.Exec(`
			INSERT OR REPLACE INTO market_listings(
				node_id, owner_id, for_sale, listing_type, contact, description, specs, price, price_amount, price_currency, billing_cycle, pinned, created_at, updated_at, moderation_status, rejection_reason
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, listing.NodeID, listing.OwnerID, boolToInt(listing.ForSale), listing.ListingType, listing.Contact,
			listing.Description, listing.Specs, listing.Price, listing.PriceAmount, listing.PriceCurrency, listing.BillingCycle,
			boolToInt(listing.Pinned), listing.CreatedAt, listing.UpdatedAt, normalizeModerationStatus(listing.ModerationStatus), listing.RejectionReason); err != nil {
			return err
		}
	}
	for _, ad := range store.Ads {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO advertisements (`+adColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			ad.ID, ad.Brand, ad.Title, ad.Description, ad.ImageURL, ad.TargetURL, ad.ButtonText,
			boolToInt(ad.Enabled), boolToInt(ad.Pinned), boolToInt(ad.Recommended), ad.Priority, ad.PositionMode,
			ad.DesktopAfter, ad.MobileAfter, ad.StartAt, ad.EndAt, ad.CreatedAt, ad.UpdatedAt, ad.Impressions, ad.Clicks); err != nil {
			return err
		}
	}
	for _, link := range store.Links {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO friend_links(id,label,url,enabled,new_tab,sort_order,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, link.ID, link.Label, link.URL, boolToInt(link.Enabled), boolToInt(link.NewTab), link.SortOrder, link.CreatedAt, link.UpdatedAt); err != nil {
			return err
		}
	}
	for _, record := range store.Recycle {
		if err := insertRecycleTx(tx, record); err != nil {
			return err
		}
	}
	for _, event := range store.Audit {
		if err := insertAuditTx(tx, event); err != nil {
			return err
		}
	}
	for _, monitor := range store.ServiceMonitors {
		payload, err := json.Marshal(monitor)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT OR REPLACE INTO service_monitors(id, owner_id, enabled, next_run_at, payload_json) VALUES(?,?,?,?,?)`,
			monitor.ID, monitor.OwnerID, boolToInt(monitor.Enabled), monitor.NextRunAt, string(payload)); err != nil {
			return err
		}
	}
	for monitorID, state := range store.ServiceMonitorStates {
		if _, exists := store.ServiceMonitors[monitorID]; !exists {
			continue
		}
		payload, err := json.Marshal(state)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT OR REPLACE INTO service_monitor_states(monitor_id, payload_json) VALUES(?,?)`, monitorID, string(payload)); err != nil {
			return err
		}
	}
	for monitorID, results := range store.ServiceMonitorResults {
		if _, exists := store.ServiceMonitors[monitorID]; !exists {
			continue
		}
		for _, result := range results {
			payload, err := json.Marshal(result)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT OR REPLACE INTO service_monitor_results(id, monitor_id, owner_id, checked_at, payload_json) VALUES(?,?,?,?,?)`,
				result.ID, monitorID, result.OwnerID, result.CheckedAt, string(payload)); err != nil {
				return err
			}
		}
	}
	layoutRaw, err := json.Marshal(normalizeAdLayoutSettings(store.AdLayout))
	if err != nil {
		return err
	}
	if err := upsertSettingTx(tx, "ad_layout", string(layoutRaw)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) ensureDefaultSettings() error {
	_, err := s.db.Exec(`
		INSERT OR IGNORE INTO settings(key, value) VALUES ('site_name', '云镜监控');
		INSERT OR IGNORE INTO settings(key, value) VALUES ('landing_enabled', 'false');
		INSERT OR IGNORE INTO settings(key, value) VALUES ('market_enabled', 'true');
		INSERT OR IGNORE INTO settings(key, value) VALUES ('ad_layout', '{"desktop_interval":3,"mobile_interval":2,"max_ads":0,"min_server_gap":1,"allow_consecutive":false,"conflict_strategy":"shift","rotation_mode":"fixed"}');
		INSERT OR IGNORE INTO settings(key, value) VALUES ('footer_hidden', 'false');
		INSERT OR IGNORE INTO settings(key, value) VALUES ('footer_text', '');
		INSERT OR IGNORE INTO settings(key, value) VALUES ('friend_links_title', '友情链接');
		INSERT OR IGNORE INTO settings(key, value) VALUES ('visitor_stats_hidden', 'false');
		INSERT OR IGNORE INTO settings(key, value) VALUES ('visitor_stats_items', '["online","today_ips","total_views"]');
		INSERT OR IGNORE INTO settings(key, value) VALUES ('time_zone', 'Asia/Shanghai');
		INSERT OR IGNORE INTO settings(key, value) VALUES ('exchange_rate_mode', 'auto');
		INSERT OR IGNORE INTO settings(key, value) VALUES ('manual_usd_cny', '7.2');
		INSERT OR IGNORE INTO settings(key, value) VALUES ('user_registration_disabled', 'false');
		INSERT OR IGNORE INTO settings(key, value) VALUES ('user_node_apply_disabled', 'false');
		INSERT OR IGNORE INTO settings(key, value) VALUES ('user_node_limit', '3');
	`)
	return err
}

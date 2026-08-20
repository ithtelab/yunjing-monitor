package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"strconv"
)

func (s *SQLiteStore) SiteName() string {
	return s.GetSettings().SiteName
}

func (s *SQLiteStore) GetSettings() Settings {
	settings := defaultSettings()
	rows, err := s.db.Query(`SELECT key, value FROM settings WHERE key IN ('site_name', 'landing_enabled', 'market_enabled', 'footer_hidden', 'footer_text', 'friend_links_title', 'visitor_stats_hidden', 'visitor_stats_items', 'time_zone', 'exchange_rate_mode', 'manual_usd_cny', 'user_registration_disabled', 'user_node_apply_disabled', 'user_node_limit')`)
	if errors.Is(err, sql.ErrNoRows) {
		return settings
	}
	if err != nil {
		log.Printf("sqlite settings read failed: %v", err)
		return settings
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			log.Printf("sqlite setting scan failed: %v", err)
			continue
		}
		switch key {
		case "site_name":
			if value != "" {
				settings.SiteName = value
			}
		case "landing_enabled":
			settings.LandingEnabled, _ = strconv.ParseBool(value)
		case "market_enabled":
			settings.MarketEnabled, _ = strconv.ParseBool(value)
		case "footer_hidden":
			settings.FooterHidden, _ = strconv.ParseBool(value)
		case "footer_text":
			settings.FooterText = value
		case "friend_links_title":
			settings.FriendLinksTitle = value
		case "visitor_stats_hidden":
			settings.VisitorStatsHidden, _ = strconv.ParseBool(value)
		case "visitor_stats_items":
			_ = json.Unmarshal([]byte(value), &settings.VisitorStatsItems)
		case "time_zone":
			settings.TimeZone = value
		case "exchange_rate_mode":
			settings.ExchangeRateMode = value
		case "manual_usd_cny":
			settings.ManualUSDCNY, _ = strconv.ParseFloat(value, 64)
		case "user_registration_disabled":
			settings.UserRegistrationDisabled, _ = strconv.ParseBool(value)
		case "user_node_apply_disabled":
			settings.UserNodeApplyDisabled, _ = strconv.ParseBool(value)
		case "user_node_limit":
			settings.UserNodeLimit, _ = strconv.Atoi(value)
		}
	}
	return normalizeSettings(settings)
}

func (s *SQLiteStore) UpdateSettings(settings Settings) error {
	settings = normalizeSettings(settings)
	itemsJSON, err := json.Marshal(settings.VisitorStatsItems)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT OR REPLACE INTO settings(key, value) VALUES ('site_name', ?)`, settings.SiteName); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO settings(key, value) VALUES ('landing_enabled', ?)`, strconv.FormatBool(settings.LandingEnabled)); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO settings(key, value) VALUES ('market_enabled', ?)`, strconv.FormatBool(settings.MarketEnabled)); err != nil {
		return err
	}
	values := map[string]string{
		"footer_hidden": strconv.FormatBool(settings.FooterHidden), "footer_text": settings.FooterText,
		"friend_links_title": settings.FriendLinksTitle, "visitor_stats_hidden": strconv.FormatBool(settings.VisitorStatsHidden),
		"visitor_stats_items": string(itemsJSON),
		"time_zone":           settings.TimeZone, "exchange_rate_mode": settings.ExchangeRateMode,
		"manual_usd_cny":             strconv.FormatFloat(settings.ManualUSDCNY, 'f', 6, 64),
		"user_registration_disabled": strconv.FormatBool(settings.UserRegistrationDisabled),
		"user_node_apply_disabled":   strconv.FormatBool(settings.UserNodeApplyDisabled),
		"user_node_limit":            strconv.Itoa(settings.UserNodeLimit),
	}
	for key, value := range values {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO settings(key,value) VALUES (?,?)`, key, value); err != nil {
			return err
		}
	}
	return tx.Commit()
}

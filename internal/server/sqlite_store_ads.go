package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

const adColumns = `id, brand, title, description, image_url, target_url, button_text, enabled, pinned, recommended, priority, position_mode, desktop_after, mobile_after, start_at, end_at, created_at, updated_at, impressions, clicks`

func scanAdvertisement(scanner interface{ Scan(...any) error }) (Advertisement, error) {
	var ad Advertisement
	var enabled, pinned, recommended int
	err := scanner.Scan(&ad.ID, &ad.Brand, &ad.Title, &ad.Description, &ad.ImageURL, &ad.TargetURL, &ad.ButtonText, &enabled, &pinned, &recommended, &ad.Priority, &ad.PositionMode, &ad.DesktopAfter, &ad.MobileAfter, &ad.StartAt, &ad.EndAt, &ad.CreatedAt, &ad.UpdatedAt, &ad.Impressions, &ad.Clicks)
	ad.Enabled, ad.Pinned, ad.Recommended = enabled != 0, pinned != 0, recommended != 0
	return ad, err
}

func (s *SQLiteStore) ListAdvertisements(includeInactive bool, now time.Time) []Advertisement {
	query := `SELECT ` + adColumns + ` FROM advertisements`
	args := []any{}
	if !includeInactive {
		query += ` WHERE enabled = 1 AND (start_at = 0 OR start_at <= ?) AND (end_at = 0 OR end_at > ?)`
		args = append(args, now.Unix(), now.Unix())
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []Advertisement{}
	for rows.Next() {
		ad, err := scanAdvertisement(rows)
		if err == nil {
			out = append(out, ad)
		}
	}
	sortAdvertisements(out, s.GetAdLayoutSettings().RotationMode, now)
	return out
}

func (s *SQLiteStore) GetAdvertisement(id string) (Advertisement, bool) {
	ad, err := scanAdvertisement(s.db.QueryRow(`SELECT `+adColumns+` FROM advertisements WHERE id = ?`, id))
	return ad, err == nil
}

func (s *SQLiteStore) SaveAdvertisement(ad Advertisement) (Advertisement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ad.ID == "" {
		id, err := newAdvertisementID()
		if err != nil {
			return Advertisement{}, err
		}
		ad.ID = id
	}
	now := time.Now().Unix()
	existing, ok := s.GetAdvertisement(ad.ID)
	if ok {
		ad.CreatedAt, ad.Impressions, ad.Clicks = existing.CreatedAt, existing.Impressions, existing.Clicks
	}
	if ad.CreatedAt == 0 {
		ad.CreatedAt = now
	}
	ad.UpdatedAt = now
	_, err := s.db.Exec(`INSERT INTO advertisements (`+adColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET brand=excluded.brand,title=excluded.title,description=excluded.description,image_url=excluded.image_url,target_url=excluded.target_url,button_text=excluded.button_text,enabled=excluded.enabled,pinned=excluded.pinned,recommended=excluded.recommended,priority=excluded.priority,position_mode=excluded.position_mode,desktop_after=excluded.desktop_after,mobile_after=excluded.mobile_after,start_at=excluded.start_at,end_at=excluded.end_at,updated_at=excluded.updated_at`,
		ad.ID, ad.Brand, ad.Title, ad.Description, ad.ImageURL, ad.TargetURL, ad.ButtonText, boolToInt(ad.Enabled), boolToInt(ad.Pinned), boolToInt(ad.Recommended), ad.Priority, ad.PositionMode, ad.DesktopAfter, ad.MobileAfter, ad.StartAt, ad.EndAt, ad.CreatedAt, ad.UpdatedAt, ad.Impressions, ad.Clicks)
	return ad, err
}

func (s *SQLiteStore) DeleteAdvertisement(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM advertisements WHERE id = ?`, id)
	return err
}
func (s *SQLiteStore) IncrementAdvertisementStat(id string, click bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	column := "impressions"
	if click {
		column = "clicks"
	}
	res, err := s.db.Exec(`UPDATE advertisements SET `+column+` = `+column+` + 1 WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("advertisement not found")
	}
	return nil
}

func (s *SQLiteStore) GetAdLayoutSettings() AdLayoutSettings {
	var raw string
	if err := s.db.QueryRow(`SELECT value FROM settings WHERE key = 'ad_layout'`).Scan(&raw); err != nil && err != sql.ErrNoRows {
		return defaultAdLayoutSettings()
	}
	var v AdLayoutSettings
	if json.Unmarshal([]byte(raw), &v) != nil {
		return defaultAdLayoutSettings()
	}
	return normalizeAdLayoutSettings(v)
}

func (s *SQLiteStore) UpdateAdLayoutSettings(v AdLayoutSettings) error {
	v = normalizeAdLayoutSettings(v)
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO settings(key,value) VALUES('ad_layout',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(raw))
	return err
}

package server

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

func scanFriendLink(scanner interface{ Scan(...any) error }) (FriendLink, error) {
	var link FriendLink
	var enabled, newTab int
	err := scanner.Scan(&link.ID, &link.Label, &link.URL, &enabled, &newTab, &link.SortOrder, &link.CreatedAt, &link.UpdatedAt)
	link.Enabled = enabled != 0
	link.NewTab = newTab != 0
	return link, err
}

func (s *SQLiteStore) ListFriendLinks(includeDisabled bool) []FriendLink {
	query := `SELECT id,label,url,enabled,new_tab,sort_order,created_at,updated_at FROM friend_links`
	if !includeDisabled {
		query += ` WHERE enabled=1`
	}
	query += ` ORDER BY sort_order ASC, created_at ASC, id ASC`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []FriendLink{}
	for rows.Next() {
		link, err := scanFriendLink(rows)
		if err == nil {
			out = append(out, link)
		}
	}
	return out
}
func (s *SQLiteStore) GetFriendLink(id string) (FriendLink, bool) {
	link, err := scanFriendLink(s.db.QueryRow(`SELECT id,label,url,enabled,new_tab,sort_order,created_at,updated_at FROM friend_links WHERE id=?`, id))
	return link, err == nil
}
func (s *SQLiteStore) SaveFriendLink(link FriendLink) (FriendLink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if link.ID == "" {
		count, err := countRows(s.db, "friend_links")
		if err != nil {
			return FriendLink{}, err
		}
		if count >= 30 {
			return FriendLink{}, fmt.Errorf("friend link limit reached")
		}
		id, err := newFriendLinkID()
		if err != nil {
			return FriendLink{}, err
		}
		link.ID = id
	}
	var duplicate string
	err := s.db.QueryRow(`SELECT id FROM friend_links WHERE lower(url)=lower(?) AND id<>?`, link.URL, link.ID).Scan(&duplicate)
	if err == nil {
		return FriendLink{}, fmt.Errorf("friend link URL already exists")
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return FriendLink{}, err
	}
	now := time.Now().Unix()
	existing, ok := s.GetFriendLink(link.ID)
	if ok {
		link.CreatedAt = existing.CreatedAt
	} else if link.CreatedAt == 0 {
		link.CreatedAt = now
	}
	link.UpdatedAt = now
	_, err = s.db.Exec(`INSERT INTO friend_links(id,label,url,enabled,new_tab,sort_order,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET label=excluded.label,url=excluded.url,enabled=excluded.enabled,new_tab=excluded.new_tab,sort_order=excluded.sort_order,updated_at=excluded.updated_at`, link.ID, link.Label, link.URL, boolToInt(link.Enabled), boolToInt(link.NewTab), link.SortOrder, link.CreatedAt, link.UpdatedAt)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique") {
		return FriendLink{}, fmt.Errorf("friend link URL already exists")
	}
	return link, err
}
func (s *SQLiteStore) DeleteFriendLink(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM friend_links WHERE id=?`, id)
	return err
}
func (s *SQLiteStore) ReorderFriendLinks(ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRow(`SELECT count(*) FROM friend_links`).Scan(&count); err != nil {
		return err
	}
	if count != len(ids) {
		return fmt.Errorf("friend link order must include every link")
	}
	seen := map[string]bool{}
	for index, id := range ids {
		if seen[id] {
			return fmt.Errorf("invalid friend link order")
		}
		seen[id] = true
		res, err := tx.Exec(`UPDATE friend_links SET sort_order=?,updated_at=? WHERE id=?`, index, time.Now().Unix(), id)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return fmt.Errorf("invalid friend link order")
		}
	}
	return tx.Commit()
}

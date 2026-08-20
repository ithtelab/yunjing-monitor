package server

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (s *Store) ListFriendLinks(includeDisabled bool) []FriendLink {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]FriendLink, 0, len(s.Links))
	for _, link := range s.Links {
		if includeDisabled || link.Enabled {
			out = append(out, link)
		}
	}
	sortFriendLinks(out)
	return out
}

func (s *Store) GetFriendLink(id string) (FriendLink, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	link, ok := s.Links[id]
	return link, ok
}

func (s *Store) SaveFriendLink(link FriendLink) (FriendLink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Links == nil {
		s.Links = map[string]FriendLink{}
	}
	if link.ID == "" {
		if len(s.Links) >= 30 {
			return FriendLink{}, fmt.Errorf("friend link limit reached")
		}
		id, err := newFriendLinkID()
		if err != nil {
			return FriendLink{}, err
		}
		link.ID = id
	}
	for id, existing := range s.Links {
		if id != link.ID && strings.EqualFold(existing.URL, link.URL) {
			return FriendLink{}, fmt.Errorf("friend link URL already exists")
		}
	}
	now := time.Now().Unix()
	if existing, ok := s.Links[link.ID]; ok {
		link.CreatedAt = existing.CreatedAt
	} else if link.CreatedAt == 0 {
		link.CreatedAt = now
	}
	link.UpdatedAt = now
	s.Links[link.ID] = link
	return link, s.saveLocked()
}

func (s *Store) DeleteFriendLink(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Links, id)
	return s.saveLocked()
}

func (s *Store) ReorderFriendLinks(ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]bool{}
	for index, id := range ids {
		link, ok := s.Links[id]
		if !ok || seen[id] {
			return fmt.Errorf("invalid friend link order")
		}
		seen[id] = true
		link.SortOrder = index
		link.UpdatedAt = time.Now().Unix()
		s.Links[id] = link
	}
	if len(seen) != len(s.Links) {
		return fmt.Errorf("friend link order must include every link")
	}
	return s.saveLocked()
}

func sortFriendLinks(out []FriendLink) {
	sort.Slice(out, func(i, j int) bool {
		if out[i].SortOrder != out[j].SortOrder {
			return out[i].SortOrder < out[j].SortOrder
		}
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt < out[j].CreatedAt
		}
		return out[i].ID < out[j].ID
	})
}
func newFriendLinkID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "link_" + base64.RawURLEncoding.EncodeToString(b[:]), nil
}

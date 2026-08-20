package server

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
	"time"
)

func defaultAdLayoutSettings() AdLayoutSettings {
	return AdLayoutSettings{DesktopInterval: 3, MobileInterval: 2, MinServerGap: 1, ConflictStrategy: "shift", RotationMode: "fixed"}
}

func normalizeAdLayoutSettings(v AdLayoutSettings) AdLayoutSettings {
	if v.DesktopInterval < 1 {
		v.DesktopInterval = 3
	}
	if v.MobileInterval < 1 {
		v.MobileInterval = 2
	}
	if v.MaxAds < 0 {
		v.MaxAds = 0
	}
	if v.MinServerGap < 0 {
		v.MinServerGap = 0
	}
	switch v.ConflictStrategy {
	case "shift", "rotate", "stack":
	default:
		v.ConflictStrategy = "shift"
	}
	if v.RotationMode != "rotate" {
		v.RotationMode = "fixed"
	}
	return v
}

func advertisementActive(ad Advertisement, now time.Time) bool {
	if !ad.Enabled {
		return false
	}
	ts := now.Unix()
	return (ad.StartAt == 0 || ts >= ad.StartAt) && (ad.EndAt == 0 || ts < ad.EndAt)
}

func sortAdvertisements(out []Advertisement, rotationMode string, now time.Time) {
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Pinned != out[j].Pinned {
			return out[i].Pinned
		}
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		if out[i].Recommended != out[j].Recommended {
			return out[i].Recommended
		}
		if out[i].UpdatedAt != out[j].UpdatedAt {
			return out[i].UpdatedAt > out[j].UpdatedAt
		}
		return out[i].ID < out[j].ID
	})
	if rotationMode == "rotate" && len(out) > 1 {
		day := int(now.UTC().Unix() / 86400)
		for start := 0; start < len(out); {
			end := start + 1
			for end < len(out) && out[end].Pinned == out[start].Pinned && out[end].Priority == out[start].Priority && out[end].Recommended == out[start].Recommended {
				end++
			}
			group := out[start:end]
			if len(group) > 1 {
				offset := day % len(group)
				rotated := append(append([]Advertisement{}, group[offset:]...), group[:offset]...)
				copy(group, rotated)
			}
			start = end
		}
	}
}

func (s *Store) ListAdvertisements(includeInactive bool, now time.Time) []Advertisement {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Advertisement, 0, len(s.Ads))
	for _, ad := range s.Ads {
		if includeInactive || advertisementActive(ad, now) {
			out = append(out, ad)
		}
	}
	sortAdvertisements(out, s.AdLayout.RotationMode, now)
	return out
}

func (s *Store) GetAdvertisement(id string) (Advertisement, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ad, ok := s.Ads[id]
	return ad, ok
}

func (s *Store) SaveAdvertisement(ad Advertisement) (Advertisement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Ads == nil {
		s.Ads = map[string]Advertisement{}
	}
	if ad.ID == "" {
		id, err := newAdvertisementID()
		if err != nil {
			return Advertisement{}, err
		}
		ad.ID = id
	}
	now := time.Now().Unix()
	if existing, ok := s.Ads[ad.ID]; ok {
		ad.CreatedAt = existing.CreatedAt
		ad.Impressions = existing.Impressions
		ad.Clicks = existing.Clicks
	}
	if ad.CreatedAt == 0 {
		ad.CreatedAt = now
	}
	ad.UpdatedAt = now
	s.Ads[ad.ID] = ad
	return ad, s.saveLocked()
}

func (s *Store) DeleteAdvertisement(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Ads, id)
	return s.saveLocked()
}

func (s *Store) IncrementAdvertisementStat(id string, click bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ad, ok := s.Ads[id]
	if !ok {
		return fmt.Errorf("advertisement not found")
	}
	if click {
		ad.Clicks++
	} else {
		ad.Impressions++
	}
	s.Ads[id] = ad
	return s.saveLocked()
}

func (s *Store) GetAdLayoutSettings() AdLayoutSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return normalizeAdLayoutSettings(s.AdLayout)
}
func (s *Store) UpdateAdLayoutSettings(v AdLayoutSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.AdLayout = normalizeAdLayoutSettings(v)
	return s.saveLocked()
}

func newAdvertisementID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "ad_" + base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func validAdvertisementID(id string) bool {
	return strings.HasPrefix(id, "ad_") && len(id) <= 64 && validDownloadName(id)
}

package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type VisitorStatsSnapshot struct {
	Online     int    `json:"online"`
	TodayIPs   int    `json:"today_ips"`
	TodayViews int64  `json:"today_views"`
	TotalViews int64  `json:"total_views"`
	Date       string `json:"date"`
}
type visitorDay struct {
	Views    int64           `json:"views"`
	IPHashes map[string]bool `json:"ip_hashes"`
}
type visitorStatsFile struct {
	TotalViews int64                  `json:"total_views"`
	Days       map[string]*visitorDay `json:"days"`
}
type VisitorStatsStore struct {
	mu       sync.Mutex
	path     string
	secret   []byte
	data     visitorStatsFile
	online   map[string]time.Time
	onlineIP map[string]string
	ipOnline map[string]int
	events   map[string]time.Time
	timer    *time.Timer
	location *time.Location
}

const (
	maxOnlineVisitors = 10000
	maxOnlinePerIP    = 20
	maxVisitorEvents  = 20000
	maxDailyIPHashes  = 100000
)

func NewVisitorStatsStore(path, secret string) *VisitorStatsStore {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		location = time.FixedZone("Asia/Shanghai", 8*3600)
	}
	s := &VisitorStatsStore{path: path, secret: []byte(secret), data: visitorStatsFile{Days: map[string]*visitorDay{}}, online: map[string]time.Time{}, onlineIP: map[string]string{}, ipOnline: map[string]int{}, events: map[string]time.Time{}, location: location}
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &s.data)
	}
	if s.data.Days == nil {
		s.data.Days = map[string]*visitorDay{}
	}
	return s
}
func (s *VisitorStatsStore) Record(visitorID, eventID, ip string, pageView bool, now time.Time) VisitorStatsSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	date := now.In(s.location).Format("2006-01-02")
	ipKey := ""
	if ip != "" {
		ipKey = s.hashIP(date, ip)
	}
	_, onlineExists := s.online[visitorID]
	if onlineExists || len(s.online) < maxOnlineVisitors && (ipKey == "" || s.ipOnline[ipKey] < maxOnlinePerIP) {
		if !onlineExists {
			s.onlineIP[visitorID] = ipKey
			if ipKey != "" {
				s.ipOnline[ipKey]++
			}
		}
		s.online[visitorID] = now
	}
	day := s.data.Days[date]
	if day == nil {
		day = &visitorDay{IPHashes: map[string]bool{}}
		s.data.Days[date] = day
	}
	if day.IPHashes == nil {
		day.IPHashes = map[string]bool{}
	}
	dirty := false
	if ip != "" {
		hash := s.hashIP(date, ip)
		if !day.IPHashes[hash] && len(day.IPHashes) < maxDailyIPHashes {
			day.IPHashes[hash] = true
			dirty = true
		}
	}
	if pageView && eventID != "" && s.events[eventID].IsZero() && len(s.events) < maxVisitorEvents {
		day.Views++
		s.data.TotalViews++
		dirty = true
		s.events[eventID] = now
	}
	if dirty {
		s.scheduleFlushLocked()
	}
	return s.snapshotLocked(now)
}
func (s *VisitorStatsStore) Snapshot(now time.Time) VisitorStatsSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	return s.snapshotLocked(now)
}
func (s *VisitorStatsStore) snapshotLocked(now time.Time) VisitorStatsSnapshot {
	date := now.In(s.location).Format("2006-01-02")
	day := s.data.Days[date]
	snap := VisitorStatsSnapshot{Online: len(s.online), TotalViews: s.data.TotalViews, Date: date}
	if day != nil {
		snap.TodayIPs = len(day.IPHashes)
		snap.TodayViews = day.Views
	}
	return snap
}
func (s *VisitorStatsStore) hashIP(date, ip string) string {
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(date + "\x00" + ip))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (s *VisitorStatsStore) pruneLocked(now time.Time) {
	for id, last := range s.online {
		if now.Sub(last) > 3*time.Minute {
			delete(s.online, id)
			ipKey := s.onlineIP[id]
			delete(s.onlineIP, id)
			if ipKey != "" && s.ipOnline[ipKey] <= 1 {
				delete(s.ipOnline, ipKey)
			} else if ipKey != "" {
				s.ipOnline[ipKey]--
			}
		}
	}
	for id, seen := range s.events {
		if now.Sub(seen) > 10*time.Minute {
			delete(s.events, id)
		}
	}
	cutoff := now.In(s.location).AddDate(0, 0, -31)
	for date := range s.data.Days {
		if parsed, err := time.ParseInLocation("2006-01-02", date, s.location); err != nil || parsed.Before(cutoff) {
			delete(s.data.Days, date)
		}
	}
}
func (s *VisitorStatsStore) scheduleFlushLocked() {
	if s.timer != nil {
		return
	}
	s.timer = time.AfterFunc(30*time.Second, func() { _ = s.Flush() })
}
func (s *VisitorStatsStore) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.timer = nil
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func visitorStatsPath(dataPath string) string {
	if strings.TrimSpace(dataPath) == "" {
		dataPath = "data/server.json"
	}
	return filepath.Join(filepath.Dir(dataPath), "visitor-stats.json")
}
func requestClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	remote := net.ParseIP(strings.TrimSpace(host))
	trusted := remote != nil && (remote.IsPrivate() || remote.IsLoopback())
	if trusted {
		for _, header := range []string{"CF-Connecting-IP", "X-Real-IP"} {
			if ip := net.ParseIP(strings.TrimSpace(r.Header.Get(header))); ip != nil {
				return ip.String()
			}
		}
		if value := r.Header.Get("X-Forwarded-For"); value != "" {
			if ip := net.ParseIP(strings.TrimSpace(strings.Split(value, ",")[0])); ip != nil {
				return ip.String()
			}
		}
	}
	if remote != nil {
		return remote.String()
	}
	return ""
}

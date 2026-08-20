package server

import (
	"strings"
	"sync"
	"time"
)

const (
	maxWebSocketConnections      = 256
	maxWebSocketConnectionsPerIP = 8
)

type loginFailure struct {
	first    time.Time
	failures int
	blocked  time.Time
}

// loginFailureLimiter blocks repeated failures while allowing a user to fix a
// typo immediately. Successful authentication clears the caller's history.
type loginFailureLimiter struct {
	mu        sync.Mutex
	entries   map[string]loginFailure
	window    time.Duration
	lockout   time.Duration
	threshold int
}

func newLoginFailureLimiter(threshold int, window, lockout time.Duration) *loginFailureLimiter {
	return &loginFailureLimiter{
		entries:   map[string]loginFailure{},
		window:    window,
		lockout:   lockout,
		threshold: threshold,
	}
}

func (l *loginFailureLimiter) Allowed(key string) bool {
	if l == nil {
		return true
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.entries[key]
	if !ok {
		return true
	}
	if !entry.blocked.IsZero() && now.Before(entry.blocked) {
		return false
	}
	if now.Sub(entry.first) > l.window {
		delete(l.entries, key)
	}
	return true
}

func (l *loginFailureLimiter) Failure(key string) {
	if l == nil {
		return
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	entry := l.entries[key]
	if entry.first.IsZero() || now.Sub(entry.first) > l.window {
		entry = loginFailure{first: now}
	}
	entry.failures++
	if entry.failures >= l.threshold {
		entry.blocked = now.Add(l.lockout)
	}
	l.entries[key] = entry
	if len(l.entries) > 4096 {
		for candidate, value := range l.entries {
			if now.Sub(value.first) > l.window+l.lockout {
				delete(l.entries, candidate)
			}
		}
	}
}

func (l *loginFailureLimiter) Success(key string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	delete(l.entries, key)
	l.mu.Unlock()
}

func loginLimitKey(rIP, identity string) string {
	return strings.TrimSpace(rIP) + "\x00" + strings.ToLower(strings.TrimSpace(identity))
}

func (s *Server) acquireWebSocket(ip string) bool {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	if s.wsConnections >= maxWebSocketConnections || s.wsByIP[ip] >= maxWebSocketConnectionsPerIP {
		return false
	}
	if s.wsByIP == nil {
		s.wsByIP = map[string]int{}
	}
	s.wsConnections++
	s.wsByIP[ip]++
	return true
}

func (s *Server) releaseWebSocket(ip string) {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	if s.wsConnections > 0 {
		s.wsConnections--
	}
	if s.wsByIP[ip] <= 1 {
		delete(s.wsByIP, ip)
	} else {
		s.wsByIP[ip]--
	}
}

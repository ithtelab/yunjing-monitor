package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type SessionKind string

const (
	SessionKindAdmin SessionKind = "admin"
	SessionKindOwner SessionKind = "owner"
)

type Session struct {
	Subject string      `json:"subject"`
	Kind    SessionKind `json:"kind"`
	Expires time.Time   `json:"expires"`
}

type SessionStore struct {
	mu       sync.Mutex
	path     string
	secret   []byte
	sessions map[string]Session
}

const (
	defaultSessionTTL    = 24 * time.Hour
	rememberedSessionTTL = 30 * 24 * time.Hour
)

type sessionFile struct {
	Version  int                `json:"version"`
	Sessions map[string]Session `json:"sessions"`
}

func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: map[string]Session{}}
}

func NewPersistentSessionStore(path, secret string) (*SessionStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("session store path is required")
	}
	if strings.TrimSpace(secret) == "" {
		return nil, errors.New("session store secret is required")
	}
	store := &SessionStore{path: path, secret: []byte(secret), sessions: map[string]Session{}}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

// Create issues an admin session (subject "admin"). Kept for existing call sites.
func (s *SessionStore) Create() (string, error) {
	return s.CreateFor(SessionKindAdmin, "admin")
}

func (s *SessionStore) CreateFor(kind SessionKind, subject string) (string, error) {
	return s.CreateForDuration(kind, subject, defaultSessionTTL)
}

func (s *SessionStore) CreateForDuration(kind SessionKind, subject string, ttl time.Duration) (string, error) {
	if kind != SessionKindAdmin && kind != SessionKindOwner {
		return "", errors.New("invalid session kind")
	}
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return "", errors.New("session subject is required")
	}
	if ttl <= 0 {
		return "", errors.New("session ttl must be positive")
	}
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(buf[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneExpiredLocked(time.Now())
	key := s.tokenKey(token)
	s.sessions[key] = Session{
		Subject: subject,
		Kind:    kind,
		Expires: time.Now().Add(ttl),
	}
	if err := s.saveLocked(); err != nil {
		delete(s.sessions, key)
		return "", err
	}
	return token, nil
}

func (s *SessionStore) Valid(token string) bool {
	_, ok := s.Get(token)
	return ok
}

func (s *SessionStore) Get(token string) (Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := s.tokenKey(token)
	sess, ok := s.sessions[key]
	if !ok {
		return Session{}, false
	}
	if time.Now().After(sess.Expires) {
		delete(s.sessions, key)
		_ = s.saveLocked()
		return Session{}, false
	}
	return sess, true
}

func (s *SessionStore) Delete(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, s.tokenKey(token))
	_ = s.saveLocked()
}

func (s *SessionStore) DeleteFor(kind SessionKind, subject string) error {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, session := range s.sessions {
		if session.Kind == kind && session.Subject == subject {
			delete(s.sessions, key)
		}
	}
	return s.saveLocked()
}

func (s *SessionStore) tokenKey(token string) string {
	if len(s.secret) == 0 {
		return token
	}
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *SessionStore) pruneExpiredLocked(now time.Time) {
	for key, session := range s.sessions {
		if !session.Expires.After(now) {
			delete(s.sessions, key)
		}
	}
}

func (s *SessionStore) load() error {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read sessions: %w", err)
	}
	if len(raw) == 0 {
		return nil
	}
	var data sessionFile
	if err := json.Unmarshal(raw, &data); err != nil {
		return fmt.Errorf("decode sessions: %w", err)
	}
	if data.Sessions != nil {
		s.sessions = data.Sessions
	}
	s.pruneExpiredLocked(time.Now())
	return nil
}

func (s *SessionStore) saveLocked() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return fmt.Errorf("create session directory: %w", err)
	}
	raw, err := json.MarshalIndent(sessionFile{Version: 1, Sessions: s.sessions}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode sessions: %w", err)
	}
	raw = append(raw, '\n')
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return fmt.Errorf("write sessions: %w", err)
	}
	if err := os.Chmod(tmp, 0600); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("secure sessions: %w", err)
	}
	if err := os.Rename(tmp, s.path); err == nil {
		return nil
	}
	// Windows does not replace an existing destination with os.Rename.
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace sessions: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace sessions: %w", err)
	}
	return nil
}

func sessionStorePath(dataPath string) string {
	return filepath.Join(filepath.Dir(dataPath), "auth-sessions.json")
}

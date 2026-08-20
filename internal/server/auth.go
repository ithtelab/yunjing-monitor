package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"
	"time"
)

func (s *Server) agentAuthorized(r *http.Request) bool {
	nodeID := strings.TrimSpace(r.Header.Get("X-Node-ID"))
	if !validNodeID(nodeID) {
		return false
	}
	token := bearerToken(r.Header.Get("Authorization"))
	return token != "" && s.store.ValidNodeToken(nodeID, hashToken(token))
}

func bearerToken(header string) string {
	scheme, token, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}

func (s *Server) adminAuthorized(r *http.Request) bool {
	cookie, err := r.Cookie("monitor_admin")
	if err != nil || cookie.Value == "" {
		return false
	}
	// Must be an admin session — owner tokens live in the same store and must not pass.
	sess, ok := s.sessions.Get(cookie.Value)
	return ok && sess.Kind == SessionKindAdmin
}

func adminCookie(r *http.Request, value string, maxAge time.Duration, persistent bool) *http.Cookie {
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	cookie := &http.Cookie{
		Name:     "monitor_admin",
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
	if maxAge < 0 {
		cookie.MaxAge = -1
		cookie.Expires = time.Unix(1, 0)
	} else if persistent {
		cookie.MaxAge = int(maxAge.Seconds())
		cookie.Expires = time.Now().Add(maxAge)
	}
	return cookie
}

func constantEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func validNodeID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len([]rune(value)) > 96 {
		return false
	}
	return !strings.ContainsAny(value, "\x00\r\n\t/\\'\"`$;&|<>!*?[]{}()")
}

func newAgentToken() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

// newNodeID generates an internal node identity that is not meant to be a display label.
func newNodeID() (string, error) {
	var buf [6]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return "n_" + base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (s *Server) validAdminOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	allowed := map[string]bool{}
	for _, value := range s.cfg.CORSOrigins {
		if value == "*" {
			continue
		}
		allowed[value] = true
	}
	return corsOriginAllowed(r, origin, allowed, false)
}

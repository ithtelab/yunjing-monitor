package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func (s *Server) handleAccountNotifications(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.notificationOwner(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	s.features.mu.RLock()
	settings := normalizeShowDocSettings(s.features.data.ShowDocSettings)
	binding, bound := s.features.data.NotificationBindings[owner.ID]
	preference, hasPreference := s.features.data.NotificationPreferences[owner.ID]
	s.features.mu.RUnlock()
	if !hasPreference {
		preference = defaultNotificationPreference(owner.ID)
	}
	writeJSON(w, map[string]any{
		"enabled":          settings.Enabled,
		"binding":          notificationBindingView(binding, bound),
		"preference":       preference,
		"available_events": append([]string(nil), defaultOwnerNotificationEvents...),
		"provider":         map[string]string{"id": "showdoc", "name": "ShowDoc Push"},
	})
}

func (s *Server) handleAccountNotificationBindStart(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.notificationOwner(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.validAdminOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return
	}
	now := time.Now().Unix()
	s.features.mu.RLock()
	settings := normalizeShowDocSettings(s.features.data.ShowDocSettings)
	for _, session := range s.features.data.NotificationBindSessions {
		if session.OwnerID == owner.ID && session.ConsumedAt == 0 && session.ExpiresAt > now {
			s.features.mu.RUnlock()
			writeJSON(w, notificationBindSessionView(session))
			return
		}
	}
	s.features.mu.RUnlock()
	if !settings.Enabled {
		http.Error(w, "ShowDoc notifications are disabled", http.StatusServiceUnavailable)
		return
	}
	if s.notificationLimiter != nil && !s.notificationLimiter.Allow("bind-start\x00"+owner.ID) {
		http.Error(w, "binding request is rate limited", http.StatusTooManyRequests)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	qrURL, qrScene, err := createShowDocQRCode(ctx)
	cancel()
	if err != nil {
		http.Error(w, safeShowDocError(err), http.StatusBadGateway)
		return
	}
	id, err := newFeatureID("bind")
	if err != nil {
		http.Error(w, "create binding session failed", http.StatusInternalServerError)
		return
	}
	session := NotificationBindSession{ID: id, OwnerID: owner.ID, QRScene: qrScene, QRCodeURL: qrURL, ExpiresAt: time.Now().Add(5 * time.Minute).Unix()}
	s.features.mu.Lock()
	s.pruneNotificationBindSessionsLocked(time.Now().Unix())
	s.features.data.NotificationBindSessions = append(s.features.data.NotificationBindSessions, session)
	err = s.features.saveLocked()
	s.features.mu.Unlock()
	if err != nil {
		http.Error(w, "save binding session failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, notificationBindSessionView(session))
}

func (s *Server) handleAccountNotificationBindStatus(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.notificationOwner(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	if !validateFeatureID(sessionID) {
		http.Error(w, "valid session_id is required", http.StatusBadRequest)
		return
	}
	s.features.mu.RLock()
	var session NotificationBindSession
	found := false
	for _, item := range s.features.data.NotificationBindSessions {
		if item.ID == sessionID && item.OwnerID == owner.ID {
			session, found = item, true
			break
		}
	}
	s.features.mu.RUnlock()
	if !found {
		http.Error(w, "binding session not found", http.StatusNotFound)
		return
	}
	if session.ConsumedAt > 0 {
		s.features.mu.RLock()
		binding, bound := s.features.data.NotificationBindings[owner.ID]
		s.features.mu.RUnlock()
		writeJSON(w, map[string]any{"status": "bound", "binding": notificationBindingView(binding, bound)})
		return
	}
	if session.ExpiresAt <= time.Now().Unix() {
		writeJSON(w, map[string]any{"status": "expired", "expires_at": session.ExpiresAt})
		return
	}
	if s.notificationPollLimiter != nil && !s.notificationPollLimiter.Allow(owner.ID+"\x00"+session.ID) {
		http.Error(w, "binding status is being checked too frequently", http.StatusTooManyRequests)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	userToken, scanned, err := checkShowDocQRCode(ctx, session.QRScene)
	cancel()
	if err != nil {
		http.Error(w, safeShowDocError(err), http.StatusBadGateway)
		return
	}
	if !scanned {
		writeJSON(w, map[string]any{"status": "pending", "expires_at": session.ExpiresAt})
		return
	}
	ctx, cancel = context.WithTimeout(r.Context(), 8*time.Second)
	pushToken, err := getShowDocPushToken(ctx, userToken)
	cancel()
	if err != nil {
		http.Error(w, safeShowDocError(err), http.StatusBadGateway)
		return
	}
	ciphertext, err := s.features.cipher.encrypt(pushToken)
	if err != nil {
		http.Error(w, "encrypt ShowDoc token failed", http.StatusInternalServerError)
		return
	}
	now := time.Now().Unix()
	binding := NotificationBinding{OwnerID: owner.ID, Provider: "showdoc", TokenCipher: ciphertext, TokenSuffix: notificationTokenSuffix(pushToken), Status: "bound", BoundAt: now}
	s.features.mu.Lock()
	for i := range s.features.data.NotificationBindSessions {
		item := &s.features.data.NotificationBindSessions[i]
		if item.ID == session.ID && item.OwnerID == owner.ID && item.ConsumedAt == 0 {
			item.ConsumedAt = now
		}
	}
	s.features.data.NotificationBindings[owner.ID] = binding
	if _, exists := s.features.data.NotificationPreferences[owner.ID]; !exists {
		s.features.data.NotificationPreferences[owner.ID] = defaultNotificationPreference(owner.ID)
	}
	err = s.features.saveLocked()
	s.features.mu.Unlock()
	if err != nil {
		http.Error(w, "save ShowDoc binding failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"status": "bound", "binding": notificationBindingView(binding, true)})
}

func (s *Server) handleAccountNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.notificationOwner(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.validAdminOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return
	}
	defer r.Body.Close()
	var preference NotificationPreference
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&preference); err != nil {
		http.Error(w, "invalid notification preference", http.StatusBadRequest)
		return
	}
	preference, err := normalizeNotificationPreference(owner.ID, preference)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	for _, nodeID := range preference.NodeIDs {
		if !s.ownerOwnsNode(owner.ID, nodeID) {
			http.Error(w, "notification node is not owned by this account", http.StatusForbidden)
			return
		}
	}
	s.features.mu.Lock()
	s.features.data.NotificationPreferences[owner.ID] = preference
	err = s.features.saveLocked()
	s.features.mu.Unlock()
	if err != nil {
		http.Error(w, "save notification preference failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "preference": preference})
}

func (s *Server) handleAccountNotificationTest(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.notificationOwner(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.validAdminOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return
	}
	if s.notificationLimiter != nil && !s.notificationLimiter.Allow("test\x00"+owner.ID+"\x00"+requestClientIP(r)) {
		http.Error(w, "test notification is rate limited", http.StatusTooManyRequests)
		return
	}
	now := time.Now()
	s.features.mu.Lock()
	settings := normalizeShowDocSettings(s.features.data.ShowDocSettings)
	binding := s.features.data.NotificationBindings[owner.ID]
	if !settings.Enabled || binding.Status != "bound" || binding.TokenCipher == "" {
		s.features.mu.Unlock()
		http.Error(w, "ShowDoc binding is unavailable", http.StatusConflict)
		return
	}
	id, err := newFeatureID("notice")
	if err == nil {
		s.features.data.NotificationDeliveries = append(s.features.data.NotificationDeliveries, NotificationDelivery{
			ID: id, OwnerID: owner.ID, Event: "notification.test", Title: "云镜监控测试通知",
			Content: "ShowDoc 微信推送绑定成功。\n时间：" + now.Format("2006-01-02 15:04:05"),
			Status:  "pending", NextAttemptAt: now.Unix(), IdempotencyKey: fmt.Sprintf("test:%s:%d", owner.ID, now.UnixNano()), CreatedAt: now.Unix(),
		})
		err = s.features.saveLocked()
	}
	s.features.mu.Unlock()
	if err != nil {
		http.Error(w, "queue test notification failed", http.StatusInternalServerError)
		return
	}
	go s.processShowDocNotifications()
	writeJSON(w, map[string]any{"ok": true, "status": "queued", "delivery_id": id})
}

func (s *Server) handleAccountNotificationUnbind(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.notificationOwner(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.validAdminOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return
	}
	s.features.mu.Lock()
	delete(s.features.data.NotificationBindings, owner.ID)
	now := time.Now().Unix()
	for i := range s.features.data.NotificationBindSessions {
		if s.features.data.NotificationBindSessions[i].OwnerID == owner.ID {
			s.features.data.NotificationBindSessions[i].ConsumedAt = now
		}
	}
	for i := range s.features.data.NotificationDeliveries {
		item := &s.features.data.NotificationDeliveries[i]
		if item.OwnerID == owner.ID && (item.Status == "pending" || item.Status == "failed" || item.Status == "digest") {
			item.Status, item.Error = "cancelled", "ShowDoc binding removed"
		}
	}
	err := s.features.saveLocked()
	s.features.mu.Unlock()
	if err != nil {
		http.Error(w, "remove ShowDoc binding failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleAccountNotificationDeliveries(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.notificationOwner(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	limit := 50
	if value := strings.TrimSpace(r.URL.Query().Get("limit")); value != "" {
		if parsed, err := parseStrictPositiveInt(value, 100); err == nil {
			limit = parsed
		}
	}
	s.features.mu.RLock()
	items := make([]NotificationDelivery, 0, limit)
	for index := len(s.features.data.NotificationDeliveries) - 1; index >= 0 && len(items) < limit; index-- {
		item := s.features.data.NotificationDeliveries[index]
		if item.OwnerID == owner.ID {
			item.IdempotencyKey = ""
			items = append(items, item)
		}
	}
	s.features.mu.RUnlock()
	writeJSON(w, items)
}

func (s *Server) notificationOwner(w http.ResponseWriter, r *http.Request) (Owner, bool) {
	owner, ok := s.ownerAuthorized(r)
	if !ok {
		http.Error(w, "owner login required", http.StatusUnauthorized)
		return Owner{}, false
	}
	if s.features == nil {
		http.Error(w, "notification service unavailable", http.StatusServiceUnavailable)
		return Owner{}, false
	}
	return owner, true
}

func notificationBindingView(binding NotificationBinding, exists bool) map[string]any {
	if !exists || binding.Status != "bound" {
		return map[string]any{"bound": false, "status": "unbound"}
	}
	return map[string]any{
		"bound": true, "provider": binding.Provider, "status": binding.Status,
		"token_suffix": binding.TokenSuffix, "bound_at": binding.BoundAt,
		"last_success_at": binding.LastSuccess, "last_error": binding.LastError,
	}
}

func notificationBindSessionView(session NotificationBindSession) map[string]any {
	return map[string]any{"status": "pending", "session_id": session.ID, "qr_code_url": session.QRCodeURL, "expires_at": session.ExpiresAt}
}

func (s *Server) pruneNotificationBindSessionsLocked(now int64) {
	out := s.features.data.NotificationBindSessions[:0]
	for _, session := range s.features.data.NotificationBindSessions {
		if session.ExpiresAt > now && session.ConsumedAt == 0 {
			out = append(out, session)
		}
	}
	s.features.data.NotificationBindSessions = out
}

func parseStrictPositiveInt(value string, max int) (int, error) {
	result := 0
	for _, character := range value {
		if character < '0' || character > '9' {
			return 0, fmt.Errorf("invalid integer")
		}
		result = result*10 + int(character-'0')
		if result > max {
			return 0, fmt.Errorf("integer is too large")
		}
	}
	if result < 1 {
		return 0, fmt.Errorf("integer must be positive")
	}
	return result, nil
}

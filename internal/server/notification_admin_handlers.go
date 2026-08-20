package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

func (s *Server) handleAdminNotifications(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		http.Error(w, "admin login required", http.StatusUnauthorized)
		return
	}
	if s.features == nil {
		http.Error(w, "notification service unavailable", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Cache-Control", "no-store")
		data := s.features.Snapshot()
		writeJSON(w, map[string]any{"data": data, "stats": notificationAdminStats(data), "orders": data.MarketOrderIntents})
	case http.MethodPost:
		saveAdminNotificationSettings(w, r, s)
	default:
		methodNotAllowed(w)
	}
}

func saveAdminNotificationSettings(w http.ResponseWriter, r *http.Request, s *Server) {
	if !s.validAdminOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return
	}
	defer r.Body.Close()
	var req struct {
		Settings *struct {
			Enabled     *bool     `json:"enabled"`
			AdminEvents *[]string `json:"admin_events"`
			RetryLimit  *int      `json:"retry_limit"`
		} `json:"settings"`
		Enabled     *bool     `json:"enabled"`
		AdminEvents *[]string `json:"admin_events"`
		RetryLimit  *int      `json:"retry_limit"`
		RetryCount  *int      `json:"retry_count"`
		AdminToken  string    `json:"admin_token"`
		ClearToken  bool      `json:"clear_token"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req) != nil {
		http.Error(w, "invalid notification settings", http.StatusBadRequest)
		return
	}
	s.features.mu.Lock()
	settings := s.features.data.ShowDocSettings
	if req.Settings != nil && req.Settings.Enabled != nil {
		settings.Enabled = *req.Settings.Enabled
	}
	if req.Enabled != nil {
		settings.Enabled = *req.Enabled
	}
	if req.Settings != nil && req.Settings.AdminEvents != nil {
		settings.AdminEvents = *req.Settings.AdminEvents
	}
	if req.AdminEvents != nil {
		settings.AdminEvents = *req.AdminEvents
	}
	if req.Settings != nil && req.Settings.RetryLimit != nil {
		settings.RetryLimit = *req.Settings.RetryLimit
	}
	if req.RetryLimit != nil {
		settings.RetryLimit = *req.RetryLimit
	} else if req.RetryCount != nil {
		settings.RetryLimit = *req.RetryCount
	}
	settings = normalizeShowDocSettings(settings)
	token := strings.TrimSpace(req.AdminToken)
	if req.ClearToken {
		settings.AdminTokenCipher, settings.AdminTokenSuffix = "", ""
	} else if token != "" {
		if len(token) < 8 || len(token) > 512 {
			s.features.mu.Unlock()
			http.Error(w, "ShowDoc Push Token must be 8-512 characters", http.StatusBadRequest)
			return
		}
		ciphertext, err := s.features.cipher.encrypt(token)
		if err != nil {
			s.features.mu.Unlock()
			http.Error(w, "encrypt ShowDoc token failed", http.StatusInternalServerError)
			return
		}
		settings.AdminTokenCipher, settings.AdminTokenSuffix = ciphertext, notificationTokenSuffix(token)
	}
	s.features.data.ShowDocSettings = settings
	err := s.features.saveLocked()
	s.features.mu.Unlock()
	if err != nil {
		http.Error(w, "save notification settings failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleAdminNotificationTest(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		http.Error(w, "admin login required", http.StatusUnauthorized)
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
	var req struct {
		AdminToken string `json:"admin_token"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req)
	token := strings.TrimSpace(req.AdminToken)
	if token == "" {
		s.features.mu.RLock()
		ciphertext := s.features.data.ShowDocSettings.AdminTokenCipher
		s.features.mu.RUnlock()
		var err error
		token, err = s.features.cipher.decrypt(ciphertext)
		if err != nil {
			http.Error(w, "administrator ShowDoc token is not configured", http.StatusConflict)
			return
		}
	}
	if len(token) < 8 || len(token) > 512 {
		http.Error(w, "invalid ShowDoc Push Token", http.StatusBadRequest)
		return
	}
	now := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	status, deliveryErr := sendShowDocPush(ctx, token, "云镜监控管理员测试", "ShowDoc 管理员推送通道可用。\n时间："+now.Format("2006-01-02 15:04:05"))
	cancel()
	record := NotificationDelivery{OwnerID: "", Event: "notification.test", Title: "云镜监控管理员测试", Content: "管理员推送通道测试", CreatedAt: now.Unix(), Attempts: 1, HTTPStatus: status}
	record.ID, _ = newFeatureID("notice")
	if deliveryErr == nil {
		record.Status, record.DeliveredAt = "delivered", time.Now().Unix()
	} else {
		record.Status, record.Error = "failed", safeShowDocError(deliveryErr)
	}
	s.features.mu.Lock()
	s.features.data.NotificationDeliveries = append(s.features.data.NotificationDeliveries, record)
	_ = s.features.saveLocked()
	s.features.mu.Unlock()
	if deliveryErr != nil {
		http.Error(w, record.Error, http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "delivery": record})
}

func (s *Server) handleAdminNotificationRetry(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		http.Error(w, "admin login required", http.StatusUnauthorized)
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
	var req struct {
		ID         string `json:"id"`
		DeliveryID string `json:"delivery_id"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req) != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		id = strings.TrimSpace(req.DeliveryID)
	}
	s.features.mu.Lock()
	found := false
	for i := range s.features.data.NotificationDeliveries {
		item := &s.features.data.NotificationDeliveries[i]
		if item.ID == id && item.Status == "failed" {
			item.Status, item.Error, item.Attempts = "pending", "", 0
			item.NextAttemptAt = time.Now().Unix()
			found = true
			break
		}
	}
	if found {
		_ = s.features.saveLocked()
	}
	s.features.mu.Unlock()
	if !found {
		http.Error(w, "failed delivery not found", http.StatusNotFound)
		return
	}
	go s.processShowDocNotifications()
	writeJSON(w, map[string]bool{"ok": true})
}

func notificationAdminStats(data PlatformFeatureData) map[string]int {
	stats := map[string]int{"bound_users": len(data.NotificationBindings), "orders": len(data.MarketOrderIntents)}
	for _, item := range data.NotificationDeliveries {
		switch item.Status {
		case "delivered":
			stats["delivered"]++
		case "failed":
			stats["failed"]++
		case "pending", "sending", "digest":
			stats["pending"]++
		}
	}
	for _, item := range data.MarketOrderIntents {
		stats["order_"+item.Status]++
	}
	return stats
}

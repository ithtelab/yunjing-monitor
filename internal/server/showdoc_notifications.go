package server

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

const showDocPushBaseURL = "https://push.showdoc.com.cn"

var showDocHTTPClient = newShowDocClient()

var notificationEvents = map[string]bool{
	"alert.firing": true, "alert.recovered": true,
	"market.approved": true, "market.rejected": true,
	"market.subscription.match": true, "market.report.created": true,
	"market.report.resolved": true, "market.appeal.resolved": true,
	"market.order.created": true, "market.order.accepted": true,
	"market.order.completed": true, "market.order.cancelled": true,
	"market.order.expired": true, "agent.upgrade.result": true,
	"backup.failed": true, "account.login": true,
	"node.token.rotated": true, "node.deleted": true,
	"notification.test": true,
}

var defaultOwnerNotificationEvents = []string{
	"alert.firing", "alert.recovered", "market.approved", "market.rejected",
	"market.subscription.match", "market.report.created", "market.report.resolved",
	"market.appeal.resolved", "market.order.created", "market.order.accepted",
	"market.order.completed", "market.order.cancelled", "market.order.expired",
	"agent.upgrade.result", "account.login", "node.token.rotated", "node.deleted",
}

func defaultShowDocSettings() ShowDocSettings {
	return ShowDocSettings{Enabled: true, RetryLimit: 5, AdminEvents: []string{"backup.failed", "agent.upgrade.result"}}
}

func normalizeShowDocSettings(settings ShowDocSettings) ShowDocSettings {
	if settings.RetryLimit < 1 || settings.RetryLimit > 10 {
		settings.RetryLimit = 5
	}
	settings.AdminEvents = allowedNotificationEvents(settings.AdminEvents, 30)
	return settings
}

func allowedNotificationEvents(values []string, max int) []string {
	out := make([]string, 0, len(values))
	for _, value := range cleanFeatureList(values, max, 48) {
		if notificationEvents[value] && value != "notification.test" {
			out = append(out, value)
		}
	}
	return out
}

func defaultNotificationPreference(ownerID string) NotificationPreference {
	return NotificationPreference{
		OwnerID: ownerID, Enabled: true,
		Events:   append([]string(nil), defaultOwnerNotificationEvents...),
		TimeZone: "Asia/Shanghai", DeliveryMode: "immediate",
	}
}

func normalizeNotificationPreference(ownerID string, preference NotificationPreference) (NotificationPreference, error) {
	preference.OwnerID = ownerID
	preference.Events = allowedNotificationEvents(preference.Events, 30)
	preference.NodeIDs = cleanFeatureList(preference.NodeIDs, 200, 80)
	preference.QuietStart = strings.TrimSpace(preference.QuietStart)
	preference.QuietEnd = strings.TrimSpace(preference.QuietEnd)
	if preference.QuietStart != "" || preference.QuietEnd != "" {
		if !validClockTime(preference.QuietStart) || !validClockTime(preference.QuietEnd) {
			return NotificationPreference{}, fmt.Errorf("quiet hours must use HH:MM")
		}
	}
	preference.TimeZone = cleanFeatureText(preference.TimeZone, 64)
	if preference.TimeZone == "" {
		preference.TimeZone = "Asia/Shanghai"
	}
	if _, err := notificationLocation(preference.TimeZone); err != nil {
		return NotificationPreference{}, fmt.Errorf("invalid time zone")
	}
	if preference.DeliveryMode != "digest" {
		preference.DeliveryMode = "immediate"
	}
	preference.UpdatedAt = time.Now().Unix()
	return preference, nil
}

func validClockTime(value string) bool {
	if len(value) != 5 || value[2] != ':' {
		return false
	}
	hour, hourErr := strconv.Atoi(value[:2])
	minute, minuteErr := strconv.Atoi(value[3:])
	return hourErr == nil && minuteErr == nil && hour >= 0 && hour < 24 && minute >= 0 && minute < 60
}

func notificationLocation(value string) (*time.Location, error) {
	if value == "UTC" {
		return time.UTC, nil
	}
	return time.LoadLocation(value)
}

type showDocAPIResponse struct {
	ErrorCode    int    `json:"error_code"`
	ErrorMessage string `json:"error_message"`
	Message      string `json:"message"`
	Data         struct {
		URL      string `json:"url"`
		QRScene  string `json:"qrscene"`
		Token    string `json:"token"`
		UserInfo struct {
			UserToken string `json:"user_token"`
		} `json:"user_info"`
	} `json:"data"`
}

func newShowDocClient() *http.Client {
	transport := &http.Transport{
		Proxy: nil, ForceAttemptHTTP2: true,
		TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 6 * time.Second,
		IdleConnTimeout: 30 * time.Second,
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: "push.showdoc.com.cn"},
		DialContext:     safeWebhookDialContext,
	}
	return &http.Client{
		Transport: transport, Timeout: 8 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 2 || !strings.EqualFold(req.URL.Scheme, "https") || !strings.EqualFold(req.URL.Hostname(), "push.showdoc.com.cn") {
				return fmt.Errorf("unexpected ShowDoc redirect")
			}
			return nil
		},
	}
}

func showDocPostForm(ctx context.Context, endpoint string, values url.Values) (showDocAPIResponse, int, error) {
	var result showDocAPIResponse
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, showDocPushBaseURL+endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return result, 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Monitor-Party-ShowDoc/1")
	resp, err := showDocHTTPClient.Do(req)
	if err != nil {
		return result, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil {
		return result, resp.StatusCode, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, resp.StatusCode, fmt.Errorf("ShowDoc HTTP %d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return result, resp.StatusCode, fmt.Errorf("decode ShowDoc response")
	}
	if result.ErrorCode != 0 {
		message := cleanFeatureText(result.ErrorMessage, 120)
		if message == "" {
			message = cleanFeatureText(result.Message, 120)
		}
		if message == "" {
			message = "ShowDoc request failed"
		}
		return result, resp.StatusCode, fmt.Errorf("%s", message)
	}
	return result, resp.StatusCode, nil
}

func createShowDocQRCode(ctx context.Context) (string, string, error) {
	result, _, err := showDocPostForm(ctx, "/server/api/wechat/getQrcodeUrl", url.Values{})
	if err != nil {
		return "", "", err
	}
	if !strings.HasPrefix(result.Data.URL, "https://mp.weixin.qq.com/") || result.Data.QRScene == "" {
		return "", "", fmt.Errorf("ShowDoc returned an invalid QR code")
	}
	return result.Data.URL, cleanFeatureText(result.Data.QRScene, 256), nil
}

func checkShowDocQRCode(ctx context.Context, qrScene string) (string, bool, error) {
	result, _, err := showDocPostForm(ctx, "/server/api/wechat/checkOrcodeStatus", url.Values{"qrscene": {qrScene}})
	if err != nil {
		return "", false, err
	}
	userToken := strings.TrimSpace(result.Data.UserInfo.UserToken)
	return userToken, userToken != "", nil
}

func getShowDocPushToken(ctx context.Context, userToken string) (string, error) {
	result, _, err := showDocPostForm(ctx, "/server/api/push/getToken", url.Values{"user_token": {userToken}, "redirect_login": {"false"}})
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(result.Data.Token)
	if len(token) < 8 || len(token) > 512 {
		return "", fmt.Errorf("ShowDoc returned an invalid push token")
	}
	return token, nil
}

func sendShowDocPush(ctx context.Context, token, title, content string) (int, error) {
	token = strings.TrimSpace(token)
	if len(token) < 8 || len(token) > 512 {
		return 0, fmt.Errorf("invalid ShowDoc push token")
	}
	_, status, err := showDocPostForm(ctx, "/server/api/push/"+url.PathEscape(token), url.Values{
		"title": {cleanFeatureText(title, 120)}, "content": {strings.TrimSpace(content)},
	})
	return status, err
}

func notificationTokenSuffix(token string) string {
	runes := []rune(strings.TrimSpace(token))
	if len(runes) <= 4 {
		return string(runes)
	}
	return string(runes[len(runes)-4:])
}

func (s *Server) enqueueShowDocEvent(event string, data any) {
	if s.features == nil || !notificationEvents[event] || event == "notification.test" {
		return
	}
	ownerID, nodeID := s.notificationEventOwner(event, data)
	title, content := formatNotificationEvent(event, data)
	if title == "" {
		return
	}
	s.features.mu.Lock()
	defer s.features.mu.Unlock()
	settings := normalizeShowDocSettings(s.features.data.ShowDocSettings)
	if !settings.Enabled {
		return
	}
	deliveryCount := len(s.features.data.NotificationDeliveries)
	if ownerID != "" {
		s.enqueueOwnerNotificationLocked(ownerID, nodeID, event, title, content, data)
	}
	if settings.AdminTokenCipher != "" && listContains(settings.AdminEvents, event) {
		s.enqueueNotificationLocked("", event, title, content, notificationIdempotencyKey("admin", event, data), "pending", time.Now().Unix())
	}
	if len(s.features.data.NotificationDeliveries) != deliveryCount {
		_ = s.features.saveLocked()
	}
}

func (s *Server) notificationEventOwner(_ string, data any) (string, string) {
	values := notificationDataMap(data)
	ownerID := notificationString(values, "owner_id")
	nodeID := notificationString(values, "node_id")
	if ownerID == "" && nodeID != "" {
		if planned, ok := s.store.GetPlannedNode(nodeID); ok {
			ownerID = planned.OwnerID
		}
	}
	return ownerID, nodeID
}

func notificationDataMap(data any) map[string]any {
	if values, ok := data.(map[string]any); ok {
		return values
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return map[string]any{}
	}
	values := map[string]any{}
	_ = json.Unmarshal(raw, &values)
	return values
}

func notificationString(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return cleanFeatureText(value, 240)
}

func (s *Server) enqueueOwnerNotificationLocked(ownerID, nodeID, event, title, content string, data any) {
	binding, ok := s.features.data.NotificationBindings[ownerID]
	if !ok || binding.Status != "bound" || binding.TokenCipher == "" {
		return
	}
	preference, ok := s.features.data.NotificationPreferences[ownerID]
	if !ok {
		preference = defaultNotificationPreference(ownerID)
	}
	if !preference.Enabled || !listContains(preference.Events, event) || nodeID != "" && len(preference.NodeIDs) > 0 && !listContains(preference.NodeIDs, nodeID) {
		return
	}
	status, dueAt := "pending", time.Now().Unix()
	if preference.DeliveryMode == "digest" {
		status, dueAt = "digest", nextDigestAt(time.Now(), preference.TimeZone).Unix()
	} else if quietUntil := notificationQuietUntil(time.Now(), preference); !quietUntil.IsZero() {
		dueAt = quietUntil.Unix()
	}
	s.enqueueNotificationLocked(ownerID, event, title, content, notificationIdempotencyKey(ownerID, event, data), status, dueAt)
}

func (s *Server) enqueueNotificationLocked(ownerID, event, title, content, key, status string, dueAt int64) {
	for _, item := range s.features.data.NotificationDeliveries {
		if item.IdempotencyKey == key {
			return
		}
	}
	id, err := newFeatureID("notice")
	if err != nil {
		return
	}
	s.features.data.NotificationDeliveries = append(s.features.data.NotificationDeliveries, NotificationDelivery{
		ID: id, OwnerID: ownerID, Event: event, Title: title, Content: content,
		Status: status, NextAttemptAt: dueAt, IdempotencyKey: key, CreatedAt: time.Now().Unix(),
	})
	if len(s.features.data.NotificationDeliveries) > 1500 {
		s.features.data.NotificationDeliveries = append([]NotificationDelivery(nil), s.features.data.NotificationDeliveries[len(s.features.data.NotificationDeliveries)-1500:]...)
	}
}

func notificationIdempotencyKey(ownerID, event string, data any) string {
	raw, _ := json.Marshal(data)
	sum := sha256.Sum256(append([]byte(ownerID+"\x00"+event+"\x00"), raw...))
	return hex.EncodeToString(sum[:])
}

func formatNotificationEvent(event string, data any) (string, string) {
	values := notificationDataMap(data)
	node := notificationString(values, "node_name")
	if node == "" {
		node = notificationString(values, "display_name")
	}
	if node == "" {
		node = notificationString(values, "node_id")
	}
	message := notificationString(values, "message")
	if message == "" {
		message = notificationString(values, "error")
	}
	if message == "" {
		message = notificationString(values, "reason")
	}
	if message == "" {
		message = notificationString(values, "resolution")
	}
	titles := map[string]string{
		"alert.firing": "服务器告警", "alert.recovered": "服务器已恢复",
		"market.approved": "服务器上架审核通过", "market.rejected": "服务器上架审核未通过",
		"market.subscription.match": "市场订阅匹配", "market.report.created": "收到市场举报",
		"market.report.resolved": "市场举报已处理", "market.appeal.resolved": "市场申诉已处理",
		"market.order.created": "收到新的购买意向", "market.order.accepted": "购买意向已接受",
		"market.order.completed": "交易意向已完成", "market.order.cancelled": "交易意向已取消",
		"market.order.expired": "交易意向已过期", "agent.upgrade.result": "Agent 升级结果",
		"backup.failed": "备份任务失败", "account.login": "账户登录提醒",
		"node.token.rotated": "Agent Token 已重置", "node.deleted": "服务器已删除",
	}
	title := titles[event]
	if title == "" {
		return "", ""
	}
	lines := []string{"事件：" + title}
	if node != "" {
		lines = append(lines, "服务器："+node)
	}
	if message != "" {
		lines = append(lines, "详情："+message)
	}
	if contact := notificationString(values, "buyer_contact"); contact != "" {
		lines = append(lines, "买家联系方式："+contact)
	}
	if price := notificationString(values, "price"); price != "" {
		lines = append(lines, "价格："+price)
	}
	if version := notificationString(values, "version"); version != "" {
		lines = append(lines, "版本："+version)
	}
	detailKeys := []struct {
		key   string
		label string
	}{
		{"reason", "原因"}, {"resolution", "处理结果"}, {"subscription_name", "订阅"},
		{"status", "状态"}, {"buyer_contact", "买家联系方式"}, {"price", "价格"},
		{"version", "版本"}, {"action", "操作"},
	}
	for _, detail := range detailKeys {
		if value := notificationString(values, detail.key); value != "" {
			lines = append(lines, detail.label+"："+value)
		}
	}
	lines = append(lines, "时间："+time.Now().Format("2006-01-02 15:04:05"))
	return title, strings.Join(lines, "\n")
}

func notificationQuietUntil(now time.Time, preference NotificationPreference) time.Time {
	if !validClockTime(preference.QuietStart) || !validClockTime(preference.QuietEnd) || preference.QuietStart == preference.QuietEnd {
		return time.Time{}
	}
	location, err := notificationLocation(preference.TimeZone)
	if err != nil {
		return time.Time{}
	}
	local := now.In(location)
	startHour, _ := strconv.Atoi(preference.QuietStart[:2])
	startMinute, _ := strconv.Atoi(preference.QuietStart[3:])
	endHour, _ := strconv.Atoi(preference.QuietEnd[:2])
	endMinute, _ := strconv.Atoi(preference.QuietEnd[3:])
	start := time.Date(local.Year(), local.Month(), local.Day(), startHour, startMinute, 0, 0, location)
	end := time.Date(local.Year(), local.Month(), local.Day(), endHour, endMinute, 0, 0, location)
	if !end.After(start) {
		if local.Before(end) {
			start = start.AddDate(0, 0, -1)
		} else {
			end = end.AddDate(0, 0, 1)
		}
	}
	if !local.Before(start) && local.Before(end) {
		return end
	}
	return time.Time{}
}

func nextDigestAt(now time.Time, timeZone string) time.Time {
	location, err := notificationLocation(timeZone)
	if err != nil {
		location = time.UTC
	}
	local := now.In(location)
	next := time.Date(local.Year(), local.Month(), local.Day(), 9, 0, 0, 0, location)
	if !next.After(local) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

func (s *Server) processShowDocNotifications() {
	if s.features == nil {
		return
	}
	if !s.features.beginNotificationProcessing() {
		return
	}
	defer s.features.endNotificationProcessing()
	s.processShowDocDigests()
	for processed := 0; processed < 25; processed++ {
		delivery, token, retryLimit, ok := s.features.nextShowDocDelivery()
		if !ok {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		status, err := sendShowDocPush(ctx, token, delivery.Title, delivery.Content)
		cancel()
		s.features.finishShowDocDelivery(delivery.ID, status, retryLimit, err)
	}
}

func (h *PlatformFeatureHub) beginNotificationProcessing() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.notificationProcessing {
		return false
	}
	h.notificationProcessing = true
	return true
}

func (h *PlatformFeatureHub) endNotificationProcessing() {
	h.mu.Lock()
	h.notificationProcessing = false
	h.mu.Unlock()
}

func (h *PlatformFeatureHub) nextShowDocDelivery() (NotificationDelivery, string, int, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now().Unix()
	settings := normalizeShowDocSettings(h.data.ShowDocSettings)
	if !settings.Enabled {
		return NotificationDelivery{}, "", settings.RetryLimit, false
	}
	for i := range h.data.NotificationDeliveries {
		item := &h.data.NotificationDeliveries[i]
		if item.Status != "pending" && item.Status != "failed" || item.NextAttemptAt > now || item.Attempts >= settings.RetryLimit {
			continue
		}
		ciphertext := settings.AdminTokenCipher
		if item.OwnerID != "" {
			binding := h.data.NotificationBindings[item.OwnerID]
			if binding.Status != "bound" {
				item.Status, item.Error = "failed", "ShowDoc binding is unavailable"
				continue
			}
			ciphertext = binding.TokenCipher
		}
		token, err := h.cipher.decrypt(ciphertext)
		if err != nil {
			item.Status, item.Error, item.Attempts = "failed", "ShowDoc token is unavailable", settings.RetryLimit
			_ = h.saveLocked()
			continue
		}
		item.Status = "sending"
		item.Attempts++
		_ = h.saveLocked()
		return *item, token, settings.RetryLimit, true
	}
	return NotificationDelivery{}, "", settings.RetryLimit, false
}

func (h *PlatformFeatureHub) finishShowDocDelivery(id string, httpStatus, retryLimit int, deliveryErr error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.data.NotificationDeliveries {
		item := &h.data.NotificationDeliveries[i]
		if item.ID != id {
			continue
		}
		item.HTTPStatus = httpStatus
		if deliveryErr == nil {
			item.Status, item.Error, item.DeliveredAt = "delivered", "", time.Now().Unix()
			if item.OwnerID != "" {
				binding := h.data.NotificationBindings[item.OwnerID]
				binding.LastSuccess, binding.LastError = item.DeliveredAt, ""
				h.data.NotificationBindings[item.OwnerID] = binding
			}
		} else {
			item.Status, item.Error = "failed", safeShowDocError(deliveryErr)
			if item.Attempts < retryLimit {
				item.NextAttemptAt = time.Now().Add(notificationRetryDelay(item.Attempts)).Unix()
			}
			if item.OwnerID != "" {
				binding := h.data.NotificationBindings[item.OwnerID]
				binding.LastError = item.Error
				h.data.NotificationBindings[item.OwnerID] = binding
			}
		}
		_ = h.saveLocked()
		return
	}
}

func notificationRetryDelay(attempt int) time.Duration {
	delays := []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour, 12 * time.Hour}
	if attempt < 1 {
		attempt = 1
	}
	if attempt > len(delays) {
		attempt = len(delays)
	}
	return delays[attempt-1]
}

func safeShowDocError(err error) string {
	if err == nil {
		return ""
	}
	value := strings.ToLower(err.Error())
	switch {
	case strings.Contains(value, "timeout"), strings.Contains(value, "deadline"):
		return "ShowDoc push timed out"
	case strings.Contains(value, "http"):
		return cleanFeatureText(err.Error(), 80)
	case strings.Contains(value, "token"):
		return "ShowDoc rejected the push token"
	default:
		return "ShowDoc push failed"
	}
}

func (s *Server) processShowDocDigests() {
	h := s.features
	h.mu.Lock()
	now := time.Now().Unix()
	groups := map[string][]int{}
	for i := range h.data.NotificationDeliveries {
		item := h.data.NotificationDeliveries[i]
		if item.Status == "digest" && item.NextAttemptAt <= now {
			groups[item.OwnerID] = append(groups[item.OwnerID], i)
		}
	}
	owners := make([]string, 0, len(groups))
	for ownerID := range groups {
		owners = append(owners, ownerID)
	}
	sort.Strings(owners)
	for _, ownerID := range owners {
		indexes := groups[ownerID]
		lines := make([]string, 0, min(len(indexes), 50)+2)
		lines = append(lines, fmt.Sprintf("过去一天共收到 %d 条通知：", len(indexes)))
		for position, index := range indexes {
			item := &h.data.NotificationDeliveries[index]
			if position < 50 {
				lines = append(lines, "- "+item.Title)
			}
			item.Status = "bundled"
		}
		if len(indexes) > 50 {
			lines = append(lines, fmt.Sprintf("还有 %d 条通知请在账户中心查看。", len(indexes)-50))
		}
		key := notificationIdempotencyKey(ownerID, "notification.digest", map[string]any{"ids": deliveryIDs(h.data.NotificationDeliveries, indexes)})
		s.enqueueNotificationLocked(ownerID, "notification.digest", "云镜监控每日摘要", strings.Join(lines, "\n"), key, "pending", now)
	}
	if len(groups) > 0 {
		_ = h.saveLocked()
	}
	h.mu.Unlock()
}

func deliveryIDs(items []NotificationDelivery, indexes []int) []string {
	out := make([]string, 0, len(indexes))
	for _, index := range indexes {
		out = append(out, items[index].ID)
	}
	return out
}

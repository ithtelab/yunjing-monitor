package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"vps-agent/internal/server/domain"
)

type AlertSettings struct {
	Enabled         bool    `json:"enabled"`
	CPUThreshold    float64 `json:"cpu_threshold"`
	MemoryThreshold float64 `json:"memory_threshold"`
	DiskThreshold   float64 `json:"disk_threshold"`
	OfflineSeconds  int     `json:"offline_seconds"`
	DueDays         int     `json:"due_days"`
	CooldownMinutes int     `json:"cooldown_minutes"`
	WebhookURL      string  `json:"webhook_url,omitempty"`
	TelegramChatID  string  `json:"telegram_chat_id,omitempty"`
	TelegramToken   string  `json:"telegram_token,omitempty"`
	TokenConfigured bool    `json:"token_configured"`
}

type AlertEvent struct {
	ID        string `json:"id"`
	OwnerID   string `json:"owner_id,omitempty"`
	NodeID    string `json:"node_id"`
	NodeName  string `json:"node_name"`
	Kind      string `json:"kind"`
	Status    string `json:"status"`
	Message   string `json:"message"`
	CreatedAt int64  `json:"created_at"`
	Notified  bool   `json:"notified"`
	Error     string `json:"error,omitempty"`
}

// RecordExternal appends an alert produced by a subsystem outside the node
// resource evaluator while preserving the same history and notification path.
func (a *AlertService) RecordExternal(event AlertEvent, notify bool) {
	settings := a.Settings()
	a.mu.Lock()
	a.events = append(a.events, event)
	if len(a.events) > 500 {
		a.events = append([]AlertEvent(nil), a.events[len(a.events)-500:]...)
	}
	hook := a.eventHook
	a.mu.Unlock()
	_ = a.Save()
	if hook != nil {
		go hook(event)
	}
	if !notify || !settings.Enabled {
		return
	}
	go func() {
		err := a.notify(event, settings)
		a.mu.Lock()
		for index := len(a.events) - 1; index >= 0; index-- {
			if a.events[index].ID != event.ID {
				continue
			}
			a.events[index].Notified = err == nil
			if err != nil {
				a.events[index].Error = err.Error()
			}
			break
		}
		a.mu.Unlock()
		_ = a.Save()
	}()
}

type alertState struct {
	Active     bool  `json:"active"`
	LastSentAt int64 `json:"last_sent_at"`
}

type alertFile struct {
	Version             int                   `json:"version"`
	Settings            AlertSettings         `json:"settings"`
	TelegramTokenCipher string                `json:"telegram_token_cipher,omitempty"`
	States              map[string]alertState `json:"states"`
	Events              []AlertEvent          `json:"events"`
}

type AlertService struct {
	mu        sync.RWMutex
	saveMu    sync.Mutex
	path      string
	cipher    *tokenCipher
	settings  AlertSettings
	token     string
	states    map[string]alertState
	events    []AlertEvent
	client    *http.Client
	eventHook func(AlertEvent)
}

func (a *AlertService) SetEventHook(hook func(AlertEvent)) {
	a.mu.Lock()
	a.eventHook = hook
	a.mu.Unlock()
}

func defaultAlertSettings() AlertSettings {
	return AlertSettings{CPUThreshold: 90, MemoryThreshold: 90, DiskThreshold: 90, OfflineSeconds: 180, DueDays: 7, CooldownMinutes: 30}
}

func alertDataPath(dataPath string) string {
	return filepath.Join(filepath.Dir(dataPath), "alerts.json")
}

func NewAlertService(path, secret string) (*AlertService, error) {
	cipher, err := newTokenCipher(secret)
	if err != nil {
		return nil, err
	}
	a := &AlertService{
		path: path, cipher: cipher, settings: defaultAlertSettings(), states: map[string]alertState{},
		client: notificationHTTPClient(),
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return a, nil
	}
	if err != nil {
		return nil, err
	}
	var file alertFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, err
	}
	a.settings = normalizeAlertSettings(file.Settings)
	a.states, a.events = file.States, file.Events
	if a.states == nil {
		a.states = map[string]alertState{}
	}
	if file.TelegramTokenCipher != "" {
		if token, err := cipher.decrypt(file.TelegramTokenCipher); err == nil {
			a.token = token
		}
	}
	return a, nil
}

func notificationHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, resolved := range addresses {
				if !publicNotificationIP(resolved.IP) {
					return nil, errors.New("notification target resolved to a private or reserved IP")
				}
			}
			if len(addresses) == 0 {
				return nil, errors.New("notification target did not resolve")
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(addresses[0].IP.String(), port))
		},
		ForceAttemptHTTP2: true,
	}
	return &http.Client{Transport: transport, Timeout: 8 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
}

func normalizeAlertSettings(value AlertSettings) AlertSettings {
	defaults := defaultAlertSettings()
	if value.CPUThreshold < 1 || value.CPUThreshold > 100 {
		value.CPUThreshold = defaults.CPUThreshold
	}
	if value.MemoryThreshold < 1 || value.MemoryThreshold > 100 {
		value.MemoryThreshold = defaults.MemoryThreshold
	}
	if value.DiskThreshold < 1 || value.DiskThreshold > 100 {
		value.DiskThreshold = defaults.DiskThreshold
	}
	if value.OfflineSeconds < 30 || value.OfflineSeconds > 86400 {
		value.OfflineSeconds = defaults.OfflineSeconds
	}
	if value.DueDays < 1 || value.DueDays > 365 {
		value.DueDays = defaults.DueDays
	}
	if value.CooldownMinutes < 1 || value.CooldownMinutes > 10080 {
		value.CooldownMinutes = defaults.CooldownMinutes
	}
	value.WebhookURL = strings.TrimSpace(value.WebhookURL)
	value.TelegramChatID = strings.TrimSpace(value.TelegramChatID)
	value.TelegramToken = ""
	return value
}

func (a *AlertService) Settings() AlertSettings {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := a.settings
	out.TokenConfigured = a.token != ""
	out.TelegramToken = ""
	return out
}

func (a *AlertService) UpdateSettings(value AlertSettings) error {
	newToken := strings.TrimSpace(value.TelegramToken)
	value = normalizeAlertSettings(value)
	if value.WebhookURL != "" {
		if err := validateNotificationURL(value.WebhookURL); err != nil {
			return err
		}
	}
	a.mu.Lock()
	if newToken != "" && newToken != "-" {
		a.token = newToken
	}
	if newToken == "-" {
		a.token = ""
	}
	value.TelegramToken = ""
	value.TokenConfigured = a.token != ""
	a.settings = value
	a.mu.Unlock()
	return a.Save()
}

func (a *AlertService) Events(limit int) []AlertEvent {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	start := len(a.events) - limit
	if start < 0 {
		start = 0
	}
	out := append([]AlertEvent(nil), a.events[start:]...)
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out
}

func (a *AlertService) Evaluate(nodes []domain.AdminNode, hosts []AkileHost) {
	settings := a.Settings()
	if !settings.Enabled {
		return
	}
	now := time.Now().Unix()
	hostByID := make(map[string]AkileHost, len(hosts))
	for _, host := range hosts {
		hostByID[host.Host.Name] = host
	}
	for _, node := range nodes {
		name := node.DisplayName
		if name == "" {
			name = node.NodeID
		}
		checks := map[string]struct {
			active  bool
			message string
		}{}
		if node.LastSeen > 0 {
			age := now - node.LastSeen
			checks["offline"] = struct {
				active  bool
				message string
			}{age > int64(settings.OfflineSeconds), fmt.Sprintf("%s 已离线 %d 秒", name, age)}
		}
		if due := node.Info.DueTime; due > 0 {
			days := int((due - now) / 86400)
			message := fmt.Sprintf("%s 将在 %d 天内到期", name, days)
			if due < now {
				message = fmt.Sprintf("%s 已到期 %d 天", name, -days)
			}
			checks["due"] = struct {
				active  bool
				message string
			}{days <= settings.DueDays, message}
		}
		if host, ok := hostByID[node.NodeID]; ok && node.Online {
			checks["cpu"] = struct {
				active  bool
				message string
			}{host.State.CPU >= settings.CPUThreshold, fmt.Sprintf("%s CPU 使用率 %.1f%%", name, host.State.CPU)}
			memPercent := percent(host.State.MemUsed, host.Host.MemTotal)
			checks["memory"] = struct {
				active  bool
				message string
			}{memPercent >= settings.MemoryThreshold, fmt.Sprintf("%s 内存使用率 %.1f%%", name, memPercent)}
			diskPercent := percent(host.State.DiskUsed, host.State.DiskTotal)
			checks["disk"] = struct {
				active  bool
				message string
			}{diskPercent >= settings.DiskThreshold, fmt.Sprintf("%s 磁盘使用率 %.1f%%", name, diskPercent)}
		} else {
			for _, kind := range []string{"cpu", "memory", "disk"} {
				checks[kind] = struct {
					active  bool
					message string
				}{false, name + " 资源状态已恢复"}
			}
		}
		for kind, check := range checks {
			a.transition(node.NodeID, name, kind, check.active, check.message, now, settings)
		}
	}
}

func percent(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(used) / float64(total) * 100
}

func (a *AlertService) transition(nodeID, nodeName, kind string, active bool, message string, now int64, settings AlertSettings) {
	key := nodeID + ":" + kind
	a.mu.Lock()
	state := a.states[key]
	if state.Active == active {
		a.mu.Unlock()
		return
	}
	state.Active = active
	status := "resolved"
	if active {
		status = "firing"
	}
	shouldNotify := now-state.LastSentAt >= int64(settings.CooldownMinutes*60)
	if !active {
		shouldNotify = true
	}
	if shouldNotify {
		state.LastSentAt = now
	}
	a.states[key] = state
	event := AlertEvent{ID: fmt.Sprintf("%d-%s-%s-%s", now, nodeID, kind, status), NodeID: nodeID, NodeName: nodeName, Kind: kind, Status: status, Message: message, CreatedAt: now}
	a.events = append(a.events, event)
	if len(a.events) > 500 {
		a.events = append([]AlertEvent(nil), a.events[len(a.events)-500:]...)
	}
	a.mu.Unlock()
	_ = a.Save()
	a.mu.RLock()
	hook := a.eventHook
	a.mu.RUnlock()
	if hook != nil {
		go hook(event)
	}
	if shouldNotify {
		go func() {
			err := a.notify(event, settings)
			a.mu.Lock()
			for index := len(a.events) - 1; index >= 0; index-- {
				if a.events[index].ID != event.ID {
					continue
				}
				a.events[index].Notified = err == nil
				if err != nil {
					a.events[index].Error = err.Error()
				}
				break
			}
			a.mu.Unlock()
			_ = a.Save()
		}()
	}
}

func (a *AlertService) notify(event AlertEvent, settings AlertSettings) error {
	prefix := "[恢复]"
	if event.Status == "firing" {
		prefix = "[告警]"
	}
	message := prefix + " " + event.Message
	var errs []string
	configured := false
	if settings.WebhookURL != "" {
		configured = true
		if err := a.postJSON(settings.WebhookURL, map[string]any{"event": event, "text": message}); err != nil {
			errs = append(errs, "webhook: "+err.Error())
		}
	}
	a.mu.RLock()
	token := a.token
	a.mu.RUnlock()
	if token != "" && settings.TelegramChatID != "" {
		configured = true
		endpoint := "https://api.telegram.org/bot" + url.PathEscape(token) + "/sendMessage"
		if err := a.postJSON(endpoint, map[string]string{"chat_id": settings.TelegramChatID, "text": message}); err != nil {
			errs = append(errs, "telegram: "+err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	if !configured {
		return errors.New("no notification channel is configured")
	}
	return nil
}

func (a *AlertService) postJSON(endpoint string, value any) error {
	if !strings.HasPrefix(endpoint, "https://api.telegram.org/") {
		if err := validateNotificationURL(endpoint); err != nil {
			return err
		}
	}
	raw, _ := json.Marshal(value)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

func validateNotificationURL(value string) error {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return errors.New("webhook_url must be an absolute HTTPS URL")
	}
	if strings.EqualFold(u.Hostname(), "localhost") {
		return errors.New("webhook_url cannot target localhost")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && !publicNotificationIP(ip) {
		return errors.New("webhook_url cannot target a private or reserved IP")
	}
	return nil
}

func publicNotificationIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsMulticast() || addr.IsUnspecified() {
		return false
	}
	for _, prefix := range notificationReservedPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

var notificationReservedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001:db8::/32"),
}

func (a *AlertService) Save() error {
	a.saveMu.Lock()
	defer a.saveMu.Unlock()
	a.mu.RLock()
	settings, token, states, events := a.settings, a.token, make(map[string]alertState, len(a.states)), append([]AlertEvent(nil), a.events...)
	for key, value := range a.states {
		states[key] = value
	}
	a.mu.RUnlock()
	ciphertext := ""
	var err error
	if token != "" {
		ciphertext, err = a.cipher.encrypt(token)
		if err != nil {
			return err
		}
	}
	settings.TelegramToken, settings.TokenConfigured = "", token != ""
	return writeJSONAtomic(a.path, alertFile{Version: 1, Settings: settings, TelegramTokenCipher: ciphertext, States: states, Events: events}, 0o600)
}

package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var platformWebhookEvents = map[string]bool{
	"*":                         true,
	"agent.upgrade.result":      true,
	"alert.firing":              true,
	"alert.recovered":           true,
	"market.approved":           true,
	"market.rejected":           true,
	"market.subscription.match": true,
	"market.report.created":     true,
	"market.report.resolved":    true,
	"market.appeal.resolved":    true,
	"market.order.created":      true,
	"market.order.accepted":     true,
	"market.order.completed":    true,
	"market.order.cancelled":    true,
	"market.order.expired":      true,
	"backup.failed":             true,
	"account.login":             true,
	"node.token.rotated":        true,
	"node.deleted":              true,
}

type platformWebhookEnvelope struct {
	DeliveryID string          `json:"delivery_id"`
	Event      string          `json:"event"`
	CreatedAt  int64           `json:"created_at"`
	Data       json.RawMessage `json:"data"`
}

func allowedWebhookEvents(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range cleanFeatureList(values, 20, 48) {
		if platformWebhookEvents[value] {
			out = append(out, value)
		}
	}
	return out
}

func (s *Server) dispatchPlatformEvent(event string, data any) {
	if s.features == nil || !platformWebhookEvents[event] || event == "*" {
		return
	}
	s.enqueueShowDocEvent(event, data)
	go s.processShowDocNotifications()
	s.features.mu.RLock()
	hooks := make([]WebhookIntegration, 0, len(s.features.data.Webhooks))
	for _, hook := range s.features.data.Webhooks {
		if hook.Enabled && (listContains(hook.Events, event) || listContains(hook.Events, "*")) {
			hooks = append(hooks, hook)
		}
	}
	s.features.mu.RUnlock()
	for _, hook := range hooks {
		s.deliverPlatformWebhook(hook, event, data)
	}
}

func (s *Server) deliverPlatformWebhook(hook WebhookIntegration, event string, data any) {
	started := time.Now()
	deliveryID, _ := newFeatureID("delivery")
	record := WebhookDelivery{ID: deliveryID, WebhookID: hook.ID, Event: event, Status: "failed", CreatedAt: started.Unix()}
	secret, err := s.features.cipher.decrypt(hook.SecretCipher)
	if err != nil {
		record.Error = "webhook secret is unavailable"
		s.features.recordWebhookDelivery(record)
		return
	}
	rawData, err := json.Marshal(data)
	if err != nil {
		record.Error = "encode webhook data"
		s.features.recordWebhookDelivery(record)
		return
	}
	body, err := json.Marshal(platformWebhookEnvelope{DeliveryID: deliveryID, Event: event, CreatedAt: started.Unix(), Data: rawData})
	if err != nil {
		record.Error = "encode webhook envelope"
		s.features.recordWebhookDelivery(record)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hook.URL, bytes.NewReader(body))
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Monitor-Party-Webhook/1")
		req.Header.Set("X-Monitor-Event", event)
		req.Header.Set("X-Monitor-Delivery", deliveryID)
		timestamp := strconv.FormatInt(started.Unix(), 10)
		req.Header.Set("X-Monitor-Timestamp", timestamp)
		req.Header.Set("X-Monitor-Signature", "sha256="+platformWebhookSignature(timestamp, body, secret))
		var resp *http.Response
		client := s.features.webhookClient
		if client == nil {
			client = newSafeWebhookClient()
		}
		resp, err = client.Do(req)
		if resp != nil {
			record.HTTPStatus = resp.StatusCode
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			if err == nil && (resp.StatusCode < 200 || resp.StatusCode >= 300) {
				err = fmt.Errorf("HTTP %d", resp.StatusCode)
			}
		}
	}
	if err == nil {
		record.Status = "delivered"
	} else {
		record.Error = safeWebhookDeliveryError(err)
	}
	record.Duration = time.Since(started).Milliseconds()
	s.features.recordWebhookDelivery(record)
}

func safeWebhookDeliveryError(err error) string {
	if err == nil {
		return ""
	}
	if strings.Contains(strings.ToLower(err.Error()), "http ") {
		return cleanFeatureText(err.Error(), 32)
	}
	if strings.Contains(strings.ToLower(err.Error()), "private or reserved") {
		return "webhook target was blocked"
	}
	if strings.Contains(strings.ToLower(err.Error()), "timeout") || strings.Contains(strings.ToLower(err.Error()), "deadline exceeded") {
		return "webhook delivery timed out"
	}
	if strings.Contains(strings.ToLower(err.Error()), "tls") || strings.Contains(strings.ToLower(err.Error()), "x509") {
		return "webhook TLS validation failed"
	}
	return "webhook delivery failed"
}

func (h *PlatformFeatureHub) recordWebhookDelivery(record WebhookDelivery) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.data.WebhookDeliveries = append(h.data.WebhookDeliveries, record)
	if len(h.data.WebhookDeliveries) > 500 {
		h.data.WebhookDeliveries = append([]WebhookDelivery(nil), h.data.WebhookDeliveries[len(h.data.WebhookDeliveries)-500:]...)
	}
	_ = h.saveLocked()
}

func platformWebhookSignature(timestamp string, body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func newSafeWebhookClient() *http.Client {
	transport := &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 6 * time.Second,
		IdleConnTimeout:       30 * time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		DialContext:           safeWebhookDialContext,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   8 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("too many redirects")
			}
			return validateNotificationURL(req.URL.String())
		},
	}
}

func safeWebhookDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid webhook address")
	}
	if port == "" {
		port = strconv.Itoa(443)
	}
	var ips []net.IP
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else {
		resolved, resolveErr := net.DefaultResolver.LookupIPAddr(ctx, host)
		if resolveErr != nil {
			return nil, resolveErr
		}
		for _, item := range resolved {
			ips = append(ips, item.IP)
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("webhook host has no address")
	}
	for _, ip := range ips {
		if !publicNotificationIP(ip) {
			return nil, fmt.Errorf("webhook host resolved to a private or reserved IP")
		}
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	var lastErr error
	for _, ip := range ips {
		conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	return nil, lastErr
}

func validateWebhookIntegration(item WebhookIntegration, secret string, current WebhookIntegration) (WebhookIntegration, string, error) {
	item.Name = cleanFeatureText(item.Name, 64)
	item.URL = strings.TrimSpace(item.URL)
	item.Events = allowedWebhookEvents(item.Events)
	if item.Name == "" || len(item.Events) == 0 || validateNotificationURL(item.URL) != nil {
		return WebhookIntegration{}, "", fmt.Errorf("valid HTTPS webhook, name and supported events are required")
	}
	secret = strings.TrimSpace(secret)
	if secret == "" && current.SecretCipher == "" {
		return WebhookIntegration{}, "", fmt.Errorf("webhook signing secret is required")
	}
	if secret != "" && (len(secret) < 16 || len(secret) > 256) {
		return WebhookIntegration{}, "", fmt.Errorf("webhook signing secret must be 16-256 characters")
	}
	return item, secret, nil
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func normalizeUpgradeArtifacts(items []AgentUpgradeArtifact) ([]AgentUpgradeArtifact, error) {
	if len(items) > 16 {
		return nil, fmt.Errorf("too many upgrade artifacts")
	}
	out := make([]AgentUpgradeArtifact, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		item.OS = strings.ToLower(cleanFeatureText(item.OS, 16))
		item.Arch = strings.ToLower(cleanFeatureText(item.Arch, 16))
		item.URL = strings.TrimSpace(item.URL)
		item.SHA256 = strings.ToLower(strings.TrimSpace(item.SHA256))
		key := item.OS + "/" + item.Arch
		if !validAgentPlatform(item.OS, item.Arch) || seen[key] || validateNotificationURL(item.URL) != nil || !validSHA256(item.SHA256) {
			return nil, fmt.Errorf("invalid or duplicate upgrade artifact")
		}
		seen[key] = true
		out = append(out, item)
	}
	return out, nil
}

func findUpgradeArtifact(items []AgentUpgradeArtifact, osName, arch string) (AgentUpgradeArtifact, bool) {
	for _, item := range items {
		if item.OS == osName && item.Arch == arch {
			return item, true
		}
	}
	return AgentUpgradeArtifact{}, false
}

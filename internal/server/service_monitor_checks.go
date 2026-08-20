package server

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const serviceMonitorResponseLimit = 1 << 20

func normalizeServiceMonitor(monitor ServiceMonitor) (ServiceMonitor, error) {
	return normalizeServiceMonitorCore(monitor)
}

func normalizeServiceMonitorCore(monitor ServiceMonitor) (ServiceMonitor, error) {
	monitor.ID = strings.TrimSpace(monitor.ID)
	monitor.OwnerID = strings.TrimSpace(monitor.OwnerID)
	monitor.Name = strings.TrimSpace(monitor.Name)
	monitor.Kind = strings.ToLower(strings.TrimSpace(monitor.Kind))
	monitor.Type = strings.ToLower(strings.TrimSpace(monitor.Type))
	if monitor.Kind == "" {
		monitor.Kind = monitor.Type
		if monitor.Kind == "icmp" {
			monitor.Kind = "ping"
		}
	}
	if monitor.Type == "" {
		monitor.Type = monitor.Kind
		if monitor.Type == "ping" {
			monitor.Type = "icmp"
		}
	}
	monitor.Target = strings.TrimSpace(monitor.Target)
	monitor.ExpectedKeyword = strings.TrimSpace(monitor.ExpectedKeyword)
	if monitor.Name == "" || utf8.RuneCountInString(monitor.Name) > 80 {
		return ServiceMonitor{}, errors.New("name must be 1-80 characters")
	}
	if monitor.ID != "" && !validNodeID(monitor.ID) {
		return ServiceMonitor{}, errors.New("invalid monitor id")
	}
	if monitor.IntervalSeconds == 0 {
		monitor.IntervalSeconds = 60
	}
	if monitor.TimeoutSeconds == 0 {
		monitor.TimeoutSeconds = 5
	}
	if monitor.FailureThreshold == 0 {
		monitor.FailureThreshold = 3
	}
	if monitor.SSLWarnDays == 0 {
		monitor.SSLWarnDays = monitor.TLSWarningDays
	}
	if monitor.SSLWarnDays == 0 {
		monitor.SSLWarnDays = 14
	}
	monitor.TLSWarningDays = monitor.SSLWarnDays
	if monitor.IntervalSeconds < 30 || monitor.IntervalSeconds > 86400 {
		return ServiceMonitor{}, errors.New("interval_seconds must be 30-86400")
	}
	if monitor.TimeoutSeconds < 1 || monitor.TimeoutSeconds > 30 {
		return ServiceMonitor{}, errors.New("timeout_seconds must be 1-30")
	}
	if monitor.FailureThreshold < 1 || monitor.FailureThreshold > 20 {
		return ServiceMonitor{}, errors.New("failure_threshold must be 1-20")
	}
	if monitor.FailureDurationSeconds < 0 || monitor.FailureDurationSeconds > 86400 {
		return ServiceMonitor{}, errors.New("failure_duration_seconds must be 0-86400")
	}
	if monitor.ExpectedStatus != 0 && (monitor.ExpectedStatus < 100 || monitor.ExpectedStatus > 599) {
		return ServiceMonitor{}, errors.New("expected_status must be 100-599")
	}
	if utf8.RuneCountInString(monitor.ExpectedKeyword) > 200 {
		return ServiceMonitor{}, errors.New("expected_keyword must be at most 200 characters")
	}
	if monitor.SSLWarnDays < 1 || monitor.SSLWarnDays > 365 {
		return ServiceMonitor{}, errors.New("ssl_warn_days must be 1-365")
	}
	monitor.NodeID = strings.TrimSpace(monitor.NodeID)
	if monitor.NodeID != "" && !validNodeID(monitor.NodeID) {
		return ServiceMonitor{}, errors.New("invalid node_id")
	}
	monitor.ProbePointIDs = cleanFeatureList(monitor.ProbePointIDs, 32, 80)
	if len(monitor.ProbePointIDs) == 0 {
		monitor.ProbePointIDs = []string{"probe_local"}
	}
	switch monitor.Kind {
	case "http", "https":
		u, err := url.Parse(monitor.Target)
		if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || !strings.EqualFold(u.Scheme, monitor.Kind) {
			return ServiceMonitor{}, fmt.Errorf("target must be a valid %s URL", monitor.Kind)
		}
		if u.Port() != "" {
			port, err := strconv.Atoi(u.Port())
			if err != nil || port < 1 || port > 65535 {
				return ServiceMonitor{}, errors.New("invalid target port")
			}
		}
		if monitor.Port != 0 {
			return ServiceMonitor{}, errors.New("port must be encoded in the HTTP URL")
		}
	case "tcp", "ssl", "ping":
		if err := validateServiceMonitorHost(monitor.Target); err != nil {
			return ServiceMonitor{}, err
		}
		if monitor.Kind == "ping" {
			if monitor.Port != 0 {
				return ServiceMonitor{}, errors.New("ping monitor does not use a port")
			}
		} else {
			if monitor.Port == 0 && monitor.Kind == "ssl" {
				monitor.Port = 443
			}
			if monitor.Port < 1 || monitor.Port > 65535 {
				return ServiceMonitor{}, errors.New("port must be 1-65535")
			}
		}
	default:
		return ServiceMonitor{}, errors.New("kind must be http, https, tcp, ping, or ssl")
	}
	return monitor, nil
}

func validateServiceMonitorHost(host string) error {
	host = strings.TrimSpace(host)
	if host == "" || len(host) > 253 || strings.ContainsAny(host, "/\\?#@\x00\r\n\t ") {
		return errors.New("target must be a hostname or IP address")
	}
	if net.ParseIP(host) != nil {
		return nil
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return errors.New("invalid target hostname")
		}
		for _, ch := range label {
			if ch != '-' && (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') {
				return errors.New("invalid target hostname")
			}
		}
	}
	return nil
}

func resolvePublicServiceTarget(ctx context.Context, host string) (string, error) {
	if ip := net.ParseIP(host); ip != nil {
		if !publicNotificationIP(ip) {
			return "", errors.New("monitor target is a private or reserved IP")
		}
		return ip.String(), nil
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return "", err
	}
	if len(addresses) == 0 {
		return "", errors.New("monitor target did not resolve")
	}
	values := make([]string, 0, len(addresses))
	for _, address := range addresses {
		if !publicNotificationIP(address.IP) {
			return "", errors.New("monitor target resolved to a private or reserved IP")
		}
		values = append(values, address.IP.String())
	}
	sort.Strings(values)
	return values[0], nil
}

func runServiceMonitorCheck(ctx context.Context, monitor ServiceMonitor) ServiceMonitorResult {
	started := time.Now()
	result := ServiceMonitorResult{MonitorID: monitor.ID, OwnerID: monitor.OwnerID, CheckedAt: started.Unix()}
	var err error
	switch monitor.Kind {
	case "http", "https":
		err = checkServiceHTTP(ctx, monitor, &result)
	case "tcp":
		err = checkServiceTCP(ctx, monitor, &result)
	case "ping":
		err = checkServicePing(ctx, monitor, &result)
	case "ssl":
		err = checkServiceTLS(ctx, monitor, &result)
	default:
		err = errors.New("unsupported monitor kind")
	}
	result.LatencyMS = time.Since(started).Milliseconds()
	if result.LatencyMS < 0 {
		result.LatencyMS = 0
	}
	result.Success = err == nil
	if err != nil {
		result.Error = cleanFeatureText(err.Error(), 240)
	}
	return result
}

func checkServiceHTTP(ctx context.Context, monitor ServiceMonitor, result *ServiceMonitorResult) error {
	var resolvedMu sync.Mutex
	resolvedIP := ""
	dialer := &net.Dialer{Timeout: time.Duration(monitor.TimeoutSeconds) * time.Second, KeepAlive: 20 * time.Second}
	transport := &http.Transport{
		Proxy: nil, ForceAttemptHTTP2: true,
		TLSHandshakeTimeout:   time.Duration(monitor.TimeoutSeconds) * time.Second,
		ResponseHeaderTimeout: time.Duration(monitor.TimeoutSeconds) * time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		DialContext: func(dialCtx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ip, err := resolvePublicServiceTarget(dialCtx, host)
			if err != nil {
				return nil, err
			}
			resolvedMu.Lock()
			resolvedIP = ip
			resolvedMu.Unlock()
			return dialer.DialContext(dialCtx, network, net.JoinHostPort(ip, port))
		},
	}
	client := &http.Client{Transport: transport, Timeout: time.Duration(monitor.TimeoutSeconds) * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, monitor.Target, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Monitor-Party-Service-Check/1")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	result.StatusCode = resp.StatusCode
	resolvedMu.Lock()
	result.ResolvedIP = resolvedIP
	resolvedMu.Unlock()
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		captureServiceCertificate(resp.TLS.PeerCertificates[0].Raw, resp.TLS.PeerCertificates[0].NotAfter, result)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, serviceMonitorResponseLimit+1))
	if err != nil {
		return err
	}
	if len(body) > serviceMonitorResponseLimit {
		return errors.New("response body exceeds 1 MiB")
	}
	if monitor.ExpectedStatus != 0 && resp.StatusCode != monitor.ExpectedStatus {
		return fmt.Errorf("expected HTTP %d, received %d", monitor.ExpectedStatus, resp.StatusCode)
	}
	if monitor.ExpectedStatus == 0 && (resp.StatusCode < 200 || resp.StatusCode >= 400) {
		return fmt.Errorf("HTTP status %d", resp.StatusCode)
	}
	if monitor.ExpectedKeyword != "" && !strings.Contains(string(body), monitor.ExpectedKeyword) {
		return errors.New("expected keyword was not found")
	}
	return nil
}

func checkServiceTCP(ctx context.Context, monitor ServiceMonitor, result *ServiceMonitorResult) error {
	ip, err := resolvePublicServiceTarget(ctx, monitor.Target)
	if err != nil {
		return err
	}
	result.ResolvedIP = ip
	conn, err := (&net.Dialer{Timeout: time.Duration(monitor.TimeoutSeconds) * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(monitor.Port)))
	if err != nil {
		return err
	}
	return conn.Close()
}

func checkServicePing(ctx context.Context, monitor ServiceMonitor, result *ServiceMonitorResult) error {
	ip, err := resolvePublicServiceTarget(ctx, monitor.Target)
	if err != nil {
		return err
	}
	result.ResolvedIP = ip
	var command *exec.Cmd
	if runtime.GOOS == "windows" {
		command = exec.CommandContext(ctx, "ping", "-n", "1", "-w", strconv.Itoa(monitor.TimeoutSeconds*1000), ip)
	} else {
		command = exec.CommandContext(ctx, "ping", "-c", "1", "-W", strconv.Itoa(monitor.TimeoutSeconds), ip)
	}
	if output, err := command.CombinedOutput(); err != nil {
		message := cleanFeatureText(string(output), 120)
		if message != "" {
			return fmt.Errorf("ping failed: %s", message)
		}
		return fmt.Errorf("ping failed: %w", err)
	}
	return nil
}

func checkServiceTLS(ctx context.Context, monitor ServiceMonitor, result *ServiceMonitorResult) error {
	ip, err := resolvePublicServiceTarget(ctx, monitor.Target)
	if err != nil {
		return err
	}
	result.ResolvedIP = ip
	raw, err := (&net.Dialer{Timeout: time.Duration(monitor.TimeoutSeconds) * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(monitor.Port)))
	if err != nil {
		return err
	}
	defer raw.Close()
	conn := tls.Client(raw, &tls.Config{ServerName: monitor.Target, MinVersion: tls.VersionTLS12})
	if err := conn.HandshakeContext(ctx); err != nil {
		return err
	}
	certificates := conn.ConnectionState().PeerCertificates
	if len(certificates) == 0 {
		return errors.New("TLS peer did not provide a certificate")
	}
	captureServiceCertificate(certificates[0].Raw, certificates[0].NotAfter, result)
	return nil
}

func captureServiceCertificate(raw []byte, notAfter time.Time, result *ServiceMonitorResult) {
	sum := sha256.Sum256(raw)
	result.TLSFingerprint = strings.ToUpper(hex.EncodeToString(sum[:]))
	result.TLSNotAfter = notAfter.Unix()
}

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"vps-agent/internal/config"
)

const probeResponseLimit = 1 << 20

type probePointAssignment struct {
	ID string `json:"id"`
}

type probeMonitorAssignment struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Kind            string   `json:"kind"`
	Target          string   `json:"target"`
	Port            int      `json:"port,omitempty"`
	IntervalSeconds int      `json:"interval_seconds"`
	TimeoutSeconds  int      `json:"timeout_seconds"`
	ExpectedStatus  int      `json:"expected_status,omitempty"`
	ExpectedKeyword string   `json:"expected_keyword,omitempty"`
	ProbePointIDs   []string `json:"probe_point_ids,omitempty"`
}

type probeAssignments struct {
	ProbePoints []probePointAssignment   `json:"probe_points"`
	Monitors    []probeMonitorAssignment `json:"monitors"`
}

type probeResult struct {
	MonitorID      string `json:"monitor_id"`
	Success        bool   `json:"success"`
	LatencyMS      int64  `json:"latency_ms"`
	StatusCode     int    `json:"status_code,omitempty"`
	Error          string `json:"error,omitempty"`
	ResolvedIP     string `json:"resolved_ip,omitempty"`
	TLSFingerprint string `json:"tls_fingerprint,omitempty"`
	TLSNotAfter    int64  `json:"tls_not_after,omitempty"`
}

type probeResultEnvelope struct {
	ProbePointID string        `json:"probe_point_id"`
	Results      []probeResult `json:"results"`
}

func runProbeLoop(ctx context.Context, cfg config.Config) {
	client := &http.Client{Timeout: 12 * time.Second}
	nextRun := map[string]time.Time{}
	run := func() {
		assignments, err := fetchProbeAssignments(ctx, client, cfg)
		if err != nil {
			log.Printf("probe assignment fetch failed: %v", err)
			return
		}
		now := time.Now()
		for _, point := range assignments.ProbePoints {
			results := runProbePointTasks(ctx, point.ID, assignments.Monitors, nextRun, now)
			if len(results) == 0 {
				continue
			}
			if err := postProbeResults(ctx, client, cfg, point.ID, results); err != nil {
				log.Printf("probe result upload failed point=%s: %v", point.ID, err)
			}
		}
	}
	run()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

func fetchProbeAssignments(ctx context.Context, client *http.Client, cfg config.Config) (probeAssignments, error) {
	var out probeAssignments
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(cfg.Server, "/")+"/api/agent/probe-assignments", nil)
	if err != nil {
		return out, err
	}
	setProbeAuth(req, cfg)
	resp, err := client.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return out, probeControlError(resp)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, probeResponseLimit)).Decode(&out); err != nil {
		return out, err
	}
	return out, nil
}

func runProbePointTasks(ctx context.Context, pointID string, monitors []probeMonitorAssignment, nextRun map[string]time.Time, now time.Time) []probeResult {
	type indexedResult struct {
		index  int
		result probeResult
	}
	jobs := make([]probeMonitorAssignment, 0)
	for _, monitor := range monitors {
		if !containsString(monitor.ProbePointIDs, pointID) {
			continue
		}
		key := pointID + ":" + monitor.ID
		if due := nextRun[key]; !due.IsZero() && now.Before(due) {
			continue
		}
		interval := time.Duration(monitor.IntervalSeconds) * time.Second
		if interval < 30*time.Second {
			interval = 30 * time.Second
		}
		nextRun[key] = now.Add(interval)
		jobs = append(jobs, monitor)
	}
	if len(jobs) == 0 {
		return nil
	}
	sem := make(chan struct{}, 4)
	completed := make(chan indexedResult, len(jobs))
	var wg sync.WaitGroup
	for index, monitor := range jobs {
		wg.Add(1)
		go func(index int, monitor probeMonitorAssignment) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			timeout := time.Duration(monitor.TimeoutSeconds) * time.Second
			if timeout < time.Second || timeout > 30*time.Second {
				timeout = 5 * time.Second
			}
			checkCtx, cancel := context.WithTimeout(ctx, timeout)
			result := executeProbe(checkCtx, monitor)
			cancel()
			completed <- indexedResult{index: index, result: result}
		}(index, monitor)
	}
	wg.Wait()
	close(completed)
	ordered := make([]probeResult, len(jobs))
	for item := range completed {
		ordered[item.index] = item.result
	}
	return ordered
}

func executeProbe(ctx context.Context, monitor probeMonitorAssignment) probeResult {
	started := time.Now()
	result := probeResult{MonitorID: monitor.ID}
	var err error
	switch strings.ToLower(monitor.Kind) {
	case "http", "https":
		err = executeHTTPProbe(ctx, monitor, &result)
	case "tcp":
		err = executeTCPProbe(ctx, monitor, &result)
	case "ping", "icmp":
		err = executePingProbe(ctx, monitor, &result)
	case "ssl":
		err = executeTLSProbe(ctx, monitor, &result)
	default:
		err = errors.New("unsupported probe kind")
	}
	result.LatencyMS = time.Since(started).Milliseconds()
	result.Success = err == nil
	if err != nil {
		result.Error = truncateProbeError(err.Error(), 240)
	}
	return result
}

func executeHTTPProbe(ctx context.Context, monitor probeMonitorAssignment, result *probeResult) error {
	u, err := url.Parse(monitor.Target)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return errors.New("invalid HTTP target")
	}
	dialer := &net.Dialer{}
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, DialContext: func(dialCtx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ip, err := resolvePublicProbeTarget(dialCtx, host)
		if err != nil {
			return nil, err
		}
		result.ResolvedIP = ip
		return dialer.DialContext(dialCtx, network, net.JoinHostPort(ip, port))
	}}
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, monitor.Target, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Monitor-Party-Agent-Probe/1")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	result.StatusCode = resp.StatusCode
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		captureProbeCertificate(resp.TLS.PeerCertificates[0].Raw, resp.TLS.PeerCertificates[0].NotAfter, result)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, probeResponseLimit+1))
	if err != nil {
		return err
	}
	if len(body) > probeResponseLimit {
		return errors.New("response body exceeds 1 MiB")
	}
	if monitor.ExpectedStatus != 0 && resp.StatusCode != monitor.ExpectedStatus {
		return fmt.Errorf("expected HTTP %d, received %d", monitor.ExpectedStatus, resp.StatusCode)
	}
	if monitor.ExpectedStatus == 0 && (resp.StatusCode < 200 || resp.StatusCode >= 400) {
		return fmt.Errorf("HTTP status %d", resp.StatusCode)
	}
	if monitor.ExpectedKeyword != "" && !bytes.Contains(body, []byte(monitor.ExpectedKeyword)) {
		return errors.New("expected keyword was not found")
	}
	return nil
}

func executeTCPProbe(ctx context.Context, monitor probeMonitorAssignment, result *probeResult) error {
	ip, err := resolvePublicProbeTarget(ctx, monitor.Target)
	if err != nil {
		return err
	}
	if monitor.Port < 1 || monitor.Port > 65535 {
		return errors.New("invalid TCP port")
	}
	result.ResolvedIP = ip
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(monitor.Port)))
	if err != nil {
		return err
	}
	return conn.Close()
}

func executePingProbe(ctx context.Context, monitor probeMonitorAssignment, result *probeResult) error {
	ip, err := resolvePublicProbeTarget(ctx, monitor.Target)
	if err != nil {
		return err
	}
	result.ResolvedIP = ip
	var command *exec.Cmd
	if runtime.GOOS == "windows" {
		command = exec.CommandContext(ctx, "ping", "-n", "1", ip)
	} else {
		command = exec.CommandContext(ctx, "ping", "-c", "1", ip)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ping failed: %s", truncateProbeError(string(output), 120))
	}
	return nil
}

func executeTLSProbe(ctx context.Context, monitor probeMonitorAssignment, result *probeResult) error {
	ip, err := resolvePublicProbeTarget(ctx, monitor.Target)
	if err != nil {
		return err
	}
	port := monitor.Port
	if port == 0 {
		port = 443
	}
	result.ResolvedIP = ip
	raw, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(port)))
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
	captureProbeCertificate(certificates[0].Raw, certificates[0].NotAfter, result)
	return nil
}

func resolvePublicProbeTarget(ctx context.Context, host string) (string, error) {
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, strings.TrimSpace(host))
	if err != nil {
		return "", err
	}
	if len(addresses) == 0 {
		return "", errors.New("target did not resolve")
	}
	values := make([]string, 0, len(addresses))
	for _, address := range addresses {
		if !isPublicProbeIP(address.IP) {
			return "", errors.New("target resolved to a private or reserved IP")
		}
		values = append(values, address.IP.String())
	}
	sort.Strings(values)
	return values[0], nil
}

func isPublicProbeIP(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsUnspecified() {
		return false
	}
	for _, raw := range []string{"100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "2001:db8::/32"} {
		prefix := netip.MustParsePrefix(raw)
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func captureProbeCertificate(raw []byte, notAfter time.Time, result *probeResult) {
	sum := sha256.Sum256(raw)
	result.TLSFingerprint = strings.ToUpper(hex.EncodeToString(sum[:]))
	result.TLSNotAfter = notAfter.Unix()
}

func postProbeResults(ctx context.Context, client *http.Client, cfg config.Config, pointID string, results []probeResult) error {
	body, err := json.Marshal(probeResultEnvelope{ProbePointID: pointID, Results: results})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.Server, "/")+"/api/agent/probe-results", bytes.NewReader(body))
	if err != nil {
		return err
	}
	setProbeAuth(req, cfg)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return probeControlError(resp)
	}
	return nil
}

func setProbeAuth(req *http.Request, cfg config.Config) {
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("X-Node-ID", cfg.NodeID)
}

func probeControlError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("server returned %s: %s", resp.Status, truncateProbeError(string(body), 240))
}

func truncateProbeError(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) > limit {
		value = value[:limit]
	}
	return value
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

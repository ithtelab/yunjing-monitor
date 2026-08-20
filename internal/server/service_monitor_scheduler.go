package server

import (
	"context"
	"fmt"
	"sync"
	"time"

	serverapp "vps-agent/internal/server/application"
)

type serviceMonitorAlert struct {
	Kind    string
	Status  string
	Message string
}

type ServiceMonitorService struct {
	store    serverapp.Store
	alerts   *AlertService
	runMu    sync.Mutex
	running  bool
	resultMu sync.Mutex
	check    func(context.Context, ServiceMonitor) ServiceMonitorResult
	now      func() time.Time
}

func NewServiceMonitorService(store serverapp.Store, alerts *AlertService) *ServiceMonitorService {
	return &ServiceMonitorService{store: store, alerts: alerts, check: runServiceMonitorCheck, now: time.Now}
}

func (s *ServiceMonitorService) RunDue(ctx context.Context) {
	s.runMu.Lock()
	if s.running {
		s.runMu.Unlock()
		return
	}
	s.running = true
	s.runMu.Unlock()
	defer func() {
		s.runMu.Lock()
		s.running = false
		s.runMu.Unlock()
	}()
	now := s.now()
	semaphore := make(chan struct{}, 4)
	var checks sync.WaitGroup
	for _, monitor := range s.store.ListServiceMonitors("", true) {
		if ctx.Err() != nil {
			return
		}
		if !monitor.Enabled || monitor.NextRunAt > now.Unix() {
			continue
		}
		if len(monitor.ProbePointIDs) > 0 && !listContains(monitor.ProbePointIDs, "probe_local") {
			continue
		}
		checks.Add(1)
		go func(monitor ServiceMonitor) {
			defer checks.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				return
			}
			checkCtx, cancel := context.WithTimeout(ctx, time.Duration(monitor.TimeoutSeconds+2)*time.Second)
			result := s.check(checkCtx, monitor)
			cancel()
			result.ProbePointID = "probe_local"
			_ = s.RecordResult(monitor, result)
		}(monitor)
	}
	checks.Wait()
}

func (s *ServiceMonitorService) RecordResult(monitor ServiceMonitor, result ServiceMonitorResult) error {
	s.resultMu.Lock()
	defer s.resultMu.Unlock()
	now := s.now()
	if result.ID == "" {
		if id, err := newFeatureID("check"); err == nil {
			result.ID = id
		} else {
			result.ID = fmt.Sprintf("check_%d_%s", now.UnixNano(), monitor.ID)
		}
	}
	if result.CheckedAt == 0 {
		result.CheckedAt = now.Unix()
	}
	result.MonitorID = monitor.ID
	result.OwnerID = monitor.OwnerID
	state, _ := s.store.GetServiceMonitorState(monitor.ID)
	state.MonitorID = monitor.ID
	previousStatus := state.Status
	state, events := applyServiceMonitorResult(monitor, state, result)
	state.LastCheckAt = result.CheckedAt
	state.LastLatencyMS = result.LatencyMS
	state.LastError = result.Error
	state.CertificateExpiresAt = result.TLSNotAfter
	if state.Status != previousStatus {
		state.LastChangedAt = result.CheckedAt
	}
	monitor.LastRunAt = result.CheckedAt
	monitor.NextRunAt = result.CheckedAt + int64(monitor.IntervalSeconds)
	monitor.UpdatedAt = result.CheckedAt
	monitor.State = state
	if err := s.store.SaveServiceMonitorCheck(monitor, state, result); err != nil {
		return err
	}
	for _, event := range events {
		s.recordAlert(monitor, event, result.CheckedAt)
	}
	return nil
}

func applyServiceMonitorResult(monitor ServiceMonitor, state ServiceMonitorState, result ServiceMonitorResult) (ServiceMonitorState, []serviceMonitorAlert) {
	now := result.CheckedAt
	if now == 0 {
		now = time.Now().Unix()
	}
	events := []serviceMonitorAlert{}
	if result.Success {
		if state.Status == "down" {
			events = append(events, serviceMonitorAlert{Kind: "service_recovery", Status: "resolved", Message: monitor.Name + " 已恢复正常"})
		}
		state.Status = "up"
		state.ConsecutiveFailures = 0
		state.FirstFailureAt = 0
		state.LastSuccessAt = now
	} else {
		state.ConsecutiveFailures++
		if state.FirstFailureAt == 0 {
			state.FirstFailureAt = now
		}
		state.LastFailureAt = now
		thresholdReached := state.ConsecutiveFailures >= monitor.FailureThreshold
		durationReached := now-state.FirstFailureAt >= int64(monitor.FailureDurationSeconds)
		if state.Status != "down" && thresholdReached && durationReached {
			state.Status = "down"
			state.LastAlertAt = now
			message := fmt.Sprintf("%s 检测失败：%s", monitor.Name, result.Error)
			events = append(events, serviceMonitorAlert{Kind: "service_down", Status: "firing", Message: message})
		}
	}
	if result.ResolvedIP != "" {
		if state.LastIP != "" && state.LastIP != result.ResolvedIP {
			events = append(events, serviceMonitorAlert{Kind: "service_ip_changed", Status: "firing", Message: fmt.Sprintf("%s 解析地址已从 %s 变更为 %s", monitor.Name, state.LastIP, result.ResolvedIP)})
		}
		state.LastIP = result.ResolvedIP
	}
	if result.TLSFingerprint != "" {
		if state.LastTLSFingerprint != "" && state.LastTLSFingerprint != result.TLSFingerprint {
			state.LastCertificateChange = now
			events = append(events, serviceMonitorAlert{Kind: "service_certificate_changed", Status: "firing", Message: monitor.Name + " 的 TLS 证书指纹发生变化"})
		}
		state.LastTLSFingerprint = result.TLSFingerprint
		state.LastTLSNotAfter = result.TLSNotAfter
		if result.TLSNotAfter > 0 && result.TLSNotAfter-now <= int64(monitor.SSLWarnDays)*86400 && state.TLSExpiryAlertedFor != result.TLSNotAfter {
			state.TLSExpiryAlertedFor = result.TLSNotAfter
			days := (result.TLSNotAfter - now + 86399) / 86400
			events = append(events, serviceMonitorAlert{Kind: "service_certificate_expiry", Status: "firing", Message: fmt.Sprintf("%s 的 TLS 证书将在 %d 天内到期", monitor.Name, days)})
		}
	}
	return state, events
}

func (s *ServiceMonitorService) recordAlert(monitor ServiceMonitor, alert serviceMonitorAlert, now int64) {
	if s.alerts == nil {
		return
	}
	event := AlertEvent{
		ID:      fmt.Sprintf("%d-%s-%s-%s", now, monitor.ID, alert.Kind, alert.Status),
		OwnerID: monitor.OwnerID, NodeID: monitor.ID, NodeName: monitor.Name,
		Kind: alert.Kind, Status: alert.Status, Message: alert.Message, CreatedAt: now,
	}
	s.alerts.RecordExternal(event, true)
}

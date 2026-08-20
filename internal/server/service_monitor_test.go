package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func sampleServiceMonitor(id, ownerID string) ServiceMonitor {
	return ServiceMonitor{
		ID: id, OwnerID: ownerID, Name: "Example API", Kind: "https", Target: "https://example.com/health",
		Enabled: true, IntervalSeconds: 60, TimeoutSeconds: 5, FailureThreshold: 2,
		FailureDurationSeconds: 30, SSLWarnDays: 14, CreatedAt: 100, UpdatedAt: 100, NextRunAt: 100,
	}
}

func TestServiceMonitorJSONPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	monitor := sampleServiceMonitor("svc_json", "owner-a")
	state := ServiceMonitorState{MonitorID: monitor.ID, Status: "up", LastSuccessAt: 120}
	result := ServiceMonitorResult{ID: "check_json", MonitorID: monitor.ID, OwnerID: monitor.OwnerID, Success: true, CheckedAt: 120}
	if err := store.SaveServiceMonitorCheck(monitor, state, result); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := reopened.GetServiceMonitor(monitor.ID); !ok || got.OwnerID != "owner-a" {
		t.Fatalf("monitor after reopen = %#v, %v", got, ok)
	}
	if got, ok := reopened.GetServiceMonitorState(monitor.ID); !ok || got.Status != "up" {
		t.Fatalf("state after reopen = %#v, %v", got, ok)
	}
	if got := reopened.ListServiceMonitorResults(monitor.ID, 10); len(got) != 1 || got[0].ID != result.ID {
		t.Fatalf("results after reopen = %#v", got)
	}
}

func TestServiceMonitorSQLitePersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.db")
	store, err := NewSQLiteStore(path, "")
	if err != nil {
		t.Fatal(err)
	}
	monitor := sampleServiceMonitor("svc_sqlite", "owner-b")
	state := ServiceMonitorState{MonitorID: monitor.ID, Status: "down", ConsecutiveFailures: 3, FirstFailureAt: 100}
	result := ServiceMonitorResult{ID: "check_sqlite", MonitorID: monitor.ID, OwnerID: monitor.OwnerID, Error: "timeout", CheckedAt: 140}
	if err := store.SaveServiceMonitorCheck(monitor, state, result); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewSQLiteStore(path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got, ok := reopened.GetServiceMonitorState(monitor.ID); !ok || got.Status != "down" {
		t.Fatalf("state after reopen = %#v, %v", got, ok)
	}
	if got := reopened.ListServiceMonitorResults(monitor.ID, 10); len(got) != 1 || got[0].Error != "timeout" {
		t.Fatalf("results after reopen = %#v", got)
	}
}

func TestServiceMonitorJSONToSQLiteMigration(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "server.json")
	jsonStore, err := NewStore(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	monitor := sampleServiceMonitor("svc_migrate", "owner-c")
	state := ServiceMonitorState{MonitorID: monitor.ID, Status: "up", LastSuccessAt: 200}
	result := ServiceMonitorResult{ID: "check_migrate", MonitorID: monitor.ID, OwnerID: monitor.OwnerID, Success: true, CheckedAt: 200}
	if err := jsonStore.SaveServiceMonitorCheck(monitor, state, result); err != nil {
		t.Fatal(err)
	}
	sqliteStore, err := NewSQLiteStore(filepath.Join(dir, "server.db"), jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	defer sqliteStore.Close()
	if got, ok := sqliteStore.GetServiceMonitor(monitor.ID); !ok || got.OwnerID != monitor.OwnerID {
		t.Fatalf("migrated monitor = %#v, %v", got, ok)
	}
	if got, ok := sqliteStore.GetServiceMonitorState(monitor.ID); !ok || got.Status != "up" {
		t.Fatalf("migrated state = %#v, %v", got, ok)
	}
	if got := sqliteStore.ListServiceMonitorResults(monitor.ID, 10); len(got) != 1 || got[0].ID != result.ID {
		t.Fatalf("migrated results = %#v", got)
	}
}

func TestServiceMonitorFailureDurationRecoveryAndOneShotEvents(t *testing.T) {
	monitor := sampleServiceMonitor("svc_state", "owner-a")
	state := ServiceMonitorState{MonitorID: monitor.ID, Status: "up"}
	state, events := applyServiceMonitorResult(monitor, state, ServiceMonitorResult{CheckedAt: 100, Error: "timeout", ResolvedIP: "203.0.113.10"})
	if len(events) != 0 || state.Status != "up" || state.ConsecutiveFailures != 1 {
		t.Fatalf("first failure state=%#v events=%#v", state, events)
	}
	state, events = applyServiceMonitorResult(monitor, state, ServiceMonitorResult{CheckedAt: 120, Error: "timeout", ResolvedIP: "203.0.113.10"})
	if len(events) != 0 || state.Status == "down" {
		t.Fatalf("duration threshold bypassed: state=%#v events=%#v", state, events)
	}
	state, events = applyServiceMonitorResult(monitor, state, ServiceMonitorResult{CheckedAt: 130, Error: "timeout", ResolvedIP: "203.0.113.10"})
	if state.Status != "down" || len(events) != 1 || events[0].Kind != "service_down" {
		t.Fatalf("down transition state=%#v events=%#v", state, events)
	}
	state, events = applyServiceMonitorResult(monitor, state, ServiceMonitorResult{CheckedAt: 140, Error: "timeout", ResolvedIP: "203.0.113.10"})
	if len(events) != 0 {
		t.Fatalf("repeated down emitted events=%#v", events)
	}
	state, events = applyServiceMonitorResult(monitor, state, ServiceMonitorResult{CheckedAt: 150, Success: true, ResolvedIP: "203.0.113.10"})
	if state.Status != "up" || len(events) != 1 || events[0].Kind != "service_recovery" {
		t.Fatalf("recovery state=%#v events=%#v", state, events)
	}
}

func TestServiceMonitorIPCertificateAndExpiryEventsAreDeduplicated(t *testing.T) {
	monitor := sampleServiceMonitor("svc_tls", "owner-a")
	state := ServiceMonitorState{MonitorID: monitor.ID}
	baseline := ServiceMonitorResult{Success: true, CheckedAt: 100, ResolvedIP: "203.0.113.10", TLSFingerprint: "OLD", TLSNotAfter: 100 + 10*86400}
	state, events := applyServiceMonitorResult(monitor, state, baseline)
	if len(events) != 1 || events[0].Kind != "service_certificate_expiry" {
		t.Fatalf("baseline events=%#v", events)
	}
	changed := ServiceMonitorResult{Success: true, CheckedAt: 101, ResolvedIP: "203.0.113.11", TLSFingerprint: "NEW", TLSNotAfter: baseline.TLSNotAfter}
	state, events = applyServiceMonitorResult(monitor, state, changed)
	if len(events) != 2 || events[0].Kind != "service_ip_changed" || events[1].Kind != "service_certificate_changed" {
		t.Fatalf("change events=%#v", events)
	}
	_, events = applyServiceMonitorResult(monitor, state, changed)
	if len(events) != 0 {
		t.Fatalf("duplicate change/expiry events=%#v", events)
	}
}

func TestServiceMonitorAccountOwnershipIsolationAndAdminVisibility(t *testing.T) {
	s := newTestServer(t)
	hash, err := hashOwnerPassword("password123")
	if err != nil {
		t.Fatal(err)
	}
	ownerA, err := s.store.CreateOwner("service-a@example.com", hash)
	if err != nil {
		t.Fatal(err)
	}
	ownerB, err := s.store.CreateOwner("service-b@example.com", hash)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.SaveServiceMonitor(sampleServiceMonitor("svc_owned", ownerA.ID)); err != nil {
		t.Fatal(err)
	}
	tokenB, err := s.sessions.CreateFor(SessionKindOwner, ownerB.ID)
	if err != nil {
		t.Fatal(err)
	}
	cookieB := &http.Cookie{Name: "monitor_owner", Value: tokenB}
	resultsReq := httptest.NewRequest(http.MethodGet, "https://monitor.example.com/api/account/service-monitors/results?monitor_id=svc_owned", nil)
	resultsReq.AddCookie(cookieB)
	resultsResp := httptest.NewRecorder()
	s.handleAccountServiceMonitorResults(resultsResp, resultsReq)
	if resultsResp.Code != http.StatusNotFound {
		t.Fatalf("cross-owner results status=%d body=%s", resultsResp.Code, resultsResp.Body.String())
	}
	deleteReq := httptest.NewRequest(http.MethodDelete, "https://monitor.example.com/api/account/service-monitors?id=svc_owned", nil)
	deleteReq.AddCookie(cookieB)
	deleteResp := httptest.NewRecorder()
	s.handleAccountServiceMonitors(deleteResp, deleteReq)
	if deleteResp.Code != http.StatusNotFound {
		t.Fatalf("cross-owner delete status=%d body=%s", deleteResp.Code, deleteResp.Body.String())
	}
	adminToken, err := s.sessions.Create()
	if err != nil {
		t.Fatal(err)
	}
	adminReq := authedAdminRequest(http.MethodGet, "https://monitor.example.com/api/admin/service-monitors", adminToken)
	adminResp := httptest.NewRecorder()
	s.handleAdminServiceMonitors(adminResp, adminReq)
	if adminResp.Code != http.StatusOK || !strings.Contains(adminResp.Body.String(), "svc_owned") {
		t.Fatalf("admin list status=%d body=%s", adminResp.Code, adminResp.Body.String())
	}
}

func TestServiceMonitorValidationAndPrivateTargetRejection(t *testing.T) {
	monitor := sampleServiceMonitor("svc_valid", "owner-a")
	monitor.Kind, monitor.Target, monitor.Port = "tcp", "127.0.0.1", 443
	if _, err := normalizeServiceMonitorCore(monitor); err != nil {
		t.Fatalf("syntax validation should not require DNS: %v", err)
	}
	if _, err := resolvePublicServiceTarget(context.Background(), monitor.Target); err == nil {
		t.Fatal("expected private target rejection")
	}
	monitor.Kind = "smtp"
	if _, err := normalizeServiceMonitorCore(monitor); err == nil {
		t.Fatal("expected unsupported kind rejection")
	}
}

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vps-agent/internal/agent"
)

func TestAgentProbeAssignmentsAndResultsAreBoundToAgent(t *testing.T) {
	s, store := newPlatformTestServer(t)
	s.serviceMonitors = NewServiceMonitorService(store, nil)
	const probeNode, otherNode = "probe-node", "other-node"
	const probeToken, otherToken = "probe-token", "other-token"
	if err := store.SetNodeToken(probeNode, probeToken, hashToken(probeToken), 20); err != nil {
		t.Fatal(err)
	}
	if err := store.SetNodeToken(otherNode, otherToken, hashToken(otherToken), 20); err != nil {
		t.Fatal(err)
	}
	s.features.mu.Lock()
	s.features.data.ProbePoints = append(s.features.data.ProbePoints, ProbePoint{ID: "probe_remote", Name: "Remote", Region: "test", Mode: "agent", NodeID: probeNode, Enabled: true})
	s.features.mu.Unlock()
	monitor := sampleServiceMonitor("svc_remote", "")
	monitor.ProbePointIDs = []string{"probe_remote"}
	if err := store.SaveServiceMonitor(monitor); err != nil {
		t.Fatal(err)
	}

	assigned := httptest.NewRequest(http.MethodGet, "/api/agent/probe-assignments", nil)
	assigned.Header.Set("X-Node-ID", probeNode)
	assigned.Header.Set("Authorization", "Bearer "+probeToken)
	assignedResp := httptest.NewRecorder()
	s.handleAgentProbeAssignments(assignedResp, assigned)
	if assignedResp.Code != http.StatusOK || !strings.Contains(assignedResp.Body.String(), monitor.ID) {
		t.Fatalf("assigned response=%d %s", assignedResp.Code, assignedResp.Body.String())
	}

	unassigned := httptest.NewRequest(http.MethodGet, "/api/agent/probe-assignments", nil)
	unassigned.Header.Set("X-Node-ID", otherNode)
	unassigned.Header.Set("Authorization", "Bearer "+otherToken)
	unassignedResp := httptest.NewRecorder()
	s.handleAgentProbeAssignments(unassignedResp, unassigned)
	if unassignedResp.Code != http.StatusOK || strings.Contains(unassignedResp.Body.String(), monitor.ID) {
		t.Fatalf("unassigned response=%d %s", unassignedResp.Code, unassignedResp.Body.String())
	}

	body := `{"probe_point_id":"probe_remote","results":[{"monitor_id":"svc_remote","success":true,"latency_ms":25,"status_code":200}]}`
	resultReq := httptest.NewRequest(http.MethodPost, "/api/agent/probe-results", strings.NewReader(body))
	resultReq.Header.Set("X-Node-ID", probeNode)
	resultReq.Header.Set("Authorization", "Bearer "+probeToken)
	resultResp := httptest.NewRecorder()
	s.handleAgentProbeResults(resultResp, resultReq)
	if resultResp.Code != http.StatusOK || len(store.ListServiceMonitorResults(monitor.ID, 10)) != 1 {
		t.Fatalf("result response=%d %s", resultResp.Code, resultResp.Body.String())
	}

	forbiddenReq := httptest.NewRequest(http.MethodPost, "/api/agent/probe-results", strings.NewReader(body))
	forbiddenReq.Header.Set("X-Node-ID", otherNode)
	forbiddenReq.Header.Set("Authorization", "Bearer "+otherToken)
	forbiddenResp := httptest.NewRecorder()
	s.handleAgentProbeResults(forbiddenResp, forbiddenReq)
	if forbiddenResp.Code != http.StatusForbidden {
		t.Fatalf("unassigned result status=%d body=%s", forbiddenResp.Code, forbiddenResp.Body.String())
	}
}

func TestOwnerNodeGroupRejectsAnotherOwnersNode(t *testing.T) {
	s, store := newPlatformTestServer(t)
	hash, _ := hashOwnerPassword("password123")
	ownerA, _ := store.CreateOwner("group-a@example.com", hash)
	ownerB, _ := store.CreateOwner("group-b@example.com", hash)
	if err := store.CreateOwnerNode(ownerA.ID, PlannedNode{NodeID: "group-node-a", Private: true}, HostInfo{}, "token-a", accountAudit(ownerA.ID, "node.create", "group-node-a"), 20, 5); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateOwnerNode(ownerB.ID, PlannedNode{NodeID: "group-node-b", Private: true}, HostInfo{}, "token-b", accountAudit(ownerB.ID, "node.create", "group-node-b"), 20, 5); err != nil {
		t.Fatal(err)
	}
	session, _ := s.sessions.CreateFor(SessionKindOwner, ownerA.ID)
	req := accountRequest(http.MethodPost, "https://monitor.example.com/api/account/node-groups", map[string]any{"group": map[string]any{"name": "bad", "node_ids": []string{"group-node-b"}}}, &http.Cookie{Name: "monitor_owner", Value: session})
	resp := httptest.NewRecorder()
	s.handleAccountNodeGroups(resp, req)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("cross-owner group status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestAgentIPBaselineAndTrafficThresholdTransitions(t *testing.T) {
	s, store := newPlatformTestServer(t)
	const nodeID = "event-node"
	if err := store.SetNodeToken(nodeID, "event-token", hashToken("event-token"), 20); err != nil {
		t.Fatal(err)
	}
	s.recordAgentIP(nodeID, "8.8.8.8")
	if got := len(s.features.Snapshot().NodeOperationalEvents); got != 0 {
		t.Fatalf("baseline created %d events", got)
	}
	s.recordAgentIP(nodeID, "1.1.1.1")
	if got := s.features.Snapshot().NodeOperationalEvents; len(got) != 1 || got[0].Kind != "ip.changed" {
		t.Fatalf("IP events=%#v", got)
	}

	store.Reports[nodeID] = agent.Metrics{NodeID: nodeID, Timestamp: time.Now().Unix()}
	store.Traffic[nodeID] = TrafficStat{RxTotal: 60, TxTotal: 0}
	s.features.mu.Lock()
	s.features.data.TrafficAlertPolicies[nodeID] = TrafficAlertPolicy{NodeID: nodeID, Enabled: true, QuotaBytes: 100, ThresholdPct: 50}
	s.features.mu.Unlock()
	s.evaluateTrafficPolicies()
	if !s.features.Snapshot().TrafficAlertPolicies[nodeID].AlertActive {
		t.Fatal("traffic alert did not become active")
	}
	count := len(s.features.Snapshot().NodeOperationalEvents)
	s.evaluateTrafficPolicies()
	if len(s.features.Snapshot().NodeOperationalEvents) != count {
		t.Fatal("active traffic threshold emitted duplicate event")
	}
	store.Traffic[nodeID] = TrafficStat{RxTotal: 10}
	s.evaluateTrafficPolicies()
	snapshot := s.features.Snapshot()
	if snapshot.TrafficAlertPolicies[nodeID].AlertActive || len(snapshot.NodeOperationalEvents) != count+1 || snapshot.NodeOperationalEvents[len(snapshot.NodeOperationalEvents)-1].Status != "recovered" {
		t.Fatalf("traffic recovery snapshot=%#v", snapshot.TrafficAlertPolicies[nodeID])
	}
}

func TestPublicStatusServiceDoesNotExposeTarget(t *testing.T) {
	s, store := newPlatformTestServer(t)
	monitor := sampleServiceMonitor("svc_public", "owner-secret")
	monitor.Target = "https://secret.example.com/internal-health"
	result := ServiceMonitorResult{ID: "check_public", MonitorID: monitor.ID, Success: true, LatencyMS: 20, CheckedAt: time.Now().Unix()}
	state := ServiceMonitorState{MonitorID: monitor.ID, Status: "up", LastSuccessAt: result.CheckedAt}
	if err := store.SaveServiceMonitorCheck(monitor, state, result); err != nil {
		t.Fatal(err)
	}
	s.features.mu.Lock()
	s.features.data.StatusPages = []PublicStatusPage{{ID: "status_public", Slug: "public-services", Name: "Services", Enabled: true, ServiceIDs: []string{monitor.ID}}}
	s.features.mu.Unlock()
	req := httptest.NewRequest(http.MethodGet, "/api/status/public-services", nil)
	resp := httptest.NewRecorder()
	s.handlePublicStatus(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	bodyText := resp.Body.String()
	if strings.Contains(bodyText, monitor.Target) || strings.Contains(bodyText, monitor.OwnerID) || !strings.Contains(bodyText, monitor.Name) {
		t.Fatalf("public status leaked or omitted fields: %s", bodyText)
	}
}

package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const agentUpgradeManifestTTL = 10 * time.Minute

type AgentUpgradeManifest struct {
	RolloutID          string `json:"rollout_id"`
	NodeID             string `json:"node_id"`
	Action             string `json:"action"`
	Version            string `json:"version"`
	OS                 string `json:"os"`
	Arch               string `json:"arch"`
	URL                string `json:"url"`
	SHA256             string `json:"sha256"`
	IssuedAt           int64  `json:"issued_at"`
	ExpiresAt          int64  `json:"expires_at"`
	SignatureAlgorithm string `json:"signature_algorithm"`
	Signature          string `json:"signature"`
}

type agentUpgradeResultRequest struct {
	RolloutID string `json:"rollout_id"`
	Action    string `json:"action"`
	Status    string `json:"status"`
	Version   string `json:"version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	SHA256    string `json:"sha256,omitempty"`
	Error     string `json:"error,omitempty"`
}

type AgentUpgradeCompletion struct {
	RolloutID string
	Action    string
	Version   string
}

func (s *Server) handleAgentUpgradeManifest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if s.features == nil || !s.agentAuthorized(r) {
		http.Error(w, "missing agent identity", http.StatusUnauthorized)
		return
	}
	nodeID := strings.TrimSpace(r.Header.Get("X-Node-ID"))
	osName := strings.ToLower(cleanFeatureText(firstNonEmpty(r.URL.Query().Get("os"), r.Header.Get("X-Agent-OS")), 16))
	arch := strings.ToLower(cleanFeatureText(firstNonEmpty(r.URL.Query().Get("arch"), r.Header.Get("X-Agent-Arch")), 16))
	if !validAgentPlatform(osName, arch) {
		http.Error(w, "valid os and arch are required", http.StatusBadRequest)
		return
	}
	rollout, action, ok := s.features.EligibleRollout(nodeID)
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	artifact, version, ok := s.rolloutArtifact(r, rollout, action, osName, arch)
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	now := time.Now()
	manifest := AgentUpgradeManifest{
		RolloutID: rollout.ID, NodeID: nodeID, Action: action, Version: version,
		OS: osName, Arch: arch, URL: artifact.URL, SHA256: artifact.SHA256,
		IssuedAt: now.Unix(), ExpiresAt: now.Add(agentUpgradeManifestTTL).Unix(),
		SignatureAlgorithm: "hmac-sha256",
	}
	manifest.Signature = signAgentUpgradeManifest(manifest, bearerToken(r.Header.Get("Authorization")))
	if err := s.features.MarkRolloutNodeState(rollout.ID, nodeID, action+"_offered"); err != nil {
		http.Error(w, "persist rollout state", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, manifest)
}

func (s *Server) handleAgentUpgradeResult(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if s.features == nil || !s.agentAuthorized(r) {
		http.Error(w, "missing agent identity", http.StatusUnauthorized)
		return
	}
	defer r.Body.Close()
	var req agentUpgradeResultRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	req.RolloutID = strings.TrimSpace(req.RolloutID)
	req.Action = strings.ToLower(strings.TrimSpace(req.Action))
	req.Status = strings.ToLower(strings.TrimSpace(req.Status))
	req.Version = cleanFeatureText(req.Version, 32)
	req.OS = strings.ToLower(cleanFeatureText(req.OS, 16))
	req.Arch = strings.ToLower(cleanFeatureText(req.Arch, 16))
	req.SHA256 = strings.ToLower(strings.TrimSpace(req.SHA256))
	req.Error = cleanFeatureMessage(req.Error, 500)
	if !validateFeatureID(req.RolloutID) || !validUpgradeResult(req.Action, req.Status) || (req.SHA256 != "" && !validSHA256(req.SHA256)) {
		http.Error(w, "invalid upgrade result", http.StatusBadRequest)
		return
	}
	nodeID := strings.TrimSpace(r.Header.Get("X-Node-ID"))
	rollout, exists := s.features.Rollout(req.RolloutID)
	if !exists {
		http.Error(w, "rollout not found", http.StatusConflict)
		return
	}
	if req.Status == "installed" || req.Status == "succeeded" {
		artifact, version, found := s.rolloutArtifact(r, rollout, req.Action, req.OS, req.Arch)
		if !found || req.Version != version || !constantEqual(req.SHA256, artifact.SHA256) {
			http.Error(w, "upgrade artifact verification failed", http.StatusConflict)
			return
		}
	}
	state := req.Status
	if req.Action == "rollback" {
		state = "rollback_" + req.Status
	}
	if err := s.features.RecordUpgradeResult(req.RolloutID, nodeID, req.Action, state); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	event := map[string]any{
		"rollout_id": req.RolloutID, "node_id": nodeID, "action": req.Action,
		"status": req.Status, "version": req.Version, "os": req.OS, "arch": req.Arch,
		"sha256": req.SHA256, "error": req.Error,
	}
	go s.dispatchPlatformEvent("agent.upgrade.result", event)
	writeJSON(w, map[string]bool{"ok": true})
}

func (h *PlatformFeatureHub) Rollout(id string) (AgentUpgradeRollout, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, item := range h.data.UpgradeRollouts {
		if item.ID == id {
			return item, true
		}
	}
	return AgentUpgradeRollout{}, false
}

func (h *PlatformFeatureHub) ReconcileAgentVersion(nodeID, agentVersion string) ([]AgentUpgradeCompletion, error) {
	// A legacy rollback target may not understand the pending receipt. Its next
	// authenticated metrics report still proves that the requested version ran.
	agentVersion = strings.TrimSpace(agentVersion)
	if agentVersion == "" {
		return nil, nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	type previousState struct {
		index     int
		state     string
		updatedAt int64
	}
	previous := make([]previousState, 0)
	completions := make([]AgentUpgradeCompletion, 0)
	previousDataUpdatedAt := h.data.UpdatedAt
	now := time.Now().Unix()
	for i := range h.data.UpgradeRollouts {
		item := &h.data.UpgradeRollouts[i]
		if !rolloutMatches(h.data, *item, nodeID) || !rolloutCanaryEligible(item.ID, nodeID, item.CanaryPercent) {
			continue
		}
		current := item.NodeStates[nodeID]
		action, target, finalState := "", "", ""
		if (item.State == "active" || item.State == "paused") && sameAgentVersion(agentVersion, item.TargetVersion) && (current == "upgrade_offered" || current == "downloading") {
			action, target, finalState = "upgrade", item.TargetVersion, "installed"
		}
		if item.State == "rollback" && sameAgentVersion(agentVersion, item.RollbackVersion) && (current == "rollback_offered" || current == "rollback_downloading") {
			action, target, finalState = "rollback", item.RollbackVersion, "rollback_installed"
		}
		if finalState == "" {
			continue
		}
		if item.NodeStates == nil {
			item.NodeStates = map[string]string{}
		}
		previous = append(previous, previousState{index: i, state: current, updatedAt: item.UpdatedAt})
		item.NodeStates[nodeID] = finalState
		item.UpdatedAt = now
		completions = append(completions, AgentUpgradeCompletion{RolloutID: item.ID, Action: action, Version: target})
	}
	if len(completions) == 0 {
		return nil, nil
	}
	if err := h.saveLocked(); err != nil {
		for _, old := range previous {
			h.data.UpgradeRollouts[old.index].NodeStates[nodeID] = old.state
			h.data.UpgradeRollouts[old.index].UpdatedAt = old.updatedAt
		}
		h.data.UpdatedAt = previousDataUpdatedAt
		return nil, err
	}
	return completions, nil
}

func sameAgentVersion(left, right string) bool {
	left, right = strings.TrimSpace(left), strings.TrimSpace(right)
	return left != "" && right != "" && strings.EqualFold(normalizeVersion(left), normalizeVersion(right))
}

func (h *PlatformFeatureHub) EligibleRollout(nodeID string) (AgentUpgradeRollout, string, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for i := len(h.data.UpgradeRollouts) - 1; i >= 0; i-- {
		item := h.data.UpgradeRollouts[i]
		if item.State != "active" && item.State != "rollback" {
			continue
		}
		if !rolloutMatches(h.data, item, nodeID) || !rolloutCanaryEligible(item.ID, nodeID, item.CanaryPercent) {
			continue
		}
		current := item.NodeStates[nodeID]
		if item.State == "active" {
			if current == "installed" || current == "succeeded" {
				continue
			}
			return item, "upgrade", true
		}
		if current == "installed" || current == "succeeded" || current == "failed" || current == "rollback_failed" || current == "rollback_offered" {
			return item, "rollback", true
		}
	}
	return AgentUpgradeRollout{}, "", false
}

func (h *PlatformFeatureHub) MarkRolloutNodeState(rolloutID, nodeID, state string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.data.UpgradeRollouts {
		item := &h.data.UpgradeRollouts[i]
		if item.ID != rolloutID {
			continue
		}
		if item.NodeStates == nil {
			item.NodeStates = map[string]string{}
		}
		if item.NodeStates[nodeID] == state {
			return nil
		}
		item.NodeStates[nodeID] = state
		item.UpdatedAt = time.Now().Unix()
		return h.saveLocked()
	}
	return fmt.Errorf("rollout not found")
}

func (h *PlatformFeatureHub) RecordUpgradeResult(rolloutID, nodeID, action, state string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.data.UpgradeRollouts {
		item := &h.data.UpgradeRollouts[i]
		if item.ID != rolloutID {
			continue
		}
		if action == "upgrade" && item.State != "active" && item.State != "paused" || action == "rollback" && item.State != "rollback" {
			return fmt.Errorf("rollout action is not active")
		}
		if !rolloutMatches(h.data, *item, nodeID) || !rolloutCanaryEligible(item.ID, nodeID, item.CanaryPercent) {
			return fmt.Errorf("node is outside rollout")
		}
		if item.NodeStates == nil {
			item.NodeStates = map[string]string{}
		}
		item.NodeStates[nodeID] = state
		item.UpdatedAt = time.Now().Unix()
		return h.saveLocked()
	}
	return fmt.Errorf("rollout not found")
}

func rolloutMatches(data PlatformFeatureData, rollout AgentUpgradeRollout, nodeID string) bool {
	if len(rollout.NodeIDs) == 0 && len(rollout.Tags) == 0 {
		return true
	}
	if listContains(rollout.NodeIDs, nodeID) {
		return true
	}
	profile := data.NodeProfiles[nodeID]
	for _, tag := range rollout.Tags {
		if listContains(profile.Tags, tag) {
			return true
		}
	}
	return false
}

func rolloutCanaryEligible(rolloutID, nodeID string, percent int) bool {
	if percent >= 100 {
		return true
	}
	if percent <= 0 {
		return false
	}
	sum := sha256.Sum256([]byte(rolloutID + "\x00" + nodeID))
	return int(binary.BigEndian.Uint16(sum[:2])%100) < percent
}

func (s *Server) rolloutArtifact(r *http.Request, rollout AgentUpgradeRollout, action, osName, arch string) (AgentUpgradeArtifact, string, bool) {
	if action == "rollback" {
		artifact, ok := findUpgradeArtifact(rollout.RollbackArtifacts, osName, arch)
		return artifact, rollout.RollbackVersion, ok && rollout.RollbackVersion != ""
	}
	if artifact, ok := findUpgradeArtifact(rollout.Artifacts, osName, arch); ok {
		return artifact, rollout.TargetVersion, true
	}
	if normalizeVersion(rollout.TargetVersion) != normalizeVersion(buildVersion) {
		return AgentUpgradeArtifact{}, rollout.TargetVersion, false
	}
	artifact, ok := s.embeddedUpgradeArtifact(r, osName, arch)
	return artifact, rollout.TargetVersion, ok
}

func (s *Server) embeddedUpgradeArtifact(r *http.Request, osName, arch string) (AgentUpgradeArtifact, bool) {
	name := "vps-agent-" + osName + "-" + arch
	if osName == "windows" {
		name += ".exe"
	}
	data, err := agentBinaries.ReadFile("agent_bins/" + name)
	if err != nil {
		return AgentUpgradeArtifact{}, false
	}
	base, err := s.externalBase(r)
	if err != nil {
		return AgentUpgradeArtifact{}, false
	}
	sum := sha256.Sum256(data)
	return AgentUpgradeArtifact{OS: osName, Arch: arch, URL: base + "/download/" + name, SHA256: hex.EncodeToString(sum[:])}, true
}

func signAgentUpgradeManifest(value AgentUpgradeManifest, token string) string {
	value.Signature = ""
	raw, _ := json.Marshal(value)
	mac := hmac.New(sha256.New, []byte(token))
	_, _ = mac.Write(raw)
	return hex.EncodeToString(mac.Sum(nil))
}

func validAgentPlatform(osName, arch string) bool {
	if osName != "linux" && osName != "windows" {
		return false
	}
	switch arch {
	case "386", "amd64", "arm64":
		return true
	case "armv7":
		return osName == "linux"
	default:
		return false
	}
}

func validUpgradeResult(action, status string) bool {
	if action != "upgrade" && action != "rollback" {
		return false
	}
	switch status {
	case "downloading", "installed", "succeeded", "failed":
		return true
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

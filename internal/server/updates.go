package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	buildVersion   = "dev"
	buildCommit    = "unknown"
	buildTime      = "unknown"
	versionPattern = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:[-+]([0-9A-Za-z.-]+))?$`)
)

func SetBuildInfo(version, commit, built string) {
	buildVersion = normalizeVersion(version)
	buildCommit = strings.TrimSpace(commit)
	buildTime = strings.TrimSpace(built)
}

type ReleaseVersion struct {
	Current         string         `json:"current"`
	Commit          string         `json:"commit,omitempty"`
	BuildTime       string         `json:"build_time,omitempty"`
	Latest          string         `json:"latest,omitempty"`
	UpdateAvailable bool           `json:"update_available"`
	Name            string         `json:"name,omitempty"`
	Summary         string         `json:"summary,omitempty"`
	ReleaseURL      string         `json:"release_url,omitempty"`
	PublishedAt     string         `json:"published_at,omitempty"`
	Repository      string         `json:"repository"`
	CheckError      string         `json:"check_error,omitempty"`
	UpdateEnabled   bool           `json:"update_enabled,omitempty"`
	Status          *UpdateStatus  `json:"status,omitempty"`
	History         []UpdateStatus `json:"history,omitempty"`
}

type UpdateStatus struct {
	State     string `json:"state"`
	Version   string `json:"version,omitempty"`
	Message   string `json:"message,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type githubRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	Body        string `json:"body"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
}

type UpdateService struct {
	mu       sync.Mutex
	cfg      Config
	client   *http.Client
	cached   githubRelease
	cachedAt time.Time
}

func NewUpdateService(cfg Config) *UpdateService {
	return &UpdateService{cfg: cfg, client: &http.Client{Timeout: 8 * time.Second}}
}

func (u *UpdateService) snapshot(force bool, admin bool) ReleaseVersion {
	release, err := u.latest(force)
	current := normalizeVersion(buildVersion)
	out := ReleaseVersion{Current: current, Repository: u.cfg.UpdateRepo}
	if admin {
		status := u.readStatus()
		out.Commit = buildCommit
		out.BuildTime = buildTime
		out.UpdateEnabled = u.cfg.UpdateEnabled
		out.Status = &status
		out.History = u.readHistory()
	}
	if err != nil {
		if admin {
			out.CheckError = err.Error()
		}
		return out
	}
	out.Latest = normalizeVersion(release.TagName)
	out.Name = strings.TrimSpace(release.Name)
	out.Summary = releaseSummary(release.Body)
	if admin {
		out.ReleaseURL = release.HTMLURL
	} else {
		out.ReleaseURL = "/monitor#changelog"
	}
	out.PublishedAt = release.PublishedAt
	out.UpdateAvailable = versionCompare(out.Latest, current) > 0
	return out
}

func (u *UpdateService) latest(force bool) (githubRelease, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if !force && u.cached.TagName != "" && time.Since(u.cachedAt) < 15*time.Minute {
		return u.cached, nil
	}
	request, err := http.NewRequest(http.MethodGet, u.cfg.UpdateAPIBase+"/repos/"+u.cfg.UpdateRepo+"/releases/latest", nil)
	if err != nil {
		return githubRelease{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "vps-monitor-update-checker")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token := strings.TrimSpace(u.cfg.GitHubToken); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := u.client.Do(request)
	if err != nil {
		return githubRelease{}, fmt.Errorf("GitHub update check failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 8<<10))
		return githubRelease{}, fmt.Errorf("GitHub update check returned HTTP %d", response.StatusCode)
	}
	var release githubRelease
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&release); err != nil {
		return githubRelease{}, err
	}
	if release.Draft || release.Prerelease || !validReleaseVersion(release.TagName) {
		return githubRelease{}, errors.New("latest GitHub release is not a stable semantic version")
	}
	u.cached, u.cachedAt = release, time.Now()
	return release, nil
}

func (u *UpdateService) createRequest(version string) error {
	if !u.cfg.UpdateEnabled {
		return errors.New("one-click updates are not enabled on this server")
	}
	version = normalizeVersion(version)
	if !validReleaseVersion(version) {
		return errors.New("invalid release version")
	}
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return err
	}
	created := time.Now().UTC().Format(time.RFC3339)
	nonce := hex.EncodeToString(nonceBytes)
	payload := strings.Join([]string{u.cfg.UpdateRepo, version, created, nonce}, "\n")
	mac := hmac.New(sha256.New, []byte(u.cfg.AuthSecret))
	_, _ = mac.Write([]byte(payload))
	request := map[string]string{"repository": u.cfg.UpdateRepo, "version": version, "created_at": created, "nonce": nonce, "signature": hex.EncodeToString(mac.Sum(nil))}
	if err := writeJSONAtomic(u.path("update-request.json"), request, 0o600); err != nil {
		return err
	}
	return writeJSONAtomic(u.path("update-status.json"), UpdateStatus{State: "pending", Version: version, Message: "更新请求已提交，等待宿主机更新服务处理", UpdatedAt: created}, 0o644)
}

func (u *UpdateService) readStatus() UpdateStatus {
	status := UpdateStatus{State: "idle"}
	raw, err := os.ReadFile(u.path("update-status.json"))
	if err == nil {
		_ = json.Unmarshal(raw, &status)
	}
	if strings.TrimSpace(status.State) == "" {
		status.State = "idle"
	}
	return status
}

func (u *UpdateService) readHistory() []UpdateStatus {
	items := []UpdateStatus{}
	raw, err := os.ReadFile(u.path("update-history.json"))
	if err != nil || json.Unmarshal(raw, &items) != nil {
		return items
	}
	if len(items) > 50 {
		items = items[len(items)-50:]
	}
	for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
		items[left], items[right] = items[right], items[left]
	}
	return items
}

func (u *UpdateService) path(name string) string {
	return filepath.Join(filepath.Dir(u.cfg.DataPath), name)
}

func writeJSONAtomic(path string, value any, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func normalizeVersion(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "dev"
	}
	if value == "dev" {
		return value
	}
	if !strings.HasPrefix(value, "v") {
		value = "v" + value
	}
	return value
}
func validReleaseVersion(value string) bool {
	return versionPattern.MatchString(normalizeVersion(value))
}
func versionCompare(a, b string) int {
	ma, mb := versionPattern.FindStringSubmatch(normalizeVersion(a)), versionPattern.FindStringSubmatch(normalizeVersion(b))
	if ma == nil {
		return -1
	}
	if mb == nil {
		return 1
	}
	for i := 1; i <= 3; i++ {
		ai, _ := strconv.Atoi(ma[i])
		bi, _ := strconv.Atoi(mb[i])
		if ai < bi {
			return -1
		}
		if ai > bi {
			return 1
		}
	}
	if ma[4] == mb[4] {
		return 0
	}
	if ma[4] == "" {
		return 1
	}
	if mb[4] == "" {
		return -1
	}
	return strings.Compare(ma[4], mb[4])
}
func releaseSummary(body string) string {
	body = strings.Join(strings.Fields(strings.TrimSpace(body)), " ")
	if len([]rune(body)) <= 240 {
		return body
	}
	return string([]rune(body)[:240]) + "..."
}

func (s *Server) handleSiteVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, s.updates.snapshot(false, false))
}

func (s *Server) handleAdminUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		http.Error(w, "admin login required", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	force := r.URL.Query().Get("force") == "1"
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, s.updates.snapshot(force, true))
}

func (s *Server) handleAdminUpdateApply(w http.ResponseWriter, r *http.Request) {
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
	var request struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&request); err != nil {
		http.Error(w, "admin password confirmation is required", http.StatusBadRequest)
		return
	}
	if !constantEqual(request.Password, s.cfg.AdminPass) {
		time.Sleep(300 * time.Millisecond)
		http.Error(w, "invalid admin credentials", http.StatusUnauthorized)
		return
	}
	info := s.updates.snapshot(true, true)
	if info.CheckError != "" {
		http.Error(w, info.CheckError, http.StatusBadGateway)
		return
	}
	if !info.UpdateAvailable {
		http.Error(w, "no newer stable release is available", http.StatusConflict)
		return
	}
	if err := s.updates.createRequest(info.Latest); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "version": info.Latest})
}

package server

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// siteContent 是公告 / 更新日志的公开内容结构。内容文件位于数据和
// 配置同目录下的 content.json；公开接口只读，Admin 接口可读写。
// 缺失或解析失败时公开接口返回空内容，避免前台报错。
type siteContent struct {
	Announcement string `json:"announcement"`
	Changelog    string `json:"changelog"`
}

// 单字段上限（约 256KB 字符），防止误粘贴超大 HTML 撑爆存储。
const maxSiteContentFieldRunes = 256 * 1024

func (s *Server) contentPath() string {
	if s.cfg.DataPath == "" {
		return "data/content.json"
	}
	return filepath.Join(filepath.Dir(s.cfg.DataPath), "content.json")
}

func (s *Server) loadSiteContentRaw() siteContent {
	content := siteContent{}
	data, err := os.ReadFile(s.contentPath())
	if err == nil && len(data) > 0 {
		// 解析失败也不报错，保持空内容，前台照常渲染占位文案。
		_ = json.Unmarshal(data, &content)
	}
	return content
}

// loadSiteContent keeps the legacy HTML API working while official notes live
// in the binary and custom structured notes live in their own data file.
func (s *Server) loadSiteContent() siteContent {
	content := s.loadSiteContentRaw()
	official, err := loadEmbeddedReleaseNotes()
	if err != nil {
		return content
	}
	custom, _ := s.loadCustomReleaseNotes()
	parts := []string{officialChangelogHeading, renderReleaseNotesHTML(official), renderReleaseNotesHTML(custom)}
	legacy := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(content.Changelog), officialChangelogHeading))
	if legacy != "" {
		parts = append(parts, legacy)
	}
	content.Changelog = strings.Join(nonEmptyStrings(parts), "\n\n")
	return content
}

func nonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func (s *Server) saveSiteContent(content siteContent) error {
	path := s.contentPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(content, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (s *Server) handleSiteContent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, s.loadSiteContent())
}

// handleAdminSiteContent 读写站点公告与更新日志（content.json）。
func (s *Server) handleAdminSiteContent(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		http.Error(w, "admin login required", http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodGet:
		// Admin edits only the mutable fields. Official embedded notes are exposed
		// through /api/admin/release-notes and cannot be overwritten here.
		writeJSON(w, s.loadSiteContentRaw())
	case http.MethodPost:
		if !s.validAdminOrigin(r) {
			http.Error(w, "invalid request origin", http.StatusForbidden)
			return
		}
		var req siteContent
		if err := json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		req.Announcement = strings.TrimSpace(req.Announcement)
		req.Changelog = stripEmbeddedOfficialChangelog(strings.TrimSpace(req.Changelog))
		if utf8.RuneCountInString(req.Announcement) > maxSiteContentFieldRunes {
			http.Error(w, "announcement too long", http.StatusBadRequest)
			return
		}
		if utf8.RuneCountInString(req.Changelog) > maxSiteContentFieldRunes {
			http.Error(w, "changelog too long", http.StatusBadRequest)
			return
		}
		if err := s.saveSiteContent(req); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	default:
		methodNotAllowed(w)
	}
}

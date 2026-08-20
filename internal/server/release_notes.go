package server

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

//go:embed release_notes.json
var embeddedReleaseNotesJSON []byte

type ReleaseNote struct {
	ID      string   `json:"id,omitempty"`
	Version string   `json:"version,omitempty"`
	Date    string   `json:"date"`
	Title   string   `json:"title"`
	Type    string   `json:"type"`
	Items   []string `json:"items"`
	Source  string   `json:"source"`
}

type releaseNotesResponse struct {
	Official   []ReleaseNote `json:"official"`
	Custom     []ReleaseNote `json:"custom"`
	All        []ReleaseNote `json:"all"`
	LegacyHTML string        `json:"legacy_html,omitempty"`
}

func loadEmbeddedReleaseNotes() ([]ReleaseNote, error) {
	var notes []ReleaseNote
	if err := json.Unmarshal(embeddedReleaseNotesJSON, &notes); err != nil {
		return nil, fmt.Errorf("parse embedded release notes: %w", err)
	}
	seen := make(map[string]bool, len(notes))
	for i := range notes {
		notes[i].Version = strings.TrimSpace(notes[i].Version)
		notes[i].Date = strings.TrimSpace(notes[i].Date)
		notes[i].Title = strings.TrimSpace(notes[i].Title)
		notes[i].Type = normalizeReleaseNoteType(notes[i].Type)
		notes[i].Source = "official"
		if notes[i].Version == "" || seen[notes[i].Version] {
			return nil, fmt.Errorf("duplicate or empty embedded release version %q", notes[i].Version)
		}
		if err := validateReleaseNote(notes[i], true); err != nil {
			return nil, fmt.Errorf("invalid embedded release note %q: %w", notes[i].Version, err)
		}
		seen[notes[i].Version] = true
	}
	sortReleaseNotes(notes)
	return notes, nil
}

func (s *Server) customReleaseNotesPath() string {
	return filepath.Join(filepath.Dir(s.cfg.DataPath), "custom-release-notes.json")
}

func (s *Server) loadCustomReleaseNotes() ([]ReleaseNote, error) {
	raw, err := os.ReadFile(s.customReleaseNotesPath())
	if errors.Is(err, os.ErrNotExist) {
		return []ReleaseNote{}, nil
	}
	if err != nil {
		return nil, err
	}
	var notes []ReleaseNote
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &notes); err != nil {
			return nil, err
		}
	}
	for i := range notes {
		notes[i].ID = strings.TrimSpace(notes[i].ID)
		if notes[i].Version != "" {
			notes[i].Version = normalizeVersion(notes[i].Version)
		}
		notes[i].Source = "custom"
		if notes[i].ID == "" || !validNodeID(notes[i].ID) {
			return nil, errors.New("custom release note id is missing or invalid")
		}
		if err := validateReleaseNote(notes[i], false); err != nil {
			return nil, fmt.Errorf("invalid custom release note %q: %w", notes[i].ID, err)
		}
	}
	sortReleaseNotes(notes)
	return notes, nil
}

func (s *Server) saveCustomReleaseNotes(notes []ReleaseNote) error {
	seen := map[string]bool{}
	seenVersions := map[string]bool{}
	officialVersions := map[string]bool{}
	if official, err := loadEmbeddedReleaseNotes(); err == nil {
		for _, note := range official {
			officialVersions[note.Version] = true
		}
	}
	for i := range notes {
		notes[i].ID = strings.TrimSpace(notes[i].ID)
		if notes[i].ID == "" {
			id, err := newNodeID()
			if err != nil {
				return err
			}
			notes[i].ID = "rn_" + strings.TrimPrefix(id, "n_")
		}
		if seen[notes[i].ID] {
			return errors.New("duplicate custom release note id")
		}
		if !validNodeID(notes[i].ID) {
			return errors.New("invalid custom release note id")
		}
		seen[notes[i].ID] = true
		notes[i].Version = strings.TrimSpace(notes[i].Version)
		if notes[i].Version != "" {
			notes[i].Version = normalizeVersion(notes[i].Version)
		}
		if notes[i].Version != "" && officialVersions[notes[i].Version] {
			return errors.New("official release versions are read-only")
		}
		if notes[i].Version != "" {
			if seenVersions[notes[i].Version] {
				return errors.New("duplicate custom release version")
			}
			seenVersions[notes[i].Version] = true
		}
		notes[i].Date = strings.TrimSpace(notes[i].Date)
		notes[i].Title = strings.TrimSpace(notes[i].Title)
		notes[i].Type = normalizeReleaseNoteType(notes[i].Type)
		notes[i].Source = "custom"
		for itemIndex := range notes[i].Items {
			notes[i].Items[itemIndex] = strings.TrimSpace(notes[i].Items[itemIndex])
		}
		if err := validateReleaseNote(notes[i], false); err != nil {
			return err
		}
	}
	sortReleaseNotes(notes)
	return writeJSONAtomic(s.customReleaseNotesPath(), notes, 0o600)
}

func normalizeReleaseNoteType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "feature", "fix", "improvement", "security", "maintenance":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "improvement"
	}
}

func validateReleaseNote(note ReleaseNote, official bool) error {
	if _, err := time.Parse("2006-01-02", note.Date); err != nil {
		return errors.New("date must use YYYY-MM-DD")
	}
	if note.Title == "" || utf8.RuneCountInString(note.Title) > 120 {
		return errors.New("title is required and must not exceed 120 characters")
	}
	if official && note.Version == "" {
		return errors.New("version is required")
	}
	if note.Version != "" && !validReleaseVersion(note.Version) {
		return errors.New("version must use semantic version format")
	}
	if utf8.RuneCountInString(note.Version) > 40 || len(note.Items) == 0 || len(note.Items) > 30 {
		return errors.New("invalid version or item count")
	}
	for _, item := range note.Items {
		if strings.TrimSpace(item) == "" || utf8.RuneCountInString(item) > 500 {
			return errors.New("release note items must be non-empty and at most 500 characters")
		}
	}
	return nil
}

func sortReleaseNotes(notes []ReleaseNote) {
	sort.SliceStable(notes, func(i, j int) bool {
		if notes[i].Date != notes[j].Date {
			return notes[i].Date > notes[j].Date
		}
		if notes[i].Version != "" && notes[j].Version != "" && validReleaseVersion(notes[i].Version) && validReleaseVersion(notes[j].Version) {
			return versionCompare(notes[i].Version, notes[j].Version) > 0
		}
		return notes[i].Version > notes[j].Version
	})
}

func (s *Server) releaseNotes() (releaseNotesResponse, error) {
	official, err := loadEmbeddedReleaseNotes()
	if err != nil {
		return releaseNotesResponse{}, err
	}
	custom, err := s.loadCustomReleaseNotes()
	if err != nil {
		return releaseNotesResponse{}, err
	}
	all := append(append([]ReleaseNote{}, official...), custom...)
	sortReleaseNotes(all)
	return releaseNotesResponse{Official: official, Custom: custom, All: all}, nil
}

func renderReleaseNotesHTML(notes []ReleaseNote) string {
	if len(notes) == 0 {
		return ""
	}
	var b strings.Builder
	for _, note := range notes {
		label := strings.TrimSpace(strings.TrimSpace(note.Version + " " + note.Title))
		b.WriteString("<h3><span>")
		b.WriteString(html.EscapeString(note.Date))
		b.WriteString("</span> · <mark>")
		b.WriteString(html.EscapeString(label))
		b.WriteString("</mark></h3>\n<ul>\n")
		for _, item := range note.Items {
			b.WriteString("<li>")
			b.WriteString(html.EscapeString(item))
			b.WriteString("</li>\n")
		}
		b.WriteString("</ul>\n")
	}
	return strings.TrimSpace(b.String())
}

func (s *Server) handleReleaseNotes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	response, err := s.releaseNotes()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	response.LegacyHTML = strings.TrimSpace(s.loadSiteContentRaw().Changelog)
	writeJSON(w, response)
}

func (s *Server) handleAdminReleaseNotes(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		http.Error(w, "admin login required", http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.handleReleaseNotes(w, r)
	case http.MethodPost:
		if !s.validAdminOrigin(r) {
			http.Error(w, "invalid request origin", http.StatusForbidden)
			return
		}
		defer r.Body.Close()
		var req struct {
			Custom []ReleaseNote `json:"custom"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(req.Custom) > 200 {
			http.Error(w, "too many custom release notes", http.StatusBadRequest)
			return
		}
		if err := s.saveCustomReleaseNotes(req.Custom); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		response, err := s.releaseNotes()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, response)
	default:
		methodNotAllowed(w)
	}
}

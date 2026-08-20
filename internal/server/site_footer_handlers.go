package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

type publicFriendLink struct {
	Label  string `json:"label"`
	URL    string `json:"url"`
	NewTab bool   `json:"new_tab"`
}
type siteFooterResponse struct {
	Hidden             bool               `json:"hidden"`
	Text               string             `json:"text"`
	LinksTitle         string             `json:"links_title"`
	VisitorStatsHidden bool               `json:"visitor_stats_hidden"`
	VisitorStatsItems  []string           `json:"visitor_stats_items"`
	Links              []publicFriendLink `json:"links"`
}

func (s *Server) handleSiteFooter(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	settings := normalizeSettings(s.store.GetSettings())
	links := s.store.ListFriendLinks(false)
	views := make([]publicFriendLink, 0, len(links))
	for _, link := range links {
		views = append(views, publicFriendLink{Label: link.Label, URL: link.URL, NewTab: link.NewTab})
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, siteFooterResponse{Hidden: settings.FooterHidden, Text: settings.FooterText, LinksTitle: settings.FriendLinksTitle, VisitorStatsHidden: settings.VisitorStatsHidden, VisitorStatsItems: settings.VisitorStatsItems, Links: views})
}

func (s *Server) handleAdminFriendLinks(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		http.Error(w, "admin login required", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, s.store.ListFriendLinks(true))
}
func (s *Server) handleAdminFriendLinkSave(w http.ResponseWriter, r *http.Request) {
	if !s.adminMutationAllowed(w, r) {
		return
	}
	var link FriendLink
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&link); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	clean, err := validateFriendLink(link)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	saved, err := s.store.SaveFriendLink(clean)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, saved)
}
func (s *Server) handleAdminFriendLinkDelete(w http.ResponseWriter, r *http.Request) {
	if !s.adminMutationAllowed(w, r) {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req) != nil || !validFriendLinkID(req.ID) {
		http.Error(w, "invalid friend link", http.StatusBadRequest)
		return
	}
	if err := s.store.DeleteFriendLink(req.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
func (s *Server) handleAdminFriendLinkReorder(w http.ResponseWriter, r *http.Request) {
	if !s.adminMutationAllowed(w, r) {
		return
	}
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil || len(req.IDs) > 30 {
		http.Error(w, "invalid friend link order", http.StatusBadRequest)
		return
	}
	if err := s.store.ReorderFriendLinks(req.IDs); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func validateFriendLink(link FriendLink) (FriendLink, error) {
	link.ID = strings.TrimSpace(link.ID)
	link.Label = strings.TrimSpace(link.Label)
	link.URL = strings.TrimSpace(link.URL)
	if link.ID != "" && !validFriendLinkID(link.ID) {
		return link, fmt.Errorf("invalid friend link id")
	}
	if link.Label == "" || utf8.RuneCountInString(link.Label) > 40 {
		return link, fmt.Errorf("friend link label must be 1-40 characters")
	}
	u, err := url.Parse(link.URL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return link, fmt.Errorf("friend link URL must be absolute http or https")
	}
	link.URL = u.String()
	if link.SortOrder < 0 {
		link.SortOrder = 0
	}
	return link, nil
}
func validFriendLinkID(id string) bool {
	return strings.HasPrefix(id, "link_") && len(id) <= 64 && validDownloadName(id)
}

package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

type adminOwnerView struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	CreatedAt int64  `json:"created_at"`
	LastLogin int64  `json:"last_login"`
	Disabled  bool   `json:"disabled"`
}

func (s *Server) handleAdminMarketOwners(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		http.Error(w, "admin login required", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if utf8.RuneCountInString(query) > 120 {
		http.Error(w, "query too long", http.StatusBadRequest)
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	owners, total := s.store.ListOwners(query, pageSize, (page-1)*pageSize)
	items := make([]adminOwnerView, 0, len(owners))
	for _, owner := range owners {
		items = append(items, adminOwnerView{ID: owner.ID, Email: owner.Email, CreatedAt: owner.CreatedAt, LastLogin: owner.LastLogin, Disabled: owner.Disabled})
	}
	writeJSON(w, map[string]any{"items": items, "total": total, "page": page, "page_size": pageSize})
}

func (s *Server) handleAdminMarketOwnerStatus(w http.ResponseWriter, r *http.Request) {
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
	var req struct {
		OwnerID  string `json:"owner_id"`
		Disabled bool   `json:"disabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	req.OwnerID = strings.TrimSpace(req.OwnerID)
	if req.OwnerID == "" || utf8.RuneCountInString(req.OwnerID) > 64 {
		http.Error(w, "invalid owner_id", http.StatusBadRequest)
		return
	}
	audit := auditForRequest(r, "admin", "admin", "owner.status", "owner", req.OwnerID, fmt.Sprintf("Set disabled=%t", req.Disabled))
	if err := s.store.UpdateOwnerAdmin(OwnerAdminMutation{OwnerID: req.OwnerID, Disabled: &req.Disabled, Audit: audit}); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if req.Disabled {
		if err := s.sessions.DeleteFor(SessionKindOwner, req.OwnerID); err != nil {
			http.Error(w, "owner disabled but session revocation failed", http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleAdminMarketOwnerResetPassword(w http.ResponseWriter, r *http.Request) {
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
	var req struct {
		OwnerID  string `json:"owner_id"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	req.OwnerID = strings.TrimSpace(req.OwnerID)
	if req.OwnerID == "" || utf8.RuneCountInString(req.OwnerID) > 64 || len(req.Password) < 8 || len(req.Password) > 128 {
		http.Error(w, "owner_id and password (8-128 characters) are required", http.StatusBadRequest)
		return
	}
	hash, err := hashOwnerPassword(req.Password)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	audit := auditForRequest(r, "admin", "admin", "owner.password.reset", "owner", req.OwnerID, "Reset owner password")
	if err := s.store.UpdateOwnerAdmin(OwnerAdminMutation{OwnerID: req.OwnerID, PasswordHash: &hash, Audit: audit}); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if err := s.sessions.DeleteFor(SessionKindOwner, req.OwnerID); err != nil {
		http.Error(w, "password reset but session revocation failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleAdminMarketModerate(w http.ResponseWriter, r *http.Request) {
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
	var req struct {
		NodeID          string `json:"node_id"`
		Status          string `json:"status"`
		RejectionReason string `json:"rejection_reason"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 32<<10)).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	req.NodeID = strings.TrimSpace(req.NodeID)
	req.Status = strings.ToLower(strings.TrimSpace(req.Status))
	req.RejectionReason = sanitizePlain(req.RejectionReason, 500)
	if !validNodeID(req.NodeID) || (req.Status != "approved" && req.Status != "pending" && req.Status != "rejected") {
		http.Error(w, "invalid node_id or moderation status", http.StatusBadRequest)
		return
	}
	if req.Status == "rejected" && req.RejectionReason == "" {
		http.Error(w, "rejection_reason is required", http.StatusBadRequest)
		return
	}
	if req.Status != "rejected" {
		req.RejectionReason = ""
	}
	audit := auditForRequest(r, "admin", "admin", "listing.moderate", "market_listing", req.NodeID, "Set moderation status to "+req.Status)
	if err := s.store.ModerateMarketListing(ListingModerationMutation{NodeID: req.NodeID, Status: req.Status, RejectionReason: req.RejectionReason, Audit: audit}); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if req.Status == "approved" && s.features != nil {
		go s.notifyMarketApproval(req.NodeID)
	} else if req.Status == "rejected" && s.features != nil {
		if listing, ok := s.store.GetMarketListing(req.NodeID); ok {
			go s.dispatchPlatformEvent("market.rejected", map[string]any{"owner_id": listing.OwnerID, "node_id": req.NodeID, "reason": req.RejectionReason})
		}
	}
	writeJSON(w, map[string]bool{"ok": true})
}

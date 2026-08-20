package server

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "golang.org/x/image/webp"
)

const maxAdImageBytes = 2 << 20

func (s *Server) handleMarketAds(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if !s.marketEnabled(w) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	ads := s.store.ListAdvertisements(false, time.Now())
	for i := range ads {
		ads[i].TargetURL = ""
		ads[i].Impressions = 0
		ads[i].Clicks = 0
	}
	writeJSON(w, MarketAdsResponse{Ads: ads, Settings: s.store.GetAdLayoutSettings()})
}

func (s *Server) handleMarketAdImpression(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.marketEnabled(w) {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req) != nil || !validAdvertisementID(req.ID) {
		http.Error(w, "invalid advertisement", http.StatusBadRequest)
		return
	}
	ad, ok := s.store.GetAdvertisement(req.ID)
	if !ok || !advertisementActive(ad, time.Now()) {
		http.Error(w, "advertisement not found", http.StatusNotFound)
		return
	}
	if s.adGlobalLimiter != nil && !s.adGlobalLimiter.Allow("global") {
		writeJSON(w, map[string]bool{"ok": true})
		return
	}
	if s.adStatLimiter != nil && !s.adStatLimiter.Allow(requestClientIP(r)+"\x00impression\x00"+req.ID) {
		writeJSON(w, map[string]bool{"ok": true})
		return
	}
	if err := s.store.IncrementAdvertisementStat(req.ID, false); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleAdvertisementRedirect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if !s.marketEnabled(w) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/r/ad/")
	ad, ok := s.store.GetAdvertisement(id)
	if !ok || !advertisementActive(ad, time.Now()) {
		http.NotFound(w, r)
		return
	}
	if _, err := cleanAdTargetURL(ad.TargetURL); err != nil {
		http.Error(w, "invalid advertisement target", http.StatusBadRequest)
		return
	}
	if (s.adGlobalLimiter == nil || s.adGlobalLimiter.Allow("global")) && (s.adStatLimiter == nil || s.adStatLimiter.Allow(requestClientIP(r)+"\x00click\x00"+id)) {
		_ = s.store.IncrementAdvertisementStat(id, true)
	}
	http.Redirect(w, r, ad.TargetURL, http.StatusFound)
}

func (s *Server) handleAdminAds(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		http.Error(w, "admin login required", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, s.store.ListAdvertisements(true, time.Now()))
}

func (s *Server) handleAdminAdSave(w http.ResponseWriter, r *http.Request) {
	if !s.adminMutationAllowed(w, r) {
		return
	}
	var ad Advertisement
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&ad); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	clean, err := validateAdvertisement(ad)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	old, hadOld := s.store.GetAdvertisement(clean.ID)
	saved, err := s.store.SaveAdvertisement(clean)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if hadOld && old.ImageURL != saved.ImageURL && strings.HasPrefix(old.ImageURL, "/ads/") {
		_ = os.Remove(filepath.Join(s.adDataDir(), filepath.Base(old.ImageURL)))
	}
	writeJSON(w, saved)
}

func (s *Server) handleAdminAdDelete(w http.ResponseWriter, r *http.Request) {
	if !s.adminMutationAllowed(w, r) {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req) != nil || !validAdvertisementID(req.ID) {
		http.Error(w, "invalid advertisement", http.StatusBadRequest)
		return
	}
	ad, _ := s.store.GetAdvertisement(req.ID)
	if err := s.store.DeleteAdvertisement(req.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if strings.HasPrefix(ad.ImageURL, "/ads/") {
		_ = os.Remove(filepath.Join(s.adDataDir(), filepath.Base(ad.ImageURL)))
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleAdminAdLayout(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		http.Error(w, "admin login required", http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, s.store.GetAdLayoutSettings())
	case http.MethodPost:
		if !s.validAdminOrigin(r) {
			http.Error(w, "invalid request origin", http.StatusForbidden)
			return
		}
		var v AdLayoutSettings
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&v); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		v = normalizeAdLayoutSettings(v)
		if v.DesktopInterval > 100 || v.MobileInterval > 100 || v.MaxAds > 1000 || v.MinServerGap > 100 {
			http.Error(w, "invalid advertisement layout", http.StatusBadRequest)
			return
		}
		if err := s.store.UpdateAdLayoutSettings(v); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, v)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleAdminAdUpload(w http.ResponseWriter, r *http.Request) {
	if !s.adminMutationAllowed(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAdImageBytes+(256<<10))
	if err := r.ParseMultipartForm(maxAdImageBytes + (128 << 10)); err != nil {
		http.Error(w, "image must be 2 MB or smaller", http.StatusBadRequest)
		return
	}
	file, _, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "image is required", http.StatusBadRequest)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxAdImageBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxAdImageBytes {
		http.Error(w, "image must be 2 MB or smaller", http.StatusBadRequest)
		return
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4000 || cfg.Height > 4000 {
		http.Error(w, "invalid image or dimensions", http.StatusBadRequest)
		return
	}
	ext := map[string]string{"jpeg": ".jpg", "png": ".png", "webp": ".webp"}[format]
	if ext == "" {
		http.Error(w, "only JPEG, PNG and WebP are allowed", http.StatusBadRequest)
		return
	}
	name, err := randomAdImageName(ext)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.MkdirAll(s.adDataDir(), 0755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(filepath.Join(s.adDataDir(), name), data, 0644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"image_url": "/ads/" + name, "width": cfg.Width, "height": cfg.Height})
}

func (s *Server) handleAdImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/ads/")
	if !validDownloadName(name) || name != filepath.Base(name) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeFile(w, r, filepath.Join(s.adDataDir(), name))
}

func (s *Server) adminMutationAllowed(w http.ResponseWriter, r *http.Request) bool {
	if !s.adminAuthorized(r) {
		http.Error(w, "admin login required", http.StatusUnauthorized)
		return false
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return false
	}
	if !s.validAdminOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return false
	}
	return true
}

func (s *Server) adDataDir() string {
	dataPath := strings.TrimSpace(s.cfg.DataPath)
	if dataPath == "" {
		switch store := s.store.(type) {
		case *Store:
			dataPath = store.path
		case *SQLiteStore:
			dataPath = store.path
		}
	}
	if dataPath == "" || dataPath == ":memory:" || strings.HasPrefix(dataPath, "file:") {
		dataPath = "data/server.json"
	}
	return filepath.Join(filepath.Dir(dataPath), "ads")
}

func validateAdvertisement(ad Advertisement) (Advertisement, error) {
	ad.ID = strings.TrimSpace(ad.ID)
	ad.Brand = strings.TrimSpace(ad.Brand)
	ad.Title = strings.TrimSpace(ad.Title)
	ad.Description = strings.TrimSpace(ad.Description)
	ad.ImageURL = strings.TrimSpace(ad.ImageURL)
	ad.ButtonText = strings.TrimSpace(ad.ButtonText)
	ad.PositionMode = strings.TrimSpace(ad.PositionMode)
	if ad.ID != "" && !validAdvertisementID(ad.ID) {
		return ad, fmt.Errorf("invalid advertisement id")
	}
	if ad.Title == "" || len([]rune(ad.Title)) > 80 || len([]rune(ad.Brand)) > 40 || len([]rune(ad.Description)) > 240 || len([]rune(ad.ButtonText)) > 24 {
		return ad, fmt.Errorf("invalid advertisement text")
	}
	if !strings.HasPrefix(ad.ImageURL, "/ads/") || !validDownloadName(filepath.Base(ad.ImageURL)) {
		return ad, fmt.Errorf("upload an advertisement image first")
	}
	target, err := cleanAdTargetURL(ad.TargetURL)
	if err != nil {
		return ad, err
	}
	ad.TargetURL = target
	if ad.ButtonText == "" {
		ad.ButtonText = "了解详情"
	}
	switch ad.PositionMode {
	case "auto", "after", "start", "end", "exclusive":
	default:
		ad.PositionMode = "auto"
	}
	if ad.DesktopAfter < 0 || ad.DesktopAfter > 10000 || ad.MobileAfter < 0 || ad.MobileAfter > 10000 || ad.Priority < -10000 || ad.Priority > 10000 {
		return ad, fmt.Errorf("invalid advertisement placement")
	}
	if ad.StartAt < 0 || ad.EndAt < 0 || ad.EndAt > 0 && ad.StartAt > 0 && ad.EndAt <= ad.StartAt {
		return ad, fmt.Errorf("advertisement end time must be after start time")
	}
	return ad, nil
}

func cleanAdTargetURL(value string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("target_url must be an absolute http or https URL")
	}
	return u.String(), nil
}
func randomAdImageName(ext string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]) + ext, nil
}

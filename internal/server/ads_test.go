package server

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sampleAdvertisement(title string) Advertisement {
	return Advertisement{Title: title, Description: "Test description", ImageURL: "/ads/test.png", TargetURL: "https://example.com/offer", ButtonText: "Details", Enabled: true, PositionMode: "auto"}
}

func TestAdvertisementScheduleBoundaries(t *testing.T) {
	now := time.Unix(1_000, 0)
	ad := sampleAdvertisement("Scheduled")
	ad.StartAt, ad.EndAt = 1_000, 2_000
	if !advertisementActive(ad, now) {
		t.Fatal("start boundary should be active")
	}
	if advertisementActive(ad, time.Unix(2_000, 0)) {
		t.Fatal("end boundary should be exclusive")
	}
	ad.Enabled = false
	if advertisementActive(ad, now) {
		t.Fatal("disabled advertisement should not be active")
	}
}

func TestAdvertisementRotationPreservesPriorityTiers(t *testing.T) {
	ads := []Advertisement{
		{ID: "ad_low_1", Priority: 1, UpdatedAt: 3},
		{ID: "ad_high", Priority: 9, UpdatedAt: 1},
		{ID: "ad_low_2", Priority: 1, UpdatedAt: 2},
	}
	sortAdvertisements(ads, "rotate", time.Unix(2*86400, 0))
	if ads[0].ID != "ad_high" {
		t.Fatalf("rotation changed priority tier order: %#v", ads)
	}
}

func TestJSONAdvertisementPersistenceOrderingAndStats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	low := sampleAdvertisement("Low")
	low.Priority = 1
	high := sampleAdvertisement("High")
	high.Priority = 9
	high.Recommended = true
	low, err = store.SaveAdvertisement(low)
	if err != nil {
		t.Fatal(err)
	}
	high, err = store.SaveAdvertisement(high)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.IncrementAdvertisementStat(high.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := store.IncrementAdvertisementStat(high.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateAdLayoutSettings(AdLayoutSettings{DesktopInterval: 5, MobileInterval: 4, ConflictStrategy: "rotate", RotationMode: "fixed"}); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	ads := reloaded.ListAdvertisements(true, time.Now())
	if len(ads) != 2 || ads[0].ID != high.ID {
		t.Fatalf("advertisement order = %#v", ads)
	}
	if ads[0].Impressions != 1 || ads[0].Clicks != 1 {
		t.Fatalf("stats = %#v", ads[0])
	}
	settings := reloaded.GetAdLayoutSettings()
	if settings.DesktopInterval != 5 || settings.MobileInterval != 4 || settings.ConflictStrategy != "rotate" {
		t.Fatalf("settings = %#v", settings)
	}
}

func TestSQLiteAdvertisementPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.db")
	store, err := NewSQLiteStore(path, "")
	if err != nil {
		t.Fatal(err)
	}
	ad, err := store.SaveAdvertisement(sampleAdvertisement("SQLite"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.IncrementAdvertisementStat(ad.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewSQLiteStore(path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	got, ok := reloaded.GetAdvertisement(ad.ID)
	if !ok || got.Title != "SQLite" || got.Clicks != 1 {
		t.Fatalf("reloaded advertisement = %#v, %v", got, ok)
	}
}

func TestAdminAdvertisementSaveAuthorizationAndPublicFiltering(t *testing.T) {
	s := newTestServer(t)
	body := `{"title":"Ad","description":"Description","image_url":"/ads/test.png","target_url":"https://example.com","enabled":true,"position_mode":"auto"}`
	guest := httptest.NewRecorder()
	s.handleAdminAdSave(guest, httptest.NewRequest(http.MethodPost, "/api/admin/ads/save", strings.NewReader(body)))
	if guest.Code != http.StatusUnauthorized {
		t.Fatalf("guest status = %d", guest.Code)
	}
	token, err := s.sessions.Create()
	if err != nil {
		t.Fatal(err)
	}
	req := adminRequestWithBody(http.MethodPost, "https://monitor.example.com/api/admin/ads/save", token, body)
	resp := httptest.NewRecorder()
	s.handleAdminAdSave(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("save status = %d body = %s", resp.Code, resp.Body.String())
	}
	var saved Advertisement
	decodeJSONResponse(t, resp, &saved)
	if saved.ID == "" {
		t.Fatal("saved advertisement missing id")
	}

	publicResp := httptest.NewRecorder()
	s.handleMarketAds(publicResp, httptest.NewRequest(http.MethodGet, "/api/market/ads", nil))
	var payload MarketAdsResponse
	decodeJSONResponse(t, publicResp, &payload)
	if len(payload.Ads) != 1 || payload.Ads[0].ID != saved.ID {
		t.Fatalf("public advertisements = %#v", payload.Ads)
	}
	if payload.Ads[0].TargetURL != "" {
		t.Fatalf("public advertisement leaked target url: %#v", payload.Ads[0])
	}
}

func TestAdminAdvertisementUploadValidation(t *testing.T) {
	s := newTestServer(t)
	token, err := s.sessions.Create()
	if err != nil {
		t.Fatal(err)
	}
	var imageData bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 32, 18))
	img.Set(0, 0, color.White)
	if err := png.Encode(&imageData, img); err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("image", "banner.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(imageData.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "https://monitor.example.com/api/admin/ads/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Host = "monitor.example.com"
	req.AddCookie(&http.Cookie{Name: "monitor_admin", Value: token})
	resp := httptest.NewRecorder()
	s.handleAdminAdUpload(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("upload status = %d body = %s", resp.Code, resp.Body.String())
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	imageURL, _ := payload["image_url"].(string)
	if !strings.HasPrefix(imageURL, "/ads/") {
		t.Fatalf("image url = %q", imageURL)
	}
}

func TestAdvertisementRedirectCountsClick(t *testing.T) {
	s := newTestServer(t)
	ad, err := s.store.SaveAdvertisement(sampleAdvertisement("Redirect"))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/r/ad/"+ad.ID, nil)
	resp := httptest.NewRecorder()
	s.handleAdvertisementRedirect(resp, req)
	if resp.Code != http.StatusFound || resp.Header().Get("Location") != ad.TargetURL {
		t.Fatalf("redirect = %d %q", resp.Code, resp.Header().Get("Location"))
	}
	got, _ := s.store.GetAdvertisement(ad.ID)
	if got.Clicks != 1 {
		t.Fatalf("clicks = %d", got.Clicks)
	}
}

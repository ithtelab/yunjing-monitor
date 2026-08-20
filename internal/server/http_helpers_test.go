package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

var staticHTMLAssetPattern = regexp.MustCompile(`(?:src|href)="(/[^"]+)"`)

func TestEmbeddedHTMLAssetsExist(t *testing.T) {
	index, err := staticFiles.ReadFile("web/dist/index.html")
	if err != nil {
		t.Fatal(err)
	}

	matches := staticHTMLAssetPattern.FindAllStringSubmatch(string(index), -1)
	if len(matches) == 0 {
		t.Fatal("index.html does not reference any static assets")
	}
	for _, match := range matches {
		assetPath := strings.TrimPrefix(strings.SplitN(match[1], "?", 2)[0], "/")
		if _, err := staticFiles.ReadFile("web/dist/" + assetPath); err != nil {
			t.Errorf("index.html references missing embedded asset %q: %v", assetPath, err)
		}
	}
}

func TestSocketURLUsesMatchingScheme(t *testing.T) {
	tests := []struct {
		name string
		base string
		want string
	}{
		{name: "https", base: "https://monitor.example.com/base/", want: "wss://monitor.example.com/base/ws"},
		{name: "http", base: "http://localhost:3000/", want: "ws://localhost:3000/ws"},
		{name: "host only", base: "monitor.example.com", want: "ws://monitor.example.com/ws"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := socketURL(tt.base); got != tt.want {
				t.Fatalf("socketURL(%q) = %q, want %q", tt.base, got, tt.want)
			}
		})
	}
}

func TestWriteJSONSetsContentTypeAndEncodesBody(t *testing.T) {
	resp := httptest.NewRecorder()
	writeJSON(resp, map[string]string{"ok": "true"})

	if got := resp.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q", got)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["ok"] != "true" {
		t.Fatalf("body = %#v", body)
	}
}

func TestMethodNotAllowedWritesStatus(t *testing.T) {
	resp := httptest.NewRecorder()
	methodNotAllowed(resp)

	if resp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusMethodNotAllowed)
	}
}

func TestShouldGzipStaticExtensions(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "index.html", want: true},
		{path: "assets/app.css", want: true},
		{path: "assets/app.js", want: true},
		{path: "config.json", want: true},
		{path: "logo.svg", want: true},
		{path: "font.woff2", want: false},
		{path: "image.png", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := shouldGzip(tt.path); got != tt.want {
				t.Fatalf("shouldGzip(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestSetStaticCacheHeaders(t *testing.T) {
	indexResp := httptest.NewRecorder()
	setStaticCache(indexResp, "index.html")
	if got := indexResp.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("index cache = %q", got)
	}

	assetResp := httptest.NewRecorder()
	setStaticCache(assetResp, "assets/app.js")
	if got := assetResp.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("asset cache = %q", got)
	}
}

func TestWithCORSAddsSecurityHeaders(t *testing.T) {
	handler := withCORS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), nil)
	req := httptest.NewRequest(http.MethodGet, "https://monitor.example.com/monitor", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	for name, want := range map[string]string{
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "DENY",
		"Referrer-Policy":           "strict-origin-when-cross-origin",
		"Strict-Transport-Security": "max-age=31536000",
	} {
		if got := resp.Header().Get(name); got != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
}

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/afficho/afficho-client/internal/config"
)

func testCORSServer(origins []string) *Server {
	cfg := config.Default()
	cfg.Security.CORSAllowedOrigins = origins
	return &Server{cfg: cfg}
}

func TestCORSNoConfigNoOrigin(t *testing.T) {
	s := testCORSServer(nil)
	handler := s.corsMiddleware(okHandler)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/status", http.NoBody)
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no CORS header, got %q", got)
	}
}

func TestCORSNoConfigWithOrigin(t *testing.T) {
	s := testCORSServer(nil)
	handler := s.corsMiddleware(okHandler)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/status", http.NoBody)
	r.Header.Set("Origin", "https://evil.com")
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no CORS header with no config, got %q", got)
	}
}

func TestCORSMatchingOrigin(t *testing.T) {
	s := testCORSServer([]string{"https://dashboard.example.com"})
	handler := s.corsMiddleware(okHandler)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/status", http.NoBody)
	r.Header.Set("Origin", "https://dashboard.example.com")
	handler.ServeHTTP(w, r)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://dashboard.example.com" {
		t.Errorf("expected matching origin, got %q", got)
	}
	if got := w.Header().Get("Vary"); got != "Origin" {
		t.Errorf("expected Vary: Origin, got %q", got)
	}
}

func TestCORSNonMatchingOrigin(t *testing.T) {
	s := testCORSServer([]string{"https://dashboard.example.com"})
	handler := s.corsMiddleware(okHandler)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/status", http.NoBody)
	r.Header.Set("Origin", "https://evil.com")
	handler.ServeHTTP(w, r)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no CORS header for non-matching origin, got %q", got)
	}
}

func TestCORSWildcard(t *testing.T) {
	s := testCORSServer([]string{"*"})
	handler := s.corsMiddleware(okHandler)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/status", http.NoBody)
	r.Header.Set("Origin", "https://anything.com")
	handler.ServeHTTP(w, r)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://anything.com" {
		t.Errorf("expected wildcard to match, got %q", got)
	}
}

func TestCORSPreflight(t *testing.T) {
	s := testCORSServer([]string{"https://dashboard.example.com"})
	handler := s.corsMiddleware(okHandler)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("OPTIONS", "/api/v1/content", http.NoBody)
	r.Header.Set("Origin", "https://dashboard.example.com")
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204 for preflight, got %d", w.Code)
	}
	if got := w.Header().Get("Access-Control-Max-Age"); got != "86400" {
		t.Errorf("expected Max-Age 86400, got %q", got)
	}
}

func TestMatchOrigin(t *testing.T) {
	tests := []struct {
		origin  string
		allowed []string
		expect  bool
	}{
		{"https://a.com", []string{"https://a.com"}, true},
		{"https://b.com", []string{"https://a.com"}, false},
		{"https://any.com", []string{"*"}, true},
		{"https://a.com", []string{"https://a.com", "https://b.com"}, true},
		{"https://c.com", []string{"https://a.com", "https://b.com"}, false},
		{"https://x.com", nil, false},
		{"https://x.com", []string{}, false},
	}

	for _, tt := range tests {
		got := matchOrigin(tt.origin, tt.allowed)
		if got != tt.expect {
			t.Errorf("matchOrigin(%q, %v) = %v, want %v", tt.origin, tt.allowed, got, tt.expect)
		}
	}
}

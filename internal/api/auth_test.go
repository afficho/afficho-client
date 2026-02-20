package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/afficho/afficho-client/internal/config"
)

// testServer creates a minimal Server for auth tests (no DB or scheduler needed).
func testAuthServer(password string) *Server {
	cfg := config.Default()
	cfg.Admin.Password = password
	return &Server{cfg: cfg}
}

// okHandler is a simple handler that returns 200.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
})

func TestRequireAuthEmptyPassword(t *testing.T) {
	s := testAuthServer("")
	handler := s.requireAuth(okHandler)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/content", http.NoBody)
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with empty password, got %d", w.Code)
	}
}

func TestRequireAuthNoCredentialsAPI(t *testing.T) {
	s := testAuthServer("secret")
	handler := s.requireAuth(okHandler)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/content", http.NoBody)
	r.Header.Set("Accept", "application/json")
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for unauthenticated API request, got %d", w.Code)
	}
}

func TestRequireAuthNoCredentialsBrowser(t *testing.T) {
	s := testAuthServer("secret")
	handler := s.requireAuth(okHandler)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/admin", http.NoBody)
	r.Header.Set("Accept", "text/html,application/xhtml+xml")
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect for browser, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc != "/admin/login" {
		t.Errorf("expected redirect to /admin/login, got %q", loc)
	}
}

func TestRequireAuthValidBasicAuth(t *testing.T) {
	s := testAuthServer("secret")
	handler := s.requireAuth(okHandler)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/content", http.NoBody)
	r.SetBasicAuth("admin", "secret")
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with valid Basic Auth, got %d", w.Code)
	}

	// Should set a session cookie.
	cookies := w.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == sessionCookieName {
			found = true
			if !c.HttpOnly {
				t.Error("expected HttpOnly cookie")
			}
		}
	}
	if !found {
		t.Error("expected session cookie to be set")
	}
}

func TestRequireAuthInvalidBasicAuth(t *testing.T) {
	s := testAuthServer("secret")
	handler := s.requireAuth(okHandler)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/content", http.NoBody)
	r.SetBasicAuth("admin", "wrong")
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with wrong password, got %d", w.Code)
	}
}

func TestRequireAuthValidSessionCookie(t *testing.T) {
	s := testAuthServer("secret")
	handler := s.requireAuth(okHandler)

	token := s.signSession("secret")
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/content", http.NoBody)
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with valid session, got %d", w.Code)
	}
}

func TestRequireAuthTamperedSessionCookie(t *testing.T) {
	s := testAuthServer("secret")
	handler := s.requireAuth(okHandler)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/content", http.NoBody)
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "12345:tampered"})
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with tampered session, got %d", w.Code)
	}
}

func TestSignVerifyRoundtrip(t *testing.T) {
	s := testAuthServer("secret")
	token := s.signSession("secret")
	if !s.verifySession(token, "secret") {
		t.Error("expected valid session after sign")
	}
}

func TestVerifySessionWrongPassword(t *testing.T) {
	s := testAuthServer("secret")
	token := s.signSession("secret")
	if s.verifySession(token, "other-password") {
		t.Error("session should not verify with different password")
	}
}

func TestVerifySessionMalformed(t *testing.T) {
	s := testAuthServer("secret")

	tests := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"no colon", "nocolon"},
		{"bad expiry", "notanumber:abc123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if s.verifySession(tt.token, "secret") {
				t.Errorf("expected false for malformed token %q", tt.token)
			}
		})
	}
}

func TestIsBrowserRequest(t *testing.T) {
	tests := []struct {
		accept string
		expect bool
	}{
		{"text/html,application/xhtml+xml", true},
		{"text/html", true},
		{"application/json", false},
		{"*/*", false},
		{"", false},
	}
	for _, tt := range tests {
		r := httptest.NewRequest("GET", "/", http.NoBody)
		if tt.accept != "" {
			r.Header.Set("Accept", tt.accept)
		}
		if got := isBrowserRequest(r); got != tt.expect {
			t.Errorf("Accept=%q: got %v, want %v", tt.accept, got, tt.expect)
		}
	}
}

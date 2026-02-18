package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	sessionCookieName = "afficho_session"
	sessionMaxAge     = 24 * time.Hour
)

// requireAuth returns chi middleware that protects routes with HTTP Basic Auth.
// If the admin password is empty, all requests are allowed through.
// After successful Basic Auth the middleware sets a signed session cookie
// so the browser does not re-prompt on every page load.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		password := s.cfg.Admin.Password
		if password == "" {
			next.ServeHTTP(w, r)
			return
		}

		// 1. Check session cookie first.
		if cookie, err := r.Cookie(sessionCookieName); err == nil {
			if s.verifySession(cookie.Value, password) {
				next.ServeHTTP(w, r)
				return
			}
		}

		// 2. Fall back to HTTP Basic Auth.
		_, pass, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(pass), []byte(password)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="Afficho Admin"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// 3. Auth succeeded — issue a session cookie to avoid re-prompting.
		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookieName,
			Value:    s.signSession(password),
			Path:     "/",
			MaxAge:   int(sessionMaxAge.Seconds()),
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})

		next.ServeHTTP(w, r)
	})
}

// signSession creates a token of the form "expiry_unix:hmac_hex".
// The HMAC is keyed with the admin password so rotating the password
// invalidates all existing sessions.
func (s *Server) signSession(password string) string {
	expiry := time.Now().Add(sessionMaxAge).Unix()
	payload := strconv.FormatInt(expiry, 10)

	mac := hmac.New(sha256.New, []byte(password))
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))

	return fmt.Sprintf("%s:%s", payload, sig)
}

// verifySession checks the "expiry_unix:hmac_hex" token.
// Returns false if expired, malformed, or the signature doesn't match.
func (s *Server) verifySession(token, password string) bool {
	parts := strings.SplitN(token, ":", 2)
	if len(parts) != 2 {
		return false
	}

	payload, sig := parts[0], parts[1]

	expiry, err := strconv.ParseInt(payload, 10, 64)
	if err != nil {
		return false
	}
	if time.Now().Unix() > expiry {
		return false
	}

	mac := hmac.New(sha256.New, []byte(password))
	mac.Write([]byte(payload))
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(sig), []byte(expected))
}

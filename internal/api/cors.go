package api

import "net/http"

// corsMiddleware handles CORS for the REST API based on the configured
// allowed origins. When no origins are configured (the default), no CORS
// headers are sent and the browser's same-origin policy blocks cross-origin
// requests. This is the most secure default for a local-network daemon.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			// Not a cross-origin request.
			next.ServeHTTP(w, r)
			return
		}

		allowed := s.cfg.Security.CORSAllowedOrigins
		if len(allowed) == 0 {
			// No configured origins — same-origin only.
			next.ServeHTTP(w, r)
			return
		}

		if matchOrigin(origin, allowed) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
		}

		// Handle preflight.
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Max-Age", "86400")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// matchOrigin checks whether origin appears in the allowed list.
// A single entry of "*" matches any origin.
func matchOrigin(origin string, allowed []string) bool {
	for _, a := range allowed {
		if a == "*" || a == origin {
			return true
		}
	}
	return false
}

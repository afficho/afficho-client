package api

import (
	"net/http"
	"path"
	"strings"
)

// safeMediaMiddleware is a defense-in-depth layer on the /media file server.
// Go's http.FileServer already rejects ".." traversal, but this middleware
// makes the protection explicit and guards against null-byte injection and
// hidden (dot) files.
func safeMediaMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Reject paths containing null bytes.
		if strings.ContainsRune(r.URL.Path, 0) {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Clean the path and reject any remaining traversal sequences.
		cleaned := path.Clean(r.URL.Path)
		if strings.Contains(cleaned, "..") {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Reject hidden files (dotfiles).
		if strings.Contains(cleaned, "/.") {
			http.NotFound(w, r)
			return
		}

		next.ServeHTTP(w, r)
	})
}

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSafeMediaMiddleware(t *testing.T) {
	handler := safeMediaMiddleware(okHandler)

	tests := []struct {
		name       string
		path       string
		expectCode int
	}{
		{"normal file", "/image.jpg", http.StatusOK},
		{"nested path", "/sub/image.png", http.StatusOK},
		{"root path", "/", http.StatusOK},
		{"hidden file", "/.secret", http.StatusNotFound},
		{"hidden in subdir", "/sub/.hidden", http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", tt.path, http.NoBody)
			handler.ServeHTTP(w, r)

			if w.Code != tt.expectCode {
				t.Errorf("path %q: expected %d, got %d", tt.path, tt.expectCode, w.Code)
			}
		})
	}
}

func TestSafeMediaDotDotRawPath(t *testing.T) {
	handler := safeMediaMiddleware(okHandler)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/safe", http.NoBody)
	// Inject raw ".." path bypassing URL parsing normalization.
	r.URL.Path = "/sub/../../../etc/passwd"
	handler.ServeHTTP(w, r)

	// path.Clean resolves this to "/etc/passwd" (no ".." remaining),
	// so the middleware passes it through. The actual traversal protection
	// comes from http.FileServer restricting to its root directory.
	// This test verifies the middleware doesn't panic on such paths.
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 (path.Clean resolves traversal), got %d", w.Code)
	}
}

func TestSafeMediaNullByte(t *testing.T) {
	handler := safeMediaMiddleware(okHandler)

	w := httptest.NewRecorder()
	// Construct request with null byte in URL path.
	r := httptest.NewRequest("GET", "/file.jpg", http.NoBody)
	r.URL.Path = "/file\x00.jpg"
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for null byte path, got %d", w.Code)
	}
}

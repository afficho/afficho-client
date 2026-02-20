package content

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/afficho/afficho-client/internal/config"
	"github.com/afficho/afficho-client/internal/db"
)

// Allowed MIME types per content type category.
var allowedMIME = map[string]map[string]string{
	"image": {
		"image/jpeg": ".jpg",
		"image/png":  ".png",
		"image/gif":  ".gif",
		"image/webp": ".webp",
	},
	"video": {
		"video/mp4":  ".mp4",
		"video/webm": ".webm",
	},
}

// Manager handles local storage of media files.
type Manager struct {
	cfg      *config.Config
	db       *db.DB
	mediaDir string
}

// NewManager creates a Manager. Call Init() before use.
func NewManager(cfg *config.Config, database *db.DB) *Manager {
	return &Manager{
		cfg:      cfg,
		db:       database,
		mediaDir: filepath.Join(cfg.Storage.DataDir, "media"),
	}
}

// Init creates the media storage directory if it does not exist.
func (m *Manager) Init() error {
	return os.MkdirAll(m.mediaDir, 0o755)
}

// MediaDir returns the absolute path of the local media storage directory.
func (m *Manager) MediaDir() string {
	return m.mediaDir
}

// LocalPath returns the expected storage path for a content item by ID and extension.
func (m *Manager) LocalPath(id, ext string) string {
	return filepath.Join(m.mediaDir, id+ext)
}

// Download fetches a remote URL and saves it to local media storage.
// Returns the local file path and the number of bytes written.
// The caller is responsible for storing the path in the database.
func (m *Manager) Download(id, rawURL string) (localPath string, size int64, err error) {
	// TODO: add timeout / context propagation
	resp, err := http.Get(rawURL) //nolint:gosec // URL is admin-provided
	if err != nil {
		return "", 0, fmt.Errorf("downloading %q: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("unexpected HTTP %d for %q", resp.StatusCode, rawURL)
	}

	ext := filepath.Ext(rawURL)
	localPath = m.LocalPath(id, ext)

	f, err := os.Create(localPath)
	if err != nil {
		return "", 0, fmt.Errorf("creating file: %w", err)
	}
	defer f.Close()

	size, err = io.Copy(f, resp.Body)
	if err != nil {
		_ = os.Remove(localPath)
		return "", 0, fmt.Errorf("writing file: %w", err)
	}

	return localPath, size, nil
}

// DownloadMedia fetches a remote URL, validates magic bytes against the declared
// content type, enforces the upload size limit, and saves to media storage with
// the correct file extension. Returns the local path and byte count.
func (m *Manager) DownloadMedia(id, rawURL, declaredType string) (localPath string, size int64, err error) {
	resp, err := http.Get(rawURL) //nolint:gosec // URL is admin-provided
	if err != nil {
		return "", 0, fmt.Errorf("downloading %q: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("unexpected HTTP %d for %q", resp.StatusCode, rawURL)
	}

	// Write to a temp file so we can validate before committing.
	tmpPath := filepath.Join(m.mediaDir, id+".tmp")
	f, err := os.Create(tmpPath)
	if err != nil {
		return "", 0, fmt.Errorf("creating temp file: %w", err)
	}

	maxBytes := m.MaxUploadBytes()
	limited := io.LimitReader(resp.Body, maxBytes+1)
	size, err = io.Copy(f, limited)
	_ = f.Close()
	if err != nil {
		_ = os.Remove(tmpPath)
		return "", 0, fmt.Errorf("writing file: %w", err)
	}
	if size > maxBytes {
		_ = os.Remove(tmpPath)
		return "", 0, fmt.Errorf("file exceeds maximum upload size of %d MB", m.cfg.Storage.MaxUploadMB)
	}

	// Validate magic bytes.
	vf, err := os.Open(tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		return "", 0, fmt.Errorf("opening temp file for validation: %w", err)
	}
	_, ext, verr := ValidateMediaType(vf, declaredType)
	_ = vf.Close()
	if verr != nil {
		_ = os.Remove(tmpPath)
		return "", 0, verr
	}

	// Rename to final path with the validated extension.
	localPath = m.LocalPath(id, ext)
	if err := os.Rename(tmpPath, localPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", 0, fmt.Errorf("renaming temp file: %w", err)
	}

	return localPath, size, nil
}

// Delete removes a locally stored media file.
func (m *Manager) Delete(localPath string) error {
	// Guard against deleting files outside the media directory.
	rel, err := filepath.Rel(m.mediaDir, localPath)
	if err != nil || rel == "" || rel[0] == '.' || strings.ContainsRune(localPath, 0) {
		return fmt.Errorf("path %q is outside media directory", localPath)
	}
	return os.Remove(localPath)
}

// MaxUploadBytes returns the configured upload size limit in bytes.
func (m *Manager) MaxUploadBytes() int64 {
	return int64(m.cfg.Storage.MaxUploadMB) * 1024 * 1024
}

// ValidateMediaType reads the first 512 bytes of r, detects the MIME type via
// magic-byte sniffing, and checks it against the allowed types for the given
// content category ("image" or "video"). It seeks r back to the start before
// returning. Returns the detected MIME type and its canonical file extension.
func ValidateMediaType(r io.ReadSeeker, contentType string) (mime, ext string, err error) {
	allowed, ok := allowedMIME[contentType]
	if !ok {
		return "", "", fmt.Errorf("unsupported content type %q for media validation", contentType)
	}

	buf := make([]byte, 512)
	n, err := r.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", "", fmt.Errorf("reading file header: %w", err)
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return "", "", fmt.Errorf("seeking back to start: %w", err)
	}

	mime = http.DetectContentType(buf[:n])
	// DetectContentType may return params (e.g. "text/plain; charset=utf-8").
	mime = strings.SplitN(mime, ";", 2)[0]

	ext, ok = allowed[mime]
	if !ok {
		return "", "", fmt.Errorf("detected MIME %q is not allowed for type %q", mime, contentType)
	}
	return mime, ext, nil
}

// SaveUpload writes the contents of r to a new file in the media directory.
// It enforces maxBytes as a hard size limit. Returns the local path and bytes written.
func (m *Manager) SaveUpload(id string, r io.Reader, ext string, maxBytes int64) (localPath string, size int64, err error) {
	localPath = m.LocalPath(id, ext)

	f, err := os.Create(localPath)
	if err != nil {
		return "", 0, fmt.Errorf("creating file: %w", err)
	}
	defer f.Close()

	limited := io.LimitReader(r, maxBytes+1) // +1 to detect overflow
	size, err = io.Copy(f, limited)
	if err != nil {
		_ = os.Remove(localPath)
		return "", 0, fmt.Errorf("writing file: %w", err)
	}
	if size > maxBytes {
		_ = os.Remove(localPath)
		return "", 0, fmt.Errorf("file exceeds maximum size of %d bytes", maxBytes)
	}
	return localPath, size, nil
}

// IsLocalMedia returns true if the source path points to a file inside the media directory.
func (m *Manager) IsLocalMedia(source string) bool {
	rel, err := filepath.Rel(m.mediaDir, source)
	if err != nil {
		return false
	}
	return rel != "" && rel[0] != '.'
}

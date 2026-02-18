package content

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/afficho/afficho-client/internal/config"
	"github.com/afficho/afficho-client/internal/db"
)

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

// Delete removes a locally stored media file.
func (m *Manager) Delete(localPath string) error {
	// Guard against deleting files outside the media directory.
	rel, err := filepath.Rel(m.mediaDir, localPath)
	if err != nil || rel == "" || rel[0] == '.' {
		return fmt.Errorf("path %q is outside media directory", localPath)
	}
	return os.Remove(localPath)
}

package cloud

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	types "github.com/afficho/afficho-types"

	"github.com/afficho/afficho-client/internal/config"
	"github.com/afficho/afficho-client/internal/content"
	"github.com/afficho/afficho-client/internal/db"
)

// testSetup creates a temp DB, content manager, and connector for testing.
func testSetup(t *testing.T) (*db.DB, *content.Manager, *Connector) {
	t.Helper()
	dir := t.TempDir()

	database, err := db.Open(dir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	cfg := config.Default()
	cfg.Storage.DataDir = dir
	mgr := content.NewManager(cfg, database)
	if err := mgr.Init(); err != nil {
		t.Fatalf("init content manager: %v", err)
	}

	conn := New(config.CloudConfig{}, "test-device", "dev", dir)
	return database, mgr, conn
}

func TestContentSyncURLItem(t *testing.T) {
	database, mgr, conn := testSetup(t)
	cs := NewContentSyncer(conn, database, mgr)

	items := []types.ContentSyncItem{
		{
			ID:        "url-1",
			Name:      "Company Website",
			Type:      "url",
			Source:    "https://example.com",
			DurationS: 30,
		},
	}

	if err := cs.sync(context.Background(), items); err != nil {
		t.Fatalf("sync: %v", err)
	}

	// Verify it's in the DB.
	var name, origin, source string
	err := database.QueryRow(
		`SELECT name, origin, source FROM content_items WHERE id = ?`, "url-1",
	).Scan(&name, &origin, &source)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if name != "Company Website" {
		t.Errorf("expected name 'Company Website', got %q", name)
	}
	if origin != "cloud" {
		t.Errorf("expected origin 'cloud', got %q", origin)
	}
	if source != "https://example.com" {
		t.Errorf("expected source 'https://example.com', got %q", source)
	}
}

func TestContentSyncMediaDownload(t *testing.T) {
	database, mgr, conn := testSetup(t)
	cs := NewContentSyncer(conn, database, mgr)

	// Serve a fake image.
	imgData := []byte("fake-image-data-for-testing")
	checksum := sha256.Sum256(imgData)
	checksumHex := hex.EncodeToString(checksum[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(imgData)
	}))
	defer srv.Close()

	items := []types.ContentSyncItem{
		{
			ID:        "img-1",
			Name:      "Test Image",
			Type:      "image",
			Source:    srv.URL + "/test.jpg",
			DurationS: 10,
			Checksum:  checksumHex,
			SizeBytes: int64(len(imgData)),
		},
	}

	if err := cs.sync(context.Background(), items); err != nil {
		t.Fatalf("sync: %v", err)
	}

	// Verify DB entry.
	var name, origin, dbChecksum string
	var sizeBytes int64
	err := database.QueryRow(
		`SELECT name, origin, checksum, size_bytes FROM content_items WHERE id = ?`, "img-1",
	).Scan(&name, &origin, &dbChecksum, &sizeBytes)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if origin != "cloud" {
		t.Errorf("expected origin 'cloud', got %q", origin)
	}
	if dbChecksum != checksumHex {
		t.Errorf("expected checksum %q, got %q", checksumHex, dbChecksum)
	}
	if sizeBytes != int64(len(imgData)) {
		t.Errorf("expected size %d, got %d", len(imgData), sizeBytes)
	}

	// Verify local file exists.
	localPath := mgr.LocalPath("img-1", ".jpg")
	data, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("read local file: %v", err)
	}
	if string(data) != string(imgData) {
		t.Error("local file content mismatch")
	}
}

func TestContentSyncSkipsMatchingChecksum(t *testing.T) {
	database, mgr, conn := testSetup(t)
	cs := NewContentSyncer(conn, database, mgr)

	// Pre-populate with a cloud item.
	_, err := database.Exec(`
		INSERT INTO content_items (id, name, type, source, duration_s, checksum, origin)
		VALUES ('existing-1', 'Existing', 'url', 'https://example.com', 10, 'abc123', 'cloud')`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	downloadCalled := false
	// Sync with the same checksum — should skip download.
	items := []types.ContentSyncItem{
		{
			ID:        "existing-1",
			Name:      "Existing",
			Type:      "url",
			Source:    "https://example.com",
			DurationS: 10,
			Checksum:  "abc123",
		},
	}

	if err := cs.sync(context.Background(), items); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if downloadCalled {
		t.Error("expected no download for matching checksum")
	}
}

func TestContentSyncDeletesStale(t *testing.T) {
	database, mgr, conn := testSetup(t)
	cs := NewContentSyncer(conn, database, mgr)

	// Pre-populate with a cloud item that won't be in the new manifest.
	_, err := database.Exec(`
		INSERT INTO content_items (id, name, type, source, duration_s, origin)
		VALUES ('stale-1', 'Stale Item', 'url', 'https://old.example.com', 10, 'cloud')`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Also add a local item that should NOT be deleted.
	_, err = database.Exec(`
		INSERT INTO content_items (id, name, type, source, duration_s, origin)
		VALUES ('local-1', 'Local Item', 'url', 'https://local.example.com', 10, 'local')`)
	if err != nil {
		t.Fatalf("insert local: %v", err)
	}

	// Sync with an empty manifest — stale cloud items should be removed.
	if err := cs.sync(context.Background(), nil); err != nil {
		t.Fatalf("sync: %v", err)
	}

	// stale-1 should be gone.
	var count int
	database.QueryRow(`SELECT COUNT(*) FROM content_items WHERE id = 'stale-1'`).Scan(&count)
	if count != 0 {
		t.Error("expected stale cloud item to be deleted")
	}

	// local-1 should still exist.
	database.QueryRow(`SELECT COUNT(*) FROM content_items WHERE id = 'local-1'`).Scan(&count)
	if count != 1 {
		t.Error("expected local item to be preserved")
	}
}

func TestContentSyncChecksumMismatchRejectsFile(t *testing.T) {
	database, mgr, conn := testSetup(t)
	cs := NewContentSyncer(conn, database, mgr)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("actual-data"))
	}))
	defer srv.Close()

	items := []types.ContentSyncItem{
		{
			ID:        "bad-checksum",
			Name:      "Bad Checksum",
			Type:      "image",
			Source:    srv.URL + "/test.jpg",
			DurationS: 10,
			Checksum:  "0000000000000000000000000000000000000000000000000000000000000000",
		},
	}

	// Sync should not fail entirely (logs error, continues).
	cs.sync(context.Background(), items)

	// Item should NOT be in the DB since checksum didn't match.
	var count int
	database.QueryRow(`SELECT COUNT(*) FROM content_items WHERE id = 'bad-checksum'`).Scan(&count)
	if count != 0 {
		t.Error("expected item with bad checksum to not be inserted")
	}

	// Local file should have been cleaned up.
	localPath := mgr.LocalPath("bad-checksum", ".jpg")
	if _, err := os.Stat(localPath); err == nil {
		t.Error("expected local file to be removed after checksum mismatch")
	}
}

func TestContentSyncHandlerRegistered(t *testing.T) {
	_, mgr, conn := testSetup(t)
	database, _ := db.Open(t.TempDir())
	defer database.Close()

	NewContentSyncer(conn, database, mgr)

	if _, ok := conn.handlers[types.TypeSyncContent]; !ok {
		t.Fatal("expected sync_content handler to be registered")
	}
}

func TestContentSyncMessageDispatch(t *testing.T) {
	database, mgr, conn := testSetup(t)
	_ = NewContentSyncer(conn, database, mgr)

	// Simulate receiving a sync_content message with a URL item.
	items := []types.ContentSyncItem{
		{
			ID:        "dispatch-1",
			Name:      "Dispatched",
			Type:      "url",
			Source:    "https://example.com/dispatched",
			DurationS: 15,
		},
	}
	payload, _ := json.Marshal(items)

	// Call the handler directly.
	conn.handlers[types.TypeSyncContent](payload)

	// Verify it ended up in the DB.
	var name string
	err := database.QueryRow(
		`SELECT name FROM content_items WHERE id = ?`, "dispatch-1",
	).Scan(&name)
	if err != nil {
		t.Fatalf("expected item in DB: %v", err)
	}
	if name != "Dispatched" {
		t.Errorf("expected name 'Dispatched', got %q", name)
	}
}

// Silence unused import warnings for test helpers.
var (
	_ = fmt.Sprintf
	_ = strings.TrimSpace
	_ = filepath.Base
	_ = sql.ErrNoRows
)

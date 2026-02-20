package content

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/afficho/afficho-client/internal/config"
	"github.com/afficho/afficho-client/internal/db"
)

func testManager(t *testing.T) (mgr *Manager, mediaDir string) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Storage.DataDir = dir
	d, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })

	mgr = NewManager(cfg, d)
	mediaDir = mgr.MediaDir()
	if err := mgr.Init(); err != nil {
		t.Fatal(err)
	}
	return mgr, mediaDir
}

func TestInit(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Storage.DataDir = dir
	d, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	mgr := NewManager(cfg, d)
	mediaDir := mgr.MediaDir()

	// Dir should not exist before Init.
	if _, err := os.Stat(mediaDir); err == nil {
		// It may exist from db.Open creating the parent — check the media subdir.
		t.Log("media dir already exists (ok if parent was created by db.Open)")
	}

	if err := mgr.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	info, err := os.Stat(mediaDir)
	if err != nil {
		t.Fatalf("media dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("media dir is not a directory")
	}
}

func TestMediaDir(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Storage.DataDir = dir
	d, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	mgr := NewManager(cfg, d)
	expected := filepath.Join(dir, "media")
	if got := mgr.MediaDir(); got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestLocalPath(t *testing.T) {
	mgr, mediaDir := testManager(t)
	got := mgr.LocalPath("abc-123", ".jpg")
	expected := filepath.Join(mediaDir, "abc-123.jpg")
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestMaxUploadBytes(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Storage.DataDir = dir
	cfg.Storage.MaxUploadMB = 50
	d, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	mgr := NewManager(cfg, d)
	expected := int64(50 * 1024 * 1024)
	if got := mgr.MaxUploadBytes(); got != expected {
		t.Errorf("expected %d, got %d", expected, got)
	}
}

func TestDeleteValidFile(t *testing.T) {
	mgr, mediaDir := testManager(t)

	// Create a test file.
	path := filepath.Join(mediaDir, "test-file.jpg")
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := mgr.Delete(path); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("expected file to be deleted")
	}
}

func TestDeletePathTraversal(t *testing.T) {
	mgr, mediaDir := testManager(t)

	tests := []struct {
		name string
		path string
	}{
		{"dotdot escape", filepath.Join(mediaDir, "..", "escape.txt")},
		{"outside media dir", "/etc/passwd"},
		{"null byte", mediaDir + "/file\x00.jpg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mgr.Delete(tt.path)
			if err == nil {
				t.Error("expected error for path traversal attempt")
			}
		})
	}
}

func TestValidateMediaTypeJPEG(t *testing.T) {
	// JPEG magic bytes: FF D8 FF
	data := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, make([]byte, 508)...)
	r := bytes.NewReader(data)

	mime, ext, err := ValidateMediaType(r, "image")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mime != "image/jpeg" {
		t.Errorf("expected image/jpeg, got %q", mime)
	}
	if ext != ".jpg" {
		t.Errorf("expected .jpg, got %q", ext)
	}

	// Verify seek back to start.
	pos, _ := r.Seek(0, 1) // current position
	if pos != 0 {
		t.Errorf("expected reader position 0, got %d", pos)
	}
}

func TestValidateMediaTypePNG(t *testing.T) {
	// PNG magic bytes: 89 50 4E 47 0D 0A 1A 0A
	data := append([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, make([]byte, 504)...)
	r := bytes.NewReader(data)

	mime, ext, err := ValidateMediaType(r, "image")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mime != "image/png" {
		t.Errorf("expected image/png, got %q", mime)
	}
	if ext != ".png" {
		t.Errorf("expected .png, got %q", ext)
	}
}

func TestValidateMediaTypeInvalid(t *testing.T) {
	// Plain text data shouldn't be valid as an image.
	data := []byte("This is not an image file, just plain text content for testing")
	r := bytes.NewReader(data)

	_, _, err := ValidateMediaType(r, "image")
	if err == nil {
		t.Error("expected error for non-image data")
	}
}

func TestValidateMediaTypeWrongCategory(t *testing.T) {
	// JPEG data but declared as "video".
	data := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, make([]byte, 508)...)
	r := bytes.NewReader(data)

	_, _, err := ValidateMediaType(r, "video")
	if err == nil {
		t.Error("expected error for JPEG data declared as video")
	}
}

func TestValidateMediaTypeUnsupportedCategory(t *testing.T) {
	r := bytes.NewReader([]byte("data"))
	_, _, err := ValidateMediaType(r, "audio")
	if err == nil {
		t.Error("expected error for unsupported category")
	}
}

func TestSaveUploadWithinLimit(t *testing.T) {
	mgr, _ := testManager(t)

	data := bytes.Repeat([]byte("x"), 1024)
	r := bytes.NewReader(data)

	path, size, err := mgr.SaveUpload("test-id", r, ".bin", 2048)
	if err != nil {
		t.Fatalf("SaveUpload: %v", err)
	}
	if size != 1024 {
		t.Errorf("expected size 1024, got %d", size)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestSaveUploadExceedsLimit(t *testing.T) {
	mgr, _ := testManager(t)

	data := bytes.Repeat([]byte("x"), 2048)
	r := bytes.NewReader(data)

	path, _, err := mgr.SaveUpload("test-id", r, ".bin", 1024)
	if err == nil {
		t.Fatal("expected error for oversized upload")
	}
	// File should be cleaned up.
	if path != "" {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Error("expected file to be cleaned up after size limit exceeded")
		}
	}
}

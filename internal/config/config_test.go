package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default()

	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("expected host 0.0.0.0, got %q", cfg.Server.Host)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("expected port 8080, got %d", cfg.Server.Port)
	}
	if !cfg.Display.LaunchBrowser {
		t.Error("expected LaunchBrowser true")
	}
	if cfg.Display.Browser != "auto" {
		t.Errorf("expected browser auto, got %q", cfg.Display.Browser)
	}
	if cfg.Display.Platform != "auto" {
		t.Errorf("expected platform auto, got %q", cfg.Display.Platform)
	}
	if cfg.Storage.DataDir != "/var/lib/afficho" {
		t.Errorf("expected data dir /var/lib/afficho, got %q", cfg.Storage.DataDir)
	}
	if cfg.Storage.MaxCacheGB != 10 {
		t.Errorf("expected max cache 10, got %d", cfg.Storage.MaxCacheGB)
	}
	if cfg.Storage.MaxUploadMB != 100 {
		t.Errorf("expected max upload 100, got %d", cfg.Storage.MaxUploadMB)
	}
	if cfg.Security.UploadConcurrencyLimit != 2 {
		t.Errorf("expected upload concurrency 2, got %d", cfg.Security.UploadConcurrencyLimit)
	}
	if cfg.Admin.Password != "" {
		t.Errorf("expected empty password, got %q", cfg.Admin.Password)
	}
}

func TestServerAddr(t *testing.T) {
	cfg := Default()
	if got := cfg.ServerAddr(); got != "0.0.0.0:8080" {
		t.Errorf("expected 0.0.0.0:8080, got %q", got)
	}

	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = 9090
	if got := cfg.ServerAddr(); got != "127.0.0.1:9090" {
		t.Errorf("expected 127.0.0.1:9090, got %q", got)
	}
}

func TestLoadMissingFile(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.toml")
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	// Should return defaults.
	if cfg.Server.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.Server.Port)
	}
}

func TestLoadValidTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	toml := `
[server]
host = "127.0.0.1"
port = 9000

[admin]
password = "secret"

[storage]
max_upload_mb = 50

[security]
cors_allowed_origins = ["https://example.com"]
upload_concurrency_limit = 5
`
	if err := os.WriteFile(path, []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("expected host 127.0.0.1, got %q", cfg.Server.Host)
	}
	if cfg.Server.Port != 9000 {
		t.Errorf("expected port 9000, got %d", cfg.Server.Port)
	}
	if cfg.Admin.Password != "secret" {
		t.Errorf("expected password secret, got %q", cfg.Admin.Password)
	}
	if cfg.Storage.MaxUploadMB != 50 {
		t.Errorf("expected max upload 50, got %d", cfg.Storage.MaxUploadMB)
	}
	if len(cfg.Security.CORSAllowedOrigins) != 1 || cfg.Security.CORSAllowedOrigins[0] != "https://example.com" {
		t.Errorf("expected CORS origins [https://example.com], got %v", cfg.Security.CORSAllowedOrigins)
	}
	if cfg.Security.UploadConcurrencyLimit != 5 {
		t.Errorf("expected upload concurrency 5, got %d", cfg.Security.UploadConcurrencyLimit)
	}
}

func TestLoadPartialTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	// Only override port; everything else should keep defaults.
	toml := `
[server]
port = 3000
`
	if err := os.WriteFile(path, []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Server.Port != 3000 {
		t.Errorf("expected port 3000, got %d", cfg.Server.Port)
	}
	// Defaults preserved for unset fields.
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("expected default host, got %q", cfg.Server.Host)
	}
	if cfg.Storage.MaxUploadMB != 100 {
		t.Errorf("expected default max upload 100, got %d", cfg.Storage.MaxUploadMB)
	}
}

func TestLoadInvalidTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	if err := os.WriteFile(path, []byte("{{invalid toml"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid TOML")
	}
}

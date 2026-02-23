package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Config holds the complete runtime configuration for the afficho client.
type Config struct {
	Server   ServerConfig   `toml:"server"`
	Admin    AdminConfig    `toml:"admin"`
	Display  DisplayConfig  `toml:"display"`
	Storage  StorageConfig  `toml:"storage"`
	Security SecurityConfig `toml:"security"`
	Cloud    CloudConfig    `toml:"cloud"`
	Update   UpdateConfig   `toml:"update"`
	Logging  LoggingConfig  `toml:"logging"`
}

// AdminConfig controls access to the local admin UI and API.
// EE note: SSO and RBAC are handled by the Afficho Cloud web console, not here.
type AdminConfig struct {
	// Password protects the admin UI and all /api/v1 write endpoints.
	// Leave empty to disable auth (only do this on a trusted network).
	Password string `toml:"password"`
}

type ServerConfig struct {
	Host string `toml:"host"`
	Port int    `toml:"port"`
}

type DisplayConfig struct {
	// LaunchBrowser controls whether the daemon automatically starts Chromium on startup.
	LaunchBrowser bool `toml:"launch_browser"`
	// Browser is the name or path of the browser executable to launch.
	// Set to "auto" to auto-detect (tries chromium-browser, chromium, google-chrome, brave-browser).
	Browser    string `toml:"browser"`
	DisplayEnv string `toml:"display_env"` // e.g. ":0" for X11
	// Platform selects the display server: "auto", "x11", or "wayland".
	// "auto" detects based on WAYLAND_DISPLAY environment variable.
	Platform string `toml:"platform"`
	// ScreenOffTime turns HDMI off at this time daily (HH:MM format, empty = never).
	ScreenOffTime string `toml:"screen_off_time"`
	// ScreenOnTime turns HDMI on at this time daily (HH:MM format, empty = never).
	ScreenOnTime string `toml:"screen_on_time"`
}

type StorageConfig struct {
	DataDir     string `toml:"data_dir"`
	MaxCacheGB  int    `toml:"max_cache_gb"`
	MaxUploadMB int    `toml:"max_upload_mb"`
}

// SecurityConfig controls CORS, rate limiting, and other security hardening.
type SecurityConfig struct {
	// CORSAllowedOrigins restricts cross-origin API requests.
	// Empty (default) = same-origin only (no CORS headers sent).
	// ["*"] = allow all origins.
	CORSAllowedOrigins []string `toml:"cors_allowed_origins"`
	// UploadConcurrencyLimit caps simultaneous file uploads to prevent disk
	// exhaustion. Default: 2.
	UploadConcurrencyLimit int `toml:"upload_concurrency_limit"`
}

// CloudConfig controls the connection to the Afficho Cloud backend.
// When Enabled is true, the daemon connects to the cloud WebSocket endpoint
// and receives remote playlists, content, commands, and alerts.
type CloudConfig struct {
	Enabled            bool   `toml:"enabled"`
	Endpoint           string `toml:"endpoint"`
	DeviceKey          string `toml:"device_key"`
	HeartbeatInterval  int    `toml:"heartbeat_interval"`  // seconds
	ReconnectMaxDelay  int    `toml:"reconnect_max_delay"` // seconds
}

// UpdateConfig controls automatic updates from GitHub releases.
type UpdateConfig struct {
	// Enabled turns on periodic update checks. Default: false (opt-in).
	Enabled bool `toml:"enabled"`
	// CheckInterval is how often to poll for new releases (e.g. "6h", "24h").
	CheckInterval string `toml:"check_interval"`
	// Channel selects the update stream: "stable" (tagged releases only) or
	// "pre-release" (includes pre-releases).
	Channel string `toml:"channel"`
}

type LoggingConfig struct {
	Debug bool `toml:"debug"`
}

// Default returns a Config populated with safe defaults.
func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 8080,
		},
		Display: DisplayConfig{
			LaunchBrowser: true,
			Browser:       "auto",
			DisplayEnv:    ":0",
			Platform:      "auto",
		},
		Storage: StorageConfig{
			DataDir:     "/var/lib/afficho",
			MaxCacheGB:  10,
			MaxUploadMB: 100,
		},
		Security: SecurityConfig{
			UploadConcurrencyLimit: 2,
		},
		Cloud: CloudConfig{
			Enabled:           false,
			Endpoint:          "wss://cloud.afficho.io/ws/device",
			HeartbeatInterval: 30,
			ReconnectMaxDelay: 300,
		},
		Update: UpdateConfig{
			Enabled:       false,
			CheckInterval: "24h",
			Channel:       "stable",
		},
	}
}

// Load reads a TOML config file and merges it over the defaults.
// If the file does not exist, defaults are returned without error.
func Load(path string) (*Config, error) {
	cfg := Default()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config %q: %w", path, err)
	}

	if _, err := toml.Decode(string(data), cfg); err != nil {
		return nil, fmt.Errorf("parsing config %q: %w", path, err)
	}

	return cfg, nil
}

// ServerAddr returns the combined host:port address for the HTTP server.
func (c *Config) ServerAddr() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}

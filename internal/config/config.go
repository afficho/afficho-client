package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Config holds the complete runtime configuration for the afficho client.
type Config struct {
	Server  ServerConfig  `toml:"server"`
	Admin   AdminConfig   `toml:"admin"`
	Display DisplayConfig `toml:"display"`
	Storage StorageConfig `toml:"storage"`
	Cloud   CloudConfig   `toml:"cloud"`
	Logging LoggingConfig `toml:"logging"`
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
	// Browser is the name of the browser executable to launch.
	Browser    string `toml:"browser"`
	DisplayEnv string `toml:"display_env"` // e.g. ":0" for X11
}

type StorageConfig struct {
	DataDir    string `toml:"data_dir"`
	MaxCacheGB int    `toml:"max_cache_gb"`
}

type CloudConfig struct {
	Enabled  bool   `toml:"enabled"`
	Endpoint string `toml:"endpoint"`
	DeviceID string `toml:"device_id"`
	Token    string `toml:"token"`
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
			Browser:       "chromium-browser",
			DisplayEnv:    ":0",
		},
		Storage: StorageConfig{
			DataDir:    "/var/lib/afficho",
			MaxCacheGB: 10,
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

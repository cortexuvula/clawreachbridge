package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"reflect"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration for ClawReach Bridge.
type Config struct {
	Bridge     BridgeConfig     `yaml:"bridge"`
	Security   SecurityConfig   `yaml:"security"`
	Logging    LoggingConfig    `yaml:"logging"`
	Health     HealthConfig     `yaml:"health"`
	Monitoring MonitoringConfig `yaml:"monitoring"`
}

// BridgeConfig contains the core proxy settings.
type BridgeConfig struct {
	ListenAddress       string        `yaml:"listen_address"`
	GatewayURL          string        `yaml:"gateway_url"`
	Origin              string        `yaml:"origin"`
	DrainTimeout        time.Duration `yaml:"drain_timeout"`
	MaxMessageSize      int64         `yaml:"max_message_size"`
	PingInterval        time.Duration `yaml:"ping_interval"`
	PongTimeout         time.Duration `yaml:"pong_timeout"`
	WriteTimeout        time.Duration `yaml:"write_timeout"`
	ReadTimeout         time.Duration `yaml:"read_timeout"`
	DialTimeout         time.Duration `yaml:"dial_timeout"`
	AllowedSubprotocols []string      `yaml:"allowed_subprotocols"`
	TLS                 TLSConfig     `yaml:"tls"`
	Media               MediaConfig   `yaml:"media"`
}

// MediaConfig controls image injection from the gateway's media directory.
type MediaConfig struct {
	Enabled     bool          `yaml:"enabled"`
	Directory   string        `yaml:"directory"`
	MaxFileSize int64         `yaml:"max_file_size"`
	MaxAge      time.Duration `yaml:"max_age"`
	Extensions  []string      `yaml:"extensions"`
	InjectPaths []string      `yaml:"inject_paths"`
}

// TLSConfig contains optional TLS settings.
type TLSConfig struct {
	Enabled  bool   `yaml:"enabled"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

// SecurityConfig contains security-related settings.
type SecurityConfig struct {
	TailscaleOnly       bool            `yaml:"tailscale_only"`
	AuthToken           string          `yaml:"auth_token"`
	RateLimit           RateLimitConfig `yaml:"rate_limit"`
	MaxConnections      int             `yaml:"max_connections"`
	MaxConnectionsPerIP int             `yaml:"max_connections_per_ip"`
}

// RateLimitConfig contains rate limiting settings.
type RateLimitConfig struct {
	Enabled              bool `yaml:"enabled"`
	ConnectionsPerMinute int  `yaml:"connections_per_minute"`
	MessagesPerSecond    int  `yaml:"messages_per_second"`
}

// LoggingConfig contains logging settings.
type LoggingConfig struct {
	Level      string `yaml:"level"`
	Format     string `yaml:"format"`
	File       string `yaml:"file"`
	MaxSizeMB  int    `yaml:"max_size_mb"`
	MaxBackups int    `yaml:"max_backups"`
	MaxAgeDays int    `yaml:"max_age_days"`
	Compress   bool   `yaml:"compress"`
}

// HealthConfig contains health check endpoint settings.
type HealthConfig struct {
	Enabled       bool   `yaml:"enabled"`
	Endpoint      string `yaml:"endpoint"`
	ListenAddress string `yaml:"listen_address"`
	Detailed      bool   `yaml:"detailed"`
}

// MonitoringConfig contains metrics settings.
type MonitoringConfig struct {
	MetricsEnabled  bool   `yaml:"metrics_enabled"`
	MetricsEndpoint string `yaml:"metrics_endpoint"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Bridge: BridgeConfig{
			ListenAddress:  "100.64.0.1:8080",
			GatewayURL:     "http://localhost:18800",
			Origin:         "https://gateway.local",
			DrainTimeout:   30 * time.Second,
			MaxMessageSize: 262144, // 256KB
			PingInterval:   30 * time.Second,
			PongTimeout:    10 * time.Second,
			WriteTimeout:   30 * time.Second,
			ReadTimeout:    60 * time.Second,
			DialTimeout:    10 * time.Second,
			Media: MediaConfig{
				Enabled:     false,
				Directory:   "",
				MaxFileSize: 10 * 1024 * 1024, // 10MB
				MaxAge:      60 * time.Second,
				Extensions:  []string{".png", ".jpg", ".jpeg", ".webp", ".gif"},
				InjectPaths: []string{"/ws/operator"},
			},
		},
		Security: SecurityConfig{
			TailscaleOnly:       true,
			MaxConnections:      1000,
			MaxConnectionsPerIP: 10,
			RateLimit: RateLimitConfig{
				Enabled:              true,
				ConnectionsPerMinute: 60,
				MessagesPerSecond:    100,
			},
		},
		Logging: LoggingConfig{
			Level:      "info",
			Format:     "json",
			MaxSizeMB:  100,
			MaxBackups: 3,
			MaxAgeDays: 28,
			Compress:   true,
		},
		Health: HealthConfig{
			Enabled:       true,
			Endpoint:      "/health",
			ListenAddress: "127.0.0.1:8081",
			Detailed:      true,
		},
		Monitoring: MonitoringConfig{
			MetricsEnabled:  false,
			MetricsEndpoint: "/metrics",
		},
	}
}

// Load reads a config file and applies environment variable overrides.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("config file not found at %s (run 'sudo clawreachbridge setup' to create one)", path)
			}
			if os.IsPermission(err) {
				return nil, fmt.Errorf("permission denied reading %s (try running with sudo)", path)
			}
			return nil, fmt.Errorf("reading config file: %w", err)
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parsing config file %s: %w (check YAML indentation)", path, err)
		}
	}

	applyEnvOverrides(cfg)

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return cfg, nil
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	// Bridge validation
	if c.Bridge.ListenAddress == "" {
		return fmt.Errorf("bridge.listen_address is required")
	}
	if _, _, err := net.SplitHostPort(c.Bridge.ListenAddress); err != nil {
		return fmt.Errorf("bridge.listen_address is invalid: %w", err)
	}
	if c.Bridge.GatewayURL == "" {
		return fmt.Errorf("bridge.gateway_url is required")
	}
	if u, err := url.Parse(c.Bridge.GatewayURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("bridge.gateway_url must use http:// or https:// scheme")
	} else if u != nil {
		host := u.Hostname()
		ip := net.ParseIP(host)
		if ip != nil && !ip.IsLoopback() && !ip.IsPrivate() {
			return fmt.Errorf("bridge.gateway_url should point to localhost or a private IP, got %s", host)
		}
	}
	if c.Bridge.Origin == "" {
		return fmt.Errorf("bridge.origin is required")
	}
	if c.Bridge.MaxMessageSize <= 0 {
		return fmt.Errorf("bridge.max_message_size must be positive")
	}
	if c.Bridge.DrainTimeout <= 0 {
		return fmt.Errorf("bridge.drain_timeout must be positive")
	}
	if c.Bridge.WriteTimeout <= 0 {
		return fmt.Errorf("bridge.write_timeout must be positive")
	}
	if c.Bridge.ReadTimeout <= 0 {
		return fmt.Errorf("bridge.read_timeout must be positive")
	}
	if c.Bridge.DialTimeout <= 0 {
		return fmt.Errorf("bridge.dial_timeout must be positive")
	}

	// Upper bounds
	if c.Bridge.MaxMessageSize > 67108864 {
		return fmt.Errorf("bridge.max_message_size must not exceed 67108864 (64MB)")
	}
	if c.Bridge.DrainTimeout > 5*time.Minute {
		return fmt.Errorf("bridge.drain_timeout must not exceed 5m")
	}
	if c.Bridge.WriteTimeout > 5*time.Minute {
		return fmt.Errorf("bridge.write_timeout must not exceed 5m")
	}
	if c.Bridge.ReadTimeout > 5*time.Minute {
		return fmt.Errorf("bridge.read_timeout must not exceed 5m")
	}
	if c.Bridge.DialTimeout > 5*time.Minute {
		return fmt.Errorf("bridge.dial_timeout must not exceed 5m")
	}

	// Listen address safety check
	if c.Security.TailscaleOnly {
		host, _, _ := net.SplitHostPort(c.Bridge.ListenAddress)
		if host == "0.0.0.0" || host == "::" || host == "" {
			return fmt.Errorf("bridge.listen_address should not bind to all interfaces when security.tailscale_only is true (use a Tailscale IP)")
		}
	}

	// TLS validation
	if c.Bridge.TLS.Enabled {
		if c.Bridge.TLS.CertFile == "" {
			return fmt.Errorf("bridge.tls.cert_file is required when TLS is enabled")
		}
		if c.Bridge.TLS.KeyFile == "" {
			return fmt.Errorf("bridge.tls.key_file is required when TLS is enabled")
		}
	}

	// Security validation
	if c.Security.MaxConnections <= 0 {
		return fmt.Errorf("security.max_connections must be positive")
	}
	if c.Security.MaxConnections > 65535 {
		return fmt.Errorf("security.max_connections must not exceed 65535")
	}
	if c.Security.MaxConnectionsPerIP <= 0 {
		return fmt.Errorf("security.max_connections_per_ip must be positive")
	}
	if c.Security.MaxConnectionsPerIP > c.Security.MaxConnections {
		return fmt.Errorf("security.max_connections_per_ip must not exceed security.max_connections")
	}
	if c.Security.RateLimit.Enabled {
		if c.Security.RateLimit.ConnectionsPerMinute <= 0 {
			return fmt.Errorf("security.rate_limit.connections_per_minute must be positive")
		}
	}

	// Logging validation
	switch c.Logging.Level {
	case "debug", "info", "warn", "error":
		// valid
	default:
		return fmt.Errorf("logging.level must be one of: debug, info, warn, error")
	}
	switch c.Logging.Format {
	case "json", "text":
		// valid
	default:
		return fmt.Errorf("logging.format must be one of: json, text")
	}

	// Health validation
	if c.Health.Enabled {
		if c.Health.ListenAddress == "" {
			return fmt.Errorf("health.listen_address is required when health is enabled")
		}
		if _, _, err := net.SplitHostPort(c.Health.ListenAddress); err != nil {
			return fmt.Errorf("health.listen_address is invalid: %w", err)
		}
		host, _, _ := net.SplitHostPort(c.Health.ListenAddress)
		ip := net.ParseIP(host)
		if ip != nil && !ip.IsLoopback() {
			return fmt.Errorf("health.listen_address should bind to a loopback address (e.g. 127.0.0.1) to avoid exposing metrics")
		}
		if c.Bridge.ListenAddress == c.Health.ListenAddress {
			return fmt.Errorf("bridge.listen_address and health.listen_address must be different")
		}
	}

	return nil
}

// applyEnvOverrides applies CLAWREACH_ prefixed environment variables.
// Convention: CLAWREACH_ + uppercase + underscores for nesting.
func applyEnvOverrides(cfg *Config) {
	envMap := map[string]func(string){
		"CLAWREACH_BRIDGE_LISTEN_ADDRESS":           func(v string) { cfg.Bridge.ListenAddress = v },
		"CLAWREACH_BRIDGE_GATEWAY_URL":              func(v string) { cfg.Bridge.GatewayURL = v },
		"CLAWREACH_BRIDGE_ORIGIN":                   func(v string) { cfg.Bridge.Origin = v },
		"CLAWREACH_BRIDGE_DRAIN_TIMEOUT":            func(v string) { cfg.Bridge.DrainTimeout = parseDuration(v, cfg.Bridge.DrainTimeout) },
		"CLAWREACH_BRIDGE_MAX_MESSAGE_SIZE":         func(v string) { cfg.Bridge.MaxMessageSize = parseInt64(v, cfg.Bridge.MaxMessageSize) },
		"CLAWREACH_BRIDGE_PING_INTERVAL":            func(v string) { cfg.Bridge.PingInterval = parseDuration(v, cfg.Bridge.PingInterval) },
		"CLAWREACH_BRIDGE_PONG_TIMEOUT":             func(v string) { cfg.Bridge.PongTimeout = parseDuration(v, cfg.Bridge.PongTimeout) },
		"CLAWREACH_BRIDGE_WRITE_TIMEOUT":            func(v string) { cfg.Bridge.WriteTimeout = parseDuration(v, cfg.Bridge.WriteTimeout) },
		"CLAWREACH_BRIDGE_READ_TIMEOUT":             func(v string) { cfg.Bridge.ReadTimeout = parseDuration(v, cfg.Bridge.ReadTimeout) },
		"CLAWREACH_BRIDGE_DIAL_TIMEOUT":             func(v string) { cfg.Bridge.DialTimeout = parseDuration(v, cfg.Bridge.DialTimeout) },
		"CLAWREACH_SECURITY_TAILSCALE_ONLY":         func(v string) { cfg.Security.TailscaleOnly = parseBool(v, cfg.Security.TailscaleOnly) },
		"CLAWREACH_SECURITY_AUTH_TOKEN":             func(v string) { cfg.Security.AuthToken = v },
		"CLAWREACH_SECURITY_MAX_CONNECTIONS":        func(v string) { cfg.Security.MaxConnections = parseInt(v, cfg.Security.MaxConnections) },
		"CLAWREACH_SECURITY_MAX_CONNECTIONS_PER_IP": func(v string) { cfg.Security.MaxConnectionsPerIP = parseInt(v, cfg.Security.MaxConnectionsPerIP) },
		"CLAWREACH_SECURITY_RATE_LIMIT_ENABLED":     func(v string) { cfg.Security.RateLimit.Enabled = parseBool(v, cfg.Security.RateLimit.Enabled) },
		"CLAWREACH_SECURITY_RATE_LIMIT_CONNECTIONS_PER_MINUTE": func(v string) {
			cfg.Security.RateLimit.ConnectionsPerMinute = parseInt(v, cfg.Security.RateLimit.ConnectionsPerMinute)
		},
		"CLAWREACH_LOGGING_LEVEL":         func(v string) { cfg.Logging.Level = v },
		"CLAWREACH_LOGGING_FORMAT":        func(v string) { cfg.Logging.Format = v },
		"CLAWREACH_LOGGING_FILE":          func(v string) { cfg.Logging.File = v },
		"CLAWREACH_HEALTH_ENABLED":        func(v string) { cfg.Health.Enabled = parseBool(v, cfg.Health.Enabled) },
		"CLAWREACH_HEALTH_LISTEN_ADDRESS": func(v string) { cfg.Health.ListenAddress = v },
		"CLAWREACH_BRIDGE_MEDIA_ENABLED":   func(v string) { cfg.Bridge.Media.Enabled = parseBool(v, cfg.Bridge.Media.Enabled) },
		"CLAWREACH_BRIDGE_MEDIA_DIRECTORY": func(v string) { cfg.Bridge.Media.Directory = v },
	}

	for env, setter := range envMap {
		if v := os.Getenv(env); v != "" {
			setter(v)
		}
	}
}

// ApplyReloadableFields returns a copy of c with reloadable fields from newCfg.
// Non-reloadable: listen_address, gateway_url, tls, health.listen_address
func (c *Config) ApplyReloadableFields(newCfg *Config) *Config {
	updated := *c
	updated.Security.RateLimit = newCfg.Security.RateLimit
	updated.Security.AuthToken = newCfg.Security.AuthToken
	updated.Security.MaxConnections = newCfg.Security.MaxConnections
	updated.Security.MaxConnectionsPerIP = newCfg.Security.MaxConnectionsPerIP
	updated.Logging.Level = newCfg.Logging.Level
	updated.Bridge.MaxMessageSize = newCfg.Bridge.MaxMessageSize
	return &updated
}

// IsReloadSafe checks if only reloadable fields changed between configs.
func IsReloadSafe(old, new *Config) []string {
	var warnings []string
	if old.Bridge.ListenAddress != new.Bridge.ListenAddress {
		warnings = append(warnings, "bridge.listen_address requires restart")
	}
	if old.Bridge.GatewayURL != new.Bridge.GatewayURL {
		warnings = append(warnings, "bridge.gateway_url requires restart")
	}
	if !reflect.DeepEqual(old.Bridge.TLS, new.Bridge.TLS) {
		warnings = append(warnings, "bridge.tls requires restart")
	}
	if old.Health.ListenAddress != new.Health.ListenAddress {
		warnings = append(warnings, "health.listen_address requires restart")
	}
	return warnings
}

func parseDuration(s string, fallback time.Duration) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}

func parseInt64(s string, fallback int64) int64 {
	var v int64
	if _, err := fmt.Sscanf(s, "%d", &v); err != nil {
		return fallback
	}
	return v
}

func parseInt(s string, fallback int) int {
	var v int
	if _, err := fmt.Sscanf(s, "%d", &v); err != nil {
		return fallback
	}
	return v
}

func parseBool(s string, fallback bool) bool {
	s = strings.ToLower(s)
	switch s {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	default:
		return fallback
	}
}

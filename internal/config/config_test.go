package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Bridge.ListenAddress == "" {
		t.Error("default listen_address should not be empty")
	}
	if cfg.Bridge.GatewayURL != "http://localhost:18800" {
		t.Errorf("default gateway_url = %q, want %q", cfg.Bridge.GatewayURL, "http://localhost:18800")
	}
	if cfg.Bridge.MaxMessageSize != 262144 {
		t.Errorf("default max_message_size = %d, want %d", cfg.Bridge.MaxMessageSize, 262144)
	}
	if cfg.Bridge.ReadTimeout != 60*time.Second {
		t.Errorf("default read_timeout = %v, want %v", cfg.Bridge.ReadTimeout, 60*time.Second)
	}
	if !cfg.Health.Detailed {
		t.Error("default health.detailed should be true")
	}
	if cfg.Bridge.DrainTimeout != 30*time.Second {
		t.Errorf("default drain_timeout = %v, want %v", cfg.Bridge.DrainTimeout, 30*time.Second)
	}
	if cfg.Health.ListenAddress != "127.0.0.1:8081" {
		t.Errorf("default health.listen_address = %q, want %q", cfg.Health.ListenAddress, "127.0.0.1:8081")
	}
	if !cfg.Security.TailscaleOnly {
		t.Error("default tailscale_only should be true")
	}
	if cfg.Security.MaxConnections != 1000 {
		t.Errorf("default max_connections = %d, want %d", cfg.Security.MaxConnections, 1000)
	}
	if cfg.Bridge.Reactions.Enabled {
		t.Error("default reactions.enabled should be false")
	}
	if cfg.Bridge.Reactions.Mode != "passthrough" {
		t.Errorf("default reactions.mode = %q, want %q", cfg.Bridge.Reactions.Mode, "passthrough")
	}
	if cfg.Bridge.Canvas.StateTracking {
		t.Error("default canvas.state_tracking should be false")
	}
	if cfg.Bridge.Canvas.JSONLBufferSize != 5 {
		t.Errorf("default canvas.jsonl_buffer_size = %d, want 5", cfg.Bridge.Canvas.JSONLBufferSize)
	}
	if cfg.Bridge.Canvas.MaxAge != 5*time.Minute {
		t.Errorf("default canvas.max_age = %v, want 5m", cfg.Bridge.Canvas.MaxAge)
	}
	if len(cfg.Security.PublicPaths) != 1 || cfg.Security.PublicPaths[0] != "/__openclaw__/a2ui/" {
		t.Errorf("default public_paths = %v, want [/__openclaw__/a2ui/]", cfg.Security.PublicPaths)
	}
}

func TestLoadFromFile(t *testing.T) {
	content := `
bridge:
  listen_address: "100.101.102.103:8080"
  gateway_url: "http://localhost:18800"
  origin: "https://gateway.local"
  drain_timeout: "5s"
  max_message_size: 2097152
  write_timeout: "15s"
  dial_timeout: "15s"
security:
  tailscale_only: true
  auth_token: "test-token"
  max_connections: 500
  max_connections_per_ip: 5
  rate_limit:
    enabled: false
logging:
  level: "debug"
  format: "text"
health:
  enabled: true
  listen_address: "127.0.0.1:8081"
  endpoint: "/health"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Bridge.ListenAddress != "100.101.102.103:8080" {
		t.Errorf("listen_address = %q, want %q", cfg.Bridge.ListenAddress, "100.101.102.103:8080")
	}
	if cfg.Bridge.DrainTimeout != 5*time.Second {
		t.Errorf("drain_timeout = %v, want %v", cfg.Bridge.DrainTimeout, 5*time.Second)
	}
	if cfg.Bridge.MaxMessageSize != 2097152 {
		t.Errorf("max_message_size = %d, want %d", cfg.Bridge.MaxMessageSize, 2097152)
	}
	if cfg.Security.AuthToken != "test-token" {
		t.Errorf("auth_token = %q, want %q", cfg.Security.AuthToken, "test-token")
	}
	if cfg.Security.MaxConnections != 500 {
		t.Errorf("max_connections = %d, want %d", cfg.Security.MaxConnections, 500)
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("level = %q, want %q", cfg.Logging.Level, "debug")
	}
	if cfg.Security.RateLimit.Enabled {
		t.Error("rate_limit.enabled should be false")
	}
}

func TestLoadDefaults(t *testing.T) {
	// Load with empty path uses defaults
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load('') error: %v", err)
	}
	if cfg.Bridge.GatewayURL != "http://localhost:18800" {
		t.Errorf("gateway_url = %q, want default", cfg.Bridge.GatewayURL)
	}
}

func TestEnvOverrides(t *testing.T) {
	t.Setenv("CLAWREACH_BRIDGE_GATEWAY_URL", "http://10.0.0.1:18800")
	t.Setenv("CLAWREACH_SECURITY_AUTH_TOKEN", "env-token")
	t.Setenv("CLAWREACH_LOGGING_LEVEL", "debug")
	t.Setenv("CLAWREACH_SECURITY_TAILSCALE_ONLY", "false")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Bridge.GatewayURL != "http://10.0.0.1:18800" {
		t.Errorf("gateway_url = %q, want env override", cfg.Bridge.GatewayURL)
	}
	if cfg.Security.AuthToken != "env-token" {
		t.Errorf("auth_token = %q, want %q", cfg.Security.AuthToken, "env-token")
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("level = %q, want %q", cfg.Logging.Level, "debug")
	}
	if cfg.Security.TailscaleOnly {
		t.Error("tailscale_only should be false from env override")
	}
}

func TestValidation(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr string
	}{
		{
			name:    "valid default",
			modify:  func(c *Config) {},
			wantErr: "",
		},
		{
			name:    "empty listen_address",
			modify:  func(c *Config) { c.Bridge.ListenAddress = "" },
			wantErr: "bridge.listen_address is required",
		},
		{
			name:    "invalid listen_address",
			modify:  func(c *Config) { c.Bridge.ListenAddress = "not-a-host-port" },
			wantErr: "bridge.listen_address is invalid",
		},
		{
			name:    "empty gateway_url",
			modify:  func(c *Config) { c.Bridge.GatewayURL = "" },
			wantErr: "bridge.gateway_url is required",
		},
		{
			name:    "invalid gateway_url scheme",
			modify:  func(c *Config) { c.Bridge.GatewayURL = "ftp://localhost:18800" },
			wantErr: "bridge.gateway_url must use http:// or https://",
		},
		{
			name:    "empty origin",
			modify:  func(c *Config) { c.Bridge.Origin = "" },
			wantErr: "bridge.origin is required",
		},
		{
			name:    "zero max_message_size",
			modify:  func(c *Config) { c.Bridge.MaxMessageSize = 0 },
			wantErr: "bridge.max_message_size must be positive",
		},
		{
			name:    "invalid log level",
			modify:  func(c *Config) { c.Logging.Level = "verbose" },
			wantErr: "logging.level must be one of",
		},
		{
			name:    "invalid log format",
			modify:  func(c *Config) { c.Logging.Format = "csv" },
			wantErr: "logging.format must be one of",
		},
		{
			name:    "tls enabled without cert",
			modify:  func(c *Config) { c.Bridge.TLS.Enabled = true },
			wantErr: "bridge.tls.cert_file is required",
		},
		{
			name: "tls enabled without key",
			modify: func(c *Config) {
				c.Bridge.TLS.Enabled = true
				c.Bridge.TLS.CertFile = "/path/to/cert.pem"
			},
			wantErr: "bridge.tls.key_file is required",
		},
		{
			name:    "zero max_connections",
			modify:  func(c *Config) { c.Security.MaxConnections = 0 },
			wantErr: "security.max_connections must be positive",
		},
		{
			name:    "zero read_timeout",
			modify:  func(c *Config) { c.Bridge.ReadTimeout = 0 },
			wantErr: "bridge.read_timeout must be positive",
		},
		{
			name:    "max_message_size exceeds 64MB",
			modify:  func(c *Config) { c.Bridge.MaxMessageSize = 67108865 },
			wantErr: "bridge.max_message_size must not exceed 67108864",
		},
		{
			name:    "max_connections exceeds 65535",
			modify:  func(c *Config) { c.Security.MaxConnections = 70000 },
			wantErr: "security.max_connections must not exceed 65535",
		},
		{
			name: "max_connections_per_ip exceeds max_connections",
			modify: func(c *Config) {
				c.Security.MaxConnections = 100
				c.Security.MaxConnectionsPerIP = 200
			},
			wantErr: "security.max_connections_per_ip must not exceed security.max_connections",
		},
		{
			name:    "drain_timeout exceeds 5m",
			modify:  func(c *Config) { c.Bridge.DrainTimeout = 6 * time.Minute },
			wantErr: "bridge.drain_timeout must not exceed 5m",
		},
		{
			name:    "write_timeout exceeds 5m",
			modify:  func(c *Config) { c.Bridge.WriteTimeout = 6 * time.Minute },
			wantErr: "bridge.write_timeout must not exceed 5m",
		},
		{
			name:    "read_timeout exceeds 5m",
			modify:  func(c *Config) { c.Bridge.ReadTimeout = 6 * time.Minute },
			wantErr: "bridge.read_timeout must not exceed 5m",
		},
		{
			name:    "dial_timeout exceeds 5m",
			modify:  func(c *Config) { c.Bridge.DialTimeout = 6 * time.Minute },
			wantErr: "bridge.dial_timeout must not exceed 5m",
		},
		{
			name: "listen_address 0.0.0.0 with tailscale_only",
			modify: func(c *Config) {
				c.Security.TailscaleOnly = true
				c.Bridge.ListenAddress = "0.0.0.0:8080"
			},
			wantErr: "bridge.listen_address should not bind to all interfaces",
		},
		{
			name: "listen_address :: with tailscale_only",
			modify: func(c *Config) {
				c.Security.TailscaleOnly = true
				c.Bridge.ListenAddress = "[::]:8080"
			},
			wantErr: "bridge.listen_address should not bind to all interfaces",
		},
		{
			name: "gateway_url public IP",
			modify: func(c *Config) {
				c.Bridge.GatewayURL = "http://8.8.8.8:18800"
			},
			wantErr: "bridge.gateway_url should point to localhost or a private IP",
		},
		{
			name:   "gateway_url localhost hostname is valid",
			modify: func(c *Config) { c.Bridge.GatewayURL = "http://localhost:18800" },
		},
		{
			name:   "gateway_url private IP is valid",
			modify: func(c *Config) { c.Bridge.GatewayURL = "http://192.168.1.1:18800" },
		},
		{
			name: "health listen_address not loopback",
			modify: func(c *Config) {
				c.Health.Enabled = true
				c.Health.ListenAddress = "0.0.0.0:8081"
			},
			wantErr: "health.listen_address should bind to a loopback address",
		},
		{
			name:   "health listen_address loopback is valid",
			modify: func(c *Config) { c.Health.ListenAddress = "127.0.0.1:8081" },
		},
		{
			name: "health and proxy same address",
			modify: func(c *Config) {
				c.Health.Enabled = true
				c.Bridge.ListenAddress = "127.0.0.1:8080"
				c.Health.ListenAddress = "127.0.0.1:8080"
				c.Security.TailscaleOnly = false
			},
			wantErr: "bridge.listen_address and health.listen_address must be different",
		},
		{
			name: "reactions passthrough is valid",
			modify: func(c *Config) {
				c.Bridge.Reactions.Enabled = true
				c.Bridge.Reactions.Mode = "passthrough"
			},
		},
		{
			name: "reactions bridge mode rejected",
			modify: func(c *Config) {
				c.Bridge.Reactions.Enabled = true
				c.Bridge.Reactions.Mode = "bridge"
			},
			wantErr: "bridge.reactions.mode \"bridge\" is not yet implemented",
		},
		{
			name: "reactions invalid mode",
			modify: func(c *Config) {
				c.Bridge.Reactions.Enabled = true
				c.Bridge.Reactions.Mode = "invalid"
			},
			wantErr: "bridge.reactions.mode must be one of: passthrough",
		},
		{
			name: "canvas valid config",
			modify: func(c *Config) {
				c.Bridge.Canvas.StateTracking = true
				c.Bridge.Canvas.JSONLBufferSize = 10
				c.Bridge.Canvas.MaxAge = 5 * time.Minute
			},
		},
		{
			name: "canvas buffer size too low",
			modify: func(c *Config) {
				c.Bridge.Canvas.StateTracking = true
				c.Bridge.Canvas.JSONLBufferSize = 0
			},
			wantErr: "bridge.canvas.jsonl_buffer_size must be between 1 and 100",
		},
		{
			name: "canvas buffer size too high",
			modify: func(c *Config) {
				c.Bridge.Canvas.StateTracking = true
				c.Bridge.Canvas.JSONLBufferSize = 101
			},
			wantErr: "bridge.canvas.jsonl_buffer_size must be between 1 and 100",
		},
		{
			name: "canvas max_age too low",
			modify: func(c *Config) {
				c.Bridge.Canvas.StateTracking = true
				c.Bridge.Canvas.MaxAge = 500 * time.Millisecond
			},
			wantErr: "bridge.canvas.max_age must be between 1s and 30m",
		},
		{
			name: "canvas max_age too high",
			modify: func(c *Config) {
				c.Bridge.Canvas.StateTracking = true
				c.Bridge.Canvas.MaxAge = 31 * time.Minute
			},
			wantErr: "bridge.canvas.max_age must be between 1s and 30m",
		},
		{
			name: "canvas disabled skips validation",
			modify: func(c *Config) {
				c.Bridge.Canvas.StateTracking = false
				c.Bridge.Canvas.JSONLBufferSize = 0 // would fail if validated
			},
		},
		{
			name:   "empty public_paths is valid",
			modify: func(c *Config) { c.Security.PublicPaths = nil },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.modify(cfg)
			err := cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("Validate() expected error containing %q, got nil", tt.wantErr)
				} else if !contains(err.Error(), tt.wantErr) {
					t.Errorf("Validate() error = %q, want containing %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestIsReloadSafe(t *testing.T) {
	old := DefaultConfig()
	new := DefaultConfig()

	// Same config — no warnings
	warnings := IsReloadSafe(old, new)
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}

	// Change listen_address — should warn
	new.Bridge.ListenAddress = "100.200.200.200:9090"
	warnings = IsReloadSafe(old, new)
	if len(warnings) != 1 {
		t.Errorf("expected 1 warning, got %d: %v", len(warnings), warnings)
	}

	// Change gateway_url too
	new.Bridge.GatewayURL = "http://other:18800"
	warnings = IsReloadSafe(old, new)
	if len(warnings) != 2 {
		t.Errorf("expected 2 warnings, got %d: %v", len(warnings), warnings)
	}
}

func TestApplyReloadableFields(t *testing.T) {
	old := DefaultConfig()
	newCfg := DefaultConfig()
	newCfg.Security.AuthToken = "new-token"
	newCfg.Logging.Level = "debug"
	newCfg.Bridge.MaxMessageSize = 2097152

	updated := old.ApplyReloadableFields(newCfg)

	// Original should be unchanged
	if old.Security.AuthToken != "" {
		t.Errorf("original auth_token should be unchanged, got %q", old.Security.AuthToken)
	}

	// Updated should have new values
	if updated.Security.AuthToken != "new-token" {
		t.Errorf("auth_token not reloaded")
	}
	if updated.Logging.Level != "debug" {
		t.Errorf("log level not reloaded")
	}
	if updated.Bridge.MaxMessageSize != 2097152 {
		t.Errorf("max_message_size not reloaded")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstr(s, substr)
}

func searchSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

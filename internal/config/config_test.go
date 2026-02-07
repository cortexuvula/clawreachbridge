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
	if cfg.Bridge.MaxMessageSize != 1048576 {
		t.Errorf("default max_message_size = %d, want %d", cfg.Bridge.MaxMessageSize, 1048576)
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

func TestReloadableFields(t *testing.T) {
	old := DefaultConfig()
	new := DefaultConfig()
	new.Security.AuthToken = "new-token"
	new.Logging.Level = "debug"
	new.Bridge.MaxMessageSize = 2097152

	old.ReloadableFields(new)

	if old.Security.AuthToken != "new-token" {
		t.Errorf("auth_token not reloaded")
	}
	if old.Logging.Level != "debug" {
		t.Errorf("log level not reloaded")
	}
	if old.Bridge.MaxMessageSize != 2097152 {
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

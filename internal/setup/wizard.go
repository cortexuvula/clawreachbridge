package setup

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cortexuvula/clawreachbridge/internal/config"
	"github.com/cortexuvula/clawreachbridge/internal/security"
)

const (
	defaultConfigPath = "/etc/clawreachbridge/config.yaml"
	defaultGatewayURL = "http://localhost:18800"
	defaultListenPort = "8080"
	defaultHealthPort = "8081"
)

// WizardOptions configures the setup wizard.
type WizardOptions struct {
	ConfigPath       string            // Override default config path
	DetectTailscale  func() string     // Override Tailscale IP detection (for testing)
	CheckGateway     func(io.Writer, string) // Override gateway check (for testing)
}

// RunWizard runs the interactive setup wizard.
// It takes io.Reader/io.Writer for testability.
func RunWizard(in io.Reader, out io.Writer, opts WizardOptions) error {
	scanner := bufio.NewScanner(in)
	configPath := opts.ConfigPath
	if configPath == "" {
		configPath = defaultConfigPath
	}

	// Check if running as root; fall back to local config if not
	isRoot := os.Geteuid() == 0
	if !isRoot && configPath == defaultConfigPath {
		configPath = "./config.yaml"
		fmt.Fprintf(out, "NOTE: Not running as root. Config will be written to %s\n", configPath)
		fmt.Fprintf(out, "      Run with sudo for system-wide install: sudo clawreachbridge setup\n\n")
	}

	fmt.Fprintln(out, "ClawReach Bridge Setup")
	fmt.Fprintln(out, "======================")
	fmt.Fprintln(out)

	// Step 1: Detect Tailscale IP
	fmt.Fprintln(out, "Detecting Tailscale...")
	detect := detectTailscaleIP
	if opts.DetectTailscale != nil {
		detect = opts.DetectTailscale
	}
	tailscaleIP := detect()
	if tailscaleIP == "" {
		fmt.Fprintln(out, "  WARNING: No Tailscale IP detected. Is Tailscale running?")
		fmt.Fprintln(out, "  Run: tailscale status")
		fmt.Fprintln(out)
	} else {
		fmt.Fprintf(out, "  Found Tailscale IP: %s\n\n", tailscaleIP)
	}

	// Step 2: Gateway URL
	gatewayURL := prompt(scanner, out,
		fmt.Sprintf("Gateway URL [%s]: ", defaultGatewayURL),
		defaultGatewayURL)

	// Validate gateway URL format (warning only)
	if u, err := url.Parse(gatewayURL); err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		fmt.Fprintf(out, "  WARNING: %q may not be a valid gateway URL (expected http:// or https://)\n\n", gatewayURL)
	}

	// Check gateway reachability (warning only)
	gwCheck := checkGateway
	if opts.CheckGateway != nil {
		gwCheck = opts.CheckGateway
	}
	gwCheck(out, gatewayURL)

	// Step 3: Listen port
	defaultAddr := defaultListenPort
	listenPort := promptPort(scanner, out,
		fmt.Sprintf("Listen port [%s]: ", defaultAddr),
		defaultAddr)

	// Build listen address
	listenHost := tailscaleIP
	if listenHost == "" {
		listenHost = prompt(scanner, out,
			"Tailscale IP (e.g. 100.64.0.1): ", "")
		if listenHost == "" {
			return fmt.Errorf("tailscale IP is required for listen_address")
		}
	}
	listenAddress := net.JoinHostPort(listenHost, listenPort)

	// Check if port is available
	if reason := checkPortAvailable(listenHost, listenPort); reason != "" {
		fmt.Fprintf(out, "  WARNING: Port %s on %s %s\n\n", listenPort, listenHost, reason)
	}

	// Step 4: Health port
	healthPort := promptPort(scanner, out,
		fmt.Sprintf("Health check port [%s]: ", defaultHealthPort),
		defaultHealthPort)
	healthAddress := net.JoinHostPort("127.0.0.1", healthPort)

	// Check if health port is available
	if reason := checkPortAvailable("127.0.0.1", healthPort); reason != "" {
		fmt.Fprintf(out, "  WARNING: Port %s on 127.0.0.1 %s\n\n", healthPort, reason)
	}

	// Step 5: Auth token (optional)
	authToken := prompt(scanner, out,
		"Auth token (leave empty for none): ", "")

	// Step 6: Check for existing config
	if _, err := os.Stat(configPath); err == nil {
		overwrite := prompt(scanner, out,
			fmt.Sprintf("Config already exists at %s. Overwrite? [y/N]: ", configPath), "n")
		if !strings.HasPrefix(strings.ToLower(overwrite), "y") {
			fmt.Fprintln(out, "Setup cancelled.")
			return nil
		}
	}

	// Step 7: Write config
	fmt.Fprintf(out, "\nWriting config to %s...\n", configPath)
	origin := "https://gateway.local"
	configContent := generateConfig(listenAddress, gatewayURL, origin, healthAddress, authToken)

	if err := writeConfig(configPath, configContent, isRoot, out); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	fmt.Fprintln(out, "  Config written successfully.")

	// Step 8: Validate the written config
	fmt.Fprintln(out, "  Validating config...")
	if _, err := config.Load(configPath); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}
	fmt.Fprintln(out, "  Config is valid.")

	// Step 9: Offer to start systemd service (Linux + root only)
	if isRoot && isSystemdAvailable() {
		fmt.Fprintln(out)
		startService := prompt(scanner, out,
			"Start clawreachbridge service now? [Y/n]: ", "y")
		if strings.HasPrefix(strings.ToLower(startService), "y") || startService == "" {
			if err := startSystemdService(out); err != nil {
				fmt.Fprintf(out, "  WARNING: Failed to start service: %v\n", err)
				fmt.Fprintln(out, "  You can start it manually: sudo systemctl start clawreachbridge")
			}
		}
	}

	// Step 10: Print summary
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Setup complete!")
	fmt.Fprintln(out, "===============")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  Config:       %s\n", configPath)
	fmt.Fprintf(out, "  Proxy:        ws://%s\n", listenAddress)
	fmt.Fprintf(out, "  Health:       http://%s/health\n", healthAddress)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Useful commands:")
	fmt.Fprintf(out, "  Check health:   curl http://%s/health\n", healthAddress)
	fmt.Fprintln(out, "  View logs:      sudo journalctl -u clawreachbridge -f")
	fmt.Fprintln(out, "  Validate:       clawreachbridge validate --config "+configPath)

	return nil
}

// prompt displays a message and reads a line from the scanner.
// Returns defaultVal if input is empty or EOF.
func prompt(scanner *bufio.Scanner, out io.Writer, message, defaultVal string) string {
	fmt.Fprint(out, message)
	if scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		if input != "" {
			return input
		}
	}
	return defaultVal
}

// validatePort checks that a port string is a valid TCP port (1-65535).
func validatePort(port string) bool {
	n, err := strconv.Atoi(port)
	if err != nil {
		return false
	}
	return n >= 1 && n <= 65535
}

// promptPort prompts for a port, re-prompting on invalid input.
// Returns defaultVal on empty/EOF input.
func promptPort(scanner *bufio.Scanner, out io.Writer, message, defaultVal string) string {
	val := prompt(scanner, out, message, defaultVal)
	for !validatePort(val) {
		fmt.Fprintf(out, "  Invalid port %q: must be a number between 1 and 65535\n", val)
		val = prompt(scanner, out, message, defaultVal)
		// If we got the default back (EOF/empty), and default is valid, accept it
		if val == defaultVal {
			return defaultVal
		}
	}
	return val
}

// detectTailscaleIP finds a local Tailscale IP address.
func detectTailscaleIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}

	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		// IsTailscaleIP expects host:port format
		if security.IsTailscaleIP(ipNet.IP.String() + ":0") {
			return ipNet.IP.String()
		}
	}
	return ""
}

// checkGateway performs a quick HTTP check against the gateway URL.
func checkGateway(out io.Writer, gatewayURL string) {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(gatewayURL)
	if err != nil {
		fmt.Fprintf(out, "  WARNING: Gateway at %s is not reachable: %v\n", gatewayURL, err)
		fmt.Fprintln(out, "  (This is OK if Gateway is not running yet)")
		fmt.Fprintln(out)
		return
	}
	resp.Body.Close()
	fmt.Fprintf(out, "  Gateway at %s is reachable.\n\n", gatewayURL)
}

// checkPortAvailable checks if a TCP port is free on the given host.
// Returns empty string if available, or a reason string if not.
func checkPortAvailable(host, port string) string {
	ln, err := net.Listen("tcp", net.JoinHostPort(host, port))
	if err != nil {
		if errors.Is(err, syscall.EACCES) {
			return "permission denied (try sudo or a port >= 1024)"
		}
		return "appears to be in use"
	}
	ln.Close()
	return ""
}

// isSystemdAvailable checks if systemctl is available.
func isSystemdAvailable() bool {
	_, err := exec.LookPath("systemctl")
	return err == nil
}

// startSystemdService starts (or restarts) the clawreachbridge service.
func startSystemdService(out io.Writer) error {
	// Reload in case service file changed
	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("daemon-reload: %w", err)
	}

	// Try restart first (handles already-running case), fall back to start
	if err := exec.Command("systemctl", "restart", "clawreachbridge").Run(); err != nil {
		if err := exec.Command("systemctl", "start", "clawreachbridge").Run(); err != nil {
			return err
		}
	}

	// Brief wait then check status
	time.Sleep(2 * time.Second)
	output, err := exec.Command("systemctl", "is-active", "clawreachbridge").Output()
	if err != nil {
		return fmt.Errorf("service did not start (status: %s)", strings.TrimSpace(string(output)))
	}
	status := strings.TrimSpace(string(output))
	if status == "active" {
		fmt.Fprintln(out, "  Service started successfully.")
	} else {
		fmt.Fprintf(out, "  Service status: %s\n", status)
	}
	return nil
}

// yamlEscapeString escapes a string for use inside YAML double quotes.
func yamlEscapeString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// generateConfig creates a commented YAML config string.
func generateConfig(listenAddress, gatewayURL, origin, healthAddress, authToken string) string {
	authTokenLine := `  auth_token: ""`
	if authToken != "" {
		authTokenLine = fmt.Sprintf(`  auth_token: "%s"`, yamlEscapeString(authToken))
	}

	return fmt.Sprintf(`# ClawReach Bridge Configuration
# Generated by: clawreachbridge setup
# Documentation: https://github.com/cortexuvula/clawreachbridge

bridge:
  # REQUIRED: Listen address (must be a Tailscale IP)
  listen_address: "%s"

  # REQUIRED: OpenClaw Gateway URL
  # The bridge auto-converts to ws:// or wss:// for WebSocket dialing
  gateway_url: "%s"

  # REQUIRED: Origin header injected into Gateway requests
  origin: "%s"

  # Shutdown: wait for active connections to finish
  drain_timeout: "30s"

  # WebSocket settings
  max_message_size: 1048576  # 1MB
  ping_interval: "30s"
  pong_timeout: "10s"
  write_timeout: "10s"
  dial_timeout: "10s"

security:
  # Only allow connections from Tailscale IPs
  tailscale_only: true

  # Auth token (optional)
  # Clients send via Authorization: Bearer <token> header or ?token=xxx query param
%s

  # Rate limiting
  rate_limit:
    enabled: true
    connections_per_minute: 60
    messages_per_second: 100

  # Connection limits
  max_connections: 1000
  max_connections_per_ip: 10

logging:
  level: "info"
  format: "json"
  file: ""  # Empty = stdout (journald captures this)

health:
  enabled: true
  endpoint: "/health"
  listen_address: "%s"

monitoring:
  metrics_enabled: false
  metrics_endpoint: "/metrics"
`, yamlEscapeString(listenAddress), yamlEscapeString(gatewayURL), yamlEscapeString(origin), authTokenLine, yamlEscapeString(healthAddress))
}

// writeConfig writes the config file, creating parent directories as needed.
func writeConfig(path, content string, setOwnership bool, out io.Writer) error {
	path = filepath.Clean(path)

	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("creating config directory %s: %w", dir, err)
		}
	}

	if err := os.WriteFile(path, []byte(content), 0640); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	// Set ownership to clawreachbridge:clawreachbridge if running as root
	if setOwnership {
		u, err := user.Lookup("clawreachbridge")
		if err != nil {
			fmt.Fprintf(out, "  WARNING: Could not look up user clawreachbridge: %v\n", err)
		} else {
			g, err := user.LookupGroup("clawreachbridge")
			if err != nil {
				fmt.Fprintf(out, "  WARNING: Could not look up group clawreachbridge: %v\n", err)
			} else {
				uid, err := strconv.Atoi(u.Uid)
				if err != nil {
					fmt.Fprintf(out, "  WARNING: Could not parse UID %q for user clawreachbridge: %v\n", u.Uid, err)
					return nil
				}
				gid, err := strconv.Atoi(g.Gid)
				if err != nil {
					fmt.Fprintf(out, "  WARNING: Could not parse GID %q for group clawreachbridge: %v\n", g.Gid, err)
					return nil
				}
				if err := os.Chown(path, uid, gid); err != nil {
					fmt.Fprintf(out, "  WARNING: Could not set ownership to clawreachbridge:clawreachbridge: %v\n", err)
				}
			}
		}
	}

	return nil
}

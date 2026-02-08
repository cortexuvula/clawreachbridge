package setup

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// noopGatewayCheck skips the HTTP check in tests.
func noopGatewayCheck(io.Writer, string) {}

func testOpts(configPath, tailscaleIP string) WizardOptions {
	return WizardOptions{
		ConfigPath:      configPath,
		DetectTailscale: func() string { return tailscaleIP },
		CheckGateway:    noopGatewayCheck,
	}
}

func TestPrompt_WithInput(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("custom-value\n")
	scanner := bufio.NewScanner(in)

	result := prompt(scanner, &out, "Enter value: ", "default")
	if result != "custom-value" {
		t.Errorf("prompt() = %q, want %q", result, "custom-value")
	}
	if !strings.Contains(out.String(), "Enter value: ") {
		t.Error("prompt should print the message to out")
	}
}

func TestPrompt_EmptyInput(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("\n")
	scanner := bufio.NewScanner(in)

	result := prompt(scanner, &out, "Enter value: ", "default-val")
	if result != "default-val" {
		t.Errorf("prompt() = %q, want %q", result, "default-val")
	}
}

func TestPrompt_EOF(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("")
	scanner := bufio.NewScanner(in)

	result := prompt(scanner, &out, "Enter value: ", "fallback")
	if result != "fallback" {
		t.Errorf("prompt() = %q, want %q on EOF", result, "fallback")
	}
}

func TestGenerateConfig(t *testing.T) {
	content := generateConfig("100.64.1.1:8080", "http://localhost:18800", "https://gateway.local", "127.0.0.1:8081", "")
	if !strings.Contains(content, `listen_address: "100.64.1.1:8080"`) {
		t.Error("config should contain listen_address")
	}
	if !strings.Contains(content, `gateway_url: "http://localhost:18800"`) {
		t.Error("config should contain gateway_url")
	}
	if !strings.Contains(content, `auth_token: ""`) {
		t.Error("config should contain empty auth_token")
	}
	if !strings.Contains(content, "# REQUIRED") {
		t.Error("config should contain REQUIRED markers")
	}
}

func TestGenerateConfig_WithAuthToken(t *testing.T) {
	content := generateConfig("100.64.1.1:8080", "http://localhost:18800", "https://gateway.local", "127.0.0.1:8081", "mysecret")
	if !strings.Contains(content, `auth_token: "mysecret"`) {
		t.Error("config should contain the auth token")
	}
}

func TestWriteConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "config.yaml")
	content := "test: value\n"

	err := writeConfig(path, content, false)
	if err != nil {
		t.Fatalf("writeConfig() error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading written config: %v", err)
	}
	if string(data) != content {
		t.Errorf("config content = %q, want %q", string(data), content)
	}

	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0640 {
		t.Errorf("config permissions = %o, want 0640", info.Mode().Perm())
	}
}

func TestRunWizard_AllDefaults_WithTailscale(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	// When Tailscale is detected, wizard skips the manual IP prompt.
	// Prompts: gateway URL, listen port, health port, auth token
	input := strings.Join([]string{
		"", // gateway URL (accept default)
		"", // listen port (accept default)
		"", // health port (accept default)
		"", // auth token (none)
	}, "\n") + "\n"

	var out bytes.Buffer
	err := RunWizard(strings.NewReader(input), &out, testOpts(configPath, "100.64.1.1"))
	if err != nil {
		t.Fatalf("RunWizard() error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "Setup complete!") {
		t.Error("wizard should print completion message")
	}
	if !strings.Contains(output, "100.64.1.1") {
		t.Error("wizard output should mention detected Tailscale IP")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	if !strings.Contains(string(data), "100.64.1.1:8080") {
		t.Error("config should contain the listen address")
	}
}

func TestRunWizard_NoTailscale_ManualIP(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	// When Tailscale is NOT detected, wizard prompts for IP manually.
	// Prompts: gateway URL, listen port, tailscale IP, health port, auth token
	input := strings.Join([]string{
		"",           // gateway URL (accept default)
		"",           // listen port (accept default)
		"100.64.2.2", // tailscale IP (manual entry)
		"",           // health port (accept default)
		"",           // auth token (none)
	}, "\n") + "\n"

	var out bytes.Buffer
	err := RunWizard(strings.NewReader(input), &out, testOpts(configPath, ""))
	if err != nil {
		t.Fatalf("RunWizard() error: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	if !strings.Contains(string(data), "100.64.2.2:8080") {
		t.Error("config should contain the manually entered listen address")
	}
}

func TestRunWizard_CustomValues(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	// With Tailscale detected: gateway URL, listen port, health port, auth token
	input := strings.Join([]string{
		"http://localhost:9999", // custom gateway URL
		"9090",                 // custom listen port
		"9091",                 // custom health port
		"my-secret-token",      // auth token
	}, "\n") + "\n"

	var out bytes.Buffer
	err := RunWizard(strings.NewReader(input), &out, testOpts(configPath, "100.64.5.5"))
	if err != nil {
		t.Fatalf("RunWizard() error: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "100.64.5.5:9090") {
		t.Error("config should contain custom listen address")
	}
	if !strings.Contains(content, "http://localhost:9999") {
		t.Error("config should contain custom gateway URL")
	}
	if !strings.Contains(content, "127.0.0.1:9091") {
		t.Error("config should contain custom health address")
	}
	if !strings.Contains(content, `"my-secret-token"`) {
		t.Error("config should contain auth token")
	}
}

func TestRunWizard_ExistingConfig_NoOverwrite(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	// Create existing config
	os.WriteFile(configPath, []byte("existing"), 0640)

	// With Tailscale: gateway URL, listen port, health port, auth token, overwrite?
	input := strings.Join([]string{
		"", // gateway URL
		"", // listen port
		"", // health port
		"", // auth token
		"n", // don't overwrite
	}, "\n") + "\n"

	var out bytes.Buffer
	err := RunWizard(strings.NewReader(input), &out, testOpts(configPath, "100.64.1.1"))
	if err != nil {
		t.Fatalf("RunWizard() error: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	if string(data) != "existing" {
		t.Error("config should not be overwritten when user says no")
	}
	if !strings.Contains(out.String(), "Setup cancelled") {
		t.Error("should print cancellation message")
	}
}

func TestRunWizard_ExistingConfig_Overwrite(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	os.WriteFile(configPath, []byte("old"), 0640)

	input := strings.Join([]string{
		"", // gateway URL
		"", // listen port
		"", // health port
		"", // auth token
		"y", // overwrite
	}, "\n") + "\n"

	var out bytes.Buffer
	err := RunWizard(strings.NewReader(input), &out, testOpts(configPath, "100.64.1.1"))
	if err != nil {
		t.Fatalf("RunWizard() error: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	if !strings.Contains(string(data), "listen_address") {
		t.Error("config should be overwritten with new content")
	}
}

func TestRunWizard_EOF_NoTailscale(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	// EOF on stdin with no Tailscale — should fail because no IP provided
	var out bytes.Buffer
	err := RunWizard(strings.NewReader(""), &out, testOpts(configPath, ""))
	if err == nil {
		t.Error("RunWizard() should error when Tailscale IP is empty and not provided")
	}
}

func TestRunWizard_EOF_WithTailscale(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	// EOF on stdin with Tailscale detected — should use all defaults
	var out bytes.Buffer
	err := RunWizard(strings.NewReader(""), &out, testOpts(configPath, "100.64.1.1"))
	if err != nil {
		t.Fatalf("RunWizard() should succeed with Tailscale detected and all defaults: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	if !strings.Contains(string(data), "100.64.1.1:8080") {
		t.Error("config should contain the default listen address")
	}
}

func TestIsPortAvailable(t *testing.T) {
	_ = isPortAvailable("127.0.0.1", "0")
}

func TestDetectTailscaleIP(t *testing.T) {
	// Just verifies the function doesn't panic.
	_ = detectTailscaleIP()
}

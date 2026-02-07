package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestSetupStdout(t *testing.T) {
	lj := Setup("info", "json", "", 100, 3, 28, true)
	if lj != nil {
		t.Error("expected nil lumberjack logger for stdout")
	}

	// Verify we can log without panic
	slog.Info("test message", "key", "value")
}

func TestSetupTextFormat(t *testing.T) {
	lj := Setup("debug", "text", "", 100, 3, 28, false)
	if lj != nil {
		t.Error("expected nil lumberjack logger for stdout")
	}

	slog.Debug("debug message should appear")
}

func TestSetupFileLogging(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "test.log")

	lj := Setup("info", "json", logFile, 10, 1, 7, false)
	if lj == nil {
		t.Fatal("expected lumberjack logger for file output")
	}
	defer lj.Close()

	slog.Info("file log test", "key", "value")

	// Verify file was created
	info, err := os.Stat(logFile)
	if err != nil {
		t.Fatalf("log file not created: %v", err)
	}
	if info.Size() == 0 {
		t.Error("log file is empty")
	}
}

func TestSetupLogLevels(t *testing.T) {
	levels := []string{"debug", "info", "warn", "error", "unknown"}
	for _, level := range levels {
		t.Run(level, func(t *testing.T) {
			lj := Setup(level, "json", "", 100, 3, 28, true)
			if lj != nil {
				t.Error("expected nil lumberjack logger for stdout")
			}
		})
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"unknown", slog.LevelInfo}, // default fallback
		{"", slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseLevel(tt.input)
			if got != tt.want {
				t.Errorf("parseLevel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

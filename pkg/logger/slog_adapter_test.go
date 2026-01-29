//go:build !integration

package logger

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestSlogAdapter(t *testing.T) {
	// Only run if DEBUG is enabled
	if os.Getenv("DEBUG") == "" {
		t.Skip("Skipping test: DEBUG environment variable not set")
	}

	// Capture stderr output
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	// Create slog logger using our adapter
	slogLogger := NewSlogLogger("test:slog")

	// Test different log levels
	slogLogger.Info("info message", "key", "value")
	slogLogger.Debug("debug message", "count", 42)
	slogLogger.Warn("warning message")
	slogLogger.Error("error message", "error", "something went wrong")

	// Close write end and read output
	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// Restore stderr
	os.Stderr = oldStderr

	// Verify output contains expected messages
	if !strings.Contains(output, "[INFO] info message") {
		t.Errorf("Expected info message in output, got: %s", output)
	}
	if !strings.Contains(output, "[DEBUG] debug message") {
		t.Errorf("Expected debug message in output, got: %s", output)
	}
	if !strings.Contains(output, "[WARN] warning message") {
		t.Errorf("Expected warn message in output, got: %s", output)
	}
	if !strings.Contains(output, "[ERROR] error message") {
		t.Errorf("Expected error message in output, got: %s", output)
	}

	// Verify attributes are included
	if !strings.Contains(output, "key=value") {
		t.Errorf("Expected 'key=value' in output, got: %s", output)
	}
	if !strings.Contains(output, "count=42") {
		t.Errorf("Expected 'count=42' in output, got: %s", output)
	}
}

func TestSlogAdapterDisabled(t *testing.T) {
	// Only run if DEBUG is not set
	if os.Getenv("DEBUG") != "" {
		t.Skip("Skipping test: DEBUG environment variable is set")
	}

	// Capture stderr output
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	// Create slog logger using our adapter
	slogLogger := NewSlogLogger("test:slog")

	// Test logging (should be disabled)
	slogLogger.Info("info message", "key", "value")
	slogLogger.Debug("debug message")

	// Close write end and read output
	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// Restore stderr
	os.Stderr = oldStderr

	// Verify no output
	if output != "" {
		t.Errorf("Expected no output when logger is disabled, got: %s", output)
	}
}

func TestNewSlogLoggerWithHandler(t *testing.T) {
	// Only run if DEBUG is enabled
	if os.Getenv("DEBUG") == "" {
		t.Skip("Skipping test: DEBUG environment variable not set")
	}

	// Capture stderr output
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	// Create logger and then slog logger from it
	logger := New("test:handler")
	slogLogger := NewSlogLoggerWithHandler(logger)

	// Test logging
	slogLogger.Info("test message from handler")

	// Close write end and read output
	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// Restore stderr
	os.Stderr = oldStderr

	// Verify output contains expected message
	if !strings.Contains(output, "test:handler") {
		t.Errorf("Expected 'test:handler' namespace in output, got: %s", output)
	}
	if !strings.Contains(output, "[INFO] test message from handler") {
		t.Errorf("Expected info message in output, got: %s", output)
	}
}

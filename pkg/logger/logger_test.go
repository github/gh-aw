//go:build !integration

package logger

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	lipgloss "charm.land/lipgloss/v2"
)

func TestBuildColorPalette(t *testing.T) {
	if got := len(buildColorPalette()); got != 12 {
		t.Errorf("palette length = %d, want 12", got)
	}
}

func TestSelectNamespaceLabelBounds(t *testing.T) {
	originalPalette, originalDebugColors := colorPalette, debugColors
	t.Cleanup(func() {
		colorPalette, debugColors = originalPalette, originalDebugColors
	})
	debugColors = true

	for _, palette := range [][]lipgloss.Style{nil, {}, {lipgloss.NewStyle()}} {
		colorPalette = palette
		for _, namespace := range []string{"", "test:logger", "日本語"} {
			if got := selectNamespaceLabel(namespace); got != namespace {
				t.Errorf("label = %q, want %q (palette length %d)", got, namespace, len(palette))
			}
		}
	}
}

// captureStderr captures stderr output during test execution
func captureStderr(f func()) string {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	f()

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String()
}

func TestNew(t *testing.T) {
	tests := []struct {
		name      string
		debugEnv  string
		namespace string
		enabled   bool
	}{
		{
			name:      "empty DEBUG disables all loggers",
			debugEnv:  "",
			namespace: "test:logger",
			enabled:   false,
		},
		{
			name:      "wildcard enables all loggers",
			debugEnv:  "*",
			namespace: "test:logger",
			enabled:   true,
		},
		{
			name:      "exact match enables logger",
			debugEnv:  "test:logger",
			namespace: "test:logger",
			enabled:   true,
		},
		{
			name:      "exact match different namespace disabled",
			debugEnv:  "test:logger",
			namespace: "other:logger",
			enabled:   false,
		},
		{
			name:      "namespace wildcard enables matching loggers",
			debugEnv:  "test:*",
			namespace: "test:logger",
			enabled:   true,
		},
		{
			name:      "namespace wildcard matches deeply nested",
			debugEnv:  "test:*",
			namespace: "test:sub:logger",
			enabled:   true,
		},
		{
			name:      "namespace wildcard does not match different prefix",
			debugEnv:  "test:*",
			namespace: "other:logger",
			enabled:   false,
		},
		{
			name:      "multiple patterns with comma",
			debugEnv:  "test:*,other:*",
			namespace: "test:logger",
			enabled:   true,
		},
		{
			name:      "multiple patterns second matches",
			debugEnv:  "test:*,other:*",
			namespace: "other:logger",
			enabled:   true,
		},
		{
			name:      "exclusion pattern disables specific logger",
			debugEnv:  "test:*,-test:skip",
			namespace: "test:skip",
			enabled:   false,
		},
		{
			name:      "exclusion does not affect other loggers",
			debugEnv:  "test:*,-test:skip",
			namespace: "test:logger",
			enabled:   true,
		},
		{
			name:      "exclusion with wildcard",
			debugEnv:  "*,-test:*",
			namespace: "test:logger",
			enabled:   false,
		},
		{
			name:      "exclusion with wildcard allows others",
			debugEnv:  "*,-test:*",
			namespace: "other:logger",
			enabled:   true,
		},
		{
			name:      "suffix wildcard",
			debugEnv:  "*:logger",
			namespace: "test:logger",
			enabled:   true,
		},
		{
			name:      "suffix wildcard no match",
			debugEnv:  "*:logger",
			namespace: "test:other",
			enabled:   false,
		},
		{
			name:      "middle wildcard",
			debugEnv:  "test:*:end",
			namespace: "test:middle:end",
			enabled:   true,
		},
		{
			name:      "middle wildcard no match prefix",
			debugEnv:  "test:*:end",
			namespace: "other:middle:end",
			enabled:   false,
		},
		{
			name:      "middle wildcard no match suffix",
			debugEnv:  "test:*:end",
			namespace: "test:middle:other",
			enabled:   false,
		},
		{
			name:      "spaces in patterns are trimmed",
			debugEnv:  "test:* , other:*",
			namespace: "other:logger",
			enabled:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment for this test
			debugEnv = tt.debugEnv

			logger := New(tt.namespace)
			if logger.Enabled() != tt.enabled {
				t.Errorf("New(%q) with DEBUG=%q: enabled = %v, want %v",
					tt.namespace, tt.debugEnv, logger.Enabled(), tt.enabled)
			}
		})
	}
}

func TestLogger_Printf(t *testing.T) {
	tests := []struct {
		name      string
		debugEnv  string
		namespace string
		format    string
		args      []any
		wantLog   bool
	}{
		{
			name:      "enabled logger prints",
			debugEnv:  "*",
			namespace: "test:logger",
			format:    "hello %s",
			args:      []any{"world"},
			wantLog:   true,
		},
		{
			name:      "disabled logger does not print",
			debugEnv:  "",
			namespace: "test:logger",
			format:    "hello %s",
			args:      []any{"world"},
			wantLog:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment
			debugEnv = tt.debugEnv

			logger := New(tt.namespace)

			output := captureStderr(func() {
				logger.Printf(tt.format, tt.args...)
			})

			if tt.wantLog {
				if output == "" {
					t.Errorf("Printf() should have logged but got empty output")
				}
				if !strings.Contains(output, tt.namespace) {
					t.Errorf("Printf() output should contain namespace %q, got %q", tt.namespace, output)
				}
				expectedMessage := "hello world"
				if !strings.Contains(output, expectedMessage) {
					t.Errorf("Printf() output should contain %q, got %q", expectedMessage, output)
				}
			} else {
				if output != "" {
					t.Errorf("Printf() should not have logged but got %q", output)
				}
			}
		})
	}
}

func TestLogger_Print(t *testing.T) {
	// Set environment
	debugEnv = "*"

	logger := New("test:print")

	output := captureStderr(func() {
		logger.Print("hello", " ", "world")
	})

	if !strings.Contains(output, "test:print") {
		t.Errorf("Print() output should contain namespace, got %q", output)
	}
	if !strings.Contains(output, "hello world") {
		t.Errorf("Print() output should contain message, got %q", output)
	}
	// Check that time diff is included
	if !strings.Contains(output, "+") {
		t.Errorf("Print() output should contain time diff, got %q", output)
	}
}

func TestLogger_TimeDiff(t *testing.T) {
	// Set environment
	debugEnv = "*"

	logger := New("test:timediff")

	// First log
	output1 := captureStderr(func() {
		logger.Printf("first message")
	})

	// Small delay
	time.Sleep(10 * time.Millisecond)

	// Second log
	output2 := captureStderr(func() {
		logger.Printf("second message")
	})

	// Both should have time diff
	if !strings.Contains(output1, "+") {
		t.Errorf("First log should contain time diff, got %q", output1)
	}
	if !strings.Contains(output2, "+") {
		t.Errorf("Second log should contain time diff, got %q", output2)
	}

	// Second log should show at least 10ms diff
	if !strings.Contains(output2, "ms") && !strings.Contains(output2, "µs") {
		t.Errorf("Second log should show millisecond or microsecond time diff, got %q", output2)
	}
}

func TestColorSelection(t *testing.T) {
	// Test that selectNamespaceLabel returns consistent colors for the same namespace
	color1 := selectNamespaceLabel("test:namespace")
	color2 := selectNamespaceLabel("test:namespace")
	if color1 != color2 {
		t.Errorf("selectNamespaceLabel should return same color for same namespace")
	}
	if color1 == "" {
		t.Error("selectNamespaceLabel should return non-empty namespace label")
	}
	if !strings.Contains(color1, "test:namespace") {
		t.Errorf("selectNamespaceLabel should include namespace text, got %q", color1)
	}
	if !strings.Contains(color1, "\x1b[") {
		t.Errorf("selectNamespaceLabel should include ANSI styling when colors are enabled, got %q", color1)
	}

	// When colors are disabled, selectNamespaceLabel should return plain namespace text.
	origDebugColors := debugColors
	debugColors = false
	t.Cleanup(func() { debugColors = origDebugColors })
	if got := selectNamespaceLabel("plain:namespace"); got != "plain:namespace" {
		t.Errorf("selectNamespaceLabel should return plain namespace when debugColors=false, got %q", got)
	}
}

func TestNoColorEnvironment(t *testing.T) {
	// Save original values
	origDebugColors := debugColors
	defer func() {
		debugColors = origDebugColors
	}()

	debugEnv = "*"
	debugColors = true
	t.Setenv("NO_COLOR", "1")

	logger := New("test:namespace")
	output := captureStderr(func() {
		logger.Printf("hello")
	})
	if strings.Contains(output, "\x1b[") {
		t.Errorf("logger output should not contain ANSI color escapes when NO_COLOR is set, got %q", output)
	}
}

func TestInitDebugEnv(t *testing.T) {
	tests := []struct {
		name               string
		debugEnvVal        string
		actionsRunnerDebug string
		runnerDebug        string
		want               string
	}{
		{
			name:               "DEBUG takes precedence over ACTIONS_RUNNER_DEBUG",
			debugEnvVal:        "cli:*",
			actionsRunnerDebug: "true",
			want:               "cli:*",
		},
		{
			name:               "ACTIONS_RUNNER_DEBUG=true enables all loggers",
			debugEnvVal:        "",
			actionsRunnerDebug: "true",
			want:               "*",
		},
		{
			name:               "ACTIONS_RUNNER_DEBUG=false does not enable loggers",
			debugEnvVal:        "",
			actionsRunnerDebug: "false",
			want:               "",
		},
		{
			name:        "RUNNER_DEBUG=1 enables all loggers",
			runnerDebug: "1",
			want:        "*",
		},
		{
			name:        "RUNNER_DEBUG=0 does not enable loggers",
			runnerDebug: "0",
		},
		{
			name:        "DEBUG takes precedence over RUNNER_DEBUG",
			debugEnvVal: "cli:*",
			runnerDebug: "1",
			want:        "cli:*",
		},
		{
			name:               "neither set returns empty",
			debugEnvVal:        "",
			actionsRunnerDebug: "",
			want:               "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DEBUG", tt.debugEnvVal)
			t.Setenv("ACTIONS_RUNNER_DEBUG", tt.actionsRunnerDebug)
			t.Setenv("RUNNER_DEBUG", tt.runnerDebug)

			got := initDebugEnv()
			if got != tt.want {
				t.Errorf("initDebugEnv() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		pattern   string
		want      bool
	}{
		{"exact match", "test:logger", "test:logger", true},
		{"no match", "test:logger", "other:logger", false},
		{"wildcard all", "test:logger", "*", true},
		{"prefix wildcard", "test:logger", "test:*", true},
		{"prefix wildcard no match", "test:logger", "other:*", false},
		{"suffix wildcard", "test:logger", "*:logger", true},
		{"suffix wildcard no match", "test:logger", "*:other", false},
		{"middle wildcard", "test:middle:logger", "test:*:logger", true},
		{"middle wildcard no match prefix", "other:middle:logger", "test:*:logger", false},
		{"middle wildcard no match suffix", "test:middle:other", "test:*:logger", false},
		{"middle wildcard overlapping prefix suffix", "aba", "ab*ba", false},
		{"middle wildcard exact boundary", "abba", "ab*ba", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchPattern(tt.namespace, tt.pattern)
			if got != tt.want {
				t.Errorf("matchPattern(%q, %q) = %v, want %v", tt.namespace, tt.pattern, got, tt.want)
			}
		})
	}
}

func TestComputeEnabled(t *testing.T) {
	tests := []struct {
		name      string
		debugEnv  string
		namespace string
		want      bool
	}{
		{"single pattern match", "test:*", "test:logger", true},
		{"single pattern no match", "test:*", "other:logger", false},
		{"multiple patterns first match", "test:*,other:*", "test:logger", true},
		{"multiple patterns second match", "test:*,other:*", "other:logger", true},
		{"multiple patterns no match", "test:*,other:*", "third:logger", false},
		{"exclusion disables", "test:*,-test:skip", "test:skip", false},
		{"exclusion allows others", "test:*,-test:skip", "test:logger", true},
		{"exclusion wildcard", "*,-test:*", "test:logger", false},
		{"exclusion wildcard allows", "*,-test:*", "other:logger", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set DEBUG for this test
			debugEnv = tt.debugEnv
			got := computeEnabled(tt.namespace)
			if got != tt.want {
				t.Errorf("computeEnabled(%q) with DEBUG=%q = %v, want %v",
					tt.namespace, tt.debugEnv, got, tt.want)
			}
		})
	}
}

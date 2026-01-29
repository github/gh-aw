//go:build !integration

package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestParseAndDisplayActionlintOutput(t *testing.T) {
	tests := []struct {
		name           string
		stdout         string
		verbose        bool
		expectedOutput []string
		expectError    bool
		expectedCount  int
		expectedKinds  map[string]int
	}{
		{
			name: "single error",
			stdout: `[
{"message":"label \"ubuntu-slim\" is unknown. available labels are \"ubuntu-latest\", \"ubuntu-22.04\", \"ubuntu-20.04\", \"windows-latest\", \"windows-2022\", \"windows-2019\", \"macos-latest\", \"macos-13\", \"macos-12\", \"macos-11\". if it is a custom label for self-hosted runner, set list of labels in actionlint.yaml config file","filepath":".github/workflows/test.lock.yml","line":10,"column":14,"kind":"runner-label","snippet":"    runs-on: ubuntu-slim\n             ^~~~~~~~~~~","end_column":24}
]`,
			expectedOutput: []string{
				".github/workflows/test.lock.yml:10:14: error: [runner-label] label \"ubuntu-slim\" is unknown",
			},
			expectError:   false,
			expectedCount: 1,
			expectedKinds: map[string]int{"runner-label": 1},
		},
		{
			name: "multiple errors",
			stdout: `[
{"message":"label \"ubuntu-slim\" is unknown. available labels are \"ubuntu-latest\", \"ubuntu-22.04\", \"ubuntu-20.04\", \"windows-latest\", \"windows-2022\", \"windows-2019\", \"macos-latest\", \"macos-13\", \"macos-12\", \"macos-11\". if it is a custom label for self-hosted runner, set list of labels in actionlint.yaml config file","filepath":".github/workflows/test.lock.yml","line":10,"column":14,"kind":"runner-label","snippet":"    runs-on: ubuntu-slim\n             ^~~~~~~~~~~","end_column":24},
{"message":"shellcheck reported issue in this script: SC2086:info:1:8: Double quote to prevent globbing and word splitting","filepath":".github/workflows/test.lock.yml","line":25,"column":9,"kind":"shellcheck","snippet":"        run: |\n        ^~~~","end_column":12}
]`,
			expectedOutput: []string{
				".github/workflows/test.lock.yml:10:14: error: [runner-label] label \"ubuntu-slim\" is unknown",
				".github/workflows/test.lock.yml:25:9: error: [shellcheck] shellcheck reported issue",
			},
			expectError:   false,
			expectedCount: 2,
			expectedKinds: map[string]int{"runner-label": 1, "shellcheck": 1},
		},
		{
			name:           "no errors - empty output",
			stdout:         "",
			expectedOutput: []string{},
			expectError:    false,
			expectedCount:  0,
			expectedKinds:  map[string]int{},
		},
		{
			name:        "invalid JSON",
			stdout:      `{invalid json}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture stderr output
			originalStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			count, kinds, err := parseAndDisplayActionlintOutput(tt.stdout, tt.verbose)

			// Restore stderr and get output
			w.Close()
			os.Stderr = originalStderr
			var buf bytes.Buffer
			buf.ReadFrom(r)
			output := buf.String()

			// Check error expectation
			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			// Check count
			if count != tt.expectedCount {
				t.Errorf("Expected count %d, got %d", tt.expectedCount, count)
			}

			// Check kinds map
			if !tt.expectError && tt.expectedKinds != nil {
				if len(kinds) != len(tt.expectedKinds) {
					t.Errorf("Expected %d kinds, got %d", len(tt.expectedKinds), len(kinds))
				}
				for kind, expectedCount := range tt.expectedKinds {
					if kinds[kind] != expectedCount {
						t.Errorf("Expected %d errors of kind %s, got %d", expectedCount, kind, kinds[kind])
					}
				}
			}

			// Check expected output strings are present
			for _, expected := range tt.expectedOutput {
				if !strings.Contains(output, expected) {
					t.Errorf("Expected output to contain %q, but it didn't.\nGot: %s", expected, output)
				}
			}
		})
	}
}

func TestGetActionlintVersion(t *testing.T) {
	// Reset the cached version before test
	originalVersion := actionlintVersion
	defer func() { actionlintVersion = originalVersion }()

	tests := []struct {
		name          string
		presetVersion string
		expectCached  bool
	}{
		{
			name:          "first call fetches version",
			presetVersion: "",
			expectCached:  false,
		},
		{
			name:          "second call returns cached version",
			presetVersion: "1.7.9",
			expectCached:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actionlintVersion = tt.presetVersion

			// If we preset a version, this should return immediately
			if tt.expectCached {
				version, err := getActionlintVersion()
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if version != tt.presetVersion {
					t.Errorf("Expected cached version %q, got %q", tt.presetVersion, version)
				}
			}
		})
	}
}

func TestParseAndDisplayActionlintOutputMultiFile(t *testing.T) {
	tests := []struct {
		name           string
		stdout         string
		verbose        bool
		expectedOutput []string
		expectError    bool
		expectedCount  int
		expectedKinds  map[string]int
	}{
		{
			name: "multiple errors from multiple files",
			stdout: `[
{"message":"label \"ubuntu-slim\" is unknown","filepath":".github/workflows/test1.lock.yml","line":10,"column":14,"kind":"runner-label","snippet":"    runs-on: ubuntu-slim\n             ^~~~~~~~~~~","end_column":24},
{"message":"shellcheck reported issue","filepath":".github/workflows/test2.lock.yml","line":25,"column":9,"kind":"shellcheck","snippet":"        run: |\n        ^~~~","end_column":12}
]`,
			expectedOutput: []string{
				".github/workflows/test1.lock.yml:10:14: error: [runner-label]",
				".github/workflows/test2.lock.yml:25:9: error: [shellcheck]",
			},
			expectError:   false,
			expectedCount: 2,
			expectedKinds: map[string]int{"runner-label": 1, "shellcheck": 1},
		},
		{
			name: "errors from three files",
			stdout: `[
{"message":"error 1","filepath":".github/workflows/a.lock.yml","line":10,"column":1,"kind":"error","snippet":"test","end_column":5},
{"message":"error 2","filepath":".github/workflows/b.lock.yml","line":20,"column":1,"kind":"error","snippet":"test","end_column":5},
{"message":"error 3","filepath":".github/workflows/c.lock.yml","line":30,"column":1,"kind":"error","snippet":"test","end_column":5}
]`,
			expectedOutput: []string{
				".github/workflows/a.lock.yml:10:1",
				".github/workflows/b.lock.yml:20:1",
				".github/workflows/c.lock.yml:30:1",
			},
			expectError:   false,
			expectedCount: 3,
			expectedKinds: map[string]int{"error": 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture stderr output
			originalStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			count, kinds, err := parseAndDisplayActionlintOutput(tt.stdout, tt.verbose)

			// Restore stderr and get output
			w.Close()
			os.Stderr = originalStderr
			var buf bytes.Buffer
			buf.ReadFrom(r)
			output := buf.String()

			// Check error expectation
			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			// Check count
			if count != tt.expectedCount {
				t.Errorf("Expected count %d, got %d", tt.expectedCount, count)
			}

			// Check kinds map
			if !tt.expectError && tt.expectedKinds != nil {
				if len(kinds) != len(tt.expectedKinds) {
					t.Errorf("Expected %d kinds, got %d", len(tt.expectedKinds), len(kinds))
				}
				for kind, expectedCount := range tt.expectedKinds {
					if kinds[kind] != expectedCount {
						t.Errorf("Expected %d errors of kind %s, got %d", expectedCount, kind, kinds[kind])
					}
				}
			}

			// Check expected output strings are present
			for _, expected := range tt.expectedOutput {
				if !strings.Contains(output, expected) {
					t.Errorf("Expected output to contain %q, but it didn't.\nGot: %s", expected, output)
				}
			}
		})
	}
}

func TestDisplayActionlintSummary(t *testing.T) {
	tests := []struct {
		name             string
		stats            *ActionlintStats
		expectedContains []string
	}{
		{
			name: "summary with errors and warnings",
			stats: &ActionlintStats{
				TotalWorkflows: 5,
				TotalErrors:    10,
				TotalWarnings:  3,
				ErrorsByKind: map[string]int{
					"runner-label": 5,
					"shellcheck":   5,
				},
			},
			expectedContains: []string{
				"Actionlint Summary",
				"Checked 5 workflow(s)",
				"Found 13 issue(s)",
				"10 error(s), 3 warning(s)",
				"Issues by type:",
				"runner-label: 5",
				"shellcheck: 5",
			},
		},
		{
			name: "summary with only errors",
			stats: &ActionlintStats{
				TotalWorkflows: 3,
				TotalErrors:    7,
				TotalWarnings:  0,
				ErrorsByKind: map[string]int{
					"syntax": 7,
				},
			},
			expectedContains: []string{
				"Actionlint Summary",
				"Checked 3 workflow(s)",
				"Found 7 issue(s)",
				"7 error(s)",
				"Issues by type:",
				"syntax: 7",
			},
		},
		{
			name: "summary with no issues",
			stats: &ActionlintStats{
				TotalWorkflows: 10,
				TotalErrors:    0,
				TotalWarnings:  0,
				ErrorsByKind:   map[string]int{},
			},
			expectedContains: []string{
				"Actionlint Summary",
				"Checked 10 workflow(s)",
				"No issues found",
			},
		},
		{
			name:             "nil stats - no output",
			stats:            nil,
			expectedContains: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original stats and restore after test
			originalStats := actionlintStats
			defer func() { actionlintStats = originalStats }()

			// Set up test stats
			actionlintStats = tt.stats

			// Capture stderr output
			originalStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			displayActionlintSummary()

			// Restore stderr and get output
			w.Close()
			os.Stderr = originalStderr
			var buf bytes.Buffer
			buf.ReadFrom(r)
			output := buf.String()

			// Check expected strings are present
			for _, expected := range tt.expectedContains {
				if !strings.Contains(output, expected) {
					t.Errorf("Expected output to contain %q, but it didn't.\nGot: %s", expected, output)
				}
			}
		})
	}
}

func TestInitActionlintStats(t *testing.T) {
	// Save original stats and restore after test
	originalStats := actionlintStats
	defer func() { actionlintStats = originalStats }()

	// Initialize stats
	initActionlintStats()

	// Check that stats were initialized
	if actionlintStats == nil {
		t.Fatal("actionlintStats should not be nil after initialization")
	}
	if actionlintStats.TotalWorkflows != 0 {
		t.Errorf("TotalWorkflows should be 0, got %d", actionlintStats.TotalWorkflows)
	}
	if actionlintStats.TotalErrors != 0 {
		t.Errorf("TotalErrors should be 0, got %d", actionlintStats.TotalErrors)
	}
	if actionlintStats.TotalWarnings != 0 {
		t.Errorf("TotalWarnings should be 0, got %d", actionlintStats.TotalWarnings)
	}
	if actionlintStats.ErrorsByKind == nil {
		t.Error("ErrorsByKind should not be nil after initialization")
	}
	if len(actionlintStats.ErrorsByKind) != 0 {
		t.Errorf("ErrorsByKind should be empty, got %d entries", len(actionlintStats.ErrorsByKind))
	}
}

func TestGetActionlintDocsURL(t *testing.T) {
	tests := []struct {
		name     string
		kind     string
		expected string
	}{
		{
			name:     "empty kind returns base URL",
			kind:     "",
			expected: "https://github.com/rhysd/actionlint/blob/main/docs/checks.md",
		},
		{
			name:     "runner-label kind",
			kind:     "runner-label",
			expected: "https://github.com/rhysd/actionlint/blob/main/docs/checks.md#check-runner-labels",
		},
		{
			name:     "shellcheck kind",
			kind:     "shellcheck",
			expected: "https://github.com/rhysd/actionlint/blob/main/docs/checks.md#check-shellcheck-integ",
		},
		{
			name:     "pyflakes kind",
			kind:     "pyflakes",
			expected: "https://github.com/rhysd/actionlint/blob/main/docs/checks.md#check-pyflakes-integ",
		},
		{
			name:     "expression kind",
			kind:     "expression",
			expected: "https://github.com/rhysd/actionlint/blob/main/docs/checks.md#check-syntax-expression",
		},
		{
			name:     "generic kind with check- prefix",
			kind:     "check-job-deps",
			expected: "https://github.com/rhysd/actionlint/blob/main/docs/checks.md#check-job-deps",
		},
		{
			name:     "generic kind without check- prefix",
			kind:     "job-deps",
			expected: "https://github.com/rhysd/actionlint/blob/main/docs/checks.md#check-job-deps",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getActionlintDocsURL(tt.kind)
			if result != tt.expected {
				t.Errorf("getActionlintDocsURL(%q) = %q, want %q", tt.kind, result, tt.expected)
			}
		})
	}
}

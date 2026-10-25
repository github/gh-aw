//go:build !integration

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAndDisplayRunnerGuardOutput(t *testing.T) {
	tests := []struct {
		name           string
		stdout         string
		verbose        bool
		expectedOutput []string
		expectError    bool
		expectedCount  int
	}{
		{
			name: "single high severity finding",
			stdout: `{
  "findings": [
    {
      "rule_id": "RGS-001",
      "name": "Unsafe Runner Usage",
      "severity": "high",
      "description": "Runner pulls from untrusted source",
      "remediation": "Pin runner image digest",
      "file": ".github/workflows/test.lock.yml",
      "line": 15
    }
  ]
}`,
			expectedOutput: []string{
				".github/workflows/test.lock.yml:15:1",
				"error",
				"RGS-001",
				"Unsafe Runner Usage",
				"Runner pulls from untrusted source",
			},
			expectError:   false,
			expectedCount: 1,
		},
		{
			name: "critical severity maps to error type",
			stdout: `{
  "findings": [
    {
      "rule_id": "RGS-002",
      "name": "Critical Finding",
      "severity": "critical",
      "description": "Dangerous configuration",
      "file": ".github/workflows/test.lock.yml",
      "line": 10
    }
  ]
}`,
			expectedOutput: []string{
				"error",
				"RGS-002",
				"Critical Finding",
			},
			expectError:   false,
			expectedCount: 1,
		},
		{
			name: "note severity maps to info type",
			stdout: `{
  "findings": [
    {
      "rule_id": "RGS-003",
      "name": "Informational Finding",
      "severity": "note",
      "description": "Minor configuration note",
      "file": ".github/workflows/test.lock.yml",
      "line": 5
    }
  ]
}`,
			expectedOutput: []string{
				"info",
				"RGS-003",
				"Informational Finding",
			},
			expectError:   false,
			expectedCount: 1,
		},
		{
			name: "info severity maps to info type",
			stdout: `{
  "findings": [
    {
      "rule_id": "RGS-004",
      "name": "Info Finding",
      "severity": "info",
      "file": ".github/workflows/test.lock.yml",
      "line": 5
    }
  ]
}`,
			expectedOutput: []string{
				"info",
				"RGS-004",
			},
			expectError:   false,
			expectedCount: 1,
		},
		{
			name: "warning severity maps to warning type",
			stdout: `{
  "findings": [
    {
      "rule_id": "RGS-005",
      "name": "Warning Finding",
      "severity": "warning",
      "description": "A warning",
      "file": ".github/workflows/test.lock.yml",
      "line": 20
    }
  ]
}`,
			expectedOutput: []string{
				"warning",
				"RGS-005",
			},
			expectError:   false,
			expectedCount: 1,
		},
		{
			name: "finding with score and grade displayed",
			stdout: `{
  "findings": [
    {
      "rule_id": "RGS-001",
      "name": "Finding",
      "severity": "high",
      "file": ".github/workflows/test.lock.yml",
      "line": 5
    }
  ],
  "score": 80,
  "grade": "B"
}`,
			expectedOutput: []string{
				"Runner-Guard Score: 80/100 (Grade: B)",
				"RGS-001",
			},
			expectError:   false,
			expectedCount: 1,
		},
		{
			name: "finding without line number defaults to 1",
			stdout: `{
  "findings": [
    {
      "rule_id": "RGS-006",
      "name": "No Line Finding",
      "severity": "high",
      "file": ".github/workflows/test.lock.yml",
      "line": 0
    }
  ]
}`,
			expectedOutput: []string{
				".github/workflows/test.lock.yml:1:1",
				"RGS-006",
			},
			expectError:   false,
			expectedCount: 1,
		},
		{
			name: "multiple findings",
			stdout: `{
  "findings": [
    {
      "rule_id": "RGS-001",
      "name": "First Finding",
      "severity": "high",
      "file": ".github/workflows/test.lock.yml",
      "line": 10
    },
    {
      "rule_id": "RGS-002",
      "name": "Second Finding",
      "severity": "warning",
      "file": ".github/workflows/test.lock.yml",
      "line": 20
    }
  ]
}`,
			expectedOutput: []string{
				"RGS-001",
				"First Finding",
				"RGS-002",
				"Second Finding",
			},
			expectError:   false,
			expectedCount: 2,
		},
		{
			name: "no findings returns zero count",
			stdout: `{
  "findings": []
}`,
			expectedOutput: []string{},
			expectError:    false,
			expectedCount:  0,
		},
		{
			name:           "empty output returns zero count",
			stdout:         "",
			expectedOutput: []string{},
			expectError:    false,
			expectedCount:  0,
		},
		{
			name:           "invalid JSON returns error",
			stdout:         "not valid json",
			expectedOutput: []string{},
			expectError:    true,
			expectedCount:  0,
		},
		{
			name: "finding without description omits description from message",
			stdout: `{
  "findings": [
    {
      "rule_id": "RGS-007",
      "name": "No Description",
      "severity": "high",
      "description": "",
      "file": ".github/workflows/test.lock.yml",
      "line": 5
    }
  ]
}`,
			expectedOutput: []string{
				"[high] RGS-007: No Description",
			},
			expectError:   false,
			expectedCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture stderr output
			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			// Use a temp dir as gitRoot (no actual files — context display is skipped gracefully)
			tmpDir := t.TempDir()
			count, err := parseAndDisplayRunnerGuardOutputWithFindings(tt.stdout, tt.verbose, tmpDir, nil)

			// Restore stderr
			w.Close()
			os.Stderr = oldStderr

			// Read captured output
			var buf bytes.Buffer
			buf.ReadFrom(r)
			output := buf.String()

			// Check error expectation
			if tt.expectError && err == nil {
				t.Errorf("Expected an error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			// Verify finding count
			if count != tt.expectedCount {
				t.Errorf("Expected count %d, got %d", tt.expectedCount, count)
			}

			// Check expected output strings
			for _, expected := range tt.expectedOutput {
				if !strings.Contains(output, expected) {
					t.Errorf("Expected output to contain %q, got:\n%s", expected, output)
				}
			}
		})
	}
}

func TestRunnerGuardPathTraversalGuard(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		skip     bool // whether the finding should be skipped (outside git root)
	}{
		{
			name:     "normal workflow file",
			filePath: ".github/workflows/test.lock.yml",
			skip:     false,
		},
		{
			name:     "file outside git root via ..",
			filePath: "../outside/file.yml",
			skip:     true,
		},
		{
			name:     "file with .. prefix but inside root",
			filePath: "..foo/file.yml", // should NOT be skipped — not a parent traversal
			skip:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			stdout := `{"findings":[{"rule_id":"RGS-TEST","name":"Test","severity":"high","file":"` +
				tt.filePath + `","line":1}]}`

			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			count, err := parseAndDisplayRunnerGuardOutputWithFindings(stdout, false, tmpDir, nil)

			w.Close()
			os.Stderr = oldStderr

			var buf bytes.Buffer
			buf.ReadFrom(r)
			output := buf.String()

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if tt.skip {
				// Skipped findings still count toward totalFindings but won't appear in output
				// The finding is parsed (count=1) but display is skipped
				if count != 1 {
					t.Errorf("Expected count 1 (finding parsed even if skipped for display), got %d", count)
				}
				if strings.Contains(output, "RGS-TEST") {
					t.Errorf("Expected skipped finding not to appear in output, got:\n%s", output)
				}
			} else {
				if count != 1 {
					t.Errorf("Expected count 1, got %d", count)
				}
			}
		})
	}
}

func TestBuildRunnerGuardContainerScanPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		scanPath string
		want     string
		wantErr  string
	}{
		{name: "default current directory", scanPath: "", want: "./"},
		{name: "flag-looking path stays positional", scanPath: "--help", want: "./--help"},
		{name: "nested relative path", scanPath: "dir/subdir", want: "./dir/subdir"},
		{name: "path traversal rejected", scanPath: "../escape", wantErr: "must stay local"},
		{name: "control character rejected", scanPath: "bad\npath", wantErr: "invalid control characters"},
		{name: "unicode format character rejected", scanPath: "bad\u202epath", wantErr: "invalid control characters"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildRunnerGuardContainerScanPath(tt.scanPath)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestRunnerGuardDockerArgsPreservesDynamicValuesAsArguments(t *testing.T) {
	t.Parallel()
	volumeMount := "/tmp/checkout with spaces:/workdir"
	containerScanPath := "./--help"

	args := runnerGuardDockerArgs(RunnerGuardImage, volumeMount, containerScanPath)

	require.Equal(t, []string{
		"run",
		"--rm",
		"-v", volumeMount,
		"-w", "/workdir",
		RunnerGuardImage,
		"scan",
		containerScanPath,
		"--format", "json",
	}, args)
	require.Contains(t, shellJoinArgs(append([]string{"docker"}, args...)), "'/tmp/checkout with spaces:/workdir'")
}

func TestRunnerGuardDockerArgsShellEscapesBangInPaths(t *testing.T) {
	t.Parallel()
	volumeMount := "/tmp/repo!42:/workdir"
	containerScanPath := "./path!subdir"

	args := runnerGuardDockerArgs(RunnerGuardImage, volumeMount, containerScanPath)
	rendered := shellJoinArgs(append([]string{"docker"}, args...))

	require.Contains(t, rendered, "'/tmp/repo!42:/workdir'")
	require.Contains(t, rendered, "'./path!subdir'")
}

func TestRunnerGuardDirectoryScanPath(t *testing.T) {
	t.Parallel()
	gitRoot := t.TempDir()

	scanPath, err := runnerGuardDirectoryScanPath(filepath.Join(gitRoot, ".github", "workflows"), gitRoot)
	require.NoError(t, err)
	require.Equal(t, "./.github/workflows", scanPath)

	scanPath, err = runnerGuardDirectoryScanPath(filepath.Join(gitRoot, "--help"), gitRoot)
	require.NoError(t, err)
	require.Equal(t, "./--help", scanPath)

	_, err = runnerGuardDirectoryScanPath(filepath.Join(gitRoot, "..", "outside"), gitRoot)
	require.ErrorContains(t, err, "must stay within git root")
}

func TestRunRunnerGuardOnDirectoryRejectsPathsOutsideRepo(t *testing.T) {
	t.Parallel()
	outsideDir := filepath.Join(t.TempDir(), "outside")
	require.NoError(t, os.MkdirAll(outsideDir, 0o755))

	err := runRunnerGuardOnDirectory(outsideDir, false, false)
	require.Error(t, err)
	require.ErrorContains(t, err, "must stay within git root")
}

func TestRunnerGuardFindingsKeepUnknownColumn(t *testing.T) {
	shared := runnerGuardFindingsToShared([]runnerGuardFinding{
		{RuleID: "rule", Severity: "warning", File: "workflow.yml", Line: 5},
	}, nil)
	if len(shared) != 1 || shared[0].Column != 0 {
		t.Fatalf("expected unknown column 0, got %#v", shared)
	}
}

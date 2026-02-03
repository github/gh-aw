//go:build !integration

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestParseAndDisplayPoutineOutput(t *testing.T) {
	tests := []struct {
		name           string
		stdout         string
		targetFile     string
		verbose        bool
		expectedOutput []string
		expectError    bool
		expectedCount  int
	}{
		{
			name: "single file with error finding",
			stdout: `{
  "findings": [
    {
      "rule_id": "injection",
      "purl": "pkg:localrepo/localrepo/local?repository_url=.",
      "meta": {
        "path": ".github/workflows/test.lock.yml",
        "line": 30,
        "details": "Sources: github.event.inputs.name"
      }
    }
  ],
  "rules": {
    "injection": {
      "id": "injection",
      "title": "Injection with Arbitrary External Contributor Input",
      "description": "The pipeline contains an injection into bash or JavaScript",
      "level": "error"
    }
  }
}`,
			targetFile: ".github/workflows/test.lock.yml",
			expectedOutput: []string{
				".github/workflows/test.lock.yml:30:1: error: [error] injection: Injection with Arbitrary External Contributor Input - Sources: github.event.inputs.name",
			},
			expectError:   false,
			expectedCount: 1,
		},
		{
			name: "single file with warning finding",
			stdout: `{
  "findings": [
    {
      "rule_id": "pr_runs_on_self_hosted",
      "purl": "pkg:localrepo/localrepo/local?repository_url=.",
      "meta": {
        "path": ".github/workflows/test.lock.yml",
        "line": 112,
        "details": "runs-on: ubuntu-slim"
      }
    }
  ],
  "rules": {
    "pr_runs_on_self_hosted": {
      "id": "pr_runs_on_self_hosted",
      "title": "Pull Request Runs on Self-Hosted GitHub Actions Runner",
      "description": "This job runs on a self-hosted GitHub Actions runner",
      "level": "warning"
    }
  }
}`,
			targetFile: ".github/workflows/test.lock.yml",
			expectedOutput: []string{
				".github/workflows/test.lock.yml:112:1: warning: [warning] pr_runs_on_self_hosted: Pull Request Runs on Self-Hosted GitHub Actions Runner - runs-on: ubuntu-slim",
			},
			expectError:   false,
			expectedCount: 1,
		},
		{
			name: "single file with note finding",
			stdout: `{
  "findings": [
    {
      "rule_id": "unpinnable_action",
      "purl": "pkg:localrepo/localrepo/local?repository_url=.",
      "meta": {
        "path": ".github/workflows/test.lock.yml",
        "line": 5
      }
    }
  ],
  "rules": {
    "unpinnable_action": {
      "id": "unpinnable_action",
      "title": "Unpinnable CI component used",
      "description": "Pinning this GitHub Action is likely ineffective",
      "level": "note"
    }
  }
}`,
			targetFile: ".github/workflows/test.lock.yml",
			expectedOutput: []string{
				".github/workflows/test.lock.yml:5:1: info: [note] unpinnable_action: Unpinnable CI component used",
			},
			expectError:   false,
			expectedCount: 1,
		},
		{
			name: "multiple findings in same file",
			stdout: `{
  "findings": [
    {
      "rule_id": "injection",
      "purl": "pkg:localrepo/localrepo/local?repository_url=.",
      "meta": {
        "path": ".github/workflows/test.lock.yml",
        "line": 30,
        "details": "Sources: github.event.inputs.name"
      }
    },
    {
      "rule_id": "pr_runs_on_self_hosted",
      "purl": "pkg:localrepo/localrepo/local?repository_url=.",
      "meta": {
        "path": ".github/workflows/test.lock.yml",
        "line": 112,
        "details": "runs-on: ubuntu-slim"
      }
    }
  ],
  "rules": {
    "injection": {
      "id": "injection",
      "title": "Injection with Arbitrary External Contributor Input",
      "level": "error"
    },
    "pr_runs_on_self_hosted": {
      "id": "pr_runs_on_self_hosted",
      "title": "Pull Request Runs on Self-Hosted GitHub Actions Runner",
      "level": "warning"
    }
  }
}`,
			targetFile: ".github/workflows/test.lock.yml",
			expectedOutput: []string{
				".github/workflows/test.lock.yml:30:1: error: [error] injection: Injection with Arbitrary External Contributor Input - Sources: github.event.inputs.name",
				".github/workflows/test.lock.yml:112:1: warning: [warning] pr_runs_on_self_hosted: Pull Request Runs on Self-Hosted GitHub Actions Runner - runs-on: ubuntu-slim",
			},
			expectError:   false,
			expectedCount: 2,
		},
		{
			name:           "file with no findings",
			stdout:         `{"findings":[],"rules":{}}`,
			targetFile:     ".github/workflows/clean.lock.yml",
			expectedOutput: []string{
				// No output expected for 0 warnings
			},
			expectError:   false,
			expectedCount: 0,
		},
		{
			name: "multiple files - filter to target file only",
			stdout: `{
  "findings": [
    {
      "rule_id": "injection",
      "purl": "pkg:localrepo/localrepo/local?repository_url=.",
      "meta": {
        "path": ".github/workflows/test1.lock.yml",
        "line": 30,
        "details": "Sources: github.event.inputs.name"
      }
    },
    {
      "rule_id": "pr_runs_on_self_hosted",
      "purl": "pkg:localrepo/localrepo/local?repository_url=.",
      "meta": {
        "path": ".github/workflows/test2.lock.yml",
        "line": 112,
        "details": "runs-on: ubuntu-slim"
      }
    }
  ],
  "rules": {
    "injection": {
      "id": "injection",
      "title": "Injection with Arbitrary External Contributor Input",
      "level": "error"
    },
    "pr_runs_on_self_hosted": {
      "id": "pr_runs_on_self_hosted",
      "title": "Pull Request Runs on Self-Hosted GitHub Actions Runner",
      "level": "warning"
    }
  }
}`,
			targetFile: ".github/workflows/test1.lock.yml",
			expectedOutput: []string{
				".github/workflows/test1.lock.yml:30:1: error: [error] injection: Injection with Arbitrary External Contributor Input - Sources: github.event.inputs.name",
			},
			expectError:   false,
			expectedCount: 1,
		},
		{
			name: "finding without line number",
			stdout: `{
  "findings": [
    {
      "rule_id": "test_rule",
      "purl": "pkg:localrepo/localrepo/local?repository_url=.",
      "meta": {
        "path": ".github/workflows/test.lock.yml"
      }
    }
  ],
  "rules": {
    "test_rule": {
      "id": "test_rule",
      "title": "Test Rule",
      "level": "warning"
    }
  }
}`,
			targetFile: ".github/workflows/test.lock.yml",
			expectedOutput: []string{
				".github/workflows/test.lock.yml:1:1: warning: [warning] test_rule: Test Rule",
			},
			expectError:   false,
			expectedCount: 1,
		},
		{
			name:           "empty output",
			stdout:         "",
			targetFile:     ".github/workflows/test.lock.yml",
			expectedOutput: []string{},
			expectError:    false,
			expectedCount:  0,
		},
		{
			name:           "invalid JSON",
			stdout:         "not valid json",
			targetFile:     ".github/workflows/test.lock.yml",
			expectedOutput: []string{},
			expectError:    true,
			expectedCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture stderr output
			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			warningCount, err := parseAndDisplayPoutineOutput(tt.stdout, tt.targetFile, tt.verbose)

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

			// Verify warning count
			if warningCount != tt.expectedCount {
				t.Errorf("Expected warning count %d, got: %d", tt.expectedCount, warningCount)
			}

			// Check expected output
			for _, expected := range tt.expectedOutput {
				if !strings.Contains(output, expected) {
					t.Errorf("Expected output to contain %q, but got:\n%s", expected, output)
				}
			}
		})
	}
}

func TestEnsurePoutineConfig(t *testing.T) {
	// Create a temporary directory
	tmpDir := testutil.TempDir(t, "test-*")

	t.Run("creates config file when it doesn't exist", func(t *testing.T) {
		err := ensurePoutineConfig(tmpDir)
		if err != nil {
			t.Fatalf("ensurePoutineConfig failed: %v", err)
		}

		// Check that the config file was created
		configPath := filepath.Join(tmpDir, ".poutine.yml")
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			t.Errorf("Config file was not created at %s", configPath)
		}

		// Read and verify the content
		content, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("Failed to read config file: %v", err)
		}

		expectedStrings := []string{
			"# Configure poutine security scanner",
			"rulesConfig:",
			"pr_runs_on_self_hosted:",
			"allowed_runners:",
			"- ubuntu-slim",
		}

		for _, expected := range expectedStrings {
			if !strings.Contains(string(content), expected) {
				t.Errorf("Config file does not contain expected string %q. Content:\n%s", expected, string(content))
			}
		}
	})

	t.Run("does not overwrite existing config file", func(t *testing.T) {
		// Create a different temporary directory
		tmpDir2 := testutil.TempDir(t, "test-*")
		configPath := filepath.Join(tmpDir2, ".poutine.yml")

		// Create a custom config file
		customContent := "# My custom poutine config\ncustom: value\n"
		err := os.WriteFile(configPath, []byte(customContent), 0644)
		if err != nil {
			t.Fatalf("Failed to create custom config file: %v", err)
		}

		// Call ensurePoutineConfig
		err = ensurePoutineConfig(tmpDir2)
		if err != nil {
			t.Fatalf("ensurePoutineConfig failed: %v", err)
		}

		// Read the file and verify it wasn't changed
		content, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("Failed to read config file: %v", err)
		}

		if string(content) != customContent {
			t.Errorf("Existing config file was overwritten. Expected:\n%s\nGot:\n%s", customContent, string(content))
		}
	})
}

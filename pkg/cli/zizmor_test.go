//go:build !integration

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseAndDisplayZizmorOutput(t *testing.T) {
	tests := []struct {
		name                      string
		stdout                    string
		stderr                    string
		verbose                   bool
		expectedOutput            []string
		expectError               bool
		expectedHighSeverityCount int
	}{
		{
			name: "single file with findings",
			stdout: `[
  {
    "ident": "excessive-permissions",
    "desc": "overly broad permissions",
    "url": "https://docs.zizmor.sh/audits/#excessive-permissions",
    "determinations": {
      "severity": "Medium"
    },
    "locations": [
      {
        "symbolic": {
          "key": {
            "Local": {
              "given_path": "./.github/workflows/test.lock.yml"
            }
          },
          "annotation": "uses write-all permissions"
        },
        "concrete": {
          "location": {
            "start_point": {
              "row": 6,
              "column": 4
            }
          }
        }
      }
    ]
  }
]`,
			stderr: " INFO audit: zizmor: 🌈 completed ./.github/workflows/test.lock.yml\n",
			expectedOutput: []string{
				"./.github/workflows/test.lock.yml:7:5: warning: [Medium] excessive-permissions: overly broad permissions (https://docs.zizmor.sh/audits/#excessive-permissions)",
			},
			expectError: false,
		},
		{
			name: "current zizmor output uses verbatim path",
			stdout: `[
  {
    "ident": "undocumented-permissions",
    "desc": "permissions without explanatory comments",
    "url": "https://docs.zizmor.sh/audits/#undocumented-permissions",
    "determinations": {
      "severity": "Low"
    },
    "locations": [
      {
        "symbolic": {
          "key": {
            "Local": {
              "verbatim_path": "./.github/workflows/test.lock.yml"
            }
          },
          "annotation": "needs an explanatory comment"
        },
        "concrete": {
          "location": {
            "start_point": {
              "row": 6,
              "column": 4
            }
          }
        }
      }
    ]
  }
]`,
			stderr: " INFO audit: zizmor: 🌈 completed ./.github/workflows/test.lock.yml\n",
			expectedOutput: []string{
				"./.github/workflows/test.lock.yml:7:5: info: [Low] undocumented-permissions: permissions without explanatory comments (https://docs.zizmor.sh/audits/#undocumented-permissions)",
			},
			expectError: false,
		},
		{
			name: "multiple findings in same file",
			stdout: `[
  {
    "ident": "excessive-permissions",
    "desc": "overly broad permissions",
    "url": "https://docs.zizmor.sh/audits/#excessive-permissions",
    "determinations": {
      "severity": "Medium"
    },
    "locations": [
      {
        "symbolic": {
          "key": {
            "Local": {
              "given_path": "./.github/workflows/test.lock.yml"
            }
          },
          "annotation": "uses write-all permissions"
        },
        "concrete": {
          "location": {
            "start_point": {
              "row": 6,
              "column": 4
            }
          }
        }
      }
    ]
  },
  {
    "ident": "template-injection",
    "desc": "template injection with untrusted input",
    "url": "https://docs.zizmor.sh/audits/#template-injection",
    "determinations": {
      "severity": "High"
    },
    "locations": [
      {
        "symbolic": {
          "key": {
            "Local": {
              "given_path": "./.github/workflows/test.lock.yml"
            }
          },
          "annotation": "may expand into attacker-controllable code"
        },
        "concrete": {
          "location": {
            "start_point": {
              "row": 11,
              "column": 23
            }
          }
        }
      }
    ]
  }
]`,
			stderr: " INFO audit: zizmor: 🌈 completed ./.github/workflows/test.lock.yml\n",
			expectedOutput: []string{
				"./.github/workflows/test.lock.yml:7:5: warning: [Medium] excessive-permissions: overly broad permissions (https://docs.zizmor.sh/audits/#excessive-permissions)",
				"./.github/workflows/test.lock.yml:12:24: error: [High] template-injection: template injection with untrusted input (https://docs.zizmor.sh/audits/#template-injection)",
			},
			expectError:               false,
			expectedHighSeverityCount: 1,
		},
		{
			name:           "file with no findings",
			stdout:         "[]",
			stderr:         " INFO audit: zizmor: 🌈 completed ./.github/workflows/clean.lock.yml\n",
			expectedOutput: []string{
				// No output expected for 0 warnings
			},
			expectError: false,
		},
		{
			name: "multiple files",
			stdout: `[
  {
    "ident": "excessive-permissions",
    "desc": "overly broad permissions",
    "url": "https://docs.zizmor.sh/audits/#excessive-permissions",
    "determinations": {
      "severity": "Medium"
    },
    "locations": [
      {
        "symbolic": {
          "key": {
            "Local": {
              "given_path": "./.github/workflows/test1.lock.yml"
            }
          },
          "annotation": "uses write-all permissions"
        },
        "concrete": {
          "location": {
            "start_point": {
              "row": 6,
              "column": 4
            }
          }
        }
      }
    ]
  },
  {
    "ident": "template-injection",
    "desc": "template injection with untrusted input",
    "url": "https://docs.zizmor.sh/audits/#template-injection",
    "determinations": {
      "severity": "High"
    },
    "locations": [
      {
        "symbolic": {
          "key": {
            "Local": {
              "given_path": "./.github/workflows/test2.lock.yml"
            }
          },
          "annotation": "may expand into attacker-controllable code"
        },
        "concrete": {
          "location": {
            "start_point": {
              "row": 11,
              "column": 23
            }
          }
        }
      }
    ]
  }
]`,
			stderr: " INFO audit: zizmor: 🌈 completed ./.github/workflows/test1.lock.yml\n INFO audit: zizmor: 🌈 completed ./.github/workflows/test2.lock.yml\n",
			expectedOutput: []string{
				"./.github/workflows/test1.lock.yml:7:5: warning: [Medium] excessive-permissions: overly broad permissions (https://docs.zizmor.sh/audits/#excessive-permissions)",
				"./.github/workflows/test2.lock.yml:12:24: error: [High] template-injection: template injection with untrusted input (https://docs.zizmor.sh/audits/#template-injection)",
			},
			expectError:               false,
			expectedHighSeverityCount: 1,
		},
		{
			name: "finding with multiple locations in same file counts as one",
			stdout: `[
  {
    "ident": "excessive-permissions",
    "desc": "overly broad permissions",
    "url": "https://docs.zizmor.sh/audits/#excessive-permissions",
    "determinations": {
      "severity": "Medium"
    },
    "locations": [
      {
        "symbolic": {
          "key": {
            "Local": {
              "given_path": "./.github/workflows/test.lock.yml"
            }
          },
          "annotation": "uses write-all permissions"
        },
        "concrete": {
          "location": {
            "start_point": {
              "row": 6,
              "column": 4
            }
          }
        }
      },
      {
        "symbolic": {
          "key": {
            "Local": {
              "given_path": "./.github/workflows/test.lock.yml"
            }
          },
          "annotation": "another location"
        },
        "concrete": {
          "location": {
            "start_point": {
              "row": 10,
              "column": 8
            }
          }
        }
      }
    ]
  }
]`,
			stderr: " INFO audit: zizmor: 🌈 completed ./.github/workflows/test.lock.yml\n",
			expectedOutput: []string{
				"./.github/workflows/test.lock.yml:7:5: warning: [Medium] excessive-permissions: overly broad permissions (https://docs.zizmor.sh/audits/#excessive-permissions)",
			},
			expectError: false,
		},
		{
			name: "findings displayed even when stderr has no completed messages (log format change fallback)",
			stdout: `[
  {
    "ident": "excessive-permissions",
    "desc": "overly broad permissions",
    "url": "https://docs.zizmor.sh/audits/#excessive-permissions",
    "determinations": {
      "severity": "Medium"
    },
    "locations": [
      {
        "symbolic": {
          "key": {
            "Local": {
              "given_path": "./.github/workflows/test.lock.yml"
            }
          },
          "annotation": "uses write-all permissions"
        },
        "concrete": {
          "location": {
            "start_point": {
              "row": 6,
              "column": 4
            }
          }
        }
      }
    ]
  }
]`,
			// No "completed" messages in stderr — simulates a zizmor log format change
			stderr: "some other stderr output without completed markers\n",
			expectedOutput: []string{
				"./.github/workflows/test.lock.yml:7:5: warning: [Medium] excessive-permissions: overly broad permissions (https://docs.zizmor.sh/audits/#excessive-permissions)",
			},
			expectError: false,
		},
		{
			name: "partial stderr markers: file absent from completed list still shows findings",
			stdout: `[
  {
    "ident": "excessive-permissions",
    "desc": "overly broad permissions",
    "url": "https://docs.zizmor.sh/audits/#excessive-permissions",
    "determinations": {
      "severity": "Medium"
    },
    "locations": [
      {
        "symbolic": {
          "key": {
            "Local": {
              "given_path": "./.github/workflows/test1.lock.yml"
            }
          },
          "annotation": "uses write-all permissions"
        },
        "concrete": {
          "location": {
            "start_point": {
              "row": 6,
              "column": 4
            }
          }
        }
      }
    ]
  },
  {
    "ident": "template-injection",
    "desc": "template injection with untrusted input",
    "url": "https://docs.zizmor.sh/audits/#template-injection",
    "determinations": {
      "severity": "High"
    },
    "locations": [
      {
        "symbolic": {
          "key": {
            "Local": {
              "given_path": "./.github/workflows/test2.lock.yml"
            }
          },
          "annotation": "may expand into attacker-controllable code"
        },
        "concrete": {
          "location": {
            "start_point": {
              "row": 11,
              "column": 23
            }
          }
        }
      }
    ]
  }
]`,
			// Only test1 has a "completed" marker; test2's marker was dropped (partial log)
			stderr: " INFO audit: zizmor: 🌈 completed ./.github/workflows/test1.lock.yml\n",
			expectedOutput: []string{
				// test1 shows first (from completedFiles ordering)
				"./.github/workflows/test1.lock.yml:7:5: warning: [Medium] excessive-permissions: overly broad permissions (https://docs.zizmor.sh/audits/#excessive-permissions)",
				// test2 still shows (appended in sorted order as extra file)
				"./.github/workflows/test2.lock.yml:12:24: error: [High] template-injection: template injection with untrusted input (https://docs.zizmor.sh/audits/#template-injection)",
			},
			expectError:               false,
			expectedHighSeverityCount: 1,
		},
		{
			name: "critical severity finding is counted as high severity",
			stdout: `[
  {
    "ident": "template-injection",
    "desc": "template injection with untrusted input",
    "url": "https://docs.zizmor.sh/audits/#template-injection",
    "determinations": {
      "severity": "Critical"
    },
    "locations": [
      {
        "symbolic": {
          "key": {
            "Local": {
              "given_path": "./.github/workflows/test.lock.yml"
            }
          },
          "annotation": "may expand into attacker-controllable code"
        },
        "concrete": {
          "location": {
            "start_point": {
              "row": 11,
              "column": 23
            }
          }
        }
      }
    ]
  }
]`,
			stderr: " INFO audit: zizmor: 🌈 completed ./.github/workflows/test.lock.yml\n",
			expectedOutput: []string{
				"./.github/workflows/test.lock.yml:12:24: error: [Critical] template-injection: template injection with untrusted input (https://docs.zizmor.sh/audits/#template-injection)",
			},
			expectError:               false,
			expectedHighSeverityCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture stderr output
			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			warningCount, highSeverityCount, err := parseAndDisplayZizmorOutput(tt.stdout, tt.stderr, tt.verbose)

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

			// Verify warning count is non-negative
			if warningCount < 0 {
				t.Errorf("Warning count should be non-negative, got: %d", warningCount)
			}

			// Verify high severity count
			if highSeverityCount != tt.expectedHighSeverityCount {
				t.Errorf("Expected high severity count %d, got %d", tt.expectedHighSeverityCount, highSeverityCount)
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

func TestBuildZizmorContainerScanPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		scanPath string
		want     string
		wantErr  string
	}{
		{name: "nested relative path", scanPath: ".github/workflows/a.lock.yml", want: "./.github/workflows/a.lock.yml"},
		{name: "flag-looking path stays positional", scanPath: "--help", want: "./--help"},
		{name: "path traversal rejected", scanPath: "../escape.lock.yml", wantErr: "must stay local"},
		{name: "empty path rejected", scanPath: "", wantErr: "cannot be empty"},
		{name: "absolute path rejected", scanPath: "/etc/passwd", wantErr: "must stay local"},
		{name: "control character rejected", scanPath: "bad\npath.lock.yml", wantErr: "invalid control characters"},
		{name: "unicode format character rejected", scanPath: "bad\u202epath.lock.yml", wantErr: "invalid control characters"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildZizmorContainerScanPath(tt.scanPath)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestZizmorImageIsPinnedAndValid(t *testing.T) {
	t.Parallel()
	ref, err := validateDockerImageRef(ZizmorImage)
	if err != nil {
		t.Fatalf("ZizmorImage must be a valid docker image reference: %v", err)
	}
	if !strings.Contains(ref, "@sha256:") {
		t.Fatalf("ZizmorImage must be pinned by digest, got %q", ref)
	}
}

func TestZizmorScanPathsRejectsSymlinkEscape(t *testing.T) {
	t.Parallel()
	gitRoot := t.TempDir()
	outside := t.TempDir()

	escapeTarget := filepath.Join(outside, "secret.lock.yml")
	if err := os.WriteFile(escapeTarget, []byte("outside"), 0o600); err != nil {
		t.Fatalf("failed to write outside file: %v", err)
	}

	linkPath := filepath.Join(gitRoot, "escape.lock.yml")
	if err := os.Symlink(escapeTarget, linkPath); err != nil {
		t.Skipf("symlinks not supported on this platform: %v", err)
	}

	_, _, err := zizmorScanPaths(gitRoot, []string{linkPath})
	if err == nil {
		t.Fatal("expected error for lock file symlink escaping git root, got nil")
	}
	if !strings.Contains(err.Error(), "escapes base directory") {
		t.Fatalf("expected escape error, got %q", err.Error())
	}
}

func TestZizmorScanPathsAcceptsFileWithinGitRoot(t *testing.T) {
	t.Parallel()
	gitRoot := t.TempDir()
	lockDir := filepath.Join(gitRoot, ".github", "workflows")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatalf("failed to create lock dir: %v", err)
	}
	lockFile := filepath.Join(lockDir, "a.lock.yml")
	if err := os.WriteFile(lockFile, []byte("workflow"), 0o600); err != nil {
		t.Fatalf("failed to write lock file: %v", err)
	}

	relPaths, containerPaths, err := zizmorScanPaths(gitRoot, []string{lockFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(relPaths) != 1 || relPaths[0] != filepath.Join(".github", "workflows", "a.lock.yml") {
		t.Fatalf("unexpected relPaths: %v", relPaths)
	}
	if len(containerPaths) != 1 || containerPaths[0] != "./.github/workflows/a.lock.yml" {
		t.Fatalf("unexpected containerPaths: %v", containerPaths)
	}
}

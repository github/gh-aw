//go:build !integration

package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestParseAndDisplayZizmorOutput(t *testing.T) {
	tests := []struct {
		name           string
		stdout         string
		stderr         string
		verbose        bool
		expectedOutput []string
		expectError    bool
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
			expectError: false,
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
			expectError: false,
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture stderr output
			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			warningCount, err := parseAndDisplayZizmorOutput(tt.stdout, tt.stderr, tt.verbose)

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

			// Check expected output
			for _, expected := range tt.expectedOutput {
				if !strings.Contains(output, expected) {
					t.Errorf("Expected output to contain %q, but got:\n%s", expected, output)
				}
			}
		})
	}
}

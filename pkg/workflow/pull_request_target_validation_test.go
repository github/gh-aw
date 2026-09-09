//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestPullRequestTargetValidation tests security validation for the pull_request_target trigger.
func TestPullRequestTargetValidation(t *testing.T) {
	tmpDir := testutil.TempDir(t, "prt-validation-test")

	tests := []struct {
		name          string
		frontmatter   string
		filename      string
		strictMode    bool
		expectError   bool
		expectWarning bool
		errorContains string
		warningCount  int
	}{
		// ---- non-strict mode ----

		{
			name: "pull_request_target with checkout disabled - non-strict - no warning",
			frontmatter: `---
strict: false
on:
  pull_request_target:
    types: [opened]
tools:
  github: false
features:
  dangerously-disable-sandbox-agent: true
sandbox:
  agent: false
checkout: false
---

# PR Target Workflow
Test workflow content.`,
			filename:      "prt-checkout-false-non-strict.md",
			strictMode:    false,
			expectError:   false,
			expectWarning: false,
			warningCount:  0,
		},
		{
			name: "pull_request_target with no checkout key - non-strict - insecure checkout warning",
			frontmatter: `---
strict: false
on:
  pull_request_target:
    types: [opened]
tools:
  github: false
features:
  dangerously-disable-sandbox-agent: true
sandbox:
  agent: false
---

# PR Target Workflow
Test workflow content.`,
			filename:      "prt-checkout-enabled-non-strict.md",
			strictMode:    false,
			expectError:   false,
			expectWarning: true,
			warningCount:  1, // insecure-checkout warning (non-strict mode)
		},
		{
			name: "pull_request_target with trusted checkout - non-strict - no warnings no error",
			frontmatter: `---
strict: false
on:
  pull_request_target:
    types: [opened]
tools:
  github:
    toolsets: [pull_requests]
permissions:
  pull-requests: read
checkout:
  repository: ${{ github.repository }}
  ref: ${{ github.event.pull_request.base.sha }}
---

# PR Target Non-Strict Trusted Checkout
Test workflow content.`,
			filename:      "prt-checkout-trusted-non-strict.md",
			strictMode:    false,
			expectError:   false,
			expectWarning: false,
			warningCount:  0,
		},
		{
			name: "pull_request trigger (not target) - non-strict - no diagnostic",
			frontmatter: `---
strict: false
on:
  pull_request:
    types: [opened]
tools:
  github: false
features:
  dangerously-disable-sandbox-agent: true
sandbox:
  agent: false
---

# PR Workflow
Test workflow content.`,
			filename:      "pr-non-strict.md",
			strictMode:    false,
			expectError:   false,
			expectWarning: false,
			warningCount:  0,
		},
		{
			name: "push trigger - non-strict - no diagnostic",
			frontmatter: `---
strict: false
on:
  push:
    branches: [main]
tools:
  github: false
features:
  dangerously-disable-sandbox-agent: true
sandbox:
  agent: false
---

# Push Workflow
Test workflow content.`,
			filename:      "push-non-strict.md",
			strictMode:    false,
			expectError:   false,
			expectWarning: false,
			warningCount:  0,
		},

		// ---- strict mode ----

		{
			name: "pull_request_target with checkout disabled - strict - dangerous-trigger warning",
			frontmatter: `---
on:
  pull_request_target:
    types: [opened]
tools:
  github:
    toolsets: [pull_requests]
permissions:
  pull-requests: read
checkout: false
---

# PR Target Strict Workflow
Test workflow content.`,
			filename:      "prt-checkout-false-strict.md",
			strictMode:    true,
			expectError:   false,
			expectWarning: true,
			warningCount:  1, // dangerous-trigger warning
		},
		{
			name: "pull_request_target with no checkout key - strict - insecure checkout error",
			frontmatter: `---
on:
  pull_request_target:
    types: [opened]
tools:
  github:
    toolsets: [pull_requests]
permissions:
  pull-requests: read
---

# PR Target Strict No Checkout
Test workflow content.`,
			filename:      "prt-checkout-enabled-strict.md",
			strictMode:    true,
			expectError:   true,
			expectWarning: true,
			errorContains: "pull_request_target trigger with checkout enabled is extremely insecure",
			warningCount:  1, // dangerous-trigger warning; insecure-checkout is returned as an error
		},
		{
			name: "pull_request_target with explicit checkout pinned to base sha - strict - warning only",
			frontmatter: `---
on:
  pull_request_target:
    types: [opened]
tools:
  github:
    toolsets: [pull_requests]
permissions:
  pull-requests: read
checkout:
  repository: ${{ github.repository }}
  ref: ${{ github.event.pull_request.base.sha }}
---

# PR Target Strict Trusted Checkout
Test workflow content.`,
			filename:      "prt-checkout-base-sha-strict.md",
			strictMode:    true,
			expectError:   false,
			expectWarning: true,
			warningCount:  1, // dangerous-trigger warning
		},
		{
			name: "pull_request_target with explicit checkout default ref in base repo - strict - warning only",
			frontmatter: `---
on:
  pull_request_target:
    types: [opened]
tools:
  github:
    toolsets: [pull_requests]
permissions:
  pull-requests: read
checkout:
  repository: ${{ github.repository }}
  sparse-checkout: |
    .github
---

# PR Target Strict Trusted Default Ref
Test workflow content.`,
			filename:      "prt-checkout-base-repo-default-ref-strict.md",
			strictMode:    true,
			expectError:   false,
			expectWarning: true,
			warningCount:  1, // dangerous-trigger warning
		},
		{
			name: "pull_request_target with trusted checkout expressions using compact syntax - strict - warning only",
			frontmatter: `---
on:
  pull_request_target:
    types: [opened]
tools:
  github:
    toolsets: [pull_requests]
permissions:
  pull-requests: read
checkout:
  repository: ${{github.repository}}
  ref: ${{github.event.pull_request.base.sha}}
---

# PR Target Strict Trusted Compact Expressions
Test workflow content.`,
			filename:      "prt-checkout-trusted-compact-expr-strict.md",
			strictMode:    true,
			expectError:   false,
			expectWarning: true,
			warningCount:  1, // dangerous-trigger warning
		},
		{
			name: "pull_request_target with explicit checkout pinned to base ref expression - strict - warning only",
			frontmatter: `---
on:
  pull_request_target:
    types: [opened]
tools:
  github:
    toolsets: [pull_requests]
permissions:
  pull-requests: read
checkout:
  repository: ${{ github.repository }}
  ref: ${{ github.event.pull_request.base.ref }}
---

# PR Target Strict Trusted Base Ref
Test workflow content.`,
			filename:      "prt-checkout-base-ref-strict.md",
			strictMode:    true,
			expectError:   false,
			expectWarning: true,
			warningCount:  1, // dangerous-trigger warning
		},
		{
			name: "pull_request_target with unwrapped trusted-looking ref string - strict - still errors",
			frontmatter: `---
on:
  pull_request_target:
    types: [opened]
tools:
  github:
    toolsets: [pull_requests]
permissions:
  pull-requests: read
checkout:
  repository: ${{ github.repository }}
  ref: github.event.pull_request.base.sha
---

# PR Target Strict Unwrapped Ref String
Test workflow content.`,
			filename:      "prt-checkout-unwrapped-ref-string-strict.md",
			strictMode:    true,
			expectError:   true,
			expectWarning: true,
			errorContains: "pull_request_target trigger with checkout enabled is extremely insecure",
			warningCount:  1, // dangerous-trigger warning
		},
		{
			name: "pull_request_target with mixed trusted and untrusted checkouts - strict - errors",
			frontmatter: `---
on:
  pull_request_target:
    types: [opened]
tools:
  github:
    toolsets: [pull_requests]
permissions:
  pull-requests: read
checkout:
  - repository: ${{ github.repository }}
    ref: ${{ github.event.pull_request.base.sha }}
  - repository: ${{ github.event.pull_request.head.repo.full_name }}
    ref: ${{ github.event.pull_request.head.sha }}
---

# PR Target Mixed Checkouts
Test workflow content.`,
			filename:      "prt-checkout-mixed-strict.md",
			strictMode:    true,
			expectError:   true,
			expectWarning: true,
			errorContains: "pull_request_target trigger with checkout enabled is extremely insecure",
			warningCount:  1, // dangerous-trigger warning
		},
		{
			name: "pull_request_target with no checkout key - strict CLI overrides frontmatter strict false",
			frontmatter: `---
strict: false
on:
  pull_request_target:
    types: [opened]
tools:
  github:
    toolsets: [pull_requests]
permissions:
  pull-requests: read
---

# PR Target Strict Opt-Out
Test workflow content.`,
			filename:      "prt-checkout-enabled-strict-frontmatter-opt-out.md",
			strictMode:    true,
			expectError:   true,
			expectWarning: true,
			errorContains: "pull_request_target trigger with checkout enabled is extremely insecure",
			warningCount:  1, // dangerous-trigger warning
		},
		{
			name: "pull_request trigger (not target) - strict - no diagnostic",
			frontmatter: `---
on:
  pull_request:
    types: [opened]
tools:
  github:
    toolsets: [pull_requests]
permissions:
  pull-requests: read
---

# PR Strict Workflow
Test workflow content.`,
			filename:      "pr-strict.md",
			strictMode:    true,
			expectError:   false,
			expectWarning: false,
			warningCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mdFile := filepath.Join(tmpDir, tt.filename)
			if err := os.WriteFile(mdFile, []byte(tt.frontmatter), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()
			compiler.SetStrictMode(tt.strictMode)
			compiler.SetNoEmit(true)

			err := compiler.CompileWorkflow(mdFile)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain %q but got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}

			if compiler.GetWarningCount() != tt.warningCount {
				t.Errorf("Expected %d warnings but got %d", tt.warningCount, compiler.GetWarningCount())
			}
		})
	}
}

func TestMatchesGitHubExpression(t *testing.T) {
	t.Parallel()

	assertions := []struct {
		name     string
		value    string
		expected string
		match    bool
	}{
		{
			name:     "spaced expression",
			value:    "${{ github.repository }}",
			expected: "github.repository",
			match:    true,
		},
		{
			name:     "compact expression",
			value:    "${{github.repository}}",
			expected: "github.repository",
			match:    true,
		},
		{
			name:     "base sha expression matches",
			value:    "${{ github.event.pull_request.base.sha }}",
			expected: "github.event.pull_request.base.sha",
			match:    true,
		},
		{
			name:     "base ref expression matches",
			value:    "${{ github.event.pull_request.base.ref }}",
			expected: "github.event.pull_request.base.ref",
			match:    true,
		},
		{
			name:     "head sha expression does not match base sha",
			value:    "${{ github.event.pull_request.head.sha }}",
			expected: "github.event.pull_request.base.sha",
			match:    false,
		},
		{
			name:     "missing closing braces",
			value:    "${{ github.repository",
			expected: "github.repository",
			match:    false,
		},
		{
			name:     "empty expression",
			value:    "${{}}",
			expected: "github.repository",
			match:    false,
		},
		{
			name:     "extra trailing tokens",
			value:    "${{ github.repository }}bar}}",
			expected: "github.repository",
			match:    false,
		},
	}

	for _, tc := range assertions {
		t.Run(tc.name, func(t *testing.T) {
			actual := matchesGitHubExpression(tc.value, tc.expected)
			if actual != tc.match {
				t.Fatalf("matchesGitHubExpression(%q, %q) = %v, want %v", tc.value, tc.expected, actual, tc.match)
			}
		})
	}
}

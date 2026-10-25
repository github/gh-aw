package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/actions"
	workflowlint "github.com/github/gh-aw/pkg/workflow/lint"
	"github.com/stretchr/testify/require"
)

func TestCompilerLintVerifiedCheckoutCleanup(t *testing.T) {
	t.Parallel()
	const authored = "      - uses: actions/checkout@0123456789abcdef0123456789abcdef01234567\n        with:\n          persist-credentials: true\n"
	for _, tc := range []struct {
		name, job, before, after, old, replacement string
		noProvenance, duplicate                    bool
		warnings, exemptions                       int
	}{
		{name: "generated", exemptions: 1, warnings: 1},
		{name: "authored before", before: authored, exemptions: 1, warnings: 2},
		{name: "authored after", after: authored, exemptions: 1, warnings: 2},
		{name: "authored cleanup name", old: "Checkout repository (gh-aw default)", replacement: "Authored checkout", warnings: 1},
		{name: "no provenance", noProvenance: true, warnings: 1},
		{name: "ambiguous copy", duplicate: true, warnings: 2},
		{name: "custom job", job: "custom", warnings: 1},
		{name: "safe outputs", job: "safe_outputs", warnings: 1},
		{name: "intervening step", old: "      - name: Clean git credentials", replacement: "      - run: echo intervening\n      - name: Clean git credentials", warnings: 1},
		{name: "missing verifier", old: "          bash \"${RUNNER_TEMP}/gh-aw/actions/verify_git_credentials.sh\"\n", warnings: 1},
		{name: "missing cleanup", old: "          bash \"${RUNNER_TEMP}/gh-aw/actions/clean_git_credentials_checkout.sh\"\n", warnings: 1},
		{name: "different cleanup command", old: "clean_git_credentials_checkout.sh", replacement: "authored_cleanup.sh", warnings: 1},
		{name: "conditional cleanup", old: "        run: |", replacement: "        if: false\n        run: |", warnings: 1},
		{name: "ignored failure", old: "        run: |", replacement: "        continue-on-error: true\n        run: |", warnings: 1},
		{name: "masked failure", old: "verify_git_credentials.sh\"", replacement: "verify_git_credentials.sh\" || true", warnings: 1},
		{name: "different shell", old: "        run: |", replacement: "        shell: bash {0}\n        run: |", warnings: 1},
		{name: "changed checkout token", old: "          persist-credentials: true", replacement: "          token: ${{ secrets.OTHER_TOKEN }}\n          persist-credentials: true", warnings: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			compiler := NewCompiler(WithLint(true))
			var generated strings.Builder
			_, _, err := compiler.generateInitialAndCheckoutSteps(&generated, &WorkflowData{
				CheckoutConfigs: []*CheckoutConfig{{CleanGitCredentials: true}},
			})
			require.NoError(t, err)
			block := generated.String()
			if tc.old != "" {
				block = strings.Replace(block, tc.old, tc.replacement, 1)
			}
			if tc.noProvenance {
				compiler.agentCheckoutLintBlocks = nil
			}
			if tc.duplicate {
				block += block
			}
			job := tc.job
			if job == "" {
				job = "agent"
			}
			source := fmt.Sprintf("jobs:\n  %s:\n    runs-on: ubuntu-latest\n    steps:\n%s%s%s", job, tc.before, block, tc.after)
			graph, err := actions.Parse([]byte(source))
			require.NoError(t, err)
			exempt, err := compiler.verifiedCleanupCheckoutLintPaths(graph)
			require.NoError(t, err)
			require.Len(t, exempt, tc.exemptions)
			runner, err := workflowlint.New(workflowlint.ArtipackedRule())
			require.NoError(t, err)
			diagnostics, err := runner.Run(context.Background(), workflowlint.Input{Workflow: graph})
			require.NoError(t, err)
			require.Len(t, diagnostics, tc.warnings, "standalone lint must not trust generated-looking cleanup")
			for _, diagnostic := range diagnostics {
				require.Equal(t, workflowlint.Warning, diagnostic.Severity)
			}
			require.NoError(t, compiler.lintCompiledWorkflow(&WorkflowData{}, source, "cleanup.md"))
			require.Equal(t, tc.warnings-tc.exemptions, compiler.GetWarningCount())
		})
	}
}

func TestCompilerLintVerifiedCleanupMultiRepo(t *testing.T) {
	t.Parallel()
	for _, sparse := range []bool{false, true} {
		t.Run(fmt.Sprintf("sparse=%t", sparse), func(t *testing.T) {
			t.Parallel()
			configs := []*CheckoutConfig{
				{Path: "./control", CleanGitCredentials: true, GitHubToken: "${{ secrets.CONTROL_TOKEN }}"},
				{Repository: "example/one", Path: "./one", CleanGitCredentials: true, GitHubToken: "${{ secrets.ONE_TOKEN }}"},
				{Repository: "example/two", Path: "./two", CleanGitCredentials: true, GitHubToken: "${{ secrets.TWO_TOKEN }}"},
			}
			if sparse {
				for _, config := range configs {
					config.SparseCheckout = "src"
				}
			}
			compiler := NewCompiler()
			var generated strings.Builder
			_, _, err := compiler.generateInitialAndCheckoutSteps(&generated, &WorkflowData{CheckoutConfigs: configs})
			require.NoError(t, err)
			block := generated.String()
			for _, token := range []string{"CONTROL_TOKEN", "ONE_TOKEN", "TWO_TOKEN"} {
				require.Contains(t, block, "token: ${{ secrets."+token+" }}")
			}
			require.Equal(t, 3, strings.Count(block, "persist-credentials: true"))
			require.Equal(t, 3, strings.Count(block, "verify_git_credentials.sh"))
			graph, err := actions.Parse([]byte("jobs:\n  agent:\n    runs-on: ubuntu-latest\n    steps:\n" + block))
			require.NoError(t, err)
			exempt, err := compiler.verifiedCleanupCheckoutLintPaths(graph)
			require.NoError(t, err)
			if sparse {
				require.Empty(t, exempt, "the partial-clone repair step interrupts immediate cleanup")
			} else {
				require.Len(t, exempt, 3)
			}
		})
	}
}

func TestCompilerLintGeneratedCheckoutCleanup(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "cleanup.md")
	source := `---
on:
  workflow_dispatch:
  admission: true
engine: copilot
permissions:
  contents: read
checkout:
  force-clean-git-credentials: true
tools:
  github: false
---
Maintain the repository.
`
	require.NoError(t, os.WriteFile(path, []byte(source), 0o600))
	compiler := NewCompiler(WithLint(true))
	compiler.gitRoot = dir
	compiler.SetReadOnly(true)
	compiler.SetSkipValidation(true)
	require.NoError(t, compiler.CompileWorkflow(path))
	require.Zero(t, compiler.GetWarningCount())
	require.NotEmpty(t, compiler.agentCheckoutLintBlocks)
	lockPath := filepath.Join(dir, "cleanup.lock.yml")
	lock, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	require.Contains(t, string(lock), "persist-credentials: true")
	require.Contains(t, string(lock), "clean_git_credentials_checkout.sh")
	require.Contains(t, string(lock), "verify_git_credentials.sh")
	compiler.lint = false
	require.NoError(t, compiler.CompileWorkflow(path))
	unlinted, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	require.Equal(t, string(lock), string(unlinted), "lint filtering must not alter runtime YAML")
	require.NoError(t, os.WriteFile(path, []byte("---\non: workflow_dispatch\ncheckout: false\ntools:\n  github: false\n---\nNo checkout.\n"), 0o600))
	compiler.lint = true
	require.NoError(t, compiler.CompileWorkflow(path))
	require.Empty(t, compiler.agentCheckoutLintBlocks, "compiler reuse must discard previous checkout provenance")
}

func TestCompilerLintCheckoutCleanupProvenanceErrors(t *testing.T) {
	t.Parallel()
	compiler := NewCompiler(WithLint(true))
	compiler.agentCheckoutLintBlocks = []string{"["}
	require.ErrorContains(t, compiler.lintCompiledWorkflow(&WorkflowData{}, "jobs:\n  agent:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo safe\n", "cleanup.md"), "parse compiler-owned agent checkout steps")
}

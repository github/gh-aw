//go:build integration

package workflow

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// TestActionsLockParityIntegration requires an authenticated gh and the
// github/gh-actions-lock extension. It never executes workflow steps.
func TestActionsLockParityIntegration(t *testing.T) {
	gh, err := exec.LookPath("gh")
	if err != nil {
		t.Skip("gh is required for actions-lock parity")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	output, err := exec.CommandContext(ctx, gh, "actions-lock", "--version").CombinedOutput()
	require.NoError(t, err, "install github/gh-actions-lock to run actions-lock parity:\n%s", output)
	output, err = exec.CommandContext(ctx, gh, "auth", "status", "--hostname", "github.com").CombinedOutput()
	require.NoError(t, err, "authenticate gh with github.com to run actions-lock parity:\n%s", output)

	root := t.TempDir()
	workflowsDir := filepath.Join(root, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0o755))
	setupDir := filepath.Join(root, "actions", "setup")
	require.NoError(t, os.MkdirAll(setupDir, 0o755))
	setup, err := os.ReadFile(filepath.Join("..", "..", "actions", "setup", "action.yml"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(setupDir, "action.yml"), setup, 0o600))

	source := filepath.Join(workflowsDir, "parity.md")
	require.NoError(t, os.WriteFile(source, []byte(`---
on: workflow_dispatch
engine: copilot
strict: false
permissions:
  contents: read
safe-outputs:
  create-issue:
steps:
  - name: Restore test cache
    uses: actions/cache/restore@v6.1.0
    with:
      path: /tmp/parity
      key: parity
---
Report the repository status.
`), 0o600))
	compiler := NewCompiler(WithVersion("dev"))
	compiler.gitRoot = root
	compiler.SetContext(ctx)
	require.NoError(t, compiler.CompileWorkflow(source))

	lockPath := filepath.Join(workflowsDir, "parity.lock.yml")
	before, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	type step struct {
		Uses yaml.Node `yaml:"uses"`
	}
	type job struct {
		Steps []step `yaml:"steps"`
	}
	type compiledWorkflow struct {
		Jobs map[string]job `yaml:"jobs"`
	}
	var original compiledWorkflow
	require.NoError(t, yaml.Unmarshal(before, &original))
	require.NotEmpty(t, original.Jobs)

	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, gh, append([]string{"actions-lock"}, args...)...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GH_ACTIONS_LOCK_REPO_ROOT="+root)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "gh actions-lock %v:\n%s", args, output)
	}
	// Read-only checks skip untracked workflows with local actions. Fix mode
	// onboards them and migrates ./actions/setup before resolving their pins.
	run("--hostname", "github.com", "--no-interactive")

	after, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	var rewritten compiledWorkflow
	require.NoError(t, yaml.Unmarshal(after, &rewritten))
	require.Len(t, rewritten.Jobs, len(original.Jobs))
	extensionLock, err := os.ReadFile(filepath.Join(workflowsDir, "actions.lock"))
	require.NoError(t, err)
	var locked struct {
		Workflows    map[string][]string `yaml:"workflows"`
		Dependencies map[string]struct {
			Commit string `yaml:"commit"`
		} `yaml:"dependencies"`
	}
	require.NoError(t, yaml.Unmarshal(extensionLock, &locked))
	workflowDependencies, ok := locked.Workflows[".github/workflows/parity.lock.yml"]
	require.True(t, ok, "actions-lock must onboard the compiled workflow")
	require.NotEmpty(t, workflowDependencies)

	remoteActions := 0
	localActions := 0
	for name, originalJob := range original.Jobs {
		rewrittenJob, ok := rewritten.Jobs[name]
		require.True(t, ok, "actions-lock removed job %s", name)
		require.Len(t, rewrittenJob.Steps, len(originalJob.Steps), "job %s", name)
		for i, originalStep := range originalJob.Steps {
			originalUse := originalStep.Uses.Value
			rewrittenUse := rewrittenJob.Steps[i].Uses.Value
			if originalUse == "" {
				require.Empty(t, rewrittenUse)
				continue
			}
			if strings.HasPrefix(originalUse, "./") {
				localActions++
				require.Equal(t, "$/"+strings.TrimPrefix(originalUse, "./"), rewrittenUse)
				continue
			}
			remoteActions++
			action, sha, ok := strings.Cut(originalUse, "@")
			require.True(t, ok)
			require.Regexp(t, `^[a-f0-9]{40}$`, sha, "compiler must emit an immutable SHA for %s", action)
			rewrittenAction, ref, ok := strings.Cut(rewrittenUse, "@")
			require.True(t, ok)
			require.Equal(t, action, rewrittenAction, "actions-lock changed the action path")
			version := strings.TrimSpace(strings.TrimPrefix(originalStep.Uses.LineComment, "#"))
			require.NotEmpty(t, version, "compiler must retain the symbolic ref for %s", action)
			require.Equal(t, version, ref, "compiler and actions-lock disagree on the ref for %s", action)
			repo := gitutil.ExtractBaseRepo(action)
			key := repo + "@" + ref
			require.Contains(t, workflowDependencies, key)
			dependency, ok := locked.Dependencies[key]
			require.True(t, ok, "actions-lock did not lock %s", rewrittenUse)
			require.Equal(t, "sha1-"+sha, dependency.Commit,
				"compiler and actions-lock resolved different commits for %s", action)
		}
	}
	require.Positive(t, remoteActions)
	require.Positive(t, localActions)
	require.Contains(t, string(before), "uses: actions/cache/restore@")
	run("--hostname", "github.com", "--verify", "--no-interactive")
}

// getGitHubRepoURLIntegration converts a repo path to a GitHub URL
// For "actions/checkout" -> "https://github.com/actions/checkout.git"
// For "github/codeql-action/upload-sarif" -> "https://github.com/github/codeql-action.git"
func getGitHubRepoURLIntegration(repo string) string {
	// For actions with subpaths (like codeql-action/upload-sarif), extract the base repo
	parts := strings.Split(repo, "/")
	if len(parts) >= 2 {
		// Take first two parts (owner/repo)
		baseRepo := parts[0] + "/" + parts[1]
		return "https://github.com/" + baseRepo + ".git"
	}
	return "https://github.com/" + repo + ".git"
}

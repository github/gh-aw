//go:build !integration

package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLogsCommand(t *testing.T) {
	t.Parallel()
	cmd := NewLogsCommand()

	require.NotNil(t, cmd, "NewLogsCommand should not return nil")
	assert.Equal(t, "logs [workflow]...", cmd.Use, "Command use should show repeatable workflows")
	assert.Equal(t, "Download and analyze agentic workflow logs and artifacts", cmd.Short, "Command short description should match")
	assert.Contains(t, cmd.Long, "Download and analyze agentic workflow logs", "Command long description should contain expected text")
	assert.Contains(t, cmd.Example, "logs --cache-before -1w", "Cache maintenance examples should use the cache-before flag name")

	// Verify flags are registered
	flags := cmd.Flags()

	// Check count flag
	countFlag := flags.Lookup("count")
	assert.NotNil(t, countFlag, "Should have 'count' flag")
	assert.Equal(t, "c", countFlag.Shorthand, "Count flag shorthand should be 'c'")

	// Check start-date flag
	startDateFlag := flags.Lookup("start-date")
	assert.NotNil(t, startDateFlag, "Should have 'start-date' flag")

	// Check end-date flag
	endDateFlag := flags.Lookup("end-date")
	assert.NotNil(t, endDateFlag, "Should have 'end-date' flag")

	// Check engine flag
	engineFlag := flags.Lookup("engine")
	assert.NotNil(t, engineFlag, "Should have 'engine' flag")
	assert.Equal(t, "e", engineFlag.Shorthand, "Engine filter flag should have shorthand '-e'")

	// Check firewall flags
	firewallFlag := flags.Lookup("firewall")
	assert.NotNil(t, firewallFlag, "Should have 'firewall' flag")
	noFirewallFlag := flags.Lookup("no-firewall")
	assert.NotNil(t, noFirewallFlag, "Should have 'no-firewall' flag")

	// Check output flag
	outputFlag := flags.Lookup("output")
	assert.NotNil(t, outputFlag, "Should have 'output' flag")
	assert.Equal(t, "o", outputFlag.Shorthand, "Output flag shorthand should be 'o'")

	// Check ref flag
	refFlag := flags.Lookup("ref")
	assert.NotNil(t, refFlag, "Should have 'ref' flag")

	// Check run ID filters
	afterRunIDFlag := flags.Lookup("after-run-id")
	assert.NotNil(t, afterRunIDFlag, "Should have 'after-run-id' flag")
	beforeRunIDFlag := flags.Lookup("before-run-id")
	assert.NotNil(t, beforeRunIDFlag, "Should have 'before-run-id' flag")
	lastFlag := flags.Lookup("last")
	assert.NotNil(t, lastFlag, "Should have 'last' flag")
	assert.Contains(t, lastFlag.Usage, "--count/-c", "--last usage should mention the canonical --count/-c flag")

	// Check tool-graph flag
	toolGraphFlag := flags.Lookup("tool-graph")
	assert.NotNil(t, toolGraphFlag, "Should have 'tool-graph' flag")

	// Check parse flag
	parseFlag := flags.Lookup("parse")
	assert.NotNil(t, parseFlag, "Should have 'parse' flag")

	// Check audit flag
	auditFlag := flags.Lookup("audit")
	assert.NotNil(t, auditFlag, "Should have 'audit' flag")

	// Check json flag
	jsonFlag := flags.Lookup("json")
	assert.NotNil(t, jsonFlag, "Should have 'json' flag")

	// Check repo flag
	repoFlag := flags.Lookup("repo")
	assert.NotNil(t, repoFlag, "Should have 'repo' flag")

	// Check cache-before flag (cache maintenance)
	cacheBeforeFlag := flags.Lookup("cache-before")
	assert.NotNil(t, cacheBeforeFlag, "Should have 'cache-before' flag")
	assert.Contains(t, cacheBeforeFlag.Usage, "-1d", "cache-before flag should document day deltas")
	assert.Contains(t, cacheBeforeFlag.Usage, "-30d", "cache-before flag should document explicit day-count deltas")
	assert.Contains(t, cacheBeforeFlag.Usage, "(Cache eviction)", "cache-before flag usage should retain cache-eviction prefix text")

	// Backward-compatible alias should remain registered but hidden from help output
	afterAliasFlag := flags.Lookup("after")
	assert.NotNil(t, afterAliasFlag, "Should retain hidden 'after' alias")
	assert.True(t, afterAliasFlag.Hidden, "'after' alias should be hidden from help output")

	maxRateLimitFlag := flags.Lookup("max-github-api-rate-limit")
	require.NotNil(t, maxRateLimitFlag, "Should have 'max-github-api-rate-limit' flag")
	maxStorageFlag := flags.Lookup("max-storage")
	require.NotNil(t, maxStorageFlag, "Should have 'max-storage' flag")
	pruneOlderRunsFlag := flags.Lookup("prune-older-runs")
	require.NotNil(t, pruneOlderRunsFlag, "Should have 'prune-older-runs' flag")
}

func TestLogsCommandFlagDefaults(t *testing.T) {
	t.Parallel()
	cmd := NewLogsCommand()
	flags := cmd.Flags()

	tests := []struct {
		flagName     string
		defaultValue string
	}{
		{"start-date", ""},
		{"end-date", ""},
		{"engine", ""},
		{"output", ".github/aw/logs"}, // Updated to match actual default
		{"ref", ""},
		{"after-run-id", "0"},
		{"before-run-id", "0"},
		{"repo", ""},
		{"artifacts", "[usage]"},
		{"max-github-api-rate-limit", "0"},
		{"max-storage", "0"},
		{"prune-older-runs", "false"},
	}

	for _, tt := range tests {
		t.Run(tt.flagName, func(t *testing.T) {
			flag := flags.Lookup(tt.flagName)
			require.NotNil(t, flag, "Flag should exist: %s", tt.flagName)
			assert.Equal(t, tt.defaultValue, flag.DefValue, "Default value should match for flag: %s", tt.flagName)
		})
	}
}

func TestLogsCommandResourceBudgetFlags(t *testing.T) {
	cmd := NewLogsCommand()
	require.NoError(t, cmd.Flags().Set("max-github-api-rate-limit", "-2000"))
	require.NoError(t, cmd.Flags().Set("max-storage", "10240"))
	require.NoError(t, cmd.Flags().Set("prune-older-runs", "true"))

	opts, err := loadCommonLogsOptions(cmd)

	require.NoError(t, err)
	assert.Equal(t, -2000, opts.MaxGitHubAPIRateLimit)
	assert.Equal(t, 10240, opts.MaxStorageMB)
	assert.True(t, opts.PruneOlderRuns)
}

func TestLogsCommandRejectsNegativeMaxStorage(t *testing.T) {
	cmd := NewLogsCommand()
	require.NoError(t, cmd.Flags().Set("max-storage", "-1"))

	_, err := loadCommonLogsOptions(cmd)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected a non-negative number of MB")
}

func TestLogsCommandRejectsRateLimitWithStdin(t *testing.T) {
	cmd := NewLogsCommand()
	require.NoError(t, cmd.Flags().Set("max-github-api-rate-limit", "12000"))

	err := runLogsCommandFromStdin(cmd, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires discovery mode")
}

func TestLogsCommandBooleanFlags(t *testing.T) {
	t.Parallel()
	cmd := NewLogsCommand()
	flags := cmd.Flags()

	boolFlags := []string{"firewall", "no-firewall", "tool-graph", "parse", "audit", "json"}

	for _, flagName := range boolFlags {
		t.Run(flagName, func(t *testing.T) {
			flag := flags.Lookup(flagName)
			require.NotNil(t, flag, "Boolean flag should exist: %s", flagName)
			assert.Equal(t, "false", flag.DefValue, "Boolean flag should default to false: %s", flagName)
		})
	}
}

func TestLogsCommandAuditFlag(t *testing.T) {
	cmd := NewLogsCommand()
	require.NoError(t, cmd.Flags().Set("audit", "true"))

	opts, err := loadCommonLogsOptions(cmd)

	require.NoError(t, err)
	assert.True(t, opts.Audit)
}

func TestLogsCommandStructure(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		commandCreator func() any
	}{
		{
			name: "logs command exists",
			commandCreator: func() any {
				return NewLogsCommand()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := tt.commandCreator()
			require.NotNil(t, cmd, "Command should not be nil")
		})
	}
}

func TestLogsCommandArgs(t *testing.T) {
	t.Parallel()
	cmd := NewLogsCommand()

	// Logs command accepts zero or more workflow arguments.
	// Only test if Args validator is set
	if cmd.Args != nil {
		// Verify it accepts no arguments
		err := cmd.Args(cmd, []string{})
		require.NoError(t, err, "Should not error with no arguments")

		// Verify it accepts 1 argument
		err = cmd.Args(cmd, []string{"workflow1"})
		require.NoError(t, err, "Should not error with 1 argument")

		err = cmd.Args(cmd, []string{"workflow1", "workflow2"})
		require.NoError(t, err, "Should not error with multiple arguments")
	}
}

func TestLogsCommandMutuallyExclusiveFlags(t *testing.T) {
	t.Parallel()
	cmd := NewLogsCommand()
	flags := cmd.Flags()

	// firewall and no-firewall are mutually exclusive
	firewallFlag := flags.Lookup("firewall")
	noFirewallFlag := flags.Lookup("no-firewall")

	require.NotNil(t, firewallFlag, "firewall flag should exist")
	require.NotNil(t, noFirewallFlag, "no-firewall flag should exist")

	// Both flags exist and are boolean
	assert.Equal(t, "bool", firewallFlag.Value.Type(), "firewall should be boolean")
	assert.Equal(t, "bool", noFirewallFlag.Value.Type(), "no-firewall should be boolean")
}

func TestLogsCommandCountFlag(t *testing.T) {
	t.Parallel()
	cmd := NewLogsCommand()
	flags := cmd.Flags()

	countFlag := flags.Lookup("count")
	require.NotNil(t, countFlag, "count flag should exist")

	// Count flag should be an integer
	assert.Equal(t, "int", countFlag.Value.Type(), "count should be integer type")

	// Count flag has shorthand
	assert.Equal(t, "c", countFlag.Shorthand, "count shorthand should be 'c'")
}

func TestLogsCommandDateFlags(t *testing.T) {
	t.Parallel()
	cmd := NewLogsCommand()
	flags := cmd.Flags()

	tests := []struct {
		flagName string
		flagType string
	}{
		{"start-date", "string"},
		{"end-date", "string"},
	}

	for _, tt := range tests {
		t.Run(tt.flagName, func(t *testing.T) {
			flag := flags.Lookup(tt.flagName)
			require.NotNil(t, flag, "Flag should exist: %s", tt.flagName)
			assert.Equal(t, tt.flagType, flag.Value.Type(), "Flag %s should be %s type", tt.flagName, tt.flagType)
		})
	}
}

func TestLogsCommandRunIDFilters(t *testing.T) {
	t.Parallel()
	cmd := NewLogsCommand()
	flags := cmd.Flags()

	tests := []struct {
		flagName string
	}{
		{"after-run-id"},
		{"before-run-id"},
	}

	for _, tt := range tests {
		t.Run(tt.flagName, func(t *testing.T) {
			flag := flags.Lookup(tt.flagName)
			require.NotNil(t, flag, "Flag should exist: %s", tt.flagName)
			assert.Equal(t, "int64", flag.Value.Type(), "Flag %s should be int64 type", tt.flagName)
		})
	}
}

func TestLogsCommandOutputFlag(t *testing.T) {
	t.Parallel()
	cmd := NewLogsCommand()
	flags := cmd.Flags()

	outputFlag := flags.Lookup("output")
	require.NotNil(t, outputFlag, "output flag should exist")

	// Output flag should be a string
	assert.Equal(t, "string", outputFlag.Value.Type(), "output should be string type")

	// Output flag has shorthand
	assert.Equal(t, "o", outputFlag.Shorthand, "output shorthand should be 'o'")

	// Output flag has default value
	assert.Equal(t, ".github/aw/logs", outputFlag.DefValue, "output default should be '.github/aw/logs'")
}

func TestLogsCommandHelpText(t *testing.T) {
	t.Parallel()
	cmd := NewLogsCommand()

	// Verify long description contains expected sections
	expectedLongSections := []string{
		"Download and analyze agentic workflow logs",
		"Downloaded artifacts include (when using --artifacts all):",
		"--artifacts all",
		strings.Join(ValidArtifactSetNames(), ", "),
	}

	for _, section := range expectedLongSections {
		assert.Contains(t, cmd.Long, section, "Long description should contain: %s", section)
	}

	// Verify example field contains example commands
	expectedExampleSections := []string{
		"gh aw logs",
		"logs workflow-a workflow-b",
		"logs owner/repo/.github/workflows/workflow-a.yml",
		"--safe-output noop",
		"--safe-output report-incomplete",
		"--artifacts all",
	}

	for _, section := range expectedExampleSections {
		assert.Contains(t, cmd.Example, section, "Example field should contain: %s", section)
	}

	safeOutputFlag := cmd.Flags().Lookup("safe-output")
	require.NotNil(t, safeOutputFlag, "safe-output flag should exist")
	assert.Contains(t, safeOutputFlag.Usage, "noop", "safe-output flag help should mention noop")
	assert.Contains(t, safeOutputFlag.Usage, "report-incomplete", "safe-output flag help should mention report-incomplete")
}

func TestSplitCrossRepoWorkflowTarget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		input        string
		expectedRepo string
		expectedName string
		expectedOK   bool
	}{
		{
			name:         "workflow name",
			input:        "owner/repo/workflow-name",
			expectedRepo: "owner/repo",
			expectedName: "workflow-name",
			expectedOK:   true,
		},
		{
			name:         "full workflow file path",
			input:        "owner/repo/.github/workflows/workflow-name.yml",
			expectedRepo: "owner/repo",
			expectedName: ".github/workflows/workflow-name.yml",
			expectedOK:   true,
		},
		{
			name:         "enterprise host workflow path",
			input:        "github.example.com/owner/repo/.github/workflows/workflow-name.yml",
			expectedRepo: "github.example.com/owner/repo",
			expectedName: ".github/workflows/workflow-name.yml",
			expectedOK:   true,
		},
		{
			name:       "local workflow path",
			input:      ".github/workflows/workflow-name.md",
			expectedOK: false,
		},
		{
			name:       "bare workflow",
			input:      "workflow-name",
			expectedOK: false,
		},
		{
			name:       "missing relative local workflow path",
			input:      "./local/workflow.md",
			expectedOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, name, ok := splitCrossRepoWorkflowTarget(tt.input)
			assert.Equal(t, tt.expectedOK, ok)
			assert.Equal(t, tt.expectedRepo, repo)
			assert.Equal(t, tt.expectedName, name)
		})
	}
}

func TestResolveLogsWorkflowTargetCrossRepo(t *testing.T) {
	t.Parallel()
	cmd := NewLogsCommand()
	target, err := resolveLogsWorkflowTarget(cmd, "other-org/other-repo/.github/workflows/daily-report.yml")
	require.NoError(t, err)
	assert.Equal(t, "other-org/other-repo", target.repoOverride)
	assert.Equal(t, "daily-report.yml", target.workflowName)
}

func TestResolveRemoteLogsWorkflowTargets(t *testing.T) {
	t.Parallel()

	targets := []logsWorkflowTarget{
		{repoOverride: "githubnext/gh-aw-cao", workflowName: "uk-ai-advisory"},
	}
	fetchWorkflows := func(context.Context, string, bool) (map[string]*GitHubWorkflow, error) {
		return map[string]*GitHubWorkflow{
			"uk-ai-advisory": {
				Name: "UK AI Advisory",
				Path: ".github/workflows/uk-ai-advisory.lock.yml",
			},
		}, nil
	}

	resolved, targetErrors := resolveRemoteLogsWorkflowTargetsWithFetcher(
		context.Background(), targets, false, fetchWorkflows,
	)

	require.Empty(t, targetErrors)
	require.Len(t, resolved, 1)
	assert.Equal(t, "UK AI Advisory", resolved[0].workflowName)
	assert.Equal(t, "githubnext/gh-aw-cao", resolved[0].repoOverride)
}

func TestLogsCommandStdinFlag(t *testing.T) {
	t.Parallel()
	cmd := NewLogsCommand()
	flags := cmd.Flags()

	// --stdin flag must be registered
	stdinFlag := flags.Lookup("stdin")
	require.NotNil(t, stdinFlag, "Should have 'stdin' flag")
	assert.Equal(t, "bool", stdinFlag.Value.Type(), "--stdin should be a boolean flag")
	assert.Equal(t, "false", stdinFlag.DefValue, "--stdin should default to false")
}

func TestLogsCommandStdinRejectsPositionalArgs(t *testing.T) {
	t.Parallel()
	cmd := NewLogsCommand()
	cmd.SetArgs([]string{"my-workflow", "--stdin"})
	// Suppress output so test output stays clean
	cmd.SetOut(nil)
	cmd.SetErr(nil)
	err := cmd.Execute()
	require.Error(t, err, "logs --stdin with a positional arg should return an error")
	require.ErrorContains(t, err, "positional arguments are not allowed with --stdin", "error message should explain the conflict")
}

// TestLogsCommand_RepoBypassesLocalWorkflowResolution verifies that specifying
// --repo prevents a "workflow not found" error from local file lookup when a
// positional workflow name argument is supplied and no local lock file exists.
// In that case the command normalizes the name and passes it directly to the
// download orchestrator. Because there is no running GitHub API in unit tests
// the orchestrator itself will fail; the test asserts only that the error is
// NOT the local-resolution "workflow not found" message.
func TestLogsCommand_RepoBypassesLocalWorkflowResolution(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := NewLogsCommand()
	// Use a workflow name that definitely does not exist locally.
	cmd.SetArgs([]string{"nonexistent-remote-workflow", "--repo", "owner/repo"})
	cmd.SetOut(nil)
	cmd.SetErr(nil)

	execErr := cmd.Execute()

	// The command must fail: there are no local workflows and the --repo target
	// does not exist / no gh auth in tests, so downstream API calls will error.
	require.Error(t, execErr, "--repo with a non-existent local workflow must not succeed in unit tests")

	// The "workflow 'X' not found" error from local FindWorkflowName must NOT appear.
	// (Any other error from downstream API calls is acceptable in unit tests.)
	assert.NotContains(t, execErr.Error(), "workflow 'nonexistent-remote-workflow' not found",
		"--repo should bypass local workflow name resolution and not produce a local-not-found error")
}

// TestLogsCommand_RepoUsesLocalResolutionWhenLockFileExists verifies that when
// --repo is set to the current repository (the common MCP server case where
// GITHUB_REPOSITORY is the current repo), FindWorkflowName is still called to
// resolve the workflow ID to its GitHub Actions display name. Without this, the
// raw workflow ID (e.g. "audit-workflows") would be passed to `gh run list
// --workflow` instead of the display name (e.g. "Agentic Workflow Audit Agent"),
// causing GitHub's API to report "could not find any workflows named X".
func TestLogsCommand_RepoUsesLocalResolutionWhenLockFileExists(t *testing.T) {
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	// Create the markdown file (required by ResolveWorkflowName).
	// The frontmatter name field is the GitHub Actions display name; the workflow
	// ID is derived from the filename ("my-test-workflow").
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "my-test-workflow.md"), []byte("---\nname: My Test Workflow Display Name\n---\n"), 0644))

	// Create a lock file whose display name differs from the workflow ID.
	// This simulates the real scenario where audit-workflows.lock.yml has
	// name: "Agentic Workflow Audit Agent" while the ID is "audit-workflows".
	lockContent := "name: \"My Test Workflow Display Name\"\non: push\n"
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "my-test-workflow.lock.yml"), []byte(lockContent), 0644))

	// Isolate file-system writes: chdir into tmpDir so that ensureLogsGitignore
	// and the default output directory (.github/aw/logs) land in tmpDir, not in
	// the repository root.
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	// GITHUB_REPOSITORY signals to repoIsLocal that owner/repo IS the current
	// repo, so local lock files are authoritative for display-name resolution.
	t.Setenv("GITHUB_REPOSITORY", "owner/repo")
	t.Setenv("GH_AW_WORKFLOWS_DIR", workflowsDir)

	cmd := NewLogsCommand()
	cmd.SetArgs([]string{"my-test-workflow", "--repo", "owner/repo", "--output", filepath.Join(tmpDir, "logs-out")})
	cmd.SetOut(nil)
	cmd.SetErr(nil)

	execErr := cmd.Execute()

	// The command must fail in unit tests (no real GitHub API access).
	require.Error(t, execErr)

	// Local "workflow not found" must NOT appear: the lock file exists and
	// GITHUB_REPOSITORY matches --repo, so FindWorkflowName should succeed.
	assert.NotContains(t, execErr.Error(), "workflow 'my-test-workflow' not found",
		"when GITHUB_REPOSITORY matches --repo and a local lock file exists, FindWorkflowName should succeed")

	// The raw workflow ID must NOT appear as the failing name in a gh run list
	// "could not find" error, because the resolved display name should be used.
	// In unit tests the API call fails with an HTTP 403 before GitHub can report
	// a workflow-not-found message, so we verify the negative: the workflow ID
	// ("my-test-workflow") is not echoed back as the unrecognised workflow name.
	assert.NotContains(t, execErr.Error(), "could not find any workflows named my-test-workflow",
		"when a local lock file exists, the display name (not the workflow ID) should be passed to gh run list")
}

//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditCommandDescriptionsAreConsistent(t *testing.T) {
	t.Parallel()
	cmd := NewAuditCommand()

	assert.Contains(t, cmd.Short, "workflow runs", "audit short description should describe multiple run inputs")
	assert.Contains(t, cmd.Long, "Audit one or more workflow runs", "audit long description should describe multiple run inputs")
	assert.Contains(t, cmd.Long, "remaining runs are compared against it", "audit help should document multi-run analysis mode")
}

func TestTrialCommandUsesStandardExamplesHeading(t *testing.T) {
	t.Parallel()
	cmd := NewTrialCommand(func(string) error { return nil })

	assert.NotEmpty(t, cmd.Example, "trial command should use cobra's Example field for examples")
	assert.NotContains(t, cmd.Long, "Single workflow:", "trial long help should avoid custom example section headings")
	assert.NotContains(t, cmd.Long, "Multiple workflows (for comparison):", "trial long help should avoid custom example section headings")
	assert.NotContains(t, cmd.Long, "Workflows from different repositories:", "trial long help should avoid custom example section headings")
	assert.NotContains(t, cmd.Long, "Repository mode examples:", "trial long help should avoid custom example section headings")
	assert.NotContains(t, cmd.Long, "Repeat and cleanup examples:", "trial long help should avoid custom example section headings")
	assert.NotContains(t, cmd.Long, "Auto-merge examples:", "trial long help should avoid custom example section headings")
	assert.NotContains(t, cmd.Long, "Advanced examples:", "trial long help should avoid custom example section headings")
}

func TestUpdateDocsIncludeCoolDownOption(t *testing.T) {
	t.Parallel()
	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "should resolve current test file path")

	docsPath := filepath.Join(filepath.Dir(currentFile), "..", "..", "docs", "src", "content", "docs", "setup", "cli.md")
	content, err := os.ReadFile(docsPath)
	require.NoError(t, err, "should read CLI setup docs")

	text := string(content)
	updateIndex := strings.Index(text, "#### `update`")
	require.NotEqual(t, -1, updateIndex, "CLI setup docs should contain the update section")

	updateSection := text[updateIndex:]
	assert.Contains(t, updateSection, "`--cool-down`", "update docs options should include --cool-down")
}

func TestCompileDocsReflectCurrentOptions(t *testing.T) {
	t.Parallel()
	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "should resolve current test file path")

	docsPath := filepath.Join(filepath.Dir(currentFile), "..", "..", "docs", "src", "content", "docs", "setup", "cli.md")
	content, err := os.ReadFile(docsPath)
	require.NoError(t, err, "should read CLI setup docs")

	text := string(content)
	compileIndex := strings.Index(text, "#### `compile`")
	require.NotEqual(t, -1, compileIndex, "CLI setup docs should contain the compile section")

	compileSection := text[compileIndex:]
	assert.NotContains(t, compileSection, "`--no-models-dev-lookup`", "compile docs options should not include removed --no-models-dev-lookup")
	assert.Contains(t, compileSection, "does not run codemods unless you pass `--fix`", "compile docs should explain --fix opt-in behavior")
}

func TestCLIDocsReflectStatusAuditExperimentsAndGradersCommands(t *testing.T) {
	t.Parallel()
	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "should resolve current test file path")

	docsPath := filepath.Join(filepath.Dir(currentFile), "..", "..", "docs", "src", "content", "docs", "setup", "cli.md")
	content, err := os.ReadFile(docsPath)
	require.NoError(t, err, "should read CLI setup docs")

	text := string(content)
	assert.Contains(t, text, "#### `experiments`", "CLI setup docs should include the experiments command")
	assert.Contains(t, text, "#### `graders`", "CLI setup docs should include the graders command")
	assert.Contains(t, text, "cat request.json | gh aw graders run daily-file-diet operational-value", "graders docs should show one-shot operational-value execution")
	assert.Contains(t, text, "graders.operational-value.script", "graders docs should describe embedded operational-value execution")
	assert.NotContains(t, text, "graders operational-value", "graders docs should not advertise removed historical regrading")
	assert.Contains(t, text, "#### `doctor`", "CLI setup docs should include the doctor command")
	assert.Contains(t, text, "The `audit` command has two modes", "audit docs should describe the current two-mode behavior")
	assert.NotContains(t, text, "enabled/disabled status, schedules, and labels", "status docs should not promise schedule output in console mode")
	assert.Contains(t, text, "Use `--json` to inspect the raw `on` data, including schedules", "status docs should direct schedule inspection to JSON output")
	assert.Contains(t, text, "runs codemods, action version updates, and workflow compilation by default and uses `--no-fix` to skip all three steps", "upgrade docs should explain the inverse --fix/--no-fix behavior")
	assert.Contains(t, text, "Print the current version and build information for the gh aw CLI extension.", "version docs should match the command help text")
	assert.Contains(t, text, "**Options:** `--repo/-r`, `--dir/-d`, `--require-owner-type`, `--json/-j`", "doctor docs should include the --dir shorthand")
	assert.Contains(t, text, "`--require-owner-type` accepts `any`, `user`, or `org` and defaults to `any`", "doctor docs should document the full owner type set and default")
	assert.Contains(t, text, "`--dir` and `--require-owner-type` require `--repo`", "doctor docs should document the repo requirement for repository-only flags")
	assert.Contains(t, text, "Outside a checkout, run `gh auth login --hostname <host>` to authenticate and set `GH_HOST=<host>` so repository diagnostics target the correct host.", "doctor docs should explain that enterprise hosts outside a checkout require both authentication and host selection")
	assert.Contains(t, text, "`doctor --repo` currently accepts `owner/repo` only.", "doctor docs should explicitly distinguish their narrower repo format")
	assert.Contains(t, text, "For repository scope, `--repo` currently accepts `owner/repo` only.", "env docs should explicitly distinguish their narrower repo format")
	assert.Contains(t, text, "**Options:** `--engine/-e` (copilot, claude, codex, gemini, pi), `--non-interactive`, `--repo/-r`", "secrets bootstrap docs should include the --repo shorthand")
	assert.Contains(t, text, "**Options:** `--concurrency`, `--days`, `--period`, `--sample`, `--eval`, `--timeout`, `--repo/-r`, `--json/-j`", "forecast docs should include --concurrency")
	assert.Contains(t, text, "| `-h`, `--help` | Show help (`gh aw help [command]` for command-specific help) |\n| `-v`, `--verbose` | Enable verbose output showing detailed information |\n| `--banner` | Display ASCII logo banner with purple GitHub color theme |", "global options table rows should remain contiguous")
}

func TestSubcommandListingsUseHyphenBullets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		longDoc string
	}{
		{name: "mcp", longDoc: NewMCPCommand().Long},
		{name: "project", longDoc: NewProjectCommand().Long},
		{name: "secrets", longDoc: NewSecretsCommand().Long},
		{name: "experiments", longDoc: NewExperimentsCommand().Long},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Contains(t, tt.longDoc, "Available subcommands:", "command should document available subcommands")
			assert.NotContains(t, tt.longDoc, "  • ", "subcommand list should use '-' bullet style consistently")
		})
	}
}

func TestSubcommandListingsMatchCobraShortDescriptions(t *testing.T) {
	t.Parallel()
	t.Run("secrets bootstrap", func(t *testing.T) {
		cmd := NewSecretsCommand()
		bootstrapCmd, _, err := cmd.Find([]string{"bootstrap"})
		require.NoError(t, err)
		require.NotNil(t, bootstrapCmd)

		assert.Contains(t, cmd.Long, "  - bootstrap - "+bootstrapCmd.Short)
	})

	t.Run("mcp list-tools", func(t *testing.T) {
		cmd := NewMCPCommand()
		listToolsCmd, _, err := cmd.Find([]string{"list-tools"})
		require.NoError(t, err)
		require.NotNil(t, listToolsCmd)

		assert.Contains(t, cmd.Long, "  - list-tools - "+listToolsCmd.Short)
	})
}

func TestHelpTextUsesStandardEgPunctuation(t *testing.T) {
	t.Parallel()
	assert.Contains(t, coolDownFlagUsage, "(e.g., 7d", "--cool-down help should use e.g., punctuation")
	assert.Contains(t, NewEnvCommand().Long, "(e.g., default_max_turns)", "env help should use e.g., punctuation")
	assert.Contains(t, NewDomainsCommand().Long, "(e.g., \"node\", \"python\", \"github\", \"copilot\")", "domains help should use e.g., punctuation")
	assert.Contains(t, NewChecksCommand().Long, "(e.g., Vercel,", "checks help should use e.g., punctuation")
	assert.Contains(t, NewViewCommand().Long, "(e.g., issues,", "view help should use e.g., punctuation")
	assert.Contains(t, NewExperimentsAnalyzeSubcommand().Long, "e.g., \"my-workflow\"", "experiments analyze help should use e.g., punctuation")
}

func TestCommandExamplesDoNotEndWithTrailingNewline(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cmd  *cobra.Command
	}{
		{name: "add", cmd: NewAddCommand(func(string) error { return nil })},
		{name: "add-wizard", cmd: NewAddWizardCommand(func(string) error { return nil })},
		{name: "logs", cmd: NewLogsCommand()},
		{name: "trial", cmd: NewTrialCommand(func(string) error { return nil })},
		{name: "mcp add", cmd: NewMCPAddSubcommand()},
		{name: "mcp list", cmd: NewMCPListSubcommand()},
		{name: "mcp inspect", cmd: NewMCPInspectSubcommand()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotEmpty(t, tt.cmd.Example, "%s command should have examples", tt.name)
			assert.False(t, strings.HasSuffix(tt.cmd.Example, "\n"), "%s command Example should not end with a trailing newline, otherwise help output renders a double blank line before Flags:", tt.name)
		})
	}
}

func TestLegacyNestedGHHelpIsRejected(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		newCmd  func() *cobra.Command
		cmdName string
		args    []string
	}{
		{name: "secrets", newCmd: NewSecretsCommand, cmdName: "secrets", args: []string{"gh", "--help"}},
		{name: "secrets gh with args", newCmd: NewSecretsCommand, cmdName: "secrets", args: []string{"gh", "set", "--help"}},
		{name: "mcp", newCmd: NewMCPCommand, cmdName: "mcp", args: []string{"gh", "--help"}},
		{name: "project", newCmd: NewProjectCommand, cmdName: "project", args: []string{"gh", "--help"}},
		{name: "pr", newCmd: NewPRCommand, cmdName: "pr", args: []string{"gh", "--help"}},
		{name: "env", newCmd: NewEnvCommand, cmdName: "env", args: []string{"gh", "--help"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd := tt.newCmd()
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			cmd.SetArgs(tt.args)

			err := cmd.Execute()
			require.Error(t, err)
			assert.Equal(t, `unknown command "gh" for "`+tt.cmdName+`"`, err.Error())
		})
	}
}

//go:build !integration

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunCommandHelpTextConsistency(t *testing.T) {
	t.Parallel()
	assert.Contains(t, runCmd.Long, "this command enters interactive mode and shows", "run command interactive mode text should be explicit")
	assert.Contains(t, runCmd.Long, "workflow content from a branch or tag")
	assert.Contains(t, runCmd.Long, "requires the workflow file to exist on the default branch")
	assert.Contains(t, runCmd.Long, "does not register new workflows or reproduce pull_request_target events")

	runApprove := runCmd.Flags().Lookup("approve")
	runPush := runCmd.Flags().Lookup("push")
	runRawField := runCmd.Flags().Lookup("raw-field")
	runRef := runCmd.Flags().Lookup("ref")
	compileApprove := compileCmd.Flags().Lookup("approve")
	require.NotNil(t, runApprove, "run command should define --approve")
	require.NotNil(t, runPush, "run command should define --push")
	require.NotNil(t, runRawField, "run command should define --raw-field")
	require.NotNil(t, runRef, "run command should define --ref")
	require.NotNil(t, compileApprove, "compile command should define --approve")
	assert.Equal(t, compileApprove.Usage, runApprove.Usage, "run --approve and compile --approve should have identical usage descriptions")
	assert.Equal(t, "Commit and push workflow files (including transitive imports) before running. Refuses to proceed when unrelated files are already staged.", runPush.Usage, "run --push should describe the staged-files precondition precisely")
	assert.Equal(t, "F", runRawField.Shorthand, "run --raw-field should keep the legacy -F shorthand for compatibility")
	assert.Equal(t, "use the long form --raw-field instead", runRawField.ShorthandDeprecated, "run -F shorthand should be marked deprecated")
	assert.Contains(t, runRef.Usage, "requires default-branch registration")
	assert.Contains(t, runRef.Usage, "repository default with --repo")
}

func TestCompileScheduleSeedHelpUsesConsistentQuotes(t *testing.T) {
	t.Parallel()
	scheduleSeedFlag := compileCmd.Flags().Lookup("schedule-seed")
	require.NotNil(t, scheduleSeedFlag, "compile command should define --schedule-seed")
	assert.Contains(t, scheduleSeedFlag.Usage, "\"github/gh-aw\"", "--schedule-seed example should use double quotes")
	assert.Contains(t, scheduleSeedFlag.Usage, "\"origin\"", "--schedule-seed remote example should use double quotes")
	assert.Contains(t, scheduleSeedFlag.Usage, "(e.g.,", "--schedule-seed example should use standard e.g., punctuation")
}

func TestCompileStagedFlagHelpText(t *testing.T) {
	t.Parallel()
	stagedFlag := compileCmd.Flags().Lookup("staged")
	require.NotNil(t, stagedFlag, "compile command should define --staged")
	assert.Equal(t, "Force all safe-outputs into staged mode", stagedFlag.Usage)
}

func TestCompileShowAllFlagHelpText(t *testing.T) {
	t.Parallel()
	showAllFlag := compileCmd.Flags().Lookup("show-all")
	require.NotNil(t, showAllFlag, "compile command should define --show-all")
	assert.Equal(t, "Display all compilation errors instead of only the highest-priority subset (default: top 5)", showAllFlag.Usage)
}

func TestCompileStrictFlagHelpText(t *testing.T) {
	t.Parallel()
	strictFlag := compileCmd.Flags().Lookup("strict")
	require.NotNil(t, strictFlag, "compile command should define --strict")
	assert.Contains(t, strictFlag.Usage, "disallows write permissions and deprecated fields")
}

func TestCompileGhAwRefMutuallyExclusiveFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "gh-aw-ref with action-tag",
			args: []string{"compile", "--gh-aw-ref", "main", "--action-tag", "v1.2.3"},
		},
		{
			name: "gh-aw-ref with action-mode",
			args: []string{"compile", "--gh-aw-ref", "main", "--action-mode", "dev"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootCmd.SetArgs(tt.args)
			t.Cleanup(func() {
				rootCmd.SetArgs([]string{})
			})

			err := rootCmd.Execute()
			require.Error(t, err)
			require.ErrorContains(t, err, "if any flags in the group", "expected mutually exclusive flag-group error")
		})
	}
}

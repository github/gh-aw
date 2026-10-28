package lint

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCacheLowTrustWriteOverrides(t *testing.T) {
	const message = "Explicit cache-write access can override the read-only default outside GitHub's write-capable trigger allowlist; use cache-mode: read for untrusted execution and review reusable callers, publisher input validation, and cache consumers"
	for _, test := range []struct {
		name, on, mode string
		want           bool
	}{
		{"target write", "pull_request_target", "write", true},
		{"comment write only", "issue_comment", "write-only", true},
		{"issues write", "issues", "write-only", true},
		{"discussion write", "discussion", "write", true},
		{"discussion comment write", "discussion_comment", "write-only", true},
		{"mixed issues", "{push: {}, issues: {}}", "write-only", true},
		{"reusable caller unknown", "workflow_call", "write-only", true},
		{"workflow run write", "{workflow_run: {workflows: [CI], branches: [main]}}", "write", true},
		{"mixed events", "[push, issue_comment]", "write", true},
		{"mixed mapping", "{push: {}, pull_request_target: {}}", "write-only", true},
		{"low trust read", "pull_request_target", "read", false},
		{"low trust none", "workflow_run", "none", false},
		{"low trust default", "issue_comment", "", false},
		{"push writer", "push", "write", false},
		{"dispatch already defaults writable", "repository_dispatch", "write", false},
		{"manual writer", "workflow_dispatch", "write-only", false},
		{"merge ref isolated", "pull_request", "write", false},
	} {
		for _, target := range []ASTTargets{ActionsAST, AWAST} {
			t.Run(test.name+"/"+target.String(), func(t *testing.T) {
				source := "on: " + test.on + "\n" + contractJob + contractSafeSteps
				if test.mode != "" {
					source += "    cache-mode: " + test.mode + "\n"
				}
				input := ruleContractInput(t, source, target, false)
				before, err := json.Marshal(input)
				require.NoError(t, err)
				runner, err := Default().Select("cache-poisoning")
				require.NoError(t, err)
				diagnostics, err := runner.Run(context.Background(), input)
				require.NoError(t, err)
				if test.want {
					require.Len(t, diagnostics, 1)
					assert.Equal(t, "cache-poisoning", diagnostics[0].RuleID)
					assert.Equal(t, Warning, diagnostics[0].Severity)
					assert.Equal(t, "jobs.check.cache-mode", diagnostics[0].Path)
					assert.Equal(t, message, diagnostics[0].Message)
				} else {
					assert.Empty(t, diagnostics)
				}
				again, err := runner.Run(context.Background(), input)
				require.NoError(t, err)
				assert.Equal(t, diagnostics, again)
				after, err := json.Marshal(input)
				require.NoError(t, err)
				assert.Equal(t, before, after)
			})
		}
	}
}

func TestCacheWriteModeInheritance(t *testing.T) {
	for _, test := range []struct {
		name, root, job, want string
	}{
		{"inherited write", "write", "", "cache-mode"},
		{"inherited write only", "write-only", "", "cache-mode"},
		{"read overrides workflow write", "write", "read", ""},
		{"none overrides workflow write", "write-only", "none", ""},
		{"job overrides workflow read", "read", "write", "jobs.check.cache-mode"},
		{"job overrides workflow none", "none", "write-only", "jobs.check.cache-mode"},
		{"both write", "write", "write-only", "jobs.check.cache-mode"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := "on: issue_comment\ncache-mode: " + test.root + "\n" + contractJob + contractSafeSteps
			if test.job != "" {
				source += "    cache-mode: " + test.job + "\n"
			}
			input := ruleContractInput(t, source, ActionsAST, false)
			runner, err := Default().Select("cache-poisoning")
			require.NoError(t, err)
			diagnostics, err := runner.Run(context.Background(), input)
			require.NoError(t, err)
			if test.want == "" {
				assert.Empty(t, diagnostics)
			} else {
				require.Len(t, diagnostics, 1)
				assert.Equal(t, test.want, diagnostics[0].Path)
			}
		})
	}
}

func TestCacheIssueReusableCallerWriteOverride(t *testing.T) {
	for _, target := range []ASTTargets{ActionsAST, AWAST} {
		input := ruleContractInput(t, `on: {push: {}, issues: {}, discussion_comment: {}}
jobs:
  call:
    uses: ./.github/workflows/worker.yml
    cache-mode: write-only
`, target, false)
		runner, err := Default().Select("cache-poisoning")
		require.NoError(t, err)
		diagnostics, err := runner.Run(context.Background(), input)
		require.NoError(t, err)
		require.Len(t, diagnostics, 1)
		assert.Equal(t, "jobs.call.cache-mode", diagnostics[0].Path)
	}
}

func TestCacheWriterJobNamesDoNotEstablishTrust(t *testing.T) {
	for _, target := range []ASTTargets{ActionsAST, AWAST} {
		input := ruleContractInput(t, `on: issue_comment
jobs:
  update_cache_memory:
    runs-on: ubuntu-latest
    cache-mode: write-only
    steps: [{run: echo untrusted}]
  call:
    uses: ./.github/workflows/worker.yml
    cache-mode: write
`, target, false)
		runner, err := Default().Select("cache-poisoning")
		require.NoError(t, err)
		diagnostics, err := runner.Run(context.Background(), input)
		require.NoError(t, err)
		var paths []string
		for _, diagnostic := range diagnostics {
			paths = append(paths, diagnostic.Path)
		}
		assert.ElementsMatch(t, []string{"jobs.call.cache-mode", "jobs.update_cache_memory.cache-mode"}, paths)
	}
}

func TestCacheWriteModePartialFailures(t *testing.T) {
	input := ruleContractInput(t, `on: workflow_run
jobs:
  invalid:
    cache-mode: [PRIVATE_VALUE]
    steps: [{run: echo data}]
  publisher:
    cache-mode: write-only
    steps: [{run: echo data}]
`, AWAST, false)
	runner, err := Default().Select("cache-poisoning")
	require.NoError(t, err)
	diagnostics, err := runner.Run(context.Background(), input)
	require.ErrorContains(t, err, "jobs.invalid.cache-mode")
	assert.NotContains(t, err.Error(), "PRIVATE_VALUE")
	require.Len(t, diagnostics, 1)
	assert.Equal(t, "jobs.publisher.cache-mode", diagnostics[0].Path)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = runner.Run(ctx, input)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestCachePolicyUnrelatedTriggerIgnoresExecution(t *testing.T) {
	input := ruleContractInput(t, "on: push\nsteps: not-a-step-list\n", AWAST, false)
	runner, err := Default().Select("cache-poisoning")
	require.NoError(t, err)
	diagnostics, err := runner.Run(context.Background(), input)
	require.NoError(t, err)
	assert.Empty(t, diagnostics)
}

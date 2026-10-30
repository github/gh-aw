//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExcludeLockYmlConfig(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
			compiler := NewCompiler()
			outputs := map[string]any{
				"create-pull-request": map[string]any{
					"exclude-lock-yml": enabled,
					"excluded-files":   []any{"dist/**"},
				},
				"push-to-pull-request-branch": map[string]any{
					"exclude-lock-yml": enabled,
					"excluded-files":   []any{"dist/**"},
				},
			}
			create := compiler.parseCreatePullRequestsConfig(outputs)
			push := compiler.parsePushToPullRequestBranchConfig(outputs)
			require.NotNil(t, create)
			require.NotNil(t, push)
			assert.Equal(t, enabled, create.ExcludeLockYml)
			assert.Equal(t, enabled, push.ExcludeLockYml)
			config := &SafeOutputsConfig{CreatePullRequests: create, PushToPullRequestBranch: push}
			for _, handler := range []string{"create_pull_request", "push_to_pull_request_branch"} {
				built := pullRequestHandlerRegistry[handler](config)
				assert.Equal(t, []string{"dist/**"}, built["excluded_files"])
				if enabled {
					assert.Equal(t, true, built["exclude_lock_yml"])
				} else {
					assert.NotContains(t, built, "exclude_lock_yml")
				}
			}
			require.NoError(t, validateSafeOutputsAllowWorkflows(config))
		})
	}
}

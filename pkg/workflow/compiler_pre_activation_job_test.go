//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPreActivationPermissions(t *testing.T) {
	t.Run("release mode without optional checks keeps permissions empty", func(t *testing.T) {
		c := NewCompiler()
		c.SetActionMode(ActionModeRelease)

		steps, permissions := c.buildPreActivationPermissions(&WorkflowData{Name: "wf"}, "./actions/setup")
		stepsStr := strings.Join(steps, "\n")

		require.NotEmpty(t, steps)
		assert.Contains(t, stepsStr, "Setup Scripts")
		assert.NotContains(t, stepsStr, "Checkout actions folder")
		assert.Empty(t, permissions)
	})

	t.Run("centralized command grants pull request read", func(t *testing.T) {
		c := NewCompiler()
		c.SetActionMode(ActionModeRelease)

		_, permissions := c.buildPreActivationPermissions(&WorkflowData{
			Name:               "wf",
			Command:            []string{"triage"},
			CommandCentralized: true,
		}, "./actions/setup")

		assert.Contains(t, permissions, "pull-requests: read")
	})

	t.Run("non-centralized command keeps permissions empty", func(t *testing.T) {
		c := NewCompiler()
		c.SetActionMode(ActionModeRelease)

		_, permissions := c.buildPreActivationPermissions(&WorkflowData{
			Name:    "wf",
			Command: []string{"triage"},
		}, "./actions/setup")

		assert.Empty(t, permissions)
	})

	t.Run("script mode merges inferred and explicit permissions", func(t *testing.T) {
		c := NewCompiler()
		c.SetActionMode(ActionModeScript)

		data := &WorkflowData{
			Name:                      "wf",
			RateLimit:                 &RateLimitConfig{},
			LabelCommandDecentralized: true,
			LabelCommandEvents:        []string{"pull_request"},
			OnPermissions: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
				PermissionIssues: PermissionWrite,
			}),
		}

		steps, permissions := c.buildPreActivationPermissions(data, "./actions/setup")
		stepsStr := strings.Join(steps, "\n")

		require.NotEmpty(t, steps)
		assert.Contains(t, stepsStr, "Checkout actions folder")
		assert.Contains(t, permissions, "actions: read")
		assert.Contains(t, permissions, "contents: read")
		assert.Contains(t, permissions, "issues: write")
		assert.Contains(t, permissions, "pull-requests: read")
	})
}

func TestApplyPreActivationIfConditionGuards(t *testing.T) {
	baseIf := "github.ref == 'refs/heads/main'"

	t.Run("combines label and comment-author guards when eligible", func(t *testing.T) {
		c := NewCompiler()
		data := &WorkflowData{
			LabelNames: []string{"bug"},
			On:         "issue_comment:\n  types: [created]\n",
			Bots:       []string{"dependabot[bot]"},
		}

		result := c.applyPreActivationIfConditionGuards(data, true, baseIf)

		assert.Contains(t, result, "github.event.label == null")
		assert.Contains(t, result, "github.event.label.name == 'bug'")
		assert.Contains(t, result, "author_association")
		assert.Contains(t, result, baseIf)
	})

	t.Run("does not add comment-author guard for expression-based bot list", func(t *testing.T) {
		c := NewCompiler()
		data := &WorkflowData{
			On:   "issue_comment:\n  types: [created]\n",
			Bots: []string{"${{ vars.ALLOWED_BOT }}"},
		}

		result := c.applyPreActivationIfConditionGuards(data, true, baseIf)

		assert.NotContains(t, result, "author_association")
		assert.Equal(t, baseIf, result)
	})

	t.Run("adds skip-author-associations guard", func(t *testing.T) {
		c := NewCompiler()
		data := &WorkflowData{
			SkipAuthorAssociations: map[string][]string{
				"issue_comment": {"OWNER", "MEMBER"},
			},
		}

		result := c.applyPreActivationIfConditionGuards(data, false, "")

		assert.Contains(t, result, "github.event_name")
		assert.Contains(t, result, "github.event.comment.author_association")
	})
}

func TestBuildPreActivationJob(t *testing.T) {
	c := NewCompiler()
	c.SetActionMode(ActionModeDev)

	data := &WorkflowData{
		Name:       "pre-activation unit test",
		If:         "github.ref == 'refs/heads/main'",
		LabelNames: []string{"bug"},
		On:         "issue_comment:\n  types: [created]\n",
		Bots:       []string{"dependabot[bot]"},
		OnNeeds:    []string{"prepare", "prepare"},
		OnSteps: []map[string]any{
			{
				"id":   "gate",
				"run":  "echo gate",
				"name": "gate",
			},
		},
	}

	job, err := c.buildPreActivationJob(data, true)
	require.NoError(t, err)
	require.NotNil(t, job)

	assert.Contains(t, job.If, "github.event.label == null")
	assert.Contains(t, job.If, "author_association")
	assert.Contains(t, job.If, data.If)
	assert.Equal(t, []string{"prepare"}, job.Needs)
	assert.Contains(t, job.Outputs, "gate_result")
	assert.Contains(t, job.Outputs, "matched_command")
	assert.Contains(t, job.Outputs["matched_command"], "''")
}

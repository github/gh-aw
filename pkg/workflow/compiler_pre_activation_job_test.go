//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPreActivationActivatedExpression(t *testing.T) {
	for _, tt := range []struct {
		name string
		data WorkflowData
	}{
		{"custom steps", WorkflowData{OnSteps: []map[string]any{{"run": "echo gate"}}}},
		{"custom needs", WorkflowData{OnNeeds: []string{"prepare"}}},
		{"author associations", WorkflowData{SkipAuthorAssociations: map[string][]string{"issues": {"BOT"}}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			value, err := buildPreActivationActivatedExpression(&tt.data, nil)
			require.NoError(t, err)
			assert.Equal(t, `"true"`, value)
			var outputs map[string]any
			require.NoError(t, yaml.Unmarshal([]byte("activated: "+value), &outputs))
			assert.Equal(t, "true", outputs["activated"], "job outputs must remain strings, not YAML booleans")
		})
	}
	t.Run("conditions remain expressions", func(t *testing.T) {
		conditions := []ConditionNode{
			BuildComparison(BuildPropertyAccess("steps.gate.outputs.ok"), "==", BuildStringLiteral("true")),
			BuildComparison(BuildPropertyAccess("steps.check.outputs.ok"), "==", BuildStringLiteral("true")),
		}
		for _, count := range []int{1, 2} {
			value, err := buildPreActivationActivatedExpression(&WorkflowData{}, conditions[:count])
			require.NoError(t, err)
			assert.Equal(t, "${{ "+strings.Join([]string{
				"steps.gate.outputs.ok == 'true'",
				"steps.check.outputs.ok == 'true'",
			}[:count], " && ")+" }}", value)
		}
	})
	t.Run("missing checks remain an error", func(t *testing.T) {
		_, err := buildPreActivationActivatedExpression(&WorkflowData{}, nil)
		require.ErrorContains(t, err, "without activation checks or custom steps")
	})
}

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
			SkipIfMatch:               &SkipIfMatchConfig{Query: "is:issue is:open"},
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

func TestPreActivationSearchPermissions(t *testing.T) {
	for _, gate := range []string{
		`skip-if-match: 'is:issue is:open in:title "[airgap-guide-drift] "'`,
		`skip-if-no-match: "is:pr is:open"`,
		"skip-if-match: \"is:issue is:open\"\n  skip-if-no-match: \"is:pr is:open\"",
	} {
		t.Run(gate, func(t *testing.T) {
			workflowFile := filepath.Join(testutil.TempDir(t, "search-permissions"), "workflow.md")
			content := "---\non:\n  pull_request:\n    types: [opened]\n  " + gate + "\nengine: claude\n---\n\nSearch gate workflow.\n"
			require.NoError(t, os.WriteFile(workflowFile, []byte(content), 0600))

			compiler := NewCompiler()
			compiler.SetActionMode(ActionModeRelease)
			require.NoError(t, compiler.CompileWorkflow(workflowFile))

			lockContent, err := os.ReadFile(strings.TrimSuffix(workflowFile, ".md") + ".lock.yml")
			require.NoError(t, err)
			var workflow struct {
				Permissions map[string]string `yaml:"permissions"`
				Jobs        map[string]struct {
					Permissions map[string]string `yaml:"permissions"`
				} `yaml:"jobs"`
			}
			require.NoError(t, yaml.Unmarshal(lockContent, &workflow))
			assert.Empty(t, workflow.Permissions)
			job, ok := workflow.Jobs["pre_activation"]
			require.True(t, ok)
			assert.Equal(t, map[string]string{
				"issues":        "read",
				"pull-requests": "read",
			}, job.Permissions)
		})
	}
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

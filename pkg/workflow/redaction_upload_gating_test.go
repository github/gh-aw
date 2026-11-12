//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCustomSecretMaskingStepIDs(t *testing.T) {
	data := &WorkflowData{SecretMasking: &SecretMaskingConfig{
		Steps: []map[string]any{
			{"name": "Unnamed", "run": "echo redacted"},
			{"id": "redact_secrets_custom_0", "run": "echo redacted"},
			{"id": "explicit-hook", "run": "echo redacted"},
		},
	}}
	ids := customSecretMaskingStepIDs(data)
	require.Equal(t, []string{"redact_secrets_custom_0_", "redact_secrets_custom_0", "explicit-hook"}, ids)
	compiler := NewCompiler()
	compiler.stepOrderTracker = NewStepOrderTracker()
	var generated strings.Builder
	compiler.generateTrackedSecretRedactionStep(&generated, "", data)
	for _, id := range ids {
		assert.Contains(t, generated.String(), "id: "+id+"\n")
		assert.Contains(t, secretRedactionSuccessCondition(data), "steps."+id+".outcome == 'success'")
	}
	assert.NotContains(t, data.SecretMasking.Steps[0], "id", "generation must not mutate the workflow configuration")
	assert.Equal(t, "redact_secrets_custom_0", data.SecretMasking.Steps[1]["id"])
}

func TestCustomSecretMaskingUploadGates(t *testing.T) {
	for _, sandbox := range []struct {
		name   string
		config string
	}{
		{name: "AWF"},
		{
			name: "without AWF",
			config: `features:
  dangerously-disable-sandbox-agent: true
sandbox:
  agent: false
`,
		},
	} {
		t.Run(sandbox.name, func(t *testing.T) {
			for _, tc := range []struct {
				name    string
				masking string
				hookIDs []string
			}{
				{name: "no hooks"},
				{
					name: "successful hook",
					masking: `secret-masking:
  steps:
    - name: Custom mask
      run: echo redacted
`,
					hookIDs: []string{"redact_secrets_custom_0"},
				},
				{
					name: "failed hook with continue-on-error",
					masking: `secret-masking:
  steps:
    - name: Custom mask
      id: custom-mask
      continue-on-error: true
      run: exit 1
`,
					hookIDs: []string{"custom-mask"},
				},
				{
					name: "skipped hook",
					masking: `secret-masking:
  steps:
    - name: Custom mask
      if: "false"
      run: echo redacted
`,
					hookIDs: []string{"redact_secrets_custom_0"},
				},
				{
					name: "multiple hooks",
					masking: `secret-masking:
  steps:
    - name: First custom mask
      run: echo redacted
    - name: Second custom mask
      id: second_mask
      if: always()
      run: echo redacted
`,
					hookIDs: []string{"redact_secrets_custom_0", "second_mask"},
				},
			} {
				t.Run(tc.name, func(t *testing.T) {
					dir := testutil.TempDir(t, "custom-masking-upload")
					source := "---\non: workflow_dispatch\npermissions:\n  contents: read\nengine: copilot\nstrict: false\nsafe-outputs:\n  create-issue:\n  threat-detection: false\n" + sandbox.config + tc.masking + "---\n\nTest custom redaction gating.\n"
					path := filepath.Join(dir, "workflow.md")
					require.NoError(t, os.WriteFile(path, []byte(source), 0644))
					require.NoError(t, NewCompiler().CompileWorkflow(path))
					content, err := os.ReadFile(stringutil.MarkdownToLockFile(path))
					require.NoError(t, err)
					var workflow struct {
						Jobs map[string]struct {
							Steps []struct {
								Name string `yaml:"name"`
								ID   string `yaml:"id"`
								If   string `yaml:"if"`
							} `yaml:"steps"`
						} `yaml:"jobs"`
					}
					require.NoError(t, yaml.Unmarshal(content, &workflow))
					steps := workflow.Jobs["agent"].Steps
					require.NotEmpty(t, steps)
					redactionCount := 0
					for _, step := range steps {
						if step.ID == "redact_secrets" {
							redactionCount++
						}
					}
					require.Equal(t, 1, redactionCount, "upload guards must reference exactly one built-in redaction step")
					redactionIDs := append([]string{"redact_secrets"}, tc.hookIDs...)
					redactionIDs = append(redactionIDs, "validate_recovery_envelopes", "validate_final_recovery_envelopes")
					outcomes := map[string]string{}
					for _, id := range redactionIDs {
						outcomes["steps."+id+".outcome"] = "success"
					}
					seen := map[string]bool{}
					uploadCount := 0
					for _, step := range steps {
						seen[step.ID] = true
						if step.Name != "Upload agent artifacts" && step.Name != "Upload agent output fallback artifact" && step.Name != "Append agent step summary" {
							continue
						}
						uploadCount++
						requiredIDs := redactionIDs
						if step.Name == "Append agent step summary" {
							requiredIDs = redactionIDs[:len(redactionIDs)-2]
						}
						var expectedCondition strings.Builder
						expectedCondition.WriteString("always()")
						for _, id := range requiredIDs {
							require.True(t, seen[id], "redactor %s must precede %s", id, step.Name)
							expectedCondition.WriteString(" && steps.")
							expectedCondition.WriteString(id)
							expectedCondition.WriteString(".outcome == 'success'")
						}
						require.Equal(t, expectedCondition.String(), step.If, step.Name)
						condition, err := ParseExpression(step.If)
						require.NoError(t, err)
						assert.True(t, evaluateRedactionUploadGate(t, condition, outcomes), "all successful redactors permit upload independently of the agent outcome")
						for _, id := range requiredIDs {
							for _, outcome := range []string{"failure", "skipped", "cancelled", ""} {
								outcomes["steps."+id+".outcome"] = outcome
								assert.False(t, evaluateRedactionUploadGate(t, condition, outcomes), "%s must not upload when %s outcome is %q", step.Name, id, outcome)
							}
							outcomes["steps."+id+".outcome"] = "success"
						}
					}
					assert.Equal(t, 3, uploadCount)
				})
			}
		})
	}
}

// Evaluate only the status function and outcome comparisons used by these upload gates.
func evaluateRedactionUploadGate(t *testing.T, node ConditionNode, outcomes map[string]string) bool {
	t.Helper()
	switch node := node.(type) {
	case *AndNode:
		return evaluateRedactionUploadGate(t, node.Left, outcomes) && evaluateRedactionUploadGate(t, node.Right, outcomes)
	case *ExpressionNode:
		if node.Expression == "always()" {
			return true
		}
		property, expected, comparison := strings.Cut(node.Expression, " == '")
		require.True(t, comparison, "unexpected comparison: %s", node.Expression)
		expected, quoted := strings.CutSuffix(expected, "'")
		require.True(t, quoted, "unexpected comparison: %s", node.Expression)
		require.Contains(t, outcomes, property)
		return outcomes[property] == expected
	default:
		t.Fatalf("Unexpected upload gate node: %T", node)
		return false
	}
}

func TestCustomSecretMaskingWorkQueueUploadGates(t *testing.T) {
	data := &WorkflowData{SecretMasking: &SecretMaskingConfig{
		Steps: []map[string]any{{"id": "custom_mask", "run": "exit 1"}},
	}}
	compiler := NewCompiler()
	compiler.stepOrderTracker = NewStepOrderTracker()
	adapter := strings.Join(compiler.finishWorkQueuePreparedAdapterSteps(data, "test", 0, nil), "")
	assert.Contains(t, adapter, "steps.claim_adapter_context.outputs.active == 'true' && steps.redact_secrets.outcome == 'success' && steps.custom_mask.outcome == 'success'")
	assert.Contains(t, adapter, "id: validate_recovery_envelopes\n")
	assert.Contains(t, adapter, "steps.validate_recovery_envelopes.outcome == 'success'")
	manifest := strings.Join(buildSafeOutputManifestUploadStep("", compiler.getActionPin, true, data, true), "")
	assert.Contains(t, manifest, "if: always() && steps.redact_secrets.outcome == 'success' && steps.custom_mask.outcome == 'success'")
	assert.Contains(t, manifest, "steps.validate_recovery_envelopes.outcome == 'success'")
}

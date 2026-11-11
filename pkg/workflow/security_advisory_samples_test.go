//go:build !integration

package workflow

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecurityAdvisorySamplesCompile(t *testing.T) {
	for _, tool := range []struct {
		key    string
		name   string
		sample map[string]any
	}{
		{
			key: "create-security-advisory", name: "create_security_advisory",
			sample: map[string]any{
				"summary": "Sample draft advisory", "description": "Reproduction and remediation details.",
				"vulnerabilities": []any{map[string]any{"package": map[string]any{"ecosystem": "npm", "name": "example"}}},
			},
		},
		{
			key: "add-security-advisory-comment", name: "add_security_advisory_comment",
			sample: map[string]any{"ghsa_id": "GHSA-2345-6789-cfgh", "body": "The proposed fix is ready for review."},
		},
	} {
		for _, shape := range []string{"object", "list", "invalid payload", "invalid shape"} {
			t.Run(tool.key+"/"+shape, func(t *testing.T) {
				var samples any = tool.sample
				switch shape {
				case "list":
					samples = []any{tool.sample}
				case "invalid payload":
					samples = map[string]any{"body": "Missing required advisory fields."}
				case "invalid shape":
					samples = "not a sample object"
				}
				frontmatter, err := yaml.Marshal(map[string]any{
					"on": "workflow_dispatch", "strict": false, "engine": "copilot",
					"features": map[string]any{"samples": true},
					"safe-outputs": map[string]any{
						"create-issue": false, "report-missing-tool": false, "report-missing-data": false,
						tool.key: map[string]any{"staged": true, "samples": samples},
					},
				})
				require.NoError(t, err)
				path := filepath.Join(t.TempDir(), "advisory-samples.md")
				require.NoError(t, os.WriteFile(path, []byte("---\n"+string(frontmatter)+"---\nReplay the configured advisory sample.\n"), 0o600))
				compiler := NewCompiler(WithVersion("test"))
				err = compiler.CompileWorkflow(path)
				if strings.HasPrefix(shape, "invalid") {
					require.Error(t, err)
					assert.Contains(t, err.Error(), "samples")
					return
				}
				require.NoError(t, err)
				data, err := compiler.ParseWorkflowFile(path)
				require.NoError(t, err)
				assert.True(t, data.UseSamples)
				assert.Equal(t, []SampleEntry{{Tool: tool.name, Arguments: tool.sample}}, collectSampleEntries(data.SafeOutputs))
				lock, err := os.ReadFile(strings.TrimSuffix(path, ".md") + ".lock.yml")
				require.NoError(t, err)
				assert.Contains(t, string(lock), "Replay safe-outputs samples (deterministic)")
				assert.Contains(t, string(lock), "apply_samples.cjs")
				assert.Contains(t, string(lock), `"tool":"`+tool.name+`"`)
				assert.Contains(t, string(lock), "id: process_safe_outputs")
			})
		}
	}
}

func TestSecurityAdvisoryExperimentalWarnings(t *testing.T) {
	const warning = "Using experimental feature: security advisory safe outputs"
	for _, tt := range []struct {
		name   string
		config *SafeOutputsConfig
		want   bool
	}{
		{name: "draft", config: &SafeOutputsConfig{CreateSecurityAdvisory: &CreateSecurityAdvisoryConfig{}}, want: true},
		{name: "comment", config: &SafeOutputsConfig{AddSecurityAdvisoryComment: &AddSecurityAdvisoryCommentConfig{}}, want: true},
		{name: "both", config: &SafeOutputsConfig{CreateSecurityAdvisory: &CreateSecurityAdvisoryConfig{}, AddSecurityAdvisoryComment: &AddSecurityAdvisoryCommentConfig{}}, want: true},
		{name: "unrelated", config: &SafeOutputsConfig{CreateIssues: &CreateIssuesConfig{}}},
		{name: "absent"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			var output bytes.Buffer
			compiler.emitExperimentalFeatureWarningsTo(&WorkflowData{SafeOutputs: tt.config}, &output)
			if tt.want {
				assert.Equal(t, 1, strings.Count(output.String(), warning))
				assert.Equal(t, 1, compiler.GetWarningCount())
			} else {
				assert.NotContains(t, output.String(), warning)
				assert.NotContains(t, compiler.GetExperimentalFeatureUsage(), warning)
			}
			batch := NewCompiler()
			batch.SetBatchMode(true)
			output.Reset()
			batch.emitExperimentalFeatureWarningsTo(&WorkflowData{SafeOutputs: tt.config}, &output)
			assert.Empty(t, output.String())
			if tt.want {
				assert.Equal(t, 1, batch.GetExperimentalFeatureUsage()[warning])
			} else {
				assert.NotContains(t, batch.GetExperimentalFeatureUsage(), warning)
			}
		})
	}
}

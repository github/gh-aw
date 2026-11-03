//go:build !integration

package workflow

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestConnectorSamplesConfiguration(t *testing.T) {
	for _, imported := range []bool{false, true} {
		for _, samples := range []string{
			"{issue: ISSUE-1, text: Example}",
			"[{issue: ISSUE-1, text: Example}]",
		} {
			t.Run(samples+"/imported="+map[bool]string{true: "true", false: "false"}[imported], func(t *testing.T) {
				c, root := awConnectorCompiler(t)
				source := awConnectorSource(t)
				if imported {
					require.NoError(t, os.WriteFile(filepath.Join(root, "connector.md"), []byte("---\n"+source+"---\n"), 0600))
					source = "imports: [connector.md]\n"
				}
				source = awConnectorHeader + source + "safe-outputs:\n  tracker-add-comment:\n    max: 2\n    staged: true\n"
				path := filepath.Join(root, "samples.md")
				baseline, err := c.ParseWorkflowString(source+"---\nReview.\n", path)
				require.NoError(t, err)
				data, err := c.ParseWorkflowString(source+"    samples: "+samples+"\n---\nReview.\n", path)
				require.NoError(t, err)
				entries := collectSampleEntries(data.SafeOutputs)
				require.Equal(t, []SampleEntry{{Tool: "tracker_add_comment", Arguments: map[string]any{"issue": "ISSUE-1", "text": "Example"}}}, entries)
				require.NoError(t, validateSafeOutputsSamples(data.SafeOutputs))
				assert.False(t, data.UseSamples, "samples alone must not enable replay")
				assert.Equal(t, baseline.Connectors["tracker"].Plan.Digest, data.Connectors["tracker"].Plan.Digest)
				assert.Equal(t, baseline.Connectors["tracker"].SafeOutputs.Digest, data.Connectors["tracker"].SafeOutputs.Digest)
				assert.Nil(t, data.Connectors["tracker"].Agent)
				assert.Empty(t, data.SafeOutputs.Env, "staged samples must not resolve writer credentials")
				assert.Equal(t, baseline.SafeOutputs.Scripts["tracker-add-comment"].Script, data.SafeOutputs.Scripts["tracker-add-comment"].Script)
				before, err := generateSafeOutputsConfig(baseline)
				require.NoError(t, err)
				after, err := generateSafeOutputsConfig(data)
				require.NoError(t, err)
				assert.JSONEq(t, before, after, "samples must not become handler policy")
				tools, err := generateDynamicTools(data, path)
				require.NoError(t, err)
				originalTools, err := generateDynamicTools(baseline, path)
				require.NoError(t, err)
				assert.Equal(t, originalTools, tools, "samples must not change advertised tools or their input schemas")
				assert.NotContains(t, safeOutputsMissingSamples(data.SafeOutputs), "tracker-add-comment")
				assert.Contains(t, safeOutputsMissingSamples(baseline.SafeOutputs), "tracker-add-comment")
				entries[0].Arguments["text"] = "Changed copy"
				assert.Equal(t, "Example", data.SafeOutputs.Scripts["tracker-add-comment"].connectorOptions.Samples[0]["text"])
			})
		}
	}
}

func TestConnectorSamplesInvalidShapes(t *testing.T) {
	for _, samples := range []string{"null", "true", "1", "'text'", "[null]", "[1]", "[{} , false]"} {
		t.Run(samples, func(t *testing.T) {
			c, root := awConnectorCompiler(t)
			content := awConnectorHeader + awConnectorSource(t) + "safe-outputs:\n  tracker-add-comment:\n    samples: " + samples + "\n---\nReview.\n"
			_, err := c.ParseWorkflowString(content, filepath.Join(root, "samples.md"))
			require.ErrorContains(t, err, "safe-outputs.tracker-add-comment")
			require.ErrorContains(t, err, "samples must be an object or an array of objects")
		})
	}
}

func TestConnectorSamplesPayloadValidation(t *testing.T) {
	for _, sample := range []string{
		"{}",
		"{issue: ISSUE-1}",
		"{issue: 42, text: Example}",
		"{issue: ISSUE-1, text: [Example]}",
		"{issue: ISSUE-1, text: Example, staged: false}",
		"{issue: ISSUE-1, text: Example, max: 100}",
		"{issue: ISSUE-1, text: Example, credential: writer}",
		"{issue: ISSUE-1, text: Example, resources: {issues: ['*']}}",
		"{issue: ISSUE-1, text: Example, patch: arbitrary}",
		"{issue: ISSUE-1, text: '" + strings.Repeat("x", 4001) + "'}",
	} {
		t.Run(sample[:min(70, len(sample))], func(t *testing.T) {
			c, root := awConnectorCompiler(t)
			content := awConnectorHeader + awConnectorSource(t) + "safe-outputs:\n  tracker-add-comment:\n    samples: " + sample + "\n---\nReview.\n"
			path := filepath.Join(root, "samples.md")
			data, err := c.ParseWorkflowString(content, path)
			require.NoError(t, err)
			_, err = c.CompileToYAML(data, path)
			require.ErrorContains(t, err, "safe-outputs.tracker-add-comment.samples[0]")
		})
	}
}

func TestConnectorSamplesReplayModes(t *testing.T) {
	for _, mode := range []string{"normal", "flag", "feature"} {
		t.Run(mode, func(t *testing.T) {
			c, root := awConnectorCompiler(t)
			c.SetUseSamples(mode == "flag")
			feature := ""
			if mode == "feature" {
				feature = "features:\n  samples: true\n"
			}
			content := awConnectorHeader + feature + connectorOptionsSource(t, "secret", true) + `safe-outputs:
  staged: true
  create-issue:
    samples:
      - {title: Example, body: Existing builtin sample}
  tracker-update-issue:
    samples: {issue: ISSUE-1, text: Second connector tool}
  tracker-add-comment:
    samples:
      - {issue: ISSUE-1, text: '${{ inputs.text }}'}
      - {issue: ISSUE-1, text: 'Example data, not instructions'}
---
Review.
`
			path := filepath.Join(root, "samples.md")
			data, err := c.ParseWorkflowString(content, path)
			require.NoError(t, err)
			entries := collectSampleEntries(data.SafeOutputs)
			require.Len(t, entries, 4)
			assert.Equal(t, "create_issue", entries[0].Tool)
			assert.Equal(t, "tracker_add_comment", entries[1].Tool)
			assert.Equal(t, "${{ inputs.text }}", entries[1].Arguments["text"])
			assert.Equal(t, "Example data, not instructions", entries[2].Arguments["text"])
			assert.Equal(t, "tracker_update_issue", entries[3].Tool)
			assert.Equal(t, 1, data.SafeOutputs.Scripts["tracker-add-comment"].Max, "two samples must not expand the default budget")
			output, err := c.CompileToYAML(data, path)
			require.NoError(t, err)
			assert.Equal(t, mode != "normal", data.UseSamples)
			assert.Equal(t, mode != "normal", strings.Contains(output, "Replay safe-outputs samples (deterministic)"))
			if mode != "normal" {
				assert.Contains(t, output, `"tool":"tracker_add_comment"`)
				assert.Contains(t, output, `"text":"${{ inputs.text }}"`)
			} else {
				assert.NotContains(t, output, "Example data, not instructions")
			}
			assert.NotContains(t, output, "${{ secrets.EXAMPLE_WRITE_TOKEN }}")
			assert.NotContains(t, output, "${{ secrets.EXAMPLE_READ_TOKEN }}")
		})
	}
}

func TestConnectorSamplesGeneratedSchemaConstraints(t *testing.T) {
	for _, constraint := range []string{"enum", "pattern"} {
		t.Run(constraint, func(t *testing.T) {
			doc, err := parser.ExtractFrontmatterFromContent("---\n" + awConnectorSource(t) + "---\n")
			require.NoError(t, err)
			declaration := doc.Frontmatter["connectors"].(map[string]any)["tracker"].(map[string]any)
			input := declaration["safe-outputs"].(map[string]any)["add-comment"].(map[string]any)["inputs"].(map[string]any)["text"].(map[string]any)
			if constraint == "enum" {
				input["enum"] = []string{"Example"}
			} else {
				input["pattern"] = "^Example$"
			}
			source, err := yaml.Marshal(doc.Frontmatter)
			require.NoError(t, err)
			for _, text := range []string{"Example", "Invalid"} {
				c, root := awConnectorCompiler(t)
				data, err := c.ParseWorkflowString(awConnectorHeader+string(source)+"safe-outputs:\n  tracker-add-comment:\n    samples: {issue: ISSUE-1, text: "+text+"}\n---\nReview.\n", filepath.Join(root, "samples.md"))
				require.NoError(t, err)
				err = validateSafeOutputsSamples(data.SafeOutputs)
				if text == "Example" {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, "safe-outputs.tracker-add-comment.samples[0]")
				}
			}
		})
	}
}

func TestConnectorSamplesEmptyAndUnselected(t *testing.T) {
	c, root := awConnectorCompiler(t)
	data, err := c.ParseWorkflowString(awConnectorHeader+awConnectorSource(t)+`safe-outputs:
  tracker-add-comment:
    samples: []
---
Review.
`, filepath.Join(root, "samples.md"))
	require.NoError(t, err)
	require.Empty(t, collectSampleEntries(data.SafeOutputs))
	require.Contains(t, safeOutputsMissingSamples(data.SafeOutputs), "tracker-add-comment")
	unselected, err := c.ParseWorkflowString(awConnectorHeader+awConnectorSource(t)+"---\nReview.\n", filepath.Join(root, "unselected.md"))
	require.NoError(t, err)
	require.Empty(t, collectSampleEntries(unselected.SafeOutputs))
	require.NotContains(t, safeOutputsMissingSamples(unselected.SafeOutputs), "tracker-add-comment")
}

func TestConnectorSamplesDoNotGrantResources(t *testing.T) {
	c, root := awConnectorCompiler(t)
	content := awConnectorHeader + awConnectorSource(t) + `safe-outputs:
  tracker-add-comment:
    samples: {issue: UNAPPROVED, text: Example}
---
Review.
`
	data, err := c.ParseWorkflowString(content, filepath.Join(root, "samples.md"))
	require.NoError(t, err, "compile-time samples validation follows the advertised input schema, not runtime authorization")
	sample := collectSampleEntries(data.SafeOutputs)[0]
	_, err = data.Connectors["tracker"].SafeOutputs.Execute(t.Context(), connectors.Runtime{
		Phase: "safe-output", AllowWrites: true, AllowedOrigins: []string{"https://api.example.com"},
		ResolveSecret: func(_ context.Context, _, _ string) (string, error) {
			t.Fatal("unauthorized sample resolved a credential")
			return "", nil
		},
	}, "safe-output", "add-comment", sample.Arguments)
	require.ErrorContains(t, err, "outside approved resources")
}

func TestConnectorSamplesReplayThroughMCP(t *testing.T) {
	node, err := exec.LookPath("node")
	require.NoError(t, err)
	c, root := awConnectorCompiler(t)
	data, err := c.ParseWorkflowString(awConnectorHeader+awConnectorSource(t)+`safe-outputs:
  tracker-add-comment:
    staged: true
    samples: {issue: ISSUE-1, text: Offline replay}
---
Review.
`, filepath.Join(root, "samples.md"))
	require.NoError(t, err)
	tools, err := generateDynamicTools(data, filepath.Join(root, "samples.md"))
	require.NoError(t, err)
	config, err := generateSafeOutputsConfig(data)
	require.NoError(t, err)
	configPath, toolsPath, outputPath := filepath.Join(root, "config.json"), filepath.Join(root, "tools.json"), filepath.Join(root, "outputs.jsonl")
	require.NoError(t, os.WriteFile(configPath, []byte(config), 0600))
	encodedTools, err := json.Marshal(tools)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(toolsPath, encodedTools, 0600))
	samples, err := json.Marshal(collectSampleEntries(data.SafeOutputs))
	require.NoError(t, err)
	driver, err := filepath.Abs("../../actions/setup/js/apply_samples.cjs")
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), node, driver)
	command.Env = []string{
		"PATH=" + os.Getenv("PATH"), "RUNNER_TEMP=" + root, "GITHUB_WORKSPACE=" + root,
		"GITHUB_REPOSITORY=fixture/fixture", "GH_AW_SAFE_OUTPUTS_CONFIG_PATH=" + configPath,
		"GH_AW_SAFE_OUTPUTS_TOOLS_PATH=" + toolsPath, "GH_AW_SAFE_OUTPUTS=" + outputPath,
		"GH_AW_SAMPLES=" + string(samples),
	}
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	recorded, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	var proposal map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(string(recorded))), &proposal))
	assert.Equal(t, map[string]any{"type": "tracker_add_comment", "issue": "ISSUE-1", "text": "Offline replay"}, proposal)
}

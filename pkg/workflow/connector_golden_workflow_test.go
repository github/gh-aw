//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/require"
)

func TestConnectorGoldenWorkflows(t *testing.T) {
	declarations, err := filepath.Glob("../../.github/workflows/connectors/*.md")
	require.NoError(t, err)
	require.Len(t, declarations, len(connectorCompilerScenarios))
	for _, scenario := range connectorCompilerScenarios {
		provider := scenario.provider
		t.Run(provider, func(t *testing.T) {
			path := filepath.Join("../../.github/workflows", provider+"-golden.md")
			source, err := os.ReadFile(path)
			require.NoError(t, err)
			document, err := parser.ExtractFrontmatterFromContent(string(source))
			require.NoError(t, err)
			validateGoldenWorkflowSource(t, provider, document.Frontmatter)
			root, err := filepath.Abs("../..")
			require.NoError(t, err)
			compiler := NewCompiler(WithVersion("v0.0.0"))
			compiler.gitRoot = root
			compiler.SetActionMode(ActionModeDev)
			data, err := compiler.ParseWorkflowFile(path)
			require.NoError(t, err)
			assertConnectorScenarioAuthority(t, data, scenario)
			connector := data.Connectors[provider]
			encoded, err := connector.Agent.Encode()
			require.NoError(t, err)
			checkMinimalConnectorAdvertisement(t, encoded, scenario.readers)
			output, err := compiler.CompileToYAML(data, path)
			require.NoError(t, err)
			require.Contains(t, output, "workflow_dispatch:")
			require.Contains(t, output, "connector_runtime.cjs")
			require.NotContains(t, output, "repository_dispatch:")
			assertConnectorScenarioEmission(t, compiler, data, scenario, output)
		})
	}
}

func validateGoldenWorkflowSource(t *testing.T, provider string, frontmatter map[string]any) {
	t.Helper()
	require.NotEmpty(t, frontmatter["name"], "golden workflows name their concrete scenario")
	switch on := frontmatter["on"].(type) {
	case string:
		require.Equal(t, "workflow_dispatch", on)
	case map[string]any:
		require.Len(t, on, 1)
		require.Contains(t, on, "workflow_dispatch")
	default:
		t.Fatal("golden workflows must be manually triggered")
	}
	tools, ok := frontmatter["tools"].(map[string]any)
	require.True(t, ok)
	require.Contains(t, tools, provider)
	for _, control := range []string{"bash", "edit", "cli-proxy"} {
		require.Equal(t, false, tools[control])
	}
	if provider != "confluence" {
		require.Equal(t, false, tools["github"], "unselected GitHub tools must remain disabled")
	}
	for name, value := range tools {
		if name != provider {
			if name == "github" && value != false {
				github, ok := value.(map[string]any)
				require.True(t, ok)
				require.Equal(t, []any{"issues"}, github["toolsets"])
				require.Equal(t, []any{"search_issues", "issue_read"}, github["allowed"])
				continue
			}
			require.Equal(t, false, value, "other tool entries may only disable built-ins")
		}
	}
	outputs, ok := frontmatter["safe-outputs"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, true, outputs["staged"], "examples must not perform live writes by default")
	effects := 0
	for name, value := range outputs {
		switch name {
		case "noop", "report-missing-tool", "report-missing-data", "report-incomplete", "threat-detection":
			continue
		}
		if _, known := getSafeOutputHandlerByKey(name); known || strings.HasPrefix(name, provider+"-") {
			effects++
			config, ok := value.(map[string]any)
			require.True(t, ok)
			require.EqualValues(t, 1, config["max"], "golden effects are capped at one proposal")
		}
	}
	require.Equal(t, 1, effects, "select one effect, excluding publication policy fields")
	require.NotContains(t, frontmatter, "steps")
	require.NotContains(t, frontmatter, "jobs")
}

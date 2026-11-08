//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMissingReportFrontmatterKeys(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"tool", "data"} {
		for _, key := range []string{"missing-" + kind, "report-missing-" + kind} {
			for _, value := range []any{nil, false, map[string]any{"max": 3, "title-prefix": "[custom]", "create-issue": true}} {
				t.Run(fmt.Sprintf("%s/%v", key, value), func(t *testing.T) {
					frontmatter := map[string]any{"on": "workflow_dispatch", "safe-outputs": map[string]any{key: value}}
					require.NoError(t, parser.ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "test.md"))
					config := NewCompiler().extractSafeOutputsConfig(frontmatter)
					require.NotNil(t, config)
					actual := config.MissingTool
					if kind == "data" {
						actual = config.MissingData
					}
					if value == false {
						assert.Nil(t, actual, "explicit false must not be auto-defaulted")
						return
					}
					require.NotNil(t, actual)
					if value != nil {
						require.NotNil(t, actual.Max)
						assert.Equal(t, "3", *actual.Max)
						assert.Equal(t, "[custom]", actual.TitlePrefix)
						require.NotNil(t, actual.CreateIssue)
						assert.Equal(t, "true", *actual.CreateIssue)
					}
				})
			}
		}
	}
}

func TestMissingReportCanonicalKeyPrecedence(t *testing.T) {
	t.Parallel()
	config := NewCompiler().extractSafeOutputsConfig(map[string]any{
		"safe-outputs": map[string]any{
			"missing-tool":        map[string]any{"max": 9},
			"report-missing-tool": false,
			"missing-data":        false,
			"report-missing-data": map[string]any{"max": 2},
		},
	})
	assert.Nil(t, config.MissingTool)
	require.NotNil(t, config.MissingData)
	require.NotNil(t, config.MissingData.Max)
	assert.Equal(t, "2", *config.MissingData.Max)
}

func TestMissingReportImportAliases(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"tool", "data"} {
		for _, topKey := range []string{"missing-" + kind, "report-missing-" + kind} {
			for _, importKey := range []string{"missing-" + kind, "report-missing-" + kind} {
				t.Run(topKey+"/"+importKey, func(t *testing.T) {
					compiler := NewCompiler()
					topRaw := map[string]any{topKey: false}
					top := compiler.extractSafeOutputsConfig(map[string]any{"safe-outputs": topRaw})
					imported := fmt.Sprintf(`{%q:{"max":7}}`, importKey)
					result, err := compiler.MergeSafeOutputs(top, []string{imported}, topRaw)
					require.NoError(t, err)
					actual := result.MissingTool
					if kind == "data" {
						actual = result.MissingData
					}
					assert.Nil(t, actual, "explicit top-level disable must override either imported spelling")
					_, err = compiler.MergeSafeOutputs(nil, []string{imported, fmt.Sprintf(`{%q:{"max":2}}`, topKey)}, nil)
					require.ErrorContains(t, err, "conflict")
					result, err = compiler.MergeSafeOutputs(nil, []string{imported}, nil)
					require.NoError(t, err)
					actual = result.MissingTool
					if kind == "data" {
						actual = result.MissingData
					}
					require.NotNil(t, actual)
					require.NotNil(t, actual.Max)
					assert.Equal(t, "7", *actual.Max)
				})
			}
		}
	}
}

func TestMissingReportCanonicalCompile(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), "reports.md")
	require.NoError(t, os.WriteFile(file, []byte(`---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
safe-outputs:
  report-missing-tool:
    max: 2
  report-missing-data:
    max: 3
---
Report unavailable capabilities and data.
`), 0600))
	require.NoError(t, NewCompiler().CompileWorkflow(file))
	content, err := os.ReadFile(filepath.Join(filepath.Dir(file), "reports.lock.yml"))
	require.NoError(t, err)
	assert.Contains(t, string(content), `"report_missing_tool"`)
	assert.Contains(t, string(content), `"report_missing_data"`)
	serialized, err := yaml.Marshal(&SafeOutputsConfig{MissingTool: &MissingToolConfig{}, MissingData: &MissingDataConfig{}})
	require.NoError(t, err)
	assert.Contains(t, string(serialized), "report-missing-tool:")
	assert.Contains(t, string(serialized), "report-missing-data:")
}

func TestMissingReportCanonicalEmptyStrictCompile(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), "reports.md")
	require.NoError(t, os.WriteFile(file, []byte(`---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
network: {}
strict: true
safe-outputs:
  report-missing-tool:
---
Report unavailable capabilities.
`), 0600))
	require.NoError(t, NewCompiler().CompileWorkflow(file))
	content, err := os.ReadFile(filepath.Join(filepath.Dir(file), "reports.lock.yml"))
	require.NoError(t, err)
	assert.Contains(t, string(content), `"report_missing_tool"`)
}

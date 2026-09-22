//go:build !integration

package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLinkSubIssueHandlerConfigIncludesTarget(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			LinkSubIssue: &LinkSubIssueConfig{
				BaseSafeOutputConfig:   BaseSafeOutputConfig{Max: strPtr("5")},
				SafeOutputTargetConfig: SafeOutputTargetConfig{Target: "123"},
			},
		},
	}

	var steps []string
	compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)

	for _, step := range steps {
		if !strings.Contains(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
			continue
		}
		parts := strings.Split(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
		require.Len(t, parts, 2)
		jsonStr := strings.Trim(strings.TrimSpace(parts[1]), "\"")
		jsonStr = strings.ReplaceAll(jsonStr, "\\\"", "\"")

		var config map[string]map[string]any
		require.NoError(t, json.Unmarshal([]byte(jsonStr), &config))
		assert.Equal(t, "123", config["link_sub_issue"]["target"])
		return
	}

	t.Fatal("GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG was not generated")
}

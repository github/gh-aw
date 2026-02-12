//go:build !integration

package workflow

import (
"encoding/json"
"strings"
"testing"

"github.com/stretchr/testify/assert"
"github.com/stretchr/testify/require"
)

func TestFooterConfiguration(t *testing.T) {
compiler := NewCompiler()
frontmatter := map[string]any{
"name": "Test",
"safe-outputs": map[string]any{
"create-issue": map[string]any{"footer": false},
},
}
config := compiler.extractSafeOutputsConfig(frontmatter)
require.NotNil(t, config)
require.NotNil(t, config.CreateIssues)
require.NotNil(t, config.CreateIssues.Footer)
assert.False(t, *config.CreateIssues.Footer)
}

func TestFooterInHandlerConfig(t *testing.T) {
compiler := NewCompiler()
workflowData := &WorkflowData{
Name: "Test",
SafeOutputs: &SafeOutputsConfig{
CreateIssues: &CreateIssuesConfig{
BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
Footer:               boolPtr(false),
},
},
}
var steps []string
compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)
stepsContent := strings.Join(steps, "")
require.Contains(t, stepsContent, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG")
for _, step := range steps {
if strings.Contains(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
parts := strings.Split(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
if len(parts) == 2 {
jsonStr := strings.TrimSpace(parts[1])
jsonStr = strings.Trim(jsonStr, "\"")
jsonStr = strings.ReplaceAll(jsonStr, "\\\"", "\"")
var config map[string]any
err := json.Unmarshal([]byte(jsonStr), &config)
require.NoError(t, err)
issueConfig, ok := config["create_issue"].(map[string]any)
require.True(t, ok)
assert.Equal(t, false, issueConfig["footer"])
}
}
}
}

//go:build !integration

package workflow

import (
"os"
"path/filepath"
"strings"
"testing"

"github.com/github/gh-aw/pkg/stringutil"
"github.com/github/gh-aw/pkg/testutil"
"github.com/stretchr/testify/assert"
"github.com/stretchr/testify/require"
)

// TestSafeOutputsJobConditionWithDetection verifies that when detection is enabled,
// the safe_outputs job checks both agent.result == 'success' and detection.result == 'success'
func TestSafeOutputsJobConditionWithDetection(t *testing.T) {
tmpDir := testutil.TempDir(t, "test-*")
workflowPath := filepath.Join(tmpDir, "test-workflow.md")

frontmatter := `---
on: workflow_dispatch
permissions:
  contents: read
engine: claude
safe-outputs:
  create-issue:
---

# Test

Create an issue.
`

require.NoError(t, os.WriteFile(workflowPath, []byte(frontmatter), 0644))

compiler := NewCompiler()
require.NoError(t, compiler.CompileWorkflow(workflowPath))

// Read the compiled YAML
lockPath := stringutil.MarkdownToLockFile(workflowPath)
yamlBytes, err := os.ReadFile(lockPath)
require.NoError(t, err)
yaml := string(yamlBytes)

// Find the safe_outputs job
assert.Contains(t, yaml, "safe_outputs:", "safe_outputs job should exist")

// Extract the safe_outputs job section
lines := strings.Split(yaml, "\n")
var safeOutputsSection []string
inSafeOutputs := false
for _, line := range lines {
if strings.Contains(line, "safe_outputs:") {
inSafeOutputs = true
}
if inSafeOutputs {
safeOutputsSection = append(safeOutputsSection, line)
// Stop at the next job
if strings.HasPrefix(line, "  ") && strings.HasSuffix(strings.TrimSpace(line), ":") && 
   !strings.Contains(line, "needs:") && 
   !strings.Contains(line, "permissions:") &&
   !strings.Contains(line, "env:") &&
   len(safeOutputsSection) > 10 {
break
}
}
}

safeOutputsYaml := strings.Join(safeOutputsSection, "\n")

// Verify the condition includes all three checks
assert.Contains(t, safeOutputsYaml, "needs.agent.result == 'success'", 
"safe_outputs condition should check agent.result == 'success'")
assert.Contains(t, safeOutputsYaml, "needs.detection.result == 'success'", 
"safe_outputs condition should check detection.result == 'success'")
assert.Contains(t, safeOutputsYaml, "needs.detection.outputs.success == 'true'", 
"safe_outputs condition should check detection.outputs.success == 'true'")

// Verify the job depends on both agent and detection
assert.Contains(t, safeOutputsYaml, "- agent", "safe_outputs should depend on agent")
assert.Contains(t, safeOutputsYaml, "- detection", "safe_outputs should depend on detection")
}

// TestSafeOutputsJobConditionWithoutDetection verifies that when detection is disabled,
// the safe_outputs job only checks agent.result == 'success'
func TestSafeOutputsJobConditionWithoutDetection(t *testing.T) {
tmpDir := testutil.TempDir(t, "test-*")
workflowPath := filepath.Join(tmpDir, "test-workflow.md")

frontmatter := `---
on: workflow_dispatch
permissions:
  contents: read
engine: claude
safe-outputs:
  threat-detection: false
  create-issue:
---

# Test

Create an issue.
`

require.NoError(t, os.WriteFile(workflowPath, []byte(frontmatter), 0644))

compiler := NewCompiler()
require.NoError(t, compiler.CompileWorkflow(workflowPath))

// Read the compiled YAML
lockPath := stringutil.MarkdownToLockFile(workflowPath)
yamlBytes, err := os.ReadFile(lockPath)
require.NoError(t, err)
yaml := string(yamlBytes)

// Find the safe_outputs job
assert.Contains(t, yaml, "safe_outputs:", "safe_outputs job should exist")

// Verify detection job does not exist
assert.NotContains(t, yaml, "detection:", "detection job should not exist when disabled")

// Extract the safe_outputs job section
lines := strings.Split(yaml, "\n")
var safeOutputsSection []string
inSafeOutputs := false
for _, line := range lines {
if strings.Contains(line, "safe_outputs:") {
inSafeOutputs = true
}
if inSafeOutputs {
safeOutputsSection = append(safeOutputsSection, line)
// Stop at the next job
if strings.HasPrefix(line, "  ") && strings.HasSuffix(strings.TrimSpace(line), ":") && 
   !strings.Contains(line, "needs:") && 
   !strings.Contains(line, "permissions:") &&
   !strings.Contains(line, "env:") &&
   len(safeOutputsSection) > 10 {
break
}
}
}

safeOutputsYaml := strings.Join(safeOutputsSection, "\n")

// Verify the condition checks agent success
assert.Contains(t, safeOutputsYaml, "needs.agent.result == 'success'", 
"safe_outputs condition should check agent.result == 'success'")

// Verify detection checks are not present
assert.NotContains(t, safeOutputsYaml, "needs.detection.result", 
"safe_outputs condition should not check detection.result when detection is disabled")
assert.NotContains(t, safeOutputsYaml, "needs.detection.outputs", 
"safe_outputs condition should not check detection.outputs when detection is disabled")

// Verify the job only depends on agent (can be "needs: agent" or "needs:\n      - agent")
assert.True(t, strings.Contains(safeOutputsYaml, "needs: agent") || strings.Contains(safeOutputsYaml, "- agent"), 
"safe_outputs should depend on agent")
assert.NotContains(t, safeOutputsYaml, "detection", "safe_outputs should not depend on detection when disabled")
}

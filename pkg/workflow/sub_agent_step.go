package workflow

import (
	"fmt"
	"path"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var subAgentStepLog = logger.New("workflow:sub_agent_step")

// generateRestoreInlineSubAgentsStep emits a step that copies inline sub-agent files
// from the activation artifact into the workspace after the base-branch restore.
//
// Sub-agent files are written by interpolate_prompt.cjs into /tmp/gh-aw/<engineDir>/
// during the activation job and uploaded as part of the activation artifact.
// This step restores them so the engine CLI can discover them.
//
// Inline sub-agents are enabled by default. The shell logic lives in
// restore_inline_sub_agents.sh for maintainability and testability.
func generateRestoreInlineSubAgentsStep(yaml *strings.Builder, data *WorkflowData, registry *EngineRegistry) {
	engineID := ""
	if data.EngineConfig != nil {
		engineID = data.EngineConfig.ID
	}
	subAgentDir := path.Join(engineConfigBaseDirForRegistry(registry, engineID), "agents")
	subAgentExt := parser.GetEngineSubAgentExt(engineID)
	subAgentStepLog.Printf("Generating restore inline sub-agents step: engine=%s, dir=%s, ext=%s", engineID, subAgentDir, subAgentExt)

	yaml.WriteString("      - name: Restore inline sub-agents from activation artifact\n")
	yaml.WriteString("        env:\n")
	fmt.Fprintf(yaml, "          GH_AW_SUB_AGENT_DIR: \"%s\"\n", subAgentDir)
	fmt.Fprintf(yaml, "          GH_AW_SUB_AGENT_EXT: \"%s\"\n", subAgentExt)
	yaml.WriteString("        run: bash \"${RUNNER_TEMP}/gh-aw/actions/restore_inline_sub_agents.sh\"\n")
}

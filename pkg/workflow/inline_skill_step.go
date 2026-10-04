package workflow

import (
	"fmt"
	"path"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var inlineSkillStepLog = logger.New("workflow:inline_skill_step")

func generateRestoreInlineSkillsStep(yaml *strings.Builder, data *WorkflowData, registry *EngineRegistry) {
	engineID := ""
	if data.EngineConfig != nil {
		engineID = data.EngineConfig.ID
	}
	skillDir := path.Join(engineConfigBaseDirForRegistry(registry, engineID), "skills")
	inlineSkillStepLog.Printf("Generating restore inline skills step: engine=%s, dir=%s", engineID, skillDir)

	yaml.WriteString("      - name: Restore inline skills from activation artifact\n")
	yaml.WriteString("        env:\n")
	fmt.Fprintf(yaml, "          GH_AW_SKILL_DIR: \"%s\"\n", skillDir)
	yaml.WriteString("        run: bash \"${RUNNER_TEMP}/gh-aw/actions/restore_inline_skills.sh\"\n")
}

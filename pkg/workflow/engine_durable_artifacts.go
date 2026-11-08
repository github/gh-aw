package workflow

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func (c *Compiler) generateDurableArtifactRestore(yaml *strings.Builder, data *WorkflowData) {
	if data.EngineConfig == nil || data.EngineConfig.Durable == nil {
		return
	}
	files, err := json.Marshal(data.durableExecutionPlan.Files)
	if err != nil {
		panic(fmt.Sprintf("BUG: cannot encode durable files: %v", err))
	}
	yaml.WriteString("      - name: Restore durable workflow state\n")
	yaml.WriteString("        id: durable_checkpoint\n")
	yaml.WriteString("        uses: " + c.getActionPin("actions/github-script") + "\n")
	yaml.WriteString("        env:\n")
	yaml.WriteString("          GH_AW_DURABLE_WORKFLOW: " + strconv.Quote(durableWorkflowID(data)) + "\n")
	yaml.WriteString("          GH_AW_DURABLE_FILES: " + strconv.Quote(string(files)) + "\n")
	yaml.WriteString("          GH_AW_DURABLE_ENGINE: " + strconv.Quote(data.EngineConfig.ID) + "\n")
	yaml.WriteString("          GH_AW_DURABLE_TOKEN: ${{ github.token }}\n")
	yaml.WriteString("        with:\n")
	yaml.WriteString("          script: |\n")
	yaml.WriteString("            const { restoreCheckpoint } = require('${{ runner.temp }}/gh-aw/actions/durable_artifact.cjs');\n")
	yaml.WriteString("            await restoreCheckpoint({ github, context, core, token: process.env.GH_AW_DURABLE_TOKEN });\n")
}

func (c *Compiler) generateDurableFileStep(yaml *strings.Builder, data *WorkflowData, name, id, condition, operation string) {
	if data.durableExecutionPlan == nil {
		return
	}
	files, err := json.Marshal(data.durableExecutionPlan.Files)
	if err != nil {
		panic(fmt.Sprintf("BUG: cannot encode durable files: %v", err))
	}
	yaml.WriteString("      - name: " + name + "\n        id: " + id + "\n        if: " + condition + "\n")
	yaml.WriteString("        uses: " + c.getActionPin("actions/github-script") + "\n")
	yaml.WriteString("        env:\n")
	yaml.WriteString("          GH_AW_DURABLE_FILES: " + strconv.Quote(string(files)) + "\n")
	yaml.WriteString("          GH_AW_DURABLE_ENGINE: " + strconv.Quote(data.EngineConfig.ID) + "\n")
	yaml.WriteString("        with:\n          script: |\n")
	yaml.WriteString("            const { " + operation + " } = require('${{ runner.temp }}/gh-aw/actions/durable_files.cjs');\n")
	yaml.WriteString("            " + operation + "({ core });\n")
}

func (c *Compiler) generateDurableFileStage(yaml *strings.Builder, data *WorkflowData) {
	condition := "always() && steps.durable_checkpoint.outcome == 'success' && steps.agentic_execution.outcome != 'skipped'"
	if data.EngineConfig != nil && data.EngineConfig.ID == "copilot" {
		condition += " && steps.capture_durable_sessions.outcome == 'success'"
	}
	c.generateDurableFileStep(yaml, data, "Stage durable session files", "stage_durable_files",
		condition, "stageFiles")
}

func (c *Compiler) generateDurableArtifactUpload(yaml *strings.Builder, data *WorkflowData) {
	if data.EngineConfig == nil || data.EngineConfig.Durable == nil {
		return
	}
	bundle := durableStateDirectory + "/bundle/"
	c.stepOrderTracker.RecordArtifactUpload("Upload durable checkpoint artifact", []string{bundle})
	condition := "always() && steps.stage_durable_files.outcome == 'success' && steps.stage_durable_files.outputs.present == 'true' && " + secretRedactionSuccessCondition(data)
	yaml.WriteString("      - name: Validate durable checkpoint artifact\n")
	yaml.WriteString("        id: validate_durable_checkpoint\n")
	yaml.WriteString("        if: " + condition + "\n")
	yaml.WriteString("        uses: " + c.getActionPin("actions/github-script") + "\n")
	yaml.WriteString("        env:\n")
	yaml.WriteString("          GH_AW_DURABLE_ENGINE: " + strconv.Quote(data.EngineConfig.ID) + "\n")
	files, err := json.Marshal(data.durableExecutionPlan.Files)
	if err != nil {
		panic(fmt.Sprintf("BUG: cannot encode durable files: %v", err))
	}
	yaml.WriteString("          GH_AW_DURABLE_FILES: " + strconv.Quote(string(files)) + "\n")
	yaml.WriteString("        with:\n")
	yaml.WriteString("          script: |\n")
	yaml.WriteString("            const { validateFiles } = require('${{ runner.temp }}/gh-aw/actions/durable_files.cjs');\n")
	yaml.WriteString("            validateFiles({ core });\n")
	yaml.WriteString("      - name: Upload durable checkpoint artifact\n")
	yaml.WriteString("        id: upload_durable_checkpoint\n")
	yaml.WriteString("        if: always() && steps.validate_durable_checkpoint.outputs.present == 'true' && steps.validate_durable_checkpoint.outcome == 'success'\n")
	yaml.WriteString("        uses: " + c.getActionPin("actions/upload-artifact") + "\n")
	yaml.WriteString("        with:\n")
	yaml.WriteString("          name: ${{ steps.durable_checkpoint.outputs.artifact-name }}\n")
	yaml.WriteString("          path: " + bundle + "\n")
	yaml.WriteString("          retention-days: " + strconv.Itoa(data.EngineConfig.Durable.RetentionDays) + "\n")
	yaml.WriteString("          if-no-files-found: error\n")
}

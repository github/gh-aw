package workflow

import (
	"fmt"
	"path"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var compilerYamlArtifactsLog = logger.New("workflow:compiler_yaml_artifacts")

// generateExtractAccessLogs is a legacy method that no longer does anything
// Network filtering is now handled at the workflow level
func (c *Compiler) generateExtractAccessLogs(yaml *strings.Builder, tools map[string]any) {
	// No proxy tools anymore - network filtering is handled at workflow level
}

// generateUploadAccessLogs is a legacy method that no longer does anything
// Network filtering is now handled at the workflow level
func (c *Compiler) generateUploadAccessLogs(yaml *strings.Builder, tools map[string]any) {
	// No proxy tools anymore - network filtering is handled at workflow level
}

// generateUnifiedArtifactUpload generates a single step that uploads all agent job artifacts
// This consolidates multiple individual upload steps into one, improving workflow readability
// and reliability. The step always runs (even on cancellation) and ignores missing files.
// Workflows with custom safe-job artifacts additionally require successful secret redaction.
// prefix is prepended to the artifact name to avoid clashes in workflow_call context.
func (c *Compiler) generateUnifiedArtifactUpload(yaml *strings.Builder, paths []string, prefix string, requireSuccessfulRedaction bool) {
	if len(paths) == 0 {
		compilerYamlArtifactsLog.Print("No paths to upload, skipping unified artifact upload")
		return
	}

	compilerYamlArtifactsLog.Printf("Generating unified artifact upload with %d paths", len(paths))

	artifactName := prefix + "agent"

	// Record the unified upload so the step-order validator can verify it comes after
	// secret redaction, covering all collected paths in a single check.
	c.stepOrderTracker.RecordArtifactUpload("Upload agent artifacts", paths)

	yaml.WriteString("      - name: Upload agent artifacts\n")
	if requireSuccessfulRedaction {
		yaml.WriteString("        if: always() && steps.redact_secrets.outcome == 'success'\n")
	} else {
		yaml.WriteString("        if: always()\n")
	}
	yaml.WriteString("        continue-on-error: true\n")
	fmt.Fprintf(yaml, "        uses: %s\n", c.getActionPin("actions/upload-artifact"))
	yaml.WriteString("        with:\n")
	fmt.Fprintf(yaml, "          name: %s\n", artifactName)

	// Write paths as multi-line YAML string
	yaml.WriteString("          path: |\n")
	for _, path := range paths {
		fmt.Fprintf(yaml, "            %s\n", path)
	}

	yaml.WriteString("          if-no-files-found: ignore\n")

	compilerYamlArtifactsLog.Printf("Generated unified artifact upload step with %d paths", len(paths))
}

func (c *Compiler) generateTrackedSecretRedactionStep(yaml *strings.Builder, yamlContent string, data *WorkflowData) {
	var step strings.Builder
	c.generateSecretRedactionStep(&step, yamlContent, data)
	yaml.WriteString(strings.Replace(step.String(),
		"      - name: Redact secrets in logs\n",
		"      - name: Redact secrets in logs\n        id: redact_secrets\n",
		1))
}

// generateAgentOutputFallbackUpload generates a small, dedicated artifact upload containing
// processed agent output and accounting evidence. These files are also part of the unified
// "agent" artifact, but that artifact is large and its upload is best-effort
// (continue-on-error). When the upload times out, downstream jobs cannot download the agent
// artifact and every safe output is silently dropped. Uploading the few-KB payload separately
// gives those jobs a reliable fallback source.
// prefix is prepended to the artifact name to avoid clashes in workflow_call context.
func (c *Compiler) generateAgentOutputFallbackUpload(yaml *strings.Builder, data *WorkflowData, prefix string) {
	if data.SafeOutputs == nil || data.SafeOutputs.AutoInjectedCreateIssue {
		return
	}

	paths := []string{
		constants.TmpGhAwDirSlash + constants.AgentOutputFilename.String(),
		constants.TmpGhAwDirSlash + constants.SafeOutputsFilename.String(),
		agentExecutionEvidencePath,
		constants.TmpGhAwDirSlash + "agent_usage.jsonl",
		constants.TmpGhAwDirSlash + "agent_usage.json",
		constants.TmpGhAwDirSlash + "sandbox/firewall-audit-logs/api-proxy-logs/token-usage.jsonl",
		path.Join(constants.AWFProxyLogsDir.String(), "api-proxy-logs/token-usage.jsonl"),
		path.Join(constants.AWFAuditDir.String(), "api-proxy-logs/token-usage.jsonl"),
	}

	// Include grader manifest/results in the fallback so detection and downstream
	// jobs have reliable access even when the large unified artifact times out.
	if data.Graders != nil && data.Graders.HasGraders() {
		paths = append(paths, collectGraderArtifactPaths(data.Graders)...)
	}

	if isArcDindTopology(data) {
		paths = rewriteTmpGhAwPathsForArcDind(paths)
	}

	c.stepOrderTracker.RecordArtifactUpload("Upload agent output fallback artifact", paths)

	yaml.WriteString("      # Small dedicated copy of the agent output so safe-output processing\n")
	yaml.WriteString("      # survives a failed or timed-out upload of the larger agent artifact\n")
	yaml.WriteString("      - name: Upload agent output fallback artifact\n")
	yaml.WriteString("        if: always()\n")
	yaml.WriteString("        continue-on-error: true\n")
	fmt.Fprintf(yaml, "        uses: %s\n", c.getActionPin("actions/upload-artifact"))
	yaml.WriteString("        with:\n")
	fmt.Fprintf(yaml, "          name: %s%s\n", prefix, constants.AgentOutputFallbackArtifactName)
	yaml.WriteString("          path: |\n")
	for _, path := range paths {
		fmt.Fprintf(yaml, "            %s\n", path)
	}
	yaml.WriteString("          if-no-files-found: ignore\n")

	compilerYamlArtifactsLog.Print("Generated agent output fallback artifact upload step")
}

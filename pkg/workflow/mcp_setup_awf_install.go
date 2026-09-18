package workflow

import "strings"

func generateAgenticWorkflowsInstallStep(c *Compiler, yaml *strings.Builder, hasAgenticWorkflows bool, workflowData *WorkflowData) {
	if !hasAgenticWorkflows {
		return
	}

	cliVersion := resolveAgenticWorkflowsCLIVersion(c, workflowData)
	resolvedToken := resolveGitHubToken("")
	actionRepo := GitHubActionsOrgRepo + "/setup-cli"
	installStep, err := generateGhAwSetupStep(ghAwSetupStepConfig{
		actionMode:           c.actionMode,
		cliVersion:           cliVersion,
		actionRepo:           actionRepo,
		fallbackActionRefTag: cliVersion,
		workflowData:         workflowData,
		withFields: map[string]string{
			"github-token": resolvedToken,
		},
	})
	if err != nil {
		mcpSetupGeneratorLog.Printf("Failed to resolve pinned setup-cli action reference for %s@%s: %v", actionRepo, cliVersion, err)
	}
	for _, line := range installStep {
		yaml.WriteString(line + "\n")
	}
	yaml.WriteString("      - name: Copy gh-aw binary for MCP Server\n")
	yaml.WriteString("        run: |\n")
	yaml.WriteString("          bash \"${RUNNER_TEMP}/gh-aw/actions/copy_gh_aw_binary_for_mcp.sh\"\n")
}

func resolveAgenticWorkflowsCLIVersion(c *Compiler, workflowData *WorkflowData) string {
	cliVersion := c.actionTag
	if cliVersion == "" {
		cliVersion = getActionTagFromFeatures(workflowData)
	}
	if cliVersion == "" {
		cliVersion = c.version
	}
	// "dev" and empty versions are not valid release pins; fall back to the
	// current compiler runtime version so setup-cli always receives a concrete
	// pinned release tag in non-dev modes.
	if cliVersion == "" || cliVersion == "dev" {
		cliVersion = getDefaultGhAWRuntimeVersion()
	}
	return cliVersion
}

func getActionTagFromFeatures(workflowData *WorkflowData) string {
	if workflowData == nil || workflowData.Features == nil {
		return ""
	}
	actionTagVal, exists := workflowData.Features["action-tag"]
	if !exists {
		return ""
	}
	actionTagStr, ok := actionTagVal.(string)
	if !ok || actionTagStr == "" {
		return ""
	}
	return actionTagStr
}

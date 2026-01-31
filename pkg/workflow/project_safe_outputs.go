package workflow

import (
	"github.com/githubnext/gh-aw/pkg/logger"
)

var projectSafeOutputsLog = logger.New("workflow:project_safe_outputs")

// applyProjectSafeOutputs checks for a project field in the frontmatter.
// Previously auto-configured update-project and create-project-status-update safe-outputs,
// but these have been removed. This function now does nothing and is kept for backward
// compatibility.
func (c *Compiler) applyProjectSafeOutputs(frontmatter map[string]any, existingSafeOutputs *SafeOutputsConfig) *SafeOutputsConfig {
	projectSafeOutputsLog.Print("Checking for project field in frontmatter")

	// Check if project field exists
	projectData, hasProject := frontmatter["project"]
	if !hasProject || projectData == nil {
		projectSafeOutputsLog.Print("No project field found in frontmatter")
		return existingSafeOutputs
	}

	projectSafeOutputsLog.Print("Project field found, but project safe-outputs have been removed")
	return existingSafeOutputs
}

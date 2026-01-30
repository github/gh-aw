package workflow

import (
	"strings"

	"github.com/githubnext/gh-aw/pkg/logger"
)

var projectSafeOutputsLog = logger.New("workflow:project_safe_outputs")

// applyProjectSafeOutputs checks for a project field in the frontmatter and automatically
// configures safe-outputs for project tracking when present. This provides the same
// project tracking behavior that campaign orchestrators have.
//
// When a project field is detected:
// - Automatically adds update-project safe-output if not already configured
// - Automatically adds create-project-status-update safe-output if not already configured
// - Applies project-specific settings (max-updates, github-token, etc.)
func (c *Compiler) applyProjectSafeOutputs(frontmatter map[string]any, existingSafeOutputs *SafeOutputsConfig) *SafeOutputsConfig {
	projectSafeOutputsLog.Print("Checking for project field in frontmatter")

	// Check if project field exists
	projectData, hasProject := frontmatter["project"]
	if !hasProject || projectData == nil {
		projectSafeOutputsLog.Print("No project field found in frontmatter")
		return existingSafeOutputs
	}

	projectSafeOutputsLog.Print("Project field found")

	projectURL, ok := projectData.(string)
	if !ok {
		// NOTE: Only string project URLs are supported.
		projectSafeOutputsLog.Print("Invalid project field format (expected string), skipping")
		return existingSafeOutputs
	}
	projectURL = strings.TrimSpace(projectURL)
	if projectURL == "" {
		projectSafeOutputsLog.Print("Empty project URL, skipping")
		return existingSafeOutputs
	}

	projectSafeOutputsLog.Printf("Project URL configured: %s", projectURL)

	// Create or update SafeOutputsConfig
	safeOutputs := existingSafeOutputs
	if safeOutputs == nil {
		safeOutputs = &SafeOutputsConfig{}
		projectSafeOutputsLog.Print("Created new SafeOutputsConfig for project tracking")
	}

	// Defaults match campaign orchestrator behavior.
	maxUpdates := 100
	maxStatusUpdates := 1

	// Configure update-project if not already configured
	if safeOutputs.UpdateProjects == nil {
		projectSafeOutputsLog.Printf("Adding update-project safe-output (max: %d)", maxUpdates)
		safeOutputs.UpdateProjects = &UpdateProjectConfig{
			BaseSafeOutputConfig: BaseSafeOutputConfig{
				Max: maxUpdates,
			},
		}
	} else {
		projectSafeOutputsLog.Print("update-project already configured, preserving existing configuration")
	}

	// Configure create-project-status-update if not already configured
	if safeOutputs.CreateProjectStatusUpdates == nil {
		projectSafeOutputsLog.Printf("Adding create-project-status-update safe-output (max: %d)", maxStatusUpdates)
		safeOutputs.CreateProjectStatusUpdates = &CreateProjectStatusUpdateConfig{
			BaseSafeOutputConfig: BaseSafeOutputConfig{
				Max: maxStatusUpdates,
			},
		}
	} else {
		projectSafeOutputsLog.Print("create-project-status-update already configured, preserving existing configuration")
	}

	return safeOutputs
}

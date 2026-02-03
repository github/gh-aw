package parser

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/goccy/go-yaml"
)

var yamlImportLog = logger.New("parser:yaml_import")

// isYAMLWorkflowFile checks if a file path points to a GitHub Actions workflow YAML file
// Returns true for .yml and .yaml files, but false for .lock.yml files
func isYAMLWorkflowFile(filePath string) bool {
	// Normalize to lowercase for case-insensitive extension check
	lower := strings.ToLower(filePath)

	// Reject .lock.yml files (these are compiled outputs from gh-aw)
	if strings.HasSuffix(lower, ".lock.yml") {
		return false
	}

	// Accept .yml and .yaml files
	return strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".yaml")
}

// isActionDefinitionFile checks if a YAML file is a GitHub Action definition (action.yml)
// rather than a workflow file. Action definitions have different structure with 'runs' field.
func isActionDefinitionFile(filePath string, content []byte) (bool, error) {
	// Quick check: action.yml or action.yaml filename
	base := filepath.Base(filePath)
	if strings.ToLower(base) == "action.yml" || strings.ToLower(base) == "action.yaml" {
		return true, nil
	}

	// Parse YAML to check structure
	var doc map[string]any
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return false, fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Action definitions have 'runs' field, workflows have 'jobs' field
	_, hasRuns := doc["runs"]
	_, hasJobs := doc["jobs"]

	// If it has 'runs' but no 'jobs', it's likely an action definition
	if hasRuns && !hasJobs {
		return true, nil
	}

	return false, nil
}

// isCopilotSetupStepsFile checks if a file is the special copilot-setup-steps file
// This file receives special handling - only steps are extracted from the setup job
// Supports both .yml and .yaml extensions for consistency with GitHub Actions
func isCopilotSetupStepsFile(filePath string) bool {
	base := filepath.Base(filePath)
	lower := strings.ToLower(base)
	return lower == "copilot-setup-steps.yml" || lower == "copilot-setup-steps.yaml"
}

// processYAMLWorkflowImport processes an imported YAML workflow file
// Returns the extracted jobs in JSON format for merging
// Special case: For copilot-setup-steps.yml, returns steps in YAML format instead of jobs
func processYAMLWorkflowImport(filePath string) (jobs string, services string, err error) {
	yamlImportLog.Printf("Processing YAML workflow import: %s", filePath)

	// Read the YAML file
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", "", fmt.Errorf("failed to read YAML file: %w", err)
	}

	// Check if this is an action definition file (not a workflow)
	isAction, err := isActionDefinitionFile(filePath, content)
	if err != nil {
		return "", "", fmt.Errorf("failed to check if file is action definition: %w", err)
	}
	if isAction {
		return "", "", fmt.Errorf("cannot import action definition file (action.yml). Only workflow files (.yml) can be imported")
	}

	// Parse the YAML workflow
	var workflow map[string]any
	if err := yaml.Unmarshal(content, &workflow); err != nil {
		return "", "", fmt.Errorf("failed to parse YAML workflow: %w", err)
	}

	// Validate this is a GitHub Actions workflow (has 'on' or 'jobs' field)
	_, hasOn := workflow["on"]
	_, hasJobs := workflow["jobs"]
	if !hasOn && !hasJobs {
		return "", "", fmt.Errorf("not a valid GitHub Actions workflow: missing 'on' or 'jobs' field")
	}

	// Special handling for copilot-setup-steps.yml: extract only steps from the setup job
	if isCopilotSetupStepsFile(filePath) {
		yamlImportLog.Printf("Detected copilot-setup-steps.yml - extracting steps from setup job")
		stepsYAML, err := extractStepsFromCopilotSetup(workflow)
		if err != nil {
			return "", "", fmt.Errorf("failed to extract steps from copilot-setup-steps.yml: %w", err)
		}
		// Return steps as "jobs" string for compatibility with the import processor.
		// The import processor will route this to ImportsResult.CopilotSetupSteps.
		return stepsYAML, "", nil
	}

	// Extract jobs section
	var jobsJSON string
	if jobsValue, ok := workflow["jobs"]; ok {
		if jobsMap, ok := jobsValue.(map[string]any); ok {
			jobsBytes, err := json.Marshal(jobsMap)
			if err != nil {
				return "", "", fmt.Errorf("failed to marshal jobs to JSON: %w", err)
			}
			jobsJSON = string(jobsBytes)
			yamlImportLog.Printf("Extracted %d jobs from YAML workflow", len(jobsMap))
		}
	}

	// Extract services from job definitions
	var servicesJSON string
	if jobsValue, ok := workflow["jobs"]; ok {
		if jobsMap, ok := jobsValue.(map[string]any); ok {
			// Collect all services from all jobs
			allServices := make(map[string]any)
			for jobName, jobValue := range jobsMap {
				if jobMap, ok := jobValue.(map[string]any); ok {
					if servicesValue, ok := jobMap["services"]; ok {
						if servicesMap, ok := servicesValue.(map[string]any); ok {
							// Merge services from this job
							for serviceName, serviceConfig := range servicesMap {
								// Use job name as prefix to avoid conflicts
								prefixedName := fmt.Sprintf("%s_%s", jobName, serviceName)
								allServices[prefixedName] = serviceConfig
								yamlImportLog.Printf("Found service: %s in job %s (stored as %s)", serviceName, jobName, prefixedName)
							}
						}
					}
				}
			}

			if len(allServices) > 0 {
				// Marshal to JSON for merging
				servicesBytes, err := json.Marshal(allServices)
				if err != nil {
					yamlImportLog.Printf("Failed to marshal services to JSON: %v", err)
				} else {
					servicesJSON = string(servicesBytes)
					yamlImportLog.Printf("Extracted %d services from YAML workflow", len(allServices))
				}
			}
		}
	}

	return jobsJSON, servicesJSON, nil
}

// extractStepsFromCopilotSetup extracts steps from the copilot-setup-steps job
// Returns the steps in YAML array format for merging into the agent job
// Ensures a checkout step is always included at the beginning
func extractStepsFromCopilotSetup(workflow map[string]any) (string, error) {
	jobsValue, ok := workflow["jobs"]
	if !ok {
		return "", fmt.Errorf("no jobs found in copilot-setup-steps.yml")
	}

	jobsMap, ok := jobsValue.(map[string]any)
	if !ok {
		return "", fmt.Errorf("jobs field is not a map in copilot-setup-steps.yml")
	}

	// Look for the copilot-setup-steps job
	setupJob, ok := jobsMap["copilot-setup-steps"]
	if !ok {
		return "", fmt.Errorf("copilot-setup-steps job not found in copilot-setup-steps.yml")
	}

	setupJobMap, ok := setupJob.(map[string]any)
	if !ok {
		return "", fmt.Errorf("copilot-setup-steps job is not a map")
	}

	// Extract steps from the job
	stepsValue, ok := setupJobMap["steps"]
	if !ok {
		return "", fmt.Errorf("no steps found in copilot-setup-steps job")
	}

	// Verify steps is actually a list
	stepsSlice, ok := stepsValue.([]any)
	if !ok {
		return "", fmt.Errorf("steps field is not a list in copilot-setup-steps job")
	}

	// Ensure checkout step is always included and placed first
	stepsSlice = ensureCheckoutStepFirst(stepsSlice)

	// Marshal steps array directly to YAML format (without "steps:" wrapper)
	// This matches the format expected by the compiler which unmarshals into []any
	stepsYAML, err := yaml.Marshal(stepsSlice)
	if err != nil {
		return "", fmt.Errorf("failed to marshal steps to YAML: %w", err)
	}

	yamlImportLog.Printf("Extracted steps from copilot-setup-steps job (YAML array format) with checkout step ensured")
	return string(stepsYAML), nil
}

// ensureCheckoutStepFirst ensures a checkout step exists and is placed first in the steps list
// If a checkout step exists, it's moved to the beginning. If not, one is added.
func ensureCheckoutStepFirst(steps []any) []any {
	// Find existing checkout step index
	checkoutIndex := -1
	for i, step := range steps {
		if stepMap, ok := step.(map[string]any); ok {
			if uses, hasUses := stepMap["uses"]; hasUses {
				if usesStr, ok := uses.(string); ok {
					// Check if this is a checkout action (actions/checkout@... or exactly "actions/checkout")
					if strings.HasPrefix(usesStr, "actions/checkout@") || usesStr == "actions/checkout" {
						checkoutIndex = i
						break
					}
				}
			}
		}
	}

	// If checkout step exists and is already first, no changes needed
	if checkoutIndex == 0 {
		yamlImportLog.Print("Checkout step already at beginning of copilot-setup-steps")
		return steps
	}

	// If checkout step exists but not first, move it to the beginning
	if checkoutIndex > 0 {
		yamlImportLog.Printf("Moving existing checkout step from position %d to beginning", checkoutIndex)
		checkoutStep := steps[checkoutIndex]
		// Remove from current position
		steps = append(steps[:checkoutIndex], steps[checkoutIndex+1:]...)
		// Prepend to beginning
		steps = append([]any{checkoutStep}, steps...)
		return steps
	}

	// No checkout step found, add a default one at the beginning
	yamlImportLog.Print("No checkout step found in copilot-setup-steps, adding default checkout step at beginning")
	defaultCheckoutStep := map[string]any{
		"name": "Checkout code",
		"uses": "actions/checkout@v4",
	}
	steps = append([]any{defaultCheckoutStep}, steps...)
	return steps
}

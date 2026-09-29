package parser

import (
	"encoding/json"
	"errors"
	"fmt"
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
		yamlImportLog.Printf("Rejecting lock file: %s", filePath)
		return false
	}

	// Accept .yml and .yaml files
	isWorkflow := strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".yaml")
	yamlImportLog.Printf("File %s is workflow: %v", filePath, isWorkflow)
	return isWorkflow
}

// isActionDefinitionFile checks if a YAML file is a GitHub Action definition (action.yml)
// rather than a workflow file. Action definitions have different structure with 'runs' field.
func isActionDefinitionFile(filePath string, content []byte) (bool, error) {
	// Quick check: action.yml or action.yaml filename
	base := filepath.Base(filePath)
	if strings.EqualFold(base, "action.yml") || strings.EqualFold(base, "action.yaml") {
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
	return strings.EqualFold(base, "copilot-setup-steps.yml") || strings.EqualFold(base, "copilot-setup-steps.yaml")
}

func processYAMLWorkflowImportWithSourceVersions(filePath string) (jobs string, services string, sourceVersions map[string]string, err error) {
	yamlImportLog.Printf("Processing YAML workflow import: %s", filePath)

	content, err := readYAMLWorkflowImportFile(filePath)
	if err != nil {
		return "", "", nil, err
	}

	workflow, err := parseAndValidateYAMLWorkflowImport(filePath, content)
	if err != nil {
		return "", "", nil, err
	}
	sourceVersions = ActionPinSourceVersions(content)

	if isCopilotSetupStepsFile(filePath) {
		yamlImportLog.Printf("Detected copilot-setup-steps.yml - extracting steps from setup job")
		stepsYAML, err := extractStepsFromCopilotSetup(workflow)
		if err != nil {
			return "", "", nil, fmt.Errorf("failed to extract steps from copilot-setup-steps.yml: %w", err)
		}
		return stepsYAML, "", sourceVersions, nil
	}

	jobsJSON, jobsMap, err := extractJobsJSON(workflow)
	if err != nil {
		return "", "", nil, err
	}
	servicesJSON := extractServicesJSONFromJobs(jobsMap)
	return jobsJSON, servicesJSON, sourceVersions, nil
}

func readYAMLWorkflowImportFile(filePath string) ([]byte, error) {
	content, err := readFileFunc(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read YAML file: %w", err)
	}
	return content, nil
}

func parseAndValidateYAMLWorkflowImport(filePath string, content []byte) (map[string]any, error) {
	isAction, err := isActionDefinitionFile(filePath, content)
	if err != nil {
		yamlImportLog.Printf("Error checking if file is action definition: %v", err)
		return nil, fmt.Errorf("failed to check if file is action definition: %w", err)
	}
	if isAction {
		yamlImportLog.Printf("Rejecting action definition file: %s", filePath)
		return nil, errors.New("cannot import action definition file (action.yml). Only workflow files (.yml) can be imported")
	}

	var workflow map[string]any
	if err := yaml.Unmarshal(content, &workflow); err != nil {
		return nil, fmt.Errorf("failed to parse YAML workflow: %w", err)
	}
	if err := validateWorkflowShape(filePath, workflow); err != nil {
		return nil, err
	}
	return workflow, nil
}

func validateWorkflowShape(filePath string, workflow map[string]any) error {
	_, hasOn := workflow["on"]
	_, hasJobs := workflow["jobs"]
	if !hasOn && !hasJobs {
		yamlImportLog.Printf("Invalid workflow file %s: missing 'on' or 'jobs' field", filePath)
		return errors.New("not a valid GitHub Actions workflow: missing 'on' or 'jobs' field")
	}
	yamlImportLog.Printf("Validated workflow file %s: hasOn=%v, hasJobs=%v", filePath, hasOn, hasJobs)
	return nil
}

func extractJobsJSON(workflow map[string]any) (string, map[string]any, error) {
	jobsValue, ok := workflow["jobs"]
	if !ok {
		return "", nil, nil
	}
	jobsMap, ok := jobsValue.(map[string]any)
	if !ok {
		return "", nil, nil
	}
	jobsBytes, err := json.Marshal(jobsMap)
	if err != nil {
		return "", nil, fmt.Errorf("failed to marshal jobs to JSON: %w", err)
	}
	yamlImportLog.Printf("Extracted %d jobs from YAML workflow", len(jobsMap))
	return string(jobsBytes), jobsMap, nil
}

func extractServicesJSONFromJobs(jobsMap map[string]any) string {
	if len(jobsMap) == 0 {
		return ""
	}
	allServices := make(map[string]any)
	for jobName, jobValue := range jobsMap {
		jobMap, ok := jobValue.(map[string]any)
		if !ok {
			continue
		}
		servicesValue, ok := jobMap["services"]
		if !ok {
			continue
		}
		servicesMap, ok := servicesValue.(map[string]any)
		if !ok {
			continue
		}
		for serviceName, serviceConfig := range servicesMap {
			prefixedName := fmt.Sprintf("%s_%s", jobName, serviceName)
			allServices[prefixedName] = serviceConfig
			yamlImportLog.Printf("Found service: %s in job %s (stored as %s)", serviceName, jobName, prefixedName)
		}
	}
	if len(allServices) == 0 {
		return ""
	}
	servicesBytes, err := json.Marshal(allServices)
	if err != nil {
		yamlImportLog.Printf("Failed to marshal services to JSON: %v", err)
		return ""
	}
	yamlImportLog.Printf("Extracted %d services from YAML workflow", len(allServices))
	return string(servicesBytes)
}

// extractStepsFromCopilotSetup extracts steps from the copilot-setup-steps job
// Returns the steps in YAML array format for merging into the agent job
// Ensures a checkout step is always included at the beginning
func extractStepsFromCopilotSetup(workflow map[string]any) (string, error) {
	jobsValue, ok := workflow["jobs"]
	if !ok {
		return "", errors.New("no jobs found in copilot-setup-steps.yml")
	}

	jobsMap, ok := jobsValue.(map[string]any)
	if !ok {
		return "", errors.New("jobs field is not a map in copilot-setup-steps.yml")
	}

	// Look for the copilot-setup-steps job
	setupJob, ok := jobsMap["copilot-setup-steps"]
	if !ok {
		return "", errors.New("copilot-setup-steps job not found in copilot-setup-steps.yml")
	}

	setupJobMap, ok := setupJob.(map[string]any)
	if !ok {
		return "", errors.New("copilot-setup-steps job is not a map")
	}

	// Extract steps from the job
	stepsValue, ok := setupJobMap["steps"]
	if !ok {
		return "", errors.New("no steps found in copilot-setup-steps job")
	}

	// Verify steps is actually a list
	stepsSlice, ok := stepsValue.([]any)
	if !ok {
		return "", errors.New("steps field is not a list in copilot-setup-steps job")
	}

	// Strip checkout steps from the imported copilot-setup-steps. The compiler
	// generates its own secure checkout (with persist-credentials: false) via
	// CheckoutManager.GenerateDefaultCheckoutStep, so the imported checkout is
	// redundant and can introduce artipacked findings when the same job uploads
	// artifacts.
	stepsSlice = stripCheckoutSteps(stepsSlice)

	// Marshal steps array directly to YAML format (without "steps:" wrapper)
	// This matches the format expected by the compiler which unmarshals into []any
	stepsYAML, err := yaml.Marshal(stepsSlice)
	if err != nil {
		return "", fmt.Errorf("failed to marshal steps to YAML: %w", err)
	}

	yamlImportLog.Printf("Extracted steps from copilot-setup-steps job (YAML array format) with checkout steps stripped")
	return string(stepsYAML), nil
}

// stripCheckoutSteps removes any actions/checkout steps from the imported
// copilot-setup-steps. The compiler generates its own secure checkout step
// (with persist-credentials: false), so the imported checkout is redundant.
// Stripping it prevents the artipacked finding where checkout + artifact
// upload coexist with persisted credentials, and avoids a duplicate checkout
// in the compiled lock file.
func stripCheckoutSteps(steps []any) []any {
	result := make([]any, 0, len(steps))
	for _, step := range steps {
		if stepMap, ok := step.(map[string]any); ok {
			if uses, hasUses := stepMap["uses"]; hasUses {
				if usesStr, ok := uses.(string); ok {
					if strings.HasPrefix(usesStr, "actions/checkout@") || usesStr == "actions/checkout" {
						yamlImportLog.Printf("Stripping checkout step from copilot-setup-steps: %s", usesStr)
						continue
					}
				}
			}
		}
		result = append(result, step)
	}
	return result
}

package parser

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/githubnext/gh-aw/pkg/logger"
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

// processYAMLWorkflowImport processes an imported YAML workflow file
// Returns the extracted jobs in JSON format for merging
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

package workflow

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/typeutil"
	"github.com/goccy/go-yaml"
)

var workflowImportMergeLog = logger.New("workflow:workflow_import_merge")

// processAndMergeServices handles the merging of imported services with main workflow services
func (c *Compiler) processAndMergeServices(frontmatter map[string]any, workflowData *WorkflowData, importsResult *parser.ImportsResult) {
	workflowImportMergeLog.Print("Processing and merging services")

	workflowData.Services = c.extractTopLevelYAMLSection(frontmatter, "services")

	// Merge imported services if any
	if importsResult.MergedServices != "" {
		// Parse imported services from YAML
		var importedServices map[string]any
		if err := yaml.Unmarshal([]byte(importsResult.MergedServices), &importedServices); err == nil {
			// If there are main workflow services, parse and merge them
			if workflowData.Services != "" {
				// Parse main workflow services
				var mainServicesWrapper map[string]any
				if err := yaml.Unmarshal([]byte(workflowData.Services), &mainServicesWrapper); err == nil {
					if mainServices, ok := mainServicesWrapper["services"].(map[string]any); ok {
						// Merge: main workflow services take precedence over imported
						for key, value := range importedServices {
							if _, exists := mainServices[key]; !exists {
								mainServices[key] = value
							}
						}
						// Convert back to YAML with "services:" wrapper. Indent
						// sequence items (e.g. ports: lists) under their parent so the
						// output matches yamllint's indent-sequences expectation, the
						// same as the non-imported extraction path.
						servicesWrapper := map[string]any{"services": mainServices}
						servicesYAML, err := yaml.MarshalWithOptions(servicesWrapper, append(append([]yaml.EncodeOption{}, DefaultMarshalOptions...), yaml.IndentSequence(true))...)
						if err == nil {
							workflowData.Services = string(servicesYAML)
						}
					}
				}
			} else {
				// Only imported services exist, wrap in "services:" format.
				// Indent sequence items to match yamllint's indent-sequences
				// expectation, consistent with the other services marshaling paths.
				servicesWrapper := map[string]any{"services": importedServices}
				servicesYAML, err := yaml.MarshalWithOptions(servicesWrapper, append(append([]yaml.EncodeOption{}, DefaultMarshalOptions...), yaml.IndentSequence(true))...)
				if err == nil {
					workflowData.Services = string(servicesYAML)
				}
			}
		}
	}

	// Extract service port expressions for AWF --allow-host-service-ports
	if workflowData.Services != "" {
		expressions, warnings := ExtractServicePortExpressions(workflowData.Services)
		workflowData.ServicePortExpressions = expressions
		for _, w := range warnings {
			workflowImportMergeLog.Printf("Warning: %s", w)
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(w))
			c.IncrementWarningCount()
		}
		if expressions != "" {
			workflowImportMergeLog.Printf("Extracted service port expressions: %s", expressions)
		}
	}
}

// mergeJobsFromYAMLImports merges jobs from imported YAML workflows with main workflow jobs
// Main workflow jobs take precedence, with outputs and injected steps merged.
func (c *Compiler) mergeJobsFromYAMLImports(mainJobs map[string]any, mergedJobsJSON string) map[string]any {
	if mergedJobsJSON == "" || mergedJobsJSON == "{}" {
		workflowImportMergeLog.Print("No imported jobs to merge")
		return mainJobs
	}

	// Initialize result with main jobs or create empty map
	result := make(map[string]any)
	maps.Copy(result, mainJobs)

	// Accumulate imported jobs separately, in import declaration order, before
	// merging with the main workflow. This ensures that when multiple imports
	// define the same built-in job (e.g. jobs.activation), their injected step
	// fields are concatenated in encounter order (import1, import2, ..., main)
	// rather than being reversed by repeated "imported-first" merges against an
	// already-merged accumulator.
	importedAccum := make(map[string]any)

	// Split by newlines to handle multiple JSON objects from different imports
	lines := strings.Split(mergedJobsJSON, "\n")
	workflowImportMergeLog.Printf("Processing %d job definition lines", len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == "{}" {
			continue
		}

		// Parse JSON line to map
		var importedJobs map[string]any
		if err := json.Unmarshal([]byte(line), &importedJobs); err != nil {
			workflowImportMergeLog.Printf("Skipping malformed job entry: %v", err)
			continue
		}

		accumulateImportedJobs(importedAccum, importedJobs)
	}

	// Merge accumulated imported jobs into the main workflow jobs. Main
	// workflow jobs take precedence (don't override), except for setup/pre/
	// regular step fields (imported steps first, then main workflow steps) and
	// outputs (union by name, with main workflow values taking precedence).
	for jobName, jobConfig := range importedAccum {
		if _, exists := result[jobName]; !exists {
			workflowImportMergeLog.Printf("Adding imported job: %s", jobName)
			result[jobName] = jobConfig
		} else {
			mergedJob, merged := mergeJobImportFields(jobName, result[jobName], jobConfig)
			if merged {
				workflowImportMergeLog.Printf("Merged outputs and injected steps for conflicting job %s (main output values take precedence)", jobName)
				result[jobName] = mergedJob
				continue
			}

			workflowImportMergeLog.Printf("Skipping imported job %s (already defined in main workflow)", jobName)
		}
	}

	workflowImportMergeLog.Printf("Successfully merged jobs: total=%d, imported=%d", len(result), len(result)-len(mainJobs))
	return result
}

func accumulateImportedJobs(accumulated map[string]any, importedJobs map[string]any) {
	for jobName, jobConfig := range importedJobs {
		existing, exists := accumulated[jobName]
		if !exists {
			accumulated[jobName] = jobConfig
			continue
		}

		// Keep injected steps in encounter order and retain the existing job
		// definition when a later import only contributes output mappings.
		mergedJob, merged := mergeJobInjectedSteps(jobName, jobConfig, existing)
		if outputs, ok := mergeJobOutputField(jobConfig, existing); ok {
			if existingMap, ok := existing.(map[string]any); ok && !merged {
				mergedJob = maps.Clone(existingMap)
			}
			mergedJob["outputs"] = outputs
			merged = true
		}
		if merged {
			workflowImportMergeLog.Printf("Merged outputs and injected steps across imports for job %s (encounter order)", jobName)
			accumulated[jobName] = mergedJob
			continue
		}

		workflowImportMergeLog.Printf("Skipping duplicate imported job %s (no mergeable fields)", jobName)
	}
}

func mergeJobImportFields(jobName string, mainJob any, importedJob any) (map[string]any, bool) {
	merged, mergedAny := mergeJobInjectedSteps(jobName, mainJob, importedJob)
	if outputs, ok := mergeJobOutputField(mainJob, importedJob); ok {
		if mainMap, ok := mainJob.(map[string]any); ok && !mergedAny {
			merged = maps.Clone(mainMap)
		}
		merged["outputs"] = outputs
		mergedAny = true
	}
	return merged, mergedAny
}

func mergeJobOutputField(mainJob any, importedJob any) (map[string]any, bool) {
	mainMap, mainOK := mainJob.(map[string]any)
	importedMap, importedOK := importedJob.(map[string]any)
	if !mainOK || !importedOK {
		return nil, false
	}
	mainOutputs, hasMainOutputs := mainMap["outputs"]
	importedOutputs, hasImportedOutputs := importedMap["outputs"]
	mainOutputMap, validMainOutputs := mainOutputs.(map[string]any)
	importedOutputMap, validImportedOutputs := importedOutputs.(map[string]any)
	if (mainOutputs != nil && !validMainOutputs) || (importedOutputs != nil && !validImportedOutputs) || (!hasMainOutputs && !hasImportedOutputs) {
		return nil, false
	}
	outputs := make(map[string]any)
	maps.Copy(outputs, importedOutputMap)
	maps.Copy(outputs, mainOutputMap)
	return outputs, true
}

func mergeJobInjectedSteps(jobName string, mainJob any, importedJob any) (map[string]any, bool) {
	mainMap, ok := mainJob.(map[string]any)
	if !ok {
		return nil, false
	}
	importedMap, ok := importedJob.(map[string]any)
	if !ok {
		return nil, false
	}

	merged := make(map[string]any, len(mainMap))
	// Merged slices and output maps are allocated separately, so a shallow copy
	// preserves the input job configurations.
	maps.Copy(merged, mainMap)

	mergedAny := false
	fieldNames := []string{"setup-steps", "pre-steps"}
	if jobName == string(constants.ActivationJobName) {
		fieldNames = append(fieldNames, "steps")
	}
	for _, fieldName := range fieldNames {
		mergedSteps, ok := mergeJobStepField(mainMap, importedMap, fieldName)
		if !ok {
			continue
		}
		merged[fieldName] = mergedSteps
		mergedAny = true
	}

	// continue-on-error is a scalar built-in job augmentation: the main workflow's value
	// always wins when present, but an imported value should still apply when the main
	// workflow leaves the field unset entirely (rather than being silently dropped just
	// because the job is also declared in the main workflow for other fields, e.g. needs).
	if _, hasMain := mainMap["continue-on-error"]; !hasMain {
		if importedValue, hasImported := importedMap["continue-on-error"]; hasImported {
			merged["continue-on-error"] = importedValue
			mergedAny = true
		}
	}

	return merged, mergedAny
}

func mergeJobStepField(mainJob map[string]any, importedJob map[string]any, fieldName string) ([]any, bool) {
	mainSteps, hasMain := extractJobStepField(mainJob, fieldName)
	importedSteps, hasImported := extractJobStepField(importedJob, fieldName)
	if !hasMain && !hasImported {
		return nil, false
	}
	if !hasMain {
		return append([]any(nil), importedSteps...), true
	}
	if !hasImported {
		return append([]any(nil), mainSteps...), true
	}

	mergedSteps := make([]any, 0, typeutil.SafeAllocationCapacity(len(importedSteps), len(mainSteps)))
	mergedSteps = append(mergedSteps, importedSteps...)
	mergedSteps = append(mergedSteps, mainSteps...)
	return mergedSteps, true
}

func extractJobStepField(jobConfig map[string]any, fieldName string) ([]any, bool) {
	raw, exists := jobConfig[fieldName]
	if !exists {
		return nil, false
	}
	steps, ok := raw.([]any)
	if !ok {
		return nil, false
	}
	return steps, true
}

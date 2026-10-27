package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/typeutil"
	"github.com/github/gh-aw/pkg/workflow/taint"
	"github.com/goccy/go-yaml"
	"github.com/rhysd/actionlint"
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
func (c *Compiler) mergeJobsFromYAMLImports(mainJobs map[string]any, mergedJobsJSON string) (map[string]any, error) {
	if mergedJobsJSON == "" || mergedJobsJSON == "{}" {
		workflowImportMergeLog.Print("No imported jobs to merge")
		return mainJobs, nil
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
	importedAccum, regularStepIDs := accumulateImportedJobLines(mergedJobsJSON)

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

	if err := validateMergedJobOutputSteps(result, regularStepIDs); err != nil {
		return nil, err
	}
	workflowImportMergeLog.Printf("Successfully merged jobs: total=%d, imported=%d", len(result), len(result)-len(mainJobs))
	return result, nil
}

func accumulateImportedJobLines(mergedJobsJSON string) (map[string]any, map[string]map[string]struct{}) {
	accumulated := make(map[string]any)
	regularStepIDs := make(map[string]map[string]struct{})
	lines := strings.Split(mergedJobsJSON, "\n")
	workflowImportMergeLog.Printf("Processing %d job definition lines", len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == "{}" {
			continue
		}
		var importedJobs map[string]any
		if err := json.Unmarshal([]byte(line), &importedJobs); err != nil {
			workflowImportMergeLog.Printf("Skipping malformed job entry: %v", err)
			continue
		}
		for jobName, jobConfig := range importedJobs {
			if regularStepIDs[jobName] == nil {
				regularStepIDs[jobName] = make(map[string]struct{})
			}
			maps.Copy(regularStepIDs[jobName], importedJobStepIDs(jobConfig, "steps"))
		}
		accumulateImportedJobs(accumulated, importedJobs)
	}
	return accumulated, regularStepIDs
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
			laterMap, ok := jobConfig.(map[string]any)
			if !ok {
				continue
			}
			_, hasLaterOutputs := laterMap["outputs"]
			if existingMap, ok := existing.(map[string]any); ok && (!merged || hasLaterOutputs && len(laterMap) == 1) {
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

func importedJobStepIDs(job any, fields ...string) map[string]struct{} {
	ids := make(map[string]struct{})
	jobMap, ok := job.(map[string]any)
	if !ok {
		return ids
	}
	for _, field := range fields {
		steps, _ := extractJobStepField(jobMap, field)
		for _, step := range steps {
			stepMap, ok := step.(map[string]any)
			if !ok {
				continue
			}
			if id, ok := stepMap["id"].(string); ok {
				ids[strings.ToLower(id)] = struct{}{}
			}
		}
	}
	return ids
}

// Check the final outputs after main-workflow overrides, not intermediate
// imports: a main output or an injected step may resolve an earlier conflict.
func validateMergedJobOutputSteps(jobs map[string]any, regularStepIDs map[string]map[string]struct{}) error {
	for _, jobName := range slices.Sorted(maps.Keys(regularStepIDs)) {
		retained := importedJobStepIDs(jobs[jobName], "steps", "setup-steps", "pre-steps")
		discarded := maps.Clone(regularStepIDs[jobName])
		for id := range retained {
			delete(discarded, id)
		}
		if len(discarded) == 0 {
			continue
		}
		jobMap, ok := jobs[jobName].(map[string]any)
		if !ok {
			continue
		}
		outputs, ok := jobMap["outputs"].(map[string]any)
		if !ok {
			continue
		}
		for _, name := range slices.Sorted(maps.Keys(outputs)) {
			text, ok := outputs[name].(string)
			if !ok {
				continue
			}
			path := "jobs." + jobName + ".outputs." + name
			err := taint.VisitInterpolations(context.Background(), text, path, func(_, _, _ int, node actionlint.ExprNode) error {
				discardedID := discardedOutputStepID(node, discarded)
				if discardedID != "" {
					return fmt.Errorf("%s references discarded regular step %q. Keep the producer in the retained job or use pre-steps to share it across imports", path, discardedID)
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func discardedOutputStepID(node actionlint.ExprNode, discarded map[string]struct{}) string {
	var discardedID string
	actionlint.VisitExprNode(node, func(child, _ actionlint.ExprNode, entering bool) {
		if !entering || discardedID != "" {
			return
		}
		var receiver actionlint.ExprNode
		var id string
		switch access := child.(type) {
		case *actionlint.ObjectDerefNode:
			receiver, id = access.Receiver, access.Property
		case *actionlint.IndexAccessNode:
			if key, ok := access.Index.(*actionlint.StringNode); ok {
				receiver, id = access.Operand, key.Value
			}
		}
		if variable, ok := receiver.(*actionlint.VariableNode); ok && strings.EqualFold(variable.Name, "steps") {
			if _, missing := discarded[strings.ToLower(id)]; missing {
				discardedID = id
			}
		}
	})
	return discardedID
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

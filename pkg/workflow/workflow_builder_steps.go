package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/goccy/go-yaml"
)

// processAndMergeSteps handles the merging of imported steps with main workflow steps.
func (c *Compiler) processAndMergeSteps(frontmatter map[string]any, workflowData *WorkflowData, importsResult *parser.ImportsResult) error { //nolint:largefunc // Existing step merge handles main and imported steps together.
	workflowBuilderLog.Print("Processing and merging custom steps")

	workflowData.CustomSteps = c.extractTopLevelYAMLSection(frontmatter, "steps")

	// Parse copilot-setup-steps if present (these go at the start)
	var copilotSetupSteps []any
	if importsResult.CopilotSetupSteps != "" {
		if err := yaml.Unmarshal([]byte(importsResult.CopilotSetupSteps), &copilotSetupSteps); err != nil {
			workflowBuilderLog.Printf("Failed to unmarshal copilot-setup steps: %v", err)
		} else {
			// Convert to typed steps for action pinning
			typedCopilotSteps, err := SliceToSteps(copilotSetupSteps)
			if err != nil {
				workflowBuilderLog.Printf("Failed to convert copilot-setup steps to typed steps: %v", err)
			} else {
				// Apply action pinning to copilot-setup steps
				typedCopilotSteps, err = applyActionPinsToTypedSteps(typedCopilotSteps, workflowData)
				if err != nil {
					return fmt.Errorf("copilot-setup steps: %w", err)
				}
				// Convert back to []any for YAML marshaling
				copilotSetupSteps = StepsToSlice(typedCopilotSteps)
			}
		}
	}

	// Parse other imported steps if present (these go after copilot-setup but before main steps)
	var otherImportedSteps []any
	if importsResult.MergedSteps != "" {
		if err := yaml.Unmarshal([]byte(importsResult.MergedSteps), &otherImportedSteps); err != nil {
			return fmt.Errorf("imported steps YAML is not recognized, expected a valid list of GitHub Actions steps: %w", err)
		}
		// Convert to typed steps for action pinning
		typedOtherSteps, err := SliceToSteps(otherImportedSteps)
		if err != nil {
			return fmt.Errorf("imported steps could not be converted to typed steps, expected each entry to be a valid step object: %w", err)
		}
		// Apply action pinning to other imported steps
		typedOtherSteps, err = applyActionPinsToTypedSteps(typedOtherSteps, workflowData)
		if err != nil {
			return fmt.Errorf("imported steps: %w", err)
		}
		// Convert back to []any for YAML marshaling
		otherImportedSteps = StepsToSlice(typedOtherSteps)
	}

	// If there are main workflow steps, parse them
	var mainSteps []any
	if workflowData.CustomSteps != "" {
		var mainStepsWrapper map[string]any
		if err := yaml.Unmarshal([]byte(workflowData.CustomSteps), &mainStepsWrapper); err != nil {
			return fmt.Errorf("custom steps YAML is not recognized, expected a 'steps:' mapping with a valid list of steps: %w", err)
		}
		if mainStepsVal, hasSteps := mainStepsWrapper["steps"]; hasSteps {
			if steps, ok := mainStepsVal.([]any); ok {
				mainSteps = steps
				// Convert to typed steps for action pinning
				typedMainSteps, err := SliceToSteps(mainSteps)
				if err != nil {
					return fmt.Errorf("main steps could not be converted to typed steps, expected each entry to be a valid step object: %w", err)
				}
				// Apply action pinning to main steps
				typedMainSteps, err = applyActionPinsToTypedSteps(typedMainSteps, workflowData)
				if err != nil {
					return fmt.Errorf("steps: %w", err)
				}
				// Convert back to []any for YAML marshaling
				mainSteps = StepsToSlice(typedMainSteps)
			}
		}
	}

	// Merge steps in the correct order:
	// 1. copilot-setup-steps (at start)
	// 2. other imported steps (after copilot-setup)
	// 3. main frontmatter steps (last)
	var allSteps []any
	if len(copilotSetupSteps) > 0 || len(mainSteps) > 0 || len(otherImportedSteps) > 0 {
		allSteps = append(allSteps, copilotSetupSteps...)
		allSteps = append(allSteps, otherImportedSteps...)
		allSteps = append(allSteps, mainSteps...)

		// Convert back to YAML with "steps:" wrapper
		stepsWrapper := map[string]any{"steps": allSteps}
		stepsYAML, err := yaml.Marshal(stepsWrapper)
		if err == nil {
			// Remove quotes from uses values with version comments
			workflowData.CustomSteps = unquoteUsesWithComments(string(stepsYAML))
		}
	}
	return nil
}

// processAndMergePreSteps handles the processing and merging of pre-steps with action pinning.
// Pre-steps run at the very beginning of the agent job, before checkout and the subsequent
// built-in steps, allowing users to mint tokens or perform other setup that must happen
// before the repository is checked out. Imported pre-steps are merged before the main
// workflow's pre-steps so that the main workflow can override or extend the imports.
func (c *Compiler) processAndMergePreSteps(frontmatter map[string]any, workflowData *WorkflowData, importsResult *parser.ImportsResult) error {
	workflowBuilderLog.Print("Processing and merging pre-steps")

	mainPreStepsYAML := c.extractTopLevelYAMLSection(frontmatter, "pre-steps")

	// Parse imported pre-steps if present (these go before the main workflow's pre-steps)
	var importedPreSteps []any
	if importsResult.MergedPreSteps != "" {
		if err := yaml.Unmarshal([]byte(importsResult.MergedPreSteps), &importedPreSteps); err != nil {
			return fmt.Errorf("imported pre-steps YAML is not recognized, expected a valid list of GitHub Actions steps: %w", err)
		}
		typedImported, err := SliceToSteps(importedPreSteps)
		if err != nil {
			return fmt.Errorf("imported pre-steps could not be converted to typed steps, expected each entry to be a valid step object: %w", err)
		}
		typedImported, err = applyActionPinsToTypedSteps(typedImported, workflowData)
		if err != nil {
			return fmt.Errorf("imported pre-steps: %w", err)
		}
		importedPreSteps = StepsToSlice(typedImported)
	}

	// Parse main workflow pre-steps if present
	var mainPreSteps []any
	if mainPreStepsYAML != "" {
		var mainWrapper map[string]any
		if err := yaml.Unmarshal([]byte(mainPreStepsYAML), &mainWrapper); err != nil {
			return fmt.Errorf("pre-steps YAML is not recognized, expected a 'pre-steps:' mapping with a valid list of steps: %w", err)
		}
		if mainVal, ok := mainWrapper["pre-steps"]; ok {
			if steps, ok := mainVal.([]any); ok {
				mainPreSteps = steps
				typedMain, err := SliceToSteps(mainPreSteps)
				if err != nil {
					return fmt.Errorf("pre-steps could not be converted to typed steps, expected each entry to be a valid step object: %w", err)
				}
				typedMain, err = applyActionPinsToTypedSteps(typedMain, workflowData)
				if err != nil {
					return fmt.Errorf("pre-steps: %w", err)
				}
				mainPreSteps = StepsToSlice(typedMain)
			}
		}
	}

	// Merge in order: imported pre-steps first, then main workflow's pre-steps
	var allPreSteps []any
	if len(importedPreSteps) > 0 || len(mainPreSteps) > 0 {
		allPreSteps = append(allPreSteps, importedPreSteps...)
		allPreSteps = append(allPreSteps, mainPreSteps...)

		stepsWrapper := map[string]any{"pre-steps": allPreSteps}
		stepsYAML, err := yaml.Marshal(stepsWrapper)
		if err == nil {
			workflowData.PreSteps = unquoteUsesWithComments(string(stepsYAML))
		}
	}
	return nil
}

// processAndMergePreAgentSteps handles processing and merging of pre-agent-steps with action pinning.
// Imported pre-agent-steps are prepended so main workflow pre-agent-steps run last.
func (c *Compiler) processAndMergePreAgentSteps(frontmatter map[string]any, workflowData *WorkflowData, importsResult *parser.ImportsResult) error {
	workflowBuilderLog.Print("Processing and merging pre-agent-steps")

	mainPreAgentStepsYAML := c.extractTopLevelYAMLSection(frontmatter, "pre-agent-steps")

	var importedPreAgentSteps []any
	if importsResult.MergedPreAgentSteps != "" {
		if err := yaml.Unmarshal([]byte(importsResult.MergedPreAgentSteps), &importedPreAgentSteps); err != nil {
			return fmt.Errorf("imported pre-agent-steps YAML is not recognized, expected a valid list of GitHub Actions steps: %w", err)
		}
		typedImported, err := SliceToSteps(importedPreAgentSteps)
		if err != nil {
			return fmt.Errorf("imported pre-agent-steps could not be converted to typed steps, expected each entry to be a valid step object: %w", err)
		}
		typedImported, err = applyActionPinsToTypedSteps(typedImported, workflowData)
		if err != nil {
			return fmt.Errorf("imported pre-agent-steps: %w", err)
		}
		importedPreAgentSteps = StepsToSlice(typedImported)
	}

	var mainPreAgentSteps []any
	if mainPreAgentStepsYAML != "" {
		var mainWrapper map[string]any
		if err := yaml.Unmarshal([]byte(mainPreAgentStepsYAML), &mainWrapper); err != nil {
			return fmt.Errorf("pre-agent-steps YAML is not recognized, expected a 'pre-agent-steps:' mapping with a valid list of steps: %w", err)
		}
		if mainVal, ok := mainWrapper["pre-agent-steps"]; ok {
			if steps, ok := mainVal.([]any); ok {
				mainPreAgentSteps = steps
				typedMain, err := SliceToSteps(mainPreAgentSteps)
				if err != nil {
					return fmt.Errorf("pre-agent-steps could not be converted to typed steps, expected each entry to be a valid step object: %w", err)
				}
				typedMain, err = applyActionPinsToTypedSteps(typedMain, workflowData)
				if err != nil {
					return fmt.Errorf("pre-agent-steps: %w", err)
				}
				mainPreAgentSteps = StepsToSlice(typedMain)
			}
		}
	}

	var allPreAgentSteps []any
	if len(importedPreAgentSteps) > 0 || len(mainPreAgentSteps) > 0 {
		allPreAgentSteps = append(allPreAgentSteps, importedPreAgentSteps...)
		allPreAgentSteps = append(allPreAgentSteps, mainPreAgentSteps...)

		stepsWrapper := map[string]any{"pre-agent-steps": allPreAgentSteps}
		stepsYAML, err := yaml.Marshal(stepsWrapper)
		if err == nil {
			workflowData.PreAgentSteps = unquoteUsesWithComments(string(stepsYAML))
		}
	}
	return nil
}

// processAndMergePostSteps handles the processing and merging of post-steps with action pinning.
// Imported post-steps are appended after the main workflow's post-steps.
func (c *Compiler) processAndMergePostSteps(frontmatter map[string]any, workflowData *WorkflowData, importsResult *parser.ImportsResult) error {
	workflowBuilderLog.Print("Processing and merging post-steps")

	mainPostStepsYAML := c.extractTopLevelYAMLSection(frontmatter, "post-steps")

	// Parse imported post-steps if present (these go after the main workflow's post-steps)
	var importedPostSteps []any
	if importsResult.MergedPostSteps != "" {
		if err := yaml.Unmarshal([]byte(importsResult.MergedPostSteps), &importedPostSteps); err != nil {
			return fmt.Errorf("imported post-steps YAML is not recognized, expected a valid list of GitHub Actions steps: %w", err)
		}
		typedImported, err := SliceToSteps(importedPostSteps)
		if err != nil {
			return fmt.Errorf("imported post-steps could not be converted to typed steps, expected each entry to be a valid step object: %w", err)
		}
		typedImported, err = applyActionPinsToTypedSteps(typedImported, workflowData)
		if err != nil {
			return fmt.Errorf("imported post-steps: %w", err)
		}
		importedPostSteps = StepsToSlice(typedImported)
	}

	// Parse main workflow post-steps if present
	var mainPostSteps []any
	if mainPostStepsYAML != "" {
		var mainWrapper map[string]any
		if err := yaml.Unmarshal([]byte(mainPostStepsYAML), &mainWrapper); err != nil {
			return fmt.Errorf("post-steps YAML is not recognized, expected a 'post-steps:' mapping with a valid list of steps: %w", err)
		}
		if mainVal, ok := mainWrapper["post-steps"]; ok {
			if steps, ok := mainVal.([]any); ok {
				mainPostSteps = steps
				typedMain, err := SliceToSteps(mainPostSteps)
				if err != nil {
					return fmt.Errorf("post-steps could not be converted to typed steps, expected each entry to be a valid step object: %w", err)
				}
				typedMain, err = applyActionPinsToTypedSteps(typedMain, workflowData)
				if err != nil {
					return fmt.Errorf("post-steps: %w", err)
				}
				mainPostSteps = StepsToSlice(typedMain)
			}
		}
	}

	// Merge in order: main workflow's post-steps first, then imported post-steps
	var allPostSteps []any
	if len(mainPostSteps) > 0 || len(importedPostSteps) > 0 {
		allPostSteps = append(allPostSteps, mainPostSteps...)
		allPostSteps = append(allPostSteps, importedPostSteps...)
		if err := validatePostStepsSafeOutputs(allPostSteps); err != nil {
			return err
		}

		stepsWrapper := map[string]any{"post-steps": allPostSteps}
		stepsYAML, err := yaml.Marshal(stepsWrapper)
		if err == nil {
			workflowData.PostSteps = unquoteUsesWithComments(string(stepsYAML))
		}
	}
	return nil
}

// frontmatterHasTrigger reports whether the given "on:" frontmatter value contains
// the specified trigger name. It handles all three YAML "on:" forms:
//   - string scalar:  on: pull_request_target
//   - sequence:       on: [pull_request_target, push]
//   - mapping:        on:\n  pull_request_target:\n    types: [closed]
func frontmatterHasTrigger(onVal any, trigger string) bool {
	switch v := onVal.(type) {
	case string:
		return v == trigger
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && s == trigger {
				return true
			}
		}
	case map[string]any:
		_, ok := v[trigger]
		return ok
	}
	return false
}

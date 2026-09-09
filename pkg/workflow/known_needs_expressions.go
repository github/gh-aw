package workflow

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
)

var knownNeedsLog = logger.New("workflow:known_needs")

// generateKnownNeedsExpressions generates expression mappings for all known needs.* variables
// that could be referenced in the prompt. This ensures that these variables are available
// for interpolation even if the markdown changes without recompilation.
//
// IMPORTANT: The prompt is generated in the ACTIVATION job, so it can only access outputs
// from jobs that the activation job depends on (i.e., jobs that run BEFORE activation).
// This typically includes:
// - needs.pre_activation.outputs.* (activated, matched_command) - only when pre_activation job exists
// - needs.<custom-job>.outputs.* for custom jobs that run before activation
//
// The function does NOT generate mappings for jobs that run AFTER activation:
// - needs.activation.outputs.* (activation is the current job)
// - needs.agent.outputs.* (agent runs AFTER activation, includes detection_success/detection_conclusion)
// - needs.<safe-output-job>.outputs.* (these run AFTER agent)
//
// preActivationJobCreated indicates whether the pre_activation job was created for this workflow.
// When false, no pre_activation output mappings are generated to avoid actionlint errors.
//
// Returns a slice of ExpressionMapping that should be merged with other expression mappings.
func generateKnownNeedsExpressions(data *WorkflowData, preActivationJobCreated bool) []*ExpressionMapping {
	knownNeedsLog.Print("Generating known needs.* expressions for activation job")

	var mappings []*ExpressionMapping

	// Pre-activation job outputs (activation depends on pre_activation only when it exists)
	// Only generate these mappings when the pre_activation job was actually created;
	// otherwise referencing needs.pre_activation.outputs.* causes actionlint errors.
	if preActivationJobCreated {
		// Always include the "activated" output
		activatedExpr := fmt.Sprintf("needs.%s.outputs.%s", constants.PreActivationJobName, constants.ActivatedOutput)
		activatedEnvVar := fmt.Sprintf("GH_AW_NEEDS_%s_OUTPUTS_%s",
			normalizeJobNameForEnvVar(string(constants.PreActivationJobName)),
			normalizeJobNameForEnvVar(constants.ActivatedOutput))
		mappings = append(mappings, &ExpressionMapping{
			Original: fmt.Sprintf("${{ %s }}", activatedExpr),
			EnvVar:   activatedEnvVar,
			Content:  activatedExpr,
		})

		// Only include "matched_command" when the workflow has a command trigger,
		// since matched_command is only declared in the pre_activation job outputs for command workflows.
		if len(data.Command) > 0 {
			matchedCmdExpr := fmt.Sprintf("needs.%s.outputs.%s", constants.PreActivationJobName, constants.MatchedCommandOutput)
			matchedCmdEnvVar := fmt.Sprintf("GH_AW_NEEDS_%s_OUTPUTS_%s",
				normalizeJobNameForEnvVar(string(constants.PreActivationJobName)),
				normalizeJobNameForEnvVar(constants.MatchedCommandOutput))
			mappings = append(mappings, &ExpressionMapping{
				Original: fmt.Sprintf("${{ %s }}", matchedCmdExpr),
				EnvVar:   matchedCmdEnvVar,
				Content:  matchedCmdExpr,
			})
		}
	}

	// Custom job outputs from frontmatter jobs
	// Only include custom jobs that would run before activation
	// (i.e., jobs that don't depend on activation, pre_activation, agent, or detection)
	if data.Jobs != nil {
		customJobNames := getCustomJobsBeforeActivation(data)
		for _, jobName := range customJobNames {
			for _, output := range knownNeedsOutputNames(data.Jobs[jobName]) {
				expr := fmt.Sprintf("needs.%s.outputs.%s", jobName, output)
				envVar := fmt.Sprintf("GH_AW_NEEDS_%s_OUTPUTS_%s",
					normalizeJobNameForEnvVar(jobName),
					normalizeJobNameForEnvVar(output))
				mappings = append(mappings, &ExpressionMapping{
					Original: fmt.Sprintf("${{ %s }}", expr),
					EnvVar:   envVar,
					Content:  expr,
				})
			}
		}
	}

	knownNeedsLog.Printf("Generated %d known needs.* expression mappings", len(mappings))
	return mappings
}

func knownNeedsOutputNames(jobConfig any) []string {
	defaultOutputs := []string{"output"}
	jobMap, ok := jobConfig.(map[string]any)
	if !ok {
		return defaultOutputs
	}
	outputsField, hasOutputs := jobMap["outputs"]
	if !hasOutputs || outputsField == nil {
		return defaultOutputs
	}
	outputsMap, ok := outputsField.(map[string]any)
	if !ok {
		return defaultOutputs
	}
	outputs := make([]string, 0, len(outputsMap))
	for output := range outputsMap {
		outputs = append(outputs, output)
	}
	sort.Strings(outputs)
	return outputs
}

// filterExpressionsForActivation filters expression mappings to remove any that reference
// custom jobs NOT in beforeActivationJobs. This prevents actionlint errors when a custom job
// explicitly depends on activation (and therefore runs AFTER activation) but the markdown body
// contains expressions like ${{ needs.that_job.outputs.foo }} that would be impossible to
// evaluate at activation time.
//
// If beforeActivationJobs is nil or empty, any expression referencing a custom job (one present
// in customJobs) is dropped because no custom job runs before activation.
//
// Only expressions referencing jobs in customJobs are considered for filtering; standard
// GitHub Actions contexts (github.*, env.*, etc.) and system job outputs (pre_activation) are
// always kept.
func filterExpressionsForActivation(mappings []*ExpressionMapping, customJobs map[string]any, beforeActivationJobs []string) []*ExpressionMapping {
	knownNeedsLog.Printf("Filtering %d expression mappings for activation (customJobs=%d, beforeActivationJobs=%d)", len(mappings), len(customJobs), len(beforeActivationJobs))
	if customJobs == nil || len(mappings) == 0 {
		return mappings
	}

	beforeActivationSet := make(map[string]struct {
	}, len(beforeActivationJobs))
	for _, j := range beforeActivationJobs {
		beforeActivationSet[j] = struct {
		}{}
	}

	filtered := make([]*ExpressionMapping, 0, len(mappings))
	for _, m := range mappings {
		// Only examine needs.* expressions
		if !strings.HasPrefix(m.Content, "needs.") {
			filtered = append(filtered, m)
			continue
		}
		// Extract the job name (needs.<jobName>.*)
		rest := m.Content[len("needs."):]
		jobName, _, ok := strings.Cut(rest, ".")
		if !ok {
			filtered = append(filtered, m)
			continue
		}
		// If it's a custom job NOT in beforeActivationJobs, drop it
		if _, isCustomJob := customJobs[jobName]; isCustomJob && !setutil.Contains(beforeActivationSet, jobName) {
			knownNeedsLog.Printf("Filtered post-activation expression from activation substitution step: %s", m.Content)
			continue
		}
		filtered = append(filtered, m)
	}
	knownNeedsLog.Printf("Filtered expressions: %d remaining from %d total", len(filtered), len(mappings))
	return filtered
}

// normalizeJobNameForEnvVar converts a job name to a valid environment variable segment
// Examples: "activation" -> "ACTIVATION", "pre_activation" -> "PRE_ACTIVATION"
func normalizeJobNameForEnvVar(jobName string) string {
	// Already in the correct format for most job names
	// Just uppercase and replace hyphens with underscores
	var result strings.Builder
	for _, char := range jobName {
		if char == '-' {
			result.WriteString("_")
		} else if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '_' {
			if char >= 'a' && char <= 'z' {
				result.WriteRune(char - 32) // Convert to uppercase
			} else {
				result.WriteRune(char)
			}
		}
	}
	return result.String()
}

// getCustomJobsBeforeActivation returns a list of custom job names that run before the activation job
// A custom job runs before activation ONLY if it explicitly depends on pre_activation
// Note: Jobs without explicit 'needs' will automatically get 'needs: activation' added by the compiler,
// so they run AFTER activation, not before. Only jobs that explicitly depend on pre_activation run before activation.
func getCustomJobsBeforeActivation(data *WorkflowData) []string {
	var jobNames []string

	if data.Jobs == nil {
		return jobNames
	}

	// Extract job names that explicitly depend on pre_activation
	for jobName, jobConfig := range data.Jobs {
		jobMap, ok := jobConfig.(map[string]any)
		if !ok {
			continue
		}

		// Check if the job explicitly depends on pre_activation
		// Jobs without explicit needs will get 'needs: activation' added automatically,
		// so they run AFTER activation
		needsField, hasNeeds := jobMap["needs"]
		if !hasNeeds {
			// No explicit dependencies - this will get needs: activation added automatically
			// So it runs AFTER activation, not before
			continue
		}

		// Parse the needs field (can be string or array)
		needsList := parseNeedsField(needsField)

		// Check if it depends on pre_activation
		dependsOnPreActivation := slices.Contains(needsList, string(constants.PreActivationJobName))

		// Only include if it depends on pre_activation (and not on activation/agent/detection)
		if dependsOnPreActivation {
			// Double-check it doesn't also depend on activation-related jobs
			hasActivationDependency := false
			for _, dep := range needsList {
				if dep == string(constants.ActivationJobName) ||
					dep == string(constants.AgentJobName) ||
					dep == string(constants.DetectionJobName) {
					hasActivationDependency = true
					break
				}
			}

			if !hasActivationDependency {
				jobNames = append(jobNames, jobName)
			}
		}
	}

	// Sort for consistent output
	sort.Strings(jobNames)

	return jobNames
}

// parseNeedsField parses the needs field from a job configuration
// The needs field can be a string (single dependency) or an array of strings
func parseNeedsField(needsField any) []string {
	// GitHub Actions allows `needs: "single-dep"` as shorthand for `needs: ["single-dep"]`
	if s, ok := needsField.(string); ok {
		return []string{s}
	}
	result := parseStringSliceAny(needsField, nil)
	if result == nil {
		return []string{}
	}
	return result
}

// Job-step helpers validate, normalize, and insert configured job steps.
package workflow

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/typeutil"
)

var exactSetupStepIDPattern = regexp.MustCompile(`(?m)^\s*id:\s*setup\s*$`)

// validateRestrictedBuiltinSetupSteps rejects jobs.<name>.setup-steps for the
// activation and pre-activation jobs. setup-steps run before any
// compiler-generated token-mint or short-circuit protection steps, so
// allowing arbitrary user-authored steps there could bypass those
// protections. By contrast, jobs.activation.steps (see
// validateRestrictedBuiltinSteps) is inserted later in the job, before
// artifact staging but after the activation gate/output has already run, so
// it is not equivalent to setup-steps and is intentionally allowed for the
// activation job. Injected steps content is still scanned for GitHub CLI
// write-command usage (see cacheActivationPreStepPermissions) regardless of
// which field it came from.
func validateRestrictedBuiltinSetupSteps(jobName string, hasSetupSteps bool) error {
	if !hasSetupSteps {
		return nil
	}

	if jobName == string(constants.ActivationJobName) ||
		jobName == string(constants.PreActivationJobName) ||
		jobName == "pre-activation" {
		return fmt.Errorf(
			"jobs.%s.setup-steps is not allowed: setup-steps are refused for activation/pre-activation jobs because they can short-circuit protections",
			jobName,
		)
	}

	return nil
}

// validateRestrictedBuiltinSteps rejects jobs.<name>.steps on built-in jobs
// other than activation. Unlike setup-steps and pre-steps, steps are only
// applied to the activation job (inserted before artifact staging); silently
// accepting the field on other built-in jobs (e.g. pre-activation,
// safe_outputs) would discard the user's configuration without feedback.
// Custom (non-built-in) jobs are unaffected since their steps field is a
// regular job definition field, not an injection field.
func validateRestrictedBuiltinSteps(jobName string, targetJobName string, hasSteps bool) error {
	if !hasSteps || !isBuiltinJobName(targetJobName) || targetJobName == string(constants.ActivationJobName) {
		return nil
	}
	// jobs.pre-activation.steps is a distinct, already-validated custom field
	// (see extractPreActivationCustomFields) handled outside of this
	// built-in setup/pre-steps injection path; it is not subject to this
	// restriction.
	if targetJobName == string(constants.PreActivationJobName) {
		return nil
	}

	return fmt.Errorf(
		"jobs.%s.steps is not allowed: steps are only supported for the activation job",
		jobName,
	)
}

// insertSetupStepsAtStart places setup-steps at the start of the job so they
// run before any compiler-generated setup, checkout, or token-mint steps.
func insertSetupStepsAtStart(steps []string, setupSteps []string) []string {
	if len(setupSteps) == 0 {
		return steps
	}

	result := make([]string, 0, typeutil.SafeAllocationCapacity(len(steps), len(setupSteps)))
	result = append(result, setupSteps...)
	result = append(result, steps...)
	return result
}

func insertPreStepsAtEarliestBoundary(steps []string, preSteps []string) []string {
	if len(preSteps) == 0 {
		return steps
	}

	firstCheckoutIdx, firstTokenMintIdx, lastSetupIdx := findPreStepBoundaries(steps)
	insertIdx := preStepInsertionIndex(steps, firstCheckoutIdx, firstTokenMintIdx, lastSetupIdx)
	if insertIdx > len(steps) {
		insertIdx = len(steps)
	}

	result := make([]string, 0, typeutil.SafeAllocationCapacity(len(steps), len(preSteps)))
	result = append(result, steps[:insertIdx]...)
	result = append(result, preSteps...)
	result = append(result, steps[insertIdx:]...)
	return result
}

func findPreStepBoundaries(steps []string) (int, int, int) {
	firstCheckoutIdx := -1
	firstTokenMintIdx := -1
	lastSetupIdx := -1
	for i, step := range steps {
		if firstCheckoutIdx == -1 && strings.Contains(step, "uses: actions/checkout@") {
			firstCheckoutIdx = i
			// Walk backward to the checkout step's list-item boundary ("- ").
			// If no boundary is found, keep the current index so insertion still
			// occurs before the checkout uses-line.
			for j := i; j >= 0; j-- {
				trimmed := strings.TrimLeft(steps[j], " ")
				if strings.HasPrefix(trimmed, "- ") {
					firstCheckoutIdx = j
					break
				}
			}
		}
		if firstTokenMintIdx == -1 && strings.Contains(step, "uses: actions/create-github-app-token@") {
			firstTokenMintIdx = i
			// Walk backward to the token-mint step's list-item boundary ("- ").
			// If no boundary is found, keep the current index so insertion still
			// occurs before the token-mint uses-line.
			for j := i; j >= 0; j-- {
				trimmed := strings.TrimLeft(steps[j], " ")
				if strings.HasPrefix(trimmed, "- ") {
					firstTokenMintIdx = j
					break
				}
			}
		}
		if exactSetupStepIDPattern.MatchString(step) {
			lastSetupIdx = i
		}
	}
	return firstCheckoutIdx, firstTokenMintIdx, lastSetupIdx
}

func preStepInsertionIndex(steps []string, firstCheckoutIdx, firstTokenMintIdx, lastSetupIdx int) int {
	insertIdx := len(steps)
	if lastSetupIdx >= 0 {
		for i := lastSetupIdx + 1; i < len(steps); i++ {
			trimmed := strings.TrimLeft(steps[i], " ")
			if strings.HasPrefix(trimmed, "- ") {
				insertIdx = i
				break
			}
		}
		if insertIdx == len(steps) {
			compilerJobsLog.Print("No step boundary found after setup step; appending pre-steps at end")
		}
	} else if firstTokenMintIdx >= 0 {
		insertIdx = firstTokenMintIdx
		if firstCheckoutIdx >= 0 {
			if firstCheckoutIdx < insertIdx {
				insertIdx = firstCheckoutIdx
			}
		}
	} else if firstCheckoutIdx >= 0 {
		insertIdx = firstCheckoutIdx
	}
	return insertIdx
}

func (c *Compiler) extractPinnedJobSteps(fieldName string, jobName string, configMap map[string]any, data *WorkflowData) ([]string, error) {
	raw, hasField := configMap[fieldName]
	if !hasField {
		return nil, nil
	}

	stepsList, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("%s for job '%s' expects an array of step objects. Example: %s:\n  - run: echo hello", fieldName, jobName, fieldName)
	}

	pinnedSteps := make([]string, 0, len(stepsList))
	for i, step := range stepsList {
		stepMap, ok := step.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s for job '%s' has a step at index %d that is not an object. Expected each entry to be a step mapping. Example: %s:\n  - run: echo hello", fieldName, jobName, i, fieldName)
		}

		typedStep, err := MapToStep(stepMap)
		if err != nil {
			return nil, fmt.Errorf("%s entry for job '%s' could not be converted to a step: %w. Check that each step has valid fields such as run, uses, or with", fieldName, jobName, err)
		}

		pinnedStep, err := applyActionPinToTypedStep(typedStep, data)
		if err != nil {
			return nil, fmt.Errorf("action in %s for job '%s' could not be pinned: %w. Check that the 'uses' field references a valid action and version", fieldName, jobName, err)
		}
		finalStepMap := pinnedStep.ToMap()
		ensureCheckoutPersistCredentials(finalStepMap)
		sanitizedMap, warnings, _ := sanitizeRunStepExpressions(finalStepMap)
		for _, w := range warnings {
			compilerJobsLog.Printf("sanitized run: expression in job '%s' step: %s", jobName, w)
		}
		stepYAML, err := ConvertStepToYAML(sanitizedMap)
		if err != nil {
			return nil, fmt.Errorf("%s for job '%s' could not be converted to YAML: %w. Check that each step is a valid object", fieldName, jobName, err)
		}
		pinnedSteps = append(pinnedSteps, stepYAML)
	}

	return pinnedSteps, nil
}

// ensureCheckoutPersistCredentials enforces with.persist-credentials: false for
// actions/checkout steps when not explicitly configured by the user.
func ensureCheckoutPersistCredentials(stepMap map[string]any) {
	uses, ok := stepMap["uses"].(string)
	if !ok || !isCheckoutAction(uses) {
		return
	}

	withRaw, hasWith := stepMap["with"]
	if !hasWith || withRaw == nil {
		stepMap["with"] = map[string]any{
			"persist-credentials": false,
		}
		return
	}

	withMap, ok := withRaw.(map[string]any)
	if !ok {
		return
	}
	if v, exists := withMap["persist-credentials"]; exists && v != nil {
		return
	}
	withMap["persist-credentials"] = false
}

// isCheckoutAction reports whether a uses value points to actions/checkout,
// including either unpinned or version-pinned forms.
func isCheckoutAction(uses string) bool {
	trimmed := strings.Trim(strings.TrimSpace(uses), "\"'")
	return strings.EqualFold(trimmed, "actions/checkout") || strings.HasPrefix(strings.ToLower(trimmed), "actions/checkout@")
}

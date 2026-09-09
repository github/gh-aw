// Built-in job augmentation applies user configuration to compiler-generated jobs.
package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/typeutil"
)

func (c *Compiler) applyBuiltinJobPreSteps(data *WorkflowData) error {
	if data == nil || data.Jobs == nil {
		return nil
	}

	for jobName, jobConfig := range data.Jobs {
		configMap, ok := jobConfig.(map[string]any)
		if !ok {
			return fmt.Errorf("jobs.%s must be an object, got %T. Example: jobs:\n  job-name:\n    setup-steps: []", jobName, jobConfig)
		}

		_, hasSetupSteps := configMap["setup-steps"]
		_, hasPreSteps := configMap["pre-steps"]
		_, hasSteps := configMap["steps"]
		if err := validateRestrictedBuiltinSetupSteps(jobName, hasSetupSteps); err != nil {
			return err
		}
		if !hasSetupSteps && !hasPreSteps && !hasSteps {
			continue
		}

		targetJobName := jobName
		if jobName == "pre-activation" {
			targetJobName = string(constants.PreActivationJobName)
		}

		if err := validateRestrictedBuiltinSteps(jobName, targetJobName, hasSteps); err != nil {
			return err
		}

		job, exists := c.jobManager.GetJob(targetJobName)
		if !exists {
			continue
		}

		setupSteps, preSteps, regularSteps, err := c.extractBuiltinJobPreSteps(
			jobName, targetJobName, configMap, data, hasSetupSteps, hasPreSteps, hasSteps,
		)
		if err != nil {
			return err
		}
		if len(setupSteps) == 0 && len(preSteps) == 0 && len(regularSteps) == 0 {
			continue
		}

		job.Steps = insertActivationStepsBeforeArtifactStaging(targetJobName, job.Steps, regularSteps)
		job.Steps = insertPreStepsAtEarliestBoundary(job.Steps, preSteps)
		job.Steps = insertSetupStepsAtStart(job.Steps, setupSteps)
		compilerJobsLog.Printf("Inserted %d setup-step(s), %d pre-step(s), and %d step(s) into built-in job '%s'", len(setupSteps), len(preSteps), len(regularSteps), targetJobName)
	}

	return nil
}

func (c *Compiler) extractBuiltinJobPreSteps(jobName, targetJobName string, configMap map[string]any, data *WorkflowData, hasSetupSteps, hasPreSteps, hasSteps bool) ([]string, []string, []string, error) {
	setupSteps, err := c.extractOptionalBuiltinJobSteps("setup-steps", jobName, configMap, data, hasSetupSteps)
	if err != nil {
		return nil, nil, nil, err
	}
	preSteps, err := c.extractOptionalBuiltinJobSteps("pre-steps", jobName, configMap, data, hasPreSteps)
	if err != nil {
		return nil, nil, nil, err
	}
	regularSteps, err := c.extractOptionalBuiltinJobSteps("steps", jobName, configMap, data, hasSteps && targetJobName == string(constants.ActivationJobName))
	if err != nil {
		return nil, nil, nil, err
	}
	return setupSteps, preSteps, regularSteps, nil
}

func (c *Compiler) extractOptionalBuiltinJobSteps(field, jobName string, configMap map[string]any, data *WorkflowData, enabled bool) ([]string, error) {
	if !enabled {
		return nil, nil
	}
	steps, err := c.extractPinnedJobSteps(field, jobName, configMap, data)
	if err != nil {
		return nil, fmt.Errorf("%s for built-in job '%s' could not be processed: %w. Check that %s is an array of valid step objects", field, jobName, err, field)
	}
	return steps, nil
}

func insertActivationStepsBeforeArtifactStaging(jobName string, steps []string, activationSteps []string) []string {
	if len(activationSteps) == 0 {
		return steps
	}
	if jobName != string(constants.ActivationJobName) {
		return steps
	}

	insertIdx := len(steps)
	for i, step := range steps {
		if strings.Contains(step, "name: "+constants.ActivationStageAmbientFoldersStepName) ||
			strings.Contains(step, "name: "+constants.ActivationUploadArtifactStepName) {
			insertIdx = i
			break
		}
	}

	result := make([]string, 0, typeutil.SafeAllocationCapacity(len(steps), len(activationSteps)))
	result = append(result, steps[:insertIdx]...)
	result = append(result, activationSteps...)
	result = append(result, steps[insertIdx:]...)
	return result
}

func normalizeBuiltinJobAlias(jobName string) string {
	switch jobName {
	case string(constants.PreActivationHyphenJobName):
		return string(constants.PreActivationJobName)
	case string(constants.SafeOutputsHyphenJobName):
		return string(constants.SafeOutputsJobName)
	default:
		return jobName
	}
}

func extractBuiltinJobNeedsAugmentation(jobName string, configMap map[string]any) ([]string, error) {
	needsValue, exists := configMap["needs"]
	if !exists || needsValue == nil {
		return nil, nil
	}

	switch typedNeeds := needsValue.(type) {
	case string:
		return []string{typedNeeds}, nil
	case []any:
		needs := make([]string, 0, len(typedNeeds))
		for i, rawNeed := range typedNeeds {
			need, ok := rawNeed.(string)
			if !ok {
				return nil, fmt.Errorf("jobs.%s.needs[%d] must be a string, got %T. Example: needs: ['build', 'test']", jobName, i, rawNeed)
			}
			needs = append(needs, need)
		}
		return needs, nil
	default:
		return nil, fmt.Errorf("jobs.%s.needs expects a string or array of strings, got %T. Example: needs: [build, test]", jobName, needsValue)
	}
}

func extractBuiltinJobIfAugmentation(jobName string, configMap map[string]any) (string, error) {
	ifValue, exists := configMap["if"]
	if !exists || ifValue == nil {
		return "", nil
	}

	ifCondition, ok := ifValue.(string)
	if !ok {
		return "", fmt.Errorf("jobs.%s.if expects a string, got %T. Example: if: github.event_name == 'push'", jobName, ifValue)
	}

	// Strip "if: " prefix to match the Job.If contract (bare expression, no prefix).
	// This mirrors how custom jobs normalize their if fields via extractExpressionFromIfString.
	if strings.HasPrefix(ifCondition, "if: ") {
		ifCondition = strings.TrimSpace(ifCondition[4:])
	}

	return ifCondition, nil
}

// applyBuiltinJobAugmentations merges supported jobs.<built-in> fields into
// compiler-generated jobs. needs entries are added additively; if conditions are combined
// with compiler-generated conditions via logical AND.
func (c *Compiler) applyBuiltinJobAugmentations(data *WorkflowData) error {
	if data == nil || data.Jobs == nil {
		return nil
	}

	allJobs := c.jobManager.GetAllJobs()
	for configuredJobName, rawConfig := range data.Jobs {
		if err := c.applyBuiltinJobAugmentation(configuredJobName, rawConfig, data, allJobs); err != nil {
			return err
		}
	}
	return nil
}

type builtinJobAugmentation struct {
	needs          []string
	ifCondition    string
	hasPermissions bool
	hasTimeout     bool
}

func parseBuiltinJobAugmentation(jobName, targetJobName string, rawConfig any) (builtinJobAugmentation, map[string]any, error) {
	configMap, ok := rawConfig.(map[string]any)
	if !ok {
		return builtinJobAugmentation{}, nil, fmt.Errorf("jobs.%s expects an object, got %T. Example: jobs:\n  %s:\n    runs-on: ubuntu-latest", jobName, rawConfig, jobName)
	}
	needs, err := extractBuiltinJobNeedsAugmentation(jobName, configMap)
	if err != nil {
		return builtinJobAugmentation{}, nil, err
	}
	ifCondition, err := extractBuiltinJobIfAugmentation(jobName, configMap)
	if err != nil {
		return builtinJobAugmentation{}, nil, err
	}
	_, hasPermissions := configMap["permissions"]
	_, hasTimeout := configMap["timeout-minutes"]
	if hasTimeout && targetJobName != string(constants.AgentJobName) && targetJobName != string(constants.DetectionJobName) {
		return builtinJobAugmentation{}, nil, fmt.Errorf("jobs.%s.timeout-minutes is supported only for the generated agent and detection jobs", jobName)
	}
	return builtinJobAugmentation{needs, ifCondition, hasPermissions, hasTimeout}, configMap, nil
}

func (c *Compiler) applyBuiltinJobAugmentation(jobName string, rawConfig any, data *WorkflowData, allJobs map[string]*Job) error {
	targetJobName := normalizeBuiltinJobAlias(jobName)
	if !isBuiltinJobName(targetJobName) {
		return nil
	}
	augmentation, configMap, err := parseBuiltinJobAugmentation(jobName, targetJobName, rawConfig)
	if err != nil {
		return err
	}
	if len(augmentation.needs) == 0 && augmentation.ifCondition == "" && !augmentation.hasPermissions && !augmentation.hasTimeout {
		return nil
	}
	targetJob, exists := c.jobManager.GetJob(targetJobName)
	if !exists {
		return fmt.Errorf("jobs.%s requires an existing built-in job %q, but this workflow does not generate it. Add the corresponding trigger/feature, or rename the job", augmentedBuiltinJobField(jobName, targetJobName, augmentation), targetJobName)
	}
	if augmentation.hasPermissions {
		if err := applyBuiltinJobPermissionsAugmentation(jobName, targetJobName, configMap, targetJob); err != nil {
			return err
		}
	}
	if augmentation.hasTimeout {
		if err := extractCustomJobTimeoutMinutes(targetJob, jobName, configMap); err != nil {
			return err
		}
	}
	return c.applyBuiltinJobNeedsAndIf(jobName, targetJobName, targetJob, data.Jobs, allJobs, augmentation)
}

func augmentedBuiltinJobField(jobName, targetJobName string, augmentation builtinJobAugmentation) string {
	if len(augmentation.needs) > 0 && (augmentation.ifCondition != "" || augmentation.hasPermissions || augmentation.hasTimeout) {
		return jobName
	}
	if len(augmentation.needs) > 0 {
		return jobName + ".needs"
	}
	if augmentation.ifCondition != "" {
		return jobName + ".if"
	}
	if augmentation.hasTimeout {
		return jobName + ".timeout-minutes"
	}
	return targetJobName + ".permissions"
}

func (c *Compiler) applyBuiltinJobNeedsAndIf(jobName, targetJobName string, targetJob *Job, customJobs map[string]any, allJobs map[string]*Job, augmentation builtinJobAugmentation) error {
	normalizedNeeds, err := normalizeBuiltinJobAugmentationNeeds(jobName, targetJobName, augmentation.needs, allJobs)
	if err != nil {
		return err
	}
	compilerOwnedNeeds := selectCompilerOwnedNeeds(targetJob.Needs, customJobs)
	targetJob.Needs = mergeJobNeeds(targetJob.Needs, normalizedNeeds)
	if augmentation.ifCondition != "" {
		targetJob.If = c.combineJobIfConditions(targetJob.If, c.guardIfAgainstStatusFuncBypass(augmentation.ifCondition, compilerOwnedNeeds))
		compilerJobsLog.Printf("Applied jobs.%s.if augmentation to %q", jobName, targetJobName)
	}
	if len(normalizedNeeds) > 0 {
		compilerJobsLog.Printf("Applied jobs.%s.needs augmentation to %q: %v", jobName, targetJobName, normalizedNeeds)
	}
	return nil
}

func normalizeBuiltinJobAugmentationNeeds(jobName, targetJobName string, needs []string, allJobs map[string]*Job) ([]string, error) {
	normalized := make([]string, 0, len(needs))
	for _, rawNeed := range needs {
		need := normalizeBuiltinJobAlias(rawNeed)
		if need == targetJobName {
			return nil, fmt.Errorf("jobs.%s.needs lists %q, but a job should not depend on itself. Remove the self-reference from needs", jobName, rawNeed)
		}
		if _, known := allJobs[need]; !known {
			return nil, fmt.Errorf("jobs.%s.needs: unknown job %q. Expected a job defined in this workflow or a generated built-in job. Example:\njobs:\n  %s:\n    needs: [activation]", jobName, rawNeed, jobName)
		}
		normalized = append(normalized, need)
	}
	return normalized, nil
}

func mergeJobNeeds(existing, added []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(added))
	merged := make([]string, 0, len(existing)+len(added))
	for _, needs := range [][]string{existing, added} {
		for _, need := range needs {
			if _, alreadySeen := seen[need]; alreadySeen {
				continue
			}
			seen[need] = struct{}{}
			merged = append(merged, need)
		}
	}
	return merged
}

// applyBuiltinJobPermissionsAugmentation merges user-declared jobs.<built-in>.permissions
// into a compiler-generated built-in job (e.g. safe_outputs, conclusion). The merge is
// additive: the compiler-computed permissions are preserved and the user's declared scopes
// are added on top, with write overriding read. This ensures scopes such as id-token: write
// that authors declare under jobs.*.permissions are retained in the compiled lock file rather
// than being dropped by the minimal least-privilege permission computation.
func applyBuiltinJobPermissionsAugmentation(configuredJobName, targetJobName string, configMap map[string]any, targetJob *Job) error {
	permissionsValue, exists := configMap["permissions"]
	if !exists || permissionsValue == nil {
		return nil
	}

	userPermissions := NewPermissionsParserFromValue(permissionsValue).ToPermissions()
	if userPermissions == nil {
		return nil
	}

	// Start from the compiler-computed permissions already rendered on the job, then merge
	// the user-declared permissions additively so no compiler-required scope is lost.
	merged := NewPermissionsParser(targetJob.Permissions).ToPermissions()
	merged.Merge(userPermissions)
	targetJob.Permissions = merged.RenderToYAML()
	compilerJobsLog.Printf("Applied jobs.%s.permissions augmentation to %q", configuredJobName, targetJobName)
	return nil
}

// selectCompilerOwnedNeeds returns the prerequisites of a built-in job that the compiler owns,
// i.e. the needs that are not custom jobs declared under top-level `jobs:`. Custom jobs are
// auto-wired as prerequisites of built-in jobs but remain author-owned, so the author picks
// their result semantics (for example an `if: always()` agent that analyses a failing probe
// job). Compiler-owned prerequisites such as activation must stay guarded.
func selectCompilerOwnedNeeds(needs []string, customJobs map[string]any) []string {
	owned := make([]string, 0, len(needs))
	for _, need := range needs {
		if _, isCustomJob := customJobs[need]; isCustomJob && !isBuiltinJobName(need) {
			continue
		}
		owned = append(owned, need)
	}
	return owned
}

// guardIfAgainstStatusFuncBypass returns userCondition augmented with explicit
// needs.<need>.result == 'success' guards for each compiler-owned prerequisite, but only
// when userCondition contains a GitHub Actions status function (always, failure, cancelled,
// success).
//
// GitHub Actions removes the implicit success() check for ALL needs entries the moment any
// status function appears in a job's if expression. Compiler-owned prerequisites such as
// activation perform security and permission checks; they must always succeed before the
// target job runs. This function makes those guards explicit so user-supplied status functions
// cannot inadvertently (or intentionally) bypass them. User-supplied needs are intentionally
// excluded: authors choose their own result semantics for setup jobs they own.
func (c *Compiler) guardIfAgainstStatusFuncBypass(userCondition string, compilerNeeds []string) string {
	if len(compilerNeeds) == 0 {
		return userCondition
	}

	// Use string-based detection: the expression parser tokenises status function calls such as
	// failure() as single ExpressionNode literals, so AST-based containsStatusFunc cannot be used
	// here. A substring check is sufficient since GitHub Actions has a fixed, well-known set of
	// status functions and user-defined functions are not supported in the expression language.
	bare := stripExpressionWrapper(userCondition)
	if !ifExpressionContainsStatusFunc(bare) {
		return userCondition
	}

	// Build explicit success guards for each compiler-owned prerequisite.
	compilerJobsLog.Printf("Status function detected in user if condition; adding explicit success guards for compiler needs: %v", compilerNeeds)
	combined := ConditionNode(&ExpressionNode{Expression: bare})
	for _, need := range compilerNeeds {
		guard := &ExpressionNode{Expression: fmt.Sprintf("needs.%s.result == 'success'", need)}
		combined = BuildAnd(combined, guard)
	}
	return RenderCondition(combined)
}

// ifExpressionContainsStatusFunc reports whether the GitHub Actions expression string
// contains a call to any of the four status check functions (always, success, failure,
// cancelled). When present, GitHub Actions removes the implicit success() gate that would
// otherwise be applied to all needs entries.
func ifExpressionContainsStatusFunc(expr string) bool {
	return strings.Contains(expr, "always(") ||
		strings.Contains(expr, "success(") ||
		strings.Contains(expr, "failure(") ||
		strings.Contains(expr, "cancelled(")
}

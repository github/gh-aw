package workflow

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
)

var onNeedsValidationLog = logger.New("workflow:on_needs_validation")

var (
	onNeedsOutputExpressionPattern = regexp.MustCompile(`^\$\{\{\s*needs\.([A-Za-z_][A-Za-z0-9_-]*)\.outputs\.[A-Za-z_][A-Za-z0-9_-]*\s*\}\}$`)
	needsJobReferencePattern       = regexp.MustCompile(`\bneeds\.([A-Za-z_][A-Za-z0-9_-]*)\.`)
)

func (c *Compiler) validateOnNeeds(data *WorkflowData) error {
	if data == nil {
		return nil
	}

	onNeedsValidationLog.Printf("Validating on.needs: %d target(s), %d job(s)", len(data.OnNeeds), len(data.Jobs))

	if err := validateOnNeedsTargets(data); err != nil {
		return err
	}

	if err := c.validatePromptNeedsAvailableBeforeActivation(data); err != nil {
		return err
	}

	if err := c.validateOnNeedsDependencyChains(data); err != nil {
		return err
	}

	if err := c.validateOnGitHubAppNeedsExpressions(data); err != nil {
		return err
	}

	return nil
}

func (c *Compiler) validatePromptNeedsAvailableBeforeActivation(data *WorkflowData) error {
	if data == nil || len(data.Jobs) == 0 {
		return nil
	}

	allowed := c.activationDependencySetForPromptNeeds(data)
	for _, jobName := range extractNeedsJobReferencesFromPrompt(c.collectRuntimeImportMarkdownForCompilerAnalysis(data)) {
		if _, isCustomJob := data.Jobs[jobName]; !isCustomJob || setutil.Contains(allowed, jobName) {
			continue
		}
		return fmt.Errorf(
			"prompt references needs.%s.* but custom job %q is not available before activation. Add it to on.needs, remove its explicit needs so gh-aw can add it automatically, or avoid evaluating it during activation",
			jobName,
			jobName,
		)
	}
	return nil
}

func (c *Compiler) activationDependencySetForPromptNeeds(data *WorkflowData) map[string]struct{} {
	allowed := make(map[string]struct{})
	for _, jobName := range data.OnNeeds {
		allowed[jobName] = struct{}{}
	}
	for _, jobName := range c.getCustomJobsDependingOnPreActivation(data.Jobs) {
		allowed[jobName] = struct{}{}
	}
	for _, jobName := range c.getCustomJobsReferencedInPromptWithNoActivationDep(data) {
		allowed[jobName] = struct{}{}
	}
	return allowed
}

func extractNeedsJobReferencesFromPrompt(promptContent string) []string {
	matches := needsJobReferencePattern.FindAllStringSubmatch(promptContent, -1)
	seen := make(map[string]struct{})
	var jobNames []string
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		jobName := match[1]
		if setutil.Contains(seen, jobName) {
			continue
		}
		seen[jobName] = struct{}{}
		jobNames = append(jobNames, jobName)
	}
	sort.Strings(jobNames)
	return jobNames
}

func validateOnNeedsTargets(data *WorkflowData) error {
	if len(data.OnNeeds) == 0 {
		return nil
	}

	customJobs := make(map[string]struct {
	}, len(data.Jobs))
	for jobName := range data.Jobs {
		if isReservedOnNeedsTarget(jobName) {
			continue
		}
		customJobs[jobName] = struct {
		}{}
	}

	for _, need := range data.OnNeeds {
		if isReservedOnNeedsTarget(need) {
			return fmt.Errorf(
				"on.needs: built-in job %q is not allowed. Expected one of the workflow's custom jobs. Example: on.needs: [secrets_fetcher]",
				need,
			)
		}
		if !setutil.Contains(customJobs, need) {
			return fmt.Errorf(
				"on.needs: unknown job %q. Expected one of the workflow's custom jobs. Example: on.needs: [secrets_fetcher]",
				need,
			)
		}

		if jobConfig, ok := data.Jobs[need].(map[string]any); ok {
			if jobDependsOnActivation(jobConfig) || jobDependsOnPreActivation(jobConfig) {
				return fmt.Errorf(
					"on.needs: job %q cannot depend on activation/pre_activation because pre_activation and activation depend on on.needs jobs",
					need,
				)
			}
		}
	}

	onNeedsValidationLog.Printf("Validated %d on.needs dependency target(s)", len(data.OnNeeds))
	return nil
}

func (c *Compiler) validateOnGitHubAppNeedsExpressions(data *WorkflowData) error {
	if data == nil || data.ActivationGitHubApp == nil {
		return nil
	}

	allowed := make(map[string]struct {
	}, len(data.OnNeeds))
	for _, j := range data.OnNeeds {
		allowed[j] = struct {
		}{}
	}
	for _, j := range c.getCustomJobsDependingOnPreActivation(data.Jobs) {
		allowed[j] = struct {
		}{}
	}
	for _, j := range c.getCustomJobsReferencedInPromptWithNoActivationDep(data) {
		allowed[j] = struct {
		}{}
	}

	onNeedsValidationLog.Printf("Validating on.github-app needs expressions against %d allowed pre-activation job(s)", len(allowed))

	fields := map[string]string{
		"client-id":   data.ActivationGitHubApp.AppID,
		"private-key": data.ActivationGitHubApp.PrivateKey,
	}

	for fieldName, value := range fields {
		jobName, ok := extractNeedsJobFromOutputExpression(value)
		if !ok {
			continue
		}

		if isReservedOnNeedsTarget(jobName) {
			return fmt.Errorf("on.github-app.%s: built-in job %q is not allowed in needs expressions", fieldName, jobName)
		}
		if _, exists := data.Jobs[jobName]; !exists {
			return fmt.Errorf("on.github-app.%s: unknown job %q in needs expression", fieldName, jobName)
		}
		if !setutil.Contains(allowed, jobName) {
			return fmt.Errorf(
				"on.github-app.%s references needs.%s.outputs.* but job %q is not available before activation. Add it to on.needs (example: on.needs: [%s])",
				fieldName,
				jobName,
				jobName,
				jobName,
			)
		}
	}

	return nil
}

func (c *Compiler) validateOnNeedsDependencyChains(data *WorkflowData) error {
	if data == nil || len(data.OnNeeds) == 0 {
		return nil
	}

	onNeedsSet := make(map[string]struct {
	}, len(data.OnNeeds))
	for _, job := range data.OnNeeds {
		onNeedsSet[job] = struct {
		}{}
	}

	promptReferencedSet := make(map[string]struct {
	})
	for _, job := range c.getCustomJobsReferencedInPromptWithNoActivationDep(data) {
		promptReferencedSet[job] = struct {
		}{}
	}

	onNeedsValidationLog.Printf("Validating on.needs dependency chains for %d root job(s)", len(data.OnNeeds))

	visited := make(map[string]struct {
	}, len(data.Jobs))
	visiting := make(map[string]struct {
	}, len(data.Jobs))
	for _, root := range data.OnNeeds {
		if err := validateOnNeedsDependencyChain(root, root, data.Jobs, onNeedsSet, promptReferencedSet, visiting, visited); err != nil {
			return err
		}
	}

	return nil
}

func validateOnNeedsDependencyChain(
	root string,
	current string,
	allJobs map[string]any,
	onNeedsSet map[string]struct {
	}, promptReferencedSet map[string]struct {
	}, visiting map[string]struct {
	}, visited map[string]struct {
	}) error {
	if setutil.Contains(visited, current) {
		return nil
	}
	if setutil.Contains(visiting, current) {
		return fmt.Errorf("on.needs: cycle detected while validating dependency chain for %q", root)
	}

	jobConfigAny, exists := allJobs[current]
	if !exists {
		return nil
	}

	jobConfig, ok := jobConfigAny.(map[string]any)
	if !ok {
		return nil
	}

	visiting[current] = struct {
	}{}
	defer delete(visiting, current)

	for _, dep := range parseNeedsField(jobConfig["needs"]) {
		if isReservedOnNeedsTarget(dep) {
			return fmt.Errorf(
				"on.needs: job %q depends on built-in job %q. Dependencies for on.needs jobs must be custom jobs that run before activation",
				current,
				dep,
			)
		}

		depAny, depExists := allJobs[dep]
		if !depExists {
			continue
		}

		depConfig, ok := depAny.(map[string]any)
		if !ok {
			continue
		}

		_, depHasExplicitNeeds := depConfig["needs"]
		if !depHasExplicitNeeds && !setutil.Contains(onNeedsSet, dep) && !setutil.Contains(promptReferencedSet, dep) {
			return fmt.Errorf(
				"on.needs: job %q depends on %q, but %q has no explicit needs and is not in on.needs. It may get an implicit needs: activation and create a cycle. Add %q to on.needs or give %q explicit needs that run before activation",
				current,
				dep,
				dep,
				dep,
				dep,
			)
		}

		if err := validateOnNeedsDependencyChain(root, dep, allJobs, onNeedsSet, promptReferencedSet, visiting, visited); err != nil {
			return err
		}
	}

	visited[current] = struct {
	}{}
	return nil
}

func extractNeedsJobFromOutputExpression(value string) (string, bool) {
	match := onNeedsOutputExpressionPattern.FindStringSubmatch(strings.TrimSpace(value))
	if len(match) != 2 {
		return "", false
	}
	return match[1], true
}

func isReservedOnNeedsTarget(jobName string) bool {
	switch jobName {
	case string(constants.AgentJobName),
		string(constants.ActivationJobName),
		string(constants.PreActivationJobName),
		"pre-activation",
		string(constants.ConclusionJobName),
		string(constants.SafeOutputsJobName),
		"safe-outputs",
		string(constants.DetectionJobName),
		string(constants.UnlockJobName),
		"push_repo_memory",
		"update_cache_memory":
		return true
	default:
		return false
	}
}

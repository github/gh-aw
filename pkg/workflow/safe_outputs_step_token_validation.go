package workflow

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
)

var safeOutputsStepTokenValidationLog = logger.New("workflow:safe_outputs_step_token_validation")

var yamlAssignmentPattern = regexp.MustCompile(`^\s*[A-Za-z0-9_.-]+:\s*`)

// collectSafeOutputStepTokenIDs returns the set of step ids referenced by
// safe-outputs `github-token` expressions of the form `${{ steps.<id>.outputs.<name> }}`,
// covering the global token, mention credentials, and per-output overrides.
func collectSafeOutputStepTokenIDs(config *SafeOutputsConfig) map[string]struct{} {
	ids := make(map[string]struct{})
	if config == nil {
		return ids
	}

	collect := func(token string) {
		for _, path := range interpolationReferences(token) {
			if len(path) >= 4 && strings.EqualFold(path[0], "steps") && strings.EqualFold(path[2], "outputs") {
				ids[path[1]] = struct{}{}
			}
		}
	}

	collect(config.GitHubToken)
	if config.Mentions != nil {
		collect(config.Mentions.GitHubToken)
	}
	for _, handler := range safeOutputHandlers {
		if handler.StructField == "" {
			continue
		}
		if base := extractBaseSafeOutputConfig(config, handler.StructField); base != nil {
			collect(base.GitHubToken)
		}
	}

	return ids
}

// jobStepIDDeclarationIndex returns the byte offset of the first step declaring
// `id: <stepID>` in the rendered job, or -1 when no such step exists.
func jobStepIDDeclarationIndex(jobYAML string, stepID string) int {
	//nolint:regexpdynamicpattern // The step id is escaped with regexp.QuoteMeta before compilation.
	pattern := regexp.MustCompile(`(?m)^\s*(?:-\s+)?id:\s*["']?` + regexp.QuoteMeta(stepID) + `["']?\s*$`)
	loc := pattern.FindStringIndex(jobYAML)
	if loc == nil {
		return -1
	}
	return loc[0]
}

// jobStepOutputConsumptionIndex returns the byte offset of the first place where the
// rendered job assigns a `${{ steps.<stepID>.outputs.* }}` expression to a YAML key (for
// example `token:`, `github-token:` or an env variable), or -1 when the job does not
// consume the step output. Free-form occurrences inside run scripts or prompt text are
// ignored so documentation-like content does not trigger false positives.
func jobStepOutputConsumptionIndex(jobYAML string, stepID string) int {
	spans, _ := actionslanguageservice.ExtractExpressions(jobYAML)
	for _, span := range spans {
		lineStart := strings.LastIndex(jobYAML[:span.Start], "\n") + 1
		if !yamlAssignmentPattern.MatchString(jobYAML[lineStart:span.Start]) {
			continue
		}
		expression, err := actionslanguageservice.ParseSyntax(span.Content)
		if err != nil {
			continue
		}
		for _, path := range actionslanguageservice.References(expression) {
			if len(path) >= 4 && strings.EqualFold(path[0], "steps") && path[1] == stepID && strings.EqualFold(path[2], "outputs") {
				return lineStart
			}
		}
	}
	return -1
}

// stepMintingHint returns the frontmatter field an author should use to inject a
// token-minting step into the given job, ahead of every token consumer.
func stepMintingHint(jobName string, stepID string) string {
	if jobName == string(constants.AgentJobName) {
		return fmt.Sprintf("pre-steps:\n  - id: %s\n    uses: some-org/token-minting-action@v1", stepID)
	}
	return fmt.Sprintf("jobs:\n  %s:\n    pre-steps:\n      - id: %s\n        uses: some-org/token-minting-action@v1", jobName, stepID)
}

// validateSafeOutputStepTokenReferences verifies that every generated job consuming a
// same-job `steps.<id>.outputs.*` token expression from safe-outputs `github-token`
// also declares the step that produces it. GitHub Actions step outputs are only
// available inside the job that produced them, so an undeclared reference compiles to an
// empty value at runtime (and fails actionlint). Reporting this at compile time points
// authors at the `pre-steps` hook of the job that needs the minting step.
func (c *Compiler) validateSafeOutputStepTokenReferences(data *WorkflowData) error {
	if data == nil || data.SafeOutputs == nil {
		return nil
	}

	ids := collectSafeOutputStepTokenIDs(data.SafeOutputs)
	if len(ids) == 0 {
		return nil
	}

	sortedIDs := sliceutil.SortedKeys(ids)
	safeOutputsStepTokenValidationLog.Printf("Validating %d same-job step token reference(s)", len(sortedIDs))

	jobs := c.jobManager.GetAllJobs()
	for _, jobName := range sliceutil.SortedKeys(jobs) {
		jobYAML := strings.Join(jobs[jobName].Steps, "")
		for _, stepID := range sortedIDs {
			consumeIdx := jobStepOutputConsumptionIndex(jobYAML, stepID)
			if consumeIdx < 0 {
				continue
			}
			if jobName == string(constants.ConclusionJobName) {
				return NewValidationError(
					"safe-outputs.github-token",
					fmt.Sprintf("${{ steps.%s.outputs.* }}", stepID),
					"conclusion accepts compiler-generated steps only and cannot run a user-provided token-minting step",
					"Use a secrets.* token expression or compiler-managed safe-outputs.github-app authentication instead",
				)
			}
			declareIdx := jobStepIDDeclarationIndex(jobYAML, stepID)
			if declareIdx >= 0 && declareIdx < consumeIdx {
				continue
			}
			field := "safe-outputs.github-token"
			if data.SafeOutputs.Mentions != nil &&
				strings.Contains(data.SafeOutputs.Mentions.GitHubToken, "steps."+stepID+".outputs.") {
				field = "safe-outputs.mentions.github-token"
			}
			if declareIdx < 0 {
				safeOutputsStepTokenValidationLog.Printf("Job %q consumes steps.%s.outputs.* without declaring the step", jobName, stepID)
				return NewValidationError(
					field,
					fmt.Sprintf("${{ steps.%s.outputs.* }}", stepID),
					fmt.Sprintf("job %q has no step with id %q; step outputs are only available inside the job that produced them, so this token would be empty at runtime and requires the minting step to run in that job", jobName, stepID),
					fmt.Sprintf("Add the token-minting step to job %q:\n\n%s", jobName, stepMintingHint(jobName, stepID)),
				)
			}
			safeOutputsStepTokenValidationLog.Printf("Job %q consumes steps.%s.outputs.* before the step that declares it", jobName, stepID)
			return NewValidationError(
				field,
				fmt.Sprintf("${{ steps.%s.outputs.* }}", stepID),
				fmt.Sprintf("job %q runs the step with id %q after the first step that consumes the token, so the token would be empty at runtime and requires the minting step to run before its first consumer", jobName, stepID),
				fmt.Sprintf("Move the token-minting step earlier in job %q, for example using its pre-steps:\n\n%s", jobName, stepMintingHint(jobName, stepID)),
			)
		}
	}

	return nil
}

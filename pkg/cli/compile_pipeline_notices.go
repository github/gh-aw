package cli

import (
	"cmp"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/workflow"
)

func appendDuplicateWorkflowNameWarnings(workflowDataList []*workflow.WorkflowData, validationResultIndexes []int, validationResults *[]ValidationResult) ([]ValidationIssue, error) {
	workflowIDsByName := make(map[string][]string)
	for _, workflowData := range workflowDataList {
		if workflowData != nil && workflowData.Name != "" {
			workflowIDsByName[workflowData.Name] = append(workflowIDsByName[workflowData.Name], workflowData.WorkflowID)
		}
	}

	warningsByName := make(map[string]ValidationIssue)
	for name, workflowIDs := range workflowIDsByName {
		if len(workflowIDs) > 1 {
			slices.Sort(workflowIDs)
			warningsByName[name] = ValidationIssue{
				Type:    "duplicate_workflow_name",
				Message: fmt.Sprintf("Duplicate workflow name %q in %s; GitHub displays them as the same agentic workflow", name, strings.Join(workflowIDs, ", ")),
			}
		}
	}

	var warnings []ValidationIssue
	reportedNames := make(map[string]struct{})
	for workflowIndex, workflowData := range workflowDataList {
		if workflowData == nil || workflowData.Name == "" {
			continue
		}

		warning, duplicate := warningsByName[workflowData.Name]
		if !duplicate {
			continue
		}

		if _, reported := reportedNames[workflowData.Name]; !reported {
			warnings = append(warnings, warning)
			reportedNames[workflowData.Name] = struct{}{}
		}

		if workflowIndex < 0 || workflowIndex >= len(validationResultIndexes) {
			return nil, fmt.Errorf("missing validation result index for workflow %q", workflowData.WorkflowID)
		}
		resultIndex := validationResultIndexes[workflowIndex]
		results := *validationResults
		if resultIndex < 0 || resultIndex >= len(results) {
			return nil, fmt.Errorf("validation result index %d for workflow %q is out of range", resultIndex, workflowData.WorkflowID)
		}
		result := &results[resultIndex]
		result.Warnings = append(result.Warnings, warning)
	}

	return warnings, nil
}

func displayBatchCompilationNotices(compiler *workflow.Compiler, config CompileConfig) {
	if config.JSONOutput || config.Verbose {
		return
	}

	featureUsage := compiler.GetExperimentalFeatureUsage()
	if len(featureUsage) > 0 {
		type featureCount struct {
			name  string
			count int
		}
		features := make([]featureCount, 0, len(featureUsage))
		for message, count := range featureUsage {
			features = append(features, featureCount{
				name:  strings.TrimPrefix(message, "Using experimental feature: "),
				count: count,
			})
		}
		slices.SortFunc(features, func(a, b featureCount) int {
			if a.count != b.count {
				return cmp.Compare(b.count, a.count)
			}
			return cmp.Compare(a.name, b.name)
		})

		fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr("Experimental features in use:"))
		for _, feature := range features {
			fmt.Fprintln(os.Stderr, console.FormatListItemStderr(fmt.Sprintf("%s: %s", feature.name, formatWorkflowCount(feature.count))))
		}
	}

	if count := compiler.DefaultModelRoutingWorkflowCount(); count > 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(workflow.DefaultModelRoutingBatchNotice(count)))
	}

	if compiler.CopilotRequestsTipNeeded() {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(
			"Copilot token-based inference may be available: add permissions.copilot-requests: write. "+
				"To suppress this tip when using a PAT, set permissions.copilot-requests: none. "+
				"See https://github.github.com/gh-aw/reference/billing/",
		))
	}
}

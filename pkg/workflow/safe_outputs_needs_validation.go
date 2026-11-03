package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
)

var safeOutputsNeedsValidationLog = logger.New("workflow:safe_outputs_needs_validation")

func validateSafeOutputsNeeds(data *WorkflowData) error {
	if data == nil || data.SafeOutputs == nil {
		return nil
	}

	safeOutputsNeedsValidationLog.Printf("Validating safe-outputs needs: %d need(s) declared", len(data.SafeOutputs.Needs))

	if err := validateSafeOutputsNeedsField(data, "needs", data.SafeOutputs.Needs); err != nil {
		return err
	}

	return nil
}

func validateSafeOutputsNeedsField(data *WorkflowData, fieldName string, needs []string) error {
	if len(needs) == 0 {
		return nil
	}

	customJobs := make(map[string]struct {
	}, len(data.Jobs))
	for jobName := range data.Jobs {
		if isReservedOnNeedsTarget(jobName, data.Jobs) {
			continue
		}
		customJobs[jobName] = struct {
		}{}
	}
	safeOutputsNeedsValidationLog.Printf("Found %d custom job(s) available as needs targets", len(customJobs))

	for _, need := range needs {
		if isReservedOnNeedsTarget(need, data.Jobs) {
			safeOutputsNeedsValidationLog.Printf("Validation failed: %q is a reserved job name", need)
			return fmt.Errorf(
				"safe-outputs.%s: built-in job %q is not allowed. Expected one of the workflow's custom jobs. Example: safe-outputs.%s: [secrets_fetcher]",
				fieldName,
				need,
				fieldName,
			)
		}
		if !setutil.Contains(customJobs, need) {
			safeOutputsNeedsValidationLog.Printf("Validation failed: %q is not a known custom job", need)
			return fmt.Errorf(
				"safe-outputs.%s: unknown job %q. Expected one of the workflow's custom jobs. Example: safe-outputs.%s: [secrets_fetcher]",
				fieldName,
				need,
				fieldName,
			)
		}
	}

	safeOutputsNeedsValidationLog.Printf("Validated %d safe-outputs.%s dependency target(s)", len(needs), fieldName)
	return nil
}

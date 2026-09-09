package workflow

import (
	"fmt"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var frontmatterTriggerLog = logger.New("workflow:frontmatter_trigger_helpers")

// extractOnTriggerValue returns the raw value for on.<trigger> when the frontmatter
// contains an "on" map with that trigger configured.
func extractOnTriggerValue(frontmatter map[string]any, trigger string) (any, bool) {
	onMap, ok := frontmatter["on"].(map[string]any)
	if !ok {
		return nil, false
	}
	value, ok := onMap[trigger]
	return value, ok
}

// extractOnTriggerMap returns the on.<trigger> value as a map when the configured
// trigger uses object syntax.
func extractOnTriggerMap(frontmatter map[string]any, trigger string) (map[string]any, bool) {
	value, ok := extractOnTriggerValue(frontmatter, trigger)
	if !ok {
		return nil, false
	}
	triggerMap, ok := value.(map[string]any)
	return triggerMap, ok
}

// normalizeStringOrStringSlice converts a string or string-like array value into a
// []string, ignoring non-string array elements.
func normalizeStringOrStringSlice(raw any) []string {
	if s, ok := raw.(string); ok {
		return []string{s}
	}
	return parseStringSliceAny(raw, nil)
}

// validDeploymentStatusStates is the exhaustive list of state values that GitHub
// Actions emits for deployment_status events. Values outside this set are rejected at
// compile time to prevent expression injection (a raw value is interpolated directly
// into a GitHub Actions expression string).
var validDeploymentStatusStates = []string{
	"error",
	"failure",
	"inactive",
	"in_progress",
	"pending",
	"queued",
	"success",
	"waiting",
}

// isValidDeploymentStatusState reports whether v is a recognised deployment status state value.
func isValidDeploymentStatusState(v string) bool {
	return slices.Contains(validDeploymentStatusStates, v)
}

// extractDeploymentStatusStateCondition reads on.deployment_status.state and converts it
// into a GitHub Actions expression string (without ${{ }} wrappers). Returns "" if not set.
func extractDeploymentStatusStateCondition(frontmatter map[string]any) (string, error) {
	dsMap, ok := extractOnTriggerMap(frontmatter, "deployment_status")
	if !ok {
		return "", nil
	}
	stateValue, ok := dsMap["state"]
	if !ok {
		return "", nil
	}

	// GitHub Actions allows state as a single string or an array
	states := normalizeStringOrStringSlice(stateValue)

	if len(states) == 0 {
		return "", nil
	}

	frontmatterTriggerLog.Printf("Building deployment_status state condition from %d state value(s)", len(states))
	for _, s := range states {
		if !isValidDeploymentStatusState(s) {
			frontmatterTriggerLog.Printf("Rejecting invalid on.deployment_status.state value %q", s)
			return "", fmt.Errorf("invalid on.deployment_status.state value %q: must be one of %s",
				s, strings.Join(validDeploymentStatusStates, ", "))
		}
	}

	parts := make([]string, 0, len(states))
	for _, s := range states {
		parts = append(parts, "github.event.deployment_status.state == '"+s+"'")
	}
	stateExpr := strings.Join(parts, " || ")

	// Guard the state check with an event_name test so the condition remains true
	// when the workflow is triggered by other events (e.g. workflow_dispatch).
	// Without the guard, a non-deployment_status event would see the state as
	// empty/undefined and the entire activation condition would evaluate to false.
	return "github.event_name != 'deployment_status' || (" + stateExpr + ")", nil
}

// validWorkflowRunConclusions is the exhaustive list of conclusion values that GitHub
// Actions emits for workflow_run events.  Values outside this set are rejected at
// compile time to prevent expression injection (a raw value is interpolated directly
// into a GitHub Actions expression string).
var validWorkflowRunConclusions = []string{
	"success",
	"failure",
	"neutral",
	"cancelled",
	"skipped",
	"timed_out",
	"action_required",
	"stale",
	"startup_failure",
}

// isValidWorkflowRunConclusion reports whether v is a recognised conclusion value.
func isValidWorkflowRunConclusion(v string) bool {
	return slices.Contains(validWorkflowRunConclusions, v)
}

// extractWorkflowRunConclusionCondition reads on.workflow_run.conclusion and converts it
// into a GitHub Actions expression string (without ${{ }} wrappers). Returns "" if not set.
func extractWorkflowRunConclusionCondition(frontmatter map[string]any) (string, error) {
	wrMap, ok := extractOnTriggerMap(frontmatter, "workflow_run")
	if !ok {
		return "", nil
	}
	conclusionValue, ok := wrMap["conclusion"]
	if !ok {
		return "", nil
	}

	conclusions := normalizeStringOrStringSlice(conclusionValue)

	if len(conclusions) == 0 {
		return "", nil
	}

	frontmatterTriggerLog.Printf("Building workflow_run conclusion condition from %d conclusion value(s)", len(conclusions))
	for _, c := range conclusions {
		if !isValidWorkflowRunConclusion(c) {
			frontmatterTriggerLog.Printf("Rejecting invalid on.workflow_run.conclusion value %q", c)
			return "", fmt.Errorf("invalid on.workflow_run.conclusion value %q: must be one of %s",
				c, strings.Join(validWorkflowRunConclusions, ", "))
		}
	}

	parts := make([]string, 0, len(conclusions))
	for _, c := range conclusions {
		parts = append(parts, "github.event.workflow_run.conclusion == '"+c+"'")
	}
	conclusionExpr := strings.Join(parts, " || ")

	// Guard the conclusion check with an event_name test so the condition remains true
	// when the workflow is triggered by other events (e.g. workflow_dispatch).
	// Without the guard, a non-workflow_run event would see conclusion as
	// empty/undefined and the entire activation condition would evaluate to false.
	return "github.event_name != 'workflow_run' || (" + conclusionExpr + ")", nil
}

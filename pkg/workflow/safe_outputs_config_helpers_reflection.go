package workflow

import (
	"reflect"
	"sort"

	"github.com/github/gh-aw/pkg/logger"
)

var safeOutputReflectionLog = logger.New("workflow:safe_outputs_config_helpers_reflection")

// safeOutputFieldMapping maps struct field names to their tool names
var safeOutputFieldMapping = map[string]string{
	"CreateIssues":                    "create_issue",
	"CreateAgentSessions":             "create_agent_session",
	"CreateDiscussions":               "create_discussion",
	"UpdateDiscussions":               "update_discussion",
	"CloseDiscussions":                "close_discussion",
	"CloseIssues":                     "close_issue",
	"ClosePullRequests":               "close_pull_request",
	"AddComments":                     "add_comment",
	"CreatePullRequests":              "create_pull_request",
	"CreatePullRequestReviewComments": "create_pull_request_review_comment",
	"CreateCodeScanningAlerts":        "create_code_scanning_alert",
	"AddLabels":                       "add_labels",
	"RemoveLabels":                    "remove_labels",
	"AddReviewer":                     "add_reviewer",
	"AssignMilestone":                 "assign_milestone",
	"AssignToAgent":                   "assign_to_agent",
	"AssignToUser":                    "assign_to_user",
	"UpdateIssues":                    "update_issue",
	"UpdatePullRequests":              "update_pull_request",
	"PushToPullRequestBranch":         "push_to_pull_request_branch",
	"UploadAssets":                    "upload_asset",
	"UpdateRelease":                   "update_release",
	"UpdateProjects":                  "update_project",
	"CreateProjects":                  "create_project",
	"CreateProjectStatusUpdates":      "create_project_status_update",
	"LinkSubIssue":                    "link_sub_issue",
	"HideComment":                     "hide_comment",
	"DispatchWorkflow":                "dispatch_workflow",
	"MissingTool":                     "missing_tool",
	"NoOp":                            "noop",
	"MarkPullRequestAsReadyForReview": "mark_pull_request_as_ready_for_review",
}

// hasAnySafeOutputEnabled uses reflection to check if any safe output field is non-nil
func hasAnySafeOutputEnabled(safeOutputs *SafeOutputsConfig) bool {
	if safeOutputs == nil {
		return false
	}

	safeOutputReflectionLog.Print("Checking if any safe outputs are enabled using reflection")

	// Check Jobs separately as it's a map
	if len(safeOutputs.Jobs) > 0 {
		safeOutputReflectionLog.Printf("Found %d custom jobs enabled", len(safeOutputs.Jobs))
		return true
	}

	// Use reflection to check all pointer fields
	val := reflect.ValueOf(safeOutputs).Elem()
	for fieldName := range safeOutputFieldMapping {
		field := val.FieldByName(fieldName)
		if field.IsValid() && !field.IsNil() {
			safeOutputReflectionLog.Printf("Found enabled safe output field: %s", fieldName)
			return true
		}
	}

	safeOutputReflectionLog.Print("No safe outputs enabled")
	return false
}

// getEnabledSafeOutputToolNamesReflection uses reflection to get enabled tool names
func getEnabledSafeOutputToolNamesReflection(safeOutputs *SafeOutputsConfig) []string {
	if safeOutputs == nil {
		return nil
	}

	safeOutputReflectionLog.Print("Getting enabled safe output tool names using reflection")
	var tools []string

	// Use reflection to check all pointer fields
	val := reflect.ValueOf(safeOutputs).Elem()
	for fieldName, toolName := range safeOutputFieldMapping {
		field := val.FieldByName(fieldName)
		if field.IsValid() && !field.IsNil() {
			tools = append(tools, toolName)
		}
	}

	// Add custom job tools
	for jobName := range safeOutputs.Jobs {
		tools = append(tools, jobName)
		safeOutputReflectionLog.Printf("Added custom job tool: %s", jobName)
	}

	// Sort tools to ensure deterministic compilation
	sort.Strings(tools)

	safeOutputReflectionLog.Printf("Found %d enabled safe output tools", len(tools))
	return tools
}

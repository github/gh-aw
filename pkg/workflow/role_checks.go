package workflow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var roleLog = logger.New("workflow:role_checks")

// generateMembershipCheck generates steps for the check_membership job that only sets outputs
func (c *Compiler) generateMembershipCheck(data *WorkflowData, steps []string) []string {
	if len(data.Command) > 0 {
		steps = append(steps, "      - name: Check team membership for command workflow\n")
	} else {
		steps = append(steps, "      - name: Check team membership for workflow\n")
	}
	steps = append(steps, fmt.Sprintf("        id: %s\n", constants.CheckMembershipStepID))
	steps = append(steps, fmt.Sprintf("        uses: %s\n", GetActionPin("actions/github-script")))

	// Add environment variables for permission check
	steps = append(steps, "        env:\n")
	steps = append(steps, fmt.Sprintf("          GH_AW_REQUIRED_ROLES: %s\n", strings.Join(data.Roles, ",")))
	if len(data.Bots) > 0 {
		steps = append(steps, fmt.Sprintf("          GH_AW_ALLOWED_BOTS: %s\n", strings.Join(data.Bots, ",")))
	}

	steps = append(steps, "        with:\n")
	// Explicitly use the GitHub Actions token (GITHUB_TOKEN) for role membership checks
	// This ensures we only use the action token and not any other custom secrets
	steps = append(steps, "          github-token: ${{ secrets.GITHUB_TOKEN }}\n")
	steps = append(steps, "          script: |\n")
	steps = append(steps, generateGitHubScriptWithRequire("check_membership.cjs"))

	return steps
}

// generateRateLimitCheck generates steps for rate limiting check
func (c *Compiler) generateRateLimitCheck(data *WorkflowData, steps []string) []string {
	steps = append(steps, "      - name: Check user rate limit\n")
	steps = append(steps, fmt.Sprintf("        id: %s\n", constants.CheckRateLimitStepID))
	steps = append(steps, fmt.Sprintf("        uses: %s\n", GetActionPin("actions/github-script")))

	// Add environment variables for rate limit check
	steps = append(steps, "        env:\n")

	// Set max (default: 5)
	max := constants.DefaultRateLimitMax
	if data.RateLimit.Max > 0 {
		max = data.RateLimit.Max
	}
	steps = append(steps, fmt.Sprintf("          GH_AW_RATE_LIMIT_MAX: \"%d\"\n", max))

	// Set window (default: 60 minutes)
	window := constants.DefaultRateLimitWindow
	if data.RateLimit.Window > 0 {
		window = data.RateLimit.Window
	}
	steps = append(steps, fmt.Sprintf("          GH_AW_RATE_LIMIT_WINDOW: \"%d\"\n", window))

	// Set events to check (if specified)
	if len(data.RateLimit.Events) > 0 {
		// Sort events alphabetically for consistent output
		events := make([]string, len(data.RateLimit.Events))
		copy(events, data.RateLimit.Events)
		sort.Strings(events)
		steps = append(steps, fmt.Sprintf("          GH_AW_RATE_LIMIT_EVENTS: %q\n", strings.Join(events, ",")))
	}

	steps = append(steps, "        with:\n")
	steps = append(steps, "          github-token: ${{ secrets.GITHUB_TOKEN }}\n")
	steps = append(steps, "          script: |\n")
	steps = append(steps, generateGitHubScriptWithRequire("check_rate_limit.cjs"))

	return steps
}

// extractRoles extracts the 'roles' field from frontmatter to determine permission requirements
func (c *Compiler) extractRoles(frontmatter map[string]any) []string {
	if rolesValue, exists := frontmatter["roles"]; exists {
		switch v := rolesValue.(type) {
		case string:
			if v == "all" {
				// Special case: "all" means no restrictions
				roleLog.Print("Roles set to 'all' - no permission restrictions")
				return []string{"all"}
			}
			// Single permission level as string
			roleLog.Printf("Extracted single role: %s", v)
			return []string{v}
		case []any:
			// Array of permission levels
			var permissions []string
			for _, item := range v {
				if str, ok := item.(string); ok {
					permissions = append(permissions, str)
				}
			}
			roleLog.Printf("Extracted %d roles from array: %v", len(permissions), permissions)
			return permissions
		case []string:
			// Already a string slice
			roleLog.Printf("Extracted %d roles: %v", len(v), v)
			return v
		}
	}
	// Default: require admin, maintainer, or write permissions
	defaultRoles := []string{"admin", "maintainer", "write"}
	roleLog.Printf("No roles specified, using defaults: %v", defaultRoles)
	return defaultRoles
}

// extractBots extracts the 'bots' field from frontmatter to determine allowed bot identifiers
func (c *Compiler) extractBots(frontmatter map[string]any) []string {
	if botsValue, exists := frontmatter["bots"]; exists {
		switch v := botsValue.(type) {
		case []any:
			// Array of bot identifiers
			var bots []string
			for _, item := range v {
				if str, ok := item.(string); ok {
					bots = append(bots, str)
				}
			}
			roleLog.Printf("Extracted %d bot identifiers from array: %v", len(bots), bots)
			return bots
		case []string:
			// Already a string slice
			roleLog.Printf("Extracted %d bot identifiers: %v", len(v), v)
			return v
		case string:
			// Single bot identifier as string
			roleLog.Printf("Extracted single bot identifier: %s", v)
			return []string{v}
		}
	}
	// No bots specified, return empty array
	roleLog.Print("No bots specified")
	return []string{}
}

// extractRateLimitConfig extracts the 'rate-limit' field from frontmatter
func (c *Compiler) extractRateLimitConfig(frontmatter map[string]any) *RateLimitConfig {
	if rateLimitValue, exists := frontmatter["rate-limit"]; exists && rateLimitValue != nil {
		switch v := rateLimitValue.(type) {
		case map[string]any:
			config := &RateLimitConfig{}

			// Extract max (default: 5)
			if maxValue, ok := v["max"]; ok {
				switch max := maxValue.(type) {
				case int:
					config.Max = max
				case int64:
					config.Max = int(max)
				case uint64:
					config.Max = int(max)
				case float64:
					config.Max = int(max)
				}
			}

			// Extract window (default: 60 minutes)
			if windowValue, ok := v["window"]; ok {
				switch window := windowValue.(type) {
				case int:
					config.Window = window
				case int64:
					config.Window = int(window)
				case uint64:
					config.Window = int(window)
				case float64:
					config.Window = int(window)
				}
			}

			// Extract events
			if eventsValue, ok := v["events"]; ok {
				switch events := eventsValue.(type) {
				case []any:
					for _, item := range events {
						if str, ok := item.(string); ok {
							config.Events = append(config.Events, str)
						}
					}
				case []string:
					config.Events = events
				case string:
					config.Events = []string{events}
				}
			} else {
				// If events not specified, infer from the 'on:' section of frontmatter
				config.Events = c.inferEventsFromTriggers(frontmatter)
				if len(config.Events) > 0 {
					roleLog.Printf("Inferred events from workflow triggers: %v", config.Events)
				}
			}

			roleLog.Printf("Extracted rate-limit config: max=%d, window=%d, events=%v", config.Max, config.Window, config.Events)
			return config
		}
	}
	roleLog.Print("No rate-limit configuration specified")
	return nil
}

// inferEventsFromTriggers infers rate-limit events from the workflow's 'on:' triggers
func (c *Compiler) inferEventsFromTriggers(frontmatter map[string]any) []string {
	onValue, exists := frontmatter["on"]
	if !exists || onValue == nil {
		return nil
	}

	var events []string
	programmaticTriggers := map[string]string{
		"discussion":                  "discussion",
		"discussion_comment":          "discussion_comment",
		"issue_comment":               "issue_comment",
		"issues":                      "issues",
		"pull_request":                "pull_request",
		"pull_request_review":         "pull_request_review",
		"pull_request_review_comment": "pull_request_review_comment",
		"repository_dispatch":         "repository_dispatch",
		"workflow_dispatch":           "workflow_dispatch",
	}

	switch on := onValue.(type) {
	case map[string]any:
		for trigger := range on {
			if eventName, ok := programmaticTriggers[trigger]; ok {
				events = append(events, eventName)
			}
		}
	case []any:
		for _, item := range on {
			if triggerStr, ok := item.(string); ok {
				if eventName, ok := programmaticTriggers[triggerStr]; ok {
					events = append(events, eventName)
				}
			}
		}
	case string:
		if eventName, ok := programmaticTriggers[on]; ok {
			events = []string{eventName}
		}
	}

	// Sort events alphabetically for consistent output
	sort.Strings(events)
	return events
}

// needsRoleCheck determines if the workflow needs permission checks with full context
func (c *Compiler) needsRoleCheck(data *WorkflowData, frontmatter map[string]any) bool {
	// If user explicitly specified "roles: all", no permission checks needed
	if len(data.Roles) == 1 && data.Roles[0] == "all" {
		roleLog.Print("Role check not needed: roles set to 'all'")
		return false
	}

	// Command workflows always need permission checks
	if len(data.Command) > 0 {
		roleLog.Print("Role check needed: command workflow")
		return true
	}

	// Check if the workflow uses only safe events (only if frontmatter is available)
	if frontmatter != nil && c.hasSafeEventsOnly(data, frontmatter) {
		roleLog.Print("Role check not needed: workflow uses only safe events")
		return false
	}

	// Permission checks are needed by default for non-safe events
	roleLog.Print("Role check needed: workflow uses non-safe events")
	return true
}

// hasSafeEventsOnly checks if the workflow uses only safe events that don't require permission checks
func (c *Compiler) hasSafeEventsOnly(data *WorkflowData, frontmatter map[string]any) bool {
	// If user explicitly specified "roles: all", skip permission checks
	if len(data.Roles) == 1 && data.Roles[0] == "all" {
		return true
	}

	// Parse the "on" section to determine events
	if onValue, exists := frontmatter["on"]; exists {
		if onMap, ok := onValue.(map[string]any); ok {
			// Check if only safe events are present
			hasUnsafeEvents := false
			hasWorkflowDispatch := false

			for eventName := range onMap {
				// Skip command events as they are handled separately
				// Skip stop-after and reaction as they are not event types
				if eventName == "command" || eventName == "stop-after" || eventName == "reaction" {
					continue
				}

				// Track if workflow_dispatch is present
				if eventName == "workflow_dispatch" {
					hasWorkflowDispatch = true
				}

				// Check if this event is in the safe list
				isSafe := false
				for _, safeEvent := range constants.SafeWorkflowEvents {
					if eventName == safeEvent {
						isSafe = true
						break
					}
				}
				if !isSafe {
					hasUnsafeEvents = true
					break
				}
			}

			// If there are events and none are unsafe, then it's safe
			eventCount := len(onMap)
			// Subtract non-event entries
			if _, hasSlashCommand := onMap["slash_command"]; hasSlashCommand {
				eventCount--
			}
			if _, hasCommand := onMap["command"]; hasCommand {
				eventCount--
			}
			if _, hasStopAfter := onMap["stop-after"]; hasStopAfter {
				eventCount--
			}
			if _, hasReaction := onMap["reaction"]; hasReaction {
				eventCount--
			}

			// Special handling for workflow_dispatch:
			// workflow_dispatch can be triggered by users with "write" access,
			// so it's only considered "safe" if "write" is in the allowed roles
			if hasWorkflowDispatch && !hasUnsafeEvents {
				// Check if "write" is in the allowed roles
				hasWriteRole := false
				for _, role := range data.Roles {
					if role == "write" {
						hasWriteRole = true
						break
					}
				}
				// If write is not in the allowed roles, workflow_dispatch needs permission checks
				if !hasWriteRole {
					return false
				}
			}

			return eventCount > 0 && !hasUnsafeEvents
		}
	}

	// If no "on" section or it's a string, check for default command trigger
	// For command workflows, they are not considered "safe only"
	return false
}

// hasWorkflowRunTrigger checks if the agentic workflow's frontmatter declares a workflow_run trigger
func (c *Compiler) hasWorkflowRunTrigger(frontmatter map[string]any) bool {
	if frontmatter == nil {
		return false
	}

	// Check the "on" section in frontmatter
	if onValue, exists := frontmatter["on"]; exists {
		// Handle map format (most common)
		if onMap, ok := onValue.(map[string]any); ok {
			_, hasWorkflowRun := onMap["workflow_run"]
			return hasWorkflowRun
		}
		// Handle string format
		if onStr, ok := onValue.(string); ok {
			return onStr == "workflow_run"
		}
	}

	return false
}

// buildWorkflowRunRepoSafetyCondition generates the if condition to ensure workflow_run is from same repo and not a fork
// The condition uses: (event_name != 'workflow_run') OR (repository IDs match AND not from fork)
// This allows all non-workflow_run events, but requires repository match and fork check for workflow_run events
func (c *Compiler) buildWorkflowRunRepoSafetyCondition() string {
	// Check that event is NOT workflow_run
	eventNotWorkflowRun := BuildNotEquals(
		BuildPropertyAccess("github.event_name"),
		BuildStringLiteral("workflow_run"),
	)

	// Check that repository IDs match
	repoIDCheck := BuildEquals(
		BuildPropertyAccess("github.event.workflow_run.repository.id"),
		BuildPropertyAccess("github.repository_id"),
	)

	// Check that the triggering repository is NOT a fork
	notFromForkCheck := &NotNode{
		Child: BuildPropertyAccess("github.event.workflow_run.repository.fork"),
	}

	// Combine repository ID check AND not-from-fork check
	repoSafetyCheck := BuildAnd(repoIDCheck, notFromForkCheck)

	// Combine with OR: allow if NOT workflow_run OR (repository matches AND not fork)
	combinedCheck := BuildOr(eventNotWorkflowRun, repoSafetyCheck)

	// Wrap in ${{ }} for GitHub Actions
	return fmt.Sprintf("${{ %s }}", combinedCheck.Render())
}

// combineJobIfConditions combines an existing if condition with workflow_run repository safety check
// Returns the combined condition, or just one of them if the other is empty
func (c *Compiler) combineJobIfConditions(existingCondition, workflowRunRepoSafety string) string {
	// If no workflow_run safety check needed, return existing condition
	if workflowRunRepoSafety == "" {
		return existingCondition
	}

	// If no existing condition, return just the workflow_run safety check
	if existingCondition == "" {
		return workflowRunRepoSafety
	}

	// Both conditions present - combine them with AND
	// Strip ${{ }} wrapper from existingCondition if present
	unwrappedExisting := stripExpressionWrapper(existingCondition)
	unwrappedSafety := stripExpressionWrapper(workflowRunRepoSafety)

	existingNode := &ExpressionNode{Expression: unwrappedExisting}
	safetyNode := &ExpressionNode{Expression: unwrappedSafety}

	combinedExpr := BuildAnd(existingNode, safetyNode)
	return combinedExpr.Render()
}

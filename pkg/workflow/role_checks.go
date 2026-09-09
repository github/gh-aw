package workflow

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var roleLog = logger.New("workflow:role_checks")

// Keep in sync with actions/setup/js/check_rate_limit.cjs and the user-rate-limit.events schema enum.
var rateLimitProgrammaticEvents = []string{
	"discussion",
	"discussion_comment",
	"issue_comment",
	"issues",
	"pull_request",
	"pull_request_review",
	"pull_request_review_comment",
	"repository_dispatch",
	"workflow_dispatch",
}

// generateMembershipCheck generates steps for the check_membership job that only sets outputs
func (c *Compiler) generateMembershipCheck(data *WorkflowData, steps []string) []string {
	if len(data.Command) > 0 {
		steps = append(steps, "      - name: Check team membership for command workflow\n")
	} else {
		steps = append(steps, "      - name: Check team membership for workflow\n")
	}
	steps = append(steps, fmt.Sprintf("        id: %s\n", constants.CheckMembershipStepID))
	// For command workflows the command position check runs first (before this step).
	// Only run the membership check when the command was actually triggered to avoid
	// a confusing "access denied" warning on every non-slash-command activation.
	if len(data.Command) > 0 {
		steps = append(steps, fmt.Sprintf("        if: steps.%s.outputs.%s == 'true'\n", constants.CheckCommandPositionStepID, constants.CommandPositionOkOutput))
	}
	steps = append(steps, fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)))

	// Add environment variables for permission check
	steps = append(steps, "        env:\n")
	steps = append(steps, fmt.Sprintf("          GH_AW_REQUIRED_ROLES: %q\n", strings.Join(data.Roles, ",")))
	if len(data.Bots) > 0 {
		steps = append(steps, fmt.Sprintf("          GH_AW_ALLOWED_BOTS: %q\n", strings.Join(data.Bots, ",")))
	}
	if data.AllowBotAuthoredTriggerComment {
		steps = append(steps, "          GH_AW_ALLOW_BOT_AUTHORED_TRIGGER_COMMENT: \"true\"\n")
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
	steps = append(steps, fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)))

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

	// Set ignored roles (if specified)
	if len(data.RateLimit.IgnoredRoles) > 0 {
		// Sort roles alphabetically for consistent output
		ignoredRoles := make([]string, len(data.RateLimit.IgnoredRoles))
		copy(ignoredRoles, data.RateLimit.IgnoredRoles)
		sort.Strings(ignoredRoles)
		steps = append(steps, fmt.Sprintf("          GH_AW_RATE_LIMIT_IGNORED_ROLES: %q\n", strings.Join(ignoredRoles, ",")))
	}

	steps = append(steps, "        with:\n")
	steps = append(steps, "          github-token: ${{ secrets.GITHUB_TOKEN }}\n")
	steps = append(steps, "          script: |\n")
	steps = append(steps, generateGitHubScriptWithRequire("check_rate_limit.cjs"))

	return steps
}

// extractLabelNames extracts the 'labels' field from frontmatter.
// When set, the pre-activation job emits a job-level if: condition that skips the workflow
// (gray ⊘ rather than red ❌) when the triggering label does not match.
func (c *Compiler) extractLabelNames(frontmatter map[string]any) []string {
	if onValue, exists := frontmatter["on"]; exists {
		if onMap, ok := onValue.(map[string]any); ok {
			if labelNamesValue, hasLabelNames := onMap["labels"]; hasLabelNames {
				names := parseOptionalStringSliceField(labelNamesValue, "on.labels")
				if len(names) > 0 {
					roleLog.Printf("Extracted %d labels: %v", len(names), names)
					return names
				}
			}
		}
	}
	return nil
}

// extractRoles extracts the 'roles' field from frontmatter to determine permission requirements
func (c *Compiler) extractRoles(frontmatter map[string]any) []string {
	// Check on.roles
	if onValue, exists := frontmatter["on"]; exists {
		if onMap, ok := onValue.(map[string]any); ok {
			if rolesValue, hasRoles := onMap["roles"]; hasRoles {
				roles := parseRolesValue(rolesValue, "on.roles")
				if roles != nil {
					return roles
				}
			}
		}
	}

	// Default: require admin, maintainer, or write permissions
	defaultRoles := []string{"admin", "maintainer", "write"}
	roleLog.Printf("No roles specified, using defaults: %v", defaultRoles)
	return defaultRoles
}

// parseRolesValue parses a roles value from frontmatter (supports string, []any, []string)
func parseRolesValue(rolesValue any, fieldName string) []string {
	switch v := rolesValue.(type) {
	case string:
		if v == "all" {
			// Special case: "all" means no restrictions
			roleLog.Printf("Roles in '%s' set to 'all' - no permission restrictions", fieldName)
			return []string{"all"}
		}
		// Single permission level as string
		roleLog.Printf("Extracted single role from '%s': %s", fieldName, v)
		return []string{v}
	case []any:
		// Array of permission levels
		permissions := parseStringSliceAny(v, roleLog)
		roleLog.Printf("Extracted %d roles from '%s' array: %v", len(permissions), fieldName, permissions)
		return permissions
	case []string:
		// Already a string slice
		roleLog.Printf("Extracted %d roles from '%s': %v", len(v), fieldName, v)
		return v
	}
	return nil
}

// extractBots extracts the 'bots' field from frontmatter to determine allowed bot identifiers
func (c *Compiler) extractBots(frontmatter map[string]any) []string {
	// Check on.bots
	if onValue, exists := frontmatter["on"]; exists {
		if onMap, ok := onValue.(map[string]any); ok {
			if botsValue, hasBots := onMap["bots"]; hasBots {
				bots := parseBotsValue(botsValue, "on.bots")
				if bots != nil {
					return bots
				}
			}
		}
	}

	// No bots specified, return empty array
	roleLog.Print("No bots specified")
	return []string{}
}

// parseBotsValue parses a bots value from frontmatter (supports string, []any, []string)
func parseBotsValue(botsValue any, fieldName string) []string {
	return parseOptionalStringSliceField(botsValue, fieldName)
}

// parseOptionalStringSliceField parses string-list fields that accept either
// a single string or an array. Empty strings are filtered out.
func parseOptionalStringSliceField(value any, fieldName string) []string {
	if singleValue, ok := value.(string); ok {
		if singleValue == "" {
			return nil
		}
		roleLog.Printf("Extracted single %s: %s", fieldName, singleValue)
		return []string{singleValue}
	}

	values := parseStringSliceAny(value, roleLog)
	if len(values) == 0 {
		roleLog.Printf("No valid %s found or unsupported type: %T", fieldName, value)
		return nil
	}

	result := make([]string, 0, len(values))
	for _, item := range values {
		if item != "" {
			result = append(result, item)
		}
	}
	if len(result) == 0 {
		return nil
	}

	roleLog.Printf("Extracted %d %s: %v", len(result), fieldName, result)
	return result
}

// extractRateLimitConfig extracts the user-rate-limit config from frontmatter.
func (c *Compiler) extractRateLimitConfig(frontmatter map[string]any) *RateLimitConfig {
	rateLimitValue, exists := frontmatter["user-rate-limit"]
	if !exists || rateLimitValue == nil {
		roleLog.Print("No user-rate-limit configuration specified")
		return nil
	}

	rateLimitMap, ok := rateLimitValue.(map[string]any)
	if !ok {
		roleLog.Printf("user-rate-limit value is not an object, ignoring configuration: %T", rateLimitValue)
		return nil
	}

	config := &RateLimitConfig{
		Max:          extractRateLimitInt(rateLimitMap, "max-runs-per-window"),
		Window:       extractRateLimitInt(rateLimitMap, "window"),
		Events:       c.extractRateLimitEvents(rateLimitMap, frontmatter),
		IgnoredRoles: extractRateLimitIgnoredRoles(rateLimitMap),
	}

	roleLog.Printf("Extracted user-rate-limit config: max=%d, window=%d, events=%v, ignored-roles=%v", config.Max, config.Window, config.Events, config.IgnoredRoles)
	return config
}

func extractRateLimitInt(config map[string]any, keys ...string) int {
	for _, key := range keys {
		if value, ok := config[key]; ok {
			switch typedValue := value.(type) {
			case int:
				return typedValue
			case int64:
				return int(typedValue)
			case uint64:
				return int(typedValue)
			case float64:
				return int(typedValue)
			}
		}
	}
	return 0
}

func (c *Compiler) extractRateLimitEvents(config map[string]any, frontmatter map[string]any) []string {
	if eventsValue, ok := config["events"]; ok {
		return extractRateLimitStringSlice(eventsValue)
	}

	events := c.inferEventsFromTriggers(frontmatter)
	if len(events) > 0 {
		roleLog.Printf("Inferred events from workflow triggers: %v", events)
	}
	return events
}

func extractRateLimitIgnoredRoles(config map[string]any) []string {
	if ignoredRolesValue, ok := config["ignored-roles"]; ok {
		return extractRateLimitStringSlice(ignoredRolesValue)
	}

	roleLog.Print("No ignored-roles specified, using defaults: admin, maintain, write")
	return []string{"admin", "maintain", "write"}
}

func extractRateLimitStringSlice(value any) []string {
	switch typedValue := value.(type) {
	case []any:
		return parseStringSliceAny(typedValue, nil)
	case []string:
		return typedValue
	case string:
		return []string{typedValue}
	}
	return nil
}

// inferEventsFromTriggers infers rate-limit events from the workflow's 'on:' triggers
func (c *Compiler) inferEventsFromTriggers(frontmatter map[string]any) []string {
	onValue, exists := frontmatter["on"]
	if !exists || onValue == nil {
		return append([]string(nil), rateLimitProgrammaticEvents...)
	}

	var events []string
	programmaticTriggers := make(map[string]string, len(rateLimitProgrammaticEvents))
	for _, event := range rateLimitProgrammaticEvents {
		programmaticTriggers[event] = event
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
	if len(events) == 0 {
		// If "on" exists but has no supported rate-limit triggers, keep the omitted-events fallback broad.
		return append([]string(nil), rateLimitProgrammaticEvents...)
	}
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
			return hasOnlySafeOnMapEvents(onMap, data.Roles)
		}
	}

	// If no "on" section or it's a string, check for default command trigger
	// For command workflows, they are not considered "safe only"
	return false
}

func hasOnlySafeOnMapEvents(onMap map[string]any, roles []string) bool {
	hasUnsafeEvents := false
	hasWorkflowDispatch := false

	for eventName := range onMap {
		if isRoleCheckConfigKey(eventName) {
			continue
		}
		if eventName == "workflow_dispatch" {
			hasWorkflowDispatch = true
		}
		if !slices.Contains(constants.SafeWorkflowEvents, eventName) {
			hasUnsafeEvents = true
			break
		}
	}

	if hasWorkflowDispatch && !hasUnsafeEvents && !slices.Contains(roles, "write") {
		return false
	}
	return countWorkflowEvents(onMap) > 0 && !hasUnsafeEvents
}

func isRoleCheckConfigKey(key string) bool {
	switch key {
	case "allow-bot-authored-trigger-comment", "bots", "command", "cooldown", "labels", "reaction", "roles", "stop-after":
		return true
	default:
		return false
	}
}

func countWorkflowEvents(onMap map[string]any) int {
	eventCount := 0
	for eventName := range onMap {
		// slash_command is not a standalone GitHub event, but the safety scan still treats it as unsafe.
		if !isRoleCheckConfigKey(eventName) && eventName != "slash_command" {
			eventCount++
		}
	}
	return eventCount
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
	return RenderCondition(combinedExpr)
}

// extractSkipField extracts a named field from the 'on:' section of frontmatter.
// Returns nil if the field is not configured.
func extractSkipField(frontmatter map[string]any, fieldName string) []string {
	onValue, exists := frontmatter["on"]
	if !exists || onValue == nil {
		return nil
	}
	if on, ok := onValue.(map[string]any); ok {
		if val, exists := on[fieldName]; exists && val != nil {
			return parseOptionalStringSliceField(val, fieldName)
		}
	}
	return nil
}

// extractSkipRoles extracts the 'skip-roles' field from the 'on:' section of frontmatter
// Returns nil if skip-roles is not configured
func (c *Compiler) extractSkipRoles(frontmatter map[string]any) []string {
	return extractSkipField(frontmatter, "skip-roles")
}

// extractSkipBots extracts the 'skip-bots' field from the 'on:' section of frontmatter
// Returns nil if skip-bots is not configured
func (c *Compiler) extractSkipBots(frontmatter map[string]any) []string {
	return extractSkipField(frontmatter, "skip-bots")
}

// extractSkipAuthorAssociations extracts the 'skip-author-associations' field from the 'on:' section.
// The field is an object keyed by event name with values as a string or string array.
func (c *Compiler) extractSkipAuthorAssociations(frontmatter map[string]any) map[string][]string {
	onValue, exists := frontmatter["on"]
	if !exists || onValue == nil {
		return nil
	}

	onMap, ok := onValue.(map[string]any)
	if !ok {
		return nil
	}

	rawValue, exists := onMap["skip-author-associations"]
	if !exists || rawValue == nil {
		return nil
	}

	rawMap, ok := rawValue.(map[string]any)
	if !ok {
		return nil
	}

	result := make(map[string][]string)
	for eventName, associationsValue := range rawMap {
		associations := parseOptionalStringSliceField(associationsValue, "on.skip-author-associations."+eventName)
		if len(associations) == 0 {
			continue
		}
		normalizedAssociations := make([]string, 0, len(associations))
		for _, association := range associations {
			normalized := strings.ToUpper(strings.TrimSpace(association))
			if normalized != "" { //nolint:tolowerequalfold
				normalizedAssociations = append(normalizedAssociations, normalized)
			}
		}
		if len(normalizedAssociations) == 0 {
			continue
		}
		result[eventName] = sliceutil.Deduplicate(normalizedAssociations)
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// extractAllowBotAuthoredTriggerComment extracts the 'allow-bot-authored-trigger-comment' boolean
// from the 'on:' section of frontmatter. When true, the confused-deputy mismatch check is skipped
// for issue_comment:edited events where the comment was authored by a bot — the bot-posted-menu /
// user-checks-box pattern. Returns false if the field is absent or not a boolean true.
func (c *Compiler) extractAllowBotAuthoredTriggerComment(frontmatter map[string]any) bool {
	if onValue, exists := frontmatter["on"]; exists {
		if onMap, ok := onValue.(map[string]any); ok {
			if val, exists := onMap["allow-bot-authored-trigger-comment"]; exists {
				if b, ok := val.(bool); ok {
					return b
				}
			}
		}
	}
	return false
}

// mergeUniqueLogged merges top-level and imported string slices using a union (deduplication preserving
// order), logging the result at debug level under the given label.
func mergeUniqueLogged(label string, top []string, imported []string) []string {
	result := sliceutil.MergeUnique(top, imported...)
	if len(result) > 0 {
		roleLog.Printf("Merged %s: %v (top=%d, imported=%d, total=%d)", label, result, len(top), len(imported), len(result))
	}
	return result
}

// mergeSkipRoles merges top-level skip-roles with imported skip-roles (union)
func mergeSkipRoles(topSkipRoles []string, importedSkipRoles []string) []string {
	return mergeUniqueLogged("skip-roles", topSkipRoles, importedSkipRoles)
}

// mergeSkipBots merges top-level skip-bots with imported skip-bots (union)
func mergeSkipBots(topSkipBots []string, importedSkipBots []string) []string {
	return mergeUniqueLogged("skip-bots", topSkipBots, importedSkipBots)
}

// mergeBots merges top-level bots with imported bots (union)
func mergeBots(topBots []string, importedBots []string) []string {
	return mergeUniqueLogged("bots", topBots, importedBots)
}

// extractActivationGitHubToken extracts the 'github-token' field from the 'on:' section of frontmatter.
// This token is used for pre-activation reactions and activation status comments.
func (c *Compiler) extractActivationGitHubToken(frontmatter map[string]any) string {
	if onValue, exists := frontmatter["on"]; exists {
		if onMap, ok := onValue.(map[string]any); ok {
			if tokenValue, hasToken := onMap["github-token"]; hasToken {
				if tokenStr, ok := tokenValue.(string); ok {
					roleLog.Printf("Extracted activation github-token from on section")
					return tokenStr
				}
			}
		}
	}
	return ""
}

// extractActivationGitHubApp extracts the 'github-app' field from the 'on:' section of frontmatter.
// When configured, a GitHub App installation access token is minted for use in reactions and status comments.
func (c *Compiler) extractActivationGitHubApp(frontmatter map[string]any) *GitHubAppConfig {
	if onValue, exists := frontmatter["on"]; exists {
		if onMap, ok := onValue.(map[string]any); ok {
			if appValue, hasApp := onMap["github-app"]; hasApp {
				if appMap, ok := appValue.(map[string]any); ok {
					roleLog.Printf("Extracted activation github-app from on section")
					return parseAppConfig(appMap)
				}
			}
		}
	}
	return nil
}

// resolveActivationGitHubToken returns the GitHub token to use for activation operations
// (reactions, status comments, skip-if checks). Precedence:
//  1. Current workflow's on.github-token (explicit override wins)
//  2. First on.github-token found across imported shared workflows
//  3. Empty string (use default GITHUB_TOKEN)
func (c *Compiler) resolveActivationGitHubToken(frontmatter map[string]any, importsResult *parser.ImportsResult) string {
	if token := c.extractActivationGitHubToken(frontmatter); token != "" {
		return token
	}
	if importsResult != nil && importsResult.MergedActivationGitHubToken != "" {
		roleLog.Print("Using on.github-token from imported shared workflow")
		return importsResult.MergedActivationGitHubToken
	}
	return ""
}

// resolveActivationGitHubApp returns the GitHub App config to use for activation operations
// (reactions, status comments, skip-if checks). Precedence:
//  1. Current workflow's on.github-app (explicit override wins)
//  2. First on.github-app found across imported shared workflows
//  3. Nil (use default GITHUB_TOKEN)
func (c *Compiler) resolveActivationGitHubApp(frontmatter map[string]any, importsResult *parser.ImportsResult) *GitHubAppConfig {
	if app := c.extractActivationGitHubApp(frontmatter); app != nil {
		return app
	}
	if importsResult != nil && importsResult.MergedActivationGitHubApp != "" {
		var appMap map[string]any
		if err := json.Unmarshal([]byte(importsResult.MergedActivationGitHubApp), &appMap); err == nil {
			app := parseAppConfig(appMap)
			if app.AppID != "" && app.PrivateKey != "" {
				roleLog.Print("Using on.github-app from imported shared workflow")
				return app
			}
		}
	}
	return nil
}

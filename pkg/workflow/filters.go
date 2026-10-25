package workflow

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var filtersLog = logger.New("workflow:filters")

// applyPullRequestDraftFilter applies draft filter conditions for pull_request triggers
func (c *Compiler) applyPullRequestDraftFilter(data *WorkflowData, frontmatter map[string]any) {
	filtersLog.Print("Applying pull request draft filter")

	prMap, hasPRConfig := pullRequestFilterConfig(data, frontmatter)
	if !hasPRConfig {
		return
	}

	// Check if draft is specified
	draftValue, hasDraft := prMap["draft"]
	if !hasDraft {
		return
	}

	// Check if draft is a boolean
	draftBool, isDraftBool := draftValue.(bool)
	if !isDraftBool {
		// If draft is not a boolean, don't add filter
		return
	}

	filtersLog.Printf("Found draft filter configuration: draft=%v", draftBool)

	// Generate conditional logic based on draft value using expression nodes
	var draftCondition ConditionNode
	if draftBool {
		// draft: true - include only draft PRs
		// The condition should be true for non-pull_request events or for draft pull_requests
		notPullRequestEvent := BuildNotEquals(
			BuildPropertyAccess("github.event_name"),
			BuildStringLiteral("pull_request"),
		)
		isDraftPR := BuildEquals(
			BuildPropertyAccess("github.event.pull_request.draft"),
			BuildBooleanLiteral(true),
		)
		draftCondition = &OrNode{
			Left:  notPullRequestEvent,
			Right: isDraftPR,
		}
	} else {
		// draft: false - exclude draft PRs
		// The condition should be true for non-pull_request events or for non-draft pull_requests
		notPullRequestEvent := BuildNotEquals(
			BuildPropertyAccess("github.event_name"),
			BuildStringLiteral("pull_request"),
		)
		isNotDraftPR := BuildEquals(
			BuildPropertyAccess("github.event.pull_request.draft"),
			BuildBooleanLiteral(false),
		)
		draftCondition = &OrNode{
			Left:  notPullRequestEvent,
			Right: isNotDraftPR,
		}
	}

	// Build condition tree and render
	existingCondition := data.If
	conditionTree := BuildConditionTree(existingCondition, draftCondition.Render())
	data.If = RenderCondition(conditionTree)
}

// applyPullRequestForkFilter applies fork filter conditions for pull_request triggers
// Supports repository patterns or an explicit author-association list.
// Default behavior: When forks field is not specified, only same-repo PRs are allowed (forks are disallowed by default)
func (c *Compiler) applyPullRequestForkFilter(data *WorkflowData, frontmatter map[string]any) {
	filtersLog.Print("Applying pull request fork filter")
	prMap, hasPRConfig := pullRequestFilterConfig(data, frontmatter)
	if !hasPRConfig {
		return
	}
	forksValue, hasForks := prMap["forks"]
	allowedForks, allowedAssociations, valid := parsePullRequestForkFilter(forksValue, hasForks)
	if !valid {
		return
	}
	if slices.Contains(allowedForks, "*") {
		filtersLog.Print("Wildcard fork pattern detected, allowing all forks")
		return
	}
	notPullRequestEvent := BuildNotEquals(
		BuildPropertyAccess("github.event_name"),
		BuildStringLiteral("pull_request"),
	)
	allowedForksCondition := BuildFromAllowedForks(allowedForks)
	if len(allowedAssociations) > 0 {
		associationsJSON, _ := json.Marshal(allowedAssociations) //nolint:jsonmarshalignoredeerror // marshaling a string slice cannot fail
		allowedForksCondition = BuildOr(
			allowedForksCondition,
			BuildFunctionCall(
				"contains",
				BuildFunctionCall("fromJSON", BuildStringLiteral(string(associationsJSON))),
				BuildPropertyAccess("github.event.pull_request.author_association"),
			),
		)
	}
	forkCondition := &OrNode{Left: notPullRequestEvent, Right: allowedForksCondition}
	conditionTree := BuildConditionTree(data.If, forkCondition.Render())
	data.If = RenderCondition(conditionTree)
}

func pullRequestFilterConfig(data *WorkflowData, frontmatter map[string]any) (map[string]any, bool) {
	// Use cached On field from ParsedFrontmatter if available, otherwise fall back to map access
	var onValue any
	var hasOn bool
	if data.ParsedFrontmatter != nil && data.ParsedFrontmatter.On != nil {
		onValue = data.ParsedFrontmatter.On
		hasOn = true
	} else {
		onValue, hasOn = frontmatter["on"]
	}

	// Check if there's an "on" section in the frontmatter
	if !hasOn {
		return nil, false
	}

	// Check if "on" is an object (not a string)
	onMap, isOnMap := onValue.(map[string]any)
	if !isOnMap {
		return nil, false
	}

	// Check if there's a pull_request section
	prValue, hasPR := onMap["pull_request"]
	if !hasPR {
		return nil, false
	}

	// Check if pull_request is an object with fork settings
	prMap, isPRMap := prValue.(map[string]any)
	if !isPRMap {
		return nil, false
	}
	return prMap, true
}

func parsePullRequestForkFilter(forksValue any, hasForks bool) ([]string, []string, bool) {
	// Default behavior: If forks field is not specified, only allow same-repo PRs (disallow all forks by default)
	var allowedForks []string
	var allowedAssociations []string
	if !hasForks {
		filtersLog.Print("No forks field specified - applying default fork filter (disallow all forks)")
		// Empty allowedForks array means only same-repo PRs are allowed
		allowedForks = []string{}
	} else {
		filtersLog.Print("Found forks filter configuration")

		// Parse repository patterns or author associations.
		// Handle string format (e.g., forks: "*" or forks: "org/*")
		if forksStr, isForksStr := forksValue.(string); isForksStr {
			allowedForks = []string{forksStr}
		} else if forksArray, isForksArray := forksValue.([]any); isForksArray {
			// Handle array format (e.g., forks: ["*", "org/repo"])
			for _, fork := range forksArray {
				if forkStr, isForkStr := fork.(string); isForkStr {
					allowedForks = append(allowedForks, forkStr)
				}
			}
		} else if forksMap, isForksMap := forksValue.(map[string]any); isForksMap {
			allowedAssociations = parseStringSliceAny(forksMap["author-association"], filtersLog)
		} else {
			// Invalid forks format, skip
			return nil, nil, false
		}
	}

	return allowedForks, allowedAssociations, true
}

// applyPullRequestStackFilter applies stacked pull request protection.
// Default behavior: run only for the latest PR in a stack (max-stack = 1).
// Set on.pull_request.max-stack or on.pull_request_review.max-stack to -1 to disable this protection.
func (c *Compiler) applyPullRequestStackFilter(data *WorkflowData, frontmatter map[string]any) {
	filtersLog.Print("Applying pull request stack filter")

	onValue, hasOn := frontmatter["on"]
	if !hasOn || !hasStackFilterTrigger(onValue) {
		return
	}

	maxStack := 1
	if configuredMaxStack, ok := extractStackMaxStack(onValue); ok {
		maxStack = configuredMaxStack
	}

	if maxStack == -1 {
		filtersLog.Print("Pull request stack filter disabled via max-stack: -1")
		return
	}

	if maxStack == 1 {
		// For max-stack: 1 (the default), use a job-level if: condition with the supported
		// equality operator. This gates the entire job (shows as "skipped") for non-top PRs.
		// GitHub Actions expressions do not support arithmetic (+, -, etc.), so we use
		// position == size to check that this PR is at the top of the stack.
		stackCondition := "(github.event_name != 'pull_request' && github.event_name != 'pull_request_review') || github.event.pull_request.stack == null || github.event.pull_request.stack.position == github.event.pull_request.stack.size"

		existingCondition := data.If
		conditionTree := BuildConditionTree(existingCondition, stackCondition)
		data.If = RenderCondition(conditionTree)
	} else {
		// For max-stack: N > 1, GitHub Actions expressions do not support arithmetic
		// operators (+, -, etc.), so we inject a PreStep that uses bash arithmetic instead.
		// The step exits 1 if this PR is not in the top N layers, stopping all subsequent
		// default-condition steps from running (they use if: success() by default).
		stackGateStep := fmt.Sprintf(
			"- name: \"Stack position gate (max-stack: %d)\"\n"+
				"  if: (github.event_name == 'pull_request' || github.event_name == 'pull_request_review') && github.event.pull_request.stack != null\n"+
				"  env:\n"+
				"    STACK_POSITION: ${{ github.event.pull_request.stack.position }}\n"+
				"    STACK_SIZE: ${{ github.event.pull_request.stack.size }}\n"+
				"  run: |\n"+
				"    max_stack=%d\n"+
				"    if (( STACK_POSITION + max_stack <= STACK_SIZE )); then\n"+
				"      printf '## Stack gate\\n\\nRun skipped: stack position %%s is not in the top %%s of %%s.\\n' \\\n"+
				"        \"$STACK_POSITION\" \"$max_stack\" \"$STACK_SIZE\" >> \"$GITHUB_STEP_SUMMARY\"\n"+
				"      exit 1\n"+
				"    fi\n",
			maxStack, maxStack,
		)

		if data.GeneratedPreSteps == "" {
			data.GeneratedPreSteps = "pre-steps:\n" + stackGateStep
		} else {
			data.GeneratedPreSteps = strings.TrimRight(data.GeneratedPreSteps, "\n") + "\n" + stackGateStep
		}
	}
}

func hasStackFilterTrigger(onValue any) bool {
	switch on := onValue.(type) {
	case string:
		return on == "pull_request" || on == "pull_request_review"
	case []any:
		for _, item := range on {
			if item == "pull_request" || item == "pull_request_review" {
				return true
			}
			if eventMap, ok := item.(map[string]any); ok {
				if _, exists := eventMap["pull_request"]; exists {
					return true
				}
				if _, exists := eventMap["pull_request_review"]; exists {
					return true
				}
			}
		}
	case map[string]any:
		if _, exists := on["pull_request"]; exists {
			return true
		}
		if _, exists := on["pull_request_review"]; exists {
			return true
		}
	}
	return false
}

func extractStackMaxStack(onValue any) (int, bool) {
	switch on := onValue.(type) {
	case map[string]any:
		return extractStackMaxStackFromMap(on)
	case []any:
		for _, item := range on {
			eventMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if maxStack, ok := extractStackMaxStackFromMap(eventMap); ok {
				return maxStack, true
			}
		}
	}

	return 0, false
}

func extractStackMaxStackFromMap(onMap map[string]any) (int, bool) {
	for _, triggerName := range []string{"pull_request", "pull_request_review"} {
		triggerValue, hasTrigger := onMap[triggerName]
		if !hasTrigger {
			continue
		}
		triggerMap, ok := triggerValue.(map[string]any)
		if !ok {
			continue
		}
		maxStackValue, hasMaxStack := triggerMap["max-stack"]
		if !hasMaxStack {
			continue
		}
		switch v := maxStackValue.(type) {
		case int:
			return v, true
		case int64:
			return int(v), true
		case uint:
			return int(v), true
		case uint64:
			return int(v), true
		case float64:
			if v == float64(int(v)) {
				return int(v), true
			}
		}
	}

	return 0, false
}

// applyLabelFilter applies label name filter conditions for labeled/unlabeled triggers
// Supports "names: []string" to filter which label changes trigger the workflow
func (c *Compiler) applyLabelFilter(data *WorkflowData, frontmatter map[string]any) {
	filtersLog.Print("Applying label filter")

	// Use cached On field from ParsedFrontmatter if available, otherwise fall back to map access
	var onValue any
	var hasOn bool
	if data.ParsedFrontmatter != nil && data.ParsedFrontmatter.On != nil {
		onValue = data.ParsedFrontmatter.On
		hasOn = true
	} else {
		onValue, hasOn = frontmatter["on"]
	}

	// Check if there's an "on" section in the frontmatter
	if !hasOn {
		return
	}

	// Check if "on" is an object (not a string)
	onMap, isOnMap := onValue.(map[string]any)
	if !isOnMap {
		return
	}

	// Check both issues, pull_request, and discussion sections for labeled/unlabeled with names
	eventSections := []struct {
		eventName  string
		eventValue any
	}{
		{"issues", onMap["issues"]},
		{"pull_request", onMap["pull_request"]},
		{"discussion", onMap["discussion"]},
	}

	var labelConditions []ConditionNode

	for _, section := range eventSections {
		if condition := labelEventSectionCondition(section.eventName, section.eventValue); condition != nil {
			labelConditions = append(labelConditions, condition)
		}
	}
	applyLabelConditions(data, labelConditions)
}

func labelEventSectionCondition(eventName string, eventValue any) ConditionNode {
	if eventValue == nil {
		return nil
	}
	// Check if the section is an object with types and names
	sectionMap, isSectionMap := eventValue.(map[string]any)
	if !isSectionMap {
		return nil
	}

	// Check for "types" field
	typesValue, hasTypes := sectionMap["types"]
	if !hasTypes {
		return nil
	}

	// Convert types to []string
	types := labelFilterStringArray(typesValue)

	// Check if types includes "labeled" or "unlabeled"
	hasLabeled := false
	hasUnlabeled := false
	for _, t := range types {
		if t == "labeled" {
			hasLabeled = true
		}
		if t == "unlabeled" {
			hasUnlabeled = true
		}
	}

	if !hasLabeled && !hasUnlabeled {
		return nil
	}

	// Check if this section uses native GitHub Actions label filtering
	// (indicated by __gh_aw_native_label_filter__ marker)
	if nativeFilterValue, hasNativeFilter := sectionMap["__gh_aw_native_label_filter__"]; hasNativeFilter {
		if usesNativeFilter, ok := nativeFilterValue.(bool); ok && usesNativeFilter {
			// Skip applying job condition filtering for this section
			// as it uses native GitHub Actions label filtering
			filtersLog.Printf("Skipping label filter for %s: using native GitHub Actions label filtering", eventName)
			return nil
		}
	}

	// Check for "names" field
	namesValue, hasNames := sectionMap["names"]
	if !hasNames {
		return nil
	}

	labelNames := labelFilterNames(namesValue)
	if len(labelNames) == 0 {
		return nil
	}
	return buildLabelEventCondition(eventName, labelNames, hasLabeled, hasUnlabeled)
}

func labelFilterNames(namesValue any) []string {
	if namesStr, isNamesStr := namesValue.(string); isNamesStr {
		return []string{namesStr}
	} else if namesArray, isNamesArray := namesValue.([]any); isNamesArray {
		return labelFilterStringArray(namesArray)
	}
	return nil
}

func labelFilterStringArray(value any) []string {
	var values []string
	if array, ok := value.([]any); ok {
		for _, item := range array {
			if text, ok := item.(string); ok {
				values = append(values, text)
			}
		}
	}
	return values
}

func buildLabelEventCondition(eventName string, labelNames []string, hasLabeled, hasUnlabeled bool) ConditionNode {
	// Build condition for this event section
	// The condition should be:
	// (event_name != 'issues' OR action != 'labeled' OR label.name in names) AND
	// (event_name != 'issues' OR action != 'unlabeled' OR label.name in names)

	// For each label name, create a condition
	var labelNameConditions []ConditionNode
	for _, labelName := range labelNames {
		labelNameConditions = append(labelNameConditions, BuildEquals(
			BuildPropertyAccess("github.event.label.name"),
			BuildStringLiteral(labelName),
		))
	}

	// Combine label name conditions with OR
	var labelNameMatch ConditionNode
	if len(labelNameConditions) == 1 {
		labelNameMatch = labelNameConditions[0]
	} else {
		labelNameMatch = &DisjunctionNode{Terms: labelNameConditions}
	}

	return labelActionCondition(eventName, labelNameMatch, hasLabeled, hasUnlabeled)
}

func labelActionCondition(eventName string, labelNameMatch ConditionNode, hasLabeled, hasUnlabeled bool) ConditionNode {
	if !hasLabeled && !hasUnlabeled {
		return nil
	}
	notThisEvent := BuildNotEquals(
		BuildPropertyAccess("github.event_name"), BuildStringLiteral(eventName))
	var notAction ConditionNode
	if hasLabeled && hasUnlabeled {
		notAction = &AndNode{
			Left:  BuildNotEquals(BuildPropertyAccess("github.event.action"), BuildStringLiteral("labeled")),
			Right: BuildNotEquals(BuildPropertyAccess("github.event.action"), BuildStringLiteral("unlabeled")),
		}
	} else if hasLabeled {
		notAction = BuildNotEquals(BuildPropertyAccess("github.event.action"), BuildStringLiteral("labeled"))
	} else {
		notAction = BuildNotEquals(BuildPropertyAccess("github.event.action"), BuildStringLiteral("unlabeled"))
	}
	return &OrNode{Left: notThisEvent, Right: &OrNode{Left: notAction, Right: labelNameMatch}}
}

func applyLabelConditions(data *WorkflowData, labelConditions []ConditionNode) {
	// If we have label conditions, combine them and apply to the workflow
	if len(labelConditions) > 0 {
		filtersLog.Printf("Applying label name filters: %d conditions found", len(labelConditions))
		var finalCondition ConditionNode
		if len(labelConditions) == 1 {
			finalCondition = labelConditions[0]
		} else {
			// Combine all conditions with AND
			finalCondition = labelConditions[0]
			for i := 1; i < len(labelConditions); i++ {
				finalCondition = &AndNode{
					Left:  finalCondition,
					Right: labelConditions[i],
				}
			}
		}

		// Build condition tree and render
		existingCondition := data.If
		conditionTree := BuildConditionTree(existingCondition, finalCondition.Render())
		data.If = RenderCondition(conditionTree)
	}
}

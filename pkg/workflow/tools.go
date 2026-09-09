package workflow

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/typeutil"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
	"github.com/goccy/go-yaml"
)

var toolsLog = logger.New("workflow:tools")

// applyDefaults applies default values for missing workflow sections
func (c *Compiler) applyDefaults(data *WorkflowData, markdownPath string) error {
	toolsLog.Printf("Applying defaults to workflow: name=%s, path=%s", data.Name, markdownPath)

	// Populate cached values after all mutations have been applied.
	// applyDefaults is the final stage that mutates data.Permissions, so the values
	// computed here represent the stable final state. Using defer ensures the cache
	// is always set on every return path, including early returns.
	defer populateWorkflowDataCache(data)

	isCommandTrigger, isLabelCommandTrigger := detectTriggerType(data, markdownPath)

	if data.On == "" {
		if err := c.applyDefaultOnSection(data, isCommandTrigger, isLabelCommandTrigger); err != nil {
			return err
		}
	}

	if c.trialMode && c.hasIssueTrigger(data.On) {
		data.On = c.injectWorkflowDispatchForIssue(data.On)
	}

	data.Concurrency = GenerateConcurrencyConfig(data, isCommandTrigger || isLabelCommandTrigger)

	if data.RunName == "" {
		data.RunName = fmt.Sprintf(`run-name: "%s"`, data.Name)
	}

	if data.TimeoutMinutes == "" {
		defaultTimeoutMinutes := int(constants.DefaultAgenticWorkflowTimeout / time.Minute)
		data.TimeoutMinutes = "timeout-minutes: " + compilerenv.BuildTimeoutMinutesExpression(compilerenv.DefaultTimeoutMinutes, defaultTimeoutMinutes)
	}

	if data.RunsOn == "" {
		data.RunsOn = "runs-on: ubuntu-latest"
	}
	// Preserve explicit false intent before default-tool resolution mutates or
	// removes entries. Runtime integrations use this alongside the effective
	// post-default tools map to distinguish absent, disabled, and enabled tools.
	if err := prepareToolsForDefaults(data); err != nil {
		return err
	}

	// Capture whether tools.bash was explicitly set to false before default-tool resolution
	// removes the "bash" key entirely (unless overridden by required git commands). This lets
	// us distinguish "bash explicitly refused" from "bash never configured", which both end up
	// with an absent "bash" key after applyDefaultTools.
	bashExplicitlyFalse := isToolExplicitlyFalse(data.Tools["bash"])
	data.Tools = c.applyDefaultTools(data.Tools, data.SafeOutputs, data.SandboxConfig, data.NetworkPermissions)
	data.BashDisabled = isBashFullyDisabled(data.Tools, bashExplicitlyFalse)
	data.ParsedTools = NewTools(data.Tools)

	// Explicitly empty permissions ({}) means user wants no permissions — do not apply defaults.
	if data.Permissions == "permissions: {}" {
		return nil
	}
	applyDefaultPermissions(data)
	return nil
}

func prepareToolsForDefaults(data *WorkflowData) error {
	data.ExplicitlyDisabledTools = collectExplicitlyDisabledTools(data.Tools)
	return expandJiraToolConfig(data.Tools)
}

func isToolExplicitlyFalse(value any) bool {
	enabled, ok := value.(bool)
	return ok && !enabled
}

func collectExplicitlyDisabledTools(tools map[string]any) map[string]struct{} {
	disabled := make(map[string]struct{})
	for name, value := range tools {
		if enabled, ok := value.(bool); ok && !enabled {
			disabled[name] = struct{}{}
		}
	}
	return disabled
}

// populateWorkflowDataCache pre-computes and stores cached values derived from data.Permissions,
// data.Concurrency, and data.ParsedTools so hot validation paths can skip repeated parsing.
func populateWorkflowDataCache(data *WorkflowData) {
	data.CachedPermissions = NewPermissionsParser(data.Permissions).ToPermissions()
	data.CachedPermissionScopeNamesErr = ValidatePermissionScopeNames(data.Permissions)
	data.CachedPermissionScopeNamesSet = true
	data.ConcurrencyGroupExpr = extractConcurrencyGroupFromYAML(data.Concurrency)
	// Pre-validate and cache the concurrency group expression so validateWorkflowData
	// can short-circuit without re-running the expensive ExpressionParser on every call.
	// CachedConcurrencyGroupExprSet is always true after applyDefaults regardless of whether
	// a group expression exists, so callers can distinguish "already computed" from "not yet computed".
	if data.ConcurrencyGroupExpr != "" {
		data.CachedConcurrencyGroupExprErr = validateConcurrencyGroupExpression(data.ConcurrencyGroupExpr)
	}
	data.CachedConcurrencyGroupExprSet = true
	// Cache the expanded + parsed toolsets for the GitHub tool so both
	// ValidatePermissions and validateToolConfiguration reuse one result.
	if data.ParsedTools != nil && data.ParsedTools.GitHub != nil {
		data.CachedParsedToolsets = ParseGitHubToolsets(data.ParsedTools.GitHub.GetToolsets())
	}
}

// detectTriggerType checks whether the workflow is a slash_command/command or label_command trigger
// by inspecting already-parsed WorkflowData fields and, if needed, reading the frontmatter from disk.
// It is intentionally a free function (not a *Compiler method) because it reads only WorkflowData
// and on-disk frontmatter and has no dependency on Compiler state.
func detectTriggerType(data *WorkflowData, markdownPath string) (isCommand, isLabelCommand bool) {
	if data.On != "" {
		return false, false
	}
	if len(data.Command) > 0 {
		return true, false
	}
	if len(data.LabelCommand) > 0 {
		return false, true
	}
	// Fall back to reading the frontmatter file
	content, err := os.ReadFile(markdownPath)
	if err != nil {
		return false, false
	}
	result, err := parser.ExtractFrontmatterFromContent(string(content))
	if err != nil {
		return false, false
	}
	onValue, exists := result.Frontmatter["on"]
	if !exists {
		return false, false
	}
	onMap, ok := onValue.(map[string]any)
	if !ok {
		return false, false
	}
	if _, has := onMap["slash_command"]; has {
		return true, false
	}
	if _, has := onMap["command"]; has {
		return true, false
	}
	if _, has := onMap["label_command"]; has {
		return false, true
	}
	return false, false
}

// applyDefaultOnSection sets data.On when it is empty by dispatching to the appropriate
// trigger handler (command, label-command, or the catch-all polling schedule).
func (c *Compiler) applyDefaultOnSection(data *WorkflowData, isCommand, isLabelCommand bool) error {
	if isCommand {
		return c.applyCommandTriggerOnSection(data)
	}
	if isLabelCommand {
		return c.applyLabelCommandTriggerOnSection(data)
	}
	data.On = `on:
  # Start either every 10 minutes, or when some kind of human event occurs.
  # Because of the implicit "concurrency" section, only one instance of this
  # workflow will run at a time.
  schedule:
    - cron: "0/10 * * * *"
  issues:
    types: [opened, edited, closed]
  issue_comment:
    types: [created, edited]
  pull_request:
    types: [opened, edited, closed]
  push:
    branches:
      - main
  workflow_dispatch:`
	return nil
}

// applyDefaultPermissions sets the default contents:read permission when no permissions are specified.
func applyDefaultPermissions(data *WorkflowData) {
	if data.Permissions != "" {
		return
	}
	perms := NewPermissionsContentsRead()
	yaml := perms.RenderToYAML()
	// RenderToYAML uses job-friendly indentation (6 spaces). WorkflowData.Permissions
	// is stored in workflow-level indentation (2 spaces) and later re-indented for jobs.
	lines := strings.Split(yaml, "\n")
	for i := 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "      ") {
			lines[i] = "  " + lines[i][6:]
		}
	}
	data.Permissions = strings.Join(lines, "\n")
}

// applyCommandTriggerOnSection generates the "on:" YAML section and job-level "if" condition
// for slash_command (and deprecated command) trigger workflows.
func (c *Compiler) applyCommandTriggerOnSection(data *WorkflowData) error {
	toolsLog.Print("Workflow is command trigger, configuring command events")

	commandEventsMap, err := c.buildCommandTriggerEventsMap(data)
	if err != nil {
		return err
	}

	// Convert merged events to YAML
	mergedEventsYAML, err := yaml.MarshalWithOptions(map[string]any{"on": commandEventsMap}, yaml.IndentSequence(true))
	if err != nil {
		return fmt.Errorf("failed to marshal command events: %w", err)
	}
	yamlStr := strings.TrimSuffix(string(mergedEventsYAML), "\n")
	yamlStr = parser.QuoteCronExpressions(yamlStr)
	yamlStr = c.commentOutProcessedFieldsInOnSection(yamlStr, map[string]any{})
	data.On = yamlStr

	// Add conditional logic for command workflows unless centralized mode is enabled.
	if !data.CommandCentralized {
		hasOtherEvents := len(data.CommandOtherEvents) > 0
		commandConditionTree, err := buildEventAwareCommandCondition(data.Command, data.CommandEvents, hasOtherEvents)
		if err != nil {
			return fmt.Errorf("failed to build command condition: %w", err)
		}

		derivedConditionTree := commandConditionTree
		if len(data.LabelCommand) > 0 {
			labelConditionTree, err := buildLabelCommandCondition(data.LabelCommand, data.LabelCommandEvents, false)
			if err != nil {
				return fmt.Errorf("failed to build combined label-command condition: %w", err)
			}
			derivedConditionTree = &OrNode{Left: commandConditionTree, Right: labelConditionTree}
		}
		data.If = RenderCondition(BuildConditionTree(data.If, derivedConditionTree.Render()))
	} else if len(data.LabelCommand) > 0 {
		// Centralized command mode: label checks for dispatches derive from aw_context metadata.
		labelConditionTree, err := buildDispatchLabelCommandCondition(data.LabelCommand, data.LabelCommandEvents)
		if err != nil {
			return fmt.Errorf("failed to build label-command condition: %w", err)
		}
		data.If = RenderCondition(BuildConditionTree(data.If, labelConditionTree.Render()))
	}
	return nil
}

// buildCommandTriggerEventsMap builds the events map for a slash_command/command trigger workflow.
// In centralized mode it uses workflow_dispatch; in decentralized mode it merges comment events.
// If label_command is also configured alongside the command trigger, it merges label events too.
func (c *Compiler) buildCommandTriggerEventsMap(data *WorkflowData) (map[string]any, error) {
	commandEventsMap := make(map[string]any)
	if data.CommandCentralized {
		if len(data.CommandOtherEvents) > 0 {
			maps.Copy(commandEventsMap, data.CommandOtherEvents)
		}
		if _, hasWorkflowDispatch := commandEventsMap["workflow_dispatch"]; !hasWorkflowDispatch {
			commandEventsMap["workflow_dispatch"] = nil
		}
		return commandEventsMap, nil
	}
	// Decentralized: merge comment events for YAML (combines pull_request_comment → issue_comment)
	for _, event := range MergeEventsForYAML(FilterCommentEvents(data.CommandEvents)) {
		commandEventsMap[event.EventName] = map[string]any{"types": event.Types}
	}
	if len(data.CommandOtherEvents) > 0 {
		maps.Copy(commandEventsMap, data.CommandOtherEvents)
	}
	// Merge label events if label_command is configured alongside slash_command.
	if len(data.LabelCommand) > 0 {
		for _, eventName := range FilterLabelCommandEvents(data.LabelCommandEvents) {
			if existingAny, ok := commandEventsMap[eventName]; ok {
				if existingMap, ok := existingAny.(map[string]any); ok {
					switch t := existingMap["types"].(type) {
					case []string:
						newTypes := make([]any, 0, typeutil.SafeAllocationCapacity(len(t), 1))
						for _, s := range t {
							newTypes = append(newTypes, s)
						}
						newTypes = append(newTypes, "labeled")
						existingMap["types"] = newTypes
					case []any:
						existingMap["types"] = append(t, "labeled")
					}
				}
			} else {
				commandEventsMap[eventName] = map[string]any{"types": []any{"labeled"}}
			}
		}
	}
	return commandEventsMap, nil
}

// applyLabelCommandTriggerOnSection generates the "on:" YAML section and job-level "if" condition
// for label_command trigger workflows.
func (c *Compiler) applyLabelCommandTriggerOnSection(data *WorkflowData) error {
	toolsLog.Print("Workflow is label-command trigger, configuring label events")

	labelEventsMap, hasDispatchItemNumber := c.buildLabelCommandEventsMap(data)
	if hasDispatchItemNumber {
		data.HasDispatchItemNumber = true
	}

	// Convert merged events to YAML
	mergedEventsYAML, err := yaml.MarshalWithOptions(map[string]any{"on": labelEventsMap}, yaml.IndentSequence(true))
	if err != nil {
		return fmt.Errorf("failed to marshal label-command events: %w", err)
	}
	yamlStr := strings.TrimSuffix(string(mergedEventsYAML), "\n")
	yamlStr = parser.QuoteCronExpressions(yamlStr)
	yamlStr = c.commentOutProcessedFieldsInOnSection(yamlStr, map[string]any{})
	data.On = yamlStr

	// Build the label-command condition
	hasOtherEvents := len(data.LabelCommandOtherEvents) > 0
	labelConditionTree, err := buildLabelCommandCondition(data.LabelCommand, data.LabelCommandEvents, hasOtherEvents)
	if err != nil {
		return fmt.Errorf("failed to build label-command condition: %w", err)
	}
	if data.LabelCommandDecentralized {
		labelConditionTree, err = buildDispatchLabelCommandCondition(data.LabelCommand, data.LabelCommandEvents)
		if err != nil {
			return fmt.Errorf("failed to build decentralized label-command condition: %w", err)
		}
	}
	data.If = RenderCondition(BuildConditionTree(data.If, labelConditionTree.Render()))
	return nil
}

// buildLabelCommandEventsMap builds the events map for a label_command trigger workflow.
// In decentralized mode it uses workflow_dispatch; in standard mode it generates label events.
// The returned bool is true when a workflow_dispatch item-number input was added, requiring
// the caller to set data.HasDispatchItemNumber.
func (c *Compiler) buildLabelCommandEventsMap(data *WorkflowData) (map[string]any, bool) {
	labelEventsMap := make(map[string]any)
	if data.LabelCommandDecentralized {
		if len(data.LabelCommandOtherEvents) > 0 {
			maps.Copy(labelEventsMap, data.LabelCommandOtherEvents)
		}
		hasDispatch := ensureWorkflowDispatchItemNumberInput(labelEventsMap)
		return labelEventsMap, hasDispatch
	}
	// Generate events: issues, pull_request, discussion with types: [labeled]
	for _, eventName := range FilterLabelCommandEvents(data.LabelCommandEvents) {
		labelEventsMap[eventName] = map[string]any{"types": []any{"labeled"}}
	}
	hasDispatch := ensureWorkflowDispatchItemNumberInput(labelEventsMap)
	mergeLabelCommandOtherEvents(labelEventsMap, data.LabelCommandOtherEvents)
	return labelEventsMap, hasDispatch
}

// mergeLabelCommandOtherEvents merges other (non-label-command) events into the events map,
// combining "types" arrays for overlapping event names and preserving other fields.
func mergeLabelCommandOtherEvents(labelEventsMap map[string]any, otherEvents map[string]any) {
	for eventKey, eventVal := range otherEvents {
		if existing, exists := labelEventsMap[eventKey]; exists {
			existingMap, _ := existing.(map[string]any)
			userMap, _ := eventVal.(map[string]any)
			if existingMap != nil && userMap != nil {
				existingTypes, _ := existingMap["types"].([]any)
				userTypes, _ := userMap["types"].([]any)
				merged := make([]any, 0, typeutil.SafeAllocationCapacity(len(existingTypes), len(userTypes)))
				merged = append(merged, existingTypes...)
				merged = append(merged, userTypes...)
				existingMap["types"] = merged
				for k, v := range userMap {
					if k != "types" {
						existingMap[k] = v
					}
				}
			}
		} else {
			labelEventsMap[eventKey] = eventVal
		}
	}
}

func ensureWorkflowDispatchItemNumberInput(eventsMap map[string]any) bool {
	dispatchAny, hasDispatch := eventsMap["workflow_dispatch"]
	if !hasDispatch || dispatchAny == nil {
		eventsMap["workflow_dispatch"] = map[string]any{
			"inputs": map[string]any{
				"item_number": map[string]any{
					"description": "The number of the issue, pull request, or discussion",
					"required":    false,
					"default":     "",
					"type":        "string",
				},
			},
		}
		return true
	}

	dispatchMap, ok := dispatchAny.(map[string]any)
	if !ok {
		toolsLog.Print("Skipping workflow_dispatch item_number injection: workflow_dispatch is not a map")
		return false
	}

	inputsAny, hasInputs := dispatchMap["inputs"]
	if !hasInputs || inputsAny == nil {
		dispatchMap["inputs"] = map[string]any{}
		inputsAny = dispatchMap["inputs"]
	}
	inputsMap, ok := inputsAny.(map[string]any)
	if !ok {
		toolsLog.Print("Skipping workflow_dispatch item_number injection: workflow_dispatch.inputs is not a map")
		return false
	}

	if _, hasItemNumber := inputsMap["item_number"]; !hasItemNumber {
		inputsMap["item_number"] = map[string]any{
			"description": "The number of the issue, pull request, or discussion",
			"required":    false,
			"default":     "",
			"type":        "string",
		}
	}
	return true
}

// mergeToolsAndMCPServers merges tools, mcp-servers, and included tools
func (c *Compiler) mergeToolsAndMCPServers(topTools, mcpServers map[string]any, includedTools string) (map[string]any, error) {
	toolsLog.Printf("Merging tools and MCP servers: topTools=%d, mcpServers=%d", len(topTools), len(mcpServers))

	// Start with top-level tools
	result := topTools
	if result == nil {
		result = make(map[string]any)
	}

	// Add MCP servers to the tools collection
	maps.Copy(result, mcpServers)

	// Merge included tools
	return c.MergeTools(result, includedTools)
}

// mergeRuntimes merges runtime configurations from frontmatter and imports
func mergeRuntimes(topRuntimes map[string]any, importedRuntimesJSON string) (map[string]any, error) {
	toolsLog.Printf("Merging runtimes: topRuntimes=%d", len(topRuntimes))
	result := make(map[string]any)

	// Start with top-level runtimes
	maps.Copy(result, topRuntimes)

	// Merge imported runtimes (newline-separated JSON objects)
	if importedRuntimesJSON != "" {
		lines := strings.SplitSeq(strings.TrimSpace(importedRuntimesJSON), "\n")
		for line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || line == "{}" {
				continue
			}

			var importedRuntimes map[string]any
			if err := json.Unmarshal([]byte(line), &importedRuntimes); err != nil {
				return nil, fmt.Errorf("failed to parse imported runtimes JSON: %w", err)
			}

			// Merge imported runtimes - later imports override earlier ones
			maps.Copy(result, importedRuntimes)
		}
	}

	toolsLog.Printf("Merged %d total runtimes", len(result))
	return result, nil
}

// hasIssueTrigger checks if the workflow has an issue trigger in its 'on' section
func (c *Compiler) hasIssueTrigger(onSection string) bool {
	hasIssue := strings.Contains(onSection, "issues:") ||
		strings.Contains(onSection, "issue:") ||
		strings.Contains(onSection, "issue_comment:")
	toolsLog.Printf("Checking for issue trigger: has_issue=%t", hasIssue)
	return hasIssue
}

// injectWorkflowDispatchForIssue adds workflow_dispatch trigger with issue_number input
func (c *Compiler) injectWorkflowDispatchForIssue(onSection string) string {
	toolsLog.Print("Injecting workflow_dispatch trigger for issue workflows")
	// Parse the existing on section to understand its structure
	var onData map[string]any
	if err := yaml.Unmarshal([]byte(onSection), &onData); err != nil {
		// If parsing fails, append workflow_dispatch manually
		return onSection + "\n  workflow_dispatch:\n    inputs:\n      issue_number:\n        description: 'Issue number for trial mode'\n        required: true\n        type: string"
	}

	// Get the 'on' section
	if onMap, exists := onData["on"]; exists {
		if triggers, ok := onMap.(map[string]any); ok {
			// Add workflow_dispatch with issue_number input
			triggers["workflow_dispatch"] = map[string]any{
				"inputs": map[string]any{
					"issue_number": map[string]any{
						"description": "Issue number for trial mode",
						"required":    true,
						"type":        "string",
					},
				},
			}

			// Convert back to YAML
			updatedOnData := map[string]any{"on": triggers}
			if yamlBytes, err := yaml.Marshal(updatedOnData); err == nil {
				yamlStr := string(yamlBytes)
				// Keep "on" quoted as it's a YAML boolean keyword
				return strings.TrimSuffix(yamlStr, "\n")
			}
		}
	}

	// Fallback: append workflow_dispatch manually
	return onSection + "\n  workflow_dispatch:\n    inputs:\n      issue_number:\n        description: 'Issue number for trial mode'\n        required: true\n        type: string"
}

// replaceIssueNumberReferences replaces github.event.issue.number with inputs.issue_number in YAML content
func (c *Compiler) replaceIssueNumberReferences(yamlContent string) string {
	// Replace all occurrences of github.event.issue.number with inputs.issue_number
	return strings.ReplaceAll(yamlContent, "github.event.issue.number", "inputs.issue_number")
}

// isBashFullyDisabled reports whether the final (post-default-resolution) tools map represents
// a complete refusal of bash/shell execution, i.e. tools.bash: false, or tools.bash: [] (empty
// allowlist). wasExplicitlyFalse must be computed by the caller from the tools map *before*
// applyDefaultTools runs, since a "bash: false" that is not overridden by required git commands
// results in the "bash" key being removed entirely — the same final state as "bash" never being
// configured at all. Only the explicit-false signal lets us tell these two cases apart.
// bash: [] (empty array), by contrast, survives applyDefaultTools unchanged, so it can be
// detected directly from the resolved tools map.
func isBashFullyDisabled(tools map[string]any, wasExplicitlyFalse bool) bool {
	if bashVal, hasBash := tools["bash"]; hasBash {
		if bashCommands, ok := bashVal.([]any); ok {
			return len(bashCommands) == 0
		}
		return false
	}
	// "bash" key is absent: either it was explicitly disabled (and not overridden by required
	// git commands), or it was never configured. Only the former counts as "fully disabled".
	return wasExplicitlyFalse
}

// applyDefaultTools adds default read-only GitHub MCP tools, creating github tool if not present
func (c *Compiler) applyDefaultTools(tools map[string]any, safeOutputs *SafeOutputsConfig, sandboxConfig *SandboxConfig, networkPermissions *NetworkPermissions) map[string]any { //nolint:largefunc // Existing defaulting logic is centralized; SDK changes only preserve explicit disabled state before this runs.
	toolsLog.Printf("Applying default tools: existingToolCount=%d", len(tools))
	// Always apply default GitHub tools (create github section if it doesn't exist)

	if tools == nil {
		tools = make(map[string]any)
	}

	// Get existing github tool configuration
	githubTool := tools["github"]

	steerIssueComments := isSteeringIssueEnabled(&WorkflowData{SafeOutputs: safeOutputs})

	// Check if github is explicitly disabled (github: false)
	if githubTool == false && !steerIssueComments {
		// Remove the github tool entirely when set to false
		delete(tools, "github")
	} else {
		// Process github tool configuration
		var githubConfig map[string]any

		if toolConfig, ok := githubTool.(map[string]any); ok {
			githubConfig = make(map[string]any)
			maps.Copy(githubConfig, toolConfig)
		} else {
			githubConfig = make(map[string]any)
		}

		// Parse the existing GitHub tool configuration for type safety
		parsedConfig := parseGitHubTool(githubTool)

		// Create a set of existing tools for efficient lookup
		existingToolsSet := make(map[string]struct{})
		if parsedConfig != nil {
			for _, tool := range parsedConfig.Allowed {
				existingToolsSet[string(tool)] = struct{}{}
			}
		}

		// Only set allowed tools if explicitly configured
		// Don't add default tools - let the MCP server use all available tools
		if len(existingToolsSet) > 0 {
			// Convert back to []any for the map
			existingAllowed := make([]any, 0, len(parsedConfig.Allowed))
			for _, tool := range parsedConfig.Allowed {
				existingAllowed = append(existingAllowed, string(tool))
			}
			githubConfig["allowed"] = existingAllowed
		}
		if steerIssueComments {
			ensureGitHubToolset(githubConfig, "issues")
			ensureGitHubAllowedTool(githubConfig, "issue_read")
		}
		tools["github"] = githubConfig
	}

	// Enable edit and bash tools by default when sandbox is enabled
	// The sandbox is enabled when:
	// 1. Explicitly configured via sandbox.agent (awf)
	// 2. Auto-enabled by firewall default enablement (when network restrictions are present)
	if isSandboxEnabled(sandboxConfig, networkPermissions) {
		toolsLog.Print("Sandbox enabled, applying default edit and bash tools")

		// Add edit tool if not present
		if _, exists := tools["edit"]; !exists {
			tools["edit"] = true
			toolsLog.Print("Added edit tool (sandbox enabled)")
		}

		// Add bash tool with wildcard if not present
		if _, exists := tools["bash"]; !exists {
			tools["bash"] = []any{"*"}
			toolsLog.Print("Added bash tool with wildcard (sandbox enabled)")
		}
	}

	// Add Git commands and file editing tools when safe-outputs includes create-pull-request or push-to-pull-request-branch
	if safeOutputs != nil && needsGitCommands(safeOutputs) {

		// Add edit tool with null value
		if _, exists := tools["edit"]; !exists {
			tools["edit"] = nil
		}
		gitCommands := []any{
			"git checkout:*",
			"git branch:*",
			"git switch:*",
			"git add:*",
			"git rm:*",
			"git commit:*",
			"git merge:*",
			"git status",
		}

		// Add bash tool with Git commands if not already present
		if _, exists := tools["bash"]; !exists {
			// bash tool doesn't exist, add it with Git commands
			tools["bash"] = gitCommands
		} else {
			// bash tool exists, merge Git commands with existing commands
			existingBash := tools["bash"]
			if existingCommands, ok := existingBash.([]any); ok {
				// Convert existing commands to strings for comparison
				existingSet := make(map[string]struct{})
				for _, cmd := range existingCommands {
					if cmdStr, ok := cmd.(string); ok {
						existingSet[cmdStr] = struct{}{}
						// If we see :* or *, all bash commands are already allowed
						if cmdStr == ":*" || cmdStr == "*" {
							// Don't add specific Git commands since all are already allowed
							goto bashComplete
						}
					}
				}

				// Add Git commands that aren't already present
				newCommands := append([]any(nil), existingCommands...)
				for _, gitCmd := range gitCommands {
					if gitCmdStr, ok := gitCmd.(string); ok {
						if _, ok := existingSet[gitCmdStr]; !ok {
							newCommands = append(newCommands, gitCmd)
						}
					}
				}
				tools["bash"] = newCommands
			} else if existingBash == false {
				// bash: false was set, but git commands are required for PR operations
				// Override with git commands only (minimum needed for PR functionality)
				toolsLog.Print("Overriding bash: false with git commands (required for PR operations)")
				tools["bash"] = gitCommands
			} else if existingBash == nil {
				_ = existingBash // Keep the nil value as-is
			}
		}
	bashComplete:
	}

	// Add default bash commands when bash is enabled but no specific commands are provided
	// This runs after git commands logic, so it only applies when git commands weren't added
	// Behavior:
	//   - bash: true → All commands allowed (converted to ["*"])
	//   - bash: false → Tool disabled (removed from tools), unless git commands were needed for PR operations
	//   - bash: nil → Add default commands
	//   - bash: [] → No commands (empty array means no tools allowed)
	//   - bash: ["cmd1", "cmd2"] → Add default commands + specific commands
	if bashTool, exists := tools["bash"]; exists {
		// Check if bash was left as nil or true after git processing
		if bashTool == nil {
			// bash is nil - only add defaults if this wasn't processed by git commands
			// If git commands were needed, bash would have been set to git commands or left as nil intentionally
			if safeOutputs == nil || !needsGitCommands(safeOutputs) {
				defaultCommands := make([]any, len(constants.DefaultBashTools))
				for i, cmd := range constants.DefaultBashTools {
					defaultCommands[i] = cmd
				}
				tools["bash"] = defaultCommands
			}
		} else if bashTool == true {
			// bash is true - convert to wildcard (allow all commands)
			tools["bash"] = []any{"*"}
		} else if bashTool == false {
			// bash is false - disable the tool by removing it
			delete(tools, "bash")
		} else if bashArray, ok := bashTool.([]any); ok {
			// bash is an array - merge default commands with custom commands
			if len(bashArray) > 0 {
				// Create a set to track existing commands to avoid duplicates
				existingCommands := make(map[string]struct{})
				for _, cmd := range bashArray {
					if cmdStr, ok := cmd.(string); ok {
						existingCommands[cmdStr] = struct{}{}
					}
				}

				// Start with default commands (append handles capacity automatically)
				var mergedCommands []any
				for _, cmd := range constants.DefaultBashTools {
					if _, ok := existingCommands[cmd]; !ok {
						mergedCommands = append(mergedCommands, cmd)
					}
				}

				// Add the custom commands
				mergedCommands = append(mergedCommands, bashArray...)
				tools["bash"] = mergedCommands
			}
			// Note: bash with empty array (bash: []) means "no bash tools allowed" and is left as-is
		}
	}

	return tools
}

func ensureGitHubToolset(githubConfig map[string]any, toolset string) {
	if githubConfig == nil || toolset == "" {
		return
	}
	toolsetsValue, exists := githubConfig["toolsets"]
	if !exists {
		return
	}
	toolsets := parseStringSliceAny(toolsetsValue, toolsLog)
	for _, existing := range toolsets {
		if existing == toolset || existing == "all" || existing == "default" || existing == "action-friendly" {
			return
		}
	}
	githubConfig["toolsets"] = appendStringAny(toolsets, toolset)
}

func ensureGitHubAllowedTool(githubConfig map[string]any, tool string) {
	if githubConfig == nil || tool == "" {
		return
	}
	if _, exists := githubConfig["allowed"]; !exists {
		// No allowlist means the GitHub MCP server exposes all read tools, including
		// pull_request_read, so there is nothing to add.
		return
	}
	allowed, _ := parseGitHubAllowedToolsAndLimits(githubConfig["allowed"])
	for _, existing := range allowed {
		if existing == tool || existing == "*" {
			return
		}
	}
	githubConfig["allowed"] = appendStringAny(allowed, tool)
}

func appendStringAny(values []string, value string) []any {
	result := make([]any, 0, typeutil.SafeAllocationCapacity(len(values), 1))
	for _, existing := range values {
		result = append(result, existing)
	}
	result = append(result, value)
	return result
}

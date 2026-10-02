package workflow

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/stringutil"
)

// ========================================
// Safe Output Tools Generation
// ========================================
//
// This file handles tool JSON generation: it takes the full set of
// safe-output tool definitions (from safe-output-tools.json) and produces a
// filtered subset containing only those tools enabled by the workflow's
// SafeOutputsConfig. Dynamic tools (dispatch-workflow, custom jobs) are also
// generated here.
//
// generateToolsMetaJSON generates a small "meta" JSON (description suffixes,
// repo params, dynamic tools) to be written at compile time. At runtime,
// generate_safe_outputs_tools.cjs reads the source safe_outputs_tools.json
// from the actions folder, applies the meta overrides, and writes the final
// tools.json—avoiding inlining the entire file into the compiled workflow YAML.

// generateDynamicTools generates MCP tool definitions for dynamic tools:
// custom safe-jobs, dispatch_workflow targets, and call_workflow targets.
// These tools are not in safe_outputs_tools.json and must be generated from
// the workflow configuration at compile time.
//
//nolint:largefunc // Dynamic tool generation is intentionally kept as one exhaustive flow.
func generateDynamicTools(data *WorkflowData, markdownPath string) ([]map[string]any, error) {
	var dynamicTools []map[string]any

	if data.LedgerConfig != nil {
		ledgerNamesByType := make(map[string][]string)
		for _, ledger := range data.LedgerConfig.Ledgers {
			ledgerNamesByType[ledger.Type] = append(ledgerNamesByType[ledger.Type], ledger.Name)
		}
		for ledgerType := range ledgerNamesByType {
			slices.Sort(ledgerNamesByType[ledgerType])
		}
		for _, spec := range []struct {
			name, ledgerType, operation, field string
			required                           bool
		}{
			{"ledger_map_put", "map", "put", "value", true},
			{"ledger_map_delete", "map", "delete", "", false},
		} {
			ledgerNames := ledgerNamesByType[spec.ledgerType]
			if len(ledgerNames) == 0 {
				continue
			}
			properties := map[string]any{
				"ledger":  map[string]any{"type": "string", "description": "Target ledger name; optional when there is only one ledger of this type, required otherwise.", "enum": ledgerNames},
				"temp_id": map[string]any{"type": "string", "description": "Optional temporary ID for same-batch references."},
			}
			required := []string{}
			if len(ledgerNames) > 1 {
				required = append(required, "ledger")
			}
			if spec.ledgerType == "map" {
				properties["key"] = map[string]any{"type": "string"}
				required = append(required, "key")
			}
			if spec.field != "" {
				fieldSchema := map[string]any{}
				properties[spec.field] = fieldSchema
				if spec.required {
					required = append(required, spec.field)
				}
			}
			dynamicTools = append(dynamicTools, map[string]any{
				"name": spec.name, "description": fmt.Sprintf("%s a %s ledger entry. Persistence follows trusted validation.", spec.operation, spec.ledgerType),
				"_ledger_type": spec.ledgerType, "_ledger_operation": spec.operation,
				"inputSchema": map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false},
			})
		}
		if noteLedgers := ledgerNamesByType["notes"]; len(noteLedgers) > 0 {
			dynamicTools = append(dynamicTools, generateNoteLedgerTools(noteLedgers)...)
		}
	}

	// Add custom job tools from SafeOutputs.Jobs
	if len(data.SafeOutputs.Jobs) > 0 {
		safeOutputsConfigLog.Printf("Adding %d custom job tools", len(data.SafeOutputs.Jobs))

		// Sort job names for deterministic output
		jobNames := sliceutil.SortedKeys(data.SafeOutputs.Jobs)

		for _, jobName := range jobNames {
			jobConfig := data.SafeOutputs.Jobs[jobName]
			normalizedJobName := stringutil.NormalizeSafeOutputIdentifier(jobName)
			customTool := generateCustomJobToolDefinition(normalizedJobName, jobConfig)
			dynamicTools = append(dynamicTools, customTool)
		}
	}

	// Add custom script tools from SafeOutputs.Scripts
	if len(data.SafeOutputs.Scripts) > 0 {
		safeOutputsConfigLog.Printf("Adding %d custom script tools to dynamic tools", len(data.SafeOutputs.Scripts))

		scriptNames := sliceutil.SortedKeys(data.SafeOutputs.Scripts)

		for _, scriptName := range scriptNames {
			scriptConfig := data.SafeOutputs.Scripts[scriptName]
			normalizedScriptName := stringutil.NormalizeSafeOutputIdentifier(scriptName)
			customTool := generateCustomScriptToolDefinition(normalizedScriptName, scriptConfig)
			dynamicTools = append(dynamicTools, customTool)
		}
	}

	// Add custom action tools from SafeOutputs.Actions
	// Each configured action is exposed as an MCP tool with schema derived from action.yml.
	// The compiler resolves action.yml at compile time; if resolution fails the tool is still
	// added with an empty inputSchema so the agent can still attempt to call it.
	if len(data.SafeOutputs.Actions) > 0 {
		safeOutputsConfigLog.Printf("Adding %d custom action tools to dynamic tools", len(data.SafeOutputs.Actions))

		actionNames := sliceutil.SortedKeys(data.SafeOutputs.Actions)

		for _, actionName := range actionNames {
			actionConfig := data.SafeOutputs.Actions[actionName]
			customTool := generateActionToolDefinition(actionName, actionConfig)
			dynamicTools = append(dynamicTools, customTool)
		}
	}

	// Add dynamic dispatch_workflow tools
	if data.SafeOutputs.DispatchWorkflow != nil && len(data.SafeOutputs.DispatchWorkflow.Workflows) > 0 {
		safeOutputsConfigLog.Printf("Adding %d dispatch_workflow tools", len(data.SafeOutputs.DispatchWorkflow.Workflows))

		if data.SafeOutputs.DispatchWorkflow.WorkflowFiles == nil {
			data.SafeOutputs.DispatchWorkflow.WorkflowFiles = make(map[string]string)
		}

		for _, workflowName := range data.SafeOutputs.DispatchWorkflow.Workflows {
			fileResult, err := findWorkflowFile(workflowName, markdownPath)
			if err != nil {
				safeOutputsConfigLog.Printf("Warning: error finding workflow %s: %v", workflowName, err)
				dynamicTools = append(dynamicTools, generateDispatchWorkflowTool(workflowName, make(map[string]any), data.SafeOutputs.DispatchWorkflow.AllowedRefs))
				continue
			}

			var workflowPath string
			var extension string
			var useMD bool
			if fileResult.lockExists {
				workflowPath = fileResult.lockPath
				extension = ".lock.yml"
			} else if fileResult.ymlExists {
				workflowPath = fileResult.ymlPath
				extension = filepath.Ext(fileResult.ymlPath)
			} else if fileResult.mdExists {
				workflowPath = fileResult.mdPath
				extension = ".lock.yml"
				useMD = true
			} else {
				safeOutputsConfigLog.Printf("Warning: no workflow file found for %s (checked .lock.yml, .yml, .md)", workflowName)
				dynamicTools = append(dynamicTools, generateDispatchWorkflowTool(workflowName, make(map[string]any), data.SafeOutputs.DispatchWorkflow.AllowedRefs))
				continue
			}

			data.SafeOutputs.DispatchWorkflow.WorkflowFiles[workflowName] = extension

			var workflowInputs map[string]any
			var inputsErr error
			if useMD {
				workflowInputs, inputsErr = extractMDWorkflowDispatchInputs(workflowPath)
			} else {
				workflowInputs, inputsErr = extractWorkflowDispatchInputs(workflowPath)
			}
			if inputsErr != nil {
				safeOutputsConfigLog.Printf("Warning: failed to extract inputs for workflow %s from %s: %v", workflowName, workflowPath, inputsErr)
				workflowInputs = make(map[string]any)
			}

			dynamicTools = append(dynamicTools, generateDispatchWorkflowTool(workflowName, workflowInputs, data.SafeOutputs.DispatchWorkflow.AllowedRefs))
		}
	}

	// Add dynamic dispatch_repository tools
	if data.SafeOutputs.DispatchRepository != nil && len(data.SafeOutputs.DispatchRepository.Tools) > 0 {
		safeOutputsConfigLog.Printf("Adding %d dispatch_repository tools to dynamic tools", len(data.SafeOutputs.DispatchRepository.Tools))

		// Sort tool keys for deterministic output
		toolKeys := sliceutil.SortedKeys(data.SafeOutputs.DispatchRepository.Tools)

		for _, toolKey := range toolKeys {
			toolConfig := data.SafeOutputs.DispatchRepository.Tools[toolKey]
			dynamicTools = append(dynamicTools, generateDispatchRepositoryTool(toolKey, toolConfig))
		}
	}

	// Add dynamic call_workflow tools
	if data.SafeOutputs.CallWorkflow != nil && len(data.SafeOutputs.CallWorkflow.Workflows) > 0 {
		safeOutputsConfigLog.Printf("Adding %d call_workflow tools", len(data.SafeOutputs.CallWorkflow.Workflows))

		if data.SafeOutputs.CallWorkflow.WorkflowFiles == nil {
			data.SafeOutputs.CallWorkflow.WorkflowFiles = make(map[string]string)
		}

		for _, workflowName := range data.SafeOutputs.CallWorkflow.Workflows {
			fileResult, err := findWorkflowFile(workflowName, markdownPath)
			if err != nil {
				safeOutputsConfigLog.Printf("Warning: error finding workflow %s: %v", workflowName, err)
				dynamicTools = append(dynamicTools, generateCallWorkflowTool(workflowName, make(map[string]any)))
				continue
			}

			var workflowPath string
			var extension string
			var useMD bool
			if fileResult.lockExists {
				workflowPath = fileResult.lockPath
				extension = ".lock.yml"
			} else if fileResult.ymlExists {
				workflowPath = fileResult.ymlPath
				extension = filepath.Ext(fileResult.ymlPath)
			} else if fileResult.mdExists {
				workflowPath = fileResult.mdPath
				extension = ".lock.yml"
				useMD = true
			} else {
				safeOutputsConfigLog.Printf("Warning: no workflow file found for %s (checked .lock.yml, .yml, .md)", workflowName)
				dynamicTools = append(dynamicTools, generateCallWorkflowTool(workflowName, make(map[string]any)))
				continue
			}

			relativePath := fmt.Sprintf("./.github/workflows/%s%s", workflowName, extension)
			data.SafeOutputs.CallWorkflow.WorkflowFiles[workflowName] = relativePath

			var workflowInputs map[string]any
			var inputsErr error
			if useMD {
				workflowInputs, inputsErr = extractMDWorkflowCallInputs(workflowPath)
			} else {
				workflowInputs, inputsErr = extractWorkflowCallInputs(workflowPath)
			}
			if inputsErr != nil {
				safeOutputsConfigLog.Printf("Warning: failed to extract inputs for workflow %s from %s: %v", workflowName, workflowPath, inputsErr)
				workflowInputs = make(map[string]any)
			}

			dynamicTools = append(dynamicTools, generateCallWorkflowTool(workflowName, workflowInputs))
		}
	}

	return dynamicTools, nil
}

func generateNoteLedgerTools(ledgerNames []string) []map[string]any {
	ledger := map[string]any{
		"type": "string", "enum": ledgerNames,
		"description": "Target notes ledger; optional when only one notes ledger exists, required otherwise.",
	}
	fields := []struct {
		name, operation string
		properties      map[string]any
		required        []string
	}{
		{
			name: "ledger_note_add", operation: "note",
			properties: map[string]any{
				"temp_id": map[string]any{"type": "string", "description": "Optional temporary ID for same-batch references."},
				"subject": map[string]any{"type": "string", "minLength": 1, "maxLength": 512},
				"note":    map[string]any{"type": "string", "minLength": 1, "maxLength": 4096},
				"reason":  map[string]any{"type": "string", "minLength": 1, "maxLength": 1024},
				"citations": map[string]any{
					"type": "array", "minItems": 1, "maxItems": 32,
					"items": map[string]any{
						"type": "object", "required": []string{"type", "path"}, "additionalProperties": false,
						"properties": map[string]any{
							"type":       map[string]any{"type": "string", "enum": []string{"repository"}},
							"path":       map[string]any{"type": "string", "minLength": 1, "description": "Repository-relative file path without traversal or encoded separators."},
							"start_line": map[string]any{"type": "integer", "minimum": 1},
							"end_line":   map[string]any{"type": "integer", "minimum": 1},
						},
					},
				},
			},
			required: []string{"subject", "note", "reason", "citations"},
		},
		{
			name: "ledger_note_vote", operation: "vote",
			properties: map[string]any{
				"note_id": map[string]any{"type": "string", "minLength": 1, "maxLength": 128},
				"vote":    map[string]any{"type": "string", "enum": []string{"up", "down"}},
				"reason":  map[string]any{"type": "string", "minLength": 1, "maxLength": 1024},
			},
			required: []string{"note_id", "vote"},
		},
	}
	tools := make([]map[string]any, 0, len(fields))
	actions := map[string]string{"note": "Add", "vote": "Vote on"}
	for _, field := range fields {
		properties := field.properties
		properties["ledger"] = ledger
		required := field.required
		if len(ledgerNames) > 1 {
			required = append(required, "ledger")
		}
		tools = append(tools, map[string]any{
			"name":         field.name,
			"description":  actions[field.operation] + " a note in a notes ledger. Persistence follows trusted validation.",
			"_ledger_type": "notes", "_ledger_operation": field.operation,
			"inputSchema": map[string]any{
				"type": "object", "properties": properties, "required": required, "additionalProperties": false,
			},
		})
	}
	return tools
}

// ToolsMeta is the structure written to tools_meta.json at compile time and read
// by generate_safe_outputs_tools.cjs at runtime. It avoids inlining the entire
// safe_outputs_tools.json into the compiled workflow YAML.
type ToolsMeta struct {
	// DescriptionSuffixes maps tool name → constraint text to append to the base description.
	// Example: " CONSTRAINTS: Maximum 5 issue(s) can be created."
	DescriptionSuffixes map[string]string `json:"description_suffixes"`
	// RepoParams maps tool name → "repo" inputSchema property definition, only present
	// when allowed-repos or a wildcard target-repo is configured for that tool.
	RepoParams map[string]map[string]any `json:"repo_params"`
	// DynamicTools contains tool definitions for custom safe-jobs, dispatch_workflow
	// targets, and call_workflow targets. These are workflow-specific and cannot be
	// derived from the static safe_outputs_tools.json at runtime.
	DynamicTools []map[string]any `json:"dynamic_tools"`
	// RequiredFieldRemovals maps tool name → list of field names to remove from the
	// inputSchema.required array. Used when a field that is required in the static
	// safe_outputs_tools.json should be optional for this specific workflow (e.g. when
	// allow-body: false is configured for close_discussion or close_issue).
	RequiredFieldRemovals map[string][]string `json:"required_field_removals,omitempty"`
	// RequiredFieldAdditions maps tool name → list of field names to add to the
	// inputSchema.required array. Used when a field that is optional in the static
	// safe_outputs_tools.json should be required for this specific workflow.
	RequiredFieldAdditions map[string][]string `json:"required_field_additions,omitempty"`
	// PropertyInjections maps tool name → property name → full JSON Schema property definition.
	// Used when a property should be injected into a tool's inputSchema at runtime based on
	// workflow configuration (e.g., a state_reason enum restricted to configured values).
	// If the property already exists in the static schema its definition is replaced;
	// otherwise it is added as a new optional property.
	PropertyInjections map[string]map[string]any `json:"property_injections,omitempty"`
	// ItemSchemas maps tool name → array property name → narrowed item schema.
	ItemSchemas map[string]map[string]map[string]any `json:"item_schemas,omitempty"`
}

// computeRequiredFieldRemovals returns a map of tool name → required fields to remove
// based on the safe-outputs configuration. Currently handles allow-body: false for
// close_discussion and close_issue.
func computeRequiredFieldRemovals(safeOutputs *SafeOutputsConfig) map[string][]string {
	removals := make(map[string][]string)
	if safeOutputs == nil {
		return removals
	}
	if safeOutputs.CloseDiscussions != nil && safeOutputs.CloseDiscussions.AllowBody != nil && !*safeOutputs.CloseDiscussions.AllowBody {
		removals["close_discussion"] = []string{"body"}
	}
	if safeOutputs.CloseIssues != nil && safeOutputs.CloseIssues.AllowBody != nil && !*safeOutputs.CloseIssues.AllowBody {
		removals["close_issue"] = []string{"body"}
	}
	return removals
}

// computeRequiredFieldAdditions returns a map of tool name → required fields to add
// based on the safe-outputs configuration.
func computeRequiredFieldAdditions(safeOutputs *SafeOutputsConfig) map[string][]string {
	additions := make(map[string][]string)
	if safeOutputs == nil {
		return additions
	}
	if safeOutputs.CreateIssues != nil && safeOutputs.CreateIssues.RequireTemporaryID {
		additions["create_issue"] = []string{"temporary_id"}
	}
	if safeOutputs.CreatePullRequests != nil && safeOutputs.CreatePullRequests.RequireTemporaryID {
		additions["create_pull_request"] = []string{"temporary_id"}
	}
	issueIntentRequiredFields := []string{"rationale", "confidence"}
	if safeOutputs.SetIssueType != nil && issueIntentRequired(safeOutputs.SetIssueType.IssueIntent) {
		additions["set_issue_type"] = issueIntentRequiredFields
	}
	if safeOutputs.SetIssueField != nil && issueIntentRequired(safeOutputs.SetIssueField.IssueIntent) {
		additions["set_issue_field"] = issueIntentRequiredFields
	}
	if safeOutputs.CloseIssues != nil && issueIntentRequired(safeOutputs.CloseIssues.IssueIntent) {
		additions["close_issue"] = issueIntentRequiredFields
	}
	if safeOutputs.AssignToUser != nil && issueIntentRequired(safeOutputs.AssignToUser.IssueIntent) {
		additions["assign_to_user"] = issueIntentRequiredFields
	}
	if safeOutputs.AssignToAgent != nil && issueIntentRequired(safeOutputs.AssignToAgent.IssueIntent) {
		additions["assign_to_agent"] = issueIntentRequiredFields
	}
	if safeOutputs.SubmitPullRequestReview != nil && len(safeOutputs.SubmitPullRequestReview.AllowedEvents) > 0 {
		if !slices.Contains(safeOutputs.SubmitPullRequestReview.AllowedEvents, "COMMENT") {
			additions["submit_pull_request_review"] = []string{"event"}
		}
	}
	return additions
}

func issueIntentRequired(issueIntent *bool) bool {
	return issueIntent != nil && *issueIntent
}

// closeIssueStateReasonValues is the full set of supported state reasons for close_issue.
var closeIssueStateReasonValues = []string{"completed", "not_planned", "duplicate"}

// computePropertyInjections returns a map of tool name → property name → property schema
// for properties that must be injected into the tool schema based on workflow configuration.
//
// Currently handles close_issue state_reason:
//   - Omitted config (no state-reason): inject state_reason with all three supported values.
//   - List config (state-reason: [...]): inject state_reason with the configured subset.
//   - Scalar config (state-reason: "..."): no injection (fixed reason, agent cannot choose).
//
//nolint:largefunc // Property injection cases are intentionally kept together.
func computePropertyInjections(safeOutputs *SafeOutputsConfig) map[string]map[string]any {
	injections := make(map[string]map[string]any)
	if safeOutputs == nil {
		return injections
	}
	if safeOutputs.CloseIssues != nil {
		c := safeOutputs.CloseIssues
		// Scalar config: agent cannot change state_reason; do not expose the field.
		if c.StateReason == "" {
			// List or omitted: expose state_reason with the permitted enum.
			enumValues := c.AllowedStateReason
			if len(enumValues) == 0 {
				enumValues = closeIssueStateReasonValues
			} else {
				// Validate each configured value against the supported API values so that
				// invalid strings (e.g. "done", "wontfix") are caught at compile time rather
				// than producing a GitHub API 422 at runtime.
				supported := make(map[string]struct{}, len(closeIssueStateReasonValues))
				for _, v := range closeIssueStateReasonValues {
					supported[v] = struct{}{}
				}
				valid := make([]string, 0, len(enumValues))
				for _, v := range enumValues {
					if _, ok := supported[v]; ok {
						valid = append(valid, v)
					} else {
						safeOutputsConfigLog.Printf("Warning: allowed-state-reason value %q is not a supported GitHub API value; valid values: %v", v, closeIssueStateReasonValues)
					}
				}
				if len(valid) == 0 {
					// All values were invalid; fall back to the full set so that compilation
					// succeeds, relying on schema validation to have already warned the author.
					safeOutputsConfigLog.Printf("Warning: all allowed-state-reason values were invalid; falling back to full supported set")
					valid = closeIssueStateReasonValues
				}
				enumValues = valid
			}
			injections["close_issue"] = map[string]any{
				"state_reason": map[string]any{
					"type":        "string",
					"enum":        enumValues,
					"description": "Optional closing state reason. Omit to use the configured default. Select 'duplicate' together with 'duplicate_of' to mark a native duplicate relationship.",
				},
			}
		}
	}

	// submit_pull_request_review event: when allowed-events restricts the set of review
	// decisions, narrow the tool schema's event enum to match so the agent cannot select
	// an event that runtime policy will reject. This retains runtime enforcement as
	// defense in depth while preventing the doomed call in the first place.
	if safeOutputs.SubmitPullRequestReview != nil && len(safeOutputs.SubmitPullRequestReview.AllowedEvents) > 0 {
		allowedEvents := safeOutputs.SubmitPullRequestReview.AllowedEvents
		injections["submit_pull_request_review"] = map[string]any{
			"event": map[string]any{
				"type":        "string",
				"enum":        allowedEvents,
				"description": "Review decision. Restricted by allowed-events configuration to: " + strings.Join(allowedEvents, ", ") + ".",
				"x-synonyms":  []string{"action"},
			},
		}
	}

	if safeOutputs.DataEnabled {
		dataProperty := map[string]any{"$ref": "#/0/inputSchema/$defs/structured_data"}
		if safeOutputs.NormalizedDataSchema != nil {
			dataProperty = safeOutputs.NormalizedDataSchema
		}
		for _, typeName := range dataSchemaBodyTypes {
			if injections[typeName] == nil {
				injections[typeName] = make(map[string]any)
			}
			injections[typeName]["data"] = dataProperty
		}
	}

	return injections
}

// generateToolsMetaJSON generates the content for tools_meta.json: a compact file
// that captures the workflow-specific customisations (description constraints,
// repo parameters, dynamic tools) without inlining the entire
// safe_outputs_tools.json into the compiled workflow YAML.
//
// At runtime, generate_safe_outputs_tools.cjs reads safe_outputs_tools.json from
// the actions folder, applies the meta overrides from tools_meta.json, and writes
// the final ${RUNNER_TEMP}/gh-aw/safeoutputs/tools.json.
//
//nolint:largefunc // Tool metadata assembly is intentionally kept as one flow.
func generateToolsMetaJSON(data *WorkflowData, markdownPath string) (string, error) {
	if data.SafeOutputs == nil && !data.LedgerConfig.Enabled() {
		empty := ToolsMeta{
			DescriptionSuffixes: map[string]string{},
			RepoParams:          map[string]map[string]any{},
			DynamicTools:        []map[string]any{},
		}
		result, err := json.Marshal(empty)
		if err != nil {
			return "", fmt.Errorf("unable to marshal empty tools meta to JSON; expected the empty ToolsMeta struct (with empty maps/slices) to be serializable: %w", err)
		}
		return string(result), nil
	}
	if data.SafeOutputs == nil {
		data.SafeOutputs = &SafeOutputsConfig{}
	}

	safeOutputsConfigLog.Print("Generating tools meta JSON for workflow")

	enabledTools := computeEnabledToolNames(data)

	// Compute description suffix for each enabled predefined tool.
	// enhanceToolDescription with an empty base returns just the constraint text
	// (e.g. " CONSTRAINTS: Maximum 5 issue(s).") so JavaScript can append it.
	descriptionSuffixes := make(map[string]string)
	for toolName := range enabledTools {
		suffix := enhanceToolDescription(toolName, "", data.SafeOutputs)
		if suffix != "" {
			descriptionSuffixes[toolName] = suffix
		}
	}

	// Compute repo parameter definition for each tool that needs it.
	repoParams := make(map[string]map[string]any)
	for toolName := range enabledTools {
		if param := computeRepoParamForTool(toolName, data.SafeOutputs); param != nil {
			repoParams[toolName] = param
		}
	}

	// Generate dynamic tool definitions (custom jobs + dispatch/call workflow tools).
	dynamicTools, err := generateDynamicTools(data, markdownPath)
	if err != nil {
		safeOutputsConfigLog.Printf("Error generating dynamic tools: %v", err)
		dynamicTools = []map[string]any{}
	}
	if dynamicTools == nil {
		dynamicTools = []map[string]any{}
	}

	// Compute required field removals (e.g. body when allow-body: false).
	requiredFieldRemovals := computeRequiredFieldRemovals(data.SafeOutputs)
	requiredFieldAdditions := computeRequiredFieldAdditions(data.SafeOutputs)

	// Compute property injections (e.g. state_reason enum for close_issue).
	propertyInjections := computePropertyInjections(data.SafeOutputs)
	itemSchemas := make(map[string]map[string]map[string]any)
	if data.SafeOutputs.AddLabels != nil && data.SafeOutputs.AddLabels.ItemSchema != nil {
		itemSchema, itemSchemaErr := normalizedAddLabelsItemSchema(
			data.SafeOutputs.AddLabels.ItemSchema,
			issueIntentRequired(data.SafeOutputs.AddLabels.IssueIntent),
		)
		if itemSchemaErr != nil {
			return "", itemSchemaErr
		}
		itemSchemas["add_labels"] = map[string]map[string]any{"labels": itemSchema}
	}

	meta := ToolsMeta{
		DescriptionSuffixes:    descriptionSuffixes,
		RepoParams:             repoParams,
		DynamicTools:           dynamicTools,
		RequiredFieldRemovals:  requiredFieldRemovals,
		RequiredFieldAdditions: requiredFieldAdditions,
		PropertyInjections:     propertyInjections,
		ItemSchemas:            itemSchemas,
	}

	result, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		safeOutputsConfigLog.Printf("Error marshaling tools meta: %v", err)
		return "", fmt.Errorf("unable to marshal tools meta to JSON; expected all computed fields (description suffixes, repo params, dynamic tools) to be serializable: %w", err)
	}

	safeOutputsConfigLog.Printf("Successfully generated tools meta JSON: %d description suffixes, %d repo params, %d dynamic tools",
		len(descriptionSuffixes), len(repoParams), len(dynamicTools))
	return string(result), nil
}

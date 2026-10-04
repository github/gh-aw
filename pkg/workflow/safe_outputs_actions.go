package workflow

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/goccy/go-yaml"
)

var safeOutputActionsLog = logger.New("workflow:safe_outputs_actions")

// SafeOutputActionConfig holds configuration for a single custom safe output action.
// Each configured action is resolved at compile time to get its inputs from action.yml,
// and is mounted as an MCP tool that the AI agent can call once per workflow run.
type SafeOutputActionConfig struct {
	Uses        string            `yaml:"uses"`
	Description string            `yaml:"description,omitempty"` // optional override of the action's description
	Env         map[string]string `yaml:"env,omitempty"`         // additional environment variables for the injected step

	// Computed at compile time (not from frontmatter):
	ResolvedRef       string                      `yaml:"-"` // Pinned action reference (e.g., "owner/repo@sha # v1")
	Inputs            map[string]*ActionYAMLInput `yaml:"-"` // Inputs parsed from action.yml
	ActionDescription string                      `yaml:"-"` // Description from action.yml
}

// actionYAMLFile is the parsed structure of a GitHub Action's action.yml.
type actionYAMLFile struct {
	Name        string                      `yaml:"name"`
	Description string                      `yaml:"description"`
	Inputs      map[string]*ActionYAMLInput `yaml:"inputs"`
}

// actionRef holds the parsed components of a GitHub Action `uses` field.
type actionRef struct {
	// Repo is the GitHub repository slug (e.g., "owner/repo").
	Repo string
	// Subdir is the sub-directory within the repository (e.g., "path/to/action").
	// Empty string means the action.yml is at the repository root.
	Subdir string
	// Ref is the git ref (tag, SHA, or branch) to checkout (e.g., "v1", "main").
	Ref string
	// IsLocal is true when the `uses` value is a local path (e.g., "./path/to/action").
	IsLocal bool
	// LocalPath is the filesystem path (only set when IsLocal is true).
	LocalPath string
}

// parseActionsConfig parses the safe-outputs.actions section from a raw frontmatter map.
// It returns a map of action names to their configurations.
func parseActionsConfig(actionsMap map[string]any) map[string]*SafeOutputActionConfig {
	if actionsMap == nil {
		return nil
	}

	result := make(map[string]*SafeOutputActionConfig)
	for actionName, actionValue := range actionsMap {
		actionConfigMap, ok := actionValue.(map[string]any)
		if !ok {
			safeOutputActionsLog.Printf("Warning: action %q config is not a map, skipping", actionName)
			continue
		}

		actionConfig := &SafeOutputActionConfig{}

		if uses, ok := actionConfigMap["uses"].(string); ok {
			actionConfig.Uses = uses
		}
		if description, ok := actionConfigMap["description"].(string); ok {
			actionConfig.Description = description
		}
		if envMap, ok := actionConfigMap["env"].(map[string]any); ok {
			actionConfig.Env = make(map[string]string, len(envMap))
			for k, v := range envMap {
				if vStr, ok := v.(string); ok {
					actionConfig.Env[k] = vStr
				}
			}
		}
		if inputsMap, ok := actionConfigMap["inputs"].(map[string]any); ok {
			actionConfig.Inputs = make(map[string]*ActionYAMLInput, len(inputsMap))
			for inputName, inputValue := range inputsMap {
				inputDef := &ActionYAMLInput{}
				if inputDefMap, ok := inputValue.(map[string]any); ok {
					if desc, ok := inputDefMap["description"].(string); ok {
						inputDef.Description = desc
					}
					if req, ok := inputDefMap["required"].(bool); ok {
						inputDef.Required = req
					}
					if def, ok := inputDefMap["default"].(string); ok {
						inputDef.Default = def
					}
				}
				actionConfig.Inputs[inputName] = inputDef
			}
		}

		if actionConfig.Uses == "" {
			safeOutputActionsLog.Printf("Warning: action %q is missing required 'uses' field, skipping", actionName)
			continue
		}

		result[actionName] = actionConfig
	}

	return result
}

// parseActionUsesField parses a GitHub Action `uses` field into its components.
// Supported formats:
//   - "owner/repo@ref"              -> repo root action
//   - "owner/repo/subdir@ref"       -> sub-directory action
//   - "./local/path"                -> local filesystem action
func parseActionUsesField(uses string) (*actionRef, error) {
	if strings.HasPrefix(uses, "./") || strings.HasPrefix(uses, "../") {
		return &actionRef{IsLocal: true, LocalPath: uses}, nil
	}

	// External action: split on "@" to get ref
	atIdx := strings.LastIndex(uses, "@")
	if atIdx < 0 {
		return nil, fmt.Errorf("invalid action ref %q: missing @ref suffix", uses)
	}

	refStr := uses[atIdx+1:]
	repoAndPath := uses[:atIdx]

	// Split repo from subdir: first two path segments are owner/repo
	parts := strings.SplitN(repoAndPath, "/", 3)
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid action ref %q: expected owner/repo format", uses)
	}

	repo := strings.Join(parts[:2], "/")
	var subdir string
	if len(parts) == 3 {
		subdir = parts[2]
	}

	return &actionRef{
		Repo:   repo,
		Subdir: subdir,
		Ref:    refStr,
	}, nil
}

// fetchAndParseActionYAML resolves the inputs and description from the action.yml
// for each configured action. Results are stored in the action config's computed fields.
// This function should be called before tool generation and step generation.
//
// Resolution priority (highest wins):
//  1. Inputs already specified in the frontmatter (config.Inputs != nil)
//  2. Inputs cached in the ActionCache (actions-lock.json)
//  3. Inputs fetched from the remote action.yml (result cached for future runs)
//
// When available, the action reference is pinned to a commit SHA for security;
// if no pin is available, later step generation falls back to the original config.Uses.
func (c *Compiler) fetchAndParseActionYAML(actionName string, config *SafeOutputActionConfig, markdownPath string, data *WorkflowData) {
	if config.Uses == "" {
		return
	}

	ref, err := parseActionUsesField(config.Uses)
	if err != nil {
		safeOutputActionsLog.Printf("Warning: failed to parse uses field %q for action %q: %v", config.Uses, actionName, err)
		return
	}

	// Remember whether inputs were provided via frontmatter so we can skip lower-
	// priority resolution paths.
	inputsFromFrontmatter := config.Inputs != nil

	var actionYAML *actionYAMLFile
	var resolvedRef string

	if ref.IsLocal {
		if !inputsFromFrontmatter {
			actionYAML, err = readLocalActionYAML(ref.LocalPath, markdownPath)
			if err != nil {
				safeOutputActionsLog.Printf("Warning: failed to read local action.yml for %q at %s: %v", actionName, ref.LocalPath, err)
			}
		}
		resolvedRef = config.Uses // local paths stay as-is
	} else {
		// Pin the action ref for security.
		pinned, pinErr := getActionPinWithData(ref.Repo, ref.Ref, data)
		var fetchRef string
		if pinErr != nil {
			safeOutputActionsLog.Printf("Warning: failed to pin action %q (%s@%s): %v", actionName, ref.Repo, ref.Ref, pinErr)
			// Fall back to using the original ref
			resolvedRef = config.Uses
			fetchRef = ref.Ref
		} else {
			resolvedRef = pinned
			// Extract the pinned SHA from the reference (format: "repo@sha # tag")
			// and use it to fetch action.yml so the schema matches the exact pinned version.
			if sha := extractSHAFromPinnedRef(pinned); sha != "" {
				fetchRef = sha
			} else {
				fetchRef = ref.Ref
			}
		}

		if !inputsFromFrontmatter {
			// Check the ActionCache for previously-fetched inputs before going to the network.
			// The cache key uses the original version tag from the `uses:` field (ref.Ref, e.g.
			// "v1") which matches the key stored in actions-lock.json.
			if data.ActionCache != nil {
				if cachedInputs, ok := data.ActionCache.GetInputs(ref.Repo, ref.Ref); ok {
					safeOutputActionsLog.Printf("Using cached inputs for %q (%s@%s)", actionName, ref.Repo, ref.Ref)
					config.Inputs = cachedInputs
				}
				if cachedDesc, ok := data.ActionCache.GetActionDescription(ref.Repo, ref.Ref); ok {
					config.ActionDescription = cachedDesc
				}
			}

			// If inputs are still not resolved, fetch action.yml from the network and
			// store the result in the cache to make future compilations deterministic.
			if config.Inputs == nil {
				actionYAML, err = fetchRemoteActionYAML(ref.Repo, ref.Subdir, fetchRef)
				if err != nil {
					safeOutputActionsLog.Printf("Warning: failed to fetch action.yml for %q (%s): %v", actionName, config.Uses, err)
				}
				// Cache the fetched inputs and description so subsequent compilations are
				// deterministic even when the network is unavailable.
				if actionYAML != nil && data.ActionCache != nil {
					seedSHA := fetchRef
					if !gitutil.IsValidFullSHA(seedSHA) {
						seedSHA = extractSHAFromPinnedRef(resolvedRef)
					}
					if gitutil.IsValidFullSHA(seedSHA) {
						data.ActionCache.Set(ref.Repo, ref.Ref, seedSHA)
					}
					if actionYAML.Inputs != nil {
						data.ActionCache.SetInputs(ref.Repo, ref.Ref, actionYAML.Inputs)
					}
					data.ActionCache.SetActionDescription(ref.Repo, ref.Ref, actionYAML.Description)
				}
			}
		}
	}

	config.ResolvedRef = resolvedRef

	// Only overwrite Inputs/ActionDescription from action.yml when the inputs were
	// not already provided via frontmatter or cache.
	if !inputsFromFrontmatter && config.Inputs == nil && actionYAML != nil {
		config.Inputs = actionYAML.Inputs
		config.ActionDescription = actionYAML.Description
	}
}

// extractSHAFromPinnedRef parses the SHA from a pinned action reference string.
// The format produced by formatActionReference is "repo@sha # version".
// Returns the SHA string, or empty string if it cannot be parsed.
func extractSHAFromPinnedRef(pinned string) string {
	// Find the @ separator between repo and sha
	_, afterAt, found := strings.Cut(pinned, "@")
	if !found {
		return ""
	}
	// Strip the "# version" comment
	if commentIdx := strings.Index(afterAt, " #"); commentIdx >= 0 {
		afterAt = strings.TrimSpace(afterAt[:commentIdx])
	}
	// Validate it looks like a full SHA (40 hex chars)
	if gitutil.IsValidFullSHA(afterAt) {
		return afterAt
	}
	return ""
}

// fetchRemoteActionYAML fetches and parses action.yml from a GitHub repository.
// It tries both action.yml and action.yaml filenames.
func fetchRemoteActionYAML(repo, subdir, ref string) (*actionYAMLFile, error) {
	for _, filename := range []string{"action.yml", "action.yaml"} {
		var contentPath string
		if subdir != "" {
			contentPath = path.Join(subdir, filename)
		} else {
			contentPath = filename
		}

		apiPath := fmt.Sprintf("/repos/%s/contents/%s?ref=%s", repo, contentPath, ref)
		safeOutputActionsLog.Printf("Fetching action YAML from: %s", apiPath)

		// Wrap the context creation and command execution in a function literal so that
		// defer cancel() runs when the literal returns (not when the outer function returns),
		// avoiding a defer-inside-loop resource-lifecycle issue.
		output, err := func() ([]byte, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := ExecGHContext(ctx, "api", apiPath, "--jq", ".content")
			return cmd.Output()
		}()
		if err != nil {
			safeOutputActionsLog.Printf("Failed to fetch %s from %s@%s: %v", filename, repo, ref, err)
			continue
		}

		// GitHub API returns base64-encoded content with embedded newlines (line-wrapping every ~76 chars).
		// The `gh api --jq .content` output is a raw string value (no surrounding quotes).
		// We strip all whitespace (newlines and spaces) from the base64 string before decoding.
		b64Content := strings.Map(func(r rune) rune {
			if r == '\n' || r == '\r' || r == ' ' {
				return -1 // remove character
			}
			return r
		}, strings.TrimSpace(string(output)))
		decoded, decErr := base64.StdEncoding.DecodeString(b64Content)
		if decErr != nil {
			safeOutputActionsLog.Printf("Failed to decode content for %s: %v", contentPath, decErr)
			continue
		}

		actionYAML, parseErr := parseActionYAMLContent(decoded)
		if parseErr != nil {
			safeOutputActionsLog.Printf("Failed to parse %s: %v", contentPath, parseErr)
			continue
		}

		return actionYAML, nil
	}

	return nil, fmt.Errorf("could not find action.yml or action.yaml in %s@%s (subdir=%q)", repo, ref, subdir)
}

// readLocalActionYAML reads and parses a local action.yml file.
func readLocalActionYAML(localPath, markdownPath string) (*actionYAMLFile, error) {
	baseDir := filepath.Dir(markdownPath)

	// Strip leading "./" from the local path
	cleanPath := strings.TrimPrefix(localPath, "./")
	actionDir := filepath.Join(baseDir, cleanPath)

	for _, filename := range []string{"action.yml", "action.yaml"} {
		fullPath := filepath.Join(actionDir, filename)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}
		return parseActionYAMLContent(content)
	}

	return nil, fmt.Errorf("could not find action.yml or action.yaml at %s", actionDir)
}

// parseActionYAMLContent parses raw action.yml YAML content.
func parseActionYAMLContent(content []byte) (*actionYAMLFile, error) {
	var parsed actionYAMLFile
	if err := yaml.Unmarshal(content, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse action YAML: %w", err)
	}
	return &parsed, nil
}

// isGitHubExpressionDefault returns true if the input's default value is a GitHub Actions
// expression (e.g., "${{ github.token }}" or "${{ github.event.pull_request.number }}").
// Such inputs should not be included in the MCP tool schema or the generated `with:` block
// so that GitHub Actions can apply the defaults naturally rather than having them overridden
// with an empty string from a missing JSON key in the agent payload.
func isGitHubExpressionDefault(input *ActionYAMLInput) bool {
	if input == nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(input.Default), "${{")
}

// generateActionToolDefinition creates an MCP tool definition for a custom safe output action.
// The tool name is the normalized action name. Inputs are derived from the action.yml.
func generateActionToolDefinition(actionName string, config *SafeOutputActionConfig) map[string]any {
	normalizedName := stringutil.NormalizeSafeOutputIdentifier(actionName)

	description := config.Description
	if description == "" {
		description = config.ActionDescription
	}
	if description == "" {
		description = fmt.Sprintf("Run the %s action", actionName)
	}
	// Append once-only constraint to description
	description += " (can only be called once)"

	// When action.yml could not be fetched at compile time (Inputs == nil), generate a
	// permissive fallback schema so the agent can still call the tool. The runtime step
	// passes the raw payload through a single `payload` input rather than individual fields.
	if config.Inputs == nil {
		inputSchema := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"payload": map[string]any{
					"type":        "string",
					"description": "JSON-encoded payload to pass to the action",
				},
			},
			"additionalProperties": true,
		}
		return map[string]any{
			"name":        normalizedName,
			"description": description,
			"inputSchema": inputSchema,
		}
	}

	inputSchema := map[string]any{
		"type":                 "object",
		"properties":           make(map[string]any),
		"additionalProperties": false,
	}

	var requiredFields []string
	properties, ok := inputSchema["properties"].(map[string]any)
	if !ok {
		properties = make(map[string]any)
		inputSchema["properties"] = properties
	}

	// Sort for deterministic output
	inputNames := sliceutil.SortedKeys(config.Inputs)

	for _, inputName := range inputNames {
		inputDef := config.Inputs[inputName]
		// Skip inputs whose defaults are GitHub expression (e.g. "${{ github.token }}").
		// These are implementation details (authentication, context values) that the agent
		// should not provide — GitHub Actions will apply the defaults automatically.
		if isGitHubExpressionDefault(inputDef) {
			continue
		}
		property := map[string]any{
			"type": "string",
		}
		if inputDef.Description != "" {
			property["description"] = inputDef.Description
		}
		if inputDef.Default != "" {
			property["default"] = inputDef.Default
		}
		if inputDef.Required {
			requiredFields = append(requiredFields, inputName)
		}
		properties[inputName] = property
	}

	if len(requiredFields) > 0 {
		sort.Strings(requiredFields)
		inputSchema["required"] = requiredFields
	}

	return map[string]any{
		"name":        normalizedName,
		"description": description,
		"inputSchema": inputSchema,
	}
}

// buildCustomSafeOutputActionsJSON builds a JSON mapping of normalized action names
// used for the GH_AW_SAFE_OUTPUT_ACTIONS env var of the handler manager step.
// This allows the handler manager to load and dispatch messages to action handlers.
// The map value is the normalized action name (same as key) for future extensibility.
func buildCustomSafeOutputActionsJSON(data *WorkflowData) string {
	if data.SafeOutputs == nil || len(data.SafeOutputs.Actions) == 0 {
		return ""
	}

	actionNames := make([]string, 0, len(data.SafeOutputs.Actions))
	for actionName := range data.SafeOutputs.Actions {
		actionNames = append(actionNames, actionName)
	}

	jsonStr, err := buildNormalizedSortedJSON(actionNames, func(normalizedName string) string {
		return normalizedName
	})
	if err != nil {
		safeOutputActionsLog.Printf("Warning: failed to marshal custom safe output actions: %v", err)
		return ""
	}
	return jsonStr
}

// actionOutputKey returns the step output key for a given normalized action name.
// The handler exports this key and the compiler uses it in step conditions and with: blocks.
func actionOutputKey(normalizedName string) string {
	return "action_" + normalizedName + "_payload"
}

// buildActionSteps generates the YAML steps for all configured safe output actions.
// Each step:
//   - Is guarded by an `if:` condition checking the payload output from process_safe_outputs
//   - Uses the resolved action reference
//   - Has a `with:` block populated from parsed payload output via fromJSON
func (c *Compiler) buildActionSteps(data *WorkflowData) []string {
	if data.SafeOutputs == nil || len(data.SafeOutputs.Actions) == 0 {
		return nil
	}

	// Sort action names for deterministic output
	actionNames := sliceutil.SortedKeys(data.SafeOutputs.Actions)

	var steps []string

	for _, actionName := range actionNames {
		config := data.SafeOutputs.Actions[actionName]
		normalizedName := stringutil.NormalizeSafeOutputIdentifier(actionName)
		outputKey := actionOutputKey(normalizedName)

		// Determine the action reference to use in the step
		actionRef := config.ResolvedRef
		if actionRef == "" {
			// Fall back to original uses value if resolution failed
			actionRef = config.Uses
		}

		// Display name: prefer the user description, then action description, then action name
		displayName := config.Description
		if displayName == "" {
			displayName = config.ActionDescription
		}
		if displayName == "" {
			displayName = actionName
		}

		steps = append(steps, fmt.Sprintf("      - name: %s\n", displayName))
		steps = append(steps, fmt.Sprintf("        id: action_%s\n", normalizedName))
		steps = append(steps, fmt.Sprintf("        if: steps.process_safe_outputs.outputs.%s != ''\n", outputKey))
		// Inject zizmor ignore annotation before uses: lines from unverified action creators.
		for _, prefix := range unverifiedCreatorActionPrefixes {
			if strings.HasPrefix(actionRef, prefix) {
				steps = append(steps, "        # zizmor: ignore[github_action_from_unverified_creator_used]\n")
				break
			}
		}
		steps = append(steps, fmt.Sprintf("        uses: %s\n", actionRef))

		// Build optional env: block for per-action environment variables
		if len(config.Env) > 0 {
			steps = append(steps, "        env:\n")
			envKeys := sliceutil.SortedKeys(config.Env)
			for _, envKey := range envKeys {
				steps = append(steps, fmt.Sprintf("          %s: %s\n", envKey, config.Env[envKey]))
			}
		}

		// Build the with: block
		if len(config.Inputs) > 0 {
			// Filter to only inputs that the agent should provide (exclude those with GitHub
			// expression defaults like "${{ github.token }}" — GitHub Actions applies them naturally).
			agentInputNames := make([]string, 0, len(config.Inputs))
			for k, v := range config.Inputs {
				if !isGitHubExpressionDefault(v) {
					agentInputNames = append(agentInputNames, k)
				}
			}
			sort.Strings(agentInputNames)

			if len(agentInputNames) > 0 {
				steps = append(steps, "        with:\n")
				for _, inputName := range agentInputNames {
					steps = append(steps, fmt.Sprintf("          %s: ${{ fromJSON(steps.process_safe_outputs.outputs.%s).%s }}\n",
						inputName, outputKey, inputName))
				}
			}
		} else {
			// When inputs couldn't be resolved, pass the raw payload as a single input
			steps = append(steps, "        with:\n")
			steps = append(steps, fmt.Sprintf("          payload: ${{ steps.process_safe_outputs.outputs.%s }}\n", outputKey))
		}
	}

	return steps
}

// resolveAllActions fetches action.yml for all configured actions and populates
// the computed fields (ResolvedRef, Inputs, ActionDescription) in each config.
// This should be called once during compilation before tool generation and step generation.
func (c *Compiler) resolveAllActions(data *WorkflowData, markdownPath string) {
	if data.SafeOutputs == nil || len(data.SafeOutputs.Actions) == 0 {
		return
	}

	safeOutputActionsLog.Printf("Resolving %d custom safe output action(s)", len(data.SafeOutputs.Actions))
	for actionName, config := range data.SafeOutputs.Actions {
		if config.ResolvedRef != "" {
			// Already resolved (e.g., called multiple times)
			continue
		}
		c.fetchAndParseActionYAML(actionName, config, markdownPath, data)
		safeOutputActionsLog.Printf("Resolved action %q: ref=%q, inputs=%d", actionName, config.ResolvedRef, len(config.Inputs))
	}
}

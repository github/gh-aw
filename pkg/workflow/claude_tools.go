package workflow

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/typeutil"
)

var claudeToolsLog = logger.New("workflow:claude_tools")

const defaultClaudeTmpWritePath = "/tmp"

// expandNeutralToolsToClaudeTools converts neutral tool names to Claude-specific tool configurations
func (e *ClaudeEngine) expandNeutralToolsToClaudeTools(tools map[string]any) map[string]any { //nolint:largefunc // Existing neutral-tool expansion remains centralized.
	claudeToolsLog.Printf("Starting neutral tools expansion: input_tools=%d", len(tools))
	result := make(map[string]any)

	neutralToolCount := 0
	// Count neutral tools
	for key := range tools {
		switch key {
		case "bash", "web-fetch", "web-search", "edit", "playwright":
			neutralToolCount++
		}
	}

	if neutralToolCount > 0 {
		claudeToolsLog.Printf("Expanding %d neutral tools to Claude-specific tools", neutralToolCount)
	}

	// Copy existing tools that are not neutral tools
	for key, value := range tools {
		switch key {
		case "bash", "web-fetch", "web-search", "edit", "playwright":
			// These are neutral tools that need conversion - skip copying, will be converted below
			continue
		default:
			// Copy MCP servers and other non-neutral tools as-is
			result[key] = value
		}
	}

	// Create or get existing claude section and allowed tools map
	claudeSection := getOrCreateToolMap(result, "claude")
	claudeAllowed := getOrCreateToolMap(claudeSection, "allowed")

	// Convert neutral tools to Claude tools
	if bashTool, hasBash := tools["bash"]; hasBash {
		// bash -> Bash, KillBash, BashOutput
		if bashCommands, ok := bashTool.([]any); ok {
			claudeAllowed["Bash"] = bashCommands
		} else {
			claudeAllowed["Bash"] = nil // Allow all bash commands
		}
	}

	if _, hasWebFetch := tools["web-fetch"]; hasWebFetch {
		// web-fetch -> WebFetch
		claudeAllowed["WebFetch"] = nil
	}

	if _, hasWebSearch := tools["web-search"]; hasWebSearch {
		// web-search -> WebSearch
		claudeAllowed["WebSearch"] = nil
	}

	if editTool, hasEdit := tools["edit"]; hasEdit && !isExplicitlyDisabledTool(editTool) {
		// edit -> Edit, MultiEdit, NotebookEdit, Write
		claudeAllowed["Edit"] = nil
		claudeAllowed["MultiEdit"] = nil
		claudeAllowed["NotebookEdit"] = nil
		claudeAllowed["Write"] = nil

		// If edit tool has specific configuration, we could handle it here
		// For now, treating it as enabling all edit capabilities
		_ = editTool
	}

	claudeToolsLog.Printf("Expansion complete: result_tools=%d, claude_allowed=%d", len(result), len(claudeAllowed))
	return result
}

func isExplicitlyDisabledTool(tool any) bool {
	enabled, ok := tool.(bool)
	return ok && !enabled
}

// computeAllowedClaudeToolsString generates the tool specification string for Claude's --allowed-tools flag.
//
// Why --allowed-tools instead of --tools (introduced in v2.0.31)?
// While --tools is simpler (e.g., "Bash,Edit,Read"), it lacks the fine-grained control gh-aw requires:
// - Specific bash commands: Bash(git:*), Bash(ls)
// - MCP tool prefixes: mcp__github__issue_read, mcp__github__*
// - Path-specific access: Read(/tmp/gh-aw/cache-memory/*)
//
// This function:
// 1. validates that only neutral tools are provided (no claude section)
// 2. converts neutral tools to Claude-specific tools format
// 3. adds default Claude tools and git commands based on safe outputs configuration
// 4. generates the allowed tools string for Claude
//
// System MCP servers (safeoutputs, mcpscripts, agenticworkflows) are not present in the
// user-visible tools map but must be explicitly added to --allowed-tools when
// --permission-mode acceptEdits is in use, because acceptEdits actually enforces the
// allowlist (unlike bypassPermissions which silently ignores it).
// Panics if callers pass a Claude-specific tools section instead of neutral tools.
func (e *ClaudeEngine) computeAllowedClaudeToolsString(tools map[string]any, safeOutputs *SafeOutputsConfig, cacheMemoryConfig *CacheMemoryConfig, driveMemoryConfig *DriveMemoryConfig, mcpScripts *MCPScriptsConfig, sandboxConfig *SandboxConfig) string {
	claudeToolsLog.Print("Computing allowed Claude tools string")

	tools = e.prepareClaudeToolsForAllowedList(tools)
	allowedTools := collectClaudeAllowedTools(tools)
	allowedTools = appendTopLevelClaudeTools(allowedTools, tools, cacheMemoryConfig, driveMemoryConfig)
	allowedTools = appendSandboxWritableTools(allowedTools, sandboxConfig)
	allowedTools = appendSafeOutputsTools(allowedTools, safeOutputs)
	allowedTools = appendMCPScriptsTools(allowedTools, mcpScripts)
	allowedTools = dedupeAllowedTools(allowedTools)

	// Sort the allowed tools alphabetically for consistent output
	sort.Strings(allowedTools)

	claudeToolsLog.Printf("Generated allowed tools string with %d tools", len(allowedTools))

	return strings.Join(allowedTools, ",")
}

// prepareClaudeToolsForAllowedList expands neutral tool definitions into Claude-specific
// format. Panics if tools already contains a "claude" section key, since callers must only
// ever pass neutral tool definitions at this stage (an internal invariant violation).
func (e *ClaudeEngine) prepareClaudeToolsForAllowedList(tools map[string]any) map[string]any {
	if tools == nil {
		tools = make(map[string]any)
	}
	if _, hasClaudeSection := tools["claude"]; hasClaudeSection {
		claudeToolsLog.Print("ERROR: Claude section found in input tools, should only contain neutral tools")
		panic("BUG: computeAllowedClaudeToolsString should only receive neutral tools, not claude section tools")
	}
	claudeToolsLog.Print("Converting neutral tools to Claude-specific format")
	tools = e.expandNeutralToolsToClaudeTools(tools)
	defaultClaudeTools := []string{"Task", "Glob", "Grep", "ExitPlanMode", "TodoWrite", "LS", "Read", "NotebookRead"}
	ensureDefaultClaudeAllowedTools(tools, defaultClaudeTools)
	claudeToolsLog.Printf("Added %d default Claude tools to allowed list", len(defaultClaudeTools))
	return tools
}

func ensureDefaultClaudeAllowedTools(tools map[string]any, defaultClaudeTools []string) {
	claudeSection := getOrCreateToolMap(tools, "claude")
	claudeAllowed := getOrCreateToolMap(claudeSection, "allowed")
	for _, defaultTool := range defaultClaudeTools {
		if _, exists := claudeAllowed[defaultTool]; !exists {
			claudeAllowed[defaultTool] = nil
		}
	}
	if _, hasBash := claudeAllowed["Bash"]; hasBash {
		if _, exists := claudeAllowed["KillBash"]; !exists {
			claudeAllowed["KillBash"] = nil
		}
		if _, exists := claudeAllowed["BashOutput"]; !exists {
			claudeAllowed["BashOutput"] = nil
		}
	}
}

func getOrCreateToolMap(container map[string]any, key string) map[string]any {
	if existing, ok := container[key]; ok {
		if existingMap, ok := existing.(map[string]any); ok {
			return existingMap
		}
	}
	created := make(map[string]any)
	container[key] = created
	return created
}

func collectClaudeAllowedTools(tools map[string]any) []string {
	claudeConfig, ok := typeutil.LookupMap(tools, "claude")
	if !ok {
		return nil
	}
	allowedMap, ok := typeutil.LookupMap(claudeConfig, "allowed")
	if !ok {
		return nil
	}
	allowedTools := make([]string, 0, len(allowedMap))
	for toolName, toolValue := range allowedMap {
		if toolName == "Bash" {
			allowedTools = appendClaudeBashTools(allowedTools, toolValue)
			continue
		}
		if isClaudeToolName(toolName) {
			allowedTools = append(allowedTools, toolName)
		}
	}
	return allowedTools
}

func appendClaudeBashTools(allowedTools []string, toolValue any) []string {
	bashCommands, ok := toolValue.([]any)
	if !ok {
		return append(allowedTools, "Bash")
	}
	if hasBashWildcard(bashCommands) {
		return append(allowedTools, "Bash")
	}
	for _, cmd := range bashCommands {
		if cmdStr, ok := cmd.(string); ok {
			normalized, _ := normalizeBashCommand(cmdStr)
			allowedTools = append(allowedTools, fmt.Sprintf("Bash(%s)", normalized))
		}
	}
	return allowedTools
}

func hasBashWildcard(commands []any) bool {
	for _, cmd := range commands {
		if cmdStr, ok := cmd.(string); ok && (cmdStr == ":*" || cmdStr == "*") {
			return true
		}
	}
	return false
}

// isClaudeToolName uses the existing Claude naming convention heuristic:
// valid Claude tool keys are expected to start with an uppercase ASCII letter.
func isClaudeToolName(toolName string) bool {
	return toolName != "" && toolName[0] >= 'A' && toolName[0] <= 'Z'
}

func appendTopLevelClaudeTools(allowedTools []string, tools map[string]any, cacheMemoryConfig *CacheMemoryConfig, driveMemoryConfig *DriveMemoryConfig) []string {
	for toolName, toolValue := range tools {
		if toolName == "claude" {
			continue
		}
		switch toolName {
		case "cache-memory":
			allowedTools = appendCacheMemoryTools(allowedTools, cacheMemoryConfig)
		case "drive-memory":
			allowedTools = appendDriveMemoryTools(allowedTools, driveMemoryConfig)
		case "agentic-workflows":
			allowedTools = append(allowedTools, "mcp__"+string(constants.AgenticWorkflowsMCPServerID))
		default:
			allowedTools = appendMCPToolPermissions(allowedTools, toolName, toolValue)
		}

	}
	return allowedTools
}

func appendDriveMemoryTools(allowedTools []string, config *DriveMemoryConfig) []string {
	if config == nil {
		return allowedTools
	}
	for _, drive := range config.Drives {
		memoryDir := driveMemoryDirFor(drive.ID)
		pattern := memoryDir + "/*" //nolint:manualpathconcat // Claude permission patterns require slash-separated globs.
		allowedTools = sliceutil.MergeUnique(allowedTools, fmt.Sprintf("Read(%s)", pattern))
		if !drive.RestoreOnly {
			allowedTools = sliceutil.MergeUnique(allowedTools, fmt.Sprintf("Write(%s)", pattern))
			allowedTools = sliceutil.MergeUnique(allowedTools, fmt.Sprintf("Edit(%s)", pattern))
			allowedTools = sliceutil.MergeUnique(allowedTools, fmt.Sprintf("MultiEdit(%s)", pattern))
		}
		allowedTools = appendCacheMemoryBashTools(allowedTools, memoryDir)
	}
	return allowedTools
}

func appendCacheMemoryTools(allowedTools []string, cacheMemoryConfig *CacheMemoryConfig) []string {
	if cacheMemoryConfig == nil {
		return allowedTools
	}
	for _, cache := range cacheMemoryConfig.Caches {
		cacheDir := cacheMemoryDirFor(cache.ID)
		cacheDirPattern := cacheDir + "/*" //nolint:manualpathconcat // Claude permission patterns require slash-separated globs.
		allowedTools = sliceutil.MergeUnique(allowedTools, fmt.Sprintf("Read(%s)", cacheDirPattern))
		allowedTools = sliceutil.MergeUnique(allowedTools, fmt.Sprintf("Write(%s)", cacheDirPattern))
		allowedTools = sliceutil.MergeUnique(allowedTools, fmt.Sprintf("Edit(%s)", cacheDirPattern))
		allowedTools = sliceutil.MergeUnique(allowedTools, fmt.Sprintf("MultiEdit(%s)", cacheDirPattern))
		allowedTools = appendCacheMemoryBashTools(allowedTools, cacheDir)
	}
	return allowedTools
}

func appendCacheMemoryBashTools(allowedTools []string, cacheDir string) []string {
	if slices.Contains(allowedTools, "Bash") {
		return allowedTools
	}
	cacheDirSlash := cacheDir + "/"
	bashCacheTools := []string{
		fmt.Sprintf("Bash(mkdir -p %s)", cacheDirSlash),
		fmt.Sprintf("Bash(cat %s)", cacheDirSlash),
		fmt.Sprintf("Bash(cat > %s)", cacheDirSlash),
		fmt.Sprintf("Bash(mv %s)", cacheDirSlash),
	}
	for _, bashTool := range bashCacheTools {
		allowedTools = sliceutil.MergeUnique(allowedTools, bashTool)
	}
	allowedTools = sliceutil.MergeUnique(allowedTools, "BashOutput")
	allowedTools = sliceutil.MergeUnique(allowedTools, "KillBash")
	return allowedTools
}

func appendMCPToolPermissions(allowedTools []string, toolName string, toolValue any) []string {
	mcpConfig, ok := toolValue.(map[string]any)
	if !ok {
		return allowedTools
	}
	isCustomMCP := false
	if hasMcp, _ := hasMCPConfig(mcpConfig); hasMcp {
		isCustomMCP = true
	}
	if toolName == "github" {
		return appendGitHubMCPTools(allowedTools, toolName, toolValue, mcpConfig)
	}
	if isCustomMCP {
		return appendGenericMCPTools(allowedTools, toolName, mcpConfig)
	}
	return allowedTools
}

func appendGitHubMCPTools(allowedTools []string, toolName string, toolValue any, mcpConfig map[string]any) []string {
	githubConfig := parseGitHubTool(toolValue)
	if githubConfig != nil && len(githubConfig.Allowed) > 0 {
		for _, tool := range githubConfig.Allowed {
			if string(tool) == "*" {
				return append(allowedTools, "mcp__"+toolName)
			}
		}
		for _, tool := range githubConfig.Allowed {
			allowedTools = append(allowedTools, fmt.Sprintf("mcp__%s__%s", toolName, string(tool)))
		}
		return allowedTools
	}
	githubMode := getGitHubType(mcpConfig)
	defaultTools := constants.DefaultGitHubToolsLocal
	if githubMode == GitHubMCPModeRemote {
		defaultTools = constants.DefaultGitHubToolsRemote
	}
	for _, defaultTool := range defaultTools {
		allowedTools = append(allowedTools, "mcp__github__"+defaultTool)
	}
	return allowedTools
}

func appendGenericMCPTools(allowedTools []string, toolName string, mcpConfig map[string]any) []string {
	allowed, hasAllowed := mcpConfig["allowed"]
	if !hasAllowed {
		return append(allowedTools, "mcp__"+toolName)
	}
	allowedSlice, ok := allowed.([]any)
	if !ok {
		return allowedTools
	}
	for _, item := range allowedSlice {
		if str, ok := item.(string); ok && str == "*" {
			return append(allowedTools, "mcp__"+toolName)
		}
	}
	for _, item := range allowedSlice {
		if str, ok := item.(string); ok {
			allowedTools = append(allowedTools, fmt.Sprintf("mcp__%s__%s", toolName, str))
		}
	}
	return allowedTools
}

func appendSandboxWritableTools(allowedTools []string, sandboxConfig *SandboxConfig) []string {
	if sandboxConfig == nil {
		return allowedTools
	}
	writablePaths := []string{defaultClaudeTmpWritePath}
	if sandboxConfig.Agent != nil && sandboxConfig.Agent.Config != nil && sandboxConfig.Agent.Config.Filesystem != nil {
		if allowWrite := sandboxConfig.Agent.Config.Filesystem.AllowWrite; allowWrite != nil {
			if sandboxConfig.Agent.Runtime == AgentRuntimeCloudHypervisor {
				writablePaths = allowWrite
			} else {
				writablePaths = append(writablePaths, allowWrite...)
			}
		}
	}
	seenPatterns := make(map[string]struct{}, len(writablePaths))
	for _, writablePath := range writablePaths {
		pattern, ok := normalizeSandboxWritablePattern(writablePath)
		if !ok {
			continue
		}
		if _, seen := seenPatterns[pattern]; seen {
			continue
		}
		seenPatterns[pattern] = struct{}{}
		allowedTools = sliceutil.MergeUnique(allowedTools, fmt.Sprintf("Read(%s)", pattern))
		allowedTools = sliceutil.MergeUnique(allowedTools, fmt.Sprintf("Write(%s)", pattern))
		allowedTools = sliceutil.MergeUnique(allowedTools, fmt.Sprintf("Edit(%s)", pattern))
		allowedTools = sliceutil.MergeUnique(allowedTools, fmt.Sprintf("MultiEdit(%s)", pattern))
	}
	return allowedTools
}

func normalizeSandboxWritablePattern(writablePath string) (string, bool) {
	path := strings.TrimSpace(writablePath)
	if path == "" || !strings.HasPrefix(path, "/") {
		return "", false
	}
	if strings.ContainsAny(path, "*?[]{}") {
		return path, true
	}
	return strings.TrimRight(path, "/") + "/*", true //nolint:manualpathconcat // Claude permission patterns require slash-separated globs.
}

func appendSafeOutputsTools(allowedTools []string, safeOutputs *SafeOutputsConfig) []string {
	if safeOutputs == nil {
		return allowedTools
	}
	allowedTools = append(allowedTools, "mcp__"+string(constants.SafeOutputsMCPServerID))
	if !slices.Contains(allowedTools, "Write") {
		// Ideally we would grant Write only for the exact safe outputs file, but Claude
		// doesn't currently honor that scoped grant reliably.
		// See: https://github.com/github/gh-aw/issues/244#issuecomment-3240319103
		allowedTools = append(allowedTools, "Write")
	}
	return allowedTools
}

func appendMCPScriptsTools(allowedTools []string, mcpScripts *MCPScriptsConfig) []string {
	if HasMCPScripts(mcpScripts) {
		allowedTools = append(allowedTools, "mcp__"+string(constants.MCPScriptsMCPServerID))
	}
	return allowedTools
}

func dedupeAllowedTools(allowedTools []string) []string {
	return sliceutil.Deduplicate(allowedTools)
}

// generateAllowedToolsComment generates a multi-line comment showing each allowed tool
func (e *ClaudeEngine) generateAllowedToolsComment(allowedToolsStr string, indent string) string {
	if allowedToolsStr == "" {
		return ""
	}

	tools := strings.Split(allowedToolsStr, ",")
	if len(tools) == 0 {
		return ""
	}

	// Pre-size the builder using the exact output size:
	//   - header line:  indent + "# Allowed tools (sorted):\n"
	//   - per tool:     indent + "# - " + toolName + "\n"
	// allowedToolsStr is comma-separated, so subtracting (len(tools)-1) gives the
	// total bytes contributed by tool names alone.
	toolNameBytes := len(allowedToolsStr) - (len(tools) - 1)
	var comment strings.Builder
	comment.Grow(
		len(indent) +
			len("# Allowed tools (sorted):\n") +
			len(tools)*len(indent) +
			len(tools)*len("# - \n") +
			toolNameBytes,
	)
	comment.WriteString(indent)
	comment.WriteString("# Allowed tools (sorted):\n")
	for _, tool := range tools {
		comment.WriteString(indent)
		comment.WriteString("# - ")
		comment.WriteString(tool)
		comment.WriteByte('\n')
	}

	return comment.String()
}

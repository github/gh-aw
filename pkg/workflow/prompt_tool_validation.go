package workflow

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/stringutil"
)

var promptToolImperative = regexp.MustCompile(`(?i)^(?:please\s+|you must\s+|must\s+)?(use|call|run)\s+(?:the\s+)?(?:tool\s+)?(.+)$`)
var promptQualifiedTool = regexp.MustCompile(`^(mcp__([A-Za-z0-9_-]+)__([A-Za-z0-9_-]+)|([A-Za-z0-9_-]+)\(([A-Za-z0-9_-]+)\))(?:$|[^A-Za-z0-9_])`)
var promptToolListSeparator = regexp.MustCompile("^`?\\s*(?:,\\s*(?:(?:and|or)\\s+)?|(?:and|or)\\s+)`?")
var promptMCPToolPhrase = regexp.MustCompile(`(?i)^(?:the\s+)?\x60?([a-z][a-z0-9_-]*)\x60?\s+(?:tool\s+)?(?:of|from)\s+(?:the\s+)?\x60?([a-z][a-z0-9_-]*)\x60?\s+MCP\s+server\b`)
var promptBareTool = regexp.MustCompile(`^([a-z][a-z0-9_]+)(?:$|[\s\x60.,(])`)
var promptListPrefix = regexp.MustCompile(`^(?:[-*+]\s+|\d+[.)]\s+)`)
var promptExecutable = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)
var promptGenericShell = regexp.MustCompile(`(?i)^(?:bash|shell)\x60?(?:\s+tool)?[.!]?$`)
var promptShellTaskIntro = regexp.MustCompile(`(?i)^(?:please\s+|you must\s+)?(?:find|list|count|inspect|check|query)\b`)
var promptShellNonInstruction = regexp.MustCompile(`(?i)\b(?:examples?|never|not|forbidden|avoid|without)\b`)
var promptNativeRead = regexp.MustCompile(`(?i)^read\([^()\n]+\)(?:$|[\s\x60.,])`)

type promptToolRequirement struct {
	server      string
	tool        string
	command     string
	nativeMCP   bool
	fullCommand string // SDK multiword rules may grant the full command, but not a pipeline segment.
}

// validatePromptTools is advisory: prompt text must never broaden security permissions.
// Only explicit imperatives and shell fences introduced as tasks are inspected.
func (c *Compiler) validatePromptTools(data *WorkflowData, markdownPath string) {
	if data == nil {
		return
	}
	contents := []string{data.MarkdownContent, data.MainWorkflowMarkdown, data.ImportedMarkdown}
	for _, entry := range data.PromptImports {
		contents = append(contents, entry.Markdown)
	}
	contents = append(contents, c.collectRuntimeImportMarkdownForCompilerAnalysis(data))
	var capabilities EngineCapabilities
	if engine, err := c.getAgenticEngine(ResolveEngineID(data)); err == nil {
		capabilities = engine.GetCapabilities()
	}
	seen := make(map[promptToolRequirement]bool)
	for _, content := range contents {
		for _, requirement := range promptToolRequirements(content) {
			key := requirement
			key.nativeMCP = false
			key.fullCommand = ""
			if seen[key] || promptToolAvailable(data, requirement, capabilities) {
				continue
			}
			seen[key] = true
			message := promptToolWarning(data, requirement)
			fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "warning", message))
			c.IncrementWarningCount()
		}
	}
}

func promptToolWarning(data *WorkflowData, requirement promptToolRequirement) string {
	name := requirement.server + "(" + requirement.tool + ")"
	advice := fmt.Sprintf("allow this specific tool in tools.%s.allowed and enable its server/toolset when needed", requirement.server)
	if requirement.command != "" {
		name = "shell(" + requirement.command + ")"
		advice = fmt.Sprintf("allow the specific command %q in tools.bash", requirement.command)
	} else if requirement.server == "bash" && requirement.tool == "" {
		name = "bash"
		advice = "enable tools.bash only for the specific commands the prompt needs"
	} else if requirement.server == "mcpscripts" {
		advice = "define the specific tool in mcp-scripts before requiring it"
	} else if requirement.server == "native-read" {
		name = "Read (native reads are unavailable for GitHub-backed Codex)"
		advice = "replace the native read instruction with an available, explicitly configured MCP tool"
	}
	if promptToolTransportUnavailable(data, requirement) {
		advice = fmt.Sprintf("use the %s CLI instead, or disable tools.cli-proxy to retain native MCP access", requirement.server)
		name = "mcp__" + requirement.server + "__" + requirement.tool + " (native MCP access is excluded by tools.cli-proxy)"
	}
	return fmt.Sprintf("Prompt explicitly requires %s, but the effective tool configuration does not allow it. Align the prompt with the permitted tools, or, if intended, %s. Permissions have not been expanded.", name, advice)
}

func promptToolRequirements(content string) []promptToolRequirement {
	content = removeXMLComments(content)
	knownTools := promptKnownTools()
	var requirements []promptToolRequirement
	var state promptToolScanState
	for rawLine := range strings.SplitSeq(content, "\n") {
		requirements = append(requirements, state.consumeLine(rawLine, knownTools)...)
	}
	return requirements
}

func promptKnownTools() map[string]promptToolRequirement {
	knownTools := make(map[string]promptToolRequirement)
	gitHubTools, _ := getGitHubToolToToolsetMap()
	for name := range gitHubTools {
		knownTools[name] = promptToolRequirement{server: "github", tool: name}
	}
	for _, handler := range safeOutputHandlers {
		if handler.ToolName == "" {
			continue
		}
		requirement := promptToolRequirement{server: "safeoutputs", tool: handler.ToolName}
		knownTools[handler.ToolName] = requirement
		for _, alias := range handler.Aliases {
			knownTools[stringutil.NormalizeSafeOutputIdentifier(alias)] = requirement
		}
	}
	return knownTools
}

type promptToolScanState struct {
	fence         string
	markdownFence bool
	nestedFence   string
	shellFence    strings.Builder
	runFence      bool
	pendingRun    bool
}

func (state *promptToolScanState) consumeLine(rawLine string, knownTools map[string]promptToolRequirement) []promptToolRequirement {
	line := strings.TrimSpace(rawLine)
	marker := ""
	for _, candidate := range []string{"```", "~~~"} {
		if strings.HasPrefix(line, candidate) {
			marker = line[:len(line)-len(strings.TrimLeft(line, candidate[:1]))]
			break
		}
	}
	if state.fence != "" {
		// Markdown output templates sometimes use same-length fences for embedded examples.
		if state.nestedFence != "" {
			if isClosingCodeFence(line, state.nestedFence) {
				state.nestedFence = ""
			}
			return nil
		}
		if state.markdownFence && marker == state.fence && strings.TrimSpace(strings.TrimPrefix(line, marker)) != "" {
			state.nestedFence = marker
			return nil
		}
		if marker != "" && isClosingCodeFence(line, state.fence) {
			var requirements []promptToolRequirement
			if state.runFence {
				requirements = promptShellRequirements(state.shellFence.String())
			}
			state.fence, state.runFence = "", false
			state.markdownFence = false
			state.shellFence.Reset()
			return requirements
		}
		if state.runFence {
			state.shellFence.WriteString(rawLine)
			state.shellFence.WriteByte('\n')
		}
		return nil
	}
	if marker != "" {
		state.fence = marker
		language := strings.TrimSpace(strings.TrimPrefix(line, marker))
		state.runFence = state.pendingRun && slices.Contains([]string{"bash", "sh", "shell"}, language)
		state.markdownFence = slices.Contains([]string{"markdown", "md"}, strings.ToLower(language))
		state.pendingRun = false
		return nil
	}
	if line == "" {
		return nil
	}
	line = promptListPrefix.ReplaceAllString(line, "")
	requirements, pending := promptInstructionRequirements(line, knownTools)
	state.pendingRun = pending
	return requirements
}

func promptInstructionRequirements(line string, knownTools map[string]promptToolRequirement) ([]promptToolRequirement, bool) {
	match := promptToolImperative.FindStringSubmatch(line)
	if len(match) < 3 {
		return nil, promptShellTaskIntro.MatchString(line) && !promptShellNonInstruction.MatchString(line)
	}
	var verb, target string
	for index, group := range match {
		switch index {
		case 1:
			verb = group
		case 2:
			target = strings.TrimSpace(group)
		}
	}
	pending := strings.EqualFold(verb, "run") && slices.ContainsFunc([]string{":", "the following:", "following:", "the following commands:", "following commands:", "commands:"}, func(intro string) bool { return strings.EqualFold(intro, target) })
	return promptInvocationRequirements(verb, target, knownTools), pending
}

func promptInvocationRequirements(verb, target string, knownTools map[string]promptToolRequirement) []promptToolRequirement {
	inline := strings.HasPrefix(target, "`")
	target = strings.TrimPrefix(target, "`")
	if strings.EqualFold(verb, "run") {
		if match := promptMCPToolPhrase.FindStringSubmatch(target); len(match) > 2 {
			captures := [3]string(match)
			tool, server := captures[1], captures[2]
			canonical := strings.ToLower(server)
			if strings.EqualFold(server, "gh-aw") {
				server = constants.AgenticWorkflowsMCPServerID.String()
			} else if builtInToolNames[canonical] || slices.Contains([]string{
				constants.SafeOutputsMCPServerID.String(),
				constants.MCPScriptsMCPServerID.String(),
				constants.AgenticWorkflowsMCPServerID.String(),
			}, canonical) {
				server = canonical
			}
			return []promptToolRequirement{{server: server, tool: tool, nativeMCP: true}}
		}
	}
	if (strings.EqualFold(verb, "use") || strings.EqualFold(verb, "call")) && promptNativeRead.MatchString(target) {
		return []promptToolRequirement{{server: "native-read", tool: "Read"}}
	}
	if promptGenericShell.MatchString(target) {
		return []promptToolRequirement{{server: "bash"}}
	}
	wrapper, argument, hasArgument := strings.Cut(target, "(")
	if hasArgument && (strings.EqualFold(wrapper, "shell") || strings.EqualFold(wrapper, "bash")) {
		if end := strings.LastIndex(argument, ")"); end >= 0 {
			command, _ := normalizeBashCommand(strings.TrimSpace(argument[:end]))
			command = strings.TrimSuffix(command, ":*")
			return promptShellRequirements(command)
		}
		return nil
	}
	if requirements, qualified := promptQualifiedRequirements(target); qualified {
		return requirements
	}
	if strings.EqualFold(verb, "run") && inline {
		if end := strings.IndexByte(target, '`'); end >= 0 {
			return promptShellRequirements(target[:end])
		}
		return nil
	}
	if strings.EqualFold(verb, "run") && promptLiteralShellCommand(target) {
		return promptShellRequirements(strings.TrimSuffix(target, "."))
	}
	if strings.EqualFold(verb, "use") || strings.EqualFold(verb, "call") {
		if bare := promptBareTool.FindStringSubmatch(target); len(bare) >= 2 {
			for _, name := range bare[1:] {
				if requirement, known := knownTools[name]; known {
					return []promptToolRequirement{requirement}
				}
			}
		}
	}
	return nil
}

func promptQualifiedRequirements(target string) ([]promptToolRequirement, bool) {
	var requirements []promptToolRequirement
	qualified := false
	for {
		groups := promptQualifiedTool.FindStringSubmatch(target)
		if len(groups) < 2 {
			break
		}
		var invocation, server, tool string
		for index, group := range groups {
			if group == "" {
				continue
			}
			switch index {
			case 1:
				invocation = group
			case 2, 4:
				server = group
			case 3, 5:
				tool = group
			}
		}
		// Parenthesized "(s)" is ambiguous pluralization; explicit MCP names are not.
		if tool == "s" && !strings.HasPrefix(invocation, "mcp__") {
			break
		}
		qualified = true
		// Native read tools are available by default; Read(path) is not an MCP reference.
		if !slices.Contains([]string{"Read", "read", "Glob", "glob", "Grep", "grep", "LS", "NotebookRead", "read_file", "read_many_files", "list_directory", "grep_search", "view"}, server) {
			requirements = append(requirements, promptToolRequirement{server: server, tool: tool, nativeMCP: true})
		}
		target = target[len(invocation):]
		separator := promptToolListSeparator.FindString(target)
		if separator == "" {
			break
		}
		target = target[len(separator):]
	}
	return requirements, qualified
}

func promptLiteralShellCommand(target string) bool {
	// Unquoted prose is ambiguous. Recognize established command stems or a
	// literal executable followed by an option; other commands require backticks.
	var command string
	for index, field := range strings.Fields(target) {
		switch index {
		case 0:
			command = field
		case 1:
			return constants.CopilotStemCommands[command] ||
				(promptExecutable.MatchString(command) && strings.HasPrefix(field, "-"))
		}
	}
	return false
}

// Split only simple literal shell commands, respecting quotes (notably jq filters).
// Expansions, subshells, redirections and control structures are deliberately not inferred.
func promptShellRequirements(script string) []promptToolRequirement {
	commands, valid := splitPromptShellCommands(script)
	if !valid {
		return nil
	}
	var requirements []promptToolRequirement
	for _, command := range commands {
		command = strings.TrimSpace(command)
		if command == "" {
			continue
		}
		for field := range strings.FieldsSeq(command) {
			if strings.ContainsAny(field, "='\"") || slices.Contains([]string{"if", "then", "else", "fi", "for", "while", "do", "done", "case", "esac", "function", "!"}, field) {
				return nil
			}
			break
		}
		requirements = append(requirements, promptToolRequirement{command: command})
	}
	if len(requirements) > 1 {
		for i := range requirements {
			requirements[i].fullCommand = strings.TrimSpace(script)
		}
	}
	return requirements
}

func splitPromptShellCommands(script string) ([]string, bool) {
	var commands []string
	start, skipTo := 0, 0
	var quote rune
	escaped := false
	for i, ch := range script {
		if i < skipTo {
			continue
		}
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
		case '$', '`', '(', ')', '<', '>':
			return nil, false
		case '#':
			if i == start || strings.HasSuffix(script[:i], " ") || strings.HasSuffix(script[:i], "\t") {
				commands = append(commands, script[start:i])
				if end := strings.IndexByte(script[i:], '\n'); end >= 0 {
					skipTo = i + end + 1
				} else {
					skipTo = len(script)
				}
				start = skipTo
			}
		case '|', '&', ';', '\n':
			commands = append(commands, script[start:i])
			start = i + 1
		}
	}
	if quote != 0 || escaped {
		return nil, false
	}
	commands = append(commands, script[start:])
	return commands, true
}

func promptToolTransportUnavailable(data *WorkflowData, requirement promptToolRequirement) bool {
	return requirement.nativeMCP && slices.Contains(getMCPCLIExcludeFromAgentConfig(data), requirement.server)
}

func promptToolAvailable(data *WorkflowData, requirement promptToolRequirement, capabilities EngineCapabilities) bool {
	if requirement.command != "" || (requirement.server == "bash" && requirement.tool == "") {
		return promptShellAvailable(data, requirement.command, requirement.fullCommand, capabilities)
	}
	if promptToolTransportUnavailable(data, requirement) {
		return false
	}
	// Framework-generated servers are not necessarily present in the tools map.
	switch requirement.server {
	case "native-read":
		return ResolveEngineID(data) != "codex" || NewCodexEngine().ResolveLLMProvider(data) != LLMProviderGitHub
	case "safeoutputs":
		return slices.Contains(collectSafeOutputsManifestTools(data.SafeOutputs), requirement.tool)
	case "mcpscripts":
		if !IsMCPScriptsEnabled(data.MCPScripts) {
			return false
		}
		tool, present := data.MCPScripts.Tools[requirement.tool]
		return present && tool != nil
	case "agenticworkflows":
		return true // Availability depends on engine runtime features, not tools.allowed.
	}
	value, present := data.Tools[requirement.server]
	if !present || isToolExplicitlyFalse(value) {
		return false
	}
	config, ok := value.(map[string]any)
	if !ok {
		return true
	}
	if allowed, restricted := config["allowed"]; restricted {
		list := parseStringSliceAny(allowed, nil)
		if requirement.server == "github" {
			list = getGitHubAllowedTools(config)
		}
		if !slices.Contains(list, "*") && !slices.Contains(list, requirement.tool) {
			return false
		}
	}
	if requirement.server == "github" {
		toolToToolset, _ := getGitHubToolToToolsetMap()
		if toolset, known := toolToToolset[requirement.tool]; known {
			return slices.Contains(ParseGitHubToolsets(getGitHubToolsets(config)), toolset)
		}
	}
	return true
}

func promptShellAvailable(data *WorkflowData, command, fullCommand string, capabilities EngineCapabilities) bool {
	if data.BashDisabled || isBashExplicitlyRefused(data.Tools) {
		return !capabilities.BashDisable && !capabilities.BashCommandAllowlist
	}
	// Engines without command allowlisting retain their native shell defaults.
	if !capabilities.BashCommandAllowlist {
		return true
	}
	engine := ResolveEngineID(data)
	tools := withMountedCLIShellCommandsInRestrictedBash(data)
	value, present := tools["bash"]
	if !present || isToolExplicitlyFalse(value) {
		return false
	}
	allowed, restricted := value.([]any)
	if !restricted {
		return true
	}
	if command == "" {
		return len(allowed) > 0
	}
	for _, entry := range allowed {
		prefix, ok := entry.(string)
		if !ok {
			continue
		}
		prefix = strings.TrimSpace(prefix)
		piWildcard := strings.HasSuffix(prefix, " *") || strings.HasSuffix(prefix, ":*")
		prefix, _ = normalizeBashCommand(prefix)
		if prefix == "*" || prefix == ":*" {
			return true
		}
		colonWildcard := strings.HasSuffix(prefix, ":*")
		prefix = strings.TrimSuffix(prefix, ":*")
		if engine == "copilot" {
			prefix, _ = sanitizeCopilotShellCommand(prefix)
		}
		// Match canonical engine rules, not the spelling before wildcard normalization.
		exactOnly := engine == "claude" && !colonWildcard
		if engine == "copilot" && isCopilotSDKMode(data) {
			exactOnly = strings.Contains(prefix, " ") && !colonWildcard
			if exactOnly && fullCommand != "" {
				if fullCommand == prefix {
					return true
				}
				continue
			}
		}
		if engine == "pi" {
			exactOnly = strings.Contains(prefix, " ") && !piWildcard
		}
		if prefix != "" && (command == prefix || (!exactOnly && strings.HasPrefix(command, prefix+" "))) {
			return true
		}
	}
	return false
}

package workflow

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const copilotSDKToolConfigVersion = 1

type copilotSDKToolCapabilities struct {
	Bash      bool `json:"bash"`
	Edit      bool `json:"edit"`
	WebFetch  bool `json:"webFetch"`
	WebSearch bool `json:"webSearch"`
	MCP       bool `json:"mcp"`
	CLIProxy  bool `json:"cliProxy"`
}

type copilotSDKPermissionConfig struct {
	AllowedTools []string `json:"allowedTools"`
}

type copilotSDKToolConfig struct {
	Version                 int                        `json:"version"`
	Capabilities            copilotSDKToolCapabilities `json:"capabilities"`
	Permissions             copilotSDKPermissionConfig `json:"permissions"`
	ExplicitlyDisabledTools []string                   `json:"explicitlyDisabledTools,omitempty"`
}

func isCopilotSDKMode(workflowData *WorkflowData) bool {
	return workflowData != nil && workflowData.EngineConfig != nil && workflowData.EngineConfig.CopilotSDK
}

func isCopilotToolValueEnabled(tools map[string]any, name string) bool {
	value, exists := tools[name]
	if !exists {
		return false
	}
	if enabled, ok := value.(bool); ok {
		return enabled
	}
	return true
}

func isCopilotEditToolEnabled(tools map[string]any, workflowData *WorkflowData) bool {
	if workflowData != nil {
		// ExplicitlyDisabledTools is captured before default-tool resolution can
		// remove edit:false. Keep this guard before drive-memory so an author
		// can explicitly refuse write/edit tools even when drive-memory is used.
		if _, explicitlyDisabled := workflowData.ExplicitlyDisabledTools["edit"]; explicitlyDisabled {
			return false
		}
	}
	if value, configured := tools["edit"]; configured {
		if enabled, ok := value.(bool); ok {
			return enabled
		}
		return true
	}
	if workflowData != nil && workflowData.DriveMemoryConfig != nil && len(workflowData.DriveMemoryConfig.Drives) > 0 {
		return true
	}
	if workflowData != nil && workflowData.ParsedTools != nil {
		return workflowData.ParsedTools.Edit != nil
	}
	return false
}

func validateCopilotSDKEngineArgs(workflowData *WorkflowData) error {
	if !isCopilotSDKMode(workflowData) || len(workflowData.EngineConfig.Args) == 0 {
		return nil
	}
	for _, arg := range workflowData.EngineConfig.Args {
		flag, _, _ := strings.Cut(arg, "=")
		switch flag {
		case "--allow-tool", "--allow-all-tools", "--deny-tool", "--available-tools", "--excluded-tools", "--allow-all", "--yolo":
			return fmt.Errorf("engine.args cannot override Copilot SDK tool permissions with %q; configure workflow tools instead", flag)
		}
	}
	return nil
}

func isCopilotBashToolEnabled(workflowData *WorkflowData) bool {
	if workflowData == nil || workflowData.BashDisabled || workflowData.Tools == nil {
		return false
	}
	value, exists := workflowData.Tools["bash"]
	if !exists {
		return false
	}
	switch bash := value.(type) {
	case bool:
		return bash
	case []any:
		return len(bash) > 0
	default:
		return true
	}
}

func hasCopilotSDKMCPTools(workflowData *WorkflowData) bool {
	if workflowData == nil {
		return false
	}
	if HasSafeOutputsEnabled(workflowData.SafeOutputs) || IsMCPScriptsEnabled(workflowData.MCPScripts) {
		return true
	}
	for name, value := range workflowData.Tools {
		if !isCopilotToolValueEnabled(workflowData.Tools, name) {
			continue
		}
		switch name {
		case "github":
			// ExplicitlyDisabledTools is captured before default-tool resolution, which can
			// re-add a "github" entry (e.g. for steering issue comments) even when the author
			// explicitly set github: false. Honor the author's explicit refusal here.
			if _, explicitlyDisabled := workflowData.ExplicitlyDisabledTools["github"]; explicitlyDisabled {
				continue
			}
			return true
		case "bash", "edit", "web-fetch", "web-search", "playwright",
			"agentic-workflows", "cache-memory", "drive-memory", "repo-memory",
			"comment-memory", "cli-proxy", "timeout", "startup-timeout":
			continue
		}
		if config, ok := value.(map[string]any); ok {
			if hasMCP, _ := hasMCPConfig(config); hasMCP {
				return true
			}
		}
	}
	return false
}

func extractCopilotAllowedTools(args []string) []string {
	allowed := map[string]struct{}{"read": {}}
	for i := 0; i < len(args)-1; i++ {
		if args[i] != "--allow-tool" {
			continue
		}
		value := strings.TrimSpace(args[i+1])
		if value != "" && !strings.HasPrefix(value, "--") {
			allowed[value] = struct{}{}
		}
		i++
	}
	result := make([]string, 0, len(allowed))
	for value := range allowed {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func buildCopilotSDKToolConfig(workflowData *WorkflowData, toolArgs []string) copilotSDKToolConfig {
	if workflowData == nil {
		workflowData = &WorkflowData{}
	}
	tools := workflowData.Tools
	config := copilotSDKToolConfig{
		Version: copilotSDKToolConfigVersion,
		Capabilities: copilotSDKToolCapabilities{
			Bash:     isCopilotBashToolEnabled(workflowData),
			Edit:     isCopilotEditToolEnabled(tools, workflowData),
			WebFetch: isCopilotToolValueEnabled(tools, "web-fetch"),
			// The Copilot SDK runtime cannot authorize or execute web-search (see
			// WebSearch: false in copilot_engine.go), so never advertise it as SDK-visible
			// even if the workflow declares tools.web-search.
			WebSearch: false,
			MCP:       hasCopilotSDKMCPTools(workflowData),
			CLIProxy:  workflowData.ParsedTools != nil && workflowData.ParsedTools.CLIProxy,
		},
		Permissions: copilotSDKPermissionConfig{
			AllowedTools: extractCopilotAllowedTools(toolArgs),
		},
	}
	for name := range workflowData.ExplicitlyDisabledTools {
		config.ExplicitlyDisabledTools = append(config.ExplicitlyDisabledTools, name)
	}
	if _, explicitlyDisabled := workflowData.ExplicitlyDisabledTools["bash"]; workflowData.BashDisabled && !explicitlyDisabled {
		config.ExplicitlyDisabledTools = append(config.ExplicitlyDisabledTools, "bash")
	}
	sort.Strings(config.ExplicitlyDisabledTools)
	return config
}

func buildCopilotSDKToolConfigJSON(workflowData *WorkflowData, toolArgs []string) string {
	if !isCopilotSDKMode(workflowData) {
		return ""
	}
	configJSON, err := json.Marshal(buildCopilotSDKToolConfig(workflowData, toolArgs))
	if err != nil {
		panic(fmt.Sprintf("BUG: failed to marshal Copilot SDK tool config: %v", err))
	}
	return string(configJSON)
}

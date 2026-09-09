//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestCollectExplicitlyDisabledTools(t *testing.T) {
	tools := map[string]any{
		"bash":       false,
		"edit":       true,
		"github":     false,
		"web-fetch":  nil,
		"custom-mcp": map[string]any{"command": "server"},
	}
	expected := map[string]struct{}{"bash": {}, "github": {}}
	if actual := collectExplicitlyDisabledTools(tools); !reflect.DeepEqual(actual, expected) {
		t.Fatalf("collectExplicitlyDisabledTools() = %#v, want %#v", actual, expected)
	}
}

func TestBuildCopilotSDKToolConfigPreservesTriState(t *testing.T) {
	tests := []struct {
		name         string
		workflowData *WorkflowData
		args         []string
		capabilities copilotSDKToolCapabilities
		permissions  []string
		disabled     []string
	}{
		{
			name: "absent tools remain unavailable",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{CopilotSDK: true},
				Tools:        map[string]any{},
				ParsedTools:  NewTools(map[string]any{}),
			},
			capabilities: copilotSDKToolCapabilities{},
			permissions:  []string{"read"},
		},
		{
			name: "explicit false remains distinguishable from absence",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{CopilotSDK: true},
				Tools: map[string]any{
					"bash":      false,
					"edit":      false,
					"web-fetch": false,
					"github":    false,
					"cli-proxy": false,
				},
				ParsedTools: NewTools(map[string]any{
					"bash":      false,
					"edit":      false,
					"web-fetch": false,
					"github":    false,
					"cli-proxy": false,
				}),
				BashDisabled: true,
				ExplicitlyDisabledTools: map[string]struct{}{
					"bash": {}, "cli-proxy": {}, "edit": {}, "github": {}, "web-fetch": {},
				},
			},
			capabilities: copilotSDKToolCapabilities{},
			permissions:  []string{"read"},
			disabled:     []string{"bash", "cli-proxy", "edit", "github", "web-fetch"},
		},
		{
			name: "enabled tools map to permissions and capabilities",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{CopilotSDK: true},
				Tools: map[string]any{
					"bash":       []any{"*"},
					"edit":       true,
					"web-fetch":  nil,
					"web-search": nil,
					"github":     map[string]any{},
					"cli-proxy":  true,
				},
				ParsedTools: NewTools(map[string]any{
					"bash":       []any{"*"},
					"edit":       true,
					"web-fetch":  nil,
					"web-search": nil,
					"github":     map[string]any{},
					"cli-proxy":  true,
				}),
			},
			args: []string{
				"--allow-tool", "write",
				"--allow-tool", "web_fetch",
				"--allow-tool", "shell",
				"--allow-tool", "github",
			},
			capabilities: copilotSDKToolCapabilities{
				Bash: true, Edit: true, WebFetch: true, WebSearch: false, MCP: true, CLIProxy: true,
			},
			permissions: []string{"github", "read", "shell", "web_fetch", "write"},
		},
		{
			name: "empty bash allowlist is explicitly disabled",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{CopilotSDK: true},
				Tools:        map[string]any{"bash": []any{}},
				ParsedTools:  NewTools(map[string]any{"bash": []any{}}),
				BashDisabled: true,
			},
			capabilities: copilotSDKToolCapabilities{},
			permissions:  []string{"read"},
			disabled:     []string{"bash"},
		},
		{
			// Default-tool resolution re-adds a "github" entry for steering issue comments
			// even when the author explicitly set github: false. The SDK contract must still
			// honor the explicit refusal and keep MCP/github out of the visible capabilities.
			name: "explicit github false stays disabled despite steering issue comments re-adding github",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{CopilotSDK: true},
				Tools: map[string]any{
					"github": map[string]any{"allowed": []any{"issue_read"}},
				},
				ParsedTools: NewTools(map[string]any{
					"github": map[string]any{"allowed": []any{"issue_read"}},
				}),
				ExplicitlyDisabledTools: map[string]struct{}{"github": {}},
			},
			capabilities: copilotSDKToolCapabilities{},
			permissions:  []string{"read"},
			disabled:     []string{"github"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := buildCopilotSDKToolConfig(tt.workflowData, tt.args)
			if config.Version != copilotSDKToolConfigVersion {
				t.Fatalf("Version = %d, want %d", config.Version, copilotSDKToolConfigVersion)
			}
			if !reflect.DeepEqual(config.Capabilities, tt.capabilities) {
				t.Errorf("Capabilities = %#v, want %#v", config.Capabilities, tt.capabilities)
			}
			if !reflect.DeepEqual(config.Permissions.AllowedTools, tt.permissions) {
				t.Errorf("AllowedTools = %#v, want %#v", config.Permissions.AllowedTools, tt.permissions)
			}
			if !reflect.DeepEqual(config.ExplicitlyDisabledTools, tt.disabled) {
				t.Errorf("ExplicitlyDisabledTools = %#v, want %#v", config.ExplicitlyDisabledTools, tt.disabled)
			}
		})
	}
}

func TestBuildCopilotSDKToolConfigJSONOnlyForSDKMode(t *testing.T) {
	workflowData := &WorkflowData{
		EngineConfig: &EngineConfig{CopilotSDK: false},
		Tools:        map[string]any{"edit": true},
	}
	if actual := buildCopilotSDKToolConfigJSON(workflowData, []string{"--allow-tool", "write"}); actual != "" {
		t.Fatalf("buildCopilotSDKToolConfigJSON() = %q, want empty string outside SDK mode", actual)
	}
}

func TestValidateCopilotSDKEngineArgs(t *testing.T) {
	tests := []struct {
		name      string
		config    *EngineConfig
		wantError bool
	}{
		{name: "SDK permission flag", config: &EngineConfig{CopilotSDK: true, Args: []string{"--allow-tool", "write"}}, wantError: true},
		{name: "SDK equals permission flag", config: &EngineConfig{CopilotSDK: true, Args: []string{"--excluded-tools=bash"}}, wantError: true},
		{name: "SDK unrelated flag", config: &EngineConfig{CopilotSDK: true, Args: []string{"--no-custom-instructions"}}},
		{name: "CLI permission flag remains compatible", config: &EngineConfig{CopilotSDK: false, Args: []string{"--allow-tool", "write"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCopilotSDKEngineArgs(&WorkflowData{EngineConfig: tt.config})
			if tt.wantError && err == nil {
				t.Fatal("validateCopilotSDKEngineArgs() error = nil, want error")
			}
			if !tt.wantError && err != nil {
				t.Fatalf("validateCopilotSDKEngineArgs() error = %v, want nil", err)
			}
		})
	}
}

func TestCopilotSDKExplicitEditDisableOverridesDriveMemory(t *testing.T) {
	workflowData := &WorkflowData{
		EngineConfig:            &EngineConfig{CopilotSDK: true},
		Tools:                   map[string]any{"edit": false},
		ParsedTools:             NewTools(map[string]any{"edit": false}),
		ExplicitlyDisabledTools: map[string]struct{}{"edit": {}},
		DriveMemoryConfig:       &DriveMemoryConfig{Drives: []DriveMemoryEntry{{ID: "notes"}}},
	}
	toolArgs := NewCopilotEngine().computeCopilotToolArguments(workflowData.Tools, nil, nil, workflowData)
	if len(toolArgs) != 0 {
		t.Fatalf("computeCopilotToolArguments() = %#v, want no write permission", toolArgs)
	}
	config := buildCopilotSDKToolConfig(workflowData, toolArgs)
	if config.Capabilities.Edit {
		t.Fatal("Capabilities.Edit = true, want false for explicitly disabled edit")
	}
}

func TestCopilotSDKWebFetchContractFixtureMatchesCompiler(t *testing.T) {
	const workflowMarkdown = `---
on: workflow_dispatch
permissions:
  contents: read
engine:
  id: copilot
  copilot-sdk: true
tools:
  bash: false
  cli-proxy: false
  edit: false
  github: false
  web-fetch:
---

# SDK web fetch contract fixture
`
	compiler := NewCompiler(WithSkipValidation(true))
	workflowData, err := compiler.ParseWorkflowString(workflowMarkdown, "sdk-web-fetch-contract.md")
	if err != nil {
		t.Fatalf("ParseWorkflowString() error = %v", err)
	}

	var fixture struct {
		ServerArgs []string             `json:"serverArgs"`
		ToolConfig copilotSDKToolConfig `json:"toolConfig"`
	}
	fixtureJSON, err := os.ReadFile("../../actions/setup/js/testdata/copilot_sdk_web_fetch_contract.json")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if err := json.Unmarshal(fixtureJSON, &fixture); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	copilotArgs, toolArgs := NewCopilotEngine().buildCopilotArgs(workflowData)
	var contractArgs []string
	for index := 0; index < len(copilotArgs); index++ {
		switch copilotArgs[index] {
		case "--disable-builtin-mcps", "--no-ask-user":
			contractArgs = append(contractArgs, copilotArgs[index])
		case "--allow-tool":
			if index+1 < len(copilotArgs) {
				contractArgs = append(contractArgs, copilotArgs[index], copilotArgs[index+1])
				index++
			}
		}
	}
	if !reflect.DeepEqual(contractArgs, fixture.ServerArgs) {
		t.Errorf("compiler server args = %#v, want fixture %#v", contractArgs, fixture.ServerArgs)
	}
	actualConfig := buildCopilotSDKToolConfig(workflowData, toolArgs)
	if !reflect.DeepEqual(actualConfig, fixture.ToolConfig) {
		t.Errorf("compiler tool config = %#v, want fixture %#v", actualConfig, fixture.ToolConfig)
	}
}

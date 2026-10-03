package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/pelletier/go-toml/v2"
)

type codexNativeConfig struct {
	Defaults       map[string]any `json:"defaults"`
	Overrides      map[string]any `json:"overrides"`
	DisablePlugins bool           `json:"disablePlugins"`
}

func parseCodexConfig(config string) (map[string]any, error) {
	result := make(map[string]any)
	if strings.TrimSpace(config) == "" {
		return result, nil
	}
	if strings.Contains(config, "${{") {
		return nil, errors.New("engine.config: GitHub Actions expressions are not supported in Codex TOML; set the value in engine.env and reference it as ${ENV_VAR} instead")
	}
	if err := toml.Unmarshal([]byte(config), &result); err != nil {
		return nil, fmt.Errorf("engine.config: invalid Codex TOML configuration: %w", err)
	}
	if err := validateCodexConfigValues(result); err != nil {
		return nil, err
	}
	if policy, ok := result["shell_environment_policy"].(map[string]any); ok {
		if _, canonical := policy["filters"]; canonical {
			for _, legacy := range []string{"exclude", "include_only"} {
				if _, exists := policy[legacy]; exists {
					return nil, fmt.Errorf("engine.config: shell_environment_policy mixes filters with %s; use either a filters table or legacy exclude/include_only arrays, not both", legacy)
				}
			}
		}
	}
	return result, nil
}

func validateCodexConfigValues(value any) error {
	switch typed := value.(type) {
	case map[string]any:
		for _, entry := range typed {
			if err := validateCodexConfigValues(entry); err != nil {
				return err
			}
		}
	case []any:
		for _, entry := range typed {
			if err := validateCodexConfigValues(entry); err != nil {
				return err
			}
		}
	case string, bool:
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return errors.New("engine.config: Codex numeric values must be finite")
		}
	case int64:
		if typed < -9007199254740991 || typed > 9007199254740991 {
			return errors.New("engine.config: Codex integer values must be within the runtime's safe integer range")
		}
	default:
		return fmt.Errorf("engine.config: unsupported Codex TOML value type %T; use strings, booleans, numbers, arrays, or tables", value)
	}
	return nil
}

func (e *CodexEngine) buildNativeConfig(workflowData *WorkflowData, mcpTools []string) (*codexNativeConfig, error) {
	config := &codexNativeConfig{
		Defaults: map[string]any{
			"history": map[string]any{"persistence": "none"},
			"otel":    map[string]any{"metrics_exporter": "none"},
			"shell_environment_policy": map[string]any{
				"inherit":                 "all",
				"ignore_default_excludes": false,
				"include_only":            codexShellEnvironmentVars(workflowData),
			},
		},
		Overrides:      make(map[string]any),
		DisablePlugins: workflowData == nil || len(workflowData.Plugins) == 0,
	}
	config.Defaults["features"] = map[string]any{"plugins": !config.DisablePlugins}
	if isFirewallEnabled(workflowData) {
		config.Defaults["model_provider"] = codexOpenAIProxyProviderID
		config.Defaults["model_providers"] = map[string]any{
			codexOpenAIProxyProviderID: map[string]any{
				"name":                 codexOpenAIProxyProviderName,
				"base_url":             e.getOpenAIProxyProviderBaseURL(workflowData),
				"env_key":              "CODEX_API_KEY",
				"wire_api":             "responses",
				"requires_openai_auth": false,
				"supports_websockets":  false,
			},
		}
	}
	config.Defaults["mcp_servers"] = codexNativeServerDefaults(workflowData, mcpTools)
	if workflowData != nil && workflowData.EngineConfig != nil {
		var err error
		config.Overrides, err = parseCodexConfig(workflowData.EngineConfig.Config)
		if err != nil {
			return nil, err
		}
		if workflowData.IsDetectionRun {
			// Detection inherits inference settings, not the agent's MCP tool configuration.
			if _, exists := config.Overrides["mcp_servers"]; exists {
				codexMCPLog.Print("Skipping inherited agent MCP settings for Codex detection")
				delete(config.Overrides, "mcp_servers")
			}
		}
		if err := validateCodexManagedConfig(config, isFirewallEnabled(workflowData)); err != nil {
			return nil, err
		}
	}
	return config, nil
}

func codexNativeServerDefaults(workflowData *WorkflowData, mcpTools []string) map[string]any {
	servers := make(map[string]any)
	for _, name := range mcpTools {
		switch name {
		case "safe-outputs":
			name = constants.SafeOutputsMCPServerID.String()
		case "agentic-workflows":
			name = constants.AgenticWorkflowsMCPServerID.String()
		case "mcp-scripts":
			name = constants.MCPScriptsMCPServerID.String()
		case "cache-memory":
			continue
		}
		server := map[string]any{
			"startup_timeout_sec": int(constants.DefaultMCPStartupTimeout / time.Second),
			"tool_timeout_sec":    int(constants.DefaultToolTimeout / time.Second),
		}
		if workflowData != nil {
			if timeout := templatableIntValue(&workflowData.ToolsStartupTimeout); timeout > 0 {
				server["startup_timeout_sec"] = timeout
			}
			if timeout := templatableIntValue(&workflowData.ToolsTimeout); timeout > 0 {
				server["tool_timeout_sec"] = timeout
			}
		}
		if name == "github" && workflowData != nil {
			userAgent := "github-agentic-workflow"
			if workflowData.Name != "" {
				userAgent = SanitizeArtifactIdentifier(workflowData.Name)
			}
			if workflowData.EngineConfig != nil && workflowData.EngineConfig.UserAgent != "" {
				userAgent = workflowData.EngineConfig.UserAgent
			}
			server["http_headers"] = map[string]any{"User-Agent": userAgent}
		}
		servers[name] = server
	}
	return servers
}

func validateCodexManagedConfig(config *codexNativeConfig, firewallEnabled bool) error {
	if rawServers, exists := config.Overrides["mcp_servers"]; exists {
		servers, ok := rawServers.(map[string]any)
		if !ok {
			return errors.New("engine.config: mcp_servers must be a TOML table")
		}
		declared, ok := config.Defaults["mcp_servers"].(map[string]any)
		if !ok {
			return errors.New("invalid compiler-generated Codex MCP defaults: expected a table")
		}
		for name, raw := range servers {
			if _, exists := declared[name]; !exists {
				return fmt.Errorf("engine.config: MCP server %q is not declared; configure servers through mcp-servers so the MCP gateway can enforce their policies", name)
			}
			options, ok := raw.(map[string]any)
			if !ok {
				return fmt.Errorf("engine.config: mcp_servers.%s must be a TOML table", name)
			}
			for _, key := range []string{"url", "command", "args", "env", "env_vars", "bearer_token_env_var"} {
				if _, exists := options[key]; exists {
					return fmt.Errorf("engine.config: mcp_servers.%s.%s is managed by the MCP gateway; configure connection settings through mcp-servers instead", name, key)
				}
				for _, key := range []string{"http_headers", "env_http_headers"} {
					if headers, ok := options[key].(map[string]any); ok {
						for name := range headers {
							if strings.EqualFold(name, "Authorization") {
								return errors.New("engine.config: MCP gateway authorization is managed; configure upstream credentials through mcp-servers instead")
							}
						}
					}
				}
			}
		}
	}
	if firewallEnabled {
		if provider, exists := config.Overrides["model_provider"]; exists && provider != codexOpenAIProxyProviderID {
			return errors.New("engine.config: model_provider is managed by AWF; configure inference with engine.model-provider or engine.env.OPENAI_BASE_URL instead")
		}
		if providers, ok := config.Overrides["model_providers"].(map[string]any); ok {
			if proxy, ok := providers[codexOpenAIProxyProviderID].(map[string]any); ok {
				for _, key := range []string{"base_url", "env_key", "wire_api", "requires_openai_auth", "supports_websockets"} {
					if _, exists := proxy[key]; exists {
						return fmt.Errorf("engine.config: model_providers.%s.%s is managed by AWF; configure the upstream endpoint through engine.env.OPENAI_BASE_URL instead", codexOpenAIProxyProviderID, key)
					}
				}
			}
		}
	}
	return nil
}

func codexHome(workflowData *WorkflowData) string {
	if home := getEngineEnvOverrides(workflowData)["CODEX_HOME"]; home != "" {
		return home
	}
	return constants.TmpMcpConfigDir
}

func codexConfigEnv(workflowData *WorkflowData) map[string]string {
	result := make(map[string]string)
	if workflowData == nil || workflowData.EngineConfig == nil {
		return result
	}
	for name, value := range getEngineEnvOverrides(workflowData) {
		if strings.Contains(workflowData.EngineConfig.Config, "${"+name+"}") {
			result[name] = value
		}
	}
	return result
}

func codexShellEnvironmentVars(workflowData *WorkflowData) []string {
	names := map[string]struct{}{}
	for _, name := range []string{
		"PATH", "HOME", "SHELL", "TMPDIR", "TMP", "TEMP", "LANG", "LC_ALL", "LC_CTYPE", "USER", "LOGNAME",
		"GITHUB_WORKSPACE", "RUNNER_TEMP", "RUNNER_TOOL_CACHE", "PLAYWRIGHT_BROWSERS_PATH",
		"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL",
		"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "all_proxy", "no_proxy",
		"SSL_CERT_FILE", "SSL_CERT_DIR", "CURL_CA_BUNDLE", "REQUESTS_CA_BUNDLE", "NODE_EXTRA_CA_CERTS",
	} {
		names[name] = struct{}{}
	}
	if workflowData != nil {
		env := getEngineEnvOverrides(workflowData)
		for name, value := range env {
			if !strings.Contains(value, "${{ secrets.") && !ContainsJobOutputExpr(value) {
				names[name] = struct{}{}
			} else {
				delete(names, name)
			}
		}
		for _, name := range workflowData.ExcludedEnv {
			delete(names, name)
		}
	}
	return sliceutil.SortedKeys(names)
}

func (e *CodexEngine) renderConfigurationStep(workflowData *WorkflowData, mcpTools []string, configOnly bool) (GitHubActionStep, error) {
	config, err := e.buildNativeConfig(workflowData, mcpTools)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize Codex configuration: %w", err)
	}
	env := codexConfigEnv(workflowData)
	env["CODEX_HOME"] = codexHome(workflowData)
	env["GH_AW_CODEX_CONFIG_JSON"] = string(payload)
	command := `"$(command -v node)" "${RUNNER_TEMP}/gh-aw/actions/convert_gateway_config_codex.cjs" --bootstrap`
	if configOnly {
		command += "\n" + `"$(command -v node)" "${RUNNER_TEMP}/gh-aw/actions/convert_gateway_config_codex.cjs" --config-only`
	}
	return FormatStepWithCommandAndEnv([]string{"      - name: Configure Codex"}, command, env), nil
}

func (c *Compiler) validateCodexCompatibility(workflowData *WorkflowData) error {
	if workflowData == nil || workflowData.EngineConfig == nil {
		return nil
	}
	config := workflowData.EngineConfig
	engineID := workflowData.AI
	if engineID == "" {
		engineID = config.ID
	}
	engine, err := c.getAgenticEngine(engineID)
	if err != nil {
		return err
	}
	if _, ok := engine.(*CodexEngine); !ok {
		return nil
	}
	if _, err := parseCodexConfig(config.Config); err != nil {
		return err
	}
	if config.Command != "" {
		return nil
	}
	provider := NewCodexEngine().ResolveLLMProvider(workflowData)
	if provider != LLMProviderOpenAI && provider != LLMProviderGitHub {
		return NewValidationError("engine.model-provider", string(provider),
			"Codex requires an OpenAI Responses-compatible provider",
			"Use openai or github, or configure a Responses-compatible bridge with engine.env.OPENAI_BASE_URL.\n\nExample:\nengine:\n  id: codex\n  model-provider: openai")
	}
	if provider == LLMProviderGitHub && !isFirewallEnabled(workflowData) {
		return NewValidationError("sandbox.agent", "false",
			"Codex with the GitHub provider requires the AWF sandbox",
			"Remove sandbox.agent: false to enable authenticated GitHub inference.\n\nExample:\nengine:\n  id: codex\n  model: copilot/gpt-5.3-codex")
	}
	if workflowData.Tools["web-fetch"] == false {
		if search, enabled := workflowData.Tools["web-search"]; enabled && search != false {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Codex web-search includes page fetching; tools.web-fetch: false cannot disable browsing independently. Use network.hosted-web to restrict hosted retrieval."))
			c.IncrementWarningCount()
		}
	}
	return nil
}

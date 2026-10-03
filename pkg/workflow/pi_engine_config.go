package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/github/gh-aw/pkg/constants"
)

type piSessionConfig struct {
	Enabled bool   `json:"enabled"`
	ID      string `json:"id"`
	Resume  string `json:"resume"`
	Fork    string `json:"fork"`
	Export  bool   `json:"export"`
}

func piSessionSettings(data *WorkflowData) piSessionConfig {
	var parsed struct {
		Session piSessionConfig `json:"session"`
	}
	if data != nil && data.EngineConfig != nil && data.EngineConfig.Config != "" {
		if err := json.Unmarshal([]byte(data.EngineConfig.Config), &parsed); err != nil {
			panic(fmt.Sprintf("BUG: invalid validated Pi configuration: %v", err))
		}
	}
	return parsed.Session
}

func piToolPolicyJSON(data *WorkflowData) string {
	policy := make(map[string]any)
	if data != nil {
		for _, name := range []string{"bash", "edit"} {
			if value, ok := data.Tools[name]; ok {
				policy[name] = value
			}
		}
	}
	encoded, err := json.Marshal(policy)
	if err != nil {
		panic(fmt.Sprintf("BUG: cannot encode Pi tool policy: %v", err))
	}
	return string(encoded)
}

func (c *Compiler) validatePiEngineConfig(data *WorkflowData) error {
	if data == nil || data.EngineConfig == nil || data.EngineConfig.ID != string(constants.PiEngine) {
		return nil
	}
	config := data.EngineConfig
	if config.Driver != "" && config.Driver != "pi_agent_core_driver.cjs" && config.Driver != "pi_rpc_driver.cjs" &&
		(HasBashExplicitRestriction(data.Tools) || data.Tools["edit"] == false) {
		return errors.New("engine 'pi' tool restrictions require the built-in CLI, SDK, or RPC driver; custom drivers must implement their own tool policy")
	}
	if config.Driver == "pi_agent_core_driver.cjs" && len(config.Args) > 0 {
		return errors.New("engine 'pi' SDK mode does not consume CLI arguments; use engine.config.settings or the CLI/RPC execution mode")
	}
	provider := piConfiguredProvider(data)
	if !slices.Contains([]string{"github-copilot", "anthropic", "openai", "google"}, provider) &&
		isFirewallEnabled(data) && config.LLMProvider == "" {
		return fmt.Errorf("engine 'pi' provider %q has no credential-isolated AWF route; configure engine.model-provider and a compatible custom API target, or explicitly disable the agent sandbox for native provider authentication", provider)
	}
	if config.Version != "" && config.Version != "latest" && !isExpression(config.Version) &&
		!versionAtLeast(config.Version, string(constants.DefaultPiVersion), "1.0.0") {
		return errors.New("engine 'pi' requires version 1.0.0 or newer for native MCP and session extensions; update engine.version or omit it")
	}
	return validatePiJSONConfig(config.Config)
}

func validatePiJSONConfig(raw string) error {
	if raw == "" {
		return nil
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return fmt.Errorf("engine.config for Pi must be a JSON object: %w", err)
	}
	if parsed == nil {
		return errors.New("engine.config for Pi must be a JSON object, not null")
	}
	for key, value := range parsed {
		if !slices.Contains([]string{"settings", "model", "mcp", "session"}, key) {
			return fmt.Errorf("engine.config for Pi contains unknown field %q; supported fields are settings, model, mcp, session", key)
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(value, &object); err != nil || object == nil {
			return fmt.Errorf("engine.config.%s for Pi must be a JSON object", key)
		}
	}
	if err := validatePiNestedConfig(raw); err != nil {
		return err
	}
	if session, ok := parsed["session"]; ok {
		return validatePiSessionConfig(session)
	}
	return nil
}

func validatePiSessionConfig(raw json.RawMessage) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return fmt.Errorf("engine.config.session for Pi is invalid: %w", err)
	}
	for key := range fields {
		if !slices.Contains([]string{"enabled", "id", "resume", "fork", "export"}, key) {
			return fmt.Errorf("engine.config.session for Pi contains unknown field %q", key)
		}
	}
	var session piSessionConfig
	if err := json.Unmarshal(raw, &session); err != nil {
		return fmt.Errorf("engine.config.session for Pi is invalid: %w", err)
	}
	if !session.Enabled && (session.ID != "" || session.Resume != "" || session.Fork != "" || session.Export) {
		return errors.New("engine.config.session for Pi requires enabled: true to resume, fork, name, or export sessions")
	}
	if session.Resume != "" && session.Fork != "" {
		return errors.New("engine.config.session for Pi cannot combine resume and fork")
	}
	if session.ID != "" && session.Resume != "" {
		return errors.New("engine.config.session for Pi cannot combine id and resume")
	}
	if slices.ContainsFunc([]string{session.ID, session.Resume, session.Fork}, containsExpression) {
		return errors.New("engine 'pi' session identifiers and paths must be literal values, not GitHub Actions expressions")
	}
	return nil
}

func (e *PiEngine) applyPiConfigEnv(env map[string]string, data *WorkflowData) {
	env["GH_AW_PI_CONFIG"] = "{}"
	if data.EngineConfig == nil {
		return
	}
	config := data.EngineConfig
	env["GH_AW_PI_BARE"] = strconv.FormatBool(config.Bare)
	if config.Command != "" {
		env["GH_AW_PI_COMMAND"] = config.Command
	}
	if config.Config != "" {
		env["GH_AW_PI_CONFIG"] = config.Config
	}
	if config.Driver == "pi_rpc_driver.cjs" {
		encoded, err := json.Marshal(e.buildPiArgs(data))
		if err != nil {
			panic(fmt.Sprintf("BUG: cannot encode Pi RPC arguments: %v", err))
		}
		env["GH_AW_PI_ARGS"] = string(encoded)
	}
}

func applyPiToolPolicyEnv(env map[string]string, data *WorkflowData) {
	env["GH_AW_PI_TOOL_POLICY"] = piToolPolicyJSON(data)
	if data.EngineConfig == nil {
		return
	}
	if data.EngineConfig.MaxToolCalls != "" {
		env["GH_AW_MAX_TOOL_CALLS"] = data.EngineConfig.MaxToolCalls
	}
	if data.EngineConfig.MaxToolDenials != "" {
		env["GH_AW_MAX_TOOL_DENIALS"] = data.EngineConfig.MaxToolDenials
	}
}

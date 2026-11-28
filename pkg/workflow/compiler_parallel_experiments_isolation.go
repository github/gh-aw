package workflow

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// This is deliberately an allowlist. New host-side capabilities must receive
// an isolation review before they can run in the secondary pass.
func (c *Compiler) validateParallelExperimentIsolation(data *WorkflowData) error { //nolint:largefunc // Keep the complete fail-closed capability boundary in one reviewable allowlist.
	allowed := []string{
		"name", "description", "on", "permissions", "network", "engine", "experiments",
		"safe-outputs", "timeout-minutes", "max-ai-credits",
		"max-daily-ai-credits", "strict", "tools", "if", "observability", "model-routing",
	}
	for field := range data.RawFrontmatter {
		if field == "imports" && len(data.ImportPaths) == 0 && len(data.ImportedFiles) == 0 {
			switch imports := data.RawFrontmatter[field].(type) {
			case []any:
				if len(imports) == 0 {
					continue
				}
			case []string:
				if len(imports) == 0 {
					continue
				}
			}
		}
		if !slices.Contains(allowed, field) {
			return fmt.Errorf("parallel-experiments cannot isolate %s; remove unsupported host capabilities from this workflow", field)
		}
	}
	check := *c
	check.dryRun = true
	if err := check.validateDryRunEffects(data); err != nil {
		return fmt.Errorf("parallel-experiments: %w", err)
	}
	if data.GHES || !isFirewallEnabled(data) || isArcDindTopology(data) {
		return errors.New("parallel-experiments requires the standard AWF sandbox and GitHub.com cache-mode: read support")
	}
	if data.AI != "copilot" || data.EngineConfig == nil {
		return errors.New("parallel-experiments requires the sandboxed copilot engine")
	}
	if data.NetworkPermissions == nil || len(data.NetworkPermissions.Allowed) != 0 || data.NetworkPermissions.AllowedInput {
		return errors.New("parallel-experiments requires network.allowed: [] to prevent external mutations; inference uses the sandbox API proxy")
	}
	if network, ok := data.RawFrontmatter["network"].(map[string]any); ok {
		for field := range network {
			if field != "allowed" && field != "blocked" {
				return fmt.Errorf("parallel-experiments cannot isolate network.%s; only allowed: [] and blocked are supported", field)
			}
		}
	}
	if len(data.SubAgents) != 0 || len(data.Skills) != 0 || len(data.Plugins) != 0 || len(data.ImportPaths) != 0 ||
		len(data.PromptImports) != 0 || len(data.IncludedFiles) != 0 || len(data.AmbientFolders) != 0 ||
		data.AgentFile != "" || len(data.ResolvedMCPServers) != 0 || data.MCPScripts != nil ||
		data.GeneratedPreSteps != "" || data.CustomSteps != "" || data.PostSteps != "" ||
		data.Container != "" || data.Services != "" || len(data.Connectors) != 0 ||
		data.CacheMemoryConfig != nil || data.RepoMemoryConfig != nil || data.DriveMemoryConfig != nil ||
		data.CommentMemoryConfig != nil || data.LedgerConfig != nil || data.Cache != "" ||
		data.TokenBrokerEnabled || data.TopLevelGitHubApp != nil || data.UseSamples {
		return errors.New("parallel-experiments cannot isolate custom host steps, credentials, imports, sub-agents, plugins, MCP servers, or persistent memory")
	}
	if rawEngine, ok := data.RawFrontmatter["engine"].(map[string]any); ok {
		for field := range rawEngine {
			if !slices.Contains([]string{"id", "model", "copilot-sdk", "model-routing"}, field) {
				return fmt.Errorf("parallel-experiments cannot isolate engine.%s; only id, model, copilot-sdk and model-routing are supported", field)
			}
		}
	}
	engine := data.EngineConfig
	if engine.Command != "" || engine.HarnessScript != "" || engine.Driver != "" || engine.InlineDriver != nil ||
		len(engine.Env) != 0 || engine.Auth != nil || engine.Config != "" || len(engine.Args) != 0 ||
		engine.Agent != "" || engine.APITarget != "" || engine.IsInlineDefinition || engine.Durable != nil ||
		len(engine.Extensions) != 0 || len(engine.FallbackModels) != 0 {
		return errors.New("parallel-experiments cannot isolate custom engine code, authentication, model routing or retry fallbacks")
	}
	if engine.ModelRouting != nil {
		return errors.New("parallel-experiments does not support model routing; set top-level model-routing: false")
	}
	if rawTools, ok := data.RawFrontmatter["tools"].(map[string]any); ok {
		for name, value := range rawTools {
			if name == "github" {
				_ = value // Experimental copies disable the built-in GitHub tool entirely.
				continue
			}
			if name != "bash" && name != "edit" {
				return fmt.Errorf("parallel-experiments cannot isolate tools.%s; supported tools are bash, edit and github: false", name)
			}
		}
	}
	if rawOutputs, ok := data.RawFrontmatter["safe-outputs"].(map[string]any); ok {
		for name := range rawOutputs {
			if !slices.Contains([]string{"create-issue", "add-comment", "update-issue", "noop", "staged", "threat-detection"}, name) {
				return fmt.Errorf("parallel-experiments cannot isolate safe-outputs.%s; only staged issue/comment proposals are supported", name)
			}
		}
	}
	if parallelContainsCredentials(data.RawFrontmatter) {
		return errors.New("parallel-experiments cannot isolate custom credentials; use only the built-in read-only job token")
	}
	return nil
}

func parallelContainsCredentials(value any) bool {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			if key == "observability" {
				continue // Existing host-only telemetry export is explicitly authorized.
			}
			if slices.Contains([]string{"github-token", "github-app", "token", "secrets", "env", "steps", "post-steps"}, key) ||
				parallelContainsCredentials(child) {
				return true
			}
		}
	case []any:
		return slices.ContainsFunc(value, parallelContainsCredentials)
	case string:
		return strings.Contains(value, "secrets.") || strings.Contains(value, "secrets[")
	}
	return false
}

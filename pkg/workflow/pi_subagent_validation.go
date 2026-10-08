package workflow

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/parser"
)

var piSubagentToolNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_*-]+$`)

func validatePiSubagents(data *WorkflowData) error {
	if data == nil || data.EngineConfig == nil || data.EngineConfig.ID != "pi" {
		return nil
	}
	provider := piConfiguredProvider(data)
	seen := make(map[string]struct{})
	for _, agent := range data.SubAgents {
		parsed, err := parser.ExtractFrontmatterFromContent(agent.Content)
		if err != nil {
			return fmt.Errorf("pi sub-agent %q has invalid frontmatter: %w", agent.Name, err)
		}
		name := agent.Name
		if value, ok := parsed.Frontmatter["name"]; ok {
			explicitName, valid := value.(string)
			if !valid || explicitName != name {
				return fmt.Errorf("pi sub-agent %q name must match its agent marker", name)
			}
		}
		if _, duplicate := seen[name]; duplicate {
			return fmt.Errorf("pi sub-agent %q is defined more than once across workflow imports", name)
		}
		seen[name] = struct{}{}
		description, ok := parsed.Frontmatter["description"].(string)
		if !ok || strings.TrimSpace(description) == "" {
			return fmt.Errorf("pi sub-agent %q requires a non-empty description in its frontmatter", name)
		}
		if value, ok := parsed.Frontmatter["model"]; ok {
			if err := validatePiSubagentModel(name, value, provider); err != nil {
				return err
			}
		}
		if err := validatePiSubagentTools(name, parsed.Frontmatter["tools"]); err != nil {
			return err
		}
	}
	if len(data.SubAgents) > 0 && data.EngineConfig.Driver != "" &&
		data.EngineConfig.Driver != "pi_agent_core_driver.cjs" && data.EngineConfig.Driver != "pi_rpc_driver.cjs" {
		return fmt.Errorf("pi inline sub-agents require the built-in CLI, SDK, or RPC driver; custom driver %q does not load managed delegation", data.EngineConfig.Driver)
	}
	return nil
}

func validatePiSubagentModel(name string, value any, provider string) error {
	model, valid := value.(string)
	if !valid || strings.TrimSpace(model) == "" {
		return fmt.Errorf("pi sub-agent %q model must be a non-empty string", name)
	}
	if containsExpression(model) || strings.ContainsAny(model, "*[]") {
		return fmt.Errorf("pi sub-agent %q model must be a literal model or alias, not a glob or expression", name)
	}
	identifier, err := ParseModelIdentifier(model)
	if err != nil {
		return fmt.Errorf("pi sub-agent %q has invalid model %q: %w", name, model, err)
	}
	if identifier.Provider != "" && piSubagentProvider(identifier.Provider) != piSubagentProvider(provider) {
		return fmt.Errorf("pi sub-agent %q model %q must use the parent's provider %q", name, model, provider)
	}
	for key, value := range identifier.Params {
		if key != "effort" {
			return fmt.Errorf("pi sub-agent %q model supports only the effort parameter", name)
		}
		switch value {
		case "none", "minimal", "low", "medium", "high", "xhigh":
		default:
			return fmt.Errorf("pi sub-agent %q has unsupported thinking effort %q", name, value)
		}
	}
	return nil
}

func validatePiSubagentTools(name string, value any) error {
	if value == nil {
		return nil
	}
	var tools []any
	switch typed := value.(type) {
	case string:
		for tool := range strings.SplitSeq(typed, ",") {
			tools = append(tools, strings.TrimSpace(tool))
		}
	case []any:
		tools = typed
	default:
		return fmt.Errorf("pi sub-agent %q tools must be a list of tool names", name)
	}
	for _, tool := range tools {
		if toolName, ok := tool.(string); !ok || !piSubagentToolNamePattern.MatchString(toolName) {
			return fmt.Errorf("pi sub-agent %q tools must be a list of tool names", name)
		}
	}
	return nil
}

func piSubagentProvider(provider string) string {
	switch provider {
	case "github", "github-copilot", "copilot":
		return "copilot"
	case "codex", "openai":
		return "openai"
	case "gemini", "google":
		return "google"
	default:
		return provider
	}
}

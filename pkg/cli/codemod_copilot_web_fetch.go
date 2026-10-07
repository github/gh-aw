package cli

import (
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var copilotWebFetchCodemodLog = logger.New("cli:codemod_copilot_web_fetch")

func getCopilotWebFetchRemovalCodemod() Codemod {
	return Codemod{
		ID:           "copilot-web-fetch-removal",
		Name:         "Remove unsupported Copilot web-fetch tool",
		Description:  "Removes tools.web-fetch when Copilot CLI runs in offline BYOK mode. Copilot SDK mode retains its proxy-aware custom fetch tool.",
		IntroducedIn: "1.0.0",
		Apply: func(content string, frontmatter map[string]any) (string, bool, error) {
			return applyCopilotWebFetchRemoval(content, frontmatter, "")
		},
		ApplyWithContext: func(content string, frontmatter map[string]any, filePath string) (string, bool, error) {
			return applyCopilotWebFetchRemoval(content, frontmatter, filePath)
		},
	}
}

func applyCopilotWebFetchRemoval(content string, frontmatter map[string]any, filePath string) (string, bool, error) {
	tools, ok := frontmatter["tools"].(map[string]any)
	if !ok {
		return content, false, nil
	}
	webFetch, exists := tools["web-fetch"]
	if !exists {
		return content, false, nil
	}
	if enabled, ok := webFetch.(bool); ok && !enabled {
		return content, false, nil
	}

	compiler := workflow.NewCompiler()
	var engineConfig *workflow.EngineConfig
	if filePath != "" {
		resolvedConfig, err := compiler.ResolveEffectiveEngineConfig(content, filePath)
		if err != nil {
			copilotWebFetchCodemodLog.Printf("Unable to resolve effective engine; preserving tools.web-fetch: %v", err)
			return content, false, nil
		}
		engineConfig = resolvedConfig
	} else {
		if _, hasEngine := frontmatter["engine"]; !hasEngine {
			return content, false, nil
		}
		_, engineConfig, _ = compiler.ExtractEngineConfig(frontmatter)
	}
	if engineConfig == nil || engineConfig.ID != "copilot" || engineConfig.CopilotSDK {
		return content, false, nil
	}

	updated, applied, err := removeYAMLMappingPath(content, []string{"tools", "web-fetch"}, false)
	if applied {
		copilotWebFetchCodemodLog.Print("Removed unsupported tools.web-fetch for Copilot CLI")
	}
	return updated, applied, err
}

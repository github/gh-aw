package cli

import (
	"path/filepath"
	"strings"

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

func getCopilotWebSearchRemovalCodemod() Codemod {
	return Codemod{
		ID:           "copilot-web-search-removal",
		Name:         "Remove unsupported Copilot web-search tool",
		Description:  "Removes tools.web-search when Copilot runs in offline BYOK mode, including SDK mode.",
		IntroducedIn: "1.0.0",
		Apply: func(content string, frontmatter map[string]any) (string, bool, error) {
			return applyCopilotWebToolRemoval(content, frontmatter, "", "web-search")
		},
		ApplyWithContext: func(content string, frontmatter map[string]any, filePath string) (string, bool, error) {
			return applyCopilotWebToolRemoval(content, frontmatter, filePath, "web-search")
		},
	}
}

func applyCopilotWebFetchRemoval(content string, frontmatter map[string]any, filePath string) (string, bool, error) {
	return applyCopilotWebToolRemoval(content, frontmatter, filePath, "web-fetch")
}

func applyCopilotWebToolRemoval(content string, frontmatter map[string]any, filePath, tool string) (string, bool, error) {
	tools, ok := frontmatter["tools"].(map[string]any)
	if !ok {
		return content, false, nil
	}
	value, exists := tools[tool]
	if !exists {
		return content, false, nil
	}
	if enabled, ok := value.(bool); ok && !enabled {
		return content, false, nil
	}
	if _, hasEngine := frontmatter["engine"]; !hasEngine && isNestedWorkflowFragment(filePath) {
		copilotWebFetchCodemodLog.Printf("Unable to determine effective engine for nested fragment; preserving tools.%s", tool)
		return content, false, nil
	}

	compiler := workflow.NewCompiler()
	var engineConfig *workflow.EngineConfig
	if filePath != "" {
		resolvedConfig, err := compiler.ResolveEffectiveEngineConfig(content, filePath)
		if err != nil {
			copilotWebFetchCodemodLog.Printf("Unable to resolve effective engine; preserving tools.%s: %v", tool, err)
			return content, false, nil
		}
		engineConfig = resolvedConfig
	} else {
		if _, hasEngine := frontmatter["engine"]; !hasEngine {
			return content, false, nil
		}
		_, engineConfig, _ = compiler.ExtractEngineConfig(frontmatter)
	}
	if engineConfig == nil || engineConfig.ID != "copilot" || (tool == "web-fetch" && engineConfig.CopilotSDK) {
		return content, false, nil
	}

	updated, applied, err := removeYAMLMappingPath(content, []string{"tools", tool}, false)
	if applied {
		copilotWebFetchCodemodLog.Printf("Removed unsupported tools.%s for Copilot", tool)
	}
	return updated, applied, err
}

func isNestedWorkflowFragment(filePath string) bool {
	if filePath == "" {
		return false
	}
	parts := strings.Split(filepath.ToSlash(filepath.Clean(filePath)), "/")
	for i, part := range parts {
		if part == "workflows" {
			return i < len(parts)-2
		}
	}
	return false
}

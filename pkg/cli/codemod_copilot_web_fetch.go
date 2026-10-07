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
			if extractEngineIDFromFrontmatter(frontmatter) != "copilot" {
				return content, false, nil
			}

			_, engineConfig, _ := (&workflow.Compiler{}).ExtractEngineConfig(frontmatter)
			if engineConfig != nil && engineConfig.CopilotSDK {
				return content, false, nil
			}

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

			newContent, applied, err := applyFrontmatterLineTransform(content, func(lines []string) ([]string, bool) {
				result, modified := removeFieldFromBlock(lines, "web-fetch", "tools")
				return result, modified
			})
			if applied {
				copilotWebFetchCodemodLog.Print("Removed unsupported tools.web-fetch for Copilot CLI")
			}
			return newContent, applied, err
		},
	}
}

package cli

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var playwrightCLIModeCodemodLog = logger.New("cli:codemod_playwright_cli_mode")

// getPlaywrightCLIModeRemovalCodemod creates a codemod that removes the now-redundant
// 'tools.playwright.mode: cli' field. CLI is the only supported built-in Playwright
// integration and is the default when mode is omitted, so an explicit 'mode: cli' no
// longer changes behavior.
func getPlaywrightCLIModeRemovalCodemod() Codemod {
	return Codemod{
		ID:           "playwright-cli-mode-removal",
		Name:         "Remove redundant 'tools.playwright.mode: cli'",
		Description:  "Removes 'tools.playwright.mode: cli' because CLI is now the only built-in Playwright integration and the default when mode is omitted.",
		IntroducedIn: "1.5.0",
		Apply: func(content string, frontmatter map[string]any) (string, bool, error) {
			toolsValue, hasTools := frontmatter["tools"]
			if !hasTools {
				return content, false, nil
			}

			toolsMap, ok := toolsValue.(map[string]any)
			if !ok {
				return content, false, nil
			}

			playwrightValue, hasPlaywright := toolsMap["playwright"]
			if !hasPlaywright {
				return content, false, nil
			}

			playwrightMap, ok := playwrightValue.(map[string]any)
			if !ok {
				return content, false, nil
			}

			modeValue, hasMode := playwrightMap["mode"]
			if !hasMode {
				return content, false, nil
			}

			modeStr, ok := modeValue.(string)
			if !ok || !strings.EqualFold(modeStr, "cli") {
				// Leave 'mode: mcp' (or expression-valued mode) untouched; only the
				// removed-then-restored-as-default 'cli' value is safe to drop here.
				return content, false, nil
			}

			newContent, applied, err := applyFrontmatterLineTransform(content, func(lines []string) ([]string, bool) {
				result, modified := removeFieldFromPlaywright(lines, "mode")
				if !modified {
					return lines, false
				}
				return result, true
			})
			if applied {
				playwrightCLIModeCodemodLog.Print("Removed redundant tools.playwright.mode: cli")
			}
			return newContent, applied, err
		},
	}
}

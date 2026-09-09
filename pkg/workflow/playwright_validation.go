// This file provides playwright tool validation for agentic workflows.
//
// # Playwright Mode Validation
//
// validatePlaywrightMode rejects the removed MCP mode. Playwright uses CLI mode
// when mode is omitted or set to cli.
//
// # Migration
//
// To migrate from MCP mode to the built-in CLI integration:
//
//  1. Remove `mode: mcp` from your playwright tool configuration:
//
//     tools:
//       playwright:
//
//  2. Update prompts to use `playwright-cli <command>` via bash instead of
//     MCP browser tool calls. For example:
//     - Old: use browser_navigate MCP tool
//     - New: run `playwright-cli goto <url>` in bash
//
//  3. Use `localhost` directly when accessing local servers — playwright-cli
//     runs on the runner host, not in a separate Docker container.

package workflow

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml"
)

var playwrightBrowserInstallPattern = regexp.MustCompile(`(?im)(?:^|&&|\|\||;)[ \t]*(?:(?:npx|npm[ \t]+(?:exec|x)|pnpm[ \t]+(?:exec|dlx)|yarn(?:[ \t]+(?:exec|dlx))?|bunx)[ \t]+(?:(?:--yes|--no-install|--)[ \t]+)*)?playwright(?:@[^\s;&|]+)?[ \t]+install(?:[ \t]|$)`)

func normalizePlaywrightBrowser(browser string) string {
	switch strings.ToLower(strings.TrimSpace(browser)) {
	case "chrome", "chrome-for-testing", "chromium":
		return "chromium"
	case "firefox":
		return "firefox"
	case "webkit":
		return "webkit"
	default:
		return ""
	}
}

// validatePlaywrightMode validates that Playwright mode is static and rejects
// the removed built-in MCP integration.
func (c *Compiler) validatePlaywrightMode(workflowData *WorkflowData) error {
	if workflowData == nil || workflowData.Tools == nil {
		return nil
	}

	playwrightTool, ok := workflowData.Tools["playwright"]
	if !ok || playwrightTool == false {
		return nil
	}
	if config, ok := playwrightTool.(map[string]any); ok {
		if mode, ok := config["mode"].(string); ok && hasExpressionMarker(mode) {
			return NewValidationError(
				"tools.playwright.mode",
				mode,
				"mode must be a literal value; expressions are not allowed",
				"Remove mode because the built-in Playwright integration is CLI-only",
			)
		}
		if mode, ok := config["mode"].(string); ok && strings.EqualFold(mode, "mcp") {
			return NewValidationError(
				"tools.playwright.mode",
				mode,
				"built-in Playwright MCP support has been removed",
				"Remove `mode: mcp`, then update prompts to run `playwright-cli <command>` from bash. If MCP is still required, configure Playwright explicitly under `mcp-servers`. See https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/playwright.md",
			)
		}
		if browsers, ok := config["browsers"].([]any); ok {
			if len(browsers) == 0 {
				return NewValidationError(
					"tools.playwright.browsers",
					"[]",
					"at least one browser is required",
					"Omit browsers to use the default Chromium browser",
				)
			}
			for _, browser := range browsers {
				name, ok := browser.(string)
				if !ok || normalizePlaywrightBrowser(name) == "" {
					return NewValidationError(
						"tools.playwright.browsers",
						fmt.Sprint(browser),
						"unsupported browser; choose chrome, chrome-for-testing, chromium, firefox, or webkit",
						"Set browsers to a list containing supported Playwright browser names",
					)
				}
			}
		}
	}
	return nil
}

func (c *Compiler) emitPlaywrightBrowserInstallWarning(workflowData *WorkflowData, markdownPath string) {
	if workflowData == nil || !isPlaywrightCLIMode(workflowData.Tools) || !hasPlaywrightBrowserInstallStep(workflowData) {
		return
	}

	fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "warning",
		"custom steps install Playwright browser engines. Remove those installation commands and use `tools.playwright.browsers` instead; the compiler provisions the selected browsers before the agent starts."))
	c.IncrementWarningCount()
}

func hasPlaywrightBrowserInstallStep(workflowData *WorkflowData) bool {
	sections := []string{
		workflowData.PreSteps,
		workflowData.CustomSteps,
		workflowData.PreAgentSteps,
		workflowData.PostSteps,
	}

	for _, section := range sections {
		if section == "" {
			continue
		}
		var wrapper map[string][]WorkflowStep
		if err := yaml.Unmarshal([]byte(section), &wrapper); err != nil {
			continue
		}
		for _, steps := range wrapper {
			for _, step := range steps {
				if playwrightBrowserInstallPattern.MatchString(step.Run) {
					return true
				}
			}
		}
	}
	return false
}

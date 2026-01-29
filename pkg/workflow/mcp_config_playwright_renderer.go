package workflow

import (
	"strings"

	"github.com/githubnext/gh-aw/pkg/logger"
)

var mcpPlaywrightLog = logger.New("workflow:mcp_config_playwright_renderer")

// renderPlaywrightMCPConfig generates the Playwright MCP server configuration
// Uses Docker container to launch Playwright MCP for consistent browser environment
// This is a shared function used by both Claude and Custom engines
func renderPlaywrightMCPConfig(yaml *strings.Builder, playwrightConfig *PlaywrightToolConfig, isLast bool) {
	mcpPlaywrightLog.Print("Rendering Playwright MCP configuration")
	renderPlaywrightMCPConfigWithOptions(yaml, playwrightConfig, isLast, false, false)
}

// renderPlaywrightMCPConfigWithOptions generates the Playwright MCP server configuration with engine-specific options
// Per MCP Gateway Specification v1.0.0 section 3.2.1, stdio-based MCP servers MUST be containerized.
// Uses MCP Gateway spec format: container, entrypointArgs, mounts, and args fields.
func renderPlaywrightMCPConfigWithOptions(yaml *strings.Builder, playwrightConfig *PlaywrightToolConfig, isLast bool, includeCopilotFields bool, inlineArgs bool) {
	args := generatePlaywrightDockerArgs(playwrightConfig)
	customArgs := getPlaywrightCustomArgs(playwrightConfig)

	// Extract all expressions from playwright arguments and replace them with env var references
	expressions := extractExpressionsFromPlaywrightArgs(args.AllowedDomains, customArgs)
	allowedDomains := replaceExpressionsInPlaywrightArgs(args.AllowedDomains, expressions)

	// Also replace expressions in custom args
	if len(customArgs) > 0 {
		customArgs = replaceExpressionsInPlaywrightArgs(customArgs, expressions)
	}

	// Use official Playwright MCP Docker image (no version tag - only one image)
	playwrightImage := "mcr.microsoft.com/playwright/mcp"

	yaml.WriteString("              \"playwright\": {\n")

	// Add type field for Copilot (per MCP Gateway Specification v1.0.0, use "stdio" for containerized servers)
	if includeCopilotFields {
		yaml.WriteString("                \"type\": \"stdio\",\n")
	}

	// MCP Gateway spec fields for containerized stdio servers
	yaml.WriteString("                \"container\": \"" + playwrightImage + "\",\n")

	// Docker runtime args (goes before container image in docker run command)
	// These are additional flags for docker run like --init and --network
	dockerArgs := []string{"--init", "--network", "host"}
	if inlineArgs {
		yaml.WriteString("                \"args\": [")
		for i, arg := range dockerArgs {
			if i > 0 {
				yaml.WriteString(", ")
			}
			yaml.WriteString("\"" + arg + "\"")
		}
		yaml.WriteString("],\n")
	} else {
		yaml.WriteString("                \"args\": [\n")
		for i, arg := range dockerArgs {
			yaml.WriteString("                  \"" + arg + "\"")
			if i < len(dockerArgs)-1 {
				yaml.WriteString(",")
			}
			yaml.WriteString("\n")
		}
		yaml.WriteString("                ],\n")
	}

	// Build entrypoint args for Playwright MCP server (goes after container image)
	entrypointArgs := []string{"--output-dir", "/tmp/gh-aw/mcp-logs/playwright"}
	if len(allowedDomains) > 0 {
		domainsStr := strings.Join(allowedDomains, ";")
		entrypointArgs = append(entrypointArgs, "--allowed-hosts", domainsStr)
		entrypointArgs = append(entrypointArgs, "--allowed-origins", domainsStr)
	}
	// Append custom args if present
	if len(customArgs) > 0 {
		entrypointArgs = append(entrypointArgs, customArgs...)
	}

	// Render entrypointArgs
	if inlineArgs {
		yaml.WriteString("                \"entrypointArgs\": [")
		for i, arg := range entrypointArgs {
			if i > 0 {
				yaml.WriteString(", ")
			}
			yaml.WriteString("\"" + arg + "\"")
		}
		yaml.WriteString("],\n")
	} else {
		yaml.WriteString("                \"entrypointArgs\": [\n")
		for i, arg := range entrypointArgs {
			yaml.WriteString("                  \"" + arg + "\"")
			if i < len(entrypointArgs)-1 {
				yaml.WriteString(",")
			}
			yaml.WriteString("\n")
		}
		yaml.WriteString("                ],\n")
	}

	// Add volume mounts
	yaml.WriteString("                \"mounts\": [\"/tmp/gh-aw/mcp-logs:/tmp/gh-aw/mcp-logs:rw\"]\n")

	// Note: tools field is NOT included here - the converter script adds it back
	// for Copilot. This keeps the gateway config compatible with the schema.

	if isLast {
		yaml.WriteString("              }\n")
	} else {
		yaml.WriteString("              },\n")
	}
}

// Package workflow provides Playwright MCP server configuration and Docker setup.
//
// # Playwright MCP Server
//
// This file handles the configuration and rendering of the Playwright MCP server,
// which provides AI agents with browser automation capabilities through the
// Model Context Protocol (MCP). Playwright enables agents to interact with
// web pages, take screenshots, extract content, and perform accessibility testing.
//
// Key responsibilities:
//   - Generating Playwright MCP server configuration
//   - Managing Docker container setup for Playwright
//   - Handling allowed domains for browser navigation
//   - Processing custom Playwright arguments
//   - Extracting and managing domain secrets from expressions
//   - Rendering configuration for different AI engines
//
// Container configuration:
// Playwright runs in a Docker container using the official Microsoft Playwright
// MCP image (mcr.microsoft.com/playwright/mcp). The container is configured with:
//   - --init flag for proper signal handling
//   - --network host for network access
//   - Volume mounts for log storage
//   - Output directory for screenshots and artifacts
//
// Domain restrictions:
// For security, Playwright is restricted to specific allowed domains configured
// in the workflow frontmatter. These domains are passed via:
//   - --allowed-hosts: Domains the browser can navigate to
//   - --allowed-origins: Domains that can be used as origins
//
// Expression handling:
// When allowed_domains contains GitHub Actions expressions like ${{ secrets.DOMAIN }},
// these are extracted and made available as environment variables. The actual
// secret values are resolved at runtime and passed to the Playwright container.
//
// Engine compatibility:
// The renderer supports multiple AI engines with engine-specific formatting:
//   - Copilot: Includes "type" field, inline args
//   - Claude/Custom: Multi-line args, simplified format
//   - All engines: Same core configuration structure
//
// Related files:
//   - mcp_playwright_config.go: Playwright configuration types and parsing
//   - mcp_renderer.go: Main MCP renderer that calls this function
//   - mcp_setup_generator.go: Includes Playwright in setup sequence
//
// Example configuration:
//
//	tools:
//	  playwright:
//	    version: v1.41.0
//	    allowed_domains:
//	      - github.com
//	      - api.github.com
//	      - ${{ secrets.CUSTOM_DOMAIN }}
//	    custom_args:
//	      - --debug
//	      - --timeout=30000
package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
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
		// Per Playwright MCP documentation:
		// --allowed-hosts expects comma-separated list
		// --allowed-origins expects semicolon-separated list
		allowedHostsStr := strings.Join(allowedDomains, ",")
		allowedOriginsStr := strings.Join(allowedDomains, ";")
		entrypointArgs = append(entrypointArgs, "--allowed-hosts", allowedHostsStr)
		entrypointArgs = append(entrypointArgs, "--allowed-origins", allowedOriginsStr)
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

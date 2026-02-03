package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var mcpSerenaLog = logger.New("workflow:mcp_config_serena_renderer")

// selectSerenaContainer determines which Serena container image to use based on requested languages
// Returns the container image path that supports all requested languages
func selectSerenaContainer(serenaTool any) string {
	// Extract languages from the serena tool configuration
	var requestedLanguages []string

	if toolMap, ok := serenaTool.(map[string]any); ok {
		// Check for short syntax (array of language names)
		if langs, ok := toolMap["langs"].([]any); ok {
			for _, lang := range langs {
				if langStr, ok := lang.(string); ok {
					requestedLanguages = append(requestedLanguages, langStr)
				}
			}
		}

		// Check for detailed language configuration
		if langs, ok := toolMap["languages"].(map[string]any); ok {
			for langName := range langs {
				requestedLanguages = append(requestedLanguages, langName)
			}
		}
	}

	// If we parsed serena from SerenaToolConfig
	if serenaConfig, ok := serenaTool.(*SerenaToolConfig); ok {
		requestedLanguages = append(requestedLanguages, serenaConfig.ShortSyntax...)
		if serenaConfig.Languages != nil {
			for langName := range serenaConfig.Languages {
				requestedLanguages = append(requestedLanguages, langName)
			}
		}
	}

	// If no languages specified, use default container
	if len(requestedLanguages) == 0 {
		return constants.DefaultSerenaMCPServerContainer
	}

	// Check if all requested languages are supported by the default container
	defaultSupported := true
	for _, lang := range requestedLanguages {
		supported := false
		for _, supportedLang := range constants.SerenaLanguageSupport[constants.DefaultSerenaMCPServerContainer] {
			if lang == supportedLang {
				supported = true
				break
			}
		}
		if !supported {
			defaultSupported = false
			mcpSerenaLog.Printf("Language '%s' not found in default container support list", lang)
			break
		}
	}

	if defaultSupported {
		return constants.DefaultSerenaMCPServerContainer
	}

	// Check if Oraios container supports the languages
	oraiosSupported := true
	for _, lang := range requestedLanguages {
		supported := false
		for _, supportedLang := range constants.SerenaLanguageSupport[constants.OraiosSerenaContainer] {
			if lang == supportedLang {
				supported = true
				break
			}
		}
		if !supported {
			oraiosSupported = false
			break
		}
	}

	if oraiosSupported {
		mcpSerenaLog.Printf("Using Oraios Serena container as fallback for languages: %v", requestedLanguages)
		return constants.OraiosSerenaContainer
	}

	// Default to the new GitHub container if neither supports all languages
	mcpSerenaLog.Printf("Using default Serena container (some languages may not be supported): %v", requestedLanguages)
	return constants.DefaultSerenaMCPServerContainer
}

// renderSerenaMCPConfigWithOptions generates the Serena MCP server configuration with engine-specific options
// Supports two modes:
// - "docker" (default): Uses Docker container with stdio transport (ghcr.io/github/serena-mcp-server:latest)
// - "local": Uses local uvx with HTTP transport on fixed port
func renderSerenaMCPConfigWithOptions(yaml *strings.Builder, serenaTool any, isLast bool, includeCopilotFields bool, inlineArgs bool) {
	customArgs := getSerenaCustomArgs(serenaTool)

	// Determine the mode - check if serenaTool is a map with mode field
	mode := "docker" // default
	if toolMap, ok := serenaTool.(map[string]any); ok {
		if modeStr, ok := toolMap["mode"].(string); ok {
			mode = modeStr
		}
	}

	yaml.WriteString("              \"serena\": {\n")

	if mode == "local" {
		// Local mode: use HTTP transport
		// The MCP server is started in a separate step using uvx
		if includeCopilotFields {
			yaml.WriteString("                \"type\": \"http\",\n")
		}

		yaml.WriteString("                \"url\": \"http://localhost:$GH_AW_SERENA_PORT\"\n")
	} else {
		// Docker mode: use stdio transport (default behavior)
		// Add type field for Copilot (per MCP Gateway Specification v1.0.0, use "stdio" for containerized servers)
		if includeCopilotFields {
			yaml.WriteString("                \"type\": \"stdio\",\n")
		}

		// Select the appropriate Serena container based on requested languages
		containerImage := selectSerenaContainer(serenaTool)
		yaml.WriteString("                \"container\": \"" + containerImage + ":latest\",\n")

		// Docker runtime args (--network host for network access)
		if inlineArgs {
			yaml.WriteString("                \"args\": [\"--network\", \"host\"],\n")
		} else {
			yaml.WriteString("                \"args\": [\n")
			yaml.WriteString("                  \"--network\",\n")
			yaml.WriteString("                  \"host\"\n")
			yaml.WriteString("                ],\n")
		}

		// Serena entrypoint
		yaml.WriteString("                \"entrypoint\": \"serena\",\n")

		// Entrypoint args for Serena MCP server
		if inlineArgs {
			yaml.WriteString("                \"entrypointArgs\": [\"start-mcp-server\", \"--context\", \"codex\", \"--project\", \"${{ github.workspace }}\"")
			// Append custom args if present
			writeArgsToYAMLInline(yaml, customArgs)
			yaml.WriteString("],\n")
		} else {
			yaml.WriteString("                \"entrypointArgs\": [\n")
			yaml.WriteString("                  \"start-mcp-server\",\n")
			yaml.WriteString("                  \"--context\",\n")
			yaml.WriteString("                  \"codex\",\n")
			yaml.WriteString("                  \"--project\",\n")
			yaml.WriteString("                  \"${{ github.workspace }}\"")
			// Append custom args if present
			writeArgsToYAML(yaml, customArgs, "                  ")
			yaml.WriteString("\n")
			yaml.WriteString("                ],\n")
		}

		// Add volume mount for workspace access
		yaml.WriteString("                \"mounts\": [\"${{ github.workspace }}:${{ github.workspace }}:rw\"]\n")

		// Note: tools field is NOT included here - the converter script adds it back
		// for Copilot. This keeps the gateway config compatible with the schema.
	}

	if isLast {
		yaml.WriteString("              }\n")
	} else {
		yaml.WriteString("              },\n")
	}
}

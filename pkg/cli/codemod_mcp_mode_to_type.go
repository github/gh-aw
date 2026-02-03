package cli

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var mcpModeToTypeCodemodLog = logger.New("cli:codemod_mcp_mode_to_type")

// getMCPModeToTypeCodemod creates a codemod for migrating 'mode' to 'type' in custom MCP server configurations
func getMCPModeToTypeCodemod() Codemod {
	return Codemod{
		ID:           "mcp-mode-to-type-migration",
		Name:         "Migrate MCP 'mode' to 'type'",
		Description:  "Renames 'mode' field to 'type' in custom MCP server configurations (mcp-servers section). Does not affect GitHub or Serena tool configurations.",
		IntroducedIn: "0.7.0",
		Apply: func(content string, frontmatter map[string]any) (string, bool, error) {
			// Check if mcp-servers section exists
			mcpServersValue, hasMCPServers := frontmatter["mcp-servers"]
			if !hasMCPServers {
				return content, false, nil
			}

			mcpServersMap, ok := mcpServersValue.(map[string]any)
			if !ok {
				return content, false, nil
			}

			// Check if any MCP server has a 'mode' field
			hasMode := false
			for _, serverValue := range mcpServersMap {
				serverConfig, ok := serverValue.(map[string]any)
				if !ok {
					continue
				}
				if _, hasModeField := serverConfig["mode"]; hasModeField {
					hasMode = true
					break
				}
			}

			if !hasMode {
				return content, false, nil
			}

			// Parse frontmatter to get raw lines
			frontmatterLines, markdown, err := parseFrontmatterLines(content)
			if err != nil {
				return content, false, err
			}

			// Rename 'mode' to 'type' in all MCP servers
			result, modified := renameModeToTypeInMCPServers(frontmatterLines)
			if !modified {
				return content, false, nil
			}

			// Reconstruct the content
			newContent := reconstructContent(result, markdown)
			mcpModeToTypeCodemodLog.Print("Applied MCP mode-to-type migration")
			return newContent, true, nil
		},
	}
}

// renameModeToTypeInMCPServers renames 'mode:' to 'type:' within mcp-servers blocks
func renameModeToTypeInMCPServers(lines []string) ([]string, bool) {
	var result []string
	var modified bool
	var inMCPServers bool
	var mcpServersIndent string

	for i, line := range lines {
		trimmedLine := strings.TrimSpace(line)

		// Track if we're in mcp-servers block
		if strings.HasPrefix(trimmedLine, "mcp-servers:") {
			inMCPServers = true
			mcpServersIndent = getIndentation(line)
			result = append(result, line)
			continue
		}

		// Check if we've left mcp-servers block
		if inMCPServers && len(trimmedLine) > 0 && !strings.HasPrefix(trimmedLine, "#") {
			if hasExitedBlock(line, mcpServersIndent) {
				inMCPServers = false
			}
		}

		// Rename 'mode:' to 'type:' if we're in mcp-servers block
		if inMCPServers && strings.HasPrefix(trimmedLine, "mode:") {
			newLine, replaced := findAndReplaceInLine(line, "mode", "type")
			if replaced {
				result = append(result, newLine)
				modified = true
				mcpModeToTypeCodemodLog.Printf("Renamed 'mode' to 'type' on line %d", i+1)
				continue
			}
		}

		result = append(result, line)
	}

	return result, modified
}

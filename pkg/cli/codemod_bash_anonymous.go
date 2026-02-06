package cli

import "github.com/github/gh-aw/pkg/logger"

var bashAnonymousCodemodLog = logger.New("cli:codemod_bash_anonymous")

// getBashAnonymousRemovalCodemod creates a codemod for removing anonymous bash tool syntax
func getBashAnonymousRemovalCodemod() Codemod {
	return Codemod{
		ID:           "bash-anonymous-removal",
		Name:         "Replace anonymous bash tool syntax with explicit true",
		Description:  "Replaces 'bash:' (anonymous/nil syntax) with 'bash: true' to make configuration explicit",
		IntroducedIn: "0.9.0",
		Apply: func(content string, frontmatter map[string]any) (string, bool, error) {
			// Check if tools.bash exists
			toolsValue, hasTools := frontmatter["tools"]
			if !hasTools {
				return content, false, nil
			}

			toolsMap, ok := toolsValue.(map[string]any)
			if !ok {
				return content, false, nil
			}

			// Check if bash field exists and is nil
			bashValue, hasBash := toolsMap["bash"]
			if !hasBash {
				return content, false, nil
			}

			// Only modify if bash is nil (anonymous syntax)
			if bashValue != nil {
				return content, false, nil
			}

			// Parse frontmatter to get raw lines
			frontmatterLines, markdown, err := parseFrontmatterLines(content)
			if err != nil {
				return content, false, err
			}

			// Replace the bash field from anonymous to explicit true
			modifiedLines, modified := replaceBashAnonymousWithTrue(frontmatterLines)
			if !modified {
				return content, false, nil
			}

			// Reconstruct the content
			newContent := reconstructContent(modifiedLines, markdown)
			bashAnonymousCodemodLog.Print("Applied bash anonymous removal, replaced with 'bash: true'")
			return newContent, true, nil
		},
	}
}

// replaceBashAnonymousWithTrue replaces 'bash:' with 'bash: true' in the tools block
func replaceBashAnonymousWithTrue(lines []string) ([]string, bool) {
	var result []string
	var modified bool
	var inToolsBlock bool
	var toolsIndent string

	for _, line := range lines {
		trimmedLine := line

		// Trim to check content but preserve original spacing
		trimmed := trimLine(trimmedLine)

		// Track if we're in the tools block
		if trimmed == "tools:" {
			inToolsBlock = true
			toolsIndent = getIndentation(line)
			result = append(result, line)
			continue
		}

		// Check if we've left the tools block
		if inToolsBlock && len(trimmed) > 0 && !startsWith(trimmed, "#") {
			if hasExitedBlock(line, toolsIndent) {
				inToolsBlock = false
			}
		}

		// Replace bash: with bash: true if in tools block
		if inToolsBlock && (trimmed == "bash:" || startsWith(trimmed, "bash: ")) {
			// Check if it's just 'bash:' with nothing after the colon
			if trimmed == "bash:" {
				indent := getIndentation(line)
				result = append(result, indent+"bash: true")
				modified = true
				bashAnonymousCodemodLog.Printf("Replaced 'bash:' with 'bash: true'")
				continue
			}
		}

		result = append(result, line)
	}

	return result, modified
}

// Helper function to trim whitespace
func trimLine(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

// Helper function to check if string starts with prefix
func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

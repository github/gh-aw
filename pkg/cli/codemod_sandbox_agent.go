package cli

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var sandboxAgentCodemodLog = logger.New("cli:codemod_sandbox_agent")

// getSandboxAgentFalseRemovalCodemod creates a codemod for removing sandbox.agent: false
func getSandboxAgentFalseRemovalCodemod() Codemod {
	return Codemod{
		ID:           "sandbox-agent-false-removal",
		Name:         "Remove deprecated sandbox.agent: false",
		Description:  "Removes 'sandbox.agent: false' as the agent sandbox is now mandatory and defaults to 'awf'",
		IntroducedIn: "0.5.0",
		Apply: func(content string, frontmatter map[string]any) (string, bool, error) {
			// Check if sandbox.agent: false exists
			sandboxValue, hasSandbox := frontmatter["sandbox"]
			if !hasSandbox {
				return content, false, nil
			}

			sandboxMap, ok := sandboxValue.(map[string]any)
			if !ok {
				return content, false, nil
			}

			// Check if agent field exists in sandbox and is set to false
			agentValue, hasAgent := sandboxMap["agent"]
			if !hasAgent {
				return content, false, nil
			}

			agentBool, isBool := agentValue.(bool)
			if !isBool || agentBool {
				// Not a boolean false, skip
				return content, false, nil
			}

			// Parse frontmatter to get raw lines
			frontmatterLines, markdown, err := parseFrontmatterLines(content)
			if err != nil {
				return content, false, err
			}

			// Find and remove the agent: false line within the sandbox block
			var modified bool
			var inSandboxBlock bool
			var sandboxIndent string

			result := make([]string, 0, len(frontmatterLines))

			for i, line := range frontmatterLines {
				trimmedLine := strings.TrimSpace(line)

				// Track if we're in the sandbox block
				if strings.HasPrefix(trimmedLine, "sandbox:") {
					inSandboxBlock = true
					sandboxIndent = getIndentation(line)
					result = append(result, line)
					continue
				}

				// Check if we've left the sandbox block
				if inSandboxBlock && len(trimmedLine) > 0 && !strings.HasPrefix(trimmedLine, "#") {
					if hasExitedBlock(line, sandboxIndent) {
						inSandboxBlock = false
					}
				}

				// Remove agent: false line if in sandbox block
				if inSandboxBlock && strings.HasPrefix(trimmedLine, "agent:") {
					// Check if this is "agent: false"
					if strings.Contains(trimmedLine, "agent: false") || strings.Contains(trimmedLine, "agent:false") {
						modified = true
						sandboxAgentCodemodLog.Printf("Removed sandbox.agent: false on line %d", i+1)
						continue
					}
				}

				result = append(result, line)
			}

			if !modified {
				return content, false, nil
			}

			// Check if sandbox block is now empty (only has the "sandbox:" line)
			// If so, remove the sandbox block entirely
			var cleanedLines []string
			inSandboxBlock = false
			sandboxLineIndex := -1
			hasSandboxContent := false

			for i, line := range result {
				trimmedLine := strings.TrimSpace(line)

				if strings.HasPrefix(trimmedLine, "sandbox:") {
					inSandboxBlock = true
					sandboxIndent = getIndentation(line)
					sandboxLineIndex = i
					continue
				}

				if inSandboxBlock {
					currentIndent := getIndentation(line)

					// Check if we've left the sandbox block
					if len(trimmedLine) > 0 && !strings.HasPrefix(trimmedLine, "#") && len(currentIndent) <= len(sandboxIndent) && strings.Contains(line, ":") {
						inSandboxBlock = false
					} else if len(trimmedLine) > 0 && len(currentIndent) > len(sandboxIndent) {
						// Found content in sandbox block
						hasSandboxContent = true
					}
				}

				cleanedLines = append(cleanedLines, line)
			}

			// If sandbox block had no content, remove it
			if !hasSandboxContent && sandboxLineIndex >= 0 {
				// Remove the sandbox: line
				finalLines := make([]string, 0, len(cleanedLines))
				for i, line := range cleanedLines {
					if i != sandboxLineIndex {
						finalLines = append(finalLines, line)
					}
				}
				cleanedLines = finalLines
				sandboxAgentCodemodLog.Print("Removed empty sandbox block")
			} else {
				// Use the sandbox: line from result
				cleanedLines = append([]string{}, result...)
			}

			// Reconstruct the content
			newContent := reconstructContent(cleanedLines, markdown)
			sandboxAgentCodemodLog.Print("Applied sandbox.agent: false removal")
			return newContent, true, nil
		},
	}
}

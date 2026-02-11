package cli

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var sandboxAgentCodemodLog = logger.New("cli:codemod_sandbox_agent")

// getSandboxFalseToAgentFalseCodemod creates a codemod for converting sandbox: false to sandbox.agent: false
func getSandboxFalseToAgentFalseCodemod() Codemod {
	return Codemod{
		ID:           "sandbox-false-to-agent-false",
		Name:         "Convert sandbox: false to sandbox.agent: false",
		Description:  "Converts top-level 'sandbox: false' to 'sandbox: { agent: false }' as top-level boolean is no longer supported",
		IntroducedIn: "0.10.0",
		Apply: func(content string, frontmatter map[string]any) (string, bool, error) {
			// Check if sandbox exists and is a boolean false
			sandboxValue, hasSandbox := frontmatter["sandbox"]
			if !hasSandbox {
				return content, false, nil
			}

			sandboxBool, isBool := sandboxValue.(bool)
			if !isBool || sandboxBool {
				// Not a boolean false, skip
				return content, false, nil
			}

			// Parse frontmatter to get raw lines
			frontmatterLines, markdown, err := parseFrontmatterLines(content)
			if err != nil {
				return content, false, err
			}

			// Find and replace "sandbox: false" line
			var modified bool
			result := make([]string, 0, len(frontmatterLines))

			for i, line := range frontmatterLines {
				trimmedLine := strings.TrimSpace(line)

				// Check if this is the "sandbox: false" line
				if strings.HasPrefix(trimmedLine, "sandbox:") {
					if strings.Contains(trimmedLine, "sandbox: false") || strings.Contains(trimmedLine, "sandbox:false") {
						// Get the indentation of the original line
						indent := getIndentation(line)
						
						// Replace with sandbox.agent: false format
						result = append(result, indent+"sandbox:")
						result = append(result, indent+"  agent: false")
						
						modified = true
						sandboxAgentCodemodLog.Printf("Converted sandbox: false to sandbox.agent: false on line %d", i+1)
						continue
					}
				}

				result = append(result, line)
			}

			if !modified {
				return content, false, nil
			}

			// Reconstruct the content
			newContent := reconstructContent(result, markdown)
			sandboxAgentCodemodLog.Print("Applied sandbox: false to sandbox.agent: false conversion")
			return newContent, true, nil
		},
	}
}

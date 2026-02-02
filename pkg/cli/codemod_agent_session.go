package cli

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var agentSessionCodemodLog = logger.New("cli:codemod_agent_session")

// getAgentTaskToAgentSessionCodemod creates a codemod for migrating create-agent-task to create-agent-session
func getAgentTaskToAgentSessionCodemod() Codemod {
	return Codemod{
		ID:           "agent-task-to-agent-session-migration",
		Name:         "Migrate create-agent-task to create-agent-session",
		Description:  "Replaces deprecated 'safe-outputs.create-agent-task' field with 'safe-outputs.create-agent-session'",
		IntroducedIn: "0.4.0",
		Apply: func(content string, frontmatter map[string]any) (string, bool, error) {
			// Check if safe-outputs.create-agent-task exists
			safeOutputsValue, hasSafeOutputs := frontmatter["safe-outputs"]
			if !hasSafeOutputs {
				return content, false, nil
			}

			safeOutputsMap, ok := safeOutputsValue.(map[string]any)
			if !ok {
				return content, false, nil
			}

			// Check if create-agent-task field exists in safe-outputs (deprecated)
			_, hasAgentTask := safeOutputsMap["create-agent-task"]
			if !hasAgentTask {
				return content, false, nil
			}

			// Check if create-agent-session already exists - if so, don't migrate to avoid data loss
			_, hasAgentSession := safeOutputsMap["create-agent-session"]
			if hasAgentSession {
				agentSessionCodemodLog.Print("Skipping migration: create-agent-session already exists")
				return content, false, nil
			}

			// Parse frontmatter to get raw lines
			frontmatterLines, markdown, err := parseFrontmatterLines(content)
			if err != nil {
				return content, false, err
			}

			// Find and replace create-agent-task with create-agent-session within the safe-outputs block
			var modified bool
			var inSafeOutputsBlock bool
			var safeOutputsIndent string

			result := make([]string, len(frontmatterLines))

			for i, line := range frontmatterLines {
				trimmedLine := strings.TrimSpace(line)

				// Track if we're in the safe-outputs block
				if strings.HasPrefix(trimmedLine, "safe-outputs:") {
					inSafeOutputsBlock = true
					safeOutputsIndent = getIndentation(line)
					result[i] = line
					continue
				}

				// Check if we've left the safe-outputs block
				if inSafeOutputsBlock && len(trimmedLine) > 0 && !strings.HasPrefix(trimmedLine, "#") {
					if hasExitedBlock(line, safeOutputsIndent) {
						inSafeOutputsBlock = false
					}
				}

				// Replace create-agent-task with create-agent-session if in safe-outputs block
				if inSafeOutputsBlock && strings.HasPrefix(trimmedLine, "create-agent-task:") {
					replacedLine, didReplace := findAndReplaceInLine(line, "create-agent-task", "create-agent-session")
					if didReplace {
						result[i] = replacedLine
						modified = true
						agentSessionCodemodLog.Printf("Replaced safe-outputs.create-agent-task with safe-outputs.create-agent-session on line %d", i+1)
					} else {
						result[i] = line
					}
				} else {
					result[i] = line
				}
			}

			if !modified {
				return content, false, nil
			}

			// Reconstruct the content
			newContent := reconstructContent(result, markdown)
			agentSessionCodemodLog.Print("Applied create-agent-task to create-agent-session migration")
			return newContent, true, nil
		},
	}
}

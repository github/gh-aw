package cli

import (
	"github.com/github/gh-aw/pkg/logger"
)

var slashCommandCodemodLog = logger.New("cli:codemod_slash_command")

// getCommandToSlashCommandCodemod creates a codemod for migrating on.command to on.slash_command
func getCommandToSlashCommandCodemod() Codemod {
	return Codemod{
		ID:           "command-to-slash-command-migration",
		Name:         "Migrate on.command to on.slash_command",
		Description:  "Replaces deprecated 'on.command' field with 'on.slash_command'",
		IntroducedIn: "0.2.0",
		Apply: func(content string, frontmatter map[string]any) (string, bool, error) {
			// Check if on.command exists
			onValue, hasOn := frontmatter["on"]
			if !hasOn {
				return content, false, nil
			}

			onMap, ok := onValue.(map[string]any)
			if !ok {
				return content, false, nil
			}

			// Check if command field exists in on
			_, hasCommand := onMap["command"]
			if !hasCommand {
				return content, false, nil
			}

			// Parse frontmatter to get raw lines
			frontmatterLines, markdown, err := parseFrontmatterLines(content)
			if err != nil {
				return content, false, err
			}

			// Find and replace the command line within the on: block
			var modified bool
			var inOnBlock bool
			var onIndent string

			result := make([]string, len(frontmatterLines))

			for i, line := range frontmatterLines {
				trimmedLine := getTrimmedLine(line)

				// Track if we're in the on block
				if startsWithKey(trimmedLine, "on") {
					inOnBlock = true
					onIndent = getIndentation(line)
					result[i] = line
					continue
				}

				// Check if we've left the on block
				if inOnBlock && len(trimmedLine) > 0 && !isComment(trimmedLine) {
					if hasExitedBlock(line, onIndent) {
						inOnBlock = false
					}
				}

				// Replace command with slash_command if in on block
				if inOnBlock && startsWithKey(trimmedLine, "command") {
					replacedLine, didReplace := findAndReplaceInLine(line, "command", "slash_command")
					if didReplace {
						result[i] = replacedLine
						modified = true
						slashCommandCodemodLog.Printf("Replaced on.command with on.slash_command on line %d", i+1)
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
			slashCommandCodemodLog.Print("Applied on.command to on.slash_command migration")
			return newContent, true, nil
		},
	}
}

// Helper functions for better readability
func getTrimmedLine(line string) string {
	return trimSpace(line)
}

func startsWithKey(line, key string) bool {
	return startsWithPrefix(line, key+":")
}

func isComment(line string) bool {
	return startsWithPrefix(line, "#")
}

func startsWithPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func trimSpace(s string) string {
	start := 0
	end := len(s)

	// Trim leading whitespace
	for start < end {
		c := s[start]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			break
		}
		start++
	}

	// Trim trailing whitespace
	for end > start {
		c := s[end-1]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			break
		}
		end--
	}

	return s[start:end]
}

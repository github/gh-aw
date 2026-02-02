package cli

import (
	"github.com/github/gh-aw/pkg/logger"
)

var timeoutMinutesCodemodLog = logger.New("cli:codemod_timeout_minutes")

// getTimeoutMinutesCodemod creates a codemod for migrating timeout_minutes to timeout-minutes
func getTimeoutMinutesCodemod() Codemod {
	return Codemod{
		ID:           "timeout-minutes-migration",
		Name:         "Migrate timeout_minutes to timeout-minutes",
		Description:  "Replaces deprecated 'timeout_minutes' field with 'timeout-minutes'",
		IntroducedIn: "0.1.0",
		Apply: func(content string, frontmatter map[string]any) (string, bool, error) {
			// Check if the deprecated field exists
			value, exists := frontmatter["timeout_minutes"]
			if !exists {
				return content, false, nil
			}

			// Parse frontmatter to get raw lines
			frontmatterLines, markdown, err := parseFrontmatterLines(content)
			if err != nil {
				return content, false, err
			}

			// Replace the field in raw lines while preserving formatting
			var modified bool
			result := make([]string, len(frontmatterLines))
			for i, line := range frontmatterLines {
				replacedLine, didReplace := findAndReplaceInLine(line, "timeout_minutes", "timeout-minutes")
				if didReplace {
					result[i] = replacedLine
					modified = true
					timeoutMinutesCodemodLog.Printf("Replaced timeout_minutes with timeout-minutes on line %d", i+1)
				} else {
					result[i] = line
				}
			}

			if !modified {
				return content, false, nil
			}

			// Reconstruct the content
			newContent := reconstructContent(result, markdown)
			timeoutMinutesCodemodLog.Printf("Applied timeout_minutes migration (value: %v)", value)
			return newContent, true, nil
		},
	}
}

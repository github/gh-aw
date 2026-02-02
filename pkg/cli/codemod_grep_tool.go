package cli

import "github.com/github/gh-aw/pkg/logger"

var grepToolCodemodLog = logger.New("cli:codemod_grep_tool")

// getGrepToolRemovalCodemod creates a codemod for removing the deprecated tools.grep field
func getGrepToolRemovalCodemod() Codemod {
	return Codemod{
		ID:           "grep-tool-removal",
		Name:         "Remove deprecated tools.grep field",
		Description:  "Removes 'tools.grep' field as grep is now always enabled as part of default bash tools",
		IntroducedIn: "0.7.0",
		Apply: func(content string, frontmatter map[string]any) (string, bool, error) {
			// Check if tools.grep exists
			toolsValue, hasTools := frontmatter["tools"]
			if !hasTools {
				return content, false, nil
			}

			toolsMap, ok := toolsValue.(map[string]any)
			if !ok {
				return content, false, nil
			}

			// Check if grep field exists in tools
			_, hasGrep := toolsMap["grep"]
			if !hasGrep {
				return content, false, nil
			}

			// Parse frontmatter to get raw lines
			frontmatterLines, markdown, err := parseFrontmatterLines(content)
			if err != nil {
				return content, false, err
			}

			// Remove the grep field from the tools block
			modifiedLines, modified := removeFieldFromBlock(frontmatterLines, "grep", "tools")
			if !modified {
				return content, false, nil
			}

			// Reconstruct the content
			newContent := reconstructContent(modifiedLines, markdown)
			grepToolCodemodLog.Print("Applied grep tool removal")
			return newContent, true, nil
		},
	}
}

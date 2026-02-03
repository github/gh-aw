package cli

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var installScriptURLCodemodLog = logger.New("cli:codemod_install_script_url")

// getInstallScriptURLCodemod creates a codemod for migrating githubnext/gh-aw to github/gh-aw in install script URLs
func getInstallScriptURLCodemod() Codemod {
	return Codemod{
		ID:           "install-script-url-migration",
		Name:         "Migrate install script URL from githubnext/gh-aw to github/gh-aw",
		Description:  "Updates install script URLs in job steps from the older githubnext/gh-aw location to the new github/gh-aw location",
		IntroducedIn: "0.9.0",
		Apply: func(content string, frontmatter map[string]any) (string, bool, error) {
			// Parse frontmatter to get raw lines
			frontmatterLines, markdown, err := parseFrontmatterLines(content)
			if err != nil {
				return content, false, err
			}

			// Define patterns to search and replace
			// Order matters: Check URL patterns first (with slash), then general patterns
			oldPatterns := []string{
				"https://raw.githubusercontent.com/githubnext/gh-aw/",
				"githubnext/gh-aw",
			}

			newReplacements := []string{
				"https://raw.githubusercontent.com/github/gh-aw/",
				"github/gh-aw",
			}

			modified := false
			result := make([]string, len(frontmatterLines))

			for i, line := range frontmatterLines {
				modifiedLine := line

				// Try to replace each old pattern with the new one in all lines
				for j, oldPattern := range oldPatterns {
					if strings.Contains(modifiedLine, oldPattern) {
						modifiedLine = strings.ReplaceAll(modifiedLine, oldPattern, newReplacements[j])
						modified = true
						installScriptURLCodemodLog.Printf("Replaced '%s' with '%s' on line %d", oldPattern, newReplacements[j], i+1)
					}
				}

				result[i] = modifiedLine
			}

			if !modified {
				return content, false, nil
			}

			// Reconstruct the content
			newContent := reconstructContent(result, markdown)
			installScriptURLCodemodLog.Print("Applied install script URL migration")
			return newContent, true, nil
		},
	}
}

package cli

import (
	"github.com/github/gh-aw/pkg/logger"
)

var safeInputsModeCodemodLog = logger.New("cli:codemod_safe_inputs")

// getSafeInputsModeCodemod creates a codemod for removing the deprecated safe-inputs.mode field
func getSafeInputsModeCodemod() Codemod {
	return Codemod{
		ID:           "safe-inputs-mode-removal",
		Name:         "Remove deprecated safe-inputs.mode field",
		Description:  "Removes the deprecated 'safe-inputs.mode' field (HTTP is now the only supported mode)",
		IntroducedIn: "0.2.0",
		Apply: func(content string, frontmatter map[string]any) (string, bool, error) {
			// Check if safe-inputs.mode exists
			safeInputsValue, hasSafeInputs := frontmatter["safe-inputs"]
			if !hasSafeInputs {
				return content, false, nil
			}

			safeInputsMap, ok := safeInputsValue.(map[string]any)
			if !ok {
				return content, false, nil
			}

			// Check if mode field exists in safe-inputs
			_, hasMode := safeInputsMap["mode"]
			if !hasMode {
				return content, false, nil
			}

			// Parse frontmatter to get raw lines
			frontmatterLines, markdown, err := parseFrontmatterLines(content)
			if err != nil {
				return content, false, err
			}

			// Remove the mode field from the safe-inputs block
			result, modified := removeFieldFromBlock(frontmatterLines, "mode", "safe-inputs")
			if !modified {
				return content, false, nil
			}

			// Reconstruct the content
			newContent := reconstructContent(result, markdown)
			safeInputsModeCodemodLog.Print("Applied safe-inputs.mode removal")
			return newContent, true, nil
		},
	}
}

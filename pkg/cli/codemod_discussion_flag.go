package cli

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var discussionFlagCodemodLog = logger.New("cli:codemod_discussion_flag")

// getDiscussionFlagRemovalCodemod creates a codemod for removing the deprecated discussion field from add-comment
func getDiscussionFlagRemovalCodemod() Codemod {
	return Codemod{
		ID:           "add-comment-discussion-removal",
		Name:         "Remove deprecated add-comment.discussion field",
		Description:  "Removes the deprecated 'safe-outputs.add-comment.discussion' field (detection is now automatic based on context)",
		IntroducedIn: "0.3.0",
		Apply: func(content string, frontmatter map[string]any) (string, bool, error) {
			// Check if safe-outputs exists
			safeOutputsValue, hasSafeOutputs := frontmatter["safe-outputs"]
			if !hasSafeOutputs {
				return content, false, nil
			}

			safeOutputsMap, ok := safeOutputsValue.(map[string]any)
			if !ok {
				return content, false, nil
			}

			// Check if add-comment exists in safe-outputs
			addCommentValue, hasAddComment := safeOutputsMap["add-comment"]
			if !hasAddComment {
				return content, false, nil
			}

			addCommentMap, ok := addCommentValue.(map[string]any)
			if !ok {
				return content, false, nil
			}

			// Check if discussion field exists in add-comment
			_, hasDiscussion := addCommentMap["discussion"]
			if !hasDiscussion {
				return content, false, nil
			}

			// Parse frontmatter to get raw lines
			frontmatterLines, markdown, err := parseFrontmatterLines(content)
			if err != nil {
				return content, false, err
			}

			// Remove the discussion field from the add-comment block in safe-outputs
			var result []string
			var modified bool
			var inSafeOutputsBlock bool
			var safeOutputsIndent string
			var inAddCommentBlock bool
			var addCommentIndent string
			var inDiscussionField bool

			for i, line := range frontmatterLines {
				trimmedLine := strings.TrimSpace(line)

				// Track if we're in the safe-outputs block
				if strings.HasPrefix(trimmedLine, "safe-outputs:") {
					inSafeOutputsBlock = true
					safeOutputsIndent = getIndentation(line)
					result = append(result, line)
					continue
				}

				// Check if we've left the safe-outputs block
				if inSafeOutputsBlock && len(trimmedLine) > 0 && !strings.HasPrefix(trimmedLine, "#") {
					if hasExitedBlock(line, safeOutputsIndent) {
						inSafeOutputsBlock = false
						inAddCommentBlock = false
					}
				}

				// Track if we're in the add-comment block within safe-outputs
				if inSafeOutputsBlock && strings.HasPrefix(trimmedLine, "add-comment:") {
					inAddCommentBlock = true
					addCommentIndent = getIndentation(line)
					result = append(result, line)
					continue
				}

				// Check if we've left the add-comment block
				if inAddCommentBlock && len(trimmedLine) > 0 && !strings.HasPrefix(trimmedLine, "#") {
					if hasExitedBlock(line, addCommentIndent) {
						inAddCommentBlock = false
					}
				}

				// Remove discussion field line if in add-comment block
				if inAddCommentBlock && strings.HasPrefix(trimmedLine, "discussion:") {
					modified = true
					inDiscussionField = true
					discussionFlagCodemodLog.Printf("Removed safe-outputs.add-comment.discussion on line %d", i+1)
					continue
				}

				// Skip any nested content under the discussion field (shouldn't be any, but for completeness)
				if inDiscussionField {
					// Empty lines within the field block should be removed
					if len(trimmedLine) == 0 {
						continue
					}

					currentIndent := getIndentation(line)
					discussionIndent := addCommentIndent + "  " // discussion would be 2 spaces more than add-comment

					// If this line has more indentation than discussion field, skip it
					if len(currentIndent) > len(discussionIndent) {
						discussionFlagCodemodLog.Printf("Removed nested discussion property on line %d: %s", i+1, trimmedLine)
						continue
					}
					// We've exited the discussion field
					inDiscussionField = false
				}

				result = append(result, line)
			}

			if !modified {
				return content, false, nil
			}

			// Reconstruct the content
			newContent := reconstructContent(result, markdown)
			discussionFlagCodemodLog.Print("Applied add-comment.discussion removal")
			return newContent, true, nil
		},
	}
}

package cli

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var uploadAssetsCodemodLog = logger.New("cli:codemod_upload_assets")

// getUploadAssetsCodemod creates a codemod for migrating upload-assets to upload-asset (plural to singular)
func getUploadAssetsCodemod() Codemod {
	return Codemod{
		ID:           "upload-assets-to-upload-asset-migration",
		Name:         "Migrate upload-assets to upload-asset",
		Description:  "Replaces deprecated 'safe-outputs.upload-assets' field with 'safe-outputs.upload-asset' (plural to singular)",
		IntroducedIn: "0.3.0",
		Apply: func(content string, frontmatter map[string]any) (string, bool, error) {
			// Check if safe-outputs.upload-assets exists
			safeOutputsValue, hasSafeOutputs := frontmatter["safe-outputs"]
			if !hasSafeOutputs {
				return content, false, nil
			}

			safeOutputsMap, ok := safeOutputsValue.(map[string]any)
			if !ok {
				return content, false, nil
			}

			// Check if upload-assets field exists in safe-outputs (plural is deprecated)
			_, hasUploadAssets := safeOutputsMap["upload-assets"]
			if !hasUploadAssets {
				return content, false, nil
			}

			// Parse frontmatter to get raw lines
			frontmatterLines, markdown, err := parseFrontmatterLines(content)
			if err != nil {
				return content, false, err
			}

			// Find and replace upload-assets with upload-asset within the safe-outputs block
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

				// Replace upload-assets with upload-asset if in safe-outputs block
				if inSafeOutputsBlock && strings.HasPrefix(trimmedLine, "upload-assets:") {
					replacedLine, didReplace := findAndReplaceInLine(line, "upload-assets", "upload-asset")
					if didReplace {
						result[i] = replacedLine
						modified = true
						uploadAssetsCodemodLog.Printf("Replaced safe-outputs.upload-assets with safe-outputs.upload-asset on line %d", i+1)
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
			uploadAssetsCodemodLog.Print("Applied upload-assets to upload-asset migration")
			return newContent, true, nil
		},
	}
}

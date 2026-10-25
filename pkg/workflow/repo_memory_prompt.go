package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var repoMemoryPromptLog = logger.New("workflow:repo_memory_prompt")

// buildRepoMemoryPromptSection builds a PromptSection for repo memory instructions.
// Returns a PromptSection that references a template file with substitutions, or nil if no memory is configured.
func buildRepoMemoryPromptSection(config *RepoMemoryConfig) *PromptSection {
	if config == nil || len(config.Memories) == 0 {
		return nil
	}

	// Check if there's only one memory with ID "default" to use singular template
	if len(config.Memories) == 1 && config.Memories[0].ID == "default" {
		memory := config.Memories[0]
		memoryDir := fmt.Sprintf("/tmp/gh-aw/repo-memory/%s/", memory.ID)

		repoMemoryPromptLog.Printf("Building single default repo memory prompt section: branch=%s", memory.BranchName)

		// Build description text (with leading space if non-empty)
		descriptionText := ""
		if memory.Description != "" {
			descriptionText = " " + memory.Description
		}

		// Build target repo text
		targetRepoText := " of the current repository"
		if memory.TargetRepo != "" {
			targetRepoText = fmt.Sprintf(" of repository `%s`", memory.TargetRepo)
		}

		// Build constraints section
		// The value is either "\n" (blank line only) or "\n\n**Constraints:**\n...\n"
		// so that the template line __GH_AW_MEMORY_CONSTRAINTS__\nExamples... renders correctly.
		constraintsText := "\n"
		if len(memory.FileGlob) > 0 || memory.MaxFileSize > 0 || memory.MaxFileCount > 0 || memory.MaxPatchSize > 0 {
			var constraints strings.Builder
			constraints.WriteString("\n\n**Constraints:**\n")
			if len(memory.FileGlob) > 0 {
				fmt.Fprintf(&constraints, "- **Allowed Files**: Only files matching patterns: %s\n", strings.Join(memory.FileGlob, ", "))
			}
			if memory.MaxFileSize > 0 {
				fmt.Fprintf(&constraints, "- **Max File Size**: %d bytes (%.2f MB) per file\n", memory.MaxFileSize, float64(memory.MaxFileSize)/1048576.0)
			}
			if memory.MaxFileCount > 0 {
				fmt.Fprintf(&constraints, "- **Max File Count**: %d files per commit\n", memory.MaxFileCount)
			}
			if memory.MaxPatchSize > 0 {
				fmt.Fprintf(&constraints, "- **Max Patch Size**: %d bytes (%d KB) total per push (max: %d KB)\n", memory.MaxPatchSize, memory.MaxPatchSize/1024, maxRepoMemoryPatchSize/1024)
			}
			constraintsText = constraints.String()
		}

		wikiNoteText := ""
		if memory.Wiki {
			repoMemoryPromptLog.Print("Wiki mode enabled for repo memory")
			wikiNoteText = "\n\n> **GitHub Wiki**: This memory is backed by the GitHub Wiki for this repository. " +
				"Files use GitHub Wiki Markdown syntax. Follow GitHub Wiki conventions when creating or editing pages " +
				"(e.g., use standard Markdown headers, use `[[Page Name]]` syntax for internal wiki links, " +
				"name page files with spaces replaced by hyphens or use the wiki page title as the filename)."
		}

		repoMemoryPromptLog.Printf("Built single repo memory prompt section: branch=%s, has_constraints=%t, wiki=%t",
			memory.BranchName, len(constraintsText) > 2, memory.Wiki)

		return &PromptSection{
			Content: repoMemoryPromptFile,
			IsFile:  true,
			EnvVars: map[string]string{
				"GH_AW_MEMORY_DIR":         memoryDir,
				"GH_AW_MEMORY_DESCRIPTION": descriptionText,
				"GH_AW_MEMORY_BRANCH_NAME": memory.BranchName,
				"GH_AW_MEMORY_TARGET_REPO": targetRepoText,
				"GH_AW_MEMORY_CONSTRAINTS": constraintsText,
				"GH_AW_WIKI_NOTE":          wikiNoteText,
			},
		}
	}

	// Multiple memories or non-default single memory - use multi template
	repoMemoryPromptLog.Printf("Building multiple repo memory prompt section: count=%d", len(config.Memories))

	// Build memory list
	var memoryList strings.Builder
	for _, memory := range config.Memories {
		memoryDir := fmt.Sprintf("/tmp/gh-aw/repo-memory/%s/", memory.ID)
		fmt.Fprintf(&memoryList, "- **%s**: `%s`", memory.ID, memoryDir)
		if memory.Description != "" {
			fmt.Fprintf(&memoryList, " - %s", memory.Description)
		}
		fmt.Fprintf(&memoryList, " (branch: `%s`", memory.BranchName)
		if memory.TargetRepo != "" {
			fmt.Fprintf(&memoryList, " in `%s`", memory.TargetRepo)
		}
		if memory.Wiki {
			memoryList.WriteString(", GitHub Wiki")
		}
		memoryList.WriteString(")\n")
	}

	// Build allowed extensions text - check if all memories have the same extensions
	allowedExtsText := strings.Join(config.Memories[0].AllowedExtensions, "`, `")
	allSame := true
	for i := 1; i < len(config.Memories); i++ {
		if len(config.Memories[i].AllowedExtensions) != len(config.Memories[0].AllowedExtensions) {
			allSame = false
			break
		}
		for j, ext := range config.Memories[i].AllowedExtensions {
			if ext != config.Memories[0].AllowedExtensions[j] {
				allSame = false
				break
			}
		}
		if !allSame {
			break
		}
	}

	// If not all the same, build a union of all extensions
	if !allSame {
		repoMemoryPromptLog.Print("Memories have different allowed extensions, building union set")
		extensionSet := make(map[string]struct {
		})
		for _, mem := range config.Memories {
			for _, ext := range mem.AllowedExtensions {
				extensionSet[ext] = struct {
				}{}
			}
		}
		// Convert set to sorted slice for consistent output
		allExtensions := sliceutil.SortedKeys(extensionSet)
		allowedExtsText = strings.Join(allExtensions, "`, `")
	}

	repoMemoryPromptLog.Printf("Built multi repo memory prompt section: memories=%d, extensions=%q, all_same_exts=%t",
		len(config.Memories), allowedExtsText, allSame)

	return &PromptSection{
		Content: repoMemoryPromptMultiFile,
		IsFile:  true,
		EnvVars: map[string]string{
			"GH_AW_MEMORY_LIST":               memoryList.String(),
			"GH_AW_MEMORY_ALLOWED_EXTENSIONS": allowedExtsText,
		},
	}
}

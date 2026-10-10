package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var removeLog = logger.New("cli:remove_command")

// RemoveWorkflows removes workflows matching a pattern
func RemoveWorkflows(pattern string, keepOrphans bool, workflowDir string) error {
	removeLog.Printf("Removing workflows: pattern=%q, keepOrphans=%v, workflowDir=%q", pattern, keepOrphans, workflowDir)
	workflowsDir := workflowDir
	if workflowsDir == "" {
		workflowsDir = getWorkflowsDir()
	}

	if _, err := os.Stat(workflowsDir); os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No .github/workflows directory found."))
		return nil
	}

	// Find all markdown files in .github/workflows
	mdFiles, err := filepath.Glob(filepath.Join(workflowsDir, "*.md"))
	if err != nil {
		return fmt.Errorf("failed to find workflow files: %w", err)
	}

	// Filter out README.md files
	mdFiles = filterWorkflowFiles(mdFiles)

	removeLog.Printf("Found %d workflow files", len(mdFiles))
	if len(mdFiles) == 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No workflow files found to remove."))
		return nil
	}

	var filesToRemove []string

	// If no pattern specified, list all files for user to see
	if pattern == "" {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Available workflows to remove:"))
		for _, file := range mdFiles {
			workflowName, _ := extractWorkflowNameFromFile(file)
			base := filepath.Base(file)
			name := normalizeWorkflowID(base)
			if workflowName != "" {
				fmt.Fprintf(os.Stderr, "  %-20s - %s\n", name, workflowName)
			} else {
				fmt.Fprintf(os.Stderr, "  %s\n", name)
			}
		}
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("\nUsage: %s remove <filter>", string(constants.CLIExtensionPrefix))))
		return nil
	}

	// Find matching files by workflow name or filename
	for _, file := range mdFiles {
		base := filepath.Base(file)
		filename := normalizeWorkflowID(base)
		workflowName, _ := extractWorkflowNameFromFile(file)

		// Check if pattern matches filename or workflow name
		if strings.Contains(strings.ToLower(filename), strings.ToLower(pattern)) ||
			strings.Contains(strings.ToLower(workflowName), strings.ToLower(pattern)) {
			filesToRemove = append(filesToRemove, file)
		}
	}

	if len(filesToRemove) == 0 {
		removeLog.Printf("No workflows matched pattern: %q", pattern)
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No workflows found matching pattern: "+pattern))
		return nil
	}

	removeLog.Printf("Found %d workflows to remove", len(filesToRemove))

	// Preview orphaned includes that would be removed (if orphan removal is enabled)
	var orphanedIncludes []string
	if !keepOrphans {
		var err error
		orphanedIncludes, err = previewOrphanedIncludes(filesToRemove, false)
		if err != nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to preview orphaned includes: %v", err)))
			orphanedIncludes = []string{} // Continue with empty list
		}
	}

	// Show what will be removed
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("The following workflows will be removed:"))
	for _, file := range filesToRemove {
		workflowName, _ := extractWorkflowNameFromFile(file)
		if workflowName != "" {
			fmt.Fprintf(os.Stderr, "  %s - %s\n", filepath.Base(file), workflowName)
		} else {
			fmt.Fprintf(os.Stderr, "  %s\n", filepath.Base(file))
		}

		// Also check for corresponding .lock.yml file in .github/workflows
		lockFile := stringutil.MarkdownToLockFile(file)
		if fileutil.FileExists(lockFile) {
			fmt.Fprintf(os.Stderr, "  %s (compiled workflow)\n", filepath.Base(lockFile))
		}
	}

	// Show orphaned includes that will also be removed
	if len(orphanedIncludes) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("\nThe following orphaned include files will also be removed (suppress with --no-remove-orphans):"))
		for _, include := range orphanedIncludes {
			fmt.Fprintf(os.Stderr, "  %s (orphaned include)\n", include)
		}
	}

	// Ask for confirmation
	confirmed, err := console.ConfirmAction(
		"Are you sure you want to remove these workflows?",
		"Yes, remove",
		"No, cancel",
	)
	if err != nil {
		return fmt.Errorf("failed to get confirmation: %w", err)
	}
	if !confirmed {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Operation cancelled."))
		return nil
	}

	// Remove the files
	var removedFiles []string
	removedPackageSources := make(map[string]struct{})
	for _, file := range filesToRemove {
		if source := readFullSourceFromFile(file); source != "" {
			if repoSpec, ok, err := parseManifestSourceSpec(source); err == nil && ok && repoSpec != nil {
				removedPackageSources[repositoryPackageIdentifier(repoSpec.RepoSlug, repoSpec.PackagePath)] = struct{}{}
			}
		}
		if err := os.Remove(file); err != nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to remove %s: %v", file, err)))
		} else {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Removed: "+filepath.Base(file)))
			removedFiles = append(removedFiles, file)
		}

		// Also remove corresponding .lock.yml file
		lockFile := stringutil.MarkdownToLockFile(file)
		if fileutil.FileExists(lockFile) {
			if err := os.Remove(lockFile); err != nil {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to remove %s: %v", lockFile, err)))
			} else {
				fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Removed: "+filepath.Base(lockFile)))
			}
		}
	}

	// Clean up orphaned include files (if orphan removal is enabled)
	if len(removedFiles) > 0 && !keepOrphans {
		if err := cleanupOrphanedIncludes(false); err != nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to clean up orphaned includes: %v", err)))
		}
	}
	for packageSource := range removedPackageSources {
		if err := removePackageOwnedFilesIfUnused(packageSource); err != nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to remove package-owned files for %s: %v", packageSource, err)))
		}
	}

	// Stage changes to git if in a git repository
	if len(removedFiles) > 0 && isGitRepo() {
		stageWorkflowChanges()
	}

	return nil
}

// cleanupOrphanedIncludes removes include files that are no longer used by any workflow
func cleanupOrphanedIncludes(verbose bool) error {
	removeLog.Print("Cleaning up orphaned include files")
	// Get all remaining markdown files
	mdFiles, err := getMarkdownWorkflowFiles("")
	if err != nil {
		// No markdown files means we can clean up all includes
		removeLog.Print("No markdown files found, cleaning up all includes")
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No markdown files found, cleaning up all includes"))
		}
		return cleanupAllIncludes(verbose)
	}

	// Collect all include dependencies from remaining workflows
	usedIncludes := make(map[string]struct {
	})

	for _, mdFile := range mdFiles {
		content, err := os.ReadFile(mdFile)
		if err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not read %s for include analysis: %v", mdFile, err)))
			}
			continue
		}

		// Find includes used by this workflow
		includes, err := findIncludesInContent(string(content))
		if err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not analyze includes in %s: %v", mdFile, err)))
			}
			continue
		}

		for _, include := range includes {
			usedIncludes[include] = struct {
			}{}
		}
	}

	// Find all include files in the workflows directory
	// Only consider files in subdirectories (like shared/) as potential include files
	// Root-level .md files are workflow files, not include files
	workflowsDir := constants.GetWorkflowDir()
	var allIncludes []string

	walkErr := filepath.Walk(workflowsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && strings.HasSuffix(info.Name(), ".md") {
			relPath, err := filepath.Rel(workflowsDir, path)
			if err != nil {
				return err
			}

			// Only consider files in subdirectories as potential include files
			// Root-level .md files are workflow files, not include files
			if strings.Contains(relPath, string(filepath.Separator)) {
				allIncludes = append(allIncludes, relPath)
			}
		}

		return nil
	})

	if walkErr != nil {
		return fmt.Errorf("failed to scan include files: %w", walkErr)
	}

	// Remove unused includes
	for _, include := range allIncludes {
		if !setutil.Contains(usedIncludes, include) {
			includePath := filepath.Join(workflowsDir, include)
			if err := os.Remove(includePath); err != nil {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to remove orphaned include %s: %v", include, err)))
				}
			} else {
				fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Removed orphaned include: "+include))
			}
		}
	}

	return nil
}

// previewOrphanedIncludes returns a list of include files that would become orphaned if the specified files were removed
func previewOrphanedIncludes(filesToRemove []string, verbose bool) ([]string, error) {
	// Get all current markdown files
	allMdFiles, err := getMarkdownWorkflowFiles("")
	if err != nil {
		return nil, err
	}

	// Create a map of files to remove for quick lookup
	removeMap := make(map[string]struct {
	})
	for _, file := range filesToRemove {
		removeMap[file] = struct {
		}{}
	}

	// Get the files that would remain after removal
	var remainingFiles []string
	for _, file := range allMdFiles {
		if !setutil.Contains(removeMap, file) {
			remainingFiles = append(remainingFiles, file)
		}
	}

	// If no files remain, all include files would be orphaned
	if len(remainingFiles) == 0 {
		return getAllIncludeFiles()
	}

	// Collect all include dependencies from remaining workflows
	usedIncludes := make(map[string]struct {
	})

	for _, mdFile := range remainingFiles {
		content, err := os.ReadFile(mdFile)
		if err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not read %s for include analysis: %v", mdFile, err)))
			}
			continue
		}

		// Find includes used by this workflow
		includes, err := findIncludesInContent(string(content))
		if err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not analyze includes in %s: %v", mdFile, err)))
			}
			continue
		}

		for _, include := range includes {
			usedIncludes[include] = struct {
			}{}
		}
	}

	// Find all include files and check which ones would be orphaned
	allIncludes, err := getAllIncludeFiles()
	if err != nil {
		return nil, err
	}

	var orphanedIncludes []string
	for _, include := range allIncludes {
		if !setutil.Contains(usedIncludes, include) {
			orphanedIncludes = append(orphanedIncludes, include)
		}
	}

	return orphanedIncludes, nil
}

// getAllIncludeFiles returns all include files in .github/workflows subdirectories
func getAllIncludeFiles() ([]string, error) {
	workflowsDir := constants.GetWorkflowDir()
	var allIncludes []string

	walkErr := filepath.Walk(workflowsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && strings.HasSuffix(info.Name(), ".md") {
			relPath, err := filepath.Rel(workflowsDir, path)
			if err != nil {
				return err
			}

			// Only consider files in subdirectories as potential include files
			// Root-level .md files are workflow files, not include files
			if strings.Contains(relPath, string(filepath.Separator)) {
				allIncludes = append(allIncludes, relPath)
			}
		}

		return nil
	})

	return allIncludes, walkErr
}

// cleanupAllIncludes removes all include files when no workflows remain
func cleanupAllIncludes(verbose bool) error {
	workflowsDir := constants.GetWorkflowDir()

	walkErr := filepath.Walk(workflowsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && strings.HasSuffix(info.Name(), ".md") {
			relPath, _ := filepath.Rel(workflowsDir, path)

			// Only remove files in subdirectories (like shared/) as these are include files
			// Root-level .md files are workflow files, not include files
			if strings.Contains(relPath, string(filepath.Separator)) {
				if err := os.Remove(path); err != nil {
					if verbose {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to remove include %s: %v", relPath, err)))
					}
				} else {
					fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Removed include: "+relPath))
				}
			}
		}

		return nil
	})

	return walkErr
}

// findIncludesInContent finds all import references in content
func findIncludesInContent(content string) ([]string, error) {
	var includes []string
	// Manual index-based scan avoids the iter.Seq yield overhead of strings.Lines.
	for remaining := content; remaining != ""; {
		var line string
		if idx := strings.IndexByte(remaining, '\n'); idx >= 0 {
			line = remaining[:idx]
			remaining = remaining[idx+1:]
		} else {
			line = remaining
			remaining = ""
		}
		if path := parseIncludePath(line); path != "" {
			if includes == nil {
				includes = make([]string, 0, 4)
			}
			includes = append(includes, path)
		}
	}

	if includes == nil {
		return []string{}, nil
	}
	return includes, nil
}

// parseIncludePath extracts the file path from @include/@import/{{#import}} directive lines
// without allocating a regex submatch slice or a directive struct.
// Returns an empty string if the line is not a recognised directive.
// Section references (e.g. file.md#Section) are stripped from the returned path.
func parseIncludePath(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return ""
	}

	// Fast path: the vast majority of lines are not directives.
	// Checking the first byte avoids three full HasPrefix comparisons.
	if trimmed[0] != '@' && trimmed[0] != '{' {
		return ""
	}

	var rest string

	switch {
	case strings.HasPrefix(trimmed, "@include"):
		rest = trimmed[len("@include"):]
	case strings.HasPrefix(trimmed, "@import"):
		rest = trimmed[len("@import"):]
	case strings.HasPrefix(trimmed, "{{#import"):
		rest = trimmed[len("{{#import"):]
		// Skip optional marker '?'
		if rest != "" && rest[0] == '?' {
			rest = rest[1:]
		}
		// Skip optional whitespace, then an optional single colon, then optional whitespace
		// (mirrors the regex \s*:?\s* in IncludeDirectivePattern)
		rest = strings.TrimSpace(rest)
		if rest != "" && rest[0] == ':' {
			rest = strings.TrimSpace(rest[1:])
		}
		// Extract path up to closing "}}" and require only whitespace after it.
		before, after, ok := strings.Cut(rest, "}}")
		if !ok || strings.TrimSpace(after) != "" {
			return ""
		}
		path := strings.TrimSpace(before)
		if path == "" {
			return ""
		}
		// Strip section reference (file.md#Section → file.md)
		if filePath, _, ok := strings.Cut(path, "#"); ok {
			return filePath
		}
		return path
	default:
		return ""
	}

	// Handle @include and @import: skip optional marker '?'
	if rest != "" && rest[0] == '?' {
		rest = rest[1:]
	}
	// Require at least one whitespace character after the directive keyword
	if rest == "" || (rest[0] != ' ' && rest[0] != '\t') {
		return ""
	}
	path := strings.TrimSpace(rest)
	if path == "" {
		return ""
	}
	// Strip section reference (file.md#Section → file.md)
	if filePath, _, ok := strings.Cut(path, "#"); ok {
		return filePath
	}
	return path
}

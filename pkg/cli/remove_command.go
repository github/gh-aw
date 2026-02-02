package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var removeLog = logger.New("cli:remove_command")

// RemoveWorkflows removes workflows matching a pattern
func RemoveWorkflows(pattern string, keepOrphans bool) error {
	removeLog.Printf("Removing workflows: pattern=%q, keepOrphans=%v", pattern, keepOrphans)
	workflowsDir := getWorkflowsDir()

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
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("\nUsage: %s remove <pattern>", string(constants.CLIExtensionPrefix))))
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
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("No workflows found matching pattern: %s", pattern)))
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
		if _, err := os.Stat(lockFile); err == nil {
			fmt.Fprintf(os.Stderr, "  %s (compiled workflow)\n", filepath.Base(lockFile))
		}
	}

	// Show orphaned includes that will also be removed
	if len(orphanedIncludes) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("\nThe following orphaned include files will also be removed (suppress with --keep-orphans):"))
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
	for _, file := range filesToRemove {
		if err := os.Remove(file); err != nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to remove %s: %v", file, err)))
		} else {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Removed: %s", filepath.Base(file))))
			removedFiles = append(removedFiles, file)
		}

		// Also remove corresponding .lock.yml file
		lockFile := stringutil.MarkdownToLockFile(file)
		if _, err := os.Stat(lockFile); err == nil {
			if err := os.Remove(lockFile); err != nil {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to remove %s: %v", lockFile, err)))
			} else {
				fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Removed: %s", filepath.Base(lockFile))))
			}
		}
	}

	// Clean up orphaned include files (if orphan removal is enabled)
	if len(removedFiles) > 0 && !keepOrphans {
		if err := cleanupOrphanedIncludes(false); err != nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to clean up orphaned includes: %v", err)))
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
	usedIncludes := make(map[string]bool)

	for _, mdFile := range mdFiles {
		content, err := os.ReadFile(mdFile)
		if err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not read %s for include analysis: %v", mdFile, err)))
			}
			continue
		}

		// Find includes used by this workflow
		includes, err := findIncludesInContent(string(content), filepath.Dir(mdFile), verbose)
		if err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not analyze includes in %s: %v", mdFile, err)))
			}
			continue
		}

		for _, include := range includes {
			usedIncludes[include] = true
		}
	}

	// Find all include files in .github/workflows
	// Only consider files in subdirectories (like shared/) as potential include files
	// Root-level .md files are workflow files, not include files
	workflowsDir := ".github/workflows"
	var allIncludes []string

	err = filepath.Walk(workflowsDir, func(path string, info os.FileInfo, err error) error {
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

	if err != nil {
		return fmt.Errorf("failed to scan include files: %w", err)
	}

	// Remove unused includes
	for _, include := range allIncludes {
		if !usedIncludes[include] {
			includePath := filepath.Join(workflowsDir, include)
			if err := os.Remove(includePath); err != nil {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to remove orphaned include %s: %v", include, err)))
				}
			} else {
				fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Removed orphaned include: %s", include)))
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
	removeMap := make(map[string]bool)
	for _, file := range filesToRemove {
		removeMap[file] = true
	}

	// Get the files that would remain after removal
	var remainingFiles []string
	for _, file := range allMdFiles {
		if !removeMap[file] {
			remainingFiles = append(remainingFiles, file)
		}
	}

	// If no files remain, all include files would be orphaned
	if len(remainingFiles) == 0 {
		return getAllIncludeFiles()
	}

	// Collect all include dependencies from remaining workflows
	usedIncludes := make(map[string]bool)

	for _, mdFile := range remainingFiles {
		content, err := os.ReadFile(mdFile)
		if err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not read %s for include analysis: %v", mdFile, err)))
			}
			continue
		}

		// Find includes used by this workflow
		includes, err := findIncludesInContent(string(content), filepath.Dir(mdFile), verbose)
		if err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not analyze includes in %s: %v", mdFile, err)))
			}
			continue
		}

		for _, include := range includes {
			usedIncludes[include] = true
		}
	}

	// Find all include files and check which ones would be orphaned
	allIncludes, err := getAllIncludeFiles()
	if err != nil {
		return nil, err
	}

	var orphanedIncludes []string
	for _, include := range allIncludes {
		if !usedIncludes[include] {
			orphanedIncludes = append(orphanedIncludes, include)
		}
	}

	return orphanedIncludes, nil
}

// getAllIncludeFiles returns all include files in .github/workflows subdirectories
func getAllIncludeFiles() ([]string, error) {
	workflowsDir := ".github/workflows"
	var allIncludes []string

	err := filepath.Walk(workflowsDir, func(path string, info os.FileInfo, err error) error {
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

	return allIncludes, err
}

// cleanupAllIncludes removes all include files when no workflows remain
func cleanupAllIncludes(verbose bool) error {
	workflowsDir := ".github/workflows"

	err := filepath.Walk(workflowsDir, func(path string, info os.FileInfo, err error) error {
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
					fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Removed include: %s", relPath)))
				}
			}
		}

		return nil
	})

	return err
}

// findIncludesInContent finds all import references in content
func findIncludesInContent(content, baseDir string, verbose bool) ([]string, error) {
	_ = baseDir // unused parameter for now, keeping for potential future use
	_ = verbose // unused parameter for now, keeping for potential future use
	var includes []string

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		directive := parser.ParseImportDirective(line)
		if directive != nil {
			includePath := directive.Path

			// Handle section references (file.md#Section)
			var filePath string
			if strings.Contains(includePath, "#") {
				parts := strings.SplitN(includePath, "#", 2)
				filePath = parts[0]
			} else {
				filePath = includePath
			}

			includes = append(includes, filePath)
		}
	}

	return includes, scanner.Err()
}

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
)

var updateMergeLog = logger.New("cli:update_merge")

// hasLocalModifications checks if the local workflow file has been modified from its source
// It resolves the source field and imports on the remote content, then compares with local
// Note: stop-after field is ignored during comparison as it's a deployment-specific setting
func hasLocalModifications(sourceContent, localContent, sourceSpec string, verbose bool) bool {
	updateMergeLog.Printf("Checking for local modifications: source_spec=%s", sourceSpec)
	// Normalize both contents
	sourceNormalized := stringutil.NormalizeWhitespace(sourceContent)
	localNormalized := stringutil.NormalizeWhitespace(localContent)

	// Remove stop-after field from both contents for comparison
	// This field is deployment-specific and should not trigger "local modifications" warnings
	sourceNormalized, _ = RemoveFieldFromOnTrigger(sourceNormalized, "stop-after")
	localNormalized, _ = RemoveFieldFromOnTrigger(localNormalized, "stop-after")

	// Parse the source spec to get repo and ref information
	parsedSourceSpec, err := parseSourceSpec(sourceSpec)
	if err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Failed to parse source spec: %v", err)))
		}
		// Fall back to simple comparison
		return sourceNormalized != localNormalized
	}

	// Add the source field to the remote content
	sourceWithSource, err := UpdateFieldInFrontmatter(sourceNormalized, "source", sourceSpec)
	if err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Failed to add source field to remote content: %v", err)))
		}
		// Fall back to simple comparison
		return sourceNormalized != localNormalized
	}

	// Resolve imports on the remote content
	workflow := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: parsedSourceSpec.Repo,
			Version:  parsedSourceSpec.Ref,
		},
		WorkflowPath: parsedSourceSpec.Path,
	}

	sourceResolved, err := processIncludesInContent(sourceWithSource, workflow, parsedSourceSpec.Ref, verbose)
	if err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Failed to process imports on remote content: %v", err)))
		}
		// Use the version with source field but without resolved imports
		sourceResolved = sourceWithSource
	}

	// Normalize again after processing
	sourceResolvedNormalized := stringutil.NormalizeWhitespace(sourceResolved)

	// Compare the normalized contents
	hasModifications := sourceResolvedNormalized != localNormalized

	updateMergeLog.Printf("Local modifications detected: %v", hasModifications)

	if verbose && hasModifications {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Local modifications detected"))
	}

	return hasModifications
}

// MergeWorkflowContent performs a 3-way merge of workflow content using git merge-file
// It returns the merged content, whether conflicts exist, and any error
func MergeWorkflowContent(base, current, new, oldSourceSpec, newRef string, verbose bool) (string, bool, error) {
	updateMergeLog.Printf("Starting 3-way merge: old_ref=%s, new_ref=%s", oldSourceSpec, newRef)

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Performing 3-way merge using git merge-file"))
	}

	// Parse the old source spec to get the current ref
	sourceSpec, err := parseSourceSpec(oldSourceSpec)
	if err != nil {
		updateMergeLog.Printf("Failed to parse source spec: %v", err)
		return "", false, fmt.Errorf("failed to parse source spec: %w", err)
	}
	currentSourceSpec := fmt.Sprintf("%s/%s@%s", sourceSpec.Repo, sourceSpec.Path, sourceSpec.Ref)

	// Fix the base version by adding the source field to match what both current and new have
	// This prevents unnecessary conflicts over the source field
	baseWithSource, err := UpdateFieldInFrontmatter(base, "source", currentSourceSpec)
	if err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to add source to base content: %v", err)))
		}
		// Continue with original base content
		baseWithSource = base
	}

	// Update the source field in the new content with the new ref
	newWithUpdatedSource, err := UpdateFieldInFrontmatter(new, "source", fmt.Sprintf("%s/%s@%s", sourceSpec.Repo, sourceSpec.Path, newRef))
	if err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to update source in new content: %v", err)))
		}
		// Continue with original new content
		newWithUpdatedSource = new
	}

	// Normalize whitespace in all three versions to reduce spurious conflicts
	baseNormalized := stringutil.NormalizeWhitespace(baseWithSource)
	currentNormalized := stringutil.NormalizeWhitespace(current)
	newNormalized := stringutil.NormalizeWhitespace(newWithUpdatedSource)

	// Create temporary directory for merge files
	tmpDir, err := os.MkdirTemp("", "gh-aw-merge-*")
	if err != nil {
		return "", false, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Write base, current, and new versions to temporary files
	baseFile := filepath.Join(tmpDir, "base.md")
	currentFile := filepath.Join(tmpDir, "current.md")
	newFile := filepath.Join(tmpDir, "new.md")

	if err := os.WriteFile(baseFile, []byte(baseNormalized), 0644); err != nil {
		return "", false, fmt.Errorf("failed to write base file: %w", err)
	}
	if err := os.WriteFile(currentFile, []byte(currentNormalized), 0644); err != nil {
		return "", false, fmt.Errorf("failed to write current file: %w", err)
	}
	if err := os.WriteFile(newFile, []byte(newNormalized), 0644); err != nil {
		return "", false, fmt.Errorf("failed to write new file: %w", err)
	}

	// Execute git merge-file
	// Format: git merge-file <current> <base> <new>
	cmd := exec.Command("git", "merge-file",
		"-L", "current (local changes)",
		"-L", "base (original)",
		"-L", "new (upstream)",
		"--diff3", // Use diff3 style conflict markers for better context
		currentFile, baseFile, newFile)

	output, err := cmd.CombinedOutput()

	// git merge-file returns:
	// - 0 if merge was successful without conflicts
	// - >0 if conflicts were found (appears to return number of conflicts, but file is still updated)
	// The exit code can be >1 for multiple conflicts, not just errors
	hasConflicts := false
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode := exitErr.ExitCode()
			if exitCode > 0 && exitCode < 128 {
				// Conflicts found (exit codes 1-127 indicate conflicts)
				// Exit codes >= 128 typically indicate system errors
				hasConflicts = true
				updateMergeLog.Printf("Merge conflicts detected: exit_code=%d", exitCode)
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Merge conflicts detected (exit code: %d)", exitCode)))
				}
			} else {
				// Real error (exit code >= 128)
				updateMergeLog.Printf("Git merge-file failed: exit_code=%d", exitCode)
				return "", false, fmt.Errorf("git merge-file failed: %w\nOutput: %s", err, output)
			}
		} else {
			return "", false, fmt.Errorf("failed to execute git merge-file: %w", err)
		}
	}

	updateMergeLog.Printf("Merge completed: has_conflicts=%v", hasConflicts)

	// Read the merged content from the current file (git merge-file updates it in-place)
	mergedContent, err := os.ReadFile(currentFile)
	if err != nil {
		return "", false, fmt.Errorf("failed to read merged content: %w", err)
	}

	mergedStr := string(mergedContent)

	// Process @include directives if present and no conflicts
	// Skip include processing if there are conflicts to avoid errors
	if !hasConflicts {
		sourceSpec, err := parseSourceSpec(oldSourceSpec)
		if err == nil {
			workflow := &WorkflowSpec{
				RepoSpec: RepoSpec{
					RepoSlug: sourceSpec.Repo,
					Version:  newRef,
				},
				WorkflowPath: sourceSpec.Path,
			}

			processedContent, err := processIncludesInContent(mergedStr, workflow, newRef, verbose)
			if err != nil {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to process includes: %v", err)))
				}
				// Return unprocessed content on error
			} else {
				mergedStr = processedContent
			}
		}
	}

	return mergedStr, hasConflicts, nil
}

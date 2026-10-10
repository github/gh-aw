package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/github/gh-aw/pkg/constants"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
)

var updateMergeLog = logger.New("cli:update_merge")

// hasLocalModifications checks if the local workflow file has been modified from its source
// It resolves the source field and imports on the remote content, then compares with local
// Note: stop-after field is ignored during comparison as it's a deployment-specific setting
// localWorkflowDir, if non-empty, is passed to import processing so that relative import paths
// whose files exist locally are preserved — giving an accurate comparison against local content.
func hasLocalModifications(sourceContent, localContent, sourceSpec, localWorkflowDir string, verbose bool) bool {
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

	sourceResolved, err := processIncludesInContent(sourceWithSource, workflow, parsedSourceSpec.Ref, localWorkflowDir, verbose)
	if err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Failed to process imports on remote content: %v", err)))
		}
		// Use the version with source field but without resolved imports
		sourceResolved = sourceWithSource
	}

	// Normalize again after processing.
	// Remove the source field from both before comparing: it is managed by the update
	// tool (not user-editable content), and its position in the local file may differ
	// from where the tool would place it (at the end of frontmatter).  Retaining it
	// causes false-positive "local modifications" detections when the only difference
	// is source field position, which in turn triggers merge mode and can produce
	// spurious merge conflict markers.
	sourceResolvedNormalized := stringutil.NormalizeWhitespace(sourceResolved)
	if withoutSource, removeErr := RemoveTopLevelFieldFromFrontmatter(sourceResolvedNormalized, "source"); removeErr == nil {
		sourceResolvedNormalized = withoutSource
	}
	if withoutSource, removeErr := RemoveTopLevelFieldFromFrontmatter(localNormalized, "source"); removeErr == nil {
		localNormalized = withoutSource
	}

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
// localWorkflowPath is the filesystem path of the local workflow file being updated;
// when non-empty its directory is used to preserve relative import paths whose files
// exist locally rather than rewriting them to cross-repo references.
func MergeWorkflowContent(base, current, new, oldSourceSpec, newRefOrSourceSpec, localWorkflowPath string, verbose bool) (string, bool, error) {
	updateMergeLog.Printf("Starting 3-way merge: old_source=%s, new_ref_or_source=%s", oldSourceSpec, newRefOrSourceSpec)

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Performing 3-way merge using git merge-file"))
	}

	// Parse the old source spec to get the current ref
	sourceSpec, err := parseSourceSpec(oldSourceSpec)
	if err != nil {
		updateMergeLog.Printf("Failed to parse source spec: %v", err)
		return "", false, fmt.Errorf("failed to parse source spec: %w", err)
	}
	// Support both legacy ref-only and full source spec for the merge target.
	newSourceSpec := fmt.Sprintf("%s/%s@%s", sourceSpec.Repo, sourceSpec.Path, newRefOrSourceSpec)
	if tentativeSourceSpec, parseErr := parseSourceSpec(newRefOrSourceSpec); parseErr == nil {
		newSourceSpec = sourceSpecWithRef(tentativeSourceSpec, tentativeSourceSpec.Ref)
	}
	parsedNewSourceSpec, err := parseSourceSpec(newSourceSpec)
	if err != nil {
		return "", false, fmt.Errorf("failed to parse new source spec: %w", err)
	}
	newRef := parsedNewSourceSpec.Ref

	// The source field is managed by the updater, not user content. Exclude it from the
	// textual merge so changing only its ref cannot overlap local frontmatter additions.
	baseWithoutSource, err := RemoveTopLevelFieldFromFrontmatter(base, "source")
	if err != nil {
		return "", false, fmt.Errorf("failed to remove source from base content: %w", err)
	}
	currentWithoutSource, err := RemoveTopLevelFieldFromFrontmatter(current, "source")
	if err != nil {
		return "", false, fmt.Errorf("failed to remove source from current content: %w", err)
	}
	newWithoutSource, err := RemoveTopLevelFieldFromFrontmatter(new, "source")
	if err != nil {
		return "", false, fmt.Errorf("failed to remove source from new content: %w", err)
	}

	// Normalize whitespace in all three versions to reduce spurious conflicts.
	baseNormalized := stringutil.NormalizeWhitespace(baseWithoutSource)
	currentNormalized := stringutil.NormalizeWhitespace(currentWithoutSource)
	newNormalized := stringutil.NormalizeWhitespace(newWithoutSource)

	// If upstream content did not change, only advance the managed source ref.
	// Avoid invoking git merge-file or include processing, which would rewrite
	// locally customized frontmatter even though there is nothing to merge.
	if baseNormalized == newNormalized {
		updatedCurrent, updateErr := UpdateFieldInFrontmatter(current, "source", newSourceSpec)
		if updateErr != nil {
			return "", false, fmt.Errorf("failed to update source in current content: %w", updateErr)
		}
		return updatedCurrent, false, nil
	}

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

	if err := os.WriteFile(baseFile, []byte(baseNormalized), constants.FilePermPublic); err != nil {
		return "", false, fmt.Errorf("failed to write base file: %w", err)
	}
	if err := os.WriteFile(currentFile, []byte(currentNormalized), constants.FilePermPublic); err != nil {
		return "", false, fmt.Errorf("failed to write current file: %w", err)
	}
	if err := os.WriteFile(newFile, []byte(newNormalized), constants.FilePermPublic); err != nil {
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
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
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
		workflow := &WorkflowSpec{
			RepoSpec: RepoSpec{
				RepoSlug: parsedNewSourceSpec.Repo,
				Version:  newRef,
			},
			WorkflowPath: parsedNewSourceSpec.Path,
		}

		localWorkflowDir := ""
		if localWorkflowPath != "" {
			localWorkflowDir = filepath.Dir(localWorkflowPath)
		}
		processedContent, err := processIncludesInContent(mergedStr, workflow, newRef, localWorkflowDir, verbose)
		if err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to process includes: %v", err)))
			}
			// Return unprocessed content on error
		} else {
			mergedStr = processedContent
		}

	}

	if hasConflicts {
		mergedStr, err = updateTopLevelFieldInFrontmatterRaw(mergedStr, "source", newSourceSpec)
	} else {
		mergedStr, err = UpdateFieldInFrontmatter(mergedStr, "source", newSourceSpec)
	}
	if err != nil {
		return "", false, fmt.Errorf("failed to restore source in merged content: %w", err)
	}

	return mergedStr, hasConflicts, nil
}

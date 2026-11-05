package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
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
	if withoutSource, removeErr := removeSourceMetadata(sourceResolvedNormalized); removeErr == nil {
		sourceResolvedNormalized = withoutSource
	}
	if withoutSource, removeErr := removeSourceMetadata(localNormalized); removeErr == nil {
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

func removeSourceMetadata(content string) (string, error) {
	for _, field := range []string{"source", "source-sha"} {
		var err error
		content, err = RemoveTopLevelFieldFromFrontmatter(content, field)
		if err != nil {
			return "", err
		}
	}
	return content, nil
}

func normalizeImportsForMerge(content string, sourceSpec *SourceSpec, localWorkflowDir string, verbose bool) (string, error) {
	workflow := &WorkflowSpec{
		RepoSpec:     RepoSpec{RepoSlug: sourceSpec.Repo, Version: sourceSpec.Ref},
		WorkflowPath: sourceSpec.Path,
	}
	return processImportsWithWorkflowSpec(content, workflow, sourceSpec.Ref, localWorkflowDir, verbose)
}

// normalizeLegacyImportMergeBase recognizes frontmatter serialized by older updates,
// independently of local Markdown edits. Only the base is adjusted, so upstream
// comments and field order are still available to the textual merge.
func normalizeLegacyImportMergeBase(base, current string) string {
	parsed, err := parser.ExtractFrontmatterFromContent(base)
	if err != nil {
		return base
	}
	if _, hasImports := parsed.Frontmatter["imports"]; !hasImports {
		return base
	}
	local, err := parser.ExtractFrontmatterFromContent(current)
	if err != nil {
		return base
	}
	canonical, err := reconstructWorkflowFileFromMap(parsed.Frontmatter, parsed.Markdown)
	if err != nil {
		return base
	}
	comparable, err := parser.ReconstructWorkflowFile(strings.Join(local.FrontmatterLines, "\n"), parsed.Markdown)
	if err != nil || canonical != comparable {
		return base
	}
	return strings.Replace(base, strings.Join(parsed.FrontmatterLines, "\n"), strings.Join(local.FrontmatterLines, "\n"), 1)
}

// Preserve historical merge behavior for previously serialized installations.
// Matching base/current frontmatter needs no normalization, even when Markdown changed.
func normalizeLegacyImportMergeInputs(base, current, upstream string) (string, string, string, bool) {
	baseParsed, err := parser.ExtractFrontmatterFromContent(base)
	if err != nil {
		return base, current, upstream, false
	}
	currentParsed, err := parser.ExtractFrontmatterFromContent(current)
	if err != nil || strings.Join(baseParsed.FrontmatterLines, "\n") == strings.Join(currentParsed.FrontmatterLines, "\n") {
		return base, current, upstream, false
	}
	canonicalCurrent, err := canonicalImportMergeContent(current)
	canonicalCurrentParsed, canonicalErr := parser.ExtractFrontmatterFromContent(canonicalCurrent)
	if err != nil || canonicalErr != nil ||
		strings.Join(canonicalCurrentParsed.FrontmatterLines, "\n") != strings.Join(currentParsed.FrontmatterLines, "\n") {
		return base, current, upstream, false
	}
	canonicalBase, err := canonicalImportMergeContent(base)
	if err != nil {
		return base, current, upstream, false
	}
	canonicalUpstream, err := canonicalImportMergeContent(upstream)
	if err != nil {
		return base, current, upstream, false
	}
	return stringutil.NormalizeWhitespace(canonicalBase), current, stringutil.NormalizeWhitespace(canonicalUpstream), true
}

func canonicalImportMergeContent(content string) (string, error) {
	parsed, err := parser.ExtractFrontmatterFromContent(content)
	if err != nil {
		return "", err
	}
	if _, hasImports := parsed.Frontmatter["imports"]; !hasImports {
		return "", errors.New("no imports to normalize")
	}
	canonical, err := reconstructWorkflowFileFromMap(parsed.Frontmatter, parsed.Markdown)
	if err != nil {
		return "", err
	}
	return canonical, nil
}

func restoreUpstreamFrontmatterFormatting(merged, upstream string) (string, error) {
	mergedParsed, err := parser.ExtractFrontmatterFromContent(merged)
	if err != nil {
		return "", err
	}
	upstreamParsed, err := parser.ExtractFrontmatterFromContent(upstream)
	if err != nil {
		return "", err
	}

	formatted := upstream
	fields := make([]string, 0, len(upstreamParsed.Frontmatter)+len(mergedParsed.Frontmatter))
	for field := range upstreamParsed.Frontmatter {
		fields = append(fields, field)
	}
	for field := range mergedParsed.Frontmatter {
		if _, exists := upstreamParsed.Frontmatter[field]; !exists {
			fields = append(fields, field)
		}
	}
	slices.Sort(fields)
	for _, field := range fields {
		value, exists := mergedParsed.Frontmatter[field]
		if !exists {
			if _, upstreamHasField := upstreamParsed.Frontmatter[field]; !upstreamHasField {
				continue
			}
		} else if upstreamValue, upstreamHasField := upstreamParsed.Frontmatter[field]; upstreamHasField &&
			reflect.DeepEqual(value, upstreamValue) { //nolint:reflectdeepequalusage // YAML values are heterogeneous nested maps and slices.
			continue
		}
		formatted, err = replaceTopLevelFrontmatterField(formatted, field, value, exists)
		if err != nil {
			return "", err
		}
	}
	formattedParsed, err := parser.ExtractFrontmatterFromContent(formatted)
	if err != nil {
		return "", err
	}
	return parser.ReconstructWorkflowFile(strings.Join(formattedParsed.FrontmatterLines, "\n"), mergedParsed.Markdown)
}

func replaceTopLevelFrontmatterField(content, field string, value any, exists bool) (string, error) {
	parsed, err := parser.ExtractFrontmatterFromContent(content)
	if err != nil {
		return "", err
	}
	startLine, found := parsed.FieldLines[field]
	start := startLine - parsed.FrontmatterStart
	end := len(parsed.FrontmatterLines)
	for _, line := range parsed.FieldLines {
		if line > startLine && line-parsed.FrontmatterStart < end {
			end = line - parsed.FrontmatterStart
		}
	}
	if found && (start < 0 || start >= end || end > len(parsed.FrontmatterLines)) {
		return "", fmt.Errorf("invalid frontmatter field range for %q", field)
	}

	var replacement []string
	if exists {
		serialized, err := reconstructWorkflowFileFromMap(map[string]any{field: value}, "")
		if err != nil {
			return "", err
		}
		serializedParsed, err := parser.ExtractFrontmatterFromContent(serialized)
		if err != nil {
			return "", err
		}
		replacement = serializedParsed.FrontmatterLines
	}
	frontmatter := append([]string(nil), parsed.FrontmatterLines...)
	if !found {
		if !exists {
			return content, nil
		}
		frontmatter = append(frontmatter, replacement...)
	} else {
		for _, line := range slices.Backward(frontmatter[start+1 : end]) {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				break
			}
			end--
		}
		updated := make([]string, 0, len(frontmatter)-(end-start)+len(replacement))
		updated = append(updated, frontmatter[:start]...)
		updated = append(updated, replacement...)
		updated = append(updated, frontmatter[end:]...)
		frontmatter = updated
	}
	return parser.ReconstructWorkflowFile(strings.Join(frontmatter, "\n"), parsed.Markdown)
}

func normalizeImportFormattingForMerge(content string, parsed *parser.FrontmatterResult) (string, bool, bool) {
	imports, exists := parsed.Frontmatter["imports"]
	if !exists {
		return content, false, true
	}

	importLine, exists := parsed.FieldLines["imports"]
	if !exists {
		return content, false, false
	}
	start := importLine - parsed.FrontmatterStart
	end := len(parsed.FrontmatterLines)
	for _, line := range parsed.FieldLines {
		if line > importLine && line-parsed.FrontmatterStart < end {
			end = line - parsed.FrontmatterStart
		}
	}
	if start < 0 || start >= end || end > len(parsed.FrontmatterLines) {
		return content, false, false
	}
	for _, line := range parsed.FrontmatterLines[start:end] {
		if strings.Contains(line, "#") {
			return content, false, false
		}
	}

	normalized, err := replaceImportsFieldInContent(content, parsed, imports)
	if err != nil {
		return content, false, false
	}
	return normalized, normalized != content, true
}

func normalizeLegacyImportsForMerge(base, current, upstream string) (string, string, string, bool) {
	baseParsed, err := parser.ExtractFrontmatterFromContent(base)
	if err != nil {
		return base, current, upstream, false
	}
	currentParsed, err := parser.ExtractFrontmatterFromContent(current)
	if err != nil {
		return base, current, upstream, false
	}
	upstreamParsed, err := parser.ExtractFrontmatterFromContent(upstream)
	if err != nil {
		return base, current, upstream, false
	}
	baseImports, baseHasImports := baseParsed.Frontmatter["imports"]
	currentImports, currentHasImports := currentParsed.Frontmatter["imports"]
	if !baseHasImports || !currentHasImports {
		return base, current, upstream, false
	}
	baseImportsYAML, err := serializeImportsField(baseImports)
	if err != nil {
		return base, current, upstream, false
	}
	currentImportsYAML, err := serializeImportsField(currentImports)
	if err != nil || strings.Join(baseImportsYAML, "\n") != strings.Join(currentImportsYAML, "\n") {
		return base, current, upstream, false
	}

	normalizedBase, baseChanged, baseOK := normalizeImportFormattingForMerge(base, baseParsed)
	normalizedCurrent, currentChanged, currentOK := normalizeImportFormattingForMerge(current, currentParsed)
	normalizedUpstream, upstreamChanged, upstreamOK := normalizeImportFormattingForMerge(upstream, upstreamParsed)
	if !baseOK || !currentOK || !upstreamOK {
		return base, current, upstream, false
	}
	return normalizedBase, normalizedCurrent, normalizedUpstream,
		baseChanged || currentChanged || upstreamChanged
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
	baseWithoutSource, err := removeSourceMetadata(base)
	if err != nil {
		return "", false, fmt.Errorf("failed to remove source from base content: %w", err)
	}
	currentWithoutSource, err := removeSourceMetadata(current)
	if err != nil {
		return "", false, fmt.Errorf("failed to remove source from current content: %w", err)
	}
	newWithoutSource, err := removeSourceMetadata(new)
	if err != nil {
		return "", false, fmt.Errorf("failed to remove source from new content: %w", err)
	}

	// Resolve import paths consistently in all three inputs before merging,
	// while preserving their authored frontmatter formatting.
	mergeWorkflowDir := ""
	if localWorkflowPath != "" {
		mergeWorkflowDir = filepath.Dir(localWorkflowPath)
	}
	baseWithoutSource, err = normalizeImportsForMerge(baseWithoutSource, sourceSpec, mergeWorkflowDir, verbose)
	if err != nil {
		return "", false, fmt.Errorf("failed to normalize base imports: %w", err)
	}
	currentWithoutSource, err = normalizeImportsForMerge(currentWithoutSource, sourceSpec, mergeWorkflowDir, verbose)
	if err != nil {
		return "", false, fmt.Errorf("failed to normalize current imports: %w", err)
	}
	newWithoutSource, err = normalizeImportsForMerge(newWithoutSource, parsedNewSourceSpec, mergeWorkflowDir, verbose)
	if err != nil {
		return "", false, fmt.Errorf("failed to normalize upstream imports: %w", err)
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

	legacyUpstream := ""
	restoreLegacyFrontmatter := false
	if baseNormalized != currentNormalized {
		legacyBase := normalizeLegacyImportMergeBase(baseNormalized, currentNormalized)
		if legacyBase != baseNormalized {
			baseNormalized = legacyBase
		} else {
			legacyUpstream = newNormalized
			var normalized bool
			baseNormalized, currentNormalized, newNormalized, normalized = normalizeLegacyImportMergeInputs(baseNormalized, currentNormalized, newNormalized)
			if normalized {
				restoreLegacyFrontmatter = true
			}
		}
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

	runMergeFile := func() ([]byte, error) {
		cmd := exec.Command("git", "merge-file",
			"-L", "current (local changes)",
			"-L", "base (original)",
			"-L", "new (upstream)",
			"--diff3",
			currentFile, baseFile, newFile)
		return cmd.CombinedOutput()
	}
	classifyMergeFileResult := func(output []byte, err error) (bool, int, error) {
		if err == nil {
			return false, 0, nil
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode := exitErr.ExitCode()
			if exitCode > 0 && exitCode < 128 {
				return true, exitCode, nil
			}
			return false, exitCode, fmt.Errorf("git merge-file failed: %w\nOutput: %s", err, output)
		}
		return false, 0, fmt.Errorf("failed to execute git merge-file: %w", err)
	}

	output, err := runMergeFile()
	hasConflicts, conflictExitCode, err := classifyMergeFileResult(output, err)
	if err != nil {
		return "", false, err
	}
	if hasConflicts {
		// Earlier updates re-marshaled frontmatter when rewriting imports. Retry
		// with only the imports block normalized, preserving comments and other edits.
		normalizedBase, normalizedCurrent, normalizedUpstream, changed := normalizeLegacyImportsForMerge(
			baseNormalized, currentNormalized, newNormalized)
		if changed {
			if err := os.WriteFile(baseFile, []byte(normalizedBase), constants.FilePermPublic); err != nil {
				return "", false, fmt.Errorf("failed to write normalized base file: %w", err)
			}
			if err := os.WriteFile(currentFile, []byte(normalizedCurrent), constants.FilePermPublic); err != nil {
				return "", false, fmt.Errorf("failed to write normalized current file: %w", err)
			}
			if err := os.WriteFile(newFile, []byte(normalizedUpstream), constants.FilePermPublic); err != nil {
				return "", false, fmt.Errorf("failed to write normalized upstream file: %w", err)
			}
			output, err = runMergeFile()
			hasConflicts, conflictExitCode, err = classifyMergeFileResult(output, err)
			if err != nil {
				return "", false, err
			}
		}
		if hasConflicts {
			updateMergeLog.Printf("Merge conflicts detected: exit_code=%d", conflictExitCode)
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Merge conflicts detected (exit code: %d)", conflictExitCode)))
			}
		}
	}

	updateMergeLog.Printf("Merge completed: has_conflicts=%v", hasConflicts)

	// Read the merged content from the current file (git merge-file updates it in-place)
	mergedContent, err := os.ReadFile(currentFile)
	if err != nil {
		return "", false, fmt.Errorf("failed to read merged content: %w", err)
	}

	mergedStr := string(mergedContent)
	if restoreLegacyFrontmatter && !hasConflicts {
		mergedStr, err = restoreUpstreamFrontmatterFormatting(mergedStr, legacyUpstream)
		if err != nil {
			return "", false, fmt.Errorf("failed to restore upstream frontmatter formatting: %w", err)
		}
	}

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

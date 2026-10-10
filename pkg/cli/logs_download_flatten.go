// This file provides command-line interface functionality for gh-aw.
// This file (logs_download_flatten.go) contains functions for flattening
// downloaded artifact directories into the run's output directory, undoing
// the per-artifact directory nesting created by `gh run download`.

package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/fileutil"
)

// flattenSingleFileArtifacts checks artifact directories and flattens any that contain a single file
// This handles the case where gh CLI creates a directory for each artifact, even if it's just one file
func flattenSingleFileArtifacts(outputDir string, verbose bool) error {
	logsDownloadLog.Printf("Flattening single-file artifacts in: %s", outputDir)
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return fmt.Errorf("failed to read output directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != downloadedArtifactsMarkerDir {
			flattenSingleFileArtifact(outputDir, entry, verbose)
		}
	}
	return nil
}

func flattenSingleFileArtifact(outputDir string, entry os.DirEntry, verbose bool) {
	artifactDir := filepath.Join(outputDir, entry.Name())
	artifactEntries, err := os.ReadDir(artifactDir)
	if err != nil {
		logsDownloadLog.Printf("Failed to read artifact directory %s: %v", artifactDir, err)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to read artifact directory %s: %v", artifactDir, err)))
		}
		return
	}
	logsDownloadLog.Printf("Artifact directory %s contains %d entries", entry.Name(), len(artifactEntries))
	if len(artifactEntries) != 1 {
		if verbose && len(artifactEntries) > 1 {
			var fileNames []string
			for _, artifactEntry := range artifactEntries {
				fileNames = append(fileNames, artifactEntry.Name())
			}
			logsDownloadLog.Printf("Artifact directory %s has %d files, not flattening: %v", entry.Name(), len(artifactEntries), fileNames)
		}
		return
	}
	var singleEntry os.DirEntry
	for _, artifactEntry := range artifactEntries {
		singleEntry = artifactEntry
	}
	if singleEntry.IsDir() {
		logsDownloadLog.Printf("Artifact directory %s contains a subdirectory, not flattening", entry.Name())
		return
	}
	sourcePath := filepath.Join(artifactDir, singleEntry.Name())
	destPath := filepath.Join(outputDir, singleEntry.Name())
	logsDownloadLog.Printf("Flattening: %s → %s", sourcePath, destPath)
	if err := os.Rename(sourcePath, destPath); err != nil {
		logsDownloadLog.Printf("Failed to move file %s to %s: %v", sourcePath, destPath, err)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to move file %s to %s: %v", sourcePath, destPath, err)))
		}
		return
	}
	if err := os.Remove(artifactDir); err != nil {
		logsDownloadLog.Printf("Failed to remove empty directory %s: %v", artifactDir, err)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to remove empty directory %s: %v", artifactDir, err)))
		}
		return
	}
	logsDownloadLog.Printf("Successfully flattened: %s/%s → %s", entry.Name(), singleEntry.Name(), singleEntry.Name())
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Unfolded single-file artifact: %s → %s", filepath.Join(entry.Name(), singleEntry.Name()), singleEntry.Name())))
	}
}

// findArtifactDir looks for an artifact directory by its base name (suffix) in outputDir.
// It handles three cases:
//  1. Exact match: "agent" → outputDir/agent
//  2. Legacy name: for "agent", also checks "agent-artifacts"
//  3. Prefixed name (workflow_call): "*-agent" → outputDir/<hash>-agent
//
// Returns the first matching directory path, or empty string if none found.
func findArtifactDir(outputDir, baseName string, legacyName string) string {
	// First, try exact match
	exactPath := filepath.Join(outputDir, baseName)
	if fileutil.DirExists(exactPath) {
		return exactPath
	}

	// Try legacy name if provided
	if legacyName != "" {
		legacyPath := filepath.Join(outputDir, legacyName)
		if fileutil.DirExists(legacyPath) {
			return legacyPath
		}
	}

	// Scan for prefixed names (workflow_call context): any directory ending with "-{baseName}"
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return ""
	}
	suffix := "-" + baseName
	for _, entry := range entries {
		if entry.IsDir() && strings.HasSuffix(entry.Name(), suffix) {
			return filepath.Join(outputDir, entry.Name())
		}
	}

	return ""
}

// flattenArtifactTree moves all files from sourceDir into outputDir, preserving relative paths,
// then removes artifactDir (which may equal sourceDir, or be a parent of it in the old-structure
// case). label is used in log and user-facing messages.
// Cleanup failures are non-fatal: they are logged (and optionally printed) but do not return an error.
func flattenArtifactTree(sourceDir, artifactDir, outputDir, label string, verbose bool) error {
	walkErr := filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == sourceDir {
			return nil
		}
		return flattenArtifactPath(sourceDir, path, info, outputDir, label, verbose)
	})

	if walkErr != nil {
		return fmt.Errorf("failed to flatten %s: %w", label, walkErr)
	}

	// Remove the now-empty artifact directory structure.
	// Don't fail the entire operation if cleanup fails.
	if err := os.RemoveAll(artifactDir); err != nil {
		logsDownloadLog.Printf("Failed to remove %s directory %s: %v", label, artifactDir, err)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to remove %s directory: %v", label, err)))
		}
	} else {
		logsDownloadLog.Printf("Removed %s directory: %s", label, artifactDir)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Flattened %s and removed nested structure", label)))
		}
	}

	return nil
}

func flattenArtifactPath(sourceDir, path string, info os.FileInfo, outputDir, label string, verbose bool) error {
	relPath, err := filepath.Rel(sourceDir, path)
	if err != nil {
		return fmt.Errorf("failed to get relative path for %s: %w", path, err)
	}
	destPath := filepath.Join(outputDir, relPath)
	if info.IsDir() {
		if err := os.MkdirAll(destPath, constants.DirPermPublic); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", destPath, err)
		}
		logsDownloadLog.Printf("Created directory: %s", destPath)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(destPath), constants.DirPermPublic); err != nil {
		return fmt.Errorf("failed to create parent directory for %s: %w", destPath, err)
	}
	if fileutil.FileExists(destPath) {
		logsDownloadLog.Printf("Skipping duplicate flattened file %s from %s; destination already exists", relPath, label)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Skipped duplicate flattened file: "+relPath))
		}
		return nil
	}
	if err := os.Rename(path, destPath); err != nil {
		return fmt.Errorf("failed to move file %s to %s: %w", path, destPath, err)
	}
	logsDownloadLog.Printf("Moved file: %s → %s", path, destPath)
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Flattened: %s → %s", relPath, relPath)))
	}
	return nil
}

// flattenUnifiedArtifact flattens the unified agent artifact directory structure.
// The artifact is uploaded with all paths under /tmp/gh-aw/, so the action strips the
// common prefix and files land directly inside the artifact directory (new structure).
// For backward compatibility, it also handles the old structure where the full
// tmp/gh-aw/ path was preserved inside the artifact directory.
// New artifact name: "agent"   (preferred)
// Legacy artifact name: "agent-artifacts" (backward compat for older workflow runs)
// In workflow_call context, the artifact may be prefixed: "<hash>-agent"
func flattenUnifiedArtifact(outputDir string, verbose bool) error {
	agentArtifactsDir := findArtifactDir(outputDir, "agent", "agent-artifacts")
	if agentArtifactsDir == "" {
		// No unified artifact, nothing to flatten
		return nil
	}

	logsDownloadLog.Printf("Flattening unified agent artifact directory: %s", agentArtifactsDir)

	// Move the artifact outside its eventual output paths before flattening. New
	// artifacts contain an "agent/" subtree which would otherwise be written
	// back into agentArtifactsDir and deleted by flattenArtifactTree cleanup.
	stagingDir, err := os.MkdirTemp(outputDir, ".gh-aw-agent-artifact-*")
	if err != nil {
		return fmt.Errorf("failed to create agent artifact staging directory: %w", err)
	}
	if err := os.Remove(stagingDir); err != nil {
		return fmt.Errorf("failed to prepare agent artifact staging directory: %w", err)
	}
	if err := os.Rename(agentArtifactsDir, stagingDir); err != nil {
		return fmt.Errorf("failed to stage agent artifact directory: %w", err)
	}

	// Determine the source path: old structure preserves the tmp/gh-aw/ prefix inside the artifact.
	sourceDir := stagingDir
	tmpGhAwPath := filepath.Join(stagingDir, "tmp", "gh-aw")
	if fileutil.DirExists(tmpGhAwPath) {
		logsDownloadLog.Printf("Found old artifact structure with tmp/gh-aw prefix")
		sourceDir = tmpGhAwPath
	} else {
		logsDownloadLog.Printf("Found new artifact structure without tmp/gh-aw prefix")
	}

	if err := flattenArtifactTree(sourceDir, stagingDir, outputDir, "unified agent artifact", verbose); err != nil {
		return err
	}

	return nil
}

// flattenAgentOutputFallbackArtifact flattens the tiny fallback artifact that carries
// agent_output.json and safeoutputs.jsonl when the unified agent artifact upload fails.
func flattenAgentOutputFallbackArtifact(outputDir string, verbose bool) error {
	fallbackDir := findArtifactDir(outputDir, constants.AgentOutputFallbackArtifactName.String(), "")
	if fallbackDir == "" {
		return nil
	}

	logsDownloadLog.Printf("Flattening agent output fallback artifact directory: %s", fallbackDir)
	return flattenArtifactTree(fallbackDir, fallbackDir, outputDir, "agent output fallback artifact", verbose)
}

// flattenActivationArtifact flattens the activation artifact directory structure.
// The activation artifact contains aw_info.json and aw-prompts/prompt.txt.
// This function moves those files to the root output directory and removes the nested structure.
// In workflow_call context, the artifact may be prefixed: "<hash>-activation"
func flattenActivationArtifact(outputDir string, verbose bool) error {
	activationDir := findArtifactDir(outputDir, "activation", "")
	if activationDir == "" {
		// No activation artifact, nothing to flatten
		return nil
	}

	logsDownloadLog.Printf("Flattening activation artifact directory: %s", activationDir)

	return flattenArtifactTree(activationDir, activationDir, outputDir, "activation artifact", verbose)
}

// flattenAgentOutputsArtifact flattens the agent_outputs artifact directory structure.
// The agent_outputs artifact contains session logs with detailed token usage data
// that are critical for accurate token count parsing.
func flattenAgentOutputsArtifact(outputDir string, verbose bool) error {
	agentOutputsDir := filepath.Join(outputDir, "agent_outputs")

	// Check if agent_outputs directory exists
	if _, err := os.Stat(agentOutputsDir); os.IsNotExist(err) {
		// No agent_outputs artifact, nothing to flatten
		logsDownloadLog.Print("No agent_outputs artifact found (session logs may be missing)")
		return nil
	}

	logsDownloadLog.Printf("Flattening agent_outputs directory: %s", agentOutputsDir)

	return flattenArtifactTree(agentOutputsDir, agentOutputsDir, outputDir, "agent_outputs artifact", verbose)
}

// flattenSafeOutputsItemsArtifact flattens the safe-outputs-items artifact directory
// structure. The safe-outputs-items artifact contains safe-output-items.jsonl and
// temporary-id-map.json. After flattening, these files land at the run directory root
// where extractCreatedItemsFromManifest and loadResolvedTemporaryIDTargets expect them.
// The artifact may be prefixed in workflow_call context: "<hash>-safe-outputs-items".
func flattenSafeOutputsItemsArtifact(outputDir string, verbose bool) error {
	safeOutputsItemsDir := findArtifactDir(outputDir, constants.SafeOutputItemsArtifactName.String(), "")
	if safeOutputsItemsDir == "" {
		// No safe-outputs-items artifact, nothing to flatten
		return nil
	}

	logsDownloadLog.Printf("Flattening safe-outputs-items artifact directory: %s", safeOutputsItemsDir)

	return flattenArtifactTree(safeOutputsItemsDir, safeOutputsItemsDir, outputDir, "safe-outputs-items artifact", verbose)
}

// flattenDownloadedArtifacts normalizes the directory structure of all known artifact
// types after a successful download, moving files up out of per-artifact subdirectories
// so downstream audit/parsing code finds them at their expected paths.
func flattenDownloadedArtifacts(ctx context.Context, opts downloadArtifactsOptions) error {
	// Flatten single-file artifacts
	if err := flattenSingleFileArtifacts(opts.outputDir, opts.verbose); err != nil {
		return fmt.Errorf("failed to flatten artifacts: %w", err)
	}

	// Flatten activation artifact directory structure (contains aw_info.json and prompt.txt)
	if err := flattenActivationArtifact(opts.outputDir, opts.verbose); err != nil {
		return fmt.Errorf("failed to flatten activation artifact: %w", err)
	}

	ensureUsageAwInfoFallback(ctx, opts)

	// Flatten unified agent directory structure
	if err := flattenUnifiedArtifact(opts.outputDir, opts.verbose); err != nil {
		return fmt.Errorf("failed to flatten unified artifact: %w", err)
	}

	// Flatten the fallback after the unified artifact so primary files win when
	// both artifacts are present, while fallback files populate the run root when
	// the unified upload failed.
	if err := flattenAgentOutputFallbackArtifact(opts.outputDir, opts.verbose); err != nil {
		return fmt.Errorf("failed to flatten agent output fallback artifact: %w", err)
	}

	// Flatten agent_outputs artifact if present
	if err := flattenAgentOutputsArtifact(opts.outputDir, opts.verbose); err != nil {
		return fmt.Errorf("failed to flatten agent_outputs artifact: %w", err)
	}

	// Flatten safe-outputs-items artifact if present.
	// This artifact contains safe-output-items.jsonl and temporary-id-map.json.
	// Flattening moves them to the run root so extractCreatedItemsFromManifest
	// and loadResolvedTemporaryIDTargets can find them at their expected paths.
	if err := flattenSafeOutputsItemsArtifact(opts.outputDir, opts.verbose); err != nil {
		return fmt.Errorf("failed to flatten safe-outputs-items artifact: %w", err)
	}

	return nil
}

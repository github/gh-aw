package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/workflow"
)

// This file installs non-markdown resources referenced by packages.

func addNonWorkflowResourceWithTracking(resolved *ResolvedWorkflow, tracker *FileTracker, opts AddOptions, gitRoot, githubWorkflowsDir, workflowName string) (bool, error) {
	addLog.Printf("Dispatching non-workflow resource check: actionWorkflow=%t, skillFile=%t, agentFile=%t, resourceFile=%t",
		resolved.IsActionWorkflow, resolved.IsPackageSkillFile, resolved.IsPackageAgentFile, resolved.IsPackageResourceFile)
	// Action workflow files (.yml) are copied as-is to .github/workflows/ without any
	// frontmatter processing, dependency fetching, or compilation.
	if resolved.IsActionWorkflow {
		return true, addActionWorkflowWithTracking(resolved, tracker, opts, githubWorkflowsDir, workflowName)
	}
	// Package skill files are copied as-is to the agentic engine skill directory.
	if resolved.IsPackageSkillFile {
		return true, addSkillFileWithTracking(resolved, tracker, opts, gitRoot)
	}
	// Package agent files are copied as-is to the agentic engine agents directory.
	if resolved.IsPackageAgentFile {
		return true, addAgentFileWithTracking(resolved, tracker, opts, gitRoot)
	}
	// Package resources are copied as-is to their declared repository-relative destinations.
	if resolved.IsPackageResourceFile {
		if resolved.IsPackageProjectFile {
			return true, mergeProjectFileWithTracking(resolved, tracker, gitRoot)
		}
		return true, addResourceFileWithTracking(resolved, tracker, opts, gitRoot)
	}
	return false, nil
}

func addResourceFileWithTracking(resolved *ResolvedWorkflow, tracker *FileTracker, opts AddOptions, gitRoot string) error {
	destination := filepath.Clean(filepath.FromSlash(resolved.Spec.DestinationPath))
	if destination == "." || filepath.IsAbs(destination) || strings.HasPrefix(destination, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("resource destination %q is invalid", resolved.Spec.DestinationPath)
	}
	destFile := filepath.Join(gitRoot, destination)
	if err := fileutil.ValidatePathWithinBase(gitRoot, destFile); err != nil {
		return fmt.Errorf("failed to validate resource destination %q: %w", resolved.Spec.DestinationPath, err)
	}
	rel, err := filepath.Rel(gitRoot, destFile)
	if err != nil {
		return fmt.Errorf("failed to validate resource destination %q: %w", resolved.Spec.DestinationPath, err)
	}

	addLog.Printf("Adding resource file: dest=%s, content_size=%d bytes", destFile, len(resolved.Content))

	fileExists := fileutil.FileExists(destFile)
	if fileExists && !opts.Force {
		packageSource := packageSourceForSpec(resolved.Spec, resolved.SourceInfo)
		if owned, drifted := packageOwnershipAllowsOverwrite(gitRoot, rel, packageSource); !owned || drifted {
			if owned {
				return fmt.Errorf("resource %q has local modifications; use --force to overwrite", resolved.Spec.DestinationPath)
			}
			return fmt.Errorf("resource %q already exists; use --force to overwrite", resolved.Spec.DestinationPath)
		}
	}
	if err := os.MkdirAll(filepath.Dir(destFile), constants.DirPermPublic); err != nil {
		return fmt.Errorf("failed to create resource directory %s: %w", filepath.Dir(destFile), err)
	}
	if tracker != nil {
		if fileExists {
			tracker.TrackModified(destFile)
		} else {
			tracker.TrackCreated(destFile)
		}
	}
	if err := os.WriteFile(destFile, resolved.Content, constants.FilePermPublic); err != nil {
		return fmt.Errorf("failed to write resource file %q: %w", destFile, err)
	}
	if !opts.Quiet {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Added resource: "+filepath.ToSlash(rel)))
	}
	return nil
}

// addActionWorkflowWithTracking installs a raw GitHub Actions YAML workflow file (.yml)
// directly to the target directory without any frontmatter processing or compilation.
func addActionWorkflowWithTracking(resolved *ResolvedWorkflow, tracker *FileTracker, opts AddOptions, githubWorkflowsDir, workflowName string) error {
	destFile := filepath.Join(githubWorkflowsDir, workflowName+".yml")

	addLog.Printf("Adding action workflow: dest=%s, content_size=%d bytes", destFile, len(resolved.Content))

	if opts.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Adding action workflow: "+destFile))
	}

	fileExists := false
	if fileutil.FileExists(destFile) {
		fileExists = true
		if !opts.Force {
			if opts.FromWildcard {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Action workflow '%s' already exists. Skipping.", workflowName+".yml")))
				return nil
			}
			return fmt.Errorf("action workflow '%s' already exists in %s. Use --force to overwrite", workflowName+".yml", githubWorkflowsDir)
		}
		if !opts.showInteractiveProgress() {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Overwriting existing file: "+destFile))
		}
	}

	if tracker != nil {
		if fileExists {
			tracker.TrackModified(destFile)
		} else {
			tracker.TrackCreated(destFile)
		}
	}

	if err := os.WriteFile(destFile, resolved.Content, constants.FilePermPublic); err != nil {
		return fmt.Errorf("failed to write action workflow file '%s': %w", destFile, err)
	}

	if !opts.Quiet {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Added action workflow: "+filepath.Base(destFile)))
	}

	return nil
}

// addSkillFileWithTracking installs a single skill file from a package to the agentic engine
// skill directory. The file's path relative to the skill directory is preserved so that
// nested files (e.g. scripts/ subdirectories) are written with their full structure intact.
func addSkillFileWithTracking(resolved *ResolvedWorkflow, tracker *FileTracker, opts AddOptions, gitRoot string) error {
	engineSkillDir := workflow.GetEngineSkillDir(opts.EngineOverride)
	skillDir := filepath.Join(gitRoot, engineSkillDir, resolved.SkillName)
	relPath, err := resolveSkillRelativePath(resolved)
	if err != nil {
		return err
	}

	destFile := filepath.Join(skillDir, relPath)
	relToSkillDir, err := filepath.Rel(skillDir, destFile)
	if err != nil {
		return fmt.Errorf("failed to validate destination path %q for skill %q: %w", destFile, resolved.SkillName, err)
	}
	if relToSkillDir == ".." || strings.HasPrefix(relToSkillDir, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("skill file path %q escapes destination skill directory %q", relPath, skillDir)
	}

	// Ensure the destination directory exists (handles nested subdirectories).
	if err := os.MkdirAll(filepath.Dir(destFile), constants.DirPermPublic); err != nil {
		return fmt.Errorf("failed to create skill directory %s: %w", filepath.Dir(destFile), err)
	}

	addLog.Printf("Adding skill file: dest=%s, skill=%s, content_size=%d bytes", destFile, resolved.SkillName, len(resolved.Content))
	if opts.Verbose {
		skillDisplayDir := filepath.ToSlash(filepath.Join(engineSkillDir, resolved.SkillName))
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Adding skill file to %s: %s", skillDisplayDir, relPath)))
	}

	fileExists := false
	if fileutil.FileExists(destFile) {
		fileExists = true
		if !opts.Force {
			if opts.Verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Skill file '%s' already exists. Skipping.", destFile)))
			}
			return nil
		}
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Overwriting existing skill file: "+destFile))
	}
	if tracker != nil {
		if fileExists {
			tracker.TrackModified(destFile)
		} else {
			tracker.TrackCreated(destFile)
		}
	}
	if err := os.WriteFile(destFile, resolved.Content, constants.FilePermPublic); err != nil {
		return fmt.Errorf("failed to write skill file '%s': %w", destFile, err)
	}
	if !opts.Quiet {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Added skill file: %s/%s/%s", engineSkillDir, resolved.SkillName, relPath)))
	}
	return nil
}

func resolveSkillRelativePath(resolved *ResolvedWorkflow) (string, error) {
	// Determine the relative path under the skill directory so nested files preserve
	// structure (e.g. "scripts/query.sh"). Match a skill-name path component that is
	// immediately under skills/ or .github/skills/ to avoid accidental first matches.
	parts := strings.Split(filepath.ToSlash(resolved.Spec.WorkflowPath), "/")
	var relParts []string
	for i, part := range parts {
		if i >= len(parts)-1 {
			break
		}
		if part != resolved.SkillName {
			continue
		}
		if i > 0 && parts[i-1] == "skills" {
			relParts = parts[i+1:]
			break
		}
		if i > 1 && parts[i-1] == "skills" && parts[i-2] == ".github" {
			relParts = parts[i+1:]
			break
		}
	}
	if len(relParts) == 0 {
		addLog.Printf("Failed to determine relative skill path for %q from source %q", resolved.SkillName, resolved.Spec.WorkflowPath)
		return "", fmt.Errorf("failed to determine relative path for skill %q from source path %q", resolved.SkillName, resolved.Spec.WorkflowPath)
	}
	relPath := filepath.Clean(filepath.Join(relParts...))
	if relPath == "." || relPath == "" || relPath == string(os.PathSeparator) {
		return "", fmt.Errorf("relative skill path %q from source path %q is empty. Expected a file path under the skill directory. Example: scripts/query.sh", relPath, resolved.Spec.WorkflowPath)
	}
	return relPath, nil
}

// addAgentFileWithTracking installs a single agent file from a package to the agentic engine
// agents directory.
func addAgentFileWithTracking(resolved *ResolvedWorkflow, tracker *FileTracker, opts AddOptions, gitRoot string) error {
	engineAgentsDir := workflow.GetEngineSubAgentDir(opts.EngineOverride)
	agentsDir := filepath.Join(gitRoot, engineAgentsDir)
	if err := os.MkdirAll(agentsDir, constants.DirPermPublic); err != nil {
		return fmt.Errorf("failed to create agents directory %s: %w", agentsDir, err)
	}

	fileName := filepath.Base(resolved.Spec.WorkflowPath)
	destFile := filepath.Join(agentsDir, fileName)

	addLog.Printf("Adding agent file: dest=%s, content_size=%d bytes", destFile, len(resolved.Content))

	if opts.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Adding agent file to %s: %s", engineAgentsDir, fileName)))
	}

	fileExists := false
	if fileutil.FileExists(destFile) {
		fileExists = true
		if !opts.Force {
			if opts.Verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Agent file '%s' already exists. Skipping.", destFile)))
			}
			return nil
		}
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Overwriting existing agent file: "+destFile))
	}

	if tracker != nil {
		if fileExists {
			tracker.TrackModified(destFile)
		} else {
			tracker.TrackCreated(destFile)
		}
	}

	if err := os.WriteFile(destFile, resolved.Content, constants.FilePermPublic); err != nil {
		return fmt.Errorf("failed to write agent file '%s': %w", destFile, err)
	}

	if !opts.Quiet {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Added agent file: %s/%s", engineAgentsDir, fileName)))
	}

	return nil
}

// printCompilationError formats and writes a compilation error to stderr.
// Redirect-only workflow errors are treated as informational messages rather than errors,
// since they occur when a redirect placeholder was downloaded without resolving to the full
// workflow content. In that case the user is directed to run `gh aw update`.
// All other errors are written using FormatErrorChain for standard error formatting.
func printCompilationError(err error, quiet bool) {
	var redirectErr *workflow.RedirectOnlyWorkflowError
	if errors.As(err, &redirectErr) {
		if !quiet {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(redirectErr.Error()))
		}
		return
	}
	fmt.Fprintln(os.Stderr, console.FormatErrorChain(err))
}

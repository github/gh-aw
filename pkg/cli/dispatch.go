package cli

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/workflow"
)

// extractWorkflowNamesFromSafeOutputs extracts workflow names from a named key under
// safe-outputs in workflow frontmatter. It handles both array and map forms:
//
//	key: [name1, name2]
//	key: {workflows: [name1, name2]}
//
// Workflow names that contain GitHub Actions expression syntax (e.g. "${{") are skipped.
func extractWorkflowNamesFromSafeOutputs(content, safeOutputsKey string) []string {
	result, err := parser.ExtractFrontmatterFromContent(content)
	if err != nil || result.Frontmatter == nil {
		return nil
	}

	safeOutputsMap, ok := result.Frontmatter["safe-outputs"].(map[string]any)
	if !ok {
		return nil
	}

	workflowConfig, exists := safeOutputsMap[safeOutputsKey]
	if !exists {
		return nil
	}

	var workflowNames []string

	switch v := workflowConfig.(type) {
	case []any:
		for _, item := range v {
			if name, ok := item.(string); ok && !strings.Contains(name, "${{") {
				workflowNames = append(workflowNames, name)
			}
		}
	case map[string]any:
		if workflowsArray, ok := v["workflows"].([]any); ok {
			for _, item := range workflowsArray {
				if name, ok := item.(string); ok && !strings.Contains(name, "${{") {
					workflowNames = append(workflowNames, name)
				}
			}
		}
	}

	return workflowNames
}

// extractDispatchWorkflowNames extracts workflow names from the safe-outputs.dispatch-workflow
// frontmatter field. It handles both array and map forms of the configuration.
// Workflow names that contain GitHub Actions expression syntax (e.g. "${{") are skipped.
func extractDispatchWorkflowNames(content string) []string {
	return extractWorkflowNamesFromSafeOutputs(content, "dispatch-workflow")
}

// extractCallWorkflowNames extracts worker workflow names from the safe-outputs.call-workflow
// frontmatter field. It handles both array and map forms of the configuration.
// Workflow names that contain GitHub Actions expression syntax (e.g. "${{") are skipped.
func extractCallWorkflowNames(content string) []string {
	return extractWorkflowNamesFromSafeOutputs(content, "call-workflow")
}

// fileDownloadFn is the type for a function that downloads a file from a GitHub repository.
// It is used for dependency injection in fetchAndSaveRemoteDispatchWorkflows to allow tests
// to provide a fast-failing mock instead of making real network calls.
type fileDownloadFn func(ctx context.Context, owner, repo, path, ref string) ([]byte, error)

// fetchAndSaveRemoteDispatchWorkflows fetches and saves the workflow files referenced in the
// safe-outputs.dispatch-workflow configuration of a remote workflow. Each listed workflow name
// (without extension) is resolved as a sibling file ("<name>.md") in the same directory as
// the source workflow and downloaded from the same remote repository.
//
// Workflow names that use GitHub Actions expression syntax (e.g. "${{") are silently skipped
// because they are dynamic values that cannot be resolved at add-time.
//
// If a target file already exists from a different source (different owner/repo in its
// 'source:' frontmatter field, or no source field at all), an error is returned.
// Files from the same source are silently skipped. Download failures are non-fatal.
//
// An optional downloader function may be provided as the last argument to override the default
// parser.DownloadFileFromGitHub implementation (used in tests to avoid real network calls).
func fetchAndSaveRemoteDispatchWorkflows(ctx context.Context, content string, spec *WorkflowSpec, targetDir string, verbose bool, force bool, tracker *FileTracker, downloaders ...fileDownloadFn) error {
	config, ok := newRemoteDispatchWorkflowFetch(ctx, content, spec, targetDir, verbose, force, tracker, downloaders...)
	if !ok {
		return nil
	}
	for _, workflowName := range config.workflowNames {
		if err := config.fetch(workflowName); err != nil {
			return err
		}
	}
	return nil
}

type remoteDispatchWorkflowFetch struct {
	ctx                                          context.Context
	owner, repo, ref, workflowBaseDir, targetDir string
	absTargetDir                                 string
	spec                                         *WorkflowSpec
	verbose, force                               bool
	tracker                                      *FileTracker
	downloader                                   fileDownloadFn
	workflowNames                                []string
}

func newRemoteDispatchWorkflowFetch(ctx context.Context, content string, spec *WorkflowSpec, targetDir string, verbose bool, force bool, tracker *FileTracker, downloaders ...fileDownloadFn) (*remoteDispatchWorkflowFetch, bool) {
	remoteWorkflowLog.Printf("Fetching remote dispatch workflows: repo=%s, targetDir=%s, force=%v", spec.RepoSlug, targetDir, force)
	if spec.RepoSlug == "" {
		return nil, false
	}
	parts := strings.SplitN(spec.RepoSlug, "/", 2)
	if len(parts) != 2 {
		return nil, false
	}
	workflowNames := extractDispatchWorkflowNames(content)
	if len(workflowNames) == 0 {
		return nil, false
	}
	ref := resolveRemoteWorkflowRef(ctx, spec)
	absTargetDir, err := filepath.Abs(targetDir)
	if err != nil {
		remoteWorkflowLog.Printf("Failed to resolve absolute path for target directory %s: %v", targetDir, err)
		return nil, false
	}
	downloader := fileDownloadFn(parser.DownloadFileFromGitHub)
	if len(downloaders) > 0 && downloaders[0] != nil {
		downloader = downloaders[0]
	}
	remoteWorkflowLog.Printf("Found %d dispatch workflow(s) to fetch from %s@%s", len(workflowNames), spec.RepoSlug, ref)
	return &remoteDispatchWorkflowFetch{ctx: ctx, owner: parts[0], repo: parts[1], ref: ref, workflowBaseDir: getParentDir(spec.WorkflowPath), targetDir: targetDir, absTargetDir: absTargetDir, spec: spec, verbose: verbose, force: force, tracker: tracker, downloader: downloader, workflowNames: workflowNames}, true
}

func resolveRemoteWorkflowRef(ctx context.Context, spec *WorkflowSpec) string {
	if spec.Version != "" {
		return spec.Version
	}
	defaultBranch, err := getRepoDefaultBranch(ctx, spec.RepoSlug)
	if err != nil {
		remoteWorkflowLog.Printf("Failed to resolve default branch for %s, falling back to 'main': %v", spec.RepoSlug, err)
		spec.Version = "main"
		return spec.Version
	}
	spec.Version = defaultBranch
	return spec.Version
}

func (config *remoteDispatchWorkflowFetch) fetch(workflowName string) error {
	remoteFilePath, targetPath, ok := config.workflowPaths(workflowName)
	if !ok {
		return nil
	}
	fileExists, skip, err := config.checkExisting(workflowName, targetPath)
	if err != nil || skip {
		return err
	}
	workflowContent, err := config.downloader(config.ctx, config.owner, config.repo, remoteFilePath, config.ref)
	if err != nil {
		return config.saveYMLFallback(workflowName, remoteFilePath, err)
	}
	return config.saveMarkdown(workflowContent, remoteFilePath, targetPath, fileExists)
}

func (config *remoteDispatchWorkflowFetch) workflowPaths(workflowName string) (string, string, bool) {
	remoteFilePath := path.Join(config.workflowBaseDir, workflowName+".md")
	targetPath := filepath.Join(config.targetDir, filepath.Clean(workflowName+".md"))
	absTargetPath, err := filepath.Abs(targetPath)
	if err != nil {
		remoteWorkflowLog.Printf("Failed to resolve absolute path for dispatch workflow %s: %v", workflowName, err)
		return "", "", false
	}
	if rel, err := filepath.Rel(config.absTargetDir, absTargetPath); err != nil || strings.HasPrefix(rel, "..") {
		if config.verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Refusing to write dispatch workflow outside target directory: %q", workflowName)))
		}
		return "", "", false
	}
	return path.Clean(remoteFilePath), targetPath, true
}

func (config *remoteDispatchWorkflowFetch) checkExisting(workflowName, targetPath string) (bool, bool, error) {
	if _, err := os.Stat(targetPath); err != nil {
		return false, false, nil
	}
	if config.force {
		return true, false, nil
	}
	existingSourceRepo := readSourceRepoFromFile(targetPath)
	if existingSourceRepo == config.spec.RepoSlug {
		if config.verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Dispatch workflow from same source already exists, skipping: "+targetPath))
		}
		return true, true, nil
	}
	return true, false, fmt.Errorf(
		"dispatch workflow %q already exists at %s (existing source: %q, installing from: %q); remove the file or use --force to overwrite",
		workflowName, targetPath, sourceRepoLabel(existingSourceRepo), config.spec.RepoSlug,
	)
}

func (config *remoteDispatchWorkflowFetch) saveYMLFallback(workflowName, remoteFilePath string, mdErr error) error {
	remoteWorkflowLog.Printf(".md fetch failed for dispatch workflow %s, trying .yml fallback", workflowName)
	ymlRemotePath := path.Clean(strings.TrimSuffix(remoteFilePath, ".md") + ".yml")
	ymlLocalPath := filepath.Join(config.targetDir, filepath.Clean(workflowName+".yml"))
	ymlContent, err := config.downloader(config.ctx, config.owner, config.repo, ymlRemotePath, config.ref)
	if err != nil {
		if config.verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to fetch dispatch workflow %s: %v", remoteFilePath, mdErr)))
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(ymlLocalPath), constants.DirPermPublic); err != nil {
		return fmt.Errorf("failed to create directory for dispatch workflow %s: %w", ymlRemotePath, err)
	}
	_, err = os.Stat(ymlLocalPath)
	ymlFileExists := err == nil
	// Track before writing so rollback captures the original content.
	config.track(ymlLocalPath, ymlFileExists)
	if err := os.WriteFile(ymlLocalPath, ymlContent, constants.FilePermSensitive); err != nil {
		return fmt.Errorf("failed to write dispatch workflow %s: %w", ymlRemotePath, err)
	}
	if config.verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Fetched dispatch workflow (.yml): "+ymlLocalPath))
	}
	return nil
}

func (config *remoteDispatchWorkflowFetch) saveMarkdown(workflowContent []byte, remoteFilePath, targetPath string, fileExists bool) error {
	depSourceString := path.Join(config.spec.RepoSlug, remoteFilePath) + "@" + config.ref
	if updated, err := addSourceToWorkflow(string(workflowContent), depSourceString); err == nil {
		workflowContent = []byte(updated)
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), constants.DirPermPublic); err != nil {
		return fmt.Errorf("failed to create directory for dispatch workflow %s: %w", remoteFilePath, err)
	}
	// Track before writing so rollback captures the original content.
	config.track(targetPath, fileExists)
	if err := os.WriteFile(targetPath, workflowContent, constants.FilePermSensitive); err != nil {
		return fmt.Errorf("failed to write dispatch workflow %s: %w", remoteFilePath, err)
	}
	if config.verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Fetched dispatch workflow: "+targetPath))
	}
	fetchDownloadedWorkflowFrontmatterImports(config.ctx, workflowContent, config.spec, remoteFilePath, config.targetDir, config.verbose, config.force, config.tracker)
	return nil
}

func (config *remoteDispatchWorkflowFetch) track(filePath string, fileExists bool) {
	if config.tracker == nil {
		return
	}
	if fileExists {
		config.tracker.TrackModified(filePath)
		return
	}
	config.tracker.TrackCreated(filePath)
}

func fetchDownloadedWorkflowFrontmatterImports(ctx context.Context, workflowContent []byte, parentSpec *WorkflowSpec, remoteFilePath, targetDir string, verbose bool, force bool, tracker *FileTracker) {
	depSpec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: parentSpec.RepoSlug,
			Version:  parentSpec.Version,
		},
		WorkflowPath: remoteFilePath,
	}
	if err := fetchAndSaveRemoteFrontmatterImports(ctx, string(workflowContent), depSpec, targetDir, verbose, force, tracker); err != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to fetch frontmatter imports for %s: %v", remoteFilePath, err)))
	}
}

// fetchAndSaveDispatchWorkflowsFromParsedFile parses a locally-saved workflow file to obtain
// the fully merged safe-outputs configuration (including dispatch workflows that originate
// from imported shared workflows), then fetches any referenced dispatch workflow files that
// don't already exist locally.
//
// This is needed because import-derived dispatch workflows cannot be discovered by static
// frontmatter inspection alone — they only become visible after the compiler processes all
// imports and merges the safe-outputs configuration.
//
// All early returns (empty RepoSlug, invalid slug, parse failure, no dispatch workflows) are
// intentional no-ops: this function is best-effort and must never block the add workflow flow.
// Parse failures are logged at debug level so they can be investigated when needed.
// Source conflicts are reported as warnings (not errors) because the main file is already written.
func fetchAndSaveDispatchWorkflowsFromParsedFile(ctx context.Context, destFile string, spec *WorkflowSpec, targetDir string, verbose bool, force bool, tracker *FileTracker) {
	remoteWorkflowLog.Printf("Fetching import-derived dispatch workflows from parsed file: %s, repo=%s", destFile, spec.RepoSlug)
	if spec.RepoSlug == "" {
		return
	}

	parts := strings.SplitN(spec.RepoSlug, "/", 2)
	if len(parts) != 2 {
		return
	}
	owner, repo := parts[0], parts[1]
	ref := spec.Version
	if ref == "" {
		ref = "main"
	}

	// Parse the locally-saved workflow to get the full merged safe-outputs config.
	compiler := workflow.NewCompiler()
	data, err := compiler.ParseWorkflowFile(destFile)
	if err != nil {
		remoteWorkflowLog.Printf("Failed to parse workflow file %s for import-derived dispatch workflows: %v", destFile, err)
		return
	}
	if data == nil || data.SafeOutputs == nil || data.SafeOutputs.DispatchWorkflow == nil {
		return
	}

	workflowNames := data.SafeOutputs.DispatchWorkflow.Workflows
	if len(workflowNames) == 0 {
		return
	}

	// Filter out GitHub Actions expression syntax
	filtered := make([]string, 0, len(workflowNames))
	for _, name := range workflowNames {
		if !strings.Contains(name, "${{") {
			filtered = append(filtered, name)
		}
	}
	if len(filtered) == 0 {
		return
	}

	remoteWorkflowLog.Printf("Processing %d import-derived dispatch workflow(s) (filtered from %d)", len(filtered), len(workflowNames))

	workflowBaseDir := getParentDir(spec.WorkflowPath)

	absTargetDir, absErr := filepath.Abs(targetDir)
	if absErr != nil {
		remoteWorkflowLog.Printf("Failed to resolve absolute path for target directory %s: %v", targetDir, absErr)
		return
	}

	for _, workflowName := range filtered {
		// Early rejection of path traversal patterns (authoritative check is filepath.Rel below).
		if strings.Contains(workflowName, "..") {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Skipping dispatch workflow with unsafe name: %q", workflowName)))
			}
			continue
		}

		var remoteFilePath string
		if workflowBaseDir != "" {
			remoteFilePath = path.Join(workflowBaseDir, workflowName+".md")
		} else {
			remoteFilePath = workflowName + ".md"
		}
		remoteFilePath = path.Clean(remoteFilePath)

		localRelPath := filepath.Clean(workflowName + ".md")
		targetPath := filepath.Join(targetDir, localRelPath)

		absTargetPath, absErr2 := filepath.Abs(targetPath)
		if absErr2 != nil {
			remoteWorkflowLog.Printf("Failed to resolve absolute path for dispatch workflow %s: %v", workflowName, absErr2)
			continue
		}
		if rel, relErr := filepath.Rel(absTargetDir, absTargetPath); relErr != nil || strings.HasPrefix(rel, "..") {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Refusing to write dispatch workflow outside target directory: %q", workflowName)))
			}
			continue
		}

		// Check whether the target file already exists.
		fileExists := false
		if _, statErr := os.Stat(targetPath); statErr == nil {
			fileExists = true
			if !force {
				existingSourceRepo := readSourceRepoFromFile(targetPath)
				if existingSourceRepo == spec.RepoSlug {
					if verbose {
						fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Dispatch workflow (from import) from same source already exists, skipping: "+targetPath))
					}
					continue
				}
				// Different or missing source — warn and skip (post-write best-effort).
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf(
					"Dispatch workflow %q already exists at %s from a different source (existing: %q, needed: %q); use --force to overwrite",
					workflowName, targetPath, sourceRepoLabel(existingSourceRepo), spec.RepoSlug,
				)))
				continue
			}
		}

		// Download from source repository — try .md first, then .yml as fallback
		workflowContent, err := parser.DownloadFileFromGitHub(ctx, owner, repo, remoteFilePath, ref)
		if err != nil {
			// .md not found — try .yml fallback
			ymlRemotePath := path.Clean(strings.TrimSuffix(remoteFilePath, ".md") + ".yml")
			ymlLocalPath := filepath.Join(targetDir, filepath.Clean(workflowName+".yml"))

			ymlContent, ymlErr := parser.DownloadFileFromGitHub(ctx, owner, repo, ymlRemotePath, ref)
			if ymlErr != nil {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to fetch dispatch workflow %s: %v", remoteFilePath, err)))
				}
				continue
			}
			if mkErr := os.MkdirAll(filepath.Dir(ymlLocalPath), constants.DirPermPublic); mkErr != nil {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to create directory for dispatch workflow %s: %v", ymlRemotePath, mkErr)))
				}
				continue
			}
			// Capture whether file exists before writing (for correct tracker classification).
			_, ymlFileExistsErr := os.Stat(ymlLocalPath)
			ymlFileExists := ymlFileExistsErr == nil
			if writeErr := os.WriteFile(ymlLocalPath, ymlContent, constants.FilePermSensitive); writeErr != nil {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to write dispatch workflow %s: %v", ymlRemotePath, writeErr)))
				}
				continue
			}
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Fetched dispatch workflow (.yml, from import): "+ymlLocalPath))
			}
			if tracker != nil {
				if ymlFileExists {
					tracker.TrackModified(ymlLocalPath)
				} else {
					tracker.TrackCreated(ymlLocalPath)
				}
			}
			continue
		}

		// Embed the source field for future conflict detection.
		depSourceString := spec.RepoSlug + "/" + remoteFilePath + "@" + ref
		if updated, srcErr := addSourceToWorkflow(string(workflowContent), depSourceString); srcErr == nil {
			workflowContent = []byte(updated)
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), constants.DirPermPublic); err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to create directory for dispatch workflow %s: %v", remoteFilePath, err)))
			}
			continue
		}

		if err := os.WriteFile(targetPath, workflowContent, constants.FilePermSensitive); err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to write dispatch workflow %s: %v", remoteFilePath, err)))
			}
			continue
		}

		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Fetched dispatch workflow (from import): "+targetPath))
		}

		if tracker != nil {
			if fileExists {
				tracker.TrackModified(targetPath)
			} else {
				tracker.TrackCreated(targetPath)
			}
		}

		fetchDownloadedWorkflowFrontmatterImports(ctx, workflowContent, spec, remoteFilePath, targetDir, verbose, force, tracker)
	}
}

// fetchAndSaveRemoteCallWorkflows fetches and saves the worker workflow files referenced in the
// safe-outputs.call-workflow configuration of a remote workflow. Each listed workflow name
// (without extension) is resolved as a sibling file ("<name>.md") in the same directory as
// the source workflow and downloaded from the same remote repository.
//
// Workflow names that use GitHub Actions expression syntax (e.g. "${{") are silently skipped
// because they are dynamic values that cannot be resolved at add-time.
//
// If a target file already exists from a different source (different owner/repo in its
// 'source:' frontmatter field, or no source field at all), an error is returned.
// Files from the same source are silently skipped. Download failures are non-fatal.
//
// An optional downloader function may be provided as the last argument to override the default
// parser.DownloadFileFromGitHub implementation (used in tests to avoid real network calls).
func fetchAndSaveRemoteCallWorkflows(ctx context.Context, content string, spec *WorkflowSpec, targetDir string, verbose bool, force bool, tracker *FileTracker, downloaders ...fileDownloadFn) error {
	remoteWorkflowLog.Printf("Fetching remote call-workflow workers: repo=%s, targetDir=%s, force=%v", spec.RepoSlug, targetDir, force)
	downloader := fileDownloadFn(parser.DownloadFileFromGitHub)
	if len(downloaders) > 0 && downloaders[0] != nil {
		downloader = downloaders[0]
	}
	if spec.RepoSlug == "" {
		return nil
	}

	parts := strings.SplitN(spec.RepoSlug, "/", 2)
	if len(parts) != 2 {
		return nil
	}
	owner, repo := parts[0], parts[1]
	ref := spec.Version
	if ref == "" {
		defaultBranch, err := getRepoDefaultBranch(ctx, spec.RepoSlug)
		if err != nil {
			remoteWorkflowLog.Printf("Failed to resolve default branch for %s, falling back to 'main': %v", spec.RepoSlug, err)
			ref = "main"
		} else {
			ref = defaultBranch
		}
		spec.Version = ref
	}

	workflowNames := extractCallWorkflowNames(content)
	if len(workflowNames) == 0 {
		return nil
	}

	remoteWorkflowLog.Printf("Found %d call-workflow worker(s) to fetch from %s@%s", len(workflowNames), spec.RepoSlug, ref)

	workflowBaseDir := getParentDir(spec.WorkflowPath)

	absTargetDir, err := filepath.Abs(targetDir)
	if err != nil {
		remoteWorkflowLog.Printf("Failed to resolve absolute path for target directory %s: %v", targetDir, err)
		return nil
	}

	for _, workflowName := range workflowNames {
		var remoteFilePath string
		if workflowBaseDir != "" {
			remoteFilePath = path.Join(workflowBaseDir, workflowName+".md")
		} else {
			remoteFilePath = workflowName + ".md"
		}
		remoteFilePath = path.Clean(remoteFilePath)

		localRelPath := filepath.Clean(workflowName + ".md")
		targetPath := filepath.Join(targetDir, localRelPath)

		absTargetPath, absErr := filepath.Abs(targetPath)
		if absErr != nil {
			remoteWorkflowLog.Printf("Failed to resolve absolute path for call-workflow worker %s: %v", workflowName, absErr)
			continue
		}
		if rel, relErr := filepath.Rel(absTargetDir, absTargetPath); relErr != nil || strings.HasPrefix(rel, "..") {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Refusing to write call-workflow worker outside target directory: %q", workflowName)))
			}
			continue
		}

		// Build expected full source string now so it can be used in the conflict check below.
		expectedSource := spec.RepoSlug + "/" + remoteFilePath + "@" + ref

		fileExists := false
		if _, statErr := os.Stat(targetPath); statErr == nil {
			fileExists = true
			if !force {
				// Allow if the existing file was installed from the exact same source path.
				existingSource := readFullSourceFromFile(targetPath)
				if existingSource == expectedSource {
					if verbose {
						fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Call-workflow worker from same source already exists, skipping: "+targetPath))
					}
					continue
				}
				return fmt.Errorf(
					"call-workflow worker %q already exists at %s (existing source: %q, installing from: %q); remove the file or use --force to overwrite",
					workflowName, targetPath, sourceRepoLabel(existingSource), spec.RepoSlug,
				)
			}
		}

		// Download from the source repository — try .md first, then .yml as fallback
		// (the call-workflow validator accepts either .md or .yml files locally).
		workflowContent, err := downloader(ctx, owner, repo, remoteFilePath, ref)
		if err != nil {
			remoteWorkflowLog.Printf(".md fetch failed for call-workflow worker %s, trying .yml fallback", workflowName)
			// .md not found — try .yml fallback (e.g. plain GitHub Actions reusable workflow)
			ymlRemotePath := path.Clean(strings.TrimSuffix(remoteFilePath, ".md") + ".yml")
			ymlLocalPath := filepath.Join(targetDir, filepath.Clean(workflowName+".yml"))

			ymlContent, ymlErr := downloader(ctx, owner, repo, ymlRemotePath, ref)
			if ymlErr != nil {
				// Neither .md nor .yml found — best-effort, continue
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to fetch call-workflow worker %s: %v", remoteFilePath, err)))
				}
				continue
			}
			// .yml fallback succeeded — write it (no source field for yml)
			if mkErr := os.MkdirAll(filepath.Dir(ymlLocalPath), constants.DirPermPublic); mkErr != nil {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to create directory for call-workflow worker %s: %v", ymlRemotePath, mkErr)))
				}
				continue
			}
			_, ymlFileExistsErr := os.Stat(ymlLocalPath)
			ymlFileExists := ymlFileExistsErr == nil
			// Track before writing so rollback captures the original content.
			if tracker != nil {
				if ymlFileExists {
					tracker.TrackModified(ymlLocalPath)
				} else {
					tracker.TrackCreated(ymlLocalPath)
				}
			}
			if writeErr := os.WriteFile(ymlLocalPath, ymlContent, constants.FilePermSensitive); writeErr != nil {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to write call-workflow worker %s: %v", ymlRemotePath, writeErr)))
				}
				continue
			}
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Fetched call-workflow worker (.yml): "+ymlLocalPath))
			}
			continue
		}

		if updated, srcErr := addSourceToWorkflow(string(workflowContent), expectedSource); srcErr == nil {
			workflowContent = []byte(updated)
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), constants.DirPermPublic); err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to create directory for call-workflow worker %s: %v", remoteFilePath, err)))
			}
			continue
		}

		// Track before writing so rollback captures the original content.
		if tracker != nil {
			if fileExists {
				tracker.TrackModified(targetPath)
			} else {
				tracker.TrackCreated(targetPath)
			}
		}

		if err := os.WriteFile(targetPath, workflowContent, constants.FilePermSensitive); err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to write call-workflow worker %s: %v", remoteFilePath, err)))
			}
			continue
		}

		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Fetched call-workflow worker: "+targetPath))
		}

		fetchDownloadedWorkflowFrontmatterImports(ctx, workflowContent, spec, remoteFilePath, targetDir, verbose, force, tracker)
	}

	return nil
}

// fetchAndSaveCallWorkflowsFromParsedFile parses a locally-saved workflow file to obtain
// the fully merged safe-outputs configuration (including call-workflow workers that originate
// from imported shared workflows), then fetches any referenced worker files that
// don't already exist locally.
//
// This is needed because import-derived call-workflow workers cannot be discovered by static
// frontmatter inspection alone — they only become visible after the compiler processes all
// imports and merges the safe-outputs configuration.
//
// All early returns (empty RepoSlug, invalid slug, parse failure, no call workflows) are
// intentional no-ops: this function is best-effort and must never block the add workflow flow.
func fetchAndSaveCallWorkflowsFromParsedFile(ctx context.Context, destFile string, spec *WorkflowSpec, targetDir string, verbose bool, force bool, tracker *FileTracker) {
	remoteWorkflowLog.Printf("Fetching import-derived call-workflow workers from parsed file: %s, repo=%s", destFile, spec.RepoSlug)
	if spec.RepoSlug == "" {
		return
	}

	parts := strings.SplitN(spec.RepoSlug, "/", 2)
	if len(parts) != 2 {
		return
	}
	owner, repo := parts[0], parts[1]
	ref := spec.Version
	if ref == "" {
		ref = "main"
	}

	compiler := workflow.NewCompiler()
	data, err := compiler.ParseWorkflowFile(destFile)
	if err != nil {
		remoteWorkflowLog.Printf("Failed to parse workflow file %s for import-derived call-workflow workers: %v", destFile, err)
		return
	}
	if data == nil || data.SafeOutputs == nil || data.SafeOutputs.CallWorkflow == nil {
		return
	}

	workflowNames := data.SafeOutputs.CallWorkflow.Workflows
	if len(workflowNames) == 0 {
		return
	}

	filtered := make([]string, 0, len(workflowNames))
	for _, name := range workflowNames {
		if !strings.Contains(name, "${{") {
			filtered = append(filtered, name)
		}
	}
	if len(filtered) == 0 {
		return
	}

	remoteWorkflowLog.Printf("Processing %d import-derived call-workflow worker(s) (filtered from %d)", len(filtered), len(workflowNames))

	workflowBaseDir := getParentDir(spec.WorkflowPath)

	absTargetDir, absErr := filepath.Abs(targetDir)
	if absErr != nil {
		remoteWorkflowLog.Printf("Failed to resolve absolute path for target directory %s: %v", targetDir, absErr)
		return
	}

	for _, workflowName := range filtered {
		if strings.Contains(workflowName, "..") {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Skipping call-workflow worker with unsafe name: %q", workflowName)))
			}
			continue
		}

		var remoteFilePath string
		if workflowBaseDir != "" {
			remoteFilePath = path.Join(workflowBaseDir, workflowName+".md")
		} else {
			remoteFilePath = workflowName + ".md"
		}
		remoteFilePath = path.Clean(remoteFilePath)

		localRelPath := filepath.Clean(workflowName + ".md")
		targetPath := filepath.Join(targetDir, localRelPath)

		absTargetPath, absErr2 := filepath.Abs(targetPath)
		if absErr2 != nil {
			remoteWorkflowLog.Printf("Failed to resolve absolute path for call-workflow worker %s: %v", workflowName, absErr2)
			continue
		}
		if rel, relErr := filepath.Rel(absTargetDir, absTargetPath); relErr != nil || strings.HasPrefix(rel, "..") {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Refusing to write call-workflow worker outside target directory: %q", workflowName)))
			}
			continue
		}

		// Build expected full source string now so it can be used in the conflict check below.
		expectedSource := spec.RepoSlug + "/" + remoteFilePath + "@" + ref

		fileExists := false
		if _, statErr := os.Stat(targetPath); statErr == nil {
			fileExists = true
			if !force {
				// Allow if the existing file was installed from the exact same source path.
				existingSource := readFullSourceFromFile(targetPath)
				if existingSource == expectedSource {
					if verbose {
						fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Call-workflow worker (from import) from same source already exists, skipping: "+targetPath))
					}
					continue
				}
				// Different or missing source — warn and skip (post-write best-effort).
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf(
					"Call-workflow worker %q already exists at %s from a different source (existing: %q, needed: %q); use --force to overwrite",
					workflowName, targetPath, sourceRepoLabel(existingSource), spec.RepoSlug,
				)))
				continue
			}
		}

		// Download from source repository — try .md first, then .yml as fallback
		workflowContent, err := parser.DownloadFileFromGitHub(ctx, owner, repo, remoteFilePath, ref)
		if err != nil {
			// .md not found — try .yml fallback (e.g. plain GitHub Actions reusable workflow)
			ymlRemotePath := path.Clean(strings.TrimSuffix(remoteFilePath, ".md") + ".yml")
			ymlLocalPath := filepath.Join(targetDir, filepath.Clean(workflowName+".yml"))

			ymlContent, ymlErr := parser.DownloadFileFromGitHub(ctx, owner, repo, ymlRemotePath, ref)
			if ymlErr != nil {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to fetch call-workflow worker %s: %v", remoteFilePath, err)))
				}
				continue
			}
			if mkErr := os.MkdirAll(filepath.Dir(ymlLocalPath), constants.DirPermPublic); mkErr != nil {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to create directory for call-workflow worker %s: %v", ymlRemotePath, mkErr)))
				}
				continue
			}
			_, ymlFileExistsErr := os.Stat(ymlLocalPath)
			ymlFileExists := ymlFileExistsErr == nil
			// Track before writing so rollback captures the original content.
			if tracker != nil {
				if ymlFileExists {
					tracker.TrackModified(ymlLocalPath)
				} else {
					tracker.TrackCreated(ymlLocalPath)
				}
			}
			if writeErr := os.WriteFile(ymlLocalPath, ymlContent, constants.FilePermSensitive); writeErr != nil {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to write call-workflow worker %s: %v", ymlRemotePath, writeErr)))
				}
				continue
			}
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Fetched call-workflow worker (.yml, from import): "+ymlLocalPath))
			}
			continue
		}

		if updated, srcErr := addSourceToWorkflow(string(workflowContent), expectedSource); srcErr == nil {
			workflowContent = []byte(updated)
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), constants.DirPermPublic); err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to create directory for call-workflow worker %s: %v", remoteFilePath, err)))
			}
			continue
		}

		// Track before writing so rollback captures the original content.
		if tracker != nil {
			if fileExists {
				tracker.TrackModified(targetPath)
			} else {
				tracker.TrackCreated(targetPath)
			}
		}

		if err := os.WriteFile(targetPath, workflowContent, constants.FilePermSensitive); err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to write call-workflow worker %s: %v", remoteFilePath, err)))
			}
			continue
		}

		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Fetched call-workflow worker (from import): "+targetPath))
		}

		fetchDownloadedWorkflowFrontmatterImports(ctx, workflowContent, spec, remoteFilePath, targetDir, verbose, force, tracker)
	}
}

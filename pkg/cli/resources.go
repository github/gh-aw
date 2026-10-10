package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/gitutil"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/workflow"
)

type extractedResource struct {
	path              string
	isGraderEvaluator bool
}

type resourceDownloader func(ctx context.Context, owner, repo, filePath, ref string) ([]byte, error)

// extractResourceEntries extracts file paths from the top-level "resources" frontmatter
// field and validated grader evaluator paths.
// Returns an error if any entry contains GitHub Actions expression syntax (e.g. "${{"),
// since macros are not permitted in resource paths.
func extractResourceEntries(content string) ([]extractedResource, error) {
	result, err := parser.ExtractFrontmatterFromContent(content)
	if err != nil {
		remoteWorkflowLog.Printf("Failed to extract frontmatter for resources: %v", err)
		return nil, nil
	}
	if result.Frontmatter == nil {
		return nil, nil
	}

	var resources []extractedResource
	if resourcesField, exists := result.Frontmatter["resources"]; exists {
		switch v := resourcesField.(type) {
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok {
					resources = append(resources, extractedResource{path: s})
				}
			}
		case []string:
			for _, s := range v {
				resources = append(resources, extractedResource{path: s})
			}
		}
	}

	graders, err := workflow.ParseGradersFromFrontmatter(result.Frontmatter)
	if err != nil {
		return nil, err
	}
	if graders != nil {
		// Include evaluator paths even for disabled graders so package resources stay
		// complete and gh aw update can restore them if the grader is later re-enabled.
		for _, grader := range graders.Graders {
			if grader != nil && grader.Run != "" {
				resources = append(resources, extractedResource{path: grader.Run, isGraderEvaluator: true})
			}
		}
	}

	// Reject entries that contain GitHub Actions expression syntax — macros are not allowed.
	unique := make([]extractedResource, 0, len(resources))
	seen := make(map[string]int, len(resources))
	for _, resource := range resources {
		p := resource.path
		if strings.Contains(p, "${{") {
			return nil, fmt.Errorf("resources entry %q contains GitHub Actions expression syntax (${{) which is not allowed; use static paths only", p)
		}
		if existingIndex, exists := seen[p]; exists {
			unique[existingIndex].isGraderEvaluator = unique[existingIndex].isGraderEvaluator || resource.isGraderEvaluator
			continue
		}
		seen[p] = len(unique)
		unique = append(unique, resource)
	}

	return unique, nil
}

// fetchAndSaveRemoteResources fetches files listed in the top-level "resources" frontmatter
// field from the same remote repository and saves them locally. Resources are resolved as
// relative paths from the same directory as the source workflow in the remote repo.
//
// GitHub Actions expression syntax (e.g. "${{") is not allowed in resource paths and will
// cause an error. Download failures for individual files are non-fatal (best-effort).
//
// For Markdown resource files: if the target already exists from a different source repository
// (different 'source:' frontmatter field, or no source field), an error is returned. Files
// from the same source are silently skipped.
// For non-Markdown resource files: if the target already exists and force is false, an error
// is returned regardless of origin (non-markdown files have no source tracking).
func fetchAndSaveRemoteResources(ctx context.Context, content string, spec *WorkflowSpec, targetDir string, verbose bool, force bool, tracker *FileTracker) error {
	return fetchAndSaveRemoteResourcesWithDownloader(ctx, content, spec, targetDir, verbose, force, tracker, parser.DownloadFileFromGitHub)
}

func fetchAndSaveRemoteResourcesWithDownloader(ctx context.Context, content string, spec *WorkflowSpec, targetDir string, verbose bool, force bool, tracker *FileTracker, download resourceDownloader) error { //nolint:largefunc // Keep resource conflict, download, and tracking behavior together.
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

	resourcePaths, err := extractResourceEntries(content)
	if err != nil {
		return err
	}
	if len(resourcePaths) == 0 {
		return nil
	}

	// Resources are resolved relative to the source workflow's directory in the remote repo.
	workflowBaseDir := getParentDir(spec.WorkflowPath)
	var gitRoot string

	for _, resource := range resourcePaths {
		resourcePath := resource.path
		// Early rejection of path traversal patterns. This is a fast first-pass check;
		// the symlink-aware path validation below is the authoritative security control.
		if strings.Contains(resourcePath, "..") {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Skipping resource with unsafe path: %q", resourcePath)))
			}
			continue
		}

		// Resolve the remote file path. Explicitly local grader evaluators follow ordinary
		// resource behavior; workspace-relative grader evaluators install at their exact
		// repository-relative run path.
		var remoteFilePath string
		isRepoRootAnchoredGraderEvaluator := resource.isGraderEvaluator && !strings.HasPrefix(resourcePath, "./")
		if isRepoRootAnchoredGraderEvaluator {
			// Repository-root anchored evaluators are resolved from the repository root,
			// not the declaring package's directory: the ".github/workflows" convention
			// still maps onto the workflow's own directory (which may itself live under a
			// package), but any other repository-root path (e.g. ".github/graders/...")
			// must be fetched from the true repository root regardless of package nesting.
			remoteFilePath = resourcePath
			if strings.HasPrefix(remoteFilePath, constants.WorkflowsDirSlash) && workflowBaseDir != "" {
				remoteFilePath = path.Join(workflowBaseDir, strings.TrimPrefix(remoteFilePath, constants.WorkflowsDirSlash))
			}
		} else if rest, ok := strings.CutPrefix(resourcePath, "/"); ok {
			remoteFilePath = rest
		} else if workflowBaseDir != "" {
			remoteFilePath = path.Join(workflowBaseDir, strings.TrimPrefix(resourcePath, "./"))
		} else {
			remoteFilePath = strings.TrimPrefix(resourcePath, "./")
		}
		remoteFilePath = path.Clean(remoteFilePath)

		// Derive the local relative path by stripping the workflow base dir prefix
		localRelPath := remoteFilePath
		if workflowBaseDir != "" && strings.HasPrefix(remoteFilePath, workflowBaseDir+"/") {
			localRelPath = remoteFilePath[len(workflowBaseDir)+1:]
		}
		localRelPath = filepath.Clean(filepath.FromSlash(localRelPath))
		localRelPath = strings.TrimLeft(localRelPath, string(filepath.Separator))
		if localRelPath == "" || localRelPath == "." {
			continue
		}
		targetBaseDir := targetDir
		if isRepoRootAnchoredGraderEvaluator {
			if gitRoot == "" {
				gitRoot, err = gitutil.FindGitRootFrom(targetDir)
				if err != nil {
					return fmt.Errorf("failed to resolve repository root for grader resource %q: %w", resourcePath, err)
				}
			}
			targetBaseDir = gitRoot
			localRelPath = filepath.FromSlash(resourcePath)
		}
		targetPath := filepath.Join(targetBaseDir, localRelPath)

		if err := fileutil.ValidatePathWithinBase(targetBaseDir, targetPath); err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Refusing to write resource outside target directory: %q", resourcePath)))
			}
			continue
		}

		// Check whether the target file already exists.
		fileExists := false
		if fileutil.FileExists(targetPath) {
			fileExists = true
			// Shared evaluators may be referenced by multiple workflows in one package.
			// Their conflict handling is deferred until the source content can be compared.
			if !force && !resource.isGraderEvaluator {
				isMarkdown := strings.HasSuffix(strings.ToLower(targetPath), ".md")
				if isMarkdown {
					// For markdown files, allow same-source overwrites.
					existingSourceRepo := readSourceRepoFromFile(targetPath)
					if existingSourceRepo == spec.RepoSlug {
						if verbose {
							fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Resource file from same source already exists, skipping: "+targetPath))
						}
						continue
					}
					return fmt.Errorf(
						"resource %q already exists at %s (existing source: %q, installing from: %q); remove the file or use --force to overwrite",
						resourcePath, targetPath, sourceRepoLabel(existingSourceRepo), spec.RepoSlug,
					)
				}
				// Non-markdown files have no source tracking — always conflict.
				return fmt.Errorf(
					"resource %q already exists at %s; remove the file or use --force to overwrite",
					resourcePath, targetPath,
				)
			}
		}

		// Download from source repository
		fileContent, err := download(ctx, owner, repo, remoteFilePath, ref)
		if err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to fetch resource %s: %v", remoteFilePath, err)))
			}
			continue
		}
		if fileExists && !force && resource.isGraderEvaluator {
			existingContent, readErr := os.ReadFile(targetPath)
			if readErr != nil {
				return fmt.Errorf("failed to read existing grader resource %q: %w", targetPath, readErr)
			}
			if bytes.Equal(existingContent, fileContent) {
				continue
			}
			return fmt.Errorf("resource %q already exists at %s; remove the file or use --force to overwrite", resourcePath, targetPath)
		}

		// For markdown resources, embed the source field for future conflict detection.
		if strings.HasSuffix(strings.ToLower(remoteFilePath), ".md") {
			depSourceString := path.Join(spec.RepoSlug, remoteFilePath) + "@" + ref
			if updated, srcErr := addSourceToWorkflow(string(fileContent), depSourceString); srcErr == nil {
				fileContent = []byte(updated)
			}
		}

		// Create parent directory if needed
		if err := os.MkdirAll(filepath.Dir(targetPath), constants.DirPermPublic); err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to create directory for resource %s: %v", remoteFilePath, err)))
			}
			continue
		}

		// Write the file
		if err := os.WriteFile(targetPath, fileContent, constants.FilePermSensitive); err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to write resource %s: %v", remoteFilePath, err)))
			}
			continue
		}

		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Fetched resource: "+targetPath))
		}

		// Track the file
		if tracker != nil {
			if fileExists {
				tracker.TrackModified(targetPath)
			} else {
				tracker.TrackCreated(targetPath)
			}
		}
	}

	return nil
}

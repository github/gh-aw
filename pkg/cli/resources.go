package cli

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/fileutil"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/parser"
)

type extractedResource struct {
	path string
}

type resourceDownloader func(ctx context.Context, owner, repo, filePath, ref string) ([]byte, error)

// extractResourceEntries extracts file paths from the top-level "resources" frontmatter
// field.
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

	// Reject entries that contain GitHub Actions expression syntax — macros are not allowed.
	unique := make([]extractedResource, 0, len(resources))
	seen := make(map[string]int, len(resources))
	for _, resource := range resources {
		p := resource.path
		if strings.Contains(p, "${{") {
			return nil, fmt.Errorf("resources entry %q contains GitHub Actions expression syntax (${{) which is not allowed; use static paths only", p)
		}
		if _, exists := seen[p]; exists {
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
	owner, repo := parts[0], parts[1] //nolint:uncheckedsliceindex // len(parts) == 2 was checked above.
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

		var remoteFilePath string
		if rest, ok := strings.CutPrefix(resourcePath, "/"); ok {
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
		targetPath := filepath.Join(targetBaseDir, localRelPath)

		if err := fileutil.ValidatePathWithinBase(targetBaseDir, targetPath); err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Refusing to write resource outside target directory: %q", resourcePath)))
			}
			continue
		}

		// Check whether the target file already exists.
		fileExists := fileutil.FileExists(targetPath)
		if fileExists {
			if !force {
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

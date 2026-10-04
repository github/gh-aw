package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var helpersLog = logger.New("cli:helpers")

// getParentDir returns the directory part of a forward-slash workflow path.
// Workflow paths always use forward slashes (e.g. ".github/workflows/foo.md"),
// so path.Dir is correct here; filepath.Dir is not used to avoid OS-specific
// separator handling. Returns "" when the path has no directory component
// (i.e. path.Dir would return ".").
func getParentDir(p string) string {
	dir := path.Dir(p)
	if dir == "." {
		return ""
	}
	return dir
}

// getRepositoryRelativePath converts an absolute file path to a repository-relative path
// This ensures stable workflow identifiers regardless of where the repository is cloned
func getRepositoryRelativePath(absPath string) (string, error) {
	// Get the repository root for the specific file
	repoRoot, err := findGitRootForPath(absPath)
	if err != nil {
		// If we can't get the repo root, just use the basename as fallback
		helpersLog.Printf("Warning: could not get repository root for %s: %v, using basename", absPath, err)
		return filepath.Base(absPath), nil
	}

	// Convert both paths to absolute to ensure they can be compared
	absPath, err = filepath.Abs(absPath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Get the relative path from repo root
	relPath, err := filepath.Rel(repoRoot, absPath)
	if err != nil {
		return "", fmt.Errorf("failed to get relative path: %w", err)
	}

	// Normalize path separators to forward slashes for consistency across platforms
	// This ensures the same hash value on Windows, Linux, and macOS
	relPath = filepath.ToSlash(relPath)

	return relPath, nil
}

// getAbsoluteWorkflowDir converts a relative workflow dir to absolute path
func getAbsoluteWorkflowDir(workflowDir string, gitRoot string) string {
	absWorkflowDir := workflowDir
	if !filepath.IsAbs(absWorkflowDir) {
		absWorkflowDir = filepath.Join(gitRoot, workflowDir)
	}
	return absWorkflowDir
}

// readSourceRepoFromFile reads the 'source' frontmatter field from a local workflow file
// and returns the "owner/repo" portion (e.g. "github/gh-aw"). Returns "" if the file
// cannot be read, has no source field, or the field is not in the expected format.
func readSourceRepoFromFile(path string) string {
	helpersLog.Printf("Reading source repo from file: %s", path)
	content, err := os.ReadFile(path)
	if err != nil {
		helpersLog.Printf("Failed to read file: %s", err)
		return ""
	}
	result, err := parser.ExtractFrontmatterFromContent(string(content))
	if err != nil || result.Frontmatter == nil {
		return ""
	}
	sourceRaw, ok := result.Frontmatter["source"]
	if !ok {
		return ""
	}
	source, ok := sourceRaw.(string)
	if !ok || source == "" {
		return ""
	}
	// source format: "owner/repo/path/to/file.md@ref" — extract just "owner/repo"
	slashParts := strings.SplitN(source, "/", 3)
	if len(slashParts) < 2 {
		return ""
	}
	repo := strings.Join(slashParts[:2], "/")
	helpersLog.Printf("Extracted source repo: %s", repo)
	return repo
}

// readFullSourceFromFile reads the full 'source' frontmatter field from a local workflow file
// (e.g. "owner/repo/.github/workflows/worker.md@main"). Returns "" if the file cannot be
// read, has no source field, or the field value is not a string.
func readFullSourceFromFile(filePath string) string {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}
	result, err := parser.ExtractFrontmatterFromContent(string(content))
	if err != nil || result.Frontmatter == nil {
		return ""
	}
	sourceRaw, ok := result.Frontmatter["source"]
	if !ok {
		return ""
	}
	source, ok := sourceRaw.(string)
	if !ok {
		return ""
	}
	return source
}

// sourceRepoLabel returns the source repo string for display in error messages.
// When the repo string is empty (file has no source field or is not a markdown file),
// a human-readable placeholder is returned so the error message is not confusing.
func sourceRepoLabel(repo string) string {
	if repo == "" {
		return "(no source field)"
	}
	return repo
}

// marshalIndentJSONOrWrap marshals v as indented JSON, wrapping any error with a
// short description of what was being marshaled so failures are easier to debug.
// The label string should name the value, e.g. "outcomes report".
func marshalIndentJSONOrWrap(v any, label string) ([]byte, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal %s to JSON: %w", label, err)
	}
	return data, nil
}

package parser

import (
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

// isUnderWorkflowsDirectory checks whether a file path lives under the workflow
// directory. Shared workflows in .github/workflows/shared/... are still workflow
// files and must undergo the same validation rules as top-level workflows.
func isUnderWorkflowsDirectory(filePath string) bool {
	normalizedPath := filepath.ToSlash(filePath)
	return strings.Contains(normalizedPath, constants.WorkflowsDirSlash)
}

// isCustomAgentFile checks if a file path is a custom agent file under .github/agents/
// Custom agent files use GitHub Copilot's agent format, which differs from gh-aw workflow format.
// These files have a different schema for the 'tools' field (array vs object).
func isCustomAgentFile(filePath string) bool {
	normalizedPath := filepath.ToSlash(filePath)
	return strings.Contains(normalizedPath, constants.AgentsDir) && strings.HasSuffix(strings.ToLower(normalizedPath), ".md")
}

// isRepositoryImport checks if an import spec is a repository-only import (no file path)
// Format: owner/repo@ref or owner/repo (downloads entire .github folder, no agent extraction)
// Only common workflow-adjacent file extensions are rejected so dotted repository
// names such as "githubnext/gh-aw.dev" remain valid repository imports.
// Callers that also accept local imports should attempt local path resolution
// first so existing two-segment local paths win over this remote-import heuristic.
func isRepositoryImport(importPath string) bool {
	cleanPath := importPath
	if before, _, ok := strings.Cut(importPath, "#"); ok {
		cleanPath = before
	}
	pathWithoutRef := cleanPath
	if before, _, ok := strings.Cut(cleanPath, "@"); ok {
		pathWithoutRef = before
	}
	parts := strings.Split(pathWithoutRef, "/")
	if len(parts) != 2 {
		return false
	}
	if strings.HasPrefix(pathWithoutRef, ".") || strings.HasPrefix(pathWithoutRef, "/") {
		return false
	}
	if strings.HasPrefix(pathWithoutRef, "shared/") {
		return false
	}
	owner := parts[0]
	repo := parts[1]
	if owner == "" || repo == "" {
		return false
	}
	for _, ext := range []string{".md", ".yaml", ".yml", ".json"} {
		if strings.HasSuffix(strings.ToLower(repo), ext) {
			return false
		}
	}
	return true
}

// IsWorkflowSpec checks if a path looks like a workflowspec (owner/repo/path[@ref]).
func IsWorkflowSpec(path string) bool {
	cleanPath := path
	if before, _, ok := strings.Cut(path, "#"); ok {
		cleanPath = before
	}
	if before, _, ok := strings.Cut(cleanPath, "@"); ok {
		cleanPath = before
	}
	parts := strings.Split(cleanPath, "/")
	if len(parts) < 3 {
		return false
	}
	// Preserve legacy behavior expected by parser tests: URL-like paths are
	// currently treated as workflowspecs because downstream parsing supports
	// repository/path extraction from slash-delimited remote references.
	if strings.Contains(cleanPath, "://") {
		return true
	}
	if strings.HasPrefix(cleanPath, ".") {
		return false
	}
	if strings.HasPrefix(cleanPath, "shared/") {
		return false
	}
	if strings.HasPrefix(cleanPath, "/") {
		return false
	}
	owner := parts[0]
	repo := parts[1]
	if owner == "" || repo == "" {
		return false
	}
	return true
}

func findGitHubFolder(baseDir string) string {
	githubFolder := baseDir
	for !strings.HasSuffix(githubFolder, ".github") {
		parent := filepath.Dir(githubFolder)
		if parent == githubFolder || parent == "." || parent == "/" {
			githubFolder = baseDir
			break
		}
		githubFolder = parent
	}
	return githubFolder
}

func computeIncludeResolveAndSecurityBases(filePath, baseDir string) (string, string, string) {
	githubFolder := findGitHubFolder(baseDir)
	resolveBase := baseDir
	securityBase := githubFolder
	normalizedFilePath := filePath
	if strings.HasSuffix(githubFolder, ".github") {
		repoRoot := filepath.Dir(githubFolder)
		filePathSlash := filepath.ToSlash(filePath)
		if strings.HasPrefix(filePathSlash, constants.GithubDir) {
			resolveBase = repoRoot
		} else if stripped, ok := strings.CutPrefix(filePathSlash, "/"); ok {
			if !strings.HasPrefix(stripped, constants.GithubDir) && !strings.HasPrefix(stripped, ".agents/") {
				return "", "", filePath
			}
			normalizedFilePath = filepath.FromSlash(stripped)
			resolveBase = repoRoot
			if strings.HasPrefix(stripped, ".agents/") {
				securityBase = filepath.Join(repoRoot, ".agents")
			} else {
				securityBase = githubFolder
			}
		}
	}
	return resolveBase, securityBase, normalizedFilePath
}

package cli

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var specLog = logger.New("cli:spec")

// RepoSpec represents a parsed repository specification
type RepoSpec struct {
	RepoSlug string // e.g., "owner/repo"
	Version  string // optional version/tag/SHA/branch
}

// SourceSpec represents a parsed source specification from workflow frontmatter
type SourceSpec struct {
	Repo string // e.g., "owner/repo"
	Path string // e.g., "workflows/workflow-name.md"
	Ref  string // optional ref (version/tag/SHA/branch)
}

// WorkflowSpec represents a parsed workflow specification
type WorkflowSpec struct {
	RepoSpec            // embedded RepoSpec for Repo and Version fields
	WorkflowPath string // e.g., "workflows/workflow-name.md"
	WorkflowName string // e.g., "workflow-name"
	IsWildcard   bool   // true if this is a wildcard spec (e.g., "owner/repo/*")
}

// String returns the canonical string representation of the workflow spec
// in the format "owner/repo/path[@version]" or just the WorkflowPath for local specs
func (w *WorkflowSpec) String() string {
	// For local workflows (starting with "./"), return just the WorkflowPath
	if strings.HasPrefix(w.WorkflowPath, "./") {
		return w.WorkflowPath
	}

	// For remote workflows, use the standard format
	spec := w.RepoSlug + "/" + w.WorkflowPath
	if w.Version != "" {
		spec += "@" + w.Version
	}
	return spec
}

// isRepoOnlySpec checks if a specification is repo-only (owner/repo[@version]) without workflow path
func isRepoOnlySpec(spec string) bool {
	// URLs are not repo-only specs
	if strings.HasPrefix(spec, "http://") || strings.HasPrefix(spec, "https://") {
		return false
	}

	// Local paths are not repo-only specs
	if strings.HasPrefix(spec, "./") {
		return false
	}

	// Handle version first (anything after @)
	parts := strings.SplitN(spec, "@", 2)
	specWithoutVersion := parts[0]

	// Split by slashes
	slashParts := strings.Split(specWithoutVersion, "/")

	// Exactly 2 parts means repo-only: owner/repo
	return len(slashParts) == 2
}

// parseRepoSpec parses repository specification like "org/repo@version" or "org/repo@branch" or "org/repo@commit"
// Also supports GitHub URLs like "https://github.com/owner/repo[@version]"
func parseRepoSpec(repoSpec string) (*RepoSpec, error) {
	specLog.Printf("Parsing repo spec: %q", repoSpec)
	parts := strings.SplitN(repoSpec, "@", 2)
	repo := parts[0]
	var version string
	if len(parts) == 2 {
		version = parts[1]
		specLog.Printf("Version specified: %s", version)
	}

	// Check if this is a GitHub URL
	if strings.HasPrefix(repo, "https://github.com/") || strings.HasPrefix(repo, "http://github.com/") {
		specLog.Print("Detected GitHub URL format")
		// Parse GitHub URL: https://github.com/owner/repo
		repoURL, err := url.Parse(repo)
		if err != nil {
			specLog.Printf("Failed to parse GitHub URL: %v", err)
			return nil, fmt.Errorf("invalid GitHub URL: %w", err)
		}

		// Extract owner/repo from path
		pathParts := strings.Split(strings.Trim(repoURL.Path, "/"), "/")
		if len(pathParts) != 2 || pathParts[0] == "" || pathParts[1] == "" {
			specLog.Printf("Invalid GitHub URL path parts: %v", pathParts)
			return nil, fmt.Errorf("invalid GitHub URL: must be https://github.com/owner/repo. Example: https://github.com/github/gh-aw")
		}

		repo = fmt.Sprintf("%s/%s", pathParts[0], pathParts[1])
		specLog.Printf("Extracted repo from URL: %s", repo)
	} else if repo == "." {
		specLog.Print("Resolving current directory as repo")
		// Handle current directory as repo (local workflow)
		currentRepo, err := GetCurrentRepoSlug()
		if err != nil {
			specLog.Printf("Failed to get current repo: %v", err)
			return nil, fmt.Errorf("failed to get current repository info: %w", err)
		}
		repo = currentRepo
		specLog.Printf("Resolved current repo: %s", repo)
	} else {
		// Validate repository format (org/repo)
		repoParts := strings.Split(repo, "/")
		if len(repoParts) != 2 || repoParts[0] == "" || repoParts[1] == "" {
			return nil, fmt.Errorf("repository must be in format 'owner/repo'. Example: github/gh-aw")
		}
	}

	spec := &RepoSpec{
		RepoSlug: repo,
		Version:  version,
	}

	specLog.Printf("Parsed repo spec successfully: repo=%s, version=%s", repo, version)
	return spec, nil
}

// parseGitHubURL attempts to parse a GitHub URL and extract workflow specification components
// Supports URLs like:
//   - https://github.com/owner/repo/blob/branch/path/to/workflow.md
//   - https://github.com/owner/repo/blob/main/workflows/workflow.md
//   - https://github.com/owner/repo/tree/branch/path/to/workflow.md
//   - https://github.com/owner/repo/raw/branch/path/to/workflow.md
//   - https://raw.githubusercontent.com/owner/repo/refs/heads/branch/path/to/workflow.md
//   - https://raw.githubusercontent.com/owner/repo/COMMIT_SHA/path/to/workflow.md
//   - https://raw.githubusercontent.com/owner/repo/refs/tags/tag/path/to/workflow.md
func parseGitHubURL(spec string) (*WorkflowSpec, error) {
	specLog.Printf("Parsing GitHub URL: %s", spec)
	// First validate that this is a GitHub URL (github.com or raw.githubusercontent.com)
	parsedURL, err := url.Parse(spec)
	if err != nil {
		specLog.Printf("Failed to parse URL: %v", err)
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	// Must be a GitHub URL
	if parsedURL.Host != "github.com" && parsedURL.Host != "raw.githubusercontent.com" {
		specLog.Printf("Invalid host: %s", parsedURL.Host)
		return nil, fmt.Errorf("URL must be from github.com or raw.githubusercontent.com")
	}

	owner, repo, ref, filePath, err := parser.ParseRepoFileURL(spec)
	if err != nil {
		specLog.Printf("Failed to parse repo file URL: %v", err)
		return nil, err
	}

	specLog.Printf("Parsed GitHub URL: owner=%s, repo=%s, ref=%s, path=%s", owner, repo, ref, filePath)

	// Ensure the file path ends with .md
	if !strings.HasSuffix(filePath, ".md") {
		return nil, fmt.Errorf("GitHub URL must point to a .md file")
	}

	// Validate owner and repo
	if !parser.IsValidGitHubIdentifier(owner) || !parser.IsValidGitHubIdentifier(repo) {
		return nil, fmt.Errorf("invalid GitHub URL: '%s/%s' does not look like a valid GitHub repository", owner, repo)
	}

	return &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: fmt.Sprintf("%s/%s", owner, repo),
			Version:  ref,
		},
		WorkflowPath: filePath,
		WorkflowName: normalizeWorkflowID(filePath),
	}, nil
}

// parseWorkflowSpec parses a workflow specification in the new format
// Format: owner/repo/workflows/workflow-name[@version] or owner/repo/workflow-name[@version]
// Also supports full GitHub URLs like https://github.com/owner/repo/blob/branch/path/to/workflow.md
// Also supports local paths like ./workflows/workflow-name.md
func parseWorkflowSpec(spec string) (*WorkflowSpec, error) {
	specLog.Printf("Parsing workflow spec: %q", spec)

	// Check if this is a GitHub URL
	if strings.HasPrefix(spec, "http://") || strings.HasPrefix(spec, "https://") {
		specLog.Print("Detected GitHub URL format")
		return parseGitHubURL(spec)
	}

	// Check if this is a local path starting with "./"
	if strings.HasPrefix(spec, "./") {
		specLog.Print("Detected local path format")
		return parseLocalWorkflowSpec(spec)
	}

	// Handle version first (anything after @)
	parts := strings.SplitN(spec, "@", 2)
	specWithoutVersion := parts[0]
	var version string
	if len(parts) == 2 {
		version = parts[1]
	}

	// Split by slashes
	slashParts := strings.Split(specWithoutVersion, "/")

	// Must have at least 3 parts: owner/repo/workflow-path
	if len(slashParts) < 3 {
		return nil, fmt.Errorf("workflow specification must be in format 'owner/repo/workflow-name[@version]'")
	}

	owner := slashParts[0]
	repo := slashParts[1]

	// Check if this is a /files/REF/ format (e.g., owner/repo/files/main/path.md)
	// This is the format used when copying file paths from GitHub UI
	var workflowPath string
	if len(slashParts) >= 4 && slashParts[2] == "files" {
		// Extract the ref (branch/tag/commit) from slashParts[3]
		ref := slashParts[3]
		// The file path is everything after /files/REF/
		workflowPath = strings.Join(slashParts[4:], "/")

		// If version was not explicitly provided via @, use the ref from /files/REF/
		if version == "" {
			version = ref
		}
	} else {
		// Standard format: owner/repo/path or owner/repo/workflow-name
		workflowPath = strings.Join(slashParts[2:], "/")
	}

	// Validate owner and repo parts are not empty
	if owner == "" || repo == "" {
		return nil, fmt.Errorf("invalid workflow specification: owner and repo cannot be empty")
	}

	// Basic validation that owner and repo look like GitHub identifiers
	if !parser.IsValidGitHubIdentifier(owner) || !parser.IsValidGitHubIdentifier(repo) {
		return nil, fmt.Errorf("invalid workflow specification: '%s/%s' does not look like a valid GitHub repository", owner, repo)
	}

	// Check if this is a wildcard specification (owner/repo/*)
	if workflowPath == "*" {
		return &WorkflowSpec{
			RepoSpec: RepoSpec{
				RepoSlug: fmt.Sprintf("%s/%s", owner, repo),
				Version:  version,
			},
			WorkflowPath: "*",
			WorkflowName: "*",
			IsWildcard:   true,
		}, nil
	}

	// Handle different cases based on the number of path parts
	if len(slashParts) == 3 && !strings.HasSuffix(workflowPath, ".md") {
		// Three-part spec: owner/repo/workflow-name
		// Add "workflows/" prefix
		workflowPath = "workflows/" + workflowPath + ".md"
	} else {
		// Four or more parts: owner/repo/workflows/workflow-name or owner/repo/path/to/workflow-name
		// Require .md extension to be explicit
		if !strings.HasSuffix(workflowPath, ".md") {
			return nil, fmt.Errorf("workflow specification with path must end with '.md' extension: %s", workflowPath)
		}
	}

	return &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: fmt.Sprintf("%s/%s", owner, repo),
			Version:  version,
		},
		WorkflowPath: workflowPath,
		WorkflowName: strings.TrimSuffix(filepath.Base(workflowPath), ".md"),
	}, nil
}

// parseLocalWorkflowSpec parses a local workflow specification starting with "./"
func parseLocalWorkflowSpec(spec string) (*WorkflowSpec, error) {
	specLog.Printf("Parsing local workflow spec: %s", spec)
	// Validate that it's a .md file
	if !strings.HasSuffix(spec, ".md") {
		specLog.Printf("Invalid extension for local workflow: %s", spec)
		return nil, fmt.Errorf("local workflow specification must end with '.md' extension: %s", spec)
	}

	// Get current repository info
	repoInfo, err := GetCurrentRepoSlug()
	if err != nil {
		specLog.Printf("Failed to get current repo slug: %v", err)
		return nil, fmt.Errorf("failed to get current repository info for local workflow: %w", err)
	}

	specLog.Printf("Parsed local workflow: repo=%s, path=%s", repoInfo, spec)

	return &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: repoInfo,
			Version:  "", // Local workflows have no version
		},
		WorkflowPath: spec, // Keep the "./" prefix in WorkflowPath
		WorkflowName: strings.TrimSuffix(filepath.Base(spec), ".md"),
	}, nil
}

// parseSourceSpec parses a source specification like "owner/repo/path@ref"
// This is used for parsing the source field from workflow frontmatter
func parseSourceSpec(source string) (*SourceSpec, error) {
	specLog.Printf("Parsing source spec: %q", source)
	// Split on @ to separate ref
	parts := strings.SplitN(source, "@", 2)
	pathPart := parts[0]

	// Parse path: owner/repo/path/to/workflow.md
	slashParts := strings.Split(pathPart, "/")
	if len(slashParts) < 3 {
		return nil, fmt.Errorf("invalid source format: must be owner/repo/path[@ref]")
	}

	spec := &SourceSpec{
		Repo: fmt.Sprintf("%s/%s", slashParts[0], slashParts[1]),
		Path: strings.Join(slashParts[2:], "/"),
	}

	if len(parts) == 2 {
		spec.Ref = parts[1]
	}

	specLog.Printf("Parsed source spec: repo=%s, path=%s, ref=%s", spec.Repo, spec.Path, spec.Ref)
	return spec, nil
}

// buildSourceString builds the source string in the format owner/repo/path@ref
func buildSourceString(workflow *WorkflowSpec) string {
	if workflow.RepoSlug == "" || workflow.WorkflowPath == "" {
		return ""
	}

	// For local workflows, remove the "./" prefix from the WorkflowPath
	workflowPath := strings.TrimPrefix(workflow.WorkflowPath, "./")

	// Format: owner/repo/path@ref (consistent with add command syntax)
	source := workflow.RepoSlug + "/" + workflowPath
	if workflow.Version != "" {
		source += "@" + workflow.Version
	}

	return source
}

// buildSourceStringWithCommitSHA builds the source string with the actual commit SHA
// This is used when adding workflows to include the precise commit that was installed
func buildSourceStringWithCommitSHA(workflow *WorkflowSpec, commitSHA string) string {
	if workflow.RepoSlug == "" || workflow.WorkflowPath == "" {
		return ""
	}

	// For local workflows, remove the "./" prefix from the WorkflowPath
	workflowPath := strings.TrimPrefix(workflow.WorkflowPath, "./")

	// Format: owner/repo/path@commitSHA
	source := workflow.RepoSlug + "/" + workflowPath
	if commitSHA != "" {
		source += "@" + commitSHA
	} else if workflow.Version != "" {
		// Fallback to the version if no commit SHA is available
		source += "@" + workflow.Version
	}

	return source
}

// IsCommitSHA checks if a version string looks like a commit SHA (40-character hex string)
func IsCommitSHA(version string) bool {
	if len(version) != 40 {
		return false
	}
	// Check if all characters are hexadecimal
	for _, char := range version {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') && (char < 'A' || char > 'F') {
			return false
		}
	}
	return true
}

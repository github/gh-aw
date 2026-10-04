package cli

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var specLog = logger.New("cli:spec")

// RepoSpec represents a parsed repository specification
type RepoSpec struct {
	RepoSlug    string // e.g., "owner/repo"
	Version     string // optional version/tag/SHA/branch
	PackagePath string // optional repository package subpath, e.g. "packages/repo-assist"
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
	Host         string // explicit hostname from URL (e.g., "github.com", "myorg.ghe.com"); empty = use configured GH_HOST
	// FromRepositoryManifest is true when this workflow was selected from an aw.yml
	// repository package manifest (root or nested package path).
	FromRepositoryManifest bool
	// RawURL is set only for generic HTTP(S) URL specs whose host is not a recognized
	// GitHub host.  When non-empty, WorkflowPath, RepoSlug, Version, and Host are all
	// empty; the spec is resolved by fetching the URL and dispatching on Content-Type.
	RawURL string
	// IsPackageSkillFile is true when this spec refers to a file that belongs to a skill
	// directory from an aw.yml package manifest. The file is installed as-is to the
	// agentic engine skill folder rather than compiled as a workflow.
	IsPackageSkillFile bool
	// IsPackageAgentFile is true when this spec refers to an agent .md file from an aw.yml
	// package manifest. The file is installed as-is to the agentic engine agents folder.
	IsPackageAgentFile bool
	// IsPackageResourceFile is true when this spec refers to a declarative repository
	// resource from an aw.yml package manifest. The file is installed as-is to DestinationPath.
	IsPackageResourceFile bool
	// DestinationPath is the repository-root-relative install path resolved from a package
	// manifest entry (e.g. ".github/workflows/reviewer.md"). WorkflowName is derived from
	// it, so installation writes the file under the declared destination name.
	// Empty for specs that do not originate from a package manifest entry.
	DestinationPath string
	// SkillName is the name of the skill directory for package skill files.
	// Only meaningful when IsPackageSkillFile is true.
	SkillName string
}

// isLocalWorkflowPath checks if a path refers to a local filesystem workflow.
// Local paths include:
//   - Relative paths starting with "./", "../", ".\", or "..\", or equal to "." / ".."
//   - Absolute paths as determined by filepath.IsAbs (OS-specific)
//   - UNC-style paths starting with "\\" or "//" (Windows network paths)
func isLocalWorkflowPath(path string) bool {
	// Explicit relative path checks (POSIX and Windows-style)
	if path == "." || path == ".." {
		return true
	}
	if strings.HasPrefix(path, "./") || strings.HasPrefix(path, "../") ||
		strings.HasPrefix(path, ".\\") || strings.HasPrefix(path, "..\\") {
		return true
	}

	// OS-specific absolute paths (e.g., "/foo", "C:\foo", "D:/foo", UNC on Windows)
	if filepath.IsAbs(path) {
		return true
	}

	// UNC paths (e.g., "\\server\share\file.md" or "//server/share/file.md")
	if strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, "//") {
		return true
	}
	return false
}

// String returns the canonical string representation of the workflow spec
// in the format "owner/repo/path[@version]", just the WorkflowPath for local
// specs, or the raw URL for generic URL specs.
func (w *WorkflowSpec) String() string {
	// For generic URL specs, return the raw URL.
	if w.RawURL != "" {
		return w.RawURL
	}

	// For local workflows, return just the WorkflowPath
	if isLocalWorkflowPath(w.WorkflowPath) {
		return w.WorkflowPath
	}

	// For remote workflows, use the standard format
	spec := strings.Join([]string{w.RepoSlug, w.WorkflowPath}, "/")
	if w.Version != "" {
		spec += "@" + w.Version
	}
	return spec
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

	githubHost := getGitHubHost()
	githubHostPrefix := githubHost + "/"
	githubHostHTTPPrefix := "http://" + strings.TrimPrefix(githubHost, "https://") + "/"

	// Check if this is a GitHub URL
	if strings.HasPrefix(repo, githubHostPrefix) || strings.HasPrefix(repo, githubHostHTTPPrefix) {
		specLog.Print("Detected GitHub URL format")
		// Parse GitHub URL: https://github.com/owner/repo or https://enterprise.github.com/owner/repo
		repoURL, err := url.Parse(repo)
		if err != nil {
			specLog.Printf("Failed to parse GitHub URL: %v", err)
			return nil, fmt.Errorf("could not parse GitHub URL %q (use a URL like https://github.com/owner/repo): %w", repo, err)
		}

		// Extract owner/repo from path
		pathParts := strings.Split(strings.Trim(repoURL.Path, "/"), "/")
		if len(pathParts) != 2 || pathParts[0] == "" || pathParts[1] == "" {
			specLog.Printf("Invalid GitHub URL path parts: %v", pathParts)
			return nil, fmt.Errorf("invalid GitHub URL: must be %s/owner/repo. Example: %s/github/gh-aw", githubHost, githubHost)
		}

		repo = fmt.Sprintf("%s/%s", pathParts[0], pathParts[1])
		specLog.Printf("Extracted repo from URL: %s", repo)
	} else if repo == "." {
		specLog.Print("Resolving current directory as repo")
		// Handle current directory as repo (local workflow)
		currentRepo, err := GetCurrentRepoSlug()
		if err != nil {
			specLog.Printf("Failed to get current repo: %v", err)
			return nil, fmt.Errorf("failed to get current repository info (run this command from inside a git repository with a GitHub remote, or specify 'owner/repo' explicitly): %w", err)
		}
		repo = currentRepo
		specLog.Printf("Resolved current repo: %s", repo)
	} else {
		// Validate repository format (org/repo)
		repoParts := strings.Split(repo, "/")
		if len(repoParts) != 2 || repoParts[0] == "" || repoParts[1] == "" {
			return nil, errors.New("repository must be in format 'owner/repo'. Example: github/gh-aw")
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
//   - https://myorg.ghe.com/owner/repo/blob/branch/path/to/workflow.md (GHE)
func parseGitHubURL(spec string) (*WorkflowSpec, error) {
	specLog.Printf("Parsing GitHub URL: %s", spec)
	parsedURL, err := url.Parse(spec)
	if err != nil {
		specLog.Printf("Failed to parse URL: %v", err)
		return nil, fmt.Errorf("could not parse URL %q (use a URL like https://github.com/owner/repo/blob/main/workflows/workflow.md): %w", spec, err)
	}

	if parsedURL.Host == "" {
		return nil, fmt.Errorf("URL %q is missing a host. Use a full URL. Example: https://github.com/owner/repo/blob/main/workflows/workflow.md", spec)
	}

	if !isGitHubHost(parsedURL.Host) {
		return nil, fmt.Errorf("URL host %q is not supported. Expected github.com or a GitHub Enterprise host (*.ghe.com). Example: https://github.com/owner/repo/blob/main/workflows/workflow.md", parsedURL.Host)
	}

	owner, repo, ref, filePath, err := parser.ParseRepoFileURL(spec)
	if err != nil {
		specLog.Printf("Failed to parse repo file URL: %v", err)
		return nil, err
	}

	specLog.Printf("Parsed GitHub URL: owner=%s, repo=%s, ref=%s, path=%s, host=%s", owner, repo, ref, filePath, parsedURL.Host)

	// Ensure the file path ends with .md
	if !strings.HasSuffix(filePath, ".md") {
		return nil, errors.New("GitHub URL must point to a .md file. Example: https://github.com/owner/repo/blob/main/workflows/workflow.md")
	}

	// Validate owner and repo
	if !parser.IsValidGitHubIdentifier(owner) || !parser.IsValidGitHubRepositoryName(repo) {
		return nil, fmt.Errorf("GitHub URL contains '%s/%s', which does not look like a valid GitHub repository. Expected owner and repository names with only letters, numbers, hyphens, and underscores", owner, repo)
	}

	// For raw.githubusercontent.com content, the API host is github.com.
	// For all other hosts (github.com, GHE), use the URL's host as-is.
	host := parsedURL.Host
	if host == "raw.githubusercontent.com" {
		host = "github.com"
	}

	return &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: fmt.Sprintf("%s/%s", owner, repo),
			Version:  ref,
		},
		WorkflowPath: filePath,
		WorkflowName: normalizeWorkflowID(filePath),
		Host:         host,
	}, nil
}

// parseWorkflowSpec parses a workflow specification in the new format
// Format: owner/repo/workflows/workflow-name[@version] or owner/repo/workflow-name[@version]
// Also supports full GitHub URLs like https://github.com/owner/repo/blob/branch/path/to/workflow.md
// Also supports local paths like ./workflows/workflow-name.md

// isGitHubHost returns true if the given host is a recognized GitHub or GitHub Enterprise host:
// github.com, raw.githubusercontent.com, or any *.ghe.com host.
func isGitHubHost(host string) bool {
	return host == "github.com" ||
		host == "raw.githubusercontent.com" ||
		strings.HasSuffix(host, ".ghe.com") ||
		strings.HasSuffix(host, ".github.com")
}

// Returns the rewritten URL and true on a match, or ("", false) otherwise.
func rewriteAutomationsURL(u *url.URL) (string, bool) {
	// Only rewrite github.com (not GHE) automations UI URLs.
	if u.Host != "github.com" {
		return "", false
	}
	// Path must be: /{owner}/{repo}/agents/automations/{id}
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segments) != 5 || segments[2] != "agents" || segments[3] != "automations" {
		return "", false
	}
	owner, repo, id := segments[0], segments[1], segments[4]
	if owner == "" || repo == "" || id == "" {
		return "", false
	}
	capiURL := fmt.Sprintf("https://%s/agents/repos/%s/%s/automations/%s",
		constants.GitHubCopilotMCPDomain, owner, repo, id)
	return capiURL, true
}

func explicitHostForRepo(repoSlug string) string {
	if repoHost := getGitHubHostForRepo(repoSlug); repoHost != getGitHubHost() {
		if u, parseErr := url.Parse(repoHost); parseErr == nil && u.Host != "" {
			return u.Host
		}
	}
	return ""
}

func parseWorkflowSpec(spec string) (*WorkflowSpec, error) {
	specLog.Printf("Parsing workflow spec: %q", spec)

	// Check if this is a GitHub URL
	if strings.HasPrefix(spec, "http://") || strings.HasPrefix(spec, "https://") {
		specLog.Print("Detected URL format")
		// Try to parse as a recognized GitHub URL first.
		parsedURL, urlErr := url.Parse(spec)
		if urlErr == nil && isGitHubHost(parsedURL.Host) {
			specLog.Print("Detected GitHub URL format")
			// Rewrite dotcom automations UI URLs to the CAPI endpoint before further parsing.
			if capiURL, ok := rewriteAutomationsURL(parsedURL); ok {
				specLog.Printf("Rewrote automations UI URL to CAPI URL")
				return &WorkflowSpec{
					RawURL:       capiURL,
					WorkflowName: genericURLWorkflowName(capiURL),
				}, nil
			}
			return parseGitHubURL(spec)
		}
		// Non-GitHub HTTP(S) URL: return a generic URL spec whose content will be
		// fetched at resolution time and dispatched on Content-Type.
		if urlErr != nil {
			return nil, fmt.Errorf("could not parse URL %q (use a fully qualified http(s) URL): %w", spec, urlErr)
		}
		if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
			return nil, fmt.Errorf("URL scheme %q is not supported. Only http and https are supported. Example: https://example.com/workflow.md", parsedURL.Scheme)
		}
		specLog.Printf("Detected generic import URL: %s", spec)
		return &WorkflowSpec{
			RawURL:       spec,
			WorkflowName: genericURLWorkflowName(spec),
		}, nil
	}

	// Check if this is a local path
	if isLocalWorkflowPath(spec) {
		specLog.Print("Detected local path format")

		ws, err := parseLocalWorkflowSpec(spec)
		if err != nil {
			return nil, err
		}

		// Detect local wildcard specs like "./*.md" and mark them so that
		// downstream expansion (e.g., expandLocalWildcardWorkflows) can run.
		if strings.ContainsAny(spec, "*?[") {
			ws.IsWildcard = true
			// Ensure a stable WorkflowName for wildcard specs.
			if ws.WorkflowName == "" {
				ws.WorkflowName = spec
			}
		}

		return ws, nil
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
		return nil, errors.New("workflow specification format is not recognized. Expected 'owner/repo/workflow-name[@version]'. Example: github/gh-aw/ci-doctor")
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
		return nil, errors.New("workflow specification is missing owner or repo. Expected 'owner/repo/workflow-name[@version]'. Example: github/gh-aw/ci-doctor")
	}

	// Basic validation that owner and repo look like GitHub identifiers
	if !parser.IsValidGitHubIdentifier(owner) || !parser.IsValidGitHubRepositoryName(repo) {
		return nil, fmt.Errorf("workflow specification contains '%s/%s', which does not look like a valid GitHub repository. Expected owner and repository names with only letters, numbers, hyphens, and underscores", owner, repo)
	}

	repoSlug := fmt.Sprintf("%s/%s", owner, repo)

	// Determine the API host for this repo. getGitHubHostForRepo returns the canonical
	// host, which for well-known public-only repos (githubnext/agentics, github/gh-aw)
	// is always public GitHub regardless of GHE configuration. If the repo's canonical
	// host differs from the configured host, record the explicit hostname so API fetches
	// target the correct server.
	explicitHost := explicitHostForRepo(repoSlug)

	// Check if this is a wildcard specification (owner/repo/*)
	if workflowPath == "*" {
		return &WorkflowSpec{
			RepoSpec: RepoSpec{
				RepoSlug: repoSlug,
				Version:  version,
			},
			WorkflowPath: "*",
			WorkflowName: "*",
			IsWildcard:   true,
			Host:         explicitHost,
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
			return nil, fmt.Errorf("workflow specification path %q must end with '.md' extension. Example: owner/repo/workflows/ci-doctor.md", workflowPath)
		}
	}

	return &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: repoSlug,
			Version:  version,
		},
		WorkflowPath: workflowPath,
		WorkflowName: normalizeWorkflowID(workflowPath),
		Host:         explicitHost,
	}, nil
}

// parseLocalWorkflowSpec parses a local workflow specification starting with "./"
func parseLocalWorkflowSpec(spec string) (*WorkflowSpec, error) {
	specLog.Printf("Parsing local workflow spec: %s", spec)
	// Validate that it's a .md file
	if !strings.HasSuffix(spec, ".md") {
		specLog.Printf("Invalid extension for local workflow: %s", spec)
		return nil, fmt.Errorf("local workflow specification %q must end with '.md' extension. Example: ./workflows/ci-doctor.md", spec)
	}

	specLog.Printf("Parsed local workflow: path=%s", spec)

	return &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "", // Local workflows have no remote repo
			Version:  "", // Local workflows have no version
		},
		WorkflowPath: spec, // Keep the "./" prefix in WorkflowPath
		WorkflowName: normalizeWorkflowID(spec),
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
		return nil, errors.New("invalid source format: must be owner/repo/path[@ref]")
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

// buildSourceStringWithCommitSHA builds the source string with the actual commit SHA
// This is used when adding workflows to include the precise commit that was installed
func buildSourceStringWithCommitSHA(workflow *WorkflowSpec, commitSHA string) string {
	if workflow.RepoSlug == "" || workflow.WorkflowPath == "" {
		return ""
	}

	if workflow.FromRepositoryManifest {
		ref := workflow.Version
		if commitSHA != "" {
			ref = commitSHA
		}
		if ref == "" {
			return repositoryPackageIdentifier(workflow.RepoSlug, workflow.PackagePath)
		}
		return repositoryPackageIdentifier(workflow.RepoSlug, workflow.PackagePath) + "@" + ref
	}

	// For local workflows, remove the "./" prefix from the WorkflowPath
	workflowPath := strings.TrimPrefix(workflow.WorkflowPath, "./")

	// Format: owner/repo/path@commitSHA
	source := strings.Join([]string{workflow.RepoSlug, workflowPath}, "/")
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

// genericURLWorkflowName derives a best-effort workflow name from a raw import URL.
// It uses the last non-empty path segment, stripping any well-known file extensions.
// If no useful name can be derived the constant "imported-workflow" is returned.
func genericURLWorkflowName(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "imported-workflow"
	}

	// Walk path segments from the end looking for a non-empty one.
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for i := range slices.Backward(segments) {
		seg := segments[i]
		if seg == "" {
			continue
		}
		// Strip known extensions: .md, .json, .yaml, .yml
		for _, ext := range []string{".md", ".json", ".yaml", ".yml"} {
			if s, ok := strings.CutSuffix(seg, ext); ok {
				seg = s
				break
			}
		}
		if seg == "" {
			continue
		}
		// URL-decode percent-encoded characters (e.g. %20 → space) before
		// applying kebab-casing so that names like "My%20Workflow" become
		// "my-workflow" rather than "my-20workflow".
		if decoded, err := url.PathUnescape(seg); err == nil {
			seg = decoded
		}
		return toKebabCase(seg)
	}
	return "imported-workflow"
}

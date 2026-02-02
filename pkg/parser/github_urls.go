package parser

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var urlLog = logger.New("parser:github_urls")

// GitHubURLType represents the type of GitHub URL
type GitHubURLType string

const (
	URLTypeRun         GitHubURLType = "run"        // GitHub Actions run
	URLTypePullRequest GitHubURLType = "pull"       // Pull request
	URLTypeIssue       GitHubURLType = "issue"      // Issue
	URLTypeBlob        GitHubURLType = "blob"       // File blob view
	URLTypeTree        GitHubURLType = "tree"       // Directory tree view
	URLTypeRaw         GitHubURLType = "raw"        // Raw file view
	URLTypeRawContent  GitHubURLType = "rawcontent" // raw.githubusercontent.com
	URLTypeUnknown     GitHubURLType = "unknown"    // Unknown type
)

// GitHubURLComponents represents the parsed components of a GitHub URL
type GitHubURLComponents struct {
	Host       string        // Hostname (e.g., "github.com", "github.example.com", "raw.githubusercontent.com")
	Owner      string        // Repository owner
	Repo       string        // Repository name
	Type       GitHubURLType // Type of URL (run, pull, issue, blob, tree, raw, rawcontent)
	Number     int64         // Number for runs, PRs, issues, jobs
	Path       string        // File path for blob/tree/raw URLs
	Ref        string        // Git reference (branch, tag, SHA) for file URLs
	JobID      int64         // Job ID for job URLs (e.g., /job/123)
	StepNumber int           // Step number from URL fragment (e.g., #step:7:1)
	StepLine   int           // Line number within step from URL fragment
}

// ParseGitHubURL parses a GitHub URL and extracts its components.
// Supports various URL formats:
//   - GitHub Actions runs: https://github.com/owner/repo/actions/runs/12345678
//   - GitHub Actions runs (short): https://github.com/owner/repo/runs/12345678
//   - GitHub Actions job URLs: https://github.com/owner/repo/actions/runs/12345678/job/98765432
//   - GitHub Actions step URLs: https://github.com/owner/repo/actions/runs/12345678/job/98765432#step:7:1
//   - Pull requests: https://github.com/owner/repo/pull/123
//   - Issues: https://github.com/owner/repo/issues/123
//   - File blob: https://github.com/owner/repo/blob/main/path/to/file.md
//   - File tree: https://github.com/owner/repo/tree/main/path/to/dir
//   - File raw: https://github.com/owner/repo/raw/main/path/to/file.md
//   - Raw content: https://raw.githubusercontent.com/owner/repo/main/path/to/file.md
//   - Enterprise URLs: https://github.example.com/owner/repo/...
func ParseGitHubURL(urlStr string) (*GitHubURLComponents, error) {
	urlLog.Printf("Parsing GitHub URL: %s", urlStr)
	// Parse the URL
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		urlLog.Printf("Failed to parse URL: %v", err)
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	// Check if it's a GitHub-like host
	host := parsedURL.Host
	if host == "" {
		return nil, fmt.Errorf("URL must include a host")
	}

	urlLog.Printf("Detected host: %s", host)

	// Handle raw.githubusercontent.com specially
	if host == "raw.githubusercontent.com" {
		urlLog.Print("Detected raw.githubusercontent.com URL")
		return parseRawGitHubContentURL(parsedURL)
	}

	// Parse the path
	pathParts := strings.Split(strings.Trim(parsedURL.Path, "/"), "/")

	// Need at least owner and repo
	if len(pathParts) < 2 {
		return nil, fmt.Errorf("invalid GitHub URL format: path too short")
	}

	owner := pathParts[0]
	repo := pathParts[1]

	// Validate owner and repo are not empty
	if owner == "" || repo == "" {
		return nil, fmt.Errorf("invalid GitHub URL: owner and repo cannot be empty")
	}

	// Determine the type based on path structure
	if len(pathParts) >= 4 {
		urlType := pathParts[2]
		urlLog.Printf("Detected URL type segment: %s for %s/%s", urlType, owner, repo)

		switch urlType {
		case "actions":
			// Pattern: /owner/repo/actions/runs/12345678
			if len(pathParts) >= 5 && pathParts[3] == "runs" {
				urlLog.Print("Parsing GitHub Actions run URL")
				components, err := parseRunURL(host, owner, repo, pathParts[4:])
				if err != nil {
					return nil, err
				}
				// Parse fragment for step information
				parseStepFragment(parsedURL.Fragment, components)
				return components, nil
			}

		case "runs":
			// Pattern: /owner/repo/runs/12345678 (short form)
			if len(pathParts) >= 4 {
				urlLog.Print("Parsing GitHub Actions run URL (short form)")
				components, err := parseRunURL(host, owner, repo, pathParts[3:])
				if err != nil {
					return nil, err
				}
				// Parse fragment for step information
				parseStepFragment(parsedURL.Fragment, components)
				return components, nil
			}

		case "pull":
			// Pattern: /owner/repo/pull/123
			if len(pathParts) >= 4 {
				urlLog.Print("Parsing pull request URL")
				prNumber, err := strconv.ParseInt(pathParts[3], 10, 32)
				if err != nil {
					return nil, fmt.Errorf("invalid PR number: %s", pathParts[3])
				}
				return &GitHubURLComponents{
					Host:   host,
					Owner:  owner,
					Repo:   repo,
					Type:   URLTypePullRequest,
					Number: prNumber,
				}, nil
			}

		case "issues":
			// Pattern: /owner/repo/issues/123
			if len(pathParts) >= 4 {
				issueNumber, err := strconv.ParseInt(pathParts[3], 10, 32)
				if err != nil {
					return nil, fmt.Errorf("invalid issue number: %s", pathParts[3])
				}
				return &GitHubURLComponents{
					Host:   host,
					Owner:  owner,
					Repo:   repo,
					Type:   URLTypeIssue,
					Number: issueNumber,
				}, nil
			}

		case "blob", "tree", "raw":
			// Pattern: /owner/repo/{blob|tree|raw}/ref/path/to/file
			if len(pathParts) >= 5 {
				urlLog.Printf("Parsing file URL (type=%s)", urlType)
				ref := pathParts[3]
				filePath := strings.Join(pathParts[4:], "/")

				var urlTypeEnum GitHubURLType
				switch urlType {
				case "blob":
					urlTypeEnum = URLTypeBlob
				case "tree":
					urlTypeEnum = URLTypeTree
				case "raw":
					urlTypeEnum = URLTypeRaw
				}

				urlLog.Printf("Parsed file URL: ref=%s, path=%s", ref, filePath)
				return &GitHubURLComponents{
					Host:  host,
					Owner: owner,
					Repo:  repo,
					Type:  urlTypeEnum,
					Path:  filePath,
					Ref:   ref,
				}, nil
			}
		}
	}

	return nil, fmt.Errorf("unrecognized GitHub URL format")
}

// parseRunURL parses the run ID portion of a GitHub Actions URL
// Supports:
//   - /runs/12345678
//   - /runs/12345678/job/98765432
//   - /runs/12345678/job/98765432#step:7:1
//   - /runs/12345678/attempts/2
func parseRunURL(host, owner, repo string, parts []string) (*GitHubURLComponents, error) {
	if len(parts) == 0 {
		return nil, fmt.Errorf("missing run ID")
	}

	runID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid run ID: %s", parts[0])
	}

	components := &GitHubURLComponents{
		Host:   host,
		Owner:  owner,
		Repo:   repo,
		Type:   URLTypeRun,
		Number: runID,
	}

	// Check for additional path components (job ID, attempts, etc.)
	if len(parts) >= 3 && parts[1] == "job" {
		jobID, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid job ID: %s", parts[2])
		}
		components.JobID = jobID
	}

	return components, nil
}

// parseStepFragment parses the URL fragment for step information
// Supports formats like: #step:7:1 (step 7, line 1)
func parseStepFragment(fragment string, components *GitHubURLComponents) {
	if fragment == "" {
		return
	}

	// Check if fragment starts with "step:"
	if !strings.HasPrefix(fragment, "step:") {
		return
	}

	// Parse step:number:line format
	parts := strings.Split(fragment, ":")
	if len(parts) >= 2 {
		if stepNum, err := strconv.Atoi(parts[1]); err == nil {
			components.StepNumber = stepNum
		}
	}
	if len(parts) >= 3 {
		if lineNum, err := strconv.Atoi(parts[2]); err == nil {
			components.StepLine = lineNum
		}
	}
}

// parseRawGitHubContentURL parses raw.githubusercontent.com URLs
// Supports URLs like:
//   - https://raw.githubusercontent.com/owner/repo/refs/heads/branch/path/to/file.md
//   - https://raw.githubusercontent.com/owner/repo/COMMIT_SHA/path/to/file.md
//   - https://raw.githubusercontent.com/owner/repo/refs/tags/tag/path/to/file.md
func parseRawGitHubContentURL(parsedURL *url.URL) (*GitHubURLComponents, error) {
	pathParts := strings.Split(strings.Trim(parsedURL.Path, "/"), "/")

	// Need at least: owner, repo, ref-or-sha, and filename
	if len(pathParts) < 4 {
		return nil, fmt.Errorf("invalid raw.githubusercontent.com URL format: path too short")
	}

	owner := pathParts[0]
	repo := pathParts[1]

	// Determine the reference and file path based on the third part
	var ref string
	var filePath string

	if pathParts[2] == "refs" {
		// Format: /owner/repo/refs/heads/branch/path/to/file
		// or /owner/repo/refs/tags/tag/path/to/file
		if len(pathParts) < 5 {
			return nil, fmt.Errorf("invalid raw.githubusercontent.com URL format: refs path too short")
		}
		// pathParts[3] is "heads" or "tags"
		ref = pathParts[4] // branch or tag name
		filePath = strings.Join(pathParts[5:], "/")
	} else {
		// Format: /owner/repo/COMMIT_SHA/path/to/file or /owner/repo/branch/path/to/file
		ref = pathParts[2]
		filePath = strings.Join(pathParts[3:], "/")
	}

	// Validate owner and repo
	if owner == "" || repo == "" {
		return nil, fmt.Errorf("invalid raw.githubusercontent.com URL: owner and repo cannot be empty")
	}

	return &GitHubURLComponents{
		Host:  "raw.githubusercontent.com",
		Owner: owner,
		Repo:  repo,
		Type:  URLTypeRawContent,
		Path:  filePath,
		Ref:   ref,
	}, nil
}

// ParseRunURL is a convenience function that parses a run ID or URL and extracts run information.
// It accepts:
//   - Numeric run ID: "1234567890"
//   - GitHub Actions run URL: "https://github.com/owner/repo/actions/runs/12345678"
//   - Short run URL: "https://github.com/owner/repo/runs/12345678"
//   - Job URL: "https://github.com/owner/repo/actions/runs/12345678/job/98765432"
//   - Job URL with step: "https://github.com/owner/repo/actions/runs/12345678/job/98765432#step:7:1"
//   - Enterprise URLs: "https://github.example.com/owner/repo/actions/runs/12345678"
//
// For deep URLs with job/step information, use ParseRunURLExtended to get all details.
func ParseRunURL(input string) (runID int64, owner, repo, hostname string, err error) {
	// First try to parse as a direct numeric ID
	if runID, err := strconv.ParseInt(input, 10, 64); err == nil {
		return runID, "", "", "", nil
	}

	// Try to parse as a GitHub URL
	components, err := ParseGitHubURL(input)
	if err != nil {
		return 0, "", "", "", fmt.Errorf("invalid run ID or URL '%s': must be a numeric run ID or a GitHub URL containing '/actions/runs/{run-id}' or '/runs/{run-id}'", input)
	}

	if components.Type != URLTypeRun {
		return 0, "", "", "", fmt.Errorf("URL is not a GitHub Actions run URL")
	}

	return components.Number, components.Owner, components.Repo, components.Host, nil
}

// ParseRunURLExtended is similar to ParseRunURL but returns additional information
// including job ID and step details from deep URLs.
func ParseRunURLExtended(input string) (*GitHubURLComponents, error) {
	// First try to parse as a direct numeric ID
	if runID, err := strconv.ParseInt(input, 10, 64); err == nil {
		return &GitHubURLComponents{
			Type:   URLTypeRun,
			Number: runID,
		}, nil
	}

	// Try to parse as a GitHub URL
	components, err := ParseGitHubURL(input)
	if err != nil {
		return nil, fmt.Errorf("invalid run ID or URL '%s': must be a numeric run ID or a GitHub URL containing '/actions/runs/{run-id}' or '/runs/{run-id}'", input)
	}

	if components.Type != URLTypeRun {
		return nil, fmt.Errorf("URL is not a GitHub Actions run URL")
	}

	return components, nil
}

// ParsePRURL is a convenience function that parses a GitHub PR URL and extracts PR information.
// Expected format: https://github.com/owner/repo/pull/123 or https://github.enterprise.com/owner/repo/pull/123
func ParsePRURL(prURL string) (owner, repo string, prNumber int, err error) {
	components, err := ParseGitHubURL(prURL)
	if err != nil {
		return "", "", 0, err
	}

	if components.Type != URLTypePullRequest {
		return "", "", 0, fmt.Errorf("URL is not a GitHub PR URL")
	}

	// Validate that Number fits in int range (important for 32-bit systems)
	// Note: PR numbers are parsed with ParseInt(..., 10, 32) so they should always fit
	const maxInt = int(^uint(0) >> 1)
	const minInt = -maxInt - 1
	if components.Number > int64(maxInt) || components.Number < int64(minInt) {
		return "", "", 0, fmt.Errorf("PR number %d is out of range for int type", components.Number)
	}

	return components.Owner, components.Repo, int(components.Number), nil
}

// ParseRepoFileURL is a convenience function that parses a GitHub repository file URL.
// It accepts URLs like:
//   - https://github.com/owner/repo/blob/main/path/to/file.md
//   - https://github.com/owner/repo/tree/main/path/to/dir
//   - https://github.com/owner/repo/raw/main/path/to/file.md
//   - https://raw.githubusercontent.com/owner/repo/main/path/to/file.md
func ParseRepoFileURL(fileURL string) (owner, repo, ref, filePath string, err error) {
	components, err := ParseGitHubURL(fileURL)
	if err != nil {
		return "", "", "", "", err
	}

	// Check if it's a file-related URL type
	switch components.Type {
	case URLTypeBlob, URLTypeTree, URLTypeRaw, URLTypeRawContent:
		return components.Owner, components.Repo, components.Ref, components.Path, nil
	default:
		return "", "", "", "", fmt.Errorf("URL is not a GitHub file URL")
	}
}

// IsValidGitHubIdentifier checks if a string is a valid GitHub username or repository name
func IsValidGitHubIdentifier(s string) bool {
	// GitHub identifiers can contain alphanumeric characters, hyphens, and underscores
	// They cannot start or end with a hyphen and must be 1-39 characters long
	if len(s) == 0 || len(s) > 39 {
		return false
	}
	if s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	for _, ch := range s {
		if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') && ch != '-' && ch != '_' {
			return false
		}
	}
	return true
}

// GetRepoSlug returns the repository slug in "owner/repo" format
func (c *GitHubURLComponents) GetRepoSlug() string {
	return fmt.Sprintf("%s/%s", c.Owner, c.Repo)
}

// GetWorkflowName returns the workflow name from a file path (without .md extension)
func (c *GitHubURLComponents) GetWorkflowName() string {
	if c.Path == "" {
		return ""
	}
	return strings.TrimSuffix(filepath.Base(c.Path), ".md")
}

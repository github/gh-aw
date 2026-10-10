package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/stringutil"
)

var remoteWorkflowLog = logger.New("cli:remote_workflow")

var resolveRefToSHAForHost = parser.ResolveRefToSHAForHost
var downloadFileFromGitHubForHost = parser.DownloadFileFromGitHubForHost
var waitBeforeSHAResolutionRetry = sleepForSHAResolutionRetry

var shaResolutionRetryDelays = []time.Duration{
	1 * time.Second,
	3 * time.Second,
	9 * time.Second,
}

var transientHTTP5xxPattern = regexp.MustCompile(`http 5\d{2}`)

// FetchedWorkflow contains content and metadata from a directly fetched workflow file.
// This is the unified type that combines content with source information.
type FetchedWorkflow struct {
	Content                []byte   // The raw content of the workflow file
	CommitSHA              string   // The resolved commit SHA at the time of fetch (empty for local)
	IsLocal                bool     // true if this is a local workflow (from filesystem)
	SourcePath             string   // The original source path (local path or remote path)
	ConvertedFromJSON      bool     // true when the fetched source was JSON converted to markdown
	JSONConversionWarnings []string // best-effort conversion warnings produced during JSON import
}

// FetchWorkflowFromSourceWithContext fetches a workflow file from local disk or GitHub.
// The context is used to cancel remote ref resolution retries (for example, on Ctrl-C).
func FetchWorkflowFromSourceWithContext(ctx context.Context, spec *WorkflowSpec, verbose bool) (*FetchedWorkflow, error) {
	remoteWorkflowLog.Printf("Fetching workflow from source: spec=%s", spec.String())

	// Handle generic HTTP(S) URL imports (non-GitHub hosts).
	if spec.RawURL != "" {
		return fetchGenericURLWorkflow(ctx, spec, verbose)
	}

	// Handle local workflows
	if isLocalWorkflowPath(spec.WorkflowPath) {
		return fetchLocalWorkflow(spec, verbose)
	}

	// Handle remote workflows from GitHub
	return fetchRemoteWorkflow(ctx, spec, verbose)
}

// fetchLocalWorkflow reads a workflow file from the local filesystem
func fetchLocalWorkflow(spec *WorkflowSpec, verbose bool) (*FetchedWorkflow, error) {
	remoteWorkflowLog.Printf("Reading local workflow: %s", spec.WorkflowPath)

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Reading local workflow: "+spec.WorkflowPath))
	}

	content, err := os.ReadFile(spec.WorkflowPath)
	if err != nil {
		return nil, fmt.Errorf("local workflow '%s' not found: %w", spec.WorkflowPath, err)
	}

	remoteWorkflowLog.Printf("Read local workflow: bytes=%d", len(content))

	return &FetchedWorkflow{
		Content:    content,
		CommitSHA:  "", // Local workflows don't have a commit SHA
		IsLocal:    true,
		SourcePath: spec.WorkflowPath,
	}, nil
}

// fetchRemoteWorkflow fetches a workflow file directly from GitHub using the API
func fetchRemoteWorkflow(ctx context.Context, spec *WorkflowSpec, verbose bool) (*FetchedWorkflow, error) {
	remoteWorkflowLog.Printf("Fetching remote workflow: repo=%s, path=%s, version=%s",
		spec.RepoSlug, spec.WorkflowPath, spec.Version)

	// Parse owner and repo from the slug
	parts := strings.SplitN(spec.RepoSlug, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repository slug: %s", spec.RepoSlug)
	}
	owner := parts[0]
	repo := parts[1]

	// Determine the ref to use
	ref := spec.Version
	if ref == "" {
		ref = "main" // Default to main branch
		remoteWorkflowLog.Print("No version specified, defaulting to 'main'")
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Fetching %s/%s/%s@%s...", owner, repo, spec.WorkflowPath, ref)))
	}

	// Resolve the ref to a commit SHA for source tracking.
	commitSHA, err := resolveCommitSHAWithRetries(ctx, owner, repo, ref, spec.WorkflowPath, spec.Host, verbose)
	if err != nil {
		return nil, err
	}
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Resolved to commit: "+commitSHA[:7]))
	}

	// Download the workflow file from GitHub
	content, err := downloadFileFromGitHubForHost(ctx, owner, repo, spec.WorkflowPath, ref, spec.Host)
	if err != nil {
		// Try with common workflow directory prefixes if the direct path fails.
		// This handles short workflow names without path separators (e.g. "my-workflow.md").
		if !strings.HasPrefix(spec.WorkflowPath, "workflows/") && !strings.Contains(spec.WorkflowPath, "/") {
			for _, prefix := range []string{"workflows/", constants.WorkflowsDirSlash} {
				altPath := prefix + spec.WorkflowPath
				if !strings.HasSuffix(altPath, ".md") {
					altPath += ".md"
				}
				remoteWorkflowLog.Printf("Direct path failed, trying: %s", altPath)
				if altContent, altErr := downloadFileFromGitHubForHost(ctx, owner, repo, altPath, ref, spec.Host); altErr == nil {
					remoteWorkflowLog.Printf("Downloaded workflow via alt path: %s (%d bytes)", altPath, len(altContent))
					return &FetchedWorkflow{
						Content:    altContent,
						CommitSHA:  commitSHA,
						IsLocal:    false,
						SourcePath: altPath,
					}, nil
				}
			}
		}
		return nil, fmt.Errorf("failed to download workflow from %s/%s/%s@%s: %w", owner, repo, spec.WorkflowPath, ref, err)
	}

	remoteWorkflowLog.Printf("Downloaded workflow: path=%s bytes=%d", spec.WorkflowPath, len(content))

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Downloaded workflow (%d bytes)", len(content))))
	}

	return &FetchedWorkflow{
		Content:    content,
		CommitSHA:  commitSHA,
		IsLocal:    false,
		SourcePath: spec.WorkflowPath,
	}, nil
}

func resolveCommitSHAWithRetries(ctx context.Context, owner, repo, ref, workflowPath, host string, verbose bool) (string, error) {
	attempts := len(shaResolutionRetryDelays) + 1
	var lastErr error

	for attempt := 1; attempt <= attempts; attempt++ {
		commitSHA, err := resolveRefToSHAForHost(ctx, owner, repo, ref, host)
		if err == nil {
			remoteWorkflowLog.Printf("Resolved ref %s to SHA: %s", ref, commitSHA)
			return commitSHA, nil
		}

		lastErr = err
		remoteWorkflowLog.Printf("Failed to resolve ref %s to SHA (attempt %d/%d): %v", ref, attempt, attempts, err)

		if !isTransientSHAResolutionError(err) {
			retryCommand := fmt.Sprintf("gh aw add %s/%s/%s@<40-char-sha>", owner, repo, workflowPath)
			if hostHint, ok := hostResolutionHintForNotFound(owner, repo, ref, workflowPath, host, err); ok {
				return "", fmt.Errorf(
					"failed to resolve '%s' to commit SHA for '%s/%s'. Expected the GitHub API to return a commit SHA for the ref. %s: %w",
					ref, owner, repo, hostHint, err,
				)
			}
			return "", fmt.Errorf(
				"failed to resolve '%s' to commit SHA for '%s/%s'. Expected the GitHub API to return a commit SHA for the ref. Try: %s: %w",
				ref, owner, repo, retryCommand, err,
			)
		}

		if attempt < attempts {
			delay := shaResolutionRetryDelays[attempt-1]
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
					fmt.Sprintf("Transient SHA resolution failure for '%s' (attempt %d/%d). Retrying in %s...", ref, attempt, attempts, delay),
				))
			}
			if waitErr := waitBeforeSHAResolutionRetry(ctx, delay); waitErr != nil {
				retryCommand := fmt.Sprintf("gh aw add %s/%s/%s@<40-char-sha>", owner, repo, workflowPath)
				return "", fmt.Errorf(
					"failed to resolve '%s' to commit SHA because retry wait was cancelled. Expected the GitHub API to return a commit SHA for the ref. Try: %s: %w",
					ref, retryCommand, waitErr,
				)
			}
		}
	}

	retryCommand := fmt.Sprintf("gh aw add %s/%s/%s@<40-char-sha>", owner, repo, workflowPath)
	return "", fmt.Errorf(
		"failed to resolve '%s' to commit SHA after %d retries for '%s/%s'. Expected the GitHub API to return a commit SHA for the ref. Check rate limits or try: %s: %w",
		ref, len(shaResolutionRetryDelays), owner, repo, retryCommand, lastErr,
	)
}

// hostResolutionHintForNotFound returns a user-facing hint and whether it is applicable.
// hasHint is true only for 404-style resolution failures on non-github.com hosts.
func hostResolutionHintForNotFound(owner, repo, ref, workflowPath, explicitHost string, err error) (hint string, hasHint bool) {
	if err == nil {
		return "", false
	}

	errorText := strings.ToLower(err.Error())
	if !strings.Contains(errorText, "http 404") && !strings.Contains(errorText, "status 404") {
		return "", false
	}

	normalizedExplicitHost := normalizeHostForHint(explicitHost)
	if normalizedExplicitHost != "" {
		return "", false
	}

	resolvedHost := normalizeHostForHint(getGitHubHost())
	if resolvedHost == "" || resolvedHost == "github.com" {
		return "", false
	}

	trimmedPath := strings.TrimPrefix(workflowPath, "/")
	fullURL := fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s", owner, repo, ref, trimmedPath)
	return fmt.Sprintf(
		"Shorthand specs resolved on %s. Try using a full github.com source URL instead (for example: gh aw add %s)",
		resolvedHost, fullURL,
	), true
}

func normalizeHostForHint(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	host = strings.TrimPrefix(strings.TrimPrefix(host, "https://"), "http://")
	if idx := strings.Index(host, "/"); idx >= 0 {
		host = host[:idx]
	}
	return strings.TrimSuffix(host, "/")
}

// sleepForSHAResolutionRetry waits for the retry delay or context cancellation.
// It returns ctx.Err() when the context is cancelled before the delay elapses,
// otherwise nil when the delay completes normally.
func sleepForSHAResolutionRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// isTransientSHAResolutionError returns true when the ref-to-SHA failure appears
// transient and worth retrying (rate limits, network/timeout failures, or HTTP 5xx).
// All other errors are treated as permanent and fail immediately.
func isTransientSHAResolutionError(err error) bool {
	if err == nil {
		return false
	}

	errorText := strings.ToLower(err.Error())
	if strings.Contains(errorText, "http 429") ||
		strings.Contains(errorText, "rate limit") ||
		strings.Contains(errorText, "timeout") ||
		strings.Contains(errorText, "timed out") ||
		strings.Contains(errorText, "context deadline exceeded") ||
		strings.Contains(errorText, "temporary") ||
		strings.Contains(errorText, "connection reset") ||
		strings.Contains(errorText, "connection refused") ||
		strings.Contains(errorText, "eof") {
		return true
	}

	return transientHTTP5xxPattern.MatchString(errorText)
}

// fetchGenericURLWorkflow fetches a workflow from an arbitrary HTTP(S) URL and dispatches
// on the response Content-Type to produce a FetchedWorkflow.
//
// Supported content types:
//   - text/markdown, text/x-markdown → treated as raw gh-aw workflow markdown.
//   - application/json (or any +json suffix) → treated as a JSON workflow definition
//     and converted via ConvertJSONWorkflowToMarkdown.
//
// Any other content type is an error with an actionable message.
// Warnings from JSON conversion are printed to stderr when verbose is true.
func fetchGenericURLWorkflow(ctx context.Context, spec *WorkflowSpec, verbose bool) (*FetchedWorkflow, error) {
	remoteWorkflowLog.Printf("Fetching generic URL workflow")

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Fetching workflow from URL..."))
	}

	resource, err := FetchImportURL(ctx, spec.RawURL, FetchOptions{})
	if err != nil {
		return nil, err
	}

	ct := resource.ContentType
	remoteWorkflowLog.Printf("Fetched URL resource: content_type=%q bytes=%d", ct, len(resource.Body))

	switch {
	case ct == "text/markdown" || ct == "text/x-markdown":
		remoteWorkflowLog.Printf("URL returned markdown content (%d bytes)", len(resource.Body))
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Downloaded workflow markdown (%d bytes)", len(resource.Body))))
		}
		return &FetchedWorkflow{
			Content:    resource.Body,
			CommitSHA:  "",
			IsLocal:    false,
			SourcePath: spec.RawURL,
		}, nil

	case ct == "application/json" || strings.HasSuffix(ct, "+json"):
		remoteWorkflowLog.Printf("URL returned JSON content (%d bytes); converting", len(resource.Body))
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Downloaded JSON workflow (%d bytes); converting to markdown...", len(resource.Body))))
		}

		remoteWorkflowLog.Printf("JSON payload:\n%s", string(resource.Body))

		var wf JSONWorkflow
		if err := json.Unmarshal(resource.Body, &wf); err != nil {
			return nil, fmt.Errorf("failed to parse JSON workflow from URL: %w", err)
		}

		nameOverride := selectJSONImportNameOverride(spec.WorkflowName, &wf)
		generated, err := ConvertJSONWorkflowToMarkdown(&wf, ConvertOptions{NameOverride: nameOverride})
		if err != nil {
			return nil, fmt.Errorf("failed to convert JSON workflow: %w", err)
		}

		remoteWorkflowLog.Printf("Converted JSON to markdown: filename=%s bytes=%d warnings=%d",
			generated.Filename, len(generated.Markdown), len(generated.Warnings))

		if verbose {
			for _, w := range generated.Warnings {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage("JSON workflow import: "+w))
			}
		}

		// Use the generated filename as the WorkflowName on the spec so that
		// downstream code (e.g. add_command.go) uses the correct file name.
		spec.WorkflowName = generated.Filename

		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Converted JSON workflow to markdown (%d bytes)", len(generated.Markdown))))
		}

		return &FetchedWorkflow{
			Content:                []byte(generated.Markdown),
			CommitSHA:              "",
			IsLocal:                false,
			SourcePath:             spec.RawURL,
			ConvertedFromJSON:      true,
			JSONConversionWarnings: generated.Warnings,
		}, nil

	default:
		if ct == "" {
			return nil, errors.New(console.FormatErrorMessage(
				"URL did not return a Content-Type header. Expected text/markdown or application/json."))
		}
		return nil, errors.New(console.FormatErrorMessage(
			fmt.Sprintf("unsupported Content-Type %q from URL. Expected text/markdown or application/json.", ct)))
	}
}

func selectJSONImportNameOverride(currentName string, wf *JSONWorkflow) string {
	if wf == nil {
		return currentName
	}

	if name := sanitizeJSONImportName(wf.Name); name != "" {
		return name
	}

	if rawTitle, ok := wf.Extra["title"]; ok {
		if title, ok := rawTitle.(string); ok {
			if sanitized := sanitizeJSONImportName(title); sanitized != "" {
				return sanitized
			}
		}
	}

	return currentName
}

func sanitizeJSONImportName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return stringutil.SanitizeForFilename(toKebabCase(value))
}

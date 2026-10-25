package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

const importURLMaxBytes = 500 * 1024      // 500 KB
const importURLTimeout = 30 * time.Second // default per-request timeout

var importURLFetcherLog = logger.New("cli:import_url_fetcher")

// FetchOptions configures FetchImportURL.
type FetchOptions struct {
	// HTTPClient overrides the default http.Client.  When nil, a client with
	// importURLTimeout is used.  Callers that supply their own client are
	// responsible for configuring an appropriate timeout.
	HTTPClient *http.Client
}

// FetchedResource is the result of fetching a URL for workflow import.
type FetchedResource struct {
	URL         string // the original URL
	ContentType string // canonicalized media type without parameters (e.g. "application/json")
	Body        []byte
}

// FetchImportURL fetches rawURL and returns its content and canonicalized Content-Type.
//
// Resolution order:
//  1. HEAD request to detect Content-Type without downloading the body.
//     If the server returns 405/501 or omits Content-Type, skip to step 2.
//  2. GET request – response headers are checked before the body is consumed.
//
// Authentication is attached only when BOTH of the following hold:
//   - the request scheme is "https"
//   - the request host is an exact match for one of the default GitHub import
//     hosts (github.com, raw/media/objects.githubusercontent.com,
//     api.githubcopilot.com), or for the hostname extracted from the GH_HOST
//     environment variable
//
// In that case the value of GITHUB_TOKEN (falling back to GH_TOKEN, then gh auth token) is sent as
// "Authorization: Bearer <token>".  For all other hosts, or for any HTTP (non-TLS)
// request, no authentication header is added.  TLS verification is always enabled.
//
// The body is capped at importURLMaxBytes to prevent runaway downloads.
func FetchImportURL(ctx context.Context, rawURL string, opts FetchOptions) (*FetchedResource, error) {
	importURLFetcherLog.Printf("Fetching import URL (redacted for security)")

	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: importURLTimeout}
	}

	// Attempt HEAD first to get Content-Type without downloading the body.
	ct, headOK := tryHead(ctx, client, rawURL)
	importURLFetcherLog.Printf("HEAD result: content_type=%q ok=%v", ct, headOK)

	// Always perform the GET to retrieve the body.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build GET request: %w", err)
	}
	attachImportAuthHeader(req, rawURL)

	logRequestVerbose(req)

	importURLFetcherLog.Printf("Sending GET request to %s", req.URL.Host)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL: %w", sanitizeHTTPError(err))
	}
	defer resp.Body.Close()

	importURLFetcherLog.Printf("GET response: status=%d content-type=%q", resp.StatusCode, resp.Header.Get("Content-Type"))

	if err := validateImportURLResponse(resp); err != nil {
		return nil, err
	}

	// Prefer Content-Type obtained via HEAD; fall back to GET response headers.
	if !headOK || ct == "" {
		ct = canonicalContentType(resp.Header.Get("Content-Type"))
	}
	body, err := readImportURLBody(resp.Body)
	if err != nil {
		return nil, err
	}
	importURLFetcherLog.Printf("Fetched import URL: content_type=%s, bytes=%d", ct, len(body))
	return &FetchedResource{URL: rawURL, ContentType: ct, Body: body}, nil
}

func validateImportURLResponse(resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return errors.New(console.FormatErrorMessage(
			fmt.Sprintf("access denied (HTTP %d). Check that the URL is accessible or set an auth token.", resp.StatusCode),
		))
	case http.StatusNotFound:
		return errors.New(console.FormatErrorMessage("URL not found (HTTP 404)"))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logResponseBodyVerbose(resp)
		return errors.New(console.FormatErrorMessage(
			fmt.Sprintf("unexpected HTTP %d response from server", resp.StatusCode),
		))
	}
	return nil
}

func readImportURLBody(reader io.Reader) ([]byte, error) {
	// Guard against oversized responses.
	limited := io.LimitReader(reader, int64(importURLMaxBytes)+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	if len(body) > importURLMaxBytes {
		return nil, errors.New(console.FormatErrorMessage(
			fmt.Sprintf("response body exceeds size limit (%s)", console.FormatFileSize(importURLMaxBytes)),
		))
	}

	return body, nil
}

// tryHead issues a HEAD request and returns the canonicalized Content-Type and whether
// the request succeeded (status 2xx with a Content-Type header).  Any error or non-useful
// response is silently swallowed – the caller falls back to GET.
func tryHead(ctx context.Context, client *http.Client, rawURL string) (string, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return "", false
	}
	attachImportAuthHeader(req, rawURL)

	importURLFetcherLog.Printf("Sending HEAD request to %s", req.URL.Host)
	resp, err := client.Do(req)
	if err != nil {
		importURLFetcherLog.Printf("HEAD request failed (will fallback to GET): %v", sanitizeHTTPError(err))
		return "", false
	}
	defer resp.Body.Close()

	importURLFetcherLog.Printf("HEAD response: status=%d content-type=%q", resp.StatusCode, resp.Header.Get("Content-Type"))

	// 405 Method Not Allowed / 501 Not Implemented → server doesn't support HEAD.
	if resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotImplemented {
		importURLFetcherLog.Print("HEAD not supported by server, falling back to GET")
		return "", false
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		importURLFetcherLog.Printf("HEAD returned non-2xx status %d, falling back to GET", resp.StatusCode)
		return "", false
	}

	ct := canonicalContentType(resp.Header.Get("Content-Type"))
	importURLFetcherLog.Printf("HEAD content-type resolved: %q", ct)
	return ct, ct != ""
}

// canonicalContentType strips parameters (e.g. "; charset=utf-8") from a Content-Type
// header value and returns the lower-cased media type.  Returns "" on parse failure.
func canonicalContentType(raw string) string {
	if raw == "" {
		return ""
	}
	mt, _, err := mime.ParseMediaType(raw)
	if err != nil {
		// Best-effort: strip parameters manually.
		if idx := strings.IndexByte(raw, ';'); idx != -1 {
			raw = raw[:idx]
		}
		return strings.ToLower(strings.TrimSpace(raw))
	}
	return strings.ToLower(mt)
}

// attachImportAuthHeader adds authentication headers to req when
// ALL of the following are true:
//   - the request scheme is "https" (tokens are never sent over plaintext HTTP)
//   - the request host is an exact match for one of the default GitHub import
//     hosts (github.com, raw/media/objects.githubusercontent.com,
//     api.githubcopilot.com), or for the hostname extracted from the GH_HOST
//     environment variable
//
// The token is read from GH_TOKEN, falling back to GITHUB_TOKEN.  Nothing is
// added when no matching host is found, no token is set, or the request is
// not over HTTPS.  The token value is never logged.
var defaultImportAuthHosts = map[string]struct{}{
	"github.com":                     {},
	"raw.githubusercontent.com":      {},
	"media.githubusercontent.com":    {},
	"objects.githubusercontent.com":  {},
	constants.GitHubCopilotMCPDomain: {},
}

const copilotIntegrationHeaderValue = "agentic-workflows"

const (
	copilotAutomationSegmentCount    = 6
	copilotAutomationAgentsSegment   = "agents"
	copilotAutomationReposSegment    = "repos"
	copilotAutomationResourceSegment = "automations"
)

func attachImportAuthHeader(req *http.Request, rawURL string) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return
	}

	// Never send credentials over plaintext HTTP — HTTPS is required.
	if !strings.EqualFold(parsed.Scheme, "https") {
		importURLFetcherLog.Printf("Skipping auth header for non-HTTPS URL: scheme=%s", parsed.Scheme)
		return
	}

	host := strings.ToLower(parsed.Hostname())

	// Authoritative GitHub hosts to which the token may be sent.
	if _, ok := defaultImportAuthHosts[host]; !ok && host != importAuthGHHost() {
		importURLFetcherLog.Printf("Skipping auth header for non-GitHub host: %s", host)
		return
	}

	token, err := parser.GetGitHubToken()
	if err != nil {
		importURLFetcherLog.Printf("No GitHub token available: %v", err)
		return
	}

	importURLFetcherLog.Printf("Attaching auth header for host: %s", host)
	req.Header.Set("Authorization", "Bearer "+token)
	if isCopilotAutomationImportURL(parsed) {
		req.Header.Set("Copilot-Integration-Id", copilotIntegrationHeaderValue)
	}
}

// isCopilotAutomationImportURL reports whether u targets a Copilot automation API route
// with exactly six path segments: /agents/repos/{owner}/{repo}/automations/{id}.
// It returns false when u is nil, the host is not api.githubcopilot.com, or the
// path does not match that automation pattern.
func isCopilotAutomationImportURL(u *url.URL) bool {
	if u == nil || !strings.EqualFold(u.Hostname(), constants.GitHubCopilotMCPDomain) {
		return false
	}
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	return len(segments) == copilotAutomationSegmentCount &&
		segments[0] == copilotAutomationAgentsSegment &&
		segments[1] == copilotAutomationReposSegment &&
		segments[2] != "" &&
		segments[3] != "" &&
		segments[4] == copilotAutomationResourceSegment &&
		segments[5] != ""
}

// buildRequestLogString formats req in HTTP/1.1 wire format with the Authorization
// header partially redacted. Bearer tokens show the first 4 characters followed by
// "***"; all other schemes are replaced with "***".
func buildRequestLogString(req *http.Request) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s %s HTTP/1.1\r\n", req.Method, req.URL.RequestURI())
	fmt.Fprintf(&sb, "Host: %s\r\n", req.URL.Host)
	for key, vals := range req.Header {
		val := strings.Join(vals, ", ")
		if strings.EqualFold(key, "Authorization") {
			if parts := strings.SplitN(val, " ", 2); len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
				credential := parts[1]
				prefix := credential
				if len(prefix) > 4 {
					prefix = prefix[:4]
				}
				val = "Bearer " + prefix + "***"
			} else {
				val = "***"
			}
		}
		fmt.Fprintf(&sb, "%s: %s\r\n", key, val)
	}
	return sb.String()
}

// logRequestVerbose writes the outgoing request in HTTP/1.1 wire format
// to the debug logger with the Authorization value partially redacted.
func logRequestVerbose(req *http.Request) {
	if !importURLFetcherLog.Enabled() {
		return
	}
	importURLFetcherLog.Print(buildRequestLogString(req))
}

// logResponseBodyVerbose reads and logs the first 512 bytes of a non-2xx response
// body via the debug logger to help diagnose server-side errors.
func logResponseBodyVerbose(resp *http.Response) {
	snippet, err := io.ReadAll(io.LimitReader(resp.Body, 512))
	if err != nil || len(snippet) == 0 {
		return
	}
	importURLFetcherLog.Printf("Response body (first %d bytes):\n%s", len(snippet), string(snippet))
}

func importAuthGHHost() string {
	// Use unified resolution (GITHUB_SERVER_URL > GITHUB_ENTERPRISE_HOST > GITHUB_HOST > GH_HOST).
	// Return "" when no host env var is set so that callers do not add a
	// redundant entry for github.com, which is already in defaultImportAuthHosts.
	if !isAnyGitHubHostEnvVarSet() {
		return ""
	}
	// getGitHubHost (defined in this package) returns a normalized https://… URL;
	// url.Parse is used only to extract the hostname so that port numbers are stripped.
	resolved := getGitHubHost()
	u, parseErr := url.Parse(resolved)
	if parseErr != nil || u.Hostname() == "" {
		// getGitHubHost always returns a well-formed URL; an error here is unexpected.
		importURLFetcherLog.Printf("importAuthGHHost: unexpected url.Parse failure for %q: %v", resolved, parseErr)
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// sanitizeHTTPError strips the request URL from a *url.Error (the error type
// returned by http.Client.Do) so that signed or token-bearing query parameters
// are never written to logs or returned in error messages.
//
// Note: errors from the HTTP stack that are not *url.Error (e.g. context
// cancellation, TLS handshake failures surfaced as net.OpError) are returned
// unchanged.  Those typically contain the host but not query parameters.
func sanitizeHTTPError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		// Return only the underlying network error, discarding the URL.
		return urlErr.Err
	}
	return err
}

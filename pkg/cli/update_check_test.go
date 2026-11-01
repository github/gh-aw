//go:build !integration

package cli

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeReleaseClient struct {
	do func(ctx context.Context, method string, path string, body io.Reader, response any) error
}

func (f fakeReleaseClient) DoWithContext(ctx context.Context, method string, path string, body io.Reader, response any) error {
	return f.do(ctx, method, path, body, response)
}

func TestShouldCheckForUpdate(t *testing.T) {
	// Save original environment
	origCI := os.Getenv("CI")
	origCopilotAgentSessionID := os.Getenv("COPILOT_AGENT_SESSION_ID")
	origMCP := os.Getenv("GH_AW_MCP_SERVER")
	origGetLastCheckFilePath := getLastCheckFilePathFunc
	defer func() {
		os.Setenv("CI", origCI)
		os.Setenv("COPILOT_AGENT_SESSION_ID", origCopilotAgentSessionID)
		os.Setenv("GH_AW_MCP_SERVER", origMCP)
		getLastCheckFilePathFunc = origGetLastCheckFilePath
	}()

	tests := []struct {
		name           string
		noCheckUpdate  bool
		ciEnv          string
		mcpEnv         string
		lastCheckTime  string
		expectedResult bool
	}{
		{
			name:           "should check when flag is false and no recent check",
			noCheckUpdate:  false,
			ciEnv:          "",
			mcpEnv:         "",
			lastCheckTime:  "",
			expectedResult: true,
		},
		{
			name:           "should not check when flag is true",
			noCheckUpdate:  true,
			ciEnv:          "",
			mcpEnv:         "",
			lastCheckTime:  "",
			expectedResult: false,
		},
		{
			name:           "should not check in CI environment",
			noCheckUpdate:  false,
			ciEnv:          "true",
			mcpEnv:         "",
			lastCheckTime:  "",
			expectedResult: false,
		},
		{
			name:           "should not check in MCP server mode",
			noCheckUpdate:  false,
			ciEnv:          "",
			mcpEnv:         "true",
			lastCheckTime:  "",
			expectedResult: false,
		},
		{
			name:           "should not check when recent check exists",
			noCheckUpdate:  false,
			ciEnv:          "",
			mcpEnv:         "",
			lastCheckTime:  time.Now().Format(time.RFC3339),
			expectedResult: false,
		},
		{
			name:           "should check when last check is old",
			noCheckUpdate:  false,
			ciEnv:          "",
			mcpEnv:         "",
			lastCheckTime:  time.Now().Add(-25 * time.Hour).Format(time.RFC3339),
			expectedResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			if tt.ciEnv == "" {
				os.Unsetenv("CI")
				os.Unsetenv("CONTINUOUS_INTEGRATION")
				os.Unsetenv("GITHUB_ACTIONS")
				os.Unsetenv("COPILOT_AGENT_SESSION_ID")
			} else {
				os.Setenv("CI", tt.ciEnv)
			}

			if tt.mcpEnv == "" {
				os.Unsetenv("GH_AW_MCP_SERVER")
			} else {
				os.Setenv("GH_AW_MCP_SERVER", tt.mcpEnv)
			}

			// Create temporary last check file if needed
			tmpDir := t.TempDir()
			lastCheckFile := filepath.Join(tmpDir, lastCheckFileName)

			// Override the function to use temp directory
			getLastCheckFilePathFunc = func() string {
				return lastCheckFile
			}

			if tt.lastCheckTime != "" {
				err := os.WriteFile(lastCheckFile, []byte(tt.lastCheckTime), 0644)
				if err != nil {
					t.Fatalf("Failed to create test file: %v", err)
				}
			}

			result := shouldCheckForUpdate(tt.noCheckUpdate)
			if result != tt.expectedResult {
				t.Errorf("shouldCheckForUpdate() = %v, want %v", result, tt.expectedResult)
			}
		})
	}
}

func TestIsRunningAsMCPServer(t *testing.T) {
	// Save original environment
	origMCP := os.Getenv("GH_AW_MCP_SERVER")
	defer func() {
		os.Setenv("GH_AW_MCP_SERVER", origMCP)
	}()

	tests := []struct {
		name     string
		mcpEnv   string
		expected bool
	}{
		{
			name:     "not in MCP server mode",
			mcpEnv:   "",
			expected: false,
		},
		{
			name:     "in MCP server mode",
			mcpEnv:   "true",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("GH_AW_MCP_SERVER", tt.mcpEnv)
			result := isRunningAsMCPServer()
			if result != tt.expected {
				t.Errorf("isRunningAsMCPServer() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetLastCheckFilePath(t *testing.T) {
	isolateUpdateCheckCache(t)

	path := getLastCheckFilePath()
	if path == "" {
		t.Error("getLastCheckFilePath() returned empty string")
	}

	// Check that the path contains expected components
	if !filepath.IsAbs(path) {
		t.Errorf("getLastCheckFilePath() returned non-absolute path: %s", path)
	}

	// Check that the directory exists or can be created
	dir := filepath.Dir(path)
	if _, err := os.Stat(dir); err != nil {
		if !os.IsNotExist(err) {
			t.Errorf("Unexpected error checking directory: %v", err)
		}
	}
}

func TestUpdateLastCheckTime(t *testing.T) {
	// Save original function
	origGetLastCheckFilePath := getLastCheckFilePathFunc
	defer func() {
		getLastCheckFilePathFunc = origGetLastCheckFilePath
	}()

	// Create temporary directory
	tmpDir := t.TempDir()
	lastCheckFile := filepath.Join(tmpDir, lastCheckFileName)

	// Override the function to use temp directory
	getLastCheckFilePathFunc = func() string {
		return lastCheckFile
	}

	// Update the last check time
	updateLastCheckTime()

	// Verify the file was created
	if _, err := os.Stat(lastCheckFile); err != nil {
		t.Fatalf("Last check file was not created: %v", err)
	}

	// Read and verify the timestamp
	data, err := os.ReadFile(lastCheckFile)
	if err != nil {
		t.Fatalf("Failed to read last check file: %v", err)
	}

	timestamp, err := time.Parse(time.RFC3339, string(data))
	if err != nil {
		t.Fatalf("Failed to parse timestamp: %v", err)
	}

	// Check that the timestamp is recent (within 1 second)
	if time.Since(timestamp) > time.Second {
		t.Errorf("Timestamp is not recent: %v", timestamp)
	}
}

func TestCheckForUpdatesAsync_ContextCancellation(t *testing.T) {
	// Test that async update check respects context cancellation
	origGetLastCheckFilePath := getLastCheckFilePathFunc
	defer func() {
		getLastCheckFilePathFunc = origGetLastCheckFilePath
	}()

	// Ensure we're not in CI mode
	t.Setenv("CI", "")
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("CONTINUOUS_INTEGRATION", "")
	t.Setenv("COPILOT_AGENT_SESSION_ID", "")
	t.Setenv("GH_AW_MCP_SERVER", "")

	// Create temporary directory for last check file
	tmpDir := t.TempDir()
	lastCheckFile := filepath.Join(tmpDir, lastCheckFileName)

	// Override the function to use temp directory
	getLastCheckFilePathFunc = func() string {
		return lastCheckFile
	}

	// Create a cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately
	cancel()

	// Call CheckForUpdatesAsync with cancelled context and join the goroutine
	join := CheckForUpdatesAsync(ctx, false, false)
	join()

	// The update check should not have created a last check file
	// because the context was cancelled
	// Note: The check might still run if it started before cancellation,
	// so we just verify no panics occurred
}

func TestCheckForUpdatesAsync_JoinsGoroutine(t *testing.T) {
	// Test that the returned join function waits for the goroutine to complete
	origGetLastCheckFilePath := getLastCheckFilePathFunc
	origCheckForUpdatesWithContext := checkForUpdatesWithContextFunc
	defer func() {
		getLastCheckFilePathFunc = origGetLastCheckFilePath
		checkForUpdatesWithContextFunc = origCheckForUpdatesWithContext
	}()

	// Ensure we're not in CI mode so that shouldCheckForUpdate returns true
	t.Setenv("CI", "")
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("CONTINUOUS_INTEGRATION", "")
	t.Setenv("COPILOT_AGENT_SESSION_ID", "")
	t.Setenv("GH_AW_MCP_SERVER", "")

	// Create temporary directory for last check file
	tmpDir := t.TempDir()
	lastCheckFile := filepath.Join(tmpDir, lastCheckFileName)
	getLastCheckFilePathFunc = func() string {
		return lastCheckFile
	}

	started := make(chan struct{})
	release := make(chan struct{})
	checkForUpdatesWithContextFunc = func(_ context.Context, _ bool, _ bool) {
		close(started)
		<-release
	}

	ctx := context.Background()

	join := CheckForUpdatesAsync(ctx, false, false)
	<-started

	// join() must wait until the worker exits.
	done := make(chan struct{})
	go func() {
		defer close(done)
		join()
	}()

	select {
	case <-done:
		t.Fatal("join returned before worker exited")
	case <-time.After(100 * time.Millisecond):
		// join is correctly blocked waiting for worker completion
	}

	close(release)

	select {
	case <-done:
		// goroutine joined successfully after worker exit
	case <-time.After(2 * time.Second):
		t.Fatal("join function did not return within 2 seconds")
	}
}

func TestFindLatestPublishedReleaseTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		releases []Release
		want     string
	}{
		{
			name: "returns first non-draft release tag",
			releases: []Release{
				{TagName: "v1.2.0-beta.1", Draft: false, Prerelease: true},
				{TagName: "v1.1.0", Draft: false, Prerelease: false},
			},
			want: "v1.2.0-beta.1",
		},
		{
			name: "skips draft releases",
			releases: []Release{
				{TagName: "v1.3.0", Draft: true},
				{TagName: "v1.2.0", Draft: false},
			},
			want: "v1.2.0",
		},
		{
			name: "skips empty tags",
			releases: []Release{
				{TagName: "", Draft: false},
				{TagName: "v1.0.0", Draft: false},
			},
			want: "v1.0.0",
		},
		{
			name: "returns empty when no published releases",
			releases: []Release{
				{TagName: "", Draft: true},
				{TagName: "", Draft: false},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findLatestPublishedReleaseTag(tt.releases)
			assert.Equal(t, tt.want, got, "unexpected latest published release tag")
		})
	}
}

func TestGetLatestReleaseWithClient_StableReleaseUsesLatestEndpoint(t *testing.T) {
	t.Parallel()

	client := fakeReleaseClient{
		do: func(ctx context.Context, method string, path string, body io.Reader, response any) error {
			assert.Equal(t, http.MethodGet, method)
			assert.Equal(t, "repos/github/gh-aw/releases/latest", path)
			assert.Nil(t, body)
			release, ok := response.(*Release)
			if !ok {
				t.Fatalf("response type = %T, want *Release", response)
			}
			release.TagName = "v1.2.3"
			return nil
		},
	}

	got, err := getLatestReleaseWithClient(context.Background(), client, false)
	require.NoError(t, err)
	assert.Equal(t, "v1.2.3", got)
}

func TestGetLatestReleaseWithClient_IncludePrereleasesUsesReleasesEndpoint(t *testing.T) {
	t.Parallel()

	client := fakeReleaseClient{
		do: func(ctx context.Context, method string, path string, body io.Reader, response any) error {
			assert.Equal(t, http.MethodGet, method)
			assert.Equal(t, "repos/github/gh-aw/releases?per_page=50", path)
			assert.Nil(t, body)
			releases, ok := response.(*[]Release)
			if !ok {
				t.Fatalf("response type = %T, want *[]Release", response)
			}
			*releases = []Release{{TagName: "v1.2.3-beta.1", Prerelease: true}}
			return nil
		},
	}

	got, err := getLatestReleaseWithClient(context.Background(), client, true)
	require.NoError(t, err)
	assert.Equal(t, "v1.2.3-beta.1", got)
}

func TestGetLatestReleaseWithClient_PropagatesContextErrors(t *testing.T) {
	t.Parallel()

	client := fakeReleaseClient{
		do: func(ctx context.Context, method string, path string, body io.Reader, response any) error {
			return context.Canceled
		},
	}

	got, err := getLatestReleaseWithClient(context.Background(), client, false)
	assert.Empty(t, got)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestIsCurrentVersionAtLeastLatest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		currentVersion string
		latestVersion  string
		want           bool
	}{
		{
			name:           "equal versions",
			currentVersion: "v0.76.1",
			latestVersion:  "v0.76.1",
			want:           true,
		},
		{
			name:           "equal versions without v prefix",
			currentVersion: "0.76.1",
			latestVersion:  "v0.76.1",
			want:           true,
		},
		{
			name:           "newer stable release than latest stable",
			currentVersion: "v0.77.0",
			latestVersion:  "v0.76.1",
			want:           true,
		},
		{
			name:           "newer prerelease than latest stable",
			currentVersion: "v0.77.0-beta.1",
			latestVersion:  "v0.76.1",
			want:           true,
		},
		{
			name:           "older prerelease than latest stable",
			currentVersion: "v0.76.1-beta.1",
			latestVersion:  "v0.76.1",
			want:           false,
		},
		{
			name:           "older stable release",
			currentVersion: "v0.76.0",
			latestVersion:  "v0.76.1",
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isCurrentVersionAtLeastLatest(tt.currentVersion, tt.latestVersion))
		})
	}
}

func TestGitHubDotComRESTClientOptions(t *testing.T) {
	t.Parallel()

	opts := gitHubDotComRESTClientOptions()
	assert.Equal(t, "github.com", opts.Host)
	assert.Equal(t, constants.DefaultHTTPClientTimeout, opts.Timeout)
}

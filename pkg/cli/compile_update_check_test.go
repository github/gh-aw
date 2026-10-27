//go:build !integration

package cli

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/workflow"
)

func TestShouldRunCompileUpdateCheck(t *testing.T) {
	origGetFilePath := getCompileUpdateCheckFilePathFunc
	origIsTerminal := compileUpdateCheckIsTerminalFunc
	t.Cleanup(func() {
		getCompileUpdateCheckFilePathFunc = origGetFilePath
		compileUpdateCheckIsTerminalFunc = origIsTerminal
	})

	tmpDir := t.TempDir()
	lastCheckFile := filepath.Join(tmpDir, compileUpdateCheckFileName)
	getCompileUpdateCheckFilePathFunc = func() string {
		return lastCheckFile
	}
	compileUpdateCheckIsTerminalFunc = func() bool {
		return true
	}

	t.Setenv("CI", "")
	t.Setenv("CONTINUOUS_INTEGRATION", "")
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("COPILOT_AGENT_SESSION_ID", "")
	t.Setenv("GH_AW_MCP_SERVER", "")
	t.Setenv(compileUpdateCheckDisableEnv, "")
	assert.True(t, shouldRunCompileUpdateCheck(false), "check should run when not disabled")

	require.NoError(
		t,
		os.WriteFile(lastCheckFile, []byte(time.Now().Format(time.RFC3339)), 0600),
		"recent compile update marker should be written",
	)
	assert.False(t, shouldRunCompileUpdateCheck(false), "recent marker should suppress the background check")

	t.Setenv(compileUpdateCheckDisableEnv, "1")
	assert.False(t, shouldRunCompileUpdateCheck(false), "check should be disabled by environment variable")

	t.Setenv(compileUpdateCheckDisableEnv, "")
	assert.False(t, shouldRunCompileUpdateCheck(true), "check should be disabled by flag")

	compileUpdateCheckIsTerminalFunc = func() bool {
		return false
	}
	assert.False(t, shouldRunCompileUpdateCheck(false), "check should be disabled in non-interactive environments")
}

func TestRunCompileUpdateCheck(t *testing.T) {
	originalVersion := GetVersion()
	originalRelease := workflow.IsRelease()
	originalLatestURL := compileUpdateCheckLatestReleaseURL
	originalProbeURLFunc := compileUpdateCheckProbeURLFunc
	defer func() {
		SetVersionInfo(originalVersion)
		workflow.SetIsRelease(originalRelease)
		compileUpdateCheckLatestReleaseURL = originalLatestURL
		compileUpdateCheckProbeURLFunc = originalProbeURLFunc
	}()

	tests := []struct {
		name           string
		currentVersion string
		latestVersion  string
		existingTags   map[string]bool
		expected       *compileUpdateNotification
	}{
		{
			name:           "returns minor version upgrade hint",
			currentVersion: "v1.2.3",
			latestVersion:  "v1.3.0",
			existingTags: map[string]bool{
				"v1.2.3": true,
				"v1.3.0": true,
			},
			expected: &compileUpdateNotification{
				Kind:           compileUpdateNotificationMinorBehind,
				CurrentVersion: "v1.2.3",
				LatestVersion:  "v1.3.0",
			},
		},
		{
			name:           "returns prominent notice when current tag is missing",
			currentVersion: "v1.2.3",
			latestVersion:  "v1.3.0",
			existingTags: map[string]bool{
				"v1.3.0": true,
			},
			expected: &compileUpdateNotification{
				Kind:           compileUpdateNotificationRemovedTag,
				CurrentVersion: "v1.2.3",
				LatestVersion:  "v1.3.0",
			},
		},
		{
			name:           "ignores patch-only difference",
			currentVersion: "v1.2.3",
			latestVersion:  "v1.2.4",
			existingTags: map[string]bool{
				"v1.2.3": true,
				"v1.2.4": true,
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newCompileUpdateCheckTestServer(t, tt.latestVersion, tt.existingTags)
			defer server.Close()

			SetVersionInfo(tt.currentVersion)
			workflow.SetIsRelease(true)
			compileUpdateCheckLatestReleaseURL = server.URL + "/releases/latest"
			compileUpdateCheckProbeURLFunc = func(tag string) string {
				return fmt.Sprintf("%s/raw/%s/go.mod", server.URL, tag)
			}

			got, err := runCompileUpdateCheck(context.Background(), server.Client())
			require.NoError(t, err, "runCompileUpdateCheck should not fail")
			assert.Equal(t, tt.expected, got, "unexpected compile update notification")
		})
	}
}

func TestRunCompileUpdateCheckProbesCurrentReleaseOnce(t *testing.T) {
	originalVersion, originalRelease := GetVersion(), workflow.IsRelease()
	originalURL, originalProbe := compileUpdateCheckLatestReleaseURL, compileUpdateCheckProbeURLFunc
	t.Cleanup(func() {
		SetVersionInfo(originalVersion)
		workflow.SetIsRelease(originalRelease)
		compileUpdateCheckLatestReleaseURL, compileUpdateCheckProbeURLFunc = originalURL, originalProbe
	})
	SetVersionInfo("v1.2.3")
	workflow.SetIsRelease(true)
	server, methods := newCompileUpdateCheckMethodServer(t, "v1.2.3", map[string]bool{"v1.2.3": true})
	defer server.Close()
	compileUpdateCheckLatestReleaseURL = server.URL + "/releases/latest"
	compileUpdateCheckProbeURLFunc = func(tag string) string {
		return fmt.Sprintf("%s/raw/%s/go.mod", server.URL, tag)
	}
	got, err := runCompileUpdateCheck(context.Background(), server.Client())
	require.NoError(t, err)
	require.Nil(t, got)
	require.Len(t, methodsForPrefix(methods, "/raw/"), 1, "identical current/latest tags need only one probe")
	require.Empty(t, methodsForPrefix(methods, "/github/gh-aw/releases/tag/"), "the redirect identifies the release without fetching its page")
}

func TestPrintCompileUpdateNotification(t *testing.T) {
	tests := []struct {
		name         string
		notification *compileUpdateNotification
		expected     []string
	}{
		{
			name: "minor version behind",
			notification: &compileUpdateNotification{
				Kind:           compileUpdateNotificationMinorBehind,
				CurrentVersion: "v1.2.3",
				LatestVersion:  "v1.3.0",
			},
			expected: []string{
				"Compiler upgrade recommended: gh-aw v1.2.3 is behind the latest release v1.3.0.",
				"Hint: upgrade the compiler with: gh extension upgrade github/gh-aw",
			},
		},
		{
			name: "removed tag warning",
			notification: &compileUpdateNotification{
				Kind:           compileUpdateNotificationRemovedTag,
				CurrentVersion: "v1.2.3",
				LatestVersion:  "v1.3.0",
			},
			expected: []string{
				"The installed gh-aw compiler version v1.2.3 is no longer available as a repository tag.",
				"Update the compiler before recompiling workflows (latest release: v1.3.0).",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldStderr := os.Stderr
			r, w, err := os.Pipe()
			require.NoError(t, err, "pipe creation should succeed")
			defer r.Close()
			os.Stderr = w

			printCompileUpdateNotification(tt.notification)

			require.NoError(t, w.Close(), "pipe writer should close cleanly")
			os.Stderr = oldStderr

			var buf bytes.Buffer
			_, err = buf.ReadFrom(r)
			require.NoError(t, err, "pipe reader should capture stderr output")
			output := buf.String()

			for _, expected := range tt.expected {
				assert.Contains(t, output, expected, "output should contain expected message")
			}
		})
	}
}

func TestIsMinorVersionBehind(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		current string
		latest  string
		want    bool
	}{
		{
			name:    "explicit minor behind",
			current: "v1.2.3",
			latest:  "v1.3.0",
			want:    true,
		},
		{
			name:    "current missing minor returns false",
			current: "v1",
			latest:  "v1.1.0",
			want:    false,
		},
		{
			name:    "latest missing minor returns false",
			current: "v1.0.0",
			latest:  "v1",
			want:    false,
		},
		{
			name:    "prerelease still counts explicit minor",
			current: "v1.0.0-rc.1",
			latest:  "v1.1.0",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isMinorVersionBehind(tt.current, tt.latest))
		})
	}
}

func TestRunCompileUpdateCheckUsesHEADRequests(t *testing.T) {
	originalVersion := GetVersion()
	originalRelease := workflow.IsRelease()
	originalLatestURL := compileUpdateCheckLatestReleaseURL
	originalProbeURLFunc := compileUpdateCheckProbeURLFunc
	defer func() {
		SetVersionInfo(originalVersion)
		workflow.SetIsRelease(originalRelease)
		compileUpdateCheckLatestReleaseURL = originalLatestURL
		compileUpdateCheckProbeURLFunc = originalProbeURLFunc
	}()

	SetVersionInfo("v1.2.3")
	workflow.SetIsRelease(true)

	server, methods := newCompileUpdateCheckMethodServer(t, "v1.3.0", map[string]bool{
		"v1.2.3": true,
		"v1.3.0": true,
	})
	defer server.Close()

	compileUpdateCheckLatestReleaseURL = server.URL + "/releases/latest"
	compileUpdateCheckProbeURLFunc = func(tag string) string {
		return fmt.Sprintf("%s/raw/%s/go.mod", server.URL, tag)
	}

	notification, err := runCompileUpdateCheck(context.Background(), server.Client())
	require.NoError(t, err, "runCompileUpdateCheck should not fail")
	require.NotNil(t, notification, "runCompileUpdateCheck should return a notification")

	assert.Equal(t, []string{http.MethodHead}, methodsForPath(methods, "/releases/latest"), "latest release lookup should use HEAD")

	probeMethods := methodsForPrefix(methods, "/raw/")
	require.NotEmpty(t, probeMethods, "probe lookups should be recorded")
	for _, method := range probeMethods {
		assert.Equal(t, http.MethodHead, method, "probe lookups should use HEAD")
	}
}

func TestStartCompileUpdateCheckDoesNotBlockShutdown(t *testing.T) {
	originalClientFactory := compileUpdateCheckHTTPClientFactory
	originalGetFilePath := getCompileUpdateCheckFilePathFunc
	originalIsTerminal := compileUpdateCheckIsTerminalFunc
	originalVersion := GetVersion()
	originalRelease := workflow.IsRelease()
	defer func() {
		compileUpdateCheckHTTPClientFactory = originalClientFactory
		getCompileUpdateCheckFilePathFunc = originalGetFilePath
		compileUpdateCheckIsTerminalFunc = originalIsTerminal
		SetVersionInfo(originalVersion)
		workflow.SetIsRelease(originalRelease)
	}()

	tempDir := t.TempDir()
	SetVersionInfo("v1.2.3")
	workflow.SetIsRelease(true)
	t.Setenv("CI", "")
	t.Setenv("CONTINUOUS_INTEGRATION", "")
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("COPILOT_AGENT_SESSION_ID", "")
	t.Setenv("GH_AW_MCP_SERVER", "")
	t.Setenv(compileUpdateCheckDisableEnv, "")
	getCompileUpdateCheckFilePathFunc = func() string {
		return filepath.Join(tempDir, compileUpdateCheckFileName)
	}
	compileUpdateCheckIsTerminalFunc = func() bool {
		return true
	}

	unblockRequest := make(chan struct{}) // cleanup closes this to unblock any pending request
	requestStarted := make(chan struct{}, 1)
	requestCancelled := make(chan struct{})
	requestBudget := make(chan time.Duration, 1)
	t.Cleanup(func() {
		close(unblockRequest)
	})

	compileUpdateCheckHTTPClientFactory = func() *http.Client {
		return &http.Client{
			Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				deadline, _ := req.Context().Deadline()
				requestBudget <- time.Until(deadline)
				select {
				case requestStarted <- struct{}{}:
				default:
				}
				select {
				case <-unblockRequest:
				case <-req.Context().Done():
					close(requestCancelled)
				}
				return nil, req.Context().Err()
			}),
		}
	}

	finish := StartCompileUpdateCheck(context.Background(), false, false)
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("background update check did not start an HTTP request")
	}
	budget := <-requestBudget
	require.Positive(t, budget)
	require.LessOrEqual(t, budget, compileUpdateCheckTimeout, "all requests must share the total update-check budget")

	start := time.Now()
	finish()
	assert.Less(t, time.Since(start), 100*time.Millisecond, "finish should not wait for a background update check")
	select {
	case <-requestCancelled:
	case <-time.After(time.Second):
		t.Fatal("finish should cancel the outstanding network request")
	}
}

func TestStartCompileUpdateCheckSilentlyHandlesLockedDownNetwork(t *testing.T) {
	originalClientFactory := compileUpdateCheckHTTPClientFactory
	originalGetFilePath := getCompileUpdateCheckFilePathFunc
	originalIsTerminal := compileUpdateCheckIsTerminalFunc
	originalVersion := GetVersion()
	originalRelease := workflow.IsRelease()
	defer func() {
		compileUpdateCheckHTTPClientFactory = originalClientFactory
		getCompileUpdateCheckFilePathFunc = originalGetFilePath
		compileUpdateCheckIsTerminalFunc = originalIsTerminal
		SetVersionInfo(originalVersion)
		workflow.SetIsRelease(originalRelease)
	}()

	tempDir := t.TempDir()
	SetVersionInfo("v1.2.3")
	workflow.SetIsRelease(true)
	t.Setenv("CI", "")
	t.Setenv("CONTINUOUS_INTEGRATION", "")
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("COPILOT_AGENT_SESSION_ID", "")
	t.Setenv("GH_AW_MCP_SERVER", "")
	t.Setenv(compileUpdateCheckDisableEnv, "")
	getCompileUpdateCheckFilePathFunc = func() string {
		return filepath.Join(tempDir, compileUpdateCheckFileName)
	}
	compileUpdateCheckIsTerminalFunc = func() bool {
		return true
	}
	requestStarted := make(chan struct{}, 1)
	compileUpdateCheckHTTPClientFactory = func() *http.Client {
		return &http.Client{
			Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				select {
				case requestStarted <- struct{}{}:
				default:
				}
				return nil, context.DeadlineExceeded
			}),
		}
	}

	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err, "pipe creation should succeed")
	defer r.Close()
	os.Stderr = w
	defer func() {
		os.Stderr = oldStderr
	}()

	finish := StartCompileUpdateCheck(context.Background(), false, false)
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("background update check did not attempt its network request")
	}
	finish()

	require.NoError(t, w.Close(), "pipe writer should close cleanly")

	var buf bytes.Buffer
	_, err = buf.ReadFrom(r)
	require.NoError(t, err, "pipe reader should capture stderr output")
	assert.Empty(t, strings.TrimSpace(buf.String()), "locked-down network failures should not print user-facing output")
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newCompileUpdateCheckTestServer(t *testing.T, latestVersion string, existingTags map[string]bool) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/github/gh-aw/releases/tag/"+latestVersion, http.StatusFound)
	})
	mux.HandleFunc("/github/gh-aw/releases/tag/"+latestVersion, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("release page"))
	})
	mux.HandleFunc("/raw/", func(w http.ResponseWriter, r *http.Request) {
		tag := r.URL.Path[len("/raw/"):]
		tag = tag[:len(tag)-len("/go.mod")]
		if existingTags[tag] {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("module github.com/github/gh-aw\n"))
			return
		}
		http.NotFound(w, r)
	})

	return httptest.NewServer(mux)
}

func newCompileUpdateCheckMethodServer(t *testing.T, latestVersion string, existingTags map[string]bool) (*httptest.Server, map[string][]string) {
	t.Helper()

	var mu sync.Mutex
	methods := map[string][]string{}
	record := func(path string, method string) {
		mu.Lock()
		defer mu.Unlock()
		methods[path] = append(methods[path], method)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		record(r.URL.Path, r.Method)
		http.Redirect(w, r, "/github/gh-aw/releases/tag/"+latestVersion, http.StatusFound)
	})
	mux.HandleFunc("/github/gh-aw/releases/tag/"+latestVersion, func(w http.ResponseWriter, r *http.Request) {
		record(r.URL.Path, r.Method)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("release page"))
	})
	mux.HandleFunc("/raw/", func(w http.ResponseWriter, r *http.Request) {
		record(r.URL.Path, r.Method)
		tag := r.URL.Path[len("/raw/"):]
		tag = tag[:len(tag)-len("/go.mod")]
		if existingTags[tag] {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("module github.com/github/gh-aw\n"))
			return
		}
		http.NotFound(w, r)
	})

	return httptest.NewServer(mux), methods
}

func methodsForPath(methods map[string][]string, path string) []string {
	return slices.Clone(methods[path])
}

func methodsForPrefix(methods map[string][]string, prefix string) []string {
	var collected []string
	for path, values := range methods {
		if strings.HasPrefix(path, prefix) {
			collected = append(collected, values...)
		}
	}
	return collected
}

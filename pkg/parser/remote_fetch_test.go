//go:build !integration

package parser

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cli/go-gh/v2/pkg/api"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func withPublicAPIClient(t *testing.T, transport http.RoundTripper) {
	t.Helper()
	orig := publicAPIClient
	publicAPIClient = &http.Client{Transport: transport}
	t.Cleanup(func() {
		publicAPIClient = orig
	})
}

func installFailingGit(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	name := "git"
	content := "#!/bin/sh\nexit 1\n"
	if runtime.GOOS == "windows" {
		name = "git.bat"
		content = "@echo off\r\nexit /b 1\r\n"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("failed to write fake git executable: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestBuildCommitLookupAPIPath(t *testing.T) {
	t.Run("escapes refs containing slash", func(t *testing.T) {
		got := buildCommitLookupAPIPath("owner", "repo", "feature/github-agentic-workflows")
		want := "repos/owner/repo/commits/feature%2Fgithub-agentic-workflows"
		if got != want {
			t.Fatalf("buildCommitLookupAPIPath() = %q, want %q", got, want)
		}
	})

	t.Run("keeps plain refs readable", func(t *testing.T) {
		got := buildCommitLookupAPIPath("owner", "repo", "main")
		want := "repos/owner/repo/commits/main"
		if got != want {
			t.Fatalf("buildCommitLookupAPIPath() = %q, want %q", got, want)
		}
	})
}

func TestResolveRefToSHAAcceptsUppercaseFullSHA(t *testing.T) {
	const ref = "EA222E359276C0702A5F5203547FF9D88D0DDD76"

	sha, err := resolveRefToSHA(context.Background(), "owner", "repo", ref, "github.com")
	if err != nil {
		t.Fatalf("resolveRefToSHA() error = %v", err)
	}
	if sha != ref {
		t.Fatalf("resolveRefToSHA() = %q, want %q", sha, ref)
	}
}

func TestResolveRefToSHAViaGit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake git uses a POSIX shell")
	}
	const commitSHA = "3a2844b7e9c422d3c10d287c895573f7108da1b3"
	const tagSHA = "d746ffe35508b1917358783b479e04febd2b8f71"
	tests := []struct {
		name    string
		ref     string
		output  string
		wantErr string
	}{
		{"branch", "main", commitSHA + "\trefs/heads/main\n", ""},
		{"lightweight tag", "v1", commitSHA + "\trefs/tags/v1\n", ""},
		{"annotated tag", "v1", tagSHA + "\trefs/tags/v1\n" + commitSHA + "\trefs/tags/v1^{}\n", ""},
		{"peeled first", "v1", commitSHA + "\trefs/tags/v1^{}\n" + tagSHA + "\trefs/tags/v1\n", ""},
		{"missing ref", "missing", "", "no matching ref"},
		{"invalid peeled SHA", "v1", tagSHA + "\trefs/tags/v1\ninvalid\trefs/tags/v1^{}\n", "invalid SHA format"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			script := "#!/bin/sh\n[ \"$4\" = \"$3^{}\" ] || exit 1\nprintf '%s' '" + tt.output + "'\n"
			if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			got, err := resolveRefToSHAViaGit(t.Context(), "owner", "repo", tt.ref, "github.com")
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("resolveRefToSHAViaGit() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != commitSHA {
				t.Fatalf("resolveRefToSHAViaGit() = %q, want %q", got, commitSHA)
			}
		})
	}
}

func TestBuildContentsAPIPath(t *testing.T) {
	t.Run("escapes refs with reserved query chars", func(t *testing.T) {
		got := buildContentsAPIPath("owner", "repo", ".github/workflows/demo.md", "release+candidate#1")
		want := "repos/owner/repo/contents/.github/workflows/demo.md?ref=release%2Bcandidate%231"
		if got != want {
			t.Fatalf("buildContentsAPIPath() = %q, want %q", got, want)
		}
	})

	t.Run("keeps plain refs readable", func(t *testing.T) {
		got := buildContentsAPIPath("owner", "repo", ".github/workflows/demo.md", "main")
		want := "repos/owner/repo/contents/.github/workflows/demo.md?ref=main"
		if got != want {
			t.Fatalf("buildContentsAPIPath() = %q, want %q", got, want)
		}
	})

	t.Run("escapes path segments with reserved chars", func(t *testing.T) {
		got := buildContentsAPIPath("owner", "repo", "skills/path with spaces/file#100%.md", "main")
		want := "repos/owner/repo/contents/skills/path%20with%20spaces/file%23100%25.md?ref=main"
		if got != want {
			t.Fatalf("buildContentsAPIPath() = %q, want %q", got, want)
		}
	})
}

func TestGitFallbackRequiresNonEmptyRef(t *testing.T) {
	t.Run("all files fallback validates ref", func(t *testing.T) {
		_, err := listDirAllFilesViaGitForHost(context.Background(), "owner", "repo", "", "skills/demo", "")
		if err == nil {
			t.Fatal("expected error for empty ref")
		}
		if !strings.Contains(err.Error(), "non-empty ref") {
			t.Fatalf("expected non-empty ref error, got %q", err)
		}
	})

	t.Run("subdirs fallback validates ref", func(t *testing.T) {
		_, err := listDirSubdirsViaGitForHost(context.Background(), "owner", "repo", "   ", "skills", "")
		if err == nil {
			t.Fatal("expected error for empty ref")
		}
		if !strings.Contains(err.Error(), "non-empty ref") {
			t.Fatalf("expected non-empty ref error, got %q", err)
		}
	})
}

func TestListContentsRecursivelyWithDepth_MaxDepthGuard(t *testing.T) {
	_, err := listContentsRecursivelyWithDepth(t.Context(), nil, "owner", "repo", "main", "skills/demo/deep", 11, 10)
	if err == nil {
		t.Fatal("expected depth limit error")
	}
	if !strings.Contains(err.Error(), "maximum skill directory recursion depth exceeded") {
		t.Fatalf("expected depth limit error, got %q", err)
	}
}

type fakeCommitResolver struct {
	do func(ctx context.Context, method string, path string, body io.Reader, response any) error
}

func (f fakeCommitResolver) DoWithContext(ctx context.Context, method string, path string, body io.Reader, response any) error {
	return f.do(ctx, method, path, body, response)
}

func TestResolveRefToSHAWithFallbacks_UsesRESTClient(t *testing.T) {
	client := fakeCommitResolver{
		do: func(_ context.Context, method string, path string, body io.Reader, response any) error {
			if method != http.MethodGet {
				t.Fatalf("DoWithContext() method = %q, want %q", method, http.MethodGet)
			}
			if path != "repos/owner/repo/commits/feature%2Fbranch" {
				t.Fatalf("DoWithContext() path = %q, want %q", path, "repos/owner/repo/commits/feature%2Fbranch")
			}
			if body != nil {
				t.Fatal("DoWithContext() body should be nil for commit lookup")
			}
			resp, ok := response.(*commitLookupResponse)
			if !ok {
				t.Fatalf("response type = %T, want *commitLookupResponse", response)
			}
			resp.SHA = "0123456789abcdef0123456789abcdef01234567"
			return nil
		},
	}

	gitCalled := false
	publicCalled := false
	sha, err := resolveRefToSHAWithFallbacks(
		context.Background(),
		client,
		"owner",
		"repo",
		"feature/branch",
		"github.com",
		func(context.Context, string, string, string, string) (string, error) {
			gitCalled = true
			return "", nil
		},
		func(context.Context, string, string, string) (string, error) {
			publicCalled = true
			return "", nil
		},
	)
	if err != nil {
		t.Fatalf("resolveRefToSHAWithFallbacks() error = %v", err)
	}
	if sha != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("resolveRefToSHAWithFallbacks() SHA = %q", sha)
	}
	if gitCalled {
		t.Fatal("git fallback should not run on successful REST lookup")
	}
	if publicCalled {
		t.Fatal("public fallback should not run on successful REST lookup")
	}
}

func TestResolveRefToSHAWithFallbacks_UsesGitFallbackOnAuthError(t *testing.T) {
	client := fakeCommitResolver{
		do: func(_ context.Context, method string, path string, body io.Reader, response any) error {
			return &api.HTTPError{StatusCode: http.StatusUnauthorized, Message: "Unauthorized"}
		},
	}

	publicCalled := false
	sha, err := resolveRefToSHAWithFallbacks(
		context.Background(),
		client,
		"owner",
		"repo",
		"main",
		"github.com",
		func(context.Context, string, string, string, string) (string, error) {
			return "0123456789abcdef0123456789abcdef01234567", nil
		},
		func(context.Context, string, string, string) (string, error) {
			publicCalled = true
			return "", nil
		},
	)
	if err != nil {
		t.Fatalf("resolveRefToSHAWithFallbacks() error = %v", err)
	}
	if sha != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("resolveRefToSHAWithFallbacks() SHA = %q", sha)
	}
	if publicCalled {
		t.Fatal("public fallback should not run when git fallback succeeds")
	}
}

func TestResolveRefToSHAWithFallbacks_UsesPublicFallbackAfterGitFallbackOnGitHubDotCom(t *testing.T) {
	client := fakeCommitResolver{
		do: func(_ context.Context, method string, path string, body io.Reader, response any) error {
			return &api.HTTPError{StatusCode: http.StatusForbidden, Message: "Forbidden"}
		},
	}

	gitCalled := false
	publicCalled := false
	sha, err := resolveRefToSHAWithFallbacks(
		context.Background(),
		client,
		"owner",
		"repo",
		"main",
		"",
		func(context.Context, string, string, string, string) (string, error) {
			gitCalled = true
			return "", errors.New("git fallback failed")
		},
		func(context.Context, string, string, string) (string, error) {
			publicCalled = true
			return "fedcba9876543210fedcba9876543210fedcba98", nil
		},
	)
	if err != nil {
		t.Fatalf("resolveRefToSHAWithFallbacks() error = %v", err)
	}
	if sha != "fedcba9876543210fedcba9876543210fedcba98" {
		t.Fatalf("resolveRefToSHAWithFallbacks() SHA = %q", sha)
	}
	if !gitCalled {
		t.Fatal("git fallback should run on GitHub API auth errors")
	}
	if !publicCalled {
		t.Fatal("public fallback should run when git fallback also fails on github.com")
	}
}

func TestResolveRefToSHAWithFallbacks_DoesNotUsePublicFallbackOnEnterpriseHost(t *testing.T) {
	tests := []struct {
		name string
		host string
		env  string
	}{
		{
			name: "resolved from environment",
			host: "",
			env:  "enterprise.example.com",
		},
		{
			name: "explicit enterprise host",
			host: "enterprise.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GITHUB_SERVER_URL", "")
			t.Setenv("GITHUB_ENTERPRISE_HOST", "")
			t.Setenv("GITHUB_HOST", "")
			t.Setenv("GH_HOST", tt.env)

			client := fakeCommitResolver{
				do: func(_ context.Context, method string, path string, body io.Reader, response any) error {
					return &api.HTTPError{StatusCode: http.StatusForbidden, Message: "Forbidden"}
				},
			}

			publicCalled := false
			_, err := resolveRefToSHAWithFallbacks(
				context.Background(),
				client,
				"owner",
				"repo",
				"main",
				tt.host,
				func(context.Context, string, string, string, string) (string, error) {
					return "", errors.New("git fallback failed")
				},
				func(context.Context, string, string, string) (string, error) {
					publicCalled = true
					return "", nil
				},
			)
			if err == nil {
				t.Fatal("expected enterprise host auth failure when both API and git fallback fail")
			}
			if publicCalled {
				t.Fatal("public fallback must not run when resolved host is enterprise")
			}
		})
	}
}

func TestCanUseUnauthenticatedPublicGitHubFallback(t *testing.T) {
	t.Run("explicit github.com host", func(t *testing.T) {
		if !canUseUnauthenticatedPublicGitHubFallback("owner", "repo", "github.com") {
			t.Fatal("expected public fallback to be allowed for explicit github.com host")
		}
	})

	t.Run("resolved public org host override", func(t *testing.T) {
		t.Setenv("GITHUB_SERVER_URL", "")
		t.Setenv("GITHUB_ENTERPRISE_HOST", "")
		t.Setenv("GITHUB_HOST", "")
		t.Setenv("GH_HOST", "enterprise.example.com")
		if !canUseUnauthenticatedPublicGitHubFallback("github", "gh-aw", "") {
			t.Fatal("expected public fallback for repos explicitly mapped to github.com")
		}
	})

	t.Run("resolved enterprise host", func(t *testing.T) {
		t.Setenv("GITHUB_SERVER_URL", "")
		t.Setenv("GITHUB_ENTERPRISE_HOST", "")
		t.Setenv("GITHUB_HOST", "")
		t.Setenv("GH_HOST", "enterprise.example.com")
		if canUseUnauthenticatedPublicGitHubFallback("owner", "repo", "") {
			t.Fatal("public fallback must not be allowed when resolved host is enterprise")
		}
	})

	t.Run("unresolved empty host", func(t *testing.T) {
		orig := getGitHubHostForRepoFunc
		getGitHubHostForRepoFunc = func(owner, repo string) string {
			return ""
		}
		t.Cleanup(func() {
			getGitHubHostForRepoFunc = orig
		})

		if canUseUnauthenticatedPublicGitHubFallback("owner", "repo", "") {
			t.Fatal("public fallback must not be allowed when host resolution is empty")
		}
	})
}

func TestResolveRefToSHA_ClientCreationAuthError_UsesGitFallback(t *testing.T) {
	origFactory := createRESTClientForHostFunc
	origGit := resolveRefToSHAViaGitFunc
	t.Cleanup(func() {
		createRESTClientForHostFunc = origFactory
		resolveRefToSHAViaGitFunc = origGit
	})

	authErr := errors.New("unauthorized: authentication required")
	createRESTClientForHostFunc = func(host string) (*api.RESTClient, error) {
		return nil, authErr
	}
	gitCalled := false
	resolveRefToSHAViaGitFunc = func(_ context.Context, owner, repo, ref, host string) (string, error) {
		gitCalled = true
		return "aabbccddeeff00112233445566778899aabbccdd", nil
	}

	sha, err := resolveRefToSHA(context.Background(), "owner", "repo", "main", "github.com")
	if err != nil {
		t.Fatalf("resolveRefToSHA() unexpected error = %v", err)
	}
	if sha != "aabbccddeeff00112233445566778899aabbccdd" {
		t.Fatalf("resolveRefToSHA() SHA = %q, want git-fallback SHA", sha)
	}
	if !gitCalled {
		t.Fatal("git ls-remote fallback should be invoked when client creation returns an auth error")
	}
}

func TestResolveRefToSHA_ClientCreationAuthError_GithubDotCom_UsesPublicAPIWhenGitFails(t *testing.T) {
	origFactory := createRESTClientForHostFunc
	origGit := resolveRefToSHAViaGitFunc
	t.Cleanup(func() {
		createRESTClientForHostFunc = origFactory
		resolveRefToSHAViaGitFunc = origGit
	})

	createRESTClientForHostFunc = func(host string) (*api.RESTClient, error) {
		return nil, errors.New("unauthorized: authentication required")
	}
	resolveRefToSHAViaGitFunc = func(_ context.Context, _, _, _, _ string) (string, error) {
		return "", errors.New("git ls-remote failed")
	}

	publicRequests := 0
	withPublicAPIClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		publicRequests++
		if req.URL.Host != "api.github.com" {
			t.Fatalf("public fallback used host %q, want api.github.com", req.URL.Host)
		}
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader(`{"message":"not found"}`)),
			Header:     make(http.Header),
		}, nil
	}))

	_, err := resolveRefToSHA(context.Background(), "owner", "repo", "main", "github.com")
	if err != nil && strings.Contains(err.Error(), "failed to create GitHub REST client") {
		t.Fatal("client creation auth error should not bubble up directly — public API fallback must be attempted first")
	}
	if publicRequests != 1 {
		t.Fatalf("public fallback request count = %d, want 1", publicRequests)
	}
}

func TestResolveRefToSHA_ClientCreationAuthError_EnterpriseHostDoesNotUsePublicAPIWhenGitFails(t *testing.T) {
	origFactory := createRESTClientForHostFunc
	origGit := resolveRefToSHAViaGitFunc
	t.Cleanup(func() {
		createRESTClientForHostFunc = origFactory
		resolveRefToSHAViaGitFunc = origGit
	})

	createRESTClientForHostFunc = func(host string) (*api.RESTClient, error) {
		return nil, errors.New("unauthorized: authentication required")
	}
	resolveRefToSHAViaGitFunc = func(_ context.Context, _, _, _, _ string) (string, error) {
		return "", errors.New("git ls-remote failed")
	}

	publicRequests := 0
	withPublicAPIClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		publicRequests++
		return nil, errors.New("public API should not be contacted")
	}))

	_, err := resolveRefToSHA(context.Background(), "owner", "repo", "main", "enterprise.example.com")
	if err == nil {
		t.Fatal("expected enterprise host auth failure when REST client creation and git fallback fail")
	}
	if publicRequests != 0 {
		t.Fatalf("public fallback request count = %d, want 0", publicRequests)
	}
}

func TestDownloadFileFromGitHub_ForbiddenUsesUnauthenticatedRawURL(t *testing.T) {
	for _, filePath := range []string{".github/aw/instructions.md", ".github/skills/agentic-workflows/SKILL.md"} {
		for _, clientCreationFailure := range []bool{false, true} {
			for _, apiErr := range []error{
				&api.HTTPError{StatusCode: http.StatusForbidden, Message: "Forbidden"},
				errors.New("gh: Resource protected by organization SAML enforcement (HTTP 403)"),
				errors.New("gh: forbidden (HTTP 403)"),
			} {
				t.Run(fmt.Sprintf("%s/clientCreationFailure=%t/%s", filePath, clientCreationFailure, apiErr), func(t *testing.T) {
					originalFactory := createRESTClientForHostFunc
					originalFetch := fetchRemoteFileContentFunc
					t.Cleanup(func() {
						createRESTClientForHostFunc = originalFactory
						fetchRemoteFileContentFunc = originalFetch
					})
					createRESTClientForHostFunc = func(string) (*api.RESTClient, error) {
						if clientCreationFailure {
							return nil, apiErr
						}
						return &api.RESTClient{}, nil
					}
					fetchRemoteFileContentFunc = func(context.Context, *api.RESTClient, string, string, string, string, any) error {
						return apiErr
					}
					installFailingGit(t)
					t.Setenv("GH_TOKEN", "test-token-not-for-target-org")
					rawRequests := 0
					withPublicAPIClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
						rawRequests++
						wantURL := "https://raw.githubusercontent.com/github/gh-aw/release/test/" + filePath
						if req.URL.String() != wantURL {
							t.Fatalf("request URL = %q, want %q", req.URL, wantURL)
						}
						if req.Header.Get("Authorization") != "" {
							t.Fatal("raw fallback must not send credentials")
						}
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       io.NopCloser(strings.NewReader("# Public instructions\n")),
							Header:     make(http.Header),
						}, nil
					}))

					content, err := DownloadFileFromGitHubForHost(context.Background(), "github", "gh-aw", filePath, "release/test", "github.com")
					if err != nil {
						t.Fatalf("download failed: %v", err)
					}
					if string(content) != "# Public instructions\n" {
						t.Fatalf("unexpected content: %q", content)
					}
					if rawRequests != 1 {
						t.Fatalf("raw request count = %d, want 1", rawRequests)
					}
				})
			}
		}
	}
}

func TestDownloadFileFromGitHub_ForbiddenFallbackFailure(t *testing.T) {
	originalFactory := createRESTClientForHostFunc
	originalFetch := fetchRemoteFileContentFunc
	t.Cleanup(func() {
		createRESTClientForHostFunc = originalFactory
		fetchRemoteFileContentFunc = originalFetch
	})
	createRESTClientForHostFunc = func(string) (*api.RESTClient, error) {
		return &api.RESTClient{}, nil
	}
	fetchRemoteFileContentFunc = func(context.Context, *api.RESTClient, string, string, string, string, any) error {
		return &api.HTTPError{StatusCode: http.StatusForbidden, Message: "Forbidden"}
	}
	installFailingGit(t)
	var hosts []string
	withPublicAPIClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		hosts = append(hosts, req.URL.Host)
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader(`{"message":"not found"}`)),
			Header:     make(http.Header),
		}, nil
	}))

	content, err := DownloadFileFromGitHubForHost(context.Background(), "github", "gh-aw", ".github/aw/instructions.md", "main", "github.com")
	if err == nil || content != nil {
		t.Fatalf("expected download failure, got content %q and error %v", content, err)
	}
	if got := strings.Join(hosts, ","); got != "raw.githubusercontent.com,api.github.com" {
		t.Fatalf("fallback request hosts = %q", got)
	}
}

func TestDownloadFileFromGitHubWithDepth_AuthErrorOnEnterpriseHostDoesNotUsePublicAPI(t *testing.T) {
	origFactory := createRESTClientForHostFunc
	origFetch := fetchRemoteFileContentFunc
	origGit := downloadFileViaGitFunc
	t.Cleanup(func() {
		createRESTClientForHostFunc = origFactory
		fetchRemoteFileContentFunc = origFetch
		downloadFileViaGitFunc = origGit
	})

	createRESTClientForHostFunc = func(host string) (*api.RESTClient, error) {
		return &api.RESTClient{}, nil
	}
	fetchRemoteFileContentFunc = func(ctx context.Context, client *api.RESTClient, owner, repo, path, ref string, fileContent any) error {
		return &api.HTTPError{StatusCode: http.StatusForbidden, Message: "Forbidden"}
	}
	downloadFileViaGitFunc = func(context.Context, string, string, string, string, string) ([]byte, error) {
		return nil, errors.New("git fallback failed")
	}

	publicRequests := 0
	withPublicAPIClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		publicRequests++
		return nil, errors.New("public API should not be contacted")
	}))

	_, err := downloadFileFromGitHubWithDepth(context.Background(), "owner", "repo", ".github/workflows/demo.md", "main", 0, "enterprise.example.com")
	if err == nil {
		t.Fatal("expected enterprise host auth failure when API and git fallback fail")
	}
	if publicRequests != 0 {
		t.Fatalf("public fallback request count = %d, want 0", publicRequests)
	}
}

func TestDownloadFileViaGit_EnterpriseHostDoesNotUseRawGitHubusercontent(t *testing.T) {
	installFailingGit(t)

	rawRequests := 0
	withPublicAPIClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "raw.githubusercontent.com" {
			rawRequests++
		}
		return nil, errors.New("raw.githubusercontent.com should not be contacted")
	}))

	_, err := downloadFileViaGit(context.Background(), "owner", "repo", ".github/workflows/demo.md", "main", "enterprise.example.com")
	if err == nil {
		t.Fatal("expected git fallback to fail after raw URL fallback is denied")
	}
	if rawRequests != 0 {
		t.Fatalf("raw.githubusercontent.com request count = %d, want 0", rawRequests)
	}
}

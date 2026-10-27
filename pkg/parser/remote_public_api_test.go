//go:build !integration

package parser

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/ctxutil"
)

func TestPublicAPIFallbackResponses(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	for _, tt := range []struct {
		name      string
		commit    bool
		status    int
		body      string
		want      string
		wantError string
	}{
		{"contents", false, http.StatusOK, `{"content":"aGVsbG8="}`, `{"content":"aGVsbG8="}`, ""},
		{"contents status error", false, http.StatusNotFound, " not found \n", "", "GitHub API returned 404: not found"},
		{"commit", true, http.StatusOK, `{"sha":"` + sha + `"}`, sha, ""},
		{"commit status error", true, http.StatusForbidden, " rate limited \n", "", "unauthenticated public API failed for owner/repo@feature/branch: HTTP 403: rate limited"},
		{"invalid SHA", true, http.StatusOK, `{"sha":"invalid"}`, "", `invalid SHA returned from public API: "invalid"`},
		{"invalid JSON", true, http.StatusOK, `{`, "", "failed to parse commit response: unexpected end of JSON input"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			completed := false
			var reportedErr error
			ctx := ctxutil.WithOperationReporter(t.Context(), func(operation string) func(error) {
				want := "GitHub public GET owner/repo/contents/nested/file name.md@feature/branch"
				if tt.commit {
					want = "GitHub public GET owner/repo/commits/feature/branch"
				}
				if operation != want {
					t.Errorf("operation = %q, want %q", operation, want)
				}
				return func(err error) {
					completed = true
					reportedErr = err
				}
			})
			withPublicAPIClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
				wantURL := "https://api.github.com/repos/owner/repo/contents/nested/file%20name.md?ref=feature%2Fbranch"
				if tt.commit {
					wantURL = "https://api.github.com/repos/owner/repo/commits/feature%2Fbranch"
				}
				if req.URL.String() != wantURL {
					t.Errorf("URL = %s, want %s", req.URL, wantURL)
				}
				if req.Header.Get("Accept") != "application/vnd.github+json" || req.Header.Get("Authorization") != "" {
					t.Errorf("unexpected public request headers: %v", req.Header)
				}
				return &http.Response{
					StatusCode: tt.status,
					Body:       io.NopCloser(strings.NewReader(tt.body)),
				}, nil
			}))

			var got string
			var err error
			if tt.commit {
				got, err = resolveRefToSHAViaPublicAPI(ctx, "owner", "repo", "feature/branch")
			} else {
				var body []byte
				body, err = fetchPublicGitHubContentsAPI(ctx, "owner", "repo", "nested/file name.md", "feature/branch")
				got = string(body)
			}
			if !completed || !errors.Is(reportedErr, err) {
				t.Errorf("operation completion = %v, %v; want true, %v", completed, reportedErr, err)
			}
			if tt.wantError != "" {
				if err == nil || err.Error() != tt.wantError {
					t.Fatalf("error = %v, want %q", err, tt.wantError)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("result = %q, want %q", got, tt.want)
			}
		})
	}
}

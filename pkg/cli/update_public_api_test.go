//go:build !integration

package cli

import (
	"net/http"
	"testing"

	"github.com/github/gh-aw/pkg/ctxutil"
	"github.com/stretchr/testify/require"
)

func TestFetchPublicGitHubAPI(t *testing.T) {
	original := updatePublicAPIClient
	t.Cleanup(func() { updatePublicAPIClient = original })

	for _, tt := range []struct {
		name   string
		status int
		body   string
		err    string
	}{
		{"success", http.StatusOK, `{"default_branch":"main"}`, ""},
		{"status error", http.StatusForbidden, " rate limited \n", "GitHub API returned 403: rate limited"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const endpoint = "/repos/owner/repo/contents/nested/file%20name.md?ref=feature%2Fbranch"
			completed := false
			var reportedErr error
			ctx := ctxutil.WithOperationReporter(t.Context(), func(operation string) func(error) {
				require.Equal(t, "GitHub public GET "+endpoint, operation)
				return func(err error) {
					completed = true
					reportedErr = err
				}
			})
			updatePublicAPIClient = &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				require.Equal(t, "https://api.github.com"+endpoint, req.URL.String())
				require.Equal(t, "application/vnd.github+json", req.Header.Get("Accept"))
				require.Empty(t, req.Header.Get("Authorization"))
				return updateGitHubTestResponse(req, tt.status, tt.body, nil), nil
			})}

			body, err := fetchPublicGitHubAPI(ctx, endpoint)
			require.True(t, completed, "operation completion must be reported")
			require.Equal(t, err, reportedErr)
			if tt.err != "" {
				require.EqualError(t, err, tt.err)
				require.Nil(t, body)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.body, string(body))
			}
		})
	}
}

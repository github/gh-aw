//go:build !integration

package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeBase64FileContent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    func() string // build the raw API-style input
		expected string
		wantErr  bool
	}{
		{
			name: "plain base64 without newlines",
			input: func() string {
				return base64.StdEncoding.EncodeToString([]byte("hello world"))
			},
			expected: "hello world",
		},
		{
			name: "GitHub API style with embedded newlines every 60 chars",
			input: func() string {
				encoded := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("hello world", 10)))
				// Simulate GitHub API line-wrapping at 60 characters
				var sb strings.Builder
				for i, c := range encoded {
					if i > 0 && i%60 == 0 {
						sb.WriteByte('\n')
					}
					sb.WriteRune(c)
				}
				return sb.String()
			},
			expected: strings.Repeat("hello world", 10),
		},
		{
			name:     "CRLF wrapped content",
			input:    func() string { return "aGVs\r\nbG8gd29y\r\nbGQ=\r\n" },
			expected: "hello world",
		},
		{
			name:     "embedded Unicode and ASCII whitespace stripped",
			input:    func() string { return " \taG\u00a0Vs\tbG8g\vd29y\fbGQ=\u2003\n" },
			expected: "hello world",
		},
		{
			name:     "empty content",
			input:    func() string { return "" },
			expected: "",
		},
		{
			name:     "whitespace only content",
			input:    func() string { return " \t\r\n\u2003" },
			expected: "",
		},
		{
			name: "leading and trailing whitespace stripped",
			input: func() string {
				return "  " + base64.StdEncoding.EncodeToString([]byte("trim me")) + "\n"
			},
			expected: "trim me",
		},
		{
			name: "binary content round-trips correctly",
			input: func() string {
				data := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE}
				return base64.StdEncoding.EncodeToString(data)
			},
			expected: string([]byte{0x00, 0x01, 0x02, 0xFF, 0xFE}),
		},
		{
			name:    "invalid base64 returns error",
			input:   func() string { return "!!!not-valid-base64!!!" },
			wantErr: true,
		},
		{
			name:    "truncated base64 returns error",
			input:   func() string { return "aGVsbG8" },
			wantErr: true,
		},
		{
			name:    "invalid suffix discards partial content",
			input:   func() string { return "aGVs!!!!" },
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := decodeBase64FileContent(tt.input())
			if tt.wantErr {
				require.ErrorContains(t, err, "failed to decode file content")
				var corruptInput base64.CorruptInputError
				require.ErrorAs(t, err, &corruptInput)
				assert.Nil(t, got)
				return
			}
			require.NoError(t, err, "unexpected error decoding base64 content")
			assert.Equal(t, tt.expected, string(got), "decoded content should match expected")
		})
	}
}

func TestDownloadWorkflowContentDecodesContentsResponse(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "wrapped whitespace", raw: " \taG Vs\r\nbG8=\u2003\n"},
		{name: "malformed", raw: "aGVs!!!!", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, ctx := newUpdateGitHubTestClient(t, func(req *http.Request) (*http.Response, error) {
				assert.Equal(t, "/repos/owner/repo/contents/workflow.md", req.URL.Path)
				assert.Equal(t, "feature/one", req.URL.Query().Get("ref"))
				body, err := json.Marshal(map[string]string{"content": tt.raw})
				require.NoError(t, err)
				return updateGitHubTestResponse(req, http.StatusOK, string(body), nil), nil
			})

			content, err := downloadWorkflowContent(ctx, "owner/repo", "workflow.md", "feature/one", false)
			if tt.wantErr {
				require.ErrorContains(t, err, "failed to decode file content")
				assert.Nil(t, content)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "hello", string(content))
		})
	}
}

// TestDownloadWorkflowContentViaGitValidation ensures that downloadWorkflowContentViaGit
// rejects dangerous ref/path values before spawning any git subprocess (CWE-88).
func TestDownloadWorkflowContentViaGitValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		repo        string
		path        string
		ref         string
		errContains string
	}{
		{
			name:        "path starting with dash is rejected",
			repo:        "owner/repo",
			path:        "--output=/tmp/evil",
			ref:         "abc123",
			errContains: "refusing git fallback",
		},
		{
			name:        "ref starting with dash is rejected",
			repo:        "owner/repo",
			path:        "workflow.md",
			ref:         "--upload-pack=evil",
			errContains: "refusing git fallback",
		},
		{
			name:        "path with traversal is rejected",
			repo:        "owner/repo",
			path:        "../../../etc/passwd",
			ref:         "abc123",
			errContains: "refusing git fallback",
		},
		{
			name:        "ref with dotdot is rejected",
			repo:        "owner/repo",
			path:        "workflow.md",
			ref:         "main..evil",
			errContains: "refusing git fallback",
		},
		{
			name:        "empty ref is rejected",
			repo:        "owner/repo",
			path:        "workflow.md",
			ref:         "",
			errContains: "refusing git fallback",
		},
		{
			name:        "empty path is rejected",
			repo:        "owner/repo",
			path:        "",
			ref:         "abc123",
			errContains: "refusing git fallback",
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := downloadWorkflowContentViaGit(ctx, tt.repo, tt.path, tt.ref, false)
			require.Error(t, err, "expected validation error for invalid input")
			assert.Contains(t, err.Error(), tt.errContains)
		})
	}
}

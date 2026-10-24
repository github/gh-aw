//go:build !integration

package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUpdateWorkflowReusesDownloads(t *testing.T) {
	originalResolve, originalDownload := resolveLatestRefFn, downloadWorkflowContentFn
	t.Cleanup(func() {
		resolveLatestRefFn, downloadWorkflowContentFn = originalResolve, originalDownload
	})

	for _, tt := range []struct {
		name      string
		latestRef string
		force     bool
		wantBase  int
		wantNew   int
	}{
		{"already current", "v1.0.0", false, 1, 0},
		{"merging local changes", "v1.1.0", false, 1, 1},
		{"forced same ref", "v1.0.0", true, 1, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "example.md")
			const source = "owner/repo/workflows/example.md@v1.0.0"
			const base = "---\non: workflow_dispatch\n---\n\n# Example\n\nBase content.\n"
			local := strings.Replace(base, "on: workflow_dispatch", "on: workflow_dispatch\nsource: "+source+"\npermissions:\n  contents: read", 1)
			require.NoError(t, os.WriteFile(path, []byte(local), 0o600))
			resolveLatestRefFn = func(context.Context, string, string, bool, bool, time.Duration) (latestRefResolution, error) {
				return latestRefResolution{Ref: tt.latestRef}, nil
			}
			calls := make(map[string]int)
			downloadWorkflowContentFn = func(_ context.Context, repo, path, ref string, _ bool) ([]byte, error) {
				require.Equal(t, "owner/repo", repo)
				require.Equal(t, "workflows/example.md", path)
				calls[ref]++
				switch ref {
				case "v1.0.0":
					return []byte(base), nil
				case "v1.1.0":
					return []byte(strings.Replace(base, "Base content.", "Upstream content.", 1)), nil
				default:
					return nil, fmt.Errorf("unexpected ref %s", ref)
				}
			}

			err := updateWorkflow(context.Background(), &workflowWithSource{
				Name: "example", Path: path, SourceSpec: source,
			}, UpdateWorkflowsOptions{NoCompile: true, Force: tt.force})
			require.NoError(t, err)
			require.Equal(t, tt.wantBase, calls["v1.0.0"])
			require.Equal(t, tt.wantNew, calls["v1.1.0"])
			content, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Contains(t, string(content), "contents: read", "local changes must survive")
			if tt.wantNew > 0 {
				require.Contains(t, string(content), "Upstream content.")
				require.Contains(t, string(content), "@v1.1.0")
				require.NotContains(t, string(content), "<<<<<<<")
			}
		})
	}
}

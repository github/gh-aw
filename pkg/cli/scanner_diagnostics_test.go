//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/scanfindings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEvalResultRecordsOversizedLines(t *testing.T) {
	longAnswer := strings.Repeat("x", 128*1024)
	content := fmt.Sprintf("\r\ninvalid JSON\r\n%s\r\n{\"id\":\"large\",\"answer\":%q,\"runid\":\"1\"}\r\n{\"id\":\"next\",\"answer\":\"YES\",\"runid\":\"2\"}",
		longAnswer, longAnswer)

	records := parseEvalResultRecords([]byte(content))

	require.Len(t, records, 2)
	assert.Equal(t, evalResultRecord{ID: "large", Answer: longAnswer, RunID: "1"}, records[0])
	assert.Equal(t, evalResultRecord{ID: "next", Answer: "YES", RunID: "2"}, records[1])
}

func TestProcessIncludesPreservesLinesWithoutScannerLimit(t *testing.T) {
	workflow := &WorkflowSpec{RepoSpec: RepoSpec{RepoSlug: "owner/repo", Version: "main"}}
	packageDir := t.TempDir()
	processors := map[string]func(string) (string, error){
		"package": func(content string) (string, error) {
			return processIncludesWithWorkflowSpec(content, workflow, "", packageDir, "", false)
		},
		"update": func(content string) (string, error) {
			return processIncludesInContent(content, workflow, "", "", false)
		},
	}
	longLine := strings.Repeat("x", 128*1024)
	for name, process := range processors {
		t.Run(name, func(t *testing.T) {
			for _, tt := range []struct {
				name    string
				content string
				want    string
			}{
				{name: "empty"},
				{name: "blank lines", content: "\n\n", want: "\n\n"},
				{name: "no final newline", content: "text", want: "text\n"},
				{name: "CRLF", content: "text\r\n\r\nlast\r", want: "text\n\nlast\n"},
				{
					name:    "oversized before import",
					content: longLine + "\r\n@include? shared/next.md\r\nafter",
					want:    longLine + "\n{{#import? owner/repo/shared/next.md@main}}\nafter\n",
				},
			} {
				t.Run(tt.name, func(t *testing.T) {
					got, err := process(tt.content)
					require.NoError(t, err)
					assert.Equal(t, tt.want, got)
				})
			}
		})
	}
}

func TestFetchRemoteIncludesAfterOversizedLines(t *testing.T) {
	longLine := strings.Repeat("x", 128*1024)
	files := map[string]string{
		"parent.md": longLine + "\r\n@include child.md\r\n",
		"child.md":  "child content",
	}
	var fetched []string
	targetDir := t.TempDir()
	err := fetchAndSaveRemoteIncludesWithOptions(t.Context(), longLine+"\r\n@include parent.md", includesFetchOptions{
		spec:      &WorkflowSpec{RepoSpec: RepoSpec{RepoSlug: "owner/repo", Version: "main"}},
		targetDir: targetDir,
		strict:    true,
		fetchFn: func(_ context.Context, path string, _ *WorkflowSpec, _ bool) ([]byte, string, error) {
			fetched = append(fetched, path)
			content, ok := files[path]
			if !ok {
				return nil, "", fmt.Errorf("unexpected include: %s", path)
			}
			return []byte(content), "", nil
		},
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"parent.md", "child.md"}, fetched)
	for path, want := range files {
		content, err := os.ReadFile(filepath.Join(targetDir, path))
		require.NoError(t, err)
		assert.Equal(t, want, string(content))
	}
}

func TestZizmorCompletedOrderAfterOversizedLine(t *testing.T) {
	var findings []zizmorFinding
	for _, path := range []string{"a.lock.yml", "z.lock.yml"} {
		var finding zizmorFinding
		finding.Ident = "excessive-permissions"
		finding.Desc = "overly broad permissions"
		finding.Determinations.Severity = "Medium"
		var location zizmorLocation
		location.Symbolic.Key.Local.GivenPath = path
		finding.Locations = []zizmorLocation{location}
		findings = append(findings, finding)
	}
	stdout, err := json.Marshal(findings)
	require.NoError(t, err)
	stderr := strings.Repeat("x", 128*1024) + "\r\n INFO audit: zizmor: 🌈 completed z.lock.yml\r\n INFO audit: zizmor: 🌈 completed a.lock.yml"
	var diagnostics []scanfindings.Finding

	warnings, high, err := parseAndDisplayZizmorOutputWithOptions(string(stdout), stderr, false, zizmorResultOptions{findings: &diagnostics})

	require.NoError(t, err)
	assert.Equal(t, 2, warnings)
	assert.Zero(t, high)
	require.Len(t, diagnostics, 2)
	assert.Equal(t, "z.lock.yml", diagnostics[0].File)
	assert.Equal(t, "a.lock.yml", diagnostics[1].File)
}

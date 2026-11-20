//go:build !integration

package parser

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRelativizeIncludedFilePath(t *testing.T) {
	tests := []struct {
		name     string
		baseDir  string
		repoRoot string
		filePath string
		want     string
	}{
		{
			name:     "file under baseDir returns baseDir-relative slash path",
			baseDir:  filepath.FromSlash("/repo/workflows"),
			repoRoot: filepath.FromSlash("/repo"),
			filePath: filepath.FromSlash("/repo/workflows/shared/a.md"),
			want:     "shared/a.md",
		},
		{
			name:     "file equal to baseDir returns dot",
			baseDir:  filepath.FromSlash("/repo/workflows"),
			repoRoot: filepath.FromSlash("/repo"),
			filePath: filepath.FromSlash("/repo/workflows"),
			want:     ".",
		},
		{
			name:     "file outside baseDir but under repoRoot returns repoRoot-relative slash path",
			baseDir:  filepath.FromSlash("/repo/workflows"),
			repoRoot: filepath.FromSlash("/repo"),
			filePath: filepath.FromSlash("/repo/.github/shared/b.md"),
			want:     ".github/shared/b.md",
		},
		{
			name:     "file outside both baseDir and repoRoot returns slash-converted absolute path",
			baseDir:  filepath.FromSlash("/repo/workflows"),
			repoRoot: filepath.FromSlash("/repo"),
			filePath: filepath.FromSlash("/other/shared/c.md"),
			want:     "/other/shared/c.md",
		},
		{
			name:     "empty repoRoot with file outside baseDir falls back to slash-converted path",
			baseDir:  filepath.FromSlash("/repo/workflows"),
			repoRoot: "",
			filePath: filepath.FromSlash("/other/shared/d.md"),
			want:     "/other/shared/d.md",
		},
		{
			name:     "empty repoRoot with file under baseDir still resolves via baseDir",
			baseDir:  filepath.FromSlash("/repo/workflows"),
			repoRoot: "",
			filePath: filepath.FromSlash("/repo/workflows/e.md"),
			want:     "e.md",
		},
		{
			name:     "file equal to repoRoot when outside baseDir returns dot",
			baseDir:  filepath.FromSlash("/repo/workflows"),
			repoRoot: filepath.FromSlash("/repo"),
			filePath: filepath.FromSlash("/repo"),
			want:     ".",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := relativizeIncludedFilePath(tt.baseDir, tt.repoRoot, tt.filePath)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestExtractBodyLevelImportPathsReturnsScannerErrors(t *testing.T) {
	content := "{{#runtime-import tools.md}}\n" + strings.Repeat("x", 64*1024+1)
	_, err := ExtractBodyLevelImportPaths(content, "")
	require.ErrorContains(t, err, "failed to scan workflow body for runtime imports")
}

func TestProcessIncludesForFieldReturnsScannerErrors(t *testing.T) {
	content := strings.Repeat("x", 64*1024+1) + "\n@include tools.md"
	_, _, err := processIncludesForField(content, "", nil, "", nil)
	require.ErrorContains(t, err, "failed to scan content while expanding imports")
}

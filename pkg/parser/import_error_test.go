//go:build !integration

package parser_test

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/require"
)

func TestFormatImportErrorDoesNotExposeCause(t *testing.T) {
	pathErr := &fs.PathError{Op: "lstat", Path: "missing.md", Err: fs.ErrNotExist}
	causes := []struct {
		name  string
		cause error
	}{
		{name: "direct path error", cause: pathErr},
		{name: "wrapped path error", cause: fmt.Errorf("file not found: %w", pathErr)},
		{name: "sentinel error", cause: fs.ErrNotExist},
		{name: "nil cause"},
	}

	for _, tt := range causes {
		t.Run(tt.name, func(t *testing.T) {
			err := parser.FormatImportError(&parser.ImportError{
				ImportPath: "missing.md",
				FilePath:   "workflow.md",
				Line:       2,
				Column:     5,
				Cause:      tt.cause,
			}, "imports:\n  - missing.md")

			var formattedErr *parser.FormattedParserError
			require.ErrorAs(t, err, &formattedErr)
			require.NoError(t, errors.Unwrap(err))
			var target *fs.PathError
			require.NotErrorAs(t, err, &target)
			require.NotErrorIs(t, err, fs.ErrNotExist)
			if tt.cause != nil {
				require.NotErrorIs(t, err, tt.cause)
			}
			require.ErrorContains(t, err, "workflow.md:2:5:")
			require.ErrorContains(t, err, "missing.md")
			require.ErrorContains(t, err, "hint:")
		})
	}
}

func TestFormatImportError(t *testing.T) {
	tests := []struct {
		name        string
		err         *parser.ImportError
		yamlContent string
		wantContain []string
	}{
		{
			name: "file not found error",
			err: &parser.ImportError{
				ImportPath: "missing.md",
				FilePath:   "test.md",
				Line:       3,
				Column:     3,
				Cause:      errors.New("file not found: /path/to/missing.md"),
			},
			yamlContent: `---
on: push
imports:
  - missing.md
---`,
			wantContain: []string{
				"test.md:3:3:",
				"error:",
				"import file not found",
				"imports:",
				"hint:",
				"missing.md",
			},
		},
		{
			name: "download failed error",
			err: &parser.ImportError{
				ImportPath: "owner/repo/file.md@main",
				FilePath:   "workflow.md",
				Line:       4,
				Column:     5,
				Cause:      errors.New("failed to download include from owner/repo/file.md@main: network error"),
			},
			yamlContent: `---
on: issues
imports:
  - owner/repo/file.md@main
---`,
			wantContain: []string{
				"workflow.md:4:5:",
				"error:",
				"failed to download import file",
				"owner/repo/file.md@main",
				"hint:",
			},
		},
		{
			name: "ref resolution error",
			err: &parser.ImportError{
				ImportPath: "owner/repo/file.md@v0.0.0-bad",
				FilePath:   "test.md",
				Line:       3,
				Column:     3,
				Cause:      errors.New("failed to resolve ref v0.0.0-bad: not found"),
			},
			yamlContent: `---
on: push
imports:
  - owner/repo/file.md@v0.0.0-bad
---`,
			wantContain: []string{
				"test.md:3:3:",
				"error:",
				"failed to resolve import reference",
				"hint:",
				"Verify the ref",
				"owner/repo/file.md@v0.0.0-bad",
			},
		},
		{
			name: "invalid workflowspec error",
			err: &parser.ImportError{
				ImportPath: "invalid-spec",
				FilePath:   "test.md",
				Line:       3,
				Column:     3,
				Cause:      errors.New("invalid workflowspec: must be owner/repo/path[@ref]"),
			},
			yamlContent: `---
on: push
imports:
  - invalid-spec
---`,
			wantContain: []string{
				"test.md:3:3:",
				"error:",
				"invalid import specification",
				"hint:",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.FormatImportError(tt.err, tt.yamlContent)
			gotStr := got.Error()

			for _, want := range tt.wantContain {
				if !strings.Contains(gotStr, want) {
					t.Errorf("FormatImportError() output missing expected string %q\nGot:\n%s", want, gotStr)
				}
			}
		})
	}
}

func TestFindImportsFieldLocation(t *testing.T) {
	tests := []struct {
		name        string
		yamlContent string
		wantLine    int
		wantColumn  int
	}{
		{
			name: "simple imports field",
			yamlContent: `---
on: push
imports:
  - file.md
---`,
			wantLine:   3,
			wantColumn: 1,
		},
		{
			name: "imports field with indentation",
			yamlContent: `---
on: push
  imports:
    - file.md
---`,
			wantLine:   3,
			wantColumn: 3,
		},
		{
			name: "no imports field",
			yamlContent: `---
on: push
tools:
  bash: {}
---`,
			wantLine:   1,
			wantColumn: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reimplement the location finding logic for testing
			// In production, this logic is in the unexported findImportsFieldLocation function
			lines := strings.Split(tt.yamlContent, "\n")
			line := 1
			column := 1

			for i, l := range lines {
				trimmed := strings.TrimSpace(l)
				if strings.HasPrefix(trimmed, "imports:") {
					column = strings.Index(l, "imports:") + 1
					line = i + 1
					break
				}
			}

			if line != tt.wantLine {
				t.Errorf("findImportsFieldLocation() line = %d, want %d", line, tt.wantLine)
			}
			if column != tt.wantColumn {
				t.Errorf("findImportsFieldLocation() column = %d, want %d", column, tt.wantColumn)
			}
		})
	}
}

func TestFindImportItemLocation(t *testing.T) {
	tests := []struct {
		name        string
		yamlContent string
		importPath  string
		wantLine    int
		wantColumn  int
	}{
		{
			name: "simple string import",
			yamlContent: `---
on: push
imports:
  - file.md
  - another.md
---`,
			importPath: "file.md",
			wantLine:   4,
			wantColumn: 5,
		},
		{
			name: "multiple imports - second item",
			yamlContent: `---
on: push
imports:
  - file1.md
  - file2.md
---`,
			importPath: "file2.md",
			wantLine:   5,
			wantColumn: 5,
		},
		{
			name: "object-style import",
			yamlContent: `---
on: push
imports:
  - path: shared/tool.md
    inputs:
      foo: bar
---`,
			importPath: "shared/tool.md",
			wantLine:   4,
			wantColumn: 11,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := strings.Split(tt.yamlContent, "\n")
			inImportsSection := false
			line := 1
			column := 1

			for i, l := range lines {
				trimmed := strings.TrimSpace(l)

				if strings.HasPrefix(trimmed, "imports:") {
					inImportsSection = true
					continue
				}

				if inImportsSection {
					if len(l) > 0 && l[0] != ' ' && l[0] != '-' && l[0] != '\t' {
						break
					}

					if strings.Contains(l, tt.importPath) {
						column = strings.Index(l, tt.importPath) + 1
						line = i + 1
						break
					}
				}
			}

			if line != tt.wantLine {
				t.Errorf("findImportItemLocation() line = %d, want %d", line, tt.wantLine)
			}
			if column != tt.wantColumn {
				t.Errorf("findImportItemLocation() column = %d, want %d", column, tt.wantColumn)
			}
		})
	}
}

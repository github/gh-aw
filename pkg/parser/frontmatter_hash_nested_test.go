//go:build !integration

package parser

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNestedImportHashResolution(t *testing.T) {
	type fixtureFile struct {
		Path        string `json:"path"`
		ImportPath  string `json:"importPath"`
		Frontmatter string `json:"frontmatter"`
		Body        string `json:"body"`
	}

	var fixture struct {
		Workflow    fixtureFile   `json:"workflow"`
		Imports     []fixtureFile `json:"imports"`
		Expressions []string      `json:"expressions"`
	}
	data, err := os.ReadFile("testdata/nested_import_hash.json")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &fixture))

	var paths, texts, bodies []string
	for _, file := range fixture.Imports {
		paths = append(paths, file.ImportPath)
		texts = append(texts, file.Frontmatter)
		bodies = append(bodies, file.Body)
	}
	slices.Sort(paths)
	slices.Sort(texts)
	slices.Sort(bodies)
	hash := func(text string) string {
		sum := sha256.Sum256([]byte(text))
		return hex.EncodeToString(sum[:])
	}
	expectedFrontmatter := hash(marshalSorted(map[string]any{
		"frontmatter-text":      fixture.Workflow.Frontmatter,
		"imports":               paths,
		"imported-frontmatters": strings.Join(texts, "\n---\n"),
		"template-expressions":  fixture.Expressions,
	}))
	expectedBody := hash(strings.Join(append([]string{fixture.Workflow.Body}, bodies...), "\n---\n"))

	for _, baseDir := range []string{
		".github/workflows",
		filepath.Join(t.TempDir(), ".github", "workflows"),
		filepath.Join(".github", ".github", "workflows"),
		filepath.Join(t.TempDir(), ".github", ".github", "workflows"),
	} {
		t.Run(baseDir, func(t *testing.T) {
			files := make(map[string][]byte)
			for _, file := range append([]fixtureFile{fixture.Workflow}, fixture.Imports...) {
				files[filepath.Join(baseDir, file.Path)] = []byte("---\n" + file.Frontmatter + "\n---\n" + file.Body)
			}
			reader := func(path string) ([]byte, error) {
				content, ok := files[path]
				require.True(t, ok, "unexpected import read: %s", path)
				return content, nil
			}
			compute := func() (string, string) {
				frontmatter, err := ComputeFrontmatterHashFromFileWithReader(filepath.Join(baseDir, fixture.Workflow.Path), nil, reader)
				require.NoError(t, err)
				body, err := ComputeBodyHashFromParsedContent(fixture.Workflow.Body, fixture.Workflow.Frontmatter, baseDir, reader)
				require.NoError(t, err)
				return frontmatter, body
			}
			frontmatter, body := compute()
			assert.Equal(t, expectedFrontmatter, frontmatter)
			assert.Equal(t, expectedBody, body)

			for _, file := range fixture.Imports {
				path := filepath.Join(baseDir, file.Path)
				original := files[path]
				files[path] = []byte("---\n" + file.Frontmatter + "\ndescription: changed\n---\n" + file.Body)
				changedFrontmatter, sameBody := compute()
				assert.NotEqual(t, frontmatter, changedFrontmatter, file.Path)
				assert.Equal(t, body, sameBody, file.Path)
				files[path] = append(slices.Clone(original), []byte("\nChanged ${{ vars.NESTED_CHANGE }}")...)
				changedFrontmatter, changedBody := compute()
				assert.NotEqual(t, frontmatter, changedFrontmatter, file.Path)
				assert.NotEqual(t, body, changedBody, file.Path)
				files[path] = original
			}
		})
	}
}

func TestHashImportPathsMatchNativeImports(t *testing.T) {
	for _, name := range []string{"repository", ".github"} {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), name)
			baseDir := filepath.Join(root, ".github", "workflows")
			for _, file := range []string{".github/workflows/workflow.md", ".github/workflows/shared/tool.md", ".github/agents/planner.md", ".agents/planner.md"} {
				fullPath := filepath.Join(root, filepath.FromSlash(file))
				require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0700))
				require.NoError(t, os.WriteFile(fullPath, []byte("fixture"), 0600))
			}
			for _, importPath := range []string{"workflow.md", "./workflow.md", "shared/tool.md", "../agents/planner.md", ".github/workflows/workflow.md", "/.github/workflows/workflow.md", "/.agents/planner.md"} {
				nativePath, err := ResolveIncludePath(importPath, baseDir, nil)
				require.NoError(t, err, importPath)
				hashPath, err := resolveHashImportPath(importPath, baseDir, baseDir)
				require.NoError(t, err, importPath)
				assert.Equal(t, nativePath, hashPath, importPath)
			}
			for _, importPath := range []string{".github/../../outside.md", "/.agents/../../outside.md", "/outside.md"} {
				_, err := ResolveIncludePath(importPath, baseDir, nil)
				require.ErrorContains(t, err, "security")
				_, err = resolveHashImportPath(importPath, baseDir, baseDir)
				require.ErrorContains(t, err, "security")
			}
		})
	}
}

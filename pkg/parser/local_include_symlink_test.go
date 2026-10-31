//go:build !integration && !js && !wasm

package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func setupLocalIncludeSymlinks(t *testing.T, folder string) (string, string) {
	t.Helper()
	base := filepath.Join(t.TempDir(), folder)
	outside := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(base, "workflows"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(base, "shared"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(base, "shared", "safe.md"), []byte("safe"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.md"), []byte("secret"), 0644))
	links := map[string]string{
		"relative.md": "../shared/safe.md",
		"absolute.md": filepath.Join(base, "shared", "safe.md"),
		"parent":      "../shared",
		"escape.md":   filepath.Join(outside, "secret.md"),
		"escape-dir":  outside,
		"dangling.md": filepath.Join(outside, "missing.md"),
		"dangling":    filepath.Join(outside, "missing"),
		"loop.md":     "loop.md",
	}
	for name, target := range links {
		require.NoError(t, os.Symlink(target, filepath.Join(base, "workflows", name)))
	}
	return base, outside
}

func TestResolveIncludePathSymlinks(t *testing.T) {
	base, _ := setupLocalIncludeSymlinks(t, ".github")
	workflows := filepath.Join(base, "workflows")
	for _, test := range []struct {
		path    string
		wantErr bool
	}{
		{path: "relative.md"},
		{path: "absolute.md"},
		{path: "parent/safe.md"},
		{path: "escape.md", wantErr: true},
		{path: "escape-dir/secret.md", wantErr: true},
		{path: "escape-dir/missing.md", wantErr: true},
		{path: "dangling.md", wantErr: true},
		{path: "dangling/child.md", wantErr: true},
		{path: "loop.md", wantErr: true},
	} {
		t.Run(test.path, func(t *testing.T) {
			resolved, err := ResolveIncludePath(test.path, workflows, nil)
			if test.wantErr {
				require.Error(t, err)
				require.Empty(t, resolved)
				return
			}
			require.NoError(t, err)
			require.Equal(t, filepath.Join(workflows, test.path), resolved)
			content, err := ReadFile(resolved)
			require.NoError(t, err)
			require.Equal(t, "safe", string(content))
		})
	}
}

func TestLocalIncludeReadRejectsSymlinkReplacement(t *testing.T) {
	for _, folder := range []string{".github", ".agents"} {
		t.Run(folder, func(t *testing.T) {
			for _, path := range []string{"relative.md", "parent/safe.md"} {
				t.Run(path, func(t *testing.T) {
					base, outside := setupLocalIncludeSymlinks(t, folder)
					resolved, err := resolveAndValidateLocalIncludePath(path, filepath.Join(base, "workflows"), base)
					require.NoError(t, err)
					link := filepath.Join(base, "workflows", "relative.md")
					target := filepath.Join(outside, "secret.md")
					if path == "parent/safe.md" {
						link = filepath.Join(base, "workflows", "parent")
						target = outside
						require.NoError(t, os.WriteFile(filepath.Join(outside, "safe.md"), []byte("secret"), 0644))
					}
					require.NoError(t, os.Remove(link))
					require.NoError(t, os.Symlink(target, link))
					content, err := ReadFile(resolved)
					require.Error(t, err)
					require.Empty(t, content)
					require.ErrorContains(t, err, "escapes base directory")
				})
			}
		})
	}
}

func TestLocalIncludeExpansionRejectsSymlinkEscape(t *testing.T) {
	base, _ := setupLocalIncludeSymlinks(t, ".github")
	content, _, err := ExpandIncludesWithManifest("@include escape.md", filepath.Join(base, "workflows"), false)
	require.Error(t, err)
	require.Empty(t, content)
	require.ErrorContains(t, err, "security:")

	result, err := ProcessImportsFromFrontmatterWithSource(map[string]any{"imports": []string{"escape.md"}}, filepath.Join(base, "workflows"), nil, "", "")
	require.Error(t, err)
	require.Nil(t, result)
	require.ErrorContains(t, err, "security:")
}

func TestResolveIncludePathDotAgentsSymlinks(t *testing.T) {
	base, _ := setupLocalIncludeSymlinks(t, ".agents")
	workflows := filepath.Join(filepath.Dir(base), ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflows, 0755))
	for _, path := range []string{"relative.md", "absolute.md", "parent/safe.md"} {
		t.Run(path, func(t *testing.T) {
			resolved, err := ResolveIncludePath("/.agents/workflows/"+path, workflows, nil)
			require.NoError(t, err)
			content, err := ReadFile(resolved)
			require.NoError(t, err)
			require.Equal(t, "safe", string(content))
		})
	}
	for _, path := range []string{"escape.md", "escape-dir/secret.md", "dangling.md", "dangling/child.md"} {
		t.Run(path, func(t *testing.T) {
			resolved, err := ResolveIncludePath("/.agents/workflows/"+path, workflows, nil)
			require.Error(t, err)
			require.Empty(t, resolved)
		})
	}
}

func TestLocalIncludeRejectsSymlinkedSecurityBase(t *testing.T) {
	for _, folder := range []string{".github", ".agents"} {
		t.Run(folder, func(t *testing.T) {
			base := filepath.Join(t.TempDir(), folder)
			outside := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.md"), []byte("secret"), 0644))
			require.NoError(t, os.Symlink(outside, base))
			resolved, err := resolveAndValidateLocalIncludePath("secret.md", base, base)
			require.ErrorContains(t, err, "must not be a symlink")
			require.Empty(t, resolved)
			content, err := ReadFile(filepath.Join(base, "secret.md"))
			require.ErrorContains(t, err, "is a symlink or changed while opening")
			require.Empty(t, content)
		})
	}
}

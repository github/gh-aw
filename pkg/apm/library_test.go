package apm_test

import (
	"fmt"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/apm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPMLibraryHasNoCompilerDependencies(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	for _, filename := range files {
		if strings.HasSuffix(filename, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filename, nil, parser.ImportsOnly)
		require.NoError(t, err)
		for _, spec := range file.Imports {
			name, err := strconv.Unquote(spec.Path.Value)
			require.NoError(t, err)
			assert.False(t, strings.HasPrefix(name, "github.com/github/gh-aw/"), "%s depends on compiler internals: %s", filename, name)
			assert.NotContains(t, name, "microsoft/apm", "%s depends on Microsoft APM: %s", filename, name)
		}
	}
}

func TestAPMLibraryReusableDependencyViews(t *testing.T) {
	source := []byte(`name: independent-library-consumer
version: 1.0.0
targets: [claude, copilot]
dependencies:
  apm:
    - gitlab.com/acme/prompts#v1.0.0
    - git: ssh://git@example.com/acme/repo
      path: instructions
      ref: main
      alias: " custom "
      skills: [review]
      x-vendor: {future: keep}
    - id: acme/package
      registry: enterprise
      version: ^1.0.0
    - path: ./local-package
  mcp:
    - name: server
      registry: false
      transport: stdio
      command: server
devDependencies:
  apm: [owner/dev-package#main]
scripts: {lint: "echo lint"}
policy: {hash_algorithm: sha256}
`)
	manifest, err := apm.ParseManifest(source)
	require.NoError(t, err, "the document library must not impose gh-aw's skill-only restrictions")
	dependencies := manifest.APMDependencies(false)
	require.Len(t, dependencies, 4)
	assert.Equal(t, apm.ShorthandDependency, dependencies[0].Kind)
	assert.Equal(t, "gitlab.com/acme/prompts#v1.0.0", dependencies[0].Shorthand)
	assert.Equal(t, apm.GitDependency, dependencies[1].Kind)
	assert.Equal(t, "custom", dependencies[1].Alias)
	assert.Equal(t, apm.RegistryDependency, dependencies[2].Kind)
	assert.Equal(t, apm.LocalDependency, dependencies[3].Kind)
	assert.Equal(t, []string{"claude", "copilot"}, manifest.TargetRestrictions())
	assert.Len(t, manifest.APMDependencies(true), 1)
	assert.Len(t, manifest.ServiceDependencies(false), 1)
	dependencies[1].Skills[0] = "changed"
	fields := dependencies[1].Fields()
	fields["x-vendor"] = "changed"
	assert.Equal(t, []string{"review"}, manifest.APMDependencies(false)[1].Skills)
	assert.Equal(t, map[string]any{"future": "keep"}, manifest.APMDependencies(false)[1].Field("x-vendor"))
	encoded := manifest.Bytes()
	assert.Equal(t, source, encoded)
}

func TestAPMLibraryRegistryAndLocalLockViews(t *testing.T) {
	hash := "sha256:" + strings.Repeat("a", 64)
	source := []byte("lockfile_version: '2'\ndependencies:\n" +
		"  - source: registry\n    repo_url: acme/package\n    resolved_url: https://registry.example/package.tgz\n    resolved_hash: " + hash + "\n" +
		"    deployed_files: [file.md]\n    deployed_file_hashes: {file.md: " + hash + "}\n    x-vendor: {future: keep}\n" +
		"  - source: local\n    local_path: ./local\n    content_hash: " + hash + "\n")
	lock, err := apm.ParseLockfile(source)
	require.NoError(t, err)
	entries := lock.Dependencies()
	require.Len(t, entries, 2)
	assert.Equal(t, hash, entries[0].ResolvedHash)
	assert.Equal(t, "https://registry.example/package.tgz", entries[0].ResolvedURL)
	assert.Equal(t, hash, entries[1].ContentHash)
	entries[0].DeployedFiles[0] = "changed"
	entries[0].DeployedFileHashes["file.md"] = "changed"
	vendor, ok := entries[0].Field("x-vendor").(map[string]any)
	require.True(t, ok)
	vendor["future"] = "changed"
	assert.Equal(t, []string{"file.md"}, lock.Dependencies()[0].DeployedFiles)
	assert.Equal(t, hash, lock.Dependencies()[0].DeployedFileHashes["file.md"])
	assert.Equal(t, map[string]any{"future": "keep"}, lock.Dependencies()[0].Field("x-vendor"))
	assert.Equal(t, source, lock.Bytes())
}

func ExampleParseManifest() {
	manifest, err := apm.ParseManifest([]byte("name: example\nversion: 1.0.0\ndependencies:\n  apm: [owner/repo#v1.0.0]\n"))
	if err != nil {
		panic(err)
	}
	for _, dependency := range manifest.APMDependencies(false) {
		fmt.Println(dependency.Kind, dependency.Shorthand)
	}
	// Output: shorthand owner/repo#v1.0.0
}

func TestAPMLibraryECMAScriptPatternSemantics(t *testing.T) {
	base := "name: example\nversion: 1.0.0\n"
	for _, whitespace := range []string{"\t", "\u00a0", "\u2003", "\ufeff", "\u2028"} {
		_, err := apm.ParseManifest([]byte(base + fmt.Sprintf("dependencies:\n  apm:\n    - git: %q\n", whitespace)))
		require.Error(t, err, "ECMAScript whitespace-only sources must be rejected")
		_, err = apm.ParseManifest([]byte(base + fmt.Sprintf("dependencies:\n  mcp:\n    - name: server\n      registry: false\n      transport: stdio\n      command: %q\n", "first"+whitespace+"second")))
		require.Error(t, err, "commands without explicit args must contain no ECMAScript whitespace")
		manifest, err := apm.ParseManifest([]byte(base + fmt.Sprintf("dependencies:\n  apm:\n    - git: owner/repo\n      alias: %q\n", whitespace+"tools"+whitespace)))
		require.NoError(t, err)
		assert.Equal(t, "tools", manifest.APMDependencies(false)[0].Alias)
	}
}

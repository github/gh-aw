package apm

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testCommit = "0123456789abcdef0123456789abcdef01234567"

func TestAPMManifestContract(t *testing.T) {
	valid := []string{
		"name: project\nversion: 1.0.0\n",
		"name: project\nversion: 1.0.0-beta.1+build.2\ntargets: [copilot, claude]\n",
		"name: project\nversion: 1.0.0\ndependencies:\n  apm:\n    - owner/repo/skills/review#v1.0.0\n",
		"name: project\nversion: 1.0.0\ndependencies:\n  apm:\n    - git: owner/repo\n      path: skills/review\n      ref: main\n      alias: ' .safe '\n",
		"name: project\nversion: 1.0.0\ndependencies:\n  apm:\n    - id: owner/package\n      registry: enterprise\n      version: ^1.0.0\n",
		"name: project\nversion: 1.0.0\ndependencies:\n  mcp:\n    - name: server\n      registry: !!bool false\n      transport: stdio\n      command: server\n",
		"name: project\nversion: 1.0.0\nregistries:\n  enterprise:\n    url: https://registry.example.com\n    insecure: !!bool false\n",
	}
	for _, content := range valid {
		t.Run(content, func(t *testing.T) {
			manifest, err := ParseManifest([]byte(content))
			require.NoError(t, err)
			assert.Equal(t, "project", manifest.Name)
		})
	}
	invalid := []string{
		"[]",
		"name: project\n",
		"name: ''\nversion: 1.0.0\n",
		"name: project\nversion: !!int 1\n",
		"name: project\nversion: 1.0.0\ntarget: copilot\ntargets: claude\n",
		"name: project\nversion: 1.0.0\ndependencies:\n  conflict_resolution: nest\n",
		"name: project\nversion: 1.0.0\ndependencies:\n  apm:\n    - git: owner/repo\n      id: owner/package\n",
		"name: project\nversion: 1.0.0\ndependencies:\n  apm:\n    - id: owner/package\n",
		"name: project\nversion: 1.0.0\ndependencies:\n  apm:\n    - git: owner/repo\n      alias: ..\n",
		"name: project\nversion: 1.0.0\ndependencies:\n  mcp:\n    - name: server\n      registry: !!bool false\n      transport: http\n",
		"name: project\nversion: 1.0.0\ndependencies:\n  mcp:\n    - name: server\n      registry: !!bool false\n      transport: stdio\n      command: 'server --arg'\n",
		"$schema: https://example.com/unknown.json\nname: project\nversion: 1.0.0\n",
	}
	for _, content := range invalid {
		t.Run(content, func(t *testing.T) {
			_, err := ParseManifest([]byte(content))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "apm.yml")
		})
	}
}

func TestAPMSafeYAML(t *testing.T) {
	base := "name: project\nversion: 1.0.0\n"
	for _, extra := range []string{
		"name: duplicate\n", "unknown: &anchor value\n", "unknown: *anchor\n",
		"unknown: !custom value\n", "unknown: !!python/object value\n",
		"---\nname: second\nversion: 1.0.0\n", "<<: {injected: value}\n",
	} {
		_, err := ParseManifest([]byte(base + extra))
		require.Error(t, err, extra)
	}
	manifest, err := ParseManifest([]byte("$schema: " + ManifestSchemaID + "\n" + base + "x-vendor: {octal: 077, flag: yes, null: null, typed: !!int 077}\n"))
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"octal": "077", "flag": "yes", "null": "null", "typed": int64(77)}, manifest.Field("x-vendor"))
	conventional, err := ParseManifest([]byte(base + "x-vendor: {octal: 077, flag: true, null: null}\n"))
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"octal": int64(77), "flag": true, "null": nil}, conventional.Field("x-vendor"))
	_, err = ParseManifest([]byte(base + strings.Repeat(" ", MaxDocumentBytes)))
	require.ErrorContains(t, err, "document limit")
}

func TestAPMPreservationAndWarnings(t *testing.T) {
	content := []byte("# Keep this comment\nname: project\nversion: nightly\nx-vendor: {nested: [one, two]}\nfuture: value\nworkspaces: [pkg]\n")
	manifest, err := ParseManifest(content)
	require.NoError(t, err)
	assert.Len(t, manifest.Warnings, 2)
	vendor, ok := manifest.Field("x-vendor").(map[string]any)
	require.True(t, ok)
	vendor["nested"] = "changed"
	encoded := manifest.Bytes()
	assert.Equal(t, content, encoded)
	assert.NotEqual(t, vendor, manifest.Field("x-vendor"))
	encoded[0] = '!'
	again := manifest.Bytes()
	assert.Equal(t, content, again)
}

func TestAPMLockfileContract(t *testing.T) {
	content := []byte("lockfile_version: '1'\ndependencies:\n  - repo_url: owner/repo\n    resolved_commit: " + testCommit + "\n    depth: !!int 1\nx-vendor: {future: value}\nattestations: {opaque: keep}\n")
	lock, err := ParseLockfile(content)
	require.NoError(t, err)
	assert.Equal(t, 1, lock.Dependencies()[0].Depth)
	encoded := lock.Bytes()
	assert.Equal(t, content, encoded)
	for _, invalid := range []string{
		"lockfile_version: '3'\ndependencies: []\n",
		"lockfile_version: '1'\ndependencies:\n  - repo_url: owner/repo\n",
		"lockfile_version: '1'\ndependencies:\n  - repo_url: owner/repo\n    resolved_commit: short\n",
		"lockfile_version: '1'\ndependencies:\n  - source: registry\n    repo_url: owner/repo\n",
	} {
		_, err := ParseLockfile([]byte(invalid))
		require.Error(t, err)
	}
}

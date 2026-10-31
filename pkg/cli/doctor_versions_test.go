//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoctorReleaseSemanticVersions(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ current, latest, want string }{
		{"v1.9.0", "v1.10.0", "outdated"},
		{"1.10.0", "v1.10.0", "current"},
		{"v1.11.0-rc.1", "v1.10.0", "current"},
		{"v1.10.0-rc.1", "v1.10.0", "outdated"},
		{"dev", "v1.10.0", "development"},
		{"v1.10.0", "invalid", "unknown"},
	} {
		check := diagnoseDoctorRelease(tt.current, tt.latest, nil)
		assert.Equal(t, tt.want, check.Status)
	}
	check := diagnoseDoctorRelease("v1.0.0", "", errors.New("HTTP 403"))
	assert.Equal(t, "unknown", check.Status)
	assert.Contains(t, check.Message, "HTTP 403")
	assert.Error(t, doctorVersionCheckError(check, nil))
}

func TestDoctorCompiledVersionSemanticVersions(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ current, latest, want string }{
		{"v1.9.0", "v1.10.0", "outdated"},
		{"1.10.0", "v1.10.0", "current"},
		{"v1.11.0-rc.1", "v1.10.0", "newer"},
		{"v1.10.0-rc.1", "v1.10.0", "outdated"},
		{"", "v1.10.0", "unknown"},
		{"dev", "v1.10.0", "unknown"},
		{"v1.10.0", "", "unknown"},
	} {
		assert.Equal(t, tt.want, doctorCompiledVersionStatus(tt.current, tt.latest))
	}
}

func doctorVersionFixture(t *testing.T, source string) (string, *workflow.LockMetadata) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "test.md")
	require.NoError(t, os.WriteFile(file, []byte(source), 0600))
	hash, err := parser.ComputeFrontmatterHashFromFile(file, parser.NewImportCache(filepath.Dir(file)))
	require.NoError(t, err)
	bodyHash, err := parser.ComputeBodyHashFromFile(file)
	require.NoError(t, err)
	return file, &workflow.LockMetadata{SchemaVersion: "v4", FrontmatterHash: hash, BodyHash: bodyHash, CompilerVersion: "v1.9.0"}
}

func writeDoctorVersionMetadata(t *testing.T, file string, metadata *workflow.LockMetadata) {
	t.Helper()
	data, err := json.Marshal(metadata)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(stringutil.MarkdownToLockFile(file), append(append([]byte("# gh-aw-metadata: "), data...), []byte("\non: workflow_dispatch\n")...), 0600))
}

func inspectDoctorVersionFixture(t *testing.T, file string) *DoctorCompiledWorkflowCheck {
	t.Helper()
	cache := parser.NewImportCache(filepath.Dir(file))
	cache.SetReadOnly(true)
	check, err := inspectDoctorCompiledWorkflow(file, filepath.Dir(file), "v1.10.0", cache)
	require.NoError(t, err)
	require.NotNil(t, check)
	return check
}

func TestDoctorCompiledWorkflowFreshness(t *testing.T) {
	t.Parallel()
	source := "---\non: workflow_dispatch\nengine: copilot\n---\nOriginal body\n"
	file, metadata := doctorVersionFixture(t, source)
	check := inspectDoctorVersionFixture(t, file)
	assert.Equal(t, "missing", check.SourceStatus)
	writeDoctorVersionMetadata(t, file, metadata)
	check = inspectDoctorVersionFixture(t, file)
	assert.Equal(t, "current", check.SourceStatus)
	assert.Equal(t, "outdated", check.CompilerStatus)
	assert.Equal(t, "test.lock.yml", check.LockFile)
	require.NoError(t, os.WriteFile(file, []byte(source+"Prompt-only change\n"), 0600))
	assert.Equal(t, "current", inspectDoctorVersionFixture(t, file).SourceStatus)
	require.NoError(t, os.WriteFile(file, []byte(strings.Replace(source, "copilot", "claude", 1)), 0600))
	assert.Equal(t, "stale", inspectDoctorVersionFixture(t, file).SourceStatus)
}

func TestDoctorCompiledWorkflowFullBodyFreshness(t *testing.T) {
	t.Parallel()
	source := "---\non:\n  workflow_dispatch:\n  stale-check: full\nengine: copilot\n---\nOriginal body\n"
	file, metadata := doctorVersionFixture(t, source)
	writeDoctorVersionMetadata(t, file, metadata)
	assert.Equal(t, "current", inspectDoctorVersionFixture(t, file).SourceStatus)
	require.NoError(t, os.WriteFile(file, []byte(source+"Changed body\n"), 0600))
	assert.Equal(t, "stale", inspectDoctorVersionFixture(t, file).SourceStatus)
	metadata.BodyHash = ""
	writeDoctorVersionMetadata(t, file, metadata)
	check := inspectDoctorVersionFixture(t, file)
	assert.Equal(t, "unknown", check.SourceStatus)
	assert.Contains(t, check.Message, "body hash")
}

func TestDoctorCompiledWorkflowImportFreshness(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file, imported := filepath.Join(dir, "test.md"), filepath.Join(dir, "shared.md")
	require.NoError(t, os.WriteFile(imported, []byte("---\nenv:\n  MODE: first\n---\nShared\n"), 0600))
	require.NoError(t, os.WriteFile(file, []byte("---\non: workflow_dispatch\nimports:\n  - shared.md\n---\nPrompt\n"), 0600))
	hash, err := parser.ComputeFrontmatterHashFromFile(file, parser.NewImportCache(dir))
	require.NoError(t, err)
	writeDoctorVersionMetadata(t, file, &workflow.LockMetadata{FrontmatterHash: hash, CompilerVersion: "v1.10.0"})
	assert.Equal(t, "current", inspectDoctorVersionFixture(t, file).SourceStatus)
	require.NoError(t, os.WriteFile(imported, []byte("---\nenv:\n  MODE: second\n---\nShared\n"), 0600))
	assert.Equal(t, "stale", inspectDoctorVersionFixture(t, file).SourceStatus)
	require.NoError(t, os.Remove(imported))
	assert.Equal(t, "unknown", inspectDoctorVersionFixture(t, file).SourceStatus)
}

func TestDoctorCompiledWorkflowUnknownMetadata(t *testing.T) {
	t.Parallel()
	for _, lock := range []string{
		"name: old\n# gh-aw-metadata: {\"compiler_version\":\"v1.0.0\"}\n",
		"# gh-aw-metadata: broken\nname: bad\n",
		"# gh-aw-metadata: {\"compiler_version\":\"v1.10.0\"}\n",
	} {
		file, _ := doctorVersionFixture(t, "---\non: workflow_dispatch\n---\nPrompt\n")
		require.NoError(t, os.WriteFile(stringutil.MarkdownToLockFile(file), []byte(lock), 0600))
		check := inspectDoctorVersionFixture(t, file)
		assert.Equal(t, "unknown", check.SourceStatus)
		assert.Error(t, doctorVersionCheckError(nil, []DoctorCompiledWorkflowCheck{*check}))
	}
}

func TestDoctorVersionIntegration(t *testing.T) {
	release := diagnoseDoctorRelease("v1.9.0", "v1.10.0", nil)
	runtime := setupRepositoryRuntime{
		checkAuth:             func(context.Context) error { return nil },
		checkAgentAssignments: func(context.Context, string, string) ([]DoctorAgentAssignmentCheck, error) { return nil, nil },
		checkRelease:          func(context.Context) *DoctorReleaseCheck { return release },
		checkCompiledWorkflows: func(_ context.Context, dir, latest string) ([]DoctorCompiledWorkflowCheck, error) {
			assert.Equal(t, ".", dir)
			assert.Equal(t, "v1.10.0", latest)
			return []DoctorCompiledWorkflowCheck{{Workflow: "test.md", SourceStatus: "current", CompilerStatus: "outdated"}}, nil
		},
	}
	var err error
	output := captureSetupStdout(t, func() error {
		err = runSetupAuthWithRuntime(SetupAuthOptions{JSON: true, CheckAgentAssignment: true}, runtime)
		return nil
	})
	require.Error(t, err)
	var result SetupAuthResult
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	assert.Equal(t, "outdated", result.Release.Status)
	require.Len(t, result.CompiledWorkflows, 1)
	assert.Equal(t, "outdated", result.CompiledWorkflows[0].CompilerStatus)
	runtime.checkRelease = func(context.Context) *DoctorReleaseCheck {
		t.Fatal("ordinary setup must not query releases")
		return nil
	}
	require.NoError(t, runSetupAuthWithRuntime(SetupAuthOptions{}, runtime))
}

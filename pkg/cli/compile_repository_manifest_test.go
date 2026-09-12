//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompileWorkflows_ValidatesRootAwManifest(t *testing.T) {
	tmpDir := testutil.TempDir(t, "aw-manifest-*")
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(originalWd) })
	require.NoError(t, os.Chdir(tmpDir))

	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	require.NoError(t, cmd.Run())

	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".github", "workflows"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".github", "workflows", "test.md"), []byte(`---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
---

# Test
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# Repo Assist\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "aw.yml"), []byte(`manifest-version: "1"
min-version: v9.9.9
name: Repo Assist
`), 0o644))

	originalVersion := GetVersion()
	SetVersionInfo("v1.2.3")
	t.Cleanup(func() { SetVersionInfo(originalVersion) })

	_, err = CompileWorkflows(context.Background(), CompileConfig{})
	require.Error(t, err)
	require.ErrorContains(t, err, `requires gh-aw`)
}

func TestCompileWorkflows_JSONOutputIncludesManifestValidationResult(t *testing.T) {
	tmpDir := testutil.TempDir(t, "aw-manifest-json-*")
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(originalWd) })
	require.NoError(t, os.Chdir(tmpDir))

	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	require.NoError(t, cmd.Run())

	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".github", "workflows"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".github", "workflows", "test.md"), []byte(`---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
---

# Test
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "aw.yml"), []byte(`name: Repo Assist
docs: docs/overview.md
`), 0o644))

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	_, err = CompileWorkflows(context.Background(), CompileConfig{JSONOutput: true})

	_ = w.Close()
	os.Stdout = oldStdout

	output, readErr := io.ReadAll(r)
	require.NoError(t, readErr)
	require.Error(t, err)

	var results []ValidationResult
	require.NoError(t, json.Unmarshal(output, &results), "output: %s", string(output))
	require.Len(t, results, 1)
	assert.Equal(t, "aw.yml", results[0].Workflow)
	assert.False(t, results[0].Valid)
	require.NotEmpty(t, results[0].Errors)
	assert.Equal(t, "manifest_error", results[0].Errors[0].Type)
	assert.Contains(t, results[0].Errors[0].Message, "docs")
}

func TestCompileWorkflows_RequiresCanonicalAwManifest(t *testing.T) {
	tmpDir := testutil.TempDir(t, "aw-manifest-legacy-*")
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(originalWd) })
	require.NoError(t, os.Chdir(tmpDir))

	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	require.NoError(t, cmd.Run())

	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".github", "workflows"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".github", "workflows", "test.md"), []byte(`---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
---

# Test
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "agents.yml"), []byte(`docs: docs/overview.md
`), 0o644))

	manifestPath, err := findLocalRepositoryPackageManifest(tmpDir)
	require.NoError(t, err)
	assert.Empty(t, manifestPath)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	_, err = CompileWorkflows(context.Background(), CompileConfig{JSONOutput: true})

	_ = w.Close()
	os.Stdout = oldStdout

	output, readErr := io.ReadAll(r)
	require.NoError(t, readErr)
	require.NoError(t, err)

	var results []ValidationResult
	require.NoError(t, json.Unmarshal(output, &results), "output: %s", string(output))
	require.Len(t, results, 1)
	assert.Equal(t, "test.md", results[0].Workflow)
	assert.Empty(t, results[0].Warnings)
	assert.Empty(t, results[0].Errors)
}

func TestCompileWorkflows_AcceptsManifestWithoutManifestVersion(t *testing.T) {
	tmpDir := testutil.TempDir(t, "aw-manifest-no-version-*")
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(originalWd) })
	require.NoError(t, os.Chdir(tmpDir))

	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	require.NoError(t, cmd.Run())

	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".github", "workflows"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".github", "workflows", "test.md"), []byte(`---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
---

# Test
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# Repo Assist\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "aw.yml"), []byte(`name: Repo Assist
`), 0o644))

	_, err = CompileWorkflows(context.Background(), CompileConfig{})
	require.NoError(t, err)
}

func TestCompileWorkflows_UsesManifestScheduleSeed(t *testing.T) {
	tmpDir := testutil.TempDir(t, "aw-manifest-schedule-seed-*")
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(originalWd) })
	require.NoError(t, os.Chdir(tmpDir))

	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	require.NoError(t, cmd.Run())

	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".github", "workflows"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".github", "workflows", "test.md"), []byte(`---
on: daily
permissions:
  contents: read
engine: copilot
---

# Test
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# Repo Assist\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "aw.yml"), []byte(`name: Repo Assist
schedule-seed: github/gh-aw
`), 0o644))

	wasRelease := workflow.IsRelease()
	workflow.SetIsRelease(true)
	t.Cleanup(func() { workflow.SetIsRelease(wasRelease) })

	_, err = CompileWorkflows(context.Background(), CompileConfig{})
	require.NoError(t, err)

	lockContent, err := os.ReadFile(filepath.Join(tmpDir, ".github", "workflows", "test.lock.yml"))
	require.NoError(t, err)
	expectedCron, err := parser.ScatterSchedule("FUZZY:DAILY * * *", "github/gh-aw/.github/workflows/test.md")
	require.NoError(t, err)
	assert.Contains(t, string(lockContent), expectedCron, "compiled schedule should use the aw.yml schedule seed")
}

func TestCompileWorkflows_ResolvesImportedManifestRootRelativeWorkflow(t *testing.T) {
	tmpDir := testutil.TempDir(t, "aw-manifest-import-root-relative-*")
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(originalWd) })
	require.NoError(t, os.Chdir(tmpDir))

	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	require.NoError(t, cmd.Run())

	writePackageTestFile(t, tmpDir, "README.md", "# Repo Assist\n")
	writePackageTestFile(t, tmpDir, "aw.yml", `name: Repo Assist
includes:
  - ambient-context/aw.yml
`)
	writePackageTestFile(t, tmpDir, "ambient-context/aw.yml", `name: Ambient Context
includes:
  - .github/workflows/ambient-context.md
`)
	writePackageTestFile(t, tmpDir, ".github/workflows/ambient-context.md", `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
---

# Ambient Context
`)

	_, err = CompileWorkflows(context.Background(), CompileConfig{})
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(tmpDir, ".github", "workflows", "ambient-context.lock.yml"))
	assert.NoFileExists(t, filepath.Join(tmpDir, "ambient-context", ".github", "workflows", "ambient-context.md"))
}

func TestCompileWorkflows_RequiresPackageReadme(t *testing.T) {
	tmpDir := testutil.TempDir(t, "aw-manifest-readme-*")
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(originalWd) })
	require.NoError(t, os.Chdir(tmpDir))

	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	require.NoError(t, cmd.Run())

	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".github", "workflows"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".github", "workflows", "test.md"), []byte(`---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
---

# Test
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "aw.yml"), []byte(`manifest-version: "1"
name: Repo Assist
`), 0o644))

	_, err = CompileWorkflows(context.Background(), CompileConfig{})
	require.Error(t, err)
	require.ErrorContains(t, err, "missing required README.md")
}

func TestCompileWorkflows_RejectsManifestWorkflowWithPrivateTrue(t *testing.T) {
	tmpDir := testutil.TempDir(t, "aw-manifest-private-true-*")
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(originalWd) })
	require.NoError(t, os.Chdir(tmpDir))

	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	require.NoError(t, cmd.Run())

	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "workflows"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "workflows", "review.md"), []byte(`---
private: true
---

# Review
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# Repo Assist\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "aw.yml"), []byte(`manifest-version: "1"
name: Repo Assist
files:
  - workflows/review.md
`), 0o644))

	_, err = CompileWorkflows(context.Background(), CompileConfig{})
	require.Error(t, err)
	require.ErrorContains(t, err, `workflow "workflows/review.md" sets private: true`)
}

func TestValidateRepositoryManifestForCompilation_PropagatesGitRootErrors(t *testing.T) {
	originalFindGitRoot := findGitRootForManifestValidation
	t.Cleanup(func() {
		findGitRootForManifestValidation = originalFindGitRoot
	})

	findGitRootForManifestValidation = func() (string, error) {
		return "", errors.New("permission denied")
	}

	stats := &CompilationStats{}
	var results []ValidationResult
	_, err := validateRepositoryManifestForCompilation(CompileConfig{}, stats, &results)
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to find git root for manifest validation")
	require.ErrorContains(t, err, "permission denied")
}

func TestApplyRepositoryManifestDefaults_ScheduleSeed(t *testing.T) {
	t.Parallel()

	manifest := &repositoryPackageManifest{ScheduleSeed: "github/gh-aw"}

	t.Run("uses manifest value when flag omitted", func(t *testing.T) {
		config := applyRepositoryManifestDefaults(CompileConfig{}, manifest)
		assert.Equal(t, "github/gh-aw", config.ScheduleSeed)
	})

	t.Run("preserves explicit flag value", func(t *testing.T) {
		config := applyRepositoryManifestDefaults(CompileConfig{ScheduleSeed: "octo/repo"}, manifest)
		assert.Equal(t, "octo/repo", config.ScheduleSeed)
	})

	t.Run("handles missing manifest", func(t *testing.T) {
		config := applyRepositoryManifestDefaults(CompileConfig{}, nil)
		assert.Empty(t, config.ScheduleSeed)
	})
}

func TestParseRepositoryPackageManifest_ScheduleSeed(t *testing.T) {
	t.Parallel()

	manifest, _, err := parseRepositoryPackageManifest("aw.yml", []byte("name: Repo Assist\nschedule-seed: github/gh-aw\n"))
	require.NoError(t, err)
	assert.Equal(t, "github/gh-aw", manifest.ScheduleSeed)
}

func TestParseRepositoryPackageManifest_RejectsInvalidScheduleSeed(t *testing.T) {
	t.Parallel()

	_, _, err := parseRepositoryPackageManifest("aw.yml", []byte("name: Repo Assist\nschedule-seed: invalid\n"))
	require.Error(t, err)
	require.ErrorContains(t, err, "schedule-seed")
}

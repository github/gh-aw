package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestGeneratedJobNamesAndPermissionSourceComments(t *testing.T) {
	dir := testutil.TempDir(t, "job-annotations")
	path := filepath.Join(dir, "annotations.md")
	source := `---
on:
  issues:
    types: [opened]
  # Grant read access to the issue
  permissions:
    # Needed by on.steps
    issues: read # issue lookup
engine: copilot
strict: false
permissions:
  contents: read
safe-outputs:
  add-comment:
  threat-detection: true
jobs:
  probe:
    runs-on: ubuntu-latest
    # Custom job needs checkout access
    permissions:
      # Read the source
      contents: read # repo checkout
    steps:
      - run: echo ok
  safe_outputs:
    # Mint a token in the safe outputs job
    permissions:
      # Author requested OIDC
      id-token: write # token mint
---
Run the agent.
`
	require.NoError(t, os.WriteFile(path, []byte(source), 0600))
	require.NoError(t, NewCompiler().CompileWorkflow(path))
	compiled, err := os.ReadFile(filepath.Join(dir, "annotations.lock.yml"))
	require.NoError(t, err)

	var workflow map[string]any
	require.NoError(t, yaml.Unmarshal(compiled, &workflow))
	jobs := workflow["jobs"].(map[string]any)
	for _, id := range []string{"pre_activation", "activation", "agent", "detection", "safe_outputs", "conclusion"} {
		job, ok := jobs[id].(map[string]any)
		require.True(t, ok, "missing generated job %s", id)
		require.Equal(t, generatedJobNames[id], job["name"], "display name of %s", id)
	}
	require.NotContains(t, jobs["probe"].(map[string]any), "name", "custom jobs must not get generated names")
	output := string(compiled)
	for _, comment := range []string{
		"# Grant read access to the issue",
		"# Needed by on.steps",
		"# issue lookup",
		"# Custom job needs checkout access",
		"# Read the source",
		"# repo checkout",
		"# Mint a token in the safe outputs job",
		"# Author requested OIDC",
		"# token mint",
		"# Permissions for the pre_activation job",
		"# Includes permissions declared in on.permissions.",
	} {
		require.Contains(t, output, comment)
	}
	require.NotContains(t, output, "# # Needed by on.steps", "source comments should not be double-commented")
	require.Contains(t, output, "  probe:\n")
	require.Contains(t, output, "    # Custom job needs checkout access\n    # Read the source\n    # repo checkout\n    permissions:")
}

func TestSourceJobPermissionCommentsAreScoped(t *testing.T) {
	source := `on:
  # trigger comment
  push:
  # before on permissions
  permissions:
    # on permission
    contents: read # read repo
  steps:
    # unrelated step
jobs:
  first:
    # unrelated job
    runs-on: ubuntu-latest
    # before first permissions
    permissions:
      # first permission
      issues: read
    steps:
      # unrelated step
  second:
    permissions: # custom purpose
      contents: read # second permission
    # not a permission comment
`
	comments := sourceJobPermissionComments(source)
	require.Equal(t, []string{"# before on permissions", "# on permission", "# read repo"}, comments["pre_activation"])
	require.Equal(t, []string{"# before first permissions", "# first permission"}, comments["first"])
	require.Equal(t, []string{"# custom purpose", "# second permission"}, comments["second"])
	require.NotContains(t, strings.Join(comments["first"], "\n"), "unrelated")
}

func TestOptionalGeneratedJobAnnotations(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = &JobManager{jobs: map[string]*Job{
		"evals":                  {Name: "evals", Permissions: "contents: read"},
		"update_cache_memory":    {Name: "update_cache_memory", Permissions: "contents: write"},
		updateDriveMemoryJobName: {Name: updateDriveMemoryJobName, Permissions: "contents: write"},
		"custom":                 {Name: "custom", Permissions: "contents: read"},
	}}
	compiler.annotateGeneratedJobs(&WorkflowData{})

	require.Equal(t, "Evaluations", compiler.jobManager.jobs["evals"].DisplayName)
	require.Equal(t, "Update cache memory", compiler.jobManager.jobs["update_cache_memory"].DisplayName)
	require.Equal(t, "Update drive memory", compiler.jobManager.jobs[updateDriveMemoryJobName].DisplayName)
	require.Empty(t, compiler.jobManager.jobs["custom"].DisplayName)
	for _, job := range compiler.jobManager.jobs {
		require.Contains(t, job.PermissionsComment, "# Permissions for the")
	}
}

func TestSourceJobPermissionCommentsWithWideIndentation(t *testing.T) {
	source := strings.Join([]string{
		"on:",
		"    issues:",
		"        types: [opened]",
		"    # Trigger permissions",
		"    permissions:",
		"        issues: read # Read issue",
		"jobs:",
		"    probe:",
		"        runs-on: ubuntu-latest",
		"        # Job permissions",
		"        permissions:",
		"            # Read source",
		"            contents: read # Checkout",
		"        steps:",
		"            - run: echo ok",
	}, "\n")
	comments := sourceJobPermissionComments(source)
	require.Equal(t, []string{"# Trigger permissions", "# Read issue"}, comments["pre_activation"])
	require.Equal(t, []string{"# Job permissions", "# Read source", "# Checkout"}, comments["probe"])
}

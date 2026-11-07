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
  contents: read # Agent checkout
  issues: read # Agent issue lookup
safe-outputs:
  add-comment:
  threat-detection: true
  jobs:
    publish:
      runs-on: ubuntu-latest
      permissions: # Publish the report
        issues: write # Post the report issue
      steps:
        - run: echo ok
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
		require.Equal(t, id, job["name"], "display name of %s", id)
	}
	require.Equal(t, "probe", jobs["probe"].(map[string]any)["name"], "unnamed custom jobs use their ID")
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
	require.Contains(t, output, "    # Custom job needs checkout access\n    permissions:\n      # Read the source\n      contents: read # repo checkout")
	require.Contains(t, output, "issues: read # Agent issue lookup")
	require.Contains(t, output, "contents: read # Agent checkout")
	require.Contains(t, output, "permissions: # Publish the report")
	require.Contains(t, output, "issues: write # Post the report issue")
	require.Contains(t, output, "id-token: write # token mint")
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
	comments, err := permissionCommentSources(source)
	require.NoError(t, err)
	require.Equal(t, "# before on permissions", comments["pre_activation"].key.HeadComment)
	require.Equal(t, "# on permission", permissionCommentEntry(comments["pre_activation"].value, "contents").key.HeadComment)
	require.Equal(t, "# read repo", permissionCommentEntry(comments["pre_activation"].value, "contents").value.LineComment)
	require.Equal(t, "# before first permissions", comments["first"].key.HeadComment)
	require.Equal(t, "# first permission", permissionCommentEntry(comments["first"].value, "issues").key.HeadComment)
	require.Equal(t, "# custom purpose", comments["second"].key.LineComment)
	require.Equal(t, "# second permission", permissionCommentEntry(comments["second"].value, "contents").value.LineComment)
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

	require.Equal(t, "evals", compiler.jobManager.jobs["evals"].DisplayName)
	require.Equal(t, "update_cache_memory", compiler.jobManager.jobs["update_cache_memory"].DisplayName)
	require.Equal(t, updateDriveMemoryJobName, compiler.jobManager.jobs[updateDriveMemoryJobName].DisplayName)
	require.Equal(t, "custom", compiler.jobManager.jobs["custom"].DisplayName)
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
	comments, err := permissionCommentSources(source)
	require.NoError(t, err)
	require.Equal(t, "# Trigger permissions", comments["pre_activation"].key.HeadComment)
	require.Equal(t, "# Read issue", permissionCommentEntry(comments["pre_activation"].value, "issues").value.LineComment)
	require.Equal(t, "# Job permissions", comments["probe"].key.HeadComment)
	require.Equal(t, "# Read source", permissionCommentEntry(comments["probe"].value, "contents").key.HeadComment)
	require.Equal(t, "# Checkout", permissionCommentEntry(comments["probe"].value, "contents").value.LineComment)
}

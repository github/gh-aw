//go:build !integration

package workflow

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRepositoryWorkflowsCompileWithoutEmitting(t *testing.T) {
	// Resolve remote imports from the checked-in repository cache, not pkg/workflow.
	t.Chdir(filepath.Join("..", ".."))
	sources, err := filepath.Glob(filepath.Join(".github", "workflows", "*.md"))
	require.NoError(t, err)
	require.NotEmpty(t, sources, "should discover repository agentic workflows")

	for _, source := range sources {
		name := strings.TrimSuffix(filepath.Base(source), ".md")
		t.Run(name, func(t *testing.T) {
			compiler := NewCompiler(
				WithVersion("integration"),
				WithWorkflowIdentifier(name),
				WithNoEmit(true),
			)
			compiler.SetQuiet(true)

			require.NoError(t, compiler.CompileWorkflow(source), "repository workflow should compile without emitting lock files")
		})
	}
}

func TestWindowsGrowerProbePermissions(t *testing.T) {
	t.Chdir(filepath.Join("..", ".."))
	compilers := make(map[string]*Compiler)
	for _, name := range []string{"windows", "windows-grower"} {
		compiler := NewCompiler(
			WithVersion("integration"),
			WithWorkflowIdentifier(name),
			WithNoEmit(true),
		)
		compiler.SetQuiet(true)
		require.NoError(t, compiler.CompileWorkflow(filepath.Join(".github", "workflows", name+".md")))
		compilers[name] = compiler
	}

	required := NewPermissions()
	for _, job := range compilers["windows"].jobManager.GetAllJobs() {
		required.Merge(NewPermissionsParser(job.Permissions).ToPermissions())
	}
	require.NotEmpty(t, required.RenderToYAML(), "the Windows callee must declare job permissions")

	grower := compilers["windows-grower"].jobManager
	probe, exists := grower.GetJob("windows_probe")
	require.True(t, exists)
	require.Equal(t, "./.github/workflows/windows.lock.yml", probe.Uses)
	require.Equal(t, []string{"activation"}, probe.Needs)
	require.True(t, probe.SecretsInherit)
	require.Equal(t, required.RenderToYAML(), probe.Permissions,
		"the reusable caller must grant exactly the union of the callee's job permissions")

	var rendered strings.Builder
	grower.renderJobTo(&rendered, probe)
	require.Contains(t, rendered.String(), "    "+required.RenderToYAML(),
		"reusable calls must retain their job-scoped permissions in the emitted workflow")

	agent, exists := grower.GetJob("agent")
	require.True(t, exists)
	agentPermissions := NewPermissionsParser(agent.Permissions).ToPermissions()
	require.Equal(t, map[PermissionScope]PermissionLevel{
		PermissionActions:         PermissionRead,
		PermissionContents:        PermissionRead,
		PermissionCopilotRequests: PermissionWrite,
		PermissionIssues:          PermissionRead,
	}, agentPermissions.permissions,
		"probe permissions must not broaden the OpenAI-backed grower agent's token")
}

func TestWindowsWorkflowDryRunConfiguration(t *testing.T) {
	t.Chdir(filepath.Join("..", ".."))
	compiler := NewCompiler(WithNoEmit(true), WithWorkflowIdentifier("windows"))
	compiler.SetQuiet(true)
	compiler.SetDryRun(true)
	require.NoError(t, compiler.CompileWorkflow(filepath.Join(".github", "workflows", "windows.md")))
	require.Equal(t, compiler.GetExperimentalWarningCount(), compiler.GetWarningCount(),
		"the Windows dry-run should have no blocking compiler warnings")
}

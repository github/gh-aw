package cli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppendLocalRepositoryPackageWorkflowSpecs(t *testing.T) {
	require.Nil(t, appendLocalRepositoryPackageWorkflowSpecs(nil, nil))

	specs := appendLocalRepositoryPackageWorkflowSpecs(nil, &resolvedRepositoryPackage{
		InstallationSource: []resolvedPackageInstallable{{
			SourcePath:      "/package/workflows/source.md",
			DestinationPath: ".github/workflows/renamed.md",
		}},
		ResourceFiles: []resolvedPackageResource{{
			SourcePath:      "/package/assets/config.yml",
			DestinationPath: ".github/agent.yml",
		}},
		ProjectFile: &resolvedPackageResource{
			SourcePath:      "/package/.github/workflows/aw.json",
			DestinationPath: ".github/workflows/aw.json",
		},
		SkillFiles: []resolvedPackageSkillFile{{
			SourcePath: "/package/skills/review/SKILL.md",
			SkillName:  "review",
		}},
		AgentFiles: []string{"/package/agents/triage.md"},
	})

	require.Len(t, specs, 5)
	assert.Equal(t, "renamed", specs[0].WorkflowName)
	assert.True(t, specs[0].FromRepositoryManifest)
	assert.Equal(t, "agent", specs[1].WorkflowName)
	assert.True(t, specs[1].IsPackageResourceFile)
	assert.Equal(t, "aw.json", specs[2].WorkflowName)
	assert.Equal(t, "review/SKILL", specs[3].WorkflowName)
	assert.True(t, specs[3].IsPackageSkillFile)
	assert.Equal(t, "triage", specs[4].WorkflowName)
	assert.True(t, specs[4].IsPackageAgentFile)
}

func TestPackageSpecFallbacks(t *testing.T) {
	_, _, _, err := resolveLocalPackageWorkflowSpec("https://example.com/workflow.md")
	require.ErrorIs(t, err, errNotHandled)

	_, _, _, err = resolveRepositoryPackageWorkflowSpec(context.Background(), "not-a-repository-spec")
	require.ErrorIs(t, err, errNotHandled)

	_, _, _, err = resolveRepositoryPackageFallback(context.Background(), "")
	require.ErrorContains(t, err, "invalid specification")
}

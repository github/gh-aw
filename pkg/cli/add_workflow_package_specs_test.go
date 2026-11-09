//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAppendLocalRepositoryPackageWorkflowSpecs(t *testing.T) {
	projectFile := &resolvedPackageResource{
		SourcePath:      "aw.json",
		DestinationPath: "aw.json",
	}
	pkg := &resolvedRepositoryPackage{
		InstallationSource: []resolvedPackageInstallable{{
			SourcePath:      "workflows/review.md",
			DestinationPath: ".github/workflows/review-custom.md",
		}},
		ResourceFiles: []resolvedPackageResource{{
			SourcePath:      "config/rules.json",
			DestinationPath: ".github/rules.json",
		}},
		ProjectFile: projectFile,
		SkillFiles: []resolvedPackageSkillFile{{
			SourcePath: "skills/review/SKILL.md",
			SkillName:  "review",
		}},
		AgentFiles: []string{"agents/reviewer.md"},
	}

	specs := appendLocalRepositoryPackageWorkflowSpecs(nil, pkg)
	require.Len(t, specs, 5)
	require.Equal(t, "review-custom", specs[0].WorkflowName)
	require.Equal(t, ".github/workflows/review-custom.md", specs[0].DestinationPath)
	require.True(t, specs[1].IsPackageResourceFile)
	require.Equal(t, "aw.json", specs[2].WorkflowName)
	require.True(t, specs[3].IsPackageSkillFile)
	require.True(t, specs[4].IsPackageAgentFile)
}

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveLocalPackageSkillFiles(t *testing.T) {
	packageDir := t.TempDir()
	skillDir := filepath.Join(packageDir, "skills", "review")
	require.NoError(t, os.MkdirAll(filepath.Join(skillDir, "scripts"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, packageSkillMarkerFile), []byte("# Review\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "scripts", "check.sh"), []byte("#!/bin/sh\n"), 0o644))

	skillFiles, warnings, err := resolveLocalPackageSkillFiles(packageDir, packageDir, []string{"skills/review"})
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Len(t, skillFiles, 2)
	assert.Equal(t, "review", skillFiles[0].SkillName)
	assert.Equal(t, "review", skillFiles[1].SkillName)
	assert.Equal(t, filepath.Join(skillDir, packageSkillMarkerFile), skillFiles[0].SourcePath)
	assert.Equal(t, filepath.Join(skillDir, "scripts", "check.sh"), skillFiles[1].SourcePath)
}

func TestResolveLocalPackageSkillFilesWarnsForMissingMarker(t *testing.T) {
	packageDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(packageDir, "skills", "incomplete"), 0o755))

	skillFiles, warnings, err := resolveLocalPackageSkillFiles(packageDir, packageDir, []string{"skills/incomplete"})
	require.NoError(t, err)
	assert.Empty(t, skillFiles)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], packageSkillMarkerFile)
}

func TestResolveLocalPackageAgentFiles(t *testing.T) {
	packageDir := t.TempDir()
	agentsDir := filepath.Join(packageDir, packageAgentsDirectory)
	githubAgentsDir := filepath.Join(packageDir, ".github", packageAgentsDirectory)
	require.NoError(t, os.MkdirAll(filepath.Join(agentsDir, "nested"), 0o755))
	require.NoError(t, os.MkdirAll(githubAgentsDir, 0o755))
	for _, name := range []string{"review.md", "triage.MD", "ignore.txt", filepath.Join("nested", "nested.md")} {
		require.NoError(t, os.WriteFile(filepath.Join(agentsDir, name), []byte("# Agent\n"), 0o644))
	}
	require.NoError(t, os.WriteFile(filepath.Join(githubAgentsDir, "copilot.md"), []byte("# Copilot\n"), 0o644))

	agentFiles, warnings, err := resolveLocalPackageAgentFiles(packageDir, packageDir, nil)
	require.NoError(t, err)
	require.Empty(t, warnings)
	assert.ElementsMatch(t, []string{
		filepath.Join(agentsDir, "review.md"),
		filepath.Join(agentsDir, "triage.MD"),
		filepath.Join(githubAgentsDir, "copilot.md"),
	}, agentFiles)

	explicitFiles, warnings, err := resolveLocalPackageAgentFiles(packageDir, packageDir, []string{".github/agents/copilot.md"})
	require.NoError(t, err)
	require.Empty(t, warnings)
	assert.Equal(t, []string{filepath.Join(githubAgentsDir, "copilot.md")}, explicitFiles)
}

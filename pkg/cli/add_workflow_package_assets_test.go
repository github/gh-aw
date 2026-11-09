//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveLocalPackageSkillFiles(t *testing.T) {
	packageDir := t.TempDir()
	skillDir := filepath.Join(packageDir, packageSkillsDirectory, "review")
	require.NoError(t, os.MkdirAll(filepath.Join(skillDir, "references"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, packageSkillMarkerFile), []byte("skill"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "references", "guide.md"), []byte("guide"), 0600))

	files, warnings, err := resolveLocalPackageSkillFiles(packageDir, packageDir, []string{"skills/review"})
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Len(t, files, 2)
	require.Equal(t, "review", files[0].SkillName)
	require.Equal(t, "review", files[1].SkillName)
}

func TestResolveLocalPackageSkillFilesWarnsForMissingMarker(t *testing.T) {
	packageDir := t.TempDir()
	skillDir := filepath.Join(packageDir, "custom-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "notes.md"), []byte("notes"), 0600))

	files, warnings, err := resolveLocalPackageSkillFiles(packageDir, packageDir, []string{"custom-skill"})
	require.NoError(t, err)
	require.Empty(t, files)
	require.Len(t, warnings, 1)
}

func TestResolveLocalPackageAgentFiles(t *testing.T) {
	packageDir := t.TempDir()
	agentsDir := filepath.Join(packageDir, packageAgentsDirectory)
	require.NoError(t, os.MkdirAll(filepath.Join(agentsDir, "nested"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "review.md"), []byte("agent"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "README.txt"), []byte("ignored"), 0600))

	files, warnings, err := resolveLocalPackageAgentFiles(packageDir, packageDir, nil)
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, []string{filepath.Join(agentsDir, "review.md")}, files)

	files, warnings, err = resolveLocalPackageAgentFiles(packageDir, packageDir, []string{"agents/explicit.md"})
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, []string{filepath.Join(agentsDir, "explicit.md")}, files)
}

func TestScanLocalPackageSkillDirsAndCollectFiles(t *testing.T) {
	packageDir := t.TempDir()
	skillsDir := filepath.Join(packageDir, packageSkillsDirectory)
	skillDir := filepath.Join(skillsDir, "research")
	require.NoError(t, os.MkdirAll(skillDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, packageSkillMarkerFile), []byte("skill"), 0600))
	require.NoError(t, os.Mkdir(filepath.Join(skillsDir, "not-a-skill"), 0755))

	dirs, err := scanLocalPackageSkillDirs(packageDir)
	require.NoError(t, err)
	require.Equal(t, []string{skillDir}, dirs)

	files, err := collectLocalPackageSkillDirFiles(skillDir)
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, "research", files[0].SkillName)
}

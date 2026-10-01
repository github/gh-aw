package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

func resolveLocalPackageSkillFiles(packageDir, packageRoot string, explicitSkillDirs []string) ([]resolvedPackageSkillFile, []string, error) {
	seenSkillDirs := make(map[string]struct{})
	var warnings []string

	var skillDirs []string
	appendIfNew := func(dir string) {
		cleaned := filepath.Clean(dir)
		if _, exists := seenSkillDirs[cleaned]; exists {
			return
		}
		seenSkillDirs[cleaned] = struct{}{}
		skillDirs = append(skillDirs, cleaned)
	}

	for _, dir := range explicitSkillDirs {
		baseDir := packageDir
		if strings.HasPrefix(filepath.ToSlash(dir), constants.GithubDir) {
			baseDir = packageRoot
		}
		appendIfNew(filepath.Join(baseDir, filepath.FromSlash(dir)))
	}
	autoScanned, err := scanLocalPackageSkillDirs(packageDir)
	if err != nil {
		if len(skillDirs) == 0 {
			return nil, nil, err
		}
		warnings = append(warnings, fmt.Sprintf("failed to auto-scan skills directory, proceeding with manifest skills only: %v", err))
	}
	for _, dir := range autoScanned {
		appendIfNew(dir)
	}

	manifestSkillDirSet := make(map[string]struct{}, len(explicitSkillDirs))
	for _, dir := range explicitSkillDirs {
		baseDir := packageDir
		if strings.HasPrefix(filepath.ToSlash(dir), constants.GithubDir) {
			baseDir = packageRoot
		}
		manifestSkillDirSet[filepath.Clean(filepath.Join(baseDir, filepath.FromSlash(dir)))] = struct{}{}
	}

	var skillFiles []resolvedPackageSkillFile
	for _, skillDir := range skillDirs {
		if _, fromManifest := manifestSkillDirSet[skillDir]; fromManifest {
			markerPath := filepath.Join(skillDir, packageSkillMarkerFile)
			if _, err := os.Stat(markerPath); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					warnings = append(warnings, fmt.Sprintf("Skill directory %q is missing required %s marker file", skillDir, packageSkillMarkerFile))
					continue
				}
				return nil, nil, fmt.Errorf("failed to validate skill marker %q: %w", markerPath, err)
			}
		}
		files, err := collectLocalPackageSkillDirFiles(skillDir)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to list files in skill directory %q: %w", skillDir, err)
		}
		skillFiles = append(skillFiles, files...)
	}

	return skillFiles, warnings, nil
}

func collectLocalPackageSkillDirFiles(skillDir string) ([]resolvedPackageSkillFile, error) {
	var skillFiles []resolvedPackageSkillFile
	skillName := filepath.Base(skillDir)
	err := filepath.WalkDir(skillDir, func(currentPath string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		skillFiles = append(skillFiles, resolvedPackageSkillFile{SourcePath: currentPath, SkillName: skillName})
		return nil
	})
	return skillFiles, err
}

func resolveLocalPackageAgentFiles(packageDir, packageRoot string, explicitAgentFiles []string) ([]string, []string, error) {
	if len(explicitAgentFiles) > 0 {
		agentFiles := make([]string, 0, len(explicitAgentFiles))
		for _, sourcePath := range explicitAgentFiles {
			baseDir := packageDir
			if strings.HasPrefix(filepath.ToSlash(sourcePath), constants.GithubDir) {
				baseDir = packageRoot
			}
			agentFiles = append(agentFiles, filepath.Clean(filepath.Join(baseDir, filepath.FromSlash(sourcePath))))
		}
		return agentFiles, nil, nil
	}

	var agentFiles []string
	for _, root := range []string{packageAgentsDirectory, constants.GithubDir + packageAgentsDirectory} {
		agentsDir := filepath.Join(packageDir, filepath.FromSlash(root))
		entries, err := os.ReadDir(agentsDir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, nil, fmt.Errorf("failed to scan agents directory %q: %w", agentsDir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
				continue
			}
			agentFiles = append(agentFiles, filepath.Join(agentsDir, entry.Name()))
		}
	}
	return agentFiles, nil, nil
}

func scanLocalPackageSkillDirs(packageDir string) ([]string, error) {
	var skillDirs []string
	for _, root := range []string{packageSkillsDirectory, constants.GithubDir + packageSkillsDirectory} {
		skillsDir := filepath.Join(packageDir, filepath.FromSlash(root))
		entries, err := os.ReadDir(skillsDir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("failed to scan skills directory %q: %w", skillsDir, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			skillDir := filepath.Join(skillsDir, entry.Name())
			if _, err := os.Stat(filepath.Join(skillDir, packageSkillMarkerFile)); err == nil {
				skillDirs = append(skillDirs, skillDir)
			}
		}
	}
	return skillDirs, nil
}

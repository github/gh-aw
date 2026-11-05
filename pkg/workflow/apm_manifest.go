//go:build !js && !wasm

package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/apm"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var apmManifestLog = logger.New("workflow:apm_manifest")

func (c *Compiler) resolveAPMManifestSkills(data *WorkflowData, markdownPath string) error {
	value, exists := data.RawFrontmatter["apm"]
	if !exists {
		return nil
	}
	manifestPath, ok := value.(string)
	if !ok || filepath.Base(manifestPath) != "apm.yml" || filepath.IsAbs(manifestPath) {
		return errors.New("apm must name a repository-confined apm.yml relative to the workflow")
	}
	if parser.ExtractAPMManifestPathText(data.FrontmatterYAML) != manifestPath {
		return errors.New("apm must use a plain or simply quoted path, without YAML escapes or block scalars")
	}
	root, err := gitutil.FindGitRootFrom(filepath.Dir(markdownPath))
	if err != nil {
		return fmt.Errorf("apm: cannot locate repository root: %w", err)
	}
	path := filepath.Join(filepath.Dir(markdownPath), manifestPath)
	manifestBytes, err := fileutil.ReadFileWithinBase(root, path)
	if err != nil {
		return fmt.Errorf("apm: cannot read manifest: %w", err)
	}
	manifest, err := apm.ParseManifest(manifestBytes)
	if err != nil {
		return err
	}
	for _, warning := range manifest.Warnings {
		fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "warning", warning))
		c.IncrementWarningCount()
	}
	lockPath := filepath.Join(filepath.Dir(path), "apm.lock.yaml")
	lockBytes, err := fileutil.ReadFileWithinBase(root, lockPath)
	if err != nil {
		return fmt.Errorf("apm: a readable adjacent apm.lock.yaml is required: %w", err)
	}
	lock, err := apm.ParseLockfile(lockBytes)
	if err != nil {
		return err
	}
	if err := validateAPMTarget(manifest, data); err != nil {
		return err
	}
	skills, err := lockedAPMSkills(manifest, lock)
	if err != nil {
		return err
	}
	if err := mergeAPMSkills(data, skills); err != nil {
		return err
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	data.APMManifestPath = filepath.ToSlash(relative)
	data.IncludedFiles = append(data.IncludedFiles, path, lockPath)
	apmManifestLog.Printf("Consumed manifest %s: %d locked skills", data.APMManifestPath, len(skills))
	return nil
}

func validateAPMTarget(manifest *apm.Manifest, data *WorkflowData) error {
	targets := manifest.TargetRestrictions()
	if len(targets) == 0 {
		return nil
	}
	engine := data.AI
	if data.EngineConfig != nil {
		engine = data.EngineConfig.ID
	}
	for _, name := range targets {
		if name == "all" || name == engine {
			return nil
		}
	}
	return fmt.Errorf("apm.yml: target restriction %v excludes workflow engine %s", targets, engine)
}

func mergeAPMSkills(data *WorkflowData, skills []string) error {
	refs := append([]SkillReference(nil), data.SkillReferences...)
	if len(refs) == 0 {
		for _, spec := range data.Skills {
			refs = append(refs, SkillReference{Skill: spec})
		}
	}
	seen := make(map[string]string)
	for _, ref := range refs {
		base, _, _ := strings.Cut(ref.Skill, "@")
		seen[base] = ref.Skill
	}
	for _, spec := range skills {
		if err := validateSkillSpecValue(spec, len(refs)); err != nil {
			return err
		}
		base, _, _ := strings.Cut(spec, "@")
		if previous, exists := seen[base]; exists {
			if previous != spec {
				return fmt.Errorf("apm: skill %s conflicts with explicit skills pin %s", spec, previous)
			}
			continue
		}
		refs = append(refs, SkillReference{Skill: spec})
		seen[base] = spec
	}
	data.SkillReferences = refs
	data.Skills = make([]string, 0, len(refs))
	for _, ref := range refs {
		data.Skills = append(data.Skills, ref.Skill)
	}
	return nil
}

package workflow

import (
	"errors"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/apm"
)

// lockedAPMSkills projects an explicitly supported skill-only manifest onto the
// native gh-aw installer. It never resolves mutable refs or rewrites either file.
func lockedAPMSkills(manifest *apm.Manifest, lock *apm.Lockfile) ([]string, error) {
	if manifest == nil || lock == nil {
		return nil, errors.New("APM skills require a manifest and lockfile")
	}
	if err := validateSkillManifest(manifest); err != nil {
		return nil, err
	}
	direct, err := manifestGitDependencies(manifest)
	if err != nil {
		return nil, err
	}
	entries := make(map[string]apm.LockedDependency)
	refs := make(map[string]GitReference)
	for i, entry := range lock.Dependencies() {
		if entry.IsDev || entry.Depth == 0 {
			continue
		}
		ref, err := lockedSkillReference(entry)
		if err != nil {
			return nil, fmt.Errorf("apm.lock.yaml: dependencies[%d]: %w", i, err)
		}
		key := ref.identity()
		if _, exists := entries[key]; exists {
			return nil, fmt.Errorf("apm.lock.yaml: duplicate dependency identity %q", key)
		}
		entries[key], refs[key] = entry, ref
	}
	directKeys := make(map[string]struct{})
	for _, ref := range direct {
		key := ref.identity()
		entry, ok := entries[key]
		if !ok || entry.Depth != 1 || entry.ResolvedRef != ref.Ref {
			return nil, fmt.Errorf("apm.lock.yaml is stale or missing direct dependency %s#%s", key, ref.Ref)
		}
		if commitPattern.MatchString(ref.Ref) && entry.ResolvedCommit != ref.Ref {
			return nil, fmt.Errorf("apm.lock.yaml commit differs from manifest SHA for %s", key)
		}
		if _, exists := directKeys[key]; exists {
			return nil, fmt.Errorf("apm.yml: duplicate dependency identity %q", key)
		}
		directKeys[key] = struct{}{}
	}
	for key, entry := range entries {
		if _, exists := directKeys[key]; entry.Depth == 1 && !exists {
			return nil, fmt.Errorf("apm.lock.yaml contains undeclared direct dependency %s", key)
		}
		// Transitive resolution needs package-manifest verification, not merely
		// trusting additional entries in an inventory.
		if entry.Depth > 1 {
			return nil, fmt.Errorf("apm.lock.yaml: transitive dependency %s requires an APM package resolver; native compiler mode supports direct skills only", key)
		}
	}
	var specs []string
	for _, ref := range direct {
		key := ref.identity()
		specs = append(specs, refs[key].SkillSpec(entries[key].ResolvedCommit))
	}
	return specs, nil
}

func validateSkillManifest(manifest *apm.Manifest) error {
	for _, field := range []string{"scripts", "includes", "compilation", "policy", "marketplace", "replace", "registries"} {
		if value := manifest.Field(field); value != nil {
			return fmt.Errorf("apm.yml: %s is not supported by native skill-only compilation", field)
		}
	}
	if host := manifest.Field("default_host"); host != nil && host != "github.com" {
		return errors.New("apm.yml: native skills support only default_host: github.com")
	}
	block, ok := manifest.Field("dependencies").(map[string]any)
	if !ok {
		return nil
	}
	for key, value := range block {
		if key == "apm" || key == "conflict_resolution" || strings.HasPrefix(key, "x-") {
			continue
		}
		if values, ok := value.([]any); ok && len(values) == 0 {
			continue
		}
		return fmt.Errorf("apm.yml: dependencies.%s is not supported by native skill-only compilation", key)
	}
	return nil
}

func manifestGitDependencies(manifest *apm.Manifest) ([]GitReference, error) {
	var refs []GitReference
	for i, value := range manifest.APMDependencies(false) {
		ref, err := manifestGitReference(value)
		if err != nil {
			return nil, fmt.Errorf("apm.yml: dependencies.apm[%d]: %w", i, err)
		}
		if ref.Ref == "" {
			return nil, fmt.Errorf("apm.yml: dependencies.apm[%d] requires an explicit #ref for frozen compilation", i)
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func manifestGitReference(value apm.Dependency) (GitReference, error) {
	if value.Kind == apm.ShorthandDependency {
		return ParseGitHubReference(value.Shorthand)
	}
	if value.Kind != apm.GitDependency {
		return GitReference{}, errors.New("registry and local dependencies are not supported by native skill-only compilation")
	}
	for key := range value.Fields() {
		if key != "git" && key != "path" && key != "ref" && !strings.HasPrefix(key, "x-") {
			return GitReference{}, fmt.Errorf("git option %s is not supported by native skill-only compilation", key)
		}
	}
	ref, err := ParseGitHubReference(value.Git)
	if err != nil {
		return ref, err
	}
	if path := value.Path; path != "" {
		if ref.Path != "" {
			return ref, errors.New("git source and path both specify a virtual path")
		}
		withPath, err := ParseGitHubReference(strings.Join([]string{ref.Repository, path}, "/"))
		if err != nil {
			return ref, err
		}
		if withPath.Ref != "" {
			return ref, errors.New("path must not contain a ref; use the separate ref field")
		}
		ref.Path = withPath.Path
	}
	if declared := value.Ref; declared != "" {
		if ref.Ref != "" {
			return ref, errors.New("git source and ref both specify a ref")
		}
		withRef, err := ParseGitHubReference(ref.Repository + "#" + declared)
		if err != nil {
			return ref, err
		}
		ref.Ref = withRef.Ref
	}
	return ref, nil
}

func lockedSkillReference(entry apm.LockedDependency) (GitReference, error) {
	if entry.Source != "" || entry.Port != 0 || (entry.Host != "" && entry.Host != "github.com") {
		return GitReference{}, errors.New("only github.com Git sources are supported")
	}
	if entry.Constraint != "" || entry.Alias != "" || len(entry.SkillSubset) != 0 {
		return GitReference{}, errors.New("constraints, aliases and skill subsets require a package resolver")
	}
	ref, err := ParseGitHubReference(entry.RepoURL)
	if err != nil {
		return ref, err
	}
	if ref.Ref != "" {
		return ref, errors.New("repo_url must not contain a ref")
	}
	if entry.VirtualPath != "" {
		if ref.Path != "" && ref.Path != entry.VirtualPath {
			return ref, errors.New("repo_url and virtual_path disagree")
		}
		parsed, err := ParseGitHubReference(strings.Join([]string{ref.Repository, entry.VirtualPath}, "/"))
		if err != nil {
			return ref, err
		}
		if parsed.Ref != "" {
			return ref, errors.New("virtual_path must not contain a ref; use resolved_ref")
		}
		ref.Path = parsed.Path
	}
	if entry.MaterializationRepoURL != "" {
		material, err := ParseGitHubReference(entry.MaterializationRepoURL)
		if err != nil || !strings.EqualFold(material.Repository, ref.Repository) || material.Path != "" || material.Ref != "" {
			return ref, errors.New("materialization_repo_url must identify the same repository")
		}
		ref.Repository = material.Repository
	}
	if !commitPattern.MatchString(entry.ResolvedCommit) {
		return ref, errors.New("resolved_commit must be a full lowercase SHA")
	}
	if ref.Path == "" && entry.PackageType != "skill_bundle" {
		return ref, errors.New("repository-wide installs require package_type: skill_bundle; mixed APM packages are not supported")
	}
	if strings.HasSuffix(ref.Path, ".md") {
		return ref, errors.New("native skills require a skill directory, not an instruction, prompt or agent file")
	}
	return ref, nil
}

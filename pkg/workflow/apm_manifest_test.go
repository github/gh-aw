//go:build !integration && !js && !wasm

package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const apmTestCommit = "0123456789abcdef0123456789abcdef01234567"

func apmTestFiles(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, ".git"), 0755))
	dir := filepath.Join(root, ".github", "workflows")
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "apm.yml"), []byte("name: review\nversion: 1.0.0\ntargets: copilot\ndependencies:\n  apm:\n    - owner/repo/skills/review#main\n"), 0644))
	lock := "lockfile_version: '1'\ndependencies:\n  - repo_url: owner/repo\n    virtual_path: skills/review\n    resolved_ref: main\n    resolved_commit: " + apmTestCommit + "\n    depth: !!int 1\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "apm.lock.yaml"), []byte(lock), 0644))
	return root, filepath.Join(dir, "review.md")
}

func apmTestData() *WorkflowData {
	return &WorkflowData{
		RawFrontmatter:  map[string]any{"apm": "apm.yml"},
		FrontmatterYAML: "apm: apm.yml",
		AI:              "copilot",
		EngineConfig:    &EngineConfig{ID: "copilot"},
	}
}

func TestAPMCompilerResolvesAndInstallsNativeSkills(t *testing.T) {
	_, path := apmTestFiles(t)
	data := apmTestData()
	compiler := NewCompiler()
	require.NoError(t, compiler.resolveAPMManifestSkills(data, path))
	assert.Equal(t, []string{"owner/repo/skills/review@" + apmTestCommit}, data.Skills)
	assert.Equal(t, ".github/workflows/apm.yml", data.APMManifestPath)
	assert.Len(t, data.IncludedFiles, 2)
	compiler.SetActionMode(ActionModeDev)
	data.Name, data.On = "review", "\"on\":\n  workflow_dispatch:"
	job, err := compiler.buildActivationJob(data, false, "", "review.lock.yml")
	require.NoError(t, err)
	steps := strings.Join(job.Steps, "")
	assert.Contains(t, steps, "owner/repo/skills/review@"+apmTestCommit)
	assert.Contains(t, steps, "install_frontmatter_skills.cjs")
	assert.NotContains(t, steps, "microsoft/apm")
}

func TestAPMCompilerEndToEnd(t *testing.T) {
	_, path := apmTestFiles(t)
	content := "---\non: workflow_dispatch\npermissions:\n  contents: read\nengine: copilot\nstrict: false\napm: apm.yml\n---\nReview code.\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	compiler := NewCompiler(WithSkipValidation(true))
	compiler.SetActionMode(ActionModeDev)
	require.NoError(t, compiler.CompileWorkflow(path))
	lock, err := os.ReadFile(strings.TrimSuffix(path, ".md") + ".lock.yml")
	require.NoError(t, err)
	assert.Contains(t, string(lock), "owner/repo/skills/review@"+apmTestCommit)
	assert.NotContains(t, string(lock), "uses: microsoft/apm")
	changed := strings.ReplaceAll(string(mustReadAPMTestFile(t, filepath.Join(filepath.Dir(path), "apm.lock.yaml"))), apmTestCommit, strings.Repeat("b", 40))
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(path), "apm.lock.yaml"), []byte(changed), 0644))
	require.NoError(t, compiler.CompileWorkflow(path))
	moved, err := os.ReadFile(strings.TrimSuffix(path, ".md") + ".lock.yml")
	require.NoError(t, err)
	assert.NotEqual(t, string(lock), string(moved))
	assert.Contains(t, string(moved), "owner/repo/skills/review@"+strings.Repeat("b", 40))
}

func mustReadAPMTestFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	return content
}

func TestAPMCompilerRejectsMissingStaleAndEscapingFiles(t *testing.T) {
	for _, tc := range []string{"missing", "stale", "target", "escape", "symlink", "escaped-scalar"} {
		t.Run(tc, func(t *testing.T) {
			root, path := apmTestFiles(t)
			data := apmTestData()
			switch tc {
			case "missing":
				require.NoError(t, os.Remove(filepath.Join(filepath.Dir(path), "apm.lock.yaml")))
			case "stale":
				require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(path), "apm.lock.yaml"), []byte("lockfile_version: '1'\ndependencies: []\n"), 0644))
			case "target":
				data.EngineConfig.ID = "claude"
			case "escape":
				data.RawFrontmatter["apm"] = "../../../apm.yml"
				data.FrontmatterYAML = "apm: ../../../apm.yml"
			case "symlink":
				outside := filepath.Join(t.TempDir(), "apm.yml")
				require.NoError(t, os.WriteFile(outside, []byte("name: outside\nversion: 1.0.0\n"), 0644))
				require.NoError(t, os.Symlink(outside, filepath.Join(root, "apm.yml")))
				data.RawFrontmatter["apm"] = "../../apm.yml"
				data.FrontmatterYAML = "apm: ../../apm.yml"
			case "escaped-scalar":
				data.FrontmatterYAML = `apm: "apm\u002eyml"`
			}
			require.Error(t, NewCompiler().resolveAPMManifestSkills(data, path))
			assert.Empty(t, data.Skills)
		})
	}
}

func TestAPMCompilerMergesSkillsWithoutLosingAuth(t *testing.T) {
	data := apmTestData()
	spec := "owner/repo/skills/review@" + apmTestCommit
	data.SkillReferences = []SkillReference{{Skill: spec, GitHubToken: "${{ secrets.PAT }}"}}
	require.NoError(t, mergeAPMSkills(data, []string{spec}))
	assert.Len(t, data.SkillReferences, 1)
	assert.Equal(t, "${{ secrets.PAT }}", data.SkillReferences[0].GitHubToken)
	require.ErrorContains(t, mergeAPMSkills(data, []string{"owner/repo/skills/review@" + strings.Repeat("b", 40)}), "conflicts")
}

func TestAPMDependabotDirectoriesAndPreservation(t *testing.T) {
	root, path := apmTestFiles(t)
	workflowsDir := filepath.Dir(path)
	configPath := filepath.Join(root, ".github", "dependabot.yml")
	config := `# Preserve policy
version: 2
registries:
  private:
    type: git
    url: https://github.com
    password: "${{ secrets.DEPENDABOT_PAT }}"
updates:
  - package-ecosystem: apm
    directories: ["/.github/workflows"]
    schedule: {interval: daily}
    registries: [private]
    groups: {context: {patterns: ["*"]}}
    cooldown: {default-days: 7}
`
	require.NoError(t, os.WriteFile(configPath, []byte(config), 0644))
	compiler := NewCompiler()
	require.NoError(t, compiler.GenerateDependabotManifests(context.Background(), []*WorkflowData{{APMManifestPath: ".github/workflows/apm.yml"}}, workflowsDir, false))
	assert.Equal(t, config, string(mustReadAPMTestFile(t, configPath)), "covered directories must not rewrite configuration")
	require.NoError(t, compiler.GenerateDependabotManifests(context.Background(), []*WorkflowData{
		{APMManifestPath: ".github/workflows/apm.yml"}, {APMManifestPath: "contexts/review/apm.yml"},
		{APMManifestPath: "contexts/review/apm.yml"},
	}, workflowsDir, false))
	updated := mustReadAPMTestFile(t, configPath)
	assert.Contains(t, string(updated), "# Preserve policy")
	assert.Contains(t, string(updated), "cooldown:")
	assert.Contains(t, string(updated), "groups:")
	var parsed map[string]any
	require.NoError(t, yaml.Unmarshal(updated, &parsed))
	updates, ok := parsed["updates"].([]any)
	require.True(t, ok)
	assert.Len(t, updates, 2)
	require.NoError(t, compiler.GenerateDependabotManifests(context.Background(), []*WorkflowData{{APMManifestPath: "contexts/review/apm.yml"}}, workflowsDir, false))
	assert.Equal(t, updated, mustReadAPMTestFile(t, configPath))
}

func TestAPMDependabotMalformedConfigNotOverwritten(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "dependabot.yml")
	content := []byte("version: 2\nupdates: [malformed\n")
	require.NoError(t, os.WriteFile(filename, content, 0644))
	require.Error(t, NewCompiler().generateDependabotConfigForDirectories(filename, map[string][]string{"apm": {"/"}}))
	assert.Equal(t, content, mustReadAPMTestFile(t, filename))
}

func TestAPMDependabotRecursiveDirectoryCoverage(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "dependabot.yml")
	content := []byte("version: 2\nupdates:\n  - package-ecosystem: apm\n    directories: ['/contexts/**']\n    schedule: {interval: daily}\n")
	require.NoError(t, os.WriteFile(filename, content, 0644))
	require.NoError(t, NewCompiler().generateDependabotConfigForDirectories(filename, map[string][]string{"apm": {"/contexts/review/nested"}}))
	assert.Equal(t, content, mustReadAPMTestFile(t, filename))
}

func TestAPMDependabotRootManifestDirectory(t *testing.T) {
	root, workflow := apmTestFiles(t)
	compiler := NewCompiler()
	require.NoError(t, compiler.GenerateDependabotManifests(context.Background(), []*WorkflowData{{APMManifestPath: "apm.yml"}}, filepath.Dir(workflow), false))
	content := mustReadAPMTestFile(t, filepath.Join(root, ".github", "dependabot.yml"))
	var config DependabotConfig
	require.NoError(t, yaml.Unmarshal(content, &config))
	require.Len(t, config.Updates, 1)
	assert.Equal(t, "apm", config.Updates[0].PackageEcosystem)
	assert.Equal(t, "/", config.Updates[0].Directory)
}

func TestAPMDependabotInvalidMappingsAndPatternsNotOverwritten(t *testing.T) {
	for _, content := range []string{
		"version: 2\nupdates: []\nupdates: []\n",
		"version: 2\nupdates:\n  - package-ecosystem: apm\n    directories: ['/[']\n",
	} {
		filename := filepath.Join(t.TempDir(), "dependabot.yml")
		require.NoError(t, os.WriteFile(filename, []byte(content), 0644))
		require.Error(t, NewCompiler().generateDependabotConfigForDirectories(filename, map[string][]string{"apm": {"/"}}))
		assert.Equal(t, content, string(mustReadAPMTestFile(t, filename)))
	}
}

func TestAPMDependabotMergedDirectoryCoverage(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "dependabot.yml")
	content := []byte("version: 2\nupdates:\n  - &apm\n    package-ecosystem: apm\n    directory: /other\n    schedule: {interval: daily}\n  - <<: *apm\n    directory: /contexts\n")
	require.NoError(t, os.WriteFile(filename, content, 0644))
	require.NoError(t, NewCompiler().generateDependabotConfigForDirectories(filename, map[string][]string{"apm": {"/contexts"}}))
	assert.Equal(t, content, mustReadAPMTestFile(t, filename), "inherited ecosystem fields must count toward directory coverage")
}

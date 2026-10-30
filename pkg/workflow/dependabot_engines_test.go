//go:build !integration && !js && !wasm

package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/testutil"
)

func TestDependabotBuiltInEnginePackages(t *testing.T) {
	tests := []struct {
		id, pkg, version string
	}{
		{"copilot", "@github/copilot", string(constants.DefaultCopilotVersion)},
		{"claude", "@anthropic-ai/claude-code", string(constants.DefaultClaudeCodeVersion)},
		{"codex", "@openai/codex", string(constants.DefaultCodexVersion)},
		{"pi", "@earendil-works/pi-coding-agent", string(constants.DefaultPiVersion)},
		{"gemini", "@google/gemini-cli", string(constants.DefaultGeminiVersion)},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			compiler := NewCompiler()
			for _, version := range []string{"", "1.2.3"} {
				data := &WorkflowData{EngineConfig: &EngineConfig{ID: tt.id, Version: version}}
				expected := version
				if expected == "" {
					expected = tt.version
				}
				got := compiler.collectNpmDependencies([]*WorkflowData{data})
				want := []NpmDependency{{Name: tt.pkg, Version: expected}}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("version %q: got %v, want %v", version, got, want)
				}
			}
		})
	}
}

func TestDependabotEnginePackageResolution(t *testing.T) {
	tests := []struct {
		name string
		data *WorkflowData
		want []NpmDependency
	}{
		{"legacy engine", &WorkflowData{AI: "codex"}, []NpmDependency{{"@openai/codex", string(constants.DefaultCodexVersion)}}},
		{"config overrides legacy", &WorkflowData{AI: "codex", EngineConfig: &EngineConfig{ID: "claude"}}, []NpmDependency{{"@anthropic-ai/claude-code", string(constants.DefaultClaudeCodeVersion)}}},
		{"custom command", &WorkflowData{EngineConfig: &EngineConfig{ID: "claude", Command: "my-claude"}}, nil},
		{"dynamic version", &WorkflowData{EngineConfig: &EngineConfig{ID: "claude", Version: "${{ inputs.version }}"}}, nil},
		{"dynamic version suffix", &WorkflowData{EngineConfig: &EngineConfig{ID: "claude", Version: "1.2.${{ inputs.patch }}"}}, nil},
		{"unknown engine", &WorkflowData{EngineConfig: &EngineConfig{ID: "not-an-engine"}}, nil},
		{"omitted engine defaults to copilot", &WorkflowData{}, []NpmDependency{{"@github/copilot", string(constants.DefaultCopilotVersion)}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewCompiler().collectNpmDependencies([]*WorkflowData{tt.data})
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDependabotDistinctEngineVersions(t *testing.T) {
	compiler := NewCompiler()
	workflows := []*WorkflowData{
		{EngineConfig: &EngineConfig{ID: "copilot", Version: "1.2.3"}},
		{EngineConfig: &EngineConfig{ID: "copilot", Version: "2.3.4"}},
		{EngineConfig: &EngineConfig{ID: "copilot", Version: "1.2.3"}},
		{CustomSteps: "npx @github/copilot@3.4.5", EngineConfig: &EngineConfig{ID: "copilot", Command: "local-copilot"}},
	}
	got := compiler.collectNpmDependencies(workflows)
	if len(got) != 3 {
		t.Fatalf("got %v, want all three versions exactly once", got)
	}
	versions := map[string]bool{}
	for _, dep := range got {
		if dep.Name == "@github/copilot" {
			versions[dep.Version] = true
		} else if strings.HasPrefix(dep.Name, "gh-aw-engine-") {
			versions[strings.TrimPrefix(dep.Version, "npm:@github/copilot@")] = true
		} else {
			t.Fatalf("unexpected dependency %v", dep)
		}
	}
	for _, version := range []string{"1.2.3", "2.3.4", "3.4.5"} {
		if !versions[version] {
			t.Errorf("missing version %s in %v", version, got)
		}
	}
	reversed := []*WorkflowData{workflows[3], workflows[2], workflows[1], workflows[0]}
	if other := compiler.collectNpmDependencies(reversed); !reflect.DeepEqual(got, other) {
		t.Fatalf("dependency ordering is unstable: %v vs %v", got, other)
	}
	// With no MCP dependency, one engine version uses the canonical package name.
	engines := compiler.collectNpmDependencies(workflows[:3])
	if len(engines) != 2 || engines[0].Name != "@github/copilot" {
		t.Fatalf("got %v, want one canonical dependency and one alias", engines)
	}
}

func TestDependabotDetectionEnginePackages(t *testing.T) {
	compiler := NewCompiler()
	tests := []struct {
		name      string
		main      *EngineConfig
		detection *ThreatDetectionConfig
		want      []NpmDependency
	}{
		{
			"distinct detector", &EngineConfig{ID: "claude", Command: "local-claude"},
			&ThreatDetectionConfig{EngineConfig: &EngineConfig{ID: "codex", Version: "1.2.3"}},
			[]NpmDependency{{"@openai/codex", "1.2.3"}},
		},
		{
			"inherited version", &EngineConfig{ID: "copilot", Version: "1.2.3"},
			&ThreatDetectionConfig{},
			[]NpmDependency{{"@github/copilot", "1.2.3"}},
		},
		{
			"normalized pi detector", &EngineConfig{ID: "pi", Command: "local-pi", Version: "1.2.3"},
			&ThreatDetectionConfig{},
			[]NpmDependency{{"@github/copilot", string(constants.DefaultCopilotVersion)}},
		},
		{
			"disabled detector", &EngineConfig{ID: "claude", Command: "local-claude"},
			&ThreatDetectionConfig{EngineDisabled: true, EngineConfig: &EngineConfig{ID: "codex"}},
			nil,
		},
		{
			"detector custom command", &EngineConfig{ID: "claude", Command: "local-claude"},
			&ThreatDetectionConfig{EngineConfig: &EngineConfig{ID: "codex", Command: "local-codex"}},
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &WorkflowData{EngineConfig: tt.main, SafeOutputs: &SafeOutputsConfig{ThreatDetection: tt.detection}}
			got := compiler.collectNpmDependencies([]*WorkflowData{data})
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
	// A detector can install a different version of the same CLI on its fresh runner.
	data := &WorkflowData{
		EngineConfig: &EngineConfig{ID: "copilot", Version: "1.2.3"},
		SafeOutputs: &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{
			EngineConfig: &EngineConfig{ID: "copilot", Version: "2.3.4"},
		}},
	}
	if got := compiler.collectNpmDependencies([]*WorkflowData{data}); len(got) != 2 {
		t.Fatalf("got %v, want agent and detector versions", got)
	}
}

func registerDependabotTestEngine(c *Compiler, id, manager, pkg, version string) {
	c.engineCatalog.Register(&EngineDefinition{
		ID: id,
		Behaviors: &EngineBehaviorDefinition{Installation: &EngineInstallationDefinition{
			PackageManager: manager, PackageName: pkg, Version: version,
		}},
	})
}

func TestDependabotDeclarativeEnginePackages(t *testing.T) {
	compiler := NewCompiler()
	for _, manager := range []string{"npm", "pip", "uv", "go"} {
		t.Run(manager, func(t *testing.T) {
			registerDependabotTestEngine(compiler, manager, manager, "example", "1.2.3")
			data := &WorkflowData{EngineConfig: &EngineConfig{ID: manager}}
			for _, override := range []string{"", "2.3.4", "${{ inputs.version }}"} {
				data.EngineConfig.Version = override
				var got []string
				switch manager {
				case "npm":
					for _, dep := range compiler.collectNpmDependencies([]*WorkflowData{data}) {
						got = append(got, dep.Name+"@"+dep.Version)
					}
				case "pip", "uv":
					for _, dep := range compiler.collectPipDependencies([]*WorkflowData{data}) {
						got = append(got, dep.Name+dep.Version)
					}
				case "go":
					for _, dep := range compiler.collectGoDependencies([]*WorkflowData{data}) {
						got = append(got, dep.Path+"@"+dep.Version)
					}
				}
				if strings.Contains(override, "$") {
					if len(got) != 0 {
						t.Fatalf("dynamic version should be omitted: %v", got)
					}
					continue
				}
				version := override
				if version == "" {
					version = "1.2.3"
				}
				separator := "@"
				if manager == "pip" || manager == "uv" {
					separator = "=="
				}
				if !reflect.DeepEqual(got, []string{"example" + separator + version}) {
					t.Fatalf("got %v, want version %s", got, version)
				}
			}
			data.EngineConfig.Command = "local-engine"
			if got := compiler.engineInstallations(data); len(got) != 0 {
				t.Fatalf("custom command should not install packages: %v", got)
			}
		})
	}
	registerDependabotTestEngine(compiler, "default-version", "npm", "example", "1.2.3")
	compiler.engineCatalog.Get("default-version").Version = "3.4.5"
	data := &WorkflowData{EngineConfig: &EngineConfig{ID: "default-version"}}
	if got := compiler.enginePackages(data, "npm"); !reflect.DeepEqual(got, []string{"example@3.4.5"}) {
		t.Fatalf("definition default version not applied: %v", got)
	}
	registerDependabotTestEngine(compiler, "python-range", "uv", "example", ">=1.2.3")
	data.EngineConfig.ID = "python-range"
	if got := compiler.enginePackages(data, "pip"); !reflect.DeepEqual(got, []string{"example>=1.2.3"}) {
		t.Fatalf("Python version specifier changed: %v", got)
	}
	compiler.engineCatalog.Register(&EngineDefinition{
		ID: "claude-alias", RuntimeID: "claude", Version: "3.4.5",
	})
	data.EngineConfig.ID = "claude-alias"
	if got := compiler.enginePackages(data, "npm"); !reflect.DeepEqual(got, []string{"@anthropic-ai/claude-code@3.4.5"}) {
		t.Fatalf("runtime alias installation not resolved: %v", got)
	}
}

func TestGenerateDependabotManifests_EngineInstallations(t *testing.T) {
	compiler := NewCompiler()
	root := testutil.TempDir(t, "dependabot-engines-*")
	workflowDir := filepath.Join(root, ".github", "workflows")
	binDir := filepath.Join(root, "bin")
	for _, dir := range []string{workflowDir, binDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(binDir, "npm"), []byte("#!/bin/sh\nprintf '{\"lockfileVersion\":3}\\n' > package-lock.json\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	registerDependabotTestEngine(compiler, "python", "uv", "example-python", "1.2.3")
	registerDependabotTestEngine(compiler, "golang", "go", "example.com/engine", "v1.2.3")
	workflows := []*WorkflowData{
		{EngineConfig: &EngineConfig{ID: "copilot"}},
		{EngineConfig: &EngineConfig{ID: "copilot", Version: "1.2.3"}},
		{EngineConfig: &EngineConfig{ID: "python"}},
		{EngineConfig: &EngineConfig{ID: "golang"}},
	}
	if err := compiler.GenerateDependabotManifests(context.Background(), workflows, workflowDir, false); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(workflowDir, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest PackageJSON
	if err := json.Unmarshal(content, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Dependencies) != 2 {
		t.Fatalf("engine versions were lost: %v", manifest.Dependencies)
	}
	for _, file := range []struct{ path, want string }{
		{"requirements.txt", "example-python==1.2.3"},
		{"go.mod", "example.com/engine v1.2.3"},
		{"package-lock.json", `"lockfileVersion":3`},
		{"../dependabot.yml", "package-ecosystem: npm"},
		{"../dependabot.yml", "package-ecosystem: pip"},
		{"../dependabot.yml", "package-ecosystem: gomod"},
	} {
		content, err := os.ReadFile(filepath.Join(workflowDir, file.path))
		if err != nil || !strings.Contains(string(content), file.want) {
			t.Errorf("%s: got %s (%v), want %q", file.path, content, err, file.want)
		}
	}
}

func TestDependabotImportedEnginePackagesSurviveCatalogReset(t *testing.T) {
	root := testutil.TempDir(t, "dependabot-engine-imports-*")
	compiler := NewCompiler()
	var workflows []*WorkflowData
	for index, version := range []string{"1.2.3", "2.3.4"} {
		definition := fmt.Sprintf(`---
engine:
  id: example-engine
  detection-engine: codex
  behaviors:
    installation:
      package-manager: npm
      package-name: "@example/engine"
      version: %q
    execution:
      command-name: example-engine
---
# Shared engine
`, version)
		definitionName := fmt.Sprintf("engine-%d.md", index)
		if err := os.WriteFile(filepath.Join(root, definitionName), []byte(definition), 0o644); err != nil {
			t.Fatal(err)
		}
		content := fmt.Sprintf(`---
on: workflow_dispatch
permissions:
  contents: read
imports:
  - %s
safe-outputs:
  threat-detection: true
---
# Workflow
`, definitionName)
		path := filepath.Join(root, fmt.Sprintf("workflow-%d.md", index))
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		data, err := compiler.ParseWorkflowFile(path)
		if err != nil {
			t.Fatal(err)
		}
		workflows = append(workflows, data)
	}
	// Simulate parsing another workflow that imports no custom definition.
	compiler.engineRegistry = NewEngineRegistry()
	compiler.engineCatalog = NewEngineCatalog(compiler.engineRegistry)
	deps := compiler.collectNpmDependencies(workflows)
	if len(deps) != 3 {
		t.Fatalf("got %v, want both imported versions and the declared detector", deps)
	}
	found := map[string]bool{}
	for _, dep := range deps {
		found[dep.Name+"@"+dep.Version] = true
	}
	if !found["@example/engine@1.2.3"] || !found["@openai/codex@"+string(constants.DefaultCodexVersion)] {
		t.Fatalf("imported engine metadata was lost: %v", deps)
	}
	aliasFound := false
	for _, dep := range deps {
		aliasFound = aliasFound || dep.Version == "npm:@example/engine@2.3.4"
	}
	if !aliasFound {
		t.Fatalf("second imported version was lost: %v", deps)
	}
}

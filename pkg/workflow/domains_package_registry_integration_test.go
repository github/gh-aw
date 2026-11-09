//go:build integration

package workflow_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workflow"
	"gopkg.in/yaml.v3"
)

// TestCompiledWorkflowGatesPackageRegistries compiles real workflows and asserts that the
// firewall allow-list in the generated lock file only contains package registry domains when
// the workflow explicitly opts into the corresponding ecosystem. Selecting an engine alone
// must never make npm or PyPI reachable from the sandbox.
func TestCompiledWorkflowGatesPackageRegistries(t *testing.T) {
	registries := []string{"registry.npmjs.org", "pypi.org", "files.pythonhosted.org"}

	tests := []struct {
		name              string
		engine            string
		removeRuntime     bool
		networkBlock      string
		expectContains    []string
		expectNotContains []string
	}{
		{
			name:              "copilot deny-all network",
			engine:            "copilot",
			networkBlock:      "network: {}",
			expectNotContains: registries,
		},
		{
			name:              "copilot defaults and github",
			engine:            "copilot",
			networkBlock:      "network:\n  allowed:\n    - defaults\n    - github",
			expectNotContains: registries,
		},
		{
			name:              "claude deny-all network",
			engine:            "claude",
			networkBlock:      "network: {}",
			expectNotContains: registries,
		},
		{
			name:              "claude defaults and github",
			engine:            "claude",
			networkBlock:      "network:\n  allowed:\n    - defaults\n    - github",
			expectNotContains: registries,
		},
		{
			name:              "copilot explicit node opt-in",
			engine:            "copilot",
			networkBlock:      "network:\n  allowed:\n    - defaults\n    - node",
			expectContains:    []string{"registry.npmjs.org"},
			expectNotContains: []string{"pypi.org", "files.pythonhosted.org"},
		},
		{
			name:              "copilot explicit python opt-in",
			engine:            "copilot",
			networkBlock:      "network:\n  allowed:\n    - defaults\n    - python",
			expectContains:    []string{"pypi.org", "files.pythonhosted.org"},
			expectNotContains: []string{"registry.npmjs.org"},
		},
		{
			name:           "copilot node runtime declaration",
			engine:         "copilot",
			networkBlock:   "network:\n  allowed:\n    - defaults\nruntimes:\n  node:\n    version: \"24\"",
			expectContains: []string{"registry.npmjs.org"},
		},
		{
			name:              "aider without imported python runtime",
			engine:            "aider",
			networkBlock:      "network:\n  allowed: []",
			expectNotContains: append(registries, "api.openai.com"),
		},
		{
			name:              "aider explicit python opt-in",
			engine:            "aider",
			networkBlock:      "network:\n  allowed:\n    - python",
			expectContains:    []string{"pypi.org", "files.pythonhosted.org"},
			expectNotContains: []string{"registry.npmjs.org"},
		},
		{
			name:              "aider python runtime opt-in",
			engine:            "aider",
			networkBlock:      "network:\n  allowed: []\nruntimes:\n  python:\n    version: \"3.12\"",
			expectContains:    []string{"pypi.org", "files.pythonhosted.org"},
			expectNotContains: []string{"registry.npmjs.org"},
		},
		{
			name:              "mini-SWE full shared import opts into python runtime",
			engine:            "mini-swe-agent",
			networkBlock:      "network:\n  allowed: []",
			expectContains:    []string{"api.githubcopilot.com", "host.docker.internal", "pypi.org", "files.pythonhosted.org"},
			expectNotContains: []string{"registry.npmjs.org"},
		},
		{
			name:              "mini-SWE shared import without runtime excludes registries",
			engine:            "mini-swe-agent",
			removeRuntime:     true,
			networkBlock:      "network:\n  allowed: []",
			expectContains:    []string{"api.githubcopilot.com", "host.docker.internal"},
			expectNotContains: registries,
		},
		{
			name:              "mini-SWE explicit python network opt-in",
			engine:            "mini-swe-agent",
			removeRuntime:     true,
			networkBlock:      "network:\n  allowed:\n    - python",
			expectContains:    []string{"pypi.org", "files.pythonhosted.org"},
			expectNotContains: []string{"registry.npmjs.org"},
		},
		{
			name:              "mini-SWE caller python runtime opt-in",
			engine:            "mini-swe-agent",
			removeRuntime:     true,
			networkBlock:      "network:\n  allowed: []\nruntimes:\n  python:\n    version: \"3.12\"",
			expectContains:    []string{"pypi.org", "files.pythonhosted.org"},
			expectNotContains: []string{"registry.npmjs.org"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := testutil.TempDir(t, "registry-gate-*")
			workflowPath := filepath.Join(tempDir, "test-workflow.md")
			engineBlock := "engine: " + tt.engine + "\n"
			if tt.engine == "aider" {
				// Load the real engine definition without the import's runtime opt-in.
				source, err := os.ReadFile("../../.github/workflows/shared/aider.md")
				if err != nil {
					t.Fatal(err)
				}
				parts := strings.SplitN(string(source), "---", 3)
				if len(parts) != 3 {
					t.Fatal("shared Aider workflow has invalid frontmatter")
				}
				var frontmatter struct {
					Engine yaml.Node `yaml:"engine"`
				}
				if err := yaml.Unmarshal([]byte(parts[1]), &frontmatter); err != nil {
					t.Fatal(err)
				}
				engineYAML, err := yaml.Marshal(frontmatter)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(tempDir, "engine.md"),
					[]byte("---\n"+string(engineYAML)+"---\n"), 0600); err != nil {
					t.Fatal(err)
				}
				engineBlock = "engine:\n  id: aider\nmodel: openai/gpt-5\nimports:\n  - engine.md\n"
			}
			if tt.engine == "mini-swe-agent" {
				source, err := os.ReadFile("../../.github/workflows/shared/mini-swe-agent.md")
				if err != nil {
					t.Fatal(err)
				}
				if tt.removeRuntime {
					parts := strings.SplitN(string(source), "---", 3)
					if len(parts) != 3 {
						t.Fatal("shared mini-SWE workflow has invalid frontmatter")
					}
					var frontmatter map[string]any
					if err := yaml.Unmarshal([]byte(parts[1]), &frontmatter); err != nil {
						t.Fatal(err)
					}
					// Preserve bootstrap steps and environment exclusions; only remove
					// the imported runtime's explicit network capability.
					delete(frontmatter, "runtimes")
					frontmatterYAML, err := yaml.Marshal(frontmatter)
					if err != nil {
						t.Fatal(err)
					}
					source = []byte("---\n" + string(frontmatterYAML) + "---\n" + parts[2])
				}
				if err := os.WriteFile(filepath.Join(tempDir, "engine.md"), source, 0600); err != nil {
					t.Fatal(err)
				}
				engineBlock = "engine:\n  id: mini-swe-agent\nmodel: copilot/claude-haiku-5.5\nimports:\n  - engine.md\n"
			}
			content := "---\non: issues\npermissions:\n  contents: read\n" + engineBlock +
				"strict: false\n" + tt.networkBlock + "\n---\n\n# Registry Gate Test\n\nDo nothing.\n"
			if err := os.WriteFile(workflowPath, []byte(content), 0644); err != nil {
				t.Fatalf("failed to write workflow: %v", err)
			}

			compiler := workflow.NewCompiler()
			if err := compiler.CompileWorkflow(workflowPath); err != nil {
				t.Fatalf("CompileWorkflow failed: %v", err)
			}

			lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
			if err != nil {
				t.Fatalf("failed to read lock file: %v", err)
			}
			lock := string(lockContent)
			allowedDomains := strings.Join(extractCompiledAWFAllowedDomains(lock), "\n")
			if tt.engine == "mini-swe-agent" {
				var compiled struct {
					Jobs map[string]struct {
						Env   map[string]string `yaml:"env"`
						Steps []struct {
							Name string `yaml:"name"`
							Run  string `yaml:"run"`
						} `yaml:"steps"`
					} `yaml:"jobs"`
				}
				if err := yaml.Unmarshal(lockContent, &compiled); err != nil {
					t.Fatal(err)
				}
				var agentRun, bootstrapRun string
				bootstrapIndex, executionIndex := -1, -1
				for _, job := range compiled.Jobs {
					for index, step := range job.Steps {
						switch step.Name {
						case "Stage hash-locked mini-SWE-agent wheels":
							bootstrapRun, bootstrapIndex = step.Run, index
							if job.Env["GH_AW_ENGINE_VERSION"] != "2.4.6" {
								t.Errorf("bootstrap must receive the pinned engine version, got %q", job.Env["GH_AW_ENGINE_VERSION"])
							}
						case "Execute mini-SWE-agent":
							agentRun, executionIndex = step.Run, index
						}
					}
				}
				if bootstrapIndex < 0 || executionIndex <= bootstrapIndex {
					t.Fatal("trusted wheel staging must precede AWF agent execution")
				}
				if strings.Contains(bootstrapRun, "awf ") || !strings.Contains(bootstrapRun, `"pip", "download"`) {
					t.Fatal("wheel downloads must run in the trusted bootstrap, outside AWF")
				}
				if !strings.Contains(agentRun, "--no-index") || !strings.Contains(agentRun, "--require-hashes") {
					t.Fatal("agent dependency installation must be offline and hash verified")
				}
				// Assert emitted AWF rules, not incidental mentions in scripts/docs.
				agentRun = strings.ReplaceAll(agentRun, `\"`, `"`)
				matches := regexp.MustCompile(`"allowDomains":(\[[^]]*\])`).FindAllStringSubmatch(agentRun, -1)
				if len(matches) != 1 {
					t.Fatalf("expected one agent AWF allow-list, got %d", len(matches))
				}
				var rules []string
				if err := json.Unmarshal([]byte(matches[0][1]), &rules); err != nil {
					t.Fatal(err)
				}
				for _, expected := range tt.expectContains {
					if !strings.Contains(","+strings.Join(rules, ",")+",", ","+expected+",") {
						t.Errorf("expected AWF allow-list to contain %q: %v", expected, rules)
					}
				}
				for _, unexpected := range tt.expectNotContains {
					for _, rule := range rules {
						suffix := strings.TrimPrefix(rule, "*.")
						if unexpected == suffix || strings.HasSuffix(unexpected, "."+suffix) {
							t.Errorf("AWF rule %q must not allow registry %q", rule, unexpected)
						}
					}
				}
				return
			}

			for _, expected := range tt.expectContains {
				if !strings.Contains(allowedDomains, expected) {
					t.Errorf("expected compiled workflow to allow %q", expected)
				}
			}
			for _, unexpected := range tt.expectNotContains {
				if strings.Contains(allowedDomains, unexpected) {
					t.Errorf("compiled workflow must not allow %q without explicit ecosystem opt-in", unexpected)
				}
			}
		})
	}
}

func extractCompiledAWFAllowedDomains(lock string) []string {
	lock = strings.ReplaceAll(lock, `\"`, `"`)
	matches := regexp.MustCompile(`"allowDomains"\s*:\s*(\[[^\]]*\])`).FindAllStringSubmatch(lock, -1)
	var domains []string
	for _, match := range matches {
		var current []string
		if err := json.Unmarshal([]byte(match[1]), &current); err == nil {
			domains = append(domains, current...)
		}
	}
	return domains
}

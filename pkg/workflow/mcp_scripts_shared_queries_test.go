//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
)

func loadSharedMCPScripts(t *testing.T, filename string) *MCPScriptsConfig {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "shared", filename))
	if err != nil {
		t.Fatal(err)
	}
	result, err := parser.ExtractFrontmatterFromContent(string(content))
	if err != nil {
		t.Fatal(err)
	}
	scripts, err := json.Marshal(result.Frontmatter["mcp-scripts"])
	if err != nil {
		t.Fatal(err)
	}
	return NewCompiler().mergeMCPScripts(nil, []string{string(scripts)})
}

func TestSharedGitHubQueryRepositoryBindings(t *testing.T) {
	config := loadSharedMCPScripts(t, "github-queries-mcp-script.md")
	var manifest MCPScriptsConfigJSON
	if err := json.Unmarshal([]byte(GenerateMCPScriptsToolsConfig(config)), &manifest); err != nil {
		t.Fatal(err)
	}
	var setup strings.Builder
	if err := generateMCPScriptsSetup(&setup, &WorkflowData{MCPScripts: config}); err != nil {
		t.Fatal(err)
	}
	startup := strings.Split(setup.String(), "      - name: Start MCP Scripts HTTP Server\n")[1]
	if len(manifest.Tools) != 3 {
		t.Fatalf("expected three GitHub query tools, got %d", len(manifest.Tools))
	}
	for _, tool := range manifest.Tools {
		t.Run(tool.Name, func(t *testing.T) {
			expected := map[string]string{
				"GH_TOKEN":          "${{ secrets.GITHUB_TOKEN }}",
				"GITHUB_REPOSITORY": "${{ github.repository }}",
			}
			if tool.Name == "github-discussion-query" {
				expected["GITHUB_REPOSITORY_OWNER"] = "${{ github.repository_owner }}"
			}
			if !reflect.DeepEqual(config.Tools[tool.Name].Env, expected) {
				t.Fatalf("env = %v, want only required bindings %v", config.Tools[tool.Name].Env, expected)
			}
			if len(tool.Env) != len(expected) {
				t.Fatalf("manifest env = %v, want %d bindings", tool.Env, len(expected))
			}
			for name, expression := range expected {
				source := mcpScriptEnvBindingName(tool.Name, name)
				if tool.Env[name] != source {
					t.Errorf("manifest %s must reference scoped binding %s", name, source)
				}
				if !strings.Contains(startup, source+": "+expression+"\n") {
					t.Errorf("startup must bind %s to %s", source, expression)
				}
				if strings.Contains(startup, "          "+name+": ") {
					t.Errorf("startup must not expose unscoped %s", name)
				}
			}
		})
	}
}

// Execute repository selection only, before any API calls or scratch-file writes.
func TestSharedGitHubQueryRepositoryDefaults(t *testing.T) {
	config := loadSharedMCPScripts(t, "github-queries-mcp-script.md")
	for _, name := range []string{"github-issue-query", "github-pr-query", "github-discussion-query"} {
		windows := []string{"2026-10-01"}
		if name == "github-discussion-query" {
			windows = append(windows, "")
		}
		for _, since := range windows {
			for _, repo := range []string{"", "other/project"} {
				t.Run(name+"/repo="+repo+"/since="+since, func(t *testing.T) {
					run := config.Tools[name].Run
					var script, expected string
					wantRepo := repo
					if wantRepo == "" {
						wantRepo = "current/repository"
					}
					if name == "github-discussion-query" {
						prefix, _, ok := strings.Cut(run, "# Build GraphQL query")
						if !ok {
							t.Fatal("missing GraphQL query boundary")
						}
						script = prefix + "\nprintf '%s/%s' \"$OWNER\" \"$NAME\"\n"
						expected = wantRepo
					} else {
						defaults, _, ok := strings.Cut(run, "OUTPUT_FILE=")
						if !ok {
							t.Fatal("missing output-file boundary")
						}
						_, pagination, ok := strings.Cut(run, "# Fetch all items updated in a date window. REST results are ordered by\n")
						if !ok {
							t.Fatal("missing pagination boundary")
						}
						selection, _, ok := strings.Cut(pagination, "PAGE=1")
						if !ok {
							t.Fatal("missing pagination loop boundary")
						}
						script = defaults + selection + "fi\nprintf '%s' \"$API_PATH\"\n"
						resource := "issues"
						if name == "github-pr-query" {
							resource = "pulls"
						}
						expected = "repos/" + wantRepo + "/" + resource
					}
					// Date normalization is unrelated to repository selection and uses GNU date on runners.
					script = "date() { printf '%s\\n' '2026-10-01T00:00:00Z'; }\n" + script
					cmd := exec.Command("bash", "-c", script)
					cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "INPUT_REPO=" + repo, "INPUT_SINCE=" + since}
					context := map[string]string{
						"${{ github.repository }}":       "current/repository",
						"${{ github.repository_owner }}": "current",
					}
					for envName, expression := range config.Tools[name].Env {
						if value, ok := context[expression]; ok {
							cmd.Env = append(cmd.Env, envName+"="+value)
						}
					}
					output, err := cmd.CombinedOutput()
					if err != nil {
						t.Fatalf("repository selection failed: %v\n%s", err, output)
					}
					if string(output) != expected {
						t.Errorf("repository selection = %q, want %q", output, expected)
					}
				})
			}
		}
	}
}

func TestSharedPRDataRepositoryDefault(t *testing.T) {
	config := loadSharedMCPScripts(t, "pr-data-mcp-script.md")
	tool := config.Tools["fetch-pr-data"]
	if tool == nil {
		t.Fatal("missing fetch-pr-data tool")
	}
	expectedEnv := map[string]string{
		"GH_TOKEN":          "${{ secrets.GITHUB_TOKEN }}",
		"GITHUB_REPOSITORY": "${{ github.repository }}",
	}
	if !reflect.DeepEqual(tool.Env, expectedEnv) {
		t.Fatalf("env = %v, want only required bindings %v", tool.Env, expectedEnv)
	}
	refs := generateMCPScriptEnvRefs(tool.Name, tool.Env)
	if refs["GITHUB_REPOSITORY"] != mcpScriptEnvBindingName(tool.Name, "GITHUB_REPOSITORY") {
		t.Fatal("repository context must use a tool-scoped transport binding")
	}
	for _, repo := range []string{"", "other/project"} {
		t.Run("repo="+repo, func(t *testing.T) {
			// Stub the CLI and clock, leaving the complete default-selection script intact.
			script := "date() { printf '%s\\n' '2026-10-01'; }\ngh() { printf '%s\\n' \"$@\"; }\n" + tool.Run
			cmd := exec.Command("bash", "-c", script)
			cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "INPUT_REPO=" + repo, "GITHUB_REPOSITORY=current/repository"}
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("fetch-pr-data failed: %v\n%s", err, output)
			}
			wantRepo := repo
			if wantRepo == "" {
				wantRepo = "current/repository"
			}
			expected := strings.Join([]string{
				"pr", "list", "--repo", wantRepo, "--search", "created:>=2026-10-01",
				"--state", "all", "--json",
				"number,title,author,headRefName,createdAt,state,url,body,labels,updatedAt,closedAt,mergedAt",
				"--limit", "100", "",
			}, "\n")
			if string(output) != expected {
				t.Errorf("CLI arguments = %q, want %q", output, expected)
			}
		})
	}
}

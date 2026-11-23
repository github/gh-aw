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

func requireSharedGitHubQueryPrerequisites(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is required for bounded GitHub query tests")
	}
	if err := exec.Command("date", "-u", "-d", "2026-10-01", "+%s").Run(); err != nil {
		t.Skip("GNU-compatible date is required for bounded GitHub query tests")
	}
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

// Execute repository selection only, before any API calls or pagination.
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
						_, sinceBranch, ok := strings.Cut(run, `elif [[ -n "$SINCE" ]]; then`)
						if !ok {
							t.Fatal("missing since branch")
						}
						repoSelection, _, ok := strings.Cut(sinceBranch, "PAGE=1")
						if !ok {
							t.Fatal("missing pagination loop boundary")
						}
						script = `REPO="${INPUT_REPO:-}"
SINCE="${INPUT_SINCE:-}"` + "\n" + repoSelection + "\nprintf '%s' \"$API_PATH\"\n"
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

func TestSharedGitHubQueryBoundedRangesRetryAndSplit(t *testing.T) {
	requireSharedGitHubQueryPrerequisites(t)
	config := loadSharedMCPScripts(t, "github-queries-mcp-script.md")
	for _, name := range []string{"github-issue-query", "github-pr-query"} {
		t.Run(name, func(t *testing.T) {
			if timeout := config.Tools[name].Timeout; timeout != 600 {
				t.Fatalf("timeout = %d, want 600 seconds", timeout)
			}
			callLog := filepath.Join(t.TempDir(), "queries.log")
			stub := `GH_CALLS=0
DAY_ONE_CALLS=0
gh() {
  local search=""
  while [[ $# -gt 0 ]]; do
    if [[ "$1" == "--search" ]]; then
      search="$2"
      shift 2
    else
      shift
    fi
  done
  printf '%s\n' "$search" >> "$GH_CALL_LOG"
  case "$search" in
    "updated:2026-10-01..2026-10-04")
      GH_CALLS=$((GH_CALLS + 1))
      if [[ "$GH_CALLS" -eq 1 ]]; then
        echo "HTTP 502: 502 Bad Gateway" >&2
        return 1
      fi
      printf '[{"number":1,"updatedAt":"2026-10-01T12:00:00Z"},{"number":2,"updatedAt":"2026-10-02T12:00:00Z"}]\n'
      ;;
    "updated:2026-10-01..2026-10-02")
      printf '[{"number":1,"updatedAt":"2026-10-01T12:00:00Z"},{"number":2,"updatedAt":"2026-10-02T12:00:00Z"}]\n'
      ;;
    "updated:2026-10-01..2026-10-01")
      DAY_ONE_CALLS=$((DAY_ONE_CALLS + 1))
      if [[ "$DAY_ONE_CALLS" -eq 1 ]]; then
        printf '{"number":'
        return 0
      fi
      printf '[{"number":1,"updatedAt":"2026-10-01T12:00:00Z"}]\n'
      ;;
    "updated:2026-10-02..2026-10-02")
      printf '[{"number":2,"updatedAt":"2026-10-02T12:00:00Z"}]\n'
      ;;
    "updated:2026-10-03..2026-10-04")
      printf '[{"number":3,"updatedAt":"2026-10-03T12:00:00Z"}]\n'
      ;;
    *)
      echo "unexpected query range: $search" >&2
      return 1
      ;;
  esac
}
`
			cmd := exec.Command("bash", "-c", stub+config.Tools[name].Run)
			cmd.Env = []string{
				"PATH=" + os.Getenv("PATH"),
				"GH_CALL_LOG=" + callLog,
				"INPUT_STATE=all",
				"INPUT_SINCE=2026-10-01T00:00:00Z",
				"INPUT_UNTIL=2026-10-04T23:59:59Z",
				"INPUT_LIMIT=2",
				"INPUT_JQ=.",
				"GITHUB_REPOSITORY=current/repository",
			}
			output, err := cmd.Output()
			if err != nil {
				t.Fatalf("bounded query failed: %v", err)
			}
			var items []struct {
				Number int `json:"number"`
			}
			if err := json.Unmarshal(output, &items); err != nil {
				t.Fatalf("query output is invalid JSON: %v\n%s", err, output)
			}
			var numbers []int
			for _, item := range items {
				numbers = append(numbers, item.Number)
			}
			if want := []int{1, 2, 3}; !reflect.DeepEqual(numbers, want) {
				t.Errorf("query item numbers = %v, want %v", numbers, want)
			}
			log, err := os.ReadFile(callLog)
			if err != nil {
				t.Fatal(err)
			}
			wantCalls := []string{
				"updated:2026-10-01..2026-10-04",
				"updated:2026-10-01..2026-10-04",
				"updated:2026-10-01..2026-10-02",
				"updated:2026-10-01..2026-10-01",
				"updated:2026-10-01..2026-10-01",
				"updated:2026-10-02..2026-10-02",
				"updated:2026-10-03..2026-10-04",
			}
			if got := strings.Fields(string(log)); !reflect.DeepEqual(got, wantCalls) {
				t.Errorf("query ranges = %v, want %v", got, wantCalls)
			}
		})
	}
}

func TestSharedGitHubQuerySingleDayLimitFails(t *testing.T) {
	requireSharedGitHubQueryPrerequisites(t)
	config := loadSharedMCPScripts(t, "github-queries-mcp-script.md")
	script := `gh() {
  printf '[{"number":1,"updatedAt":"2026-10-01T12:00:00Z"},{"number":2,"updatedAt":"2026-10-01T13:00:00Z"}]\n'
}
` + config.Tools["github-issue-query"].Run
	cmd := exec.Command("bash", "-c", script)
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"INPUT_STATE=all",
		"INPUT_SINCE=2026-10-01T00:00:00Z",
		"INPUT_UNTIL=2026-10-01T23:59:59Z",
		"INPUT_LIMIT=1",
		"INPUT_JQ=.",
		"GITHUB_REPOSITORY=current/repository",
	}
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected a saturated single-day query to fail")
	}
	if !strings.Contains(string(output), "item limit for single-day range 2026-10-01") {
		t.Fatalf("error = %q, want single-day item-limit guidance", output)
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

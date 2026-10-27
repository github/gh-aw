package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/github/gh-aw-mcpg/internal/config"
	"github.com/github/gh-aw-mcpg/internal/guard"
	"github.com/stretchr/testify/require"
)

// Run in the pinned gateway's internal/server package via the Python test's overlay.
func TestDailyPRReviewRuntimeRepositoryScope(t *testing.T) {
	wasm := os.Getenv("AWMG_WASM_GUARD_PATH")
	require.NotEmpty(t, wasm)
	_, err := os.Stat(wasm)
	require.NoError(t, err)
	t.Setenv(guard.WASMGuardsDirEnvVar, "")
	t.Setenv("GITHUB_REPOSITORY", "github/gh-aw")
	t.Setenv("GITHUB_TOKEN", "local-test-token")
	t.Setenv("GITHUB_MCP_SERVER_TOKEN", "local-test-token")
	t.Setenv("GITHUB_API_URL", startMockRepoVisibilityServer(t, "public", false).URL)
	override := os.Getenv(config.EnvForcePublicRepos)
	require.Equal(t, "false", override, "must test the workflow's transported override")

	for _, forcePublic := range []string{"true", override} {
		t.Run("force-public="+forcePublic, func(t *testing.T) {
			t.Setenv(config.EnvForcePublicRepos, forcePublic)
			for _, repo := range []string{"gh-aw", "another-public-repo"} {
				t.Run(repo, func(t *testing.T) {
					const diff = "diff --git a/example.go b/example.go\n+untrusted change\n"
					backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.Method != http.MethodPost {
							w.WriteHeader(http.StatusMethodNotAllowed)
							return
						}
						var request struct {
							ID     interface{} `json:"id"`
							Method string      `json:"method"`
							Params struct {
								Name      string                 `json:"name"`
								Arguments map[string]interface{} `json:"arguments"`
							} `json:"params"`
						}
						if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
							t.Errorf("decode MCP request: %v", err)
							w.WriteHeader(http.StatusBadRequest)
							return
						}
						var result interface{}
						switch request.Method {
						case "initialize":
							result = map[string]interface{}{
								"protocolVersion": "2024-11-05", "capabilities": map[string]interface{}{},
								"serverInfo": map[string]interface{}{"name": "local-github", "version": "1"},
							}
						case "tools/list":
							result = map[string]interface{}{"tools": []interface{}{
								map[string]interface{}{"name": "pull_request_read", "inputSchema": map[string]interface{}{"type": "object"}},
								map[string]interface{}{"name": "search_repositories", "inputSchema": map[string]interface{}{"type": "object"}},
							}}
						case "tools/call":
							text := diff
							if request.Params.Name == "search_repositories" {
								text = `{"items":[{"full_name":"github/` + repo + `","private":false,"owner":{"type":"Organization"}}]}`
							} else if request.Params.Arguments["method"] == "get" {
								text = `{"number":1,"state":"open","author_association":"NONE","user":{"login":"outsider"},"base":{"repo":{"full_name":"github/` + repo + `"}},"head":{"repo":{"full_name":"outsider/fork"}}}`
							}
							result = map[string]interface{}{"content": []interface{}{
								map[string]interface{}{"type": "text", "text": text},
							}, "isError": false}
						default:
							w.WriteHeader(http.StatusAccepted)
							return
						}
						w.Header().Set("Content-Type", "application/json")
						if err := json.NewEncoder(w).Encode(map[string]interface{}{
							"jsonrpc": "2.0", "id": request.ID, "result": result,
						}); err != nil {
							t.Errorf("encode MCP response: %v", err)
						}
					}))
					defer backend.Close()

					// Registration mutates policies in the positive control, so decode afresh.
					var freshPolicy map[string]interface{}
					require.NoError(t, json.Unmarshal([]byte(os.Getenv("DAILY_REVIEW_READ_POLICY")), &freshPolicy))
					us, err := NewUnified(context.Background(), &config.Config{
						Gateway:  &config.GatewayConfig{},
						DIFCMode: "filter",
						Servers: map[string]*config.ServerConfig{
							"github": {Type: "http", URL: backend.URL, Guard: "github-guard", GuardPolicies: freshPolicy},
						},
						Guards: map[string]*config.GuardConfig{
							"github-guard": {Type: "wasm", Path: wasm},
						},
					})
					require.NoError(t, err)
					defer us.Close()
					require.NotEqual(t, "noop", us.guardRegistry.Get("github").Name())
					result, data, err := us.callBackendTool(callCtx("daily-review"), "github", "pull_request_read",
						map[string]interface{}{"owner": "github", "repo": repo, "pullNumber": 1, "method": "get_diff"})
					allowed := repo == "gh-aw" || forcePublic == "true"
					if allowed {
						require.NoError(t, err)
						require.NotNil(t, result)
						require.False(t, result.IsError)
					} else {
						require.ErrorContains(t, err, "[Filtered]")
						require.ErrorContains(t, err, "integrity policy")
					}
					encoded, err := json.Marshal(result)
					require.NoError(t, err)
					require.Equal(t, allowed, strings.Contains(string(encoded), "untrusted change"), string(encoded))
					if !allowed {
						encodedData, err := json.Marshal(data)
						require.NoError(t, err)
						require.NotContains(t, string(encodedData), "untrusted change")
					}
				})
			}
		})
	}
}

func TestDailyPRReviewRuntimeSinkVisibility(t *testing.T) {
	require.Equal(t, "false", os.Getenv(config.EnvForcePublicRepos))
	t.Setenv(guard.WASMGuardsDirEnvVar, "")
	t.Setenv("GITHUB_REPOSITORY", "github/gh-aw")
	t.Setenv("GITHUB_TOKEN", "local-test-token")
	t.Setenv("GITHUB_MCP_SERVER_TOKEN", "local-test-token")
	t.Setenv("GITHUB_API_URL", startMockRepoVisibilityServer(t, "public", false).URL)
	us := newMinimalUnifiedServerForGuardTest(&config.Config{
		Gateway: &config.GatewayConfig{},
		Servers: map[string]*config.ServerConfig{
			"safeoutputs": {GuardPolicies: map[string]interface{}{
				"write-sink": map[string]interface{}{
					"accept": []interface{}{"private:github/gh-aw"}, "sink-visibility": "private",
				},
			}},
		},
	})
	require.False(t, us.isServerExemptFromSinkVisibility("safeoutputs"))
	require.NoError(t, us.registerGuard("safeoutputs"))
	sink, ok := us.guardRegistry.Get("safeoutputs").(*guard.WriteSinkGuard)
	require.True(t, ok)
	require.Equal(t, "public", sink.SinkVisibility(), "runtime public visibility must still remove private sink accept tags")
}

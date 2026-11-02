//go:build !integration

package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

const connectorAssemblyFixture = "testdata/connector-assembly"

type assemblySelection struct {
	Definition  string                    `yaml:"definition"`
	Connection  string                    `yaml:"connection"`
	On          []string                  `yaml:"on"`
	Tools       []string                  `yaml:"tools"`
	Context     map[string]map[string]any `yaml:"context"`
	SafeOutputs []string                  `yaml:"safe-outputs"`
}

func assemblyExampleSelection(t *testing.T, name string) assemblySelection {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(connectorAssemblyFixture, ".github/workflows", name+".md"))
	require.NoError(t, err)
	parts := strings.SplitN(string(data), "---", 3)
	require.Len(t, parts, 3)
	var document struct {
		Connectors map[string]assemblySelection `yaml:"connectors"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(parts[1]), &document))
	require.Len(t, document.Connectors, 1)
	return document.Connectors["issues"]
}

func assemblyBinding(selection assemblySelection) connectors.Binding {
	binding := connectors.Binding{
		APIVersion: connectors.APIVersion,
		Kind:       "ConnectorBinding", Definition: selection.Definition, Connection: selection.Connection,
		Triggers: selection.On, MCP: selection.Tools, SafeOutputs: selection.SafeOutputs,
	}
	for recipe := range selection.Context {
		binding.Context = append(binding.Context, recipe)
	}
	return binding
}

func TestConnectorAssemblyExampleSelections(t *testing.T) {
	for _, name := range []string{"tools-only", "context-only", "effects-only", "combined"} {
		t.Run(name, func(t *testing.T) {
			selection := assemblyExampleSelection(t, name)
			require.Empty(t, selection.On, "dispatch examples must not claim live connector ingress")
			require.Equal(t, name == "tools-only" || name == "combined", len(selection.Tools) > 0)
			require.Equal(t, name == "context-only" || name == "combined", len(selection.Context) > 0)
			require.Equal(t, name == "effects-only" || name == "combined", len(selection.SafeOutputs) > 0)
			plan, err := connectors.Lower(connectorAssemblyFixture, assemblyBinding(selection))
			require.NoError(t, err)
			require.NotEmpty(t, plan.Digest)
		})
	}
}

func TestConnectorAssemblyExampleCompilation(t *testing.T) {
	for _, name := range []string{"tools-only", "context-only", "effects-only", "combined"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.CopyFS(root, os.DirFS(connectorAssemblyFixture)))
			c := NewCompiler()
			c.gitRoot = root
			c.SetActionMode(ActionModeDev)
			c.version = "v0.0.0-1-g683ddf9617"
			path := filepath.Join(root, ".github/workflows", name+".md")
			require.NoError(t, c.CompileWorkflow(path))
			lock, err := os.ReadFile(strings.TrimSuffix(path, ".md") + ".lock.yml")
			require.NoError(t, err)
			var document struct {
				Jobs map[string]any `yaml:"jobs"`
			}
			require.NoError(t, yaml.Unmarshal(lock, &document))
			require.Contains(t, document.Jobs, "agent")
			agent, err := json.Marshal(document.Jobs["agent"])
			require.NoError(t, err)
			safe, err := json.Marshal(document.Jobs["safe_outputs"])
			require.NoError(t, err)
			reads := name == "tools-only" || name == "combined"
			prepares := name == "context-only" || name == "combined"
			writes := name == "effects-only" || name == "combined"
			require.Equal(t, reads, strings.Contains(string(agent), "${{ secrets.ASSEMBLY_READ_TOKEN }}"))
			require.Equal(t, prepares, strings.Contains(string(agent), "${{ secrets.ASSEMBLY_CONTEXT_TOKEN }}"))
			require.NotContains(t, string(agent), "${{ secrets.ASSEMBLY_WRITE_TOKEN }}")
			require.Equal(t, prepares, strings.Contains(string(agent), "connectors invoke context"))
			require.Equal(t, prepares, strings.Contains(string(agent), "/tmp/gh-aw/connector-context.jsonl"))
			require.Equal(t, writes, strings.Contains(string(safe), "${{ secrets.ASSEMBLY_WRITE_TOKEN }}"))
			require.NotContains(t, string(safe), "${{ secrets.ASSEMBLY_READ_TOKEN }}")
			require.NotContains(t, string(safe), "${{ secrets.ASSEMBLY_CONTEXT_TOKEN }}")
			if reads {
				require.Contains(t, string(agent), "connector-issues")
				require.Contains(t, string(agent), "get-issue")
			}
			if writes {
				require.Contains(t, string(safe), "connector-6-issues-add-comment")
				require.Contains(t, string(safe), "--allow-writes")
			}
			if name == "combined" {
				require.Contains(t, string(safe), "create_issue")
				require.Contains(t, string(agent), "github")
			}
		})
	}
}

func TestConnectorAssemblyExampleCompilationRejectsUnsafeSelections(t *testing.T) {
	for _, test := range []struct {
		name, selection, diagnostic string
	}{
		{"unknown-tool", "tools: [missing]", "must name exactly one"},
		{"write-as-tool", "tools: [add-comment]", "must name exactly one"},
		{"unknown-effect", "safe-outputs: [missing]", "unknown safe-output"},
		{"unknown-context", "context: {missing: {}}", "unknown context"},
		{"unapproved-context", "context: {issue-details: {issue: OTHER}}", "outside approved resources"},
		{"expression-resource", "context: {issue-details: {issue: '${{ github.event.issue.title }}'}}", "outside approved resources"},
		{"trigger-ingress", "on: [issue-created]", "ingress"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.CopyFS(root, os.DirFS(connectorAssemblyFixture)))
			content, err := os.ReadFile(filepath.Join(root, ".github/workflows/tools-only.md"))
			require.NoError(t, err)
			content = []byte(strings.Replace(string(content), "tools: [get-issue]", test.selection, 1))
			path := filepath.Join(root, ".github/workflows", test.name+".md")
			require.NoError(t, os.WriteFile(path, content, 0600))
			c := NewCompiler()
			c.gitRoot = root
			err = c.CompileWorkflow(path)
			require.ErrorContains(t, err, test.diagnostic)
			_, err = os.Stat(strings.TrimSuffix(path, ".md") + ".lock.yml")
			require.ErrorIs(t, err, os.ErrNotExist, "rejected workflows must not emit a runnable lock")
		})
	}
}

func TestConnectorAssemblyExampleWorkQueueEffectsRejected(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.CopyFS(root, os.DirFS(connectorAssemblyFixture)))
	path := filepath.Join(root, ".github/workflows/effects-only.md")
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	content = []byte(strings.Replace(string(content), "connectors:\n", "tools:\n  work-queue:\n    worker: true\nconnectors:\n", 1))
	require.NoError(t, os.WriteFile(path, content, 0600))
	c := NewCompiler()
	c.gitRoot = root
	c.SetActionMode(ActionModeDev)
	c.version = "v0.0.0-1-g683ddf9617"
	require.ErrorContains(t, c.CompileWorkflow(path), "requires a trusted per-Claim delivery adapter")
	_, err = os.Stat(strings.TrimSuffix(path, ".md") + ".lock.yml")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestConnectorAssemblyExampleTLSExecution(t *testing.T) {
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /issues/ISSUE-1":
			assert.Contains(t, []string{"Bearer fixture-agent", "Bearer fixture-context"}, r.Header.Get("Authorization"))
			_, err := io.WriteString(w, `{"id":"ISSUE-1","title":"Fixture issue","private":"excluded"}`)
			assert.NoError(t, err)
		case "POST /issues/ISSUE-1/comments":
			assert.Equal(t, "Bearer fixture-safe-output", r.Header.Get("Authorization"))
			var body map[string]string
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, map[string]string{"body": "Assembly example completed."}, body)
			_, err := io.WriteString(w, `{"id":"COMMENT-1"}`)
			assert.NoError(t, err)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()

	root := t.TempDir()
	require.NoError(t, os.CopyFS(root, os.DirFS(connectorAssemblyFixture)))
	selection := assemblyExampleSelection(t, "combined")
	specPath := filepath.Join(root, selection.Definition)
	spec, err := connectors.LoadSpec(specPath)
	require.NoError(t, err)
	spec.Endpoints["api"] = server.URL
	data, err := json.Marshal(spec)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(specPath, data, 0600))
	plan, err := connectors.Lower(root, assemblyBinding(selection))
	require.NoError(t, err)
	resolutions := 0
	runtime := func(phase string) connectors.Runtime {
		return connectors.Runtime{
			Phase: phase, AllowedOrigins: []string{server.URL}, Client: server.Client(),
			AllowWrites: phase == "safe-output",
			ResolveSecret: func(_ context.Context, requestedPhase, name string) (string, error) {
				expected := map[string]string{
					"agent": "ASSEMBLY_READ_TOKEN", "context": "ASSEMBLY_CONTEXT_TOKEN",
					"safe-output": "ASSEMBLY_WRITE_TOKEN",
				}
				if requestedPhase != phase || name != expected[phase] {
					return "", fmt.Errorf("credential crossed phase boundary")
				}
				resolutions++
				return "fixture-" + phase, nil
			},
		}
	}
	readArgs := map[string]any{"issue": "ISSUE-1"}
	result, err := plan.Execute(t.Context(), runtime("agent"), "mcp", "get-issue", readArgs)
	require.NoError(t, err)
	require.True(t, result.Untrusted)
	require.JSONEq(t, `{"id":"ISSUE-1","title":"Fixture issue"}`, string(result.Data))
	result, err = plan.Execute(t.Context(), runtime("context"), "context", "issue-details", selection.Context["issue-details"])
	require.NoError(t, err)
	require.True(t, result.Untrusted)
	staged, err := plan.PrepareSafeOutput("add-comment", map[string]any{
		"issue": "ISSUE-1", "text": "Assembly example completed.",
	})
	require.NoError(t, err)
	require.Equal(t, 2, requests, "preparing an effect must not execute it")
	_, err = plan.ExecuteSafeOutput(t.Context(), runtime("agent"), *staged)
	require.Error(t, err)
	require.Equal(t, 2, resolutions, "wrong phase must be rejected before credentials")
	_, err = plan.ExecuteSafeOutput(t.Context(), runtime("safe-output"), *staged)
	require.NoError(t, err)
	require.Equal(t, 3, requests)
	require.Equal(t, 3, resolutions)
	_, err = plan.Execute(t.Context(), runtime("agent"), "mcp", "get-issue", map[string]any{"issue": "OTHER"})
	require.Error(t, err)
	require.Equal(t, 3, resolutions, "resource denial must happen before credentials")
	require.ErrorIs(t, plan.ActivateTriggers(), connectors.ErrIngressUnavailable)
}

//go:build !integration

package cli

import (
	"context"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestJSONFieldNames verifies that jsonFieldNames extracts the correct JSON tag
// names from a struct via reflection.
func TestJSONFieldNames(t *testing.T) {
	t.Parallel()
	type sampleArgs struct {
		Alpha   string   `json:"alpha,omitempty"`
		Beta    int      `json:"beta"`
		Gamma   []string `json:"gamma,omitempty"`
		ignored string   //nolint:unused // unexported — must be excluded
		Skip    string   `json:"-"`
		NoTag   string
	}

	got := jsonFieldNames(sampleArgs{})
	// sorted result
	assert.Equal(t, []string{"alpha", "beta", "gamma"}, got, "should include only tagged exported fields, sorted")
}

// TestExtractUnknownParams verifies that the error-message parser correctly
// extracts unknown parameter names from the jsonschema-go validation error.
func TestExtractUnknownParams(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		errMsg   string
		expected []string
	}{
		{
			name:     "single unknown param",
			errMsg:   `validating "arguments": validating root: unexpected additional properties ["workflow-name"]`,
			expected: []string{"workflow-name"},
		},
		{
			name:     "multiple unknown params",
			errMsg:   `validating "arguments": validating root: unexpected additional properties ["workflow-name" "invalid-param"]`,
			expected: []string{"workflow-name", "invalid-param"},
		},
		{
			name:     "underscore param",
			errMsg:   `validating "arguments": validating root: unexpected additional properties ["workflow_name"]`,
			expected: []string{"workflow_name"},
		},
		{
			name:     "no match — different error",
			errMsg:   "some other validation error",
			expected: nil,
		},
		{
			name:     "empty string",
			errMsg:   "",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractUnknownParams(tt.errMsg)
			assert.Equal(t, tt.expected, got, "extracted unknown params should match")
		})
	}
}

func TestExtractUnknownParamsFromSchemaError(t *testing.T) {
	t.Parallel()
	type sampleArgs struct {
		Name string `json:"name" jsonschema:"Name field"`
	}

	schema, err := GenerateSchema[sampleArgs]()
	require.NoError(t, err)

	resolved, err := schema.Resolve(&jsonschema.ResolveOptions{})
	require.NoError(t, err)

	err = resolved.Validate(map[string]any{
		"name":          "octocat",
		"workflow-name": "typo",
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "unexpected additional properties")
	assert.Equal(t, []string{"workflow-name"}, extractUnknownParams(err.Error()))
}

// TestFindSimilarParam verifies the fuzzy matching of parameter names.
func TestFindSimilarParam(t *testing.T) {
	t.Parallel()
	compileParams := []string{"actionlint", "fix", "grype", "max_tokens", "poutine", "runner-guard", "strict", "syft", "workflows", "yamllint", "zizmor"}

	tests := []struct {
		name        string
		unknown     string
		validParams []string
		expected    string
	}{
		{
			name:        "workflow-name → workflows (compile)",
			unknown:     "workflow-name",
			validParams: compileParams,
			expected:    "workflows",
		},
		{
			name:        "workflow_name → workflows (compile)",
			unknown:     "workflow_name",
			validParams: compileParams,
			expected:    "workflows",
		},
		{
			name:        "workflowname → workflows (compile)",
			unknown:     "workflowname",
			validParams: compileParams,
			expected:    "workflows",
		},
		{
			name:        "max-tokens → max_tokens (compile)",
			unknown:     "max-tokens",
			validParams: compileParams,
			expected:    "max_tokens",
		},
		{
			name:        "runnerguard → runner-guard (compile)",
			unknown:     "runnerguard",
			validParams: compileParams,
			expected:    "runner-guard",
		},
		{
			name:        "completely unrelated — no match",
			unknown:     "banana",
			validParams: compileParams,
			expected:    "",
		},
		{
			name:        "empty validParams — no match",
			unknown:     "workflow-name",
			validParams: []string{},
			expected:    "",
		},
		{
			name:        "exact match after normalization",
			unknown:     "strict",
			validParams: compileParams,
			expected:    "strict",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := findSimilarParam(tt.unknown, tt.validParams)
			assert.Equal(t, tt.expected, got, "similar param suggestion should match")
		})
	}
}

// TestBuildHelpfulParamError verifies the structure of the helpful error message.
func TestBuildHelpfulParamError(t *testing.T) {
	t.Parallel()
	t.Run("single unknown param with suggestion", func(t *testing.T) {
		t.Parallel()
		msg := buildHelpfulParamError(
			"compile",
			[]string{"workflow-name"},
			[]string{"workflows", "strict"},
		)
		assert.Contains(t, msg, "Unknown parameter 'workflow-name'", "should mention unknown param")
		assert.Contains(t, msg, "Did you mean 'workflows'?", "should suggest workflows")
		assert.Contains(t, msg, "agenticworkflows compile --help", "should point to help")
	})

	t.Run("single unknown param without suggestion", func(t *testing.T) {
		t.Parallel()
		msg := buildHelpfulParamError(
			"compile",
			[]string{"banana"},
			[]string{"workflows", "strict"},
		)
		assert.Contains(t, msg, "Unknown parameter 'banana'", "should mention unknown param")
		assert.NotContains(t, msg, "Did you mean", "should not suggest for unrelated param")
		assert.Contains(t, msg, "agenticworkflows compile --help", "should point to help")
	})

	t.Run("multiple unknown params", func(t *testing.T) {
		t.Parallel()
		msg := buildHelpfulParamError(
			"compile",
			[]string{"workflow-name", "abc"},
			[]string{"workflows", "strict"},
		)
		assert.Contains(t, msg, "Unknown parameter 'workflow-name'", "should mention first unknown param")
		assert.Contains(t, msg, "Unknown parameter 'abc'", "should mention second unknown param")
		assert.Contains(t, msg, "agenticworkflows compile --help", "should point to help")
	})

	t.Run("empty tool name omits help line", func(t *testing.T) {
		t.Parallel()
		msg := buildHelpfulParamError("", []string{"workflow-name"}, []string{"workflows"})
		assert.NotContains(t, msg, "--help", "no tool name means no help line")
	})

	t.Run("prompt param points to ad hoc evaluation mode instead of a suggestion", func(t *testing.T) {
		t.Parallel()
		msg := buildHelpfulParamError("status", []string{"prompt"}, []string{"pattern"})
		assert.Contains(t, msg, "Unknown parameter 'prompt'", "should mention unknown param")
		assert.Contains(t, msg, "agentic-workflows custom agent", "should point to the custom agent")
		assert.NotContains(t, msg, "Did you mean", "should not offer a fuzzy-match suggestion for a freeform param")
	})

	t.Run("scenario param points to ad hoc evaluation mode", func(t *testing.T) {
		t.Parallel()
		msg := buildHelpfulParamError("compile", []string{"scenario"}, []string{"workflows", "strict"})
		assert.Contains(t, msg, "Unknown parameter 'scenario'", "should mention unknown param")
		assert.Contains(t, msg, "ad hoc scenario evaluation", "should mention ad hoc scenario evaluation")
	})
}

// TestArgumentValidationMiddleware_TransformsAdditionalPropertiesError verifies
// that the middleware replaces raw schema validation errors with helpful messages.
func TestArgumentValidationMiddleware_TransformsAdditionalPropertiesError(t *testing.T) {
	t.Parallel()
	toolParams := map[string]toolParamEntry{
		"compile": {"actionlint", "fix", "grype", "max_tokens", "poutine", "runner-guard", "strict", "syft", "workflows", "yamllint", "zizmor"},
	}

	middleware := argumentValidationMiddleware(toolParams)

	// Build a fake "additional properties" tool error result.
	rawErrMsg := `validating "arguments": validating root: unexpected additional properties ["workflow-name"]`
	fakeResult := &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{
			&mcp.TextContent{Text: rawErrMsg},
		},
	}

	// Wrap a handler that returns the fake error result.
	handler := middleware(func(_ context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		return fakeResult, nil
	})

	// Call the middleware with a request carrying tool name "compile" so
	// extractMCPToolName resolves it and the help line is included in the output.
	result, err := handler(context.Background(), "tools/call", fakeToolCallRequest("compile"))
	require.NoError(t, err, "middleware should not return an error")

	toolResult, ok := result.(*mcp.CallToolResult)
	require.True(t, ok, "result should be *mcp.CallToolResult")
	assert.True(t, toolResult.IsError, "IsError should remain true")

	require.Len(t, toolResult.Content, 1, "should have one content element")
	text, ok := toolResult.Content[0].(*mcp.TextContent)
	require.True(t, ok, "content should be *mcp.TextContent")

	assert.NotContains(t, text.Text, "validating root", "raw schema error should be gone")
	assert.Contains(t, text.Text, "Unknown parameter 'workflow-name'", "should name the bad param")
	assert.Contains(t, text.Text, "Did you mean 'workflows'?", "should suggest workflows")
}

// TestArgumentValidationMiddleware_PassesThroughNonValidationErrors verifies
// that the middleware does not modify tool results that are unrelated to schema
// validation.
func TestArgumentValidationMiddleware_PassesThroughNonValidationErrors(t *testing.T) {
	t.Parallel()
	toolParams := mcpToolParams()
	middleware := argumentValidationMiddleware(toolParams)

	// A regular tool error (not a schema validation error).
	regularErr := &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{
			&mcp.TextContent{Text: "workflow 'nonexistent' not found"},
		},
	}

	handler := middleware(func(_ context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		return regularErr, nil
	})

	result, err := handler(context.Background(), "tools/call", fakeToolCallRequest("compile"))
	require.NoError(t, err)

	toolResult, ok := result.(*mcp.CallToolResult)
	require.True(t, ok)
	text := toolResult.Content[0].(*mcp.TextContent)
	assert.Equal(t, "workflow 'nonexistent' not found", text.Text, "non-validation errors should be unchanged")
}

// TestArgumentValidationMiddleware_PassesThroughSuccessResults verifies that
// successful tool results are not modified.
func TestArgumentValidationMiddleware_PassesThroughSuccessResults(t *testing.T) {
	t.Parallel()
	toolParams := mcpToolParams()
	middleware := argumentValidationMiddleware(toolParams)

	successResult := &mcp.CallToolResult{
		IsError: false,
		Content: []mcp.Content{
			&mcp.TextContent{Text: `[{"workflow":"test.md","valid":true}]`},
		},
	}

	handler := middleware(func(_ context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		return successResult, nil
	})

	result, err := handler(context.Background(), "tools/call", fakeToolCallRequest("compile"))
	require.NoError(t, err)

	toolResult, ok := result.(*mcp.CallToolResult)
	require.True(t, ok)
	assert.False(t, toolResult.IsError)
}

// TestArgumentValidationMiddleware_UnknownToolReturnsInternalError verifies
// that an unregistered tool name is reported as an internal server error.
func TestArgumentValidationMiddleware_UnknownToolReturnsInternalError(t *testing.T) {
	t.Parallel()
	toolParams := map[string]toolParamEntry{
		"compile": {"workflows"},
	}
	middleware := argumentValidationMiddleware(toolParams)

	rawErr := `validating "arguments": validating root: unexpected additional properties ["workflow-name"]`
	handler := middleware(func(_ context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: rawErr}},
		}, nil
	})

	result, err := handler(context.Background(), "tools/call", fakeToolCallRequest("not-a-real-tool"))
	assert.Nil(t, result, "unknown tool should not return a result payload")
	require.Error(t, err, "unknown tool should return a JSON-RPC error")

	var rpcErr *jsonrpc.Error
	require.ErrorAs(t, err, &rpcErr, "error should be a JSON-RPC error")
	assert.Equal(t, int64(jsonrpc.CodeInternalError), rpcErr.Code, "unknown tool should use internal-error code")
}

// TestArgumentValidationMiddleware_PassesThroughNonToolCallMethods verifies
// that the middleware ignores methods other than "tools/call".
func TestArgumentValidationMiddleware_PassesThroughNonToolCallMethods(t *testing.T) {
	t.Parallel()
	toolParams := mcpToolParams()
	middleware := argumentValidationMiddleware(toolParams)

	// A tools/list response would never contain IsError, but even if it did
	// the middleware should leave it alone.
	called := false
	handler := middleware(func(_ context.Context, method string, _ mcp.Request) (mcp.Result, error) {
		called = true
		assert.Equal(t, "tools/list", method)
		return nil, nil
	})

	_, err := handler(context.Background(), "tools/list", fakeToolCallRequest(""))
	require.NoError(t, err)
	assert.True(t, called)
}

// TestMCPToolParams verifies that the tool parameter registry is populated and
// consistent with the known tools.  Parameter names are derived automatically
// from the *Args struct json tags via reflection, so this test validates that
// every tool is present, has non-empty params, and that no unexpected tools
// have been added to mcpToolParams() without also being listed here.
func TestMCPToolParams(t *testing.T) {
	t.Parallel()
	params := mcpToolParams()

	// This list must mirror createMCPServer in mcp_server.go.  If a new tool is
	// registered there, add it here as well — the len check below will fail and
	// catch any discrepancy at test time.
	expectedTools := []string{"status", "compile", "logs", "audit", "audit-diff", "checks", "mcp-inspect", "work-queue", "add", "update", "fix"}

	// Verify the registry has exactly the expected number of entries so that a
	// new tool added to mcpToolParams() without also adding it to expectedTools
	// (or vice-versa) is caught immediately.
	assert.Len(t, params, len(expectedTools), "mcpToolParams() must contain exactly the expected tools; update expectedTools if a new tool was added")

	for _, tool := range expectedTools {
		t.Run(tool, func(t *testing.T) {
			t.Parallel()
			toolParams, ok := params[tool]
			require.True(t, ok, "tool '%s' should be in the parameter registry", tool)
			assert.NotEmpty(t, toolParams, "tool '%s' should have at least one parameter", tool)

			// Spot-check a known parameter for each tool to confirm reflection is working.
			switch tool {
			case "compile":
				assert.Contains(t, toolParams, "workflows", "compile tool must include 'workflows' param")
			case "audit":
				// experiment and variant were previously missing from the hardcoded map;
				// reflection must now include them automatically.
				assert.Contains(t, toolParams, "run_id", "audit tool must include 'run_id' alias param")
				assert.Contains(t, toolParams, "experiment", "audit tool must include 'experiment' param")
				assert.Contains(t, toolParams, "variant", "audit tool must include 'variant' param")
			}
		})
	}
}

// fakeToolCallRequest returns a minimal mcp.Request whose GetParams() type-assertion
// to *mcp.CallToolParamsRaw will succeed and return the given tool name.
func fakeToolCallRequest(toolName string) mcp.Request {
	return &mcp.ServerRequest[*mcp.CallToolParamsRaw]{
		Params: &mcp.CallToolParamsRaw{Name: toolName},
	}
}

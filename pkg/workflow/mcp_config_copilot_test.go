//go:build !integration

package workflow

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

func TestRenderSharedMCPConfig_CopilotFields(t *testing.T) {
	tests := []struct {
		name              string
		toolConfig        map[string]any
		renderer          MCPConfigRenderer
		expectedContent   []string
		unexpectedContent []string
	}{
		{
			name: "Copilot engine with stdio MCP server",
			toolConfig: map[string]any{
				"type":    "stdio",
				"command": "docker",
				"args":    []string{"run", "--rm", "-i", "mcp/time"},
				"env":     map[string]any{"TZ": "UTC"},
				"allowed": []string{"get_current_time"},
			},
			renderer: MCPConfigRenderer{
				IndentLevel:           "  ",
				Format:                "json",
				RequiresCopilotFields: true,
			},
			expectedContent: []string{
				`"type": "stdio"`, // stdio type for copilot
				`"tools": [`,
				`"get_current_time"`,
				`"command": "docker"`,
				`"args": [`,
				`"env": {`,
			},
			unexpectedContent: []string{},
		},
		{
			name: "Copilot engine with HTTP MCP server",
			toolConfig: map[string]any{
				"type": "http",
				"url":  "https://api.example.com/mcp",
				"headers": map[string]any{
					"Authorization": "Bearer token",
				},
			},
			renderer: MCPConfigRenderer{
				IndentLevel:           "  ",
				Format:                "json",
				RequiresCopilotFields: true,
			},
			expectedContent: []string{
				`"type": "http"`, // http stays http for copilot
				`"tools": [`,
				`"*"`, // default to all tools when no allowed specified
				`"url": "https://api.example.com/mcp"`,
				`"headers": {`,
			},
			unexpectedContent: []string{},
		},
		{
			name: "Claude engine with stdio MCP server (no copilot fields)",
			toolConfig: map[string]any{
				"type":    "stdio",
				"command": "npx",
				"args":    []string{"-y", "my-server"},
				"env":     map[string]any{"NODE_ENV": "production"},
			},
			renderer: MCPConfigRenderer{
				IndentLevel:           "  ",
				Format:                "json",
				RequiresCopilotFields: false,
			},
			expectedContent: []string{
				// After auto-containerization, npx becomes container with entrypoint
				`"type": "stdio"`,
				`"container": "node:lts-alpine"`, // Auto-assigned container for npx
				`"entrypoint": "npx"`,
				`"entrypointArgs": [`,
				`"env": {`,
			},
			unexpectedContent: []string{
				`"tools":`, // should NOT include tools field
			},
		},
		{
			name: "Claude engine with HTTP MCP server (no copilot fields)",
			toolConfig: map[string]any{
				"type": "http",
				"url":  "https://api.example.com/mcp",
				"headers": map[string]any{
					"Authorization": "Bearer token",
				},
			},
			renderer: MCPConfigRenderer{
				IndentLevel:           "  ",
				Format:                "json",
				RequiresCopilotFields: false,
			},
			expectedContent: []string{
				`"type": "http"`,
				`"url": "https://api.example.com/mcp"`,
				`"headers": {`,
			},
			unexpectedContent: []string{
				`"tools":`, // should NOT include tools field
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output strings.Builder

			err := renderSharedMCPConfig(&output, "test-tool", tt.toolConfig, tt.renderer)
			if err != nil {
				t.Fatalf("renderSharedMCPConfig failed: %v", err)
			}

			result := output.String()

			// Check expected content
			for _, expected := range tt.expectedContent {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected content not found: %q\nActual output:\n%s", expected, result)
				}
			}

			// Check unexpected content
			for _, unexpected := range tt.unexpectedContent {
				if strings.Contains(result, unexpected) {
					t.Errorf("Unexpected content found: %q\nActual output:\n%s", unexpected, result)
				}
			}
		})
	}
}

func TestRenderSharedMCPConfig_ToolsFieldGeneration(t *testing.T) {
	tests := []struct {
		name          string
		toolConfig    map[string]any
		expectedTools string
	}{
		{
			name: "Specific allowed tools",
			toolConfig: map[string]any{
				"type":    "stdio",
				"command": "docker",
				"allowed": []string{"get_time", "set_timezone"},
			},
			expectedTools: `"tools": [
    "get_time",
    "set_timezone"
  ]`,
		},
		{
			name: "No allowed tools - defaults to all",
			toolConfig: map[string]any{
				"type":    "stdio",
				"command": "docker",
			},
			expectedTools: `"tools": [
    "*"
  ]`,
		},
		{
			name: "Empty allowed array - defaults to all",
			toolConfig: map[string]any{
				"type":    "stdio",
				"command": "docker",
				"allowed": []string{},
			},
			expectedTools: `"tools": [
    "*"
  ]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			renderer := MCPConfigRenderer{
				IndentLevel:           "  ",
				Format:                "json",
				RequiresCopilotFields: true,
			}

			var output strings.Builder
			err := renderSharedMCPConfig(&output, "test-tool", tt.toolConfig, renderer)
			if err != nil {
				t.Fatalf("renderSharedMCPConfig failed: %v", err)
			}

			result := output.String()
			if !strings.Contains(result, tt.expectedTools) {
				t.Errorf("Expected tools format not found:\n%q\nActual output:\n%s", tt.expectedTools, result)
			}
		})
	}
}

// TestRenderCustomMCPEnvVars_NonCopilotSecretsEscaped verifies that for non-Copilot
// JSON engines, secrets in custom MCP server env blocks are rendered as \${VAR}
// (backslash-escaped) rather than ${VAR} (unescaped). Unescaped references would
// be expanded by bash inside the unquoted heredoc that carries the MCP gateway
// JSON config -- a secret containing '"' or '\' would corrupt the JSON and cause
// the gateway to fail before the agent runs. Backslash-escaping keeps the JSON
// valid regardless of the secret's runtime value.
func TestRenderCustomMCPEnvVars_NonCopilotSecretsEscaped(t *testing.T) {
	tests := []struct {
		name              string
		toolConfig        map[string]any
		renderer          MCPConfigRenderer
		expectedContent   []string
		unexpectedContent []string
		// validateJSON, when true, replaces \${VAR} placeholders with a benign
		// string (simulating bash heredoc stripping the leading backslash) and
		// then verifies the result is parseable as a JSON object. This catches
		// regressions where the rendered fragment would produce invalid JSON once
		// the gateway resolves environment variables at runtime.
		validateJSON bool
	}{
		{
			name: "Non-Copilot stdio container - secret in env uses backslash-escaped var",
			toolConfig: map[string]any{
				"type":      "stdio",
				"container": "some/image:latest",
				"env": map[string]any{
					"MY_TOKEN": "${{ secrets.MY_TOKEN }}",
				},
			},
			renderer: MCPConfigRenderer{
				IndentLevel:           "  ",
				Format:                "json",
				RequiresCopilotFields: false,
			},
			// Secret must be rendered as \${MY_TOKEN} so the unquoted heredoc
			// leaves a literal ${MY_TOKEN} string in the JSON (valid JSON).
			expectedContent: []string{
				`"MY_TOKEN": "\${MY_TOKEN}"`,
			},
			// Must NOT appear as an unescaped bash variable reference -- that
			// would let bash splice the raw secret value into the JSON.
			unexpectedContent: []string{
				`"MY_TOKEN": "${MY_TOKEN}"`,
			},
		},
		{
			name: "Copilot stdio - secret in env also uses backslash-escaped var",
			toolConfig: map[string]any{
				"type":      "stdio",
				"container": "some/image:latest",
				"env": map[string]any{
					"MY_TOKEN": "${{ secrets.MY_TOKEN }}",
				},
				"allowed": []string{"*"},
			},
			renderer: MCPConfigRenderer{
				IndentLevel:           "  ",
				Format:                "json",
				RequiresCopilotFields: true,
			},
			expectedContent: []string{
				`"MY_TOKEN": "\${MY_TOKEN}"`,
			},
			unexpectedContent: []string{
				`"MY_TOKEN": "${MY_TOKEN}"`,
			},
		},
		{
			name: "Non-Copilot stdio container - secret with fallback in env uses backslash-escaped var",
			toolConfig: map[string]any{
				"type":      "stdio",
				"container": "some/image:latest",
				"env": map[string]any{
					"DD_SITE": "${{ secrets.DD_SITE || 'datadoghq.com' }}",
				},
			},
			renderer: MCPConfigRenderer{
				IndentLevel:           "  ",
				Format:                "json",
				RequiresCopilotFields: false,
			},
			expectedContent: []string{
				`"DD_SITE": "\${DD_SITE}"`,
			},
			unexpectedContent: []string{
				`"DD_SITE": "${DD_SITE}"`,
			},
		},
		{
			// The var *name* is always a safe identifier, but this test verifies
			// that the \${VAR} placeholder produces structurally valid JSON once
			// the gateway resolves the env var at runtime. Before the fix, a bare
			// ${VAR} would have been spliced directly by bash into the heredoc;
			// any secret containing '"' or '\' would have corrupted the JSON.
			name: "Non-Copilot stdio - env placeholder produces valid JSON after gateway resolution",
			toolConfig: map[string]any{
				"type":      "stdio",
				"container": "some/image:latest",
				"env": map[string]any{
					"SPECIAL_KEY": `${{ secrets.SPECIAL_KEY }}`,
				},
			},
			renderer: MCPConfigRenderer{
				IndentLevel:           "  ",
				Format:                "json",
				RequiresCopilotFields: false,
			},
			expectedContent: []string{
				`"SPECIAL_KEY": "\${SPECIAL_KEY}"`,
			},
			unexpectedContent: []string{
				`"SPECIAL_KEY": "${SPECIAL_KEY}"`,
			},
			validateJSON: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output strings.Builder

			err := renderSharedMCPConfig(&output, "test-tool", tt.toolConfig, tt.renderer)
			if err != nil {
				t.Fatalf("renderSharedMCPConfig failed: %v", err)
			}

			result := output.String()

			for _, expected := range tt.expectedContent {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected content not found: %q\nActual output:\n%s", expected, result)
				}
			}

			for _, unexpected := range tt.unexpectedContent {
				if strings.Contains(result, unexpected) {
					t.Errorf("Unexpected content found: %q\nActual output:\n%s", unexpected, result)
				}
			}

			if tt.validateJSON {
				// Simulate bash heredoc processing (\${VAR} → ${VAR}) and then
				// gateway env-var substitution (${VAR} → a benign placeholder).
				// The resulting fragment must be parseable as a JSON object,
				// proving that no secret value -- regardless of its content --
				// can corrupt the JSON structure.
				escapedVarRe := regexp.MustCompile(`\\\$\{[A-Z0-9_]+\}`)
				resolved := escapedVarRe.ReplaceAllString(result, "placeholder-value")
				var obj map[string]any
				if err := json.Unmarshal([]byte("{"+resolved+"}"), &obj); err != nil {
					t.Errorf("Rendered output is not valid JSON after placeholder substitution: %v\nResolved fragment:\n%s", err, resolved)
				}
			}
		})
	}
}

func TestRenderCustomMCPEnvVars_TOMLEnvFallbackUsesShellVariable(t *testing.T) {
	result := renderCustomMCPEnvVars(map[string]string{
		"SENTRY_HOST": "${{ env.SENTRY_HOST || 'sentry.io' }}",
	}, true)

	if got := result["SENTRY_HOST"]; got != "${SENTRY_HOST}" {
		t.Errorf("expected SENTRY_HOST to be rendered as ${SENTRY_HOST}, got %q", got)
	}
}

func TestRenderCustomMCPEnvVars_TOMLEnvNoFallbackUsesShellVariable(t *testing.T) {
	result := renderCustomMCPEnvVars(map[string]string{
		"DD_SITE": "${{ env.DD_SITE }}",
	}, true)

	if got := result["DD_SITE"]; got != "${DD_SITE}" {
		t.Errorf("expected DD_SITE to be rendered as ${DD_SITE}, got %q", got)
	}
}

func TestRenderCustomMCPEnvVars_TOMLRepeatedEnvVariableUsesShellVariable(t *testing.T) {
	result := renderCustomMCPEnvVars(map[string]string{
		"HOST_URL": "${{ env.HOST || 'localhost' }}/${{ env.HOST }}",
	}, true)

	if got := result["HOST_URL"]; got != "${HOST}/${HOST}" {
		t.Errorf("expected HOST_URL to be rendered as ${HOST}/${HOST}, got %q", got)
	}
}

func TestRenderSharedMCPConfig_TypeConversion(t *testing.T) {
	tests := []struct {
		name           string
		inputType      string
		copilotFields  bool
		expectedType   string
		shouldHaveType bool
	}{
		{
			name:           "stdio to local conversion for copilot",
			inputType:      "stdio",
			copilotFields:  true,
			expectedType:   `"type": "stdio"`,
			shouldHaveType: true,
		},
		{
			name:           "http stays http for copilot",
			inputType:      "http",
			copilotFields:  true,
			expectedType:   `"type": "http"`,
			shouldHaveType: true,
		},
		{
			name:           "stdio included for claude",
			inputType:      "stdio",
			copilotFields:  false,
			expectedType:   `"type":`,
			shouldHaveType: true,
		},
		{
			name:           "http included for claude",
			inputType:      "http",
			copilotFields:  false,
			expectedType:   `"type":`,
			shouldHaveType: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toolConfig := map[string]any{
				"type": tt.inputType,
			}

			if tt.inputType == "http" {
				toolConfig["url"] = "https://api.example.com/mcp"
			} else {
				// Use container-based config: command-based stdio is rejected by MCP Gateway v0.2.30+
				toolConfig["container"] = "test-image:latest"
			}

			renderer := MCPConfigRenderer{
				IndentLevel:           "  ",
				Format:                "json",
				RequiresCopilotFields: tt.copilotFields,
			}

			var output strings.Builder
			err := renderSharedMCPConfig(&output, "test-tool", toolConfig, renderer)
			if err != nil {
				t.Fatalf("renderSharedMCPConfig failed: %v", err)
			}

			result := output.String()

			if tt.shouldHaveType {
				if !strings.Contains(result, tt.expectedType) {
					t.Errorf("Expected type field not found: %q\nActual output:\n%s", tt.expectedType, result)
				}
			} else {
				if strings.Contains(result, tt.expectedType) {
					t.Errorf("Type field should not be present, but found: %q\nActual output:\n%s", tt.expectedType, result)
				}
			}
		})
	}
}

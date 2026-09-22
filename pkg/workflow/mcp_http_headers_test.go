//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestExtractSecretsFromValue(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected map[string]string
	}{
		{
			name:  "Single secret",
			value: "${{ secrets.DD_API_KEY }}",
			expected: map[string]string{
				"DD_API_KEY": "${{ secrets.DD_API_KEY }}",
			},
		},
		{
			name:  "Secret with default value",
			value: "${{ secrets.DD_SITE || 'datadoghq.com' }}",
			expected: map[string]string{
				"DD_SITE": "${{ secrets.DD_SITE || 'datadoghq.com' }}",
			},
		},
		{
			name:  "Multiple secrets in one value",
			value: "Bearer ${{ secrets.TOKEN1 }} and ${{ secrets.TOKEN2 }}",
			expected: map[string]string{
				"TOKEN1": "${{ secrets.TOKEN1 }}",
				"TOKEN2": "${{ secrets.TOKEN2 }}",
			},
		},
		{
			name:     "No secrets",
			value:    "Just a plain string",
			expected: map[string]string{},
		},
		{
			name:     "Empty string",
			value:    "",
			expected: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractSecretsFromValue(tt.value)

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d secrets, got %d", len(tt.expected), len(result))
			}

			for key, expectedValue := range tt.expected {
				if actualValue, exists := result[key]; !exists {
					t.Errorf("Expected secret %s not found", key)
				} else if actualValue != expectedValue {
					t.Errorf("For key %s, expected %q, got %q", key, expectedValue, actualValue)
				}
			}
		})
	}
}

func TestExtractSecretsFromHeaders(t *testing.T) {
	headers := map[string]string{
		"DD_API_KEY":         "${{ secrets.DD_API_KEY }}",
		"DD_APPLICATION_KEY": "${{ secrets.DD_APPLICATION_KEY }}",
		"DD_SITE":            "${{ secrets.DD_SITE || 'datadoghq.com' }}",
		"Static":             "no-secrets-here",
	}

	result := ExtractSecretsFromMap(headers)

	expected := map[string]string{
		"DD_API_KEY":         "${{ secrets.DD_API_KEY }}",
		"DD_APPLICATION_KEY": "${{ secrets.DD_APPLICATION_KEY }}",
		"DD_SITE":            "${{ secrets.DD_SITE || 'datadoghq.com' }}",
	}

	if len(result) != len(expected) {
		t.Errorf("Expected %d secrets, got %d", len(expected), len(result))
	}

	for key, expectedValue := range expected {
		if actualValue, exists := result[key]; !exists {
			t.Errorf("Expected secret %s not found", key)
		} else if actualValue != expectedValue {
			t.Errorf("For key %s, expected %q, got %q", key, expectedValue, actualValue)
		}
	}
}

func TestRenderSharedMCPConfig_HTTPWithHeaderSecrets(t *testing.T) {
	toolConfig := map[string]any{
		"type": "http",
		"url":  "https://mcp.datadoghq.com/api/unstable/mcp-server/mcp",
		"headers": map[string]any{
			"DD_API_KEY":         "${{ secrets.DD_API_KEY }}",
			"DD_APPLICATION_KEY": "${{ secrets.DD_APPLICATION_KEY }}",
			"DD_SITE":            "${{ secrets.DD_SITE || 'datadoghq.com' }}",
		},
		"allowed": []string{"search_datadog_dashboards", "search_datadog_slos"},
	}

	renderer := MCPConfigRenderer{
		IndentLevel:           "  ",
		Format:                "json",
		RequiresCopilotFields: true,
	}

	var output strings.Builder
	err := renderSharedMCPConfig(&output, "datadog", toolConfig, renderer)
	if err != nil {
		t.Fatalf("renderSharedMCPConfig failed: %v", err)
	}

	result := output.String()

	// Check that headers use env var references instead of secret expressions
	expectedHeaderChecks := []string{
		`"headers": {`,
		`"DD_API_KEY": "\${DD_API_KEY}"`,
		`"DD_APPLICATION_KEY": "\${DD_APPLICATION_KEY}"`,
		`"DD_SITE": "\${DD_SITE}"`,
	}

	for _, expected := range expectedHeaderChecks {
		if !strings.Contains(result, expected) {
			t.Errorf("Expected header content not found: %q\nActual output:\n%s", expected, result)
		}
	}

	// Check that env passthrough section is present
	expectedEnvChecks := []string{
		`"env": {`,
		`"DD_API_KEY": "\${DD_API_KEY}"`,
		`"DD_APPLICATION_KEY": "\${DD_APPLICATION_KEY}"`,
		`"DD_SITE": "\${DD_SITE}"`,
	}

	for _, expected := range expectedEnvChecks {
		if !strings.Contains(result, expected) {
			t.Errorf("Expected env passthrough not found: %q\nActual output:\n%s", expected, result)
		}
	}

	// Check that tools field is present
	if !strings.Contains(result, `"tools": [`) {
		t.Errorf("Expected tools field not found in output:\n%s", result)
	}

	// Verify original secret expressions are NOT in the output (they should be replaced)
	unexpectedChecks := []string{
		`${{ secrets.DD_API_KEY }}`,
		`${{ secrets.DD_APPLICATION_KEY }}`,
		`${{ secrets.DD_SITE || 'datadoghq.com' }}`,
	}

	for _, unexpected := range unexpectedChecks {
		if strings.Contains(result, unexpected) {
			t.Errorf("Unexpected secret expression found (should be replaced): %q\nActual output:\n%s", unexpected, result)
		}
	}
}

func TestRenderSharedMCPConfig_HTTPWithoutSecrets(t *testing.T) {
	toolConfig := map[string]any{
		"type": "http",
		"url":  "https://api.example.com/mcp",
		"headers": map[string]any{
			"X-Custom-Header": "static-value",
		},
	}

	renderer := MCPConfigRenderer{
		IndentLevel:           "  ",
		Format:                "json",
		RequiresCopilotFields: true,
	}

	var output strings.Builder
	err := renderSharedMCPConfig(&output, "example", toolConfig, renderer)
	if err != nil {
		t.Fatalf("renderSharedMCPConfig failed: %v", err)
	}

	result := output.String()

	// Check that headers are rendered normally
	if !strings.Contains(result, `"X-Custom-Header": "static-value"`) {
		t.Errorf("Expected static header not found in output:\n%s", result)
	}

	// Check that env section is NOT present when there are no secrets
	if strings.Contains(result, `"env": {`) {
		t.Errorf("Unexpected env section found (no secrets to passthrough):\n%s", result)
	}
}

func TestCollectHTTPMCPHeaderSecrets(t *testing.T) {
	tools := map[string]any{
		"datadog": map[string]any{
			"type": "http",
			"url":  "https://mcp.datadoghq.com/api/unstable/mcp-server/mcp",
			"headers": map[string]any{
				"DD_API_KEY":         "${{ secrets.DD_API_KEY }}",
				"DD_APPLICATION_KEY": "${{ secrets.DD_APPLICATION_KEY }}",
			},
		},
		"github": map[string]any{
			// Built-in tool, not HTTP MCP
			"allowed": []string{"get_repository"},
		},
		"custom": map[string]any{
			"type": "http",
			"url":  "https://api.custom.com/mcp",
			"headers": map[string]any{
				"API_TOKEN": "${{ secrets.CUSTOM_API_TOKEN }}",
			},
		},
		"stdio-tool": map[string]any{
			"type":    "stdio",
			"command": "docker",
		},
	}

	result := collectHTTPMCPHeaderSecrets(tools)

	expected := map[string]string{
		"DD_API_KEY":         "${{ secrets.DD_API_KEY }}",
		"DD_APPLICATION_KEY": "${{ secrets.DD_APPLICATION_KEY }}",
		"CUSTOM_API_TOKEN":   "${{ secrets.CUSTOM_API_TOKEN }}",
	}

	if len(result) != len(expected) {
		t.Errorf("Expected %d secrets, got %d", len(expected), len(result))
	}

	for key, expectedValue := range expected {
		if actualValue, exists := result[key]; !exists {
			t.Errorf("Expected secret %s not found", key)
		} else if actualValue != expectedValue {
			t.Errorf("For key %s, expected %q, got %q", key, expectedValue, actualValue)
		}
	}
}

func TestRenderSharedMCPConfig_PropertyOrder(t *testing.T) {
	toolConfig := map[string]any{
		"type": "http",
		"url":  "https://api.example.com/mcp",
		"headers": map[string]any{
			"Authorization": "${{ secrets.API_KEY }}",
		},
		"allowed": []string{"tool1"},
	}

	renderer := MCPConfigRenderer{
		IndentLevel:           "  ",
		Format:                "json",
		RequiresCopilotFields: true,
	}

	var output strings.Builder
	err := renderSharedMCPConfig(&output, "example", toolConfig, renderer)
	if err != nil {
		t.Fatalf("renderSharedMCPConfig failed: %v", err)
	}

	result := output.String()

	// Verify property order: type, url, headers, tools, env
	typeIdx := strings.Index(result, `"type":`)
	urlIdx := strings.Index(result, `"url":`)
	headersIdx := strings.Index(result, `"headers":`)
	toolsIdx := strings.Index(result, `"tools":`)
	envIdx := strings.Index(result, `"env":`)

	if typeIdx == -1 || urlIdx == -1 || headersIdx == -1 || toolsIdx == -1 || envIdx == -1 {
		t.Fatalf("Missing required properties in output:\n%s", result)
	}

	// Check order: type < url < headers < tools < env
	if typeIdx >= urlIdx || urlIdx >= headersIdx || headersIdx >= toolsIdx || toolsIdx >= envIdx {
		t.Errorf("Properties are not in expected order (type, url, headers, tools, env):\n%s", result)
	}
}

func TestRenderSharedMCPConfig_WithAuth(t *testing.T) {
	t.Run("renders auth after headers with type and audience", func(t *testing.T) {
		// Set sentinel values so we can assert neither the var names nor the values
		// appear in the rendered config (guards against value-embedding regressions).
		t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", "https://sentinel-oidc-url.example.com/token")
		t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "sentinel-oidc-token-value")

		toolConfig := map[string]any{
			"type": "http",
			"url":  "https://my-server.example.com/mcp",
			"headers": map[string]any{
				"X-Custom-Header": "custom-value",
			},
			"auth": map[string]any{
				"type":     "github-oidc",
				"audience": "https://my-server.example.com",
			},
		}

		renderer := MCPConfigRenderer{
			IndentLevel:           "  ",
			Format:                "json",
			RequiresCopilotFields: true,
		}

		var output strings.Builder
		err := renderSharedMCPConfig(&output, "my-server", toolConfig, renderer)
		if err != nil {
			t.Fatalf("renderSharedMCPConfig failed: %v", err)
		}

		result := output.String()

		// Auth block is rendered
		if !strings.Contains(result, `"auth": {`) {
			t.Errorf("Expected auth block not found in output:\n%s", result)
		}
		if !strings.Contains(result, `"type": "github-oidc"`) {
			t.Errorf("Expected auth.type not found in output:\n%s", result)
		}
		if !strings.Contains(result, `"audience": "https://my-server.example.com"`) {
			t.Errorf("Expected auth.audience not found in output:\n%s", result)
		}
		// Assert neither the env var names nor the sentinel values are embedded.
		if strings.Contains(result, "ACTIONS_ID_TOKEN_REQUEST_URL") || strings.Contains(result, "ACTIONS_ID_TOKEN_REQUEST_TOKEN") {
			t.Errorf("Auth config must not embed Actions OIDC runtime var names:\n%s", result)
		}
		if strings.Contains(result, "https://sentinel-oidc-url.example.com/token") || strings.Contains(result, "sentinel-oidc-token-value") {
			t.Errorf("Auth config must not embed Actions OIDC runtime var values:\n%s", result)
		}

		// Property order: type < url < headers < auth < tools
		typeIdx := strings.Index(result, `"type": "http"`)
		urlIdx := strings.Index(result, `"url":`)
		headersIdx := strings.Index(result, `"headers":`)
		authIdx := strings.Index(result, `"auth":`)
		toolsIdx := strings.Index(result, `"tools":`)

		if typeIdx == -1 || urlIdx == -1 || headersIdx == -1 || authIdx == -1 || toolsIdx == -1 {
			t.Fatalf("Missing required properties in output:\n%s", result)
		}

		if typeIdx >= urlIdx || urlIdx >= headersIdx || headersIdx >= authIdx || authIdx >= toolsIdx {
			t.Errorf("Properties not in expected order (type, url, headers, auth, tools):\n%s", result)
		}
	})

	t.Run("renders auth without audience (type only)", func(t *testing.T) {
		// Set sentinel values so we can assert neither the var names nor the values
		// appear in the rendered config (guards against value-embedding regressions).
		t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", "https://sentinel-oidc-url.example.com/token")
		t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "sentinel-oidc-token-value")

		toolConfig := map[string]any{
			"type": "http",
			"url":  "https://my-server.example.com/mcp",
			"auth": map[string]any{
				"type": "github-oidc",
			},
		}

		renderer := MCPConfigRenderer{
			IndentLevel:           "  ",
			Format:                "json",
			RequiresCopilotFields: true,
		}

		var output strings.Builder
		err := renderSharedMCPConfig(&output, "my-server", toolConfig, renderer)
		if err != nil {
			t.Fatalf("renderSharedMCPConfig failed: %v", err)
		}

		result := output.String()

		if !strings.Contains(result, `"auth": {`) {
			t.Errorf("Expected auth block not found:\n%s", result)
		}
		if !strings.Contains(result, `"type": "github-oidc"`) {
			t.Errorf("Expected auth.type not found:\n%s", result)
		}
		if strings.Contains(result, `"audience"`) {
			t.Errorf("Unexpected audience field in output:\n%s", result)
		}
		// Assert neither the env var names nor the sentinel values are embedded.
		if strings.Contains(result, "ACTIONS_ID_TOKEN_REQUEST_URL") || strings.Contains(result, "ACTIONS_ID_TOKEN_REQUEST_TOKEN") {
			t.Errorf("Auth config must not embed Actions OIDC runtime var names:\n%s", result)
		}
		if strings.Contains(result, "https://sentinel-oidc-url.example.com/token") || strings.Contains(result, "sentinel-oidc-token-value") {
			t.Errorf("Auth config must not embed Actions OIDC runtime var values:\n%s", result)
		}
	})

	t.Run("no auth block when auth is absent", func(t *testing.T) {
		toolConfig := map[string]any{
			"type": "http",
			"url":  "https://my-server.example.com/mcp",
		}

		renderer := MCPConfigRenderer{
			IndentLevel:           "  ",
			Format:                "json",
			RequiresCopilotFields: true,
		}

		var output strings.Builder
		err := renderSharedMCPConfig(&output, "my-server", toolConfig, renderer)
		if err != nil {
			t.Fatalf("renderSharedMCPConfig failed: %v", err)
		}

		result := output.String()

		if strings.Contains(result, `"auth"`) {
			t.Errorf("Unexpected auth block in output:\n%s", result)
		}
	})
}

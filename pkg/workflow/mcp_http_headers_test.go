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

func TestReplaceSecretsWithEnvVars(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		secrets  map[string]string
		expected string
	}{
		{
			name:  "Replace single secret",
			value: "${{ secrets.DD_API_KEY }}",
			secrets: map[string]string{
				"DD_API_KEY": "${{ secrets.DD_API_KEY }}",
			},
			expected: "\\${DD_API_KEY}",
		},
		{
			name:  "Replace secret with default",
			value: "${{ secrets.DD_SITE || 'datadoghq.com' }}",
			secrets: map[string]string{
				"DD_SITE": "${{ secrets.DD_SITE || 'datadoghq.com' }}",
			},
			expected: "\\${DD_SITE}",
		},
		{
			name:  "Replace in Bearer token",
			value: "Bearer ${{ secrets.TOKEN }}",
			secrets: map[string]string{
				"TOKEN": "${{ secrets.TOKEN }}",
			},
			expected: "Bearer \\${TOKEN}",
		},
		{
			name:     "No replacement needed",
			value:    "static-value",
			secrets:  map[string]string{},
			expected: "static-value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ReplaceSecretsWithEnvVars(tt.value, tt.secrets)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
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

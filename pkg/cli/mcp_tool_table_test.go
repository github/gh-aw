//go:build !integration

package cli

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/types"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRenderMCPToolTable(t *testing.T) {
	// Create mock data
	mockInfo := &parser.MCPServerInfo{
		Config: parser.MCPServerConfig{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "stdio",
			Command: "test"}, Name: "test-server",

			Allowed: []string{"tool1", "tool3"}, // Only tool1 and tool3 are allowed
		},
		Tools: []*mcp.Tool{
			{
				Name:        "tool1",
				Description: "This is a short description",
			},
			{
				Name:        "tool2",
				Description: "This is a very long description that exceeds the maximum length limit and should be truncated in non-verbose mode",
			},
			{
				Name:        "tool3",
				Description: "Another tool with a medium-length description",
			},
		},
	}

	t.Run("empty_tools_list_returns_empty_string", func(t *testing.T) {
		emptyInfo := &parser.MCPServerInfo{
			Config: parser.MCPServerConfig{Name: "empty-server"},
			Tools:  []*mcp.Tool{},
		}

		opts := DefaultMCPToolTableOptions()
		result := renderMCPToolTable(emptyInfo, opts)

		if result != "" {
			t.Errorf("Expected empty string for empty tools list, got: %s", result)
		}
	})

	t.Run("renders_table_with_default_options", func(t *testing.T) {
		opts := DefaultMCPToolTableOptions()
		result := renderMCPToolTable(mockInfo, opts)

		// Verify table is rendered
		if result == "" {
			t.Error("Expected non-empty result")
		}

		// Check for table headers
		if !strings.Contains(result, "Tool Name") {
			t.Error("Expected 'Tool Name' header")
		}
		if !strings.Contains(result, "Allow") {
			t.Error("Expected 'Allow' header")
		}
		if !strings.Contains(result, "Description") {
			t.Error("Expected 'Description' header")
		}

		// Check for tool names
		if !strings.Contains(result, "tool1") {
			t.Error("Expected 'tool1' in output")
		}
		if !strings.Contains(result, "tool2") {
			t.Error("Expected 'tool2' in output")
		}

		// Check for allow/disallow indicators
		if !strings.Contains(result, "✅") {
			t.Error("Expected '✅' for allowed tools")
		}
		if !strings.Contains(result, "🚫") {
			t.Error("Expected '🚫' for disallowed tools")
		}

		// Check for summary
		if !strings.Contains(result, "Summary") {
			t.Error("Expected summary line")
		}
		if !strings.Contains(result, "2 allowed") {
			t.Error("Expected '2 allowed' in summary")
		}
		if !strings.Contains(result, "1 not allowed") {
			t.Error("Expected '1 not allowed' in summary")
		}
	})

	t.Run("truncates_descriptions_when_requested", func(t *testing.T) {
		opts := MCPToolTableOptions{
			TruncateLength:  30,
			ShowSummary:     false,
			ShowVerboseHint: false,
		}
		result := renderMCPToolTable(mockInfo, opts)

		// Long description should be truncated
		if strings.Contains(result, "exceeds the maximum") {
			t.Error("Long description should be truncated")
		}
		if !strings.Contains(result, "...") {
			t.Error("Truncated descriptions should end with '...'")
		}
	})

	t.Run("no_truncation_when_length_is_zero", func(t *testing.T) {
		opts := MCPToolTableOptions{
			TruncateLength:  0, // No truncation
			ShowSummary:     false,
			ShowVerboseHint: false,
		}
		result := renderMCPToolTable(mockInfo, opts)

		// Full description should be present
		if !strings.Contains(result, "exceeds the maximum length limit") {
			t.Error("Full description should be present when truncation is disabled")
		}
	})

	t.Run("hides_summary_when_disabled", func(t *testing.T) {
		opts := MCPToolTableOptions{
			TruncateLength:  60,
			ShowSummary:     false,
			ShowVerboseHint: false,
		}
		result := renderMCPToolTable(mockInfo, opts)

		if strings.Contains(result, "Summary") {
			t.Error("Summary should not be present when ShowSummary is false")
		}
	})

	t.Run("shows_verbose_hint_when_enabled", func(t *testing.T) {
		opts := MCPToolTableOptions{
			TruncateLength:  60,
			ShowSummary:     false,
			ShowVerboseHint: true,
		}
		result := renderMCPToolTable(mockInfo, opts)

		if !strings.Contains(result, "Run with --verbose") {
			t.Error("Expected verbose hint when ShowVerboseHint is true")
		}
	})

	t.Run("custom_summary_format", func(t *testing.T) {
		opts := MCPToolTableOptions{
			TruncateLength:  60,
			ShowSummary:     true,
			SummaryFormat:   "\nCustom: %d/%d/%d\n",
			ShowVerboseHint: false,
		}
		result := renderMCPToolTable(mockInfo, opts)

		if !strings.Contains(result, "Custom: 2/1/3") {
			t.Error("Expected custom summary format")
		}
	})

	t.Run("no_allowed_tools_means_all_allowed", func(t *testing.T) {
		noAllowedInfo := &parser.MCPServerInfo{
			Config: parser.MCPServerConfig{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "stdio",
				Command: "test"}, Name: "no-allowed-server",

				Allowed: []string{}, // Empty allowed list means all tools allowed
			},
			Tools: []*mcp.Tool{
				{
					Name:        "any_tool",
					Description: "Any tool should be allowed",
				},
			},
		}

		opts := MCPToolTableOptions{
			TruncateLength:  60,
			ShowSummary:     true,
			ShowVerboseHint: false,
		}
		result := renderMCPToolTable(noAllowedInfo, opts)

		// Should show all tools as allowed
		if !strings.Contains(result, "1 allowed") {
			t.Error("Expected all tools to be allowed when no allowed list is specified")
		}
		if !strings.Contains(result, "0 not allowed") {
			t.Error("Expected 0 not allowed when no allowed list is specified")
		}
	})

	t.Run("wildcard_allows_all_tools", func(t *testing.T) {
		wildcardInfo := &parser.MCPServerInfo{
			Config: parser.MCPServerConfig{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "stdio",
				Command: "test"}, Name: "wildcard-server",

				Allowed: []string{"*"}, // Wildcard in workflow config
			},
			Tools: []*mcp.Tool{
				{
					Name:        "any_tool1",
					Description: "First tool",
				},
				{
					Name:        "any_tool2",
					Description: "Second tool",
				},
			},
		}

		opts := MCPToolTableOptions{
			TruncateLength:  60,
			ShowSummary:     true,
			ShowVerboseHint: false,
		}
		result := renderMCPToolTable(wildcardInfo, opts)

		// All tools should be allowed due to wildcard
		if !strings.Contains(result, "2 allowed") {
			t.Error("Expected all tools to be allowed with wildcard")
		}
		if !strings.Contains(result, "0 not allowed") {
			t.Error("Expected 0 not allowed with wildcard")
		}
	})
}

func TestDefaultMCPToolTableOptions(t *testing.T) {
	opts := DefaultMCPToolTableOptions()

	if opts.TruncateLength != 60 {
		t.Errorf("Expected default TruncateLength to be 60, got %d", opts.TruncateLength)
	}

	if !opts.ShowSummary {
		t.Error("Expected default ShowSummary to be true")
	}

	if opts.SummaryFormat != "\n📊 Summary: %d allowed, %d not allowed out of %d total tools\n" {
		t.Errorf("Expected default SummaryFormat, got: %s", opts.SummaryFormat)
	}

	if opts.ShowVerboseHint {
		t.Error("Expected default ShowVerboseHint to be false")
	}
}

func TestRenderMCPHierarchyTree(t *testing.T) {
	// Create test configs
	configs := []parser.MCPServerConfig{
		{
			BaseMCPServerConfig: types.BaseMCPServerConfig{
				Type: "stdio",
			},
			Name:    "github",
			Allowed: []string{"list_issues", "create_issue"},
		},
		{
			BaseMCPServerConfig: types.BaseMCPServerConfig{
				Type: "stdio",
			},
			Name:    "filesystem",
			Allowed: []string{"*"},
		},
	}

	// Create test server infos
	serverInfos := map[string]*parser.MCPServerInfo{
		"github": {
			Config: parser.MCPServerConfig{
				Name:    "github",
				Allowed: []string{"list_issues", "create_issue"},
			},
			Tools: []*mcp.Tool{
				{Name: "list_issues", Description: "List GitHub issues"},
				{Name: "create_issue", Description: "Create a new GitHub issue"},
				{Name: "list_pull_requests", Description: "List pull requests"},
			},
			Resources: []*mcp.Resource{
				{Name: "repo", URI: "github://repo"},
			},
			Roots: []*mcp.Root{
				{Name: "root", URI: "github://root"},
			},
		},
		"filesystem": {
			Config: parser.MCPServerConfig{
				Name:    "filesystem",
				Allowed: []string{"*"},
			},
			Tools: []*mcp.Tool{
				{Name: "read_file", Description: "Read a file"},
				{Name: "write_file", Description: "Write to a file"},
			},
		},
	}

	// Render tree
	output := renderMCPHierarchyTree(configs, serverInfos)

	// Verify output contains expected elements
	expectedStrings := []string{
		"MCP Servers",
		"github",
		"filesystem",
		"list_issues",
		"create_issue",
		"read_file",
		"write_file",
		"Tools",
		"Resources",
		"Roots",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("Expected output to contain '%s', but it didn't.\nGot:\n%s", expected, output)
		}
	}

	// Verify output is not empty
	if output == "" {
		t.Error("renderMCPHierarchyTree returned empty string")
	}
}

func TestRenderMCPHierarchyTree_EmptyConfigs(t *testing.T) {
	configs := []parser.MCPServerConfig{}
	serverInfos := map[string]*parser.MCPServerInfo{}

	output := renderMCPHierarchyTree(configs, serverInfos)

	if output != "" {
		t.Errorf("Expected empty output for empty configs, got: %s", output)
	}
}

func TestRenderMCPHierarchyTree_MissingServerInfo(t *testing.T) {
	configs := []parser.MCPServerConfig{
		{
			BaseMCPServerConfig: types.BaseMCPServerConfig{
				Type: "stdio",
			},
			Name: "missing-server",
		},
	}
	serverInfos := map[string]*parser.MCPServerInfo{}

	output := renderMCPHierarchyTree(configs, serverInfos)

	// Should still render, but with a warning
	if !strings.Contains(output, "missing-server") {
		t.Errorf("Expected output to contain server name, got: %s", output)
	}
	if !strings.Contains(output, "Server info not available") {
		t.Errorf("Expected output to contain warning about missing info, got: %s", output)
	}
}

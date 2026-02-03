package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var mcpInspectServerLog = logger.New("cli:mcp_inspect_server")

// MCP timeout constants
const (
	MCPConnectTimeout    = 10 * time.Second // Timeout for establishing MCP server connections
	MCPOperationTimeout  = 5 * time.Second  // Timeout for MCP operations (ListTools, ListResources)
	MCPServerHTTPTimeout = 30 * time.Minute // Timeout for HTTP server session
)

// headerRoundTripper is a custom http.RoundTripper that adds custom headers to all requests
type headerRoundTripper struct {
	base    http.RoundTripper
	headers map[string]string
}

// RoundTrip implements http.RoundTripper interface
func (h *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone the request to avoid modifying the original
	reqCopy := req.Clone(req.Context())

	// Add custom headers if any are configured
	if h.headers != nil {
		for key, value := range h.headers {
			reqCopy.Header.Set(key, value)
		}
	}

	// Use the base transport to perform the request
	return h.base.RoundTrip(reqCopy)
}

// inspectMCPServer connects to an MCP server and queries its capabilities
func inspectMCPServer(config parser.MCPServerConfig, toolFilter string, verbose bool, useActionsSecrets bool) error {
	mcpInspectServerLog.Printf("Inspecting MCP server: name=%s, type=%s", config.Name, config.Type)
	fmt.Fprintf(os.Stderr, "%s %s (%s)\n",
		console.FormatCommandMessage(config.Name),
		console.FormatInfoMessage(config.Type),
		console.FormatInfoMessage(buildConnectionString(config)))

	// Validate secrets/environment variables
	mcpInspectServerLog.Print("Validating server secrets")
	if err := validateServerSecrets(config, verbose, useActionsSecrets); err != nil {
		mcpInspectServerLog.Printf("Secret validation failed: %v", err)
		errorBox := console.RenderErrorBox(fmt.Sprintf("❌ Secret validation failed: %s", err))
		for _, line := range errorBox {
			fmt.Fprintln(os.Stderr, line)
		}
		return nil // Don't return error, just show validation failure
	}

	// Connect to the server
	mcpInspectServerLog.Printf("Connecting to MCP server: %s", config.Name)
	info, err := connectToMCPServer(config, verbose)
	if err != nil {
		mcpInspectServerLog.Printf("Connection failed: %v", err)
		errorBox := console.RenderErrorBox(fmt.Sprintf("❌ Connection failed: %s", err))
		for _, line := range errorBox {
			fmt.Fprintln(os.Stderr, line)
		}
		return nil // Don't return error, just show connection failure
	}

	mcpInspectServerLog.Printf("Successfully connected to MCP server: %s", config.Name)

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("✅ Successfully connected to MCP server"))
	}

	// Display server capabilities
	displayServerCapabilities(info, toolFilter)

	return nil
}

// buildConnectionString creates a display string for the connection details
func buildConnectionString(config parser.MCPServerConfig) string {
	switch config.Type {
	case "stdio":
		if config.Container != "" {
			return fmt.Sprintf("docker: %s", config.Container)
		}
		if len(config.Args) > 0 {
			return fmt.Sprintf("cmd: %s %s", config.Command, strings.Join(config.Args, " "))
		}
		return fmt.Sprintf("cmd: %s", config.Command)
	case "http":
		return config.URL
	default:
		return config.Type
	}
}

// connectToMCPServer establishes a connection to the MCP server and queries its capabilities
func connectToMCPServer(config parser.MCPServerConfig, verbose bool) (*parser.MCPServerInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	switch config.Type {
	case "stdio":
		return connectStdioMCPServer(ctx, config, verbose)
	case "docker":
		// Docker MCP servers are treated as stdio servers that run via docker command
		return connectStdioMCPServer(ctx, config, verbose)
	case "http":
		return connectHTTPMCPServer(ctx, config, verbose)
	default:
		return nil, fmt.Errorf("unsupported MCP server type: %s", config.Type)
	}
}

// connectStdioMCPServer connects to a stdio-based MCP server using the Go SDK
func connectStdioMCPServer(ctx context.Context, config parser.MCPServerConfig, verbose bool) (*parser.MCPServerInfo, error) {
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Starting stdio MCP server: %s %s", config.Command, strings.Join(config.Args, " "))))
	}

	// Validate the command exists
	if config.Command != "" {
		if _, err := exec.LookPath(config.Command); err != nil {
			return nil, fmt.Errorf("command not found: %s", config.Command)
		}
	}

	// Create the command for the MCP server
	var cmd *exec.Cmd
	if config.Container != "" {
		// Docker container mode
		args := append([]string{"run", "--rm", "-i"}, config.Args...)
		cmd = exec.Command("docker", args...)
	} else {
		// Direct command mode
		cmd = exec.Command(config.Command, config.Args...)
	}

	// Set environment variables
	cmd.Env = os.Environ()
	for key, value := range config.Env {
		// Resolve environment variable references
		resolvedValue := os.ExpandEnv(value)
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, resolvedValue))
	}

	// Create MCP client and connect
	client := mcp.NewClient(&mcp.Implementation{Name: "gh-aw-inspector", Version: "1.0.0"}, nil)
	transport := &mcp.CommandTransport{Command: cmd}

	// Create a timeout context for connection
	connectCtx, cancel := context.WithTimeout(ctx, MCPConnectTimeout)
	defer cancel()

	session, err := client.Connect(connectCtx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MCP server: %w", err)
	}
	defer session.Close()

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Successfully connected to MCP server"))
	}

	// Query server capabilities
	info := &parser.MCPServerInfo{
		Config:    config,
		Connected: true,
		Tools:     []*mcp.Tool{},
		Resources: []*mcp.Resource{},
		Roots:     []*mcp.Root{},
	}

	// List tools
	listToolsCtx, cancel := context.WithTimeout(ctx, MCPOperationTimeout)
	defer cancel()

	toolsResult, err := session.ListTools(listToolsCtx, &mcp.ListToolsParams{})
	if err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to list tools: %v", err)))
		}
	} else {
		info.Tools = append(info.Tools, toolsResult.Tools...)
	}

	// List resources
	listResourcesCtx, cancel := context.WithTimeout(ctx, MCPOperationTimeout)
	defer cancel()

	resourcesResult, err := session.ListResources(listResourcesCtx, &mcp.ListResourcesParams{})
	if err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to list resources: %v", err)))
		}
	} else {
		info.Resources = append(info.Resources, resourcesResult.Resources...)
	}

	// Note: Roots are not directly available via MCP protocol in the current spec,
	// so we'll keep an empty list or try to infer from resources
	for _, resource := range info.Resources {
		// Simple heuristic: extract root URIs from resources
		if strings.Contains(resource.URI, "://") {
			parts := strings.SplitN(resource.URI, "://", 2)
			if len(parts) == 2 {
				rootURI := parts[0] + "://"
				// Check if we already have this root
				found := false
				for _, root := range info.Roots {
					if root.URI == rootURI {
						found = true
						break
					}
				}
				if !found {
					info.Roots = append(info.Roots, &mcp.Root{
						URI:  rootURI,
						Name: parts[0],
					})
				}
			}
		}
	}

	return info, nil
}

// connectHTTPMCPServer connects to an HTTP-based MCP server using the Go SDK
func connectHTTPMCPServer(ctx context.Context, config parser.MCPServerConfig, verbose bool) (*parser.MCPServerInfo, error) {
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Connecting to HTTP MCP server: %s", config.URL)))
	}

	// Create MCP client
	client := mcp.NewClient(&mcp.Implementation{Name: "gh-aw-inspector", Version: "1.0.0"}, nil)

	// Create streamable client transport for HTTP
	transport := &mcp.StreamableClientTransport{
		Endpoint: config.URL,
	}

	// Add custom headers if provided
	if len(config.Headers) > 0 {
		// Create a custom HTTP client with header injection
		baseTransport := http.DefaultTransport
		if baseTransport == nil {
			baseTransport = &http.Transport{}
		}

		transport.HTTPClient = &http.Client{
			Transport: &headerRoundTripper{
				base:    baseTransport,
				headers: config.Headers,
			},
		}
	}

	// Create a timeout context for connection
	connectCtx, cancel := context.WithTimeout(ctx, MCPConnectTimeout)
	defer cancel()

	session, err := client.Connect(connectCtx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to HTTP MCP server: %w", err)
	}
	defer session.Close()

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Successfully connected to HTTP MCP server"))
	}

	// Query server capabilities
	info := &parser.MCPServerInfo{
		Config:    config,
		Connected: true,
		Tools:     []*mcp.Tool{},
		Resources: []*mcp.Resource{},
		Roots:     []*mcp.Root{},
	}

	// List tools
	listToolsCtx, cancel := context.WithTimeout(ctx, MCPOperationTimeout)
	defer cancel()

	toolsResult, err := session.ListTools(listToolsCtx, &mcp.ListToolsParams{})
	if err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to list tools: %v", err)))
		}
	} else {
		info.Tools = append(info.Tools, toolsResult.Tools...)
	}

	// List resources
	listResourcesCtx, cancel := context.WithTimeout(ctx, MCPOperationTimeout)
	defer cancel()

	resourcesResult, err := session.ListResources(listResourcesCtx, &mcp.ListResourcesParams{})
	if err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to list resources: %v", err)))
		}
	} else {
		info.Resources = append(info.Resources, resourcesResult.Resources...)
	}

	// Extract root URIs from resources (simple heuristic)
	for _, resource := range info.Resources {
		if strings.Contains(resource.URI, "://") {
			parts := strings.SplitN(resource.URI, "://", 2)
			if len(parts) == 2 {
				rootURI := parts[0] + "://"
				// Check if we already have this root
				found := false
				for _, root := range info.Roots {
					if root.URI == rootURI {
						found = true
						break
					}
				}
				if !found {
					info.Roots = append(info.Roots, &mcp.Root{
						URI:  rootURI,
						Name: parts[0],
					})
				}
			}
		}
	}

	return info, nil
}

// displayServerCapabilities shows the server's tools, resources, and roots in formatted tables
func displayServerCapabilities(info *parser.MCPServerInfo, toolFilter string) {
	// Display tools with allowed/not allowed status
	if len(info.Tools) > 0 {
		// If a specific tool is requested, show detailed information
		if toolFilter != "" {
			displayDetailedToolInfo(info, toolFilter)
		} else {
			fmt.Fprintf(os.Stderr, "\n%s\n", console.FormatSectionHeader("🛠️  Tool Access Status"))

			// Configure options for inspect command
			// Use a slightly shorter truncation length than list-tools for better fit
			opts := MCPToolTableOptions{
				TruncateLength:  50,
				ShowSummary:     true,
				ShowVerboseHint: false,
			}

			// Render the table using the shared helper
			table := renderMCPToolTable(info, opts)
			fmt.Print(table)

			// Add helpful hint about how to allow tools in workflow frontmatter
			displayToolAllowanceHint(info)
		}

	} else {
		if toolFilter != "" {
			fmt.Fprintf(os.Stderr, "\n%s\n", console.FormatWarningMessage(fmt.Sprintf("Tool '%s' not found", toolFilter)))
		} else {
			fmt.Fprintf(os.Stderr, "\n%s\n", console.FormatWarningMessage("No tools available"))
		}
	}

	// Display resources (skip if showing specific tool details)
	if toolFilter == "" && len(info.Resources) > 0 {
		fmt.Fprintf(os.Stderr, "\n%s\n", console.FormatSectionHeader("📚 Available Resources"))

		headers := []string{"URI", "Name", "Description", "MIME Type"}
		rows := make([][]string, 0, len(info.Resources))

		for _, resource := range info.Resources {
			description := resource.Description
			if len(description) > 40 {
				description = description[:37] + "..."
			}

			mimeType := resource.MIMEType
			if mimeType == "" {
				mimeType = "N/A"
			}

			rows = append(rows, []string{resource.URI, resource.Name, description, mimeType})
		}

		table := console.RenderTable(console.TableConfig{
			Headers: headers,
			Rows:    rows,
		})
		fmt.Print(table)
	} else if toolFilter == "" {
		fmt.Fprintf(os.Stderr, "\n%s\n", console.FormatWarningMessage("No resources available"))
	}

	// Display roots (skip if showing specific tool details)
	if toolFilter == "" && len(info.Roots) > 0 {
		fmt.Fprintf(os.Stderr, "\n%s\n", console.FormatSectionHeader("🌳 Available Roots"))

		headers := []string{"URI", "Name"}
		rows := make([][]string, 0, len(info.Roots))

		for _, root := range info.Roots {
			rows = append(rows, []string{root.URI, root.Name})
		}

		table := console.RenderTable(console.TableConfig{
			Headers: headers,
			Rows:    rows,
		})
		fmt.Print(table)
	} else if toolFilter == "" {
		fmt.Fprintf(os.Stderr, "\n%s\n", console.FormatWarningMessage("No roots available"))
	}

	fmt.Fprintln(os.Stderr)
}

// displayDetailedToolInfo shows detailed information about a specific tool
func displayDetailedToolInfo(info *parser.MCPServerInfo, toolName string) {
	// Find the specific tool
	var foundTool *mcp.Tool
	for _, tool := range info.Tools {
		if tool.Name == toolName {
			foundTool = tool
			break
		}
	}

	if foundTool == nil {
		fmt.Fprintf(os.Stderr, "\n%s\n", console.FormatWarningMessage(fmt.Sprintf("Tool '%s' not found", toolName)))
		fmt.Fprintf(os.Stderr, "Available tools: ")
		toolNames := make([]string, len(info.Tools))
		for i, tool := range info.Tools {
			toolNames[i] = tool.Name
		}
		fmt.Fprintf(os.Stderr, "%s\n", strings.Join(toolNames, ", "))
		return
	}

	// Check if tool is allowed
	isAllowed := len(info.Config.Allowed) == 0 // Default to allowed if no allowlist
	for _, allowed := range info.Config.Allowed {
		if allowed == toolName {
			isAllowed = true
			break
		}
	}

	fmt.Fprintf(os.Stderr, "\n%s\n", console.FormatSectionHeader(fmt.Sprintf("🛠️  Tool Details: %s", foundTool.Name)))

	// Display basic information
	fmt.Fprintf(os.Stderr, "📋 **Name:** %s\n", foundTool.Name)

	// Show title if available and different from name
	if foundTool.Title != "" && foundTool.Title != foundTool.Name {
		fmt.Fprintf(os.Stderr, "📄 **Title:** %s\n", foundTool.Title)
	}
	if foundTool.Annotations != nil && foundTool.Annotations.Title != "" && foundTool.Annotations.Title != foundTool.Name && foundTool.Annotations.Title != foundTool.Title {
		fmt.Fprintf(os.Stderr, "📄 **Annotation Title:** %s\n", foundTool.Annotations.Title)
	}

	fmt.Fprintf(os.Stderr, "📝 **Description:** %s\n", foundTool.Description)

	// Display allowance status
	if isAllowed {
		fmt.Fprintf(os.Stderr, "✅ **Status:** Allowed\n")
	} else {
		fmt.Fprintf(os.Stderr, "🚫 **Status:** Not allowed (add to 'allowed' list in workflow frontmatter)\n")
	}

	// Display annotations if available
	if foundTool.Annotations != nil {
		fmt.Fprintf(os.Stderr, "\n%s\n", console.FormatSectionHeader("⚙️  Tool Attributes"))

		if foundTool.Annotations.ReadOnlyHint {
			fmt.Fprintf(os.Stderr, "🔒 **Read-only:** This tool does not modify its environment\n")
		} else {
			fmt.Fprintf(os.Stderr, "🔓 **Modifies environment:** This tool can make changes\n")
		}

		if foundTool.Annotations.IdempotentHint {
			fmt.Fprintf(os.Stderr, "🔄 **Idempotent:** Calling with same arguments has no additional effect\n")
		}

		if foundTool.Annotations.DestructiveHint != nil {
			if *foundTool.Annotations.DestructiveHint {
				fmt.Fprintf(os.Stderr, "⚠️  **Destructive:** May perform destructive updates\n")
			} else {
				fmt.Fprintf(os.Stderr, "➕ **Additive:** Performs only additive updates\n")
			}
		}

		if foundTool.Annotations.OpenWorldHint != nil {
			if *foundTool.Annotations.OpenWorldHint {
				fmt.Fprintf(os.Stderr, "🌐 **Open world:** Interacts with external entities\n")
			} else {
				fmt.Fprintf(os.Stderr, "🏠 **Closed world:** Domain of interaction is closed\n")
			}
		}
	}

	// Display input schema
	if foundTool.InputSchema != nil {
		fmt.Fprintf(os.Stderr, "\n%s\n", console.FormatSectionHeader("📥 Input Schema"))
		if schemaJSON, err := json.MarshalIndent(foundTool.InputSchema, "", "  "); err == nil {
			fmt.Fprintf(os.Stderr, "```json\n%s\n```\n", string(schemaJSON))
		} else {
			fmt.Fprintf(os.Stderr, "Error displaying input schema: %v\n", err)
		}
	} else {
		fmt.Fprintf(os.Stderr, "\n%s\n", console.FormatInfoMessage("📥 No input schema defined"))
	}

	// Display output schema
	if foundTool.OutputSchema != nil {
		fmt.Fprintf(os.Stderr, "\n%s\n", console.FormatSectionHeader("📤 Output Schema"))
		if schemaJSON, err := json.MarshalIndent(foundTool.OutputSchema, "", "  "); err == nil {
			fmt.Fprintf(os.Stderr, "```json\n%s\n```\n", string(schemaJSON))
		} else {
			fmt.Fprintf(os.Stderr, "Error displaying output schema: %v\n", err)
		}
	} else {
		fmt.Fprintf(os.Stderr, "\n%s\n", console.FormatInfoMessage("📤 No output schema defined"))
	}

	fmt.Fprintln(os.Stderr)
}

// displayToolAllowanceHint shows helpful information about how to allow tools in workflow frontmatter
func displayToolAllowanceHint(info *parser.MCPServerInfo) {
	// Create a map for quick lookup of allowed tools
	allowedMap := make(map[string]bool)
	for _, allowed := range info.Config.Allowed {
		allowedMap[allowed] = true
	}

	// Count blocked tools and collect their names
	var blockedTools []string
	for _, tool := range info.Tools {
		if len(info.Config.Allowed) > 0 && !allowedMap[tool.Name] {
			blockedTools = append(blockedTools, tool.Name)
		}
	}

	if len(blockedTools) > 0 {
		fmt.Fprintf(os.Stderr, "\n%s\n", console.FormatInfoMessage("💡 To allow blocked tools, add them to your workflow frontmatter:"))

		// Show the frontmatter syntax example
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "```yaml\n")
		fmt.Fprintf(os.Stderr, "tools:\n")
		fmt.Fprintf(os.Stderr, "  %s:\n", info.Config.Name)
		fmt.Fprintf(os.Stderr, "    allowed:\n")

		// Add currently allowed tools first (if any)
		for _, allowed := range info.Config.Allowed {
			fmt.Fprintf(os.Stderr, "      - %s\n", allowed)
		}

		// Show first few blocked tools as examples (limit to 3 for readability)
		exampleCount := len(blockedTools)
		if exampleCount > 3 {
			exampleCount = 3
		}

		for i := 0; i < exampleCount; i++ {
			fmt.Fprintf(os.Stderr, "      - %s\n", blockedTools[i])
		}

		if len(blockedTools) > 3 {
			fmt.Fprintf(os.Stderr, "      # ... and %d more tools\n", len(blockedTools)-3)
		}

		fmt.Fprintf(os.Stderr, "```\n")

		if len(blockedTools) > 3 {
			fmt.Fprintf(os.Stderr, "\n%s\n", console.FormatInfoMessage(fmt.Sprintf("📋 All blocked tools: %s", strings.Join(blockedTools, ", "))))
		}
	} else if len(info.Config.Allowed) == 0 {
		// No explicit allowed list - all tools are allowed by default
		fmt.Fprintf(os.Stderr, "\n%s\n", console.FormatInfoMessage("💡 All tools are currently allowed (no 'allowed' list specified)"))
		if len(info.Tools) > 0 {
			fmt.Fprintf(os.Stderr, "\n%s\n", console.FormatInfoMessage("To restrict tools, add an 'allowed' list to your workflow frontmatter:"))
			fmt.Fprintf(os.Stderr, "\n")
			fmt.Fprintf(os.Stderr, "```yaml\n")
			fmt.Fprintf(os.Stderr, "tools:\n")
			fmt.Fprintf(os.Stderr, "  %s:\n", info.Config.Name)
			fmt.Fprintf(os.Stderr, "    allowed:\n")
			fmt.Fprintf(os.Stderr, "      - %s  # Allow only specific tools\n", info.Tools[0].Name)
			if len(info.Tools) > 1 {
				fmt.Fprintf(os.Stderr, "      - %s\n", info.Tools[1].Name)
			}
			fmt.Fprintf(os.Stderr, "```\n")
		}
	} else {
		// All tools are explicitly allowed
		fmt.Fprintf(os.Stderr, "\n%s\n", console.FormatSuccessMessage("✅ All available tools are explicitly allowed in your workflow"))
	}

	fmt.Fprintf(os.Stderr, "\n%s\n", console.FormatInfoMessage("📖 For more information, see: https://github.com/github/gh-aw/blob/main/docs/tools.md"))
}

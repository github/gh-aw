package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var mcpRegistryLog = logger.New("cli:mcp_registry")

// MCPRegistryServerForProcessing represents a flattened server for internal use
type MCPRegistryServerForProcessing struct {
	Name                 string                `json:"name"`
	Description          string                `json:"description"`
	Repository           string                `json:"repository"`
	Command              string                `json:"command"`
	Args                 []string              `json:"args"`
	RuntimeHint          string                `json:"runtime_hint"`
	RuntimeArguments     []string              `json:"runtime_arguments"`
	Transport            string                `json:"transport"`
	Config               map[string]any        `json:"config"`
	EnvironmentVariables []EnvironmentVariable `json:"environment_variables"`
}

// MCPRegistryClient handles communication with MCP registries
type MCPRegistryClient struct {
	registryURL string
	httpClient  *http.Client
}

// NewMCPRegistryClient creates a new MCP registry client
func NewMCPRegistryClient(registryURL string) *MCPRegistryClient {
	if registryURL == "" {
		registryURL = string(constants.DefaultMCPRegistryURL)
	}

	mcpRegistryLog.Printf("Creating MCP registry client: url=%s", registryURL)

	return &MCPRegistryClient{
		registryURL: registryURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// createRegistryRequest creates an HTTP request with appropriate headers for the MCP registry
func (c *MCPRegistryClient) createRegistryRequest(method, url string) (*http.Request, error) {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}

	// Set standard headers
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "gh-aw-cli")

	return req, nil
}

// SearchServers searches for MCP servers in the registry by fetching all servers and filtering locally
func (c *MCPRegistryClient) SearchServers(query string) ([]MCPRegistryServerForProcessing, error) {
	mcpRegistryLog.Printf("Searching MCP servers: query=%q", query)

	// Always use servers endpoint for listing all servers
	searchURL := fmt.Sprintf("%s/servers", c.registryURL)

	// Create HTTP request with proper headers
	req, err := c.createRegistryRequest("GET", searchURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create registry request: %w", err)
	}

	// Make HTTP request with spinner
	spinnerMessage := fmt.Sprintf("Fetching servers from %s...", searchURL)
	spinner := console.NewSpinner(spinnerMessage)
	spinner.Start()
	resp, err := c.httpClient.Do(req)

	if err != nil {
		spinner.Stop()
		return nil, fmt.Errorf("failed to search MCP registry: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		spinner.Stop()
		body, _ := io.ReadAll(resp.Body)
		// Provide more helpful error messages for common HTTP status codes
		switch resp.StatusCode {
		case http.StatusForbidden:
			return nil, fmt.Errorf("MCP registry access forbidden (403): %s\nThis may be due to network or firewall restrictions", string(body))
		case http.StatusUnauthorized:
			return nil, fmt.Errorf("MCP registry access unauthorized (401): %s\nAuthentication may be required", string(body))
		case http.StatusNotFound:
			return nil, fmt.Errorf("MCP registry endpoint not found (404): %s\nPlease verify the registry URL is correct", string(body))
		case http.StatusTooManyRequests:
			return nil, fmt.Errorf("MCP registry rate limit exceeded (429): %s\nPlease try again later", string(body))
		default:
			return nil, fmt.Errorf("MCP registry returned status %d: %s", resp.StatusCode, string(body))
		}
	}

	// Parse response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		spinner.Stop()
		return nil, fmt.Errorf("failed to read registry response: %w", err)
	}

	var response ServerListResponse
	if err := json.Unmarshal(body, &response); err != nil {
		spinner.Stop()
		return nil, fmt.Errorf("failed to parse registry response: %w", err)
	}

	// Stop spinner with success message
	spinner.StopWithMessage(fmt.Sprintf("✓ Fetched %d servers from registry", len(response.Servers)))

	// Convert servers to flattened format and filter by status
	mcpRegistryLog.Printf("Processing %d servers from registry", len(response.Servers))
	servers := make([]MCPRegistryServerForProcessing, 0, len(response.Servers))
	for _, serverResp := range response.Servers {
		server := serverResp.Server

		// Only include active servers (check in _meta)
		if meta, ok := serverResp.Meta["io.modelcontextprotocol.registry/official"].(map[string]any); ok {
			if status, ok := meta["status"].(string); ok && status != StatusActive {
				continue
			}
		}

		processedServer := MCPRegistryServerForProcessing{
			Name:        server.Name,
			Description: server.Description,
		}

		// Set repository URL if available
		if server.Repository != nil && server.Repository.URL != "" {
			processedServer.Repository = server.Repository.URL
		}

		// Extract transport and config from first package if available
		if len(server.Packages) > 0 {
			pkg := server.Packages[0]

			// Use transport type from package
			if pkg.Transport != nil {
				processedServer.Transport = pkg.Transport.Type
			}
			if processedServer.Transport == "" {
				processedServer.Transport = "stdio" // default fallback
			}

			// Set command from package identifier
			processedServer.Command = pkg.Identifier

			// Set runtime hint (used for the actual command execution)
			processedServer.RuntimeHint = pkg.RuntimeHint

			// Extract runtime arguments
			var runtimeArgs []string
			for _, arg := range pkg.RuntimeArguments {
				if arg.Type == ArgumentTypePositional && arg.Value != "" {
					runtimeArgs = append(runtimeArgs, arg.Value)
				}
			}
			processedServer.RuntimeArguments = runtimeArgs

			// Extract string values from package arguments as command args
			var args []string
			for _, arg := range pkg.PackageArguments {
				if arg.Type == ArgumentTypePositional && arg.Value != "" {
					args = append(args, arg.Value)
				}
			}
			processedServer.Args = args

			// Convert environment variables to config
			if len(pkg.EnvironmentVariables) > 0 {
				processedServer.Config = make(map[string]any)
				envVars := make(map[string]any)

				for _, envVar := range pkg.EnvironmentVariables {
					// Use name as key, and create a placeholder value for secrets
					if envVar.IsSecret {
						envVars[envVar.Name] = fmt.Sprintf("${%s}", envVar.Name)
					} else if envVar.Default != "" {
						envVars[envVar.Name] = envVar.Default
					} else {
						envVars[envVar.Name] = fmt.Sprintf("${%s}", envVar.Name)
					}
				}
				processedServer.Config["env"] = envVars

				// Preserve environment variable metadata for proper GitHub Actions conversion
				processedServer.EnvironmentVariables = pkg.EnvironmentVariables
			}
		} else if len(server.Remotes) > 0 {
			// Handle remote servers
			remote := server.Remotes[0]
			processedServer.Transport = remote.Type
			processedServer.Config = map[string]any{
				"url": remote.URL,
			}

			// Add headers if present
			if len(remote.Headers) > 0 {
				headers := make(map[string]any)
				for _, header := range remote.Headers {
					if header.IsSecret {
						headers[header.Name] = fmt.Sprintf("${%s}", header.Name)
					} else if header.Default != "" {
						headers[header.Name] = header.Default
					} else {
						headers[header.Name] = fmt.Sprintf("${%s}", header.Name)
					}
				}
				processedServer.Config["headers"] = headers
			}
		} else {
			processedServer.Transport = "stdio" // default fallback
		}

		servers = append(servers, processedServer)
	}

	// Apply local filtering if query is provided
	if query != "" {
		var filteredServers []MCPRegistryServerForProcessing
		queryLower := strings.ToLower(query)

		for _, server := range servers {
			// Check if query matches name or description (case-insensitive)
			if strings.Contains(strings.ToLower(server.Name), queryLower) ||
				strings.Contains(strings.ToLower(server.Description), queryLower) {
				filteredServers = append(filteredServers, server)
			}
		}

		mcpRegistryLog.Printf("Filtered to %d servers matching query", len(filteredServers))
		return filteredServers, nil
	}

	// Validate minimum server count for production registry
	// Note: This validation helps detect issues with the registry API, but we make it more lenient
	// to accommodate potential changes in the registry size
	if strings.Contains(c.registryURL, "api.mcp.github.com") && len(servers) < 10 {
		return nil, fmt.Errorf("registry validation failed: expected at least 10 servers from production registry, got %d\nThis may indicate an issue with the registry API or access restrictions", len(servers))
	}

	return servers, nil
}

// GetServer gets a specific server by name from the registry
func (c *MCPRegistryClient) GetServer(serverName string) (*MCPRegistryServerForProcessing, error) {
	mcpRegistryLog.Printf("Getting MCP server: name=%s", serverName)

	// Use the servers endpoint and filter locally, just like SearchServers
	serversURL := fmt.Sprintf("%s/servers", c.registryURL)

	// Create HTTP request with proper headers
	req, err := c.createRegistryRequest("GET", serversURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create registry request: %w", err)
	}

	// Make HTTP request with spinner
	spinner := console.NewSpinner(fmt.Sprintf("Fetching MCP server '%s'...", serverName))
	spinner.Start()
	resp, err := c.httpClient.Do(req)

	if err != nil {
		spinner.Stop()
		return nil, fmt.Errorf("failed to get MCP server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		spinner.Stop()
		body, _ := io.ReadAll(resp.Body)
		// Provide more helpful error messages for common HTTP status codes
		switch resp.StatusCode {
		case http.StatusForbidden:
			return nil, fmt.Errorf("MCP registry access forbidden (403): %s\nThis may be due to network or firewall restrictions", string(body))
		case http.StatusUnauthorized:
			return nil, fmt.Errorf("MCP registry access unauthorized (401): %s\nAuthentication may be required", string(body))
		case http.StatusNotFound:
			return nil, fmt.Errorf("MCP registry endpoint not found (404): %s\nPlease verify the registry URL is correct", string(body))
		case http.StatusTooManyRequests:
			return nil, fmt.Errorf("MCP registry rate limit exceeded (429): %s\nPlease try again later", string(body))
		default:
			return nil, fmt.Errorf("MCP registry returned status %d: %s", resp.StatusCode, string(body))
		}
	}

	// Parse response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		spinner.Stop()
		return nil, fmt.Errorf("failed to read registry response: %w", err)
	}

	var response ServerListResponse
	if err := json.Unmarshal(body, &response); err != nil {
		spinner.Stop()
		return nil, fmt.Errorf("failed to parse server response: %w", err)
	}

	// Stop spinner with success message
	spinner.StopWithMessage(fmt.Sprintf("✓ Fetched MCP server '%s'", serverName))

	// Find exact match by name, filtering locally
	for _, serverResp := range response.Servers {
		server := serverResp.Server

		// Check status from _meta
		isActive := true
		if meta, ok := serverResp.Meta["io.modelcontextprotocol.registry/official"].(map[string]any); ok {
			if status, ok := meta["status"].(string); ok {
				isActive = (status == StatusActive)
			}
		}

		if server.Name == serverName && isActive {
			// Convert to flattened format similar to SearchServers
			processedServer := MCPRegistryServerForProcessing{
				Name:        server.Name,
				Description: server.Description,
			}

			// Set repository URL if available
			if server.Repository != nil && server.Repository.URL != "" {
				processedServer.Repository = server.Repository.URL
			}

			// Extract transport and config from first package if available
			if len(server.Packages) > 0 {
				pkg := server.Packages[0]

				// Use transport type from package
				if pkg.Transport != nil {
					processedServer.Transport = pkg.Transport.Type
				}
				if processedServer.Transport == "" {
					processedServer.Transport = "stdio" // default fallback
				}

				// Set command from package identifier
				processedServer.Command = pkg.Identifier

				// Set runtime hint (used for the actual command execution)
				processedServer.RuntimeHint = pkg.RuntimeHint

				// Extract runtime arguments
				var runtimeArgs []string
				for _, arg := range pkg.RuntimeArguments {
					if arg.Type == ArgumentTypePositional && arg.Value != "" {
						runtimeArgs = append(runtimeArgs, arg.Value)
					}
				}
				processedServer.RuntimeArguments = runtimeArgs

				// Extract string values from package arguments as command args
				var args []string
				for _, arg := range pkg.PackageArguments {
					if arg.Type == ArgumentTypePositional && arg.Value != "" {
						args = append(args, arg.Value)
					}
				}
				processedServer.Args = args

				// Convert environment variables to config
				if len(pkg.EnvironmentVariables) > 0 {
					processedServer.Config = make(map[string]any)
					envVars := make(map[string]any)

					for _, envVar := range pkg.EnvironmentVariables {
						// Use name as key, and create a placeholder value for secrets
						if envVar.IsSecret {
							envVars[envVar.Name] = fmt.Sprintf("${%s}", envVar.Name)
						} else if envVar.Default != "" {
							envVars[envVar.Name] = envVar.Default
						} else {
							envVars[envVar.Name] = fmt.Sprintf("${%s}", envVar.Name)
						}
					}
					processedServer.Config["env"] = envVars

					// Preserve environment variable metadata for proper GitHub Actions conversion
					processedServer.EnvironmentVariables = pkg.EnvironmentVariables
				}
			} else if len(server.Remotes) > 0 {
				// Handle remote servers
				remote := server.Remotes[0]
				processedServer.Transport = remote.Type
				processedServer.Config = map[string]any{
					"url": remote.URL,
				}

				// Add headers if present
				if len(remote.Headers) > 0 {
					headers := make(map[string]any)
					for _, header := range remote.Headers {
						if header.IsSecret {
							headers[header.Name] = fmt.Sprintf("${%s}", header.Name)
						} else if header.Default != "" {
							headers[header.Name] = header.Default
						} else {
							headers[header.Name] = fmt.Sprintf("${%s}", header.Name)
						}
					}
					processedServer.Config["headers"] = headers
				}
			} else {
				processedServer.Transport = "stdio" // default fallback
			}

			mcpRegistryLog.Printf("Found MCP server: name=%s, transport=%s", serverName, processedServer.Transport)
			return &processedServer, nil
		}
	}

	mcpRegistryLog.Printf("MCP server not found: name=%s", serverName)
	return nil, fmt.Errorf("MCP server '%s' not found in registry", serverName)
}

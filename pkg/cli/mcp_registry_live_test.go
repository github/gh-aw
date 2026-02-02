//go:build integration

package cli

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/constants"
)

// TestMCPRegistryClient_LiveSearchServers tests SearchServers against the live GitHub MCP registry
func TestMCPRegistryClient_LiveSearchServers(t *testing.T) {
	// Create client with default production registry URL
	client := NewMCPRegistryClient(string(constants.DefaultMCPRegistryURL))

	// Test 1: Search for all servers (empty query)
	t.Run("search_all_servers", func(t *testing.T) {
		servers, err := client.SearchServers("")
		if err != nil {
			// Check if it's a network/firewall issue
			if strings.Contains(err.Error(), "network") || strings.Contains(err.Error(), "firewall") ||
				strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "connection") {
				t.Skipf("Skipping due to network restrictions: %v", err)
				return
			}
			t.Fatalf("SearchServers failed: %v", err)
		}

		// The production registry should have multiple servers
		if len(servers) < 10 {
			t.Logf("Warning: Expected at least 10 servers from production registry, got %d", len(servers))
			t.Logf("This may indicate an issue with the registry or API changes")
		} else {
			t.Logf("✓ Successfully fetched %d servers from live registry", len(servers))
		}

		// Validate that servers have required fields
		if len(servers) > 0 {
			firstServer := servers[0]
			if firstServer.Name == "" {
				t.Errorf("First server has empty name")
			}
			if firstServer.Description == "" {
				t.Errorf("First server has empty description")
			}
			if firstServer.Transport == "" {
				t.Errorf("First server has empty transport")
			}
			t.Logf("✓ First server structure validated: name=%s, transport=%s", firstServer.Name, firstServer.Transport)
		}
	})

	// Test 2: Search for specific servers by query
	t.Run("search_with_query", func(t *testing.T) {
		// Search for GitHub-related servers
		servers, err := client.SearchServers("github")
		if err != nil {
			if strings.Contains(err.Error(), "network") || strings.Contains(err.Error(), "firewall") ||
				strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "connection") {
				t.Skipf("Skipping due to network restrictions: %v", err)
				return
			}
			t.Fatalf("SearchServers with query failed: %v", err)
		}

		if len(servers) == 0 {
			t.Errorf("Expected at least one server matching 'github', got none")
		} else {
			t.Logf("✓ Found %d servers matching 'github'", len(servers))
			// Verify that results match the query
			for _, server := range servers {
				lowerName := strings.ToLower(server.Name)
				lowerDesc := strings.ToLower(server.Description)
				if !strings.Contains(lowerName, "github") && !strings.Contains(lowerDesc, "github") {
					t.Errorf("Server '%s' doesn't contain 'github' in name or description", server.Name)
				}
			}
		}
	})

	// Test 3: Verify different transport types are supported
	t.Run("verify_transport_types", func(t *testing.T) {
		servers, err := client.SearchServers("")
		if err != nil {
			if strings.Contains(err.Error(), "network") || strings.Contains(err.Error(), "firewall") ||
				strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "connection") {
				t.Skipf("Skipping due to network restrictions: %v", err)
				return
			}
			t.Fatalf("SearchServers failed: %v", err)
		}

		transportTypes := make(map[string]int)
		for _, server := range servers {
			transportTypes[server.Transport]++
		}

		t.Logf("✓ Found transport types: %v", transportTypes)

		// stdio should be the most common transport type
		if transportTypes["stdio"] == 0 {
			t.Errorf("Expected at least one server with stdio transport")
		}
	})
}

// TestMCPRegistryClient_LiveGetServer tests GetServer against the live GitHub MCP registry
func TestMCPRegistryClient_LiveGetServer(t *testing.T) {
	// Create client with default production registry URL
	client := NewMCPRegistryClient(string(constants.DefaultMCPRegistryURL))

	// Test 1: Get a known server that should exist in the registry
	// We'll use the GitHub MCP server as it's maintained by GitHub
	t.Run("get_github_server", func(t *testing.T) {
		// First, search for GitHub servers to find an actual server name
		servers, err := client.SearchServers("github")
		if err != nil {
			if strings.Contains(err.Error(), "network") || strings.Contains(err.Error(), "firewall") ||
				strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "connection") {
				t.Skipf("Skipping due to network restrictions: %v", err)
				return
			}
			t.Fatalf("SearchServers failed: %v", err)
		}

		if len(servers) == 0 {
			t.Skip("No GitHub servers found in registry to test GetServer")
			return
		}

		// Get the first server's name
		serverName := servers[0].Name
		t.Logf("Testing GetServer with: %s", serverName)

		// Now test GetServer with that name
		server, err := client.GetServer(serverName)
		if err != nil {
			t.Fatalf("GetServer failed for '%s': %v", serverName, err)
		}

		// Validate the returned server
		if server.Name != serverName {
			t.Errorf("Expected server name '%s', got '%s'", serverName, server.Name)
		}
		if server.Description == "" {
			t.Errorf("Server description is empty")
		}
		if server.Transport == "" {
			t.Errorf("Server transport is empty")
		}

		t.Logf("✓ Successfully retrieved server: %s", server.Name)
		t.Logf("  Description: %s", server.Description)
		t.Logf("  Transport: %s", server.Transport)
		if server.Command != "" {
			t.Logf("  Command: %s", server.Command)
		}
		if server.RuntimeHint != "" {
			t.Logf("  Runtime Hint: %s", server.RuntimeHint)
		}
	})

	// Test 2: Get a server that doesn't exist
	t.Run("get_nonexistent_server", func(t *testing.T) {
		_, err := client.GetServer("nonexistent/fake-server-12345")
		if err == nil {
			t.Errorf("Expected error for nonexistent server, got nil")
		}

		expectedErrorSubstring := "not found in registry"
		if !strings.Contains(err.Error(), expectedErrorSubstring) {
			t.Errorf("Expected error to contain '%s', got: %s", expectedErrorSubstring, err.Error())
		}

		t.Logf("✓ Correctly returned error for nonexistent server")
	})
}

// TestMCPRegistryClient_LiveResponseStructure tests that the v0.1 API structure is correctly parsed
func TestMCPRegistryClient_LiveResponseStructure(t *testing.T) {
	// Create client with default production registry URL
	client := NewMCPRegistryClient(string(constants.DefaultMCPRegistryURL))

	servers, err := client.SearchServers("")
	if err != nil {
		if strings.Contains(err.Error(), "network") || strings.Contains(err.Error(), "firewall") ||
			strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "connection") {
			t.Skipf("Skipping due to network restrictions: %v", err)
			return
		}
		t.Fatalf("SearchServers failed: %v", err)
	}

	if len(servers) == 0 {
		t.Skip("No servers returned from registry")
		return
	}

	// Test that we can parse various fields correctly from the v0.1 structure
	t.Run("validate_v0.1_fields", func(t *testing.T) {
		hasPackageArgs := false
		hasEnvVars := false
		hasRuntimeHint := false
		hasRepository := false

		for _, server := range servers {
			// Check for package arguments
			if len(server.Args) > 0 {
				hasPackageArgs = true
				t.Logf("✓ Found server with package arguments: %s", server.Name)
			}

			// Check for environment variables
			if len(server.EnvironmentVariables) > 0 {
				hasEnvVars = true
				t.Logf("✓ Found server with environment variables: %s (count: %d)",
					server.Name, len(server.EnvironmentVariables))
			}

			// Check for runtime hint
			if server.RuntimeHint != "" {
				hasRuntimeHint = true
				t.Logf("✓ Found server with runtime hint: %s (hint: %s)",
					server.Name, server.RuntimeHint)
			}

			// Check for repository
			if server.Repository != "" {
				hasRepository = true
			}
		}

		// Log what we found
		if hasPackageArgs {
			t.Logf("✓ Successfully parsed packageArguments from v0.1 API")
		}
		if hasEnvVars {
			t.Logf("✓ Successfully parsed environmentVariables from v0.1 API")
		}
		if hasRuntimeHint {
			t.Logf("✓ Successfully parsed runtimeHint from v0.1 API")
		}
		if hasRepository {
			t.Logf("✓ Successfully parsed repository from v0.1 API")
		}

		// At least some servers should have these fields
		if !hasRuntimeHint {
			t.Logf("Warning: No servers found with runtimeHint field")
		}
	})

	// Test transport type parsing
	t.Run("validate_transport_parsing", func(t *testing.T) {
		for _, server := range servers {
			// Transport should always be set
			if server.Transport == "" {
				t.Errorf("Server '%s' has empty transport", server.Name)
			}

			// Transport should be one of the expected values
			validTransports := map[string]bool{
				"stdio":           true,
				"sse":             true,
				"streamable-http": true,
			}

			if !validTransports[server.Transport] {
				t.Logf("Note: Server '%s' has unexpected transport type: %s",
					server.Name, server.Transport)
			}
		}
		t.Logf("✓ Transport types validated for %d servers", len(servers))
	})
}

// TestMCPRegistryClient_GitHubRegistryAccessibility tests that the GitHub MCP registry is accessible
func TestMCPRegistryClient_GitHubRegistryAccessibility(t *testing.T) {
	// This test verifies that the production GitHub MCP registry is accessible
	// It checks basic HTTP connectivity to the /servers endpoint

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	registryURL := string(constants.DefaultMCPRegistryURL) + "/servers"

	req, err := http.NewRequest("GET", registryURL, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	// Set standard headers that our MCP client uses
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "gh-aw-cli")

	resp, err := client.Do(req)
	if err != nil {
		t.Logf("Network request failed: %v", err)
		t.Logf("This may be expected in environments with network restrictions")
		t.Skip("GitHub MCP registry is not accessible - this may be due to network/firewall restrictions")
		return
	}
	defer resp.Body.Close()

	// We expect either 200 (success) or 403 (firewall/network restriction)
	// Both indicate the endpoint exists and is reachable
	switch resp.StatusCode {
	case http.StatusOK:
		t.Logf("✓ GitHub MCP registry is accessible and returned 200 OK")
	case http.StatusForbidden:
		t.Logf("✓ GitHub MCP registry is reachable but returned 403 (expected due to network restrictions)")
	default:
		t.Errorf("GitHub MCP registry returned unexpected status: %d", resp.StatusCode)
	}

	// Verify the Content-Type header indicates this is a JSON API
	contentType := resp.Header.Get("Content-Type")
	if contentType != "" && !strings.Contains(contentType, "application/json") {
		t.Logf("Note: Content-Type is '%s', expected JSON", contentType)
	}
}

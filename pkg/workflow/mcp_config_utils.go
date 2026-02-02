// Package workflow provides utility functions for MCP configuration processing.
//
// # MCP Configuration Utilities
//
// This file contains helper functions for processing and transforming MCP
// configuration data during workflow compilation. These utilities handle
// common operations needed across different MCP server types.
//
// Key functionality:
//   - URL rewriting for Docker networking
//   - Localhost to host.docker.internal translation
//
// URL rewriting:
// When MCP servers run on the host machine (like safe-outputs HTTP server
// on port 3001) but need to be accessed from within a Docker container
// (like the firewall container running the AI agent), localhost URLs must
// be rewritten to use host.docker.internal.
//
// This ensures that containerized AI agents can communicate with MCP servers
// running on the host system while maintaining network isolation.
//
// Supported URL patterns:
//   - http://localhost:port → http://host.docker.internal:port
//   - https://localhost:port → https://host.docker.internal:port
//   - http://127.0.0.1:port → http://host.docker.internal:port
//   - https://127.0.0.1:port → https://host.docker.internal:port
//
// Use cases:
//   - Safe-outputs HTTP server accessed from firewall container
//   - Safe-inputs HTTP server accessed from firewall container
//   - Custom HTTP MCP servers on localhost
//
// Related files:
//   - mcp_renderer.go: Uses URL rewriting for HTTP MCP servers
//   - safe_outputs.go: Safe outputs HTTP server configuration
//   - safe_inputs.go: Safe inputs HTTP server configuration
//
// Example:
//
//	// Before: http://localhost:3001
//	// After:  http://host.docker.internal:3001
//	url := rewriteLocalhostToDockerHost("http://localhost:3001")
package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var mcpUtilsLog = logger.New("workflow:mcp-config-utils")

// rewriteLocalhostToDockerHost rewrites localhost URLs to use host.docker.internal
// This is necessary when MCP servers run on the host machine but are accessed from within
// a Docker container (e.g., when firewall/sandbox is enabled)
func rewriteLocalhostToDockerHost(url string) string {
	// Define the localhost patterns to replace and their docker equivalents
	// Each pattern is a (prefix, replacement) pair
	replacements := []struct {
		prefix      string
		replacement string
	}{
		{"http://localhost", "http://host.docker.internal"},
		{"https://localhost", "https://host.docker.internal"},
		{"http://127.0.0.1", "http://host.docker.internal"},
		{"https://127.0.0.1", "https://host.docker.internal"},
	}

	for _, r := range replacements {
		if strings.HasPrefix(url, r.prefix) {
			newURL := r.replacement + url[len(r.prefix):]
			mcpUtilsLog.Printf("Rewriting localhost URL for Docker access: %s -> %s", url, newURL)
			return newURL
		}
	}

	return url
}

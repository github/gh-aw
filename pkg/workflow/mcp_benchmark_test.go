//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// BenchmarkRenderPlaywrightMCPConfig benchmarks Playwright MCP config generation
func BenchmarkRenderPlaywrightMCPConfig(b *testing.B) {
	playwrightTool := map[string]any{
		"container":       "mcr.microsoft.com/playwright:v1.41.0",
		"allowed-domains": []any{"github.com", "*.github.io"},
	}
	playwrightConfig := parsePlaywrightTool(playwrightTool)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var yaml strings.Builder
		renderPlaywrightMCPConfig(&yaml, playwrightConfig, true)
	}
}

// BenchmarkGeneratePlaywrightDockerArgs benchmarks Playwright args generation
func BenchmarkGeneratePlaywrightDockerArgs(b *testing.B) {
	playwrightTool := map[string]any{
		"container": "mcr.microsoft.com/playwright:v1.41.0",
		"allowed-domains": []any{
			"github.com",
			"*.github.io",
			"api.github.com",
			"*.googleapis.com",
		},
	}
	playwrightConfig := parsePlaywrightTool(playwrightTool)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = generatePlaywrightDockerArgs(playwrightConfig)
	}
}

// BenchmarkRenderPlaywrightMCPConfig_Complex benchmarks complex Playwright config
func BenchmarkRenderPlaywrightMCPConfig_Complex(b *testing.B) {
	playwrightTool := map[string]any{
		"container": "mcr.microsoft.com/playwright:v1.41.0",
		"allowed-domains": []any{
			"github.com",
			"*.github.io",
			"api.github.com",
			"*.googleapis.com",
		},
		"args": []any{"--debug", "--timeout", "30000"},
	}
	playwrightConfig := parsePlaywrightTool(playwrightTool)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var yaml strings.Builder
		renderPlaywrightMCPConfig(&yaml, playwrightConfig, true)
	}
}

// BenchmarkExtractExpressionsFromPlaywrightArgs benchmarks expression extraction
func BenchmarkExtractExpressionsFromPlaywrightArgs(b *testing.B) {
	allowedDomains := []string{
		"github.com",
		"*.github.io",
		"${{ github.server_url }}",
		"*.example.com",
	}
	customArgs := []string{"--debug", "--timeout", "${{ github.event.inputs.timeout }}"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = extractExpressionsFromPlaywrightArgs(allowedDomains, customArgs)
	}
}

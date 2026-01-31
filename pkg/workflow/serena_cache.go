package workflow

import (
	"fmt"
	"strings"

	"github.com/githubnext/gh-aw/pkg/logger"
)

var serenaCacheLog = logger.New("workflow:serena_cache")

// isSerenaEnabled checks if the Serena tool is configured in the workflow
func isSerenaEnabled(data *WorkflowData) bool {
	if data == nil {
		return false
	}

	// Check ParsedTools first (strongly-typed)
	if data.ParsedTools != nil && data.ParsedTools.Serena != nil {
		serenaCacheLog.Print("Serena tool detected via ParsedTools")
		return true
	}

	// Fallback to Tools map for backward compatibility
	if data.Tools != nil {
		if _, exists := data.Tools["serena"]; exists {
			serenaCacheLog.Print("Serena tool detected via Tools map")
			return true
		}
	}

	return false
}

// generateSerenaCacheStep adds a cache step for .serena/cache if Serena tool is enabled
// The cache is configured to:
// - Use path: .serena/cache
// - Ignore if the folder doesn't exist (continue-on-error: true)
// - Expire in 7 days
// - Use "last cache wins" strategy (save-always: true)
func (c *Compiler) generateSerenaCacheStep(yaml *strings.Builder, data *WorkflowData, needsCheckout bool) {
	// Only add cache if Serena is enabled and checkout was performed
	if !isSerenaEnabled(data) || !needsCheckout {
		return
	}

	serenaCacheLog.Print("Generating Serena cache step")

	yaml.WriteString("      - name: Cache Serena\n")
	fmt.Fprintf(yaml, "        uses: %s\n", GetActionPin("actions/cache"))
	yaml.WriteString("        continue-on-error: true\n")
	yaml.WriteString("        with:\n")
	yaml.WriteString("          path: .serena/cache\n")
	yaml.WriteString("          key: serena-${{ runner.os }}-${{ github.run_id }}-${{ github.run_attempt }}\n")
	yaml.WriteString("          restore-keys: |\n")
	yaml.WriteString("            serena-${{ runner.os }}-\n")
	yaml.WriteString("          save-always: true\n")
}

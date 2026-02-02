package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var compilerYamlArtifactsLog = logger.New("workflow:compiler_yaml_artifacts")

// generateExtractAccessLogs is a legacy method that no longer does anything
// Network filtering is now handled at the workflow level
func (c *Compiler) generateExtractAccessLogs(yaml *strings.Builder, tools map[string]any) {
	// No proxy tools anymore - network filtering is handled at workflow level
}

// generateUploadAccessLogs is a legacy method that no longer does anything
// Network filtering is now handled at the workflow level
func (c *Compiler) generateUploadAccessLogs(yaml *strings.Builder, tools map[string]any) {
	// No proxy tools anymore - network filtering is handled at workflow level
}

// generateUnifiedArtifactUpload generates a single step that uploads all agent job artifacts
// This consolidates multiple individual upload steps into one, improving workflow readability
// and reliability. The step always runs (even on cancellation) and ignores missing files.
func (c *Compiler) generateUnifiedArtifactUpload(yaml *strings.Builder, paths []string) {
	if len(paths) == 0 {
		compilerYamlArtifactsLog.Print("No paths to upload, skipping unified artifact upload")
		return
	}

	compilerYamlArtifactsLog.Printf("Generating unified artifact upload with %d paths", len(paths))

	yaml.WriteString("      - name: Upload agent artifacts\n")
	yaml.WriteString("        if: always()\n")
	yaml.WriteString("        continue-on-error: true\n")
	fmt.Fprintf(yaml, "        uses: %s\n", GetActionPin("actions/upload-artifact"))
	yaml.WriteString("        with:\n")
	yaml.WriteString("          name: agent-artifacts\n")

	// Write paths as multi-line YAML string
	yaml.WriteString("          path: |\n")
	for _, path := range paths {
		fmt.Fprintf(yaml, "            %s\n", path)
	}

	yaml.WriteString("          if-no-files-found: ignore\n")

	compilerYamlArtifactsLog.Printf("Generated unified artifact upload step with %d paths", len(paths))
}

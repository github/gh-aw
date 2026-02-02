package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var engineOutputLog = logger.New("workflow:engine_output")

// RedactedURLsLogPath is the path where redacted URL domains are logged during sanitization
const RedactedURLsLogPath = "/tmp/gh-aw/redacted-urls.log"

// generateCleanupStep generates the cleanup step YAML for workspace files, excluding /tmp/gh-aw/ files
// Returns the YAML string and whether a cleanup step was generated
func generateCleanupStep(outputFiles []string) (string, bool) {
	if engineOutputLog.Enabled() {
		engineOutputLog.Printf("Generating cleanup step for %d output files", len(outputFiles))
	}
	// Filter to get only workspace files (exclude /tmp/gh-aw/ files)
	var workspaceFiles []string
	for _, file := range outputFiles {
		if !strings.HasPrefix(file, "/tmp/gh-aw/") {
			workspaceFiles = append(workspaceFiles, file)
		}
	}

	// Only generate cleanup step if there are workspace files to delete
	if len(workspaceFiles) == 0 {
		engineOutputLog.Print("No workspace files to clean up")
		return "", false
	}

	engineOutputLog.Printf("Generated cleanup step for %d workspace files", len(workspaceFiles))

	var yaml strings.Builder
	yaml.WriteString("      - name: Clean up engine output files\n")
	yaml.WriteString("        run: |\n")
	for _, file := range workspaceFiles {
		fmt.Fprintf(&yaml, "          rm -fr %s\n", file)
	}

	return yaml.String(), true
}

// generateEngineOutputCollection generates a step that collects engine-declared output files as artifacts
func (c *Compiler) generateEngineOutputCollection(yaml *strings.Builder, engine CodingAgentEngine) {
	outputFiles := engine.GetDeclaredOutputFiles()
	if len(outputFiles) == 0 {
		engineOutputLog.Print("No engine output files to collect")
		return
	}

	// Add redacted URLs log file to the output files list
	// This file is created during content sanitization if any URLs were redacted
	outputFiles = append(outputFiles, RedactedURLsLogPath)

	engineOutputLog.Printf("Generating engine output collection step for %d files", len(outputFiles))

	// Note: Secret redaction is now handled earlier in the compilation flow,
	// before any artifact uploads. This ensures all files are scanned before upload.

	// Record artifact upload for validation
	c.stepOrderTracker.RecordArtifactUpload("Upload engine output files", outputFiles)

	// Create a single upload step that handles all declared output files
	// The action will ignore missing files automatically with if-no-files-found: ignore
	yaml.WriteString("      - name: Upload engine output files\n")
	fmt.Fprintf(yaml, "        uses: %s\n", GetActionPin("actions/upload-artifact"))
	yaml.WriteString("        with:\n")
	yaml.WriteString("          name: agent_outputs\n")

	// Create the path list for all declared output files
	yaml.WriteString("          path: |\n")
	for _, file := range outputFiles {
		yaml.WriteString("            " + file + "\n")
	}

	yaml.WriteString("          if-no-files-found: ignore\n")

	// Add cleanup step to remove output files after upload
	// Only clean files under the workspace, ignore files in /tmp/gh-aw/
	cleanupYaml, hasCleanup := generateCleanupStep(outputFiles)
	if hasCleanup {
		yaml.WriteString(cleanupYaml)
	}
}

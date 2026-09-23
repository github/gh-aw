package cli

import (
	"os"
	"strings"
)

// runnerGuardSecretExfiltrationRule is the runner-guard rule that flags outbound HTTP
// requests to non-GitHub domains in privileged job contexts as possible secret exfiltration.
const runnerGuardSecretExfiltrationRule = "RGS-012"

// readWorkflowLines reads a workflow file at path and splits it into lines. An empty or
// unreadable path returns nil, so findings are preserved rather than silently dropped.
func readWorkflowLines(path string) []string {
	if path == "" {
		return nil
	}

	// #nosec G304 -- path is produced by resolveRunnerGuardFilePath, which validates that the
	// resolved path stays within the repository root.
	content, err := os.ReadFile(path)
	if err != nil {
		runnerGuardLog.Printf("Failed to read workflow %s for step analysis: %v", path, err)
		return nil
	}

	return strings.Split(string(content), "\n")
}

// isStepBoundaryLine reports whether line marks the start of a GitHub Actions step.
func isStepBoundaryLine(line string) bool {
	trimmed := strings.TrimLeft(line, " ")
	if !strings.HasPrefix(trimmed, "- ") {
		return false
	}
	rest := strings.TrimSpace(trimmed[2:])
	return strings.HasPrefix(rest, "name:") || strings.HasPrefix(rest, "uses:")
}

// isStepNameLine reports whether a step boundary line's step name matches marker.
func isStepNameLine(line string, marker string) bool {
	trimmed := strings.TrimLeft(line, " ")
	rest := strings.TrimPrefix(trimmed, "- ")
	if !strings.HasPrefix(strings.TrimSpace(rest), "name:") {
		return false
	}
	return strings.Contains(rest, marker)
}

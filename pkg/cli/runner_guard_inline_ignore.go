package cli

import "strings"

var generatedSafeOutputJobIDs = map[string]struct{}{
	"activation":       {},
	"conclusion":       {},
	"push_evals_state": {},
	"safe_outputs":     {},
	"unlock":           {},
}

const (
	// runnerGuardIgnoreLookBehind catches findings reported on a command immediately after an
	// inline suppression comment in block-style run scripts.
	runnerGuardIgnoreLookBehind = 3
	// runnerGuardIgnoreLookAhead catches findings reported on nearby generated setup or step
	// boundary lines before the suppressed command. The largest observed offset is 28 lines,
	// for an inline-ignore comment inside a helper function body defined a few lines below the
	// step's `- name:` boundary (the line runner-guard attributes the finding to), so 35 leaves
	// a small buffer without scanning the file.
	runnerGuardIgnoreLookAhead = 35
)

// filterRunnerGuardIgnoredFindings drops findings that have a local inline suppression comment
// in the compiled workflow. runner-guard sometimes reports shell findings on a nearby generated
// setup line rather than the command line, so a forward-looking window is used to catch comments
// that sit immediately before the relevant command in the next run block.
func filterRunnerGuardIgnoredFindings(findings []runnerGuardFinding, gitRoot string) []runnerGuardFinding {
	filtered := make([]runnerGuardFinding, 0, len(findings))
	fileLinesByPath := make(map[string][]string)

	for _, finding := range findings {
		resolvedPath := resolveRunnerGuardFilePath(gitRoot, finding.File)
		lines, ok := fileLinesByPath[resolvedPath]
		if !ok {
			lines = readWorkflowLines(resolvedPath)
			fileLinesByPath[resolvedPath] = lines
		}

		if hasRunnerGuardInlineIgnore(lines, finding.Line, finding.RuleID) {
			runnerGuardLog.Printf("Suppressing %s finding with inline runner-guard ignore in %s", finding.RuleID, finding.File)
			continue
		}
		filtered = append(filtered, finding)
	}

	return filtered
}

// filterGeneratedSafeOutputPermissionFindings removes RGS-005 findings for compiler-generated
// jobs whose write permissions are calculated from lifecycle and safe-output configuration.
// Agent jobs are intentionally not included: their declared permissions must remain read-only.
func filterGeneratedSafeOutputPermissionFindings(findings []runnerGuardFinding, gitRoot string) []runnerGuardFinding {
	filtered := make([]runnerGuardFinding, 0, len(findings))
	fileLinesByPath := make(map[string][]string)

	for _, finding := range findings {
		if finding.RuleID != "RGS-005" {
			filtered = append(filtered, finding)
			continue
		}
		if _, ok := generatedSafeOutputJobIDs[finding.JobID]; !ok {
			filtered = append(filtered, finding)
			continue
		}

		resolvedPath := resolveRunnerGuardFilePath(gitRoot, finding.File)
		lines, ok := fileLinesByPath[resolvedPath]
		if !ok {
			lines = readWorkflowLines(resolvedPath)
			fileLinesByPath[resolvedPath] = lines
		}
		if !isGeneratedGhAwWorkflow(lines) {
			filtered = append(filtered, finding)
			continue
		}
		runnerGuardLog.Printf("Suppressing RGS-005 for compiler-generated safe-output job %s in %s", finding.JobID, finding.File)
	}

	return filtered
}

func isGeneratedGhAwWorkflow(lines []string) bool {
	for _, line := range lines {
		if strings.Contains(line, "To regenerate this workflow, run:") {
			return true
		}
	}
	return false
}

func hasRunnerGuardInlineIgnore(lines []string, lineNum int, ruleID string) bool {
	if len(lines) == 0 || ruleID == "" {
		return false
	}
	if lineNum > len(lines) {
		return false
	}

	center := lineNum - 1
	start := max(center-runnerGuardIgnoreLookBehind, 0)
	end := min(center+runnerGuardIgnoreLookAhead+1, len(lines))

	marker := "runner-guard:ignore " + ruleID
	for _, line := range lines[start:end] {
		if strings.Contains(line, marker) {
			return true
		}
	}
	return false
}

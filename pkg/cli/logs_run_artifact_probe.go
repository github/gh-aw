package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/stringutil"
)

// runContainsSafeOutputType checks if a run's agent_output.json contains a specific safe output type
func runContainsSafeOutputType(runDir string, safeOutputType string, verbose bool) (bool, error) {
	logsOrchestratorLog.Printf("Checking run for safe output type: dir=%s, type=%s", runDir, safeOutputType)
	// Normalize the type for comparison (convert dashes to underscores)
	normalizedType := stringutil.NormalizeSafeOutputIdentifier(safeOutputType)

	// Look for agent_output.json in the run directory
	agentOutputPath := filepath.Join(runDir, constants.AgentOutputFilename.String())

	// Support both new flattened form and old directory form
	if stat, err := os.Stat(agentOutputPath); err != nil || stat.IsDir() {
		// Try old structure
		oldPath := filepath.Join(runDir, constants.AgentOutputArtifactName.String(), constants.AgentOutputArtifactName.String())
		if fileutil.FileExists(oldPath) {
			agentOutputPath = oldPath
		} else {
			// No agent_output.json found
			return false, nil
		}
	}

	// Read the file
	content, err := os.ReadFile(agentOutputPath)
	if err != nil {
		// File doesn't exist or can't be read
		return false, nil
	}

	// Parse the JSON
	var safeOutput struct {
		Items []json.RawMessage `json:"items"`
	}

	if err := json.Unmarshal(content, &safeOutput); err != nil {
		return false, fmt.Errorf("could not parse agent_output.json, expected valid JSON with an \"items\" array: %w", err)
	}

	// Check each item for the specified type
	for _, itemRaw := range safeOutput.Items {
		var item struct {
			Type string `json:"type"`
		}

		if err := json.Unmarshal(itemRaw, &item); err != nil {
			continue // Skip malformed items
		}

		// Normalize the item type for comparison
		normalizedItemType := stringutil.NormalizeSafeOutputIdentifier(item.Type)

		if normalizedItemType == normalizedType {
			return true, nil
		}
	}

	return false, nil
}

// inferWorkflowPathFromDisplayName derives a best-effort workflow file path from a
// workflow display name when the GitHub API returns an empty path for the run.
//
// The inference converts the display name to a kebab-case slug using the same
// character-replacement rules as the JS sanitizeWorkflowName helper, then wraps it
// in the conventional lock-file path:
//
//	"Auto-Triage Issues"  →  ".github/workflows/auto-triage-issues.lock.yml"
//	"CI Failure Doctor"   →  ".github/workflows/ci-failure-doctor.lock.yml"
//
// The result is a best-effort guess and may not exactly match the actual file when
// the workflow filename was chosen independently of its display name. It is only used
// when no authoritative path is available.
func inferWorkflowPathFromDisplayName(displayName string) string {
	if displayName == "" {
		return ""
	}
	// Match sanitizeWorkflowName.cjs: lowercase, replace :[\/\s] with "-",
	// replace other non-identifier chars with "-", preserve "." and "_".
	slug := stringutil.SanitizeName(displayName, &stringutil.SanitizeOptions{
		PreserveSpecialChars: []rune{'.', '_'},
		TrimHyphens:          true,
	})
	if slug == "" {
		return ""
	}
	return constants.WorkflowsDirSlash + slug + ".lock.yml"
}

// runHasDifcFilteredItems checks if a run's gateway logs contain any DIFC_FILTERED events.
// It parses the gateway logs (falling back to rpc-messages.jsonl when gateway.jsonl is absent)
// and returns true when at least one DIFC integrity- or secrecy-filtered event is present.
func runHasDifcFilteredItems(runDir string, verbose bool) (bool, error) {
	logsOrchestratorLog.Printf("Checking run for DIFC filtered items: dir=%s", runDir)

	gatewayMetrics, err := parseGatewayLogs(runDir, verbose)
	if err != nil {
		// No gateway log file present — not an error for workflows without MCP
		return false, nil
	}

	if gatewayMetrics == nil {
		return false, nil
	}

	return gatewayMetrics.TotalFiltered > 0, nil
}

// runHasEvals checks whether a run's output directory contains an evals results file
// (evals.jsonl). It looks in five locations:
//  1. runDir/evals.jsonl — produced when flattenSingleFileArtifacts collapsed the
//     one-file evals artifact from its directory directly to the run root.
//  2. runDir/evals/evals.jsonl — un-flattened artifact directory.
//  3. runDir/{hash}-evals/evals.jsonl — workflow_call hash-prefixed variant.
//  4. runDir/usage/evals.jsonl — compact usage artifact captured by the conclusion job.
//  5. runDir/{hash}-usage/evals.jsonl — workflow_call hash-prefixed compact usage artifact.
func runHasEvals(runDir string, verbose bool) bool {
	if findEvalsResultFile(runDir) != "" {
		return true
	}
	if verbose {
		logsOrchestratorLog.Printf("No evals results found in: %s", runDir)
	}
	return false
}

// findEvalsNotEvaluatedStatusFile returns the path of the run-level evals_status.json
// marker when the run's evals judge did not start, failed, or produced no assistant
// response (status "not_evaluated"), or an empty string otherwise. A not-evaluated run
// has no evals.jsonl, so it never satisfies runHasEvals; the marker is diagnostic
// evidence only and is never treated as answers. It is looked up in the run root
// (flattened artifact), evals/ and {hash}-evals/ artifacts, and usage/evals/ and
// {hash}-usage/evals/ compact usage artifacts.
func findEvalsNotEvaluatedStatusFile(runDir string) string {
	statusName := constants.EvalsStatusFilename.String()
	candidates := []string{filepath.Join(runDir, statusName)}
	entries, err := os.ReadDir(runDir)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name := entry.Name()
			if name == constants.EvalsArtifactName.String() || strings.HasSuffix(name, "-"+constants.EvalsArtifactName.String()) {
				candidates = append(candidates, filepath.Join(runDir, name, statusName))
			}
			if name == constants.UsageArtifactName.String() || strings.HasSuffix(name, "-"+constants.UsageArtifactName.String()) {
				candidates = append(candidates, filepath.Join(runDir, name, constants.EvalsArtifactName.String(), statusName))
			}
		}
	}
	for _, candidate := range candidates {
		data, readErr := os.ReadFile(candidate)
		if readErr != nil {
			continue
		}
		var marker struct {
			Status string `json:"status"`
		}
		if json.Unmarshal(data, &marker) == nil && marker.Status == "not_evaluated" {
			logsOrchestratorLog.Printf("Found evals not-evaluated marker at: %s", candidate)
			return candidate
		}
	}
	return ""
}

// findEvalsResultFile returns the first evals.jsonl path found in the locations
// documented on runHasEvals, or an empty string when the run has no evals results.
func findEvalsResultFile(runDir string) string {
	logsOrchestratorLog.Printf("Checking run for evals results: dir=%s", runDir)

	// Case 1: flattenSingleFileArtifacts moved the file directly to the run root.
	rootEvalsFile := filepath.Join(runDir, constants.EvalsResultFilename.String())
	if fileutil.FileExists(rootEvalsFile) {
		logsOrchestratorLog.Printf("Found evals results at: %s", rootEvalsFile)
		return rootEvalsFile
	}
	usageEvalsFile := filepath.Join(runDir, constants.UsageArtifactName.String(), constants.EvalsResultFilename.String())
	if fileutil.FileExists(usageEvalsFile) {
		logsOrchestratorLog.Printf("Found evals results in usage artifact at: %s", usageEvalsFile)
		return usageEvalsFile
	}

	entries, err := os.ReadDir(runDir)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Match exact "evals" or workflow_call prefixed "{hash}-evals".
		if name == constants.EvalsArtifactName.String() || strings.HasSuffix(name, "-"+constants.EvalsArtifactName.String()) {
			evalsFile := filepath.Join(runDir, name, constants.EvalsResultFilename.String())
			if fileutil.FileExists(evalsFile) {
				logsOrchestratorLog.Printf("Found evals results at: %s", evalsFile)
				return evalsFile
			}
		}
		// Match workflow_call-prefixed "{hash}-usage".
		if strings.HasSuffix(name, "-"+constants.UsageArtifactName.String()) {
			evalsFile := filepath.Join(runDir, name, constants.EvalsResultFilename.String())
			if fileutil.FileExists(evalsFile) {
				logsOrchestratorLog.Printf("Found evals results in workflow_call usage artifact at: %s", evalsFile)
				return evalsFile
			}
		}
	}
	return ""
}

package cli

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var mcpLogsGuardrailLog = logger.New("cli:mcp_logs_guardrail")

const (
	// CharsPerToken is the approximate number of characters per token
	// Using OpenAI's rule of thumb: ~4 characters per token
	CharsPerToken = 4
)

// MCPLogsGuardrailResponse represents the response returned by the logs tool.
// The full data is always written to a file; this response provides the file
// path so the caller can read the data.
// Partial reports known collection or episode evidence gaps. Continuation and
// Continuations preserve the available single-target and per-target cursors.
type MCPLogsGuardrailResponse struct {
	Message         string                 `json:"message"`
	FilePath        string                 `json:"file_path,omitempty"`
	Partial         bool                   `json:"partial,omitempty"`
	Continuation    *ContinuationData      `json:"continuation,omitempty"`
	Continuations   []WorkflowContinuation `json:"continuations,omitempty"`
	CoverageStatus  string                 `json:"coverage_status,omitempty"`
	CoverageReasons []string               `json:"coverage_reasons,omitempty"`
}

func (response *MCPLogsGuardrailResponse) markCollectionStateUnknown(reason string) {
	response.CoverageStatus = "unknown"
	response.CoverageReasons = append(response.CoverageReasons, reason)
	response.Message = "Collection state could not be fully decoded; unknown fields do not establish collection completeness."
}

func (response *MCPLogsGuardrailResponse) decodeCollectionSection(raw json.RawMessage, target any, reason string) bool {
	if len(raw) == 0 {
		return true
	}
	if err := json.Unmarshal(raw, target); err != nil {
		response.markCollectionStateUnknown(reason)
		return false
	}
	return true
}

func extractLogsCollectionState(outputStr string) MCPLogsGuardrailResponse {
	response := MCPLogsGuardrailResponse{}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal([]byte(outputStr), &parsed); err != nil || parsed == nil {
		response.markCollectionStateUnknown("invalid_report_json")
		return response
	}
	decode := response.decodeCollectionSection
	if !decode(parsed["continuation"], &response.Continuation, "invalid_continuation") {
		response.Continuation = nil
	}
	if !decode(parsed["continuations"], &response.Continuations, "invalid_continuations") {
		response.Continuations = nil
	}
	response.Partial = response.Continuation != nil || len(response.Continuations) > 0

	var episodes []json.RawMessage
	decode(parsed["episodes"], &episodes, "invalid_episode_coverage")
	var retrievalFields map[string]json.RawMessage
	decode(parsed["episode_retrieval"], &retrievalFields, "invalid_episode_retrieval")
	if retrievalFields != nil || len(episodes) > 0 {
		// Selected-run coverage cannot establish complete episode membership.
		response.CoverageStatus = "unknown"
	}
	knownPartial := false
	for _, raw := range episodes {
		var episode struct {
			Coverage EpisodeCoverage `json:"coverage"`
		}
		// Type errors leave independently decoded fields available as evidence.
		decode(raw, &episode, "invalid_episode_coverage")
		if episode.Coverage.Status == "partial" || episode.Coverage.CollectionTruncated {
			knownPartial = true
		}
		response.CoverageReasons = append(response.CoverageReasons, episode.Coverage.Reasons...)
	}
	var retrieval EpisodeRetrieval
	decode(retrievalFields["stop_reasons"], &retrieval.StopReasons, "invalid_episode_retrieval")
	decode(retrievalFields["issues"], &retrieval.Issues, "invalid_episode_retrieval")
	for _, reason := range retrieval.StopReasons {
		if reason != "" {
			response.CoverageReasons = append(response.CoverageReasons, episodeRetrievalCoverageReason(reason))
			knownPartial = true
		}
	}
	for _, issue := range retrieval.Issues {
		if issue.Reason != "" {
			response.CoverageReasons = append(response.CoverageReasons, episodeRetrievalCoverageReason(issue.Reason))
			knownPartial = true
		}
	}
	if knownPartial {
		response.CoverageStatus = "partial"
		response.Partial = true
	}
	response.CoverageReasons = slices.DeleteFunc(response.CoverageReasons, func(reason string) bool { return reason == "" })
	slices.Sort(response.CoverageReasons)
	response.CoverageReasons = slices.Compact(response.CoverageReasons)
	return response
}

// extractLogsStaleWarning returns the top-level "stale_warning" field embedded
// in the logs JSON output, or "" when absent or unparseable. This is a
// dedicated field (distinct from the generic "message" field, which is also
// used for non-warning hints such as the usage-only artifact hint) so that
// only genuine stale-data warnings are surfaced as "WARNING" in the MCP
// response.
func extractLogsStaleWarning(outputStr string) string {
	var parsed struct {
		StaleWarning string `json:"stale_warning"`
	}
	if err := json.Unmarshal([]byte(outputStr), &parsed); err != nil {
		mcpLogsGuardrailLog.Printf("extractLogsStaleWarning: failed to parse output JSON: %v", err)
		return ""
	}
	return parsed.StaleWarning
}

// buildLogsFileResponse writes the logs JSON output to a content-addressed cache
// file and returns a JSON response containing the file path.
// The file is named by the SHA256 hash of its content so that identical results
// are deduplicated — if the file already exists it is not rewritten.
// The cache directory is kept separate from the artifact download directory so
// these summary files are never included in artifact uploads.
func buildLogsFileResponse(outputStr string) string {
	return buildLogsFileResponseWithCache(outputStr, defaultMCPLogsCache.store)
}

func buildLogsFileResponseWithCache(outputStr string, store func(string) (string, error)) string {
	filePath, err := store(outputStr)
	if err != nil {
		mcpLogsGuardrailLog.Printf("Failed to save logs report: %v", err)
		return buildLogsFileErrorResponse(err.Error())
	}

	response := extractLogsCollectionState(outputStr)
	response.FilePath = filePath

	var msgs []string
	if response.Message != "" {
		msgs = append(msgs, response.Message)
	}
	if response.Continuation != nil || len(response.Continuations) > 0 {
		msgs = append(msgs, fmt.Sprintf("PARTIAL RESULTS: the download stopped before all matching runs were collected. Collected logs data has been written to '%s'. Read file_path and resume using continuation or every per-target entry in continuations, preserving its repository and all cursor parameters.", filePath))
		if response.Continuation != nil {
			msgs = append(msgs, response.Continuation.Message)
		}
	} else if response.Partial {
		msgs = append(msgs, fmt.Sprintf("PARTIAL RESULTS: episode collection or evidence has known gaps. Collected logs data has been written to '%s'. Read file_path for episode coverage and episode_retrieval bounds, counters, stop reasons and issues; no resumable cursor is available.", filePath))
	} else if response.Message != "" {
		msgs = append(msgs, fmt.Sprintf("Collection state is unknown. Original output has been written to '%s'; use file_path to inspect it.", filePath))
	} else {
		msgs = append(msgs, fmt.Sprintf("Logs data has been written to '%s'. Use file_path to read the collected report.", filePath))
	}
	if response.CoverageStatus != "" {
		msgs = append(msgs, "Episode membership coverage is "+response.CoverageStatus+"; the selected runs do not establish complete episode membership.")
	}
	// Surface the stale-data warning (when no date range was requested and the
	// newest run returned is unexpectedly old) directly in the tool response so
	// callers see it without having to open the file.
	if warning := extractLogsStaleWarning(outputStr); warning != "" {
		msgs = append(msgs, "WARNING: "+warning)
	}
	response.Message = strings.Join(msgs, " ")

	responseJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		mcpLogsGuardrailLog.Printf("Failed to marshal logs file response: %v", err)
		return fmt.Sprintf(`{"message":"Logs data written to file","file_path":%q}`, filePath)
	}

	return string(responseJSON)
}

// buildLogsFileErrorResponse returns a JSON error response when file writing fails.
func buildLogsFileErrorResponse(errMsg string) string {
	response := MCPLogsGuardrailResponse{
		Message: fmt.Sprintf("⚠️  %s. The logs data could not be saved to a file.", errMsg),
	}
	responseJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return fmt.Sprintf(`{"message":%q}`, errMsg)
	}
	return string(responseJSON)
}

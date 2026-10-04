package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var mcpLogsGuardrailLog = logger.New("cli:mcp_logs_guardrail")

const (
	// CharsPerToken is the approximate number of characters per token
	// Using OpenAI's rule of thumb: ~4 characters per token
	CharsPerToken = 4

	// mcpLogsCacheDir is the directory where MCP logs data files are cached.
	// This lives under /tmp/gh-aw/ so that agents can read the files, but
	// is separate from the artifact download directory (/tmp/gh-aw/aw-mcp/logs)
	// so that these JSON summary files are not included in artifact uploads.
	mcpLogsCacheDir = "/tmp/gh-aw/logs-cache"
)

// MCPLogsGuardrailResponse represents the response returned by the logs tool.
// The full data is always written to a file; this response provides the file
// path so the caller can read the data.
// Partial is true when the download stopped early (timeout or count limit) and
// the written data contains a continuation cursor; Continuation then carries the
// parameters needed to fetch the remaining logs.
type MCPLogsGuardrailResponse struct {
	Message      string            `json:"message"`
	FilePath     string            `json:"file_path,omitempty"`
	Partial      bool              `json:"partial,omitempty"`
	Continuation *ContinuationData `json:"continuation,omitempty"`
}

// extractLogsContinuation returns the continuation cursor embedded in the logs
// JSON output, or nil when the output is not JSON or the results are complete.
func extractLogsContinuation(outputStr string) *ContinuationData {
	var parsed struct {
		Continuation *ContinuationData `json:"continuation"`
	}
	if err := json.Unmarshal([]byte(outputStr), &parsed); err != nil {
		return nil
	}
	return parsed.Continuation
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
	if err := ensureMCPLogsCacheDir(); err != nil {
		return buildLogsFileErrorResponse(err.Error())
	}

	sum := sha256.Sum256([]byte(outputStr))
	filePath := filepath.Join(mcpLogsCacheDir, hex.EncodeToString(sum[:])+".json")
	if err := writeMCPLogsCacheFile(filePath, outputStr); err != nil {
		return buildLogsFileErrorResponse(err.Error())
	}

	response := MCPLogsGuardrailResponse{FilePath: filePath}
	var msgs []string
	if continuation := extractLogsContinuation(outputStr); continuation != nil {
		response.Partial = true
		response.Continuation = continuation
		msgs = append(msgs, fmt.Sprintf("PARTIAL RESULTS: the download stopped before all matching runs were collected. %s Partial logs data has been written to '%s'. Use the file_path to read the collected data and the continuation parameters to fetch the remaining logs.", continuation.Message, filePath))
	} else {
		msgs = append(msgs, fmt.Sprintf("Logs data has been written to '%s'. Use the file_path to read the full data.", filePath))
	}
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

func ensureMCPLogsCacheDir() error {
	// Verify or create the cache directory. Use Lstat to detect symlinks and
	// refuse to follow them, hardening against symlink-based directory attacks.
	if info, err := os.Lstat(mcpLogsCacheDir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("logs cache path %q is a symlink; refusing to use it", mcpLogsCacheDir)
		}
		if !info.IsDir() {
			return fmt.Errorf("logs cache path %q is not a directory", mcpLogsCacheDir)
		}
	} else if os.IsNotExist(err) {
		if mkErr := os.MkdirAll(mcpLogsCacheDir, constants.DirPermPublic); mkErr != nil && !os.IsExist(mkErr) {
			mcpLogsGuardrailLog.Printf("Failed to create logs cache directory: %v", mkErr)
			return fmt.Errorf("failed to create logs cache directory: %w", mkErr)
		}
	} else {
		mcpLogsGuardrailLog.Printf("Failed to stat logs cache directory: %v", err)
		return fmt.Errorf("failed to access logs cache directory: %w", err)
	}
	if chmodErr := os.Chmod(mcpLogsCacheDir, constants.DirPermPublic); chmodErr != nil {
		mcpLogsGuardrailLog.Printf("Failed to set logs cache directory permissions: %v", chmodErr)
		return fmt.Errorf("failed to set logs cache directory permissions: %w", chmodErr)
	}
	return nil
}

func writeMCPLogsCacheFile(filePath, outputStr string) error {
	// Skip writing if a file with identical content already exists.
	if fileInfo, err := os.Lstat(filePath); err == nil {
		if fileInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("logs cache file path %q is a symlink; refusing to use it", filePath)
		}
		if !fileInfo.Mode().IsRegular() {
			return fmt.Errorf("logs cache file path %q is not a regular file", filePath)
		}
		if chmodErr := os.Chmod(filePath, constants.FilePermPublic); chmodErr != nil {
			mcpLogsGuardrailLog.Printf("Failed to update logs cache file permissions: %v", chmodErr)
			return fmt.Errorf("failed to set logs cache file permissions: %w", chmodErr)
		}
		mcpLogsGuardrailLog.Printf("Logs data already cached at: %s", filePath)
	} else if os.IsNotExist(err) {
		// Write with O_EXCL to avoid following symlinks or races.
		writeErr := func() (err error) {
			f, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, constants.FilePermPublic)
			if err != nil {
				return fmt.Errorf("failed to create logs cache file: %w", err)
			}
			defer func() {
				if closeErr := f.Close(); closeErr != nil && err == nil {
					err = fmt.Errorf("failed to write logs data to file: %w", closeErr)
				}
			}()

			if _, err = f.WriteString(outputStr); err != nil {
				return fmt.Errorf("failed to write logs data to file: %w", err)
			}
			return nil
		}()
		if writeErr != nil {
			mcpLogsGuardrailLog.Printf("Failed to populate logs cache file %s: %v", filePath, writeErr)
			_ = os.Remove(filePath)
			return writeErr
		}
		if chmodErr := os.Chmod(filePath, constants.FilePermPublic); chmodErr != nil {
			_ = os.Remove(filePath)
			mcpLogsGuardrailLog.Printf("Failed to set logs cache file permissions: %v", chmodErr)
			return fmt.Errorf("failed to set logs cache file permissions: %w", chmodErr)
		}
		mcpLogsGuardrailLog.Printf("Logs data written to file: %s (%d bytes)", filePath, len(outputStr))
	} else {
		mcpLogsGuardrailLog.Printf("Failed to stat logs cache file: %v", err)
		return fmt.Errorf("failed to access logs cache file: %w", err)
	}
	return nil
}

// makeMCPFirstRequestArtifactsReadable exposes only the selected runs' event
// logs to the agent sandbox, which runs as a different user from the MCP server.
// Other runs and downloaded artifacts retain their private modes.
func makeMCPFirstRequestArtifactsReadable(outputDir string, runItems []string) error {
	runIDs := mcpRunIDs(runItems)
	if len(runItems) > 0 && len(runIDs) == 0 {
		return errors.New("could not resolve run IDs for MCP log artifacts")
	}

	parentDir := filepath.Dir(outputDir)
	for _, dir := range []string{parentDir, outputDir} {
		info, err := os.Lstat(dir)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to inspect MCP logs directory %q: %w", dir, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("MCP logs directory %q is a symlink", dir)
		}
		if !info.IsDir() {
			return fmt.Errorf("MCP logs path %q is not a directory", dir)
		}
		if err := os.Chmod(dir, constants.DirPermPublic); err != nil {
			return fmt.Errorf("failed to make MCP logs directory %q readable: %w", dir, err)
		}
	}

	for _, runID := range runIDs {
		runDir := filepath.Join(outputDir, "run-"+runID)
		info, err := os.Lstat(runDir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("failed to inspect MCP logs directory %q: %w", runDir, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("MCP logs path %q is not a regular directory", runDir)
		}
		if err := os.Chmod(runDir, constants.DirPermPublic); err != nil {
			return fmt.Errorf("failed to make MCP logs directory %q readable: %w", runDir, err)
		}
		if err := makeMCPFirstRequestRunReadable(outputDir, runDir); err != nil {
			return err
		}
	}
	return nil
}

func mcpLogsRequestFirstRequestArtifacts(artifacts []string) bool {
	for _, artifact := range artifacts {
		if ArtifactSet(artifact) == ArtifactSetAll ||
			ArtifactSet(artifact) == ArtifactSetAgent ||
			ArtifactSet(artifact) == ArtifactSetFirewall {
			return true
		}
	}
	return false
}

func mcpRunItemsFromLogsOutput(output string) []string {
	var data struct {
		Runs []struct {
			RunID int64 `json:"run_id"`
		} `json:"runs"`
	}
	if err := json.Unmarshal([]byte(output), &data); err != nil {
		return nil
	}
	runItems := make([]string, 0, len(data.Runs))
	for _, run := range data.Runs {
		if run.RunID > 0 {
			runItems = append(runItems, strconv.FormatInt(run.RunID, 10))
		}
	}
	return runItems
}

func mcpRunIDs(runItems []string) []string {
	seen := make(map[string]struct{}, len(runItems))
	runIDs := make([]string, 0, len(runItems))
	for _, item := range runItems {
		runID, err := strconv.ParseInt(item, 10, 64)
		if err != nil {
			parsedURL, urlErr := url.Parse(item)
			if urlErr != nil {
				continue
			}
			segments := strings.Split(strings.Trim(parsedURL.Path, "/"), "/")
			parseRunID := false
			for _, segment := range segments {
				if parseRunID {
					runID, err = strconv.ParseInt(segment, 10, 64)
					break
				}
				parseRunID = segment == "runs"
			}
		}
		if err != nil || runID <= 0 {
			continue
		}
		value := strconv.FormatInt(runID, 10)
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		runIDs = append(runIDs, value)
	}
	return runIDs
}

func makeMCPFirstRequestRunReadable(outputDir, runDir string) error {
	return filepath.WalkDir(runDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("MCP logs path %q is a symlink", path)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("failed to inspect MCP logs file %q: %w", path, err)
		}
		if info.Mode().IsRegular() && isMCPFirstRequestArtifact(outputDir, path) {
			if err := os.Chmod(path, constants.FilePermPublic); err != nil {
				return err
			}
			for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
				if err := os.Chmod(dir, constants.DirPermPublic); err != nil {
					return err
				}
				if dir == runDir {
					break
				}
			}
		}
		return nil
	})
}

func isMCPFirstRequestArtifact(outputDir, path string) bool {
	relativePath, err := filepath.Rel(outputDir, path)
	if err != nil {
		return false
	}
	normalizedPath := filepath.ToSlash(relativePath)
	baseName := filepath.Base(path)
	isEventLog := baseName == "event-logs.jsonl" || baseName == "events.jsonl"
	if !isEventLog {
		return false
	}
	return strings.Contains(normalizedPath, "sandbox/firewall/logs/api-proxy-logs/") ||
		strings.Contains(normalizedPath, "sandbox/firewall-audit-logs/api-proxy-logs/") ||
		(baseName == "events.jsonl" && strings.Contains(normalizedPath, "sandbox/agent/logs/copilot-session-state/"))
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

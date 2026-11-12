package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const maxClaudeAuditLogLine = 4 << 20

func countBlockedSafeOutputsCommands(logsPath string) int {
	if logsPath == "" {
		return 0
	}

	file, err := os.Open(filepath.Join(logsPath, "agent-stdio.log"))
	if err != nil {
		return 0
	}
	defer func() {
		if err := file.Close(); err != nil {
			auditReportLog.Printf("Failed to close Claude agent log: %v", err)
		}
	}()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), maxClaudeAuditLogLine)
	return countBlockedSafeOutputsFromScanner(scanner)
}

func countBlockedSafeOutputsFromScanner(scanner *bufio.Scanner) int {
	type toolUse struct {
		Type  string `json:"type"`
		ID    string `json:"id"`
		Name  string `json:"name"`
		Input struct {
			Command string `json:"command"`
		} `json:"input"`
	}
	type permissionDenial struct {
		ToolName  string `json:"tool_name"`
		ToolUseID string `json:"tool_use_id"`
	}
	type logEntry struct {
		Type    string `json:"type"`
		Message struct {
			Content []toolUse `json:"content"`
		} `json:"message"`
		PermissionDenials []permissionDenial `json:"permission_denials"`
	}

	safeOutputsToolUses := make(map[string]struct{})
	blockedCalls := make(map[string]struct{})
	for scanner.Scan() {
		var entry logEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}

		switch entry.Type {
		case "assistant":
			for _, content := range entry.Message.Content {
				if content.Type == "tool_use" && content.Name == "Bash" && isSafeOutputsCommand(content.Input.Command) {
					safeOutputsToolUses[content.ID] = struct{}{}
				}
			}
		case "result":
			for _, denial := range entry.PermissionDenials {
				if !strings.EqualFold(denial.ToolName, "Bash") {
					continue
				}
				if _, exists := safeOutputsToolUses[denial.ToolUseID]; exists && denial.ToolUseID != "" {
					blockedCalls[denial.ToolUseID] = struct{}{}
				}
			}
		}
	}
	if scanner.Err() != nil {
		return len(blockedCalls)
	}
	return len(blockedCalls)
}

func isSafeOutputsCommand(command string) bool {
	command = strings.TrimSpace(command)
	return command == "safeoutputs" ||
		strings.HasPrefix(command, "safeoutputs ") ||
		strings.HasPrefix(command, "safeoutputs\t") ||
		strings.HasPrefix(command, "safeoutputs\n") ||
		strings.HasPrefix(command, "safeoutputs\r") ||
		strings.HasPrefix(command, "safeoutputs;") ||
		strings.HasPrefix(command, "safeoutputs&") ||
		strings.HasPrefix(command, "safeoutputs|")
}

func generateSafeOutputsBlockedFindings(processedRun ProcessedRun) []AuditFinding {
	blockedCalls := countBlockedSafeOutputsCommands(processedRun.Run.LogsPath)
	if blockedCalls == 0 {
		return nil
	}
	return []AuditFinding{{
		Code:        AuditFindingSafeOutputsBlocked,
		Category:    "tooling",
		Severity:    "high",
		Title:       "Safe Output Command Blocked",
		Description: fmt.Sprintf("Claude denied %d safeoutputs Bash invocation(s) through its tool-permission rules", blockedCalls),
		Impact:      "The requested safe output may not have been published",
	}}
}

func appendSafeOutputsBlockedRecommendations(recommendations []Recommendation, findings []AuditFinding) []Recommendation {
	for _, finding := range findings {
		if finding.Code == AuditFindingSafeOutputsBlocked {
			return append(recommendations, Recommendation{
				Priority: "high",
				Action:   "Allow the safeoutputs CLI through the Bash allowlist",
				Reason:   "Claude denied a safeoutputs command under the configured tool permissions",
				Example:  "Add `safeoutputs *` to `tools.bash` and recompile",
			})
		}
	}
	return recommendations
}

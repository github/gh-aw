package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/fileutil"
)

// findTokenUsageFile searches for token-usage.jsonl in the run directory
func findTokenUsageFile(runDir string) string {
	usageArtifactCandidate := filepath.Join(runDir, "usage", "agent", "token_usage.jsonl")
	if fileutil.FileExists(usageArtifactCandidate) {
		tokenUsageLog.Printf("Found token usage file in usage artifact: %s", usageArtifactCandidate)
		return usageArtifactCandidate
	}

	// Primary path: sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl
	primary := filepath.Join(runDir, "sandbox", "firewall", "logs", tokenUsageJSONLPath)
	if fileutil.FileExists(primary) {
		tokenUsageLog.Printf("Found token usage file at primary path: %s", primary)
		return primary
	}

	// AWF v0.27.7+ audit-dir path: sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl
	// In newer AWF versions the proxy logs are written under --audit-dir rather than
	// --proxy-logs-dir, so check this path explicitly before falling back to the walk.
	awfAuditPath := filepath.Join(runDir, "sandbox", "firewall", "audit", tokenUsageJSONLPath)
	if fileutil.FileExists(awfAuditPath) {
		tokenUsageLog.Printf("Found token usage file at AWF audit path: %s", awfAuditPath)
		return awfAuditPath
	}

	if legacy := findLegacyAPIProxyLogFile(runDir, tokenUsageJSONLPath); legacy != "" {
		tokenUsageLog.Printf("Found token usage file in legacy artifact: %s", legacy)
		return legacy
	}

	// Walk sandbox directory for any token-usage.jsonl
	var found string
	if walkErr := filepath.Walk(runDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			tokenUsageLog.Printf("walk error at %s: %v", path, err)
			return nil
		}
		if info == nil || info.IsDir() {
			return nil
		}
		if info.Name() == "token-usage.jsonl" || info.Name() == "token_usage.jsonl" {
			found = path
			return filepath.SkipAll
		}
		return nil
	}); walkErr != nil && !errors.Is(walkErr, filepath.SkipAll) {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("filesystem error walking %s: %v", runDir, walkErr)))
	}
	if found != "" {
		tokenUsageLog.Printf("Found token usage file via walk: %s", found)
		return found
	}

	tokenUsageLog.Print("No token usage file found")
	return ""
}

// findAgentUsageFile searches for agent_usage.json in the run directory.
func findAgentUsageFile(runDir string) string {
	primary := filepath.Join(runDir, agentUsageJSONPath)
	if fileutil.FileExists(primary) {
		tokenUsageLog.Printf("Found agent usage file at primary path: %s", primary)
		return primary
	}

	var found string
	if walkErr := filepath.Walk(runDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			tokenUsageLog.Printf("walk error at %s: %v", path, err)
			return nil
		}
		if info == nil || info.IsDir() {
			return nil
		}
		if info.Name() == agentUsageJSONPath {
			found = path
			return filepath.SkipAll
		}
		return nil
	}); walkErr != nil && !errors.Is(walkErr, filepath.SkipAll) {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("filesystem error walking %s: %v", runDir, walkErr)))
	}

	if found != "" {
		tokenUsageLog.Printf("Found agent usage file via walk: %s", found)
	}
	return found
}

func findUsageJSONLFiles(runDir string) []string {
	usageDir := filepath.Join(runDir, "usage")
	if _, err := os.Stat(usageDir); err != nil {
		return nil
	}

	var files []string
	if walkErr := filepath.Walk(usageDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			tokenUsageLog.Printf("walk error at %s: %v", path, err)
			return nil
		}
		if info == nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(strings.ToLower(info.Name()), ".jsonl") {
			files = append(files, path)
		}
		return nil
	}); walkErr != nil {
		tokenUsageLog.Printf("usage walk error at %s: %v", usageDir, walkErr)
	}

	sort.Strings(files)
	return files
}

func findLegacyAPIProxyLogFile(runDir, relativePath string) string {
	entries, err := os.ReadDir(runDir)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "firewall-audit-logs") || strings.HasPrefix(name, "firewall-logs") {
			candidate := filepath.Join(runDir, name, relativePath)
			if fileutil.FileExists(candidate) {
				return candidate
			}
		}
	}
	return ""
}

func findAPIProxyEventsFile(runDir string) string {
	primary := filepath.Join(runDir, "sandbox", "firewall", "logs", proxyEventsJSONLPath)
	if fileutil.FileExists(primary) {
		return primary
	}

	awfAuditPath := filepath.Join(runDir, "sandbox", "firewall", "audit", proxyEventsJSONLPath)
	if fileutil.FileExists(awfAuditPath) {
		return awfAuditPath
	}

	return findLegacyAPIProxyLogFile(runDir, proxyEventsJSONLPath)
}

func findAgentStdioFile(runDir string) string {
	primary := filepath.Join(runDir, "agent-stdio.log")
	if fileutil.FileExists(primary) {
		return primary
	}

	var found string
	if walkErr := filepath.Walk(runDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info == nil || info.IsDir() {
			return nil
		}
		if info.Name() == "agent-stdio.log" {
			found = path
			return filepath.SkipAll
		}
		return nil
	}); walkErr != nil && !errors.Is(walkErr, filepath.SkipAll) {
		tokenUsageLog.Printf("findAgentStdioFile walk error: %v", walkErr)
	}

	return found
}

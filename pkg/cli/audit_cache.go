package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/github/gh-aw/pkg/constants"
	"golang.org/x/sync/errgroup"
)

type auditCacheSource string

const (
	auditCacheSourceFull auditCacheSource = "full"
	auditCacheSourceLogs auditCacheSource = "logs"
)

func auditPath(runOutputDir string) string {
	return filepath.Join(runOutputDir, auditFileName)
}

func existingAuditPath(runOutputDir string) string {
	path := auditPath(runOutputDir)
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return ""
}

func loadCachedAuditData(runOutputDir string, run WorkflowRun, source auditCacheSource) (AuditData, bool) {
	data, err := os.ReadFile(auditPath(runOutputDir))
	if err != nil {
		return AuditData{}, false
	}
	var auditData AuditData
	if err := json.Unmarshal(data, &auditData); err != nil {
		return AuditData{}, false
	}
	if auditData.Overview.RunID != run.DatabaseID ||
		auditData.Overview.Status != run.Status ||
		auditData.Overview.Conclusion != run.Conclusion ||
		!auditData.Overview.UpdatedAt.Equal(run.UpdatedAt) ||
		auditData.CacheSource != source {
		return AuditData{}, false
	}
	return auditData, true
}

func writeAuditData(runOutputDir string, auditData AuditData) error {
	data, err := json.MarshalIndent(auditData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal audit data: %w", err)
	}
	if err := os.MkdirAll(runOutputDir, constants.DirPermSensitive); err != nil {
		return fmt.Errorf("failed to create audit output directory: %w", err)
	}
	if err := os.WriteFile(auditPath(runOutputDir), data, constants.FilePermSensitive); err != nil {
		return fmt.Errorf("failed to write audit file: %w", err)
	}
	if err := os.Chmod(auditPath(runOutputDir), constants.FilePermSensitive); err != nil {
		return fmt.Errorf("failed to secure audit file: %w", err)
	}
	return nil
}

func writeLogsAuditFiles(processedRuns []ProcessedRun, verbose bool) {
	var group errgroup.Group
	group.SetLimit(runtime.GOMAXPROCS(0))
	for _, processedRun := range processedRuns {
		group.Go(func() error {
			writeLogsAuditFile(processedRun, processedRuns, verbose)
			return nil
		})
	}
	_ = group.Wait()
}

func writeLogsAuditFile(processedRun ProcessedRun, processedRuns []ProcessedRun, verbose bool) {
	runOutputDir := processedRun.Run.LogsPath
	auditData, ok := loadCachedAuditData(runOutputDir, processedRun.Run, auditCacheSourceLogs)
	if !ok {
		metrics := LogMetrics{}
		if summary, ok := loadRunSummary(runOutputDir, verbose); ok {
			metrics = summary.Metrics
		}
		auditData, _ = buildLocalAuditData(processedRun, metrics, processedRun.MCPToolUsage)
		auditData.CacheSource = auditCacheSourceLogs
	}
	auditData.Comparison = buildAuditComparisonForProcessedRuns(processedRun, processedRuns)
	if err := writeAuditData(runOutputDir, auditData); err != nil {
		logsOrchestratorLog.Printf("Failed to write audit file for run %d: %v", processedRun.Run.DatabaseID, err)
	}
}

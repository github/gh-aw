package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/atif"
	"github.com/spf13/cobra"
)

func addATIFFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("atif", false, "Export unified sessions as ATIF-v1.8 JSON (.atif)")
}

func convertSessionATIF(ctx context.Context, content []byte) ([]byte, error) {
	trajectory, err := atif.Convert(ctx, bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("convert session to ATIF: %w", err)
	}
	var output bytes.Buffer
	if err := atif.Encode(&output, trajectory); err != nil {
		return nil, fmt.Errorf("encode ATIF trajectory: %w", err)
	}
	return output.Bytes(), nil
}

// exportRunATIF adds a sibling trajectory when a unified session is available.
// It retains the original JSONL and never downloads missing sessions.
func exportRunATIF(ctx context.Context, runID int64, runDir string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sessionPath, err := findUnifiedSessionPath(runDir)
	if err != nil || sessionPath == "" {
		return err
	}
	content, err := readATIFSessionFile(sessionPath)
	if err != nil {
		return fmt.Errorf("read session for run %d ATIF export: %w", runID, err)
	}
	conversionContent, err := normalizeATIFSession(ctx, content, sessionPath, runDir)
	if err != nil {
		return err
	}
	output, err := convertSessionATIF(ctx, conversionContent)
	if err != nil {
		return fmt.Errorf("run %d: %w", runID, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	outputPath := strings.TrimSuffix(sessionPath, filepath.Ext(sessionPath)) + ".atif"
	if err := writeSessionFileAtomically(outputPath, output); err != nil {
		return fmt.Errorf("write ATIF trajectory for run %d: %w", runID, err)
	}
	return nil
}

func readATIFSessionFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("unified session must be a regular file for ATIF export")
	}
	if info.Size() > atif.MaxSessionBytes {
		return nil, errors.New("unified session exceeds the 64 MiB ATIF input limit")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	content, readErr := io.ReadAll(io.LimitReader(file, atif.MaxSessionBytes+1))
	if err := errors.Join(readErr, file.Close()); err != nil {
		return nil, err
	}
	if len(content) > atif.MaxSessionBytes {
		return nil, errors.New("unified session exceeds the 64 MiB ATIF input limit")
	}
	return content, nil
}

func normalizeATIFSession(ctx context.Context, content []byte, sessionPath, runDir string) ([]byte, error) {
	legacy, err := hasLegacyPiObservations(content)
	if err != nil {
		return nil, err
	}
	if !legacy {
		return content, nil
	}
	info, err := parseAwInfo(filepath.Join(runDir, "aw_info.json"), false)
	if errors.Is(err, os.ErrNotExist) {
		// Without trusted engine metadata these remain opaque extension evidence.
		return content, nil
	}
	if err != nil {
		return nil, err
	}
	if info.EngineID != "pi" {
		return content, nil
	}
	return runSessionParser(ctx, "normalize", sessionPath, "pi")
}

func exportLogsATIF(ctx context.Context, run WorkflowRun, outputDir string, storageLimit *logsStorageLimit, reserved bool) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	runDir := run.LogsPath
	if runDir == "" {
		runDir = resolveAuditOutputDir(outputDir, run.DatabaseID)
	}
	if sessionPath, err := findUnifiedSessionPath(runDir); err != nil || sessionPath == "" {
		return err
	}
	if !reserved && storageLimit != nil {
		if err := storageLimit.reserve(runDir); err != nil {
			return err
		}
		defer func() { err = errors.Join(err, storageLimit.finalizeDownload(runDir)) }()
	}
	return storageLimit.runDownloadWithPruning(ctx, runDir, func() error {
		return exportRunATIF(ctx, run.DatabaseID, runDir)
	}, false, true)
}

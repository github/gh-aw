package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func runOfflineGraders(ctx context.Context, directory string, selectors []string, output io.Writer) error {
	definitions, err := loadAdditionalGraders(selectors)
	if err != nil {
		return err
	}
	runs, err := loadOfflineGradingRuns(directory)
	if err != nil {
		return err
	}
	originals, err := offlineGradingOriginals(directory, runs)
	if err != nil {
		return err
	}
	regraded, err := regradeProcessedRuns(ctx, directory, runs, definitions)
	if err != nil {
		return err
	}
	for index := range regraded {
		run := &regraded[index] //nolint:uncheckedsliceindex // index comes from range over regraded.
		if run.OriginalGraders != nil {
			continue
		}
		if original, ok := originals[run.Identity]; ok {
			run.OriginalGraders = original.Data
			run.OriginalEvidence = append(run.OriginalEvidence, original)
		}
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(struct {
		Format     string        `json:"format"`
		Version    int           `json:"version"`
		CLIVersion string        `json:"cli_version"`
		Runs       []RegradedRun `json:"runs"`
	}{"gh-aw-regrading", 1, GetVersion(), regraded})
}

func offlineGradingOriginals(directory string, runs []ProcessedRun) (map[string]RegradeOriginalEvidence, error) {
	originals := make(map[string]RegradeOriginalEvidence)
	path, err := filepath.Abs(filepath.Join(directory, "summary.json"))
	if err != nil {
		return nil, err
	}
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return originals, nil
	}
	data, err := regradeReadEvidence(path, maxGraderPayloadBytes)
	if err != nil {
		return nil, err
	}
	var document struct {
		Runs []struct {
			RunID      int64           `json:"run_id"`
			URL        string          `json:"url"`
			Repository string          `json:"repository"`
			RunAttempt string          `json:"run_attempt"`
			Graders    json.RawMessage `json:"graders"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	if len(document.Runs) != len(runs) {
		return nil, fmt.Errorf("logs summary contains %d runs but offline discovery produced %d; cannot safely correlate original grader evidence", len(document.Runs), len(runs))
	}
	uniqueRuns, err := offlineGradingUniqueRuns(runs)
	if err != nil {
		return nil, err
	}
	for index, entry := range document.Runs {
		if len(entry.Graders) == 0 || string(entry.Graders) == "null" {
			continue
		}
		summaryRun, err := offlineGradingRunDataMetadata(RunData{
			RunID: entry.RunID, URL: entry.URL, Repository: entry.Repository, RunAttempt: entry.RunAttempt,
		})
		if err != nil {
			return nil, err
		}
		identity, err := offlineGradingOriginalIdentity(summaryRun, uniqueRuns, entry.RunID)
		if err != nil {
			return nil, err
		}
		if previous, exists := originals[identity]; exists && regradeDigest(previous.Data) != regradeDigest(entry.Graders) {
			return nil, fmt.Errorf("run identity %s has conflicting recorded grader evidence", identity)
		}
		originals[identity] = RegradeOriginalEvidence{
			Path: path, Field: fmt.Sprintf("runs/%d/graders", index),
			Data: entry.Graders, Digest: regradeDigest(entry.Graders),
		}
	}
	return originals, nil
}

func offlineGradingOriginalIdentity(summaryRun WorkflowRun, runs []ProcessedRun, runID int64) (string, error) {
	var identity string
	matches := 0
	for _, run := range runs {
		if err := validateOfflineRunIdentity(summaryRun, run.Run); err == nil {
			identity = harborRunIdentity(run)
			matches++
		}
	}
	if matches != 1 {
		return "", fmt.Errorf("cannot safely correlate original grader evidence for summary run %d: matched %d downloaded runs", runID, matches)
	}
	return identity, nil
}

func offlineGradingUniqueRuns(runs []ProcessedRun) ([]ProcessedRun, error) {
	uniqueRuns := make([]ProcessedRun, 0, len(runs))
	seenRuns := make(map[string]string, len(runs))
	for _, run := range runs {
		identity := harborRunIdentity(run)
		if previousPath, exists := seenRuns[identity]; exists {
			if previousPath != run.Run.LogsPath {
				return nil, fmt.Errorf("run identity %s has conflicting evidence directories", identity)
			}
			continue
		}
		seenRuns[identity] = run.Run.LogsPath
		uniqueRuns = append(uniqueRuns, run)
	}
	return uniqueRuns, nil
}

// regradeCheckSourcePath rejects links and exported Harbor evidence, including
// selecting a trial's evidence subdirectory rather than the export root.
func regradeCheckSourcePath(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for current := absolute; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("regrading source must not contain symlinks: %s", current)
		}
		if err == nil && info.IsDir() {
			nativeHarbor, err := regradeNativeHarborBundle(current)
			if err != nil {
				return err
			}
			if nativeHarbor {
				return fmt.Errorf("cannot regrade native Harbor bundle evidence: %s", current)
			}
			for _, marker := range []string{"gh-aw-export.json", filepath.Join("gh-aw", "metadata.json")} {
				if _, err := os.Lstat(filepath.Join(current, marker)); err == nil {
					return fmt.Errorf("cannot regrade Harbor export evidence: %s", current)
				} else if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR) {
					return err
				}
			}
			metadataPath := filepath.Join(current, "metadata.json")
			if metadata, err := os.Lstat(metadataPath); err == nil {
				if !metadata.Mode().IsRegular() {
					return fmt.Errorf("regrading metadata must be a regular file: %s", metadataPath)
				}
				if metadata.Size() <= 1024*1024 {
					data, err := os.ReadFile(metadataPath)
					if err != nil {
						return err
					}
					var marker struct {
						Format string `json:"format"`
					}
					if json.Unmarshal(data, &marker) == nil && strings.HasPrefix(marker.Format, "gh-aw-harbor") {
						return fmt.Errorf("cannot regrade Harbor export evidence: %s", current)
					}
				}
			} else if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		if parent := filepath.Dir(current); parent == current {
			break
		}
	}
	return nil
}

func loadOfflineGradingRuns(directory string) ([]ProcessedRun, error) {
	root, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	if err := regradeCheckSourcePath(root); err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("--logs-dir must select a downloaded logs directory")
	}
	summaryPath := filepath.Join(root, "summary.json")
	if _, err := os.Lstat(summaryPath); err == nil {
		return offlineGradingSummaryRuns(root, summaryPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return discoverOfflineGradingRuns(root)
}

func discoverOfflineGradingRuns(root string) ([]ProcessedRun, error) {
	runs := make([]ProcessedRun, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		return discoverOfflineGradingDirectory(path, entry, &runs)
	})
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, errors.New("no downloaded workflow runs found in --logs-dir")
	}
	return runs, nil
}

func discoverOfflineGradingDirectory(path string, entry fs.DirEntry, runs *[]ProcessedRun) error {
	if entry.Type()&os.ModeSymlink != 0 {
		return fmt.Errorf("offline logs collection contains a symbolic link: %s", path)
	}
	if !entry.IsDir() {
		return nil
	}
	if strings.EqualFold(entry.Name(), "harbor") || strings.HasPrefix(entry.Name(), ".harbor-stage-") ||
		strings.HasPrefix(entry.Name(), ".harbor-backup-") {
		return filepath.SkipDir
	}
	id, kind, artifact := strings.Cut(entry.Name(), "-")
	if _, validID := parseEpisodeRunID(id); artifact && validID && validEpisodeArtifactKind(kind) {
		return filepath.SkipDir
	}
	if err := regradeCheckSourcePath(path); err != nil {
		return err
	}
	runID := offlineGradingDirectoryID(entry.Name())
	if runID > 0 {
		attempts, err := offlineGradingHasAttempts(path)
		if err != nil || attempts {
			return err
		}
	}
	if _, err := os.Lstat(filepath.Join(path, ".episode-artifacts.json")); err == nil && strings.HasPrefix(entry.Name(), "collection-") {
		run, err := offlineGradingEpisodeRun(path)
		if err != nil {
			return err
		}
		for _, existing := range *runs {
			if harborRunIdentity(existing) == harborRunIdentity(run) {
				return fmt.Errorf("duplicate episode collection identity %s in %s and %s",
					harborRunIdentity(run), existing.Run.LogsPath, run.Run.LogsPath)
			}
		}
		*runs = append(*runs, run)
		return filepath.SkipDir
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_, summaryErr := os.Lstat(filepath.Join(path, runSummaryFileName))
	_, metadataErr := os.Lstat(filepath.Join(path, runAPIResponseFileName))
	if runID == 0 && errors.Is(summaryErr, os.ErrNotExist) && errors.Is(metadataErr, os.ErrNotExist) {
		return nil
	}
	run, err := offlineGradingRun(path, runID)
	if err != nil {
		return err
	}
	*runs = append(*runs, run)
	return filepath.SkipDir
}

func offlineGradingDirectoryID(name string) int64 {
	if !strings.HasPrefix(name, "run-") {
		return 0
	}
	value, _, _ := strings.Cut(strings.TrimPrefix(name, "run-"), "-")
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

func offlineGradingRun(directory string, fallbackID int64) (ProcessedRun, error) {
	run, err := offlineGradingRawRun(directory)
	if err != nil {
		return ProcessedRun{}, err
	}
	var awContext *AwContext
	summaryPath := filepath.Join(directory, runSummaryFileName)
	if _, err := os.Lstat(summaryPath); err == nil {
		data, err := regradeReadEvidence(summaryPath, maxGraderPayloadBytes)
		if err != nil {
			return ProcessedRun{}, err
		}
		var summary RunSummary
		if err := json.Unmarshal(data, &summary); err != nil {
			return ProcessedRun{}, fmt.Errorf("invalid run summary %s: %w", summaryPath, err)
		}
		summaryID := summary.Run.DatabaseID
		if summaryID == 0 {
			summaryID = summary.RunID
		}
		if run.DatabaseID > 0 && summaryID > 0 && run.DatabaseID != summaryID {
			return ProcessedRun{}, fmt.Errorf("run summary and raw Actions metadata disagree on run identity in %s", directory)
		}
		if err := validateOfflineRunIdentity(run, summary.Run); err != nil {
			return ProcessedRun{}, err
		}
		applyWorkflowRunMetadata(&run, summary.Run)
		awContext = summary.AwContext
		if run.DatabaseID == 0 {
			run.DatabaseID = summary.RunID
		}
		if run.DatabaseID == 0 {
			run.DatabaseID = fallbackID
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return ProcessedRun{}, err
	}
	if run.DatabaseID == 0 {
		run.DatabaseID = fallbackID
	}
	if run.DatabaseID <= 0 {
		return ProcessedRun{}, fmt.Errorf("downloaded run %s has no positive run identity", directory)
	}
	if fallbackID > 0 && run.DatabaseID != fallbackID {
		return ProcessedRun{}, fmt.Errorf("downloaded run identity %d does not match selected run %d", run.DatabaseID, fallbackID)
	}
	if err := validateOfflineRunIdentity(run, run); err != nil {
		return ProcessedRun{}, err
	}
	run.LogsPath = directory
	return ProcessedRun{Run: run, AwContext: awContext}, nil
}

func offlineGradingSummaryRuns(root, summaryPath string) ([]ProcessedRun, error) {
	data, err := regradeReadEvidence(summaryPath, maxGraderPayloadBytes)
	if err != nil {
		return nil, err
	}
	var summary struct {
		Runs         []RunData `json:"runs"`
		LogsLocation string    `json:"logs_location"`
	}
	if err := json.Unmarshal(data, &summary); err != nil {
		return nil, fmt.Errorf("invalid logs collection %s: %w", summaryPath, err)
	}
	if len(summary.Runs) == 0 {
		return nil, fmt.Errorf("logs collection %s contains no runs", summaryPath)
	}
	runs := make([]ProcessedRun, 0, len(summary.Runs))
	for _, entry := range summary.Runs {
		if entry.RunID <= 0 {
			return nil, fmt.Errorf("logs collection %s contains a run without a positive identity", summaryPath)
		}
		source, err := offlineGradingSummarySource(root, summary.LogsLocation, entry.LogsPath, entry.RunID)
		if err != nil {
			return nil, err
		}
		run, err := offlineGradingSelectedRun(source, entry.RunID)
		if err != nil {
			return nil, err
		}
		if run.AwContext == nil {
			run.AwContext = entry.AwContext
		}
		metadata, err := offlineGradingRunDataMetadata(entry)
		if err != nil {
			return nil, err
		}
		if err := validateOfflineRunIdentity(metadata, run.Run); err != nil {
			return nil, err
		}
		applyWorkflowRunMetadata(&metadata, run.Run)
		metadata.LogsPath = source
		run.Run = metadata
		runs = append(runs, run)
	}
	return runs, nil
}

func offlineGradingSummarySource(root, logsLocation, recorded string, id int64) (string, error) {
	if recorded != "" {
		candidate, err := filepath.Abs(recorded)
		if err != nil {
			return "", err
		}
		relative, err := filepath.Rel(root, candidate)
		if err != nil {
			return "", err
		}
		if relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			if _, err := os.Lstat(candidate); err == nil {
				return candidate, regradeCheckSourcePath(candidate)
			} else if !errors.Is(err, os.ErrNotExist) {
				return "", err
			}
		}
		if !filepath.IsAbs(recorded) {
			relative = filepath.Clean(recorded)
			if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return "", fmt.Errorf("logs collection source escapes --logs-dir: %s", recorded)
			}
			candidate = filepath.Join(root, relative)
			if _, err := os.Lstat(candidate); err == nil {
				return candidate, regradeCheckSourcePath(candidate)
			} else if !errors.Is(err, os.ErrNotExist) {
				return "", err
			}
		}
		if logsLocation != "" {
			oldRoot, err := filepath.Abs(logsLocation)
			if err != nil {
				return "", err
			}
			oldPath := recorded
			if !filepath.IsAbs(recorded) {
				oldPath = filepath.Join(oldRoot, recorded)
			}
			relative, err := filepath.Rel(oldRoot, oldPath)
			if err == nil && relative != ".." &&
				!strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative) {
				candidate = filepath.Join(root, relative)
				if _, err := os.Lstat(candidate); err == nil {
					return candidate, regradeCheckSourcePath(candidate)
				} else if !errors.Is(err, os.ErrNotExist) {
					return "", err
				}
			}
		}
	}
	return filepath.Join(root, fmt.Sprintf("run-%d", id)), nil
}

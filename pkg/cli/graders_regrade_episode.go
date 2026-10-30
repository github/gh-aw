package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

func regradeEvidenceRoots(source string) ([]string, error) {
	roots := []string{source}
	inventory := filepath.Join(source, ".episode-artifacts.json")
	if _, err := os.Lstat(inventory); errors.Is(err, os.ErrNotExist) {
		return roots, nil
	}
	if _, err := regradeReadEvidence(inventory, maxGraderPayloadBytes); err != nil {
		return nil, err
	}
	dirs, _, err := readEpisodeArtifactDirs(source)
	if err != nil {
		return nil, err
	}
	for _, directory := range dirs {
		if err := regradeCheckSourcePath(directory); err != nil {
			return nil, err
		}
		roots = append(roots, directory)
	}
	return roots, nil
}

func findRegradeSessionPath(source string) (string, error) {
	roots, err := regradeEvidenceRoots(source)
	if err != nil {
		return "", err
	}
	for _, root := range roots {
		path, err := findUnifiedSessionPath(root)
		if err != nil || path != "" {
			return path, err
		}
		if usage := findArtifactDir(root, constants.UsageArtifactName.String(), ""); usage != "" {
			path, err = findUnifiedSessionPath(usage)
			if err != nil || path != "" {
				return path, err
			}
		}
	}
	return "", nil
}

func regradeFindOriginalFile(roots []string, name string) string {
	for _, root := range roots {
		if path := findGraderFile(root, name); path != "" {
			return path
		}
	}
	return ""
}

func offlineGradingHasAttempts(directory string) (bool, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "attempt-") {
			attempt, err := strconv.Atoi(strings.TrimPrefix(entry.Name(), "attempt-"))
			if err == nil && attempt > 0 {
				return true, nil
			}
		}
	}
	return false, nil
}

func offlineGradingSelectedRun(source string, fallbackID int64) (ProcessedRun, error) {
	if _, err := os.Lstat(filepath.Join(source, ".episode-artifacts.json")); err == nil && strings.HasPrefix(filepath.Base(source), "collection-") {
		return offlineGradingEpisodeRun(source)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return ProcessedRun{}, err
	}
	return offlineGradingRun(source, fallbackID)
}

func offlineGradingEpisodeRun(source string) (ProcessedRun, error) {
	if _, err := regradeEvidenceRoots(source); err != nil {
		return ProcessedRun{}, err
	}
	attemptDir := filepath.Dir(source)
	attempt, err := strconv.Atoi(strings.TrimPrefix(filepath.Base(attemptDir), "attempt-"))
	runID := offlineGradingDirectoryID(filepath.Base(filepath.Dir(attemptDir)))
	if err != nil || attempt <= 0 || runID <= 0 {
		return ProcessedRun{}, fmt.Errorf("episode collection %s has no valid run/attempt directory identity", source)
	}
	data, err := regradeReadEvidence(filepath.Join(source, "aw_info.json"), maxGraderPayloadBytes)
	if err != nil {
		return ProcessedRun{}, err
	}
	var info AwInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return ProcessedRun{}, fmt.Errorf("invalid episode collection metadata: %w", err)
	}
	if info.RunID != 0 && int64(info.RunID) != runID || info.RunAttempt != "" && info.RunAttempt != strconv.Itoa(attempt) {
		return ProcessedRun{}, fmt.Errorf("episode collection %s metadata conflicts with its run/attempt directory identity", source)
	}
	metadata := WorkflowRun{DatabaseID: runID, Attempt: attempt, Repository: info.Repository,
		WorkflowName: info.WorkflowName, HeadSha: info.SHA, Event: info.EventName, Actor: info.Actor}
	if err := offlineGradingEpisodeRepository(attemptDir, &metadata); err != nil {
		return ProcessedRun{}, err
	}
	processed, err := offlineGradingRun(source, runID)
	if err != nil {
		return ProcessedRun{}, err
	}
	if err := validateOfflineRunIdentity(metadata, processed.Run); err != nil {
		return ProcessedRun{}, err
	}
	applyWorkflowRunMetadata(&metadata, processed.Run)
	metadata.LogsPath = source
	return ProcessedRun{Run: metadata, AwContext: info.Context}, nil
}

func offlineGradingEpisodeRepository(attemptDir string, run *WorkflowRun) error {
	repoDir := filepath.Dir(filepath.Dir(attemptDir))
	ownerDir := filepath.Dir(repoDir)
	hostDir := filepath.Dir(ownerDir)
	if filepath.Base(filepath.Dir(hostDir)) != "episodes" {
		return nil
	}
	repository := path.Join(filepath.Base(ownerDir), filepath.Base(repoDir))
	if run.Repository != "" && !strings.EqualFold(run.Repository, repository) {
		return fmt.Errorf("episode cache path conflicts with recorded repository %q", run.Repository)
	}
	run.Repository = repository
	run.URL = (&url.URL{Scheme: "https", Host: filepath.Base(hostDir),
		Path: fmt.Sprintf("/%s/actions/runs/%d", repository, run.DatabaseID)}).String()
	return nil
}

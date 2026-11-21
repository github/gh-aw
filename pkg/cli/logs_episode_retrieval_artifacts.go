package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/errorutil"
)

const maxEpisodeArtifactBytes = 64 * 1024 * 1024

var errEpisodeArtifactExpired = errors.New("compact episode artifact has expired")
var errEpisodeArtifactPriorAttempt = errors.New("artifact metadata belongs to a different run attempt")

func (c *episodeCollector) loadRun(ctx context.Context, run WorkflowRun) (ProcessedRun, error) {
	artifacts, err := c.listArtifacts(ctx, run)
	if err != nil {
		return ProcessedRun{}, err
	}
	runDir := filepath.Join(c.opts.OutputDir, "episodes", c.report.Host, strings.ToLower(run.Repository),
		fmt.Sprintf("run-%d", run.DatabaseID), fmt.Sprintf("attempt-%d", run.Attempt))
	if err := os.MkdirAll(runDir, constants.DirPermSensitive); err != nil {
		return ProcessedRun{}, fmt.Errorf("create episode run directory: %w", err)
	}
	infos, dirs, err := c.loadCompactArtifacts(ctx, run, artifacts, runDir)
	if err != nil {
		return ProcessedRun{}, err
	}
	info, err := selectEpisodeInfo(infos)
	if err != nil {
		return ProcessedRun{}, err
	}
	collectionDir := filepath.Join(runDir, "collection-"+rand.Text())
	if err := os.Mkdir(collectionDir, constants.DirPermSensitive); err != nil {
		return ProcessedRun{}, fmt.Errorf("create episode collection directory: %w", err)
	}
	encoded, err := json.Marshal(info)
	if err != nil {
		return ProcessedRun{}, fmt.Errorf("encode episode run metadata: %w", err)
	}
	if err := writeSensitiveFile(filepath.Join(collectionDir, "aw_info.json"), encoded); err != nil {
		return ProcessedRun{}, fmt.Errorf("save episode run metadata: %w", err)
	}
	inventory := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		inventory = append(inventory, filepath.Join("..", filepath.Base(dir)))
	}
	encoded, err = json.Marshal(inventory)
	if err != nil {
		return ProcessedRun{}, fmt.Errorf("encode episode artifact inventory: %w", err)
	}
	if err := writeSensitiveFile(filepath.Join(collectionDir, ".episode-artifacts.json"), encoded); err != nil {
		return ProcessedRun{}, fmt.Errorf("save episode artifact inventory: %w", err)
	}
	processed, err := processEpisodeArtifacts(run, collectionDir, info)
	if err == nil {
		for _, artifactInfo := range infos {
			if artifactInfo.Context != nil && strings.TrimSpace(artifactInfo.Context.EpisodeID) == strings.TrimSpace(info.Context.EpisodeID) {
				processed.EpisodeContexts = append(processed.EpisodeContexts, artifactInfo)
			}
		}
	}
	if err == nil && (c.report.EpisodeID == "" || c.report.EpisodeID == strings.TrimSpace(info.Context.EpisodeID)) {
		candidate := episodeCandidate{repository: strings.ToLower(run.Repository), runID: run.DatabaseID}
		for _, artifactInfo := range infos {
			if artifactInfo.Context != nil {
				c.enqueueRecorded(artifactInfo.Context.Repo, artifactInfo.Context.RunID, candidate)
				c.enqueueRecorded(artifactInfo.Context.RootRepo, artifactInfo.Context.RootRunID, candidate)
			}
		}
	}
	return processed, err
}

func completedEpisodeArtifact(dir string) (bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, ".episode-complete"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read episode artifact cache marker: %w", err)
	}
	return string(data) == "v1\n", nil
}

func (c *episodeCollector) loadArtifact(ctx context.Context, run WorkflowRun, artifact episodeArtifact, runDir string) (string, error) {
	// Artifact IDs are immutable. Names and URLs from the response never select
	// a local destination or remote endpoint.
	dir := filepath.Join(runDir, strconv.FormatInt(artifact.ID, 10)+"-"+episodeArtifactKind(artifact.Name))
	if complete, err := completedEpisodeArtifact(dir); err != nil {
		return "", err
	} else if complete {
		if err := writeSensitiveFile(filepath.Join(dir, ".episode-artifact-name"), []byte(artifact.Name)); err != nil {
			return "", err
		}
		return dir, nil
	}
	if artifact.Expired {
		return "", fmt.Errorf("%w: %q", errEpisodeArtifactExpired, artifact.Name)
	}
	if artifact.SizeInBytes > maxEpisodeArtifactBytes {
		return "", fmt.Errorf("compact artifact %q exceeds the 64 MiB archive limit", artifact.Name)
	}
	endpoint := fmt.Sprintf("repos/%s/actions/artifacts/%d/zip", run.Repository, artifact.ID)
	data, err := c.api(ctx, endpoint)
	if err != nil {
		return "", err
	}
	stage := filepath.Join(filepath.Dir(runDir), ".episode-artifact-"+rand.Text())
	if err := os.Mkdir(stage, constants.DirPermSensitive); err != nil {
		return "", fmt.Errorf("stage episode artifact: %w", err)
	}
	defer os.RemoveAll(stage)
	extracted := filepath.Join(stage, "extracted")
	if err := os.Mkdir(extracted, constants.DirPermSensitive); err != nil {
		return "", err
	}
	if err := extractEpisodeArtifact(ctx, data, extracted); err != nil {
		return "", fmt.Errorf("extract compact episode artifact: %w", err)
	}
	if err := writeSensitiveFile(filepath.Join(extracted, ".episode-artifact-name"), []byte(artifact.Name)); err != nil {
		return "", err
	}
	if err := writeSensitiveFile(filepath.Join(extracted, ".episode-complete"), []byte("v1\n")); err != nil {
		return "", err
	}
	if err := os.Rename(extracted, dir); err != nil {
		if complete, markerErr := completedEpisodeArtifact(dir); markerErr != nil {
			return "", markerErr
		} else if complete {
			return dir, nil
		}
		return "", fmt.Errorf("publish episode artifact cache: %w", err)
	}
	return dir, nil
}

func compareEpisodeArtifacts(a, b episodeArtifact) int {
	if aOutcome, bOutcome := episodeArtifactNeedsIdentity(a.Name), episodeArtifactNeedsIdentity(b.Name); aOutcome != bOutcome {
		if aOutcome {
			return 1
		}
		return -1
	}
	// Prefer info over usage when both contain a copy of aw_info.json.
	if kind := strings.Compare(episodeArtifactKind(a.Name), episodeArtifactKind(b.Name)); kind != 0 {
		return kind
	}
	return strings.Compare(a.Name, b.Name)
}

func (c *episodeCollector) loadCompactArtifacts(ctx context.Context, run WorkflowRun, artifacts []episodeArtifact, runDir string) ([]*AwInfo, []string, error) {
	slices.SortStableFunc(artifacts, compareEpisodeArtifacts)
	var infos []*AwInfo
	var dirs []string
	var priorAttempt bool
	for _, artifact := range artifacts {
		if episodeArtifactNeedsIdentity(artifact.Name) {
			info, err := selectEpisodeInfo(infos)
			if err != nil {
				continue
			}
			if c.report.EpisodeID != "" && strings.TrimSpace(info.Context.EpisodeID) != c.report.EpisodeID {
				continue
			}
			if run.Attempt > 1 && (run.StartedAt.IsZero() || artifact.CreatedAt.IsZero() || artifact.CreatedAt.Before(run.StartedAt)) {
				c.issue(episodeCandidate{repository: run.Repository, runID: run.DatabaseID}, "outcome_attempt_unverified",
					errors.New("safe-output manifest cannot be verified as belonging to the current run attempt"))
				continue
			}
		}
		dir, err := c.loadArtifact(ctx, run, artifact, runDir)
		if err != nil {
			if errors.Is(err, errEpisodeArtifactExpired) {
				c.issue(episodeCandidate{repository: run.Repository, runID: run.DatabaseID}, "expired_artifact", err)
				continue
			}
			if _, identityErr := selectEpisodeInfo(infos); identityErr == nil && c.handleStop(err) {
				break // Retain verified membership; the explicit stop qualifies incomplete usage.
			}
			if episodeArtifactKind(artifact.Name) != "info" && episodeArtifactUnavailable(err) {
				c.issue(episodeCandidate{repository: run.Repository, runID: run.DatabaseID}, "missing_artifact", err)
				continue
			}
			return nil, nil, err
		}
		if episodeArtifactNeedsIdentity(artifact.Name) {
			dirs = append(dirs, dir)
			continue
		}
		artifactInfos, err := readEpisodeArtifactInfo(dir, run)
		if err != nil {
			if errors.Is(err, errEpisodeArtifactPriorAttempt) {
				priorAttempt = true
				continue
			}
			return nil, nil, err
		}
		infos = append(infos, artifactInfos...)
		dirs = append(dirs, dir)
	}
	if len(infos) == 0 && priorAttempt {
		return nil, nil, fmt.Errorf("no current-attempt canonical identity: %w", errEpisodeArtifactPriorAttempt)
	}
	return infos, dirs, nil
}

func episodeArtifactUnavailable(err error) bool {
	return errorutil.IsNotFoundError(err) || errorutil.IsGoneError(err)
}

// Enrichment artifacts mirror accounting already present in usage. Keep them
// out of existing accounting readers while outcome readers use the full inventory.
func readEpisodeArtifactDirs(runDir string) ([]string, bool, error) {
	dirs, found, err := readEpisodeAllArtifactDirs(runDir)
	dirs = slices.DeleteFunc(dirs, func(dir string) bool {
		return strings.HasSuffix(filepath.Base(dir), "-enrichment")
	})
	return dirs, found, err
}

// readEpisodeAllArtifactDirs returns the immutable, validated inventory for one
// collection. Older collections keep their own inventory and cached evidence.
func readEpisodeAllArtifactDirs(runDir string) ([]string, bool, error) {
	id, kind, artifactDir := strings.Cut(filepath.Base(filepath.Clean(runDir)), "-")
	if _, validID := parseEpisodeRunID(id); artifactDir && validID && validEpisodeArtifactKind(kind) {
		return nil, false, nil // An archive cannot supply its own trusted collection inventory.
	}
	data, err := os.ReadFile(filepath.Join(runDir, ".episode-artifacts.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, fmt.Errorf("read episode artifact inventory: %w", err)
	}
	var inventory []string
	if err := json.Unmarshal(data, &inventory); err != nil {
		return nil, true, fmt.Errorf("decode episode artifact inventory: %w", err)
	}
	dirs := make([]string, 0, len(inventory))
	for _, relative := range inventory {
		name := filepath.Base(relative)
		id, kind, valid := strings.Cut(name, "-")
		if _, validID := parseEpisodeRunID(id); !valid || !validID || !validEpisodeArtifactKind(kind) || relative != filepath.Join("..", name) {
			return nil, true, fmt.Errorf("invalid episode artifact inventory entry %q", relative)
		}
		dir := filepath.Join(runDir, relative)
		if complete, err := completedEpisodeArtifact(dir); err != nil {
			return nil, true, err
		} else if !complete {
			return nil, true, fmt.Errorf("episode artifact inventory entry %q is incomplete", relative)
		}
		dirs = append(dirs, dir)
	}
	return dirs, true, nil
}

func extractEpisodeArtifact(ctx context.Context, data []byte, dir string) error {
	if len(data) > maxEpisodeArtifactBytes {
		return errors.New("compact artifact exceeds the 64 MiB archive limit")
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("open compact artifact archive: %w", err)
	}
	var total uint64
	for _, file := range reader.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		if file.UncompressedSize64 > maxEpisodeArtifactBytes-total {
			return errors.New("compact artifact exceeds the 64 MiB extracted-size limit")
		}
		total += file.UncompressedSize64
		if err := extractZipFile(file, dir, false); err != nil {
			return err
		}
	}
	return nil
}

func episodeArtifactKind(name string) string {
	if artifactMatchesFilter(name, []string{"safe-outputs-items"}) {
		return "adoption"
	}
	if artifactMatchesFilter(name, []string{"info"}) {
		return "info"
	}
	if artifactMatchesFilter(name, []string{"agent-output-fallback"}) {
		return "enrichment"
	}
	return "usage"
}

func validEpisodeArtifactKind(kind string) bool {
	return kind == "info" || kind == "usage" || kind == "adoption" || kind == "enrichment"
}

func episodeArtifactNeedsIdentity(name string) bool {
	kind := episodeArtifactKind(name)
	return kind == "adoption" || kind == "enrichment"
}

func readEpisodeArtifactInfo(dir string, run WorkflowRun) ([]*AwInfo, error) {
	var infos []*AwInfo
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() != "aw_info.json" {
			return nil
		}
		info, err := parseAwInfo(path, false)
		if err != nil {
			return fmt.Errorf("read episode run context: %w", err)
		}
		if info.RunAttempt != "" && info.RunAttempt != strconv.Itoa(run.Attempt) {
			return errEpisodeArtifactPriorAttempt
		}
		if info.RunID != 0 && int64(info.RunID) != run.DatabaseID || episodeRepositoryConflict(info.Repository, run.Repository) {
			return errors.New("artifact metadata conflicts with the requested repository/run identity")
		}
		infos = append(infos, info)
		return nil
	})
	return infos, err
}

func selectEpisodeInfo(infos []*AwInfo) (*AwInfo, error) {
	var selected *AwInfo
	for _, info := range infos {
		if info.Context == nil || strings.TrimSpace(info.Context.EpisodeID) == "" {
			continue
		}
		if selected != nil && strings.TrimSpace(selected.Context.EpisodeID) != strings.TrimSpace(info.Context.EpisodeID) {
			return nil, errors.New("compact artifacts contain conflicting canonical episode identities")
		}
		if selected == nil {
			selected = info
		}
	}
	if selected == nil {
		return nil, errors.New("compact artifacts have no recorded context.episode_id")
	}
	return selected, nil
}

func processEpisodeArtifacts(run WorkflowRun, runDir string, info *AwInfo) (ProcessedRun, error) {
	result := DownloadResult{Run: run}
	result.Run.LogsPath = runDir
	metrics := extractRunMetricsAndMetadata(&result, runDir, false)
	usage, err := analyzeTokenUsage(runDir, false)
	if err != nil {
		return ProcessedRun{}, fmt.Errorf("read compact episode usage: %w", err)
	}
	activity, err := loadUsageActivitySummary(runDir)
	if err != nil {
		return ProcessedRun{}, fmt.Errorf("read compact episode activity: %w", err)
	}
	result.TokenUsage = usage
	backfillRunTokenUsageFromFirewall(&metrics, &result, usage)
	applyUsageActivitySummaryToResult(activity, &result, true)
	return ProcessedRun{
		Run: result.Run, AwContext: info.Context, AccessAnalysis: result.AccessAnalysis,
		FirewallAnalysis: result.FirewallAnalysis, RedactedDomainsAnalysis: result.RedactedDomainsAnalysis,
		MissingTools: result.MissingTools, MissingData: result.MissingData, Noops: result.Noops,
		ContinuationRequests: result.ContinuationRequests,
		MCPFailures:          result.MCPFailures, SkillActivations: result.SkillActivations,
		GatewaySteeringEvents: result.GatewaySteeringEvents, MCPToolUsage: result.MCPToolUsage,
		TokenUsage: result.TokenUsage, ModelRouting: analyzeModelRouting(runDir),
		WorkingSet: result.WorkingSet, Ledger: result.Ledger, WorkQueue: result.WorkQueue,
		Friction: result.Friction, GitHubRateLimitUsage: result.GitHubRateLimitUsage,
		SafeOutputs: result.SafeOutputs,
	}, nil
}

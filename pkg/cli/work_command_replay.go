package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/github/gh-aw/pkg/workqueue"
	"github.com/spf13/cobra"
)

const workQueueReplayArtifactBytes = 162 << 20

type workQueueReplayRun struct {
	ID         int64  `json:"id"`
	RunAttempt int    `json:"run_attempt"`
	Path       string `json:"path"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

type workQueueReplayArtifact struct {
	ID          int64 `json:"id"`
	SizeInBytes int64 `json:"size_in_bytes"`
}

type workQueueReplayPrediction struct {
	Reason     string   `json:"reason"`
	WorkIDs    []string `json:"work_ids"`
	Dispatches []string `json:"dispatches"`
}

func workReplayArtifact(cmd *cobra.Command, runID int64, file string) error {
	if (cmd.Flags().Changed("run-id") && cmd.Flags().Changed("file")) ||
		(cmd.Flags().Changed("run-id") && runID <= 0) ||
		(cmd.Flags().Changed("file") && file == "") {
		return errors.New("artifact_replay_invalid: choose one snapshot source: a positive Actions run ID or a downloaded snapshot file. Example: replay --run-id 38645334879, or replay --file /path/to/work-queue.snapshot.json")
	}
	if cmd.Flags().Changed("branch") {
		return errors.New("artifact_replay_invalid: artifact replay is read-only and cannot use --branch. Omit --run-id and --file to replay the live ledger instead")
	}
	pool, _ := cmd.Flags().GetString("pool")
	if pool == "" && (cmd.Flags().Changed("pool") || cmd.Flags().Changed("max-claims") || cmd.Flags().Changed("max-dispatches") || cmd.Flags().Changed("concurrency")) {
		return errors.New("artifact_replay_invalid: a scheduling prediction requires a pool name. Example: replay --run-id 38645334879 --pool default --max-claims 3 --max-dispatches 3 --concurrency 3")
	}
	repository, _ := cmd.Flags().GetString("repo")
	var data []byte
	var run *workQueueReplayRun
	var err error
	if file != "" {
		data, err = readBoundedWorkQueueArtifact(file, workQueueReplayArtifactBytes)
	} else {
		if repository == "" {
			repository, err = getWorkQueueRepoSlug()
			if err != nil {
				return err
			}
		}
		data, run, err = downloadWorkQueueReplaySnapshot(cmd.Context(), repository, runID)
	}
	if err != nil {
		return err
	}
	envelope, state, err := replayCurrentWorkQueueSnapshot(data)
	if err != nil {
		return fmt.Errorf("artifact replay could not validate the captured ledger; use the original work-queue.snapshot.json from the activation artifact without editing it: %w", err)
	}
	if envelope.Worker != nil {
		if _, err := currentWorkQueueAssignmentReceipt(state, *envelope.Worker, envelope.Origin); err != nil {
			return err
		}
	}
	if repository != "" && !strings.EqualFold(envelope.Origin.Repository, repository) {
		return errors.New("artifact_replay_invalid: the snapshot belongs to a different repository than --repo. Use the repository that produced the artifact; do not edit its origin")
	}
	if run != nil {
		workflow, _, _ := strings.Cut(run.Path, "@")
		if envelope.Origin.RunID != strconv.FormatInt(run.ID, 10) ||
			envelope.Origin.RunAttempt != run.RunAttempt || envelope.Origin.Workflow != workflow {
			return errors.New("artifact_replay_invalid: the snapshot's run, attempt, or workflow differs from the requested Actions run. Use the original matching activation artifact; do not edit its origin")
		}
	}
	return printWorkQueueArtifactReplay(cmd, envelope, state)
}

// workQueuePredictionParameters routes every worker profile that pool Work names;
// the compiled routes and concurrency are not part of the ledger.
func workQueuePredictionParameters(cmd *cobra.Command, state workqueue.Projection, pool string) workqueue.DispatchParameters {
	maxClaims, _ := cmd.Flags().GetInt("max-claims")
	maxDispatches, _ := cmd.Flags().GetInt("max-dispatches")
	concurrency, _ := cmd.Flags().GetInt("concurrency")
	profiles := map[string]workqueue.DispatchProfile{}
	for _, work := range state.Works {
		if work.Pool == pool {
			profiles[work.WorkerProfile] = workqueue.DispatchProfile{Workflow: logsWorkflowsPathSegment + work.WorkerProfile + ".lock.yml"}
		}
	}
	return workqueue.DispatchParameters{Pool: pool, MaxClaims: maxClaims, MaxDispatches: maxDispatches, Concurrency: concurrency, Profiles: profiles}
}

func printWorkQueueArtifactReplay(cmd *cobra.Command, envelope currentWorkQueueEnvelope, state workqueue.Projection) error {
	result := struct {
		Status     string                     `json:"status"`
		Repository string                     `json:"repository"`
		RunID      string                     `json:"run_id"`
		Role       string                     `json:"role"`
		SHA        *string                    `json:"sha"`
		Tip        string                     `json:"tip"`
		Stats      workqueue.Stats            `json:"stats"`
		Prediction *workQueueReplayPrediction `json:"prediction,omitempty"`
	}{
		Status: "artifact_replay", Repository: envelope.Origin.Repository, RunID: envelope.Origin.RunID,
		Role: envelope.Role, SHA: envelope.SHA, Tip: state.Tip, Stats: state.Stats,
	}
	human := fmt.Sprintf("Artifact replay: %d transactions, %d Work, %d Claims; no writes or launches", state.Stats.Transactions, state.Stats.Work, state.Stats.Claims)
	if pool, _ := cmd.Flags().GetString("pool"); pool != "" {
		decision, err := workqueue.PlanDispatch(state, workQueuePredictionParameters(cmd, state, pool), "request:artifact_prediction")
		if err != nil {
			return fmt.Errorf("artifact replay could not predict scheduling; use positive budgets (max-claims/max-dispatches 1..256, concurrency 1..4096): %w", currentWorkQueueDiagnosticError("prediction", err))
		}
		prediction := workQueueReplayPrediction{Reason: decision.Reason, WorkIDs: []string{}, Dispatches: decision.Dispatches}
		for _, operation := range decision.Operations {
			prediction.WorkIDs = append(prediction.WorkIDs, operation.WorkID)
		}
		result.Prediction = &prediction
		human += fmt.Sprintf("\nCaptured scheduling prediction: %s, %d dispatches; no live dependency refresh or launch",
			decision.Reason, len(decision.Dispatches))
	}
	return workPrint(cmd, result, human)
}

func downloadWorkQueueReplaySnapshot(ctx context.Context, repository string, runID int64) ([]byte, *workQueueReplayRun, error) {
	if !isValidOwnerRepoSlug(repository) {
		return nil, nil, errors.New("artifact_replay_invalid: --repo should identify the repository containing the Actions run. Example: --repo github/gh-aw")
	}
	client, err := api.NewRESTClient(api.ClientOptions{Timeout: 60 * time.Second})
	if err != nil {
		return nil, nil, err
	}
	base := fmt.Sprintf("repos/%s/actions/runs/%d", repository, runID)
	raw, err := readWorkQueueReplayAPI(ctx, client, base, 1<<20)
	if err != nil {
		return nil, nil, err
	}
	var run workQueueReplayRun
	if json.Unmarshal(raw, &run) != nil || run.ID != runID || run.RunAttempt < 1 ||
		!strings.EqualFold(run.Repository.FullName, repository) || run.Path == "" {
		return nil, nil, errors.New("artifact_replay_invalid: GitHub's run metadata does not match the requested repository and run ID. Verify --repo and --run-id against the Actions run URL")
	}
	candidates, err := listWorkQueueReplayArtifacts(ctx, client, base)
	if err != nil {
		return nil, nil, err
	}
	data, err := selectWorkQueueReplaySnapshot(ctx, client, repository, &run, candidates)
	return data, &run, err
}

func listWorkQueueReplayArtifacts(ctx context.Context, client *api.RESTClient, base string) ([]workQueueReplayArtifact, error) {
	var candidates []workQueueReplayArtifact
	seen := make(map[int64]bool)
	for page := 1; page <= 10; page++ {
		raw, err := readWorkQueueReplayAPI(ctx, client, fmt.Sprintf("%s/artifacts?per_page=100&page=%d", base, page), 1<<20)
		if err != nil {
			return nil, err
		}
		var listing struct {
			Artifacts []struct {
				ID          int64  `json:"id"`
				Name        string `json:"name"`
				Expired     bool   `json:"expired"`
				SizeInBytes int64  `json:"size_in_bytes"`
			} `json:"artifacts"`
		}
		if json.Unmarshal(raw, &listing) != nil || listing.Artifacts == nil {
			return nil, errors.New("artifact_replay_invalid: GitHub returned an unreadable artifact inventory. Retry the read-only replay after verifying the API host and connectivity")
		}
		for _, artifact := range listing.Artifacts {
			if artifact.Name != "activation" {
				continue
			}
			if artifact.Expired {
				continue
			}
			if seen[artifact.ID] || artifact.ID <= 0 ||
				artifact.SizeInBytes <= 0 || artifact.SizeInBytes > workQueueReplayArtifactBytes {
				return nil, errors.New("artifact_replay_invalid: an activation artifact ID is duplicated or invalid, or its archive is outside the 162 MiB size limit. Use --file with the original downloaded snapshot")
			}
			seen[artifact.ID] = true
			candidates = append(candidates, workQueueReplayArtifact{ID: artifact.ID, SizeInBytes: artifact.SizeInBytes})
		}
		if len(listing.Artifacts) < 100 {
			break
		}
		if page == 10 {
			return nil, errors.New("artifact_replay_invalid: this run has too many artifacts to search within the 1,000-artifact limit. Download its original snapshot and replay it with --file /path/to/work-queue.snapshot.json")
		}
	}
	if len(candidates) == 0 {
		return nil, errors.New("artifact_replay_invalid: this run has no activation artifact retained for replay. Choose an agentic workflow run with retained activation artifacts, or use --file /path/to/work-queue.snapshot.json")
	}
	return candidates, nil
}

func selectWorkQueueReplaySnapshot(ctx context.Context, client *api.RESTClient, repository string, run *workQueueReplayRun, candidates []workQueueReplayArtifact) ([]byte, error) {
	var selected []byte
	archiveBudget, snapshotBudget := int64(workQueueReplayArtifactBytes), int64(workQueueReplayArtifactBytes)
	workflow, _, _ := strings.Cut(run.Path, "@")
	for _, artifact := range candidates {
		if artifact.SizeInBytes > archiveBudget {
			return nil, errors.New("artifact_replay_invalid: searching activation artifacts exceeds the aggregate 162 MiB archive limit. Use --file with the original matching snapshot")
		}
		raw, err := readWorkQueueReplayAPI(ctx, client, fmt.Sprintf("repos/%s/actions/artifacts/%d/zip", repository, artifact.ID), archiveBudget)
		if err != nil {
			return nil, err
		}
		archiveBudget -= int64(len(raw))
		data, err := workQueueReplaySnapshotFromZIPLimit(raw, snapshotBudget)
		if err != nil {
			return nil, err
		}
		snapshotBudget -= int64(len(data))
		var snapshot struct {
			Origin *workqueue.Actor `json:"origin"`
		}
		if json.Unmarshal(data, &snapshot) != nil || snapshot.Origin == nil {
			return nil, errors.New("artifact_replay_invalid: an activation snapshot has unreadable provenance. Use --file with the original matching snapshot")
		}
		if snapshot.Origin.RunID != strconv.FormatInt(run.ID, 10) ||
			snapshot.Origin.RunAttempt != run.RunAttempt || snapshot.Origin.Workflow != workflow ||
			!strings.EqualFold(snapshot.Origin.Repository, repository) {
			continue
		}
		if selected != nil {
			return nil, errors.New("artifact_replay_invalid: multiple activation snapshots match the requested run, attempt, and workflow. Use --file with the original intended snapshot")
		}
		selected = data
	}
	if selected == nil {
		return nil, errors.New("artifact_replay_invalid: no retained activation snapshot matches the requested run, attempt, repository, and workflow. Use --file with the original matching snapshot; do not edit its origin")
	}
	return selected, nil
}

func readWorkQueueReplayAPI(ctx context.Context, client *api.RESTClient, endpoint string, limit int64) (data []byte, err error) {
	response, err := client.RequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		var httpError *api.HTTPError
		if errors.As(err, &httpError) {
			return nil, workQueueReplayDownloadError(httpError.StatusCode)
		}
		return nil, fmt.Errorf("artifact replay could not download the Actions snapshot; check GitHub authentication, the API host, and connectivity: %w", currentWorkQueueDiagnosticError("download", err))
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			err = errors.Join(err, errors.New("artifact_replay_download_failed: could not close the response stream. Retry the read-only replay"))
		}
	}()
	if response.StatusCode != http.StatusOK {
		return nil, workQueueReplayDownloadError(response.StatusCode)
	}
	return readWorkQueueReplayBytes(response.Body, limit)
}

func workQueueReplayDownloadError(status int) error {
	explanation := "GitHub did not return the requested run or artifact. Verify --repo and --run-id, authentication, and connectivity before retrying the read-only replay"
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		explanation = "GitHub denied access. Authenticate gh with a credential that can read this repository's Actions runs and artifacts; artifact replay does not require write permissions"
	case http.StatusNotFound, http.StatusGone:
		explanation = "The run or artifact is missing, expired, or inaccessible. Verify --repo and --run-id and artifact retention, or use --file with an original downloaded snapshot"
	case http.StatusTooManyRequests:
		explanation = "GitHub rate-limited the download. Wait for the rate limit to reset, then retry the read-only replay"
	}
	return fmt.Errorf("artifact_replay_download_failed: HTTP %d. %s", status, explanation)
}

func readWorkQueueReplayBytes(reader io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, errors.New("artifact_replay_invalid: the artifact download or snapshot read was interrupted. Retry the read-only replay using the original artifact")
	}
	if int64(len(data)) > limit {
		return nil, errors.New("artifact_replay_invalid: the artifact or metadata exceeds its bounded input limit. Use --file with the original snapshot if the activation archive is too large; snapshots should be no larger than 162 MiB")
	}
	return data, nil
}

func workQueueReplaySnapshotFromZIPLimit(data []byte, limit int64) (snapshot []byte, err error) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("artifact_replay_invalid: the activation artifact is not a readable ZIP archive. Download the original artifact again; --file expects work-queue.snapshot.json, not the ZIP archive")
	}
	var selected *zip.File
	for _, entry := range archive.File {
		if entry.Name != workQueueSnapshotFile {
			continue
		}
		if selected != nil || !entry.Mode().IsRegular() || entry.UncompressedSize64 > uint64(limit) {
			return nil, errors.New("artifact_replay_invalid: the archive's snapshot entry is duplicated, not a regular file, or exceeds the aggregate 162 MiB snapshot limit. Use --file with the original matching snapshot")
		}
		selected = entry
	}
	if selected == nil {
		return nil, errors.New("artifact_replay_invalid: the activation artifact contains no root work-queue.snapshot.json. Choose a workflow run that captured a work-queue snapshot")
	}
	reader, err := selected.Open()
	if err != nil {
		return nil, errors.New("artifact_replay_invalid: the snapshot entry could not be opened. Download the original activation artifact again and retry the read-only replay")
	}
	defer func() {
		if closeErr := reader.Close(); closeErr != nil {
			err = errors.Join(err, errors.New("artifact_replay_invalid: could not close the snapshot stream. Download the original artifact again before retrying"))
		}
	}()
	return readWorkQueueReplayBytes(reader, limit)
}

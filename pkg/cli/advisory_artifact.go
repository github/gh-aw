package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/workflow"
)

const maxAdvisoryArchiveBytes = 8 << 20

var advisoryArchiveDigest = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type advisoryArtifact struct {
	ID      int64  `json:"id"`
	Size    int64  `json:"size_in_bytes"`
	Digest  string `json:"digest"`
	Expired bool   `json:"expired"`
	Run     struct {
		ID           int64 `json:"id"`
		RepositoryID int64 `json:"repository_id"`
	} `json:"workflow_run"`
}

type advisorySourceRun struct {
	ID             int64              `json:"id"`
	Event          string             `json:"event"`
	Conclusion     string             `json:"conclusion"`
	Attempt        int                `json:"run_attempt"`
	Repository     struct{ ID int64 } `json:"repository"`
	HeadRepository struct{ ID int64 } `json:"head_repository"`
}

func (o advisoryHandoff) loadPatch(ctx context.Context) ([]byte, error) {
	if o.sideRepo == "" {
		return readAdvisoryPatch(o.filename)
	}
	if !validAdvisoryRepo(o.sideRepo) || o.runID <= 0 || o.artifactID <= 0 {
		return nil, errors.New("provide a private --side-repo and positive --run and --artifact IDs")
	}
	repoPath := "repos/" + o.sideRepo
	var repo struct {
		ID         int64  `json:"id"`
		FullName   string `json:"full_name"`
		Visibility string `json:"visibility"`
	}
	if err := readAdvisoryAPI(ctx, repoPath, &repo); err != nil {
		return nil, err
	}
	if repo.ID <= 0 || repo.Visibility != "private" || !strings.EqualFold(repo.FullName, o.sideRepo) {
		return nil, errors.New("the artifact source must be the specified private repository")
	}
	if o.sideRepoID != 0 && o.sideRepoID != repo.ID {
		return nil, errors.New("the artifact source repository changed; verify the source and run gh aw advisory record again")
	}
	artifactPath := fmt.Sprintf("%s/actions/artifacts/%d", repoPath, o.artifactID)
	var artifact advisoryArtifact
	if err := readAdvisoryAPI(ctx, artifactPath, &artifact); err != nil {
		return nil, err
	}
	if err := artifact.verify(o, repo.ID); err != nil {
		return nil, err
	}
	// Check the attempt after fetching metadata so a concurrent rerun cannot supply a new artifact after the check.
	var run advisorySourceRun
	if err := readAdvisoryAPI(ctx, fmt.Sprintf("%s/actions/runs/%d", repoPath, o.runID), &run); err != nil {
		return nil, err
	}
	if err := run.verify(o.runID, repo.ID); err != nil {
		return nil, err
	}
	cmd := workflow.ExecGHContext(ctx, "api", "--hostname", "github.com", path.Join(artifactPath, "zip"))
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("prepare advisory artifact download: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start advisory artifact download: %w", err)
	}
	body, readErr := io.ReadAll(io.LimitReader(pipe, maxAdvisoryArchiveBytes+1))
	// Close before waiting so an oversized response cannot block the producer.
	closeErr := pipe.Close()
	waitErr := cmd.Wait()
	if readErr != nil || closeErr != nil || waitErr != nil || len(body) > maxAdvisoryArchiveBytes {
		return nil, errors.New("could not download the bounded advisory artifact; verify Actions read access and artifact size")
	}
	return extractAdvisoryPatch(body, artifact.Digest)
}

func (r advisorySourceRun) verify(runID, repoID int64) error {
	if r.ID != runID || r.Repository.ID != repoID || r.HeadRepository.ID != repoID {
		return errors.New("the source run and its head must belong to the specified private repository")
	}
	if r.Event != "workflow_dispatch" || r.Conclusion != "success" {
		return errors.New("the source run must be a successful manual dispatch; select a completed advisory workflow run")
	}
	if r.Attempt != 1 {
		return errors.New("the artifact source must be a successful first attempt; start a fresh manual dispatch instead of rerunning and use its new run and artifact IDs")
	}
	return nil
}

func readAdvisoryAPI(ctx context.Context, path string, dst any) error {
	body, err := workflow.RunGHContextWithHost(ctx, "Verifying advisory artifact source", "github.com", "api", "--hostname", "github.com", path)
	if err != nil {
		return errors.New("could not verify advisory artifact source on GitHub; check repository and Actions read access")
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return errors.New("GitHub returned invalid advisory artifact metadata")
	}
	return nil
}

func (a advisoryArtifact) verify(o advisoryHandoff, repoID int64) error {
	if a.ID != o.artifactID || a.Run.ID != o.runID || a.Run.RepositoryID != repoID {
		return errors.New("artifact does not belong to the specified repository and run")
	}
	if a.Expired || a.Size <= 0 || a.Size > maxAdvisoryArchiveBytes || !advisoryArchiveDigest.MatchString(a.Digest) {
		return errors.New("artifact is expired, oversized, or missing its GitHub SHA-256 digest")
	}
	return nil
}

func extractAdvisoryPatch(body []byte, digest string) ([]byte, error) {
	if fmt.Sprintf("sha256:%x", sha256.Sum256(body)) != digest {
		return nil, errors.New("downloaded advisory artifact does not match its GitHub digest")
	}
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, errors.New("advisory artifact is not a valid ZIP")
	}
	var patch *zip.File
	for _, file := range reader.File {
		if file.Name != "handoff/fix.patch" {
			continue
		}
		if patch != nil || !file.Mode().IsRegular() || file.UncompressedSize64 > maxAdvisoryPatchBytes {
			return nil, errors.New("artifact must contain one bounded regular handoff/fix.patch")
		}
		patch = file
	}
	if patch == nil {
		return nil, errors.New("artifact is missing handoff/fix.patch")
	}
	src, err := patch.Open()
	if err != nil {
		return nil, fmt.Errorf("open patch in advisory artifact: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(src, maxAdvisoryPatchBytes+1))
	if err := errors.Join(err, src.Close()); err != nil {
		return nil, fmt.Errorf("read patch in advisory artifact: %w", err)
	}
	return validateAdvisoryPatch(data)
}

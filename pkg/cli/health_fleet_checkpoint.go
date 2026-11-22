package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/repoutil"
)

const healthFleetCheckpointVersion = 1
const healthFleetCheckpointLimit = 128 << 20

func loadHealthFleetCheckpoint(config HealthConfig, host, repo string, start, end time.Time) (*healthFleetState, error) {
	if config.Checkpoint == "" {
		return newHealthFleetState(config, host, repo, start, end), nil
	}
	info, err := os.Lstat(config.Checkpoint)
	if errors.Is(err, os.ErrNotExist) {
		return newHealthFleetState(config, host, repo, start, end), nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect health checkpoint: %w", err)
	}
	if !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0) || info.Size() > healthFleetCheckpointLimit {
		return nil, errors.New("health checkpoint must be a private regular file within the size limit")
	}
	file, err := os.Open(config.Checkpoint)
	if err != nil {
		return nil, fmt.Errorf("open health checkpoint: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			healthLog.Printf("Close checkpoint: %v", err)
		}
	}()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("health checkpoint changed while opening it")
	}
	body, err := io.ReadAll(io.LimitReader(file, healthFleetCheckpointLimit+1))
	if err != nil {
		return nil, fmt.Errorf("read health checkpoint: %w", err)
	}
	var state healthFleetState
	if len(body) > healthFleetCheckpointLimit || json.Unmarshal(body, &state) != nil ||
		state.Version != healthFleetCheckpointVersion || state.Runs == nil || state.Verified == nil || state.Responses == nil ||
		state.Scope.Start.IsZero() || state.Scope.End.Before(state.Scope.Start) ||
		(state.Scope.HeadSHA != "" && !validHealthBlobSHA(state.Scope.HeadSHA)) {
		return nil, errors.New("invalid or unsupported health checkpoint; use a new checkpoint path")
	}
	if state.Scope.Repository != "" {
		savedRepo, savedHost := repoutil.NormalizeRepoForAPI(state.Scope.Repository)
		if !validHealthFleetRepository(savedHost, savedRepo) ||
			(repo != "" && (!strings.EqualFold(host, savedHost) || !strings.EqualFold(repo, savedRepo))) {
			return nil, errors.New("health checkpoint repository does not match --repo")
		}
	} else if repo != "" || state.Scope.Branch != "" {
		return nil, errors.New("health checkpoint is missing the repository scope")
	}
	if config.explicitTimeWindow && ((config.inputStartDate && !start.Equal(state.Scope.Start)) ||
		(config.inputEndDate && !end.Equal(state.Scope.End))) {
		return nil, errors.New("health checkpoint time window does not match; omit date flags to resume")
	}
	return &state, nil
}

func newHealthFleetState(config HealthConfig, host, repo string, start, end time.Time) *healthFleetState {
	state := &healthFleetState{
		Version: healthFleetCheckpointVersion, Scope: healthFleetScope{Start: start, End: end},
		Threshold: config.Threshold, RegistryPage: 1,
		LogsDir:   config.LogsDir,
		Windows:   []healthFleetWindow{{Start: start.Unix(), End: end.Unix(), Page: 1}},
		Runs:      make(map[int64]WorkflowRun),
		Verified:  make(map[string]healthFleetWorkflowEvidence),
		Responses: make(map[string][]byte),
	}
	if repo != "" {
		state.Scope.Repository = path.Join(host, repo)
	}
	return state
}

func saveHealthFleetCheckpoint(destination string, state *healthFleetState) error {
	body, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode health checkpoint: %w", err)
	}
	if len(body) > healthFleetCheckpointLimit {
		return errors.New("health checkpoint exceeded its size limit; use a narrower time window")
	}
	file, err := os.CreateTemp(filepath.Dir(destination), ".gh-aw-health-*")
	if err != nil {
		return fmt.Errorf("create health checkpoint: %w", err)
	}
	defer func() {
		if err := os.Remove(file.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
			healthLog.Printf("Remove checkpoint temporary file: %v", err)
		}
	}()
	_, writeErr := file.Write(body)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return fmt.Errorf("write health checkpoint: %w", err)
	}
	if err := os.Rename(file.Name(), destination); err != nil {
		return fmt.Errorf("publish health checkpoint: %w", err)
	}
	return nil
}

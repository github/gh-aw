package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/atif"
	"github.com/github/gh-aw/pkg/constants"
)

// Snapshot only recorded grading/session/provenance inputs, never source code
// scripts or a task environment. Original bytes and artifact-relative paths survive.
func harborSnapshot(ctx context.Context, source, destination string) (files []string, resultErr error) {
	if err := harborCheckPath(source); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(source)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	files = []string{}
	err = fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("harbor evidence contains a symbolic link: %s", path)
		}
		if entry.IsDir() {
			if entry.Name() == harborExportDirectory || entry.Name() == ".git" ||
				strings.HasPrefix(entry.Name(), ".harbor-stage-") || strings.HasPrefix(entry.Name(), ".harbor-backup-") {
				return fs.SkipDir
			}
			return nil
		}
		switch entry.Name() {
		case "aw_session.jsonl", "grader_results.json", "grader_manifest.json",
			"state.json", "state.jsonl", "aw_info.json", "run.json", "run_summary.json",
			"jobs.json", "summary.json", "aw_context.json", "episode_manifest.json":
		default:
			return nil
		}
		if err := harborSnapshotFile(root, path, destination, entry); err != nil {
			return err
		}
		files = append(files, "gh-aw/evidence/"+path)
		return nil
	})
	return files, err
}

func harborSnapshotFile(root *os.Root, path, destination string, entry fs.DirEntry) error {
	info, err := entry.Info()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("harbor evidence must be a regular file: %s", path)
	}
	if info.Size() > atif.MaxSessionBytes {
		return fmt.Errorf("harbor evidence exceeds the 64 MiB input limit: %s", path)
	}
	file, err := root.Open(path)
	if err != nil {
		return err
	}
	content, readErr := io.ReadAll(io.LimitReader(file, atif.MaxSessionBytes+1))
	if err := errors.Join(readErr, file.Close()); err != nil {
		return err
	}
	if len(content) > atif.MaxSessionBytes {
		return fmt.Errorf("harbor evidence exceeds the 64 MiB input limit: %s", path)
	}
	target := filepath.Join(destination, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(target), constants.DirPermOwnerOnly); err != nil {
		return err
	}
	return os.WriteFile(target, content, constants.FilePermSensitive)
}

// Audit summaries normalize boolean values to 1/0. Gate rewards on the original
// JSON token instead, and retain its precision rather than re-encoding a float64.
func harborRecordedRewards(evidence string, graders *GradersData) (map[string]json.Number, map[string]any, error) {
	if graders == nil {
		return nil, nil, nil
	}
	path := findGraderFile(evidence, "grader_results.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var document graderArtifactFullDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, nil, err
	}
	counts := make(map[string]int)
	summaries := make(map[string]GraderResult)
	for _, result := range graders.Results {
		summaries[result.ID] = result
	}
	for _, result := range document.Results {
		counts[result.ID]++
	}
	rewards := make(map[string]json.Number)
	metrics := make(map[string]any)
	relative, err := filepath.Rel(evidence, path)
	if err != nil {
		return nil, nil, err
	}
	for _, result := range document.Results {
		if (result.Status != "pass" && result.Status != "fail") || result.ID == "" || counts[result.ID] != 1 {
			continue
		}
		number, ok := harborRawNumericValue(result.Value)
		if !ok {
			continue
		}
		key := "gh-aw." + result.ID
		rewards[key] = number
		summary := summaries[result.ID]
		metrics[key] = map[string]any{
			"grader_id": result.ID, "status": result.Status, "source_field": "value",
			"source_file":    "gh-aw/evidence/" + filepath.ToSlash(relative),
			"transformation": "none; original numeric JSON value",
			"direction":      summary.Direction, "threshold": summary.Threshold, "unit": summary.Unit,
		}
	}
	return rewards, map[string]any{
		"policy":  "recorded status=pass/fail finite JSON numbers only; unchanged metrics, not Harbor task success; booleans, passed flags and Actions conclusions never become rewards",
		"metrics": metrics,
	}, nil
}

func harborRawNumericValue(raw json.RawMessage) (json.Number, bool) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return "", false
	}
	number, ok := value.(json.Number)
	if !ok {
		return "", false
	}
	finite, err := number.Float64()
	return number, err == nil && !math.IsNaN(finite) && !math.IsInf(finite, 0)
}

func harborRecordedAgent(evidence string) (map[string]any, string) {
	info := map[string]any{"name": "gh-aw", "version": "unknown"}
	for _, path := range []string{"aw_info.json", "aw_info.json/aw_info.json", "usage/aw_info.json", "agent/aw_info.json"} {
		raw, err := os.ReadFile(filepath.Join(evidence, filepath.FromSlash(path)))
		if err != nil {
			continue
		}
		var recorded struct {
			EngineID     string `json:"engine_id"`
			AgentVersion string `json:"agent_version"`
			Model        string `json:"model"`
		}
		if json.Unmarshal(raw, &recorded) != nil || recorded.EngineID == "" {
			continue
		}
		info["name"] = recorded.EngineID
		if recorded.AgentVersion != "" {
			info["version"] = recorded.AgentVersion
		}
		if recorded.Model != "" {
			info["model_info"] = map[string]any{"name": recorded.Model}
		}
		return info, "gh-aw/evidence/" + path + "; engine_id, agent_version (not gh-aw version), model"
	}
	return info, "unavailable; generic gh-aw identity, unknown agent version"
}

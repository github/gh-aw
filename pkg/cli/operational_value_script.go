package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/github/gh-aw/actions/setup"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
)

var valueFunctionLog = logger.New("cli:value")

type valueFunctionConfig struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Unit       string         `json:"unit"`
	Direction  string         `json:"direction"`
	Paths      []string       `json:"paths"`
	Script     string         `json:"script"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

type scriptValueMeasurement struct {
	info       valueMeasurementDefinition
	config     valueFunctionConfig
	node       string
	worker     string
	permission string
}

func loadScriptValueMeasurement(ctx context.Context, definition valueMeasurementDefinition, path string) (valueMeasurement, func(), error) {
	config, digest, err := readValueFunctionConfig(path)
	if err != nil {
		return nil, nil, err
	}
	definition.ID, definition.Name, definition.Unit = config.ID, config.Name, config.Unit
	definition.Direction, definition.Digest = config.Direction, digest
	node, err := exec.LookPath("node")
	if err != nil {
		return nil, nil, errors.New("value functions require Node.js 20 or newer with permission support")
	}
	permission, err := graderNodePermissionFlag(ctx, node)
	if err != nil {
		return nil, nil, err
	}
	worker, err := setup.SessionParserSources.ReadFile("js/custom_grader_worker.cjs")
	if err != nil {
		return nil, nil, err
	}
	dir, err := os.MkdirTemp("", "gh-aw-value-function-*")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() {
		if err := os.RemoveAll(dir); err != nil {
			valueFunctionLog.Printf("Failed to remove value-function runtime: %v", err)
		}
	}
	workerPath := filepath.Join(dir, "worker.cjs")
	if err := os.WriteFile(workerPath, worker, constants.FilePermSensitive); err != nil {
		cleanup()
		return nil, nil, err
	}
	return &scriptValueMeasurement{definition, config, node, workerPath, permission}, cleanup, nil
}

func readValueFunctionConfig(path string) (valueFunctionConfig, string, error) {
	var config valueFunctionConfig
	info, err := os.Stat(path)
	if err != nil {
		return config, "", err
	}
	if !info.Mode().IsRegular() {
		return config, "", errors.New("value-function definition must be a regular JSON file")
	}
	file, err := os.Open(path)
	if err != nil {
		return config, "", err
	}
	defer func() {
		if err := file.Close(); err != nil {
			valueFunctionLog.Printf("Failed to close value-function definition: %v", err)
		}
	}()
	info, err = file.Stat()
	if err != nil {
		return config, "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 512*1024 {
		return config, "", errors.New("value-function definition must be a regular JSON file no larger than 512 KiB")
	}
	content, err := io.ReadAll(io.LimitReader(file, 512*1024+1))
	if err != nil {
		return config, "", err
	}
	if len(content) > 512*1024 {
		return config, "", errors.New("value-function definition exceeds 512 KiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return config, "", fmt.Errorf("decode value-function definition: %w", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return config, "", errors.New("value-function definition must contain exactly one JSON object")
	}
	if err := validateValueFunctionConfig(config); err != nil {
		return config, "", err
	}
	digest := sha256.Sum256(content)
	return config, "sha256:" + hex.EncodeToString(digest[:]), nil
}

func validateValueFunctionConfig(config valueFunctionConfig) error {
	if strings.TrimSpace(config.ID) == "" || strings.TrimSpace(config.Name) == "" || strings.TrimSpace(config.Unit) == "" ||
		strings.TrimSpace(config.Script) == "" || len(config.Script) > 256*1024 {
		return errors.New("value-function definition requires id, name, unit, and a nonempty script of at most 256 KiB")
	}
	if config.Direction != "higher_is_better" && config.Direction != "lower_is_better" {
		return errors.New("value-function direction must be higher_is_better or lower_is_better")
	}
	if len(config.Paths) == 0 || len(config.Paths) > 32 {
		return errors.New("value-function paths must select between 1 and 32 repository-relative files or directories")
	}
	for _, path := range config.Paths {
		if err := gitutil.ValidateGitPath(path); err != nil {
			return err
		}
	}
	return nil
}

func (m *scriptValueMeasurement) definition() valueMeasurementDefinition { return m.info }

func (m *scriptValueMeasurement) measure(git valueGit, sha string) (valueSnapshot, error) {
	snapshot := valueSnapshot{SHA: sha}
	files, err := readValueSnapshotFiles(git, sha, m.config.Paths)
	if err != nil {
		return snapshot, err
	}
	input, err := json.Marshal(map[string]any{
		"id": "longitudinal-value", "script": "const snapshot = config.snapshot;\nconst parameters = config.parameters;\n" + m.config.Script,
		"data": map[string]any{
			"session": []any{}, "lookups": map[string]any{"toolCallsById": []any{}},
			"config": map[string]any{"snapshot": map[string]any{"sha": sha, "files": files}, "parameters": m.config.Parameters},
		},
	})
	if err != nil {
		return snapshot, err
	}
	output, err := m.run(git.ctx, input)
	if err != nil {
		return snapshot, fmt.Errorf("value function at %s: %w", sha, err)
	}
	var response struct {
		OK    bool     `json:"ok"`
		Value *float64 `json:"value"`
		Error string   `json:"error"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		return snapshot, fmt.Errorf("invalid value-function response: %w", err)
	}
	if !response.OK {
		return snapshot, fmt.Errorf("value function at %s: %s", sha, response.Error)
	}
	if response.Value != nil && (math.IsNaN(*response.Value) || math.IsInf(*response.Value, 0)) {
		return snapshot, errors.New("value function must return a finite number or null")
	}
	snapshot.Value = response.Value
	return snapshot, nil
}

func (m *scriptValueMeasurement) run(parent context.Context, input []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, graderJSTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, m.node, m.permission, "--allow-fs-read="+m.worker, "--max-old-space-size=256", m.worker)
	cmd.Env = []string{}
	cmd.Stdin = bytes.NewReader(input)
	var output, diagnostics valueFunctionOutput
	cmd.Stdout, cmd.Stderr = &output, &diagnostics
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("worker failed: %w: %s", err, strings.TrimSpace(diagnostics.buffer.String()))
	}
	return output.buffer.Bytes(), nil
}

type valueFunctionOutput struct{ buffer bytes.Buffer }

func (output *valueFunctionOutput) Write(data []byte) (int, error) {
	if output.buffer.Len()+len(data) > 64*1024 {
		return 0, errors.New("value-function output exceeds 64 KiB")
	}
	return output.buffer.Write(data)
}

func readValueSnapshotFiles(git valueGit, sha string, paths []string) (map[string]string, error) {
	args := []string{"ls-tree", "-r", "-l", "-z", "--full-tree", sha, "--"}
	for _, path := range paths {
		args = append(args, ":(literal)"+path)
	}
	inventory, err := git.run(args...)
	if err != nil {
		return nil, err
	}
	files := make(map[string]string)
	total := 0
	for entry := range strings.SplitSeq(inventory, "\x00") {
		if entry == "" {
			continue
		}
		metadata, path, ok := strings.Cut(entry, "\t")
		fields := strings.Fields(metadata)
		if !ok || len(fields) != 4 {
			return nil, errors.New("invalid value-function Git tree entry")
		}
		mode, kind, sizeText := fields[0], fields[1], fields[3] //nolint:uncheckedsliceindex // Exactly four Git tree fields are required above.
		if kind != "blob" || mode != "100644" && mode != "100755" {
			return nil, errors.New("value-function paths must contain only regular tracked files, not symlinks or submodules")
		}
		size, err := strconv.Atoi(sizeText)
		if err != nil || size < 0 || size > 8*1024*1024-total || len(files) >= 512 {
			return nil, errors.New("value-function snapshot exceeds the 512-file or 8 MiB limit; narrow paths")
		}
		content, err := git.output("show", sha+":"+path)
		if err != nil {
			return nil, err
		}
		if !utf8.Valid(content) {
			return nil, fmt.Errorf("value-function input %s is not UTF-8 text", path)
		}
		total += len(content)
		files[path] = string(content)
	}
	return files, nil
}

func (m *scriptValueMeasurement) change(before, after valueSnapshot) valueChange {
	if before.Value == nil || after.Value == nil {
		return valueChange{Reason: "measurement_unavailable"}
	}
	value := *after.Value - *before.Value
	if m.info.Direction == "lower_is_better" {
		value = -value
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return valueChange{Reason: "non_finite_measurement_change"}
	}
	return valueChange{Value: &value}
}

func (*scriptValueMeasurement) cohort([]valueSnapshot) *valueCohort { return nil }

func (m *scriptValueMeasurement) format(snapshot valueSnapshot) string {
	if snapshot.Value == nil {
		return "unavailable"
	}
	return fmt.Sprintf("%g %s", *snapshot.Value, m.info.Unit)
}

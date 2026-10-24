package cli

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/actions/setup"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/workflow"
)

const (
	maxGraderPayloadBytes = 50 * 1024 * 1024
	graderJSTimeout       = 7 * time.Second
)

//go:embed graders_run.cjs
var gradersRunScript []byte

type graderRunConfig struct {
	Workflow string
	GraderID string
	RunID    int64
	Repo     string
	Input    io.Reader
	Output   io.Writer
	Path     string
}

type graderRunDefinition struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Source      string         `json:"source"`
	Unit        string         `json:"unit"`
	Direction   string         `json:"direction"`
	Threshold   *float64       `json:"threshold"`
	Config      map[string]any `json:"config,omitempty"`
	Script      string         `json:"script,omitempty"`
	Run         string         `json:"run,omitempty"`
	ShellScript string         `json:"shellScript,omitempty"`
}

func runGrader(ctx context.Context, config graderRunConfig) error {
	grader, err := loadGraderRunDefinition(config.Workflow, config.GraderID)
	if err != nil {
		return err
	}
	payload, err := loadGraderRunPayload(ctx, config)
	if err != nil {
		return err
	}
	return runSessionGrader(ctx, grader, payload, config.Output, config.Path)
}

func loadGraderRunDefinition(workflowArg, graderID string) (graderRunDefinition, error) {
	workflowPath, err := ResolveWorkflowPath(workflowArg)
	if err != nil {
		return graderRunDefinition{}, err
	}
	data, err := workflow.NewCompiler().ParseWorkflowFile(workflowPath)
	if err != nil {
		return graderRunDefinition{}, fmt.Errorf("cannot parse graders in %s: %w", workflowPath, err)
	}
	definition := data.Graders.Graders[graderID]
	if definition == nil {
		return graderRunDefinition{}, fmt.Errorf("workflow %s does not enable grader %q", workflowPath, graderID)
	}
	if definition.Enabled != nil && !*definition.Enabled {
		return graderRunDefinition{}, fmt.Errorf("grader %q is disabled in workflow %s", graderID, workflowPath)
	}
	source := "builtin"
	if definition.Script != "" {
		source = "inline"
	} else if definition.Run != "" {
		source = "operational-value"
		if err := definition.PrepareShellScript(workflowPath); err != nil {
			return graderRunDefinition{}, err
		}
	}
	return graderRunDefinition{
		ID: graderID, Name: definition.Name, Description: definition.Description, Source: source, Unit: definition.Unit,
		Direction: definition.Direction, Threshold: definition.Threshold, Config: definition.Config, Script: definition.Script,
		Run: definition.Run, ShellScript: definition.ShellScript,
	}, nil
}

func loadGraderRunPayload(ctx context.Context, config graderRunConfig) (payload json.RawMessage, err error) {
	if config.RunID == 0 {
		return readGraderPayload(config.Input, "standard input")
	}
	tempDir, err := os.MkdirTemp("", "gh-aw-grader-run-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create grader run directory: %w", err)
	}
	defer os.RemoveAll(tempDir)
	params := buildConcurrentDownloadParams(tempDir, false, config.Repo, nil, false, nil)
	names, err := listRunArtifactNames(ctx, config.RunID, params.dlOwner, params.dlRepo, params.dlHost, false)
	if err != nil {
		return nil, err
	}
	name, err := sessionArtifactName(names, constants.UsageArtifactName.String(), "")
	if err != nil {
		return nil, err
	}
	if name == "" {
		return nil, fmt.Errorf("run %d has no usage artifact containing a unified session", config.RunID)
	}
	run := &parser.GitHubURLComponents{Number: config.RunID, Owner: params.dlOwner, Repo: params.dlRepo, Host: params.dlHost}
	if err := downloadSessionArtifact(ctx, run, params.dlHost, name, filepath.Join(tempDir, "usage"), false); err != nil {
		return nil, err
	}
	sessionPath, err := findUnifiedSessionPath(tempDir)
	if err != nil {
		return nil, err
	}
	if sessionPath == "" {
		return nil, fmt.Errorf("run %d usage artifact does not contain aw_session.jsonl", config.RunID)
	}
	file, err := os.Open(sessionPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read unified session for run %d: %w", config.RunID, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("cannot close unified session for run %d: %w", config.RunID, closeErr))
		}
	}()
	return readGraderPayload(file, "run usage artifact")
}

func readGraderPayload(reader io.Reader, source string) (json.RawMessage, error) {
	if reader == nil {
		return nil, fmt.Errorf("cannot read unified session from %s", source)
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxGraderPayloadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("cannot read unified session from %s: %w", source, err)
	}
	if len(data) > maxGraderPayloadBytes {
		return nil, fmt.Errorf("unified session from %s exceeds the %d-byte limit", source, maxGraderPayloadBytes)
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, fmt.Errorf("unified session from %s is empty", source)
	}
	var events []json.RawMessage
	if bytes.HasPrefix(data, []byte("[")) {
		if err := json.Unmarshal(data, &events); err != nil {
			return nil, fmt.Errorf("unified session from %s is not a valid JSON array: %w", source, err)
		}
	} else {
		for index, line := range bytes.Split(data, []byte("\n")) {
			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}
			if !json.Valid(line) {
				return nil, fmt.Errorf("unified session from %s has invalid JSON on line %d", source, index+1)
			}
			events = append(events, json.RawMessage(line))
		}
	}
	for index, event := range events {
		var entry struct {
			Type       string          `json:"type"`
			Data       json.RawMessage `json:"data"`
			Provenance json.RawMessage `json:"provenance"`
		}
		if err := json.Unmarshal(event, &entry); err != nil || entry.Type == "" ||
			!bytes.HasPrefix(entry.Data, []byte("{")) || !bytes.HasPrefix(entry.Provenance, []byte("{")) {
			return nil, fmt.Errorf("unified session from %s has an invalid event at index %d; expected type, data, and provenance", source, index)
		}
	}
	return json.Marshal(events)
}

func runSessionGrader(ctx context.Context, grader graderRunDefinition, payload json.RawMessage, output io.Writer, searchPath string) error {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		return errors.New("node is required to run graders")
	}
	var permissionFlag string
	if grader.Source != "operational-value" {
		permissionFlag, err = graderNodePermissionFlag(ctx, nodePath)
		if err != nil {
			return err
		}
	}
	directory, err := os.MkdirTemp("", "gh-aw-grader-runtime-*")
	if err != nil {
		return fmt.Errorf("failed to stage grader runtime: %w", err)
	}
	defer os.RemoveAll(directory)
	directory, err = stageGraderRuntime(directory)
	if err != nil {
		return err
	}
	input, err := json.Marshal(struct {
		Grader  graderRunDefinition `json:"grader"`
		Payload json.RawMessage     `json:"payload"`
	}{grader, payload})
	if err != nil {
		return fmt.Errorf("failed to encode grader input: %w", err)
	}
	timeout := graderJSTimeout
	if grader.Source == "operational-value" {
		timeout = 125 * time.Second
	}
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	args := []string{permissionFlag, "--allow-fs-read=" + directory}
	switch grader.Source {
	case "inline":
		args = append(args, "--allow-child-process")
	case "operational-value":
		// Node's permissions do not sandbox a spawned Bash interpreter. Shell
		// replay has the same trusted-code boundary as the separate Actions job.
		args = nil
	}
	args = append(args, filepath.Join(directory, "graders_run.cjs"))
	cmd := exec.CommandContext(commandCtx, nodePath, args...)
	cmd.Env = []string{}
	if grader.Source == "operational-value" && searchPath != "" {
		cmd.Env = []string{"PATH=" + searchPath}
	}
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = output
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("grader timed out after %s", timeout)
		}
		return fmt.Errorf("grader failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func stageGraderRuntime(directory string) (string, error) {
	for _, name := range []string{"error_codes.cjs", "trace_graders.cjs", "grader_session.cjs", "trajectory_graders.cjs", "builtin_graders.json", "working_set_metrics.cjs", "custom_graders.cjs", "custom_grader_worker.cjs", "operational_value_grader.cjs", "operational_value_worker.cjs"} {
		content, err := setup.SessionParserSources.ReadFile("js/" + name)
		if err != nil {
			return "", fmt.Errorf("cannot load grader runtime %s: %w", name, err)
		}
		if err := os.WriteFile(filepath.Join(directory, name), content, constants.FilePermSensitive); err != nil {
			return "", fmt.Errorf("failed to stage grader runtime %s: %w", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "graders_run.cjs"), gradersRunScript, constants.FilePermSensitive); err != nil {
		return "", fmt.Errorf("failed to stage grader runner: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return "", fmt.Errorf("failed to resolve grader runtime path: %w", err)
	}
	return resolved, nil
}

func graderNodePermissionFlag(ctx context.Context, nodePath string) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, graderJSTimeout)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, nodePath, "--print",
		`process.allowedNodeEnvironmentFlags.has("--permission") ? "--permission" : process.allowedNodeEnvironmentFlags.has("--experimental-permission") ? "--experimental-permission" : ""`)
	cmd.Env = []string{}
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to detect Node.js permission support: %w", err)
	}
	flag := strings.TrimSpace(string(output))
	if flag != "--permission" && flag != "--experimental-permission" {
		return "", errors.New("node permission support is required to run graders; install Node.js 20 or newer")
	}
	return flag, nil
}

func parseGraderRunID(value string) (int64, error) {
	runID, err := strconv.ParseInt(value, 10, 64)
	if err != nil || runID <= 0 {
		return 0, errors.New("run ID must be a positive integer")
	}
	return runID, nil
}

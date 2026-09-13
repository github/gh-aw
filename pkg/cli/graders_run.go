package cli

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/repoutil"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/workflow"
)

const (
	maxGraderPayloadBytes             = 50 * 1024 * 1024
	maxGraderCommandOutputBytes       = 1024 * 1024
	maxOperationalValueEvaluatorBytes = 64 * 1024
	graderJSTimeout                   = 7 * time.Second
	operationalValueSyntaxTimeout     = 5 * time.Second
	operationalValueEvaluatorTimeout  = 2 * time.Minute
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
}

type graderRunDefinition struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Source       string         `json:"source"`
	Unit         string         `json:"unit"`
	Direction    string         `json:"direction"`
	Threshold    *float64       `json:"threshold"`
	Config       map[string]any `json:"config,omitempty"`
	Script       string         `json:"script,omitempty"`
	Digest       string         `json:"digest,omitempty"`
	Run          string         `json:"-"`
	WorkflowPath string         `json:"-"`
}

type boundedCommandBuffer struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (b *boundedCommandBuffer) Write(data []byte) (int, error) {
	written := len(data)
	remaining := b.limit - b.Len()
	if remaining > 0 {
		if len(data) > remaining {
			_, _ = b.Buffer.Write(data[:remaining])
		} else {
			_, _ = b.Buffer.Write(data)
		}
	}
	if written > remaining {
		b.exceeded = true
	}
	return written, nil
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
	if grader.Source == "operational-value" {
		evaluatorHost := getGitHubHostForRepo("")
		if config.Repo != "" {
			ownerRepo, host := repoutil.NormalizeRepoForAPI(config.Repo)
			evaluatorHost = getGitHubHostForRepo(ownerRepo)
			if host != "" {
				evaluatorHost = stringutil.NormalizeGitHubHostURL(host)
			}
		}
		return runOperationalValuePayload(ctx, grader, payload, config.Output, evaluatorHost)
	}
	return runJavaScriptGrader(ctx, grader, payload, config.Output)
}

func loadGraderRunDefinition(workflowArg, graderID string) (graderRunDefinition, error) {
	workflowPath, err := ResolveWorkflowPath(workflowArg)
	if err != nil {
		return graderRunDefinition{}, err
	}
	content, err := os.ReadFile(workflowPath)
	if err != nil {
		return graderRunDefinition{}, fmt.Errorf("cannot read workflow %s: %w", workflowPath, err)
	}
	parsed, err := parser.ExtractFrontmatterFromContent(string(content))
	if err != nil {
		return graderRunDefinition{}, fmt.Errorf("cannot parse workflow %s: %w", workflowPath, err)
	}
	graders, err := workflow.ParseGradersFromFrontmatter(parsed.Frontmatter)
	if err != nil {
		return graderRunDefinition{}, fmt.Errorf("cannot parse graders in %s: %w", workflowPath, err)
	}
	if graders == nil || graders.Graders[graderID] == nil {
		return graderRunDefinition{}, fmt.Errorf("workflow %s does not declare grader %q", workflowPath, graderID)
	}
	definition := graders.Graders[graderID]
	if definition.Enabled != nil && !*definition.Enabled {
		return graderRunDefinition{}, fmt.Errorf("grader %q is disabled in workflow %s", graderID, workflowPath)
	}
	source := "inline"
	if slices.Contains(workflow.BuiltinGraderIDs, graderID) {
		source = "builtin"
	}
	if graderID == "operational-value" {
		source = "operational-value"
	}
	return graderRunDefinition{
		ID:           graderID,
		Name:         definition.Name,
		Source:       source,
		Unit:         definition.Unit,
		Direction:    definition.Direction,
		Threshold:    definition.Threshold,
		Config:       definition.Config,
		Script:       definition.Script,
		Digest:       definition.ScriptDigest(),
		Run:          definition.Run,
		WorkflowPath: workflowPath,
	}, nil
}

func loadGraderRunPayload(ctx context.Context, config graderRunConfig) (json.RawMessage, error) {
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
	artifactNames := make([]string, 0, 2)
	for _, name := range names {
		if name == constants.AgentArtifactName.String() || name == constants.AgentOutputFallbackArtifactName.String() {
			artifactNames = append(artifactNames, name)
		}
	}
	if len(artifactNames) == 0 {
		return nil, fmt.Errorf("run %d has no agent artifact", config.RunID)
	}
	if err := downloadArtifactsByName(ctx, downloadArtifactsOptions{
		runID: config.RunID, outputDir: tempDir, owner: params.dlOwner, repo: params.dlRepo, hostname: params.dlHost,
	}, artifactNames); err != nil {
		return nil, err
	}
	if err := flattenUnifiedArtifact(tempDir, false); err != nil {
		return nil, fmt.Errorf("failed to unpack run %d agent artifact: %w", config.RunID, err)
	}
	payloadPath := findGraderFile(tempDir, constants.GraderPayloadFilename.String())
	if payloadPath == "" {
		return nil, fmt.Errorf("run %d agent artifact does not contain %s", config.RunID, constants.GraderPayloadFilename)
	}
	file, err := os.Open(payloadPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read grader payload for run %d: %w", config.RunID, err)
	}
	defer file.Close()
	return readGraderPayload(file, "run artifact")
}

func readGraderPayload(reader io.Reader, source string) (json.RawMessage, error) {
	if reader == nil {
		return nil, fmt.Errorf("cannot read grader payload from %s", source)
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxGraderPayloadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("cannot read grader payload from %s: %w", source, err)
	}
	if len(data) > maxGraderPayloadBytes {
		return nil, fmt.Errorf("grader payload from %s exceeds the %d-byte limit", source, maxGraderPayloadBytes)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("grader payload from %s is empty", source)
	}
	if !json.Valid(data) {
		return nil, fmt.Errorf("grader payload from %s is not valid JSON", source)
	}
	return data, nil
}

func runJavaScriptGrader(ctx context.Context, grader graderRunDefinition, payload json.RawMessage, output io.Writer) error {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		return errors.New("node is required to run graders")
	}
	script, err := os.CreateTemp("", "gh-aw-grader-run-*.cjs")
	if err != nil {
		return fmt.Errorf("failed to stage grader runtime: %w", err)
	}
	scriptPath := script.Name()
	defer os.Remove(scriptPath)
	if err := script.Chmod(constants.FilePermSensitive); err != nil {
		_ = script.Close()
		return fmt.Errorf("failed to secure grader runtime: %w", err)
	}
	if _, err := script.Write(gradersRunScript); err != nil {
		_ = script.Close()
		return fmt.Errorf("failed to stage grader runtime: %w", err)
	}
	if err := script.Close(); err != nil {
		return fmt.Errorf("failed to stage grader runtime: %w", err)
	}
	input, err := json.Marshal(struct {
		Grader  graderRunDefinition `json:"grader"`
		Payload json.RawMessage     `json:"payload"`
	}{Grader: grader, Payload: payload})
	if err != nil {
		return fmt.Errorf("failed to encode grader input: %w", err)
	}
	commandCtx, cancel := context.WithTimeout(ctx, graderJSTimeout)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, nodePath, scriptPath)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = output
	stderr := &boundedCommandBuffer{limit: maxGraderCommandOutputBytes}
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("grader timed out after %s", graderJSTimeout)
		}
		if message := bytes.TrimSpace(stderr.Bytes()); len(message) > 0 {
			return errors.New(string(message))
		}
		return fmt.Errorf("grader failed: %w", err)
	}
	return nil
}

func runOperationalValuePayload(ctx context.Context, grader graderRunDefinition, payload json.RawMessage, output io.Writer, evaluatorHost string) error {
	evaluatorPath, cleanup, err := stageOperationalValueEvaluator(grader)
	if err != nil {
		return err
	}
	defer cleanup()
	if _, err := runOperationalValueEvaluatorBash(ctx, evaluatorPath, []string{"-n", evaluatorPath}, nil, operationalValueSyntaxTimeout, evaluatorHost); err != nil {
		return fmt.Errorf("operational-value evaluator has invalid Bash syntax: %w", err)
	}
	request, err := buildOperationalValueRequest(payload, grader.Config)
	if err != nil {
		return err
	}
	result, err := runOperationalValueEvaluatorBash(ctx, evaluatorPath, []string{evaluatorPath}, request, operationalValueEvaluatorTimeout, evaluatorHost)
	if err != nil {
		return fmt.Errorf("operational-value evaluator failed: %w", err)
	}
	metrics, err := validateOperationalValueMetrics(result)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(metrics)
	if err != nil {
		return fmt.Errorf("failed to encode operational-value metrics: %w", err)
	}
	_, err = fmt.Fprintln(output, string(encoded))
	return err
}

func stageOperationalValueEvaluator(grader graderRunDefinition) (string, func(), error) {
	if grader.Run == "" {
		return "", nil, fmt.Errorf("workflow %s does not declare an enabled graders.operational-value.run", grader.WorkflowPath)
	}
	repoRoot, err := gitutil.FindGitRootFrom(filepath.Dir(grader.WorkflowPath))
	if err != nil {
		return "", nil, fmt.Errorf("cannot resolve operational-value evaluator: %w", err)
	}
	evaluatorPath := workflow.ResolveOperationalValueEvaluatorPath(repoRoot, grader.WorkflowPath, grader.Run)
	content, err := readOperationalValueEvaluator(repoRoot, evaluatorPath)
	if err != nil {
		return "", nil, err
	}
	tempFile, err := os.CreateTemp(filepath.Dir(evaluatorPath), "."+filepath.Base(evaluatorPath)+".frozen-*")
	if err != nil {
		return "", nil, fmt.Errorf("cannot stage operational-value evaluator: %w", err)
	}
	tempPath := tempFile.Name()
	cleanup := func() { _ = os.Remove(tempPath) }
	if err := tempFile.Chmod(constants.FilePermSensitive); err != nil {
		_ = tempFile.Close()
		cleanup()
		return "", nil, fmt.Errorf("cannot secure staged operational-value evaluator: %w", err)
	}
	if _, err := tempFile.Write(content); err != nil {
		_ = tempFile.Close()
		cleanup()
		return "", nil, fmt.Errorf("cannot write staged operational-value evaluator: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("cannot close staged operational-value evaluator: %w", err)
	}
	return tempPath, cleanup, nil
}

func readOperationalValueEvaluator(repoRoot, evaluatorPath string) ([]byte, error) {
	if err := fileutil.ValidatePathWithinBase(repoRoot, evaluatorPath); err != nil {
		return nil, errors.New("operational-value evaluator escapes the Git repository")
	}
	info, err := os.Lstat(evaluatorPath)
	if err != nil {
		return nil, fmt.Errorf("cannot inspect operational-value evaluator: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("operational-value evaluator must be a regular file, not a symbolic link")
	}
	file, err := os.Open(evaluatorPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read operational-value evaluator: %w", err)
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxOperationalValueEvaluatorBytes+1))
	if err != nil {
		return nil, fmt.Errorf("cannot read operational-value evaluator: %w", err)
	}
	if len(content) > maxOperationalValueEvaluatorBytes {
		return nil, fmt.Errorf("operational-value evaluator exceeds the %d-byte limit", maxOperationalValueEvaluatorBytes)
	}
	if !utf8.Valid(content) {
		return nil, errors.New("operational-value evaluator must be valid UTF-8")
	}
	if !bytes.HasPrefix(content, []byte("#!/usr/bin/env bash\n")) && !bytes.HasPrefix(content, []byte("#!/bin/bash\n")) {
		return nil, errors.New("operational-value evaluator must start with a Bash shebang")
	}
	return content, nil
}

func buildOperationalValueRequest(payload json.RawMessage, config map[string]any) ([]byte, error) {
	var input struct {
		Run   json.RawMessage `json:"run"`
		Event json.RawMessage `json:"event"`
	}
	if err := json.Unmarshal(payload, &input); err != nil {
		return nil, fmt.Errorf("cannot decode operational-value grader payload: %w", err)
	}
	if len(bytes.TrimSpace(input.Run)) == 0 || bytes.Equal(bytes.TrimSpace(input.Run), []byte("null")) {
		return nil, errors.New("operational-value grader payload must contain run")
	}
	if len(bytes.TrimSpace(input.Event)) == 0 {
		return nil, errors.New("operational-value grader payload must contain event")
	}
	if config == nil {
		config = map[string]any{}
	}
	request, err := json.Marshal(struct {
		SchemaVersion int             `json:"schemaVersion"`
		Run           json.RawMessage `json:"run"`
		Event         json.RawMessage `json:"event"`
		Config        map[string]any  `json:"config"`
	}{SchemaVersion: 1, Run: input.Run, Event: input.Event, Config: config})
	if err != nil {
		return nil, fmt.Errorf("failed to encode operational-value request: %w", err)
	}
	return request, nil
}

type operationalValueMetric struct {
	ID    string   `json:"id"`
	Value *float64 `json:"value"`
}

func validateOperationalValueMetrics(data []byte) ([]operationalValueMetric, error) {
	var rawMetrics []struct {
		ID    string          `json:"id"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(data, &rawMetrics); err != nil {
		return nil, fmt.Errorf("operational-value evaluator returned invalid JSON: %w", err)
	}
	if len(rawMetrics) == 0 {
		return nil, errors.New("operational-value evaluator must return a non-empty metric array")
	}
	metrics := make([]operationalValueMetric, 0, len(rawMetrics))
	ids := make(map[string]struct{}, len(rawMetrics))
	for _, rawMetric := range rawMetrics {
		if strings.TrimSpace(rawMetric.ID) == "" {
			return nil, errors.New("operational-value metric id must be a non-empty string")
		}
		if _, exists := ids[rawMetric.ID]; exists {
			return nil, fmt.Errorf("operational-value metric id is duplicated: %s", rawMetric.ID)
		}
		ids[rawMetric.ID] = struct{}{}
		valueJSON := bytes.TrimSpace(rawMetric.Value)
		if len(valueJSON) == 0 {
			return nil, fmt.Errorf("operational-value metric %s must include value", rawMetric.ID)
		}
		metric := operationalValueMetric{ID: rawMetric.ID}
		if !bytes.Equal(valueJSON, []byte("null")) {
			var value float64
			if err := json.Unmarshal(valueJSON, &value); err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
				return nil, fmt.Errorf("operational-value metric %s must be null or a finite number in [0,1]", rawMetric.ID)
			}
			metric.Value = &value
		}
		metrics = append(metrics, metric)
	}
	return metrics, nil
}

func runOperationalValueEvaluatorBash(ctx context.Context, evaluatorPath string, args []string, input []byte, timeout time.Duration, evaluatorHost string) ([]byte, error) {
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, "/bin/bash", args...)
	cmd.Dir = filepath.Dir(evaluatorPath)
	cmd.Env = operationalValueEvaluatorEnvironment(os.Environ(), evaluatorHost)
	cmd.Stdin = bytes.NewReader(input)
	stdout := &boundedCommandBuffer{limit: maxGraderCommandOutputBytes}
	stderr := &boundedCommandBuffer{limit: maxGraderCommandOutputBytes}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("timed out after %s", timeout)
	}
	if stdout.exceeded || stderr.exceeded {
		return nil, fmt.Errorf("output exceeded the %d-byte limit", maxGraderCommandOutputBytes)
	}
	if err != nil {
		if message := strings.TrimSpace(stderr.String()); message != "" {
			return nil, errors.New(message)
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

func operationalValueEvaluatorEnvironment(environ []string, evaluatorHost string) []string {
	keys := []string{
		"PATH", "HOME", "TMPDIR", "TEMP", "TMP", "SystemRoot", "ComSpec",
		"GH_TOKEN", "GH_HOST", "GITHUB_API_URL", "GITHUB_GRAPHQL_URL", "GITHUB_SERVER_URL",
	}
	values := make(map[string]string, len(environ))
	for _, entry := range environ {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	hostURL, err := url.Parse(evaluatorHost)
	if err == nil && hostURL.Scheme != "" && hostURL.Host != "" {
		serverURL := strings.TrimSuffix(hostURL.String(), "/")
		values["GH_HOST"] = hostURL.Host
		values["GITHUB_SERVER_URL"] = serverURL
		if strings.EqualFold(hostURL.Hostname(), "github.com") {
			values["GITHUB_API_URL"] = "https://api.github.com"
			values["GITHUB_GRAPHQL_URL"] = "https://api.github.com/graphql"
		} else {
			values["GITHUB_API_URL"] = serverURL + "/api/v3"
			values["GITHUB_GRAPHQL_URL"] = serverURL + "/api/graphql"
		}
	}
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		if value := values[key]; value != "" {
			env = append(env, key+"="+value)
		}
	}
	return env
}

func parseGraderRunID(value string) (int64, error) {
	runID, err := strconv.ParseInt(value, 10, 64)
	if err != nil || runID <= 0 {
		return 0, errors.New("run ID must be a positive integer")
	}
	return runID, nil
}

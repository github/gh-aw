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
	"path"
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
	maxOperationalValueEvaluatorBytes = 64 * 1024
	maxOperationalValueOutputBytes    = 1024 * 1024
	graderJSTimeout                   = 7 * time.Second
	operationalValueEvaluatorTimeout  = 2 * time.Minute
	operationalValueSyntaxTimeout     = 5 * time.Second
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
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Source    string         `json:"source"`
	Unit      string         `json:"unit"`
	Direction string         `json:"direction"`
	Threshold *float64       `json:"threshold"`
	Config    map[string]any `json:"config,omitempty"`
	Run       string         `json:"-"`
	Script    string         `json:"script,omitempty"`
	Digest    string         `json:"digest,omitempty"`
}

func runGrader(ctx context.Context, config graderRunConfig) error {
	grader, err := loadGraderRunDefinition(config.Workflow, config.GraderID)
	if err != nil {
		return err
	}
	if grader.Source == "operational-value" && config.RunID != 0 {
		return errors.New("operational-value graders require a JSON request on standard input; historical replay is not supported")
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
		return runOperationalValuePayload(ctx, config.Workflow, grader, payload, config.Output, evaluatorHost)
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
		ID:        graderID,
		Name:      definition.Name,
		Source:    source,
		Unit:      definition.Unit,
		Direction: definition.Direction,
		Threshold: definition.Threshold,
		Config:    definition.Config,
		Run:       definition.Run,
		Script:    definition.Script,
		Digest:    definition.ScriptDigest(),
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
	stderr := &boundedCommandBuffer{limit: maxOperationalValueOutputBytes}
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

func runOperationalValuePayload(ctx context.Context, workflowArg string, grader graderRunDefinition, payload json.RawMessage, output io.Writer, evaluatorHost string) error {
	evaluatorContent, err := loadOperationalValueEvaluatorContent(workflowArg, grader)
	if err != nil {
		return err
	}
	tempDir, err := os.MkdirTemp("", "gh-aw-operational-value-evaluator-*")
	if err != nil {
		return fmt.Errorf("failed to stage operational-value evaluator: %w", err)
	}
	defer os.RemoveAll(tempDir)
	evaluatorPath := filepath.Join(tempDir, "operational-value.sh")
	if err := os.WriteFile(evaluatorPath, evaluatorContent, constants.FilePermExecutable); err != nil {
		return fmt.Errorf("failed to stage operational-value evaluator: %w", err)
	}
	if _, err := runOperationalValueEvaluatorBash(ctx, evaluatorPath, []string{"-n", evaluatorPath}, nil, operationalValueSyntaxTimeout, evaluatorHost); err != nil {
		return fmt.Errorf("operational-value evaluator has invalid Bash syntax: %w", err)
	}
	result, err := runOperationalValueEvaluatorBash(ctx, evaluatorPath, []string{evaluatorPath}, payload, operationalValueEvaluatorTimeout, evaluatorHost)
	if err != nil {
		return fmt.Errorf("operational-value evaluator failed: %w", err)
	}
	if err := validateOperationalValueMetrics(result); err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, string(result))
	return err
}

func loadOperationalValueEvaluatorContent(workflowArg string, grader graderRunDefinition) ([]byte, error) {
	if grader.Script != "" {
		return validateOperationalValueEvaluatorSource([]byte(grader.Script))
	}
	workflowPath, err := ResolveWorkflowPath(workflowArg)
	if err != nil {
		return nil, err
	}
	repoRoot, err := gitutil.FindGitRootFrom(filepath.Dir(workflowPath))
	if err != nil {
		return nil, errors.New("cannot resolve operational-value evaluator outside a Git repository")
	}
	evaluatorPath := workflow.ResolveOperationalValueEvaluatorPath(repoRoot, workflowPath, grader.Run)
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
	return validateOperationalValueEvaluatorSource(content)
}

func validateOperationalValueEvaluatorSource(content []byte) ([]byte, error) {
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

func validateOperationalValueMetrics(data []byte) error {
	var metrics []map[string]json.RawMessage
	if err := json.Unmarshal(data, &metrics); err != nil || len(metrics) == 0 {
		return errors.New("operational-value evaluator must return one non-empty JSON metric array")
	}
	ids := make(map[string]struct{}, len(metrics))
	for _, metric := range metrics {
		if len(metric) != 2 || metric["id"] == nil || metric["value"] == nil {
			return errors.New("operational-value evaluator metrics must contain exactly id and value")
		}
		var id string
		if err := json.Unmarshal(metric["id"], &id); err != nil || strings.TrimSpace(id) == "" {
			return errors.New("operational-value evaluator metric ids must be non-empty strings")
		}
		if _, exists := ids[id]; exists {
			return errors.New("operational-value evaluator metric ids must be unique")
		}
		ids[id] = struct{}{}
		if string(metric["value"]) == "null" {
			continue
		}
		var value float64
		if err := json.Unmarshal(metric["value"], &value); err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return errors.New("operational-value evaluator metric values must be null or finite numbers")
		}
	}
	return nil
}

type boundedCommandBuffer struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (buffer *boundedCommandBuffer) Write(data []byte) (int, error) {
	written := len(data)
	remaining := buffer.limit - buffer.Len()
	if remaining > 0 {
		remaining = min(len(data), remaining)
		_, _ = buffer.Buffer.Write(data[:remaining])
	}
	if written > remaining {
		buffer.exceeded = true
	}
	return written, nil
}

func runOperationalValueEvaluatorBash(ctx context.Context, evaluatorPath string, args []string, input []byte, timeout time.Duration, evaluatorHost string) ([]byte, error) {
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, "/bin/bash", args...)
	cmd.Dir = filepath.Dir(evaluatorPath)
	cmd.Env = operationalValueEvaluatorEnvironment(os.Environ(), evaluatorHost)
	cmd.Stdin = bytes.NewReader(input)
	stdout := &boundedCommandBuffer{limit: maxOperationalValueOutputBytes}
	stderr := &boundedCommandBuffer{limit: maxOperationalValueOutputBytes}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("timed out after %s", timeout)
	}
	if stdout.exceeded || stderr.exceeded {
		return nil, fmt.Errorf("output exceeded the %d-byte limit", maxOperationalValueOutputBytes)
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
	allowed := map[string]struct{}{
		"PATH": {}, "HOME": {}, "TMPDIR": {}, "TEMP": {}, "TMP": {},
		"SystemRoot": {}, "ComSpec": {}, "GH_TOKEN": {}, "GH_HOST": {},
		"GITHUB_API_URL": {}, "GITHUB_GRAPHQL_URL": {}, "GITHUB_SERVER_URL": {},
	}
	values := make(map[string]string)
	for _, entry := range environ {
		key, value, ok := strings.Cut(entry, "=")
		if _, allowedKey := allowed[key]; ok && allowedKey {
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
			apiURL := *hostURL
			apiURL.Path = path.Join(apiURL.Path, "api/v3")
			graphqlURL := *hostURL
			graphqlURL.Path = path.Join(graphqlURL.Path, "api/graphql")
			values["GITHUB_API_URL"] = apiURL.String()
			values["GITHUB_GRAPHQL_URL"] = graphqlURL.String()
		}
	}
	result := make([]string, 0, len(values))
	for key, value := range values {
		result = append(result, key+"="+value)
	}
	slices.Sort(result)
	return result
}

func parseGraderRunID(value string) (int64, error) {
	runID, err := strconv.ParseInt(value, 10, 64)
	if err != nil || runID <= 0 {
		return 0, errors.New("run ID must be a positive integer")
	}
	return runID, nil
}

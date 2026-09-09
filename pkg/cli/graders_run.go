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
	"slices"
	"strconv"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/repoutil"
	"github.com/github/gh-aw/pkg/stringutil"
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
}

type graderRunDefinition struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Source    string         `json:"source"`
	Unit      string         `json:"unit"`
	Direction string         `json:"direction"`
	Threshold *float64       `json:"threshold"`
	Config    map[string]any `json:"config,omitempty"`
	Script    string         `json:"script,omitempty"`
	Digest    string         `json:"digest,omitempty"`
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
		return runOperationalValuePayload(ctx, config.Workflow, payload, config.Output, evaluatorHost)
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
	stderr := &boundedCommandBuffer{limit: maxOperationalValueRegradeOutputBytes}
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

func runOperationalValuePayload(ctx context.Context, workflowArg string, payload json.RawMessage, output io.Writer, evaluatorHost string) error {
	evaluator, err := loadOperationalValueReportEvaluator(ctx, workflowArg, evaluatorHost)
	if err != nil {
		return err
	}
	defer evaluator.cleanup()
	result, err := runOperationalValueEvaluatorBash(ctx, "/bin/bash", evaluator.EvaluatorPath,
		[]string{evaluator.EvaluatorPath, "--grade-run"}, payload, operationalValueEvaluatorTimeout, evaluatorHost)
	if err != nil {
		return fmt.Errorf("operational-value evaluator --grade-run failed: %w", err)
	}
	if !json.Valid(result) {
		return errors.New("operational-value evaluator returned invalid JSON")
	}
	_, err = fmt.Fprintln(output, string(result))
	return err
}

func parseGraderRunID(value string) (int64, error) {
	runID, err := strconv.ParseInt(value, 10, 64)
	if err != nil || runID <= 0 {
		return 0, errors.New("run ID must be a positive integer")
	}
	return runID, nil
}

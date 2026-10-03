package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/workflow"
)

var sessionsDownloadLog = logger.New("cli:sessions_download")

func downloadSession(ctx context.Context, run *parser.GitHubURLComponents, format string, verbose bool) ([]byte, error) {
	root, err := os.MkdirTemp("", "gh-aw-session-download-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create session download directory: %w", err)
	}
	defer os.RemoveAll(root)

	hostname := resolveAuditHostname(run.Host)
	names, err := listRunArtifactNames(ctx, run.Number, run.Owner, run.Repo, hostname, verbose)
	if err != nil {
		return nil, err
	}
	usage, err := sessionArtifactName(names, "usage", "")
	if err != nil {
		return nil, err
	}
	sessionPath := filepath.Join(root, "usage", "aw_session.jsonl")
	if usage != "" {
		if err := downloadSessionArtifact(ctx, run, hostname, usage, filepath.Join(root, "usage"), verbose); err != nil {
			return nil, err
		}
		content, err := os.ReadFile(sessionPath)
		if err == nil {
			sessionsDownloadLog.Printf("Using published unified session for run %d", run.Number)
			return formatSession(ctx, content, sessionPath, format)
		}
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to read published session: %w", err)
		}
	}

	content, err := reconstructSession(ctx, run, hostname, names, root, verbose)
	if err != nil {
		return nil, err
	}
	if format == "markdown" {
		if err := os.MkdirAll(filepath.Dir(sessionPath), constants.DirPermSensitive); err != nil {
			return nil, fmt.Errorf("failed to create reconstructed session directory: %w", err)
		}
		if err := os.WriteFile(sessionPath, content, constants.FilePermSensitive); err != nil {
			return nil, fmt.Errorf("failed to write reconstructed session: %w", err)
		}
	}
	return formatSession(ctx, content, sessionPath, format)
}

func reconstructSession(ctx context.Context, run *parser.GitHubURLComponents, hostname string, names []string, root string, verbose bool) ([]byte, error) {
	sessionsDownloadLog.Printf("No published unified session for run %d; reconstructing from agent", run.Number)
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No aw_session.jsonl in usage; reconstructing from the agent artifact"))
	}
	agent, err := sessionArtifactName(names, constants.AgentArtifactName.String(), "agent-artifacts")
	if err != nil {
		return nil, err
	}
	if agent == "" {
		agent, err = sessionArtifactName(names, "agent-stdio-log", "agent-stdio.log")
		if err != nil {
			return nil, err
		}
	}
	if agent == "" {
		return nil, fmt.Errorf("run %d has no unified session in usage and no agent artifact or legacy agent log to reconstruct it from", run.Number)
	}
	agentDirectory, err := os.MkdirTemp(root, ".agent-artifact-")
	if err != nil {
		return nil, fmt.Errorf("failed to create agent artifact directory: %w", err)
	}
	if err := downloadSessionArtifact(ctx, run, hostname, agent, agentDirectory, verbose); err != nil {
		return nil, err
	}
	// A separate staging directory avoids deleting nested agent/graders evidence
	// when the artifact wrapper is removed after flattening.
	sourceDirectory := agentDirectory
	legacyDirectory := filepath.Join(agentDirectory, "tmp", "gh-aw")
	if info, err := os.Stat(legacyDirectory); err == nil && info.IsDir() {
		sourceDirectory = legacyDirectory
	} else if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to inspect agent artifact layout: %w", err)
	}
	if err := flattenArtifactTree(sourceDirectory, agentDirectory, root, "agent", verbose); err != nil {
		return nil, err
	}
	engine, err := sessionEngine(ctx, run, hostname, names, root, verbose)
	if err != nil {
		return nil, err
	}
	content, err := runSessionParser(ctx, "reconstruct", root, engine)
	if err != nil {
		return nil, fmt.Errorf("failed to reconstruct session for run %d: %w", run.Number, err)
	}
	return content, nil
}

// Prefer exact names over workflow_call prefixes, but never silently combine
// different called workflows' sessions.
func sessionArtifactName(names []string, base, legacy string) (string, error) {
	for _, name := range []string{base, legacy} {
		if name != "" && slices.Contains(names, name) {
			return name, nil
		}
	}
	var matches []string
	for _, name := range names {
		if strings.HasSuffix(name, "-"+base) || (legacy != "" && strings.HasSuffix(name, "-"+legacy)) {
			matches = append(matches, name)
		}
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("multiple %s artifacts found: %s; use the individual workflow run ID", base, strings.Join(matches, ", "))
	}
	if len(matches) == 1 {
		return matches[0], nil //nolint:uncheckedsliceindex // len(matches) == 1
	}
	return "", nil
}

func downloadSessionArtifact(ctx context.Context, run *parser.GitHubURLComponents, hostname, name, directory string, verbose bool) error {
	if err := validateArtifactName(name); err != nil {
		return err
	}
	args := []string{"run", "download", strconv.FormatInt(run.Number, 10), "--name", name, "--dir", directory}
	if repoFlag := buildRepoFlag(run.Owner, run.Repo, hostname); repoFlag != "" {
		args = append(args, "-R", repoFlag)
	}
	sessionsDownloadLog.Printf("Downloading session artifact: gh %s", strings.Join(args, " "))
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Downloading artifact: "+name))
	}
	output, err := workflow.ExecGHContext(ctx, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to download %s artifact for run %d: %w: %s", name, run.Number, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func sessionEngine(ctx context.Context, run *parser.GitHubURLComponents, hostname string, names []string, root string, verbose bool) (string, error) {
	for _, file := range []string{filepath.Join(root, "aw_info.json"), filepath.Join(root, "usage", "aw_info.json")} {
		info, err := parseAwInfo(file, false)
		if err == nil {
			return info.EngineID, nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("failed to read session engine metadata: %w", err)
		}
	}
	for _, artifact := range []struct{ base, legacy string }{
		{"info", ""}, {"aw-info", "aw_info"}, {"activation", ""},
	} {
		name, err := sessionArtifactName(names, artifact.base, artifact.legacy)
		if err != nil {
			return "", err
		}
		if name == "" {
			continue
		}
		directory := filepath.Join(root, artifact.base)
		if err := downloadSessionArtifact(ctx, run, hostname, name, directory, verbose); err != nil {
			return "", err
		}
		if err := flattenArtifactTree(directory, directory, root, artifact.base, verbose); err != nil {
			return "", err
		}
		info, err := parseAwInfo(filepath.Join(root, "aw_info.json"), false)
		if err == nil {
			return info.EngineID, nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("failed to read session engine metadata: %w", err)
		}
	}
	engineID, err := inferSessionEngineFromLogs(root)
	if err != nil {
		return "", err
	}
	if engineID != "" {
		sessionsDownloadLog.Printf("Inferred session engine from agent logs using audit parsers: %s", engineID)
		return engineID, nil
	}
	// The collector can read canonical events without engine metadata and can
	// recognize Pi's dedicated stream. Other logs use its custom-engine parser.
	sessionsDownloadLog.Print("No engine metadata available; using the unified collector's source detection")
	return "", nil
}

func inferSessionEngineFromLogs(root string) (string, error) {
	if _, engineID := inferFallbackLogMetrics(root); engineID != "" {
		return engineID, nil
	}
	// Older Copilot artifacts retain debug process logs even when their stdio
	// stream is plain text and aw_info.json was not uploaded.
	logPath, found := findAgentLogFile(root, workflow.NewCopilotEngine())
	if !found {
		return "", nil
	}
	content, err := os.ReadFile(logPath)
	if err != nil {
		return "", fmt.Errorf("failed to read agent log for engine inference: %w", err)
	}
	_, engineID := inferBestEngineMetricsFromContent(string(content))
	return engineID, nil
}

func formatSession(ctx context.Context, content []byte, sessionPath, format string) ([]byte, error) {
	if err := validateSessionJSONL(content); err != nil {
		return nil, err
	}
	if format == "markdown" {
		return runSessionParser(ctx, "markdown", sessionPath)
	}
	return content, nil
}

func validateSessionJSONL(content []byte) error {
	records := 0
	for index, line := range bytes.Split(content, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var event struct {
			Type       string          `json:"type"`
			Data       json.RawMessage `json:"data"`
			Provenance struct {
				Component string `json:"component"`
			} `json:"provenance"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			return fmt.Errorf("invalid session JSONL at line %d: %w", index+1, err)
		}
		data := bytes.TrimSpace(event.Data)
		if !strings.Contains(event.Type, ".") || len(data) == 0 || data[0] != '{' { //nolint:uncheckedsliceindex // len(data) > 0
			return fmt.Errorf("invalid session event at line %d: expected a namespaced type and object data", index+1)
		}
		isHeader := event.Type == "session.format" && event.Provenance.Component == "collector"
		if records == 0 {
			if !isHeader {
				return errors.New("unified session is missing its leading session.format header")
			}
			var header struct {
				Version int `json:"version"`
			}
			if err := json.Unmarshal(data, &header); err != nil {
				return fmt.Errorf("invalid unified session file-format header: %w", err)
			}
			if header.Version != 1 {
				return fmt.Errorf("unsupported unified session file-format version: %d", header.Version)
			}
		} else if isHeader {
			return errors.New("unified session contains multiple collector format headers")
		}
		records++
	}
	if records == 0 {
		return errors.New("unified session is empty")
	}
	return nil
}

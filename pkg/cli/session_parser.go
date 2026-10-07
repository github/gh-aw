package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/github/gh-aw/actions/setup"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/workflow"
)

var errNoRecognizableAgentSession = errors.New("no recognizable agent session")

type agentExecutionData struct {
	Categories []string          `json:"categories"`
	ErrorCodes []json.RawMessage `json:"errorCodes"`
	ErrorTypes []string          `json:"errorTypes"`
	ExitCode   *int              `json:"exitCode"`
}

func parseAgentExecution(data json.RawMessage) (*agentExecutionData, error) {
	var payload struct {
		agentExecutionData
		ExitCode json.RawMessage `json:"exitCode"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("invalid agent.execution data: %w", err)
	}
	execution := payload.agentExecutionData
	if execution.Categories == nil || execution.ErrorTypes == nil || execution.ErrorCodes == nil {
		return nil, errors.New("agent.execution requires categories, errorCodes, and errorTypes arrays")
	}
	for _, values := range [][]string{execution.Categories, execution.ErrorTypes} {
		seen := make(map[string]struct{})
		for _, value := range values {
			_, duplicate := seen[value]
			if value == "" || duplicate {
				return nil, errors.New("agent.execution categories and errorTypes must contain unique nonempty strings")
			}
			seen[value] = struct{}{}
		}
	}
	seenCodes := make(map[string]struct{})
	for _, raw := range execution.ErrorCodes {
		var text string
		key := ""
		if err := json.Unmarshal(raw, &text); err == nil && text != "" {
			key = "string:" + text
		} else if number, valid := parseAgentExecutionNumber(raw); valid {
			key = "number:" + strconv.FormatInt(int64(number), 10)
		}
		_, duplicate := seenCodes[key]
		if key == "" || duplicate {
			return nil, errors.New("agent.execution errorCodes must contain unique nonempty strings or safe integers")
		}
		seenCodes[key] = struct{}{}
	}
	if payload.ExitCode != nil {
		number, valid := parseAgentExecutionNumber(payload.ExitCode)
		if !valid || number < 0 || number > 255 {
			return nil, errors.New("agent.execution exitCode must be an integer between 0 and 255")
		}
		exitCode := int(number)
		execution.ExitCode = &exitCode
	}
	return &execution, nil
}

func parseAgentExecutionNumber(raw json.RawMessage) (float64, bool) {
	number, err := strconv.ParseFloat(string(raw), 64)
	return number, err == nil && !math.IsNaN(number) && math.Abs(number) <= 9007199254740991 && math.Trunc(number) == number
}

func runSessionParser(ctx context.Context, args ...string) ([]byte, error) {
	return runSessionParserWithSources(ctx, nil, args...)
}

func runSessionParserWithSources(ctx context.Context, additionalSources map[string][]byte, args ...string) ([]byte, error) {
	additionalSources, err := sessionParserEngineSources(additionalSources, args)
	if err != nil {
		return nil, err
	}
	tempDir, err := os.MkdirTemp("", "gh-aw-session-parser-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create session parser directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	if err := fs.WalkDir(setup.SessionParserSources, "js", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		content, err := setup.SessionParserSources.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(tempDir, entry.Name()), content, constants.FilePermSensitive)
	}); err != nil {
		return nil, fmt.Errorf("failed to prepare session parser sources: %w", err)
	}
	for name, content := range additionalSources {
		if filepath.Base(name) != name || name == "." || name == ".." {
			return nil, fmt.Errorf("invalid additional session parser source name %q", name)
		}
		if err := os.WriteFile(filepath.Join(tempDir, name), content, constants.FilePermSensitive); err != nil {
			return nil, fmt.Errorf("failed to prepare additional session parser source %s: %w", name, err)
		}
	}

	cmd := exec.CommandContext(ctx, "node", append([]string{filepath.Join(tempDir, "session_cli.cjs")}, args...)...)
	cmd.Dir = tempDir
	var diagnostics bytes.Buffer
	cmd.Stderr = &diagnostics
	output, err := cmd.Output()
	if diagnostics.Len() > 0 {
		if _, writeErr := fmt.Fprint(os.Stderr, diagnostics.String()); writeErr != nil {
			return nil, fmt.Errorf("failed to write session parser diagnostics: %w", writeErr)
		}
	}
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) && exitError.ExitCode() == 2 {
			return nil, fmt.Errorf("%w: %s", errNoRecognizableAgentSession, diagnostics.String())
		}
		return nil, fmt.Errorf("failed to execute session parser (Node.js is required): %w: %s", err, diagnostics.String())
	}

	return output, nil
}

func sessionParserEngineSources(additionalSources map[string][]byte, args []string) (map[string][]byte, error) {
	engineID := ""
	for index, value := range args {
		if index == 2 {
			engineID = value
			break
		}
	}
	if engineID == "" {
		return additionalSources, nil
	}
	var engine *workflow.BehaviorDefinedEngine
	if registered, err := workflow.GetGlobalEngineRegistry().GetEngine(engineID); err == nil {
		if behavior, ok := registered.(*workflow.BehaviorDefinedEngine); ok {
			engine = behavior
		}
	} else {
		var loadErr error
		engine, loadErr = loadLocalLogParserEngine(engineID)
		if loadErr != nil {
			return nil, fmt.Errorf("failed to load engine log parser: %w", loadErr)
		}
	}
	if engine == nil {
		return additionalSources, nil
	}
	sources := make(map[string][]byte, len(additionalSources)+1)
	maps.Copy(sources, additionalSources)
	sources[engine.GetID()+"_log_parser.cjs"] = []byte(engine.GetLogParserScriptSource())
	return sources, nil
}

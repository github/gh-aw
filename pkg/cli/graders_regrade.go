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
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/github/gh-aw/actions/setup"
	"github.com/github/gh-aw/pkg/constants"
)

// RegradedRun keeps prospective scores separate from immutable downloaded evidence.
// Results are the runtime's complete records, including implementation provenance.
type RegradedRun struct {
	Version          int                       `json:"version"`
	CLIVersion       string                    `json:"cli_version"`
	Identity         string                    `json:"identity"`
	IdentityMissing  []string                  `json:"identity_missing,omitempty"`
	Run              WorkflowRun               `json:"run"`
	SourceDirectory  string                    `json:"source_directory"`
	SessionStatus    string                    `json:"session_status"`
	SessionError     string                    `json:"session_error,omitempty"`
	SessionPath      string                    `json:"session_path,omitempty"`
	SessionDigest    string                    `json:"session_digest,omitempty"`
	PayloadDigest    string                    `json:"payload_digest,omitempty"`
	Definitions      []RegradeDefinition       `json:"definitions"`
	Results          []json.RawMessage         `json:"results"`
	OriginalGraders  json.RawMessage           `json:"original_graders,omitempty"`
	OriginalEvidence []RegradeOriginalEvidence `json:"original_evidence"`
}

// RegradeDefinition identifies the resolved definition without publishing code.
type RegradeDefinition struct {
	Selector         string         `json:"selector"`
	Workflow         string         `json:"workflow,omitempty"`
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	Description      string         `json:"description,omitempty"`
	Source           string         `json:"source"`
	Unit             string         `json:"unit"`
	Direction        string         `json:"direction"`
	Threshold        *float64       `json:"threshold"`
	Config           map[string]any `json:"config,omitempty"`
	DefinitionDigest string         `json:"definition_digest"`
	WorkflowDigest   string         `json:"workflow_digest,omitempty"`
	ConfigDigest     string         `json:"config_digest"`
	ScriptDigest     string         `json:"script_digest,omitempty"`
	RuntimeDigest    string         `json:"runtime_digest"`
}

type RegradeOriginalEvidence struct {
	Path   string          `json:"path"`
	Field  string          `json:"field,omitempty"`
	Digest string          `json:"digest,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// loadAdditionalGraders accepts repeatable and comma-separated workflow:grader selectors.
func loadAdditionalGraders(selectors []string) ([]graderRunDefinition, error) {
	definitions := make([]graderRunDefinition, 0)
	seen := make(map[string]struct{})
	selectedIDs := make(map[string]string)
	for _, list := range selectors {
		for selector := range strings.SplitSeq(list, ",") {
			selector = strings.TrimSpace(selector)
			index := strings.LastIndex(selector, ":")
			if index <= 0 || index == len(selector)-1 {
				return nil, fmt.Errorf("invalid additional grader %q: expected workflow:grader", selector)
			}
			definition, err := loadGraderRunDefinitionMode(selector[:index], selector[index+1:], true)
			if err != nil {
				return nil, err
			}
			if err := validateRegradeDefinition(definition); err != nil {
				return nil, err
			}
			definition.Workflow, err = filepath.Abs(definition.Workflow)
			if err != nil {
				return nil, err
			}
			definition.Selector = definition.Workflow + ":" + definition.ID
			if _, exists := seen[definition.Selector]; !exists {
				if previous, exists := selectedIDs[definition.ID]; exists {
					return nil, fmt.Errorf("additional grader ID %q is selected from both %q and %q; use distinct grader IDs", definition.ID, previous, definition.Selector)
				}
				selectedIDs[definition.ID] = definition.Selector
				seen[definition.Selector] = struct{}{}
				definitions = append(definitions, definition)
			}
		}
	}
	slices.SortFunc(definitions, func(a, b graderRunDefinition) int {
		return strings.Compare(a.Selector, b.Selector)
	})
	return definitions, nil
}

func validateRegradeDefinition(definition graderRunDefinition) error {
	if definition.Run != "" || definition.ShellScript != "" || definition.Source == "operational-value" {
		return fmt.Errorf("grader %q uses Bash; collection regrading supports only built-in and isolated JavaScript graders", definition.ID)
	}
	if definition.ID == "" || (definition.Source != "builtin" && definition.Source != "inline") ||
		(definition.Source == "builtin" && definition.Script != "") {
		return fmt.Errorf("grader %q has an unsupported collection definition", definition.ID)
	}
	return nil
}

// regradeProcessedRuns performs no evidence writes and no network requests.
// Invalid/missing sessions and grader execution errors are represented per run;
// invalid definitions, unsafe source paths and cancellation fail the collection.
func regradeProcessedRuns(ctx context.Context, outputDir string, runs []ProcessedRun, definitions []graderRunDefinition) ([]RegradedRun, error) {
	definitions = slices.Clone(definitions)
	selectedIDs := make(map[string]struct{})
	for _, definition := range definitions {
		if err := validateRegradeDefinition(definition); err != nil {
			return nil, err
		}
		if _, exists := selectedIDs[definition.ID]; exists {
			return nil, fmt.Errorf("duplicate collection grader ID %q; use distinct grader IDs", definition.ID)
		}
		selectedIDs[definition.ID] = struct{}{}
	}
	slices.SortFunc(definitions, func(a, b graderRunDefinition) int {
		return strings.Compare(regradeDefinitionSelector(a), regradeDefinitionSelector(b))
	})
	for index := 1; index < len(definitions); index++ {
		if regradeDefinitionSelector(definitions[index-1]) == regradeDefinitionSelector(definitions[index]) { //nolint:uncheckedsliceindex // The loop bounds guard both adjacent indexes.
			return nil, fmt.Errorf("duplicate collection grader selector %q", regradeDefinitionSelector(definitions[index])) //nolint:uncheckedsliceindex // index is less than len(definitions).
		}
	}
	metadata, err := regradeDefinitionsMetadata(definitions)
	if err != nil {
		return nil, err
	}
	selected, identities := harborUniqueRuns(runs)
	for _, run := range runs {
		identity := harborRunIdentity(run)
		if selected[identity].Run.LogsPath != run.Run.LogsPath {
			return nil, fmt.Errorf("run identity %s has conflicting evidence directories", identity)
		}
	}
	result := make([]RegradedRun, 0, len(identities))
	for _, identity := range identities {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		run, err := regradeOneProcessedRun(ctx, outputDir, selected[identity], definitions, metadata)
		if err != nil {
			return nil, err
		}
		result = append(result, run)
	}
	return result, ctx.Err()
}

func regradeOneProcessedRun(ctx context.Context, outputDir string, processed ProcessedRun, definitions []graderRunDefinition, metadata []RegradeDefinition) (RegradedRun, error) {
	source := processed.Run.LogsPath
	if source == "" {
		source = resolveAuditOutputDir(outputDir, processed.Run.DatabaseID)
	}
	if err := regradeCheckSourcePath(source); err != nil {
		return RegradedRun{}, err
	}
	run := RegradedRun{
		Version: 1, CLIVersion: GetVersion(), Identity: harborRunIdentity(processed), Run: processed.Run,
		IdentityMissing: regradeIdentityMissing(processed),
		SourceDirectory: source, SessionStatus: "missing", Definitions: metadata,
		Results:          make([]json.RawMessage, 0, len(definitions)),
		OriginalEvidence: make([]RegradeOriginalEvidence, 0),
	}
	regradeReadOriginalEvidence(&run)
	payload, err := regradeSessionPayload(ctx, source, &run)
	if err != nil {
		run.SessionError = err.Error()
	}
	for _, definition := range definitions {
		if err := ctx.Err(); err != nil {
			return RegradedRun{}, err
		}
		if run.SessionStatus != "recorded" {
			run.Results = append(run.Results, regradeErrorResult(definition, "unavailable", run.SessionError))
			continue
		}
		run.Results = append(run.Results, regradeSessionResult(ctx, definition, payload))
	}
	return run, nil
}

func regradeSessionResult(ctx context.Context, definition graderRunDefinition, payload json.RawMessage) json.RawMessage {
	var output bytes.Buffer
	executionErr := runSessionGrader(ctx, definition, payload, &output, "")
	raw := bytes.TrimSpace(output.Bytes())
	var record struct {
		ID             string          `json:"id"`
		Status         string          `json:"status"`
		Implementation json.RawMessage `json:"implementation"`
	}
	if json.Unmarshal(raw, &record) == nil && record.ID == definition.ID &&
		validRegradeResultStatus(record.Status) && len(record.Implementation) > 0 &&
		(executionErr == nil || record.Status == "error") {
		return json.RawMessage(bytes.Clone(raw))
	}
	if executionErr == nil {
		executionErr = errors.New("grader runtime returned an invalid result")
	}
	return regradeErrorResult(definition, "error", regradeExecutionError(executionErr))
}

func validRegradeResultStatus(status string) bool {
	switch status {
	case "pass", "fail", "error", "unavailable":
		return true
	default:
		return false
	}
}

func regradeExecutionError(err error) string {
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		// Infrastructure stderr can contain process IDs and random staging paths.
		// Runtime-emitted grader errors are preserved separately, without rewriting.
		return fmt.Sprintf("grader runtime exited with status %d", exitError.ExitCode())
	}
	return err.Error()
}

func regradeDefinitionSelector(definition graderRunDefinition) string {
	if definition.Selector != "" {
		return definition.Selector
	}
	return definition.Workflow + ":" + definition.ID
}

func regradeDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func regradeDefinitionsMetadata(definitions []graderRunDefinition) ([]RegradeDefinition, error) {
	runtime := sha256.New()
	runtime.Write(gradersRunScript)
	for _, name := range []string{"trace_graders.cjs", "grader_session.cjs", "error_codes.cjs", "trajectory_graders.cjs", "failure_graders.cjs", "failure_observations.cjs", "builtin_graders.json", "working_set_metrics.cjs", "custom_graders.cjs", "custom_grader_worker.cjs"} {
		content, err := setup.SessionParserSources.ReadFile("js/" + name)
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(runtime, "\x00"+name+"\x00"); err != nil {
			return nil, err
		}
		runtime.Write(content)
	}
	metadata := make([]RegradeDefinition, 0, len(definitions))
	for _, definition := range definitions {
		data, err := json.Marshal(definition)
		if err != nil {
			return nil, fmt.Errorf("encode grader %q definition: %w", definition.ID, err)
		}
		config, err := json.Marshal(definition.Config)
		if err != nil {
			return nil, err
		}
		item := RegradeDefinition{
			Selector: regradeDefinitionSelector(definition), Workflow: definition.Workflow,
			ID: definition.ID, Name: definition.Name, Description: definition.Description,
			Source: definition.Source, Unit: definition.Unit, Direction: definition.Direction,
			Threshold: definition.Threshold, Config: definition.Config,
			DefinitionDigest: regradeDigest(data), ConfigDigest: regradeDigest(config),
			RuntimeDigest: hex.EncodeToString(runtime.Sum(nil)),
		}
		if definition.Script != "" {
			item.ScriptDigest = regradeDigest([]byte(definition.Script))
		}
		if definition.Workflow != "" {
			content, err := os.ReadFile(definition.Workflow)
			if err != nil {
				return nil, fmt.Errorf("read grader workflow provenance: %w", err)
			}
			item.WorkflowDigest = regradeDigest(content)
		}
		metadata = append(metadata, item)
	}
	return metadata, nil
}

func regradeErrorResult(definition graderRunDefinition, status, message string) json.RawMessage {
	data, err := json.Marshal(map[string]any{
		"id": definition.ID, "name": definition.Name, "source": definition.Source,
		"status": status, "value": nil, "error": message,
		"implementation": map[string]any{"id": "gh-aw/offline-regrading", "version": 1, "cli_version": GetVersion()},
	})
	if err != nil {
		return json.RawMessage(`{"id":"offline-regrading","status":"error","value":null,"error":"cannot encode offline grader failure","implementation":{"id":"gh-aw/offline-regrading","version":1}}`)
	}
	return data
}

func regradeSessionPayload(ctx context.Context, source string, run *RegradedRun) (json.RawMessage, error) {
	path, err := findRegradeSessionPath(source)
	if err != nil {
		run.SessionStatus = "invalid"
		return nil, err
	}
	if path == "" {
		return nil, errors.New("unified session evidence is missing (aw_session.jsonl)")
	}
	run.SessionPath = path
	run.SessionStatus = "invalid"
	data, err := regradeReadEvidence(path, maxGraderPayloadBytes)
	if err != nil {
		return nil, err
	}
	run.SessionDigest = regradeDigest(data)
	normalized := data
	if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("[")) {
		if err := regradeCheckSourcePath(filepath.Join(source, "aw_info.json")); err != nil {
			return nil, err
		}
		normalized, err = normalizeATIFSession(ctx, data, path, source)
		if err != nil {
			return nil, err
		}
	}
	payload, err := readGraderPayload(bytes.NewReader(normalized), path)
	if err != nil {
		return nil, err
	}
	var events []json.RawMessage
	if err := json.Unmarshal(payload, &events); err != nil || len(events) == 0 {
		return nil, errors.New("unified session contains no recorded events")
	}
	if err := regradeValidateSessionIdentity(events, source, run.Run); err != nil {
		return nil, err
	}
	run.PayloadDigest = regradeDigest(payload)
	run.SessionStatus = "recorded"
	return payload, nil
}

func regradeReadOriginalEvidence(run *RegradedRun) {
	roots, err := regradeEvidenceRoots(run.SourceDirectory)
	if err != nil {
		run.OriginalEvidence = append(run.OriginalEvidence, RegradeOriginalEvidence{
			Path: filepath.Join(run.SourceDirectory, ".episode-artifacts.json"), Error: err.Error(),
		})
		return
	}
	for _, name := range []string{constants.GraderResultsFilename.String(), constants.GraderManifestFilename.String()} {
		path := regradeFindOriginalFile(roots, name)
		if path == "" {
			continue
		}
		item := RegradeOriginalEvidence{Path: path}
		data, err := regradeReadEvidence(path, maxGraderResultsBytes)
		if err == nil {
			item.Digest = regradeDigest(data)
			if !json.Valid(data) {
				err = errors.New("original grader evidence is invalid JSON")
			}
		}
		if err != nil {
			item.Error = err.Error()
		} else {
			item.Data = json.RawMessage(data)
			if name == constants.GraderResultsFilename.String() {
				run.OriginalGraders = item.Data
			}
		}
		run.OriginalEvidence = append(run.OriginalEvidence, item)
	}
}

func regradeReadEvidence(path string, limit int64) ([]byte, error) {
	if err := regradeCheckSourcePath(path); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("evidence %s must be a regular file", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, limit+1))
	if err := errors.Join(readErr, file.Close()); err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("evidence %s exceeds the %d-byte limit", path, limit)
	}
	return data, nil
}

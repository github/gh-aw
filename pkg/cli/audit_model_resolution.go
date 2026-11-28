package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

// Model resolution records are runner-written before the engine starts (T-UAS-074).
// Identifiers are validated before they are quoted into findings.
var (
	modelResolutionEnginePattern   = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
	modelResolutionRolePattern     = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9 ._:@/-]{0,127}$`)
	modelResolutionModelPattern    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/?=&+:@-]{0,199}$`)
	modelResolutionProviderPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
	modelResolutionCausePattern    = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
)

const (
	modelResolutionCauseModelUnavailable    = "model_unavailable"
	modelResolutionCauseUnsupportedEndpoint = "unsupported_endpoint"
)

// sessionModelResolutionFailure is one failed model.resolution record. The record
// does not persist whether the role was a declared sub-agent or an optional
// repository agent, so it cannot by itself establish why a run failed.
type sessionModelResolutionFailure struct {
	Engine          string
	Role            string
	ConfiguredModel string
	Provider        string
	Cause           string
}

type sessionAgentExecutionSummary struct {
	Categories []string `json:"categories"`
	ExitCode   *int     `json:"exitCode"`
}

// maxModelResolutionSessionBytes bounds the unified session read for model resolution evidence.
const maxModelResolutionSessionBytes int64 = 256 << 20

// sessionModelResolutionEvidence holds failed resolution records plus the startup
// and continuation evidence needed to decide whether they explain the run outcome.
type sessionModelResolutionEvidence struct {
	Failures []sessionModelResolutionFailure
	// NativeAgentActivity is true when the unified session contains native agent
	// session.start, session.init, assistant, or tool events. Wrapper state, router
	// traffic, and harness-written agent records are not native activity.
	NativeAgentActivity bool
	AgentExecution      *sessionAgentExecutionSummary
	agentExecutionSeen  bool
}

type sessionModelResolutionPayload struct {
	Engine          string           `json:"engine"`
	Role            string           `json:"role"`
	ConfiguredModel string           `json:"configured_model"`
	ResolvedModel   json.RawMessage  `json:"resolved_model"`
	Provider        *json.RawMessage `json:"provider"`
	Status          string           `json:"status"`
	Cause           *string          `json:"cause"`
}

func readSessionModelResolutionEvidence(runDir string) (*sessionModelResolutionEvidence, error) {
	if runDir == "" {
		return nil, nil
	}
	const relative = "usage/aw_session.jsonl"
	path := filepath.Join(runDir, constants.UsageArtifactName.String(), "aw_session.jsonl")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", relative, err)
	}
	if err := validateSubagentSessionSource(path); err != nil {
		return nil, fmt.Errorf("%s: %w", relative, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: unified session is not a regular file", relative)
	}
	if info.Size() > maxModelResolutionSessionBytes {
		return nil, fmt.Errorf("%s: unified session exceeds the %d-byte audit input limit", relative, maxModelResolutionSessionBytes)
	}
	evidence, err := parseSessionModelResolutionFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", relative, err)
	}
	return evidence, nil
}

func parseSessionModelResolutionFile(path string) (evidence *sessionModelResolutionEvidence, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			evidence, err = nil, closeErr
		}
	}()
	return parseSessionModelResolutionEvidence(file, maxModelResolutionSessionBytes)
}

// parseSessionModelResolutionEvidence reads at most maxBytes of the unified session.
// A larger session is rejected as invalid rather than partially attributed.
func parseSessionModelResolutionEvidence(input io.Reader, maxBytes int64) (*sessionModelResolutionEvidence, error) {
	limited := &io.LimitedReader{R: input, N: maxBytes + 1}
	reader := bufio.NewReader(limited)
	evidence := &sessionModelResolutionEvidence{}
	seen := make(map[sessionModelResolutionFailure]struct{})
	headerSeen := false
	for lineNumber := 1; ; lineNumber++ {
		line, oversized, readErr := readUnifiedSessionLine(reader)
		if limited.N == 0 {
			return nil, fmt.Errorf("unified session exceeds the %d-byte audit input limit", maxBytes)
		}
		if oversized {
			if !headerSeen {
				return nil, fmt.Errorf("oversized unified session header on line %d", lineNumber)
			}
			auditReportLog.Printf("Skipping oversized unified session record on line %d", lineNumber)
		} else if line = bytes.TrimSpace(line); len(line) > 0 {
			if !headerSeen {
				var header unifiedDetectionEvent
				if err := json.Unmarshal(line, &header); err != nil {
					return nil, fmt.Errorf("invalid unified session header on line %d", lineNumber)
				}
				if err := validateUnifiedDetectionHeader(header); err != nil {
					return nil, err
				}
				headerSeen = true
			} else if err := evidence.observe(line, lineNumber, seen); err != nil {
				return nil, err
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return nil, fmt.Errorf("failed to read unified session: %w", readErr)
		}
	}
	if !headerSeen {
		return nil, errors.New("unified session is missing its leading session.format header")
	}
	return evidence, nil
}

func (evidence *sessionModelResolutionEvidence) observe(line []byte, lineNumber int, seen map[sessionModelResolutionFailure]struct{}) error {
	var event unifiedDetectionEvent
	if err := json.Unmarshal(line, &event); err != nil {
		auditReportLog.Printf("Skipping malformed unified session event on line %d", lineNumber)
		return nil
	}
	provenance := event.Provenance
	switch {
	case event.Type == "model.resolution":
		if provenance.Component != "model_resolution" || provenance.Phase != "agent" || provenance.Path != "agent/model-resolution.json" {
			return nil
		}
		failure, failed, valid := decodeFailedModelResolution(event.Data)
		if !valid {
			auditReportLog.Printf("Ignoring invalid model.resolution on line %d", lineNumber)
			return nil
		}
		if !failed {
			return nil
		}
		if _, duplicate := seen[failure]; duplicate {
			return nil
		}
		seen[failure] = struct{}{}
		evidence.Failures = append(evidence.Failures, failure)
	case event.Type == "agent.execution" && provenance.Component == "execution" && provenance.Phase == "agent":
		// The unified-session contract allows one agent.execution summary; a duplicate
		// would make attribution order-dependent, so the session is rejected.
		if evidence.agentExecutionSeen {
			return fmt.Errorf("unified session contains multiple agent.execution records (line %d)", lineNumber)
		}
		evidence.agentExecutionSeen = true
		var summary sessionAgentExecutionSummary
		if json.Unmarshal(event.Data, &summary) == nil && summary.ExitCode != nil {
			evidence.AgentExecution = &summary
		}
	case provenance.Component == "agent" && provenance.Phase == "agent" &&
		(event.Type == "session.start" || event.Type == "session.init" ||
			strings.HasPrefix(event.Type, "assistant.") || strings.HasPrefix(event.Type, "tool.")):
		evidence.NativeAgentActivity = true
	}
	return nil
}

// decodeFailedModelResolution validates a model.resolution payload against the
// T-UAS-074 contract. It reports failed=true only for status "failed" records.
func decodeFailedModelResolution(data json.RawMessage) (sessionModelResolutionFailure, bool, bool) {
	var payload sessionModelResolutionPayload
	if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) || json.Unmarshal(data, &payload) != nil {
		return sessionModelResolutionFailure{}, false, false
	}
	if payload.Status != "failed" {
		return sessionModelResolutionFailure{}, false, true
	}
	if !modelResolutionEnginePattern.MatchString(payload.Engine) ||
		!modelResolutionRolePattern.MatchString(payload.Role) ||
		!modelResolutionModelPattern.MatchString(payload.ConfiguredModel) ||
		payload.Cause == nil || !modelResolutionCausePattern.MatchString(*payload.Cause) ||
		!bytes.Equal(bytes.TrimSpace(payload.ResolvedModel), []byte("null")) {
		return sessionModelResolutionFailure{}, false, false
	}
	provider := ""
	if payload.Provider != nil && !bytes.Equal(bytes.TrimSpace(*payload.Provider), []byte("null")) {
		if json.Unmarshal(*payload.Provider, &provider) != nil || !modelResolutionProviderPattern.MatchString(provider) {
			return sessionModelResolutionFailure{}, false, false
		}
	}
	return sessionModelResolutionFailure{
		Engine:          payload.Engine,
		Role:            payload.Role,
		ConfiguredModel: payload.ConfiguredModel,
		Provider:        provider,
		Cause:           *payload.Cause,
	}, true, true
}

// modelResolutionFailureGroup lists the roles that failed on one engine/provider/model/cause.
type modelResolutionFailureGroup struct {
	Engine          string
	Provider        string
	ConfiguredModel string
	Cause           string
	Roles           []string
}

func groupModelResolutionFailures(failures []sessionModelResolutionFailure) []modelResolutionFailureGroup {
	var groups []*modelResolutionFailureGroup
	for _, failure := range failures {
		var group *modelResolutionFailureGroup
		for _, candidate := range groups {
			if candidate.Engine == failure.Engine && candidate.Provider == failure.Provider &&
				candidate.ConfiguredModel == failure.ConfiguredModel && candidate.Cause == failure.Cause {
				group = candidate
				break
			}
		}
		if group == nil {
			group = &modelResolutionFailureGroup{
				Engine: failure.Engine, Provider: failure.Provider,
				ConfiguredModel: failure.ConfiguredModel, Cause: failure.Cause,
			}
			groups = append(groups, group)
		}
		if !slices.Contains(group.Roles, failure.Role) {
			group.Roles = append(group.Roles, failure.Role)
		}
	}
	result := make([]modelResolutionFailureGroup, 0, len(groups))
	for _, group := range groups {
		result = append(result, *group)
	}
	return result
}

func (group modelResolutionFailureGroup) describe() string {
	roles := make([]string, 0, len(group.Roles))
	for _, role := range group.Roles {
		roles = append(roles, fmt.Sprintf("%q", role))
	}
	provider := group.Provider
	if provider == "" {
		provider = "(not recorded)"
	}
	return fmt.Sprintf("engine %s role(s) %s configured model %q (provider %s, recorded cause %s)",
		group.Engine, strings.Join(roles, ", "), group.ConfiguredModel, provider, group.Cause)
}

func describeModelResolutionGroups(groups []modelResolutionFailureGroup) string {
	descriptions := make([]string, 0, len(groups))
	for _, group := range groups {
		descriptions = append(descriptions, group.describe())
	}
	return strings.Join(descriptions, "; ")
}

// modelResolutionContinuationReason explains why failed records are not attributed
// as the run's failure cause. It returns "" only when the run-level startup evidence
// permits a fatal startup stop: a failed run, a failed agent execution, and no native
// agent activity. Each role still needs its own stop evidence (modelResolutionStopEvidence).
func modelResolutionContinuationReason(run WorkflowRun, evidence *sessionModelResolutionEvidence) string {
	switch {
	case run.Conclusion != "failure":
		conclusion := run.Conclusion
		if conclusion == "" {
			conclusion = "unknown"
		}
		return fmt.Sprintf("The run conclusion is %q, so these records are not treated as a failure cause.", conclusion)
	case evidence.NativeAgentActivity:
		return "The unified session contains native agent session activity (session.start, session.init, assistant, or tool events), so the run continued past model resolution and these records are not treated as its failure cause."
	case evidence.AgentExecution == nil:
		return "The unified session has no agent execution record, so a startup stop caused by these records cannot be established."
	case *evidence.AgentExecution.ExitCode == 0:
		return "The agent execution record exited with code 0, so these records are not treated as the failure cause."
	default:
		return ""
	}
}

const modelResolutionNoStopEvidenceReason = "agent-stdio.log contains no harness refusal or model-specific execution error for these roles, so the nonzero agent exit is not treated as caused by their resolution."

// partitionModelResolutionStopEvidence splits failures into those with affirmative
// startup-stop evidence in the same run's agent-stdio.log and those without.
func partitionModelResolutionStopEvidence(runDir string, failures []sessionModelResolutionFailure) (stopped, unproven []sessionModelResolutionFailure) {
	logData, err := readModelAvailabilityArtifact(runDir, "agent-stdio.log", 32<<20)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		auditReportLog.Printf("Could not read model resolution startup log: %v", err)
	}
	for _, failure := range failures {
		if err == nil && modelResolutionStopEvidence(logData, failure) {
			stopped = append(stopped, failure)
		} else {
			unproven = append(unproven, failure)
		}
	}
	return stopped, unproven
}

// modelResolutionStopEvidence reports whether the startup log affirmatively links this
// failed role to the startup stop: a harness refusal naming the role (or, for the main
// role, its configured model), or the role's model-specific resolution error. Optional
// repository agents log only a warning and suppress model-not-supported errors, so
// neither form is produced for them.
func modelResolutionStopEvidence(logData []byte, failure sessionModelResolutionFailure) bool {
	subagentRefusal := "sub-agent '" + failure.Role + "'"
	mainRefusal := "'" + failure.ConfiguredModel + "'"
	resolutionError := fmt.Sprintf("model resolution: engine=%s role=%s configured=%s failed cause=%s:",
		failure.Engine, failure.Role, failure.ConfiguredModel, failure.Cause)
	for line := range bytes.Lines(logData) {
		text := string(line)
		if strings.Contains(strings.ToLower(text), "refusing to start") &&
			(strings.Contains(text, subagentRefusal) || (failure.Role == "main" && strings.Contains(text, mainRefusal))) {
			return true
		}
		if strings.Contains(text, resolutionError) &&
			(failure.Role == "main" || strings.Contains(text, "The requested model is not supported")) {
			return true
		}
	}
	return false
}

func modelResolutionStartupEvidence(evidence *sessionModelResolutionEvidence) string {
	var categories []string
	for _, category := range evidence.AgentExecution.Categories {
		if modelResolutionCausePattern.MatchString(category) && !slices.Contains(categories, category) {
			categories = append(categories, category)
		}
	}
	text := fmt.Sprintf(" Startup evidence: agent execution exited with code %d", *evidence.AgentExecution.ExitCode)
	if len(categories) > 0 {
		text += " (categories: " + strings.Join(categories, ", ") + ")"
	}
	text += ", the unified session contains no native agent session activity, and agent-stdio.log records a harness refusal or model-specific resolution error for each listed role."
	return text
}

func modelResolutionAdvisory(groups []modelResolutionFailureGroup, reason string) AuditFinding {
	return AuditFinding{
		Code:        AuditFindingModelResolutionFailed,
		Category:    "configuration",
		Severity:    "low",
		Title:       "Model Resolution Failure Recorded",
		Description: "The unified session records failed model resolution: " + describeModelResolutionGroups(groups) + ". " + reason + " A failed record can come from an optional repository agent, whose unresolved model is only a warning.",
		Impact:      "Advisory: verify the listed model pins. This record alone does not establish that model resolution stopped the run.",
	}
}

func generateSessionModelResolutionFindings(run WorkflowRun, evidence *sessionModelResolutionEvidence) []AuditFinding {
	if reason := modelResolutionContinuationReason(run, evidence); reason != "" {
		return []AuditFinding{modelResolutionAdvisory(groupModelResolutionFailures(evidence.Failures), reason)}
	}

	stopped, unproven := partitionModelResolutionStopEvidence(run.LogsPath, evidence.Failures)
	var findings []AuditFinding
	if len(stopped) > 0 {
		confirmed, unsupported, unverified := classifyModelResolutionGroups(run, groupModelResolutionFailures(stopped))
		startup := modelResolutionStartupEvidence(evidence)
		if len(confirmed) > 0 {
			findings = append(findings, AuditFinding{
				Code:        AuditFindingModelUnavailable,
				Category:    "configuration",
				Severity:    "high",
				Title:       "Model Unavailable",
				Description: "Startup model resolution failed before the agent started inference. " + strings.Join(confirmed, " ") + startup,
				Impact:      "The agent could not start inference. This evidence does not establish global model retirement or why provider availability changed.",
			})
		}
		if len(unsupported) > 0 {
			findings = append(findings, AuditFinding{
				Code:        AuditFindingModelEndpointUnsupported,
				Category:    "configuration",
				Severity:    "high",
				Title:       "Model Endpoint Unsupported",
				Description: "Startup model resolution failed before the agent started inference. " + strings.Join(unsupported, " ") + startup,
				Impact:      "The model is live, but the engine cannot call its endpoint. Choose a model that supports the engine's endpoint.",
			})
		}
		if len(unverified) > 0 {
			findings = append(findings, AuditFinding{
				Code:        AuditFindingModelAvailabilityUnknown,
				Category:    "configuration",
				Severity:    "high",
				Title:       "Model Availability Unverified",
				Description: "Startup model resolution failed before the agent started inference. " + strings.Join(unverified, " ") + startup,
				Impact:      "Inspect model discovery and authentication evidence before changing the model or runtime.",
			})
		}
	}
	if len(unproven) > 0 {
		findings = append(findings, modelResolutionAdvisory(groupModelResolutionFailures(unproven), modelResolutionNoStopEvidenceReason))
	}
	return findings
}

// classifyModelResolutionGroups checks each failure group against this run's own
// inventory, keeping the recorded cause when it is not model_unavailable. Groups whose
// recorded cause is unsupported_endpoint name a live model the engine cannot call.
func classifyModelResolutionGroups(run WorkflowRun, groups []modelResolutionFailureGroup) (confirmed, unsupported, unverified []string) {
	var reflection []byte
	var reflectionErr error
	reflectionRead := false
	for _, group := range groups {
		if group.Cause == modelResolutionCauseUnsupportedEndpoint {
			unsupported = append(unsupported, group.describe()+": the model is live, but the engine cannot call its endpoint; choose a compatible model.")
			continue
		}
		var detail string
		isConfirmed := false
		switch {
		case group.Cause != modelResolutionCauseModelUnavailable:
			detail = fmt.Sprintf("the harness recorded cause %s, not model_unavailable, so this run's inventory is not used to confirm absence", group.Cause)
		case group.Provider == "":
			detail = "no provider was recorded, so no configured endpoint inventory can be matched"
		default:
			if !reflectionRead {
				reflection, reflectionErr = readModelAvailabilityReflection(run.LogsPath)
				reflectionRead = true
			}
			isConfirmed, detail = checkModelAvailability(reflection, reflectionErr, group.Provider, group.ConfiguredModel)
		}
		if isConfirmed {
			confirmed = append(confirmed, group.describe()+": "+detail)
		} else {
			unverified = append(unverified, group.describe()+": "+detail+"; model absence cannot be confirmed from this run's inventory.")
		}
	}
	return confirmed, unsupported, unverified
}

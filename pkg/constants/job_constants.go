package constants

// JobName represents a GitHub Actions job identifier.
// This semantic type distinguishes job names from arbitrary strings,
// preventing mixing of job identifiers with other string types.
//
// Example usage:
//
//	const AgentJobName JobName = "agent"
//	func GetJob(name JobName) (*Job, error) { ... }
type JobName string

// String returns the string representation of the job name
func (j JobName) String() string {
	return string(j)
}

// IsValid returns true if the job name is non-empty
func (j JobName) IsValid() bool {
	return j != ""
}

// StepID represents a GitHub Actions step identifier.
// This semantic type distinguishes step IDs from arbitrary strings,
// preventing mixing of step identifiers with job names or other strings.
//
// Example usage:
//
//	const CheckMembershipStepID StepID = "check_membership"
//	func GetStep(id StepID) (*Step, error) { ... }
type StepID string

// String returns the string representation of the step ID
func (s StepID) String() string {
	return string(s)
}

// IsValid returns true if the step ID is non-empty
func (s StepID) IsValid() bool {
	return s != ""
}

// MCPServerID represents a built-in MCP server identifier.
// This semantic type distinguishes MCP server IDs from arbitrary strings,
// preventing accidental mixing of server identifiers with other string types.
//
// Example usage:
//
//	const SafeOutputsMCPServerID MCPServerID = "safeoutputs"
//	func GetServer(id MCPServerID) (*Server, error) { ... }
type MCPServerID string

// String returns the string representation of the MCP server ID
func (m MCPServerID) String() string {
	return string(m)
}

// Job name constants for GitHub Actions workflow jobs
const AgentJobName JobName = "agent"
const ActivationJobName JobName = "activation"
const PreActivationJobName JobName = "pre_activation"
const PreActivationHyphenJobName JobName = "pre-activation"
const DetectionJobName JobName = "detection"
const EvalsJobName JobName = "evals"
const SafeOutputsJobName JobName = "safe_outputs"
const SafeOutputsHyphenJobName JobName = "safe-outputs"
const UploadAssetsJobName JobName = "upload_assets"
const UploadCodeScanningJobName JobName = "upload_code_scanning_sarif"
const UploadCodeCoverageJobName JobName = "upload_code_coverage"
const ConclusionJobName JobName = "conclusion"
const UnlockJobName JobName = "unlock"

// KnownBuiltInJobNames contains all known built-in workflow job names (including aliases).
// It is used for O(1) membership checks when validating or filtering user-defined job
// names to avoid collisions with framework-generated jobs. For example, workflow code
// can check this map before treating a frontmatter jobs.<name> entry as a custom job.
var KnownBuiltInJobNames = map[string]struct{}{
	string(AgentJobName):               {},
	string(ActivationJobName):          {},
	string(PreActivationJobName):       {},
	string(PreActivationHyphenJobName): {},
	string(DetectionJobName):           {},
	string(EvalsJobName):               {},
	string(SafeOutputsJobName):         {},
	string(SafeOutputsHyphenJobName):   {},
	string(UploadAssetsJobName):        {},
	string(UploadCodeScanningJobName):  {},
	string(UploadCodeCoverageJobName):  {},
	string(ConclusionJobName):          {},
	string(UnlockJobName):              {},
}

// Artifact name constants
const SafeOutputArtifactName ArtifactName = "safe-output"
const AgentOutputArtifactName ArtifactName = "agent-output"

// AgentArtifactName is the name of the unified agent artifact that contains all agent job outputs,
// including safe outputs, agent output, engine logs, and other agent-related files.
const AgentArtifactName ArtifactName = "agent"

// AgentOutputFallbackArtifactName is the name of a small, dedicated artifact that contains
// only the processed agent output JSON and the raw safe-output NDJSON. Those files are also
// part of the large "agent" artifact, but that upload is best-effort (continue-on-error) and
// occasionally times out against blob storage. When it does, every safe output is silently
// dropped, so downstream jobs fall back to this tiny artifact instead.
const AgentOutputFallbackArtifactName ArtifactName = "agent-output-fallback"

// DetectionArtifactName is the artifact name for the threat detection log.
const DetectionArtifactName ArtifactName = "detection"

// EvalsArtifactName is the artifact name for the BinEval evaluation results.
const EvalsArtifactName ArtifactName = "evals"

// EvalsResultFilename is the filename of the evaluation results JSONL file.
const EvalsResultFilename Filename = "evals.jsonl"

// EvalsBranchPrefix is the branch prefix used for durable eval result storage.
const EvalsBranchPrefix = "evals"

// LegacyDetectionArtifactName is the old artifact name used before the rename.
// Kept for backward compatibility when downloading artifacts from older workflow runs.
const LegacyDetectionArtifactName ArtifactName = "threat-detection.log"

// AgentOutputFilename is the filename of the agent output JSON file
const AgentOutputFilename Filename = "agent_output.json"

// SafeOutputsFilename is the filename of the raw safe outputs NDJSON file copied to /tmp/gh-aw/
const SafeOutputsFilename Filename = "safeoutputs.jsonl"

// TokenUsageFilename is the filename of the aggregated token usage JSON file written to /tmp/gh-aw/
// by parse_token_usage.cjs. It is included in the agent artifact so third-party tools can
// consume structured token data without parsing the step summary or GITHUB_OUTPUT.
const TokenUsageFilename Filename = "agent_usage.json"

// GradersDirName is the directory name holding grader manifest and results files. The agent
// job writes them under /tmp/gh-aw/agent/graders/ and the conclusion job mirrors them into
// the graders/ subdirectory of the usage artifact.
const GradersDirName ArtifactName = "graders"

// GraderManifestFilename is the filename of the grader manifest JSON written to /tmp/gh-aw/agent/graders/
// by trace_graders.cjs. Lists which graders were configured and their enabled state.
const GraderManifestFilename Filename = "grader_manifest.json"

// GraderResultsFilename is the filename of the normalized grader results JSON written to /tmp/gh-aw/agent/graders/
// by trace_graders.cjs. Contains deterministic metric values computed from trace files.
const GraderResultsFilename Filename = "grader_results.json"

// GraderPayloadFilename is the filename of the preprocessed trace payload consumed by graders.
const GraderPayloadFilename Filename = "grader_payload.json"

// OperationalValueEvaluatorFilename is the filename of the frozen operational-value evaluator archived for replay.
const OperationalValueEvaluatorFilename Filename = "operational_value_evaluator.sh"

// GradersDir is the subdirectory under TmpGhAwAgentDir where grader output files are written.
const GradersDir = TmpGhAwDir + "/agent/graders"

// GradersDirSlash is GradersDir with a trailing slash.
const GradersDirSlash = GradersDir + "/"

// GithubRateLimitsFilename is the filename of the GitHub API rate-limit log written to /tmp/gh-aw/.
// Each line is a JSON object recording the x-ratelimit-* headers (or rate-limit API snapshot)
// captured during github.rest API calls, enabling post-run analysis of rate-limit consumption.
const GithubRateLimitsFilename Filename = "github_rate_limits.jsonl"

// OtelJsonlFilename is the filename of the OTLP span mirror written to /tmp/gh-aw/
// by send_otlp_span.cjs. Each line is a full OTLP/HTTP JSON traces payload.
// Included in the agent artifact so spans are available without a live collector.
const OtelJsonlFilename Filename = "otel.jsonl"

// OtlpExportErrorsFilename is the filename of the OTLP per-endpoint export failure log
// written to /tmp/gh-aw/ by send_otlp_span.cjs. Each line is a JSON object containing the
// collector host, optional status, and sanitized failure reason for one terminal export failure.
const OtlpExportErrorsFilename Filename = "otlp-export-errors.jsonl"

// ArtifactPrefixOutputName is the job output name that exposes the artifact name prefix.
// In workflow_call context, the prefix is a stable hash derived from the workflow inputs,
// ensuring artifact names are unique when the same workflow is called multiple times in
// the same workflow run (e.g. multiple jobs each calling the same reusable workflow).
// Empty string in non-workflow_call context.
const ArtifactPrefixOutputName = "artifact_prefix"

// ActivationArtifactName is the artifact name for the activation job output
// (aw_info.json and prompt.txt).
const ActivationArtifactName ArtifactName = "activation"

// ActivationStageAmbientFoldersStepName is the step name used to stage ambient
// folders before the activation artifact is packaged. It is a stable anchor
// used to determine the insertion point for jobs.activation.steps injected
// via built-in job step merging; keep it in sync with any renames of that
// step.
const ActivationStageAmbientFoldersStepName = "Stage ambient folders for activation artifact"

// ActivationUploadArtifactStepName is the step name used to upload the
// activation artifact. It is a stable anchor used to determine the insertion
// point for jobs.activation.steps injected via built-in job step merging;
// keep it in sync with any renames of that step.
const ActivationUploadArtifactStepName = "Upload activation artifact"

// ExperimentArtifactName is the artifact name for A/B experiment state
// uploaded by the activation job when experiments are declared in the frontmatter.
const ExperimentArtifactName ArtifactName = "experiment"

// UsageArtifactName is the compact artifact produced by the conclusion job with
// workflow-run metadata and token-usage files used by lightweight reporting paths.
const UsageArtifactName ArtifactName = "usage"

// SafeOutputItemsArtifactName is the artifact name for the safe output items manifest.
// This artifact contains the JSONL manifest of all items created by safe output handlers
// and is uploaded by the safe_outputs job to avoid conflicting with the "agent" artifact
// that is already uploaded by the agent job.
const SafeOutputItemsArtifactName ArtifactName = "safe-outputs-items"

// TemporaryIdMapFilename is the filename of the temporary ID map JSON file written to /tmp/gh-aw/.
// This file contains a JSON object mapping temporary IDs (e.g., aw_abc123) to their resolved
// GitHub resource references ({repo, number}) for review and audit purposes.
// It is uploaded alongside the safe-output-items.jsonl manifest in the safe-outputs-items artifact.
const TemporaryIdMapFilename Filename = "temporary-id-map.json"

// SafeOutputErrorsFilename is the filename of the structured safe-output failure report
// written to /tmp/gh-aw/ by the safe-output handler manager whenever the "Process Safe Outputs"
// step fails. It records the error code, the gh-aw generated error message and the failing
// safe-output types, so failures stay diagnosable after the job logs expire.
// It is uploaded alongside the safe-output-items.jsonl manifest in the safe-outputs-items artifact.
const SafeOutputErrorsFilename = "safe-output-errors.json"

// SarifArtifactName is the artifact name used to transfer the SARIF file generated by
// the create_code_scanning_alert handler from the safe_outputs job to the
// upload_code_scanning_sarif job.  The safe_outputs job uploads the file under this name;
// the upload job downloads it and passes it to github/codeql-action/upload-sarif.
const SarifArtifactName ArtifactName = "code-scanning-sarif"

// SarifArtifactDownloadPath is the local path where the upload_code_scanning_sarif job
// downloads the SARIF artifact.  The file will be available at this path + the SARIF
// filename ("code-scanning-alert.sarif") after actions/download-artifact completes.
const SarifArtifactDownloadPath FilePath = "/tmp/gh-aw/sarif/"

// SarifFileName is the name of the SARIF file generated by create_code_scanning_alert.cjs
// and uploaded / downloaded as part of the code-scanning-sarif artifact.
const SarifFileName Filename = "code-scanning-alert.sarif"

// MCP server ID constants

// GitHubMCPServerID is the identifier for the GitHub MCP server.
// Using this constant provides a single source of truth for the GitHub MCP server
// identifier and makes refactoring easier.
const GitHubMCPServerID MCPServerID = "github"

const SafeOutputsMCPServerID MCPServerID = "safeoutputs"

// MCPScriptsMCPServerID is the identifier for the mcp-scripts MCP server
const MCPScriptsMCPServerID MCPServerID = "mcpscripts"

// MCPScriptsMCPVersion is the version of the mcp-scripts MCP server
const MCPScriptsMCPVersion = "1.0.0"

// AgenticWorkflowsMCPServerID is the identifier for the agentic-workflows MCP server
const AgenticWorkflowsMCPServerID MCPServerID = "agenticworkflows"

// Step IDs for pre-activation job
const CheckMembershipStepID StepID = "check_membership"
const CheckStopTimeStepID StepID = "check_stop_time"
const CheckSkipIfMatchStepID StepID = "check_skip_if_match"
const CheckSkipIfNoMatchStepID StepID = "check_skip_if_no_match"
const CheckCommandPositionStepID StepID = "check_command_position"
const RemoveTriggerLabelStepID StepID = "remove_trigger_label"
const GetTriggerLabelStepID StepID = "get_trigger_label"
const CheckRateLimitStepID StepID = "check_rate_limit"
const CheckCooldownStepID StepID = "check_cooldown"
const CheckSkipRolesStepID StepID = "check_skip_roles"
const CheckSkipBotsStepID StepID = "check_skip_bots"
const CheckSkipIfCheckFailingStepID StepID = "check_skip_if_check_failing"

// PreActivationAppTokenStepID is the step ID for the unified GitHub App token mint step
// emitted in the pre-activation job when on.github-app is configured alongside skip-if checks.
const PreActivationAppTokenStepID StepID = "pre-activation-app-token"

// ParseMCPGatewayStepID is the step ID for the MCP gateway log parsing step in the agent job.
// Its effective_tokens output is exposed as an agent job output so that the safe_outputs job
// can pass the value as GH_AW_EFFECTIVE_TOKENS to the footer template renderer.
const ParseMCPGatewayStepID StepID = "parse-mcp-gateway"

// DetectAgentErrorsStepID is the step ID for the post-execution error detection step in the
// agent job. It runs on the host runner (outside the AWF sandbox container) so that it can
// write to GITHUB_OUTPUT, which is not accessible from inside the container. Any engine that
// provides a detection script (via GetErrorDetectionScriptId) will emit this step.
const DetectAgentErrorsStepID StepID = "detect-agent-errors"

// Output names for pre-activation job steps
const IsTeamMemberOutput = "is_team_member"
const StopTimeOkOutput = "stop_time_ok"
const SkipCheckOkOutput = "skip_check_ok"
const SkipNoMatchCheckOkOutput = "skip_no_match_check_ok"
const CommandPositionOkOutput = "command_position_ok"
const MatchedCommandOutput = "matched_command"
const RateLimitOkOutput = "rate_limit_ok"
const CooldownOkOutput = "cooldown_ok"
const SkipRolesOkOutput = "skip_roles_ok"
const SkipBotsOkOutput = "skip_bots_ok"
const SkipIfCheckFailingOkOutput = "skip_if_check_failing_ok"
const ActivatedOutput = "activated"

// Rate limit defaults
const DefaultRateLimitMax = 5     // Default maximum runs per time window
const DefaultRateLimitWindow = 60 // Default time window in minutes (1 hour)

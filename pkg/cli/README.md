# cli Package

> CLI command implementations for the `gh aw` extension — the primary user interface for authoring, compiling, running, and monitoring agentic GitHub workflows.

## Overview

The `cli` package implements all commands exposed through the `gh aw` CLI extension. Each command is implemented as a Cobra command with a dedicated `New*Command()` constructor and a `Run*()` function that encapsulates the testable business logic.

The package is intentionally decomposed into many small files grouped by feature domain (e.g., `compile_*.go`, `audit_*.go`, `run_*.go`, `mcp_*.go`). This structure keeps individual files under 300 lines and promotes independent testing of each sub-domain.

Besides Cobra entry points, the package also exposes reusable helpers for workflow resolution, dependency analysis, PR creation, and run auditing so multiple commands can share the same business logic and tests.

All diagnostic output MUST go to `stderr` using `console` formatting helpers. Structured output (JSON, hashes, graphs) goes to `stdout`.

## Command Groups

| Command | Entry Point | Description |
|---------|-------------|-------------|
| `gh aw add` | `NewAddCommand` | Add remote or local workflows to the repository |
| `gh aw add-wizard` | `NewAddWizardCommand` | Interactive wizard for adding workflows |
| `gh aw new` | `newCmd` (main.go) | Create a new workflow file (supports `--force`, `--interactive`, `--engine`) |
| `gh aw compile` | Cobra `compileCmd` (`cmd/gh-aw/main.go`); orchestration via `CompileWorkflows` (`compile_orchestrator.go`) | Compile `.md` workflow files into GitHub Actions `.lock.yml` |
| `gh aw enable` | `enableCmd` (main.go) | Enable a workflow |
| `gh aw disable` | `disableCmd` (main.go) | Disable a workflow |
| `gh aw run` | `RunWorkflowOnGitHub` (main.go) | Dispatch and monitor workflow runs |
| `gh aw audit` | `NewAuditCommand` | Audit a specific workflow run by run ID |
| `gh aw audit diff` | `NewAuditDiffSubcommand` | Diff audit data between multiple runs |
| `gh aw logs` | `NewLogsCommand` | Download and analyze workflow run logs |
| `gh aw mcp` | `NewMCPCommand` | Manage MCP server configurations |
| `gh aw mcp add` | `NewMCPAddSubcommand` | Add an MCP tool to a workflow |
| `gh aw mcp inspect` | `NewMCPInspectSubcommand` | Inspect MCP servers in a workflow |
| `gh aw mcp list` | `NewMCPListSubcommand` | List workflows using MCP servers |
| `gh aw mcp list-tools` | `NewMCPListToolsSubcommand` | List tools for a specific MCP server |
| `gh aw mcp server` | `NewMCPServerCommand` | Run as an MCP server (for IDE integration) |
| `gh aw update` | `NewUpdateCommand` | Update workflows from upstream sources |
| `gh aw upgrade` | `NewUpgradeCommand` | Upgrade workflows to latest format |
| `gh aw validate` | `NewValidateCommand` | Validate workflow files without compiling |
| `gh aw fix` | `NewFixCommand` | Apply automatic codemods to fix deprecated patterns |
| `gh aw status` | `NewStatusCommand` | Show status of workflows in the repository |
| `gh aw health` | `NewHealthCommand` | Compute health metrics across workflow runs |
| `gh aw checks` | `NewChecksCommand` | Show CI check results for a PR |
| `gh aw domains` | `NewDomainsCommand` | List domains used by workflows |
| `gh aw hash` | `NewHashCommand` | Print frontmatter hash of a workflow file |
| `gh aw init` | `NewInitCommand` | Initialize a repository for agentic workflows |
| `gh aw list` | `NewListCommand` | List installed workflows |
| `gh aw pr` | `NewPRCommand` | Pull-request helpers |
| `gh aw pr transfer` | `NewPRTransferSubcommand` | Transfer a pull request to another repository |
| `gh aw project` | `NewProjectCommand` | Project management helpers |
| `gh aw project new` | `NewProjectNewCommand` | Create a new GitHub Project V2 board |
| `gh aw remove` | `RemoveWorkflows` (main.go) | Remove workflow files from the repository |
| `gh aw secrets` | `NewSecretsCommand` | Manage workflow secrets |
| `gh aw secrets set` | (secret_set_command.go) | Create or update a repository secret |
| `gh aw secrets bootstrap` | (secret_set_command.go) | Validate and configure all required secrets for workflows |
| `gh aw env` | `NewEnvCommand` | Manage compiler defaults as GitHub variables |
| `gh aw env pull` | (env_command.go) | Download compiler defaults into a YAML file |
| `gh aw env push` | (env_command.go) | Upload compiler defaults from a YAML file |
| `gh aw view` | `NewViewCommand` | Render unified timeline and safe outputs for a workflow run |
| `gh aw lint` | `NewLintCommand` | Lint existing `.lock.yml` workflows with actionlint |
| `gh aw experiments` | `NewExperimentsCommand` | Explore ongoing A/B experiments in the repository (hidden) |
| `gh aw experiments list` | `NewExperimentsListSubcommand` | List all experiment workflow branches |
| `gh aw experiments analyze` | `NewExperimentsAnalyzeSubcommand` | Analyze a specific experiment workflow in detail |
| `gh aw forecast` | `NewForecastCommand` | Forecast token usage and costs for agentic workflows |
| `gh aw trial` | `NewTrialCommand` | Run trial workflow executions |
| `gh aw deploy` | `NewDeployCommand` | Deploy agentic workflows to a target repository using a pull request |
| `gh aw outcomes` | `NewOutcomesCommand` | Check what happened to a workflow run's safe outputs |
| `gh aw outcomes history` | `NewOutcomesHistorySubcommand` | Score recent closed issues and merged PRs against the objective mapping |
| _No `gh aw deps` command_ | `deps_*.go` (internal utilities) | Dependency reporting/advisory helpers used by other commands |
| `gh aw doctor` | `NewDoctorCommand` | Run diagnostics to verify CLI authentication and repository setup |
| `gh aw version` | `versionCmd` (main.go) | Show version information |
| `gh aw completion` | `NewCompletionCommand` | Generate shell completion scripts |

## Public API

The `cli` package is intentionally large and command-oriented. The tables below document the most important exported, source-verified entry points and data types rather than every helper. Callers SHOULD treat `New*Command()` constructors as the stable integration surface for Cobra wiring, and SHOULD use the corresponding `Run*` or library helpers for programmatic use.

### Types

| Type | Kind | Description |
|------|------|-------------|
| `AddOptions` | struct | Options for installing workflows from local paths, URLs, or source repositories. |
| `AddWorkflowsResult` | struct | Result of `AddWorkflows` and `AddResolvedWorkflows`, including PR metadata and dispatch detection. |
| `CompileConfig` | struct | Compilation and validation settings for `CompileWorkflows`, including scanner and output controls. |
| `ValidationResult` | struct | Per-workflow validation result used by compile and validate flows. |
| `ResolvedWorkflow` | struct | Metadata and fetched content for a single resolved workflow source. |
| `ResolvedWorkflows` | struct | Batch resolution result for `gh aw add`, including warnings and bootstrap profile metadata. |
| `RunOptions` | struct | Options for triggering workflows on GitHub, including enable/push/wait/dry-run behavior. |
| `WorkflowRunResult` | struct | JSON-friendly result for a triggered or simulated workflow run. |
| `HealthConfig` | struct | Configuration for workflow health analysis. |
| `WorkflowHealth` | struct | Per-workflow health metrics computed from historical runs. |
| `HealthSummary` | struct | Aggregate health summary across workflows. |
| `ChecksConfig` | struct | Inputs for normalizing CI check state for a pull request. |
| `ChecksResult` | struct | Normalized check-rollup result returned by `FetchChecksResult`. |
| `OutcomesConfig` | struct | Configuration for safe-output outcome evaluation for a single run. |
| `OutcomesData` | struct | Structured outcomes report for JSON output. |
| `UpdateWorkflowsOptions` | struct | Options controlling `gh aw update` behavior for sourced workflows. |
| `WorkflowStatus` | struct | Status view for a workflow, combining list metadata with latest run state. |
| `MCPRegistryClient` | struct | Client for the MCP registry used by MCP subcommands. |
| `ToolGraph` | struct | Directed graph of MCP tool transitions used in log analysis. |
| `FileTracker` | struct | Tracks modified files during multi-step CLI operations. |
| `RepeatOptions` | struct | Retry-loop configuration for `ExecuteWithRepeat`. |
| `PollOptions` | struct | Signal-aware polling configuration for `PollWithSignalHandling`. |
| `FixConfig` | struct | Configuration for codemod-based fixes. |
| `ForecastConfig` | struct | Configuration for Monte Carlo AIC forecasting. |
| `ExperimentsListConfig` | struct | Configuration for listing workflow experiments. |
| `ExperimentsAnalyzeConfig` | struct | Configuration for detailed experiment analysis. |
| `AuditOptions` | struct | Shared options for audit and audit-diff execution, including artifact sets and filters. |
| `Codemod` | struct | A single code transformation applied to workflow files by `gh aw fix`, with optional file-path-aware `ApplyWithContext`. |
| `DependencyGraph` | struct | Tracks workflow file dependencies (imports) for efficient recompilation. |
| `DependencyReport` | struct | Aggregated dependency health report including outdated modules and security advisories. |
| `DockerImagesOptions` | struct | Selects which Docker-based static analysis tools (`Zizmor`, `Poutine`, `Actionlint`, etc.) are requested. |
| `LogAnalysis` | interface | Read-only interface for accessing domain analysis results; implemented by `DomainAnalysis` and `FirewallAnalysis`. |
| `MutableLogAnalysis` | interface | Extends `LogAnalysis` with mutation methods (`SetAllowedDomains`, `SetBlockedDomains`, `AddMetrics`) used during log aggregation. |
| `Metadata` | struct | Pagination metadata for MCP registry list responses. |
| `OutdatedDependency` | struct | A Go module dependency with a newer version available, including current/latest versions and age. |
| `SecurityAdvisory` | struct | A security vulnerability (GHSA/CVE) affecting a dependency, with severity and patched versions. |
| `SetupAuthOptions` | struct | Options for checking `gh` authentication status during repository setup. |
| `SetupRepositoryCheckOptions` | struct | Options for validating repository access and ownership during setup. |
| `TrialOptions` | struct | Configuration for running workflow trials, including repo context, timeout, and repeat count. |

### Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `NewAddCommand` | `func NewAddCommand(validateEngine func(string) error) *cobra.Command` | Constructs the `gh aw add` command. |
| `AddWorkflows` | `func AddWorkflows(ctx context.Context, workflows []string, opts AddOptions) (*AddWorkflowsResult, error)` | Resolves and installs one or more workflows. |
| `AddResolvedWorkflows` | `func AddResolvedWorkflows(ctx context.Context, workflowStrings []string, resolved *ResolvedWorkflows, opts AddOptions) (*AddWorkflowsResult, error)` | Installs workflows from a pre-resolved batch. |
| `ResolveWorkflows` | `func ResolveWorkflows(ctx context.Context, workflows []string, verbose bool) (*ResolvedWorkflows, error)` | Parses workflow specs and fetches their source content. |
| `CompileWorkflows` | `func CompileWorkflows(ctx context.Context, config CompileConfig) ([]*workflow.WorkflowData, error)` | Compiles workflow markdown into GitHub Actions lock files. |
| `CompileWorkflowWithValidation` | `func CompileWorkflowWithValidation(ctx context.Context, compiler *workflow.Compiler, filePath string, opts CompileValidationOptions) error` | Compiles and validates a single workflow file. |
| `CompileWorkflowDataWithValidation` | `func CompileWorkflowDataWithValidation(ctx context.Context, compiler *workflow.Compiler, workflowData *workflow.WorkflowData, filePath string, opts CompileValidationOptions) error` | Validates a pre-loaded workflow object. |
| `RunWorkflowOnGitHub` | `func RunWorkflowOnGitHub(ctx context.Context, workflowIdOrName string, opts RunOptions) error` | Validates, optionally compiles and pushes, and dispatches one workflow run. |
| `RunWorkflowsOnGitHub` | `func RunWorkflowsOnGitHub(ctx context.Context, workflowNames []string, opts RunOptions) error` | Dispatches multiple workflows sequentially. |
| `WaitForWorkflowCompletion` | `func WaitForWorkflowCompletion(ctx context.Context, repoSlug, runID string, timeoutMinutes int, verbose bool) error` | Polls GitHub until a workflow run completes or times out. |
| `AuditWorkflowRun` | `func AuditWorkflowRun(ctx context.Context, runID int64, opts AuditOptions) error` | Downloads artifacts and renders an audit report for a run. |
| `RunAuditDiff` | `func RunAuditDiff(ctx context.Context, baseRunID int64, compareRunIDs []int64, opts AuditOptions) error` | Diffs multiple audit reports. |
| `DownloadWorkflowLogs` | `func DownloadWorkflowLogs(ctx context.Context, opts LogsDownloadOptions) error` | Downloads and parses workflow logs and artifacts. |
| `NewHealthCommand` | `func NewHealthCommand() *cobra.Command` | Constructs the `gh aw health` command. |
| `RunHealth` | `func RunHealth(config HealthConfig) error` | Computes workflow health metrics for one workflow or all workflows. |
| `CalculateWorkflowHealth` | `func CalculateWorkflowHealth(workflowName string, runs []WorkflowRun, threshold float64) WorkflowHealth` | Calculates success-rate and trend metrics for one workflow. |
| `CalculateHealthSummary` | `func CalculateHealthSummary(workflowHealths []WorkflowHealth, period string, threshold float64) HealthSummary` | Aggregates workflow health results. |
| `GroupRunsByWorkflow` | `func GroupRunsByWorkflow(runs []WorkflowRun) map[string][]WorkflowRun` | Groups historical runs by workflow identifier. |
| `NewChecksCommand` | `func NewChecksCommand() *cobra.Command` | Constructs the `gh aw checks` command. |
| `RunChecks` | `func RunChecks(config ChecksConfig) error` | Emits normalized PR check state. |
| `FetchChecksResult` | `func FetchChecksResult(repoOverride string, prNumber string) (*ChecksResult, error)` | Returns normalized CI state for a pull request. |
| `NewOutcomesCommand` | `func NewOutcomesCommand() *cobra.Command` | Constructs the `gh aw outcomes` command. |
| `RunOutcomes` | `func RunOutcomes(ctx context.Context, config OutcomesConfig) error` | Evaluates what happened to safe outputs from a workflow run. |
| `EvaluateOutcomes` | `func EvaluateOutcomes(ctx context.Context, items []CreatedItemReport, repoOverride string, mapping *github.ObjectiveMapping) []OutcomeReport` | Evaluates current state for created GitHub objects. |
| `ComputeOutcomeSummary` | `func ComputeOutcomeSummary(reports []OutcomeReport, mapping *github.ObjectiveMapping) OutcomeSummary` | Summarizes outcome reports. |
| `NewUpdateCommand` | `func NewUpdateCommand(validateEngine func(string) error) *cobra.Command` | Constructs the `gh aw update` command. |
| `RunUpdateWorkflows` | `func RunUpdateWorkflows(ctx context.Context, opts UpdateWorkflowsOptions) error` | CLI wrapper for updating sourced workflows. |
| `UpdateWorkflows` | `func UpdateWorkflows(ctx context.Context, opts UpdateWorkflowsOptions) error` | Updates workflows with `source:` frontmatter from upstream definitions. |
| `NewStatusCommand` | `func NewStatusCommand() *cobra.Command` | Constructs the `gh aw status` command. |
| `GetWorkflowStatuses` | `func GetWorkflowStatuses(ctx context.Context, pattern string, ref string, labelFilter string, repoOverride string) ([]WorkflowStatus, error)` | Returns workflow status data for programmatic callers. |
| `StatusWorkflows` | `func StatusWorkflows(ctx context.Context, pattern string, verbose bool, jsonOutput bool, ref string, labelFilter string, repoOverride string) error` | Renders workflow status to terminal or JSON. |
| `InitRepository` | `func InitRepository(opts InitOptions) error` | Initializes repository-local gh-aw support files. |
| `CreateWorkflowMarkdownFile` | `func CreateWorkflowMarkdownFile(workflowName string, verbose bool, force bool, engine string) error` | Creates a new workflow markdown file. |
| `ResolveWorkflowPath` | `func ResolveWorkflowPath(workflowFile string) (string, error)` | Resolves a workflow identifier to a local file path. |
| `ExtractWorkflowDescription` | `func ExtractWorkflowDescription(content string) string` | Reads the frontmatter description from workflow content. |
| `ExtractWorkflowDescriptionFromFile` | `func ExtractWorkflowDescriptionFromFile(filePath string) string` | Reads the description from a workflow file. |
| `ExtractWorkflowEngine` | `func ExtractWorkflowEngine(content string) string` | Reads the preferred engine from workflow frontmatter. |
| `ExtractWorkflowPrivate` | `func ExtractWorkflowPrivate(content string) bool` | Reports whether workflow frontmatter marks the workflow private. |
| `UpdateFieldInFrontmatter` | `func UpdateFieldInFrontmatter(content, fieldName, fieldValue string) (string, error)` | Updates a top-level YAML frontmatter field. |
| `SetFieldInOnTrigger` | `func SetFieldInOnTrigger(content, fieldName, fieldValue string) (string, error)` | Sets a field inside the `on:` block. |
| `RemoveFieldFromOnTrigger` | `func RemoveFieldFromOnTrigger(content, fieldName string) (string, error)` | Removes a field from the `on:` block. |
| `UpdateScheduleInOnBlock` | `func UpdateScheduleInOnBlock(content, scheduleExpr string) (string, error)` | Rewrites the workflow schedule expression inside `on:`. |
| `ScanWorkflowsForMCP` | `func ScanWorkflowsForMCP(workflowsDir string, serverFilter string, verbose bool) ([]WorkflowMCPMetadata, error)` | Scans workflow markdown for MCP usage. |
| `ListToolsForMCP` | `func ListToolsForMCP(workflowFile string, mcpServerName string, verbose bool) error` | Lists tools exposed by one MCP server reference. |
| `ListWorkflowMCP` | `func ListWorkflowMCP(workflowFile string, verbose bool) error` | Lists MCP server configuration for a workflow. |
| `InspectWorkflowMCP` | `func InspectWorkflowMCP(ctx context.Context, workflowFile string, serverFilter string, toolFilter string, verbose bool, useActionsSecrets bool) error` | Inspects MCP server definitions and resolved secrets. |
| `AddMCPTool` | `func AddMCPTool(ctx context.Context, workflowFile string, mcpServerID string, registryURL string, transportType string, customToolID string, verbose bool) error` | Adds an MCP tool or server reference to workflow frontmatter. |
| `GenerateDependencyReport` | `func GenerateDependencyReport(ctx context.Context, verbose bool) (*DependencyReport, error)` | Generates a dependency report for the current repo. |
| `CheckOutdatedDependencies` | `func CheckOutdatedDependencies(ctx context.Context, verbose bool) ([]OutdatedDependency, error)` | Detects outdated dependencies. |
| `CheckSecurityAdvisories` | `func CheckSecurityAdvisories(ctx context.Context, verbose bool) ([]SecurityAdvisory, error)` | Detects known GitHub advisories affecting dependencies. |
| `DisplayDependencyReport` | `func DisplayDependencyReport(report *DependencyReport)` | Renders a dependency report. |
| `DisplayDependencyReportJSON` | `func DisplayDependencyReportJSON(report *DependencyReport) error` | Writes a dependency report as JSON. |
| `DisplayOutdatedDependencies` | `func DisplayOutdatedDependencies(outdated []OutdatedDependency, totalDeps int)` | Renders outdated dependencies. |
| `DisplaySecurityAdvisories` | `func DisplaySecurityAdvisories(advisories []SecurityAdvisory)` | Renders advisory results. |
| `RunForecast` | `func RunForecast(config ForecastConfig) error` | Runs token and AIC forecast analysis. |
| `RunExperimentsList` | `func RunExperimentsList(config ExperimentsListConfig) error` | Lists experiment branches and summary statistics. |
| `RunExperimentsAnalyze` | `func RunExperimentsAnalyze(config ExperimentsAnalyzeConfig) error` | Produces detailed statistical analysis for one experiment workflow. |
| `ExecuteWithRepeat` | `func ExecuteWithRepeat(options RepeatOptions) error` | Repeats an operation with delay and retry controls. |
| `PollWithSignalHandling` | `func PollWithSignalHandling(options PollOptions) error` | Polls while honoring interruption signals. |
| `InstallShellCompletion` | `func InstallShellCompletion(verbose bool, rootCmd CommandProvider) error` | Installs shell completion scripts for the CLI. |
| `DetectShell` | `func DetectShell() ShellType` | Detects the current interactive shell. |
| `GetBinaryPath` | `func GetBinaryPath() (string, error)` | Returns the current gh-aw binary path. |
| `GetCurrentRepoSlug` | `func GetCurrentRepoSlug() (string, error)` | Returns the current repository slug. |
| `GetVersion` | `func GetVersion() string` | Returns the CLI version string. |
| `SetVersionInfo` | `func SetVersionInfo(v string)` | Sets the CLI version string at startup. |

### Constants

| Constant | Type | Value | Description |
|----------|------|-------|-------------|
| `CheckStateFailed` | `CheckState` | `"failed"` | Normalized CI state indicating one or more checks failed. |
| `CheckStatePending` | `CheckState` | `"pending"` | Normalized CI state indicating checks are still running or queued. |
| `CheckStateNoChecks` | `CheckState` | `"no_checks"` | Normalized CI state indicating no checks were configured or triggered. |
| `CheckStatePolicyBlocked` | `CheckState` | `"policy_blocked"` | Normalized CI state indicating policy or account gates blocked the PR. |
| `CheckStateSuccess` | `CheckState` | `"success"` | Normalized CI state indicating all required checks passed. |
| `CheckAndPrepareDockerImages` | `func(ctx context.Context, opts DockerImagesOptions) error` | Pre-pulls security-scanner Docker images |
| `UpdateContainerPins` | `func(ctx context.Context, workflowDir string, verbose bool) (bool, error)` | Updates container image SHA pins in workflow files |
| `CreatePRWithChanges` | `func(ctx context.Context, branchPrefix, commitMessage, prTitle, prBody string, verbose bool) (string, error)` | Creates a GitHub PR from uncommitted changes |
| `AutoMergePullRequestsCreatedAfter` | `func(repoSlug string, createdAfter time.Time, verbose bool) error` | Auto-merges eligible PRs created after a given time |
| `PreflightCheckForCreatePR` | `func(bool) error` | Validates prerequisites before creating a PR |
| `DisableAllWorkflowsExcept` | `func(repoSlug string, exceptWorkflows []string, verbose bool) error` | Disables all workflows in a repo except the named ones |
| `GetEngineSecretNameAndValue` | `func(engine string, existingSecrets map[string]struct{}) (string, string, bool, error)` | Prompts for and validates an engine API secret |
| `CheckForUpdatesAsync` | `func(ctx, noCheckUpdate, verbose bool) func()` | Checks for a newer `gh-aw` version in the background; returns a join function the caller must invoke before exit |
| `FetchChecksResult` | `func(repoOverride, prNumber string) (*ChecksResult, error)` | Fetches CI check results for a pull request |
| `ValidEngineNames` | `func() []string` | Returns the supported engine names for shell completion |
| `CompleteWorkflowNames` | `func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective)` | Shell-completion provider for workflow names |
| `CompleteEngineNames` | `func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective)` | Shell-completion provider for engine names |
| `CompleteDirectories` | `func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective)` | Shell-completion provider for directory paths |
| `RegisterEngineFlagCompletion` | `func(*cobra.Command)` | Registers shell completions for the `--engine` flag |
| `RegisterDirFlagCompletion` | `func(*cobra.Command, string)` | Registers shell completions for a directory flag |
| `UninstallShellCompletion` | `func(verbose bool) error` | Uninstalls shell completion scripts |
| `IsCommitSHA` | `func(string) bool` | Returns true if the string is a full Git commit SHA |
| `ValidateWorkflowIntent` | `func(string) error` | Validates the workflow intent string |

### Additional Exported Types

The `cli` package exports many types used across its command implementations. The following supplements the main "Key Types" table above:

| Type | Kind | Description |
|------|------|-------------|
| `AccessLogEntry` | struct | A single entry from an AWF network access log |
| `AccessLogSummary` | struct | Aggregated summary of access log entries |
| `ActionInput` | struct | An input parameter definition from `action.yml` |
| `ActionMetadata` | struct | Parsed `action.yml` metadata for a composite action |
| `ActionOutput` | struct | An output definition from `action.yml` |
| `ActionlintStats` | struct | Static-analysis statistics from an actionlint run |
| `AddInteractiveConfig` | struct | Configuration for the interactive `add-wizard` command |
| `AgenticAssessment` | struct | Agentic behavior assessment derived from audit logs |
| `AmbientContextMetrics` | struct | Token metrics for ambient context (input, cached, and output token counts) |
| `Argument` | struct | A command-line argument definition from the MCP registry API |
| `ArtifactSet` | string alias | Named set of artifacts (e.g. `"agent"`, `"detection"`) |
| `AuditComparisonClassification` | struct | A classification label and reason codes for an audit comparison |
| `AuditComparisonData` | struct | Full comparison between two audit runs |
| `AuditComparisonBaseline` | struct | Baseline metrics for an audit comparison |
| `AuditComparisonDelta` | struct | Numeric delta between baseline and compare run |
| `AuditComparisonIntDelta` | struct | Integer-valued delta in an audit comparison |
| `AuditComparisonMCPFailureDelta` | struct | MCP failure count delta in an audit comparison |
| `AuditComparisonRecommendation` | struct | A recommendation produced by an audit comparison |
| `AuditComparisonStringDelta` | struct | String-valued delta in an audit comparison |
| `AuditEngineConfig` | struct | Engine configuration captured in an audit run |
| `AuditLogEntry` | struct | A structured entry from the agent audit log |
| `AwContext` | struct | Agentic workflow context parsed from the run |
| `AwInfo` | struct | Top-level `gh-aw` metadata block from an audit artifact |
| `AwInfoSteps` | struct | Step-level metadata in `aw_info.json` (e.g. firewall type) |
| `BashCommandsDiff` | struct | Per-command diff of bash tool calls between two audit runs |
| `BehaviorFingerprint` | struct | Pattern fingerprint of agent behavior across turns |
| `CheckState` | string alias | CI check state (`"success"`, `"failure"`, `"pending"`, ...) |
| `CodemodResult` | struct | Result of a single codemod transformation |
| `CommandProvider` | interface | Interface implemented by Cobra root commands for shell-completion helpers |
| `CompilationStats` | struct | Statistics from a compilation run (files, errors, warnings) |
| `CombinedTrialResult` | struct | Combined results from multiple trial runs |
| `ContinuationData` | struct | State for multi-turn agent continuations |
| `CopilotCodingAgentDetector` | struct | Detector for Copilot coding-agent log patterns |
| `CreatedItemReport` | struct | Report of an item created by a safe-output action (type, URL, number, repo) |
| `CrossRunSummary` | struct | Summary of cross-run metrics across multiple workflow runs |
| `DependencyInfo` | struct | Metadata for a single dependency in `go.mod` or `package.json` |
| `DependencyInfoWithIndirect` | struct | `DependencyInfo` extended with an `Indirect` flag |
| `DevcontainerBuild` | struct | Build configuration section of `devcontainer.json` |
| `DevcontainerCodespaces` | struct | GitHub Codespaces-specific settings in `devcontainer.json` |
| `DevcontainerConfig` | struct | Parsed `.devcontainer/devcontainer.json` configuration |
| `DevcontainerCustomizations` | struct | VSCode customizations block in `devcontainer.json` |
| `DevcontainerRepoPermissions` | struct | Repository permissions block in `devcontainer.json` |
| `DevcontainerVSCode` | struct | VSCode-specific settings block in `devcontainer.json` |
| `DifcFilteredEvent` | struct | A DIFC-filtered event from the MCP gateway log |
| `DockerUnavailableError` | struct | Error returned when the Docker daemon is not reachable |
| `DomainAnalysis` | struct | Aggregated per-domain network request analysis |
| `DomainBreakdown` | struct | Per-domain outcome breakdown from outcome evaluation |
| `DomainBuckets` | struct | Domain requests bucketed by category (allow, deny, unknown) |
| `DomainDiffEntry` | struct | Per-domain diff between two runs |
| `DownloadResult` | struct | Result of a log artifact download |
| `EpisodeData` | struct | A single agent episode (one tool-call turn) |
| `ErrorSummary` | struct | Aggregated error summary for a workflow run |
| `FetchedWorkflow` | struct | A workflow fetched from a remote or local source with metadata |
| `FileInfo` | struct | File metadata captured during a workflow run |
| `FirewallAnalysis` | struct | Analysis of AWF network firewall logs |
| `FirewallDiff` | struct | Diff of firewall domain access between two audit runs |
| `FirewallDiffSummary` | struct | Summary statistics for a firewall diff |
| `FirewallLogEntry` | struct | A single entry from the AWF firewall log |
| `GatewayLogEntry` | struct | A log entry from the MCP gateway proxy |
| `GatewayMetrics` | struct | Aggregate metrics from MCP gateway logs |
| `GatewayServerMetrics` | struct | Per-server metrics from the MCP gateway |
| `GatewayToolMetrics` | struct | Per-tool metrics from the MCP gateway |
| `GitHubRateLimitDiff` | struct | Diff of GitHub API rate-limit consumption between two audit runs |
| `GitHubRateLimitEntry` | struct | A GitHub API rate-limit snapshot from the agent run |
| `GitHubWorkflow` | struct | Minimal GitHub Actions workflow metadata |
| `GuardPolicyEvent` | struct | A single guard-policy evaluation event from the MCP gateway log |
| `GuardPolicySummary` | struct | Summary of guard-policy evaluations during a run |
| `InitOptions` | struct | Options for `InitRepository` |
| `JobData` | struct | Data for a single GitHub Actions job |
| `JobInfo` | struct | Metadata for a GitHub Actions job |
| `JobInfoWithDuration` | struct | `JobInfo` extended with a human-readable duration string |
| `ListWorkflowRunsOptions` | struct | Options for listing workflow runs |
| `LockFileStatus` | struct | Status of a compiled `.lock.yml` file |
| `LogsData` | struct | Full log data downloaded for a workflow run |
| `LogsSummary` | struct | Summary view of downloaded log data |
| `MCPConfig` | struct | MCP server configuration as parsed from a workflow |
| `MCPFailureReport` | struct | Report of MCP server failures during a run |
| `MCPLogsGuardrailResponse` | struct | Guardrail evaluation response from MCP log analysis |
| `MCPPackage` | struct | An npm/pip package entry used by an MCP server |
| `MCPRegistryServerForProcessing` | struct | Server entry retrieved from the MCP registry |
| `MCPServerHealth` | struct | Health metrics for a single MCP server |
| `MCPServerHealthDetail` | struct | Detailed health breakdown for a single MCP server |
| `MCPSlowestToolCall` | struct | The slowest tool call recorded for an MCP server |
| `MCPToolCall` | struct | A single MCP tool invocation from an agent turn |
| `MCPToolDiffEntry` | struct | Per-tool diff entry between two audit runs |
| `MCPToolSummary` | struct | Aggregated MCP tool usage summary |
| `MCPToolUsageData` | struct | Per-tool usage counts and latencies |
| `MCPToolUsageSummary` | struct | Aggregate MCP tool usage summary for a run |
| `MCPToolsDiff` | struct | Full diff of MCP tool calls between two audit runs |
| `MCPToolsDiffSummary` | struct | Summary statistics for an MCP tools diff |
| `MetricsData` | struct | Core performance metrics for a workflow run |
| `MetricsTrendData` | struct | Trend data for a metric across multiple runs |
| `MissingDataReport` | struct | Report of missing expected data in a run |
| `MissingDataSummary` | struct | Aggregated summary of missing-data reports |
| `MissingToolReport` | struct | Report of a missing MCP tool during a run |
| `MissingToolSummary` | struct | Aggregated summary of missing-tool reports |
| `ModelTokenUsage` | struct | Token usage for a single AI model |
| `ModelTokenUsageRow` | struct | A single row in a model token usage table |
| `NoopReport` | struct | Report for a noop safe-output event |
| `ObservabilityInsight` | struct | An insight derived from observability data |
| `OverviewData` | struct | High-level overview data for a workflow run |
| `OutcomeEvaluation` | struct | Evaluation state embedded in `OutcomeReport` (status, merge/close metadata) |
| `OutcomeReport` | struct | Result of evaluating one safe output item — outcome, timing, human engagement, and objective value |
| `OutcomeStatus` | string alias | Outcome classification: `accepted`, `rejected`, `ignored`, `pending`, `skipped`, `unknown`, `lifecycle`, `lifecycle_close`, `error` |
| `OutcomeSummary` | struct | Aggregated outcome statistics across multiple safe output items |
| `OutcomesHistoryConfig` | struct | Configuration for `RunOutcomesHistory` |
| `PRCheckRun` | struct | A single CI check run attached to a pull request |
| `PRCommitStatus` | struct | A commit status context for a pull request |
| `PRInfo` | type alias | Backward-compatible alias for `PullRequest` |
| `PerRunFirewallBreakdown` | struct | Per-run firewall domain breakdown in a cross-run report |
| `PerformanceMetrics` | struct | Performance counters for a workflow run |
| `PolicyAnalysis` | struct | Analysis of guard-policy evaluation results |
| `PolicyManifest` | struct | A manifest of guard policies applied during a run |
| `FirewallPolicyRule` | struct | A single firewall policy rule from the policy manifest |
| `PolicySummaryDisplay` | struct | Display-friendly summary of policy evaluation results |
| `PollResult` | int alias | Result code returned by `PollWithSignalHandling` |
| `ProcessedRun` | struct | A fully-processed workflow run with parsed artifacts |
| `ProjectConfig` | struct | Configuration for `gh aw project new` |
| `PromptAnalysis` | struct | Analysis of the prompt sent to the agent |
| `ProxyInfo` | struct | Proxy server configuration for network requests |
| `PullRequest` | struct | A GitHub pull request |
| `RPCMessageEntry` | struct | A single RPC message from MCP gateway logs |
| `Recommendation` | struct | An actionable recommendation derived from audit data |
| `RedactedDomainsAnalysis` | struct | Analysis of redacted domain entries in firewall logs |
| `RedactedDomainsLogSummary` | struct | Summarised redacted-domain log data |
| `Release` | struct | A GitHub release entry |
| `Remote` | struct | A Git remote |
| `RepoSpec` | struct | A parsed repository specifier (`owner/repo[@ref]`) |
| `Repository` | struct | A GitHub repository |
| `RuleHitStats` | struct | Statistics for a single AWF firewall rule |
| `RunData` | struct | All data collected for a single workflow run |
| `RunMetricsDiff` | struct | Diff of core metrics between two audit runs |
| `RunSummary` | struct | Summary of a workflow run |
| `SafeOutputChainMetrics` | struct | Metrics for safe-output action chains in a run |
| `SafeOutputSummary` | struct | Summary of safe-output events in a run |
| `SafeOutputTypeDetail` | struct | Detailed information for a single safe-output type |
| `SecretInfo` | struct | Metadata for a configured repository secret |
| `SecretRequirement` | struct | A required secret for a workflow |
| `ServerDetail` | struct | Full details for a server from the MCP registry API |
| `ServerListResponse` | struct | Response envelope from the MCP registry `/v0.1/servers` endpoint |
| `ServerResponse` | struct | Response envelope wrapping server data and registry metadata |
| `SessionAnalysis` | struct | Analysis of agent session metadata |
| `ShellType` | string alias | Shell type detected by `DetectShell` (e.g. `"bash"`, `"zsh"`) |
| `SourceSpec` | struct | A parsed workflow source specifier (local, remote, or registry) |
| `TaskDomainInfo` | struct | Domain information associated with a specific agent task |
| `TokenUsageDiff` | struct | Diff of token usage between two audit runs |
| `TokenUsageEntry` | struct | Per-request token usage from the agent |
| `TokenUsageSummary` | struct | Aggregated token usage for a workflow run |
| `ToolCallDiffEntry` | struct | Per-tool-call diff entry between two audit runs |
| `ToolCallInfo` | type alias | Alias for `workflow.ToolCallInfo` — a single tool call record |
| `ToolCallsDiff` | struct | Full diff of tool calls between two audit runs |
| `ToolCallsDiffSummary` | struct | Summary statistics for a tool calls diff |
| `ToolTransition` | struct | A transition between tool calls in an agent episode |
| `ToolUsageInfo` | struct | Usage information for a single tool |
| `ToolUsageStatsBase` | struct | Identity and metrics shared by tool usage summaries |
| `ToolUsageSummary` | struct | Aggregated tool usage statistics |
| `Transport` | struct | MCP server transport configuration |
| `TrendDirection` | int alias | Direction of a metric trend (`TrendImproving`, `TrendStable`, `TrendDegrading`) |
| `TrialArtifacts` | struct | Artifacts generated during a trial run |
| `TrialRepoContext` | struct | Repository context used during a trial run |
| `VSCodeMCPServer` | struct | An MCP server entry in `.vscode/mcp.json` |
| `VSCodeSettings` | struct | Parsed `.vscode/settings.json` |
| `ValidationIssue` | struct | A validation error, warning, or audit issue entry |
| `ValidationResult` | struct | Result of a workflow compilation validation pass |
| `WorkflowDomainsDetail` | struct | Detailed per-workflow domain information |
| `WorkflowDomainsSummary` | struct | Summary of domains used across workflows |
| `WorkflowFailure` | struct | A workflow failure record |
| `WorkflowFileStatus` | struct | Status of a workflow file (exists, outdated, etc.) |
| `WorkflowListItem` | struct | A single item in `gh aw list`; shared workflow metadata fields (name, engine, compiled status, labels, triggers) also embedded in `WorkflowStatus` |
| `WorkflowMCPMetadata` | struct | MCP server metadata scanned from a workflow file |
| `WorkflowNode` | struct | A node in the workflow dependency graph |
| `WorkflowOption` | struct | A selectable workflow option for interactive prompts |
| `WorkflowRun` | struct | A GitHub Actions workflow run record |
| `WorkflowRunInfo` | struct | Summary of a workflow run from the GitHub API |
| `WorkflowSpec` | struct | A fully resolved workflow specification with source metadata |
| `WorkflowStats` | struct | Aggregate statistics for a workflow |
| `LogMetrics` | type alias | Alias for `workflow.LogMetrics` — log parsing metrics |
| `PostTransformFunc` | func type | A post-compilation transformation function |
| `LogParser[T]` | generic func type | Generic log-parser function type parameterized on analysis result |
| `ExperimentState` | struct | State stored in `experiments/*` git branches (counts and run history) |
| `ExperimentRunRecord` | struct | A single workflow run record in experiment state history |
| `ExperimentVariantStats` | struct | Counts for all variants of a named A/B experiment |
| `ExperimentInfo` | struct | Summary of a single experiment workflow (for `experiments list` output) |
| `ForecastResult` | struct | Full forecast result returned by `RunForecast` |
| `ForecastWorkflowResult` | struct | Per-workflow forecast result including Monte Carlo projections |
| `ForecastMonteCarloSummary` | struct | Monte Carlo simulation summary (P10/P50/P90 confidence intervals) |
| `ForecastEvaluation` | struct | Backtesting evaluation comparing forecast against actual runs |

### Constants and Sentinel Errors

#### Sentinel Errors

| Variable | Type | Description |
|----------|------|-------------|
| `ErrInterrupted` | `error` | Returned by signal-aware polling when the user interrupts with Ctrl-C. |
| `ErrNoArtifacts` | `error` | Returned when no run artifacts are found for a given workflow run. |

#### `CheckState` Constants

| Constant | Value | Description |
|----------|-------|-------------|
| `CheckStateFailed` | `"failed"` | At least one required check failed. |
| `CheckStatePending` | `"pending"` | Checks are still running. |
| `CheckStateNoChecks` | `"no_checks"` | No checks are configured for the workflow. |
| `CheckStatePolicyBlocked` | `"policy_blocked"` | A branch protection policy prevented the run. |
| `CheckStateSuccess` | `"success"` | All required checks passed. |

#### Docker Image Constants

| Constant | Value | Description |
|----------|-------|-------------|
| `ZizmorImage` | `"ghcr.io/zizmorcore/zizmor:latest"` | Default image for Zizmor security scanner. |
| `PoutineImage` | `"ghcr.io/boostsecurityio/poutine:latest"` | Default image for Poutine supply-chain scanner. |
| `ActionlintImage` | `"rhysd/actionlint:1.7.12"` | Default image for Actionlint workflow linter. |
| `RunnerGuardImage` | `"ghcr.io/vigilant-llc/runner-guard:latest"` | Default image for Runner Guard sandbox. |

#### Timeline Event Constants

| Constant | Value | Description |
|----------|-------|-------------|
| `TimelineSourceGateway` | `"gateway"` | Events originating from the MCP gateway. |
| `TimelineSourceFirewall` | `"firewall"` | Events originating from the AWF firewall. |
| `TimelineSourceAgent` | `"agent"` | Events originating from the agent itself. |
| `TimelineKindToolCall` | `"tool_call"` | A tool call event. |
| `TimelineKindDIFCFiltered` | `"difc_filtered"` | A DIFC-filtered event. |
| `TimelineKindGuardPolicyBlocked` | `"guard_blocked"` | A runner-guard policy block event. |
| `TimelineKindNetworkAllowed` | `"net_allowed"` | A network request that was allowed. |
| `TimelineKindNetworkBlocked` | `"net_blocked"` | A network request that was blocked. |
| `TimelineKindAgentTurn` | `"agent_turn"` | An agent turn boundary event. |
| `TimelineKindAgentToolStart` | `"agent_tool_start"` | Start of a tool call from the agent. |
| `TimelineKindAgentToolDone` | `"agent_tool_done"` | Completion of a tool call from the agent. |

#### `WorkflowIDExplanation`

A multi-line string constant (`string`) containing a user-facing explanation of what a workflow ID is: the basename of the Markdown workflow file without the `.md` extension.

## Usage Examples

### Compiling a workflow

```go
data, err := cli.CompileWorkflows(ctx, cli.CompileConfig{
    MarkdownFiles: []string{".github/workflows/my-workflow.md"},
    Verbose:       true,
    Validate:      true,
    Strict:        false,
})
```

### Running a workflow

```go
err := cli.RunWorkflowOnGitHub(ctx, "my-workflow", cli.RunOptions{
    Repo:    "owner/repo",
    Verbose: true,
})
```

### Auditing a run

```go
err := cli.AuditWorkflowRun(ctx, runID, cli.AuditOptions{
    Owner:     "owner",
    Repo:      "repo",
    Hostname:  "github.com",
    OutputDir: "/tmp/output",
    Verbose:   true,
    Parse:     true,
})
```

### Checking workflow health

```go
err := cli.RunHealth(cli.HealthConfig{
    Pattern:   "*.md",
    Threshold: 0.8,
    Period:    "30d",
})
```

## Design Decisions

- **File-per-feature decomposition**: Large feature domains (compile, audit, logs, run) are split into multiple files (`_command.go`, `_config.go`, `_helpers.go`, `_orchestrator.go`, etc.) to keep each file focused and under 300 lines.
- **Testable Run functions**: Every command has a `New*Command()` for Cobra wiring and a `Run*()` function with explicit parameters for unit testing without CLI arg parsing overhead.
- **Stderr for diagnostics**: All user-visible messages use `console.Format*Message` helpers and write to `stderr`, preserving `stdout` for structured machine-readable output.
- **Context propagation**: Long-running operations accept `context.Context` to support cancellation (SIGINT, timeouts).
- **Config structs**: Command options are collected into dedicated `*Config` or `*Options` structs rather than passed as long argument lists, improving readability and testability.

## Dependencies

**Internal**:
- `github.com/github/gh-aw/pkg/workflow` — workflow compilation and data types
- `github.com/github/gh-aw/pkg/parser` — markdown frontmatter parsing
- `github.com/github/gh-aw/pkg/console` — terminal output formatting
- `github.com/github/gh-aw/pkg/logger` — structured debug logging
- `github.com/github/gh-aw/pkg/constants` — engine names, job names, feature flags
- `github.com/github/gh-aw/pkg/agentdrain` — Drain log anomaly detection for audit analysis
- `github.com/github/gh-aw/pkg/envutil` — environment variable reading with bounds validation
- `github.com/github/gh-aw/pkg/errorutil` — shared error classification helpers for GitHub and gh CLI responses
- `github.com/github/gh-aw/pkg/semverutil` — semantic version comparison for dependency checks
- `github.com/github/gh-aw/pkg/workflow/compilerenv` — enterprise compiler-default and timezone override helpers
- `github.com/github/gh-aw/pkg/sliceutil` — slice utilities
- `github.com/github/gh-aw/pkg/stats` — incremental statistics for health metrics
- `github.com/github/gh-aw/pkg/styles` — terminal color styles and lipgloss configuration
- `github.com/github/gh-aw/pkg/timeutil` — human-readable duration formatting
- `github.com/github/gh-aw/pkg/tty` — terminal detection
- `github.com/github/gh-aw/pkg/types` — shared MCP server configuration types
- `github.com/github/gh-aw/pkg/typeutil` — type conversion helpers for dynamic frontmatter values
- `github.com/github/gh-aw/pkg/fileutil` — file system helpers
- `github.com/github/gh-aw/pkg/gitutil` — Git and GitHub CLI helpers
- `github.com/github/gh-aw/pkg/repoutil` — repository name parsing and normalization
- `github.com/github/gh-aw/pkg/stringutil` — string manipulation and sanitization utilities
- `github.com/github/gh-aw/pkg/syncutil` — thread-safe one-shot caching (used for repository slug lookup)
- `github.com/github/gh-aw/pkg/github` — label-based objective value mapping for issue prioritization scoring
- `github.com/github/gh-aw/pkg/intent` — intent attribution and resolution for mapping PRs and issues to labelled intent records
- `github.com/github/gh-aw/pkg/modelsdev` — model pricing lookup backed by the public `models.dev` catalog
- `github.com/github/gh-aw/pkg/setutil` — set operations backed by `map[K]struct{}`
- `github.com/github/gh-aw/pkg/colorwriter` — color-profile-aware writers and ANSI degradation for terminal output
- `github.com/github/gh-aw/pkg/ctxutil` — non-nil `context.Context` fallback for optional context options
- `github.com/github/gh-aw/pkg/githubapi` — go-gh REST client options with repository-standard defaults

**Test-only**:
- `github.com/github/gh-aw/pkg/testutil` — shared test fixtures and assertion helpers used by CLI package tests

**External**:
- `github.com/spf13/cobra` — CLI framework
- `github.com/cli/go-gh/v2` — GitHub CLI integration

## Thread Safety

Individual command `Run*` functions are not concurrently safe unless explicitly documented. The `CompileWorkflows` orchestrator serializes compilation by default; parallel compilation is gated by `CompileConfig` flags.

### Additional exported helpers

- MCP cache helpers: `GetPermission`, `SetPermission`, `GetRepo`, `SetRepo`
- HTTP wiring helpers: `RoundTrip`, `WriteHeader`
- External scanner helpers: `RunGrantOnLockFiles`, `RunGrypeOnLockFiles`, `RunSyftOnLockFiles`, `RunYamllintOnFiles`

<!-- BEGIN SOURCE-VERIFIED EXPORT COVERAGE -->
## Source-verified export coverage

This appendix is generated from the current non-test Go source files in this package and records any exported top-level symbols that are not already described above.

| Category | Count |
|----------|------:|
| Types | 287 |
| Constants | 77 |
| Variables | 2 |
| Functions and methods | 214 |
| Additional symbols documented in this appendix | 195 |

### Additional types

| File | Symbol | Declaration | Description |
|------|--------|-------------|-------------|
| `audit_cross_run.go` | `DomainInventoryEntry` | `type DomainInventoryEntry struct { Domain string `json:"domain"` SeenInRuns int `json:"seen_in_runs"` TotalAllowed int `json:"total_allowed"` TotalBlocked int `json:"total_blocked"` OverallStatus string `json:"overall_status"` // "allowed", "denied", "mixed" PerRunStatus []DomainRunStatus `json:"per_run_status"` }` | DomainInventoryEntry describes a single domain seen across multiple runs. |
| `audit_cross_run.go` | `DomainRunStatus` | `type DomainRunStatus struct { RunID int64 `json:"run_id"` Status string `json:"status"` // "allowed", "denied", "mixed", "absent" Allowed int `json:"allowed"` Blocked int `json:"blocked"` }` | DomainRunStatus records the status of a domain in a single run. |
| `audit_cross_run.go` | `ErrorTrendData` | `type ErrorTrendData struct { RunsWithErrors int `json:"runs_with_errors"` TotalErrors int `json:"total_errors"` AvgErrorsPerRun float64 `json:"avg_errors_per_run"` RunsWithWarnings int `json:"runs_with_warnings"` TotalWarnings int `json:"total_warnings"` }` | ErrorTrendData summarizes error and warning patterns across runs. |
| `audit_cross_run.go` | `MCPServerCrossRunHealth` | `type MCPServerCrossRunHealth struct { MCPServerStatsBase RunsConnected int `json:"runs_connected"` // Runs where server was used (appeared in tool usage) TotalRuns int `json:"total_runs"` ErrorRate float64 `json:"error_rate"` // 0.0–1.0 Unreliable bool `json:"unreliable"` // True if error_rate > 0.10 or connected < 75% of runs }` | MCPServerCrossRunHealth describes the health of a single MCP server across runs. |
| `audit_report.go` | `JobStepData` | `type JobStepData = JobStep` | JobStepData is an alias for JobStep (see logs_models.go). |
| `audit_report.go` | `MCPServerStats` | `type MCPServerStats struct { MCPServerStatsBase // RequestCount is kept for backward-compatible report schemas that label per-server // request volume; in MCP usage summaries this currently mirrors ToolCallCount. RequestCount int `json:"request_count" console:"header:Requests"` TotalInputSize int `json:"total_input_size" console:"header:Total Input,format:number"` TotalOutputSize int `json:"total_output_size" console:"header:Total Output,format:number"` AvgDuration string `json:"avg_duration,omitempty" console:"header:Avg Duration,omitempty"` }` | MCPServerStats contains server-level statistics |
| `audit_report.go` | `OverviewDisplay` | `type OverviewDisplay struct { RunID int64 `console:"header:Run ID"` Workflow string `console:"header:Workflow"` Status string `console:"header:Status"` Duration string `console:"header:Duration,omitempty"` Event string `console:"header:Event"` Branch string `console:"header:Branch"` URL string `console:"header:URL"` Files string `console:"header:Files,omitempty"` Experiment string `console:"header:Experiment,omitempty"` }` | OverviewDisplay is a display-optimized version of OverviewData for console rendering |
| `audit_report_experiments.go` | `ExperimentData` | `type ExperimentData struct { // Assignments maps each experiment name to the variant selected for this run. // e.g. {"caveman": "yes", "style": "concise"} Assignments map[string]string `json:"assignments"` // CumulativeCounts maps each experiment name to a per-variant invocation counter. // e.g. {"caveman": {"yes": 3, "no": 2}} CumulativeCounts map[string]map[string]int `json:"cumulative_counts,omitempty"` }` | ExperimentData represents the A/B experiment assignments for a single workflow run. |
| `compile_validation.go` | `CompileValidationOptions` | `type CompileValidationOptions struct { Verbose bool RunZizmorPerFile bool RunPoutinePerFile bool RunActionlintPerFile bool Strict bool ValidateActionSHAs bool }` | CompileValidationOptions holds optional validation flags for workflow compilation. |
| `deps_security.go` | `GitHubAdvisoryResponse` | `type GitHubAdvisoryResponse struct { GHSAID string `json:"ghsa_id"` CVEID string `json:"cve_id"` Summary string `json:"summary"` Severity string `json:"severity"` HTMLURL string `json:"html_url"` // Vulnerabilities contains affected versions and patches Vulnerabilities []struct { Package struct { Ecosystem string `json:"ecosystem"` Name string `json:"name"` } `json:"package"` VulnerableVersionRange string `json:"vulnerable_version_range"` FirstPatchedVersion string `json:"first_patched_version"` } `json:"vulnerabilities"` }` | GitHubAdvisoryResponse represents the GitHub Advisory API response |
| `devcontainer.go` | `DevcontainerFeatures` | `type DevcontainerFeatures map[string]any` | DevcontainerFeatures represents features to install in the devcontainer |
| `domains_command.go` | `DomainItem` | `type DomainItem struct { Domain string `json:"domain" console:"header:Domain"` Ecosystem string `json:"ecosystem" console:"header:Ecosystem"` Status string `json:"status" console:"header:Status"` }` | DomainItem represents a single domain entry for tabular display |
| `engine_secrets.go` | `EngineSecretConfig` | `type EngineSecretConfig struct { // Ctx is the context for cancellation (optional, but recommended for proper Ctrl-C handling) Ctx context.Context // RepoSlug is the repository slug to check for existing secrets (optional) RepoSlug string // Engine is the engine type to collect secrets for (e.g., "copilot", "claude", "codex") Engine string // Verbose enables verbose output Verbose bool // ExistingSecrets is a map of secret names that already exist in the repository ExistingSecrets map[string]struct{} // IncludeSystemSecrets includes system-level secrets like GH_AW_GITHUB_TOKEN IncludeSystemSecrets bool // IncludeOptional includes optional secrets in the requirements list IncludeOptional bool }` | EngineSecretConfig contains configuration for engine secret collection operations |
| `exit_code_error.go` | `ExitCodeError` | `type ExitCodeError struct { Code int }` | ExitCodeError is returned by library functions that need to propagate a specific process exit code to the cmd/ entry-point. |
| `experiments_analyze_statistics.go` | `ExperimentAnalysis` | `type ExperimentAnalysis struct { // ExperimentName is the name of the A/B experiment (key in state.counts). ExperimentName string `json:"experiment_name"` // Hypothesis is the null/alternative hypothesis text (from experiment config). Hypothesis string `json:"hypothesis,omitempty"` // AnalysisType is the statistical test declared in the experiment config // (t_test, mann_whitney, proportion_test, bayesian_ab). AnalysisType string `json:"analysis_type,omitempty"` // MinSamples is the minimum runs per variant required before analysis is reliable. // Defaults to 20 when not declared in the experiment config (R-STAT-007). MinSamples int `json:"min_samples"` // TotalRuns is the total number of observed runs across all variants. TotalRuns int `json:"total_runs"` // Variants holds per-variant statistics in alphabetical order. Variants []VariantAnalysis `json:"variants"` // Balance test (chi-square goodness-of-fit against expected allocation, §11.1). ChiSquare float64 `json:"chi_square"` DegreesOfFreedom int `json:"degrees_of_freedom"` PValue float64 `json:"p_value"` IsBalanced bool `json:"is_balanced"` // BonferroniAlpha is the Bonferroni-corrected significance threshold for experiments // with K ≥ 3 variants (§11.3: α_adjusted = 0.05 / (K − 1)). // Zero when fewer than 3 variants are declared. BonferroniAlpha float64 `json:"bonferroni_alpha,omitempty"` // Guardrails lists the declared metric thresholds. // Pass/fail evaluation requires per-run outcome data not stored in state.json (R-STAT-009). Guardrails []GuardrailStatus `json:"guardrails,omitempty"` // Recommendation is the analysis recommendation: EXTEND or READY_FOR_ANALYSIS. // EXTEND is issued when any variant is below min_samples (R-STAT-007). Recommendation string `json:"recommendation"` // Rationale is a one-sentence explanation of the recommendation. Rationale string `json:"rationale"` }` | ExperimentAnalysis holds statistical analysis results for one named A/B experiment. |
| `experiments_analyze_statistics.go` | `GuardrailStatus` | `type GuardrailStatus struct { Name string `json:"name"` Threshold string `json:"threshold"` }` | GuardrailStatus represents a declared guardrail metric threshold (R-STAT-009). |
| `experiments_analyze_statistics.go` | `VariantAnalysis` | `type VariantAnalysis struct { // Name is the variant identifier (e.g., "concise", "detailed"). Name string `json:"name"` // Count is the number of times this variant was selected (from state.counts). Count int `json:"count"` // ObservedPct is the observed percentage share of total runs (0–100). ObservedPct float64 `json:"observed_pct"` // ExpectedPct is the expected percentage share based on declared weights or equal split (0–100). ExpectedPct float64 `json:"expected_pct"` // MinSamples is the minimum required count for this variant. MinSamples int `json:"min_samples"` // BelowMinSamples is true when Count < MinSamples. BelowMinSamples bool `json:"below_min_samples"` }` | VariantAnalysis holds per-variant statistics for one experiment. |
| `experiments_command.go` | `ExperimentDetails` | `type ExperimentDetails struct { WorkflowID string `json:"workflow_id"` Branch string `json:"branch"` TotalRuns int `json:"total_runs"` Experiments []ExperimentVariantStats `json:"experiments"` RecentRuns []ExperimentRunRecord `json:"recent_runs,omitempty"` // Analyses holds the statistical analysis for each named experiment. // Populated by RunExperimentsAnalyze; absent in list output. Analyses []ExperimentAnalysis `json:"analyses,omitempty"` }` | ExperimentDetails represents detailed information about a specific experiment workflow. |
| `firewall_log.go` | `DomainRequestStats` | `type DomainRequestStats struct { Allowed int `json:"allowed"` Blocked int `json:"blocked"` }` | DomainRequestStats tracks request statistics per domain |
| `firewall_policy.go` | `EnrichedRequest` | `type EnrichedRequest struct { Timestamp float64 `json:"ts"` Host string `json:"host"` Status int `json:"status"` RuleID string `json:"rule_id"` Action string `json:"action"` // "allow" or "deny" Reason string `json:"reason,omitempty"` }` | EnrichedRequest represents a firewall request enriched with policy rule attribution. |
| `fix_codemods.go` | `GuidedError` | `type GuidedError struct { Cause error }` | GuidedError is returned when a codemod with Guided: true emits an error. |
| `forecast_types.go` | `ForecastRunSample` | `type ForecastRunSample struct { // RunID is the GitHub Actions run ID. RunID int64 `json:"run_id"` // AIC is the AI Credit cost for this individual run. AIC float64 `json:"aic"` // Date is the ISO-8601 calendar date the run started (YYYY-MM-DD). // Empty when the run's start timestamp is unavailable. Date string `json:"date,omitempty"` // RunURL links to the GitHub Actions run details page. RunURL string `json:"run_url,omitempty"` }` | ForecastRunSample holds the data for a single workflow run used in the forecast computation. |
| `forecast_types.go` | `ForecastVariantResult` | `type ForecastVariantResult struct { ExperimentName string `json:"experiment_name"` Variant string `json:"variant"` RunCount int `json:"run_count"` Fraction float64 `json:"fraction"` }` | ForecastVariantResult contains projected metrics split by A/B experiment variant. |
| `gateway_logs_timeline.go` | `TimelineEventKind` | `type TimelineEventKind string` | TimelineEventKind classifies the type of a unified timeline event. |
| `gateway_logs_timeline.go` | `TimelineEventSource` | `type TimelineEventSource string` | TimelineEventSource identifies which system produced a timeline event. |
| `gateway_logs_timeline.go` | `UnifiedTimelineEvent` | `type UnifiedTimelineEvent struct { Time time.Time // Normalised wall-clock time used for sorting Source TimelineEventSource // Which system produced this event Kind TimelineEventKind // Event classification // Gateway-specific fields (tool_call, difc_filtered, guard_blocked) ServerName string // MCP server name or server ID ToolName string // Tool name invoked Method string // JSON-RPC method (may duplicate ToolName) Status string // "success" or "error" Error string // Non-empty when Status == "error" Duration float64 // Round-trip time in milliseconds (0 when unknown) AuthorLogin string // GitHub login of the content author (DIFC events) // Firewall-specific fields (net_allowed, net_blocked) Host string // Target host (domain:port) HTTPMethod string // HTTP method (GET, CONNECT, …) HTTPStatus int // HTTP response status code Decision string // Proxy decision string (e.g. TCP_TUNNEL:HIER_DIRECT) // Agent-specific fields (agent_turn, agent_tool_start, agent_tool_done) TurnIndex int // 1-based conversation turn number (agent_turn events) ToolCallID string // Opaque call ID that pairs start/done events Success bool // True when tool execution succeeded (agent_tool_done events) // Message content fields (agent_turn, assistant_message, reasoning) // MessageContent holds the first portion of the message text for display. MessageContent string // Shared fields Reason string // Human-readable reason or description }` | UnifiedTimelineEvent represents a single event from the MCP Gateway, the AWF firewall, or the agent session, normalised to a common structure for merged timeline rendering. |
| `import_url_fetcher.go` | `FetchOptions` | `type FetchOptions struct { // HTTPClient overrides the default http.Client. When nil, a client with // importURLTimeout is used. Callers that supply their own client are // responsible for configuring an appropriate timeout. HTTPClient *http.Client }` | FetchOptions configures FetchImportURL. |
| `import_url_fetcher.go` | `FetchedResource` | `type FetchedResource struct { URL string // the original URL ContentType string // canonicalized media type without parameters (e.g. "application/json") Body []byte }` | FetchedResource is the result of fetching a URL for workflow import. |
| `interactive.go` | `InteractiveWorkflowBuilder` | `type InteractiveWorkflowBuilder struct { ctx context.Context nonTTYScanner *bufio.Scanner WorkflowName string Trigger string Engine string Tools []string SafeOutputs []string Intent string NetworkAccess string CustomDomains []string }` | InteractiveWorkflowBuilder collects user input to build an agentic workflow |
| `jsonworkflow_to_markdown.go` | `ConvertOptions` | `type ConvertOptions struct { // NameOverride, when non-empty, replaces the filename derived from the JSON. NameOverride string }` | ConvertOptions configures ConvertJSONWorkflowToMarkdown. |
| `jsonworkflow_to_markdown.go` | `GeneratedWorkflow` | `type GeneratedWorkflow struct { // Filename is the kebab-cased base name (without .md extension). Filename string // Markdown is the complete file content: YAML frontmatter followed by the prompt body. Markdown string // Warnings lists fields that could not be fully translated. Warnings []string }` | GeneratedWorkflow is the output of ConvertJSONWorkflowToMarkdown. |
| `jsonworkflow_to_markdown.go` | `IntervalTrigger` | `type IntervalTrigger struct { Types []string `json:"types"` }` | IntervalTrigger schedules the workflow. |
| `jsonworkflow_to_markdown.go` | `IssueTrigger` | `type IssueTrigger struct { Types []string `json:"types"` Query string `json:"query,omitempty"` }` | IssueTrigger fires when a GitHub issue is opened. |
| `jsonworkflow_to_markdown.go` | `JSONWorkflow` | `type JSONWorkflow struct { // Identification ID string `json:"id"` Name string `json:"name"` // Human-readable description → frontmatter description: Description string `json:"description"` // Main body / prompt text → markdown body after frontmatter. // Instructions takes precedence when both are set. Instructions string `json:"instructions"` // Prompt maps to the markdown body like Instructions does. // Instructions takes precedence when both are set. Prompt string `json:"prompt"` // Preferred AI engine → frontmatter engine: Engine string `json:"engine"` // On is a generic trigger configuration → frontmatter on: (passed through // as-is). Takes precedence over Triggers when both are set. On any `json:"on"` // Triggers is a structured trigger block that is converted to the gh-aw // "on:" frontmatter field via convertTriggersToOn. // The On field takes precedence when both are set. Triggers *JSONWorkflowTriggers `json:"triggers"` // Tools lists tool IDs → frontmatter tools: (converted via convertToolsToConfig). Tools []string `json:"tools"` // Permissions maps GitHub Actions permission scopes to access levels // (e.g. {"issues": "write"}) → frontmatter permissions: Permissions map[string]string `json:"permissions"` // Tags → frontmatter tags: Tags []string `json:"tags"` // Extra holds any top-level keys not listed above so they can be preserved // as a comment block. Extra map[string]any `json:"-"` }` | JSONWorkflow is a generic JSON workflow definition for import. |
| `jsonworkflow_to_markdown.go` | `JSONWorkflowTriggers` | `type JSONWorkflowTriggers struct { Interval *IntervalTrigger `json:"interval,omitempty"` Issues *IssueTrigger `json:"issues,omitempty"` WorkflowRun *WorkflowRunTrigger `json:"workflow_run,omitempty"` }` | JSONWorkflowTriggers is the structured trigger block for a JSON workflow. |
| `jsonworkflow_to_markdown.go` | `WorkflowRunTrigger` | `type WorkflowRunTrigger struct { Types []string `json:"types"` Workflows []string `json:"workflows"` Conclusions []string `json:"conclusions"` }` | WorkflowRunTrigger fires when a workflow run completes. |
| `log_aggregation.go` | `LogAnalysis` | `type LogAnalysis interface { // GetAllowedDomains returns the list of allowed domains GetAllowedDomains() []string // GetBlockedDomains returns the list of blocked domains GetBlockedDomains() []string }` | LogAnalysis is a read-only interface for accessing domain analysis results. |
| `log_aggregation.go` | `MutableLogAnalysis` | `type MutableLogAnalysis interface { LogAnalysis // SetAllowedDomains sets the list of allowed domains SetAllowedDomains(domains []string) // SetBlockedDomains sets the list of blocked domains SetBlockedDomains(domains []string) // AddMetrics adds metrics from another analysis AddMetrics(other LogAnalysis) }` | MutableLogAnalysis extends LogAnalysis with mutation methods for aggregation. |
| `logs_episode.go` | `EpisodeEdge` | `type EpisodeEdge struct { SourceRunID int64 `json:"source_run_id"` TargetRunID int64 `json:"target_run_id"` EdgeType string `json:"edge_type"` Confidence string `json:"confidence"` Reasons []string `json:"reasons,omitempty"` SourceRepo string `json:"source_repo,omitempty"` SourceRef string `json:"source_ref,omitempty"` EventType string `json:"event_type,omitempty"` EpisodeID string `json:"episode_id,omitempty"` }` | EpisodeEdge represents a deterministic lineage edge between two workflow runs. |
| `logs_episode.go` | `EpisodeToolCall` | `type EpisodeToolCall struct { Tool string `json:"tool"` Server string `json:"server"` Tokens int `json:"tokens"` DurationMS int64 `json:"duration_ms"` Status string `json:"status"` Error string `json:"error,omitempty"` }` | EpisodeToolCall represents a single MCP tool call within an episode. |
| `logs_github_rate_limit_usage.go` | `GitHubRateLimitResourceUsage` | `type GitHubRateLimitResourceUsage struct { Resource string `json:"resource" console:"header:Resource"` RequestsMade int `json:"requests_made" console:"header:Requests Made,format:number"` QuotaConsumed int `json:"quota_consumed" console:"header:Quota Consumed,format:number"` FinalRemaining int `json:"final_remaining" console:"header:Remaining,format:number"` Limit int `json:"limit" console:"header:Limit,format:number"` }` | GitHubRateLimitResourceUsage summarizes API usage for a single GitHub rate-limit resource category (e. |
| `logs_github_rate_limit_usage.go` | `GitHubRateLimitUsage` | `type GitHubRateLimitUsage struct { TotalRequestsMade int `json:"total_requests_made" console:"header:Total GitHub API Calls,format:number"` CoreConsumed int `json:"core_consumed" console:"header:Core Quota Consumed,format:number"` CoreConsumedSource string `json:"core_consumed_source,omitempty" console:"-"` CoreRemaining int `json:"core_remaining" console:"header:Core Remaining,format:number"` CoreLimit int `json:"core_limit" console:"header:Core Limit,format:number"` Resources []*GitHubRateLimitResourceUsage `json:"resources,omitempty"` }` | GitHubRateLimitUsage provides an aggregated view of GitHub API quota consumed by a single workflow run. |
| `logs_models.go` | `AggregatedSummaryBase` | `type AggregatedSummaryBase struct { Count int `json:"count" console:"header:Occurrences"` Workflows []string `json:"workflows" console:"-"` // List of workflow names WorkflowsDisplay string `json:"-" console:"header:Workflows,maxlen:40"` // Formatted display of workflows FirstReason string `json:"first_reason" console:"-"` // Reason from the first occurrence FirstReasonDisplay string `json:"-" console:"header:First Reason,maxlen:50"` // Formatted display of first reason RunIDs []int64 `json:"run_ids" console:"-"` // List of run IDs }` | AggregatedSummaryBase holds the shared tail fields that appear byte-for-byte identically in MissingToolSummary and MissingDataSummary (and as a subset in MCPFailureSummary). |
| `logs_models.go` | `JobStep` | `type JobStep struct { Name string `json:"name"` Status string `json:"status,omitempty"` Conclusion string `json:"conclusion,omitempty"` }` | JobStep represents basic information about an individual workflow job step. |
| `logs_models.go` | `MCPFailureSummary` | `type MCPFailureSummary struct { ServerName string `json:"server_name" console:"header:Server"` AggregatedSummaryBase `console:"-"` }` | MCPFailureSummary aggregates MCP server failure reports across runs |
| `logs_models.go` | `MCPServerStatsBase` | `type MCPServerStatsBase struct { ServerName string `json:"server_name" console:"header:Server"` ToolCallCount int `json:"tool_call_count" console:"header:Tool Calls"` // ErrorCount keeps the omitempty tags of MCPServerStats, the only embedder that // serializes/renders these tags directly; the other embedders override MarshalJSON. ErrorCount int `json:"error_count,omitempty" console:"header:Errors,omitempty"` }` | MCPServerStatsBase holds the per-server identity and volume fields shared by the MCP server health/stats report types. |
| `logs_models.go` | `ReportProvenance` | `type ReportProvenance struct { Timestamp string `json:"timestamp"` WorkflowName string `json:"workflow_name,omitempty"` // Tracks which workflow reported this RunID int64 `json:"run_id,omitempty"` // Tracks which run reported this ExperimentName string `json:"experiment_name,omitempty"` // Assigned experiment name for this run (if present) Variant string `json:"variant,omitempty"` // Assigned variant value for ExperimentName (if present) }` | ReportProvenance holds the shared provenance fields common to all report record types. |
| `logs_orchestrator_types.go` | `LogsDownloadOptions` | `type LogsDownloadOptions struct { WorkflowName string Count int StartDate string EndDate string OutputDir string Engine string Ref string BeforeRunID int64 AfterRunID int64 RepoOverride string Verbose bool ToolGraph bool NoStaged bool FirewallOnly bool NoFirewall bool Parse bool JSONOutput bool TimeoutMinutes int SummaryFile string SafeOutputType string FilteredIntegrity bool EvalsOnly bool Train bool Format string ArtifactSets []string After string ReportFile string }` | LogsDownloadOptions holds parameters for DownloadWorkflowLogs. |
| `logs_orchestrator_types.go` | `StdinLogsOptions` | `type StdinLogsOptions struct { RunURLs []string OutputDir string Engine string RepoOverride string Verbose bool ToolGraph bool NoStaged bool FirewallOnly bool NoFirewall bool Parse bool JSONOutput bool Timeout int SummaryFile string SafeOutputType string FilteredIntegrity bool EvalsOnly bool Train bool Format string ReportFile string // ArtifactSets defaults to nil (download all artifacts) when this API is used // programmatically. The CLI passes ["usage"] to match the logs command default. ArtifactSets []string }` | StdinLogsOptions holds parameters for DownloadWorkflowLogsFromStdin. |
| `logs_report_firewall.go` | `FirewallLogSummary` | `type FirewallLogSummary struct { TotalRequests int `json:"total_requests" console:"header:Total Requests"` AllowedRequests int `json:"allowed_requests" console:"header:Allowed"` BlockedRequests int `json:"blocked_requests" console:"header:Blocked"` AllowedDomains []string `json:"allowed_domains" console:"-"` BlockedDomains []string `json:"blocked_domains" console:"-"` RequestsByDomain map[string]DomainRequestStats `json:"requests_by_domain,omitempty" console:"-"` ByWorkflow map[string]*FirewallAnalysis `json:"by_workflow,omitempty" console:"-"` }` | FirewallLogSummary contains aggregated firewall log data |
| `mcp_registry_types.go` | `EnvironmentVariable` | `type EnvironmentVariable struct { Name string `json:"name"` Description string `json:"description,omitempty"` IsRequired bool `json:"isRequired,omitempty"` IsSecret bool `json:"isSecret,omitempty"` Default string `json:"default,omitempty"` Format string `json:"format,omitempty"` Placeholder string `json:"placeholder,omitempty"` Choices []string `json:"choices,omitempty"` }` | EnvironmentVariable represents an environment variable configuration |
| `mcp_tool_table.go` | `MCPToolTableOptions` | `type MCPToolTableOptions struct { // TruncateLength is the maximum length for tool descriptions before truncation // A value of 0 means no truncation TruncateLength int // ShowSummary controls whether to display the summary line at the bottom ShowSummary bool // SummaryFormat is the format string for the summary (default: "📊 Summary: %d allowed, %d not allowed out of %d total tools\n") SummaryFormat string // ShowVerboseHint controls whether to show the "Run with --verbose" hint in non-verbose mode ShowVerboseHint bool }` | MCPToolTableOptions configures how the MCP tool table is rendered |
| `outcome_evaluation.go` | `EvidenceStrength` | `type EvidenceStrength string` | EvidenceStrength describes how confidently the outcome can be inferred. |
| `outcome_evaluation.go` | `OutcomeStatus` | `type OutcomeStatus string` | OutcomeStatus is the normalized classification for a safe output outcome. |
| `packages.go` | `IncludeDependency` | `type IncludeDependency struct { SourcePath string // Path in the source (local) TargetPath string // Relative path where it should be copied in .github/workflows IsOptional bool // Whether this is an optional include (@include?) }` | IncludeDependency represents a file dependency from @include directives |
| `run_interactive.go` | `RunWorkflowOptions` | `type RunWorkflowOptions struct { WorkflowName string Verbose bool EngineOverride string RepoOverride string RefOverride string AutoMergePRs bool Push bool DryRun bool }` | RunWorkflowOptions holds parameters for RunSpecificWorkflowInteractively. |
| `token_usage_types.go` | `SubagentModelActual` | `type SubagentModelActual struct { Model string `json:"model"` Provider string `json:"provider,omitempty"` Requests int `json:"requests"` }` | SubagentModelActual captures model usage observed in token-usage logs. |
| `token_usage_types.go` | `SubagentModelRequest` | `type SubagentModelRequest struct { AgentName string `json:"agent_name"` RequestedModel string `json:"requested_model"` InvocationCount int `json:"invocation_count"` EffectiveModel string `json:"effective_model,omitempty"` ReasonCode string `json:"reason_code,omitempty"` }` | SubagentModelRequest captures requested/effective model attribution for a sub-agent. |
| `audit_report.go` | `AuditData` | `type AuditData struct { ... }` | AuditData is the top-level audit report payload assembled from all extracted artifacts and analyses. |
| `audit_diff.go` | `AuditDiff` | `type AuditDiff struct { ... }` | AuditDiff captures between-run differences for firewall, MCP, and run-level metrics. |
| `audit_cross_run.go` | `CrossRunAuditReport` | `type CrossRunAuditReport struct { ... }` | CrossRunAuditReport summarizes trends and comparisons across multiple workflow runs. |
| `audit_diff.go` | `DiffEntryBase` | `type DiffEntryBase struct { ... }` | DiffEntryBase contains shared status/anomaly fields used by diff entries. |
| `domain_buckets.go` | `AnalysisBase` | `type AnalysisBase struct { ... }` | AnalysisBase provides shared domain/request counters for firewall and domain analyses. |
| `logs_report_firewall.go` | `FirewallSummaryBase` | `type FirewallSummaryBase struct { ... }` | FirewallSummaryBase is the shared summary schema for firewall report rendering. |
| `experiments_analyze_statistics.go` | `MetricEvalResults` | `type MetricEvalResults struct { ... }` | MetricEvalResults tracks yes/no/unknown evaluation outcomes for one metric. |
| `logs_models.go` | `SkillActivation` | `type SkillActivation struct { ... }` | SkillActivation captures skill invocation telemetry extracted from logs. |
| `setup_repository.go` | `SetupAuthResult` | `type SetupAuthResult struct { ... }` | SetupAuthResult reports whether repository authentication checks succeeded. |
| `setup_repository.go` | `SetupRepositoryCheckResult` | `type SetupRepositoryCheckResult struct { ... }` | SetupRepositoryCheckResult captures setup preflight checks for repository/checkout state. |
| `syft.go` | `SyftScanResult` | `type SyftScanResult struct { ... }` | SyftScanResult contains summarized SBOM scan output metadata. |
| `token_usage_types.go` | `TokenCoreMetrics` | `type TokenCoreMetrics struct { ... }` | TokenCoreMetrics stores normalized token counts used in usage reporting. |
| `upgrade_command.go` | `UpgradeConfig` | `type UpgradeConfig struct { ... }` | UpgradeConfig controls upgrade command behavior and optional automation. |
| `trial_types.go` | `WorkflowTrialResult` | `type WorkflowTrialResult struct { ... }` | WorkflowTrialResult stores the outcome and extracted artifacts for a workflow trial run. |
| `update_workflows.go` | `UpdateWorkflowsOptions` | `type UpdateWorkflowsOptions struct { WorkflowNames []string AllowMajor bool Force bool Yes bool Verbose bool EngineOverride string WorkflowsDir string NoStopAfter bool StopAfter string NoMerge bool DisableReleaseBump bool DisableSecurityScanner bool NoCompile bool NoRedirect bool CoolDown time.Duration }` | UpdateWorkflowsOptions configures workflow update behavior. |
| `view_command.go` | `ViewOptions` | `type ViewOptions struct { Owner string Repo string Hostname string OutputDir string Verbose bool }` | ViewOptions holds configuration for the view command. |

### Additional constants and variables

| File | Kind | Symbol | Declaration | Description |
|------|------|--------|-------------|-------------|
| `gateway_logs_timeline.go` | `const` | `TimelineKindAssistantMessage` | `const TimelineKindAssistantMessage TimelineEventKind = "assistant_message"` | TimelineKindAssistantMessage is an assistant response message (assistant. |
| `gateway_logs_timeline.go` | `const` | `TimelineKindReasoning` | `const TimelineKindReasoning TimelineEventKind = "reasoning"` | TimelineKindReasoning is a model reasoning/thinking trace (reasoning or assistant. |
| `gateway_logs_timeline.go` | `const` | `TimelineKindSteering` | `const TimelineKindSteering TimelineEventKind = "steering"` | TimelineKindSteering is a budget or time pressure steering message injected by the AWF API proxy (token_steering or timeout_steering event from api-proxy-logs/events. |
| `health_metrics.go` | `const` | `TrendDegrading` | `const TrendDegrading` | Exported constant declared in `health_metrics.go`. |
| `health_metrics.go` | `const` | `TrendImproving` | `const TrendImproving TrendDirection = iota` | Exported constant declared in `health_metrics.go`. |
| `health_metrics.go` | `const` | `TrendStable` | `const TrendStable` | Exported constant declared in `health_metrics.go`. |
| `logs_artifact_set.go` | `const` | `ArtifactSetActivation` | `const ArtifactSetActivation ArtifactSet = "activation"` | ArtifactSetActivation downloads the activation artifact (aw_info. |
| `logs_artifact_set.go` | `const` | `ArtifactSetAgent` | `const ArtifactSetAgent ArtifactSet = "agent"` | ArtifactSetAgent downloads the unified agent artifact containing agent logs, safe outputs, token usage, and agent-side github_rate_limits. |
| `logs_artifact_set.go` | `const` | `ArtifactSetAll` | `const ArtifactSetAll ArtifactSet = "all"` | ArtifactSetAll downloads every artifact for the run (default behavior). |
| `logs_artifact_set.go` | `const` | `ArtifactSetDetection` | `const ArtifactSetDetection ArtifactSet = "detection"` | ArtifactSetDetection downloads the detection artifact containing threat detection log output. |
| `logs_artifact_set.go` | `const` | `ArtifactSetEvals` | `const ArtifactSetEvals ArtifactSet = "evals"` | ArtifactSetEvals downloads the evals artifact containing BinEval evaluation results (evals. |
| `logs_artifact_set.go` | `const` | `ArtifactSetExperiment` | `const ArtifactSetExperiment ArtifactSet = "experiment"` | ArtifactSetExperiment downloads the experiment artifact containing A/B experiment state (state. |
| `logs_artifact_set.go` | `const` | `ArtifactSetFirewall` | `const ArtifactSetFirewall ArtifactSet = "firewall"` | ArtifactSetFirewall downloads the agent artifact which now includes AWF network policy data: domain allow/deny decisions, firewall audit trail, and token-usage proxy logs. |
| `logs_artifact_set.go` | `const` | `ArtifactSetGitHubAPI` | `const ArtifactSetGitHubAPI ArtifactSet = "github-api"` | ArtifactSetGitHubAPI downloads the artifacts that contain GitHub API rate-limit logs (github_rate_limits. |
| `logs_artifact_set.go` | `const` | `ArtifactSetMCP` | `const ArtifactSetMCP ArtifactSet = "mcp"` | ArtifactSetMCP downloads the agent artifact which now includes MCP gateway traffic logs (gateway. |
| `logs_artifact_set.go` | `const` | `ArtifactSetUsage` | `const ArtifactSetUsage ArtifactSet = "usage"` | ArtifactSetUsage downloads the compact usage artifact produced by the conclusion job (aw-info. |
| `logs_models.go` | `const` | `APICallCooldown` | `const APICallCooldown = 500 * time.Millisecond` | APICallCooldown is the minimum pause between successive batch-fetch iterations to avoid hitting the GitHub API rate limit when processing many runs in a single invocation. |
| `logs_models.go` | `const` | `BatchSize` | `const BatchSize = 100` | BatchSize is the number of runs to fetch in each iteration |
| `logs_models.go` | `const` | `BatchSizeForAllWorkflows` | `const BatchSizeForAllWorkflows = 250` | BatchSizeForAllWorkflows is the larger batch size when searching for agentic workflows There can be a really large number of workflow runs in a repository, so we are generous in the batch size when used without qualific… |
| `logs_models.go` | `const` | `GitHubActionsRetentionDays` | `const GitHubActionsRetentionDays = 90` | GitHubActionsRetentionDays is GitHub's default log-retention window for GitHub Actions workflow runs. |
| `logs_models.go` | `const` | `MaxConcurrentDownloads` | `const MaxConcurrentDownloads = 10` | MaxConcurrentDownloads limits the number of parallel artifact downloads |
| `logs_models.go` | `const` | `MaxIterations` | `const MaxIterations = 20` | MaxIterations limits how many batches we fetch to prevent infinite loops |
| `logs_models.go` | `const` | `RateLimitThreshold` | `const RateLimitThreshold = 10` | RateLimitThreshold is the minimum number of GitHub API core requests that must remain before the rate-limit helper considers the budget healthy. |
| `mcp_inspect_mcp.go` | `const` | `MCPConnectTimeout` | `const MCPConnectTimeout = 10 * time.Second` | MCP timeout constants |
| `mcp_inspect_mcp.go` | `const` | `MCPOperationTimeout` | `const MCPOperationTimeout = 5 * time.Second` | MCP timeout constants |
| `mcp_inspect_mcp.go` | `const` | `MCPServerHTTPTimeout` | `const MCPServerHTTPTimeout = 30 * time.Minute` | MCP timeout constants |
| `mcp_logs_guardrail.go` | `const` | `CharsPerToken` | `const CharsPerToken = 4` | CharsPerToken is the approximate number of characters per token Using OpenAI's rule of thumb: ~4 characters per token |
| `mcp_registry_types.go` | `const` | `ArgumentTypeNamed` | `const ArgumentTypeNamed = "named"` | Argument type constants |
| `mcp_registry_types.go` | `const` | `ArgumentTypePositional` | `const ArgumentTypePositional = "positional"` | Argument type constants |
| `mcp_registry_types.go` | `const` | `StatusActive` | `const StatusActive = "active"` | Status constants for server status |
| `mcp_registry_types.go` | `const` | `StatusInactive` | `const StatusInactive = "inactive"` | Status constants for server status |
| `docker_images.go` | `const` | `GrantImage` | `const GrantImage = "anchore/grant:v0.6.8@sha256:..."` | GrantImage is the pinned image used to run Grant license scanning. |
| `docker_images.go` | `const` | `GrypeImage` | `const GrypeImage = "anchore/grype:v0.117.0@sha256:..."` | GrypeImage is the pinned image used to run Grype vulnerability scanning. |
| `docker_images.go` | `const` | `ShellcheckImage` | `const ShellcheckImage = "koalaman/shellcheck:v0.11.0@sha256:..."` | ShellcheckImage is the pinned image used for shellcheck validation. |
| `docker_images.go` | `const` | `SyftImage` | `const SyftImage = "anchore/syft:v1.51.0@sha256:..."` | SyftImage is the pinned image used for Syft SBOM generation. |
| `docker_images.go` | `const` | `YamllintImage` | `const YamllintImage = "pipelinecomponents/yamllint:latest@sha256:..."` | YamllintImage is the pinned image used for yamllint checks. |
| `flags.go` | `const` | `EngineFlagFilterUsage` | `const EngineFlagFilterUsage = "Filter logs by AI engine (...)"` | EngineFlagFilterUsage is the CLI help text for `--engine` log filtering. |
| `flags.go` | `const` | `EngineFlagOverrideUsage` | `const EngineFlagOverrideUsage = "Override AI engine (...)"` | EngineFlagOverrideUsage is the CLI help text for `--engine-override`. |
| `outcome_evaluation.go` | `const` | `OutcomeStatusLifecycleClose` | `const OutcomeStatusLifecycleClose OutcomeStatus = "lifecycle_close"` | OutcomeStatusLifecycleClose represents lifecycle close events in normalized outcome reporting. |
| `outcome_evaluation.go` | `const` | `OutcomeStatusAccepted` | `const OutcomeStatusAccepted OutcomeStatus = "accepted"` | Exported constant declared in `outcome_evaluation.go`. |
| `outcome_evaluation.go` | `const` | `OutcomeStatusError` | `const OutcomeStatusError OutcomeStatus = "error"` | Exported constant declared in `outcome_evaluation.go`. |
| `outcome_evaluation.go` | `const` | `OutcomeStatusIgnored` | `const OutcomeStatusIgnored OutcomeStatus = "ignored"` | Exported constant declared in `outcome_evaluation.go`. |
| `outcome_evaluation.go` | `const` | `OutcomeStatusLifecycle` | `const OutcomeStatusLifecycle OutcomeStatus = "lifecycle"` | Exported constant declared in `outcome_evaluation.go`. |
| `outcome_evaluation.go` | `const` | `OutcomeStatusPending` | `const OutcomeStatusPending OutcomeStatus = "pending"` | Exported constant declared in `outcome_evaluation.go`. |
| `outcome_evaluation.go` | `const` | `OutcomeStatusRejected` | `const OutcomeStatusRejected OutcomeStatus = "rejected"` | Exported constant declared in `outcome_evaluation.go`. |
| `outcome_evaluation.go` | `const` | `OutcomeStatusSkipped` | `const OutcomeStatusSkipped OutcomeStatus = "skipped"` | Exported constant declared in `outcome_evaluation.go`. |
| `outcome_evaluation.go` | `const` | `OutcomeStatusUnknown` | `const OutcomeStatusUnknown OutcomeStatus = "unknown"` | Exported constant declared in `outcome_evaluation.go`. |
| `outcome_evaluation.go` | `const` | `EvidenceMedium` | `const EvidenceMedium EvidenceStrength = "medium"` | Exported constant declared in `outcome_evaluation.go`. |
| `outcome_evaluation.go` | `const` | `EvidenceNone` | `const EvidenceNone EvidenceStrength = "none"` | Exported constant declared in `outcome_evaluation.go`. |
| `outcome_evaluation.go` | `const` | `EvidenceStrong` | `const EvidenceStrong EvidenceStrength = "strong"` | Exported constant declared in `outcome_evaluation.go`. |
| `outcome_evaluation.go` | `const` | `EvidenceWeak` | `const EvidenceWeak EvidenceStrength = "weak"` | Exported constant declared in `outcome_evaluation.go`. |
| `shell_completion.go` | `const` | `ShellBash` | `const ShellBash ShellType = "bash"` | Exported constant declared in `shell_completion.go`. |
| `shell_completion.go` | `const` | `ShellFish` | `const ShellFish ShellType = "fish"` | Exported constant declared in `shell_completion.go`. |
| `shell_completion.go` | `const` | `ShellPowerShell` | `const ShellPowerShell ShellType = "powershell"` | Exported constant declared in `shell_completion.go`. |
| `shell_completion.go` | `const` | `ShellUnknown` | `const ShellUnknown ShellType = "unknown"` | Exported constant declared in `shell_completion.go`. |
| `shell_completion.go` | `const` | `ShellZsh` | `const ShellZsh ShellType = "zsh"` | Exported constant declared in `shell_completion.go`. |
| `signal_aware_poll.go` | `const` | `PollContinue` | `const PollContinue PollResult = iota` | PollContinue indicates polling should continue |
| `signal_aware_poll.go` | `const` | `PollFailure` | `const PollFailure` | PollFailure indicates polling failed |
| `signal_aware_poll.go` | `const` | `PollSuccess` | `const PollSuccess` | PollSuccess indicates polling completed successfully |

### Additional functions and methods

| File | Symbol | Declaration | Description |
|------|--------|-------------|-------------|
| `access_log.go` | `(*DomainAnalysis).AddMetrics` | `func (*DomainAnalysis).AddMetrics(other LogAnalysis)` | AddMetrics adds metrics from another analysis |
| `compile_update_check.go` | `StartCompileUpdateCheck` | `func StartCompileUpdateCheck(ctx context.Context, noCheckUpdate bool, verbose bool) func()` | StartCompileUpdateCheck begins a best-effort update check for the compile command. |
| `copilot_agent.go` | `(*CopilotCodingAgentDetector).IsGitHubCopilotCodingAgent` | `func (*CopilotCodingAgentDetector).IsGitHubCopilotCodingAgent() bool` | IsGitHubCopilotCodingAgent uses heuristics to determine if this run was executed by GitHub Copilot coding agent (not the Copilot CLI engine or agentic workflows) |
| `copilot_agent.go` | `NewCopilotCodingAgentDetector` | `func NewCopilotCodingAgentDetector(runDir string, verbose bool) *CopilotCodingAgentDetector` | NewCopilotCodingAgentDetector creates a new detector for GitHub Copilot coding agent runs |
| `copilot_agent.go` | `NewCopilotCodingAgentDetectorWithPath` | `func NewCopilotCodingAgentDetectorWithPath(runDir string, verbose bool, workflowPath string) *CopilotCodingAgentDetector` | NewCopilotCodingAgentDetectorWithPath creates a detector with workflow path hint |
| `dependency_graph.go` | `(*DependencyGraph).BuildGraph` | `func (*DependencyGraph).BuildGraph(compiler *workflow.Compiler) error` | BuildGraph scans all workflow files and builds the dependency graph |
| `dependency_graph.go` | `(*DependencyGraph).GetAffectedWorkflows` | `func (*DependencyGraph).GetAffectedWorkflows(modifiedPath string) []string` | GetAffectedWorkflows returns the list of workflows that need to be recompiled when the given file is modified |
| `dependency_graph.go` | `(*DependencyGraph).RemoveWorkflow` | `func (*DependencyGraph).RemoveWorkflow(workflowPath string)` | RemoveWorkflow removes a workflow from the graph (e. |
| `dependency_graph.go` | `(*DependencyGraph).UpdateWorkflow` | `func (*DependencyGraph).UpdateWorkflow(workflowPath string, compiler *workflow.Compiler) error` | UpdateWorkflow updates a workflow in the graph (e. |
| `dependency_graph.go` | `NewDependencyGraph` | `func NewDependencyGraph(workflowsDir string) *DependencyGraph` | NewDependencyGraph creates a new dependency graph |
| `domain_buckets.go` | `(*DomainBuckets).GetAllowedDomains` | `func (*DomainBuckets).GetAllowedDomains() []string` | GetAllowedDomains returns the list of allowed domains |
| `domain_buckets.go` | `(*DomainBuckets).GetBlockedDomains` | `func (*DomainBuckets).GetBlockedDomains() []string` | GetBlockedDomains returns the list of blocked domains |
| `domain_buckets.go` | `(*DomainBuckets).SetAllowedDomains` | `func (*DomainBuckets).SetAllowedDomains(domains []string)` | SetAllowedDomains sets the list of allowed domains |
| `domain_buckets.go` | `(*DomainBuckets).SetBlockedDomains` | `func (*DomainBuckets).SetBlockedDomains(domains []string)` | SetBlockedDomains sets the list of blocked domains |
| `fetch.go` | `FetchWorkflowFromSourceWithContext` | `func FetchWorkflowFromSourceWithContext(ctx context.Context, spec *WorkflowSpec, verbose bool) (*FetchedWorkflow, error)` | FetchWorkflowFromSourceWithContext fetches a workflow file from local disk or GitHub. |
| `file_tracker.go` | `(*FileTracker).GetAllFiles` | `func (*FileTracker).GetAllFiles() []string` | GetAllFiles returns all tracked files (created and modified) |
| `file_tracker.go` | `(*FileTracker).RollbackAllFiles` | `func (*FileTracker).RollbackAllFiles(verbose bool) error` | RollbackAllFiles rolls back both created and modified files |
| `file_tracker.go` | `(*FileTracker).RollbackCreatedFiles` | `func (*FileTracker).RollbackCreatedFiles(verbose bool) error` | RollbackCreatedFiles deletes all files that were created during the operation |
| `file_tracker.go` | `(*FileTracker).RollbackModifiedFiles` | `func (*FileTracker).RollbackModifiedFiles(verbose bool) error` | RollbackModifiedFiles restores all modified files to their original state |
| `file_tracker.go` | `(*FileTracker).StageAllFiles` | `func (*FileTracker).StageAllFiles(verbose bool) error` | StageAllFiles stages all tracked files using git add |
| `file_tracker.go` | `(*FileTracker).TrackCreated` | `func (*FileTracker).TrackCreated(filePath string)` | TrackCreated adds a file to the created files list |
| `file_tracker.go` | `(*FileTracker).TrackModified` | `func (*FileTracker).TrackModified(filePath string)` | TrackModified adds a file to the modified files list and stores its original content |
| `file_tracker.go` | `NewFileTracker` | `func NewFileTracker() *FileTracker` | NewFileTracker creates a new file tracker |
| `firewall_log.go` | `(*FirewallAnalysis).AddMetrics` | `func (*FirewallAnalysis).AddMetrics(other LogAnalysis)` | AddMetrics adds metrics from another analysis |
| `fix_codemods.go` | `(*GuidedError).Unwrap` | `func (*GuidedError).Unwrap() error` | Exported function or method declared in `fix_codemods.go`. |
| `fix_codemods.go` | `GetCodemods` | `func GetCodemods(disabledIDs []string) ([]Codemod, error)` | GetCodemods returns all codemods except any explicitly disabled by ID. |
| `frontmatter_editor.go` | `RemoveTopLevelFieldFromFrontmatter` | `func RemoveTopLevelFieldFromFrontmatter(content, fieldName string) (string, error)` | RemoveTopLevelFieldFromFrontmatter removes a root-level frontmatter key while preserving YAML structure. |
| `gateway_logs_timeline.go` | `BuildUnifiedTimeline` | `func BuildUnifiedTimeline(logDir string, verbose bool) ([]UnifiedTimelineEvent, error)` | BuildUnifiedTimeline collects all JSONL events from the MCP Gateway, the AWF firewall, the agent session, and the AWF API proxy in logDir, merges them into a single slice, and sorts the slice in ascending wall-clock ord… |
| `import_url_fetcher.go` | `FetchImportURL` | `func FetchImportURL(ctx context.Context, rawURL string, opts FetchOptions) (*FetchedResource, error)` | FetchImportURL fetches rawURL and returns its content and canonicalized Content-Type. |
| `interactive.go` | `CreateWorkflowInteractively` | `func CreateWorkflowInteractively(ctx context.Context, workflowName string, verbose bool, force bool) error` | CreateWorkflowInteractively prompts the user to build a workflow interactively |
| `jsonworkflow_to_markdown.go` | `(*JSONWorkflow).UnmarshalJSON` | `func (*JSONWorkflow).UnmarshalJSON(data []byte) error` | UnmarshalJSON implements json. |
| `jsonworkflow_to_markdown.go` | `ConvertJSONWorkflowToMarkdown` | `func ConvertJSONWorkflowToMarkdown(a *JSONWorkflow, opts ConvertOptions) (*GeneratedWorkflow, error)` | ConvertJSONWorkflowToMarkdown converts a JSONWorkflow into a gh-aw markdown workflow file. |
| `logs_github_rate_limit_usage.go` | `(*GitHubRateLimitUsage).ResourceRows` | `func (*GitHubRateLimitUsage).ResourceRows() []*GitHubRateLimitResourceUsage` | ResourceRows returns per-resource rows sorted by total requests made descending, suitable for console table rendering. |
| `logs_models.go` | `(*AwInfo).GetFirewallVersion` | `func (*AwInfo).GetFirewallVersion() string` | GetFirewallVersion returns the AWF firewall version, preferring the new field name (awf_version) but falling back to the old field name (firewall_version) for backward compatibility with older aw_info. |
| `logs_orchestrator_stdin.go` | `DownloadWorkflowLogsFromStdin` | `func DownloadWorkflowLogsFromStdin(ctx context.Context, opts StdinLogsOptions) error` | DownloadWorkflowLogsFromStdin fetches and processes workflow run logs for runs provided as IDs or URLs, bypassing the GitHub API run-discovery step. |
| `mcp_registry.go` | `(*MCPRegistryClient).SearchServers` | `func (*MCPRegistryClient).SearchServers(ctx context.Context, query string) ([]MCPRegistryServerForProcessing, error)` | SearchServers searches for MCP servers in the registry by fetching all servers and filtering locally |
| `mcp_registry.go` | `NewMCPRegistryClient` | `func NewMCPRegistryClient(registryURL string) *MCPRegistryClient` | NewMCPRegistryClient creates a new MCP registry client |
| `mcp_schema.go` | `AddSchemaDefault` | `func AddSchemaDefault(schema *jsonschema.Schema, propertyName string, value any) error` | AddSchemaDefault adds a default value to a property in a JSON schema. |
| `mcp_schema.go` | `GenerateSchema` | `func GenerateSchema[T any]() (*jsonschema.Schema, error)` | GenerateSchema generates a JSON schema from a Go struct type. |
| `update_merge.go` | `MergeWorkflowContent` | `func MergeWorkflowContent(base, current, new, oldSourceSpec, newRefOrSourceSpec, localWorkflowPath string, verbose bool) (string, bool, error)` | MergeWorkflowContent performs marker-aware merging for source-managed workflow updates. |
| `compile_external_tools.go` | `RunActionlintOnFiles` | `func RunActionlintOnFiles(ctx context.Context, lockFiles []string, verbose bool, strict bool) error` | RunActionlintOnFiles runs actionlint over generated lock files. |
| `add_interactive_orchestrator.go` | `RunAddInteractive` | `func RunAddInteractive(ctx context.Context, config *AddInteractiveConfig) error` | RunAddInteractive drives the interactive `gh aw add` installation flow. |
| `outcome_domain_breakdown.go` | `ComputeDomainBreakdowns` | `func ComputeDomainBreakdowns(reports []OutcomeReport) []DomainBreakdown` | ComputeDomainBreakdowns aggregates outcome metrics by label/domain. |
| `packages.go` | `ExtractWorkflowPrivateSetting` | `func ExtractWorkflowPrivateSetting(content string) (bool, bool)` | ExtractWorkflowPrivateSetting extracts the private field from workflow content string. |
| `pr_automerge.go` | `AutoMergePullRequestsLegacy` | `func AutoMergePullRequestsLegacy(repoSlug string, verbose bool) error` | AutoMergePullRequestsLegacy is the legacy function that auto-merges all open PRs (used by trial command for backward compatibility) |
| `project_timezone.go` | `ConfigureProjectTimezone` | `func ConfigureProjectTimezone()` | ConfigureProjectTimezone applies the configured project timezone to CLI time rendering. |
| `token_usage_analyze.go` | `(*TokenUsageSummary).AvgDurationMs` | `func (*TokenUsageSummary).AvgDurationMs() int` | AvgDurationMs returns the average request duration in milliseconds |
| `token_usage_analyze.go` | `(*TokenUsageSummary).ModelRows` | `func (*TokenUsageSummary).ModelRows() []ModelTokenUsageRow` | ModelRows returns the by-model data as sorted rows for console rendering |
| `token_usage_analyze.go` | `(*TokenUsageSummary).TotalTokens` | `func (*TokenUsageSummary).TotalTokens() int` | TotalTokens returns the sum of all token types |
| `tool_graph.go` | `(*ToolGraph).AddSequence` | `func (*ToolGraph).AddSequence(tools []string)` | AddSequence adds a tool call sequence to the graph |
| `tool_graph.go` | `(*ToolGraph).GenerateMermaidGraph` | `func (*ToolGraph).GenerateMermaidGraph() string` | GenerateMermaidGraph generates a Mermaid state diagram from the tool graph |
| `tool_graph.go` | `NewToolGraph` | `func NewToolGraph() *ToolGraph` | NewToolGraph creates a new empty tool graph |
| `view_command.go` | `ViewWorkflowRun` | `func ViewWorkflowRun(ctx context.Context, runID int64, opts ViewOptions) error` | ViewWorkflowRun downloads artifacts for the given run (if not already cached) and renders the unified event timeline, safe outputs, and a link to the run page. |
| `vscode_config.go` | `(*VSCodeSettings).UnmarshalJSON` | `func (*VSCodeSettings).UnmarshalJSON(data []byte) error` | UnmarshalJSON custom unmarshaler for VSCodeSettings to preserve unknown fields |
| `vscode_config.go` | `(VSCodeSettings).MarshalJSON` | `func (VSCodeSettings).MarshalJSON() ([]byte, error)` | MarshalJSON custom marshaler for VSCodeSettings to include all fields |

<!-- END SOURCE-VERIFIED EXPORT COVERAGE -->

### Additional exported top-level functions (verified 2026-08-08)

| File | Function | Signature | Description |
|------|----------|-----------|-------------|
| `actions_build_command.go` | `ActionsBuildCommand` | `func ActionsBuildCommand() error` | Builds all custom GitHub Actions by bundling JavaScript dependencies. |
| `actions_build_command.go` | `ActionsCleanCommand` | `func ActionsCleanCommand() error` | Removes generated `index.js` files from all actions. |
| `actions_build_command.go` | `ActionsValidateCommand` | `func ActionsValidateCommand() error` | Validates all `action.yml` files. |
| `ci.go` | `IsRunningInCI` | `func IsRunningInCI() bool` | Checks if the process is running in a CI environment. |
| `compile_external_tools.go` | `RunGrantOnLockFiles` | `func RunGrantOnLockFiles(lockFiles []string, verbose bool, strict bool) error` | Runs the `grant` tool against lock files derived from `gh-aw-manifest` headers. |
| `compile_external_tools.go` | `RunGrypeOnLockFiles` | `func RunGrypeOnLockFiles(lockFiles []string, verbose bool, strict bool) error` | Runs Grype against pinned images referenced by lock files, deduplicated and cached per image. |
| `compile_external_tools.go` | `RunPoutineOnDirectory` | `func RunPoutineOnDirectory(workflowDir string, verbose bool, strict bool) error` | Scans all workflows in a directory once with Poutine. |
| `compile_external_tools.go` | `RunRunnerGuardOnDirectory` | `func RunRunnerGuardOnDirectory(workflowDir string, verbose bool, strict bool) error` | Scans all workflows in a directory once with runner-guard. |
| `compile_external_tools.go` | `RunShellcheckOnLockFilesAndResources` | `func RunShellcheckOnLockFilesAndResources(ctx context.Context, lockFiles []string, resources []workflow.ShellScriptResource, verbose bool, strict bool) error` | Runs shellcheck across lock files and shell script resources; returns nil when shellcheck is unavailable. |
| `compile_external_tools.go` | `RunSyftOnLockFiles` | `func RunSyftOnLockFiles(lockFiles []string, verbose bool, strict bool) error` | Generates SBOMs with Syft from images referenced by lock file manifests. |
| `compile_external_tools.go` | `RunYamllintOnFiles` | `func RunYamllintOnFiles(lockFiles []string, verbose bool, strict bool) error` | Runs yamllint once across all provided lock files. |
| `compile_external_tools.go` | `RunZizmorOnFiles` | `func RunZizmorOnFiles(lockFiles []string, verbose bool, strict bool) error` | Runs zizmor once across all provided lock files. |
| `copilot_agent_logs.go` | `ParseCopilotCodingAgentLogMetrics` | `func ParseCopilotCodingAgentLogMetrics(logContent string, verbose bool) workflow.LogMetrics` | Parses GitHub Copilot coding agent logs, which use a different format than Copilot CLI logs. |
| `docker_images.go` | `IsDockerAvailable` | `func IsDockerAvailable(ctx context.Context) bool` | Checks if the Docker daemon is running and accessible. |
| `docker_images.go` | `IsDockerImageAvailable` | `func IsDockerImageAvailable(ctx context.Context, image string) bool` | Checks if a Docker image is available locally. |
| `docker_images.go` | `IsDockerImageDownloading` | `func IsDockerImageDownloading(image string) bool` | Checks if a Docker image is currently being downloaded. |
| `docker_images.go` | `StartDockerImageDownload` | `func StartDockerImageDownload(ctx context.Context, image string) (bool, func() error)` | Starts a background Docker image pull and returns a wait function for its result. |
| `domains_command.go` | `RunListDomains` | `func RunListDomains(jsonOutput bool) error` | Lists all workflows with their domain configuration summary. |
| `domains_command.go` | `RunWorkflowDomains` | `func RunWorkflowDomains(workflowArg string, jsonOutput bool) error` | Lists all effective domains for a specific workflow. |
| `enable.go` | `DisableWorkflowsByNames` | `func DisableWorkflowsByNames(ctx context.Context, workflowNames []string, repoOverride string) error` | Disables workflows by specific names, or all workflows if none are provided. |
| `enable.go` | `EnableWorkflowsByNames` | `func EnableWorkflowsByNames(ctx context.Context, workflowNames []string, repoOverride string) error` | Enables workflows by specific names, or all workflows if none are provided. |
| `fix_codemods.go` | `GetAllCodemods` | `func GetAllCodemods() []Codemod` | Returns all available codemods in the registry. |
| `fix_command.go` | `RunFix` | `func RunFix(config FixConfig) error` | Runs the `fix` command with the given configuration. |
| `generate_action_metadata_command.go` | `GenerateActionMetadataCommand` | `func GenerateActionMetadataCommand() error` | Generates `action.yml` and README.md files for JavaScript modules. |
| `hash_command.go` | `RunHashFrontmatter` | `func RunHashFrontmatter(workflowPath string) error` | Computes and prints the frontmatter hash for a workflow. |
| `includes.go` | `FetchIncludeFromSource` | `func FetchIncludeFromSource(ctx context.Context, includePath string, baseSpec *WorkflowSpec, verbose bool) ([]byte, string, error)` | Fetches include content and its `#fragment` section from a local or remote source. |
| `list_workflows_command.go` | `RunListWorkflows` | `func RunListWorkflows(ctx context.Context, repo, path, pattern string, verbose bool, jsonOutput bool, labelFilter string) error` | Lists workflows without checking their GitHub Actions status. |
| `logs_artifact_set.go` | `ResolveArtifactFilter` | `func ResolveArtifactFilter(sets []string) []string` | Resolves named artifact sets to a filter list; returns nil for `ArtifactSetAll`. |
| `logs_artifact_set.go` | `ValidArtifactSetNames` | `func ValidArtifactSetNames() []string` | Returns the valid artifact set names, derived dynamically from the artifact-set map. |
| `logs_artifact_set.go` | `ValidateArtifactSets` | `func ValidateArtifactSets(sets []string) error` | Validates artifact set names, returning an error listing any unrecognized names. |
| `logs_metrics.go` | `ExtractLogMetricsFromRun` | `func ExtractLogMetricsFromRun(processedRun ProcessedRun) workflow.LogMetrics` | Extracts log metrics from a processed run's log directory. |
| `mcp_safe_update_cache.go` | `CollectLockFileManifests` | `func CollectLockFileManifests(workflowsDir string) map[string]*workflow.GHAWManifest` | Collects manifests from lock files so they cannot be tampered with by the agent. |
| `mcp_safe_update_cache.go` | `WritePriorManifestFile` | `func WritePriorManifestFile(cache map[string]*workflow.GHAWManifest) (string, error)` | Writes a prior-manifest cache file and returns its path; caller must remove it when done. |
| `outcomes_history.go` | `RunOutcomesHistory` | `func RunOutcomesHistory(config OutcomesHistoryConfig) error` | Runs the outcomes-history report for a given configuration. |
| `project_command.go` | `RunProjectNew` | `func RunProjectNew(ctx context.Context, config ProjectConfig) error` | Executes the project creation logic. |
| `remove_command.go` | `RemoveWorkflows` | `func RemoveWorkflows(pattern string, keepOrphans bool, workflowDir string) error` | Removes workflows matching a pattern. |
| `run_interactive.go` | `RunSpecificWorkflowInteractively` | `func RunSpecificWorkflowInteractively(ctx context.Context, opts RunWorkflowOptions) error` | Runs a known workflow interactively, collecting inputs if the workflow defines any. |
| `run_interactive.go` | `RunWorkflowInteractively` | `func RunWorkflowInteractively(ctx context.Context, opts RunWorkflowOptions) error` | Runs a workflow in interactive mode. |
| `run_workflow_validation.go` | `IsRunnable` | `func IsRunnable(markdownPath string) (bool, error)` | Checks whether the compiled `.lock.yml` for a workflow is runnable. |
| `setup_repository.go` | `RunSetupAuth` | `func RunSetupAuth(opts SetupAuthOptions) error` | Runs the repository authentication setup flow. |
| `setup_repository.go` | `RunSetupRepositoryCheck` | `func RunSetupRepositoryCheck(opts SetupRepositoryCheckOptions) error` | Runs the repository setup readiness check. |
| `trial_runner.go` | `RunWorkflowTrials` | `func RunWorkflowTrials(ctx context.Context, workflowSpecs []string, opts TrialOptions) error` | Executes trial runs for one or more workflows. |
| `update_actions_lockfile.go` | `UpdateActions` | `func UpdateActions(ctx context.Context, allowMajor, verbose, disableReleaseBump bool, coolDown time.Duration) error` | Updates pinned GitHub Actions references, bumping SHAs and cooldown state. |
| `update_actions_workflow_files.go` | `UpdateActionsInWorkflowFiles` | `func UpdateActionsInWorkflowFiles(ctx context.Context, workflowsDir, engineOverride string, verbose, disableReleaseBump bool, noCompile bool, coolDown time.Duration, approve bool) error` | Scans workflow files and updates `actions/*` references. |
| `validators.go` | `ValidateWorkflowName` | `func ValidateWorkflowName(s string) error` | Ensures a workflow name is non-empty and contains only alphanumeric characters, hyphens, and underscores. |

Method-form symbols on exported types (e.g. `(*FileTracker).TrackCreated`, `(*DomainBuckets).SetAllowedDomains`, `(*ToolGraph).AddSequence`) are already documented in the "Additional functions and methods" table above; the entries here cover only package-level functions that were not yet listed.

## Source Synchronization

Reviewed against recent source updates on 2026-08-08; the "Additional exported top-level functions" table above was added after diffing the full exported-symbol list against this README. No other public-contract deltas were identified beyond the sections above.

---

*This specification is automatically maintained by the [spec-extractor](../../.github/workflows/spec-extractor.md) workflow.*

// This file provides command-line interface functionality for gh-aw.
// This file (logs_orchestrator_types.go) contains type definitions used by the
// logs orchestrator: option structs for public API functions and internal helper types.

package cli

// LogsDownloadOptions holds parameters for DownloadWorkflowLogs.
type LogsDownloadOptions struct {
	WorkflowName   string
	Count          int
	StartDate      string
	EndDate        string
	OutputDir      string
	Engine         string
	Runtime        string
	Ref            string
	BeforeRunID    int64
	AfterRunID     int64
	RepoOverride   string
	Verbose        bool
	ToolGraph      bool
	NoStaged       bool
	FirewallOnly   bool
	NoFirewall     bool
	Parse          bool
	JSONOutput     bool
	TimeoutMinutes int
	TimeoutSeconds int
	// MaxGitHubAPIRateLimit is the maximum number of core API requests that may
	// be used in the current window before downloads wait for the reset. Negative
	// values reserve that many requests from the API-reported limit.
	MaxGitHubAPIRateLimit int
	// MaxStorageMB prunes non-essential cache data and stops new artifact
	// downloads when OutputDir cannot be reduced below this size.
	MaxStorageMB int
	// PruneOlderRuns allows the storage limiter to remove complete older run
	// directories after non-essential cache pruning cannot free enough space.
	PruneOlderRuns    bool
	SummaryFile       string
	SafeOutputType    string
	FilteredIntegrity bool
	EvalsOnly         bool
	GradersOnly       bool
	Audit             bool
	Train             bool
	Format            string
	ArtifactSets      []string
	After             string
	ReportFile        string
	// SuppressRender downloads and processes runs (including writing the summary
	// file) without emitting any report to stdout. Callers that only need the
	// downloaded artifacts, and that own stdout themselves, set this so their own
	// output is not interleaved with the logs report.
	SuppressRender bool
	// Internal orchestration flags used when several workflow targets share one
	// command invocation.
	skipEnsureGitignore    bool
	rateLimitFirstRequest  bool
	maxConcurrentDownloads int
	storageLimit           *logsStorageLimit
}

type workflowLogsResult struct {
	processedRuns       []ProcessedRun
	artifactFilter      []string
	continuation        *ContinuationData
	countLimitReached   bool
	timeoutReached      bool
	storageLimitReached bool
}

// StdinLogsOptions holds parameters for DownloadWorkflowLogsFromStdin.
type StdinLogsOptions struct {
	RunURLs           []string
	OutputDir         string
	Engine            string
	Runtime           string
	RepoOverride      string
	Verbose           bool
	ToolGraph         bool
	NoStaged          bool
	FirewallOnly      bool
	NoFirewall        bool
	Parse             bool
	JSONOutput        bool
	Timeout           int
	MaxStorageMB      int
	PruneOlderRuns    bool
	SummaryFile       string
	SafeOutputType    string
	FilteredIntegrity bool
	EvalsOnly         bool
	GradersOnly       bool
	Audit             bool
	Train             bool
	Format            string
	ReportFile        string
	// ArtifactSets defaults to nil (download all artifacts) when this API is used
	// programmatically. The CLI passes ["usage"] to match the logs command default.
	ArtifactSets []string
}

// continuationOptions carries the parameters needed by buildContinuationIfNeeded
// to produce a ContinuationData cursor for paginated log fetches.
type continuationOptions struct {
	workflowName          string
	startDate             string
	endDate               string
	engine                string
	branch                string
	afterRunID            int64
	count                 int
	timeoutMinutes        int
	maxGitHubAPIRateLimit int
	maxStorageMB          int
	pruneOlderRuns        bool
	// lastFetchedBeforeDate is the pagination date cursor collectProcessedWorkflowRuns
	// had advanced to when it stopped (from the oldest run actually fetched from the
	// API, including batches that yielded zero matching runs). When set, it is used
	// as the continuation's end_date so a resumed request starts scanning from where
	// this one left off instead of re-scanning the whole original window from the
	// newest run again.
	lastFetchedBeforeDate string
	// previousBeforeRunID is the before_run_id the caller passed into this request
	// (LogsDownloadOptions.BeforeRunID). It is used as the continuation cursor when
	// no runs were processed in this batch, so a storage-limited request with zero
	// progress does not reset the cursor to zero and re-scan from the newest run.
	previousBeforeRunID int64
}

// renderLogsOutputOptions holds configuration for renderLogsOutput.
type renderLogsOutputOptions struct {
	outputDir      string
	summaryFile    string
	format         string
	reportFile     string
	jsonOutput     bool
	toolGraph      bool
	train          bool
	audit          bool
	continuation   *ContinuationData
	message        string
	verbose        bool
	artifactFilter []string
	startDate      string
	endDate        string
	// checkStaleness enables the stale-data warning check. It is only meaningful
	// for discovery-mode rendering (pagination walking backwards through time
	// looking for runs); the stdin path processes explicit run IDs with no
	// pagination, so it leaves this false.
	checkStaleness bool
	// countLimitReached indicates the continuation (if any) was produced because
	// the count/iteration cap was hit, as opposed to a timeout. It is used to
	// scope dateRangeCoverageWarning to the cause it actually describes, rather
	// than firing for timeout-driven continuations too.
	countLimitReached bool
	continuations     []WorkflowContinuation
	// suppressRender skips all report rendering after the summary file has been
	// written, for callers that only want the downloaded artifacts.
	suppressRender bool
	apiRateLimit   *GitHubAPIRateLimitReport
	apiRateLimits  []*GitHubAPIRateLimitReport
}

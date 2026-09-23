package cli

// audit.go: shared audit types and state used across the audit_*.go files
// (command wiring, run pipeline, analysis fan-out, summary building, and
// report rendering).

import (
	"github.com/github/gh-aw/pkg/logger"
)

var auditLog = logger.New("cli:audit")

// AuditOptions contains shared options for audit and audit-diff execution.
type AuditOptions struct {
	Owner            string
	Repo             string
	Hostname         string
	OutputDir        string
	Verbose          bool
	Parse            bool
	JSONOutput       bool
	JobID            int64
	StepNumber       int
	Format           string
	ArtifactSets     []string
	ExperimentFilter string
	VariantFilter    string
	RuntimeFilter    string
	EvalsOnly        bool
	Group            bool
	NoBaseline       bool
}

type auditRunConfig struct {
	runID            int64
	owner            string
	repo             string
	hostname         string
	outputDir        string
	verbose          bool
	parse            bool
	jsonOutput       bool
	jobID            int64
	stepNumber       int
	artifactFilter   []string
	experimentFilter string
	variantFilter    string
	runtimeFilter    string
	evalsOnly        bool
	group            bool
	noBaseline       bool
	// evalsArtifactRequested is true when evals were requested via --evals or
	// explicit --artifacts evals, and is used to trigger legacy dedicated-evals
	// fallback behavior for older runs.
	evalsArtifactRequested bool
}

// auditAnalysisResults is populated concurrently during audit collection.
// Each field is written by exactly one goroutine (or one launch* helper that
// exclusively owns a disjoint field set) before collectAuditAnalysisResults
// waits on the shared errgroup and reads the aggregate result.
//
// Keep launch* helper field ownership disjoint: sharing a field between
// goroutines without adding synchronization would introduce a data race.
type auditAnalysisResults struct {
	metrics                 LogMetrics
	failedJobCount          int
	jobDetails              []JobInfoWithDuration
	missingTools            []MissingToolReport
	missingData             []MissingDataReport
	noops                   []NoopReport
	mcpFailures             []MCPFailureReport
	skillActivations        []SkillActivation
	gatewaySteeringEvents   []GatewaySteeringEvent
	accessAnalysis          *DomainAnalysis
	firewallAnalysis        *FirewallAnalysis
	policyAnalysis          *PolicyAnalysis
	mcpToolUsage            *MCPToolUsageData
	tokenUsageSummary       *TokenUsageSummary
	workingSet              *WorkingSetMetrics
	redactedDomainsAnalysis *RedactedDomainsAnalysis
	rateLimitUsage          *GitHubRateLimitUsage
	artifacts               []string
	safeItemsCount          int
}

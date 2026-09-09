package cli

// audit_analysis_fanout.go: concurrent (errgroup-based) fan-out of the audit
// sub-analyses (metrics, job details, firewall, and supplemental analyses).

import (
	"context"
	"fmt"
	"os"

	"golang.org/x/sync/errgroup"

	"github.com/github/gh-aw/pkg/console"
)

func collectAuditAnalysisResults(ctx context.Context, run WorkflowRun, runOutputDir string, verbose bool, includeFirewallAnalyses bool) (auditAnalysisResults, error) {
	results := auditAnalysisResults{}
	g, gctx := errgroup.WithContext(ctx)
	launchCoreAuditAnalyses(g, gctx, &results, run, runOutputDir, verbose)
	if includeFirewallAnalyses {
		launchFirewallAuditAnalyses(g, gctx, &results, runOutputDir, verbose)
	}
	if err := g.Wait(); err != nil {
		return results, err
	}
	if ctx.Err() != nil {
		return results, ctx.Err()
	}
	g, gctx = errgroup.WithContext(ctx)
	launchSupplementalAuditAnalyses(g, gctx, &results, runOutputDir, verbose)
	if err := g.Wait(); err != nil {
		return results, err
	}
	if ctx.Err() != nil {
		return results, ctx.Err()
	}
	usageSummary, usageErr := loadUsageActivitySummary(runOutputDir)
	if usageErr != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to load usage activity summary for run %d: %v", run.DatabaseID, usageErr)))
	}
	if usageSummary != nil {
		results.workingSet = usageSummary.WorkingSet
	}
	return results, nil
}

// launchCoreAuditAnalyses exclusively writes missingTools, missingData, noops, mcpFailures, and accessAnalysis.
func launchCoreAuditAnalyses(g *errgroup.Group, gctx context.Context, results *auditAnalysisResults, run WorkflowRun, runOutputDir string, verbose bool) {
	// Resolve experiment assignment once so all goroutines reuse the same values
	// rather than each reading state.json independently.
	expName, expVariant, _ := firstExperimentAssignment(extractExperimentData(runOutputDir))

	launchMetricsAnalysis(g, gctx, results, runOutputDir, verbose, run.WorkflowPath)
	launchJobDetailsAnalysis(g, gctx, results, run.DatabaseID, runOutputDir, verbose)
	runAuditAnalysis(g, gctx, verbose, "extractMissingToolsFromRun", "Failed to extract missing tools", func(v []MissingToolReport) {
		results.missingTools = v
	}, func() ([]MissingToolReport, error) {
		return extractMissingToolsFromRun(runOutputDir, run, verbose, expName, expVariant)
	})
	runAuditAnalysis(g, gctx, verbose, "extractMissingDataFromRun", "Failed to extract missing data", func(v []MissingDataReport) {
		results.missingData = v
	}, func() ([]MissingDataReport, error) {
		return extractMissingDataFromRun(runOutputDir, run, verbose, expName, expVariant)
	})
	runAuditAnalysis(g, gctx, verbose, "extractNoopsFromRun", "Failed to extract noops", func(v []NoopReport) {
		results.noops = v
	}, func() ([]NoopReport, error) {
		return extractNoopsFromRun(runOutputDir, run, verbose, expName, expVariant)
	})
	runAuditAnalysis(g, gctx, verbose, "extractMCPFailuresFromRun", "Failed to extract MCP failures", func(v []MCPFailureReport) {
		results.mcpFailures = v
	}, func() ([]MCPFailureReport, error) {
		return extractMCPFailuresFromRun(runOutputDir, run, verbose, expName, expVariant)
	})
	runAuditAnalysis(g, gctx, verbose, "extractSkillActivationsFromRun", "Failed to extract skill activations", func(v []SkillActivation) {
		results.skillActivations = v
	}, func() ([]SkillActivation, error) {
		return extractSkillActivationsFromRun(runOutputDir, run, verbose, expName, expVariant)
	})
	runAuditAnalysis(g, gctx, verbose, "analyzeAccessLogs", "Failed to analyze access logs", func(v *DomainAnalysis) {
		results.accessAnalysis = v
	}, func() (*DomainAnalysis, error) {
		return analyzeAccessLogs(runOutputDir, verbose)
	})
}

// launchMetricsAnalysis exclusively writes results.metrics.
func launchMetricsAnalysis(g *errgroup.Group, gctx context.Context, results *auditAnalysisResults, runOutputDir string, verbose bool, workflowPath string) {
	g.Go(func() error {
		if err := gctx.Err(); err != nil {
			return err
		}
		metrics, err := extractLogMetrics(runOutputDir, verbose, workflowPath)
		if err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to extract metrics: %v", err)))
			}
			results.metrics = LogMetrics{}
			return nil
		}
		if err := gctx.Err(); err != nil {
			return err
		}
		results.metrics = metrics
		return nil
	})
}

// launchJobDetailsAnalysis exclusively writes results.jobDetails and results.failedJobCount.
func launchJobDetailsAnalysis(g *errgroup.Group, gctx context.Context, results *auditAnalysisResults, runID int64, runOutputDir string, verbose bool) {
	g.Go(func() error {
		if err := gctx.Err(); err != nil {
			return err
		}
		jobDetails, failedJobCount, err := fetchJobDetailsWithCounts(gctx, runID, runOutputDir, verbose)
		if err != nil {
			if gctx.Err() != nil {
				return gctx.Err()
			}
			auditLog.Printf("fetchJobDetailsWithCounts failed: %v", err)
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to fetch job details: %v", err)))
			}
			return nil
		}
		if err := gctx.Err(); err != nil {
			return err
		}
		results.jobDetails = jobDetails
		results.failedJobCount = failedJobCount
		return nil
	})
}

// launchFirewallAuditAnalyses exclusively writes policyAnalysis, mcpToolUsage, and tokenUsageSummary.
func launchFirewallAuditAnalyses(g *errgroup.Group, gctx context.Context, results *auditAnalysisResults, runOutputDir string, verbose bool) {
	launchFirewallAnalysis(g, gctx, results, runOutputDir, verbose)
	runAuditAnalysis(g, gctx, verbose, "analyzeFirewallPolicy", "Failed to analyze firewall policy", func(v *PolicyAnalysis) {
		results.policyAnalysis = v
	}, func() (*PolicyAnalysis, error) {
		return analyzeFirewallPolicy(runOutputDir, verbose)
	})
	runAuditAnalysis(g, gctx, verbose, "extractMCPToolUsageData", "Failed to extract MCP tool usage", func(v *MCPToolUsageData) {
		results.mcpToolUsage = v
	}, func() (*MCPToolUsageData, error) {
		return extractMCPToolUsageData(runOutputDir, verbose)
	})
	runAuditAnalysis(g, gctx, verbose, "analyzeTokenUsage", "Failed to analyze token usage", func(v *TokenUsageSummary) {
		results.tokenUsageSummary = v
	}, func() (*TokenUsageSummary, error) {
		return analyzeTokenUsage(runOutputDir, verbose)
	})
}

// launchFirewallAnalysis exclusively writes results.firewallAnalysis.
func launchFirewallAnalysis(g *errgroup.Group, gctx context.Context, results *auditAnalysisResults, runOutputDir string, verbose bool) {
	g.Go(func() error {
		if err := gctx.Err(); err != nil {
			return err
		}
		firewallAnalysis, err := analyzeFirewallLogs(runOutputDir, verbose)
		if err != nil {
			auditLog.Printf("analyzeFirewallLogs failed: %v", err)
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to analyze firewall logs: %v", err)))
			}
		}
		if agentLogFirewall := extractFirewallFromAgentLog(runOutputDir, verbose); agentLogFirewall != nil {
			if firewallAnalysis == nil {
				firewallAnalysis = agentLogFirewall
			} else {
				firewallAnalysis.AddMetrics(agentLogFirewall)
			}
		}
		if err := gctx.Err(); err != nil {
			return err
		}
		results.firewallAnalysis = firewallAnalysis
		return nil
	})
}

// launchSupplementalAuditAnalyses exclusively writes redactedDomainsAnalysis, rateLimitUsage, artifacts, and safeItemsCount.
func launchSupplementalAuditAnalyses(g *errgroup.Group, gctx context.Context, results *auditAnalysisResults, runOutputDir string, verbose bool) {
	runAuditAnalysis(g, gctx, verbose, "analyzeRedactedDomains", "Failed to analyze redacted domains", func(v *RedactedDomainsAnalysis) {
		results.redactedDomainsAnalysis = v
	}, func() (*RedactedDomainsAnalysis, error) {
		return analyzeRedactedDomains(runOutputDir, verbose)
	})
	runAuditAnalysis(g, gctx, verbose, "analyzeGitHubRateLimits", "Failed to analyze GitHub rate limit usage", func(v *GitHubRateLimitUsage) {
		results.rateLimitUsage = v
	}, func() (*GitHubRateLimitUsage, error) {
		return analyzeGitHubRateLimits(runOutputDir, verbose)
	})
	runAuditAnalysis(g, gctx, verbose, "listArtifacts", "Failed to list artifacts", func(v []string) {
		results.artifacts = v
	}, func() ([]string, error) {
		return listArtifacts(runOutputDir)
	})
	g.Go(func() error {
		if err := gctx.Err(); err != nil {
			return err
		}
		results.safeItemsCount = len(extractCreatedItemsFromManifest(runOutputDir))
		return nil
	})
}

// runAuditAnalysis logs and warns about non-context errors without aborting the audit.
// Callers should treat a zero-value result as unavailable rather than empty.
func runAuditAnalysis[T any](g *errgroup.Group, gctx context.Context, verbose bool, name, warning string, setter func(T), fn func() (T, error)) {
	g.Go(func() error {
		if err := gctx.Err(); err != nil {
			return err
		}
		value, err := fn()
		if err != nil {
			if gctx.Err() != nil {
				return gctx.Err()
			}
			auditLog.Printf("%s failed: %v", name, err)
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("%s: %v", warning, err)))
			}
			return nil
		}
		if err := gctx.Err(); err != nil {
			return err
		}
		setter(value)
		return nil
	})
}

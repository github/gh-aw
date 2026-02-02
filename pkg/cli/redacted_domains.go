package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var redactedDomainsLog = logger.New("cli:redacted_domains")

// RedactedDomainsAnalysis represents analysis of domains that were redacted during URL sanitization.
// The redacted-urls.log file is created during content sanitization when URLs from untrusted domains
// are encountered. This helps track which domains agents attempted to access but were blocked.
type RedactedDomainsAnalysis struct {
	// TotalDomains is the total number of unique domains found in the redacted log
	TotalDomains int `json:"total_domains" console:"header:Total Domains"`
	// Domains is a sorted list of unique domain names that were redacted
	Domains []string `json:"domains" console:"title:Redacted Domains,omitempty"`
}

// RedactedDomainsLogSummary contains aggregated redacted domains data across all runs
type RedactedDomainsLogSummary struct {
	TotalDomains int                                 `json:"total_domains" console:"header:Total Domains"`
	Domains      []string                            `json:"domains" console:"title:Redacted Domains,omitempty"`
	ByWorkflow   map[string]*RedactedDomainsAnalysis `json:"by_workflow,omitempty" console:"-"`
}

// parseRedactedDomainsLog parses the redacted-urls.log file and returns analysis.
// The file contains one domain per line.
func parseRedactedDomainsLog(logPath string, verbose bool) (*RedactedDomainsAnalysis, error) {
	redactedDomainsLog.Printf("Parsing redacted domains log: %s", logPath)

	file, err := os.Open(logPath)
	if err != nil {
		redactedDomainsLog.Printf("Failed to open redacted domains log: %v", err)
		return nil, fmt.Errorf("failed to open redacted domains log: %w", err)
	}
	defer file.Close()

	domainsSet := make(map[string]bool)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		domainsSet[line] = true
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading redacted domains log: %w", err)
	}

	// Convert set to sorted slice
	var domains []string
	for domain := range domainsSet {
		domains = append(domains, domain)
	}
	sort.Strings(domains)

	analysis := &RedactedDomainsAnalysis{
		TotalDomains: len(domains),
		Domains:      domains,
	}

	if redactedDomainsLog.Enabled() {
		redactedDomainsLog.Printf("Redacted domains log parsed: total=%d domains", len(domains))
	}

	return analysis, nil
}

// analyzeRedactedDomains analyzes redacted domains logs in a run directory.
// The redacted-urls.log file is typically stored in the agent_outputs artifact directory.
func analyzeRedactedDomains(runDir string, verbose bool) (*RedactedDomainsAnalysis, error) {
	redactedDomainsLog.Printf("Analyzing redacted domains in: %s", runDir)

	// The file could be in several locations depending on artifact extraction:
	// 1. Directly in the run directory as "redacted-urls.log"
	// 2. In an "agent_outputs" subdirectory as "redacted-urls.log"
	// 3. Following the original path structure: agent_outputs/tmp/gh-aw/redacted-urls.log

	// Check for redacted-urls.log directly in the run directory
	directPath := filepath.Join(runDir, "redacted-urls.log")
	if _, err := os.Stat(directPath); err == nil {
		redactedDomainsLog.Printf("Found redacted-urls.log at direct path: %s", directPath)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Found redacted-urls.log in run directory"))
		}
		return parseRedactedDomainsLog(directPath, verbose)
	}

	// Check for redacted-urls.log in agent_outputs directory
	agentOutputsPath := filepath.Join(runDir, "agent_outputs", "redacted-urls.log")
	if _, err := os.Stat(agentOutputsPath); err == nil {
		redactedDomainsLog.Printf("Found redacted-urls.log in agent_outputs: %s", agentOutputsPath)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Found redacted-urls.log in agent_outputs directory"))
		}
		return parseRedactedDomainsLog(agentOutputsPath, verbose)
	}

	// Check for the full path structure that mirrors the upload path
	// agent_outputs/tmp/gh-aw/redacted-urls.log
	fullPath := filepath.Join(runDir, "agent_outputs", "tmp", "gh-aw", "redacted-urls.log")
	if _, err := os.Stat(fullPath); err == nil {
		redactedDomainsLog.Printf("Found redacted-urls.log at full path: %s", fullPath)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Found redacted-urls.log at full artifact path"))
		}
		return parseRedactedDomainsLog(fullPath, verbose)
	}

	// Fallback: search recursively for redacted-urls.log
	var foundPath string
	_ = filepath.Walk(runDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if !info.IsDir() && info.Name() == "redacted-urls.log" {
			foundPath = path
			return fmt.Errorf("found") // Stop walking
		}
		return nil
	})

	if foundPath != "" {
		redactedDomainsLog.Printf("Found redacted-urls.log via recursive search: %s", foundPath)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found redacted-urls.log at %s", foundPath)))
		}
		return parseRedactedDomainsLog(foundPath, verbose)
	}

	// No redacted domains log found - this is not an error, just means no URLs were redacted
	redactedDomainsLog.Print("No redacted-urls.log found")
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("No redacted-urls.log found in %s", runDir)))
	}
	return nil, nil
}

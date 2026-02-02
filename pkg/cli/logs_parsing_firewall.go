// This file provides command-line interface functionality for gh-aw.
// This file (logs_parsing_firewall.go) contains functionality for parsing
// and analyzing firewall logs from workflow runs.
//
// Key responsibilities:
//   - Locating firewall logs in various directory structures
//   - Running JavaScript firewall log parser
//   - Generating markdown summaries of firewall activity

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/cli/fileutil"
	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var logsParsingFirewallLog = logger.New("cli:logs_parsing_firewall")

// parseFirewallLogs runs the JavaScript firewall log parser and writes markdown to firewall.md
func parseFirewallLogs(runDir string, verbose bool) error {
	logsParsingFirewallLog.Printf("Parsing firewall logs in: %s", runDir)
	// Get the firewall log parser script
	jsScript := workflow.GetLogParserScript("parse_firewall_logs")
	if jsScript == "" {
		logsParsingFirewallLog.Print("Failed to get firewall log parser script")
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Failed to get firewall log parser script"))
		}
		return nil
	}

	// Check if squid logs directory exists in the run directory
	// The logs could be in workflow-logs subdirectory or directly in the run directory
	squidLogsDir := filepath.Join(runDir, "squid-logs")

	// Also check for squid logs in workflow-logs directory
	workflowLogsSquidDir := filepath.Join(runDir, "workflow-logs", "squid-logs")

	// Determine which directory to use
	var logsDir string
	if fileutil.DirExists(squidLogsDir) {
		logsDir = squidLogsDir
		logsParsingFirewallLog.Printf("Found firewall logs in squid-logs directory")
	} else if fileutil.DirExists(workflowLogsSquidDir) {
		logsDir = workflowLogsSquidDir
		logsParsingFirewallLog.Printf("Found firewall logs in workflow-logs/squid-logs directory")
	} else {
		logsParsingFirewallLog.Print("No firewall logs found, skipping parsing")
		// No firewall logs found - this is not an error, just skip parsing
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("No firewall logs found in %s, skipping firewall log parsing", filepath.Base(runDir))))
		}
		return nil
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found firewall logs in %s", logsDir)))
	}

	// Create a temporary directory for running the parser
	tempDir, err := os.MkdirTemp("", "firewall_log_parser")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a Node.js script that mimics the GitHub Actions environment
	// The firewall parser expects logs in /tmp/gh-aw/squid-logs-{workflow}/
	// We'll set GITHUB_WORKFLOW to a value that makes the parser look in our temp directory
	nodeScript := fmt.Sprintf(`
const fs = require('fs');
const path = require('path');

// Mock @actions/core for the parser
const core = {
	summary: {
		addRaw: function(content) {
			this._content = content;
			return this;
		},
		write: function() {
			console.log(this._content);
		},
		_content: ''
	},
	setFailed: function(message) {
		console.error('FAILED:', message);
		process.exit(1);
	},
	info: function(message) {
		// Silent in CLI mode
	}
};

// Set up environment
// We'll use a custom workflow name that points to our temp directory
process.env.GITHUB_WORKFLOW = 'temp-workflow';

// Override require to provide our mock
const originalRequire = require;
require = function(name) {
	if (name === '@actions/core') {
		return core;
	}
	return originalRequire.apply(this, arguments);
};

// Monkey-patch the main function to use our logs directory
const originalMain = function() {
  const fs = require("fs");
  const path = require("path");

  try {
    // Use our custom logs directory instead of /tmp/gh-aw/squid-logs-*
    const squidLogsDir = '%s';

    if (!fs.existsSync(squidLogsDir)) {
      core.info('No firewall logs directory found at: ' + squidLogsDir);
      return;
    }

    // Find all .log files
    const files = fs.readdirSync(squidLogsDir).filter(file => file.endsWith(".log"));

    if (files.length === 0) {
      core.info('No firewall log files found in: ' + squidLogsDir);
      return;
    }

    core.info('Found ' + files.length + ' firewall log file(s)');

    // Parse all log files and aggregate results
    let totalRequests = 0;
    let allowedRequests = 0;
    let blockedRequests = 0;
    const allowedDomains = new Set();
    const blockedDomains = new Set();
    const requestsByDomain = new Map();

    for (const file of files) {
      const filePath = path.join(squidLogsDir, file);
      core.info('Parsing firewall log: ' + file);

      const content = fs.readFileSync(filePath, "utf8");
      const lines = content.split("\n").filter(line => line.trim());

      for (const line of lines) {
        const entry = parseFirewallLogLine(line);
        if (!entry) {
          continue;
        }

        totalRequests++;

        // Determine if request was allowed or blocked
        const isAllowed = isRequestAllowed(entry.decision, entry.status);

        if (isAllowed) {
          allowedRequests++;
          allowedDomains.add(entry.domain);
        } else {
          blockedRequests++;
          blockedDomains.add(entry.domain);
        }

        // Track request count per domain
        if (!requestsByDomain.has(entry.domain)) {
          requestsByDomain.set(entry.domain, { allowed: 0, blocked: 0 });
        }
        const domainStats = requestsByDomain.get(entry.domain);
        if (isAllowed) {
          domainStats.allowed++;
        } else {
          domainStats.blocked++;
        }
      }
    }

    // Generate step summary
    const summary = generateFirewallSummary({
      totalRequests,
      allowedRequests,
      blockedRequests,
      allowedDomains: Array.from(allowedDomains).sort(),
      blockedDomains: Array.from(blockedDomains).sort(),
      requestsByDomain,
    });

    core.summary.addRaw(summary).write();
    core.info("Firewall log summary generated successfully");
  } catch (error) {
    core.setFailed(error instanceof Error ? error : String(error));
  }
};

// Execute the parser script to get helper functions
%s

// Replace main() call with our custom version
originalMain();
`, logsDir, jsScript)

	// Write the Node.js script
	nodeFile := filepath.Join(tempDir, "parser.js")
	if err := os.WriteFile(nodeFile, []byte(nodeScript), 0644); err != nil {
		return fmt.Errorf("failed to write node script: %w", err)
	}

	// Execute the Node.js script
	cmd := exec.Command("node", "parser.js")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to execute firewall parser script: %w\nOutput: %s", err, string(output))
	}

	// Write the output to firewall.md in the run directory
	firewallMdPath := filepath.Join(runDir, "firewall.md")
	if err := os.WriteFile(firewallMdPath, []byte(strings.TrimSpace(string(output))), 0644); err != nil {
		return fmt.Errorf("failed to write firewall.md: %w", err)
	}

	return nil
}

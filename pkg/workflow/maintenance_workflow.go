package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var maintenanceLog = logger.New("workflow:maintenance_workflow")

// generateMaintenanceCron generates a cron schedule based on the minimum expires value in days
// Schedule runs at minimum required frequency to check expirations at appropriate intervals
// Returns cron expression and description.
func generateMaintenanceCron(minExpiresDays int) (string, string) {
	// Use a pseudo-random but deterministic minute (37) to avoid load spikes at :00
	minute := 37

	// Determine frequency based on minimum expires value (in days)
	// Run at least as often as the shortest expiration would need
	if minExpiresDays <= 1 {
		// For 1 day or less, run every 2 hours
		return fmt.Sprintf("%d */2 * * *", minute), "Every 2 hours"
	} else if minExpiresDays == 2 {
		// For 2 days, run every 6 hours
		return fmt.Sprintf("%d */6 * * *", minute), "Every 6 hours"
	} else if minExpiresDays <= 4 {
		// For 3-4 days, run every 12 hours
		return fmt.Sprintf("%d */12 * * *", minute), "Every 12 hours"
	}

	// For more than 4 days, run daily
	return fmt.Sprintf("%d %d * * *", minute, 0), "Daily"
}

// GenerateMaintenanceWorkflow generates the agentics-maintenance.yml workflow
// if any workflows use the expires field for discussions or issues
func GenerateMaintenanceWorkflow(workflowDataList []*WorkflowData, workflowDir string, version string, actionMode ActionMode, verbose bool) error {
	maintenanceLog.Print("Checking if maintenance workflow is needed")

	// Check if any workflow uses expires field for discussions, issues, or pull requests
	// and track the minimum expires value to determine schedule frequency
	hasExpires := false
	minExpires := 0 // Track minimum expires value in hours

	for _, workflowData := range workflowDataList {
		if workflowData.SafeOutputs != nil {
			// Check for expired discussions
			if workflowData.SafeOutputs.CreateDiscussions != nil {
				if workflowData.SafeOutputs.CreateDiscussions.Expires > 0 {
					hasExpires = true
					expires := workflowData.SafeOutputs.CreateDiscussions.Expires
					maintenanceLog.Printf("Workflow %s has expires field set to %d hours for discussions", workflowData.Name, expires)
					if minExpires == 0 || expires < minExpires {
						minExpires = expires
					}
				}
			}
			// Check for expired issues
			if workflowData.SafeOutputs.CreateIssues != nil {
				if workflowData.SafeOutputs.CreateIssues.Expires > 0 {
					hasExpires = true
					expires := workflowData.SafeOutputs.CreateIssues.Expires
					maintenanceLog.Printf("Workflow %s has expires field set to %d hours for issues", workflowData.Name, expires)
					if minExpires == 0 || expires < minExpires {
						minExpires = expires
					}
				}
			}
			// Check for expired pull requests
			if workflowData.SafeOutputs.CreatePullRequests != nil {
				if workflowData.SafeOutputs.CreatePullRequests.Expires > 0 {
					hasExpires = true
					expires := workflowData.SafeOutputs.CreatePullRequests.Expires
					maintenanceLog.Printf("Workflow %s has expires field set to %d hours for pull requests", workflowData.Name, expires)
					if minExpires == 0 || expires < minExpires {
						minExpires = expires
					}
				}
			}
		}
	}

	if !hasExpires {
		maintenanceLog.Print("No workflows use expires field, skipping maintenance workflow generation")

		// Delete existing maintenance workflow file if it exists (no expires means no need for maintenance)
		maintenanceFile := filepath.Join(workflowDir, "agentics-maintenance.yml")
		if _, err := os.Stat(maintenanceFile); err == nil {
			maintenanceLog.Printf("Deleting existing maintenance workflow: %s", maintenanceFile)
			if err := os.Remove(maintenanceFile); err != nil {
				return fmt.Errorf("failed to delete maintenance workflow: %w", err)
			}
			maintenanceLog.Print("Maintenance workflow deleted successfully")
		}

		return nil
	}

	maintenanceLog.Printf("Generating maintenance workflow for expired discussions, issues, and pull requests (minimum expires: %d hours)", minExpires)

	// Convert hours to days for cron schedule generation
	minExpiresDays := minExpires / 24
	if minExpires%24 > 0 {
		minExpiresDays++ // Round up partial days
	}

	// Generate cron schedule based on minimum expires value
	cronSchedule, scheduleDesc := generateMaintenanceCron(minExpiresDays)
	maintenanceLog.Printf("Maintenance schedule: %s (%s)", cronSchedule, scheduleDesc)

	// Create the maintenance workflow content using strings.Builder
	var yaml strings.Builder

	// Add workflow header with logo and instructions
	customInstructions := `Alternative regeneration methods:
  make recompile

Or use the gh-aw CLI directly:
  ./gh-aw compile --validate --verbose

The workflow is generated when any workflow uses the 'expires' field
in create-discussions, create-issues, or create-pull-request safe-outputs configuration.
Schedule frequency is automatically determined by the shortest expiration time.`

	header := GenerateWorkflowHeader("", "pkg/workflow/maintenance_workflow.go", customInstructions)
	yaml.WriteString(header)

	yaml.WriteString(`name: Agentic Maintenance

on:
  schedule:
    - cron: "` + cronSchedule + `"  # ` + scheduleDesc + ` (based on minimum expires: ` + fmt.Sprintf("%d", minExpiresDays) + ` days)
  workflow_dispatch:

permissions: {}

jobs:
  close-expired-entities:
    runs-on: ubuntu-slim
    permissions:
      discussions: write
      issues: write
      pull-requests: write
    steps:
`)

	// Get the setup action reference (local or remote based on mode)
	// Pass nil for data since maintenance workflow doesn't have WorkflowData
	// In release mode without data, it will return tag-based reference
	setupActionRef := ResolveSetupActionReference(actionMode, version, "", nil)

	// Add checkout step only in dev mode (for local action paths)
	if actionMode == ActionModeDev {
		yaml.WriteString(`      - name: Checkout actions folder
        uses: ` + GetActionPin("actions/checkout") + `
        with:
          sparse-checkout: |
            actions
          persist-credentials: false

`)
	}

	// Add setup step with the resolved action reference
	yaml.WriteString(`      - name: Setup Scripts
        uses: ` + setupActionRef + `
        with:
          destination: /opt/gh-aw/actions

      - name: Close expired discussions
        uses: ` + GetActionPin("actions/github-script") + `
        with:
          script: |
`)

	// Add the close expired discussions script using require()
	yaml.WriteString(`            const { setupGlobals } = require('/opt/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io);
            const { main } = require('/opt/gh-aw/actions/close_expired_discussions.cjs');
            await main();

      - name: Close expired issues
        uses: ` + GetActionPin("actions/github-script") + `
        with:
          script: |
`)

	// Add the close expired issues script using require()
	yaml.WriteString(`            const { setupGlobals } = require('/opt/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io);
            const { main } = require('/opt/gh-aw/actions/close_expired_issues.cjs');
            await main();

      - name: Close expired pull requests
        uses: ` + GetActionPin("actions/github-script") + `
        with:
          script: |
`)

	// Add the close expired pull requests script using require()
	yaml.WriteString(`            const { setupGlobals } = require('/opt/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io);
            const { main } = require('/opt/gh-aw/actions/close_expired_pull_requests.cjs');
            await main();
`)

	// Add compile-workflows and zizmor-scan jobs only in dev mode
	// These jobs are specific to the gh-aw repository and require go.mod, make build, etc.
	// User repositories won't have these dependencies, so we skip them in release mode
	if actionMode == ActionModeDev {
		// Add compile-workflows job
		yaml.WriteString(`
  compile-workflows:
    runs-on: ubuntu-slim
    permissions:
      contents: read
      issues: write
    steps:
`)

		// Dev mode: checkout entire repository (no sparse checkout, but no credentials)
		yaml.WriteString(`      - name: Checkout repository
        uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683 # v4.2.2
        with:
          persist-credentials: false

`)

		yaml.WriteString(`
      - name: Setup Go
        uses: actions/setup-go@41dfa10bad2bb2ae585af6ee5bb4d7d973ad74ed # v5.1.0
        with:
          go-version-file: go.mod
          cache: true

      - name: Build gh-aw
        run: make build

      - name: Compile workflows
        run: |
          ./gh-aw compile --validate --verbose
          echo "✓ All workflows compiled successfully"

      - name: Setup Scripts
        uses: ` + setupActionRef + `
        with:
          destination: /opt/gh-aw/actions

      - name: Check for out-of-sync workflows and create issue if needed
        uses: ` + GetActionPin("actions/github-script") + `
        with:
          script: |
            const { setupGlobals } = require('/opt/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io);
            const { main } = require('/opt/gh-aw/actions/check_workflow_recompile_needed.cjs');
            await main();

  zizmor-scan:
    runs-on: ubuntu-slim
    needs: compile-workflows
    permissions:
      contents: read
    steps:
      - name: Checkout repository
        uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683 # v4.2.2

      - name: Setup Go
        uses: actions/setup-go@41dfa10bad2bb2ae585af6ee5bb4d7d973ad74ed # v5.1.0
        with:
          go-version-file: go.mod
          cache: true

      - name: Build gh-aw
        run: make build

      - name: Run zizmor security scanner
        run: |
          ./gh-aw compile --zizmor --verbose
          echo "✓ Zizmor security scan completed"

  secret-validation:
    runs-on: ubuntu-slim
    permissions:
      contents: read
    steps:
`)

		// Add checkout step only in dev mode (for local action paths)
		if actionMode == ActionModeDev {
			yaml.WriteString(`      - name: Checkout actions folder
        uses: ` + GetActionPin("actions/checkout") + `
        with:
          sparse-checkout: |
            actions
          persist-credentials: false

`)
		}

		yaml.WriteString(`      - name: Setup Node.js
        uses: actions/setup-node@39370e3970a6d050c480ffad4ff0ed4d3fdee5af # v4.1.0
        with:
          node-version: '22'

      - name: Setup Scripts
        uses: ` + setupActionRef + `
        with:
          destination: /opt/gh-aw/actions

      - name: Validate Secrets
        uses: ` + GetActionPin("actions/github-script") + `
        env:
          # GitHub tokens
          GH_AW_GITHUB_TOKEN: ${{ secrets.GH_AW_GITHUB_TOKEN }}
          GH_AW_GITHUB_MCP_SERVER_TOKEN: ${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN }}
          GH_AW_PROJECT_GITHUB_TOKEN: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}
          GH_AW_COPILOT_TOKEN: ${{ secrets.GH_AW_COPILOT_TOKEN }}
          # AI Engine API keys
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
          OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
          BRAVE_API_KEY: ${{ secrets.BRAVE_API_KEY }}
          # Integration tokens
          NOTION_API_TOKEN: ${{ secrets.NOTION_API_TOKEN }}
        with:
          script: |
            const { setupGlobals } = require('/opt/gh-aw/actions/setup_globals.cjs');
            setupGlobals(core, github, context, exec, io);
            const { main } = require('/opt/gh-aw/actions/validate_secrets.cjs');
            await main();

      - name: Upload secret validation report
        if: always()
        uses: ` + GetActionPin("actions/upload-artifact") + `
        with:
          name: secret-validation-report
          path: secret-validation-report.md
          retention-days: 30
          if-no-files-found: warn
`)
	}

	content := yaml.String()

	// Write the maintenance workflow file
	maintenanceFile := filepath.Join(workflowDir, "agentics-maintenance.yml")
	maintenanceLog.Printf("Writing maintenance workflow to %s", maintenanceFile)

	if err := os.WriteFile(maintenanceFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write maintenance workflow: %w", err)
	}

	maintenanceLog.Print("Maintenance workflow generated successfully")
	return nil
}

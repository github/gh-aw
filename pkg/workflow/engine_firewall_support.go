package workflow

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var engineFirewallSupportLog = logger.New("workflow:engine_firewall_support")

// hasNetworkRestrictions checks if the workflow has network restrictions defined
// Network restrictions exist if:
// - network.allowed has domains specified (non-empty list) AND it's not just "defaults"
// - network.blocked has domains specified (non-empty list)
func hasNetworkRestrictions(networkPermissions *NetworkPermissions) bool {
	if networkPermissions == nil {
		return false
	}

	// Blocked domains always restrict access, including when allowed is defaults.
	if len(networkPermissions.Blocked) > 0 {
		return true
	}

	// If allowed domains are specified and it's not just the defaults ecosystem, we have restrictions
	if len(networkPermissions.Allowed) > 0 {
		// Check if it's ONLY "defaults" (which means use default ecosystem, not a restriction)
		if len(networkPermissions.Allowed) == 1 && slices.Contains(networkPermissions.Allowed, "defaults") {
			return false
		}
		return true
	}

	// Empty allowed list [] means deny-all, which is a restriction
	if networkPermissions.ExplicitlyDefined && len(networkPermissions.Allowed) == 0 {
		return true
	}

	return false
}

// checkNetworkSupport validates that the selected engine supports network restrictions
// when network restrictions are defined in the workflow
func (c *Compiler) checkNetworkSupport(engine CodingAgentEngine, networkPermissions *NetworkPermissions) error {
	engineFirewallSupportLog.Printf("Checking network support: engine=%s, strict_mode=%t", engine.GetID(), c.strictMode)

	// First, check for explicit firewall disable
	if err := c.checkFirewallDisable(networkPermissions); err != nil {
		return err
	}

	// Check if network restrictions exist
	if !hasNetworkRestrictions(networkPermissions) {
		engineFirewallSupportLog.Print("No network restrictions defined, skipping validation")
		// No restrictions, no validation needed
		return nil
	}

	if engine.GetID() == string(constants.CopilotEngine) {
		if err := c.reportUnfirewalledComponent(
			"engine",
			engine.GetID(),
			"engine 'copilot' is not bound by firewall policies and can access network resources outside the configured restrictions",
			"Use a firewall-bound engine when network policy enforcement is required. Example:\n\nengine: claude",
		); err != nil {
			return err
		}
	}

	engineFirewallSupportLog.Printf("Engine supports firewall: %s", engine.GetID())
	return nil
}

// checkToolsNetworkSupport validates that enabled tools are bound by firewall policies.
func (c *Compiler) checkToolsNetworkSupport(tools map[string]any, networkPermissions *NetworkPermissions) error {
	if !hasNetworkRestrictions(networkPermissions) {
		return nil
	}

	collector := NewErrorCollector(c.failFast)
	for _, tool := range []string{"web-fetch", "web-search"} {
		value, exists := tools[tool]
		if !exists {
			continue
		}
		if enabled, ok := value.(bool); ok && !enabled {
			continue
		}

		if err := c.reportUnfirewalledComponent(
			"tools."+tool,
			tool,
			fmt.Sprintf("tool '%s' is not bound by firewall policies and can access network resources outside the configured restrictions", tool),
			fmt.Sprintf("Remove tools.%s when network policy enforcement is required. Example:\n\ntools:\n  %s: false", tool, tool),
		); err != nil {
			if returnErr := collector.Add(err); returnErr != nil {
				return returnErr
			}
		}
	}
	return collector.FormattedError("firewall policy")
}

func (c *Compiler) reportUnfirewalledComponent(field, value, reason, suggestion string) error {
	if c.strictMode {
		return NewValidationError(field, value, "strict mode: "+reason, suggestion)
	}

	message := reason + "."
	if suggestion != "" {
		message += " " + suggestion
	}
	fmt.Fprintln(os.Stderr, console.FormatWarningMessage(message))
	c.IncrementWarningCount()
	return nil
}

// checkFirewallDisable validates firewall: "disable" configuration
// - Warning if allowed != * (unrestricted)
// - Error in strict mode if allowed is not *
func (c *Compiler) checkFirewallDisable(networkPermissions *NetworkPermissions) error {
	if networkPermissions == nil || networkPermissions.Firewall == nil {
		return nil
	}

	// Check if firewall is explicitly disabled
	if !networkPermissions.Firewall.Enabled {
		// Check if network has restrictions (allowed list specified with domains)
		hasRestrictions := len(networkPermissions.Allowed) > 0

		if hasRestrictions {
			message := "Firewall is disabled but network restrictions are specified (network.allowed). Network may not be properly sandboxed."

			if c.strictMode {
				// In strict mode, this is an error
				return errors.New("strict mode: cannot disable firewall when network restrictions (network.allowed) are set")
			}

			// In non-strict mode, emit a warning
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(message))
			c.IncrementWarningCount()
		}
	}

	return nil
}

// generateSquidLogsUploadStep creates a GitHub Actions step to upload Squid logs as artifact.
func generateSquidLogsUploadStep(workflowName string, workflowData *WorkflowData) GitHubActionStep {
	sanitizedName := strings.ToLower(SanitizeWorkflowName(workflowName))
	artifactName := "firewall-logs-" + sanitizedName
	// Firewall logs location: /tmp/gh-aw on standard runners, ${{ runner.temp }}/gh-aw on ARC/DinD.
	// Use ${{ runner.temp }} (Actions expression) because `with:` blocks don't expand shell vars.
	firewallLogsDir := constants.AWFProxyLogsDir.String() + "/"
	if isArcDindTopology(workflowData) {
		firewallLogsDir = constants.AWFProxyLogsDirExpr + "/"
	}

	stepLines := []string{
		"      - name: Upload Firewall Logs",
		"        if: always()",
		"        continue-on-error: true",
		"        uses: " + getActionPinForData("actions/upload-artifact", workflowData),
		"        with:",
		"          name: " + artifactName,
		"          path: " + firewallLogsDir,
		"          if-no-files-found: ignore",
	}

	return GitHubActionStep(stepLines)
}

// generateFirewallLogParsingStep creates a GitHub Actions step to parse firewall logs and create step summary.
func generateFirewallLogParsingStep(workflowName string, workflowData *WorkflowData) GitHubActionStep {
	// Firewall logs are at a known location in the sandbox folder structure.
	// On ARC/DinD, /tmp/gh-aw is not daemon-visible so logs land under runner.temp/gh-aw.
	// For env: blocks, use ${{ runner.temp }} (Actions expression) since shell vars aren't expanded there.
	firewallLogsDirEnv := constants.AWFProxyLogsDir.String()
	if isArcDindTopology(workflowData) {
		firewallLogsDirEnv = constants.AWFProxyLogsDirExpr
	}

	// When the runtime profile runs AWF rootless, pass --rootless so the script uses
	// non-interactive sudo (sudo -n) with a non-sudo chmod fallback. Profiles where AWF
	// ran with host privileges (docker-sudo-iptables, cloud-hypervisor) use plain sudo.
	scriptArg := ""
	if isAWFNetworkIsolationEnabled(workflowData) && getSandboxRuntimeProfile(workflowData).Rootless {
		scriptArg = " --rootless"
	}

	stepLines := []string{
		"      - name: Print firewall logs",
		"        if: always()",
		"        continue-on-error: true",
		"        env:",
		"          AWF_LOGS_DIR: " + firewallLogsDirEnv,
		`        run: bash "${RUNNER_TEMP}/gh-aw/actions/print_firewall_logs.sh"` + scriptArg,
	}

	return GitHubActionStep(stepLines)
}

// defaultGetSquidLogsSteps returns the steps for uploading and parsing Squid logs after
// secret redaction. It is shared across engines (Claude, Codex, Copilot) whose
// GetSquidLogsSteps implementations are otherwise identical save for the logger used.
func defaultGetSquidLogsSteps(workflowData *WorkflowData, debugLog *logger.Logger) []GitHubActionStep {
	var steps []GitHubActionStep

	// Only add upload and parsing steps if firewall is enabled
	if isFirewallEnabled(workflowData) {
		debugLog.Printf("Adding Squid logs upload and parsing steps for workflow: %s", workflowData.Name)

		squidLogsUpload := generateSquidLogsUploadStep(workflowData.Name, workflowData)
		steps = append(steps, squidLogsUpload)

		// Add firewall log parsing step to create step summary
		firewallLogParsing := generateFirewallLogParsingStep(workflowData.Name, workflowData)
		steps = append(steps, firewallLogParsing)
	} else {
		debugLog.Print("Firewall disabled, skipping Squid logs upload")
	}

	return steps
}

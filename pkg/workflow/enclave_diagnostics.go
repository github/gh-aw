package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

const (
	enclaveDiagnosticsRelease      = "v0.29.5"
	enclaveDiagnosticsRoot         = "/var/lib/awf-enclave-smoke-export"
	enclaveDiagnosticsUploadAction = "actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9"
	enclaveDiagnosticsRunKey       = "${{ github.run_id }}-${{ github.run_attempt }}"
	enclaveDiagnosticsInstallerSHA = "adb59bd042bfe11a432b2c6577f3919988ff30dcc04ab9bc2f6264422eb4b021"
)

func enclaveDiagnosticsEnabled(data *WorkflowData) bool {
	agent := getAgentConfig(data)
	return agent != nil && agent.EnclaveDebugPublic != nil && *agent.EnclaveDebugPublic
}

func validateEnclaveDiagnostics(data *WorkflowData) error {
	if !enclaveDiagnosticsEnabled(data) {
		return nil
	}
	reject := func(reason string) error {
		return fmt.Errorf("sandbox.agent.enclave-debug-public: %s; use static public-only default Docker enclaves with AWF %s", reason, enclaveDiagnosticsRelease)
	}
	agent := getAgentConfig(data)
	if !enclavesEnabled(data) || len(data.Enclaves) != 1 || !isAWFNetworkIsolationEnabled(data) ||
		(agent.Runtime != "" && agent.Runtime != AgentRuntimeDocker) ||
		isArcDindTopology(data) || agent.Command != "" || len(agent.Args) > 0 || len(agent.Mounts) > 0 {
		return reject("unsupported runtime or custom execution scope")
	}
	// The maintained collector and its semantics are release-pinned together.
	// New releases need reviewed collector/runtime pins, not a mutable tag fetch.
	version := agent.Version
	if version == "" {
		version = string(constants.DefaultFirewallVersion)
	}
	if !awfVersionAtLeast(getFirewallConfig(data), constants.AWFDockerEnclaveSetupMinVersion) || version != enclaveDiagnosticsRelease {
		return reject("the diagnostic collector is supported only for the reviewed AWF release")
	}
	// Closed manifests need enclave executor roles, which AWF's public-debug
	// policy rejects. Model routing also synthesizes a closed manifest.
	if len(agent.Images) > 0 || isModelRoutingEnabled(data) {
		return reject("explicit or model-routing image manifests are ineligible")
	}
	for _, enclave := range data.Enclaves {
		if enclave == nil || enclave.Dynamic != nil || enclave.Image != "" ||
			(enclave.Runtime != "" && enclave.Runtime != "docker") || len(enclave.Repos) == 0 {
			return reject("dynamic, mixed, custom-image or empty repository scope")
		}
		if enclave.MaxInvocations != 1 {
			return reject("public diagnostic fixtures require max-invocations: 1")
		}
		for _, repo := range enclave.Repos {
			if repo == nil || repo.Sensitivity != "public" || !enclaveDynamicSelectorPattern.MatchString(repo.Repo) {
				return reject("every repository must be a canonical static public repository")
			}
		}
	}
	return nil
}

func generateEnclaveDiagnosticsPrepare(yaml *strings.Builder, data *WorkflowData) error {
	if err := validateEnclaveDiagnostics(data); err != nil {
		return err
	}
	if !enclaveDiagnosticsEnabled(data) {
		return nil
	}
	yaml.WriteString("      - name: Prepare public Docker enclave diagnostics\n")
	yaml.WriteString("        id: prepare_enclave_diagnostics\n")
	yaml.WriteString("        env:\n")
	writeFormattedYAML(yaml, "          DEBUG_RUN_KEY: %s\n", enclaveDiagnosticsRunKey)
	yaml.WriteString("        run: |\n")
	yaml.WriteString("          set -euo pipefail\n")
	yaml.WriteString("          staging_dir=\"${RUNNER_TEMP}/awf-collector-install-${DEBUG_RUN_KEY}\"\n")
	yaml.WriteString("          installer_dir=\"\"\n")
	yaml.WriteString("          cleanup() {\n")
	yaml.WriteString("            rm -rf -- \"$staging_dir\"\n")
	yaml.WriteString("            if [[ -n \"$installer_dir\" ]]; then /usr/bin/sudo -n /usr/bin/rm -rf -- \"$installer_dir\"; fi\n")
	yaml.WriteString("          }\n")
	yaml.WriteString("          trap cleanup EXIT\n")
	yaml.WriteString("          DIAGNOSTICS_STAGING=\"$staging_dir\" NODE_OPTIONS= NODE_PATH= node \"${RUNNER_TEMP}/gh-aw/actions/install_enclave_diagnostics.cjs\"\n")
	yaml.WriteString("          installer_dir=$(/usr/bin/sudo -n /usr/bin/mktemp -d /var/lib/awf-enclave-diagnostics-install.XXXXXX)\n")
	yaml.WriteString("          [[ \"$installer_dir\" == /var/lib/awf-enclave-diagnostics-install.* ]]\n")
	yaml.WriteString("          installer_path=\"$installer_dir/install_enclave_diagnostics_trusted.cjs\"\n")
	yaml.WriteString("          /usr/bin/sudo -n /usr/bin/install -o root -g root -m 0500 -- \"${RUNNER_TEMP}/gh-aw/actions/install_enclave_diagnostics_trusted.cjs\" \"$installer_path\"\n")
	yaml.WriteString("          installer_sha256=$(/usr/bin/sudo -n /usr/bin/sha256sum \"$installer_path\")\n")
	writeFormattedYAML(yaml, "          [[ \"${installer_sha256%%%% *}\" == \"%s\" ]]\n", enclaveDiagnosticsInstallerSHA)
	for _, line := range trustedEnclaveInstallerNodeLines("$staging_dir", false) {
		yaml.WriteString(line + "\n")
	}
	yaml.WriteString("          /usr/bin/env -i PATH=/usr/bin:/bin LANG=C LC_ALL=C /usr/bin/sudo -n -- \"$installer_dir/node\" --no-addons --no-global-search-paths \"$installer_path\" \"$installer_dir/node\" \"$staging_dir/collector.cjs\"\n")
	writeFormattedYAML(yaml, "          /usr/bin/env -i PATH=/usr/bin:/bin LANG=C LC_ALL=C /usr/bin/sudo -n -- %s/node %s/collector.cjs prepare docker \"$DEBUG_RUN_KEY\" \"$(id -u)\" \"$(id -g)\"\n", enclaveDiagnosticsRoot, enclaveDiagnosticsRoot)
	return nil
}

func generateEnclaveDiagnosticsCollect(yaml *strings.Builder, data *WorkflowData) {
	if !enclaveDiagnosticsEnabled(data) {
		return
	}
	yaml.WriteString("      - name: Collect public Docker enclave diagnostics\n")
	yaml.WriteString("        id: collect_enclave_diagnostics\n")
	yaml.WriteString("        if: always() && steps.prepare_enclave_diagnostics.outcome == 'success' && steps.agentic_execution.outcome != 'skipped'\n")
	yaml.WriteString("        env:\n")
	writeFormattedYAML(yaml, "          DEBUG_RUN_KEY: %s\n", enclaveDiagnosticsRunKey)
	yaml.WriteString("          EXECUTION_OUTCOME: ${{ steps.agentic_execution.outcome }}\n")
	yaml.WriteString("        run: |\n")
	yaml.WriteString("          set -euo pipefail\n")
	yaml.WriteString("          printf 'AWF execution outcome: %s\\n' \"$EXECUTION_OUTCOME\"\n")
	writeFormattedYAML(yaml, "          /usr/bin/env -i PATH=/usr/bin:/bin LANG=C LC_ALL=C /usr/bin/sudo -n -- %s/node %s/collector.cjs collect docker \"$DEBUG_RUN_KEY\" \"$(id -u)\" \"$(id -g)\"\n", enclaveDiagnosticsRoot, enclaveDiagnosticsRoot)
	yaml.WriteString(enclaveDiagnosticsUploadStep())
}

func enclaveDiagnosticsUploadStep() string {
	return "      - name: Upload validated public enclave diagnostics or safe collection status\n" +
		"        id: upload_enclave_diagnostics\n" +
		"        if: always() && steps.prepare_enclave_diagnostics.outcome == 'success' && steps.collect_enclave_diagnostics.outcome != 'skipped'\n" +
		"        uses: " + enclaveDiagnosticsUploadAction + "\n" +
		"        with:\n" +
		"          name: public-enclave-debug-" + enclaveDiagnosticsRunKey + "\n" +
		"          path: " + enclaveDiagnosticsRoot + "/" + enclaveDiagnosticsRunKey + "/upload/\n" +
		"          if-no-files-found: warn\n" +
		"          retention-days: 1\n"
}

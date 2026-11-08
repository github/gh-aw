package workflow

import (
	"errors"
	"fmt"
	"path"
)

const piDurableVersion = "1.4.0"

func (e *PiEngine) GetDurableExecutionPlan(data *WorkflowData, logFile string) (*DurableExecutionPlan, error) {
	config := data.EngineConfig
	if config == nil || config.Durable == nil {
		return nil, errors.New("pi durable execution requires engine.pi.durable")
	}
	if config.Driver != "" || config.Command != "" || config.HarnessScript != "" ||
		len(config.Extensions) > 0 || len(config.Args) > 0 || len(data.SubAgents) > 0 {
		return nil, errors.New("pi durable does not support custom drivers, commands, harnesses, extensions, CLI args or sub-agents; remove these overrides")
	}
	if piSessionSettings(data).Enabled {
		return nil, errors.New("pi durable owns the session; remove engine.config.session")
	}
	if !isFirewallEnabled(data) {
		return nil, errors.New("pi durable requires the credential-isolated model gateway; enable sandbox.agent and remove network.firewall: false")
	}
	installSteps := []GitHubActionStep{{
		"      - name: Install Pi durable dependencies",
		"        run: |",
		"          set -euo pipefail",
		"          mkdir -p /tmp/gh-aw/pi-durable-runtime",
		`          cp "${RUNNER_TEMP}/gh-aw/actions/pi-durable/package.json" /tmp/gh-aw/pi-durable-runtime/package.json`,
		`          cp "${RUNNER_TEMP}/gh-aw/actions/pi-durable/package-lock.json" /tmp/gh-aw/pi-durable-runtime/package-lock.json`,
		"          npm ci --prefix /tmp/gh-aw/pi-durable-runtime --ignore-scripts --no-audit --no-fund",
		"        env:",
		fmt.Sprintf("          NPM_CONFIG_MIN_RELEASE_AGE: '%d'", resolveEngineCooldownDays(data)),
	}}
	installSteps = append(installSteps, GitHubActionStep{
		"      - name: Verify Pi durable dependencies",
		"        run: |",
		`          node --input-type=module -e 'for (const name of ["pi-durable", "pi-ai", "chord", "pi-telemetry"]) { const p = "/tmp/gh-aw/pi-durable-runtime/node_modules/@earendil-works/" + name; const { readFileSync } = await import("node:fs"); const manifest = JSON.parse(readFileSync(p + "/package.json", "utf8")); if (manifest.version !== "` + piDurableVersion + `") throw new Error("Incompatible durable dependency " + name); await import(p + "/dist/index.js"); }'`,
	})
	executionData := *data
	executionConfig := *config
	executionConfig.Driver = "pi_durable_driver.cjs"
	executionData.EngineConfig = &executionConfig
	return &DurableExecutionPlan{
		Files:             []string{path.Join(durableStateDirectory, durableCheckpointName(data))},
		InstallationSteps: installSteps,
		ExecutionSteps:    e.GetExecutionSteps(&executionData, logFile),
	}, nil
}

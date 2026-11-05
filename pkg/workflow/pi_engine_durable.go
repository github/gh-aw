package workflow

import (
	"errors"
	"path"
	"strings"
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
	cooldown := resolveEngineCooldownDays(data)
	var installSteps []GitHubActionStep
	for _, name := range []string{"pi-durable", "pi-ai", "chord", "pi-telemetry"} {
		steps := GenerateNpmInstallStepsWithScope("@earendil-works/"+name, piDurableVersion,
			"Install Pi durable "+name, "pi-durable", NPMInstallOptions{CooldownDays: &cooldown})
		for i, step := range steps {
			for j, line := range step {
				if strings.HasPrefix(line, "        run: npm install ") {
					step[j] = strings.Replace(line, "npm install ", "npm install --prefix /tmp/gh-aw/pi-durable-runtime --no-package-lock --no-audit --no-fund ", 1)
				}
			}
			steps[i] = step
		}
		installSteps = append(installSteps, steps...)
	}
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

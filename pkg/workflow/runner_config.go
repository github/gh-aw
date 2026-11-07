package workflow

import (
	"errors"
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
)

var runnerConfigLog = logger.New("workflow:runner_config")

// extractRunnerConfig extracts runner configuration from frontmatter.
// Returns nil when no runner configuration is present.
func extractRunnerConfig(frontmatter map[string]any) *RunnerConfig {
	runner, exists := frontmatter["runner"]
	if !exists {
		return nil
	}

	runnerObj, ok := runner.(map[string]any)
	if !ok {
		runnerConfigLog.Printf("runner field has unexpected type %T, expected object", runner)
		return nil
	}

	config := &RunnerConfig{}
	if topology, ok := runnerObj["topology"].(string); ok {
		config.Topology = RunnerTopology(topology)
		runnerConfigLog.Printf("Runner topology: %s", topology)
	}
	if installation, ok := runnerObj["installation"].(string); ok {
		config.Installation = installation
	}

	if config.Topology == "" && config.Installation == "" {
		return nil
	}

	return config
}

// validateRunnerConfig validates runner topology and installation configuration.
func validateRunnerConfig(config *RunnerConfig) error {
	if config == nil {
		return nil
	}

	switch config.Topology {
	case RunnerTopologyArcDind, "":
	default:
		return fmt.Errorf("unsupported runner.topology value %q; supported values: %q", config.Topology, RunnerTopologyArcDind)
	}
	switch config.Installation {
	case "", "rootless":
	case "privileged":
		if config.Topology == RunnerTopologyArcDind {
			return errors.New("runner.installation: privileged is not supported with runner.topology: arc-dind; use rootless or omit runner.installation")
		}
	default:
		return fmt.Errorf("unsupported runner.installation value %q; supported values: %q, %q", config.Installation, "rootless", "privileged")
	}
	return nil
}

// useRootlessCLIInstallation selects installation privilege independently of
// filesystem topology, preserving the existing defaults when not configured.
func useRootlessCLIInstallation(data *WorkflowData) bool {
	if data != nil && data.RunnerConfig != nil && data.RunnerConfig.Installation != "" {
		return data.RunnerConfig.Installation == "rootless"
	}
	return isArcDindTopology(data)
}

package workflow

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"strings"
)

// loadRepoConfig loads and caches repository-level configuration from aw.json.
func (c *Compiler) loadRepoConfig() (*RepoConfig, error) {
	if c.repoConfigLoaded {
		repoConfigLog.Print("loadRepoConfig: returning cached repo config")
		return c.repoConfig, c.repoConfigErr
	}

	repoConfigLog.Printf("loadRepoConfig: loading repo config from git root: %s", c.gitRoot)
	c.repoConfig, c.repoConfigErr = LoadRepoConfig(c.gitRoot)
	c.repoConfigLoaded = true
	if c.repoConfigErr != nil {
		repoConfigLog.Printf("loadRepoConfig: failed to load repo config: %v", c.repoConfigErr)
		if errors.Is(c.repoConfigErr, errWorkQueueConfig) {
			fmt.Fprintln(os.Stderr, formatCompilerMessage(RepoConfigFileName, "error", fmt.Sprintf("invalid global work_queue configuration: %v", c.repoConfigErr)))
			return c.repoConfig, c.repoConfigErr
		}
		fmt.Fprintln(
			os.Stderr,
			formatCompilerMessage(
				RepoConfigFileName,
				"warning",
				fmt.Sprintf(
					"failed to load aw.json; compilation will continue with defaults, and action_failure_issue_expires will fall back to %d hours where applicable: %v",
					DefaultActionFailureIssueExpiresHours,
					c.repoConfigErr,
				),
			),
		)
		c.IncrementWarningCount()
	} else {
		repoConfigLog.Print("loadRepoConfig: repo config loaded successfully")
	}
	return c.repoConfig, c.repoConfigErr
}

// getCompiledProjectUTCOffset returns the validated repo-configured UTC offset
// that should be baked into compiled workflow job env for runtime scripts.
func (c *Compiler) getCompiledProjectUTCOffset() string {
	repoConfig, err := c.loadRepoConfig()
	if err != nil || repoConfig == nil {
		return ""
	}
	return strings.TrimSpace(repoConfig.UTC)
}

// getContainerPinMappings returns a container-pin mapping table from aw.json,
// or nil when the file is absent, contains no mappings, or fails to load.
// Each ContainerPinTarget entry is combined into a single "image@digest" string
// for use by the internal resolution machinery. Callers may freely mutate the
// returned map.
func (c *Compiler) getContainerPinMappings() map[string]string {
	repoConfig, err := c.loadRepoConfig()
	if err != nil || repoConfig == nil || len(repoConfig.ContainerPins) == 0 {
		return nil
	}
	repoConfigLog.Printf("getContainerPinMappings: loaded %d container-pin mapping(s) from aw.json", len(repoConfig.ContainerPins))
	cp := make(map[string]string, len(repoConfig.ContainerPins))
	for k, v := range repoConfig.ContainerPins {
		cp[k] = v.Image + "@" + v.Digest
	}
	return cp
}

// getActionPinMappings returns a defensive copy of the action-pin mapping table
// from aw.json, or nil when the file is absent, contains no mappings, or fails
// to load. Callers may freely mutate the returned map.
func (c *Compiler) getActionPinMappings() map[string]string {
	repoConfig, err := c.loadRepoConfig()
	if err != nil || repoConfig == nil || len(repoConfig.ActionPins) == 0 {
		return nil
	}

	repoConfigLog.Printf("getActionPinMappings: loaded %d action-pin mapping(s) from aw.json", len(repoConfig.ActionPins))
	cp := make(map[string]string, len(repoConfig.ActionPins))
	maps.Copy(cp, repoConfig.ActionPins)
	return cp
}

func (c *Compiler) getActionPinPrefixes() map[string]string {
	repoConfig, err := c.loadRepoConfig()
	if err != nil || repoConfig == nil || len(repoConfig.ActionPinPrefixes) == 0 {
		return nil
	}
	cp := make(map[string]string, len(repoConfig.ActionPinPrefixes))
	maps.Copy(cp, repoConfig.ActionPinPrefixes)
	return cp
}

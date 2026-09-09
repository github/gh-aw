package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/ctxutil"
	"github.com/github/gh-aw/pkg/gitutil"
)

func (c *Compiler) resolveFrontmatterPluginRefs(workflowData *WorkflowData) error {
	if workflowData == nil || len(workflowData.Plugins) == 0 {
		return nil
	}
	if len(workflowData.PluginReferences) != 0 && len(workflowData.PluginReferences) != len(workflowData.Plugins) {
		return fmt.Errorf("plugins: internal error, PluginReferences length (%d) does not match Plugins length (%d)", len(workflowData.PluginReferences), len(workflowData.Plugins))
	}

	for i, plugin := range workflowData.Plugins {
		parsed := parseSkillRefSpec(plugin)
		if !parsed.isRemote || parsed.ref == "" {
			return fmt.Errorf("plugins[%d]: plugin reference %q cannot be pinned; expected owner/repository[/path]@ref, for example github/awesome-copilot/plugins/example@v1", i, plugin)
		}
		if parsed.isFullSHA {
			continue
		}
		if workflowData.ActionResolver == nil {
			return fmt.Errorf("plugins[%d]: %q cannot be resolved to a commit SHA because no GitHub reference resolver is available; expected an ActionResolver to be configured", i, plugin)
		}

		sha, err := workflowData.ActionResolver.ResolveSHA(
			ctxutil.OrBackground(workflowData.Ctx),
			parsed.repoPath,
			parsed.ref,
		)
		if err != nil {
			return fmt.Errorf("plugins[%d]: %q could not be resolved to a commit SHA, expected a valid branch, tag, or SHA reachable from the repository: %w", i, plugin, err)
		}
		if !gitutil.IsValidFullSHA(sha) {
			return fmt.Errorf("plugins[%d]: resolved %q to invalid commit SHA %q; expected a full 40-character lowercase hexadecimal SHA", i, plugin, sha)
		}
		workflowData.Plugins[i] = parsed.repoPath + "@" + sha
		if len(workflowData.PluginReferences) != 0 {
			workflowData.PluginReferences[i].Plugin = workflowData.Plugins[i]
		}
	}

	return nil
}

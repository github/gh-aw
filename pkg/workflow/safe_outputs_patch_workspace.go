package workflow

import (
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var patchWorkspaceLog = logger.New("workflow:safe_outputs_patch_workspace")

func injectCurrentCheckoutPatchWorkspacePath(handlerName string, handlerCfg map[string]any, data *WorkflowData) {
	if handlerCfg == nil || data == nil {
		return
	}
	if handlerName != "create_pull_request" && handlerName != "push_to_pull_request_branch" {
		return
	}

	checkoutManager := NewCheckoutManager(data.CheckoutConfigs)
	currentPath := normalizeCurrentCheckoutPatchPath(checkoutManager.GetCurrentCheckoutPath())
	if currentPath == "" {
		patchWorkspaceLog.Printf("No current checkout path resolved for handler=%s; skipping workspace patch injection", handlerName)
		return
	}
	currentRepo := strings.TrimSpace(checkoutManager.GetCurrentRepository())

	targetRepo := ""
	if value, ok := handlerCfg["target-repo"].(string); ok {
		targetRepo = strings.TrimSpace(value)
	}
	// Skip for wildcard and explicitly different repositories.
	if targetRepo == "*" {
		patchWorkspaceLog.Printf("Skipping workspace patch injection for handler=%s: target-repo is wildcard", handlerName)
		return
	}
	// If handler targets an explicit repository but current checkout resolved to
	// workflow repo (empty repository slug), do not inject a workspace override.
	if targetRepo != "" && currentRepo == "" {
		patchWorkspaceLog.Printf("Skipping workspace patch injection for handler=%s: target-repo=%q but current checkout has no repository slug", handlerName, targetRepo)
		return
	}
	if targetRepo != "" && currentRepo != "" && targetRepo != currentRepo {
		patchWorkspaceLog.Printf("Skipping workspace patch injection for handler=%s: target-repo=%q does not match current=%q", handlerName, targetRepo, currentRepo)
		return
	}

	handlerCfg["patch_workspace_path"] = currentPath
	if currentRepo != "" {
		handlerCfg["current_checkout_repo"] = currentRepo
	}
	patchWorkspaceLog.Printf("Injected workspace patch for handler=%s: path=%q repo=%q", handlerName, currentPath, currentRepo)
}

func normalizeCurrentCheckoutPatchPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == "." {
		return ""
	}
	path = strings.TrimPrefix(path, "./")
	path = filepath.Clean(path)
	if path == "." || path == "" || filepath.IsAbs(path) {
		return ""
	}
	if path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(path)
}

// injectCheckoutMapping adds a checkout_mapping to handler config for create_pull_request
// and push_to_pull_request_branch when target-repo is "*" (wildcard).
// The mapping tells the JS handler where each repository is checked out on disk,
// enabling it to operate on multiple repositories without dynamic git remote switching.
//
// The mapping is keyed by lowercase repo slug and values are relative paths within
// GITHUB_WORKSPACE for cross-repo checkouts (entries with repository + path).
func injectCheckoutMapping(handlerName string, handlerCfg map[string]any, data *WorkflowData) {
	if handlerCfg == nil || data == nil {
		return
	}
	if handlerName != "create_pull_request" && handlerName != "push_to_pull_request_branch" {
		return
	}

	// Only inject when target-repo is wildcard
	targetRepo := ""
	if value, ok := handlerCfg["target-repo"].(string); ok {
		targetRepo = strings.TrimSpace(value)
	}
	if targetRepo != "*" {
		return
	}

	// Build the checkout mapping from checkout: configs
	mapping := make(map[string]string)
	for _, cfg := range data.CheckoutConfigs {
		if cfg == nil || cfg.Repository == "" || cfg.Path == "" || cfg.Wiki {
			continue
		}
		// Normalize repo slug to lowercase for consistent lookup
		repoKey := strings.ToLower(strings.TrimSpace(cfg.Repository))
		normalizedPath := normalizeCurrentCheckoutPatchPath(cfg.Path)
		if normalizedPath != "" {
			mapping[repoKey] = normalizedPath
		}
	}

	// Only inject if there are actual cross-repo checkouts configured
	if len(mapping) == 0 {
		patchWorkspaceLog.Printf("No checkout mapping entries for handler=%s (wildcard target-repo but no cross-repo checkout: configs)", handlerName)
		return
	}

	handlerCfg["checkout_mapping"] = mapping
	patchWorkspaceLog.Printf("Injected checkout_mapping for handler=%s: %d entries", handlerName, len(mapping))
}

// injectDynamicCheckout marks create_pull_request / push_to_pull_request_branch handler
// configs with dynamic_checkout when safe-outputs.dynamic-checkout is enabled. In that mode
// the safe_outputs job has no actions/checkout layout, so checkout_mapping is dropped and
// the handlers materialize each target repository themselves.
func injectDynamicCheckout(handlerName string, handlerCfg map[string]any, data *WorkflowData) {
	if handlerCfg == nil || data == nil || data.SafeOutputs == nil || !data.SafeOutputs.DynamicCheckout {
		return
	}
	if handlerName != "create_pull_request" && handlerName != "push_to_pull_request_branch" {
		return
	}
	handlerCfg["dynamic_checkout"] = true
	delete(handlerCfg, "checkout_mapping")
	patchWorkspaceLog.Printf("Injected dynamic_checkout for handler=%s", handlerName)
}

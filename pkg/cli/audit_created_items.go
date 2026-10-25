package cli

import "slices"

// resolveAuditItemIdentities uses execution artifacts, never agent-authored text,
// to recover resource references missing from older manifests.
func resolveAuditItemIdentities(items []CreatedItemReport, logsPath string) []CreatedItemReport {
	items = slices.Clone(items)
	targets, _ := loadResolvedTemporaryIDTargets(logsPath)
	for i := range items {
		item := &items[i]
		if target, ok := targets[item.TemporaryID]; ok && target.Repo != "" && target.Number > 0 &&
			(item.Provider == "" || item.Provider == "github") &&
			(item.Repo == "" || item.Repo == target.Repo) &&
			(item.Number == 0 || item.Number == target.Number) {
			item.Repo = target.Repo
			item.Number = target.Number
		}
		if item.URL != "" || (item.Repo != "" && item.Number > 0) || item.ID != nil || item.Identifier != "" {
			item.IdentityUnavailableReason = ""
			continue
		}
		item.IdentityUnavailableReason = "Execution artifacts do not contain a resource URL or identifier; no matching temporary ID mapping is available."
	}
	return items
}

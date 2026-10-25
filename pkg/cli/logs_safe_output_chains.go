package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
)

var safeOutputChainsLog = logger.New("cli:logs_safe_output_chains")

const (
	temporaryIDMapStatusLoaded  = "loaded"
	temporaryIDMapStatusMissing = "missing"
	temporaryIDMapStatusInvalid = "invalid"
)

// SafeOutputChainMetrics captures in-run chaining created by temporary IDs.
// These metrics describe compound safe-output actuation within one run; they do
// not imply cross-run lineage.
type SafeOutputChainMetrics struct {
	ManifestEntryCount         int    `json:"manifest_entry_count,omitempty"`
	TemporaryIDMapStatus       string `json:"temporary_id_map_status,omitempty"`
	TemporaryIDMappings        int    `json:"temporary_id_mappings,omitempty"`
	ChainedTargetCount         int    `json:"chained_target_count,omitempty"`
	ChainedFollowupActionCount int    `json:"chained_followup_action_count,omitempty"`
	DelegatedTempTargetCount   int    `json:"delegated_temp_target_count,omitempty"`
	ClosedTempTargetCount      int    `json:"closed_temp_target_count,omitempty"`
}

type resolvedTemporaryIDTarget struct {
	Repo   string `json:"repo"`
	Number int    `json:"number"`
}

func buildSafeOutputChainMetrics(logsPath string) SafeOutputChainMetrics {
	items := extractCreatedItemsFromManifest(logsPath)
	metrics := SafeOutputChainMetrics{
		ManifestEntryCount: len(items),
	}
	if logsPath == "" {
		return metrics
	}
	if !safeOutputArtifactsPresent(logsPath) {
		safeOutputChainsLog.Print("No safe output artifacts present, skipping chain metrics")
		return metrics
	}

	safeOutputChainsLog.Printf("Building safe output chain metrics: manifest_entries=%d", len(items))

	resolvedTargets, tempIDMapStatus := loadResolvedTemporaryIDTargets(logsPath)
	metrics.TemporaryIDMapStatus = tempIDMapStatus
	metrics.TemporaryIDMappings = len(resolvedTargets)
	safeOutputChainsLog.Printf("Temporary ID map status=%s, mappings=%d", tempIDMapStatus, len(resolvedTargets))
	if len(items) == 0 || len(resolvedTargets) == 0 {
		return metrics
	}

	return countSafeOutputChains(items, resolvedTargets, metrics)
}

func countSafeOutputChains(items []CreatedItemReport, resolvedTargets map[string]resolvedTemporaryIDTarget, metrics SafeOutputChainMetrics) SafeOutputChainMetrics {
	itemCounts := make(map[string]int)
	delegatedTargets := make(map[string]struct {
	})
	closedTargets := make(map[string]struct {
	})
	for _, item := range items {
		if item.Repo == "" || item.Number <= 0 {
			continue
		}
		key := safeOutputTargetKey(item.Repo, item.Number)
		itemCounts[key]++
		switch item.Type {
		case "assign_to_agent", "create_agent_session":
			delegatedTargets[key] = struct {
			}{}
		case "close_issue", "close_pull_request", "close_discussion", "merge_pull_request":
			closedTargets[key] = struct {
			}{}
		}
	}

	for _, target := range resolvedTargets {
		key := safeOutputTargetKey(target.Repo, target.Number)
		count := itemCounts[key]
		if count > 1 {
			metrics.ChainedTargetCount++
			metrics.ChainedFollowupActionCount += count - 1
		}
		if setutil.Contains(delegatedTargets, key) {
			metrics.DelegatedTempTargetCount++
		}
		if setutil.Contains(closedTargets, key) {
			metrics.ClosedTempTargetCount++
		}
	}

	safeOutputChainsLog.Printf("Chain metrics computed: chained_targets=%d, followup_actions=%d, delegated=%d, closed=%d",
		metrics.ChainedTargetCount, metrics.ChainedFollowupActionCount, metrics.DelegatedTempTargetCount, metrics.ClosedTempTargetCount)
	return metrics
}

func loadResolvedTemporaryIDTargets(logsPath string) (map[string]resolvedTemporaryIDTarget, string) {
	if logsPath == "" {
		return nil, ""
	}

	content, err := os.ReadFile(filepath.Join(logsPath, constants.TemporaryIdMapFilename.String()))
	if err != nil || len(content) == 0 {
		return nil, temporaryIDMapStatusMissing
	}

	var resolved map[string]resolvedTemporaryIDTarget
	if err := json.Unmarshal(content, &resolved); err != nil {
		return nil, temporaryIDMapStatusInvalid
	}
	return resolved, temporaryIDMapStatusLoaded
}

func safeOutputArtifactsPresent(logsPath string) bool {
	if logsPath == "" {
		return false
	}

	for _, filename := range []string{
		safeOutputItemsManifestFilename,
		constants.TemporaryIdMapFilename.String(),
		constants.SafeOutputErrorsFilename,
	} {
		if _, err := os.Stat(filepath.Join(logsPath, filename)); err == nil {
			return true
		}
	}

	return false
}

func safeOutputTargetKey(repo string, number int) string {
	return repo + "#" + strconv.Itoa(number)
}

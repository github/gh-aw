// This file provides command-line interface functionality for gh-aw.
// This file (logs_artifact_set.go) defines artifact set types and resolution logic
// for filtering artifact downloads in the logs and audit commands.
//
// Key responsibilities:
//   - Defining known artifact set names (all, agent, mcp, firewall, detection, github-api, activation)
//   - Mapping sets to concrete artifact name patterns
//   - Validating artifact set inputs from CLI flags and MCP arguments
//   - Determining whether a given artifact name matches an active filter
//   - Finding which filter entries are missing from a previously-downloaded run folder

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
)

var artifactSetLog = logger.New("cli:logs_artifact_set")

// ArtifactSet is a named group of related artifacts that can be downloaded together.
// Using a named set allows callers to request only the artifacts they need for a
// specific analysis, rather than downloading all artifacts for a run.
type ArtifactSet string

const (
	// ArtifactSetAll downloads every artifact for the run (default behavior).
	ArtifactSetAll ArtifactSet = "all"

	// ArtifactSetActivation downloads the activation artifact (aw_info.json, prompt.txt,
	// and github_rate_limits.jsonl from the activation job).
	ArtifactSetActivation ArtifactSet = "activation"

	// ArtifactSetAgent downloads the unified agent artifact containing agent logs,
	// safe outputs, token usage, and agent-side github_rate_limits.jsonl, plus the
	// tiny fallback artifact that carries critical agent-output files when the
	// unified upload fails.
	ArtifactSetAgent ArtifactSet = "agent"

	// ArtifactSetMCP downloads the agent artifact which now includes MCP
	// gateway traffic logs (gateway.jsonl, rpc-messages.jsonl) containing tool
	// calls, server negotiations, and RPC request/response pairs.
	ArtifactSetMCP ArtifactSet = "mcp"

	// ArtifactSetFirewall downloads the agent artifact which now includes
	// AWF network policy data: domain allow/deny decisions, firewall audit trail,
	// and token-usage proxy logs.
	ArtifactSetFirewall ArtifactSet = "firewall"

	// ArtifactSetDetection downloads the detection artifact containing threat
	// detection log output.
	ArtifactSetDetection ArtifactSet = "detection"

	// ArtifactSetGitHubAPI downloads the artifacts that contain GitHub API rate-limit
	// logs (github_rate_limits.jsonl), which are included in both the activation and
	// agent artifacts.
	ArtifactSetGitHubAPI ArtifactSet = "github-api"

	// ArtifactSetExperiment downloads the experiment artifact containing A/B experiment
	// state (state.jsonl or state.json) uploaded by the activation job when experiments are declared.
	ArtifactSetExperiment ArtifactSet = "experiment"

	// ArtifactSetUsage downloads the compact usage artifact produced by the
	// conclusion job (aw-info.jsonl, usage summaries, token usage JSONL).
	ArtifactSetUsage ArtifactSet = "usage"

	// ArtifactSetGraders downloads the artifacts that carry grader results
	// (grader_results.json): the compact usage artifact, the agent artifact, and
	// the fallback artifact used when uploading the unified agent artifact fails.
	ArtifactSetGraders ArtifactSet = "graders"

	// ArtifactSetEvals downloads the usage artifact, which now includes evals.jsonl
	// produced by the evals job (copied into usage by the conclusion job).
	ArtifactSetEvals ArtifactSet = "evals"
)

// artifactSetArtifacts maps each named set to the list of artifact base names it includes.
// A nil value for ArtifactSetAll is intentional: it signals "no filter, download
// everything" and is handled specially in ResolveArtifactFilter (a nil return from
// ResolveArtifactFilter means no filter is active so the caller downloads all artifacts).
var artifactSetArtifacts = map[ArtifactSet][]string{
	ArtifactSetAll:        nil, // no filtering – download all artifacts
	ArtifactSetActivation: {constants.ActivationArtifactName.String()},
	ArtifactSetAgent:      {constants.AgentArtifactName.String(), constants.AgentOutputFallbackArtifactName.String()},
	ArtifactSetMCP:        {constants.AgentArtifactName.String()},
	ArtifactSetFirewall:   {constants.AgentArtifactName.String()},
	ArtifactSetDetection:  {constants.DetectionArtifactName.String()},
	// github-api: both jobs upload github_rate_limits.jsonl; fetch both for a complete view.
	ArtifactSetGitHubAPI: {constants.ActivationArtifactName.String(), constants.AgentArtifactName.String()},
	// experiment: A/B experiment state uploaded by the activation job.
	ArtifactSetExperiment: {constants.ExperimentArtifactName.String()},
	// usage: compact conclusion artifact for lightweight reporting/forecasting.
	ArtifactSetUsage: {constants.UsageArtifactName.String()},
	// evals: evals results are now included in the usage artifact.
	ArtifactSetEvals: {constants.UsageArtifactName.String()},
	// graders: grader results are included in the usage artifact, remain part of
	// the unified agent artifact, and are preserved in the fallback transport.
	ArtifactSetGraders: {constants.UsageArtifactName.String(), constants.AgentArtifactName.String(), constants.AgentOutputFallbackArtifactName.String()},
}

const maxArtifactHintExamples = 2

const downloadedArtifactsMarkerDir = ".downloaded-artifacts"

// ValidArtifactSetNames returns a sorted list of valid artifact set names,
// derived dynamically from the artifactSetArtifacts map to stay in sync automatically.
func ValidArtifactSetNames() []string {
	names := make([]string, 0, len(artifactSetArtifacts))
	for k := range artifactSetArtifacts {
		names = append(names, string(k))
	}
	sort.Strings(names)
	return names
}

func usageOnlyArtifactHintMessage() string {
	examples := artifactHintExampleSets()
	switch len(examples) {
	case 0:
		return "Only the usage artifact was downloaded. Use --artifacts all to download all artifacts."
	case 1:
		return fmt.Sprintf("Only the usage artifact was downloaded. Use --artifacts all to download all artifacts, or a specific set such as --artifacts %s.", examples[0])
	default:
		return fmt.Sprintf(
			"Only the usage artifact was downloaded. Use --artifacts all to download all artifacts, or a specific set such as --artifacts %s, or combinations such as --artifacts %s.",
			examples[0],
			strings.Join(examples, ","),
		)
	}
}

func artifactHintExampleSets() []string {
	valid := ValidArtifactSetNames()
	excluded := map[string]struct{}{
		string(ArtifactSetAll):   {},
		string(ArtifactSetUsage): {},
	}
	selected := make([]string, 0, maxArtifactHintExamples)
	seen := make(map[string]struct{}, maxArtifactHintExamples)

	add := func(name string) {
		if len(selected) == maxArtifactHintExamples {
			return
		}
		if _, skip := excluded[name]; skip {
			return
		}
		if _, dup := seen[name]; dup {
			return
		}
		if slices.Contains(valid, name) {
			selected = append(selected, name)
			seen[name] = struct{}{}
		}
	}

	add(string(ArtifactSetAgent))
	add(string(ArtifactSetFirewall))
	for _, name := range valid {
		add(name)
	}

	return selected
}

// ValidateArtifactSets checks that every entry in sets is a known ArtifactSet name.
// Returns an error listing any unrecognized names.
func ValidateArtifactSets(sets []string) error {
	artifactSetLog.Printf("Validating %d artifact set(s): %s", len(sets), strings.Join(sets, ", "))
	var unknown []string
	for _, s := range sets {
		if _, ok := artifactSetArtifacts[ArtifactSet(s)]; !ok {
			unknown = append(unknown, s)
		}
	}
	if len(unknown) > 0 {
		artifactSetLog.Printf("Unknown artifact set(s) rejected: %s", strings.Join(unknown, ", "))
		return fmt.Errorf("unknown artifact set(s): %s; valid sets are: %s",
			strings.Join(unknown, ", "),
			strings.Join(ValidArtifactSetNames(), ", "))
	}
	artifactSetLog.Print("All artifact sets are valid")
	return nil
}

// ResolveArtifactFilter converts a list of set names into a deduplicated list of
// artifact base names to download.  A nil or empty input, or any entry equal to
// ArtifactSetAll, returns nil (meaning: download every artifact – no filter applied).
func ResolveArtifactFilter(sets []string) []string {
	if len(sets) == 0 {
		artifactSetLog.Print("No artifact sets specified, downloading all artifacts")
		return nil
	}

	// If "all" appears anywhere, disable filtering entirely.
	for _, s := range sets {
		if ArtifactSet(s) == ArtifactSetAll {
			artifactSetLog.Print("Artifact set 'all' specified, downloading all artifacts")
			return nil
		}
	}

	seen := make(map[string]struct {
	})
	var names []string
	for _, s := range sets {
		for _, name := range artifactSetArtifacts[ArtifactSet(s)] {
			if !setutil.Contains(seen, name) {
				seen[name] = struct {
				}{}
				names = append(names, name)
			}
		}
	}
	artifactSetLog.Printf("Resolved artifact filter: sets=%v -> artifacts=%v", sets, names)
	return names
}

// artifactMatchesFilter reports whether the given artifact name should be downloaded
// given the active filter.
//
// A nil or empty filter means "accept everything".
//
// The match is satisfied when:
//  1. The artifact name exactly equals one of the filter entries, or
//  2. The artifact name ends with "-{filterEntry}" (workflow_call prefix pattern,
//     e.g. "abc123-agent" matches filter entry "agent").
func artifactMatchesFilter(name string, filter []string) bool {
	if len(filter) == 0 {
		return true
	}
	for _, f := range filter {
		if name == f || strings.HasSuffix(name, "-"+f) {
			return true
		}
	}
	return false
}

// findMissingFilterEntries checks which entries of the given artifact filter do not yet
// have a corresponding directory on disk in outputDir.
//
// For each filter entry (e.g. "firewall-audit-logs"), the function looks for:
//  1. A directory exactly named {entry} inside outputDir, or
//  2. Any directory whose name ends with "-{entry}" (workflow_call prefix pattern,
//     e.g. "abc123-agent" for filter entry "agent").
//
// Entries that have a matching directory are considered already-downloaded.
// Entries without a match are returned as "missing" — they still need to be fetched.
func findMissingFilterEntries(filter []string, outputDir string) []string {
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		// If we can't read the directory, assume everything is missing.
		return filter
	}

	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	if markers, markerErr := os.ReadDir(filepath.Join(outputDir, downloadedArtifactsMarkerDir)); markerErr == nil {
		for _, marker := range markers {
			if !marker.IsDir() {
				dirs = append(dirs, marker.Name())
			}
		}
	}

	// A complete-download marker satisfies every filtered request: if it is
	// present the caller already downloaded all artifacts for this run.
	if slices.Contains(dirs, string(ArtifactSetAll)) {
		artifactSetLog.Printf("Complete-download marker present in %s; all filter entries satisfied", outputDir)
		return nil
	}

	var missing []string
	for _, f := range filter {
		found := false
		for _, d := range dirs {
			// Mirror the artifactMatchesFilter logic: accept exact match or any directory
			// ending in "-{f}", which covers the workflow_call prefix pattern where GitHub
			// Actions prepends a short hash (e.g. "abc123-agent"). Note that this means a
			// hypothetical directory named "super-agent" would satisfy filter entry "agent",
			// but in practice artifact directories in a run folder only come from GitHub
			// Actions downloads and follow the "{hash}-{base}" or exact-base patterns.
			if d == f || strings.HasSuffix(d, "-"+f) || agentOutputTransportAlternates(f, d) {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, f)
		}
	}
	if len(missing) > 0 {
		artifactSetLog.Printf("Missing artifact entries in %s: %v", outputDir, missing)
	} else {
		artifactSetLog.Printf("All %d artifact filter entries present in %s", len(filter), outputDir)
	}
	return missing
}

func agentOutputTransportAlternates(filterEntry, downloadedName string) bool {
	if filterEntry == constants.AgentArtifactName.String() {
		return artifactNameMatchesBase(downloadedName, constants.AgentOutputFallbackArtifactName.String())
	}
	if filterEntry == constants.AgentOutputFallbackArtifactName.String() {
		return artifactNameMatchesBase(downloadedName, constants.AgentArtifactName.String())
	}
	return false
}

func artifactNameMatchesBase(name, base string) bool {
	if base == "" {
		return false
	}
	return name == base || strings.HasSuffix(name, "-"+base)
}

func markArtifactDownloaded(outputDir, artifactName string) error {
	if err := validateArtifactName(artifactName); err != nil {
		return err
	}
	markerDir := filepath.Join(outputDir, downloadedArtifactsMarkerDir)
	if err := os.MkdirAll(markerDir, constants.DirPermPublic); err != nil {
		return fmt.Errorf("failed to create downloaded artifact marker directory: %w", err)
	}
	markerPath := filepath.Join(markerDir, artifactName)
	if err := os.WriteFile(markerPath, nil, constants.FilePermPublic); err != nil {
		return fmt.Errorf("failed to write downloaded artifact marker: %w", err)
	}
	return nil
}

// applyEvalsArtifact appends the evals artifact set to artifacts when evalsOnly is true
// and neither ArtifactSetEvals, ArtifactSetUsage, nor ArtifactSetAll is already present.
// Because evals results are now included in the usage artifact, this ensures evals.jsonl
// is downloaded without requiring the user to also pass --artifacts evals or --artifacts usage.
//
// For callers that treat an empty artifacts slice as "all", the function returns
// the empty slice unchanged and does not append evals.
func applyEvalsArtifact(artifacts []string, evalsOnly bool) []string {
	if len(artifacts) == 0 {
		return artifacts
	}
	if evalsOnly &&
		!slices.Contains(artifacts, string(ArtifactSetEvals)) &&
		!slices.Contains(artifacts, string(ArtifactSetUsage)) &&
		!slices.Contains(artifacts, string(ArtifactSetAll)) {
		return append(artifacts, string(ArtifactSetEvals))
	}
	return artifacts
}

// applyGradersArtifact appends the graders artifact set to artifacts when gradersOnly is true
// and neither ArtifactSetGraders nor ArtifactSetAll is already present.
func applyGradersArtifact(artifacts []string, gradersOnly bool) []string {
	if len(artifacts) == 0 {
		return artifacts
	}
	if gradersOnly &&
		!slices.Contains(artifacts, string(ArtifactSetGraders)) &&
		!slices.Contains(artifacts, string(ArtifactSetAll)) {
		return append(artifacts, string(ArtifactSetGraders))
	}
	return artifacts
}

// isEvalsArtifactRequested reports whether evals were explicitly requested,
// either via --evals or by including --artifacts evals. Callers use this to
// decide whether to bypass stale cache entries and trigger legacy dedicated-evals
// fallback downloads when evals.jsonl is missing from usage artifacts.
func isEvalsArtifactRequested(evalsOnly bool, artifactSets []string) bool {
	return evalsOnly || slices.Contains(artifactSets, string(ArtifactSetEvals))
}

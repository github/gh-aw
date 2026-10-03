package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
)

var safeUpdateManifestLog = logger.New("workflow:safe_update_manifest")

// ghawManifestPattern matches a "# gh-aw-manifest: {...}" line in a lock file header.
var ghawManifestPattern = regexp.MustCompile(`#\s*gh-aw-manifest:\s*(\{.+\})`)

// currentGHAWManifestVersion is the current schema version for the GHAW manifest header.
const currentGHAWManifestVersion = 1

// GHAWManifestAction represents a single GitHub Action referenced in a compiled workflow.
type GHAWManifestAction struct {
	Repo    string `json:"repo"`
	SHA     string `json:"sha"`
	Version string `json:"version,omitempty"`
}

// GHAWManifestContainer represents a Docker container image referenced in a compiled workflow.
// It records the original mutable tag, the resolved SHA-256 digest (when available),
// and the full pinned reference that combines both.
type GHAWManifestContainer struct {
	Image       string `json:"image"`                  // Original tag, e.g. "node:lts-alpine"
	Digest      string `json:"digest,omitempty"`       // SHA-256 digest, e.g. "sha256:abc123..."
	PinnedImage string `json:"pinned_image,omitempty"` // Full ref, e.g. "node:lts-alpine@sha256:abc123..."
}

// GHAWManifestResolutionFailure represents an action-ref pinning failure captured
// during compilation. These failures are embedded for lock-file auditing.
type GHAWManifestResolutionFailure struct {
	Repo      string `json:"repo"`
	Ref       string `json:"ref"`
	ErrorType string `json:"error_type"`
}

// GHAWManifestMemoryValidationScript represents a custom memory validation
// script without storing its potentially sensitive source in the lock file.
type GHAWManifestMemoryValidationScript struct {
	Memory string `json:"memory"`
	SHA256 string `json:"sha256"`
}

// GHAWManifestMCPServer represents an MCP server exposed to the agent and the
// statically-declared tool names allowed for that server.
type GHAWManifestMCPServer struct {
	Name  string   `json:"name"`
	Tools []string `json:"tools,omitempty"`
}

// GHAWManifestDetectionPolicy records the effective compiler policy independently
// of the jobs, so deleting a detection job cannot masquerade as a disabled policy.
type GHAWManifestDetectionPolicy struct {
	Mode      string `json:"mode"`
	Condition string `json:"condition,omitempty"`
}

// GHAWManifest is the single-line JSON payload embedded as a "# gh-aw-manifest: ..."
// comment in generated lock files. It records the secrets, external actions, and
// container images that were detected at the time the lock file was last compiled
// so that subsequent compilations can detect newly introduced secrets when safe
// update mode is enabled.
type GHAWManifest struct {
	Version                     int                                  `json:"version"`
	Secrets                     []string                             `json:"secrets"`
	Actions                     []GHAWManifestAction                 `json:"actions"`
	Skills                      []string                             `json:"skills,omitempty"`                    // frontmatter skill specs (owner/repo@sha or owner/repo/skill/path@sha), sorted
	Plugins                     []string                             `json:"plugins,omitempty"`                   // frontmatter plugin specs (owner/repository[/path]@sha), sorted
	ResolutionFailures          []GHAWManifestResolutionFailure      `json:"resolution_failures,omitempty"`       // unresolved action-ref pinning failures
	Containers                  []GHAWManifestContainer              `json:"containers,omitempty"`                // container images used, with digest when available
	Redirect                    string                               `json:"redirect,omitempty"`                  // frontmatter redirect target for moved workflows
	HasPullRequest              bool                                 `json:"has_pull_request,omitempty"`          // whether on: includes pull_request
	HasPullRequestTarget        bool                                 `json:"has_pull_request_target,omitempty"`   // whether on: includes pull_request_target
	MemoryValidationScripts     []GHAWManifestMemoryValidationScript `json:"memory_validation_scripts,omitempty"` // custom repo/cache memory validation scripts, hashed
	MCPServers                  []GHAWManifestMCPServer              `json:"mcp_servers"`                         // MCP servers/tools exposed to the agent, independent of engine-specific allowlist syntax
	ThreatDetectionSuppressions []ThreatDetectionSuppression         `json:"threat_detection_suppressions,omitempty"`
	ThreatDetection             *GHAWManifestDetectionPolicy         `json:"threat_detection,omitempty"`
}

// NewGHAWManifest builds a GHAWManifest from the raw secret names, action reference
// strings, container image references, and skill specs produced at compile time.
//
// secretNames entries may include or omit the "secrets." prefix; the prefix is always
// stripped before storage so the manifest contains plain names (e.g. "GITHUB_TOKEN").
// actionRefs entries follow the format produced by CollectActionReferences, e.g.
//
//	"actions/checkout@abc1234 # v4"
//
// containers is the list of container image entries with full digest info (when available).
// skillSpecs is the list of skill references from the workflow frontmatter.
func NewGHAWManifest(secretNames []string, actionRefs []string, failures []GHAWManifestResolutionFailure, containers []GHAWManifestContainer, redirect string, skillSpecs []string, pluginSpecs []string, onField any) *GHAWManifest {
	safeUpdateManifestLog.Printf("Building gh-aw-manifest: raw_secrets=%d, raw_actions=%d, containers=%d, skills=%d, plugins=%d", len(secretNames), len(actionRefs), len(containers), len(skillSpecs), len(pluginSpecs))

	// Normalize secret names to full "secrets.NAME" form and deduplicate.
	seen := make(map[string]struct {
	})
	secrets := make([]string, 0, len(secretNames))
	for _, name := range secretNames {
		full := normalizeSecretName(name)
		if !setutil.Contains(seen, full) {
			seen[full] = struct {
			}{}
			secrets = append(secrets, full)
		}
	}
	sort.Strings(secrets)

	actions := parseActionRefs(actionRefs)
	resolutionFailures := normalizeResolutionFailures(failures)

	sortedContainers := normalizeManifestContainers(containers)

	safeUpdateManifestLog.Printf("Manifest built: version=%d, secrets=%d, actions=%d, containers=%d, skills=%d",
		currentGHAWManifestVersion, len(secrets), len(actions), len(sortedContainers), len(skillSpecs))

	sortedSkills := normalizeManifestSpecs(skillSpecs)
	sortedPlugins := normalizeManifestSpecs(pluginSpecs)

	hasPR, hasPRTarget := detectPullRequestEvents(onField)

	return &GHAWManifest{
		Version:              currentGHAWManifestVersion,
		Secrets:              secrets,
		Actions:              actions,
		Skills:               sortedSkills,
		Plugins:              sortedPlugins,
		ResolutionFailures:   resolutionFailures,
		Containers:           sortedContainers,
		Redirect:             strings.TrimSpace(redirect),
		HasPullRequest:       hasPR,
		HasPullRequestTarget: hasPRTarget,
	}
}

func normalizeManifestSpecs(specs []string) []string {
	seen := make(map[string]struct{}, len(specs))
	var sorted []string
	for _, spec := range specs {
		if spec != "" && !setutil.Contains(seen, spec) {
			seen[spec] = struct{}{}
			sorted = append(sorted, spec)
		}
	}
	sort.Strings(sorted)
	return sorted
}

func normalizeManifestContainers(containers []GHAWManifestContainer) []GHAWManifestContainer {
	seen := make(map[string]struct{}, len(containers))
	sorted := make([]GHAWManifestContainer, 0, len(containers))
	for _, container := range containers {
		if container.Image != "" && !setutil.Contains(seen, container.Image) {
			seen[container.Image] = struct{}{}
			sorted = append(sorted, container)
		}
	}
	slices.SortFunc(sorted, func(a, b GHAWManifestContainer) int {
		switch {
		case a.Image < b.Image:
			return -1
		case a.Image > b.Image:
			return 1
		default:
			return 0
		}
	})
	return sorted
}

func detectPullRequestEvents(onField any) (hasPR bool, hasPRTarget bool) {
	switch v := onField.(type) {
	case string:
		return v == "pull_request", v == "pull_request_target"
	case []any:
		for _, item := range v {
			event, ok := item.(string)
			if !ok {
				continue
			}
			if event == "pull_request" {
				hasPR = true
			}
			if event == "pull_request_target" {
				hasPRTarget = true
			}
		}
	case map[string]any:
		_, hasPR = v["pull_request"]
		_, hasPRTarget = v["pull_request_target"]
	}
	return hasPR, hasPRTarget
}

func collectMemoryValidationScripts(data *WorkflowData) []GHAWManifestMemoryValidationScript {
	var scripts []GHAWManifestMemoryValidationScript
	add := func(kind, id string, validation *MemoryValidationConfig) {
		if validation == nil || validation.Script == "" {
			return
		}
		hash := sha256.Sum256([]byte(validation.Script))
		scripts = append(scripts, GHAWManifestMemoryValidationScript{
			Memory: kind + ":" + id,
			SHA256: hex.EncodeToString(hash[:]),
		})
	}
	if data.RepoMemoryConfig != nil {
		for _, memory := range data.RepoMemoryConfig.Memories {
			add("repo-memory", memory.ID, memory.Validation)
		}
	}
	if data.CacheMemoryConfig != nil {
		for _, cache := range data.CacheMemoryConfig.Caches {
			add("cache-memory", cache.ID, cache.Validation)
		}
	}
	if data.DriveMemoryConfig != nil {
		for _, drive := range data.DriveMemoryConfig.Drives {
			add("drive-memory", drive.ID, drive.Validation)
		}
	}
	slices.SortFunc(scripts, func(a, b GHAWManifestMemoryValidationScript) int {
		return strings.Compare(a.Memory, b.Memory)
	})
	return scripts
}

// normalizeSecretName ensures a secret identifier is stored as a plain name
// without the "secrets." prefix (e.g. "GITHUB_TOKEN" not "secrets.GITHUB_TOKEN").
// If the input already carries the "secrets." prefix it is stripped; otherwise
// the name is returned unchanged.
func normalizeSecretName(name string) string {
	return strings.TrimPrefix(name, "secrets.")
}

// parseActionRefs converts the action reference strings returned by
// CollectActionReferences into structured GHAWManifestAction values.
//
// Accepted formats (produced by actionReferencePattern):
//
//	"actions/checkout@abc1234 # v4"   → repo=actions/checkout, sha=abc1234, version=v4
//	"actions/checkout@v4"             → repo=actions/checkout, sha=v4, version=v4
func parseActionRefs(refs []string) []GHAWManifestAction {
	seen := make(map[string]struct {
	})
	actions := make([]GHAWManifestAction, 0, len(refs))

	for _, raw := range refs {
		ref := raw

		// Extract optional inline comment (e.g. "# v4") for the human-readable version tag.
		comment := ""
		if idx := strings.Index(ref, " # "); idx >= 0 {
			comment = strings.TrimSpace(ref[idx+3:])
			ref = strings.TrimSpace(ref[:idx])
		}

		// Split on the last "@" to separate repo from sha/version.
		at := strings.LastIndex(ref, "@")
		if at < 0 {
			continue
		}
		repo := ref[:at]
		sha := ref[at+1:]
		version := comment
		if version == "" {
			version = sha
		}

		key := repo + "@" + sha
		if setutil.Contains(seen, key) {
			continue
		}
		seen[key] = struct {
		}{}

		actions = append(actions, GHAWManifestAction{
			Repo:    repo,
			SHA:     sha,
			Version: version,
		})
	}

	// Sort for deterministic output.
	slices.SortFunc(actions, func(a, b GHAWManifestAction) int {
		if a.Repo != b.Repo {
			if a.Repo < b.Repo {
				return -1
			}
			return 1
		}
		switch {
		case a.SHA < b.SHA:
			return -1
		case a.SHA > b.SHA:
			return 1
		default:
			return 0
		}
	})

	return actions
}

func normalizeResolutionFailures(failures []GHAWManifestResolutionFailure) []GHAWManifestResolutionFailure {
	type failureKey struct {
		Repo      string
		Ref       string
		ErrorType string
	}
	seen := make(map[failureKey]bool)
	normalized := make([]GHAWManifestResolutionFailure, 0, len(failures))
	for _, f := range failures {
		repo := strings.TrimSpace(f.Repo)
		ref := strings.TrimSpace(f.Ref)
		errorType := strings.TrimSpace(f.ErrorType)
		if repo == "" || ref == "" || errorType == "" {
			continue
		}
		key := failureKey{Repo: repo, Ref: ref, ErrorType: errorType}
		if seen[key] {
			continue
		}
		seen[key] = true
		normalized = append(normalized, GHAWManifestResolutionFailure{
			Repo:      repo,
			Ref:       ref,
			ErrorType: errorType,
		})
	}
	slices.SortFunc(normalized, func(a, b GHAWManifestResolutionFailure) int {
		if a.Repo != b.Repo {
			if a.Repo < b.Repo {
				return -1
			}
			return 1
		}
		if a.Ref != b.Ref {
			if a.Ref < b.Ref {
				return -1
			}
			return 1
		}
		switch {
		case a.ErrorType < b.ErrorType:
			return -1
		case a.ErrorType > b.ErrorType:
			return 1
		default:
			return 0
		}
	})
	return normalized
}

// ToJSON serialises the manifest to a compact, single-line JSON string suitable
// for embedding in a YAML comment header.
func (m *GHAWManifest) ToJSON() (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("gh-aw-manifest could not be serialized to JSON, expected fields that marshal cleanly: %w", err)
	}
	return string(data), nil
}

// ExtractGHAWManifestFromLockFile parses the gh-aw-manifest from a lock file's
// comment header. Returns nil (with no error) when no manifest line is present,
// which is the normal state for lock files generated before this feature was
// introduced.
func ExtractGHAWManifestFromLockFile(content string) (*GHAWManifest, error) {
	match := ghawManifestPattern.FindString(content)
	if _, payload, found := strings.Cut(match, ":"); found {
		var m GHAWManifest
		if err := json.Unmarshal([]byte(payload), &m); err != nil {
			return nil, fmt.Errorf("gh-aw-manifest JSON is not recognized, expected the JSON emitted by ToJSON in the lock file header: %w", err)
		}
		safeUpdateManifestLog.Printf("Extracted gh-aw-manifest: version=%d secrets=%d actions=%d",
			m.Version, len(m.Secrets), len(m.Actions))
		return &m, nil
	}
	return nil, nil
}

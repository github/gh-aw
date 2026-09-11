package workflow

import (
	"context"
	"fmt"
	"os"
	"strings"

	actionpins "github.com/github/gh-aw/pkg/actionpins"
	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/semverutil"
)

var actionPinsLog = logger.New("workflow:action_pins")

// Type aliases — callers within pkg/workflow use these names directly.

// ActionYAMLInput is defined in pkg/actionpins; aliased here so all files in
// pkg/workflow (action_cache.go, safe_outputs_actions.go, …) can reference
// the type without an explicit import.
type ActionYAMLInput = actionpins.ActionYAMLInput

// ActionPin is the pinned GitHub Action type from pkg/actionpins.
type ActionPin = actionpins.ActionPin

// ActionPinsData is the embedded JSON structure from pkg/actionpins.
type ActionPinsData = actionpins.ActionPinsData

// ContainerPin is the pinned container image type from pkg/actionpins.
type ContainerPin = actionpins.ContainerPin

// SHAResolver is the interface for resolving a GitHub Action's commit SHA for a given version tag.
// It aliases actionpins.SHAResolver so pkg/workflow files can reference it without a separate import.
type SHAResolver = actionpins.SHAResolver

// --------------------------------------------------------------------------
// Package-private helpers used throughout pkg/workflow
// --------------------------------------------------------------------------

// formatActionReference formats an action reference with repo, SHA, and version.
func formatActionReference(repo, sha, version string) string {
	return actionpins.FormatPinnedActionReference(repo, sha, version)
}

// formatActionCacheKey generates a cache key for action resolution.
func formatActionCacheKey(repo, version string) string {
	return actionpins.FormatCacheKey(repo, version)
}

// extractActionRepo extracts the action repository from a uses string.
func extractActionRepo(uses string) string {
	return actionpins.ExtractRepo(uses)
}

// extractActionVersion extracts the version from a uses string.
func extractActionVersion(uses string) string {
	return actionpins.ExtractVersion(uses)
}

// getActionPin returns the pinned reference for the latest version of the repo
// using only the embedded pins (no WorkflowData required).
func getActionPin(repo string) string {
	pins := actionpins.GetActionPinsByRepo(repo)
	if len(pins) == 0 {
		actionPinsLog.Printf("No embedded pins found for repo: %s", repo)
		return ""
	}
	return actionpins.FormatPinnedActionReference(repo, pins[0].SHA, pins[0].Version)
}

func getActionPinForData(repo string, data *WorkflowData) string {
	if data != nil {
		return getCachedActionPin(repo, data)
	}
	return getActionPin(repo)
}

// getActionPin returns the pinned reference for the given repo.
//
// This is the preferred call site for code running inside a Compiler method because it
// reuses the compiler's shared cache/resolver and marks cached pins as used for pruning.
//
// If the compiler has an action cache and resolver, this method will check the cache for
// any existing entry and mark it as "used" for orphan pruning. This ensures compiler-generated
// action references (e.g., actions/cache/save in notify steps) are tracked.
func (c *Compiler) getActionPin(repo string) string {
	if c.ghesArtifactCompat {
		if pin, ok := actionpins.ResolveGHESActionPin(repo); ok {
			return pin
		}
	}

	// Check the cache for any existing entry for this repo (regardless of version).
	// Compiler-generated actions don't specify versions, so prefer a cached entry only
	// when it is at least as new as the latest embedded pin.
	cache := c.GetSharedActionCache()
	resolver := c.GetSharedActionResolver()
	latestEmbedded, hasEmbedded := getLatestActionPinByRepo(repo)
	if cache != nil {
		if cacheKey, entry, found := cache.FindAnyEntryForRepo(repo); found {
			if hasEmbedded {
				cachedVersion := semverutil.ParseVersion(entry.Version)
				embeddedVersion := semverutil.ParseVersion(latestEmbedded.Version)
				if cachedVersion == nil {
					actionPinsLog.Printf("Ignoring cache entry with unparseable cached version for compiler-generated action %s: cache=%s embedded=%s",
						repo, entry.Version, latestEmbedded.Version)
					return actionpins.FormatPinnedActionReference(repo, latestEmbedded.SHA, latestEmbedded.Version)
				}
				if embeddedVersion == nil {
					actionPinsLog.Printf("Using cached version for compiler-generated action %s because embedded version is unparseable: cache=%s embedded=%s",
						repo, entry.Version, latestEmbedded.Version)
					if resolver != nil {
						resolver.MarkCacheKeyAsUsed(cacheKey)
					}
					return actionpins.FormatPinnedActionReference(repo, entry.SHA, entry.Version)
				}
				if embeddedVersion.IsNewer(cachedVersion) {
					actionPinsLog.Printf("Ignoring stale cache entry for compiler-generated action %s: cache=%s embedded=%s",
						repo, entry.Version, latestEmbedded.Version)
					return actionpins.FormatPinnedActionReference(repo, latestEmbedded.SHA, latestEmbedded.Version)
				}
				// Equal or newer cached versions intentionally fall through to the cache entry below.
			}
			// Mark this cache key as used so it won't be pruned as orphaned
			if resolver != nil {
				resolver.MarkCacheKeyAsUsed(cacheKey)
			}
			return actionpins.FormatPinnedActionReference(repo, entry.SHA, entry.Version)
		}
	}

	// Fall back to embedded pins if no suitable cache entry exists
	return getActionPin(repo)
}

// getCachedActionPinFromResolver returns the pinned action reference for repo,
// preferring dynamic resolution via resolver over the embedded pins.
// For use within pkg/workflow when only a resolver is available (no WorkflowData).
func getCachedActionPinFromResolver(repo string, resolver SHAResolver) string {
	ctx := &actionpins.PinContext{}
	if resolver != nil {
		ctx.Resolver = resolver
	}
	return actionpins.ResolveLatestActionPin(repo, ctx)
}

// --------------------------------------------------------------------------
// Package-private API — delegates to pkg/actionpins with a PinContext from WorkflowData
// --------------------------------------------------------------------------

// getLatestActionPinByRepo returns the latest ActionPin for a given repository, if any.
func getLatestActionPinByRepo(repo string) (ActionPin, bool) {
	return actionpins.GetLatestActionPinByRepo(repo)
}

// getEmbeddedContainerPin returns the pinned container image for a given image reference.
func getEmbeddedContainerPin(image string) (actionpins.ContainerPin, bool) {
	return actionpins.GetContainerPin(image)
}

// lookupContainerPin returns the ContainerPin for the given image, checking cache first
// then falling back to embedded pins. Returns false if the image is not pinned.
func lookupContainerPin(image string, cache *ActionCache) (ContainerPin, bool) {
	if cache != nil {
		if pin, ok := cache.GetContainerPin(image); ok {
			return pin, true
		}
	}
	if pin, ok := getEmbeddedContainerPin(image); ok {
		return pin, true
	}
	return ContainerPin{}, false
}

// resolveContainerImage returns the digest-pinned image reference when a cache or
// embedded container pin exists for image; otherwise it returns the original image.
// When data has ContainerPinMappings set (from aw.json container_pins), the source
// image is first redirected to the mapped replacement before pin resolution.
func resolveContainerImage(image string, data *WorkflowData) string {
	// Apply container-pin mapping from aw.json before digest resolution.
	// data.PinContext() creates a new PinContext struct each call, but always sets
	// Warnings to data.ActionPinWarnings — the same underlying map — so deduplication
	// of console messages works correctly across multiple calls on the same WorkflowData.
	if data != nil && len(data.ContainerPinMappings) > 0 {
		pinCtx := data.PinContext()
		image = actionpins.ApplyContainerPinMapping(image, pinCtx)
	}
	var cache *ActionCache
	if data != nil {
		cache = data.ActionCache
	}
	if pin, ok := lookupContainerPin(image, cache); ok && pin.PinnedImage != "" {
		return pin.PinnedImage
	}
	return image
}

// resolveMCPGatewayContainerImage returns an MCP Gateway-compatible container
// reference. MCP Gateway container fields accept image[:tag] but not digest
// references, so digest-pinned images are normalized back to their base image.
func resolveMCPGatewayContainerImage(image string, data *WorkflowData) string {
	resolved := resolveContainerImage(image, data)
	base, _, hasDigest := strings.Cut(resolved, "@")
	if hasDigest {
		return base
	}
	return resolved
}

// resolveGatewayContainerFromMappings applies the container_pins redirect from
// the provided mappings map and strips any digest component, returning an
// MCP Gateway-compatible image reference (image[:tag] only).
// When mappings is nil or the image is not present in the map, the original
// image is returned unchanged.
func resolveGatewayContainerFromMappings(image string, mappings map[string]string) string {
	if len(mappings) == 0 {
		return image
	}
	// Build a minimal PinContext with no warning tracking (deduplication occurs
	// during pre-download collection; here we only need the redirect).
	ctx := &actionpins.PinContext{ContainerMappings: mappings}
	mapped := actionpins.ApplyContainerPinMapping(image, ctx)
	base, _, hasDigest := strings.Cut(mapped, "@")
	if hasDigest {
		return base
	}
	return mapped
}

// applyContainerPinMappingFromData applies the container_pins redirect from aw.json
// if data has ContainerPinMappings set; otherwise returns image unchanged.
// Unlike resolveContainerImage this does not perform a digest lookup, making it
// suitable for runtime command generation where only the registry redirect is needed.
func applyContainerPinMappingFromData(image string, data *WorkflowData) string {
	if data == nil || len(data.ContainerPinMappings) == 0 {
		return image
	}
	return actionpins.ApplyContainerPinMapping(image, data.PinContext())
}

// getActionPinWithData returns the pinned action reference for a given action@version,
// delegating to pkg/actionpins with a PinContext built from WorkflowData.
func getActionPinWithData(actionRepo, version string, data *WorkflowData) (string, error) {
	return actionpins.ResolveActionPin(actionRepo, version, data.PinContext())
}

func resolveStrictActionPin(ctx context.Context, actionRepo, version string) (string, error) {
	return actionpins.ResolveActionPin(actionRepo, version, &actionpins.PinContext{
		Ctx:           ctx,
		StrictMode:    true,
		EnforcePinned: true,
	})
}

// getCachedActionPin returns the pinned action reference for a given repository,
// preferring the dynamic resolver from WorkflowData over the embedded pins.
func getCachedActionPin(repo string, data *WorkflowData) string {
	return actionpins.ResolveLatestActionPin(repo, data.PinContext())
}

// --------------------------------------------------------------------------
// Step-level helpers that depend on WorkflowStep (stay in pkg/workflow)
// --------------------------------------------------------------------------

// warnIfOutdatedActionVersion emits a warning to stderr when rawVersion is a semver
// action-version tag (e.g. "v3", "v4.0.0") and a strictly newer version exists in the
// embedded action pins. The check is skipped when rawVersion is already a full SHA or a
// non-semver ref (branch name, etc.), and when the latest available version is not newer.
//
// Warnings are deduplicated per repo@version pair via data.ActionPinWarnings so that the
// same step appearing in multiple places (pre-steps, steps, post-steps) only produces one
// diagnostic.
//
// Partial version tags (e.g. "v4" without minor/patch) are only flagged when the latest
// available major version is higher than the requested one; within the same major the tag
// is treated as a floating reference and no warning is emitted.
func warnIfOutdatedActionVersion(actionRepo, rawVersion, latestVersion string, data *WorkflowData) {
	if data == nil {
		return
	}

	// SHAs are already pinned to a specific commit — no version to compare.
	if gitutil.IsValidFullSHA(rawVersion) {
		return
	}
	// Only check recognised action version tags (vN, vN.M, vN.M.P).
	if !semverutil.IsActionVersionTag(rawVersion) {
		return
	}

	latestSemver := semverutil.ParseVersion(latestVersion)
	requestedSemver := semverutil.ParseVersion(rawVersion)
	if latestSemver == nil || requestedSemver == nil {
		return
	}

	// For tags without a patch component (e.g. "@v4", "@v4.1"), treat them as
	// floating references that resolve to the latest compatible patch within that
	// major version line (for major-only tags) or minor version line (for
	// major.minor tags). Only warn when the latest available major version is
	// higher — same-major newer minors/patches are not "outdated" for a floating tag.
	isPartialTag := strings.Count(strings.TrimPrefix(rawVersion, "v"), ".") < 2
	if isPartialTag {
		if latestSemver.Major <= requestedSemver.Major {
			return
		}
	} else if !latestSemver.IsNewer(requestedSemver) {
		return
	}

	// Deduplicate: only emit the warning once per repo@version within this compilation.
	cacheKey := "outdated:" + actionpins.FormatCacheKey(actionRepo, rawVersion)
	if data.ActionPinWarnings == nil {
		data.ActionPinWarnings = make(map[string]bool)
	}
	if data.ActionPinWarnings[cacheKey] {
		return
	}
	data.ActionPinWarnings[cacheKey] = true

	warningMsg := fmt.Sprintf("Action %s@%s is outdated; latest available version is %s.\n  Consider upgrading (update the version tag in your workflow file).",
		actionRepo, rawVersion, latestVersion)
	fmt.Fprintln(os.Stderr, console.FormatWarningMessage(warningMsg))
	actionPinsLog.Printf("Outdated action version detected: %s@%s (latest: %s)", actionRepo, rawVersion, latestVersion)
}

// applyActionPinToTypedStep applies SHA pinning to a WorkflowStep if it uses an action.
// Returns a modified copy of the step with pinned references.
// If the step doesn't use an action or the action is not pinned, returns the original step.
// Returns an error if the step uses an unversioned action reference with no available pin,
// because emitting such a reference would produce invalid GitHub Actions workflow syntax.
// Local action refs (./...) and Docker image refs (docker://...) are always passed through as-is.
func applyActionPinToTypedStep(step *WorkflowStep, data *WorkflowData) (*WorkflowStep, error) {
	if step == nil || !step.IsUsesStep() {
		return step, nil
	}

	// Local action references (./...) and Docker image references (docker://...)
	// are valid GitHub Actions syntax that cannot and should not be pinned — emit as-is.
	if strings.HasPrefix(step.Uses, "./") || strings.HasPrefix(step.Uses, "docker://") {
		return step, nil
	}

	actionRepo := extractActionRepo(step.Uses)
	if actionRepo == "" {
		return step, nil
	}

	version := extractActionVersion(step.Uses)
	if version == "" {
		pin := getCachedActionPin(actionRepo, data)
		if pin == "" {
			return nil, fmt.Errorf("unversioned action %q has no available pin; add a @ref (e.g. @v1) or include it in action-pins.json", actionRepo)
		}

		actionPinsLog.Printf("Pinned action: %s (no ref) -> %s", actionRepo, pin)
		result := step.Clone()
		result.Uses = pin
		return result, nil
	}

	// Strip the comment suffix before checking if it's already a SHA.
	// Uses strings like "repo@sha # version" are treated as already-pinned.
	rawVersion, _, _ := strings.Cut(version, " ")

	// Warn if the requested version is older than the latest available in embedded pins.
	if latestPin, hasLatest := getLatestActionPinByRepo(actionRepo); hasLatest {
		warnIfOutdatedActionVersion(actionRepo, rawVersion, latestPin.Version, data)
	}

	pinnedRef, err := getActionPinWithData(actionRepo, rawVersion, data)
	if err != nil || pinnedRef == "" {
		actionPinsLog.Printf("Skipping pin for %s@%s: no pin available", actionRepo, rawVersion)
		return step, nil
	}

	actionPinsLog.Printf("Pinned action: %s@%s -> %s", actionRepo, rawVersion, pinnedRef)
	result := step.Clone()
	result.Uses = pinnedRef
	return result, nil
}

// applyActionPinsToTypedSteps applies SHA pinning to a slice of typed WorkflowStep pointers.
// Returns a new slice with pinned references, or an error if any step has an unversioned
// action reference with no available pin.
func applyActionPinsToTypedSteps(steps []*WorkflowStep, data *WorkflowData) ([]*WorkflowStep, error) {
	if steps == nil {
		return nil, nil
	}

	result := make([]*WorkflowStep, 0, len(steps))
	for _, step := range steps {
		if step == nil {
			result = append(result, nil)
			continue
		}
		pinned, err := applyActionPinToTypedStep(step, data)
		if err != nil {
			return nil, err
		}
		result = append(result, pinned)
	}
	return result, nil
}

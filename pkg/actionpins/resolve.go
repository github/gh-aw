package actionpins

import (
	"cmp"
	"context"
	"fmt"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/semverutil"
)

var ghesArtifactPins = map[string]ActionPin{
	"actions/upload-artifact": {
		Repo:    "actions/upload-artifact",
		Version: "v3.2.2",
		SHA:     "c6a366c94c3e0affe28c06c8df20a878f24da3cf",
	},
	"actions/download-artifact": {
		Repo:    "actions/download-artifact",
		Version: "v3.1.0",
		SHA:     "a9bc5e6ef2cb54c177f32aa5726adaa15e7e2d59",
	},
}

// recordPinResolutionFailure silently records an unresolved action-ref pinning event
// to the audit callback (ctx.RecordResolutionFailure), if one is configured.
// If ctx is nil or ctx.RecordResolutionFailure is nil, the function returns early without recording.
func recordPinResolutionFailure(ctx *PinContext, actionRepo, version string, errorType ResolutionErrorType) {
	if ctx == nil || ctx.RecordResolutionFailure == nil {
		return
	}
	actionPinsLog.Printf("Recording pin resolution failure: repo=%s version=%s error_type=%s", actionRepo, version, errorType)
	ctx.RecordResolutionFailure(ResolutionFailure{
		Repo:      actionRepo,
		Ref:       version,
		ErrorType: errorType,
	})
}

// ResolveActionPin returns the pinned action reference for a given action@version.
// It consults ctx.Resolver first, then falls back to embedded pins.
// If ctx is nil, only embedded pins are consulted.
func ResolveActionPin(actionRepo, version string, ctx *PinContext) (string, error) {
	if ctx == nil {
		ctx = &PinContext{}
	}
	actionPinsLog.Printf("Resolving action pin: repo=%s, version=%s, strict_mode=%t", actionRepo, version, ctx.StrictMode)

	// Apply repository/version mapping from aw.json action_pins before resolution.
	originalRepo, originalVersion := actionRepo, version
	actionRepo, version = applyActionPinMapping(actionRepo, version, ctx)
	mapped := actionRepo != originalRepo || version != originalVersion

	if ctx.GHES && !mapped {
		if pin, ok := ghesArtifactPins[actionRepo]; ok {
			actionPinsLog.Printf("GHES mode: using %s@%s", actionRepo, pin.Version)
			return FormatPinnedActionReference(pin.Repo, pin.SHA, pin.Version), nil
		}
	}

	isAlreadySHA := gitutil.IsValidFullSHA(version)
	if pinnedRef, ok := resolveActionPinDynamically(actionRepo, version, isAlreadySHA, ctx); ok {
		return pinnedRef, nil
	}

	if pinnedRef, ok := resolveActionPinFromHardcodedPins(actionRepo, version, isAlreadySHA, ctx); ok {
		return pinnedRef, nil
	}

	if isAlreadySHA {
		actionPinsLog.Printf("SHA %s not found in hardcoded pins, returning as-is", version)
		if label := ctx.SourceVersions[actionRepo+"@"+version]; label != "" {
			return FormatPinnedActionReference(actionRepo, version, label), nil
		}
		return actionRepo + "@" + version, nil
	}

	cacheKey := FormatCacheKey(actionRepo, version)
	errorType := ResolutionErrorTypePinNotFound
	if ctx.Resolver != nil {
		errorType = ResolutionErrorTypeDynamicResolutionFailed
	}
	recordPinResolutionFailure(ctx, actionRepo, version, errorType)
	if ctx.EnforcePinned && !ctx.AllowActionRefs {
		if ctx.Resolver != nil {
			return "", fmt.Errorf("unable to pin action %s@%s: resolution failed", actionRepo, version)
		}
		return "", fmt.Errorf("unable to pin action %s@%s", actionRepo, version)
	}

	warningMsg := fmt.Sprintf("Unable to pin action %s@%s", actionRepo, version)
	if ctx.Resolver != nil {
		warningMsg += ": resolution failed"
	}
	ctx.emitOnce(cacheKey, warningMsg, console.FormatWarningMessage)
	return "", nil
}

// ResolveGHESActionPin returns the GHES-compatible pin for repo, if one is required.
func ResolveGHESActionPin(repo string) (string, bool) {
	pin, ok := ghesArtifactPins[repo]
	if !ok {
		return "", false
	}
	actionPinsLog.Printf("Resolved GHES-compatible pin for repo=%s: version=%s", repo, pin.Version)
	return FormatPinnedActionReference(pin.Repo, pin.SHA, pin.Version), true
}

func resolveActionPinDynamically(actionRepo, version string, isAlreadySHA bool, ctx *PinContext) (string, bool) {
	if ctx.Resolver == nil || isAlreadySHA {
		logDynamicResolutionSkipped(ctx.Resolver != nil, isAlreadySHA)
		return "", false
	}

	actionPinsLog.Printf("Attempting dynamic resolution for %s@%s", actionRepo, version)
	sha, err := ctx.Resolver.ResolveSHA(cmp.Or(ctx.Ctx, context.Background()), actionRepo, version)
	if err == nil && sha != "" {
		actionPinsLog.Printf("Dynamic resolution succeeded: %s@%s → %s", actionRepo, version, sha)
		resolvedVersion := findVersionBySHA(actionRepo, sha)
		result := formatPinnedActionWithResolution(actionRepo, sha, version, resolvedVersion)
		actionPinsLog.Printf("Returning pinned reference: %s", result)
		return result, true
	}

	actionPinsLog.Printf("Dynamic resolution failed for %s@%s: %v", actionRepo, version, err)
	return "", false
}

func logDynamicResolutionSkipped(hasResolver, isAlreadySHA bool) {
	if isAlreadySHA {
		actionPinsLog.Printf("Version is already a SHA, skipping dynamic resolution")
		return
	}
	if !hasResolver {
		actionPinsLog.Printf("No action resolver available, skipping dynamic resolution")
	}
}

func resolveActionPinFromHardcodedPins(actionRepo, version string, isAlreadySHA bool, ctx *PinContext) (string, bool) {
	// When the caller is targeting a non-github.com host (e.g. GHES/GHEC), the
	// dynamic resolver already failed because it queried the wrong host. Silently
	// falling back to bundled pins in that case produces unverified SHA pins and
	// masks the real problem, so skip this fallback for version→SHA resolution.
	//
	// However, when version is already a SHA (isAlreadySHA), the lookup is purely
	// SHA→version (to find a human-readable version label for the comment). That
	// operation carries no security risk regardless of host, so it is always allowed.
	if ctx.SkipHardcodedFallback && !isAlreadySHA {
		actionPinsLog.Printf("SkipHardcodedFallback set, skipping version→SHA hardcoded pin lookup for %s@%s", actionRepo, version)
		return "", false
	}

	actionPinsLog.Printf("Falling back to hardcoded pins for %s@%s", actionRepo, version)

	matchingPins := GetActionPinsByRepo(actionRepo)
	if len(matchingPins) == 0 {
		actionPinsLog.Printf("No hardcoded pins found for %s", actionRepo)
		return "", false
	}

	actionPinsLog.Printf("Found %d hardcoded pin(s) for %s", len(matchingPins), actionRepo)
	if pinnedRef, ok := resolveExactHardcodedPin(actionRepo, version, isAlreadySHA, matchingPins); ok {
		return pinnedRef, true
	}
	if isAlreadySHA || ctx.StrictMode {
		return "", false
	}
	return resolveNonStrictHardcodedPin(actionRepo, version, matchingPins, ctx), true
}

func resolveExactHardcodedPin(actionRepo, version string, isAlreadySHA bool, matchingPins []ActionPin) (string, bool) {
	for _, pin := range matchingPins {
		if pin.Version == version {
			actionPinsLog.Printf("Exact version match: requested=%s, found=%s", version, pin.Version)
			return FormatPinnedActionReference(actionRepo, pin.SHA, pin.Version), true
		}
	}
	if !isAlreadySHA {
		return "", false
	}
	for _, pin := range matchingPins {
		if pin.SHA == version {
			actionPinsLog.Printf("Exact SHA match: requested=%s, found version=%s", version, pin.Version)
			return FormatPinnedActionReference(actionRepo, pin.SHA, pin.Version), true
		}
	}
	return "", false
}

func resolveNonStrictHardcodedPin(actionRepo, version string, matchingPins []ActionPin, ctx *PinContext) string {
	selectedPin, foundCompatible := findCompatiblePin(matchingPins, version)
	if foundCompatible {
		actionPinsLog.Printf("No exact match for version %s, using highest semver-compatible version: %s", version, selectedPin.Version)
	} else {
		selectedPin = matchingPins[0]
		actionPinsLog.Printf("No exact match for version %s, no semver-compatible versions found, using highest available: %s", version, selectedPin.Version)
	}

	cacheKey := FormatCacheKey(actionRepo, version)
	warningMsg := fmt.Sprintf("Unable to resolve %s@%s dynamically, using hardcoded pin for %s@%s",
		actionRepo, version, actionRepo, selectedPin.Version)
	ctx.emitOnce(cacheKey, warningMsg, console.FormatWarningMessage)

	actionPinsLog.Printf("Using version in non-strict mode: %s@%s (requested) → %s@%s (used)",
		actionRepo, version, actionRepo, selectedPin.Version)
	return formatPinnedActionWithResolution(actionRepo, selectedPin.SHA, version, selectedPin.Version)
}

// ResolveLatestActionPin returns the pinned action reference for a given repository,
// preferring the user's cache (via ctx.Resolver) over the embedded action_pins.json.
// If ctx is nil, only embedded pins are consulted.
func ResolveLatestActionPin(repo string, ctx *PinContext) string {
	if ctx == nil {
		return getLatestActionPinReference(repo)
	}

	pins := GetActionPinsByRepo(repo)
	if len(pins) == 0 {
		actionPinsLog.Printf("No cached pins for repo=%s, falling back to embedded latest pin", repo)
		return getLatestActionPinReference(repo)
	}

	latestVersion := pins[0].Version
	pinnedRef, err := ResolveActionPin(repo, latestVersion, ctx)
	if err != nil || pinnedRef == "" {
		actionPinsLog.Printf("Resolution failed for repo=%s latest version=%s, falling back to embedded latest pin", repo, latestVersion)
		return getLatestActionPinReference(repo)
	}
	return pinnedRef
}

func findVersionBySHA(repo, sha string) string {
	for _, pin := range GetActionPinsByRepo(repo) {
		if pin.SHA == sha {
			return pin.Version
		}
	}
	return ""
}

func findCompatiblePin(pins []ActionPin, version string) (ActionPin, bool) {
	for _, pin := range pins {
		if semverutil.IsCompatible(pin.Version, version) {
			return pin, true
		}
	}
	return ActionPin{}, false
}

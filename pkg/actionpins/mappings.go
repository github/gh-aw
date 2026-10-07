package actionpins

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/console"
)

func markExactActionPinMapping(reference string) string {
	if reference == "" {
		return reference
	}
	if !strings.Contains(reference, " # ") {
		reference += " #"
	}
	return reference + " [gh-aw-exact-pin]"
}

// applyActionPinPrefix rewrites only the repository in a resolved reference.
// The source action is resolved before this is called, so mirrors need no API access.
func applyActionPinPrefix(repo, pinnedRef string, ctx *PinContext) string {
	if pinnedRef == "" || ctx == nil || len(ctx.PrefixMappings) == 0 {
		return pinnedRef
	}

	longest := ""
	for prefix := range ctx.PrefixMappings {
		if strings.HasPrefix(repo, prefix) && len(prefix) > len(longest) {
			longest = prefix
		}
	}
	if longest == "" {
		return pinnedRef
	}
	resolvedRepo, suffix, ok := strings.Cut(pinnedRef, "@")
	if !ok || resolvedRepo != repo {
		return pinnedRef
	}
	mappedRepo := ctx.PrefixMappings[longest] + strings.TrimPrefix(repo, longest)
	ctx.emitOnce("prefix:"+repo, fmt.Sprintf("Action pin mapping applied: %s → %s", repo, mappedRepo), console.FormatInfoMessage)
	return mappedRepo + "@" + suffix
}

// ApplyResolvedActionPinPrefix substitutes a mirror repository in a resolved
// action reference while retaining the pinned SHA and version comment.
func ApplyResolvedActionPinPrefix(repo, pinnedRef string, ctx *PinContext) string {
	return applyActionPinPrefix(repo, pinnedRef, ctx)
}

var containerDigestPinPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9/:_.-]*@sha256:[a-f0-9]{64}$`)

// applyActionPinMapping checks ctx.Mappings for a redirect of actionRepo@version and
// returns the (possibly updated) repo and version. An informational message is printed
// to stderr the first time each mapping is applied (deduplicated via ctx.Warnings).
func applyActionPinMapping(actionRepo, version string, ctx *PinContext) (string, string) {
	if len(ctx.Mappings) == 0 {
		return actionRepo, version
	}
	actionPinsLog.Printf("Checking action pin mapping for %s@%s (%d mapping(s) configured)", actionRepo, version, len(ctx.Mappings))

	cacheKey := FormatCacheKey(actionRepo, version)
	mapped, ok := ctx.Mappings[cacheKey]
	if !ok {
		return actionRepo, version
	}

	mappedRepo := ExtractRepo(mapped)
	mappedVersion := ExtractVersion(mapped)
	if mappedRepo == "" || mappedVersion == "" {
		actionPinsLog.Printf("Invalid action_pins mapping value %q for key %q (must be in format owner/repo@ref); skipping", mapped, cacheKey)
		return actionRepo, version
	}

	// Emit informational message once per source key.
	notifyKey := "map:" + cacheKey
	msg := fmt.Sprintf("Action pin mapping applied: %s → %s", cacheKey, mapped)
	actionPinsLog.Printf("%s", msg)
	ctx.emitOnce(notifyKey, msg, console.FormatInfoMessage)

	return mappedRepo, mappedVersion
}

// ApplyContainerPinMapping checks ctx.ContainerMappings for a redirect of image
// and returns the (possibly updated) image reference. An informational message is
// printed to stderr the first time each mapping is applied (deduplicated via
// ctx.Warnings).
func ApplyContainerPinMapping(image string, ctx *PinContext) string {
	if ctx == nil || len(ctx.ContainerMappings) == 0 {
		return image
	}
	actionPinsLog.Printf("Checking container pin mapping for image=%s (%d mapping(s) configured)", image, len(ctx.ContainerMappings))

	mapped, ok := ctx.ContainerMappings[image]
	if !ok {
		return image
	}

	if !containerDigestPinPattern.MatchString(mapped) {
		ctx.emitOnce("container-invalid:"+image,
			fmt.Sprintf("container_pins: invalid replacement value %q for key %q (must use @sha256:<64 lowercase hex characters>); mapping skipped", mapped, image),
			console.FormatWarningMessage)
		return image
	}

	// Emit informational message once per source key.
	notifyKey := "container-map:" + image
	msg := fmt.Sprintf("Container pin mapping applied: %s → %s", image, mapped)
	actionPinsLog.Printf("%s", msg)
	ctx.emitOnce(notifyKey, msg, console.FormatInfoMessage)

	return mapped
}

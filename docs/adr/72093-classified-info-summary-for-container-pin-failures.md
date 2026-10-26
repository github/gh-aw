# ADR-72093: Classified, Deduplicated Info Summary for Container Pin Resolution Failures

**Date**: 2026-07-23
**Status**: Draft
**Deciders**: dsyme, [TODO: verify other reviewers]

---

### Context

`gh aw update` refreshes container digest pins on a best-effort basis by asking Docker Buildx (`docker buildx imagetools`) or `crane` for registry metadata. When neither tool works, or the registry rejects the request, the old code printed a warning header and then one line per image with the full raw error text. In common environments (for example, no Buildx plugin, or HTTP 403 from a private registry) the same failure repeated for every image. The result was a large, alarming warning block for a non-fatal situation (issue #72081). We still need the details for troubleshooting. We also need to keep the existing behavior: the update continues, cached pins stay in place, and callers can opt in to fatal resolution errors.

### Decision

We will have `fetchContainerDigest` return a typed `containerDigestResolutionError`. It holds a short, user-facing `reason` and the full raw `details`. The reason classifies the failure as one of:

- registry access denied (HTTP 403)
- registry authentication required (HTTP 401)
- Docker Buildx and crane are unavailable (this includes a missing Buildx plugin)
- registry metadata lookup failed (the generic fallback)

`updateContainerPins` will remove duplicate reasons across all images and print a single `FormatInfoMessage` line. Per-image raw diagnostics will go only to the `cli:update_container_pins` debug logger. The main reason for this change is a better user experience for a failure that is expected and non-fatal, while keeping the diagnostics available.

### Alternatives Considered

#### Alternative 1: Keep the per-image warning block and shorten the text

We could keep one warning per image and trim the raw error strings. This change would be small, and users could still see which image failed without debug logging. We did not choose it because it still repeats the same cause N times. It also keeps the "warning" severity, which suggests something is wrong even though the update still succeeds and existing pins are kept.

#### Alternative 2: Hide the failures completely

We could log failures only at debug level and print nothing to users. This would remove all noise. We did not choose it because users would get no hint that their pins are stale or why. A missing Buildx plugin or registry auth problem is something users can fix, so a single short line is still worth printing.

#### Alternative 3: Classify failures at the call site by matching error strings

`updateContainerPins` could inspect the error strings it receives, so no new error type would be needed. We did not choose this because the classification logic belongs with the resolver strategies, which know which tool was tried and how it failed. A typed error with `errors.As` also lets tests construct failures directly.

### Consequences

#### Positive
- Users see one short, actionable line such as `Container pins could not be updated: registry access denied (HTTP 403).` instead of a block of repeated output.
- The message can tell a registry authorization failure apart from a missing resolver tool, so users know what to fix.
- The full diagnostics are kept in the error's `details` and can be seen with `DEBUG=cli:update_container_pins`.
- Non-fatal behavior, preservation of cached pins, and opt-in `failOnResolveErrors` are unchanged.

#### Negative
- The classification depends on substring matches against tool output (`403 Forbidden`, `401 Unauthorized`, `is not a docker command`, `unknown command "buildx"`). If Docker or crane change their messages, failures will quietly fall back to the generic reason.
- The default output no longer names the failing images, so users have to enable debug logging to see which ones failed.
- The severity changes from warning to info, so the message may be easier to miss when pins really are stale.

#### Neutral
- Mixed causes are reported as a sorted list of unique reasons separated by `;`, so the output is the same on every run.
- Errors that are not a `containerDigestResolutionError` (for example, from injected test dependencies) are reported as "registry metadata lookup failed".
- The CLI reference docs and a patch changeset describe the new behavior.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*

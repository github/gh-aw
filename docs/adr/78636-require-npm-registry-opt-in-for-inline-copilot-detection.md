# ADR-78636: Require npm Registry Opt-In for Inline Copilot Threat Detection

**Date**: 2026-11-17
**Status**: Draft
**Deciders**: Unknown [TODO: verify]

---

### Context

Inline Copilot threat detection ran inside the AWF firewall with a `threat-detection` engine domain set that automatically allowed `registry.npmjs.org`, so the detector could verify `package-lock.json` changes against npm metadata. The firewall is a hostname allowlist and cannot restrict traffic to read-only metadata requests. Automatic registry egress therefore gave the detection sandbox full access to the npm host, which conflicts with the repository invariant that engine default domain sets exclude package registries (issue #78352).

### Decision

We will remove `registry.npmjs.org` from the `threat-detection` domain set and its compatibility alias, so inline Copilot detection no longer reaches the npm registry by default. Workflows that want npm lockfile validation must set `features.gh-aw-detection: false` and add `node` or `registry.npmjs.org` to `network.allowed`. The compiler then inherits only the registry host (not other `node` hosts) into the detection allowlist, and `network.blocked` rules still take precedence. The detection prompt now tells the detector to report registry verification as unavailable, not as evidence of a malicious update. The primary driver is least-privilege network egress.

### Alternatives Considered

#### Alternative 1: Keep automatic npm registry access

Keep `registry.npmjs.org` in the default detection allowlist and document it as read-only. This preserves lockfile validation without configuration. We rejected it because a hostname allowlist cannot enforce read-only access, so the "read-only" claim was inaccurate and the sandbox had unnecessary egress.

#### Alternative 2: Remove npm access entirely with no opt-in

Drop registry access and the validation guidance outright. This is the simplest and most restrictive option. We rejected it because some users rely on npm metadata validation to avoid false positives on lockfile updates, and an explicit opt-in keeps that capability available.

### Consequences

#### Positive
- Default inline Copilot detection has a smaller network surface, consistent with the package-registry invariant for engine defaults.
- Registry access becomes an explicit, auditable choice in workflow frontmatter, with `network.blocked` still taking precedence.
- Detection no longer treats failed lookups as signs of a malicious update, which reduces false positives.

#### Negative
- Existing workflows that relied on automatic npm validation lose it until they add the opt-in and recompile.
- The opt-in requires disabling `gh-aw-detection`, which couples a network decision to the choice of detector.
- An opted-in allowlist still grants full host access, not read-only access.

#### Neutral
- The default external detector uses its own prompt and never provided npm metadata verification, so it is unaffected.
- Only the registry host is inherited from `node`; the other hosts in that ecosystem set remain blocked.
- Tests, docs (`docs/src/content/docs/reference/network.md`, `.github/aw/network.md`) and `ecosystem_domains.json` were updated to match.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*

# ADR-80476: Check Literal Engine Cooldowns at Compile Time and Allow Zero Days

**Date**: 2026-11-23
**Status**: Draft
**Deciders**: Unknown

---

### Context

`engine.cooldown` sets the minimum public release age for compiler-managed engine installs (default: 3 days). [ADR-73829](73829-configurable-engine-release-age-cooldown.md) enforced this at runtime: the Copilot installer looked up GitHub release publication metadata before activating a cached binary or downloading any non-default version, and strict mode rejected `engine.cooldown: 0`.

This creates two problems:

- Copilot's runtime cooldown lookup can reject Enterprise-issued runner tokens, so valid literal Copilot versions can fail to install even though the release metadata is public.
- Strict mode provides no supported way to disable the policy, so a workflow that intentionally pins a newly published release cannot opt out.

Dynamic versions (Actions expressions and tags such as `latest`) cannot be resolved at compile time and still need a runtime decision. Checksum verification and version pins are independent requirements and are unchanged.

### Decision

1. **Compile-time check for literal versions.** Literal semver engine versions, including compiler defaults, are checked during compilation against public publication times: GitHub release `published_at` for Copilot CLI, and npm `time[version]` for Claude, Codex, Pi, Gemini, and behavior-defined npm engines. A release younger than `engine.cooldown` fails compilation.
2. **Best-effort metadata.** Lookups are unauthenticated, reject redirects, time out after 5 seconds, and cap responses at 8 MiB. Publication times and lookup failures are cached in-process for five minutes; each workflow's cooldown is evaluated against the cached timestamp, not a cached pass/fail result. If metadata is unavailable or invalid, compilation continues with one warning per release per compiler instance asking for manual validation. Offline authorization and in-memory editor compilation skip lookups and emit the same warning.
3. **No runtime lookup for literal Copilot versions.** Generated installs for literal Copilot versions pass `--cooldown-days 0`. Dynamic versions keep the runtime cooldown; missing or invalid metadata fails installation.
4. **Zero disables the policy everywhere.** `engine.cooldown: 0` disables metadata lookups, warnings, and release-age enforcement, including in strict mode. Negative values remain invalid.
5. **Single JSON value for engine configuration.** Serialized engine configuration must contain exactly one JSON value; trailing data is rejected.

This revises two points of ADR-73829: runtime-only metadata verification for literal Copilot versions, and the rule that `cooldown: 0` is allowed only outside strict mode.

### Alternatives Considered

1. **Keep runtime-only enforcement and allow zero outside strict mode (ADR-73829 as written).** Not chosen: literal Copilot installs keep depending on a lookup that Enterprise-scoped tokens can fail, and strict workflows still cannot disable the policy.
2. **Give the runtime installer a dedicated public-metadata credential.** Not chosen: this adds credential handling for public data at install time, whereas the PR keeps lookups unauthenticated and moves literal checks to compile time. *Rationale inferred from the PR description; the PR does not discuss this option.*
3. **Fail compilation whenever release metadata is unavailable.** Not chosen: this would block offline and transient-failure compiles. The PR warns once per release instead and asks for manual validation. *Rationale inferred from the PR description; the PR does not discuss this option.*

### Consequences

**Positive**

- Literal Copilot installs no longer depend on runtime metadata access, so Enterprise-token rejections no longer block them.
- Release-age violations for literal versions fail at compile time, before any workflow runs.
- `engine.cooldown: 0` is a single explicit opt-out that works in strict mode.
- In-process caching limits repeated public metadata requests across workflows compiled in one run.

**Negative**

- Literal-version checks are best-effort. When metadata is unavailable, compilation succeeds with a warning and the age check depends on manual validation.
- Literal Copilot installs no longer recheck release age at runtime, so protection depends on the compile-time check having run for the locked version.
- When the compile-time lookup cannot complete (network failure, or offline, editor, or wasm compilation), the compiler only warns and the literal Copilot release installs with no runtime cooldown check; authors must verify the release age manually.
- Strict-mode workflows can now disable release-age enforcement entirely, weakening the previous guarantee for them.
- Compilation now makes outbound requests to public GitHub and npm endpoints, and the compiler gains about 280 lines of network and cache code to maintain.

# ADR-76273: Separate mini-SWE-agent Bootstrap from Runtime PyPI Access

**Date**: 2026-11-09
**Status**: Draft
**Deciders**: [TODO: verify] PR #76273 author (`copilot-swe-agent`) and triggering actor (`pelikhan`); no explicit sign-off is recorded in the PR.

---

### Context

The mini-SWE-agent behavior-defined engine declared `pypi.org` and `files.pythonhosted.org` in its unconditional engine network defaults. The agent firewall therefore allowed package-index access regardless of `network.allowed` or any Python runtime opt-in, which violates the package opt-in invariant (tracked in #76217, a new instance of #73396, and refining the guard from ADR-73534). The hosts were needed for bootstrap: the shared sample installed its hash-pinned wheels inside AWF. The change must remove the unconditional grant without breaking hash-verified installation or the isolation of agent execution. A special case in the overlap guard had hidden the problem for this engine.

### Decision

We will download the hash-locked mini-SWE-agent wheels in a trusted `pre-agent-steps` step before AWF starts, and install them offline inside AWF with `--no-index --find-links` and `--require-hashes`. We will remove PyPI hosts from engine defaults and remove the mini-SWE-agent exception from the overlap guard. Agent-time registry access becomes an explicit author choice through `network.allowed: [python]` or a declared `runtimes.python`. The primary driver is least privilege: package registry access is granted by the workflow author, not by an engine default.

### Alternatives Considered

#### Alternative 1: Keep the engine defaults and the guard exception

Leave `pypi.org` and `files.pythonhosted.org` in the unconditional defaults and keep the overlap-guard exemption. This was the lowest-effort option and keeps bootstrap working unchanged. It was rejected because every mini-SWE-agent workflow would gain PyPI access regardless of its network policy. The exception would also set a precedent that future behavior-defined engines could copy.

#### Alternative 2: Remove the registry defaults without relocating bootstrap

Delete the PyPI entries and rely on the existing in-AWF install. This is the simplest code change, but the in-AWF install needs those hosts, so the agent would fail to bootstrap. The issue explicitly warns against this shortcut. It was rejected because it breaks the engine rather than fixing the policy.

### Consequences

#### Positive
- Engine defaults no longer grant package-registry access. The overlap guard enforces the invariant without a mini-SWE-agent exception.
- Bootstrap still works: wheels are downloaded once in a trusted step and installed offline inside AWF with hash verification, so the agent needs no registry access for bootstrap.
- Regression coverage checks emitted AWF allow-lists with and without the Python runtime opt-in, and checks that wheel-lock enforcement still rejects tampered wheels.

#### Negative
- The shared `mini-swe-agent.md` import still declares `runtimes.python`, so any workflow that imports it still gets PyPI access at agent time. Registry-free callers must vendor a copy without `runtimes` (documented in the PR and in `docs/src/content/docs/reference/third-party-agent.md`). The default import path therefore keeps the broader grant, and nothing enforces the vendoring step.
- Bootstrap now depends on a trusted pre-agent step and a wheel lock that is valid only for Linux x86-64 with CPython 3.12. Other runners must provision Python 3.12 separately.
- The embedded wheel lock is pinned to `mini-swe-agent==${GH_AW_ENGINE_VERSION}`, so every engine version bump requires regenerating the lock. The staging step fails if the versions do not match.

#### Neutral
- The PR also changes Issue Monster scheduling (30 to 15 minutes) and its slot-filling rule. These operational changes are not covered by this ADR.
- [TODO: verify] Whether the shared import should drop `runtimes.python` by default, making registry access opt-in for all importers, is not decided here and is left for a follow-up.
- The change is a policy correction to the behavior-defined engine model from ADR-75773. Its scope is the mini-SWE-agent engine only; other engines are unchanged.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*

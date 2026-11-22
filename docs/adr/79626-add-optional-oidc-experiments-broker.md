# ADR-79626: Experiments Broker With OIDC-Authenticated Assignment and Reporting

**Date**: 2026-11-21
**Status**: Draft
**Deciders**: Unknown (draft generated from PR #79626; author to confirm)

---

### Context

Experiments select prompt variants and record results through git-backed storage (`experiments.storage: repo`) or Actions caches. Concurrent runs of the same workflow therefore compete for git writes to shared state, and the git backend requires `contents: write`. Teams running many concurrent executions need atomic variant allocation and centrally collected run results without adding write access to the agent. The broker service is deployment-owned and is not implemented in this repository, so the change must be opt-in and must leave `repo` and `cache` behavior unchanged.

### Decision

We will add an opt-in `experiments.storage: broker` backend. Activation calls a deployment-owned HTTPS broker, authenticated with a GitHub Actions OIDC identity whose audience is the exact configured endpoint, to receive atomic, idempotent assignments before the agent starts. A compiler-generated `report_experiments` job, with its own OIDC identity and isolated from agent output and authored steps, submits run summaries after conclusion. Broker failures fail closed with no fallback to git or cache state. The primary driver is concurrency-safe allocation and result collection without git writes, with state authority kept outside agent-controlled code.

### Alternatives Considered

#### Alternative 1: Keep the git and cache backends

The existing `repo` and `cache` backends need no new service and already work for current workflows. They were not chosen for the concurrent case because every run writes shared state through git, which serializes runs and requires `contents: write`. They remain the default and are unchanged by this PR. [TODO: verify: the authors' stated rationale for adding a service over tuning the git backend is not in the PR body.]

#### Alternative 2: Broker with fallback to git or cache on outage

A fallback would keep experiments running during a broker outage. It was not chosen because the same experiment could then be assigned by two backends, and the broker would never see those assignments. The specification explicitly rejects this: failures are reported without storage fallback. [TODO: verify: confirm this was a deliberate trade-off rather than an implementation constraint.]

#### Alternative 3: Static API token instead of OIDC identity

A shared bearer secret would be simpler to provision. It was not chosen because any holder of the secret could act as any client. The specification ties isolation to stable repository-owner and repository identities verified from GitHub-issued OIDC tokens, and it forbids using the Actions OIDC bootstrap bearer as the broker identity. [TODO: verify: confirm no static-token design was evaluated in review.]

### Consequences

#### Positive
- Assignments are committed by the broker before the agent runs, so concurrent runs get atomic, idempotent allocation without git contention, and a lost response cannot cause a second allocation.
- Broker-backed experiments add no `contents: write` and give the agent neither OIDC permission nor broker credentials.
- Dry-run selects variants locally, makes no broker requests, and never stores broker-style observations.

#### Negative
- Operators must run a separate HTTPS service and provision the `GH_AW_EXPERIMENTS_BROKER_URL` secret; no broker server is provided here.
- Broker outages or invalid responses fail activation rather than falling back to git or cache, so affected runs stop.
- Reporting is best effort. Cancellation, runner loss, or outages leave runs unreported, and a missing report must not be read as success, failure, or zero usage.
- The existing `gh aw experiments` commands, including `analyze`, read only the git backend. Broker datasets are not visible to them until a broker analysis client exists.
- The specification is experimental and may change without deprecation.

#### Neutral
- Selecting broker storage generates two OIDC-authenticated jobs: assignment in activation and an isolated report job that runs after conclusion and may wait for graders.
- The W3C-style specification, the TLA+ model under `specs/experiments-broker/`, and the configuration docs ship with the change.
- Authored workflow-level environment hooks are neutralized for the report job, and authored customization of that job is rejected at compile time.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*

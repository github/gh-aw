# ADR-73258: Offline Collection Regrading with Separately Namespaced Harbor Rewards

**Date**: 2026-05-31
**Status**: Draft
**Deciders**: pelikhan, Copilot (PR #73258)

---

### Context

gh-aw graders score agent runs from the in-memory unified session (see ADR-70130). Until now, they ran only during the workflow or on one session piped to `gh aw graders run`. Users want to apply new or updated graders to historical runs that were already downloaded with `gh aw logs`, and to include those scores in Harbor exports, without rerunning agents. Historical evidence must stay trustworthy. Recorded scores, artifacts, and provenance must not be overwritten or mixed up with later scores. Missing evidence must not become a fake reward. Bash graders are trusted code and are not yet microVM-isolated, so running them across arbitrary downloaded collections adds risk.

### Decision

We will add an offline regrading pipeline: GH-AW logs → unified sessions → existing in-memory graders → Harbor export. It is exposed as `gh aw graders run --logs-dir` and through `gh aw logs --harbor --additional-grader WORKFLOW:GRADER`, which can be repeated. Original rewards keep their `gh-aw.<id>` keys. Additional results are written under a separate `gh-aw.regraded.<id>` namespace, with their own provenance file (`gh-aw/regrading.json`) that records definition, config, script, runtime, and session digests. Key collisions, conflicting run identities, and Bash graders are rejected rather than resolved. Missing sessions and grader failures are recorded as explicit `unavailable`/`error` results and never become numeric rewards. The pipeline reads only GH-AW logs and unified sessions. It does not accept Harbor bundles as input.

### Alternatives Considered

#### Alternative 1: Overwrite or merge regraded scores into the original reward keys

Writing new results to `gh-aw.<id>` would keep the Harbor reward schema simple and make the latest score the only one shown. It was rejected because that would destroy historical evidence, mix scores from different grader versions without any signal, and make it impossible to audit what was originally recorded. The PR rejects reward-key conflicts instead of overwriting.

#### Alternative 2: Regrade by ingesting exported Harbor bundles

Harbor bundles already contain trajectories and rewards, so they could serve as the regrading input. This was rejected because Harbor export is lossy relative to unified sessions. Using it as input would also create a loop where derived artifacts become sources of truth. The PR explicitly detects native Harbor bundles and refuses them.

#### Alternative 3: Rerun agents or allow all grader kinds, including Bash, in collection mode

Rerunning agents, or allowing Bash graders, would give the most complete coverage. It was rejected because rerunning is expensive and non-deterministic. Bash graders are trusted, non-isolated code, and running them over arbitrary downloaded collections increases risk. Single-session Bash replay is unchanged.

### Consequences

#### Positive
- Historical runs can be scored with new graders offline and deterministically, without network access or agent reruns.
- Original and regraded scores are clearly separated in Harbor (`gh-aw.<id>` vs `gh-aw.regraded.<id>`), and digests record exactly which grader implementation produced each score.
- Explicit `unavailable`/`error` states prevent missing evidence from skewing reward distributions.

#### Negative
- Bash graders cannot be used in collection or Harbor regrading mode, so some graders cannot be applied retroactively.
- About 1,000 lines of new identity validation, evidence discovery, and provenance code must be maintained alongside the existing logs and Harbor paths.
- Strict rejection of identity conflicts and reward collisions can stop a whole export when the data is only partly inconsistent.

#### Neutral
- Harbor consumers will see an additional reward namespace and an `additional_policy` entry in the reward mapping.
- `ProcessedRun` gains an optional `Regrading` field that is attached only to export copies, so report and cache records stay independent.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*

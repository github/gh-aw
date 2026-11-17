# ADR-70130: Pure Unified-Session Graders with an Isolated Custom Grader Job

**Date**: 2026-10-19
**Status**: Draft
**Deciders**: pelikhan, [TODO: verify additional reviewers]

> **Partially superseded (2026-11-16):** The isolated JavaScript grader job
> remains current. The Bash compatibility decision below has been removed.

---

### Context

Graders score agentic workflow runs. Before this change, they could read from the filesystem, be Bash or file-backed (`run`), override built-in implementations, and run user-provided code or steps inside the trusted `conclusion` job. They also consumed legacy trace payloads, which gave an incomplete and inconsistent view of the evidence. Running user JavaScript and arbitrary steps in `conclusion`, which handles privileged safe-output and reporting work, widens the trusted surface. It also makes grading results depend on whatever files happen to be on disk. Some trajectory analyses are also expensive, so running every built-in by default costs time on every run.

### Decision

Built-in and JavaScript graders will be pure computations over the redacted, in-memory unified agent session. Built-in graders run in `conclusion` after session collection. Ten fast built-ins are enabled by default, and expensive trajectory analyses are opt-in. Custom `graders.<id>.script` JavaScript runs only in a separate downstream `graders` job (`needs: [activation, conclusion]`, `contents: read`). There, a Node worker gets only immutable, frozen `session`, `config`, and `helpers`. The worker runs under the Node permission model with an empty environment, a 256 MB heap cap, a 7 s timeout, and a `vm` context with `require`, `process`, `fetch`, `eval`, `Function`, and similar globals removed. Built-in overrides and user-provided conclusion steps remain rejected. AST-based `grader-file-io` lint and a custom-job threat-detection gating rule enforce parts of the contract statically. Built-in results ship with `usage/graders/` (schema v2). Custom results use a separate source-free `graders` artifact.

Repository-owned `graders.operational-value.run` Bash is restored only in the separate job. Compilation confines and freezes the `.sh` source; Bash receives the redacted session JSONL file via `$1` / `GH_AW_SESSION_FILE` and configuration JSON via `GH_AW_GRADER_CONFIG`, with empty stdin. Existing metric arrays remain compatible after this input refactor. Execution has a stripped environment, temporary working directory, 120 s deadline, and 1 MiB output cap. Compiler validation and default `grader-shell-file-write` lint share Bash AST write detection. Shell is trusted repository code, not a filesystem/network sandbox. MicroVM isolation is future work.

### Alternatives Considered

#### Alternative 1: Keep custom graders in `conclusion` with tighter sandboxing

We could keep running custom JavaScript inside `conclusion` and only add a `vm`/permission sandbox there. That avoids an extra job and a second artifact. We rejected it because user-provided code would still run in the trusted job that holds safe-output privileges. Any sandbox escape would then compromise the most sensitive part of the pipeline. Job-level isolation adds a second boundary that does not depend on the sandbox alone.

#### Alternative 2: Keep legacy evaluator inputs and unrestricted evidence sources

Legacy stdin requests and live repository/API evidence remain rejected. Repository-owned Bash can be retained after migrating to the published session file and removing writes and live fetches. It runs only in the separate job, preserving conclusion's boundary without claiming the stronger restrictions of pure JavaScript. Arbitrary shell safety still requires a future execution-isolation boundary.

#### Alternative 3: Enable all built-in graders by default

Running every trajectory analysis by default would give the most coverage without configuration. We rejected it because the expensive analyses add cost to every workflow run. Only ten fast built-ins are on by default; the rest are opt-in.

### Consequences

#### Positive
- No user-provided scripts or steps run in the trusted `conclusion` job, which shrinks its attack surface.
- Pure graders are deterministic functions of the session. The Bash authoring contract requires the same recorded evidence, and replay uses the same compatibility runtime as the separate job.
- Restrictions are enforced at several layers: the compiler rejects disallowed forms, AST lint checks file I/O, the job has restricted permissions, and the runtime has a Node permission model, `vm` sandbox, and memory/time limits.
- Shared statistics, correlated tool-call lookups, and event indexes let graders reuse precomputed views instead of re-parsing the session.

#### Negative
- This is a breaking change. Legacy evaluator input wrappers, built-in overrides, and user conclusion steps are rejected. Bash evaluators must migrate to session-file input and remove writes. Grader artifacts move to schema v2 and a new layout, requiring recompilation.
- Workflows with custom graders gain an extra `graders` job and artifact. That adds runner time, and audit, logs, cached-report refresh, and experiments must merge the two result sources.
- JavaScript cannot read files, call the network, or load modules. Bash requires a trusted-code boundary until future microVM isolation; finite static checks are not a sandbox.
- Sandbox tuning (256 MB heap, 7 s timeout) may be too tight for some legitimate graders. [TODO: verify whether limits are configurable]

#### Neutral
- The JavaScript `grader-file-io` lint rule relies on the existing opt-in `--javascript-module` parser. Without it, the rule reports unchecked coverage instead of failing.
- Custom agent-dependent jobs, including both TruffleHog jobs, were hardened to require successful threat detection under a new default AW rule.
- Schemas, docs (graders specification, trace-graders, artifacts, audit, CLI), and skills were updated to describe the new contract.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*

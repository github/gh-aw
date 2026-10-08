# ADR-66748: Expose Approved Repository Commands as Locked Tasks via a Loopback MCP Executor

**Date**: 2026-10-07
**Status**: Draft
**Deciders**: pelikhan [TODO: verify full decider list]

---

### Context

Agentic workflows frequently need to run a small set of approved repository commands (build, format, test) but granting the agent a general shell tool gives it unrestricted execution of arbitrary command lines. The existing alternative in this codebase was to hardcode engine-specific profiles (for example a Go-specific command set baked into an engine), which does not generalize across repositories or engines and cannot be shared through workflow imports. The Copilot SDK and the bundled Pi driver both support native MCP tools, so there is an existing transport that can mediate command execution without exposing a shell. Any solution must work inside the AWF Docker runtime, must not leak workflow credentials into the child process, and must bound runaway output, runtime and concurrency.

### Decision

We will add an experimental `tools.locked-tasks` configuration in which the workflow author declares named tasks with a fixed command and fixed arguments, and we will execute them through a shared, authenticated loopback MCP server (`locked-tasks.run_task`) started inside AWF. The model supplies only a task name — never a command line or arguments — so the set of executable commands is fully determined at compile time from the workflow frontmatter. Copilot SDK and Pi consume the executor as a native MCP tool, with the Copilot SDK path verifying and restricting its concrete native tool catalog before inference. The primary driver is safety: reducing agent command execution from "arbitrary shell" to "a closed, author-approved allowlist" while keeping the capability engine-agnostic and shareable via imports.

### Alternatives Considered

#### Alternative 1: Extend the existing engine-specific hardcoded command profile

The repository already had the precedent of baking a Go-oriented command set into an engine, so broadening that list would have been the smallest change and required no new MCP server, no new schema surface (`pkg/parser/schemas/main_workflow_schema.json` +23) and no new runtime (`actions/setup/js/tasks_runtime.cjs`, `tasks_process.cjs`, `tasks_mcp_server.cjs`). It was rejected because the allowlist would be owned by the compiler rather than the workflow, would not extend to non-Go repositories, could not be merged or disabled through shared-workflow imports, and would have to be re-implemented per engine.

#### Alternative 2: Expose a general shell/bash tool constrained by an argument allowlist or regex filter

Most engines already ship a shell tool, so this would have avoided new runtime code entirely and allowed richer invocations. It was rejected because pattern-based filtering of free-form command strings is a well-known weak boundary — quoting, chaining, substitution and environment manipulation repeatedly defeat such filters — whereas a name-to-fixed-argv mapping has no string parsing step to subvert. It also provides no natural place to enforce the per-task output, timeout and concurrency limits implemented in `tasks_process.cjs`.

#### Alternative 3: Ship one MCP executor process per task or per engine

This would isolate tasks from one another and avoid shared-state concerns in the executor. It was rejected because the compiler already emits a single MCP configuration per engine (`pkg/workflow/mcp_gateway_config.go`, `mcp_manifest.go`), and N processes multiplies startup cost, port/credential management and cancellation/cleanup supervision for no additional isolation — the ADR explicitly does not claim the executor as an isolation boundary beyond AWF.

### Consequences

#### Positive
- The executable command set is closed and declared in the workflow; the agent can select a task but cannot compose a new command line, which is a materially stronger boundary than filtering shell strings.
- The capability is engine-agnostic at the configuration layer and shareable: imports merge task definitions atomically, deduplicate identical definitions, error on conflicts, and support explicit disabling (`pkg/parser/tasks.go`, `pkg/workflow/tasks.go`, `tools_merger.go`).
- Task execution is bounded and supervised — output size, timeout, concurrency, restricted child environment, cancellation and cleanup, and credential-free execution with read-only runtime staging.

#### Negative
- The change adds a new long-lived runtime surface (loopback MCP server, process supervisor, task catalog, config parser — roughly 650 new lines across `actions/setup/js/` and `pkg/`) that must be maintained, security-reviewed and kept in sync across two engine drivers.
- The feature carries significant preconditions — AWF Docker runtime on a standard Linux runner, a root current-repository checkout, and `tools.cli-proxy: false` — so it is unavailable to a meaningful share of existing workflows and creates a configuration cliff when any precondition is violated.
- The executor is explicitly *not* an additional isolation boundary beyond AWF; the approved commands themselves run with whatever authority AWF grants, so a task that invokes a flexible tool (a build script, a test runner executing repository code) can still reach arbitrary execution.

#### Neutral
- The capability ships as experimental in both documentation and schema, so the configuration shape and the `locked-tasks` MCP namespace may change; the earlier `tools.tasks` spelling is deliberately not retained as an alias, so any pre-release consumers must rename.
- Compiler wiring touches many existing validation and manifest paths (`compiler_validators.go`, `prompt_tool_validation.go`, `mcp_config_validation.go`, `safe_update_manifest.go`, `copilot_engine_*.go`), making the feature broadly entangled with the tool pipeline even though each individual change is small.
- A built-in Go task set is shipped alongside the generic mechanism, which preserves the previous Go-centric ergonomics while the generic path matures.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*

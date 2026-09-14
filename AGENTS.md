# GitHub Agentic Workflows (gh-aw)

GitHub Agentic Workflows (`gh-aw`) is a GitHub CLI extension that compiles markdown workflows into GitHub Actions.

## Important Note: gh-aw vs GitHub Copilot CLI

- **gh-aw** is the `gh aw` CLI extension.
- **copilot** is a separate CLI used as one possible runtime engine.
- Use `gh aw` commands for workflow authoring/compilation (`gh aw compile`, `gh aw run`, `gh aw audit`).

## Ambient Context (First Invocation)

To keep first-turn context small, only these repository root instruction files should be considered ambient:

| File | Purpose |
|---|---|
| `AGENTS.md` | Minimal global operating rules and routing |
| `SKILL.md` | Short repository capability summary |

Everything else should be loaded **lazily** through skills only when needed.

## Critical Rules (Always Applicable)

1. If you changed files, use `report_progress` to commit and push.
2. Before **intermediate** `report_progress` calls, run `make agent-report-progress-no-test` (fast, no tests). Before the **final** `report_progress`, run `make agent-report-progress` (includes `test-unit`).
   - `test-unit` is impacted-first by default (~30s target). Use `TEST_UNIT_RUN_FULL=1 make test-unit` or `make test-unit-all` when full-suite coverage is required.
   - Run `test-unit` only once per PR — at the final push, not on every intermediate save.
3. After Go changes, run `make fmt`.
4. After workflow markdown changes (`.md` under `.github/workflows/`), run `make recompile`.
5. Do not add `.lock.yml` files to `.gitignore`.
6. Never attempt to trigger a workflow run (e.g., `gh run`, `gh aw run`) as part of a Copilot cloud agent run. The token does not have the required access. Always fail with an error — do not task the user or ask them to run it manually.
7. **Large-file guard**: before reading any file with `github-mcp-server-get_file_contents`, check its size. Files larger than 20 KB must be read with targeted tools (`grep`, `glob`, `bash`, or `view` with `view_range`) instead of full-file reads. See [token-optimization.md](.github/aw/token-optimization.md) for the full technique.
8. Never ask the user to confirm the plan. Always execute the plan.
9. **Autopilot mode**: Never ask for confirmation before taking action. Never prompt the user with "Should I proceed?", "Do you want me to…?", or any equivalent. Always act immediately and autonomously.
10. **Operational-value contract**: Do not edit an enabled evaluator independently of its workflow. Any necessary evaluator change must be made through `.github/skills/operational-value-designer/SKILL.md`. Semantic changes update both at the same path and commit; evaluator-only corrections are prospective, and prior results are never rewritten. Validate changes with `.github/skills/operational-value-designer/scripts/verify-operational-value-contract-change.sh <base-ref>`; reserve `--correction` for explicit defect fixes.

## Upstream-managed workflow sources (read-only in this repo)

Workflows that declare a `source:` frontmatter entry (for example `source: githubnext/agentic-ops@<ref>`) are provenance-managed from an upstream bundle.

- Treat those workflow source files (for example `.github/workflows/agentic-token-audit.md` and `.github/workflows/agentic-token-optimizer.md`) as read-only in this repository.
- Do **not** manually edit their generated `.lock.yml` files.
- To change these workflows, use the approved update path:
  1. run `gh aw update` to refresh from source, and/or
  2. update the pinned source/version (`source: ...@...`), and/or
  3. make the change upstream first, then pull it in via `gh aw update`.

## Lazy Skill Loading Policy

Use skills only when the task requires specialized guidance. Do not pre-load every skill.

All skills are local to this repository under `.github/skills/`. **NEVER** use GitHub Agent Finder (`agentfinder.github.com`) or any other remote skill discovery service to look for skills.

When the relevant skill is not obvious, first discover candidates locally and then load only the minimal guidance needed:

- Discover skills by listing or searching `.github/skills/*/SKILL.md` in this repository.
- Use **skill fusion** after discovery: read only the specific skill sections or fragments needed instead of loading full skills broadly.

### Use these skills by intent

- Workflow create/update/debug/upgrade tasks → `.github/skills/agentic-workflows/SKILL.md`
- Core engineering conventions, validation flow, and command playbooks → `.github/skills/developer/SKILL.md`
- Code organization, file structure, WASM stubs, string patterns → `.github/skills/developer-code-organization/SKILL.md`
- Security best practices, template injection, shell script safety, supply chain → `.github/skills/developer-security/SKILL.md`
- Compiler internals, validation architecture, safe outputs, schema, MCP logs → `.github/skills/developer-internals/SKILL.md`
- Adding or extending declarative agentic engines → `.github/skills/custom-engine-implementation/SKILL.md`
- Release management, breaking CLI change rules, firewall log parsing → `.github/skills/developer-release/SKILL.md`
- Error handling design/patterns → `.github/skills/error-recovery-patterns/SKILL.md`
- GitHub MCP usage patterns → `.github/skills/github-mcp-server/SKILL.md`
- Query helpers for issues/PRs/workflows/discussions/labels → matching `.github/skills/github-*-query/SKILL.md`
- Security-focused reviews of agentic workflow changes (compile/scans/audit/optimization) → `.github/skills/review-agentic-workflows/SKILL.md`
- Doc-writing conventions → `.github/skills/documentation/SKILL.md`
- Reviewing or writing `git`/`gh`/remote operations against checkouts (per-checkout credentials, sparse/shallow monorepos, safe-outputs MCP runs without credentials) → `.github/skills/checkout-credential-review/SKILL.md`
- Authoring, validating, or debugging canvas extensions (loopback servers, actions, iframe rendering, state model, theme tokens) → `.github/skills/create-canvas/SKILL.md`

## Debugging & Logging

The Go codebase uses a namespace-based debug logger (`pkg/logger`) modelled after the [debug npm package](https://www.npmjs.com/package/debug). All debug output goes to **stderr** and is gated by the `DEBUG` environment variable.

### Enable logging for a `gh aw` run

```bash
# All namespaces (most verbose)
DEBUG=* gh aw compile workflow.md

# Only CLI-layer namespaces
DEBUG=cli:* gh aw compile workflow.md

# Specific namespaces
DEBUG=cli:run_workflow_execution,cli:retry gh aw run workflow.md

# All except one namespace
DEBUG=*,-cli:ci gh aw compile workflow.md

# Disable colors (useful when capturing logs to a file)
DEBUG_COLORS=0 DEBUG=* gh aw compile workflow.md 2>debug.log
```

### Key namespaces

| Pattern | What it covers |
|---|---|
| `cli:*` | All CLI command implementations (`pkg/cli/`) |
| `workflow:*` | Workflow compiler and related logic |
| `parser:*` | Frontmatter / markdown parsing |
| `mcp:*` | MCP gateway and server interactions |
| `agentdrain:*` | Agent drain / log mining pipeline |
| `repoutil:*` | Git/repo utility helpers |
| `*` | Everything |

To discover the exact namespace for a package, look for `logger.New(...)` at the top of the relevant `.go` file (e.g. `var log = logger.New("cli:run_workflow_execution")`).

### Adding debug logs

When adding new debug instrumentation to Go code, use `pkg/logger` — **never** `fmt.Println`, `log.Printf`, or other ad-hoc output:

```go
import "github.com/github/gh-aw/pkg/logger"

var log = logger.New("cli:my_package") // one per file/package, at package scope

func doSomething() {
    log.Debug("starting operation", "key", value)
    // ...
    log.Debug("operation complete", "result", result)
}
```

- Pick a namespace that matches the existing hierarchy (e.g. `cli:`, `workflow:`, `mcp:`).
- Pass structured key/value pairs after the message for queryable context.
- All output is gated by `DEBUG` at runtime — no user-visible noise in normal operation.

### GitHub Actions debug runs

When `ACTIONS_RUNNER_DEBUG=true` is set (enabled automatically on re-run with debug logging in the GitHub UI), all loggers are activated — equivalent to `DEBUG=*`. No extra configuration is needed.

### Output format

Each line shows: **namespace** (coloured in the terminal), **message**, and **+elapsed** time since the previous log in that namespace.

```
cli:run_workflow_execution Starting run +0ms
cli:retry Retrying request attempt=2 +125ms
cli:run_workflow_execution Run complete +2.5s
```

## Why this file is intentionally short

This file is loaded at first invocation and affects every task. Keep it concise and move detailed or domain-specific guidance into skills so that context is fetched only when relevant.

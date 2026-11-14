# GitHub Agentic Workflows (gh-aw)

GitHub Agentic Workflows (`gh-aw`) is a GitHub CLI extension that compiles markdown workflows into GitHub Actions.

## CLI boundary

- **gh-aw** is the `gh aw` CLI extension.
- **copilot** is a separate CLI used as one possible runtime engine.
- Use `gh aw` commands for workflow authoring/compilation (`gh aw compile`, `gh aw run`, `gh aw audit`).

## Ambient context

Load only `AGENTS.md` (rules/routing) and `SKILL.md` (capabilities) initially.
Load task-specific guidance lazily through local skills.

## Critical Rules (Always Applicable)

1. If you changed files, use `report_progress` to commit and push if available; otherwise, use Git directly after the same validation gates.
2. Before **intermediate** `report_progress` calls, run `make agent-report-progress-no-test` (fast, no tests). Before the **final** `report_progress`, run `make agent-report-progress` (includes `test-unit`).
   - `test-unit` is impacted-first by default (~30s target). Use `TEST_UNIT_RUN_FULL=1 make test-unit` or `make test-unit-all` when full-suite coverage is required.
   - Run `test-unit` only once per PR — at the final push, not on every intermediate save.
3. After Go changes, run `make fmt`.
4. After workflow markdown changes (`.md` under `.github/workflows/`), run `make recompile`.
5. Do not add `.lock.yml` files to `.gitignore`.
6. Copilot cloud agents may dispatch a workflow only when the user explicitly requests a dry-run test and the exact published source and lock revision has passed the review, compilation, and live-execution gates in `.github/aw/debug-agentic-workflow.md`. Compile with `--dry-run` and dispatch that reviewed revision; compilation alone does not start a run, and dispatch alone does not enable dry-run protections. Dry-run suppresses compiler-managed mutations, not arbitrary script effects. Non-dry-run dispatch remains prohibited. On an authorization error, stop and report it; never retry with different credentials, bypass protections, or ask the user to run it manually.
7. **Large-file guard**: before reading any file with `github-mcp-server-get_file_contents`, check its size. Files larger than 20 KB must be read with targeted tools (`grep`, `glob`, `bash`, or `view` with `view_range`) instead of full-file reads. See [token-optimization.md](.github/aw/token-optimization.md) for the full technique.
8. Execute plans without confirmation. In autopilot, act autonomously; never ask "Should I proceed?" or equivalent.
9. **Instruction packing**: Copilot CLI prompt-file queries may expose only the first **8,000 UTF-8 bytes**, including metadata. Keep objective, mandatory constraints, task routes, verification and stop conditions within **7,500 bytes**. Put examples/detail afterward with early links. Use short, explicit imperatives; preserve semantics, safety and failure behavior. Follow `.github/skills/prompt-token-efficiency/SKILL.md`; run `make check-instruction-prefixes` for maintained entry points.
10. **Grading contract**: Follow `.github/skills/operational-value-designer/SKILL.md`. Built-ins compute over the in-memory unified session in conclusion; custom `graders.<distinct-id>.script` JavaScript and repository-owned `graders.operational-value.run` Bash run only in the separate graders job. Bash reads session JSONL from `$1` / `GH_AW_SESSION_FILE`, config JSON from `GH_AW_GRADER_CONFIG`, and returns legacy metric arrays; detected file writes are rejected. Shell is trusted code, not yet microVM-isolated. Never add user code or steps to conclusion. Adopt workflow semantics and grader acceptance rules together at the same workflow path and commit; corrections are prospective and prior results are never rewritten.

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

### Skill routes

Paths below are relative to `.github/skills/`; load only the matching `SKILL.md`.

| Task | Skill directory |
|---|---|
| Workflow create/update/debug/upgrade | `agentic-workflows` |
| Engineering conventions and validation commands | `developer` |
| Code organization, WASM stubs, string patterns | `developer-code-organization` |
| Template injection, shell safety, supply chain | `developer-security` |
| Compiler, validation, safe outputs, schemas, MCP logs | `developer-internals` |
| Declarative engines | `custom-engine-implementation` |
| Releases, breaking CLI changes, firewall logs | `developer-release` |
| Error handling/recovery | `error-recovery-patterns` |
| GitHub MCP usage | `github-mcp-server` |
| Issue/PR/workflow/discussion/label queries | matching `github-*-query` |
| Workflow review: compile/scans/audit/optimization | `review-agentic-workflows` |
| Documentation | `documentation` |
| Issue/comment/review Markdown | `issue-markdown` |
| Docs-site layout, spacing, colour, navigation | `docs-design` |
| Cracktro talk deck (`docs/public/cracktro/`) | `cracktro` |
| Git/gh checkout operations: credentials, sparse/shallow, credential-free MCP | `checkout-credential-review` |
| Canvas extensions: actions, inputs, rendering, state, themes | `create-canvas` |
| Instruction/skill wording and 8,000-byte prefix packing | `prompt-token-efficiency` |

## Debugging and logging

Use `pkg/logger`, never `fmt.Println`, `log.Printf` or ad hoc debug output.
Declare a package-scope `logger.New("cli:my_package")`; call
`log.Debug("operation", "key", value)` with structured key/value pairs.
Match existing namespaces: `cli:`, `workflow:`, `parser:`, `mcp:`, `agentdrain:`,
`repoutil:`. Find the exact namespace in the file's `logger.New(...)`.

Output goes to stderr, gated by `DEBUG`, as namespace, message and elapsed time.
Use `DEBUG=*` for all, `DEBUG=cli:*` for CLI, comma-separated namespaces for a
subset, or `DEBUG=*,-cli:ci` to exclude one. Set `DEBUG_COLORS=0` when capturing.
`ACTIONS_RUNNER_DEBUG=true` enables all loggers automatically.

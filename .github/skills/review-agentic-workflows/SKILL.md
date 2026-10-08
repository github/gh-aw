---
name: review-agentic-workflows
description: Review agentic workflow changes for correctness, security posture, and optimization opportunities with compile, validation, and audit evidence.
---

# Review Agentic Workflows

Use this skill when asked to review `.github/workflows/*.md` agentic workflows or their generated `.lock.yml` outputs.
Reference workflow authoring skill guidance at: https://raw.githubusercontent.com/github/gh-aw/main/.github/skills/agentic-workflows/SKILL.md

## Goals

1. Produce a security-first review of workflow changes.
2. Compile workflows with validation and security scanners.
3. Flag suspicious changes that weaken protections.
4. Use run history (`logs`/`audit`) when available to find optimization opportunities.

## Agent ownership

The agent performs setup, source and runtime-dependency inspection, independent
review coordination when required, compilation/scanners, evidence collection,
finding resolution and cleanup. Do not give the user a technical preflight
checklist to execute. Report concrete blockers and request only authorization
or decisions the agent cannot supply. For debugging and live-test gates, follow
the [shared security-review guidance](../../aw/debug-security-review.md);
the agent prepares the evidence for human validation rather than asking the
user to conduct the review.

The user may explicitly authorize live debugging for the current session rather
than approve each run. Record the grant and agreed scope/bounds; reuse it for
in-scope revisions only after the agent repeats the required technical review.
Any compiler security warning invalidates session approval immediately, even if
later fixed: resolve it, re-review, and obtain fresh explicit authorization.
Follow the shared [session authorization rules](../../aw/debug-security-review.md#session-authorization);
a request to update this skill is not itself a session-wide execution grant.

Workflow registration/activation and secret presence, validity, or expiry are
runtime readiness checks, not pre-dispatch review gates. Do not require workflow
or secret inventories, organization-admin access, or proof of usable credentials
before an otherwise authorized run. Dispatch and the workflow's startup/authentication
checks establish readiness; report their actual failures without automatic retries
or enabling workflows. Continue to review declared credential flows, authorized
destinations, permissions, and redaction without retrieving secret values. Missing
readiness metadata alone is not a security finding or an UNKNOWN safety verdict.

## Self-contained setup (do not assume environment is ready)

### Step 0) Verify CLI availability

Run from the repository root:

```bash
if gh aw --help >/dev/null 2>&1; then
  echo "gh aw is installed"
else
  if [ -f ./install-gh-aw.sh ]; then
    echo "gh aw is missing. The agent must run the install step before continuing:"
    echo "  bash ./install-gh-aw.sh"
    echo "The agent must then verify:"
    echo "  gh aw --help"
  else
    echo "gh aw is missing and ./install-gh-aw.sh is not present in this checkout."
  fi
  return 1 2>/dev/null || exit 1
fi
```

## Review workflow

### 1) Scope the review

Run this scope check in the review step:

```bash
BASE_REF="${BASE_REF:-origin/main}"
if git rev-parse --verify "$BASE_REF" >/dev/null 2>&1; then
  git diff --name-only "$BASE_REF...HEAD" -- .github/workflows/
else
  git diff --name-only -- .github/workflows/
fi
```

If source `.md` files changed, treat generated `.lock.yml` drift as part of the review.

### 2) Compile with validation + security tools

For changed workflows, run strict compilation with validators:

```bash
gh aw compile --strict --actionlint --zizmor --poutine --runner-guard --yamllint --shellcheck
```

If `gh aw` extension is unavailable but local binary exists:

```bash
./gh-aw compile --strict --actionlint --zizmor --poutine --runner-guard --yamllint --shellcheck
```

For debug/dry-run reviews, the agent runs `gh aw compile WORKFLOW --dry-run`,
adding available scanners as required by the shared guidance. Do not combine
`--dry-run` with `--no-emit`: emitted locks are part of the evidence. A
disposable checkout is optional. Running in the current checkout and reverting
only compiler-generated changes is permitted after snapshotting all affected
files, preserving diagnostic outputs, and checking for concurrent edits.
Restore pre-existing user changes exactly; never use a broad worktree reset.
See the [snapshot/restore rules](../../aw/debug-security-review.md#compile-only-validation).

Fail review on compilation errors or High/Critical security findings unless explicitly justified.

### 3) Enforce security best practices

Require and verify:

- least-privilege `permissions:` (no `write-all` without explicit rationale)
- pinned third-party actions by full commit SHA
- safe handling of untrusted GitHub event data (no direct template injection into shell)
- explicit `safe-outputs` limits (`max`, constrained event/action sets)
- no broadening of network/tool access without justification
- no integrity downgrades (for example lower `min-integrity`)

### 4) Detect suspicious weakening changes

Treat these as suspicious until proven safe:

- permission expansion (especially new `write` scopes or global writes)
- relaxed security controls (`strict: false`, reduced guardrails, disabled scans)
- larger write blast radius (`safe-outputs` limits removed or sharply increased)
- reduced provenance controls (unpinning actions, mutable refs)
- wider external access (new unrestricted network domains/ecosystems)
- prompt or script edits that reintroduce command/template injection risk

Use targeted diffs and call out before/after impact.

### 5) Audit history and optimize (when run data exists)

If workflow run IDs/URLs are available, audit them:

```bash
gh aw audit <run-id-or-url>
gh aw logs --start-date -14d --workflow-name <workflow-name>
```

Look for optimization opportunities:

- high token/cost usage
- repeated retries/tool failures
- long-running steps or bottleneck jobs
- unnecessary MCP/tool invocations
- firewall denials causing retries or wasted turns

Recommend minimal, safe optimizations that keep or improve security posture.

## Review output contract

Always provide a short user-visible result sentence for each security review and
dry-run attempt, including failed, blocked, or unavailable checks. State the
artifact/scope, outcome, what was actually checked, and any material finding or
coverage gap. Do not leave results only in logs, artifacts, or subagent replies.
Keep security-review and dry-run results separate; neither implies live execution
or authorization. Follow the shared [review result](../../aw/debug-security-review.md#user-visible-result)
and [dry-run result](../../aw/debug-agentic-workflow.md#user-visible-dry-run-result) rules.

Return findings in three sections:

1. **Security regressions (must-fix)** — high-confidence weakening changes.
2. **Validation/scanner results** — compile and tool outcomes.
3. **Optimization opportunities** — optional improvements backed by logs/audit evidence.

Each finding should include severity, file(s), rationale, and a concrete remediation direction.

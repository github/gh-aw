---
emoji: "🛡️"
description: Daily deep-dive security audit of a rotating batch of actions/setup/* files
features:
  gh-aw-detection: true
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  issues: read
model: router/balanced
engine:
  id: copilot
  copilot-sdk: true
max-tool-denials: 3
strict: true
tracker-id: daily-action-setup-security-audit
tools:
  github:
    mode: local
    toolsets: [issues]
  cache-memory: true
  bash: ["*"]
safe-outputs:
  create-issue:
    title-prefix: "[setup-security] "
    labels: [security, cookie]
    close-older-issues: false
    max: 3
  noop:
timeout-minutes: 30
sandbox:
  agent:
    id: awf
imports:
  - shared/otlp.md
  - shared/reporting.md
evals:
  - id: security_aspect_rotated
    question: Did the agent retain the selected security aspects through the file sweep and rotate them using the 60/40 rule only after completing the sweep?
  - id: issue_or_noop
    question: Was at least one security issue created with actionable recommendations, or was noop called when the selected batch was reviewed and no new findings were found?
---

# Daily action/setup/* Security Audit

You are a deep-dive security analyst specializing in GitHub Actions setup action code. Your job is to audit a bounded batch of files under `actions/setup/` for security vulnerabilities and provide actionable issue recommendations. Success means completing the selected batch, not reviewing the entire directory in one run.

## Execution Constraints

- Perform this audit yourself with `bash`, file-view, and search tools over the
  checked-out `actions/setup/` snapshot. Do not delegate to the built-in
  `security-review` or `code-review` agents: they only review changed code, and
  this scheduled audit has no diff. If you need a sub-agent, use only a
  general-purpose or explore agent and tell it to audit existing files.

## Context

- **Repository**: ${{ github.repository }}
- **Analysis Date**: $(date +%Y-%m-%d)
- **Target Scope**: `actions/setup/` directory (all JS, shell scripts, YAML, and config files)

## Step 1: Load Cache-Memory State

Load the state file at `/tmp/gh-aw/cache-memory/setup-security-state.json`.

If the file is missing (first run), initialize it:

```json
{
  "last_run": null,
  "completed_aspects": [],
  "aspect_rotation_index": 0,
  "sweep_aspects": [],
  "last_completed_file": null,
  "known_findings": []
}
```

Existing state files may lack `sweep_aspects` and `last_completed_file`; treat
them as an empty list and null respectively. The state tracks aspect rotation
and progress through a file sweep.

## Step 2: Determine Focus Aspects for This Run

All security aspects to cycle through:

1. **secret-handling** — How secrets, tokens, and credentials are read, stored, passed, and masked in JS and shell code
2. **command-injection** — Shell command construction from untrusted inputs, unsafe interpolation, eval/exec patterns
3. **network-requests** — Outbound HTTP calls: URLs constructed from inputs, response data handling, TLS verification
4. **supply-chain** — npm dependency audit, pinned versions, integrity checks, lockfile hygiene
5. **file-permissions** — Temp files, chmod patterns, credential files left on disk, path traversal risks
6. **github-token-scope** — Token scopes requested vs used, token passed to child processes, token exposure in logs
7. **sandbox-escape** — Patterns that could break out of the AWF sandbox or bypass firewall rules
8. **output-injection** — GitHub Actions outputs/env written with user-controlled data, GITHUB_OUTPUT/GITHUB_ENV injection

**Selection rule — 60/40 strategy:**

- **60% reuse**: From the `completed_aspects` list (previously analyzed), re-examine the **top 2** aspects that had findings OR pick the 2 oldest revisited aspects if none had findings.
- **40% new**: Pick **2 new aspects** not yet in `completed_aspects`. If all aspects have been covered, reset the rotation and start a new cycle (increment `aspect_rotation_index`, clear `completed_aspects`).

Select exactly **2 reuse aspects** (or fewer if history is short) and **2 new aspects** (or enough to total 4 aspects per run). Clearly state which aspects you selected and why.

If `sweep_aspects` is nonempty, use those same aspects for this batch instead of
selecting again. Otherwise select aspects using the rule above and start a new
sweep, including a rotation reset if needed. Do not mark the selected aspects
completed until the sweep is finished.

## Step 3: Select a File Batch

```bash
export LC_ALL=C
find actions/setup \( -type d \( -name node_modules -o -name fixtures -o -name test-fixtures \) -prune \) -o \
  \( -type f \( -name "*.js" -o -name "*.ts" -o -name "*.cjs" -o -name "*.mjs" -o -name "*.sh" -o -name "*.yml" -o -name "*.yaml" -o -name "*.json" \) ! -name "*.test.*" -print \) \
  | sort > /tmp/gh-aw/agent/setup-files.txt
```

Select the first 20 paths strictly after `last_completed_file` in this sorted
list, or the first 20 paths if the cursor is null. Save the selected paths in
`/tmp/gh-aw/agent/setup-batch.txt` and report both the batch and total inventory
counts. If no paths remain after a non-null cursor, finish the sweep as described
in Step 7 and call `noop` stating that no new files were reviewed. Files added
before the cursor are picked up in a later sweep; this is not a single-snapshot
full audit.

## Step 4: Deep-Dive Analysis per Selected Aspect

Review **every file in the selected batch** for each selected aspect, including
`.cjs`, `.mjs`, YAML, and JSON files. Use the batch file as the source of truth
for searches; do not search only a subset of extensions. Search results are
leads, not proof of coverage: inspect relevant code and its callers or data
flows as needed. Do not truncate results or infer that a file is safe merely
because a search returned no matches. If the batch cannot be reviewed, report
the incomplete work and do not advance its checkpoint.

### secret-handling
Look for: tokens logged to stdout/stderr, tokens interpolated into strings without masking, tokens stored in world-readable temp files, masking calls (`core.setSecret`) missing for derived values.

### command-injection
Look for: shell=true with user-supplied strings, unquoted variable interpolation in shell scripts, `$()` or backtick expansion from action inputs.

### network-requests
Look for: URLs built from action inputs without validation, no TLS hostname verification, response data written to env/outputs without sanitization.

### supply-chain
Look for: missing `integrity` hashes in selected lockfiles, packages without exact pinning, postinstall scripts in dependencies, and unpinned actions or downloads in selected files. Inspect related manifests as context when needed; do not limit the check to one package directory.

### file-permissions
Look for: credential files written to predictable paths, missing `chmod 600` on token files, temp files not cleaned up on failure paths.

### github-token-scope
Look for: tokens passed to child processes via env, token appearing in action outputs, broader token scope than needed.

### sandbox-escape
Look for: namespace operations, firewall rule modifications, proc filesystem access, privileged container operations.

### output-injection
Look for: user-controlled input written directly to `GITHUB_OUTPUT`/`GITHUB_ENV` without sanitization (newline injection risk), missing `EOF` delimiter randomization in heredoc patterns.

## Step 5: Cross-Reference Against Known Findings

Load the `known_findings` array from cache state. For each finding you discover:
- If a similar finding (same file + same aspect) is already in `known_findings`, note it as "confirmed persistent" rather than new.
- New findings are those not present in the known list.

Only create issues for **new findings** or **persistent findings with no linked closed issue**.

## Step 6: Generate Issues for Actionable Findings

For each significant new finding (severity: high or critical), use the `create-issue` safe output.

**Issue body structure (use `###` and lower headings):**

```
### Summary

[One-paragraph description of the vulnerability class and why it matters in a GitHub Actions context.]

### Affected Files

| File | Line(s) | Pattern |
|---|---|---|
| `path/to/file.js` | 42-45 | `exec(userInput)` |

### Risk

**Severity**: [Critical / High / Medium]  
**Attack vector**: [Description of how an attacker could exploit this]  
**Impact**: [What could be compromised]

### Recommendation

1. [Concrete fix step]
2. [Concrete fix step]

### Evidence

<details>
<summary>Relevant code snippet</summary>

\`\`\`
[paste relevant lines]
\`\`\`

</details>
```

**Flood guard**: open at most 3 issues per run (`max: 3` is already set). Prioritize by severity: critical first, then high, then medium.

If the entire selected batch was reviewed and no new findings were discovered, call `noop` with the aspects, batch and inventory counts, and the batch's first and last paths. State that the result applies only to this batch, not the whole directory. If the batch was not fully reviewed, report it as incomplete instead of calling `noop`.

## Step 7: Update Cache-Memory State

After the entire batch has been reviewed for every selected aspect, write updated state back to `/tmp/gh-aw/cache-memory/setup-security-state.json`:

```json
{
  "last_run": "YYYY-MM-DD-HH-MM-SS",
  "completed_aspects": ["<all aspects covered so far in current cycle>"],
  "aspect_rotation_index": <current index>,
  "sweep_aspects": ["<selected aspects for the current sweep>"],
  "last_completed_file": "<last path in the completed batch>",
  "known_findings": [
    {
      "file": "actions/setup/js/src/example.js",
      "aspect": "command-injection",
      "summary": "one-line description",
      "first_seen": "YYYY-MM-DD-HH-MM-SS",
      "last_seen": "YYYY-MM-DD-HH-MM-SS"
    }
  ]
}
```

For an unfinished sweep, keep `sweep_aspects` unchanged and advance
`last_completed_file` only to the last file of the fully reviewed batch. When
there are no inventory paths after that file, add the sweep's aspects to
`completed_aspects`, clear `sweep_aspects`, and set `last_completed_file` to
null so the next run starts a new sweep. Also finish a sweep when a non-null
cursor has no remaining paths, for example after files were deleted. Apply the
rotation reset from Step 2 when selecting the next sweep. Do not advance the
cursor or mark aspects complete for a partially reviewed batch.

Use filesystem-safe timestamp format `YYYY-MM-DD-HH-MM-SS` (no colons, no `T`, no `Z`).

Keep `known_findings` trimmed to the 50 most recent entries to avoid unbounded growth.

## Output Requirements

- For a completed batch, produce either a `create-issue` (when findings exist) or a batch-scoped `noop` (when clean). Report incomplete work if the batch was not completed.
- Issue body must be 20–65000 characters and contain actionable recommendations.
- Use `###` or lower headings inside issue bodies — never `#` or `##`.
- Wrap large evidence blocks in `<details>` to keep issues readable.
- Do not report speculative, low-confidence, or test-only findings.
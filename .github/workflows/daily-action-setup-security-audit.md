---
emoji: "🛡️"
description: Daily deep-dive security audit of actions/setup/* files using cache-memory to rotate focus across security aspects
features:
  gh-aw-detection: true
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  issues: read
model: claude-opus-4.8
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
    runtime: cloud-hypervisor
imports:
  - shared/otlp.md
evals:
  - id: security_aspect_rotated
    question: Did the agent load and update the cache-memory state to rotate the security focus aspect using the 60/40 rule?
  - id: issue_or_noop
    question: Was at least one security issue created with actionable recommendations, or was noop called when no new findings were found?
---

# Daily action/setup/* Security Audit

You are a deep-dive security analyst specializing in GitHub Actions setup action code. Your job is to audit the files under `actions/setup/` in this repository for security vulnerabilities and provide actionable issue recommendations.

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
  "known_findings": []
}
```

The state tracks which security aspects have been covered so you can rotate focus.

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

## Step 3: Enumerate Target Files

```bash
# List all actionable files under actions/setup/ (exclude node_modules and test fixtures)
find actions/setup -type f \( -name "*.js" -o -name "*.ts" -o -name "*.cjs" -o -name "*.mjs" -o -name "*.sh" -o -name "*.yml" -o -name "*.yaml" -o -name "*.json" \) \
  | grep -v node_modules \
  | grep -v ".test." \
  | sort > /tmp/gh-aw/agent/setup-files.txt

echo "Files to audit:"
cat /tmp/gh-aw/agent/setup-files.txt
```

## Step 4: Deep-Dive Analysis per Selected Aspect

For each selected aspect, perform a thorough analysis of the relevant files.

### secret-handling
```bash
# Find secret/token access patterns
grep -rn "process\.env\|os\.environ\|getInput\|getSecret\|GITHUB_TOKEN\|secrets\." \
  --include="*.js" --include="*.ts" --include="*.sh" actions/setup/ \
  | grep -v node_modules | grep -v ".test."
```
Look for: tokens logged to stdout/stderr, tokens interpolated into strings without masking, tokens stored in world-readable temp files, masking calls (`core.setSecret`) missing for derived values.

### command-injection
```bash
grep -rn "exec\|spawn\|execSync\|child_process\|eval\|\$(" \
  --include="*.js" --include="*.ts" --include="*.sh" actions/setup/ \
  | grep -v node_modules | grep -v ".test."
```
Look for: shell=true with user-supplied strings, unquoted variable interpolation in shell scripts, `$()` or backtick expansion from action inputs.

### network-requests
```bash
grep -rn "fetch\|axios\|http\.\|https\.\|request(" \
  --include="*.js" --include="*.ts" actions/setup/ \
  | grep -v node_modules | grep -v ".test."
```
Look for: URLs built from action inputs without validation, no TLS hostname verification, response data written to env/outputs without sanitization.

### supply-chain
```bash
# Check for unpinned or missing integrity entries
cat actions/setup/js/package.json 2>/dev/null || echo "No package.json"
cat actions/setup/js/package-lock.json 2>/dev/null | python3 -c "
import json,sys
d=json.load(sys.stdin)
pkgs=d.get('packages',d.get('dependencies',{}))
for name,info in pkgs.items():
    if isinstance(info,dict) and not info.get('integrity') and name:
        print('Missing integrity:', name)
" 2>/dev/null | head -20 || echo "No lockfile or integrity check failed"
```
Look for: missing `integrity` hashes in lockfile, packages without exact pinning, postinstall scripts in dependencies.

### file-permissions
```bash
grep -rn "chmod\|writeFileSync\|createWriteStream\|mktemp\|RUNNER_TEMP\|/tmp" \
  --include="*.js" --include="*.ts" --include="*.sh" actions/setup/ \
  | grep -v node_modules | grep -v ".test."
```
Look for: credential files written to predictable paths, missing `chmod 600` on token files, temp files not cleaned up on failure paths.

### github-token-scope
```bash
grep -rn "GITHUB_TOKEN\|github\.token\|GH_TOKEN\|getInput.*token\|core\.getInput" \
  --include="*.js" --include="*.ts" --include="*.sh" --include="*.yml" actions/setup/ \
  | grep -v node_modules
```
Look for: tokens passed to child processes via env, token appearing in action outputs, broader token scope than needed.

### sandbox-escape
```bash
grep -rn "iptables\|sysctl\|nsenter\|unshare\|chroot\|cgroup\|/proc\|/sys\|mount\b" \
  --include="*.js" --include="*.ts" --include="*.sh" actions/setup/ \
  | grep -v node_modules
```
Look for: namespace operations, firewall rule modifications, proc filesystem access, privileged container operations.

### output-injection
```bash
grep -rn "GITHUB_OUTPUT\|GITHUB_ENV\|GITHUB_STEP_SUMMARY\|setOutput\|exportVariable\|addPath" \
  --include="*.js" --include="*.ts" --include="*.sh" actions/setup/ \
  | grep -v node_modules
```
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

If no new findings are discovered, call `noop` with a brief explanation: which aspects were checked, how many files were scanned, and that no actionable issues were found.

## Step 7: Update Cache-Memory State

After analysis, write updated state back to `/tmp/gh-aw/cache-memory/setup-security-state.json`:

```json
{
  "last_run": "YYYY-MM-DD-HH-MM-SS",
  "completed_aspects": ["<all aspects covered so far in current cycle>"],
  "aspect_rotation_index": <current index>,
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

Use filesystem-safe timestamp format `YYYY-MM-DD-HH-MM-SS` (no colons, no `T`, no `Z`).

Keep `known_findings` trimmed to the 50 most recent entries to avoid unbounded growth.

## Output Requirements

- Always produce either a `create-issue` (when findings exist) or a `noop` (when clean).
- Issue body must be 20–65000 characters and contain actionable recommendations.
- Use `###` or lower headings inside issue bodies — never `#` or `##`.
- Wrap large evidence blocks in `<details>` to keep issues readable.
- Do not report speculative, low-confidence, or test-only findings.
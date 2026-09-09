---
emoji: "🌐"
description: Weekly audit of the domain sets in pkg/workflow/domains.go for overlap, unclear boundaries, and non-minimal engine defaults
on:
  schedule: weekly on monday around 09:00
  workflow_dispatch:
permissions:
  copilot-requests: write
  contents: read
  issues: read
engine: pi
model: copilot/auto
network: defaults
strict: true
tracker-id: weekly-network-domains-audit
tools:
  github:
    mode: gh-proxy
    toolsets: [issues]
  cache-memory: true
  bash: ["*"]
safe-outputs:
  create-issue:
    title-prefix: "[domains-audit] "
    labels: [cookie, security]
    close-older-issues: false
    max: 3
  noop:
timeout-minutes: 20
---

# Weekly Network Domain Set Audit

You are a network security analyst reviewing the allow-list domain sets that gh-aw compiles into the egress firewall of every agentic workflow. Your job is to verify that each domain set is **clearly defined**, **non-overlapping**, and — for agentic engine defaults — **as minimal as possible**.

## Context

- **Repository**: ${{ github.repository }}
- **Primary source**: `pkg/workflow/domains.go`
- **Ecosystem data**: `pkg/workflow/data/ecosystem_domains.json`
- **Tests**: `pkg/workflow/domains*_test.go`

### Threat model you are auditing against

Agents run **inside a sandbox behind the AWF gateway / MCP gateway**. Model traffic, GitHub API traffic, and MCP server traffic are proxied through the gateway (`host.docker.internal`, the gateway IP, the `cli-proxy`, and the `gh-proxy` GitHub mode). Therefore:

- An engine does **not** need direct network access to its vendor's official cloud endpoints when traffic is proxied.
- Vendor-specific endpoints (model APIs, telemetry, analytics, auth, feature-flag, and crash-reporting hosts) are the main source of over-broad defaults.
- Engine defaults must not silently grant access to package registries or ecosystem domains — those require explicit opt-in (`network: { allowed: [node] }`, `[python]`, or a `runtimes:` entry).

## Step 1: Load prior state

Read `/tmp/gh-aw/cache-memory/domains-audit-state.json`. If it is missing, initialize:

```json
{
  "last_run": null,
  "known_findings": [],
  "domains_go_sha": null
}
```

Also record the current content hash so you can tell whether anything changed since last run:

```bash
sha256sum pkg/workflow/domains.go pkg/workflow/data/ecosystem_domains.json
```

## Step 2: Enumerate every domain set

```bash
# Engine defaults and other exported/unexported domain slices
grep -n "DefaultDomains\|Domains = \[\]string{\|compoundEcosystems\|piProviderDomains\|runtimeToEcosystem\|ecosystemPriority" pkg/workflow/domains.go

# Ecosystem categories and their sizes
python3 -c "
import json
d = json.load(open('pkg/workflow/data/ecosystem_domains.json'))
for k in sorted(d):
    print(f'{k}: {len(d[k])} domains')
"
```

Build an inventory of:

1. **Engine default sets** — `CopilotDefaultDomains`, `CodexDefaultDomains`, `ClaudeDefaultDomains`, `GeminiDefaultDomains`, `PiBaseDefaultDomains`, `PiDefaultDomains`, `piProviderDomains`, plus any engine sets added since this workflow was written (do not hardcode the list — re-derive it from the file).
2. **Feature sets** — for example `PlaywrightDomains`.
3. **Ecosystem sets** — every key in `ecosystem_domains.json`, including compound ecosystems.

## Step 3: Overlap analysis

Compute pairwise intersections between all sets and report them. Pay particular attention to:

- **Engine default ∩ ecosystem** — an engine default that duplicates or overlaps an ecosystem set (especially `node`, `python`, or any package registry) is a defect: the domain should live behind the explicit opt-in only.
- **Engine default ∩ engine default** — domains shared by several engines (for example `github.com`, `raw.githubusercontent.com`, `host.docker.internal`) should be factored into a single clearly-named shared base set rather than copy-pasted, so that the shared baseline is auditable in one place.
- **Ecosystem ∩ ecosystem** — overlapping ecosystems make `GetDomainEcosystem` classification ambiguous and force reliance on `ecosystemPriority` ordering. Check that any such overlap is deliberate and covered by the priority list.
- **Wildcard shadowing** — a wildcard entry (for example `*.googleapis.com`, `*.githubusercontent.com`) that already covers sibling explicit entries in the same or another set. Report the redundant explicit entries and, more importantly, report wildcards that are broader than necessary.

Use a script rather than eyeballing:

```bash
python3 - <<'PY'
import json, re, itertools, pathlib
src = pathlib.Path('pkg/workflow/domains.go').read_text()
sets = {}
for m in re.finditer(r'var (\w+) = \[\]string\{(.*?)\n\}', src, re.S):
    name, body = m.group(1), m.group(2)
    sets[name] = sorted(set(re.findall(r'"([^"]+)"', body)))
eco = json.load(open('pkg/workflow/data/ecosystem_domains.json'))
for k, v in eco.items():
    sets['eco:' + k] = sorted(set(v))
for a, b in itertools.combinations(sorted(sets), 2):
    common = set(sets[a]) & set(sets[b])
    if common:
        print(f'{a} ∩ {b} ({len(common)}): {sorted(common)}')
PY
```

Note that exact-string intersection misses wildcard coverage — check wildcard suffix matches separately.

## Step 4: Minimality analysis of engine defaults

For **each** engine default set, classify every domain into exactly one bucket:

| Bucket | Meaning | Expected verdict |
|---|---|---|
| `gateway` | Gateway / proxy reachability (`host.docker.internal`, gateway IP) | keep |
| `github-core` | GitHub endpoints the CLI genuinely needs for repo/API access | keep if not already covered by the `github` ecosystem |
| `vendor-model-api` | Vendor model/inference API (for example `api.openai.com`, `api.anthropic.com`, `api.githubcopilot.com`) | suspicious when traffic is gateway-proxied — should be a separate opt-in set |
| `vendor-telemetry` | Telemetry, analytics, feature flags, crash reporting (for example `telemetry.*`, `statsig.*`, `sentry.io`) | suspicious — never required for correctness |
| `vendor-auth` | Vendor login / account endpoints | suspicious — should be a separate opt-in set |
| `os-packages` | OS package mirrors, PPAs, keyservers | suspicious — belongs in an ecosystem/runtime set |
| `pki` | CRL/OCSP certificate-validation hosts | should live in a single shared `defaults`/PKI set, not duplicated per engine |
| `tooling-download` | Browser/tool binary downloads (for example Playwright CDN) | belongs in the dedicated feature set, not the engine default |
| `unclear` | Cannot be justified from code or comments | suspicious — needs documentation or removal |

For each domain, cite the in-code comment justifying it (if any). Domains with **no justification comment** and no obvious gateway/GitHub role are the highest-value findings.

Cross-check whether removing a candidate domain would break an existing test:

```bash
grep -rn "<domain>" pkg/ --include="*.go" --include="*.json" | head -20
```

## Step 5: Definition clarity analysis

For each set, check:

- Does it have a doc comment stating **what it is for** and **when it applies**?
- Is the set name unambiguous about scope (engine vs feature vs ecosystem)?
- Are there duplicate entries within a single set?
- Is the set sorted, so diffs stay reviewable?
- Are ecosystem categories in `ecosystem_domains.json` documented anywhere (docs or code comments)?

## Step 6: Deduplicate against prior findings and open issues

Search existing issues before reporting:

- Query open issues with label `security` and title prefix `[domains-audit]`.
- Compare against `known_findings` in cache state (matched on set name + domain + finding category).

Only report **new** findings, or previously reported findings that have since gotten **worse** (for example a new vendor domain added to an engine default). Say so explicitly when re-reporting.

## Step 7: Report

If you find suspicious entries, create at most 3 issues, highest severity first, grouped by theme (for example one issue per engine, or one issue for cross-cutting PKI duplication).

For each public-facing report, keep the concise summary, highest-priority findings, and recommended changes visible above the fold. Use `###` headings for report sections and `####` headings for subsections, and wrap long evidence or supporting context in `<details><summary><b>View full findings</b></summary>` blocks.

Issue body structure:

```
### Summary

[What is wrong and why it weakens the egress allow-list.]

### Affected sets

| Set | Domain | Bucket | File:line |
|---|---|---|---|
| `ClaudeDefaultDomains` | `example.vendor.com` | vendor-telemetry | pkg/workflow/domains.go:123 |

### Why this is suspicious

[Explain against the gateway threat model: agents run behind the AWF/MCP gateway and do not need direct vendor cloud access. Note any overlap with ecosystem sets or other engine defaults.]

### Suggested change

1. [Move `X` into a new opt-in set named `...`.]
2. [Factor shared domains `...` into a shared base set.]

### Verification

[Which tests reference these domains and would need updating, e.g. `pkg/workflow/domains_package_registry_test.go`.]
```

If everything checks out, call `noop` with a short summary: number of sets inspected, number of overlapping pairs found (and why each is acceptable), and confirmation that engine defaults contain no unjustified vendor endpoints.

## Step 8: Persist state

Write updated state to `/tmp/gh-aw/cache-memory/domains-audit-state.json`:

```json
{
  "last_run": "YYYY-MM-DD-HH-MM-SS",
  "domains_go_sha": "<sha256 of domains.go>",
  "known_findings": [
    {
      "set": "ClaudeDefaultDomains",
      "domain": "example.vendor.com",
      "category": "vendor-telemetry",
      "first_seen": "YYYY-MM-DD-HH-MM-SS",
      "last_seen": "YYYY-MM-DD-HH-MM-SS"
    }
  ]
}
```

Use the filesystem-safe timestamp format `YYYY-MM-DD-HH-MM-SS` (no colons, no `T`, no `Z`). Trim `known_findings` to the 100 most recent entries.

## Output requirements

- Always produce either `create-issue` output or `noop`.
- Never propose removing a domain without checking the code comments and tests that reference it.
- Do not open issues for stylistic nits alone (sorting, comment wording) unless they accompany a substantive finding.
- Report facts observed in the repository; do not speculate about vendor infrastructure you cannot verify from the code.

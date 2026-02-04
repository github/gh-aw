---
description: Bundles Dependabot alerts by runtime and manifest into a parent issue, then assigns bundles to an agent
name: Dependabot Burner
on:
  schedule:
    # Weekdays at 10:15 UTC
    - cron: "15 10 * * 1-5"
  workflow_dispatch:

strict: true

timeout-minutes: 20

permissions:
  contents: read
  issues: read
  security-events: read

network: defaults

engine: copilot

imports:
  - shared/gh.md
  - shared/keep-it-short.md

safe-outputs:
  create-issue:
    expires: 2d
    title-prefix: "[Dependabot Burner] "
    max: 30
    group: true
  update-issue:
    target: "*"
    title:
    body:
    max: 5
  link-sub-issue:
    max: 50
  assign-to-agent:
    target: "*"
    max: 20
  add-comment:
    target: "*"
    max: 20
---

# Dependabot Burner

## Objective

Collect **open Dependabot alerts** for ${{ github.repository }}, bundle them by **runtime/ecosystem** and **manifest path**, publish/update a single **parent tracking issue**, and then create **one child issue per bundle** and assign each child issue to a coding agent.

## Definitions

- **Runtime/Ecosystem**: Dependabot package ecosystem (e.g., `go_modules`, `npm`, `pip`, `github_actions`, etc.).
- **Manifest**: The `dependency.manifest_path` (e.g., `go.mod`, `package-lock.json`, `.github/workflows/ci.yml`).
- **Bundle key**: `${ecosystem} :: ${manifest_path}`.

## Constraints

- Prefer **updating** an existing tracker issue if it already exists.
- Do not create more than 20 child issues per run. If there are more bundles, prioritize:
  1) Critical/High severity
  2) Fix available
  3) Most alerts in bundle

## Step 1: Fetch open Dependabot alerts

Use `safeinputs-gh` to fetch Dependabot alerts via the REST API.

- Endpoint: `GET /repos/{owner}/{repo}/dependabot/alerts`
- Filter to `state=open`
- Paginate until complete

Example `safeinputs-gh` invocation (adjust flags as needed):

- args: `api -H 'Accept: application/vnd.github+json' /repos/${{ github.repository }}/dependabot/alerts?state=open --paginate`

If there are **no open alerts**, exit successfully without emitting any safe outputs.

## Step 2: Normalize + bundle

From each alert, extract (as available):
- `html_url`
- `number` or id
- `dependency.package.name`
- `dependency.package.ecosystem`
- `dependency.manifest_path`
- `security_advisory.severity` (or equivalent)
- `security_vulnerability.vulnerable_version_range`
- `security_vulnerability.first_patched_version.identifier` (or equivalent)
- `created_at`, `updated_at`, `fixed_at`, `dismissed_at`

Bundle alerts by `${ecosystem} :: ${manifest_path}`.

For each bundle compute:
- Count of alerts
- Highest severity in the bundle
- Whether **any** alert has a clear fix version

## Step 3: Find or create the parent tracker issue

The tracker issue title should be:

- `[Dependabot Burner] Tracker`

Use `safeinputs-gh` to search open issues by title:

- args: `issue list --repo ${{ github.repository }} --state open --search 'in:title "[Dependabot Burner] Tracker"' --json number,title,url --limit 5`

Rules:
- If an open tracker exists, pick the first result as `tracker_issue_number`.
- If none exists, create it using `create_issue(...)`.

The tracker body should include:
- A short header with repository, run date, and total alerts
- A table of bundles with columns: Ecosystem, Manifest, Alerts, Max severity, Fix available, Child issue
- A section describing how child issues are structured

If updating an existing tracker, use `update_issue(issue_number=..., operation="replace", body=...)`.

## Step 4: Create child issues for bundles

For each selected bundle (max 20):

- Create a child issue with a temporary ID (`aw_` + 12 lowercase hex).
- Title format:
  - `[Dependabot Burner] ${ecosystem} ${manifest_path} (${count} alerts)`

Child issue body must include:
- Bundle key
- A bullet list of alerts, each with:
  - Dependency name
  - Severity
  - Manifest path
  - Link to the alert (`html_url`)
  - Suggested fixed version (if known)
- A short, actionable checklist for a coding agent:
  - Identify update(s)
  - Apply minimal safe changes
  - Run tests
  - Open PR and link back to this issue

Emit:
- `create_issue(temporary_id="aw_...", title="...", body="...")`

## Step 5: Link child issues under the tracker

For each child issue created:

- `link_sub_issue(parent_issue_number=<tracker_issue_number or tracker temporary id>, sub_issue_number=<child temporary id or number>)`

## Step 6: Assign each child issue to an agent

For each child issue created, assign to the Copilot coding agent:

- `assign_to_agent(issue_number=<child temporary id or number>, agent="copilot")`

## Step 7: Update tracker with child links

Ensure the tracker body includes links to the created child issues (by number/URL). If you created the tracker in this run, you can include temporary IDs in text, but prefer final URLs/numbers when available.

If you cannot resolve child issue numbers during generation, still link them as sub-issues (temporary IDs are supported), and include a placeholder in the tracker table noting that a child issue was created.

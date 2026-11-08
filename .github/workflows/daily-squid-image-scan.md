---
name: Daily Container Image Security Scan
engine:
  id: copilot
  copilot-sdk: true
description: Scan container images used by compiled workflows for vulnerabilities, updates, and rejected licenses
emoji: "🛡️"
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  packages: read
  copilot-requests: write
strict: true
network:
  allowed:
    - defaults
    - go
tools:
  cli-proxy: true
  github:
    toolsets: [issues]
  bash:
    - "cat /tmp/gh-aw/agent/image-scan/compile-output.txt"
    - "cat /tmp/gh-aw/agent/image-scan/candidate-output.txt"
    - "wc -l /tmp/gh-aw/agent/image-scan/*-output.txt"
    - "grep -nE * /tmp/gh-aw/agent/image-scan/*-output.txt"
    - "grep -oE * /tmp/gh-aw/agent/image-scan/*-output.txt | sort | uniq -c"
safe-outputs:
  create-issue:
    title-prefix: "[container-image-scan] "
    labels: [cookie, security]
    assignees: [pelikhan]
    max: 25
    deduplicate-by-title: true
    deduplicate-by-labels: [cookie, security]
  update-issue:
    target: "*"
    required-title-prefix: "[container-image-scan] "
    body: true
    status:
    max: 25
  assign-to-user:
    target: "*"
    allowed: [pelikhan]
    max: 25
  close-issue:
    target: "*"
    required-title-prefix: "[container-image-scan] Container findings for "
    required-labels: [cookie, security]
    state-reason: duplicate
    max: 25
  noop:
    report-as-issue: false
steps:
  - name: Build gh-aw from source
    run: |
      set -e
      make build
      "$GITHUB_WORKSPACE/gh-aw" --version
  - name: Scan committed container image pins
    continue-on-error: true
    run: |
      set -uo pipefail
      output_dir="/tmp/gh-aw/agent/image-scan"
      mkdir -p "$output_dir"
      "$GITHUB_WORKSPACE/gh-aw" compile --syft --grype --grant 2>&1 | tee "$output_dir/compile-output.txt" || true
  - name: Install crane (digest resolution fallback)
    continue-on-error: true
    run: |
      set -e
      go install github.com/google/go-containerregistry/cmd/crane@v0.22.1
      echo "$(go env GOPATH)/bin" >> "$GITHUB_PATH"
  - name: Refresh container image pins (best effort)
    continue-on-error: true
    shell: bash
    run: |
      set -uo pipefail
      output_dir="/tmp/gh-aw/agent/image-scan"
      mkdir -p "$output_dir"
      # A failure to resolve a digest for one image (e.g. an upstream registry
      # returning 403/rate-limited) must not replace the committed scan evidence.
      # Candidate scans retain the last-known pin for images that cannot refresh.
      "$GITHUB_WORKSPACE/gh-aw" compile --force-refresh-container-pins 2>&1 | tee "$output_dir/candidate-output.txt"
      # continue-on-error above keeps the job running even on failure; this
      # explicit check additionally surfaces a visible ::warning:: annotation
      # in the run summary so recurring resolution failures aren't missed.
      refresh_status="${PIPESTATUS[0]}"
      if [ "$refresh_status" -ne 0 ]; then
        echo "::warning::Container pin refresh failed (exit $refresh_status) for one or more images; candidates may retain last-known pins. See $output_dir/candidate-output.txt for details."
      fi
  - name: Scan refreshed candidate container image pins
    continue-on-error: true
    run: |
      set -uo pipefail
      output_dir="/tmp/gh-aw/agent/image-scan"
      mkdir -p "$output_dir"
      echo "" >> "$output_dir/candidate-output.txt"
      "$GITHUB_WORKSPACE/gh-aw" compile --syft --grype --grant 2>&1 | tee -a "$output_dir/candidate-output.txt" || true
post-steps:
  - name: Enforce vendored vulnerability and license gates
    if: always()
    run: |
      output="/tmp/gh-aw/agent/image-scan/compile-output.txt"
      if [ ! -f "$output" ]; then
        echo "::error::Scan output not found. The compile step did not produce output."
        exit 1
      fi
      # Only gate the build on findings for the vendored ghcr.io/github/gh-aw-node
      # image, whose Dockerfile lives in this repo and can be fixed here directly.
      # Upstream-owned images (e.g. node:lts-alpine, gh-aw-firewall/*, gh-aw-mcpg,
      # github-mcp-server, grafana) are tracked-only per the workflow's policy and
      # must not fail this daily scan; they are remediated via upstream pin refreshes.
      vendored_pattern='(^|[[:space:]])ghcr\.io/github/gh-aw-node(@|:)'
      if grep -qE "$vendored_pattern.*: error: \[Critical\]" "$output"; then
        echo "::error::Critical vulnerabilities detected in the vendored ghcr.io/github/gh-aw-node container image."
        exit 1
      fi
      if grep -qE "$vendored_pattern.*: (error|warning): \[High\].* \(fix: [^)]+\)" "$output"; then
        echo "::error::High vulnerabilities with available fixes detected in the vendored ghcr.io/github/gh-aw-node container image."
        exit 1
      fi
      if grep -qE "$vendored_pattern.*: error: license policy violation:" "$output"; then
        echo "::error::License policy violations detected in the vendored ghcr.io/github/gh-aw-node container image."
        exit 1
      fi
timeout-minutes: 90
evals:
  - id: container_images_scanned
    question: Did the agent analyze container images for vulnerabilities, updates, and rejected licenses?
  - id: findings_reported_or_noop
    question: Did the agent report actionable image findings, or use noop when no findings required action?
  - id: critical_burn_down_tracked
    question: Did the agent maintain one burn-down issue per image family and the index in issue 52657, mapping every finding to a fixed version or a documented exception and stating the remediation SLA?
  - id: superseded_image_findings_closed
    question: Did the agent close legacy per-tag findings only when their exact image reference was no longer used by the compiler?
  - id: cves_mapped_or_excepted
    question: Does every reported CVE identify a fixed package/image version or a documented exception with its reason and review details?
  - id: upstream_vendored_triaged
    question: Did the agent classify each finding as vendored (fixable in this repo) or upstream (owned by another repository), and avoid requesting a local code-fix PR for upstream-owned findings?
features:
  gh-aw-detection: true
---

# Daily Container Image Security Scan

Review the Syft SBOM, Grype vulnerability, and Grant license scan results in
`/tmp/gh-aw/agent/image-scan/compile-output.txt`. This scan runs before any pin
refresh and is the authoritative evidence for committed pins, security gates,
finding counts, and legacy-issue closure.

The separate `/tmp/gh-aw/agent/image-scan/candidate-output.txt` contains the
best-effort refresh and candidate scan. Compare exact image digests and label
any differences as candidate-only; never replace committed findings or infer
adoption from candidate results, including a clean candidate scan.

### Read the scan with permitted tools

The log can exceed 500 KB. Use native `grep` with line numbers to locate
`Running (syft|grype|grant)` and `syft scanned`, then native `view` with
`view_range` to read successive ranges through the end of the scan. Read every
finding, including lower severities, license results, and scanner errors; a
truncated tool response or a severity summary is not the complete evidence.
Keep using the absolute log path, without changing directories.
Read the candidate log separately with the same native tools and pagination.

These shell commands are supported by the existing allowlist: the first gives
the line count for pagination, and the second counts findings per image
reference and severity without discarding the image tag:

```bash
wc -l /tmp/gh-aw/agent/image-scan/compile-output.txt
grep -oE '^[^[:space:]]+:1:1: (error|warning|info): \[[A-Za-z]+\]' /tmp/gh-aw/agent/image-scan/compile-output.txt | sort | uniq -c
```

Do not use `cd`, `cut`, `sed`, `awk`, `python`, `python3`, `jq`, or ad hoc
scripts for analysis; they are not permitted. If a required read is unavailable
or denied, stop analysis and report the operational failure with the configured
safe outputs. Do not retry with another unapproved command, report a clean scan,
or close legacy findings without complete scan evidence.

### Analysis and reporting

1. Read `compile-output.txt`.
2. Treat [Container CVE burn-down](https://github.com/github/gh-aw/issues/52657)
     as the tracker index only. Keep its summary to one row per image family
     with the family issue link, current image references, and finding counts;
     do not duplicate the family issues' CVE details there. Assign it to
     `pelikhan` if it is unassigned.
3. Group scanned references by stable image family, never by tag or digest:
     - `ghcr.io/github/gh-aw-firewall/*` → **Firewall**;
     - `ghcr.io/github/gh-aw-mcpg` → **MCP Gateway**;
     - `ghcr.io/github/github-mcp-server` → **GitHub MCP Server**;
     - any repository containing `serena` → **Serena**;
     - `ghcr.io/github/gh-aw-node` and `node` → **Node**;
     - for other images, use the normalized repository name without its tag
      or digest as the stable family key.
4. Classify every scanned image as **vendored** or **upstream** before
     writing any remediation guidance:
     - **Vendored**: the image's Dockerfile/build config lives in this
       repository (`github/gh-aw`), so a fix (code change, dependency bump,
       or config change) can land here directly.
       `ghcr.io/github/gh-aw-node` is **vendored**: its Dockerfile is
       `actions/setup/js/Dockerfile.safe-outputs-mcp`, published by
       `.github/workflows/publish-safe-outputs-node.yml`. Do not classify it
       as upstream merely because it is derived from `node:lts-alpine`;
       the unmodified `node:lts-alpine` image itself is upstream.
     - **Upstream**: the image is built and owned by a different repository
       or a third party, e.g. `ghcr.io/github/gh-aw-firewall/*` (owned by
       `github/gh-aw-firewall`), `ghcr.io/github/gh-aw-mcpg` (owned by
       `github/gh-aw-mcpg`), or third-party images such as
       `github-mcp-server`, `grafana/mcp-grafana`, or `serena-mcp-server`.
     For these, the code-level fix cannot land in this repo; only a
     pin/digest refresh to a newer upstream release is possible here.
5. For every image family in the scan, maintain exactly one burn-down issue
     using the configured title prefix followed by `<Family> CVE burn-down`.
     Search all issues for that exact full title before writing: update the
     existing issue if found (reopen it if needed), otherwise create it. If multiple issues have
     the exact family title, use the matching tracker already linked in #52657;
     otherwise use the lowest issue number. Read every duplicate's body and
     comments, preserve unique unresolved findings and tracking links in the
     canonical issue, and report the duplicate issue links there and in #52657
     for maintainer review. Do not automatically close family duplicates:
     close-issue is restricted to legacy `Container findings for ...` issues.
     Never close the canonical tracker, #52657, or operational-failure issues.
     Never create a separate issue for an image tag, digest, component, or individual CVE.
     Assign each family issue to `pelikhan` if it is unassigned. Put the current
     scan's family summary and collapsible details in that issue, including an
     explicit clean status when it has no findings:
     - every image name, exact pinned reference, and vendored/upstream status;
     - every finding's severity, CVE ID, package, installed version, and fixed
     package version(s);
     - every rejected or unknown license and the affected package;
     - for every CVE, either identify a fixed package version and the image
     version/digest that contains it, or record an exception with the reason,
     responsible upstream/project, relevant advisory or tracking link when
     available, and a next-review date. Do not invent fixes or describe an
     exception as risk-accepted without maintainer approval;
     - actionable remediation guidance, scoped to what is actually fixable here
     (see step 10).
6. Search for open legacy issues titled `Container findings for ...`. Compare
     each issue's complete image reference (including tag and digest, when
     present) with the exact references emitted by the current compiler scan.
     Close an issue as a duplicate of its family burn-down issue only when its
     image reference is no longer scanned, and set `duplicate_of` to that
     family's tracker. Treat a tag-only issue reference as current when that
     exact repository and tag are scanned; require an exact digest match when
     the issue includes a digest. Do not close an issue based on a partial
     tag/name match or when its reference is ambiguous; do not close
     operational-failure issues. If the compiler output is missing, create the
     operational-failure issue and stop; do not infer image references or close
     findings from incomplete scan evidence.
7. If the scan step failed to produce output, create one `Container scan
     operational failure` issue assigned to `pelikhan`.
8. Update #52657 with the index of family tracker links and aggregate finding
     counts. If there are no findings and no operational errors, show the clean
     scan in the index and call `noop`.
9. Order findings in each family issue by severity, Critical first, then High, Medium,
     Low, and Unknown, so the Critical backlog is triaged first.
10. In each family issue, state the remediation SLA cadence: Critical findings
     are remediated or explicitly risk-accepted within 7 days, High within 30
     days, and every scanned image is rebuilt on a refreshed base image at least
     weekly. The daily `gh aw compile --force-refresh-container-pins` resolves
     digests for the currently configured image tags in this scan's checkout;
     it does not rebuild images, select newer release tags, or publish a pin
     refresh PR. Do not report an upstream fix as adopted until the configured
     tag/digest and compiler pin caches have been updated in a commit, affected
     workflows regenerated and committed, and those committed pins rescanned.
     A temporary checkout refresh or its regenerated candidate workflows is
     not adoption. For findings
     on **upstream** images, do not request a local code-fix PR or task an
     agent to patch the vendored image directly — the daily pin-refresh
     can pick up same-tag rebuilds, but newer release tags require an explicit
     version update. Instead,
     label the finding "Upstream — tracked only" and, when available, link to
     the corresponding issue/advisory in the owning repository so the fix is
     pursued there, not here.
11. Keep the reports factual and compact. Never omit lower-severity
     vulnerabilities.

### Output Format

- Use `###` (h3) or lower for all report headers; never use `#` or `##` inside the report body.
- Wrap long lists, tables, and detailed findings in `<details><summary><b>...</b></summary>...</details>` blocks to reduce scrolling.
- Structure reports as: overview → key metrics/issues → collapsible detail → next actions.

Use only the configured safe outputs to create, update, assign, or close issues.
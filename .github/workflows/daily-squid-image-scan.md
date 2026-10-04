---
name: Daily Container Image Security Scan
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
  bash:
    - "cat /tmp/gh-aw/agent/image-scan/compile-output.txt"
safe-outputs:
  create-issue:
    title-prefix: "[container-image-scan] "
    labels: [cookie, security]
    assignees: [pelikhan]
    max: 25
    deduplicate-by-title: true
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
      # returning 403/rate-limited) must not block the Syft/Grype/Grant scan
      # below, so this step is intentionally decoupled and best-effort: any
      # images that fail to refresh here simply keep their last-known pin.
      "$GITHUB_WORKSPACE/gh-aw" compile --force-refresh-container-pins 2>&1 | tee "$output_dir/compile-output.txt"
      # continue-on-error above keeps the job running even on failure; this
      # explicit check additionally surfaces a visible ::warning:: annotation
      # in the run summary so recurring resolution failures aren't missed.
      refresh_status="${PIPESTATUS[0]}"
      if [ "$refresh_status" -ne 0 ]; then
        echo "::warning::Container pin refresh failed (exit $refresh_status) for one or more images; continuing with last-known pins. See $output_dir/compile-output.txt for details."
      fi
  - name: Run compile with vulnerability scanners
    continue-on-error: true
    run: |
      set -uo pipefail
      output_dir="/tmp/gh-aw/agent/image-scan"
      mkdir -p "$output_dir"
      echo "" >> "$output_dir/compile-output.txt"
      "$GITHUB_WORKSPACE/gh-aw" compile --syft --grype --grant 2>&1 | tee -a "$output_dir/compile-output.txt" || true
post-steps:
  - name: Enforce critical vulnerability and license gates
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
      if grep -E "$vendored_pattern" "$output" | grep -qE ': error: \[Critical\]'; then
        echo "::error::Critical vulnerabilities detected in the vendored ghcr.io/github/gh-aw-node container image."
        exit 1
      fi
      if grep -E "$vendored_pattern" "$output" | grep -q ': error: license policy violation:'; then
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
`/tmp/gh-aw/agent/image-scan/compile-output.txt`.

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
     - **Upstream**: the image is built and owned by a different repository
       or a third party, e.g. `ghcr.io/github/gh-aw-firewall/*` (owned by
       `github/gh-aw-firewall`), `ghcr.io/github/gh-aw-mcpg` (owned by
       `github/gh-aw-mcpg`), or third-party images such as
       `github-mcp-server`, `grafana/mcp-grafana`, or `serena-mcp-server`.
     For these, the code-level fix cannot land in this repo; only a
     pin/digest refresh to a newer upstream release is possible here.
5. For every image family in the scan, maintain exactly one burn-down issue
     using the configured title prefix followed by `<Family> CVE burn-down`.
     Search for that exact full title before writing: update the existing issue
     if found (reopen it if needed), otherwise create it. Never create a
     separate issue for an image tag, digest, component, or individual CVE.
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
     weekly (this workflow runs `gh aw compile --force-refresh-container-pins`
     daily, so a pin refresh PR is the default remediation step). For findings
     on **upstream** images, do not request a local code-fix PR or task an
     agent to patch the vendored image directly — the daily pin-refresh
     already picks up upstream fixes automatically once released. Instead,
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
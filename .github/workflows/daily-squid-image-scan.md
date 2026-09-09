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
    max: 1
    deduplicate-by-title: true
  update-issue:
    target: "52657"
    body: true
    max: 1
  assign-to-user:
    target: "52657"
    allowed: [pelikhan]
    max: 1
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
    question: When Critical or High findings existed, did the agent create a consolidated burn-down issue linking the per-image issues and stating the remediation SLA?
  - id: upstream_vendored_triaged
    question: Did the agent classify each finding as vendored (fixable in this repo) or upstream (owned by another repository), and avoid requesting a local code-fix PR for upstream-owned findings?
features:
  gh-aw-detection: true
sandbox:
  agent:
    runtime: cloud-hypervisor
---

# Daily Container Image Security Scan

Review the Syft SBOM, Grype vulnerability, and Grant license scan results in
`/tmp/gh-aw/agent/image-scan/compile-output.txt`.

1. Read `compile-output.txt`.
2. Treat [Container CVE burn-down](https://github.com/github/gh-aw/issues/52657)
     as the single tracker. Assign it to `pelikhan` if it is unassigned.
3. Classify every scanned image as **vendored** or **upstream** before
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
4. Do not create per-image finding issues. Update #52657 with the current scan's
     summary table and collapsible per-image details, including:
     - the image name, pinned reference, and its vendored/upstream classification;
     - every vulnerability with severity, CVE ID, package, installed version, and
     fixed versions;
     - every rejected or unknown license and the affected package;
     - actionable remediation guidance, scoped to what is actually fixable here
     (see step 9).
5. Close every open issue titled `Container findings for ...` as a duplicate of
     #52657. Include a short closure comment linking to #52657. Do not close
     operational-failure issues.
6. If the scan step failed to produce output, create one `Container scan
     operational failure` issue assigned to `pelikhan`.
7. If there are no findings and no operational errors, update #52657 to show
     the clean scan, then call `noop`.
8. Order the findings in #52657 by severity, Critical first, then High, Medium,
     Low, and Unknown, so the Critical backlog is triaged first.
9. In #52657, state the remediation SLA cadence: Critical findings
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
10. Keep the report factual and compact. Never omit lower-severity
     vulnerabilities.

### Output Format

- Use `###` (h3) or lower for all report headers; never use `#` or `##` inside the report body.
- Wrap long lists, tables, and detailed findings in `<details><summary><b>...</b></summary>...</details>` blocks to reduce scrolling.
- Structure reports as: overview → key metrics/issues → collapsible detail → next actions.

The configured `create-issue` safe output is the only allowed write operation.
---
jobs:
  trufflehog_scan:
    runs-on: ubuntu-latest
    needs: [agent, detection]
    if: always() && needs.agent.result != 'skipped' && needs.detection.result != 'skipped'
    permissions:
      contents: read
    outputs:
      secrets_found: ${{ steps.evaluate.outputs.secrets_found }}
      secrets_locations: ${{ steps.evaluate.outputs.secrets_locations }}
    steps:
      - name: Download agent output artifact
        id: download-agent
        continue-on-error: true
        uses: actions/download-artifact@v8.0.1
        with:
          name: agent
          path: /tmp/gh-aw/agent

      - name: Download cache-memory artifact
        id: download-cache-memory
        continue-on-error: true
        uses: actions/download-artifact@v8.0.1
        with:
          name: cache-memory
          path: /tmp/gh-aw/cache-memory

      - name: Download repo-memory artifact
        id: download-repo-memory
        continue-on-error: true
        uses: actions/download-artifact@v8.0.1
        with:
          name: repo-memory-default
          path: /tmp/gh-aw/repo-memory/default

      - name: Install TruffleHog
        id: install-trufflehog
        env:
          TRUFFLEHOG_VERSION: "3.88.27"
          # SHA256 of the linux_amd64 .tar.gz from the pinned release. Only GitHub-hosted ubuntu-latest runners
          # (always amd64) are supported; self-hosted ARM runners will need a separate per-arch hash.
          # To update: curl -fsSL https://github.com/trufflesecurity/trufflehog/releases/download/vNEW/trufflehog_NEW_linux_amd64.tar.gz | sha256sum
          TRUFFLEHOG_SHA256: "e3b2647b7a7bc1591f316da91fd33fc7397f8e3c21e2feed791c171f0c406bc7"
        run: |
          ARCH=$(uname -m)
          if [ "$ARCH" != "x86_64" ]; then
            echo "::error::TruffleHog installation only supports x86_64 (linux_amd64); detected $ARCH. Self-hosted ARM runners must install TruffleHog separately."
            exit 1
          fi
          echo "Downloading TruffleHog v${TRUFFLEHOG_VERSION}..."
          mkdir -p /tmp/gh-aw
          curl -fsSL "https://github.com/trufflesecurity/trufflehog/releases/download/v${TRUFFLEHOG_VERSION}/trufflehog_${TRUFFLEHOG_VERSION}_linux_amd64.tar.gz" -o /tmp/gh-aw/trufflehog.tar.gz
          # SHA256 covers the .tar.gz archive; the binary is extracted from the verified archive
          echo "${TRUFFLEHOG_SHA256}  /tmp/gh-aw/trufflehog.tar.gz" | sha256sum -c -
          sudo tar -xzf /tmp/gh-aw/trufflehog.tar.gz --no-same-owner -C /usr/local/bin trufflehog
          trufflehog --version

      - name: Scan agent output for secrets
        id: scan-agent-output
        continue-on-error: true
        run: |
          mkdir -p /tmp/gh-aw/agent/trufflehog
          SCAN_DIR="/tmp/gh-aw/agent"
          OUTPUT_FILE="/tmp/gh-aw/agent/trufflehog/agent-output-results.jsonl"
          if [ -d "$SCAN_DIR" ] && find "$SCAN_DIR" -mindepth 1 -maxdepth 1 -quit 2>/dev/null | grep -q .; then
            echo "Scanning agent output in $SCAN_DIR"
            trufflehog filesystem "$SCAN_DIR" \
              --json --no-update --fail \
              --exclude-paths /tmp/gh-aw/cache-memory \
              --exclude-paths /tmp/gh-aw/repo-memory \
              --exclude-paths /tmp/gh-aw/agent/trufflehog \
              2>/dev/null | tee "$OUTPUT_FILE" || SCAN_EXIT=${PIPESTATUS[0]}
            SCAN_EXIT=${SCAN_EXIT:-0}
          else
            echo "Agent output directory is empty or missing, skipping"
            SCAN_EXIT=0
          fi
          if [ "$SCAN_EXIT" -eq 183 ]; then
            echo "secrets_found=true" >> "$GITHUB_OUTPUT"
          fi

      - name: Scan cache-memory for secrets
        id: scan-cache-memory
        continue-on-error: true
        run: |
          mkdir -p /tmp/gh-aw/agent/trufflehog
          SCAN_DIR="/tmp/gh-aw/cache-memory"
          OUTPUT_FILE="/tmp/gh-aw/agent/trufflehog/cache-memory-results.jsonl"
          if [ -d "$SCAN_DIR" ] && find "$SCAN_DIR" -mindepth 1 -maxdepth 1 -quit 2>/dev/null | grep -q .; then
            echo "Scanning cache-memory in $SCAN_DIR"
            trufflehog filesystem "$SCAN_DIR" --json --no-update --fail 2>/dev/null | tee "$OUTPUT_FILE" || SCAN_EXIT=${PIPESTATUS[0]}
            SCAN_EXIT=${SCAN_EXIT:-0}
          else
            echo "cache-memory directory is empty or missing, skipping"
            SCAN_EXIT=0
          fi
          if [ "$SCAN_EXIT" -eq 183 ]; then
            echo "secrets_found=true" >> "$GITHUB_OUTPUT"
          fi

      - name: Scan repo-memory for secrets
        id: scan-repo-memory
        continue-on-error: true
        run: |
          mkdir -p /tmp/gh-aw/agent/trufflehog
          SCAN_DIR="/tmp/gh-aw/repo-memory"
          OUTPUT_FILE="/tmp/gh-aw/agent/trufflehog/repo-memory-results.jsonl"
          if [ -d "$SCAN_DIR" ] && find "$SCAN_DIR" -mindepth 1 -maxdepth 1 -quit 2>/dev/null | grep -q .; then
            echo "Scanning repo-memory in $SCAN_DIR"
            trufflehog filesystem "$SCAN_DIR" --json --no-update --fail 2>/dev/null | tee "$OUTPUT_FILE" || SCAN_EXIT=${PIPESTATUS[0]}
            SCAN_EXIT=${SCAN_EXIT:-0}
          else
            echo "repo-memory directory is empty or missing, skipping"
            SCAN_EXIT=0
          fi
          if [ "$SCAN_EXIT" -eq 183 ]; then
            echo "secrets_found=true" >> "$GITHUB_OUTPUT"
          fi

      - name: Evaluate TruffleHog results
        id: evaluate
        if: always()
        env:
          AGENT_FOUND: ${{ steps.scan-agent-output.outputs.secrets_found }}
          CACHE_FOUND: ${{ steps.scan-cache-memory.outputs.secrets_found }}
          REPO_FOUND: ${{ steps.scan-repo-memory.outputs.secrets_found }}
        run: |
          echo "==================================="
          echo "🔍 TruffleHog Scan Summary"
          echo "==================================="
          echo "Agent output:  ${AGENT_FOUND:-clean}"
          echo "Cache-memory:  ${CACHE_FOUND:-clean}"
          echo "Repo-memory:   ${REPO_FOUND:-clean}"
          echo "==================================="

          if [[ "$AGENT_FOUND" == "true" || "$CACHE_FOUND" == "true" || "$REPO_FOUND" == "true" ]]; then
            LOCATIONS=()
            [[ "$AGENT_FOUND" == "true" ]] && LOCATIONS+=("agent output")
            [[ "$CACHE_FOUND" == "true" ]] && LOCATIONS+=("cache-memory")
            [[ "$REPO_FOUND" == "true" ]] && LOCATIONS+=("repo-memory")
            LOCATIONS_STR=$(IFS=', '; echo "${LOCATIONS[*]}")
            echo "secrets_found=true" >> "$GITHUB_OUTPUT"
            echo "secrets_locations=${LOCATIONS_STR}" >> "$GITHUB_OUTPUT"
            echo "::error::TruffleHog detected secrets in: ${LOCATIONS_STR}"
            exit 1
          else
            echo "secrets_found=false" >> "$GITHUB_OUTPUT"
            echo "✅ No secrets detected by TruffleHog"
          fi

      - name: Upload TruffleHog scan results
        if: always()
        uses: actions/upload-artifact@v7.0.1
        with:
          name: trufflehog-scan-results
          path: /tmp/gh-aw/agent/trufflehog/
          if-no-files-found: ignore

  conclusion:
    pre-steps:
      - name: Report TruffleHog secret scan failure
        if: always() && needs.trufflehog_scan.result == 'failure' && needs.trufflehog_scan.outputs.secrets_found == 'true'
        continue-on-error: true
        uses: actions/github-script@v9
        env:
          GH_AW_TRUFFLEHOG_SECRETS_LOCATIONS: ${{ needs.trufflehog_scan.outputs.secrets_locations }}
          GH_AW_RUN_URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}
          GH_AW_WORKFLOW_NAME: ${{ github.workflow }}
        with:
          script: |
            const locations = process.env.GH_AW_TRUFFLEHOG_SECRETS_LOCATIONS || 'unknown locations';
            const runUrl = process.env.GH_AW_RUN_URL;
            const workflowName = process.env.GH_AW_WORKFLOW_NAME;
            const runNumber = context.runNumber;
            const { owner, repo } = context.repo;
            core.error(`🔐 TruffleHog detected secrets in: ${locations}`);
            const title = `🔐 Secrets detected in workflow run: ${workflowName} #${runNumber}`;
            const body = [
              '> [!CAUTION]',
              '> **TruffleHog detected secrets in the agentic workflow output.**',
              '',
              `**Locations:** \`${locations}\``,
              '',
              `**Workflow run:** [${workflowName} #${runNumber}](${runUrl})`,
              '',
              'Please review the `trufflehog-scan-results` artifact in the workflow run for details.',
              'Rotate any exposed credentials immediately.',
            ].join('\n');
            const issue = await github.rest.issues.create({ owner, repo, title, body, labels: ['security'] });
            core.info(`Created secret detection issue: ${issue.data.html_url}`);
---
<!--
# TruffleHog Secret Detection

This shared workflow adds [TruffleHog](https://github.com/trufflesecurity/trufflehog) secret scanning
as a dedicated `trufflehog_scan` job that runs after the `detection` job. It scans the agent's output,
cache-memory, and repo-memory for accidentally leaked secrets (API keys, tokens, credentials, etc.).

## How It Works

1. **Separate job** — `trufflehog_scan` runs after the `detection` job completes
2. **Download artifacts** — fetches `agent`, `cache-memory`, and `repo-memory` artifacts (continue-on-error)
3. **Install TruffleHog** — pinned to a specific version
4. **Scan agent output** — scans `/tmp/gh-aw/agent/` (agent output and code patches)
5. **Scan cache-memory** — scans `/tmp/gh-aw/cache-memory/`
6. **Scan repo-memory** — scans `/tmp/gh-aw/repo-memory/`
7. **Evaluate** — aggregates results; sets `secrets_found=true` output and fails the job if secrets detected
8. **Upload results** — saves JSONL scan result files as `trufflehog-scan-results` artifact for review
9. **Failure report** — a `jobs.conclusion.pre-steps` entry creates a GitHub issue with the findings
   when secrets are detected

## Job Outputs

| Output | Value |
|--------|-------|
| `secrets_found` | `true` or `false` |
| `secrets_locations` | Comma-separated list of locations where secrets were found |

## Failure Reporting

When `secrets_found=true` the `trufflehog_scan` job fails. The conclusion job (which automatically
depends on all jobs) then runs the pre-step `Report TruffleHog secret scan failure`, which creates
a GitHub issue titled `🔐 Secrets detected in workflow run: <name> #<run>` with details about the
finding and a link to the `trufflehog-scan-results` artifact.

## Usage

```yaml
---
imports:
  - shared/trufflehog.md
---
```
-->

---
private: true
emoji: "🛡️"
name: Daily VulnHunter Scan
description: Daily Claude Code workflow that runs Capital One VulnHunter's vulnhunt methodology inside the sandbox against a pre-scoped snapshot of this repository
on:
  schedule: daily
  workflow_dispatch:
permissions:
  actions: read
  contents: read
  issues: read
model: claude-sonnet-5
engine:
  id: claude
jobs:
  vulnhunter_bundle:
    runs-on: ubuntu-latest
    permissions:
      contents: read
    outputs:
      artifact_name: ${{ steps.artifact_name.outputs.value }}
    steps:
      - name: Checkout repository
        uses: actions/checkout@v7.0.1
        with:
          persist-credentials: false
      - name: Compute artifact name
        id: artifact_name
        run: |
          echo "value=vulnhunter-bundle-${{ github.run_id }}" >> "$GITHUB_OUTPUT"
      - name: Prepare VulnHunter bundle
        run: |
          set -euo pipefail
          BUNDLE_ROOT="$RUNNER_TEMP/vulnhunter-bundle"
          REPO_ROOT="$BUNDLE_ROOT/repo"
          SKILL_ROOT="$BUNDLE_ROOT/vulnhunt"
          SCOPE_ROOT="$BUNDLE_ROOT/scope"

          rm -rf "$BUNDLE_ROOT"
          mkdir -p "$REPO_ROOT" "$SKILL_ROOT" "$SCOPE_ROOT" "$BUNDLE_ROOT/out"

          # Only the `vulnhunt` skill is needed; the rest of the VulnHunter tree
          # (agents, harness, fix tooling) would just be dead weight in the sandbox.
          curl -fsSL https://codeload.github.com/capitalone/VulnHunter/tar.gz/refs/heads/main \
            | tar -xz --strip-components=2 -C "$SKILL_ROOT" --wildcards '*/vulnhunt/*'

          # Snapshot the repository, then keep only scannable, non-generated source.
          git archive --format=tar HEAD | tar -xf - -C "$REPO_ROOT"
          find "$REPO_ROOT" -type f \
            ! \( -name '*.go' -o -name '*.cjs' -o -name '*.js' -o -name '*.ts' -o -name '*.sh' \) \
            -delete
          find "$REPO_ROOT" -type f \
            \( -name '*_test.go' -o -name '*.test.cjs' -o -name '*.test.js' -o -name '*.test.ts' \) \
            -delete
          find "$REPO_ROOT" -type d -empty -delete

          # Rank files by security-risk signal so the agent never has to explore the tree.
          HITS="$RUNNER_TEMP/vulnhunter-hits.tsv"
          : > "$HITS"
          add_hits() {
            (cd "$REPO_ROOT" || return; grep -rlE "$2" . 2>/dev/null || true) \
              | sed 's|^\./||' \
              | awk -v w="$1" '{print w"\t"$0}' >> "$HITS"
          }
          add_hits 5 'exec\.Command|exec\.CommandContext|child_process|execSync|spawnSync'
          add_hits 4 'http\.NewRequest|http\.Get|http\.Post|url\.Parse|fetch\('
          add_hits 4 'Authorization|GITHUB_TOKEN|[Bb]earer |_API_KEY|\.Token'
          add_hits 3 'filepath\.Join|os\.ReadFile|os\.WriteFile|os\.Create|os\.OpenFile'
          add_hits 2 'template\.(New|Must|HTML)|regexp\.MustCompile|json\.Unmarshal'

          RANKED="$RUNNER_TEMP/vulnhunter-ranked.tsv"
          awk -F'\t' '{s[$2]+=$1} END {for (f in s) printf "%d\t%s\n", s[f], f}' "$HITS" \
            | sort -k1,1nr -k2,2 > "$RANKED"

          # Bounded per-run scope: the highest-risk core plus a window that rotates
          # daily through the remainder, so coverage accumulates across runs.
          CORE=20
          ROTATING=20
          DAY=$((10#$(date -u +%j)))
          TOTAL=$(wc -l < "$RANKED")
          TAIL_COUNT=$((TOTAL - CORE))
          {
            head -n "$CORE" "$RANKED"
            if [ "$TAIL_COUNT" -gt "$ROTATING" ]; then
              OFFSET=$(( (DAY * ROTATING) % TAIL_COUNT ))
              tail -n +"$((CORE + 1))" "$RANKED" \
                | awk -v off="$OFFSET" -v n="$ROTATING" -v total="$TAIL_COUNT" \
                    'NR > off && NR <= off + n { print } off + n > total && NR <= off + n - total { print }'
            elif [ "$TAIL_COUNT" -gt 0 ]; then
              tail -n +"$((CORE + 1))" "$RANKED"
            fi
          } | cut -f2 > "$SCOPE_ROOT/candidates.txt"
          rm -f "$HITS" "$RANKED"

          cat > "$BUNDLE_ROOT/README.md" <<'EOF'
          # VulnHunter bundle

          - `repo/` contains a source-only snapshot of the target repository
            (generated, vendored, documentation, and test files are already removed).
          - `vulnhunt/` contains only the Capital One VulnHunter `vulnhunt` skill
            (`SKILL.md` plus `phases/`).
          - `scope/candidates.txt` lists the pre-ranked files to scan this run, relative
            to `repo/`. It is the complete scan scope; do not widen it.
          - `out/` is the writable directory for scan notes and structured findings.
          EOF

          # Artifact upload drops empty directories; keep `out/` present in the sandbox.
          printf 'Write scan notes and confirmed findings here.\n' > "$BUNDLE_ROOT/out/README.md"
      - name: Upload VulnHunter bundle artifact
        uses: actions/upload-artifact@v7.0.1
        with:
          name: ${{ steps.artifact_name.outputs.value }}
          path: ${{ runner.temp }}/vulnhunter-bundle
          if-no-files-found: error
          retention-days: 7
sandbox:
  agent:
    runtime: cloud-hypervisor
    id: awf
steps:
  - name: Download VulnHunter bundle artifact
    uses: actions/download-artifact@v8.0.1
    with:
      name: ${{ needs.vulnhunter_bundle.outputs.artifact_name }}
      path: /tmp/gh-aw/agent/vulnhunter
tools:
  bash:
    - "*"
safe-outputs:
  create-issue:
    title-prefix: "[vulnhunter] "
    labels: [security, vulnhunter, cookie]
    close-older-issues: true
    max: 1
  noop:
timeout-minutes: 45
max-turns: 80
strict: true
network:
  allowed:
    - defaults
    - github
imports:
  - shared/otlp.md
  - shared/reporting.md
evals:
  - id: scan_completed
    question: Did the agent download the prepared VulnHunter bundle artifact, load the vulnhunt skill instructions, and scan the pre-ranked candidate files?
  - id: issue_created_or_noop
    question: Was a security issue created for verified exploitable findings, or was noop used when VulnHunter found nothing actionable?
features:
  gh-aw-detection: true
---

# Daily VulnHunter Scan

Run Capital One's [VulnHunter](https://github.com/capitalone/VulnHunter) methodology inside the sandbox against the pre-scoped repository snapshot packaged by the `vulnhunter_bundle` job.

The bundle job already did the reconnaissance deterministically: the snapshot contains
source files only, and `scope/candidates.txt` holds the pre-ranked, budget-sized scan
scope for this run. Work inside that scope — you must finish within the run's AI credit
budget, so spend it on analysis, not on exploring the repository.

## Task

1. Read `/tmp/gh-aw/agent/vulnhunter/README.md` for the prepared bundle layout.
2. Read `/tmp/gh-aw/agent/vulnhunter/vulnhunt/SKILL.md` for the methodology. Apply it as a
   **single agent**: ignore its orchestrator/sub-agent dispatch machinery and its Phase 1
   recon instructions, which the bundle job already replaced.
3. Read exactly these two phase files and no others:
   - `/tmp/gh-aw/agent/vulnhunter/vulnhunt/phases/phase2_class_inj.md`
   - `/tmp/gh-aw/agent/vulnhunter/vulnhunt/phases/phase2b_verify.md`
4. Read `/tmp/gh-aw/agent/vulnhunter/scope/candidates.txt`. Every path in it is relative to
   `/tmp/gh-aw/agent/vulnhunter/repo`. This list is the **complete** scan scope:
   - Do not scan, list, or grep files outside it.
   - Work through it in order — it is ranked by risk — and stop early if you are close to
     exhausting the run budget.
   - You may read a file referenced by a candidate only when it is required to confirm or
     disprove a specific finding.
5. Apply the injection phase to the candidates, falsify every candidate finding with the
   verification phase, and save confirmed findings to `/tmp/gh-aw/agent/vulnhunter/out/`.

## Reporting Rules

- Only report findings that survive VulnHunter's falsification/disproof process.
- Do not report speculative, low-confidence, or test-only issues.
- If there are no verified exploitable findings, call `noop` with a short explanation.
- If there are verified findings, create exactly one issue summarizing up to the 3 highest-confidence vulnerabilities.

## Issue Format

Use the title `VulnHunter findings in ${{ github.repository }}`.

### Output Format

- Use `###` (h3) or lower for all report headers; never use `#` or `##` inside the report body.
- Wrap long lists, tables, and detailed findings in `<details><summary><b>...</b></summary>...</details>` blocks to reduce scrolling.
- Structure reports as: overview → key metrics/issues → collapsible detail → next actions.

For each reported finding include:
- affected file(s) and function or component
- vulnerability type and severity
- attacker path or exploit preconditions
- why the finding is credible after falsification
- concrete remediation guidance

Keep the issue concise and evidence-backed.
---
private: true
emoji: "🌊"
name: DataFlow PR & Discussion Dataset Builder
description: Builds cleaned, deduplicated, quality-scored datasets from GitHub discussions and PRs using OpenDCAI/DataFlow text processing pipelines
on:
  schedule: weekly
  workflow_dispatch:
permissions:
  contents: read
  discussions: read
  pull-requests: read
  issues: read


network:
  allowed:
    - defaults
    - python
    - github-actions
imports:
  - shared/pmg.md
  - uses: shared/discussions-data-fetch.md
  - shared/reporting.md
  - shared/otlp.md
tools:
  cli-proxy: true
  bash:
    - "/tmp/gh-aw/python/venv/bin/python3 *"
  github:
    mode: gh-proxy
    min-integrity: approved
    toolsets:
      - default
      - pull_requests
      - discussions
steps:
  - name: Install DataFlow
    run: |
      mkdir -p /tmp/gh-aw/agent/dataflow/{input,output,pipeline,reports}
      mkdir -p /tmp/gh-aw/python
      python3 -m venv /tmp/gh-aw/python/venv

      VENV=/tmp/gh-aw/python/venv
      STATUS=/tmp/gh-aw/agent/dataflow/output/dataflow_runtime.json
      LOG=/tmp/gh-aw/agent/dataflow/output/dataflow_install.log
      : > "$LOG"

      dataflow_ready=false
      if timeout 5m "$VENV/bin/pip" install --quiet --disable-pip-version-check --retries 3 --timeout 60 "uv==0.8.3" >>"$LOG" 2>&1 &&
         timeout 20m env UV_HTTP_TIMEOUT=60 UV_HTTP_RETRIES=3 \
           "$VENV/bin/uv" pip install --python "$VENV/bin/python3" "open-dataflow==1.0.10" >>"$LOG" 2>&1 &&
         timeout 2m "$VENV/bin/python3" - <<'PY' >>"$LOG" 2>&1
      import json
      from pathlib import Path
      from dataflow.utils.storage import FileStorage
      from dataflow.operators.general_text import (
          CharNumberFilter,
          HashDeduplicateFilter,
      )

      fixture_dir = Path("/tmp/gh-aw/agent/dataflow/smoke")
      fixture_dir.mkdir(parents=True, exist_ok=True)
      fixture = fixture_dir / "fixture.jsonl"
      fixture.write_text(
          "\n".join(
              json.dumps(record)
              for record in [
                  {"id": "valid-1", "text": "Alpha words " * 8},
                  {"id": "short", "text": "tiny"},
                  {"id": "valid-1-dup", "text": "Alpha words " * 8},
              ]
          )
          + "\n"
      )

      storage = FileStorage(first_entry_file_name=str(fixture), cache_path=str(fixture_dir / "cache"))
      storage.step()
      CharNumberFilter(threshold=50).run(storage=storage, input_key="text")
      after_length = storage.step().read("dict")
      if len(after_length) != 2:
          raise RuntimeError(f"unexpected CharNumberFilter result: {len(after_length)}")

      storage.write(after_length)
      storage.step()
      HashDeduplicateFilter().run(storage=storage, input_key="text")
      after_dedup = storage.step().read("dict")
      if len(after_dedup) != 1:
          raise RuntimeError(f"unexpected HashDeduplicateFilter result: {len(after_dedup)}")

      print("DataFlow API smoke test passed")
      PY
      then
        dataflow_ready=true
      else
        echo "::warning::DataFlow installation or API validation failed; the workflow will use the pure-Python or mixed fallback path."
      fi

      "$VENV/bin/python3" - <<PY
      import json
      runtime = {
          "dataflow_ready": "${dataflow_ready}" == "true",
          "dataflow_version": "",
          "selected_operators": [
              "CharNumberFilter",
              "HashDeduplicateFilter",
          ],
          "install_log": "$LOG",
          "warning": "",
      }
      if runtime["dataflow_ready"]:
          import dataflow
          runtime["dataflow_version"] = getattr(dataflow, "__version__", "installed")
      else:
          runtime["warning"] = "DataFlow installation or API validation failed; the workflow must report fallback mode honestly."
      with open("$STATUS", "w") as fh:
          json.dump(runtime, fh, indent=2)
      print(json.dumps(runtime, indent=2))
      PY

  - name: Fetch merged PRs
    env:
      GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: |
      bash "${RUNNER_TEMP}/gh-aw/actions/install_gh_cli.sh"

      # Fetch up to 500 merged PRs — title, body, metadata
      gh pr list \
        --repo "$GITHUB_REPOSITORY" \
        --state merged \
        --limit 500 \
        --json number,title,body,createdAt,mergedAt,url,author,labels \
        > /tmp/gh-aw/agent/dataflow/input/prs.json

      echo "Fetched $(jq 'length' /tmp/gh-aw/agent/dataflow/input/prs.json) merged PRs"

safe-outputs:
  upload-artifact:
    max-uploads: 3
    retention-days: 30
    skip-archive: false
  create-discussion:
    expires: 7d
    category: "audits"
    max: 1
    close-older-discussions: true
    title-prefix: "[dataflow-dataset] "
  messages:
    footer: "> 🌊 *Dataset built by [{workflow_name}]({run_url})*{ai_credits_suffix}{history_link}"
    run-started: "🌊 DataFlow Dataset Builder starting! [{workflow_name}]({run_url}) is processing discussions and PRs with OpenDCAI/DataFlow..."
    run-success: "✅ DataFlow dataset ready! [{workflow_name}]({run_url}) produced a cleaned, deduplicated dataset. Artifacts uploaded. 📊"
    run-failure: "⚠️ DataFlow pipeline failed! [{workflow_name}]({run_url}) {status}. Check the run logs."
timeout-minutes: 30
tracker-id: dataflow-pr-discussion-dataset
experiments:
  caveman_mode:
    variants: ["no", "yes"]
    description: "Test whether extreme prompt compression (removing ASCII diagrams, embedded Python scripts, and step-by-step narration) preserves DataFlow dataset quality and run reliability"
    hypothesis: "H0: no change in input_token_count, retention_rate, or run_success_rate. H1: caveman prompt cuts input tokens ≥30% with retention_rate ≥0.80 and run_success_rate ≥0.90"
    metric: input_token_count
    secondary_metrics: [run_duration_ms, dataset_record_count, retention_rate]
    guardrail_metrics:
      - name: run_success_rate
        threshold: ">=0.9"
      - name: empty_output_rate
        threshold: "==0.0"
    min_samples: 10
    weight: [50, 50]
    start_date: "2026-06-05"
    issue: 37102
strict: true
evals:
  - id: caveman_mode_goal_met
    question: Does the agent output show that the objective for experiment caveman_mode was successfully completed?

features:
  gh-aw-detection: true
---

{{#if experiments.caveman_mode == 'yes' }}
Build a cleaned JSONL dataset from this repo's discussions and PRs using OpenDCAI/DataFlow.

Inputs:
- Discussions: `/tmp/gh-aw/agent/discussions-data/discussions.json`
- PRs: `/tmp/gh-aw/agent/dataflow/input/prs.json`

Output: `/tmp/gh-aw/agent/dataflow/output/dataset_clean.jsonl`
Venv: `/tmp/gh-aw/python/venv/bin/python3`

Normalise both sources into unified JSONL (fields: id, source, text, url, author, created_at).
Use the runtime status from `/tmp/gh-aw/agent/dataflow/output/dataflow_runtime.json`.
Keep the 50–100,000 character bound and 0.25 alphabetic-character ratio.
Use current DataFlow operators from `dataflow.operators.general_text` for supported stages, skip `AlphaWordsFilter`, and fall back to Python where needed so the report can distinguish `mixed` from `fallback`.
Compute retention_rate = output/input, upload `dataset_clean.jsonl` as artifact, and post a stats table (including execution mode and operators actually used) to a GitHub Discussion in category `audits`.
{{else}}
# DataFlow PR & Discussion Dataset Builder

You are a data pipeline agent that uses [OpenDCAI/DataFlow](https://github.com/OpenDCAI/DataFlow) to process GitHub discussions and pull requests into high-quality, deduplicated datasets suitable for LLM training and analysis.

## Mission

Build a cleaned, quality-scored, and deduplicated JSONL dataset from this repository's discussions and PRs, then post a summary report as a GitHub Discussion.

## Current Context

- **Repository**: ${{ github.repository }}
- **Run ID**: ${{ github.run_id }}
- **Data available**:
  - Discussions: `/tmp/gh-aw/agent/discussions-data/discussions.json` (pre-fetched by shared component)
  - PRs: `/tmp/gh-aw/agent/dataflow/input/prs.json` (pre-fetched in `steps:`)
- **DataFlow venv**: `/tmp/gh-aw/python/venv/bin/python3`
- **Output dir**: `/tmp/gh-aw/agent/dataflow/output/`

## Pipeline Overview

```
GitHub Discussions + PRs
         │
         ▼
  ┌─────────────┐
  │ Normalise   │  Convert to unified JSONL (title + body = "text", metadata preserved)
  └──────┬──────┘
         │
         ▼
  ┌─────────────┐
  │ Mixed       │  DataFlow char-length filter → Python alpha-ratio filter
  │ Filters     │
  └──────┬──────┘
         │
         ▼
  ┌─────────────┐
  │ DataFlow    │  Exact-hash deduplication (HashDeduplicateFilter)
  │ Dedup       │
  └──────┬──────┘
         │
         ▼
  ┌─────────────┐
  │ Output      │  Clean JSONL + stats report
  └─────────────┘
```

## Step-by-Step Instructions

### Step 1: Inspect the DataFlow Runtime Status

Read `/tmp/gh-aw/agent/dataflow/output/dataflow_runtime.json` first.

- If `dataflow_ready` is `true`, use the smoke-tested current API:
  - `dataflow.utils.storage.FileStorage`
  - `dataflow.operators.general_text.CharNumberFilter`
  - `dataflow.operators.general_text.HashDeduplicateFilter`
- If `dataflow_ready` is `false`, print the recorded warning and use the pure-Python fallback pipeline.
- Never use `AlphaWordsFilter`; it can trigger an implicit NLTK download.

### Step 2: Normalise Raw Data into JSONL

Convert both discussions and PRs into a unified JSONL format with a `text` field that
DataFlow operators will read.

Write a Python script `/tmp/gh-aw/agent/dataflow/pipeline/01_normalise.py`:

```python
#!/usr/bin/env python3
"""Normalise discussions and PRs into a single JSONL file for DataFlow."""

import json
import sys
from pathlib import Path

OUT = Path("/tmp/gh-aw/agent/dataflow/input/combined_raw.jsonl")
records = []

# ── Discussions ───────────────────────────────────────────────────────────────
disc_path = Path("/tmp/gh-aw/agent/discussions-data/discussions.json")
if disc_path.exists():
    discussions = json.loads(disc_path.read_text())
    for d in discussions:
        title = (d.get("title") or "").strip()
        body = (d.get("body") or "").strip()
        text = f"{title}\n\n{body}".strip() if body else title
        if text:
            records.append({
                "id": f"discussion-{d.get('number', '')}",
                "source": "discussion",
                "text": text,
                "title": title,
                "url": d.get("url", ""),
                "author": d.get("author", ""),
                "created_at": d.get("createdAt", ""),
                "category": d.get("category", ""),
                "labels": d.get("labels", []),
            })
    print(f"Loaded {len(discussions)} discussions → {sum(1 for r in records if r['source']=='discussion')} with text")

# ── Pull Requests ─────────────────────────────────────────────────────────────
pr_path = Path("/tmp/gh-aw/agent/dataflow/input/prs.json")
if pr_path.exists():
    prs = json.loads(pr_path.read_text())
    pr_count_before = len(records)
    for p in prs:
        title = (p.get("title") or "").strip()
        body = (p.get("body") or "").strip()
        text = f"{title}\n\n{body}".strip() if body else title
        author_obj = p.get("author") or {}
        if text:
            records.append({
                "id": f"pr-{p.get('number', '')}",
                "source": "pull_request",
                "text": text,
                "title": title,
                "url": p.get("url", ""),
                "author": author_obj.get("login", "") if isinstance(author_obj, dict) else str(author_obj),
                "created_at": p.get("createdAt", ""),
                "merged_at": p.get("mergedAt", ""),
                "labels": [lb.get("name","") for lb in (p.get("labels") or [])],
            })
    print(f"Loaded {len(prs)} PRs → {len(records) - pr_count_before} with text")

# Write unified JSONL
OUT.parent.mkdir(parents=True, exist_ok=True)
with OUT.open("w") as fh:
    for r in records:
        fh.write(json.dumps(r, ensure_ascii=False) + "\n")

print(f"Total records written: {len(records)} → {OUT}")
```

Run it:

```bash
/tmp/gh-aw/python/venv/bin/python3 /tmp/gh-aw/agent/dataflow/pipeline/01_normalise.py
```

### Step 3: Build and Run the DataFlow Pipeline

Write `/tmp/gh-aw/agent/dataflow/pipeline/02_pipeline.py`:

```python
#!/usr/bin/env python3
"""
Dataset cleaning pipeline:
  1. Load JSONL records and the runtime status file
  2. DataFlow char-length filter when the validated API is available
  3. Deterministic Python alpha-ratio filter (to avoid NLTK downloads)
  4. DataFlow exact-hash deduplication when available; pure-Python fallback otherwise
  5. Save clean output and honest execution-mode statistics
"""

import json
from pathlib import Path

INPUT  = "/tmp/gh-aw/agent/dataflow/input/combined_raw.jsonl"
OUTPUT = "/tmp/gh-aw/agent/dataflow/output/dataset_clean.jsonl"
STATS  = "/tmp/gh-aw/agent/dataflow/output/pipeline_stats.json"
RUNTIME = "/tmp/gh-aw/agent/dataflow/output/dataflow_runtime.json"

Path("/tmp/gh-aw/agent/dataflow/output").mkdir(parents=True, exist_ok=True)

runtime = {}
if Path(RUNTIME).exists():
    runtime = json.loads(Path(RUNTIME).read_text())

dataflow_ready = bool(runtime.get("dataflow_ready"))
if not dataflow_ready and runtime.get("warning"):
    print(f"Runtime warning: {runtime['warning']}")

stats = {
    "input_count": 0,
    "after_length_filter": 0,
    "after_alpha_filter": 0,
    "after_dedup": 0,
    "operators_used": [],
    "execution_mode": "fallback",
    "fallback_mode": True,
    "dataflow_ready": dataflow_ready,
    "dataflow_version": runtime.get("dataflow_version", ""),
    "warnings": [],
}
if runtime.get("warning"):
    stats["warnings"].append(runtime["warning"])

def alpha_ratio(text: str) -> float:
    letters = sum(1 for c in text if c.isalpha())
    return letters / max(len(text), 1)

def exact_hash_dedup(records):
    import hashlib

    seen_hashes = set()
    kept = []
    for record in records:
        digest = hashlib.sha256(record.get("text", "").encode("utf-8")).hexdigest()
        if digest in seen_hashes:
            continue
        seen_hashes.add(digest)
        kept.append(record)
    return kept

with open(INPUT) as fh:
    raw_records = [json.loads(line) for line in fh if line.strip()]

stats["input_count"] = len(raw_records)
records_after_length = raw_records
dataflow_ops_used = []
python_ops_used = []

if dataflow_ready:
    try:
        from dataflow.utils.storage import FileStorage
        from dataflow.operators.general_text import CharNumberFilter, HashDeduplicateFilter

        storage = FileStorage(
            first_entry_file_name=INPUT,
            cache_path="/tmp/gh-aw/agent/dataflow/cache",
        )
        storage.step()  # step 0 = INPUT
        CharNumberFilter(threshold=50).run(storage=storage, input_key="text")
        storage.step()  # step 1 = length-filter output
        records_after_length = storage.read("dict")
        records_after_length = [r for r in records_after_length if len(r.get('text', '')) <= 100_000]
        dataflow_ops_used.append("CharNumberFilter")
        print(f"After CharNumberFilter: {len(records_after_length)} records")
    except Exception as e:
        stats["warnings"].append(f"CharNumberFilter failed: {e}")
        print(f"CharNumberFilter failed: {e} — falling back to Python length filter")
        records_after_length = [r for r in raw_records if 50 <= len(r.get('text', '')) <= 100_000]
        python_ops_used.append("python_length_filter")
else:
    records_after_length = [r for r in raw_records if 50 <= len(r.get('text', '')) <= 100_000]
    python_ops_used.append("python_length_filter")

stats["after_length_filter"] = len(records_after_length)

records_after_alpha = [r for r in records_after_length if alpha_ratio(r.get("text", "")) >= 0.25]
stats["after_alpha_filter"] = len(records_after_alpha)

records_after_dedup = records_after_alpha
if dataflow_ready:
    try:
        storage.write(records_after_alpha)  # writes step 2
        storage.step()  # step 2 = alpha-filter output
        HashDeduplicateFilter().run(storage=storage, input_key="text")
        storage.step()  # step 3 = dedup output
        records_after_dedup = storage.read("dict")
        dataflow_ops_used.append("HashDeduplicateFilter")
        print(f"After DataFlow deduplication: {len(records_after_dedup)} records")
    except Exception as e:
        stats["warnings"].append(f"DataFlow dedup failed: {e}")
        print(f"DataFlow dedup failed: {e} — falling back to Python exact-hash dedup")
        records_after_dedup = exact_hash_dedup(records_after_alpha)
        python_ops_used.append("python_exact_hash_dedup")
else:
    records_after_dedup = exact_hash_dedup(records_after_alpha)
    python_ops_used.append("python_exact_hash_dedup")

stats["after_dedup"] = len(records_after_dedup)
stats["operators_used"] = dataflow_ops_used + ["python_alpha_ratio_filter"] + python_ops_used

if dataflow_ops_used and not python_ops_used:
    stats["execution_mode"] = "dataflow"
elif dataflow_ops_used and python_ops_used:
    stats["execution_mode"] = "mixed"
else:
    stats["execution_mode"] = "fallback"
stats["fallback_mode"] = stats["execution_mode"] == "fallback"

_DATAFLOW_INTERNAL_KEYS = frozenset({
    "char_number_filter_label",
    "minhash_deduplicated_label",
    "hash_deduplicated_label",
})

with open(OUTPUT, "w") as fh:
    for record in records_after_dedup:
        clean = {k: v for k, v in record.items() if k not in _DATAFLOW_INTERNAL_KEYS}
        fh.write(json.dumps(clean, ensure_ascii=False) + "\n")
print(f"{stats['execution_mode']} pipeline output: {len(records_after_dedup)} records → {OUTPUT}")

# ── Write stats ───────────────────────────────────────────────────────────────
Path(STATS).write_text(json.dumps(stats, indent=2))
print(json.dumps(stats, indent=2))
```

Run it:

```bash
/tmp/gh-aw/python/venv/bin/python3 /tmp/gh-aw/agent/dataflow/pipeline/02_pipeline.py
```

Verify output:

```bash
/tmp/gh-aw/python/venv/bin/python3 - << 'EOF'
import json
from pathlib import Path

output = Path("/tmp/gh-aw/agent/dataflow/output/dataset_clean.jsonl")
stats = json.loads(Path("/tmp/gh-aw/agent/dataflow/output/pipeline_stats.json").read_text())

if not output.exists():
    raise SystemExit("data-processing-error: dataset_clean.jsonl was not created")

records = [json.loads(line) for line in output.read_text().splitlines() if line.strip()]
print(f"Output records: {len(records)}")
print(json.dumps(stats, indent=2))

if stats.get("input_count", 0) > 0 and not records:
    raise SystemExit("data-processing-error: filtering and deduplication produced an empty dataset")
EOF
```

### Step 4: Upload Dataset Artifact

Stage the output file and upload it as a workflow artifact:

```bash
# Stage for upload
mkdir -p "$RUNNER_TEMP/gh-aw/safeoutputs/upload-artifacts"
cp /tmp/gh-aw/agent/dataflow/output/dataset_clean.jsonl \
   "$RUNNER_TEMP/gh-aw/safeoutputs/upload-artifacts/dataset_clean.jsonl"
```

Then call the `upload_artifact` safe-output tool:

```json
{
  "type": "upload_artifact",
  "path": "dataset_clean.jsonl"
}
```

Record the returned artifact URL for use in the discussion report.

### Step 5: Keep Repo-Memory Out of the Success Path

Do not add repo-memory initialization or signed-commit setup to this workflow run.
Report the dataset result directly from the generated artifact and `pipeline_stats.json`.

### Step 6: Compute Quality Breakdown

Read the clean output and compute a per-source breakdown:

```bash
/tmp/gh-aw/python/venv/bin/python3 - << 'EOF'
import json
from collections import Counter
from pathlib import Path

records = [json.loads(l) for l in open("/tmp/gh-aw/agent/dataflow/output/dataset_clean.jsonl")]
stats   = json.loads(Path("/tmp/gh-aw/agent/dataflow/output/pipeline_stats.json").read_text())

by_source = Counter(r.get("source", "unknown") for r in records)
avg_len   = sum(len(r.get("text", "")) for r in records) / max(len(records), 1)

report = {
    "total_clean_records": len(records),
    "by_source": dict(by_source),
    "avg_text_length_chars": round(avg_len, 1),
    "operators_used": stats.get("operators_used", []),
    "input_count": stats.get("input_count", 0),
    "execution_mode": stats.get("execution_mode", "fallback"),
    "fallback_mode": stats.get("fallback_mode", True),
    "dataflow_version": stats.get("dataflow_version", ""),
    "warnings": stats.get("warnings", []),
    "retention_rate_pct": round(len(records) / max(stats.get("input_count", 1), 1) * 100, 1),
}

Path("/tmp/gh-aw/agent/dataflow/reports/quality_breakdown.json").write_text(json.dumps(report, indent=2))
print(json.dumps(report, indent=2))
EOF
```

### Step 7: Post Discussion Report

Build the full report body in Python (do not use shell variables for the discussion body) and post it with the `create_discussion` safe output.

Read the quality breakdown and artifact URL from files, then construct the discussion:

```python
import json
from pathlib import Path

quality = json.loads(Path("/tmp/gh-aw/agent/dataflow/reports/quality_breakdown.json").read_text())
stats   = json.loads(Path("/tmp/gh-aw/agent/dataflow/output/pipeline_stats.json").read_text())

# Read artifact URL saved after upload_artifact call
artifact_url = ""
try:
    artifact_url = Path("/tmp/gh-aw/agent/url-dataset-artifact.txt").read_text().strip()
except FileNotFoundError:
    pass

run_id   = "${{ github.run_id }}"
repo     = "${{ github.repository }}"
run_url  = f"${{ github.server_url }}/{repo}/actions/runs/{run_id}"
date_str = __import__('datetime').date.today().isoformat()

by_source_rows = "\n".join(
    f"| {src} | {cnt} |"
    for src, cnt in quality.get("by_source", {}).items()
)

artifact_section = ""
if artifact_url:
    artifact_section = f"\n### 📦 Dataset Artifact\n\n[Download dataset_clean.jsonl]({artifact_url})\n"

execution_mode = quality.get("execution_mode", "fallback")
mode_summary = {
    "dataflow": "All filtering and deduplication stages used the current DataFlow operators.",
    "mixed": "DataFlow handled the supported stages while deterministic Python fallbacks preserved the existing alpha-ratio contract.",
    "fallback": "The workflow used the pure-Python fallback because DataFlow installation or API validation did not succeed.",
}.get(execution_mode, "Execution mode unavailable.")

warnings = quality.get("warnings", [])
warning_section = ""
if warnings:
    warning_section = "### Runtime Warnings\n\n" + "\n".join(f"- {warning}" for warning in warnings) + "\n\n"

operators_str = ", ".join(quality.get("operators_used", ["none"])) or "none"

body = f"""### Summary

Built a cleaned, deduplicated dataset from GitHub discussions and PRs.

| Metric | Value |
|--------|-------|
| Execution mode | `{execution_mode}` |
| Input records | {quality.get("input_count", 0):,} |
| Output (clean) records | {quality.get("total_clean_records", 0):,} |
| Retention rate | {quality.get("retention_rate_pct", 0)}% |
| Average text length | {quality.get("avg_text_length_chars", 0):,.0f} chars |
| Operators used | `{operators_str}` |

{mode_summary}

### Records by Source

| Source | Count |
|--------|-------|
{by_source_rows}

{warning_section}### Pipeline

The pipeline applied these processing stages:

1. **Normalise** — Merged discussions and PRs into unified JSONL (`title + body` → `text`)
2. **Text length filter** — Kept records with 50–100,000 characters
3. **Alpha-ratio filter** — Removed records with fewer than 25% alphabetic characters using a deterministic Python stage
4. **Near-duplicate removal** — Eliminated near-identical records using DataFlow HashDeduplicateFilter or exact-hash fallback
{artifact_section}
### Pipeline Configuration

```yaml
Execution mode: {execution_mode}
DataFlow package: open-dataflow=={quality.get("dataflow_version", "not-installed") or "not-installed"}
Source: https://github.com/OpenDCAI/DataFlow
Input: GitHub Discussions + merged PRs from {repo}
Output: JSONL — one record per item, text field for LLM use
```

<details>
<summary>Raw pipeline statistics</summary>

```json
{json.dumps(stats, indent=2)}
```

</details>

---

*Generated by DataFlow PR & Discussion Dataset Builder — Run [#{run_id}]({run_url}) on {date_str}*
"""

Path("/tmp/gh-aw/agent/discussion_body.md").parent.mkdir(parents=True, exist_ok=True)
Path("/tmp/gh-aw/agent/discussion_body.md").write_text(body)
print("Discussion body written")
print(body[:500])
```

Then emit:

```json
{
  "type": "create_discussion",
  "title": "DataFlow Dataset Build Report ([MODE]) — [DATE]",
  "body": "[contents of /tmp/gh-aw/agent/discussion_body.md]",
  "category": "audits"
}
```

Replace `[MODE]` with the actual `execution_mode` value (`dataflow`, `mixed`, or `fallback`), replace `[DATE]` with today's ISO date, and replace `[contents of ...]` with the actual text read from `/tmp/gh-aw/agent/discussion_body.md`.

## Success Criteria

- ✅ Both discussions and PRs loaded and normalised into unified JSONL
- ✅ Execution mode reported honestly as `dataflow`, `mixed`, or `fallback`
- ✅ Clean JSONL artifact uploaded
- ✅ Quality breakdown computed (input/output counts, retention rate, source split)
- ✅ Discussion posted in `audits` category with full pipeline stats

## Edge Cases

### DataFlow API Changes
Use the pinned, smoke-tested current API from `dataflow.operators.general_text`.
Do not fall back to obsolete modules such as `dataflow.operators.filter` or `dataflow.operators.dedup`.

### No Data Available
If both input files are empty or missing, post a short discussion noting:
"No discussions or PRs found to process in this run."

### Insufficient Text
Do not relax the 50–100,000 character or 0.25 alphabetic-character thresholds.
If those thresholds remove every record, fail with a `data-processing-error` instead of silently widening the filter.

## Usage

To trigger this workflow on demand:

```bash
gh aw run dataflow-pr-discussion-dataset
```

The resulting JSONL dataset is suitable for:
- **LLM fine-tuning**: Supervised fine-tuning (SFT) datasets from real developer discussions
- **RAG indexing**: Embedding-ready clean text chunks from the repository's knowledge base
- **Analytics**: Deduplicated corpus for topic modelling, sentiment analysis, clustering
{{/if}}
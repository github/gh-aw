---
name: safe-output-stats
description: Show per-workflow stats of noop, report_missing_tool, report_missing_data and report_incomplete safe outputs from gh aw logs.
---

# Safe output stats

Measure how often agents end honestly instead of inventing work: `noop` (nothing to do), `report_missing_tool` and `report_missing_data` (blocked), `report_incomplete` (infrastructure failure). Slide: cracktro `#its-ok-to-noop`.

## Run

From the repository root (needs `gh aw` and `jq`):

```bash
# Download agent artifacts, then summarize (args after -- go to `gh aw logs`)
.github/skills/safe-output-stats/safe-output-stats.sh --download -- -c 50 --start-date -2w

# One workflow, another repo
.github/skills/safe-output-stats/safe-output-stats.sh --download -- daily-issue-triage -c 30 --repo owner/repo

# Re-summarize already downloaded runs; JSON for further jq
.github/skills/safe-output-stats/safe-output-stats.sh --dir .github/aw/logs --format json
```

The script reads each `run-<id>/` folder: `run_summary.json` (workflow name) and `agent_output.json` (validated safe-output items, from the `agent` artifact set). It never calls the GitHub API unless `--download` is set. Legacy `missing_tool`/`missing_data` items count as the `report_*` types.

To list runs that used one type, use `gh aw logs --safe-output noop` (also `missing-tool`, `missing-data`, `report-incomplete`).

## Columns

| Column | Meaning |
|---|---|
| `NOOP`, `MISSING_TOOL`, `MISSING_DATA`, `INCOMPLETE` | Item counts (a run may emit several) |
| `ACTED` | Run emitted at least one other safe output (issue, comment, PR, ...) and no `report_incomplete` |
| `BLOCKED` | Only missing-tool/missing-data reports, no action |
| `SILENT` | `agent_output.json` with no items: the agent ended without any terminal safe output |
| `NO_OUTPUT` | No `agent_output.json` downloaded (agent did not run, artifact expired or not downloaded) |
| `NOOP_%` | Runs whose only outcome was `noop`, as a share of runs |

Each run gets one outcome, by precedence: `no_output`, `incomplete`, `acted`, `blocked`, `noop`, `silent`. JSON output carries `outcomes` per workflow and in `total`.

## Read the numbers

- High `NOOP_%` on a scheduled workflow: it often has nothing to do. Consider a cheaper trigger or pre-agent skip check before the agent runs.
- `MISSING_TOOL`/`MISSING_DATA` > 0: fix tool allowlists, permissions or inputs; read the reasons with `gh aw audit <run-id>`.
- `INCOMPLETE` or `SILENT` > 0: infrastructure or prompt failures; debug with the `debugging-workflows` skill.
- `NOOP_%` near 0 with steady `ACTED` on a workflow that should often be idle: check outputs for invented work.
- Small samples mislead: report the run count with every rate.

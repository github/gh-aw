---
private: true
emoji: "🎮"
name: Squad Game Planner
description: Uses Squad (bradygaster/squad) multi-agent planning to design a new game concept daily and file the plan as tracked issues
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
  copilot-requests: write
network:
  allowed:
    - defaults
imports:
  - shared/squad.md
  - shared/reporting.md
tools:
  bash: true
  github:
    mode: gh-proxy
    toolsets: [default]
safe-outputs:
  create-issue:
    title-prefix: "[squad:plan] "
    labels: [squad:plan]
    group: true
    max: 12
    expires: 1
    close-older-issues: true
    close-older-key: squad-game-planner
pre-agent-steps:
  - name: Check Squad files
    run: |
      set -euo pipefail
      GH_AW_SAFE_OUTPUTS="${GH_AW_SAFE_OUTPUTS:-${RUNNER_TEMP:-/tmp}/gh-aw/safeoutputs/outputs.jsonl}"
      mkdir -p "$(dirname "$GH_AW_SAFE_OUTPUTS")"

      missing=()
      for path in .squad/team.md .github/agents/squad.agent.md; do
        if [ ! -f "$path" ]; then
          missing+=("$path")
        fi
      done

      if [ "${#missing[@]}" -gt 0 ]; then
        message="Squad files are unavailable: ${missing[*]}. The activation-job bootstrap step likely failed."
        printf '{"type":"noop","message":"%s"}\n' "$message" >> "$GH_AW_SAFE_OUTPUTS"
        echo "$message"
      fi
---

# Squad Game Planner

Use the Squad (https://github.com/bradygaster/squad) team to plan a
brand-new, original game concept today.

## Task

1. Confirm Squad files are available before delegating work to the team:
   `.squad/team.md` and `.github/agents/squad.agent.md` should exist. If
   either file is missing, call `noop` with a short explanation instead of
   proceeding.
2. Invent one new, small-to-medium-scope game concept that has not already
   been planned by a prior run (check open and recently closed issues
   labeled `squad:plan` for titles to avoid repeating a concept).
3. Work with the Squad team to break the concept down into a plan covering
   at minimum: core gameplay loop, target platform, art/style direction,
   minimum viable feature set, and a rough milestone breakdown (e.g.
   prototype, vertical slice, playable demo).
4. File the plan as a parent tracking issue titled with the game's name,
   followed by one sub-issue per milestone or major workstream (design,
   engineering, art, testing, etc.), using `temporary_id`/`parent` to link
   sub-issues to the parent. Every issue must carry the `squad:plan` label.
5. In the parent issue body, summarize the concept, the milestone plan, and
   which Squad team member is responsible for each workstream.

## Notes

- Each issue created today auto-closes after 1 day; the next scheduled run
  starts a fresh concept, and this run's `close-older-issues` setting closes
  any still-open issues from the previous run so the tracker only ever
  reflects the latest plan.
- If Squad cannot produce a usable plan (for example the CLI is unavailable
  or misconfigured), call `noop` with a short explanation instead of filing
  incomplete issues.

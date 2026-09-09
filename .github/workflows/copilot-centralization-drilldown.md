---
private: true
name: Copilot Centralization Drilldown
description: Expands one mined centralization candidate into a concrete draft workflow or reusable prompt template.
on:
  workflow_dispatch:
    inputs:
      candidate_title:
        description: Short name for the candidate skill or repeated task
        required: true
      target_kind:
        description: workflow, shared_prompt, playbook, or auto
        required: false
        default: auto
      recommendation_kind:
        description: Recommendation from the optimizer
        required: false
        default: shared_prompt_or_chatops
      sample_prompt:
        description: Representative prompt text
        required: false
      evidence_summary:
        description: Compact summary of repeated behavior and expected value
        required: false
      candidate_json:
        description: Optional compact JSON object from the optimizer output
        required: false
permissions:
  contents: read
  copilot-requests: write
engine:
  id: codex
  model-provider: github
model: copilot/gpt-5.3-codex
strict: true
max-ai-credits: 120
max-daily-ai-credits: 500
safe-outputs:
  mentions: false
  allowed-github-references: []
  max-bot-mentions: 1
  create-issue:
    title-prefix: "[copilot-centralization-draft] "
    labels: [report, ai-optimization, workflow-design]
    expires: 30d
steps:
  - name: Normalize candidate input
    env:
      CANDIDATE_TITLE: ${{ inputs.candidate_title }}
      TARGET_KIND: ${{ inputs.target_kind }}
      RECOMMENDATION_KIND: ${{ inputs.recommendation_kind }}
      SAMPLE_PROMPT: ${{ inputs.sample_prompt }}
      EVIDENCE_SUMMARY: ${{ inputs.evidence_summary }}
      CANDIDATE_JSON: ${{ inputs.candidate_json }}
    run: |
      set -euo pipefail
      GH_AW_SAFE_OUTPUTS="${GH_AW_SAFE_OUTPUTS:-${RUNNER_TEMP:-/tmp}/gh-aw/safeoutputs/outputs.jsonl}"
      mkdir -p /tmp/gh-aw/data

      if [ -n "${CANDIDATE_JSON}" ]; then
        printf '%s' "$CANDIDATE_JSON" | jq '.' > /tmp/gh-aw/data/candidate-raw.json
      else
        jq -n \
          --arg title "$CANDIDATE_TITLE" \
          --arg recommendation_kind "$RECOMMENDATION_KIND" \
          --arg sample_prompt "$SAMPLE_PROMPT" \
          --arg evidence_summary "$EVIDENCE_SUMMARY" \
          '{
            title: $title,
            recommendation_kind: $recommendation_kind,
            sample_prompt: (if $sample_prompt == "" then null else $sample_prompt end),
            evidence_summary: (if $evidence_summary == "" then null else $evidence_summary end)
          }' > /tmp/gh-aw/data/candidate-raw.json
      fi

      candidate_title="$(jq -r '.title // empty' /tmp/gh-aw/data/candidate-raw.json)"
      sample_prompt="$(jq -r '.sample_prompt // empty' /tmp/gh-aw/data/candidate-raw.json)"
      evidence_summary="$(jq -r '.evidence_summary // empty' /tmp/gh-aw/data/candidate-raw.json)"

      if [ -z "$candidate_title" ]; then
        printf '%s\n' '{"type":"noop","message":"No candidate title was provided for drilldown."}' >> "$GH_AW_SAFE_OUTPUTS"
        exit 0
      fi

      slug="$(printf '%s' "$candidate_title" \
        | tr '[:upper:]' '[:lower:]' \
        | sed 's/[^a-z0-9]/-/g' \
        | sed 's/-\{2,\}/-/g' \
        | sed 's/^-//' \
        | sed 's/-$//' \
        | cut -c1-48)"

      if [ -z "$slug" ]; then
        slug="candidate"
      fi

      resolved_target_kind="$TARGET_KIND"
      if [ "$resolved_target_kind" = "" ] || [ "$resolved_target_kind" = "auto" ]; then
        case "$RECOMMENDATION_KIND" in
          continuous_workflow)
            resolved_target_kind="workflow"
            ;;
          shared_prompt_or_chatops)
            resolved_target_kind="shared_prompt"
            ;;
          keep_ad_hoc_but_standardize)
            resolved_target_kind="playbook"
            ;;
          *)
            resolved_target_kind="workflow"
            ;;
        esac
      fi

      sample_len=${#sample_prompt}
      evidence_len=${#evidence_summary}
      candidate_strength="weak"
      if [ "$sample_len" -ge 24 ] || [ "$evidence_len" -ge 40 ] || [ -n "$CANDIDATE_JSON" ]; then
        candidate_strength="strong"
      fi

      jq -n \
        --arg title "$candidate_title" \
        --arg slug "$slug" \
        --arg recommendation_kind "$RECOMMENDATION_KIND" \
        --arg resolved_target_kind "$resolved_target_kind" \
        --arg sample_prompt "$sample_prompt" \
        --arg evidence_summary "$evidence_summary" \
        --arg candidate_strength "$candidate_strength" \
        --slurpfile raw /tmp/gh-aw/data/candidate-raw.json '
          {
            title: $title,
            slug: $slug,
            recommendation_kind: $recommendation_kind,
            resolved_target_kind: $resolved_target_kind,
            candidate_strength: $candidate_strength,
            sample_prompt: (if $sample_prompt == "" then null else $sample_prompt end),
            evidence_summary: (if $evidence_summary == "" then null else $evidence_summary end),
            raw_candidate: $raw[0]
          }
        ' > /tmp/gh-aw/data/candidate.json

      jq -n \
        --arg resolved_target_kind "$resolved_target_kind" \
        --arg slug "$slug" '
          {
            target_path: (
              if $resolved_target_kind == "workflow" then ".github/workflows/" + $slug + ".md"
              elif $resolved_target_kind == "shared_prompt" then ".github/workflows/shared/" + $slug + ".md"
              else ".github/workflows/shared/" + $slug + "-playbook.md"
              end
            ),
            trigger_hint: (
              if $resolved_target_kind == "workflow" then "Prefer workflow_dispatch first; choose schedule or event triggers only when repeated evidence justifies automation."
              elif $resolved_target_kind == "shared_prompt" then "Prefer a reusable prompt or shared workflow component before full automation."
              else "Keep this human-in-the-loop and produce a reusable playbook or prompt template."
              end
            ),
            implementation_bias: "Prefer the smallest durable artifact that reduces repeated prompting without widening scope.",
            report_style: "Use issue sections with visible summary and one fenced draft block."
          }
        ' > /tmp/gh-aw/data/derived-plan.json
sandbox:
  agent:
    runtime: cloud-hypervisor
evals:
  - id: candidate_expanded
    question: Did the agent expand a centralization candidate into a concrete draft workflow or reusable prompt template?
  - id: artifact_created
    question: Was a draft workflow, issue, or template artifact created for the centralization opportunity?
features:
  gh-aw-detection: true
---

# Copilot Centralization Drilldown

Turn one mined centralization candidate into a concrete, reviewable draft.

Read these prepared files first:
- `/tmp/gh-aw/data/candidate.json`
- `/tmp/gh-aw/data/derived-plan.json`

Stay narrow. Do not perform broad repo exploration. Use the current repository's existing workflow style as the default convention.

If `candidate_strength` is `weak`, or the provided evidence is too vague to justify a concrete draft, call `noop` with a short explanation.

## Task

1. Decide the smallest useful deliverable:
   - a new gh-aw workflow draft
   - a reusable shared prompt/template draft
   - a reusable human playbook when automation is premature
2. Produce exactly one primary draft aligned with `resolved_target_kind` unless the evidence clearly supports a better smaller artifact.
3. Optimize for AI-credit savings by reducing repeated prompt construction, ambiguous setup work, or repeated context rebuilding.
4. Keep the draft implementation-ready and short. Do not design a platform.

## Output

Use `create-issue`.

Structure the issue with these `###` sections:
- `### Summary`
- `### Pattern Fit`
- `### Proposed Draft`
- `### AI Credit Savings Rationale`
- `### Inputs Still Needed`
- `### References`

In `### Proposed Draft`:
- include `Path: <target path>` on its own line before the draft
- include exactly one fenced `md` code block containing the full proposed file content
- wrap that fenced code block in `<details><summary><b>Proposed File Content</b></summary>...</details>` when it exceeds 20 lines
- if the best artifact is a workflow, produce a full gh-aw workflow markdown file with frontmatter and prompt body
- if the best artifact is a shared prompt or playbook, produce the full markdown file content for that path

Keep `### Summary`, `### Pattern Fit`, and `### AI Credit Savings Rationale` visible, and wrap `### Inputs Still Needed` in `<details><summary><b>Inputs Still Needed</b></summary>...</details>` when it lists more than three items.

## Constraints

- Prefer `workflow_dispatch` for a first workflow draft unless the evidence strongly supports a scheduled or event-driven trigger.
- Keep permissions read-only and route visible writes through safe outputs.
- Reuse deterministic preprocessing or explicit `noop` guidance when that materially lowers token use.
- Do not create more than one draft artifact in the issue.
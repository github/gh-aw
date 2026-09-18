---
name: Test Experiments Rich Schema
description: Integration test workflow — validates all new experiment object-form fields compile correctly
on:
  schedule:
    - cron: daily
permissions:
  contents: read
engine: copilot
experiments:
  prompt_style:
    variants: [concise, detailed]
    description: "Test whether concise prompts reduce token consumption"
    hypothesis: "H0: no change in tokens. H1: concise reduces by >=15%"
    metric: aic
    secondary_metrics: [duration_ms, discussion_word_count]
    guardrail_metrics:
      - name: success_rate
        threshold: ">=0.95"
      - name: empty_output_rate
        threshold: "==0"
    min_samples: 25
    weight: [60, 40]
    issue: 1234
    start_date: "2026-01-01"
    end_date: "2026-12-31"
    analysis_type: t_test
    tags: [cost, prompting]
    notify:
      issue: 5678
---

Rich schema experiment test.

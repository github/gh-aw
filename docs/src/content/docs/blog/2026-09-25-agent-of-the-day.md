---
title: "Agent of the Day – September 25, 2026"
description: "Daily VulnHunter Scan reviewed 41 ranked candidates in gh-aw's own codebase for injection bugs, found one suspicious pattern, and had the discipline to prove it wasn't a real vulnerability before staying silent."
authors:
  - copilot
date: 2026-09-25
metadata:
  seoDescription: "VulnHunter scans gh-aw's own code daily for injection bugs, using Capital One's methodology, and knows when a suspicious pattern isn't a real vulnerability."
  linkedPostText: "VulnHunter finds a false alarm, and knows to call it one"
---

Most security scanners are optimized for one thing: finding something to report. That incentive quietly rewards noise — every string concatenation near a shell call becomes a "finding," every developer inherits a triage queue full of things that were never actually exploitable. Today's Agent of the Day takes the opposite approach. Meet the **Daily VulnHunter Scan**, a Claude Code workflow that hunts for injection-class bugs in `gh-aw`'s own source and treats "no findings" as a perfectly good outcome.

## Agent of the Day: Daily VulnHunter Scan 🛡️

VulnHunter runs Capital One's open-sourced `vulnhunt` methodology inside a sandboxed bundle of the repository — a snapshot of `pkg/cli`, `pkg/parser`, `pkg/workflow`, and `actions/setup/js`, plus a reference guide (`phase2_class_inj.md`) covering the usual dangerous-sink suspects: SQL injection, command injection, path traversal, SSRF, XXE, unrestricted file upload, XSS, open redirect, LDAP injection, and server-side template injection. Each day it works through a ranked candidate list, checking whether attacker- or LLM-controlled data can reach one of those sinks unsanitized.

On September 24 ([run 35961274043](https://github.com/github/gh-aw/actions/runs/35961274043)), the agent reviewed 25 of 41 candidates and flagged exactly one soft lead: an unsanitized `branch` argument passed into `git checkout -b` inside `actions/setup/js/apply_samples.cjs`, which on paper looks like a textbook CWE-88 flag-injection risk. But instead of filing an issue on pattern-match alone, VulnHunter ran it through a reachability gate — tracing where that `branch` value actually originates. It found the input comes from `GH_AW_SAMPLES`, a compile-time deterministic-replay fixture produced by `gh aw compile --use-samples` and authored by the workflow developer, never by live agent output or an external actor at runtime. No trust boundary is crossed, so the candidate was falsified and no issue was created — a correct "no finding" rather than a false alarm dressed up as a vulnerability report.

The very next run, on September 25 ([run 36099855973](https://github.com/github/gh-aw/actions/runs/36099855973)), the agent went further: all 40 ranked candidates reviewed, zero surviving even initial construction, so nothing needed the deeper Phase 2b falsification pass at all. Its reasoning notes are worth reading as a mini security audit in themselves — every `os/exec` call across the codebase uses array-based arguments rather than shell-string concatenation, path-construction sinks are guarded by explicit validators like `isSafeGitRevisionArg` and `fileutil.ValidatePathWithinBase`, Docker-based scanners validate image references before building argv, and values interpolated into generated YAML are consistently escaped or sourced from trusted workflow-author configuration.

That run also came with a candid self-assessment from `gh aw`'s own audit tooling: 50 turns and 1.94M tokens for a single-agent scan, flagged as a "resource heavy" profile for its task domain, with roughly half the turns doing data-gathering that could in principle move to deterministic pre-agent steps. Comparing it against a matched September 22 baseline showed turn count climbing from 44 to 50 while blocked network requests dropped from 3 to 0 — a small but visible behavioral drift that the audit surfaced automatically, without anyone having to eyeball two log files side by side.

![gh-aw workflow activity chart](https://github.com/github/gh-aw/blob/assets/Daily-Agent-of-the-Day-Blog-Writer/328451f896dea540a14ccc9eb4f7a48d3da56be2f854e92a9bea9dd70a87cf10.png?raw=true)

What makes VulnHunter interesting isn't that it never finds anything — it's that "nothing to report" is a real, load-bearing output, backed by explicit falsification logic instead of a shrug. In a codebase that runs LLM agents against live repositories every day, a scanner that can tell the difference between "this pattern looks scary" and "this pattern is actually reachable by an attacker" is worth more than one that just counts regex matches.

Curious how workflows like this get built, scoped, and kept honest run after run? Check out [github/gh-aw](https://github.com/github/gh-aw).

---
description: Shared architectural and security constraints for designing or updating agentic workflows.
---

# Agentic Workflow Constraints

## Execution Model

Agentic workflows run as a **single GitHub Actions job** with one agent execution.

## Can Do

- read GitHub data, APIs, web pages, and local repository files
- run tools inside the single job
- use MCP servers and safe outputs
- create GitHub resources through `safe-outputs:`
- persist lightweight state with `cache-memory` or other approved mechanisms

## Cannot Do

- pause and resume for external events
- orchestrate multi-stage pipelines with job dependencies
- pass state between multiple AI jobs in one workflow run
- implement built-in rollback across external systems
- wait for another workflow or deployment to finish inside the same agent run

## Recommend Traditional GitHub Actions When

- multi-stage deployment pipelines
- fan-out/fan-in job orchestration
- long waits for approvals or external systems
- rollback logic across several steps or systems
- cross-job state transfer

Suggested response:

> This requires capabilities the single-job agentic model does not support. Use traditional GitHub Actions for orchestration and agentic workflows for the AI-specific step.

## Security Posture

- Keep the main agent job read-only.
- Do not add GitHub write permissions to the agent job.
- Route GitHub writes through `safe-outputs:`.
- Do not create issues directly through agent tools, `gh`, or GitHub APIs; use `safe-outputs.create-issue`, check for an existing issue first, and use `noop` when no issue is needed.
- Prefer `tools.github.mode: gh-proxy` with `gh` for GitHub reads.
- Prefer `tools.cli-proxy: true` with mounted `mcp-clis` commands for non-GitHub MCP tools.
- Constrain `network.allowed:` to the minimum required ecosystems or domains.
- Use `${{ steps.sanitized.outputs.text }}` for untrusted user content.
- Never pass `${{ secrets.* }}` or other credentials to `post-steps:` or custom secret-scanning steps, or write secret values to logs or artifacts. Use supported declarative authentication for agent tools, or run credential-dependent scanning in a separate least-privilege non-agent job without exposing secret values.

## Safer Alternatives First

When a requested feature increases risk:

1. explain the risk
2. propose the safer pattern first
3. require explicit confirmation before relaxing safeguards

## Common Risk Areas

- direct write permissions instead of safe outputs
- auto-merge or bypassing review
- overly broad network access
- unbounded bash allowlists for untrusted input
- shell injection: interpolating `${{ github.event.* }}` or other untrusted expressions directly into `run:` scripts; pass untrusted values through environment variables instead
- placing OIDC/secret bootstrap in `pre-steps` instead of earlier `setup-steps`
- using `post-steps:` for agent-driven write actions
- placing credentials in `post-steps:` or custom secret-scanning steps

## Self-Hosted Runner Compatibility

When `runs-on` is any value other than GitHub-hosted labels (`ubuntu-latest`, `ubuntu-slim`, `windows-latest`, `macos-latest`):

- Set `runs-on` explicitly (not inherited from imports); accepts string, array, or runner-group object. Framework jobs (activation, safe-outputs, unlock, etc.) default to hosted `ubuntu-slim`, so also set `runs-on-slim` (same forms) to route them to the self-hosted runner.
- Write transient state, tool downloads, and outputs under `$RUNNER_TEMP`, not `/tmp` (which can persist across jobs on shared runners).
- Agent steps run as the runner user, not root — don't install to system-wide paths. The egress firewall needs sudo; if unavailable, it can be disabled (removing egress filtering) — surface the trade-off to the user rather than encoding it.
- Declare every outbound domain in `network.allowed` (keep `defaults` for core GitHub/Copilot/registry endpoints). Non-allow-listed domains are blocked when the firewall is enabled.
- Do not install to `/usr/local` or the toolcache (may be read-only/shared); use job-scoped writable paths.
- Do not hardcode `/home/runner` or any literal home path — read `$HOME`; use `$RUNNER_TEMP` for transient state.
- For GitHub Enterprise Server, enable GHES compatibility (GHES-compatible artifact action versions, enterprise API endpoint).

For the full set of requirements (Docker socket, ARC / Docker-in-Docker, network egress, GHES specifics), follow the [Self-Hosted Runners](/gh-aw/reference/self-hosted-runners/) reference page.

## Shared Reminder

Reference this file from creator, updater, and debugger prompts instead of repeating the architectural explanation.

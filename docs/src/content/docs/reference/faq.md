---
title: GitHub Agentic Workflows FAQ
description: Answers about GitHub Agentic Workflows, GitHub Actions, AI engines, workflow security, permissions, costs, and configuration.
sidebar:
  order: 50
head:
  - tag: script
    attrs:
      type: application/ld+json
    content: |
      {
        "@context": "https://schema.org",
        "@type": "FAQPage",
        "mainEntity": [
          {
            "@type": "Question",
            "name": "What is GitHub Agentic Workflows?",
            "acceptedAnswer": {
              "@type": "Answer",
              "text": "GitHub Agentic Workflows lets developers define AI-powered repository automation in Markdown with YAML frontmatter, compile it into GitHub Actions workflows, and run AI agents with configurable security controls. Built-in AI engines include GitHub Copilot, Claude Code, OpenAI Codex, Google Gemini, and Pi."
            }
          },
          {
            "@type": "Question",
            "name": "How does gh-aw work?",
            "acceptedAnswer": {
              "@type": "Answer",
              "text": "A developer writes a Markdown workflow with YAML frontmatter and natural-language instructions. The gh aw compile command generates a .lock.yml GitHub Actions workflow, which invokes the selected AI engine with configured tools, permissions, network access, and controlled write operations."
            }
          },
          {
            "@type": "Question",
            "name": "Which AI engine should I use with gh-aw?",
            "acceptedAnswer": {
              "@type": "Answer",
              "text": "GitHub Copilot is the default. Claude Code supports fine-grained turn control; OpenAI Codex and Google Gemini support their provider models; Pi can route to multiple providers. Select an engine with the engine field and configure its GitHub permission, API key, or supported workload identity."
            }
          },
          {
            "@type": "Question",
            "name": "Can I run Claude Code on a schedule in GitHub Actions using gh-aw?",
            "acceptedAnswer": {
              "@type": "Answer",
              "text": "Yes. Set engine: claude in workflow frontmatter, configure ANTHROPIC_API_KEY or Anthropic Workload Identity Federation, and add a schedule trigger. The compiled GitHub Actions workflow runs Claude Code in the configured agent container."
            }
          },
          {
            "@type": "Question",
            "name": "Is gh-aw a replacement for GitHub Actions?",
            "acceptedAnswer": {
              "@type": "Answer",
              "text": "No. gh-aw runs through GitHub Actions and complements deterministic CI/CD. Conventional Actions remain appropriate for builds, tests, linting, deployments, and reproducible scripts; agentic workflows add AI reasoning for interpretation, investigation, and generation tasks."
            }
          },
          {
            "@type": "Question",
            "name": "Can agentic workflows write code and create pull requests?",
            "acceptedAnswer": {
              "@type": "Answer",
              "text": "Yes. The recommended create-pull-request safe output proposes code changes for human review through a separate, permission-scoped job. Direct write permissions and custom jobs are possible but are separate trust boundaries and require explicit review."
            }
          },
          {
            "@type": "Question",
            "name": "Does gh-aw add any cost beyond what the AI engine charges?",
            "acceptedAnswer": {
              "@type": "Answer",
              "text": "No. gh-aw itself is free and open source. You pay only your AI provider's standard inference rates (or consume Copilot quota) plus GitHub Actions compute minutes."
            }
          },
          {
            "@type": "Question",
            "name": "Can I use MCP servers with agentic workflows?",
            "acceptedAnswer": {
              "@type": "Answer",
              "text": "Yes. Model Context Protocol servers can be configured in workflow frontmatter to extend workflows with custom tools, integrations, and data sources."
            }
          }
        ]
      }
---

## What is GitHub Agentic Workflows?

GitHub Agentic Workflows lets developers define AI-powered repository automation and run AI agents through GitHub Actions. Authors write Markdown instructions with YAML frontmatter, then `gh aw compile` generates the `.lock.yml` GitHub Actions workflow. Built-in AI engines include GitHub Copilot, Claude Code, OpenAI Codex, Google Gemini, and Pi.

> [!NOTE]
> GitHub Agentic Workflows is in Public Preview.

Start with the [GitHub Agentic Workflows quickstart](/gh-aw/setup/quick-start/), learn how to [create an agentic workflow](/gh-aw/setup/creating-workflows/), or browse the [gallery by repository task](/gh-aw/gallery/).

## Determinism

### I like deterministic CI/CD. Isn't this non-deterministic?

GitHub Agentic Workflows complements conventional GitHub Actions rather than replacing it. Deterministic Actions workflows remain the right choice for builds, tests, linting, deployments, and reproducible scripts. Agentic workflows are suited to tasks requiring interpretation, investigation, or generation, such as triaging issues, drafting documentation, researching dependencies, or proposing code improvements for human review.

## Capabilities

### What's the difference between agentic workflows and regular GitHub Actions workflows?

Conventional GitHub Actions workflows define deterministic steps in YAML. Agentic workflows add an AI agent that interprets natural-language Markdown instructions and calls configured tools. YAML frontmatter still defines triggers, permissions, the AI engine, network access, and safe outputs.

### What's the difference between agentic workflows and just running a coding agent in GitHub Actions?

While a standard GitHub Actions workflow can install and run a coding agent directly, GitHub Agentic Workflows provides a structured Markdown format, configurable security defaults, predefined GitHub tools, and a common way to select AI engines.

### Is gh-aw a replacement for GitHub Actions, or does it run on top of it?

`gh-aw` runs through GitHub Actions: it compiles a Markdown workflow file into a `.lock.yml` GitHub Actions workflow. Every run is an Actions run with native triggers, runner options, job logs, and spending limits. The generated agent job adds the selected AI engine and configurable controls such as sandboxing, threat detection, and safe outputs. See [How GitHub Agentic Workflows work](/gh-aw/introduction/how-they-work/) for the full process.

### Which AI engine should I use?

GitHub Copilot is the default and supports the `copilot-requests: write` permission or a `COPILOT_GITHUB_TOKEN`. Claude Code supports fine-grained turn control (`max-turns`) and authenticates with `ANTHROPIC_API_KEY` or Anthropic WIF. Codex uses `CODEX_API_KEY` or `OPENAI_API_KEY`; Gemini uses `GEMINI_API_KEY` or Google WIF. Pi selects authentication from its `provider/model` value and has additional proxy requirements. Compare [AI engines for GitHub Agentic Workflows](/gh-aw/reference/engines/) before selecting one.

### Can I run Claude Code on a schedule in GitHub Actions using gh-aw?

Yes. Set `engine: claude` in workflow frontmatter, configure `ANTHROPIC_API_KEY` or [Anthropic Workload Identity Federation](/gh-aw/reference/auth/#anthropic-workload-identity-federation-wif), and add a schedule trigger such as `on: schedule: daily`. The compiled GitHub Actions workflow runs Claude Code in the configured agent container. See [Using Claude Code with GitHub Agentic Workflows](/gh-aw/engines/claude/).

### Can agentic workflows write code and create pull requests?

Yes. The recommended `create-pull-request` safe output proposes code changes, documentation updates, or other modifications for human review through a separate, permission-scoped job. Direct write permissions and custom jobs are possible but are separate trust boundaries. If an organization blocks PR creation from GitHub Actions, workflows can still generate diffs or suggestions in issues or comments for manual application.

### Can agentic workflows do more than code?

Yes — analyze repositories, generate reports, triage issues, research information, create documentation, and coordinate work. The AI interprets natural language instructions and uses available [tools](/gh-aw/reference/tools/) to accomplish tasks.

### Can agentic workflows mix regular GitHub Actions steps with AI agentic steps?

Yes. Add custom steps before the agentic job via [`steps:`](/gh-aw/reference/steps-jobs/#custom-steps-steps), consume agentic outputs through [custom safe output jobs](/gh-aw/reference/safe-outputs/#custom-safe-output-jobs-jobs), and pass typed data between steps and the agent with [MCP Scripts](/gh-aw/reference/mcp-scripts/).

### Can agentic workflows read other repositories?

Yes, with a **Personal Access Token (PAT)** that has access to target repositories, configured in your workflow. See [MultiRepoOps](/gh-aw/patterns/multi-repo-ops/) for coordinating across repositories, including running workflows from a separate side repository.

### Can I use agentic workflows in private repositories?

Yes. Private repositories can support proprietary code, a "sidecar" repository with limited access, workflow testing, and organization-internal automation. See [MultiRepoOps — Side Repository](/gh-aw/patterns/multi-repo-ops/#using-a-side-repository) for patterns using private repositories.

### Can I edit workflows directly on GitHub.com without recompiling?

Yes, for the **markdown body** (AI instructions) — loaded at runtime, takes effect on the next run. **Frontmatter** (tools, permissions, triggers, network rules) is embedded at compile time and requires `gh aw compile my-workflow` after edits. See [Editing Workflows](/gh-aw/guides/working-with-workflows/#editing-workflows).

### Can workflows trigger other workflows?

Yes, using the `dispatch-workflow` safe output (default `max: 1`):

```yaml wrap
safe-outputs:
  dispatch-workflow:
    max: 1
```

See [Safe Outputs](/gh-aw/reference/safe-outputs/#workflow-dispatch-dispatch-workflow).

### Can I trigger an agentic workflow from an external system like Jira?

Yes. Any system that can make an HTTP request — Jira, PagerDuty, Slack, custom APIs — can trigger a workflow via the [`repository_dispatch`](https://docs.github.com/en/actions/writing-workflows/choosing-when-your-workflow-runs/events-that-trigger-workflows#repository_dispatch) API. Add the trigger to your workflow and access the payload via `${{ github.event.client_payload.* }}`:

```yaml wrap
on:
  repository_dispatch:
    types: [jira-issue-created]
```

Then `POST` to the dispatch API from the external system:

```http
POST https://api.github.com/repos/<owner>/<repo>/dispatches
Authorization: Bearer <token>
Content-Type: application/json

{
  "event_type": "jira-issue-created",
  "client_payload": { "issue_key": "PROJ-123", "summary": "Fix the thing" }
}
```

For Jira, use **Project → Automation → Issue created → Send web request**. The token needs `repo` scope (classic PAT) or `contents: write` permission, stored in the external system's secret store and scoped to the target repository.

See [Repository Dispatch Trigger](/gh-aw/reference/triggers/#repository-dispatch-trigger-repository_dispatch). For runtime branch control from Jira issue content, see [Can the agent use an existing branch specified at runtime?](#can-the-agent-use-an-existing-branch-specified-at-runtime-eg-from-a-jira-issue)

### Can I use MCP servers with agentic workflows?

Yes — [Model Context Protocol (MCP)](/gh-aw/reference/glossary/#mcp-model-context-protocol) servers extend capabilities with custom tools. Configure them in frontmatter:

```yaml wrap
tools:
  mcp-servers:
    my-server:
      image: "ghcr.io/org/my-mcp-server:latest"
      network:
        allowed: ["api.example.com"]
```

See [Using MCPs](/gh-aw/guides/mcps/).

### How do I use mirrored or approved container images on self-hosted runners?

Use the existing image override paths instead of raw `sandbox.agent.args`:

- For repository-wide container substitutions (tooling images, MCP images, and default AWF tags), configure `.github/workflows/aw.json` `container_pins`.
- For AWF infrastructure roles specifically (for example `squid`, `agent`, `apiProxy`, `cliProxy`, `buildTools`), configure `sandbox.agent.images` with digest-pinned references.

```json title=".github/workflows/aw.json"
{
  "container_pins": {
    "ghcr.io/github/github-mcp-server:v1.10.1": {
      "image": "registry.example.com/github-mcp-server:v1.10.1",
      "digest": "sha256:3123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    }
  }
}
```

```aw wrap
sandbox:
  agent:
    version: v0.28.8
    images:
      squid: registry.example.com/approved/squid:v0.28.8@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
      agent: registry.example.com/approved/agent:v0.28.8@sha256:1123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
      apiProxy: registry.example.com/approved/api-proxy:v0.28.8@sha256:2123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
```

> [!NOTE]
> The digest values in this example are placeholders. Replace them with the exact digests from your approved registry images.

See [Self-Hosted Runners](/gh-aw/reference/self-hosted-runners/#action-and-container-substitutions-awjson) and [Sandbox custom infrastructure images](/gh-aw/reference/sandbox/#custom-infrastructure-images-sandboxagentimages).

### If my agent can use a skill, can agentic workflows use it too?

Usually yes. Prefer frontmatter [`skills:`](/gh-aw/reference/frontmatter/#frontmatter-skills-skills) to install skills for workflow runs: use local paths (for example, `skills/name` or `.github/skills/name`) during development and pinned external references for published workflows. Use [imports](/gh-aw/reference/imports/) for workflow-level config and prompts, and [APM (Agent Package Manager)](https://microsoft.github.io/apm/) for reusable package distribution of skills and other agent primitives. See [APM Dependencies](/gh-aw/reference/dependencies/).

### How do I install agent plugins in a workflow?

Use top-level [`plugins:`](/gh-aw/reference/frontmatter/#agent-plugins-plugins). gh-aw resolves plugin refs to immutable SHAs at compile time and installs them through the selected engine at runtime:

```yaml wrap
engine: copilot
plugins:
  - octo-org/agent-plugin@v1
  - plugin: octo-org/private-agent-plugin@0123456789abcdef0123456789abcdef01234567
    github-token: ${{ secrets.PRIVATE_PLUGIN_TOKEN }}
```

`imports` with package ecosystems such as [Microsoft APM](https://microsoft.github.io/apm/) are still supported for reusable distributions, but `plugins:` remains the direct workflow field for declaring Agent Plugins.

### Can I use Claude plugins with APM?

Yes. When `engine: claude` is set, APM infers the engine target and unpacks only Claude-compatible primitives. See [APM Dependencies](/gh-aw/reference/dependencies/).

### Can workflows be broken up into shareable components?

Yes — import shared configurations:

```yaml wrap
imports:
  - shared/github-tools.md
  - githubnext/agentics/shared/common-tools.md
```

See [Imports](/gh-aw/reference/imports/) and [Adding Existing Workflows](/gh-aw/guides/working-with-workflows/#adding-existing-workflows).

### Can I run workflows on a schedule?

Yes, use fuzzy schedule expressions in the `on:` trigger (recommended):

```yaml wrap
on: weekly on monday  # Automatically scattered to avoid load spikes
```

Or use standard cron syntax for fixed times:

```yaml wrap
on:
  schedule:
    - cron: "0 9 * * MON"  # Every Monday at 9am UTC
```

See [Schedule Syntax](/gh-aw/reference/schedule-syntax/) for all supported formats.

### Can I run workflows conditionally?

Yes, use the `if:` expression at the workflow level:

```yaml wrap
if: github.event_name == 'push' && github.ref == 'refs/heads/main'
```

See [Conditional Execution](/gh-aw/reference/frontmatter/#conditional-execution-if) in the Frontmatter Reference for details.

### How should I configure Go caches safely in agentic workflows?

For Go workflows, cache module downloads and build artifacts explicitly, and scope cache keys tightly:

```yaml wrap
cache:
  key: go-${{ runner.os }}-${{ hashFiles('**/go.sum') }}
  path: |
    ~/go/pkg/mod
    ~/.cache/go-build
  restore-keys: |
    go-${{ runner.os }}-

jobs:
  setup:
    steps:
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
          cache: false
      - run: |
          echo "GOMODCACHE=$HOME/go/pkg/mod" >> "$GITHUB_ENV"
          echo "GOCACHE=$HOME/.cache/go-build" >> "$GITHUB_ENV"
```

Security guidance:

- Keep keys specific to OS and dependency lock state (`go.sum`) to reduce accidental cross-context restores.
- Do not share writeable cache keys across trust boundaries (for example, untrusted fork PR runs and protected branch runs).
- Never place secrets in `GOMODCACHE`/`GOCACHE`; these directories should contain only modules and build outputs.

## Guardrails

### Agentic workflows run in GitHub Actions. Can they access my repository secrets?

Not by default. The generated AI agent job starts with read-only GitHub permissions and does not automatically receive repository secrets. Workflow authors can explicitly pass credentials to engines, tools, MCP servers, custom steps, or custom jobs, so review those paths carefully. Follow [GitHub Actions security guidelines](https://docs.github.com/en/actions/reference/security/secure-use), use least-privilege permissions, inspect the compiled `.lock.yml`, and minimize tools equipped with highly privileged secrets. See the [GitHub Agentic Workflows security architecture](/gh-aw/introduction/architecture/).

### Agentic workflows run in GitHub Actions. Can they write to the repository?

The generated agent step runs read-only by default. The recommended write path is explicit [safe outputs](/gh-aw/reference/safe-outputs/): limited operations that are sanitized and applied in separate jobs. Workflow authors can instead grant direct `write` permissions or define custom jobs, but those choices create separate trust boundaries and are not constrained by the safe-output list.

### What sanitization is done on AI outputs before applying changes?

Configured safe outputs are sanitized before being applied, including secret redaction, URL domain filtering, XML escaping, size limits, control character stripping, GitHub reference escaping, and HTTPS enforcement. Built-in safe-output writes happen in separate jobs with scoped permissions rather than in the agent job. Direct write permissions and custom jobs do not inherit all safe-output protections. See [Text Sanitization](/gh-aw/reference/safe-outputs/#text-sanitization-allowed-domains-allowed-github-references).

### How do I prevent workflow output from creating backlinks in referenced issues?

GitHub creates "mentioned in..." timeline entries when content references issue/PR numbers like `#123` or `owner/repo#456`. Set `allowed-github-references: []` to wrap all references in backticks so GitHub doesn't resolve them — useful when writing about a main repo from a sidecar:

```yaml wrap
safe-outputs:
  allowed-github-references: []   # escape all references
  create-issue:
```

Use `[repo]` to allow only same-repo references. Default (unset) leaves all references unescaped. See [Text Sanitization](/gh-aw/reference/safe-outputs/#text-sanitization-allowed-domains-allowed-github-references).

### How are agent actions constrained — commenting, opening PRs, modifying files, and calling external tools?

The default `gh-aw` agent job uses defense-in-depth with four configurable layers:

1. **Read-only agent by default** — no comments, PRs, or pushes unless you configure [safe outputs](/gh-aw/reference/safe-outputs/).
2. **Safe outputs for configured writes** — separate jobs with scoped write tokens apply sanitized changes from a structured artifact produced by the agent.
3. **Threat detection before writes** — when enabled, [agentic threat detection](/gh-aw/reference/threat-detection/) runs between the agent and safe-output jobs, blocking writes on prompt injection, secret leaks, or malicious patches.
4. **Network allowlist** — when enabled, the [Agent Workflow Firewall](/gh-aw/reference/sandbox/) blocks outbound traffic except to allowed domains.

Workflow authors can change these defaults, grant direct write permissions, or add custom jobs. For sensitive operations, add a [GitHub Environment protection rule](#can-i-require-external-human-approval-before-safe-outputs-are-applied) so a reviewer must approve before write jobs run. Compilation-time validation and tool allowlisting add further defense; see the [security architecture](/gh-aw/introduction/architecture/).

### Can I require external human approval before safe outputs are applied?

Yes. Apply **[GitHub Environment protection rules](https://docs.github.com/en/actions/managing-workflow-runs-and-deployments/managing-deployments/managing-environments-for-deployment#required-reviewers)** to a [custom safe output job](/gh-aw/reference/custom-safe-outputs/) that the built-in `safe_outputs` job depends on. The job pauses until a designated reviewer approves, enforced by GitHub's infrastructure rather than workflow logic the agent could influence. When threat detection is enabled, it runs before the gate so reviewers see output that passed automated scanning.

```yaml wrap
jobs:
  approval-gate:
    runs-on: ubuntu-latest
    needs: detection
    environment: production-deploy   # configure reviewers in Settings → Environments
    steps:
      - run: echo "Execution approved"

safe-outputs:
  needs: [approval-gate]
```

For **fully off-platform admission control** (an external policy engine, PAM/PIM, or compliance workflow), call that system from the gate job — if the call fails or is denied, the safe output jobs never run:

```yaml wrap
jobs:
  external-admission:
    runs-on: ubuntu-latest
    needs: [agent, detection]
    steps:
      - name: Request admission
        env:
          POLICY_TOKEN: ${{ secrets.POLICY_TOKEN }}
        run: |
          curl --fail -X POST https://YOUR_POLICY_ENGINE/v1/admit \
            -H "Authorization: Bearer $POLICY_TOKEN" \
            -d '{"workflow_run": "${{ github.run_id }}"}'

safe-outputs:
  needs: [external-admission]
```

### How is my code and data processed?

The workflow runs on GitHub Actions and invokes the selected [AI engine](/gh-aw/reference/engines/) in a container that may make tool and MCP calls. Data handling depends on the engine: GitHub Copilot CLI uses GitHub Copilot services; Claude Code, Codex, and Gemini use their configured provider services; Pi uses the provider selected by its `provider/model` value. See the [security architecture](/gh-aw/introduction/architecture/) for execution and data-flow details.

### Does the underlying AI engine run in a sandbox?

By default, the [AI engine](/gh-aw/reference/engines/) runs in an agent container inside a GitHub Actions VM with container isolation, Actions resource constraints, limited filesystem mounts, and network egress control through the [Agent Workflow Firewall](/gh-aw/reference/sandbox/). Sandbox and firewall settings are configurable, so explicit opt-outs broaden the agent's access. See [Sandbox Configuration](/gh-aw/reference/sandbox/).

### Can an agentic workflow use outbound network requests?

Yes. When enabled, the [Agent Workflow Firewall](/gh-aw/reference/sandbox/) blocks outbound traffic by default; declare allowed domains:

```yaml wrap
network:
  allowed:
    - defaults             # basic infrastructure
    - python               # PyPI
    - "api.example.com"    # custom domain
```

See [Network Permissions](/gh-aw/reference/network/).

### How does integrity filtering protect my workflow?

[Integrity filtering](/gh-aw/reference/integrity/) controls which GitHub content the agent sees, filtering by **author trust** and **merge status**. The MCP gateway removes content below the configured `min-integrity` threshold before the agent sees it.

For **public repositories**, `min-integrity: approved` is auto-applied at runtime — restricting content to owners, members, and collaborators. For triage or spam-detection workflows that need all users' content, set `min-integrity: none` explicitly:

```yaml wrap
tools:
  github:
    min-integrity: none
```

See [Integrity Filtering](/gh-aw/reference/integrity/).

## Configuration & Setup

### Why do slash-command workflows show many "started then skipped" runs on comments?

Expected behavior. A `slash_command` compiles into multiple event listeners (issue/PR bodies, comments, review comments). GitHub dispatches every event, then activation logic checks whether the comment starts with the matching command — non-matches exit early and appear as skipped runs. Narrow the trigger with `events:`, and use [LabelOps](/gh-aw/patterns/label-ops/) for command-style operations that shouldn't activate on every comment:

```yaml wrap
on:
  slash_command:
    name: refresh
    events: [pull_request_comment]   # only listen to PR comments
  label_command:
    name: refresh
    events: [pull_request]           # optional low-noise label trigger
```

### What is a workflow lock file?

The `.lock.yml` file is the compiled GitHub Actions workflow generated from your `.md` by `gh aw compile`. It contains SHA-pinned actions, resolved imports, permissions, and all guardrail hardening — inspect it to see exactly what will run. Commit both files:

- **`.md`**: source; edit the prompt body freely without recompiling
- **`.lock.yml`**: what GitHub Actions runs; regenerate after any frontmatter change

### What is the actions-lock.json file?

The `.github/aw/actions-lock.json` file caches resolved `action@version` → ref mappings. The compiler tries to pin each action to an immutable commit SHA, but resolving a tag to a SHA requires GitHub API access — which can fail under limited-permission tokens (e.g., Copilot Coding Agent). The cache reuses previously resolved refs regardless of the current token's capabilities; without it, compilation is unstable.

Commit it to version control. Refresh with `gh aw update-actions`, or delete and recompile to force re-resolution. See [Action Pinning](/gh-aw/reference/compilation-process/#action-pinning).

### What is `github/gh-aw-actions`?

The GitHub Actions repository containing all reusable actions that power compiled agentic workflows. Compiled `.lock.yml` files reference them as `github/gh-aw-actions/setup@<ref>` (usually a SHA, sometimes a stable version tag). Managed entirely by `gh aw compile` — never edit manually. See [The gh-aw-actions Repository](/gh-aw/reference/compilation-process/#the-gh-aw-actions-repository).

### Why is Dependabot opening PRs to update `github/gh-aw-actions`?

Dependabot scans `.lock.yml` files for action references and treats `github/gh-aw-actions` pins as regular dependencies to update. **Do not merge these PRs.** Action pins in compiled workflows should only be updated by running `gh aw compile` or `gh aw update-actions`.

Suppress these PRs by adding an `ignore` entry in `.github/dependabot.yml`:

```yaml
updates:
  - package-ecosystem: github-actions
    directory: "/.github/workflows"
    ignore:
      - dependency-name: "github/gh-aw-actions/*" # Managed by gh aw compile. Version-locked to the gh-aw compiler; do not bump.
```

See [Dependabot and gh-aw-actions](/gh-aw/reference/compilation-process/#dependabot-and-gh-aw-actions) for more details.

### How does `gh aw upgrade` resolve action versions when no GitHub Releases exist?

`gh aw upgrade` (and `gh aw update-actions`) tries the **GitHub Releases API** first via the `gh` CLI; if no releases exist, it falls back to **git tags** via `git ls-remote`. Tags are a valid source for version pinning, so the fallback is safe to ignore. A warning appears only when both sources are empty.

`github/gh-aw-actions` intentionally publishes only tags. The earlier `no releases found` warning has been fixed — the tag fallback now runs automatically.

### Why do I need a token or key?

**GitHub Copilot CLI** can use the recommended `copilot-requests: write` workflow permission with organization billing, or a fine-grained Personal Access Token stored as `COPILOT_GITHUB_TOKEN`. Claude Code, Codex, Gemini, and Pi require their documented provider authentication. See [AI engine authentication](/gh-aw/reference/auth/).

### Can I use `CLAUDE_CODE_OAUTH_TOKEN` with the Claude engine?

No. The Claude engine supports [`ANTHROPIC_API_KEY`](/gh-aw/reference/auth/#anthropic_api_key) or [Anthropic Workload Identity Federation](/gh-aw/reference/auth/#anthropic-workload-identity-federation-wif). Claude subscription OAuth tokens such as `CLAUDE_CODE_OAUTH_TOKEN` are not supported. See [Using Claude Code with GitHub Agentic Workflows](/gh-aw/engines/claude/).

### What hidden runtime dependencies does this have?

None hidden — the executing workflow uses your chosen coding agent (default: Copilot CLI), a GitHub Actions VM with NodeJS, pinned Actions from [github/gh-aw](https://github.com/github/gh-aw) releases, and the Agent Workflow Firewall container (optional but default). The compiled `.lock.yml` shows the exact YAML.

### Why are macOS runners not supported?

macOS runners (`macos-*`) don't support container jobs, which agentic workflows require for the [Agent Workflow Firewall](/gh-aw/reference/sandbox/) sandbox. Use `ubuntu-latest` or another Linux runner. For genuine macOS-only tooling, run those steps in a separate regular GitHub Actions job that coordinates with your agentic workflow.

### Can I use agentic workflows on GitHub Enterprise Server (GHES)?

Yes, but enable GHES compatibility mode on instances predating `@actions/artifact` v2.0.0 — otherwise compiled workflows fail with `GHESNotSupportedError` because the compiler emits `upload-artifact@v4+` by default. Compatibility mode emits `v3.2.2`/`v3.1.0` instead:

```json
// aw.json (applies to all workflows)
{ "ghes": true }
```

```bash
# or one-off:
gh aw compile --ghes my-workflow.md
```

`gh aw init` auto-detects GHES and writes `ghes: true` for you. See [Enterprise Configuration](/gh-aw/reference/enterprise-configuration/) for CLI and Copilot prerequisites.

### I'm not using a supported AI Engine (coding agent). What should I do?

Built-in engines are GitHub Copilot, Claude Code, OpenAI Codex, Google Gemini, and Pi. Unsupported engine samples also demonstrate how custom integrations are structured. Contribute support to the [GitHub Agentic Workflows repository](https://github.com/github/gh-aw) or open an issue describing the use case. See [AI engines for GitHub Agentic Workflows](/gh-aw/reference/engines/).

### Can I test workflows without affecting my repository?

Yes — use [TrialOps](/gh-aw/experimental/trial-ops/) to run workflows in isolated trial repositories without creating real issues, PRs, or comments.

### Where can I find help with common issues?

See [Common Issues](/gh-aw/troubleshooting/common-issues/).

### Why is my create-discussion workflow failing?

Ensure discussions are enabled (**Settings → Features → Discussions**) and the workflow has `discussions: write` permission. For category matching failures, verify spelling (case-insensitive) and use lowercase slugs (e.g., `general`, `announcements`) rather than display names.

Use `fallback-to-issue: true` (the default) to automatically create an issue if discussions aren't available. See [Discussion Creation](/gh-aw/reference/safe-outputs/#discussion-creation-create-discussion) for details.

### How do I enable discussions in add-comment?

By default, `add-comment` does not request `discussions: write` — the permission is opt-in. To comment on discussions, set `discussions: true`:

```yaml wrap
safe-outputs:
  add-comment:
    discussions: true
```

`issues: write` and `pull-requests: write` are requested by default; opt out per-permission with `issues: false` or `pull-requests: false`.

### Why is my create-pull-request workflow failing with "GitHub Actions is not permitted to create or approve pull requests"?

Some organizations block PR creation by GitHub Actions (**Settings → Actions → General → Workflow permissions**). If you can't enable it:

- **Automatic issue fallback (default)**: `fallback-as-issue: true` creates an issue with the branch link when PR creation is blocked. Requires `contents: write`, `pull-requests: write`, `issues: write`.
- **Assign to Copilot**: create an issue assigned to `copilot` for automated implementation (`assignees: [copilot]` under `create-issue`).
- **Disable fallback**: set `fallback-as-issue: false` to fail when PR creation is blocked (requires only `contents: write` and `pull-requests: write`).

See [Pull Request Creation](/gh-aw/reference/safe-outputs/#pull-request-creation-create-pull-request).

### Why don't pull requests created by agentic workflows trigger my CI checks?

PRs created with the default `GITHUB_TOKEN` or the GitHub Actions bot don't trigger `pull_request`, `pull_request_target`, or `push` workflows — a [GitHub Actions security feature](https://docs.github.com/en/actions/security-for-github-actions/security-guides/automatic-token-authentication#using-the-github_token-in-a-workflow) preventing recursive execution. Fix by setting `GH_AW_CI_TRIGGER_TOKEN` to a PAT with 'Contents: Read & Write'. See [Triggering CI](/gh-aw/reference/triggering-ci/).

### How do I suppress the "Generated by..." text in workflow outputs?

Set `footer: false` to hide the `> Generated by [Workflow](run_url) for issue #N` line while preserving the hidden XML markers used for search:

```yaml wrap
safe-outputs:
  footer: false            # hide for all
  create-pull-request:
    footer: true           # override per type
```

The hidden `<!-- gh-aw-workflow-id: ... -->` marker remains — search GitHub for `"gh-aw-workflow-id: my-workflow" in:body`. See [Footer Control](/gh-aw/reference/footers/).

### My workflow fails with "Runtime import file not found" when used in a repository ruleset

Required-status-check workflows run without filesystem access, so runtime imports can't be resolved. Set `inlined-imports: true` in frontmatter to bundle imports into `.lock.yml` at compile time. See [Self-Contained Lock Files](/gh-aw/reference/imports/#self-contained-lock-files-inlined-imports-true).

### My cross-organization `workflow_call` fails with a repository checkout error

The activation job tries to check out the callee's `.github` folder with the caller's `GITHUB_TOKEN`, which can't access a private repo in another organization (`fatal: repository '...' not found`). Set `inlined-imports: true` on the **platform workflow** (callee) to embed imports at compile time and eliminate the cross-org checkout:

```yaml
---
on:
  workflow_call:
engine: copilot
inlined-imports: true
imports:
  - shared/common-tools.md
---
```

See [Self-Contained Lock Files](/gh-aw/reference/imports/#self-contained-lock-files-inlined-imports-true).

### My workflow checkout is very slow because my repository is a large monorepo. How can I speed it up?

Use **sparse checkout** in the `checkout:` field to fetch only the paths your workflow needs — often reducing checkout from minutes to seconds:

```yaml wrap
checkout:
  sparse-checkout: |
    node/my-package
    .github
```

For multiple paths with different settings, combine checkouts:

```yaml wrap
checkout:
  - sparse-checkout: |
      node/my-package
      .github
  - repository: org/shared-libs
    path: ./libs/shared
    sparse-checkout: |
      defaults/
```

The `sparse-checkout` field accepts newline-separated path patterns compatible with `actions/checkout`. See [GitHub Repository Checkout](/gh-aw/reference/checkout/#configuration-options) for the full list of checkout configuration options.

## Workflow Design

### Should I focus on one workflow, or write many different ones?

One workflow is simpler to maintain; multiple workflows give better separation of concerns, per-task triggers and permissions, and clearer audit trails. Start with one or two and expand as patterns emerge. See [Peli's Agent Factory](/gh-aw/blog/2026-01-12-welcome-to-pelis-agent-factory/).

### Should I create agentic workflows by hand editing or using AI?

Both work. AI-assisted authoring gives interactive guidance and best practices; manual editing gives full control for advanced customization.

- **GitHub Copilot users**: after running `gh aw init`, use `agentic-workflows create` in Copilot Chat on github.com or the GitHub mobile app.
- **Claude Code and other CLI agent users**: run `gh aw init --engine claude` to initialize (skips Copilot-specific files), then use the `create.md` prompt directly — no Copilot subscription required:

  ```text wrap
  Create a workflow for GitHub Agentic Workflows using https://raw.githubusercontent.com/github/gh-aw/main/create.md

  The purpose of the workflow is <your goal here>.
  ```

See [Creating Workflows](/gh-aw/setup/creating-workflows/) or [Frontmatter Reference](/gh-aw/reference/frontmatter/).

### Can the agent use an existing branch specified at runtime (e.g., from a Jira issue)?

`create-pull-request` always creates a new branch, but you can control the name and reuse an existing remote branch:

```yaml wrap
safe-outputs:
  create-pull-request:
    preserve-branch-name: true   # use agent name as-is, no random suffix
    recreate-ref: true           # force-reset remote branch if it exists
```

To pass the branch name from a Jira issue body (or any issue body), instruct the agent in markdown:

```markdown
Read the issue body and extract the branch name from the line starting with
"Use existing branch:". Use that name when calling `create_pull_request`.
```

The agent has the issue body in context, so no extra integration is needed. For richer Jira data (status, custom fields), use a [custom safe output](/gh-aw/reference/custom-safe-outputs/) or Jira MCP server.

> [!NOTE]
> `recreate-ref` requires `preserve-branch-name: true`. The agent always starts from the configured base branch — it doesn't check out the named branch before making changes.

See [Safe Outputs (Pull Requests)](/gh-aw/reference/safe-outputs-pull-requests/).

### Are an AI agent and an agentic workflow the same thing?

No. An **AI agent** is the reasoning component executed by an AI engine. An **agentic workflow** is the Markdown-defined repository automation that configures when and how that agent runs through GitHub Actions, including its tools, permissions, and controlled outputs. Some interfaces may use "agent" as shorthand for a repository workflow, but the concepts are distinct.

### How do I forward agent and detection artifacts to a third-party server after the workflow finishes?

Add a custom job with `needs: [conclusion]` in the frontmatter `jobs:` block. The `conclusion` job is the last auto-generated job, so depending on it guarantees both `agent` and `detection` artifacts are fully uploaded:

```yaml wrap
jobs:
  forward-artifacts:
    needs: [conclusion]
    if: always()
    runs-on: ubuntu-latest
    steps:
      - uses: actions/download-artifact@v4
        with:
          name: agent
          path: artifacts/agent
      - uses: actions/download-artifact@v4
        with:
          name: detection
          path: artifacts/detection
        continue-on-error: true
      - name: Upload to third-party server
        env:
          INGEST_TOKEN: ${{ secrets.INGEST_TOKEN }}
        run: |
          tar -czf artifacts.tar.gz artifacts/
          curl --fail --retry 3 -X POST https://ingest.example.com/artifacts \
            -H "Authorization: ******" \
            -F "file=@artifacts.tar.gz" \
            -F "run_id=${{ github.run_id }}"
```

`if: always()` runs the job even on upstream failure. The `detection` artifact only exists when [threat detection](/gh-aw/reference/threat-detection/) is enabled — `continue-on-error: true` handles its absence. See [Artifacts](/gh-aw/reference/artifacts/) for artifact names and contents.

## Costs & Usage

### Who pays for the use of AI?

Depends on the engine:

- **GitHub Copilot CLI** (default): organization billing through `copilot-requests: write`, or the account supplying [`COPILOT_GITHUB_TOKEN`](/gh-aw/reference/auth/#copilot_github_token).
- **Claude Code**: the Anthropic account tied to [`ANTHROPIC_API_KEY`](/gh-aw/reference/auth/#anthropic_api_key), or the account configured for Anthropic WIF.
- **OpenAI Codex**: the OpenAI account tied to `CODEX_API_KEY` or [`OPENAI_API_KEY`](/gh-aw/reference/auth/#openai_api_key).
- **Google Gemini**: the Google account tied to [`GEMINI_API_KEY`](/gh-aw/reference/auth/#gemini_api_key), or the Google Cloud account configured for WIF.
- **Pi**: the account for the provider selected in the `provider/model` value.

### Does gh-aw add any cost beyond what the AI engine charges?

No. gh-aw itself is free and open source. You pay only your AI provider's standard inference rates (or consume Copilot quota) plus the GitHub Actions compute minutes for the run. See [Billing](/gh-aw/reference/billing/) for a detailed breakdown.

### What's the approximate cost per workflow run?

Costs vary by workflow complexity, model, and execution time. Track usage with `gh aw logs`, `gh aw audit <run-id>`, or your AI provider's portal — use separate PAT/API keys per repository for granular tracking. Reduce costs by optimizing prompts, using smaller models, limiting tool calls, reducing run frequency, and caching results.

### Are GitHub Actions minutes charged in addition to AI costs?

Yes — every run consumes Actions minutes (free for public repos, metered for private) alongside AI inference. Set an [org spending limit](https://docs.github.com/en/billing/managing-billing-for-your-products/managing-billing-for-github-actions/managing-your-spending-limit-for-github-actions) to cap Actions spend. AI inference is billed separately (see [Who pays for the use of AI?](#who-pays-for-the-use-of-ai)).

### How do retries and agent loops affect costs?

gh-aw has no automatic retries — each trigger produces exactly one run. Control reasoning depth and continuation to bound tokens and wall-clock time:

- `max-turns` (Claude) — limits AI chat iterations per run
- `max-continuations` (Copilot) — autopilot mode with consecutive triggered runs

```yaml
engine:
  id: claude
max-turns: 5
```

For scheduled workflows, run frequency is the primary cost lever — an hourly schedule adds up quickly.

### How do I control spend and set budgets?

Spend controls live at the provider level:

- **Actions minutes**: org spending limit in GitHub Billing.
- **Claude / Codex / Gemini**: API key or project-level limits in Anthropic Console / OpenAI platform.
- **Copilot**: quota-based — the plan's monthly request quota is the natural cap.

For per-repository tracking, use a dedicated API key per repository. Use `gh aw audit <run-id>` for per-run detail and `gh aw logs` for aggregate metrics.

### Can I change the model being used, e.g., use a cheaper or more advanced one?

Yes — set the model in frontmatter, or switch engines:

```yaml wrap
engine:
  id: copilot
  model: gpt-5                    # or claude-sonnet-4
```

```yaml wrap
engine: claude
```

See [AI Engines](/gh-aw/reference/engines/).

### How do I supply pricing for a custom or private model?

gh-aw computes AI Credits (AIC) using pricing data from the [models.dev](https://models.dev/) catalog. If your workflow uses a custom, private, or enterprise model that isn't in the catalog — or if you want to override the built-in price — add a `models.providers` entry to your frontmatter:

```yaml wrap
models:
  providers:
    openai:
      models:
        gpt-5-enterprise:
          cost:
            input: "3.75e-06"     # $3.75 per million input tokens
            output: "1.5e-05"     # $15.00 per million output tokens
            cache_read: "9.375e-07"   # optional — omit if caching is not supported
            cache_write: "3.75e-06"  # optional
```

Cost values are **per-token USD in scientific notation** (e.g. `3.75e-06` = $0.00000375/token = $3.75 per million tokens). Omit `cache_read` and `cache_write` if the model doesn't support prompt caching.

Use the provider key that matches your engine:

| Engine | Provider key |
|---|---|
| `copilot` | `github-copilot` |
| `claude` | `anthropic` |
| `codex` | `openai` |
| `gemini` | `google` |

These entries are **merged** with the built-in catalog at runtime — they override matching models and fill in gaps for unknown ones, so AIC accounting stays accurate. In shared workflows imported by others, `models.providers` entries merge as unions across all imports.

See [Token Optimization — Capping Spend](/gh-aw/reference/cost-management/) for budgeting options alongside custom pricing.

## Learn More

- [GitHub Agentic Workflows quickstart](/gh-aw/setup/quick-start/) — install `gh-aw` and run a first workflow
- [Create a GitHub Agentic Workflow](/gh-aw/setup/creating-workflows/) — author Markdown instructions and compile them into GitHub Actions
- [AI Issue Triage on GitHub](/gh-aw/gallery/ai-issue-triage/) — labeling, deduplication, and clarifying questions
- [Automated AI Pull Request Review](/gh-aw/gallery/automated-pr-review/) — review diffs and post feedback on new PRs
- [AI engines for GitHub Agentic Workflows](/gh-aw/reference/engines/) — compare Copilot, Claude Code, Codex, Gemini, and Pi
- [GitHub Agentic Workflows security architecture](/gh-aw/introduction/architecture/) — understand configurable permissions, isolation, and controlled writes

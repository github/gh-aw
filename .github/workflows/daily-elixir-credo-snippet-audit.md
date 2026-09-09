---
private: true
emoji: "🧪"
name: Daily Elixir Credo Snippet Audit
description: Uses Credo daily to lint Elixir snippets in repository markdown and proposes fixes
on:
  schedule:
    - cron: "daily around 8:00 on weekdays"
  workflow_dispatch:
max-daily-ai-credits: 10000
permissions:
  contents: read
  issues: read
  pull-requests: read
tracker-id: daily-elixir-credo-snippet-audit
engine:
  id: claude
timeout-minutes: 45
strict: true
runtimes:
  elixir:
    version: "1.17"
network:
  allowed:
    - defaults
    - hex.pm
    - repo.hex.pm
    - builds.hex.pm
tools:
  cli-proxy: true
  github:
    mode: local
    toolsets: [default]
  bash:
    - "*"
  edit:
imports:
  - uses: shared/daily-pr-base.md
    with:
      title-prefix: "[elixir-credo] "
      expires: 3d
      labels: [automation, elixir, docs]
      reviewers: [copilot]
sandbox:
  agent:
    id: awf
    runtime: cloud-hypervisor
steps:
  - name: Install Credo tooling project
    run: |
      set -euo pipefail
      mkdir -p /tmp/gh-aw/agent/elixir-credo
      cd /tmp/gh-aw/agent/elixir-credo
      mix new credo_audit --sup
      cd credo_audit
      cat > mix.exs <<'EOF'
      defmodule CredoAudit.MixProject do
        use Mix.Project

        def project do
          [
            app: :credo_audit,
            version: "0.1.0",
            elixir: "~> 1.17",
            start_permanent: Mix.env() == :prod,
            deps: deps()
          ]
        end

        def application do
          [
            extra_applications: [:logger]
          ]
        end

        defp deps do
          [
            {:credo, "~> 1.7", only: [:dev, :test], runtime: false}
          ]
        end
      end
      EOF
      MIX_ENV=dev mix deps.get
      MIX_ENV=dev mix deps.compile
      MIX_ENV=dev mix credo --version
features:
  gh-aw-detection: true
evals:
  - id: elixir_snippets_linted
    question: Did the agent use Credo to lint Elixir snippets found in repository markdown files?
  - id: fixes_proposed_or_noop
    question: Were fixes proposed in a PR or issue, or was noop used when no linting violations were found?
---

{{#runtime-import? .github/shared-instructions.md}}

# Daily Elixir Credo Snippet Audit

Use Credo to improve Elixir quality signals in this repository by linting Elixir
snippets embedded in markdown content.

## Context

- Repository: `${{ github.repository }}`
- Workspace: `${{ github.workspace }}`
- Credo project path: `/tmp/gh-aw/agent/elixir-credo/credo_audit`

The workflow pre-step has already installed Credo and dependencies. Use:

```bash
cd /tmp/gh-aw/agent/elixir-credo/credo_audit
MIX_ENV=dev mix credo --strict --format oneline
```

## Task

1. Find fenced Elixir snippets in markdown files under:
   - `docs/`
   - `.github/workflows/`
2. Materialize snippets into temporary `.exs` files under:
   `/tmp/gh-aw/agent/elixir-credo/snippets/`.
3. Run Credo in strict mode against those files.
4. If Credo reports actionable issues:
   - Prefer fixing the source markdown snippets directly.
   - Keep fixes minimal and preserve snippet intent.
5. If no actionable fixes are needed, use `noop` and include summary counts.

## Constraints

- Do not edit generated `.lock.yml` files.
- Keep changes scoped to markdown files containing Elixir snippets.
- If edits are made, create one PR with title prefix `[elixir-credo] ` using
  the configured safe output.

### Output Format

Use `###` or lower headings.

Structure reports as: overview → key metrics/issues → collapsible detail →
next actions.

Wrap long details in
`<details><summary><b>View Details</b></summary>...</details>`.
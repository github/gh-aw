---
# Squad Bootstrap — lazily installs and initializes Squad (https://github.com/bradygaster/squad)
# in the activation job when needed, then republishes the team state to the agent job.
#
# The Squad CLI is never installed or executed in the agent job — only the files it
# produces (`.squad/` team state and `.github/agents/squad.agent.md`) are restored there.
#
# Usage:
#   imports:
#     - shared/squad.md
#
# Optional custom credentials for `squad init` (only needed when Squad must reach other
# organizations or private repositories beyond the current one):
#   vars.SQUAD_GITHUB_APP_ID / secrets.SQUAD_GITHUB_APP_PRIVATE_KEY / vars.SQUAD_GITHUB_APP_OWNER
#     — mints a GitHub App installation token for `squad init`
#   secrets.SQUAD_GITHUB_TOKEN
#     — used if the App id is not set
# Auth precedence: GitHub App installation token > SQUAD_GITHUB_TOKEN > the workflow's
# own default token (`github.token`).
#
# Optional custom Squad CLI version (defaults to 0.11.0):
#   vars.SQUAD_CLI_VERSION
#
# No `copilot-sdk` sidecar is needed here: Copilot's `--agent squad` flag runs
# through the standard CLI harness, and the Squad CLI itself only ever runs in
# the activation job.
engine:
  id: copilot
  agent: squad
ambient-folders:
  - .squad
  - .github/agents
jobs:
  activation:
    steps:
      - name: Detect existing Squad installation
        id: squad-installation
        run: |
          if [ -f .squad/team.md ] && [ -f .github/agents/squad.agent.md ]; then
            echo "installed=true" >> "$GITHUB_OUTPUT"
          else
            echo "installed=false" >> "$GITHUB_OUTPUT"
          fi
      - name: Mint Squad GitHub App token
        id: squad-app-token
        if: ${{ steps.squad-installation.outputs.installed != 'true' && vars.SQUAD_GITHUB_APP_ID != '' }}
        uses: actions/create-github-app-token@v3.2.0
        with:
          app-id: ${{ vars.SQUAD_GITHUB_APP_ID }}
          private-key: ${{ secrets.SQUAD_GITHUB_APP_PRIVATE_KEY }}
          owner: ${{ vars.SQUAD_GITHUB_APP_OWNER }}
          permission-contents: read
      - name: Initialize Squad team
        if: ${{ steps.squad-installation.outputs.installed != 'true' }}
        env:
          SQUAD_CLI_VERSION: ${{ vars.SQUAD_CLI_VERSION }}
          GH_TOKEN: ${{ steps.squad-app-token.outputs.token || secrets.SQUAD_GITHUB_TOKEN || github.token }}
        run: npx --yes "@bradygaster/squad-cli@${SQUAD_CLI_VERSION:-0.11.0}" init --preset default

---

<!--

## Squad Bootstrap Component

This shared component moves the Squad (https://github.com/bradygaster/squad)
install/init lifecycle out of the agent job and performs it lazily:

1. **`jobs.activation.steps`** — the repository is already checked out by the
   activation job itself, so this first checks for the existing Squad team state
   and custom agent. When either is missing, it installs the pinned
   `@bradygaster/squad-cli` npm release, optionally mints a GitHub App installation
   token (or uses a supplied PAT) so `squad init` can see other organizations or
   private repositories, and runs `squad init --preset default`.
2. **`ambient-folders`** — bundles the resulting `.squad/` team state and
   `.github/agents/` files into the standard activation artifact alongside the rest
   of the prompt/skills/sub-agent packaging, then restores them into the agent
   checkout. The Squad CLI itself is never installed in the agent job; only the
   files it produced are copied in.

## Working with Squad

Squad's team state (`.squad/`) and its Copilot custom agent
(`.github/agents/squad.agent.md`) were reused or initialized during activation
and restored into this checkout before you started — do not install Squad or run
`squad init` yourself.

- Verify `.squad/team.md` exists before delegating work to the team. If it is
  missing, the activation-job bootstrap step failed — call `noop` and explain
  why instead of proceeding.
- Coordinate work through the Squad team already defined in `.squad/` rather
  than proposing a brand-new team from scratch.

-->

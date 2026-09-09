---
description: Core GitHub Actions frontmatter fields supported by GitHub Agentic Workflows.
---

## Complete Frontmatter Schema

The YAML frontmatter supports these fields:

### Core GitHub Actions Fields

- **`on:`** - Workflow triggers (required)
  - String: `"push"`, `"issues"`, etc.
  - Object: Complex trigger configuration
  - Special: `slash_command:` for /mention triggers
  - **`forks:`** - Fork allowlist for `pull_request` triggers (array or string). By default, workflows block all forks and only allow same-repo PRs. Use `["*"]` to allow all forks, or specify patterns like `["org/*", "user/repo"]`
  - **`stop-after:`** - Can be included in the `on:` object to set a deadline for workflow execution. Supports absolute timestamps ("YYYY-MM-DD HH:MM:SS"), relative time deltas (+25h, +3d, +1d12h), or a GitHub Actions expression (e.g. `${{ inputs.stop-after }}`) resolved at runtime instead of compile time. The minimum unit for relative/literal deltas is hours (h). Literal values use precise date calculations that account for varying month lengths; recompile with `gh aw compile --refresh-stop-time` to reset a literal deadline.
  - **`cooldown:`** - Can be included in the `on:` object to block the `agent` job from starting again shortly after the most recent completed run. Value must be a literal Go duration string of at least 5 minutes (e.g. `"30m"`, `"2h"`); GitHub Actions expressions are rejected. Fails open (allows activation) if run history cannot be queried.
  - **`reaction:`** - Add emoji reactions to triggering items
  - **`status-comment:`** - Post status comments when workflow starts/completes on the triggering issue, pull request, or discussion (boolean, or object with `issues`/`pull-requests`/`discussions` booleans to control trigger groups independently). Defaults to `true` for `slash_command` and `label_command` triggers; defaults to `false` for all other triggers. Must be explicitly enabled for non-command triggers with `status-comment: true`.
  - **`manual-approval:`** - Require manual approval using environment protection rules
  - **`skip-roles:`** - Skip workflow execution for users with specific repository roles (array)
    - Available roles: `admin`, `maintainer`/`maintain`, `write`, `triage`, `read`
    - Example: `skip-roles: [read]` - Skip execution for users with read-only access
  - **`skip-bots:`** - Skip workflow execution when triggered by specific GitHub actors (array)
    - Bot name matching is flexible (handles with/without `[bot]` suffix)
    - Example: `skip-bots: [dependabot, renovate]` - Skip for Dependabot and Renovate
  - **`skip-author-associations:`** - Skip workflow execution per-event based on the triggering payload's `author_association` (object keyed by event name, e.g. `issue_comment`, `issues`, `pull_request`; each value a string or array of associations like `CONTRIBUTOR`, case-insensitive)
    - Example: `skip-author-associations: { issue_comment: [first_time_contributor, contributor] }`
  - **`labels:`** - Filter label-triggered events to only fire when the triggering label matches one of these names (string or array)
    - String format: `labels: "my-label"` (single label name)
    - Array format: `labels: [label-a, label-b]` (any matching label fires the workflow)
    - Unmatched label events show as Skipped (⊘) rather than Failed (❌)
    - Use with `pull_request` triggers with `types: [labeled]` to respond only to specific labels
  - **`skip-if-match:`** - Skip workflow execution when a GitHub search query returns results (string or object)
    - String format: `skip-if-match: "is:issue is:open label:bug"` (implies max=1)
    - Object format with threshold:

      ```yaml
      skip-if-match:
        query: "is:issue is:open label:in-progress"
        max: 3      # Skip if 3 or more matches (default: 1)
        scope: none # Optional: disable automatic repo:owner/repo scoping for org-wide queries
      ```

    - Query is automatically scoped to the current repository (use `scope: none` for cross-repo queries)
    - Use to avoid duplicate work (e.g., skip if an open issue already exists)
  - **`skip-if-no-match:`** - Skip workflow execution when a GitHub search query returns no results (string or object)
    - String format: `skip-if-no-match: "is:pr is:open label:ready-to-deploy"` (implies min=1)
    - Object format with threshold:

      ```yaml
      skip-if-no-match:
        query: "is:pr is:open label:ready-to-deploy"
        min: 2      # Require at least 2 matches to proceed (default: 1)
        scope: none # Optional: disable automatic repo:owner/repo scoping for org-wide queries
      ```

    - Query is automatically scoped to the current repository (use `scope: none` for cross-repo queries)
    - Use to gate workflows on preconditions (e.g., only run if open PRs exist)
  - **`skip-if-check-failing:`** - Skip workflow execution when CI checks are failing on the triggering ref (boolean or object)
    - Boolean format: `skip-if-check-failing: true` (skip if any check is failing or pending)
    - Object format with filtering:

      ```yaml
      skip-if-check-failing:
        include:
          - build
          - test             # Only check these specific CI checks
        exclude:
          - lint             # Ignore this check
        branch: main         # Optional: check a specific branch instead of triggering ref
        allow-pending: true  # Optional: treat pending/in-progress checks as passing (default: false)
      ```

    - When `include` is omitted, all checks are evaluated
    - By default, pending/in-progress checks count as failing; set `allow-pending: true` to ignore them
    - Use to avoid running agents against broken code (e.g., skip PR review if CI is red)
  - **`github-token:`** - Custom GitHub token for pre-activation reactions, status comments, and skip-if search queries (string)
    - When specified, overrides the default `GITHUB_TOKEN` for these operations
    - Example: `github-token: ${{ secrets.MY_GITHUB_TOKEN }}`
  - **`github-app:`** - GitHub App credentials for minting a token used in pre-activation operations (object)
    - Mints a single installation access token shared across reactions, status comments, and skip-if queries
    - Can be defined in a shared agentic workflow and inherited by importing workflows
    - Fields:
      - `client-id:` - GitHub App client ID (required, e.g., `${{ vars.APP_ID }}`). Use `app-id:` for legacy compatibility.
      - `private-key:` - GitHub App private key (required, e.g., `${{ secrets.APP_PRIVATE_KEY }}`)
      - `owner:` - Optional installation owner (defaults to current repository owner)
      - `repositories:` - Optional list of repositories to grant access to
    - Example:

      ```yaml
      on:
        issues:
          types: [opened]
        github-app:
          client-id: ${{ vars.APP_ID }}
          private-key: ${{ secrets.APP_PRIVATE_KEY }}
      ```

  - **`stale-check:`** - Control whether the activation job verifies hashes against the compiled workflow (boolean or `"full"`, default: `true`)
    - When `false`, disables the hash check step; useful when workflow files are managed outside the default repository context (e.g., cross-repo org rulesets)
    - When `"full"`, checks both the frontmatter hash and body hash; use when prompt-body edits should also trigger recompilation detection

- **`github-app:`** - Top-level GitHub App credentials, used as a fallback for every nested `github-app` token-minting operation (`on.github-app`, `safe-outputs.github-app`, `checkout.github-app`, `tools.github.github-app`, `dependencies.github-app`) that does not define its own. Same fields as `on.github-app` above (`client-id`/`app-id`, `private-key`, `owner`, `repositories`).

- **`permissions:`** - GitHub token permissions
  - Object with permission levels: `read`, `none` (and limited `write` for specific scopes)
  - Common permission scopes (not exhaustive; standard GitHub Actions scopes plus `models`, `copilot-requests`): `contents`, `issues`, `pull-requests`, `discussions`, `actions`, `checks`, `statuses`, `models`, `deployments`, `security-events`, `packages`, `pages`, `attestations`, `copilot-requests`
  - Write permissions are not allowed for security reasons; use `safe-outputs` for write operations instead
  - Exceptions: `id-token: write` is allowed to enable OIDC token minting; `copilot-requests: write` is recommended when targeting the Copilot coding agent so it can authenticate with `${{ github.token }}`
- **`runs-on:`** - Runner type for the main agent job (string, array, or object)
- **`runs-on-slim:`** - Runner type for all framework/generated jobs (activation, safe-outputs, unlock, etc.). Defaults to `ubuntu-slim`. `safe-outputs.runs-on` takes precedence for safe-output jobs specifically.
- **`runner:`** - Runner topology configuration (object). `topology: arc-dind` (only enum value) targets GitHub ARC runners with rootless Docker-in-Docker: gh-aw emits the topology in the AWF config, redirects the tool cache to a shared volume, and validates that no generated step requires root. AWF then activates split-filesystem handling, network isolation, sysroot staging, and DinD pre-staging automatically.

  ```yaml
  runner:
    topology: arc-dind
  ```
- **`timeout-minutes:`** - Agent execution step timeout in minutes (integer or GitHub Actions expression, defaults to `${{ vars.GH_AW_DEFAULT_TIMEOUT_MINUTES }}` or 20 minutes; custom and safe-output jobs use the GitHub Actions platform default of 360 minutes unless explicitly set). It bounds the `agentic_execution` step only; the generated `agent` and `detection` jobs have their own timeouts (see [jobs.md](jobs.md)). Expressions are useful in compiled workflows that define `workflow_call` inputs, for example `timeout-minutes: ${{ inputs.timeout }}`. This setting applies to the workflow being compiled, not to plain GitHub Actions caller jobs that use job-level `uses:` (GitHub does not allow `timeout-minutes` on those caller jobs).
- **`concurrency:`** - Concurrency control (string or object)
  - **`queue:`** - Pending run queue behavior for the concurrency group (`single` or `max`, defaults to `single`). `single` keeps one pending run and replaces older pending runs; `max` allows up to 100 pending runs in FIFO order (useful for conclusion jobs that must not be dropped).

    ```yaml
    concurrency:
      group: "my-workflow"
      queue: max
    ```
  - **`job-discriminator:`** - Expression appended to compiler-generated job-level concurrency groups (`agent`, `output`, and `conclusion` jobs), preventing fan-out cancellations when multiple workflow instances run concurrently with different inputs. Common usage:

    ```yaml
    concurrency:
      job-discriminator: ${{ inputs.finding_id }}
    ```

    Common expressions:

    | Scenario | Expression |
    |---|---|
    | Fan-out by input | `${{ inputs.finding_id }}` |
    | Universal uniqueness | `${{ github.run_id }}` |
    | Dispatched or scheduled fallback | `${{ inputs.organization \|\| github.run_id }}` |

    `job-discriminator` is a gh-aw extension stripped from the compiled lock file. Has no effect on `workflow_dispatch`-only, `push`, or `pull_request` triggered workflows.
- **`env:`** - Environment variables (object or string)
- **`if:`** - Conditional execution expression (string)
- **`run-name:`** - Custom workflow run name (string)
- **`name:`** - Workflow name (string)
- **`pre-steps:`** - Custom workflow steps to run at the very beginning of the agent job, before checkout (object). Use for token minting or setup that must happen before the repository is checked out. Step outputs are available via `${{ steps.<id>.outputs.<name> }}` and can be referenced in `checkout.github-token` to avoid masked-value cross-job boundary issues. Same security restrictions apply as for `steps:`.
  - For job-scoped hooks under `jobs.<job-id>`, `setup-steps` run before framework GitHub App token minting and checkout, while `pre-steps` run after compiler setup and before the job's main steps.
- **`steps:`** - Custom workflow steps before AI execution (object). **Security Notice**: Custom steps run OUTSIDE the firewall sandbox with standard GitHub Actions security but NO network egress controls. Use only for deterministic data preparation, not agentic compute. **Secrets restriction**: Using `${{ secrets.* }}` expressions (other than `secrets.GITHUB_TOKEN`) in custom steps is an error in strict mode and a warning otherwise — move secret-dependent operations to a separate job outside the agent job.
- **`pre-agent-steps:`** - Custom workflow steps to run before MCP gateway startup (object or array). Use when preparation must install or configure MCP dependencies before the gateway starts. Same security restrictions apply as for `steps:`.
- **`post-steps:`** - Custom workflow steps after AI execution (object). **Security Notice**: Post-execution steps run OUTSIDE the firewall sandbox. Use only for deterministic cleanup, artifact uploads, or notifications—not agentic compute or untrusted AI execution. Same secrets restriction applies as for `steps:`.
- **`environment:`** - Environment that the job references for protection rules (string or object)
- **`container:`** - Container to run job steps in (string or object)
- **`services:`** - Service containers that run alongside the job (object)
- **`secrets:`** - Secret values passed to workflow execution (object)
  - Use GitHub Actions expressions: `${{ secrets.API_KEY }}`
  - String format: `secrets: { API_TOKEN: "${{ secrets.API_TOKEN }}" }`
  - Object format with descriptions:

    ```yaml
    secrets:
      API_TOKEN:
        value: ${{ secrets.API_TOKEN }}
        description: "API token for external service"
    ```

  - Never commit plaintext secrets
  - For reusable workflows, use `jobs.<job_id>.secrets` instead
- **`excluded-env:`** - Optional list of environment variable names to unconditionally exclude from the AWF agent container via `--exclude-env` (array of strings). Use when an env var carries a credential the compiler cannot auto-detect (for example a `workflow_dispatch` input holding a token). Names are deduplicated and merged with those auto-detected from `secrets.*` and `needs.*.outputs.*` references.
  - Example: `excluded-env: [MY_DISPATCH_TOKEN, GH_TOKEN]`

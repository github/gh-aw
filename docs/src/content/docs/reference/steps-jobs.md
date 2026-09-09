---
title: Custom Steps and Jobs
description: "Add deterministic pre-processing steps and custom GitHub Actions jobs to agentic workflows using pre-steps:, steps:, pre-agent-steps:, post-steps:, and jobs:"
sidebar:
  order: 820
---

Custom steps and jobs let you mix deterministic computation with agentic execution. All custom steps and jobs run **outside the firewall sandbox** with standard GitHub Actions security.

See [DeterministicOps](/gh-aw/patterns/deterministic-ops/) for patterns combining computation with AI reasoning.

## Choosing the Right Step Hook

Use these top-level step hooks in this order:

1. `pre-steps:` — runs before checkout and all later built-in agent-job setup; only compiler-injected OTLP masking steps may run earlier. Use this for short-lived token minting or anything that must happen before repository checkout.
2. `steps:` — runs after checkout and the normal runtime/bootstrap steps, but before the final pre-agent phase. Use this for deterministic preprocessing that needs the checked-out repository.
3. `pre-agent-steps:` — runs after framework-owned initialization such as checkout cleanup and base-branch restoration, and before MCP startup and engine execution. Use this for last-moment environment preparation immediately before the agent starts.
4. `post-steps:` — runs after the engine finishes. Use this for cleanup, summaries, uploads, or follow-up automation.

## Custom Pre-Checkout Steps (`pre-steps:`)

Add custom steps before checkout and the later pre-checkout agent-job setup. Compiler-injected OTLP masking steps may still run earlier.

```yaml wrap
pre-steps:
  - name: Mint checkout token
    id: checkout_app
    uses: actions/create-github-app-token@v2
```

Use pre-steps when later checkout or setup must consume outputs from a step in the same job.

## Custom Steps (`steps:`)

Add custom steps before agentic execution. If unspecified, a default checkout step is added automatically.

```yaml wrap
steps:
  - name: Install dependencies
    run: npm ci
```

Use custom steps to precompute data, filter triggers, or prepare context for AI agents. Steps can also short-circuit the agent by writing a `noop` entry to `$GH_AW_SAFE_OUTPUTS` — the harness detects this at startup and exits cleanly without incurring any AI inference cost. See [Skip the Agent from Steps Using `noop`](/gh-aw/reference/cost-management/#skip-the-agent-from-steps-using-noop) for details.

## Custom Pre-Agent Steps (`pre-agent-steps:`)

Add custom steps before MCP gateway startup in the agent job so prerequisite MCP installation/configuration can happen first.

```yaml wrap
pre-agent-steps:
  - name: Finalize Context
    run: ./scripts/prepare-agent-context.sh
```

Use pre-agent steps when work must happen right before the engine runs (for example, final context preparation or last-moment validations).

## Custom Post-Execution Steps (`post-steps:`)

Add custom steps after agentic execution. Run after the AI engine completes regardless of success/failure (unless conditional expressions are used).

```yaml wrap
post-steps:
  - name: Upload Results
    if: always()
    uses: actions/upload-artifact@v4
    with:
      name: workflow-results
      path: /tmp/gh-aw/
      retention-days: 7
```

Useful for artifact uploads, summaries, cleanup, or triggering downstream workflows.

## Custom Jobs (`jobs:`)

Define custom jobs that run before agentic execution. The agentic execution job waits for all custom jobs to complete. Custom jobs can share data with the agent through artifacts or job outputs.

```yaml wrap
jobs:
  super_linter:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - name: Run Super-Linter
        uses: super-linter/super-linter@v7
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

### Supported Job-Level Fields

| Field | Description |
|---|---|
| `name` | Display name for the job |
| `needs` | Jobs that must complete before this job runs |
| `runs-on` | Runner label — string, array, or object form |
| `if` | Conditional expression to control job execution |
| `permissions` | GitHub token permissions for this job |
| `outputs` | Values exposed to downstream jobs |
| `env` | Environment variables available to all steps |
| `timeout-minutes` | Maximum job duration (GitHub Actions default: 360) |
| `concurrency` | Concurrency group to prevent parallel runs |
| `continue-on-error` | Allow the workflow to continue if this job fails |
| `container` | Docker container to run steps in |
| `services` | Service containers (e.g. databases) |
| `setup-steps` | Steps injected immediately after the compiler-generated `actions/setup` step for that job (except `activation` and `pre_activation`, where compile fails) |
| `pre-steps` | Steps injected after compiler setup steps and before checkout/`steps` in that job |
| `steps` | List of steps — supports complete GitHub Actions step specification |
| `restore-memory` | Restore configured memory stores read-only before `pre-steps`/`steps` so deterministic jobs can read prior state |
| `uses` | Reusable workflow to call |
| `with` | Input parameters for a reusable workflow |
| `secrets` | Secrets passed to a reusable workflow |

The `strategy` field (matrix builds) is not supported.

`runs-on` accepts a string, an array of runner labels, or the object form:

```yaml wrap
jobs:
  build:
    runs-on:
      group: my-runner-group
      labels: [self-hosted, linux]
    steps:
      - uses: actions/checkout@v7
```

When imports define the same `jobs.<job-id>.setup-steps` or `jobs.<job-id>.pre-steps`, gh-aw merges that field deterministically: imported steps run first, then main-workflow steps. The two fields stay separate; `setup-steps` are never folded into `pre-steps` or vice versa.

## Jobs and Steps

Use this map to see where compiler-inserted steps land for each job type.

### Custom jobs

`jobs.<job-id>` steps run in this order:

1. `jobs.<job-id>.setup-steps`
2. Compiler host setup (`Configure GH_HOST for enterprise compatibility`)
3. `restore-memory` injected setup/restore steps (when enabled)
4. `jobs.<job-id>.pre-steps`
5. `jobs.<job-id>.steps`

Set `jobs.<job-id>.restore-memory: true` to restore any configured `cache-memory`, `repo-memory`, and `comment-memory` stores before deterministic job steps run. This surface is read-only: gh-aw injects restore/clone/prepare steps, but never emits cache save, repo push, or safe-output write-back steps for custom jobs.

### Built-in jobs

| Job | Step order |
|---|---|
| `pre_activation` | `jobs.pre_activation.setup-steps` is refused at compile time to prevent short-circuiting protections; use `jobs.pre_activation.pre-steps` and `jobs.pre_activation.steps` |
| `activation` | `jobs.activation.setup-steps` is refused at compile time to prevent short-circuiting protections; use `jobs.activation.pre-steps` |
| `agent` | `jobs.agent.setup-steps` → compiler setup checkout/setup → `jobs.agent.pre-steps` → runtime path setup → top-level `pre-steps` → checkout/token/runtime/custom/agent steps |
| `safe_outputs` | `jobs.safe_outputs.setup-steps` → compiler setup checkout/setup → `jobs.safe_outputs.pre-steps` → safe-outputs downloads/prep → GitHub App token minting → safe-output handlers/finalization |
| `conclusion` | `jobs.conclusion.setup-steps` → compiler setup checkout/setup → `jobs.conclusion.pre-steps` → built-in conclusion steps (including GitHub App token minting when configured) |
| `detection` | `jobs.detection.setup-steps` → compiler setup checkout/setup → `jobs.detection.pre-steps` → built-in detection steps |
| `unlock` | `jobs.unlock.setup-steps` → compiler setup checkout/setup → `jobs.unlock.pre-steps` → built-in unlock steps |

Built-in job sections also support additive `needs` and `if` overrides. For example, use `jobs.agent.needs` and `jobs.agent.if` to gate the generated agent job on a custom setup job without relying on `on.needs` plus a top-level workflow `if`:

```yaml wrap
jobs:
  build:
    runs-on: ubuntu-latest
    outputs:
      outcome: ${{ steps.result.outputs.outcome }}
    steps:
      - id: result
        run: echo "outcome=failure" >> "$GITHUB_OUTPUT"

  agent:
    needs: [build]
    if: needs.build.outputs.outcome == 'failure'
```

`jobs.<built-in>.needs` is merged with compiler-generated dependencies, and `jobs.<built-in>.if` is combined with compiler-generated conditions using logical `&&`. `jobs.<built-in>.timeout-minutes` is accepted for the `agent` and `detection` jobs only; see [Agent and Detection Job Timeouts](#agent-and-detection-job-timeouts).

Example using `timeout-minutes` and `env`:

```yaml wrap
jobs:
  build:
    runs-on: ubuntu-latest
    timeout-minutes: 15
    env:
      NODE_ENV: production
    steps:
      - uses: actions/checkout@v7
      - run: npm ci && npm run build
```

### Agent and Detection Job Timeouts

The generated `agent` and `detection` jobs are bounded by their own
`timeout-minutes` values, independently from the top-level `timeout-minutes`
that bounds the `agentic_execution` step:

| Timeout | Default | GitHub Actions variable |
| --- | --- | --- |
| `agent` job | 60 minutes | `GH_AW_DEFAULT_AGENT_JOB_TIMEOUT_MINUTES` |
| `detection` job and its execution step | 10 minutes | `GH_AW_DEFAULT_DETECTION_JOB_TIMEOUT_MINUTES` |
| `agentic_execution` step (top-level `timeout-minutes`) | 20 minutes | `GH_AW_DEFAULT_TIMEOUT_MINUTES` |

Set those variables at the repository, organization, or enterprise level to
change the defaults without editing frontmatter. When top-level
`timeout-minutes` requests a longer agentic step than the default agent job
budget, the default agent job budget is raised to match it.

Override either generated job independently when setup or post-processing needs
a different budget:

```yaml wrap
jobs:
  agent:
    timeout-minutes: 90
  detection:
    timeout-minutes: 30
```

When a job reaches its timeout, GitHub Actions cancels all remaining steps in
that job; the workflow conclusion reports the job as `timed_out`. A job-level
timeout therefore covers setup steps as well as the agentic execution step.

### Job Outputs

Custom jobs can expose outputs accessible in the agentic execution prompt via `${{ needs.job-name.outputs.output-name }}`:

```yaml wrap
jobs:
  release:
    outputs:
      release_id: ${{ steps.get_release.outputs.release_id }}
      version: ${{ steps.get_release.outputs.version }}
    steps:
      - id: get_release
        run: echo "version=${{ github.event.release.tag_name }}" >> $GITHUB_OUTPUT
```

```markdown
Generate highlights for release ${{ needs.release.outputs.version }}.
```

Job outputs must be string values.

## Learn More

- [DeterministicOps](/gh-aw/patterns/deterministic-ops/) — Patterns combining deterministic steps with AI reasoning
- [Frontmatter Reference](/gh-aw/reference/frontmatter/) — Complete frontmatter field reference
- [Custom Safe Outputs](/gh-aw/reference/custom-safe-outputs/) — Custom post-processing jobs for agentic outputs
- [Imports](/gh-aw/reference/imports/) — Composing `pre-agent-steps` and `post-steps` across shared workflows

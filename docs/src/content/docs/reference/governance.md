---
title: Configuration Governance
description: Set and manage gh-aw environment defaults across enterprise, organization, and repository scopes with gh aw env.
---

# Configuration Governance

Use governance defaults when you want consistent model and
guardrail behavior across many repositories without editing
every workflow file.

This guide shows how to manage these defaults with
`gh aw env` and how values percolate from enterprise to
organization to repository scope.

## What `gh aw env` manages

`gh aw env` manages `GH_AW_DEFAULT_*` variables as GitHub
Actions variables at repository (`--scope repo`),
organization (`--scope org`), or enterprise
(`--scope ent`) scope. The command uses a YAML file
with `default_` keys.

```yaml title="defaults.yml"
default_max_ai_credits: "5M"
default_max_daily_ai_credits: "15M"
default_max_turns: "12"
default_timeout_minutes: "30"
default_agent_job_timeout_minutes: "90"
default_detection_job_timeout_minutes: "15"
default_model_copilot: "gpt-5-mini"
default_model_claude: "claude-haiku-4-5"
default_model_codex: "gpt-5.4-mini"
default_detection_model: "gpt-5.5-mini"
default_utc: "-08:00" # UTC offset for rendered CLI timestamps
```

> [!NOTE]
> Set a key to `null` (or omit it) to delete that variable
> from the selected scope during `gh aw env update`.

## Export current defaults

Start by exporting current values from the target scope.

```bash
gh aw env get ent-defaults.yml --scope ent --enterprise MY_ENT
gh aw env get org-defaults.yml --scope org --org MY_ORG
gh aw env get repo-defaults.yml --scope repo --repo OWNER/REPO
```

## Apply defaults safely

After editing the YAML file, preview and apply the change.

```bash
gh aw env update org-defaults.yml --scope org --org MY_ORG --visibility all --dry-run
gh aw env update org-defaults.yml --scope org --org MY_ORG --visibility all
```

For organization and enterprise scopes, `--visibility` accepts
`all` (the default), `private`, or `selected`. It applies only when
creating a variable; existing variables keep their current visibility.
When using `selected`, attach repositories separately with the GitHub API.

Use `--yes` in automation to skip the interactive
confirmation prompt.

```bash
gh aw env update org-defaults.yml --scope org --org MY_ORG --yes
```

## Governance rollout pattern

Use a layered rollout: set enterprise baseline defaults,
add organization defaults only where needed, use
repository defaults for true exceptions, and keep
workflow frontmatter overrides rare and explicit. This
keeps most repositories aligned while still allowing
narrow overrides.

## How percolation and precedence work

For values resolved from GitHub Actions `vars.*`, the most
specific scope wins:

1. workflow frontmatter value (if set)
2. repository variable
3. organization variable
4. enterprise variable
5. built-in compiler fallback

Examples using this runtime path include
`GH_AW_DEFAULT_MAX_AI_CREDITS`,
`GH_AW_DEFAULT_MAX_DAILY_AI_CREDITS`,
`GH_AW_DEFAULT_DETECTION_MAX_AI_CREDITS`,
`GH_AW_DEFAULT_TIMEOUT_MINUTES`,
`GH_AW_DEFAULT_AGENT_JOB_TIMEOUT_MINUTES`,
`GH_AW_DEFAULT_DETECTION_JOB_TIMEOUT_MINUTES`,
and `GH_AW_DEFAULT_MODEL_*`.

> [!IMPORTANT]
> Some defaults are compile-time values.
> `GH_AW_DEFAULT_MAX_TURNS`,
> `GH_AW_DEFAULT_MAX_TURN_CACHE_MISSES`,
> `GH_AW_DEFAULT_DETECTION_MODEL`, and
> `GH_AW_DEFAULT_UTC` are read by the compiler process.
> If compilation runs in CI, pass these values into the
> compile step environment.

```yaml title=".github/workflows/compile.yml"
jobs:
  compile:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - name: Compile workflows
        env:
          GH_AW_DEFAULT_MAX_TURNS: ${{ vars.GH_AW_DEFAULT_MAX_TURNS }}
          GH_AW_DEFAULT_MAX_TURN_CACHE_MISSES: ${{ vars.GH_AW_DEFAULT_MAX_TURN_CACHE_MISSES }}
          GH_AW_DEFAULT_DETECTION_MODEL: ${{ vars.GH_AW_DEFAULT_DETECTION_MODEL }}
          GH_AW_DEFAULT_UTC: ${{ vars.GH_AW_DEFAULT_UTC }}
        run: gh aw compile
```

## Runtime Policy Variables

Policy variables (`GH_AW_POLICY_*`) complement the
`GH_AW_DEFAULT_*` values managed by `gh aw env`. Defaults
control numeric and model settings; policy variables are
boolean capability gates that allow or deny specific
runtime behaviors without recompiling workflows.

Like defaults, they are set as GitHub Actions variables at
repository, organization, or enterprise scope and are read
at runtime through `vars.*`.

### Disabling `create-pull-request` org-wide

`GH_AW_POLICY_ALLOW_CREATE_PULL_REQUEST` controls whether agentic workflows
are allowed to open pull requests. Set it to `"false"` to prevent any
workflow from creating PRs across every repository in an organization or
enterprise:

```bash
gh variable set GH_AW_POLICY_ALLOW_CREATE_PULL_REQUEST \
  --org my-org --body "false"
```

When the policy is active, the safe-outputs server refuses to start for
any workflow that has `safe-outputs.create-pull-request` configured:

```text
create-pull-request is disabled by runtime policy: GH_AW_POLICY_ALLOW_CREATE_PULL_REQUEST=false.
Remove safe-outputs.create-pull-request or set GH_AW_POLICY_ALLOW_CREATE_PULL_REQUEST=true.
```

Any other value — including unset — leaves the tool
enabled. To lift the restriction, set the variable to
`"true"` or delete it, either org-wide or at repository
scope:

```bash
# Re-enable for the whole org
gh variable delete GH_AW_POLICY_ALLOW_CREATE_PULL_REQUEST --org my-org

# Override at repository scope only (most-specific-wins)
gh variable set GH_AW_POLICY_ALLOW_CREATE_PULL_REQUEST \
  --repo owner/repo --body "true"
```

See [Runtime Policy Variables](/gh-aw/reference/environment-variables/#runtime-policy-variables)
for the complete list of `GH_AW_POLICY_*` variables.

## Compile-Time Runner Enforcement

Set `GH_AW_REQUIRE_SELF_HOSTED_RUNNERS` to `true` in the trusted compiler process to make `gh aw compile` fail unless every generated job selects a self-hosted runner. The check examines the compiled workflow, so it also catches GitHub-hosted runners selected by defaults when a runner key is omitted.

```yaml title="Required compile check"
jobs:
  compile:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - name: Compile workflows
        env:
          GH_AW_REQUIRE_SELF_HOSTED_RUNNERS: "true"
        run: gh aw compile
```

The compiler accepts an explicit `self-hosted` label or a runner group as evidence of self-hosted selection; custom labels without either are rejected. To enforce this organization- or enterprise-wide, make the centrally managed compile check required through a ruleset. This is a compile-time setting and is not managed by `gh aw env`.

---

## Pull request rulesets for agentic workflow injection

Use repository or organization rulesets to require one or more checks produced by
agentic workflows (for example PR review, security review, or policy checks). This
lets platform teams inject review automation into many repositories without asking
each repository to manually wire branch protection settings.

Recommended pattern:

1. Keep the workflow `name:` and reviewer job names stable so required checks do not
   drift between updates.
2. If the workflow uses imports, set `inlined-imports: true` so required-check runs
   do not fail with runtime import resolution errors in ruleset contexts.
3. Trigger reviewer workflows on PR lifecycle events that matter to rulesets
   (`opened`, `synchronize`, `reopened`, and optionally `ready_for_review`).
4. For review bots, restrict `submit-pull-request-review.allowed-events` to
   `COMMENT` and/or `REQUEST_CHANGES` unless you intentionally provide a token that
   can approve pull requests.

For runtime import failures under rulesets, see the FAQ entry
["Runtime import file not found" in rulesets](/gh-aw/reference/faq/#my-workflow-fails-with-runtime-import-file-not-found-when-used-in-a-repository-ruleset).

---

## Troubleshooting

If `gh aw env update` fails validation, make sure turn and
timeout settings are positive integers, AI credit limits
are non-zero integers, `default_utc` uses a numeric offset
(such as `+00:00` or `-08:00`), and the YAML file contains
only supported keys.

## Related reference

See [Environment Variables](/gh-aw/reference/environment-variables/),
[Compiler Enterprise Environment Controls](/gh-aw/reference/compiler-enterprise-environment-controls/),
[Cost Management](/gh-aw/reference/cost-management/), and
[Using at Scale in Organizations](/gh-aw/guides/using-at-scale/).

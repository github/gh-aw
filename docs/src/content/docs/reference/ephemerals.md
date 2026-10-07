---
title: Ephemerals
description: Features for automatically expiring workflow resources and reducing noise in your repositories
sidebar:
  order: 9
---

GitHub Agentic Workflows includes several "ephemeral" features that automatically expire resources and reduce noise in your repositories. They control costs by stopping scheduled workflows at deadlines, auto-close issues and discussions, hide older comments, and isolate automation via the [side repository pattern](/gh-aw/patterns/multi-repo-ops/#using-a-side-repository).

## Expiration Features

### Workflow Stop-After

Automatically disable workflow triggering after a deadline to control costs and prevent indefinite execution.

```yaml wrap
on: weekly on monday
  stop-after: "+25h"  # 25 hours from compilation time
```

Accepted formats are absolute dates (`YYYY-MM-DD`, `MM/DD/YYYY`, `DD/MM/YYYY`, `January 2 2006`, `1st June 2025`, ISO 8601) and relative deltas such as `+7d`, `+25h`, or `+1d12h30m`, all calculated from compilation time. The minimum granularity is hours, so minute-only units such as `+30m` are not allowed.

At the deadline, new runs are prevented while existing runs complete. Recompiling does not change the stored stop time unless you use `gh aw compile --refresh-stop-time`. Common uses include trial periods, experiments, and cost-controlled schedules.

See [Triggers Reference](/gh-aw/reference/triggers/#stop-after-configuration-stop-after) for complete documentation.

### Safe Output Expiration

Auto-close issues, discussions, and pull requests after a specified time period. This generates a maintenance workflow that runs automatically at appropriate intervals.

#### Issue Expiration

```yaml wrap
safe-outputs:
  create-issue:
    expires: 7  # Auto-close after 7 days
    labels: [automation, agentic]
```

#### Discussion Expiration

```yaml wrap
safe-outputs:
  create-discussion:
    expires: 3  # Auto-close after 3 days as "OUTDATED"
    category: "general"
```

#### Pull Request Expiration

```yaml wrap
safe-outputs:
  create-pull-request:
    expires: 14  # Auto-close after 14 days (same-repo only)
    draft: true
```

Supported formats are an integer day count (for example, `7`) or a relative duration such as `2h`, `7d`, `2w`, `1m`, or `1y`. Values under 24 hours are rounded up to a 1-day minimum for expiration.

**Maintenance workflow generation is lazy**: `agentics-maintenance.yml` is only generated when at least one workflow explicitly opts in — by setting `expires` on `create-issue`, `create-discussion`, or `create-pull-request`, or by explicitly configuring `safe-outputs.noop`. The implicit `noop` fallback that is added automatically when `safe-outputs` is present defaults `report-as-issue` to `false`, so it does not create issues and does not generate a maintenance workflow.

**Maintenance workflow frequency**: The generated `agentics-maintenance.yml` workflow runs at the minimum required frequency based on the shortest expiration time across all workflows:

| Shortest Expiration | Maintenance Frequency |
|---------------------|----------------------|
| 1 day or less | Every 2 hours |
| 2 days | Every 6 hours |
| 3-4 days | Every 12 hours |
| 5+ days | Daily |

**Expiration markers**: The system adds a visible checkbox line with an XML comment to the body of created items:
```markdown
- [x] expires <!-- gh-aw-expires: 2026-01-14T15:30:00.000Z --> on Jan 14, 2026, 3:30 PM UTC
```

The maintenance workflow searches for items with this expiration format (checked checkbox with the XML comment) and automatically closes them with appropriate comments and resolution reasons. Users can uncheck the checkbox to prevent automatic expiration.

See [Safe Outputs Reference](/gh-aw/reference/safe-outputs/) for complete documentation.

### Cache-Memory Cleanup

The maintenance workflow automatically cleans up outdated [cache-memory](/gh-aw/reference/cache-memory/) entries on every scheduled run. Cache keys follow the pattern `memory-{workflow}-{run-id}`, and the cleanup job groups caches by workflow prefix, keeps the latest run ID per group, and deletes older entries. This prevents cache storage from growing unboundedly as workflows run repeatedly.

The cleanup includes rate-limit awareness — it pauses early if the GitHub API rate limit is running low — and produces a job summary table showing how many caches were found, kept, and deleted.

You can also trigger cleanup manually using the `clean_cache_memories` operation (see [Manual maintenance operations](#manual-maintenance-operations) below).

### Manual Maintenance Operations

The generated `agentics-maintenance.yml` workflow supports manual bulk operations via `workflow_dispatch`. Admin or maintainer users can trigger operations from the GitHub Actions UI or the CLI. All operations are restricted to admin and maintainer roles and are not available on forks.

Available operations:

| Operation | Description |
|-----------|-------------|
| `disable` | Disable all agentic workflows in the repository |
| `enable` | Re-enable all agentic workflows in the repository |
| `update` | Recompile workflows and create a PR if files changed |
| `upgrade` | Upgrade agentic workflows to the latest version and create a PR if files changed |
| `safe_outputs` | Replay safe outputs from a specific workflow run (requires a run URL or run ID) |
| `create_labels` | Create any repository labels referenced in safe-outputs that do not yet exist |
| `clean_cache_memories` | Clean up outdated cache-memory entries (same as the automated scheduled cleanup) |
| `validate` | Run full workflow validation with all linters and file an issue if findings are detected |
| `activity_report` | Generate a repository activity report for the last 24 hours, week, and month, and create an issue with the results |
| `forecast` | Run a workflow token-usage forecast and create an issue with the JSON results |

**Details for select operations:**

`update` and `upgrade` run `gh aw update` or `gh aw upgrade`, stage changed files, and open a pull request for review. After merging, recompile lock files with `gh aw compile`. See [Upgrading Workflows](/gh-aw/guides/working-with-workflows/#upgrading-workflows) for the manual process.

`safe_outputs` replays safe-output processing from a previous workflow run using a run URL or numeric run ID in `run_url`.

`create_labels` runs `gh aw compile --json --no-emit`, collects unique label names across workflows, and creates any missing labels with deterministic pastel colors. It requires `issues: write` permission.

`validate` runs `gh aw compile --validate --no-emit --zizmor --actionlint --poutine --verbose` and creates or updates `[aw] workflow validation findings` if errors or warnings are found.

`activity_report` runs `gh aw logs --format markdown` for the last 24 hours, 7 days, and 30 days (up to 1000 runs each), then creates `[aw] agentic status report` with each time range in a collapsible `<details>` block. Downloaded logs are cached under `./.cache/gh-aw/activity-report-logs`. The job has a 2-hour timeout and skips the 30-day query when the GitHub API is rate-limited.

`forecast` warms the forecast cache with `gh aw logs --artifacts agent`, runs `gh aw forecast --repo <owner/repo> --timeout 30 --json` with a 30-minute graceful computation timeout, and always creates a summary issue. Successful runs create `[aw] workflow forecast report`; timeouts and other failures create `[aw] workflow forecast report (error)`.

### Maintenance Configuration

You can customize the maintenance workflow runner or disable maintenance entirely using the `aw.json` configuration file at `.github/workflows/aw.json`.

Customize the runner:

```json
{
  "maintenance": {
    "runs_on": "ubuntu-latest",
    "action_failure_issue_expires": 72,
    "disabled_jobs": [
      "close-expired-entities",
      "apply_safe_outputs",
      "label_apply_safe_outputs"
    ]
  }
}
```

`runs_on` accepts a single string or an array of strings for multi-label runners such as `["self-hosted", "linux"]`. The default runner is `ubuntu-slim`.

`action_failure_issue_expires` sets expiration, in hours, for failure issues opened by the conclusion job, including grouped parent issues when `group-reports: true`. The default is `168` (7 days).

Explicitly setting `action_failure_issue_expires` in `aw.json` is treated as an opt-in: it causes `agentics-maintenance.yml` to be generated (if not already generated by another expiring safe output) so the scheduled `close-expired-entities` job can enforce it. If `action_failure_issue_expires` is left unset and no other workflow output requires scheduled maintenance, the implicit 168-hour default is not written into failure issues, since there would be no scheduled job to close them; failure issues are created without an expiration marker in that case.

If `.github/workflows/aw.json` is present but cannot be loaded, parsed, or validated, compilation keeps the default `168`-hour expiration and emits a warning that identifies the config path and fallback value.

`disabled_jobs` lets you omit specific maintenance jobs from the generated workflow. Job IDs are case-insensitive, and `_` / `-` are treated equivalently.

Supported job IDs:

| Job ID | Effect when disabled |
| --- | --- |
| `close-expired-entities` | Omits all three close-expired cleanup jobs (`close-expired-discussions`, `close-expired-issues`, and `close-expired-pull-requests`). |
| `apply_safe_outputs` | Omits the `safe_outputs` replay job. When this is disabled, `workflow_call.outputs.applied_run_url` falls back to `inputs.run_url`; scheduled and manual runs leave that output empty. |
| `label_disable_agentic_workflow` | Omits the label-triggered disable workflow job. |
| `label_apply_safe_outputs` | Omits the label-triggered safe-outputs replay job. |
| `run_operation` | Omits the job handling `disable`, `enable`, `update`, and `upgrade`. |
| `update_pull_request_branches` | Omits the pull request branch update job. |
| `validate_workflows` | Omits the `validate` operation job. Use the job ID, not `validate`, in `disabled_jobs`. |
| `activity_report` | Omits the activity report job. |
| `forecast_report` | Omits the `forecast` operation job. |
| `close_agentic_workflows_issues` | Omits the issue cleanup job. |
| `create_labels` | Omits the label creation job. |
| `cleanup-cache-memory` | Omits the cache-memory cleanup job (generated only when a workflow uses cache memory). |

Unrecognized job IDs are rejected during config validation.

See [Self-Hosted Runners](/gh-aw/reference/self-hosted-runners/#configuring-the-maintenance-workflow-runner) for more details.

**Disable maintenance entirely:**

```json
{
  "maintenance": false
}
```

When maintenance is disabled, the compiler deletes any existing `agentics-maintenance.yml` file and emits a warning for workflows that use the `expires` field, since expiration depends on the maintenance workflow to run.

> [!WARNING]
> Disabling maintenance prevents automatic expiration of issues, discussions, and pull requests. Any `expires` configuration in your workflows will become a no-op until maintenance is re-enabled.

### Close Older Issues

Automatically close older issues with the same workflow-id marker when creating new ones. This keeps your issues focused on the latest information.

```yaml wrap
safe-outputs:
  create-issue:
    close-older-issues: true  # Close previous reports
```

When a new issue is created, up to 10 older issues with the same workflow-id marker are closed as "not planned" with a comment linking to the new issue. Requires `GH_AW_WORKFLOW_ID` to be set and appropriate repository permissions. Ideal for weekly reports and recurring analyses where only the latest result matters.

## Noise Reduction Features

### Hide Older Comments

Minimize previous comments from the same workflow before posting new ones. Useful for status update workflows where only the latest information matters.

```yaml wrap
safe-outputs:
  add-comment:
    hide-older-comments: true
    allowed-reasons: [outdated]  # Optional: restrict hiding reasons
```

Before posting, the system finds and minimizes previous comments from the same workflow (identified by `GITHUB_WORKFLOW`). Comments are hidden, not deleted. Use `allowed-reasons` to restrict which minimization reason is applied: `spam`, `abuse`, `off_topic`, `outdated` (default), `resolved`, or `low_quality`.

See [Safe Outputs Reference](/gh-aw/reference/safe-outputs/#hide-older-comments) for complete documentation.

### Side Repository Pattern

Run agentic workflows from a separate "side" repository that targets your main codebase. This isolates AI-generated issues, comments, and workflow runs from your main repository, keeping automation infrastructure separate from production code.

See [MultiRepoOps — Side Repository](/gh-aw/patterns/multi-repo-ops/#using-a-side-repository) for complete setup and usage documentation.

### Text Sanitization

Control which GitHub repository references (`#123`, `owner/repo#456`) are allowed in workflow output text. When configured, references to unlisted repositories are escaped with backticks to prevent GitHub from creating timeline items.

```yaml wrap
safe-outputs:
  allowed-github-references: []  # Escape all references
  create-issue:
    target-repo: "my-org/main-repo"
```

See [Safe Outputs Reference](/gh-aw/reference/safe-outputs/) for complete documentation.

### Use Discussions Instead of Issues

For ephemeral content, use discussions instead of issues. They have lower search weight and don't clutter project boards, making them ideal for recurring reports and status updates.

```yaml wrap
safe-outputs:
  create-discussion:
    category: "Status Updates"
    expires: 14  # Close after 2 weeks
    close-older-discussions: true  # Replace previous reports
```

## Learn More

- [Triggers Reference](/gh-aw/reference/triggers/) for complete trigger configuration, including `stop-after`
- [Safe Outputs Reference](/gh-aw/reference/safe-outputs/) for all safe output types and expiration options
- [MultiRepoOps](/gh-aw/patterns/multi-repo-ops/) for complete side-repository setup guidance

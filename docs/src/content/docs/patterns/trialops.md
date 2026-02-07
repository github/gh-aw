---
title: TrialOps
description: Test and validate agentic workflows in isolated trial repositories before deploying to production
sidebar:
  badge: { text: 'Testing', variant: 'tip' }
---

> [!NOTE]
> Part of SideRepoOps
> TrialOps is a specialized testing pattern that extends [SideRepoOps](/gh-aw/patterns/siderepoops/). While SideRepoOps runs workflows from a separate repository against your main codebase, TrialOps uses temporary trial repositories to safely validate and test workflows before production deployment.

TrialOps runs agentic workflows in isolated trial repositories to safely validate behavior, compare approaches, and iterate on prompts without affecting production. The `trial` command creates temporary private repositories where workflows execute and capture safe outputs (issues, PRs, comments) without modifying your actual codebase.

Use TrialOps to test workflows before production deployment, compare implementations, validate prompt changes, debug in isolation, or demonstrate capabilities with real results.

## How Trial Mode Works

```bash
gh aw trial githubnext/agentics/weekly-research
```

The CLI creates a temporary private repository (default: `gh-aw-trial`), installs and executes the workflow via `workflow_dispatch`, then saves results in three locations:

- **Local**: `trials/weekly-research.DATETIME-ID.json` (safe output metadata)
- **GitHub**: Trial repository at `<username>/gh-aw-trial` (actual issues/PRs/comments)
- **Console**: Summary with execution results and links

## Repository Modes

### Default Mode

Simulates running against your current repository - `github.repository` points to your repo, but outputs go to the trial repository.

```bash
gh aw trial githubnext/agentics/my-workflow
```

### Direct Mode (`--repo`)

Installs and runs the workflow in a specified repository. All outputs are created there.

```bash
gh aw trial githubnext/agentics/my-workflow --repo myorg/test-repo
```

> [!CAUTION]
> Production Risk
> Creates real issues and PRs in the target repository. Only use with test repositories.

### Logical Repository Mode (`--logical-repo`)

Simulates running against a specified repository (like default mode, but for any repo).

```bash
gh aw trial githubnext/agentics/my-workflow --logical-repo myorg/target-repo
```

### Clone Mode (`--clone-repo`)

Clones repository contents into the trial repo so workflows can analyze actual code and file structures.

```bash
gh aw trial githubnext/agentics/code-review --clone-repo myorg/real-repo
```

## Basic Usage

### Dry-Run Mode

Preview trial operations without executing workflows or creating repositories:

```bash
gh aw trial ./my-workflow.md --dry-run
```

Dry-run mode shows what actions would be taken, including repository creation, workflow installation, and execution plans. Use this to verify trial configuration before committing to the full process.

### Single Workflow

```bash
gh aw trial githubnext/agentics/weekly-research  # From GitHub
gh aw trial ./my-workflow.md                      # Local file
```

### Multiple Workflows

Compare workflows side-by-side with combined results:

```bash
gh aw trial githubnext/agentics/daily-plan githubnext/agentics/weekly-research
```

Outputs: individual result files plus `trials/combined-results.DATETIME.json`.

### Repeated Trials

Test consistency by running multiple times:

```bash
gh aw trial githubnext/agentics/my-workflow --repeat 3
```

### Custom Trial Repository

```bash
gh aw trial githubnext/agentics/my-workflow --host-repo my-custom-trial
gh aw trial ./my-workflow.md --host-repo .  # Use current repo
```

> [!TIP]
> Trial repositories persist between runs. Reuse the same `--host-repo` name across test sessions.

## Advanced Patterns

### Issue Context

Provide issue context for issue-triggered workflows:

```bash
gh aw trial githubnext/agentics/triage-workflow \
  --trigger-context "https://github.com/myorg/repo/issues/123"
```

### Auto-merge PRs

Automatically merge created PRs (useful for testing multi-step workflows):

```bash
gh aw trial githubnext/agentics/feature-workflow --auto-merge-prs
```

### Engine Comparison

```bash
gh aw trial ./my-workflow.md --engine claude
gh aw trial ./my-workflow.md --engine copilot
gh aw trial ./my-workflow.md --engine codex
```

Required API keys: `COPILOT_GITHUB_TOKEN`, `CLAUDE_CODE_OAUTH_TOKEN` or `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`.

### Append Instructions

Test workflow responses to additional constraints without modifying the source:

```bash
gh aw trial githubnext/agentics/my-workflow \
  --append "Focus on security issues and create detailed reports."
```

### Cleanup Options

```bash
gh aw trial ./my-workflow.md --delete-host-repo-after        # Delete after completion
gh aw trial ./my-workflow.md --force-delete-host-repo-before # Clean slate before running
```

## Understanding Trial Results

Results are saved in `trials/*.json` with workflow runs, issues, PRs, and comments viewable in the trial repository's Actions and Issues tabs.

**Result file structure:**

```json
{
  "workflow_name": "weekly-research",
  "run_id": "12345678",
  "safe_outputs": {
    "issues_created": [{
      "number": 5,
      "title": "Research quantum computing trends",
      "url": "https://github.com/user/gh-aw-trial/issues/5"
    }]
  },
  "agentic_run_info": {
    "duration_seconds": 45,
    "token_usage": 2500
  }
}
```

**Success indicators:** Green checkmark, expected outputs created, no errors in logs.

**Common issues:**
- **Workflow dispatch failed** - Add `workflow_dispatch` trigger
- **No safe outputs** - Configure safe outputs in workflow
- **Permission errors** - Verify API keys
- **Timeout** - Use `--timeout 60` (minutes)

## Comparing Multiple Workflows

```bash
gh aw trial ./workflow-v1.md ./workflow-v2.md ./workflow-v3.md
```

Compare quality (detail/accuracy), quantity (output count), performance (execution time), and consistency (use `--repeat`).

**Example:**

```bash
gh aw trial v1.md v2.md v3.md --repeat 2
cat trials/combined-results.*.json | jq '.results[] | {workflow: .workflow_name, issues: .safe_outputs.issues_created | length}'
```

## Trial Mode Limitations

- **Requires `workflow_dispatch` trigger** - Add to workflows that only trigger on issues/PRs/schedules
- **Safe outputs needed** - Workflows without safe outputs execute but create no visible results
- **No simulated events** - Use `--trigger-context` to provide event context like issue payloads
- **Private repositories** - Trial repos count toward your private repository quota
- **API rate limits** - Space out large runs or use `--repeat` instead of separate invocations

## Best Practices

### Development Workflow

1. Write workflows locally in your editor
2. Preview with `gh aw trial ./my-workflow.md --dry-run`
3. Test with `gh aw trial ./my-workflow.md`
4. Adjust based on trial results
5. Compare variants side-by-side
6. Validate with `--repeat`
7. Deploy to production

### Testing Strategy

```bash
# Unit testing - individual workflows
gh aw trial ./workflows/triage.md --delete-host-repo-after

# Integration testing - with actual content
gh aw trial ./workflows/code-review.md --clone-repo myorg/real-repo

# Regression testing - before/after comparison
gh aw trial ./workflow.md --host-repo regression-baseline
gh aw trial ./workflow.md --host-repo regression-test

# Performance testing - execution time and tokens
gh aw trial ./workflow.md --repeat 5
```

### Prompt Engineering

Iteratively refine prompts: run baseline → modify prompt → test variant → compare outputs → repeat.

### CI/CD Integration

```yaml
name: Test Workflows
on: [pull_request]
jobs:
  trial:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - name: Install gh-aw
        run: gh extension install github/gh-aw
      - name: Trial workflow
        env:
          COPILOT_GITHUB_TOKEN: ${{ secrets.COPILOT_GITHUB_TOKEN }}
        run: gh aw trial ./.github/workflows/my-workflow.md --delete-host-repo-after --yes
```

## Troubleshooting

| Issue | Solution |
|-------|----------|
| `workflow not found` | Use correct format: `owner/repo/workflow-name`, `owner/repo/.github/workflows/workflow.md`, or `./local-workflow.md` |
| `workflow_dispatch not supported` | Add `workflow_dispatch:` to workflow frontmatter `on:` section |
| `authentication failed` | Set API keys: `COPILOT_GITHUB_TOKEN`, `CLAUDE_CODE_OAUTH_TOKEN` or `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`. Use `--use-local-secrets` to push to trial repo |
| `failed to create trial repository` | Check `gh auth status`, verify quota with `gh api user \| jq .plan`, try explicit `--host-repo name` |
| `execution timed out` | Increase with `--timeout 60` (minutes, default: 30) |
| No issues/PRs created | Configure `safe-outputs` in workflow frontmatter, check Actions logs for errors |

## Common Trial Patterns

**Pre-deployment validation:**
```bash
gh aw trial ./new-feature.md --clone-repo myorg/production-repo --host-repo pre-deployment-test
```

**Prompt optimization:**
```bash
gh aw trial ./workflow-detailed.md ./workflow-concise.md
cat trials/combined-results.*.json | jq
```

**Documentation examples:**
```bash
gh aw trial ./workflow.md --force-delete-host-repo-before --host-repo workflow-demo
```

**Debugging production issues:**
```bash
gh aw trial ./workflow.md --clone-repo myorg/production --trigger-context "https://github.com/myorg/production/issues/456" --host-repo debug-session
```

## Related Documentation

- [SideRepoOps](/gh-aw/patterns/siderepoops/) - Run workflows from separate repositories
- [MultiRepoOps](/gh-aw/patterns/multirepoops/) - Coordinate across multiple repositories
- [Orchestration](/gh-aw/patterns/orchestration/) - Orchestrate multi-issue initiatives
- [CLI Commands](/gh-aw/setup/cli/) - Complete CLI reference
- [Safe Outputs Reference](/gh-aw/reference/safe-outputs/) - Configuration options
- [Workflow Triggers](/gh-aw/reference/triggers/) - Including workflow_dispatch
- [Security Best Practices](/gh-aw/introduction/architecture/) - Authentication and security

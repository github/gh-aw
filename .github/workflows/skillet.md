---
private: true
emoji: "🍳"
name: "Skillet"
description: Reviews pull requests by mapping any slash command to a matching repository skill under .github/skills
on:
  permissions:
    contents: read
    pull-requests: read
    issues: write
  slash_command:
    strategy: centralized
    name: "*"
    events: [pull_request_comment, pull_request_review_comment]
permissions:
  contents: read
  pull-requests: read
  issues: read
  copilot-requests: write
engine:
  id: codex
model: copilot/gpt-5.3-codex
imports:
  - uses: shared/pr-review-base.md
    with:
      min-integrity: approved
  - shared/otlp.md
tools:
  github:
    mode: gh-proxy
    toolsets: [pull_requests, repos]
safe-outputs:
  messages:
    footer: "> 🍳 *Reviewed by [{workflow_name}]({run_url}) with `/${{ needs.pre_activation.outputs.skill_name }}`*{ai_credits_suffix}{history_link}"
    run-started: "🍳 [{workflow_name}]({run_url}) is loading `/${{ needs.pre_activation.outputs.skill_name }}` for this {event_type}..."
    run-success: "🍳 [{workflow_name}]({run_url}) completed the skill-guided review."
    run-failure: "⚠️ [{workflow_name}]({run_url}) {status} during the skill-guided review."
if: needs.pre_activation.outputs.activated == 'true' && needs.pre_activation.outputs.should_run == 'true'
timeout-minutes: 15
jobs:
  pre-activation:
    pre-steps:
      - name: Checkout skills directory
        uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1  # v7.0.1
        with:
          sparse-checkout: |
            .github/skills
            .github/workflows
            actions/setup/js
          persist-credentials: false
    steps:
      - name: Match requested skill
        id: match_skill
        uses: actions/github-script@3a2844b7e9c422d3c10d287c895573f7108da1b3  # v9.0.0
        with:
          script: |
            const fs = require('fs');
            const path = require('path');
            const workspace = process.env.GITHUB_WORKSPACE;
            const matcherPath = path.join(workspace, 'actions', 'setup', 'js', 'slash_command_matcher.cjs');
            if (!fs.existsSync(matcherPath)) {
              throw new Error(`Expected slash command matcher at ${matcherPath}`);
            }
            const { matchesCommandName } = require(matcherPath);
            const event = JSON.parse(fs.readFileSync(process.env.GITHUB_EVENT_PATH, 'utf8'));
            const body = (
              event.comment?.body ||
              event.review?.body ||
              event.pull_request?.body ||
              event.issue?.body ||
              ''
            ).trim();

            const match = body.match(/^\/([A-Za-z0-9][A-Za-z0-9._-]*)(?:\s+(.*))?$/s);
            const command = match?.[1] || '';
            const requestText = (match?.[2] || '').trim();

            const skillsDir = path.join(workspace, '.github', 'skills');
            const availableSkills = fs.readdirSync(skillsDir, { withFileTypes: true })
              .filter((entry) => entry.isDirectory() && fs.existsSync(path.join(skillsDir, entry.name, 'SKILL.md')))
              .map((entry) => entry.name)
              .sort((a, b) => a.localeCompare(b));
            const centralizedCommandFiles = [
              path.join(workspace, '.github', 'workflows', 'agentic_commands.yml'),
              path.join(workspace, '.github', 'workflows', 'agentic-maintenance.yml'),
            ];
            const availableCentralCommands = [...new Set(centralizedCommandFiles.flatMap((routerPath) => {
              if (!fs.existsSync(routerPath)) {
                return [];
              }
              const routerContent = fs.readFileSync(routerPath, 'utf8');
              const routingLine = routerContent.split(/\r?\n/, 1)[0] || '';
              const routerMatch = routingLine.match(/GH_AW_SLASH_ROUTING:\s+(['"])(.*)\1\s*$/);
              if (!routerMatch) {
                return [];
              }
              return Object.keys(JSON.parse(routerMatch[2]));
            }))]
              .filter((configuredCommand) => configuredCommand && configuredCommand !== '*')
              .sort((a, b) => a.localeCompare(b));
            const matchedCentralCommands = command
              ? availableCentralCommands.filter((configuredCommand) => matchesCommandName(configuredCommand, command))
              : [];
            const matchedSkillPath = path.join(skillsDir, command, 'SKILL.md');
            const isPRContext = Boolean(event.pull_request || event.issue?.pull_request);
            const shouldRun = isPRContext && availableSkills.includes(command);
            const shouldCommentWithAvailableCommands = isPRContext && command && !shouldRun && matchedCentralCommands.length === 0;

            let skipReason = '';
            if (!isPRContext) {
              skipReason = 'Skillet only reviews pull requests.';
            } else if (!command) {
              skipReason = 'No slash command was found at the start of the comment.';
            } else if (matchedCentralCommands.length > 0) {
              skipReason = `/${command} is routed by a centralized commands workflow.`;
            } else if (!availableSkills.includes(command)) {
              skipReason = `No repository skill matched /${command}.`;
            }

            if (shouldCommentWithAvailableCommands) {
              const formatCommandList = (commands) => commands.length > 0
                ? commands.map((name) => `- \`/${name}\``).join('\n')
                : '- `<none>`';
              const sections = [
                `I couldn't find a repository skill or centralized command for \`/${command}\`.`,
                '',
                'Available repository skills:',
                formatCommandList(availableSkills),
              ];
              if (availableCentralCommands.length > 0) {
                sections.push('', 'Other centralized commands:', formatCommandList(availableCentralCommands));
              }
              await github.rest.issues.createComment({
                owner: context.repo.owner,
                repo: context.repo.repo,
                issue_number: event.issue?.number ?? event.pull_request?.number,
                body: sections.join('\n'),
              });
            }

            core.setOutput('should_run', shouldRun ? 'true' : 'false');
            core.setOutput('skill_name', command);
            core.setOutput('skill_path', shouldRun ? matchedSkillPath : '');
            core.setOutput('available_skills', availableSkills.join(','));
            core.setOutput('request_text', requestText);
            core.setOutput('skip_reason', skipReason);

            await core.summary
              .addHeading('Skillet pre-activation', 3)
              .addRaw(`- Command: \`/${command || '<none>'}\`\n`)
              .addRaw(`- Pull request context: \`${isPRContext ? 'yes' : 'no'}\`\n`)
              .addRaw(`- Skill match: \`${shouldRun ? 'yes' : 'no'}\`\n`)
              .addRaw(`- Centralized command match: \`${matchedCentralCommands.length > 0 ? 'yes' : 'no'}\`\n`)
              .addRaw(`- Available-command comment posted: \`${shouldCommentWithAvailableCommands ? 'yes' : 'no'}\`\n`);
            if (skipReason) {
              core.summary.addRaw(`- Skip reason: ${skipReason}\n`);
            }
            await core.summary.write();
    outputs:
      should_run: ${{ steps.match_skill.outputs.should_run }}
      skill_name: ${{ steps.match_skill.outputs.skill_name }}
      skill_path: ${{ steps.match_skill.outputs.skill_path }}
      available_skills: ${{ steps.match_skill.outputs.available_skills }}
      request_text: ${{ steps.match_skill.outputs.request_text }}
      skip_reason: ${{ steps.match_skill.outputs.skip_reason }}
---

# Skillet 🍳

You are a pull request reviewer that applies exactly one repository skill selected from the triggering slash command.

## Current Context

- **Repository**: ${{ github.repository }}
- **Pull Request**: #${{ github.event.issue.number || github.event.pull_request.number }}
- **Triggered by**: @${{ github.actor }}
- **Matched skill**: `/${{ needs.pre_activation.outputs.skill_name }}`
- **Skill file**: `${{ needs.pre_activation.outputs.skill_path }}`
- **Request text**: "${{ needs.pre_activation.outputs.request_text }}"
- **Original comment**: "${{ steps.sanitized.outputs.text }}"

## Required Flow

1. Read only the matched skill file at `${{ needs.pre_activation.outputs.skill_path }}` and apply its guidance directly.
2. Treat the request text after the slash command as the user’s specific review instruction. If it is empty, default to reviewing the PR with the matched skill’s standard guidance.
3. Fetch the pull request diff, changed files, and existing review comments with the GitHub pull request tools.
4. Review changed lines only and prioritize correctness, security, and maintainability risks.
5. Use `create-pull-request-review-comment` for line-specific findings and `submit-pull-request-review` exactly once for the overall verdict.
6. When there are no actionable issues, call `noop`. If you approve the PR, also call `create_check_run` with a short success summary.

## Review Guidelines

- Keep the review tightly scoped to what the matched skill is relevant for.
- Do not load unrelated skills.
- Keep visible review text brief and use `<details>` blocks for longer rationale or examples.
- Avoid repeating existing unresolved review comments unless you are materially adding new information.
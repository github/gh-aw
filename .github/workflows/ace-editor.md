---
private: true
emoji: "✏️"
name: ACE Editor Session
description: Generates an ACE editor session link when invoked with /ace command on pull request comments
on:
  slash_command:
    strategy: centralized
    name: ace
    events: [pull_request_comment]
max-daily-ai-credits: 10000
timeout-minutes: 10
engine:
  id: codex
  model-provider: openai
model: openai/gpt-5.3-codex
strict: false
permissions:
  pull-requests: read
  issues: read
jobs:
  post_ace_link:
    runs-on: ubuntu-latest
    needs: [activation]
    if: needs.activation.outputs.activated == 'true'
    permissions:
      pull-requests: write
      issues: write
    steps:
      - name: Post ACE editor session link
        uses: actions/github-script@v9.0.0
        with:
          script: |
            const prNumber = context.payload.issue.number;
            const repo = context.repo.repo;
            const owner = context.repo.owner;
            const actor = context.actor;
            const sessionId = `${owner}-${repo}-pr${prNumber}`;
            const aceUrl = `https://ace.com/session/${sessionId}`;

            await github.rest.issues.createComment({
              owner,
              repo,
              issue_number: prNumber,
              body: `👋 Hey @${actor}! Here's your ACE editor session link for this pull request:\n\n🔗 **${aceUrl}**\n\nCopy and paste this link into Slack to invite your teammates into the session! 🚀`,
            });
imports:
  - shared/otlp.md
  - shared/graders.md
sandbox:
  agent:
    runtime: cloud-hypervisor
tools:
  cli-proxy: true
evals:
  - id: ace_link_generated
    question: Did the agent generate an ACE editor session link in response to the /ace command?
  - id: comment_posted
    question: Was a comment posted to the pull request with the ACE editor link?

---

Classic action that generates an ACE editor session link on pull request comment slash command.

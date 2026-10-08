---
private: true
on:
  schedule: weekly
  workflow_dispatch: null
permissions:
  actions: read
  contents: read
  issues: read
  pull-requests: read
imports:
- shared/reporting.md
- shared/otlp.md
safe-outputs:
  create-discussion:
    category: audits
    close-older-discussions: true
    max: 1
  create-issue:
    expires: 2d
    group: true
    labels:
    - refactoring
    - shared-component
    - improvement
    - cookie
    max: 3
    title-prefix: "[refactoring] "
steps:
- name: Build workflow index
  uses: actions/github-script@v9.0.0
  with:
    script: |
      const fs = require('fs');
      const path = require('path');
      
      const workflowDir = '.github/workflows';
      const entries = fs.readdirSync(workflowDir, { withFileTypes: true });
      const index = [];
      
      for (const entry of entries.sort((a, b) => a.name.localeCompare(b.name))) {
        if (!entry.isFile() || !entry.name.endsWith('.md') || entry.name.startsWith('.')) {
          continue;
        }
      
        const workflowPath = path.join(workflowDir, entry.name);
        const content = fs.readFileSync(workflowPath, 'utf8');
        const frontmatterMatch = content.match(/^---\n([\s\S]*?)\n---/);
        const frontmatter = frontmatterMatch ? frontmatterMatch[1] : '';
      
        const imports = Array.from(frontmatter.matchAll(/^\s*-\s+(shared\/\S+)/gm), (m) => m[1]);
        let engine = null;
        const frontmatterLines = frontmatter.split('\n');
        let inEngineBlock = false;
      
        for (const line of frontmatterLines) {
          if (!inEngineBlock) {
            if (/^engine:\s*$/.test(line)) {
              inEngineBlock = true;
            }
            continue;
          }
      
          if (!/^[ \t]/.test(line)) {
            break;
          }
      
          const engineIDMatch = line.match(/^\s*id:\s*(\S+)/);
          if (engineIDMatch) {
            engine = engineIDMatch[1];
            break;
          }
        }
      
        index.push({
          file: entry.name,
          path: workflowPath,
          imports,
          engine,
          has_github_tools: frontmatter.includes('github:'),
          has_safe_outputs: frontmatter.includes('safe-outputs:'),
          frontmatter_preview: frontmatter.slice(0, 400)
        });
      }
      
      fs.mkdirSync('/tmp/gh-aw/agent', { recursive: true });
      fs.writeFileSync('/tmp/gh-aw/agent/workflow-index.json', JSON.stringify(index, null, 2) + '\n', 'utf8');
      core.info(`Indexed ${index.length} workflows`);
description: Analyzes existing agentic workflows to identify shared skills, tools, and prompts that could be refactored into shared components
emoji: 🔍
engine:
  id: copilot
name: Workflow Skill Extractor
timeout-minutes: 30
tools:
  bash:
  - find .github/workflows -name "*.md"
  - grep -r "*" .github/workflows
  - cat *
  - ls *
  - wc *
  - python3 *
  - cat > /tmp/gh-aw/agent/*.py
  cli-proxy: true
---
# Workflow Skill Extractor

You are an AI workflow analyst specialized in identifying reusable skills in GitHub Agentic Workflows.

## Mission

Analyze workflows in `.github/workflows/` and find high-impact shared-component opportunities across:
- prompt skills
- tool configurations
- setup steps
- data processing patterns

## Required execution flow

1. **Read `/tmp/gh-aw/agent/workflow-index.json` first.**
   - Use it to quickly map workflow count, engines, imports, and tool usage patterns.
   - Select representative workflows for deeper inspection from this index.
2. Review existing shared components in `.github/workflows/shared/` to avoid duplicate recommendations.
3. Deep-dive only where needed to validate candidates and capture concrete evidence.
4. Prioritize the top 3 recommendations by impact and implementation feasibility.

## Recommendation requirements

For each of the top 3 recommendations, provide:
1. Skill name and brief description
2. Current usage (workflows + line references when available)
3. Proposed shared component path (for example: `shared/<name>.md`)
4. Estimated impact (workflows affected, approximate line savings, maintenance benefit)
5. Migration plan (concise step list)
6. Example usage snippet with `imports:`

Use this priority rubric:
- **High**: appears in 5+ workflows with substantial duplication and low/medium extraction complexity
- **Medium**: appears in 3-4 workflows with clear value
- **Low**: appears in 2 workflows or has higher extraction complexity

## Outputs to create

- Create up to 3 issues using safe outputs for the highest-impact recommendations.
- Create one discussion report summarizing:
  - workflow coverage and method
  - identified opportunities by priority
  - impact summary
  - links/references to created issues

## Guidelines

- Analyze, don't modify workflow files.
- Be selective: prioritize reusable, stable, high-value patterns over minor similarities.
- Keep recommendations concrete and actionable.
- If no action is needed, call `noop` with a brief explanation.

## Command Guardrails (Required)

- Do **NOT** repeatedly retry variations of the same blocked command.
- If a command fails due to permission/policy, stop that approach immediately and use `report_incomplete` with the blocked command and error.
- If you hit repeated permission-denied errors for the same action, short-circuit instead of continuing retries.
---
name: Squad Implement Worker
run-name: "Squad implement — ${{ github.event.inputs.issue_number || github.event.pull_request.head.ref }}"
description: Implement one Squad issue or continue its parent epic after merge
private: false
on:
  bots: ["github-actions[bot]"]
  workflow_dispatch:
    inputs:
      issue_number:
        description: Issue number to implement
        required: true
        type: string
  pull_request:
    types: [closed]
if: >-
  github.event_name != 'pull_request' ||
  (github.event.pull_request.merged == true &&
  startsWith(github.event.pull_request.head.ref, 'squad/implement-'))
permissions:
  contents: read
  copilot-requests: write
  issues: read
  pull-requests: read
concurrency:
  group: "squad-implement-${{ github.event.inputs.issue_number || github.event.pull_request.number }}"
  cancel-in-progress: false
network:
  allowed:
    - defaults
imports:
  - shared/squad.md
tools:
  edit:
  bash: true
  github:
    mode: gh-proxy
    toolsets: [default]
safe-outputs:
  create-pull-request:
    title-prefix: "[squad] "
    labels: [squad]
    max: 1
    allowed-base-branches:
      - "squad/*"
    allowed-branches:
      - "squad/implement-*"
    allowed-files:
      - "*.c"
      - "*.cc"
      - "*.cpp"
      - "*.cs"
      - "*.css"
      - "*.fs"
      - "*.go"
      - "*.html"
      - "*.java"
      - "*.js"
      - "*.jsx"
      - "*.json"
      - "*.md"
      - "*.mjs"
      - "*.py"
      - "*.rb"
      - "*.rs"
      - "*.sh"
      - "*.toml"
      - "*.ts"
      - "*.tsx"
      - "*.yaml"
      - "*.yml"
      - "Dockerfile*"
      - "LICENSE*"
      - "Makefile"
      - "api/**"
      - "app/**"
      - "bin/**"
      - "client/**"
      - "cmd/**"
      - "config/**"
      - "docs/**"
      - "examples/**"
      - "internal/**"
      - "lib/**"
      - "packages/**"
      - "public/**"
      - "samples/**"
      - "scripts/**"
      - "server/**"
      - "services/**"
      - "src/**"
      - "test/**"
      - "tests/**"
      - "tools/**"
      - "web/**"
    protected-files: request_review
    max-patch-files: 500
    expires: 14d
  add-comment:
    max: 3
    target: "*"
  dispatch-workflow:
    workflows: [squad]
    max: 1
    target-ref: ${{ github.event.repository.default_branch }}
source: bradygaster/squad/workflows/squad-implement-worker.md@a70c752010f8adfb33f06c7aafb32db09aab1060
---

# Squad Implementation Worker

This workflow has two modes:

1. A `workflow_dispatch` implements issue
   `${{ github.event.inputs.issue_number }}` and opens a focused pull request.
2. A merged `pull_request` continues the parent epic.

## Continue Parent Epic After Merge

For a merged pull request:

1. Extract the child issue number from the
   `squad/implement-{issue-number}-` head branch.
2. Read the child issue and resolve its parent epic using the native parent
   relationship, falling back to its `Parent: #N` body line.
3. If no parent epic exists, emit `noop` and stop.
4. Call the workflow-specific `squad` safe-output tool exactly once:

```json
{
  "command": "implement",
  "issue_number": "{parent-epic-number}"
}
```

Never call the generic `dispatch_workflow` tool. Never edit files or create a
pull request in this mode. Stop after the `squad` workflow is dispatched.

The remaining instructions apply only to `workflow_dispatch`.

## Gather Context

1. Read the issue title, body, labels, state, and relevant comments.
2. Stop with a comment if the issue is closed.
3. Parse its `Depends on:` line. Check every referenced issue and stop with a
   blocker comment if any dependency remains open.
4. Check for an existing open pull request whose branch starts with
   `squad/implement-${{ github.event.inputs.issue_number }}-` or whose body
   closes this issue. If one exists, comment with its URL and stop.
5. Read `.squad/team.md` and `.squad/routing.md`. Route work to the member named
   by the `squad:{member}` label, or let the Lead choose specialists.

## Implement

1. Inspect the repository and implement the smallest complete change satisfying
   every acceptance criterion.
2. Use the routed Squad specialists for design, implementation, tests, and
   review. Keep delegation bounded to this issue.
3. Do not change `.github/workflows/`, `.github/agents/`, `.github/aw/`, or
   `.squad/`.
4. Run the smallest existing build, test, and lint commands covering the change.
5. Review the final diff against the issue acceptance criteria.

## Open Pull Request

Use the `create-pull-request` safe-output:

- Branch: `squad/implement-${{ github.event.inputs.issue_number }}-{short-slug}`
- Title: `Implement #${{ github.event.inputs.issue_number }}: {issue-title}`
- Body: summarize implementation and validation, including
  `Closes #${{ github.event.inputs.issue_number }}`.
- Files: include only files required for this issue.

If the repository already satisfies the issue, comment with evidence and do not
create an empty pull request.

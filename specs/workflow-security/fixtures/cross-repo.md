---
name: Compiled Security Model Seed
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  copilot-requests: write
strict: true
network:
  allowed: [defaults]
checkout:
  - repository: github/gh-aw
    path: main
    fetch-depth: 1
    sparse-checkout: README.md
  - repository: example/private-dependency
    path: dependency
    github-token: ${{ secrets.MODEL_DEPENDENCY_READ_TOKEN }}
    fetch-depth: 0
safe-outputs:
  github-app:
    client-id: ${{ vars.MODEL_APP_ID }}
    private-key: ${{ secrets.MODEL_APP_PRIVATE_KEY }}
    repositories: [gh-aw]
  create-issue:
    max: 1
tools:
  bash: ["git diff *", "git show *"]
---

# Compile-only security model seed

This fixture uses placeholder credentials and a fictitious private dependency.
Never dispatch it. It represents two independently authenticated checkouts,
read-only agent work, and one mediated issue write.

Inspect only locally available git data. Report unavailable blobs or base refs;
do not fetch, pull, push, deepen, or widen a checkout. Request an issue through
the create-issue safe output. Never read or print credentials.

---
on: gitlab-merge-request-opened
engine: copilot
permissions:
  contents: read
imports:
  - uses: "@builtin:connectors/gitlab.md"
    with:
      projects: ["42"]
      merge-requests: ["7"]
---
Compilation must fail closed because native connector ingress is unavailable.

---
on:
  gateway-dispatch:
    projects-items:
      project_id: PVT_example
      types: [edited]
      status-transitions:
        - {from: Todo, to: Ready}
    code-scanning-alert:
      types: [appeared_in_branch]
      severity: error
    dependabot-alerts:
      types: [dismissed]
      severity: critical
    jira-issue:
      project: APP
      types: [opened, closed]
    azure-devops-issue:
      project_id: project-uuid
      types: [closed, reopened]
      closed-states: [Done, Closed]
permissions:
  contents: read
engine: copilot
---

# Gateway resource events

Process ${{ toJSON(gateway.events) }} using authenticated provider clients.

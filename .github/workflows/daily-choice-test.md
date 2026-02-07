---
description: Daily test workflow using Claude with custom safe-output job containing choice inputs
on:
  schedule:
    - cron: "0 12 * * 1-5"  # 12 PM UTC, weekdays only
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
tracker-id: daily-choice-test
engine: claude
strict: true
timeout-minutes: 10

network:
  allowed:
    - defaults

tools:
  github:
    toolsets:
      - default

safe-outputs:
  staged: true
  jobs:
    test_environment:
      name: "Test Environment Deployment"
      description: "A test job with choice input"
      runs-on: ubuntu-latest
      inputs:
        environment:
          description: "Target environment"
          required: true
          type: choice
          options: ["staging", "production"]
        test_type:
          description: "Type of test to run"
          required: true
          type: choice
          options: ["smoke", "integration", "e2e"]
      output: "Environment test completed successfully"
      steps:
        - name: Display test configuration
          run: |
            if [ -f "$GH_AW_AGENT_OUTPUT" ]; then
              ENVIRONMENT=$(cat "$GH_AW_AGENT_OUTPUT" | jq -r '.items[] | select(.type == "test_environment") | .environment')
              TEST_TYPE=$(cat "$GH_AW_AGENT_OUTPUT" | jq -r '.items[] | select(.type == "test_environment") | .test_type')
              {
                echo "### Test Configuration"
                echo "- Environment: $ENVIRONMENT"
                echo "- Test Type: $TEST_TYPE"
              } >> "$GITHUB_STEP_SUMMARY"
              echo "Test would be executed on $ENVIRONMENT environment with $TEST_TYPE tests"
            else
              echo "No agent output found"
            fi
imports:
  - shared/mood.md
---

# Daily Choice Type Test

This workflow tests the choice type functionality in safe-output jobs with Claude.

## Task

Use the `test_environment` tool to configure a test deployment. Choose:
1. An environment: staging or production
2. A test type: smoke, integration, or e2e

Make your selection based on the day of the week:
- Monday/Wednesday/Friday: Use "staging" environment with "smoke" tests
- Tuesday/Thursday: Use "production" environment with "integration" tests

Provide a brief explanation of why you chose this configuration.

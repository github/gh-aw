---
permissions:
  contents: read
  pull-requests: read
# Base configuration for AI-powered PR code review workflows
# Provides: GitHub PR tools and review comment safe-outputs

tools:
  github:
    toolsets: [pull_requests, repos]

safe-outputs:
  create-pull-request-review-comment:
    side: "RIGHT"
    max: 10
  submit-pull-request-review:
    max: 1
  create-check-run:
    max: 1
---

## PR Code Review Configuration

- Read `/tmp/gh-aw/agent/pr-diff.patch` instead of calling `get_diff`; it is capped to bound context.
- Read `/tmp/gh-aw/agent/pr-review-comments.json` instead of calling `get_review_comments`; avoid duplicate comments.
- Batch findings into one unified review and submit it once.

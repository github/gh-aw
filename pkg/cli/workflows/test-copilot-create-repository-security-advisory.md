---
on:
  workflow_dispatch:
permissions:
  security-events: write
engine: copilot
features:
  dangerous-permissions-write: true
---

# Test Copilot Create Repository Security Advisory

This is a test workflow to verify that Copilot can create repository security advisories.

Please create a test security advisory with:
- Title: "Test Security Advisory"
- Summary: "This is a test advisory created by Copilot"
- Severity: Low
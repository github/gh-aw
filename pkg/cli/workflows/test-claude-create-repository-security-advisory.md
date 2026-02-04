---
on:
  workflow_dispatch:
permissions:
  security-events: write
engine: claude
features:
  dangerous-permissions-write: true
---

# Test Claude Create Repository Security Advisory

This is a test workflow to verify that Claude can create repository security advisories.

Please create a test security advisory with:
- Title: "Test Security Advisory"
- Summary: "This is a test advisory created by Claude"
- Severity: Low
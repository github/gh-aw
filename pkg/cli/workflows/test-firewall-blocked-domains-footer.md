---
on:
  workflow_dispatch:
engine: copilot
source: github/gh-aw/workflows/test-firewall-blocked-domains-footer.md@main
permissions:
  contents: read
  issues: read
  pull-requests: read
network:
  firewall: true
  allowed:
    - defaults
    - github
  blocked:
    - pypi.org
    - npmjs.org
safe-outputs:
  create-issue:
    title-prefix: "[test] "
    expires: 2h
timeout-minutes: 5
tools:
  github:
  bash:
    - "*"
---

# Test Firewall Blocked Domains Footer

This workflow tests that the footer includes a collapsed details section showing blocked domains when the firewall blocks access to certain domains.

## Test Steps

1. **Attempt to access blocked domains** - Try to access `pypi.org` and `npmjs.org` which are configured as blocked in this workflow
2. **Attempt to access allowed domains** - Access `api.github.com` to verify allowed domains still work
3. **Create a test issue** - Create an issue to verify the footer contains the blocked domains section

## Output

Create an issue with:
- Title: "Firewall Blocked Domains Footer Test - Run {{ github.run_id }}"
- Body: A brief summary of the test results:
  - Which domains were blocked (pypi.org, npmjs.org)
  - Which domains were allowed (api.github.com)
  - Confirmation that the test passed

The footer should automatically include a collapsed `<details>` section showing the blocked domains.

## Test Commands

Run these commands to trigger the firewall:

```bash
# Try to access blocked domains (these should fail)
curl -I https://pypi.org 2>&1 | head -5 || echo "pypi.org blocked as expected"
curl -I https://npmjs.org 2>&1 | head -5 || echo "npmjs.org blocked as expected"

# Try to access allowed domain (this should succeed)
curl -I https://api.github.com 2>&1 | head -5
```

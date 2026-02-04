---
on:
  pull_request:
    types: [opened, synchronize]
permissions:
  contents: read
  pull-requests: read
engine:
  id: claude
tools:
  github:
    allowed: [get_pull_request]
---

# Test Template with Pull Request Context

Review PR #${{ github.event.pull_request.number }} in repository ${{ github.repository }}.

{{#if true}}
## Standard Review

Always perform these checks:
- Review changed files
- Check for breaking changes
- Verify tests pass
- Review documentation updates
{{/if}}

{{#if false}}
## Experimental Analysis (Disabled)

This experimental analysis is currently disabled.
{{/if}}

{{#if null}}
## Advanced Checks (Disabled via Null)

Null evaluates to falsy - this section is excluded.
{{/if}}

{{#if undefined}}
## Debug Mode (Disabled via Undefined)

Undefined evaluates to falsy - this section is excluded.
{{/if}}

Provide your review feedback.

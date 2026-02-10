---
title: Example Protected Page
description: This is an example page demonstrating the disable-agentic-editing frontmatter field
disable-agentic-editing: true
sidebar:
  badge: { text: 'Protected', variant: 'caution' }
---

:::caution[Protected Content]
This page has `disable-agentic-editing: true` set in its frontmatter, which signals to AI agents that automated modifications should be avoided.
:::

## Purpose

This page serves as a live example of the documentation protection feature. When AI agents or automated workflows process this documentation site, they should detect the `disable-agentic-editing` field and skip any modification attempts.

## What This Means

### For AI Agents
- **Read-only access**: Agents can reference and cite this content
- **No automated edits**: Content should not be modified by automation
- **Report constraints**: Agents should inform users when protection prevents actions

### For Maintainers
- **Manual updates only**: Changes require direct human intervention
- **Intentional curation**: Content structure and wording are deliberately maintained
- **Quality control**: All modifications go through standard review processes

## Example Use Cases Demonstrated Here

This page might represent:

1. **Release announcements** that need to remain as originally published
2. **Legal disclaimers** requiring careful human oversight for changes
3. **Curated showcases** where selection and ordering are intentional
4. **Generated content** that shouldn't conflict with regeneration

## Technical Implementation

The frontmatter for this page looks like:

```yaml
---
title: Example Protected Page
description: This is an example page demonstrating the disable-agentic-editing frontmatter field
disable-agentic-editing: true
sidebar:
  badge: { text: 'Protected', variant: 'caution' }
---
```

The `disable-agentic-editing: true` field is:
- Validated by the Astro content schema defined in `src/content.config.ts`
- Optional (defaults to `false` if not specified)
- Boolean-typed for clear true/false semantics
- Accompanied by schema description for tooling introspection

## Verification

You can verify this field is working by:

1. Inspecting the page metadata through Astro's content collections API
2. Building the documentation site (should succeed without errors)
3. Testing agent behavior when attempting to modify this file

## Learn More

For complete documentation about this feature, see:
- [Documentation Protection Reference](/gh-aw/reference/documentation-protection/)

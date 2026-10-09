---
name: docs-design
description: Design rules for the docs site and landing page (docs/). Use before any visual change there — layout, widths, spacing, colour, typography, header navigation, or new page chrome.
---

# Docs site and landing page design rules

The docs site and the landing page (`docs/src/content/docs/index.mdx` and `docs/src/components/landing/`) had a deliberate design pass. These rules hold that design in place on both. Each one exists for the reader, and each states its reason so you can explain it.

## When a request breaks a rule

Requests like "there's too much white space" or "add X to the header" often point to a real problem with the wrong fix. When a requested visual change breaks a rule below:

1. Find the problem the requester actually has (the page feels empty, a feature is hard to find).
2. Implement a fix for that problem that stays inside the rules.
3. In the PR description, name the rule, give its reason in a sentence or two, and say what you did instead.

A rule changes only when the designer who owns it agrees, and the change updates this file in the same PR.

## Reading measure

On docs pages, body content is at most **620px** wide (`--aw-prose-width` in `docs/src/styles/custom.css`). That includes the page title, prose, lists, asides, tabs, code blocks, the edit/last-updated row and the prev/next pager.

**Why:** line length decides how easy text is to read. The comfortable range is roughly 45–75 characters per line, and about 66 is ideal. Past that, the eye loses its place on the long sweep back to the start of the next line, rereads lines or skips them, and long pages become tiring. Body text is 16px Mona Sans, a narrow face averaging ~7.3px per character, so 620px holds about 80 characters per line: already at the top of what reads comfortably. Wider columns look fuller on a big monitor and read worse at every size.

- Tables, mermaid diagrams and images may grow wider than 620px when their content needs it. They're scanned, not read line by line, so the measure doesn't apply to them.
- Size the measure in `rem`, never `ch`. `1ch` is the width of the "0" glyph (~10px here), so `70ch` is ~700px, or ~92 real characters.
- The landing page lays its sections out in a wider 72rem container, but its running text (intros, descriptions) still stays within a comfortable measure.
- If a wide screen feels empty, that space is the cost of readable text. The column is centred between the sidebar and "On this page" so the space sits evenly on both sides.

## Gutters

Desktop docs keep at least **60px** (`--aw-content-gutter`) between the content and the sidebar on the left, and between the content and "On this page" on the right.

**Why:** the gutter separates reading content from navigation. Without it, the TOC reads as part of the text, and the page feels cramped no matter how wide the screen is.

## Header navigation: keep it short

The top navigation stays sparse. It holds three section links (Docs, Workflow examples, Blog), plus search, GitHub, the theme toggle and one "Get started" button. Keep that count fixed: a new item goes in only by replacing an existing one.

**Why:** the header is the most valuable space on the site, and every item in it competes with every other. Three links can be read at a glance. Twelve can't, so visitors skim past all of them, including the one they needed. Prioritising is the point: the header carries only the few destinations most visitors need, on every page.

New features, tools, demos, launchers, announcements and secondary links go in the docs sidebar, the footer, or a link on the page where they're relevant.

## Visual direction

Near-monochrome: warm stone neutrals, near-black primary buttons, muted grey secondary text, hairline borders. The purple accent marks links, active states and focus, and is used sparingly. Colour is reserved for real product UI shown in mocks. Use the tokens in `docs/src/styles/tokens.css` rather than new hard-coded colours.

## Enforcement

`docs/scripts/check-reading-measure.mjs` runs before every docs build and fails it if the measure or gutter values change, or if `--sl-content-width` is overridden elsewhere. `docs/tests/content-width.spec.ts` checks the rendered layout.

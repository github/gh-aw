---
name: Daily Documentation Diagram
description: Creates one focused Primer-styled diagram for a rotating documentation page each day
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
  copilot-requests: write
network:
  allowed:
    - defaults
    - github
    - node
skills:
  - cathrynlavery/diagram-design/skills/diagram-design@648c2a597839301e06df1e7434a08bde9f42eed3
tools:
  cache-memory:
    retention-days: 30
    allowed-extensions: [".json"]
  edit:
  bash:
    - "*"
safe-outputs:
  steer: true
  create-pull-request:
    title-prefix: "[docs-diagram] "
    labels: [documentation, automation]
    draft: true
    if-no-changes: ignore
    protected-files: blocked
    allowed-files:
      - "docs/src/content/docs/*.mdx"
      - "docs/src/content/docs/**/*.mdx"
      - "docs/src/assets/diagrams/*.svg"
      - "docs/src/assets/diagrams/**/*.svg"
    max-patch-files: 2
    max-patch-size: 512
  noop:
engine:
  id: codex
  model-provider: openai
model: openai/gpt-5.3-codex
strict: true
timeout-minutes: 20
evals:
  - id: document_selected
    question: Did the workflow select the next suitable documentation page using its persisted round-robin state?
  - id: primer_diagram_created_or_noop
    question: Did the workflow create a valid Primer-styled SVG diagram and link it from the selected page, or explicitly noop when no suitable page exists?
---

# Daily Documentation Diagram

Create one small, useful diagram for the next eligible documentation page. Keep the change focused: exactly one new SVG and one update to the selected MDX page.

## Select the page

1. List tracked `docs/src/content/docs/**/*.mdx` files, sorted lexicographically. Exclude `index.mdx`, changelog/blog content, generated content, and pages already containing a `docs/src/assets/diagrams/` image.
2. Read `/tmp/gh-aw/cache-memory/state.json` if it exists. Its shape is `{"last_path":"docs/src/content/docs/...mdx"}`.
3. Select the first suitable path after `last_path`, wrapping to the first path. If the state is absent, malformed, or names a removed path, start from the first suitable path.
4. Read only the selected page and any directly linked local reference pages needed to understand it. Skip the selected page when a diagram would not improve comprehension; continue through at most eight consecutive candidates.
5. Always write the last candidate considered to `state.json` before finishing so the next run advances. Do not write any other cache-memory file.

## Create the diagram

1. Read and apply the installed `diagram-design` skill's layout guidance.
2. Choose the smallest diagram-design layout that explains one concrete concept from the selected page. Prefer 3–7 labelled elements and one clear relationship or flow. Do not make a decorative diagram or repeat prose without adding understanding.
3. Create `docs/src/assets/diagrams/<page-slug>-<YYYY-MM-DD>.svg`. The SVG must be self-contained: no JavaScript, external images, web fonts, or network references.
4. Match GitHub Primer aesthetics rather than diagram-design's default palette:
   - paper `#ffffff` with optional canvas `#f6f8fa`
   - foreground `#1f2328`, muted text `#59636e`, border `#d0d7de`
   - use `#0969da` only for the one focal relationship or action
   - use a system/Mona Sans-compatible font stack, 1px rules, square or minimally rounded corners, and no shadows or gradients
5. Make the SVG accessible: include `role="img"`, a unique `aria-labelledby`, and first-child `<title>` and `<desc>` elements. Use concise, readable labels and preserve sufficient contrast.
6. Add the diagram immediately after the section it explains, using the repository's existing MDX image syntax and meaningful alt text. Do not change unrelated wording or frontmatter.

## Validate and publish

1. Confirm the patch contains only the selected MDX page and the new SVG. Confirm the SVG has a `viewBox`, accessibility metadata, no `<script>` element, and no external URL.
2. Run `npm run build` from `docs/`. Revert the diagram and page change if the build fails.
3. If validation succeeds, use the pre-created `create_pull_request` safe output. Title it `[docs-diagram] Add <short concept> diagram` and summarize the selected page, concept, and validation result.
4. If no suitable page exists or validation fails, use `noop` with the number of candidates considered and a short reason. Do not create a placeholder diagram.

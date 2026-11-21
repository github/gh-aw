# Startlight Docs

## Project Structure

Inside of your Astro + Starlight project, you'll see the following folders and files:

```text
.
├── public/
├── src/
│   ├── assets/
│   ├── content/
│   │   └── docs/
│   └── content.config.ts
├── astro.config.mjs
├── package.json
└── tsconfig.json
```

Starlight looks for `.md` or `.mdx` files in the `src/content/docs/` directory. Each file is exposed as a route based on its file name.

Starlight renders the frontmatter `title` automatically. An opening Markdown
`#` heading with the same text is deduplicated at build time, ignoring inline
formatting, capitalization, and whitespace. Its anchor remains available for
existing links. Distinct headings and headings after body content are preserved.
Run `npm run test:titles` to check this behavior for Markdown and MDX pages.

Images can be added to `src/assets/` and embedded in Markdown with a relative link.

Static assets, like favicons, can be placed in the `public/` directory.

## 🧞 Commands

All commands are run from the root of the project, from a terminal:

| Command                   | Action                                           |
| :------------------------ | :----------------------------------------------- |
| `npm install`             | Installs dependencies                            |
| `npm run dev`             | Starts local dev server at `localhost:4321`      |
| `npm run build`           | Build your production site to `./dist/`          |
| `npm run preview`         | Preview your build locally, before deploying     |
| `npm run astro ...`       | Run CLI commands like `astro add`, `astro check` |
| `npm run astro -- --help` | Get help using the Astro CLI                     |

### Install through the 1ES feed

If access to npmjs is blocked, run this from `docs/`:

```sh
npm ci \
  --registry=https://ms-feed-25.pkgs.visualstudio.com/1es-public/_packaging/npm-public/npm/registry/ \
  --replace-registry-host=npmjs
```

Astro and Starlight are pinned to feed-supported releases. Keep the checked-in
lockfile and registry configuration unchanged when installing. Metadata
availability does not guarantee that the feed can serve a package's tarball.
After updating dependencies, run `make check-npm-1es` from the repository root
to verify that the locked versions can be downloaded.

## Interactive talk

The [interactive talk](https://github.github.com/gh-aw/cracktro/) is served from
`public/cracktro/` and ships with the normal docs build and Pages deployment.
It is a standalone slide deck, separate from the homepage slideshow, linked
from the site footer. The deck is laid out as a table of horizontal bands: the
title, the factory mindset, trust the process and the finale are rows of their own, and every other band (Workflows,
Security, Human agent processes, Cost Optimization, Agentic Engineering, Enterprise, Central Agentic Ops, AW factory) starts with a header
slide that lists the band's slides (click or tap a tile to jump) and a map of all bands.
The Central Agentic Ops (CAO, `gh cao`) band covers a research prototype, so each of its
slides carries a "Research prototype" badge.
Clicking a slide never changes it; on touch screens, swipe left/right to step, up/down to walk a column or switch bands, and pinch in to zoom out to the slide map.
Left/Right (or Space) step through the slides of the main line in reading
order and wrap between bands, and Page Up/Page Down (or Shift+Up/Down) move to the band above or
below like a text cursor: they keep the horizontal position (clamped to the band's length) until
Left/Right or a jump moves it. Shift+Home (or the first ⟪ button on small screens) returns to the
first slide of the band. Some Human agent processes patterns head a column of
worked examples: a lit ↓ button with the example count appears in the toolbar,
Up/Down walk down the column and back (past either end they switch bands, as
they do on slides without a column), U goes back to the pattern, and Left/Right from an example return to
the main line. Typing a slide number then Enter jumps to that slide, like clicking its dot. Home/End jump to the first/last slide, F for fullscreen, the notes toolbar button (or N) toggles speaker notes,
and M to toggle sound. G opens the slide map, whose bottom section is a keyboard manual: a full
keyboard with the shortcut keys lit and combos joined by arcs, a phone that loops every touch gesture
(point at one to play it alone), then a list of every shortcut. Notes explain what the slide shows, why the feature exists,
and when to use it for technical audiences new to AW; notes written as `Label: text` lines
show each labelled cue as its own block, and other notes stay plain text. They stay open as you navigate;
Escape hides them, and long notes scroll without advancing the slide.
The [speaker-notes skill](../.github/skills/cracktro-speaker-notes/SKILL.md)
sets the research-informed authoring tone. Notes are public and appear on the
same screen as the slides, not in a private presenter view.
K (or the kiosk dock button) starts kiosk mode for unattended
screens: the deck goes fullscreen where the browser allows it (otherwise it stays in
the window) and advances every 15 seconds through the main-line slides of the current tour or
view, skipping pattern examples, wrapping from the last slide to the first. The toolbar, dots
and notes hide; only a stop button and the sound toggle beside it stay, fading after a few idle
seconds. Starting kiosk mode with sound on lets each band's music play; the toggle (or M)
mutes or resumes it. K, Escape, the stop
button or leaving fullscreen stops kiosk mode. Kiosk mode starts with closed captions on; C (or the
CC button in the kiosk dock) turns them off or on, and the choice holds for later kiosk runs. Like a muted TV in a bar or airport, a short two-speaker news conversation
about each slide rolls in as captions at the bottom of the screen, and each slide stays up until its
conversation ends. C starts kiosk mode if needed, and captions end with it; while it runs, Escape stops it before it
hides notes. A manual move restarts the countdown, and an open
dialog holds the current slide. The slide map button (or G) opens a full-screen grid of
every slide under its band's title, each pattern's examples beside it, with the current slide focused and the tiles fanning in from it; arrows move between
tiles and Enter or a click jumps there. The search button next to the sound toggle (or `/`,
Ctrl/Cmd+K) opens a full-screen dialog that fuzzy-matches slide titles, the
`slides.md` summaries and keywords, and slide text; arrows pick a result, Enter opens it, and
`#<id>` jumps to that slide. Hovering a dock button shows its keyboard shortcut. The navigation dots form a map with one row per band and
jump directly to a slide (on narrow screens they become previous/next slide arrows
flanked by track-style previous/next band buttons; at 640px and below they slide
right-aligned under the toolbar). Each slide has a stable id
(its `slides.md` Id), so `#<id>` hashes are shareable slide links that survive
added or moved slides; old `#N` links redirect to the id. Tours are curated
subsets of the deck (Overview, Security deep dive, ...), listed at the bottom of
the slide map: picking one adds `&tour=<name>` to the hash, a pill shows the tour
and position, navigation skips the other slides, and × (or X) leaves the tour. Examples
in a pattern's column only appear together with their pattern. "Design a view"
in the slide map lets you tick slides or whole bands and copy a link that
encodes the selection as `&view=<base64url>`. Clicks never change slides.
On touch screens, swipe left or right within a band, up or down between bands,
and pinch in for the slide map. On phones the toolbar dims after a
few idle seconds and brightens on any touch, key or focus. iPhone Safari offers no
page fullscreen; to present without the browser bars, use
Share → Add to Home Screen and open the talk from that icon. Slides within a band push left, and
moving to a later band slides up (reversed when going back). Slides built on a gh-aw
workflow show its original `/agentic-workflows` prompt along the top; click it to
open the full prompt in a phone-style dialog (or press P; Escape or Done closes it).
Link previews (Open Graph, Slack, X) use `public/cracktro/og-image.png`, a
1200x630 render of the title slide; regenerate it if the title slide changes.
The ordered slide list, with a one-sentence summary per slide, lives in
[`.github/skills/cracktro/slides.md`](../.github/skills/cracktro/slides.md).
The docs build (and dev server) adds it to the page's JSON-LD as `hasPart`
entries with `#<id>` links, so crawlers and agents can find a slide without
running the deck; `src/lib/cracktro/` holds the parser and Astro integration.

The [daily Cracktro maintainer](../.github/workflows/daily-cracktro-maintainer.md)
uses `copilot/claude-opus-5.5` to check for missing shipped features and outdated
content. It proposes one focused draft PR after local browser checks, waits
while its previous PR is open, and leaves historical data snapshots intact.

Examples, review activity, repository counts, and cost figures are synthetic,
except the model routing slide's results, which are measured on this
repository's own workflows, the Issues as intent slide's counts of
issues labelled `community` and the merged pull requests that closed them
(November 2026 snapshot), the Mine · Refine · Apply slide's dated feed and
counts of Linter Miner analyzer pull requests, LintMonster issues and the
merged pull requests that closed them (May to November 2026), the Go Fan slide's counts of daily
library review issues and the merged pull requests that closed them
(February to November 2026, with shortened titles), the Continuous Red Teaming slide's
counts and dates, measured from that red-team workflow's repository memory and
the firewall fix it led to (its attack diagram is a simplified illustration),
the Semantic
clustering slide's functions, files and counts from one Semantic Function
Refactoring report (7 May 2026) and the merged pull request that closed it, and the three data slides before the finale: "First
commit to GA", which shows this repository's measured public history from its
first commit to general availability at GitHub Universe, and "How PRs land",
which shows measured time-to-merge and lines-changed distributions of its
merged pull requests, and "How workflows evolve", which shows a statistical
study of the commits that changed its own workflow Markdown. All say so in their speaker notes.
Use role icons rather than employee profiles, and never real GitHub avatars or
handles. The only people shown are the fake users `mona` (Mona the Octocat) and
`ducky` (the rubber duck), GitHub's two mascots, as local avatars in
`public/cracktro/assets/avatars/`.
Keep private repository
references, source commit identifiers, and internal attributions out of the
public assets. Speaker notes and source comments are public too.
The workflow illustration, avatars, and fonts are local.
The Enterprise band's OpenTelemetry, GitHub-native governance and
operator-owned infrastructure diagrams come from `public/cracktro/ops-visuals.js`,
a dependency-free script that sets `window.AW_OPS_VISUALS` for the deck. The
homepage imports the same file at build time for its matching sections, so
edit those diagrams in one place and keep them free of external references.
The deck engine (stage, navigation dock, band headers, search, slide map, `#<id>`
hashes, tours and animated background) lives in `public/cracktro/runtime.js`, a
talk-agnostic classic script that sets `window.CracktroDeck`. `deck.js` holds the
talk's slides, styles, prompts, band themes and synth, and ends with
`CracktroDeck.create({ slides, themes, bands, ... })`; another deck can reuse the
runtime by loading `runtime.js` and calling `create` with its own slides. The
API is declared in `public/cracktro/runtime.d.ts`; run `npm run check:cracktro`
from `docs/` to type-check the runtime and its contract. The docs build runs
this check too. The page needs no build or module loader and also works when
opened from disk.
Mona Sans reuses `public/fonts/`; Monaspace Neon and its SIL Open Font License
are in `public/cracktro/fonts/`.

## Homepage slideshow

On desktop, select the presentation icon in the homepage header to present the
landing page as an eleven-slide deck. The security trifecta and its six
defence layers, and the cost dashboard and budget guardrails, have separate
slides. The slideshow runtime, drawing tools, and
snippet expansion JavaScript are loaded on demand on the first click, not
when browsing the page. Slides fill the window edge to edge and retain their
interactive examples. Use the previous/next buttons, arrow keys or Page Up/Page
Down to navigate, Home/End to jump to the first/last slide, and Escape or the
close button to return to the page. Keyboard controls inside demos keep their
normal behavior. The presentation icon is hidden below the desktop menu breakpoint
(50rem), and resizing to mobile exits the slideshow.
Supported browsers slide the full frame left or right with CSS View Transitions;
headings, text, list items, and demo blocks fade and rise into place in reading
order. Each entrance sequence finishes within one second, including the frame
transition, and replays when revisiting a slide. Decorative backgrounds stay
stationary. Reduced-motion preferences disable both animations.
The rounded, compact toolbar floats at the bottom center without reserving
footer space. It rests at 65% opacity and becomes opaque on hover, keyboard
focus, or while its drawing palette is open.
Add `data-slideshow-hide` to any element to omit secondary content from the
presentation without hiding it on the normal page. On a whole slide section,
the attribute skips that slide and updates the navigation count. Guided-form
notes, lengthy workflow descriptions, the extra workflow catalog, and the
custom-engine footnote already use this annotation.
When a demo tab is focused, Left/Right select tabs; Up/Down and Page Up/Page Down
still navigate slides, and Tab/Shift+Tab leave the tablist.

The pencil button opens drawing tools inspired by
[Microsoft Streamer](https://github.com/microsoft/streamer): colored rectangles
(hold Shift for squares), arrows, and emoji stamps. Click to stamp an emoji or
drag to resize and rotate it. Select the pointer tool to interact with demos.
Undo (also Ctrl/Cmd+Z) and clear apply to the current slide. Drawings stay with
their slide while presenting and are cleared when exiting. Escape leaves
drawing mode and clears all annotations without leaving the slide; a second
Escape exits the presentation.
Drawing instructions appear in the question-mark tooltip on hover or keyboard focus.

Click a code snippet, prompt, or example output to expand it to a full-window
view with larger type. Enter/Space also expand a focused snippet. The view
zooms and fades smoothly (unless reduced motion is enabled); Escape or its
close button returns focus to the snippet without changing slides.
Code in the expanded view is editable as plain text for live demonstrations.
Edits affect only the expanded copy and are discarded when it closes; opening
the snippet again restores its original contents.

## Structured data

`src/components/CustomHead.astro` emits the homepage graph and a `TechArticle`
for docs pages. Blog pages get a single `BlogPosting` and breadcrumbs from
`starlight-blog`; do not add another `BlogPosting`. `CustomHead.astro` enriches
the plugin entry with the site `publisher` and, when `dateModified` is absent,
uses `lastUpdated` or falls back to `datePublished`. Breadcrumbs are preserved.

## Redirect fragments

Moved pages use the `redirects` map in `astro.config.mjs`. After a production
build, `src/lib/redirect-fragments.js` adds a script before each verified
meta refresh so `/gh-aw/workshop/#x` redirects to
`/gh-aw/guides/workshop/#x`. A destination's own fragment takes precedence:
`/gh-aw/reference/servicenow-connector/#other` redirects to
`/gh-aw/reference/connectors/#servicenow`.

The integration supports static string redirects to root-relative paths or
absolute HTTP(S) URLs. Object-form and dynamic redirects, protocol-relative
destinations, and destinations containing whitespace, control characters or
backslashes fail the build. Missing or mismatched meta refreshes also fail
the build rather than patching an unverified page.

This enhancement applies to built static pages, not the dev server. Without
JavaScript, the original meta refresh still redirects but loses the incoming
fragment. Run the rewrite unit tests from the repository root with
`make test-docs-redirect-fragments`.

## ⚠️ Known Dev-Mode Limitations

### Sitemap behavior in dev and production

The robots file references `/gh-aw/sitemap.xml`, which is the stable sitemap entrypoint for the docs site.

During a production build (`npm run build`), Astro generates `/gh-aw/sitemap-index.xml`, and the static `/gh-aw/sitemap.xml` entrypoint points crawlers at that generated sitemap index. The generated sitemap index is not available when running the local development server (`npm run dev`).

If a CI pipeline or automated tool checks the generated sitemap index URL during a local preview, it will receive a 404 response. To verify the production sitemap flow, run `npm run build` followed by `npm run preview`.

### Robots/AI discovery paths on GitHub Pages project sites

This docs site is deployed as a GitHub Pages **project site** under `/gh-aw/` (see `base: '/gh-aw/'` in `astro.config.mjs`).

- `robots.txt` is served at `/gh-aw/robots.txt`
- AI discovery file is served at `/gh-aw/.well-known/ai.txt`
- AI metadata files are served under `/gh-aw/ai/`

Root-level endpoints on `https://github.github.com/` (for example `/robots.txt`) are controlled by the main `github.github.com` site, not this repository.

## Want to learn more?

Check out [Starlight’s docs](https://starlight.astro.build/), read [the Astro documentation](https://docs.astro.build), or jump into the [Astro Discord server](https://astro.build/chat).

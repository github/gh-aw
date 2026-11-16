---
name: cracktro
description: Edit, preview, and verify the standalone interactive talk deck in docs/public/cracktro (slides, SVG diagrams, transitions, WebAudio sound).
---

# Cracktro talk deck

Use Claude Opus 5.5 (`claude-opus-5.5`) for deck work, including any subagents you delegate to.

The deck is published at `https://github.github.com/gh-aw/cracktro/`. It is a dependency-free deck of horizontal bands (Prezi-style), separate from the docs homepage slideshow.

## Agent navigation and maintenance (mandatory)

- Find slides in `slides.md`: row N is slide N (`#N`); read its Summary before opening `deck.js`.
- When a change adds, removes, moves, renames or changes a slide's message, update its `slides.md` row in the same change. Summaries are public, durable takeaways without aging counts or dates; band headers start `Band header (<band>):`.
- Never copy slide lists, numbers or summaries elsewhere (`index.html` included): the docs build and dev server inject every row into the page's JSON-LD `hasPart`; an invalid row fails the build.

## Files

- `docs/public/cracktro/index.html`: loads `deck.js` and holds the SEO/Open Graph/Twitter meta (absolute `https://github.github.com/gh-aw/cracktro/` URLs).
- `docs/public/cracktro/og-image.png`: 1200x630 social card of the title slide, UI hidden. Keep it outside `assets/` (avatars only).
- `docs/public/cracktro/deck.js`: about 950 KB: CSS, slides, navigation, synth, background. **Never read it whole.** `grep -n` the `// ---------- <section> ----------` and `// <Slide title>` markers, then `view_range`.
- `docs/public/cracktro/assets/avatars/{mona,ducky}.jpg`: the fake users Mona and Ducky (GitHub's mascots) and `mona-full.png` (full-body Mona); the only assets a test allows. Diagrams are inline SVG built in `deck.js` (e.g. the CI/CD meets Continuous AI `GA_RUNS` pipeline).
- `docs/public/cracktro/fonts/`: Monaspace Neon plus its OFL licence. Mona Sans comes from `docs/public/fonts/`.
- `docs/tests/talk.spec.ts`: Playwright tests. They check the deck, parser and built JSON-LD against `slides.md`.
- `.github/skills/cracktro/slides.md`: the ordered slide list: Heading, Notes (`illustrative` or empty), Summary; band headers included.
- `docs/src/lib/cracktro/`: `slides.js` parses `slides.md` and renders JSON-LD; `integration.js` (Astro) applies it.
- `docs/README.md`, "Interactive talk" section: user-facing description (keys, public-content rules).

## deck.js layout (search for these markers)

| Marker | Contents |
|---|---|
| `---------- styles ----------` | One CSS template string. Slide classes use prefixes (`atk-`, `mra-`, `eph-`, `bh-` for band headers, ...); check a new prefix is unused. |
| `---------- helpers ----------` | `h(tag, attrs, ...kids)` creates DOM; `letters(text, cls)` builds per-letter headings with classes like `l-grad` and `l-out`; `resetIndex()`. |
| `---------- slides ----------` | One `{ ... slides.push({...}) }` block per slide, each preceded by a `// Title: summary` comment. |
| `const OCTI` | Octicon path map (calendar, issue, pr, merge, person, copilot, ...) used by `ico(name,x,y,size,fill)`. |
| `---------- sound:` | WebAudio synth `sfx` (`slide, band, step, cut, techno, pop, key, compile, review, notify, omen, route, mr, toggle, unlock`); `MUSIC` has one theme per band; `slide(dir, music, shift)` transposes by `shift` semitones. |
| `---------- band themes ----------` | `THEME`: per-band background, grid, angle, accent, red wash and music (see `mechanics.md`). |
| `---------- navigation ----------` | `go()`, `show(n, horiz)`, keys, dots, notes, hash. |
| `---------- search ----------` | Slide search dialog (`fz-` CSS prefix, dock button `.fzb`): `fzBuild()` lazily indexes titles, JSON-LD `hasPart` summaries and slide text + notes; `fzBounds()` finds the tightest in-order match window used for scoring and `<mark>` highlights. |
| `---------- animated background ----------` | Canvas background and grid driven by the current band's `THEME`. |

The stage is a fixed 1920x1080 frame (`W`/`H`), scaled to fit the window.

## Slide object

```js
slides.push({
  notes: "...",           // speaker notes (N key); public text
  group: "security",      // band id; picks the THEME (colours, grid, music, red wash); crossing groups slides up
  experimental: "lsp",    // optional; badge label from the feature's `cracktro` entry in pkg/workflow/data/experimental_features.json (the single source of truth; talk.spec.ts enforces it); renders the top-right .xbadge
  key: "unsloppers",      // optional; names a pattern slide that heads a vertical column of examples
  under: "unsloppers",    // optional; hangs this slide in the column below the same-group slide with that key (Up/Down; see mechanics.md)
  techno: 1,              // optional techno layer level
  el: h("section", { class: "slide" }, ...),
  enter(dir) {},          // reset per-slide state; dir = 1 forward, -1 back
  tick(t) {},             // called every frame, ONLY for the current slide
  onNext() {}, onPrev() {}, onKey(e) {}, // return true to consume (in-slide steps)
});
```

## Reference files (load only what the task needs)

| File | Load when |
|---|---|
| `mechanics.md` | Changing bands, slide order or `group`, navigation, transitions, backgrounds, audio, or animated SVG flows (`EDGES`/`SEQ`/`LANE`). |
| `content-rules.md` | Adding any public text, data, avatar or image; contains the per-slide Exception bullets for measured data. |
| `workflow-prompts.md` | Adding or refreshing a slide's `/agentic-workflows` prompt line (`PROMPTS` table and phone dialog); holds the prompt-mining algorithm and the exact PR/task/session references. |
| `data-refresh.md` | Adding, changing or refreshing a data slide; holds the refresh mini-skill convention and the slide→`refresh/<slug>.md` index. |

## People and mascots

- Never use a real GitHub user's avatar, handle or name for a person, in slides, speaker notes or comments. Never fetch `github.com/<login>.png`.
- People are only the fake users `mona` and `ducky`, drawn from `assets/avatars/{mona,ducky}.jpg`. Mona is the maintainer; Ducky is the external contributor (community user). A `Developer` can be either: mix them across slides, and use both when a slide shows two developers.
- Follow https://brand.github.com/graphic-elements/mascots: use mascots sparingly, never as logos, and never on serious topics (security, enterprise, money). Use role icons there.
- Never generate or redraw mascots. Both avatars are 160x160 clips of official brand.github.com 3D art (`mona.jpg`: dark-mode purple Mona from `mona-2026-versions.png`; `ducky.jpg`: middle Ducky from `mascots-03.png`), backgrounds recoloured to the deck's `#0d1117`, never white.

Core rules (details in `content-rules.md`): everything is public; no real people (see above); no 40-hex SHAs or private repos; the only URLs are `https://github.github.com/gh-aw/introduction/overview/` and the For PMs source link `https://github.com/chrizbo/agentics-beyond-code`; speaker notes of illustrative slides must match `/illustrative|synthetic|simulated/i`.

## Preview loop

No build step (the static page lacks the injected JSON-LD). Serve the folder and open it in the browser canvas:

```bash
python3 -m http.server 4329 --bind 127.0.0.1 --directory docs/public   # run async + detached
# open http://127.0.0.1:4329/cracktro/#11   (#N = row N of slides.md)
```

After each edit:

1. Run `node --check docs/public/cracktro/deck.js`.
2. In the canvas, run `location.reload()`, wait, and take a screenshot.
3. Jump to a slide: click `[aria-label="Slide N"]` or set the hash.
4. Dispatch key events on `window`, e.g. `dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowRight'}))`.
5. Catch a transition: read `getComputedStyle(el).transform` ~200 ms after navigating.

## Tests

Run from `docs/` after `npm ci`: `npx playwright test tests/talk.spec.ts`. The config builds and previews the site (`npm run build && npm run preview`, `localhost:4321`, `/gh-aw/cracktro/`, Chromium at `/usr/bin/chromium-browser`), so it covers the built JSON-LD, not dev; under `npm run dev`, check `curl -s localhost:4321/gh-aw/cracktro/ | grep hasPart`. If `node_modules` is missing, report the tests as not run.

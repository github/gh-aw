---
name: cracktro
description: Edit, preview, and verify the standalone interactive talk deck in docs/public/cracktro (slides, SVG diagrams, transitions, WebAudio sound).
---

# Cracktro talk deck

Use Claude Opus 5.5 (`claude-opus-5.5`) for deck work, including subagents.

Published at `https://github.github.com/gh-aw/cracktro/`: a dependency-free deck of horizontal bands (Prezi-style), separate from the homepage slideshow.

## Agent navigation and maintenance (mandatory)

- Find slides in `slides.md` by Id; link them as `#<id>` (stable; never `#N`, the layout is two-dimensional). Read the Summary before opening `deck.js`.
- When you add, remove, move, rename a slide or change its message, update its `slides.md` row (Summary and Keywords) in the same change. Summaries are public, durable takeaways without aging counts or dates; band headers start `Band header (<band>):`. Keywords are required, comma-separated, accurate search terms (synonyms, acronyms, option and pattern names); the build publishes them in JSON-LD and the deck's `/` search matches them.
- Never copy slide lists, numbers or summaries elsewhere (`index.html` included): the docs build and dev server inject every row into JSON-LD `hasPart`; an invalid row fails the build.
- For speaker notes, load `../cracktro-speaker-notes/SKILL.md`: concise What/Why/When cues for technical newcomers, with research-informed tone and preserved evidence caveats.
- Before adding public content, read `content-rules.md` and the People and mascots section below. Before verification, read Preview loop and Tests below; before publishing, read Pull requests below (screenshots required for slide changes).

## Files

- `docs/public/cracktro/index.html`: loads `ops-visuals.js`, `runtime.js`, `deck.js` (classic scripts; `file://` works); holds SEO/Open Graph/Twitter meta (absolute `https://github.github.com/gh-aw/cracktro/` URLs).
- `docs/public/cracktro/og-image.png`: 1200x630 title-slide social card, UI hidden; keep it outside `assets/` (avatars only).
- `docs/public/cracktro/runtime.js`: talk-agnostic `window.CracktroDeck` runtime (stage, chrome, band headers, navigation, search, map, background). Keep talk content out.
- `docs/public/cracktro/deck.js`: about 950 KB: deck CSS, slides, prompts, themes, bands, synth. **Never read it whole.** `grep -n` `// ---------- <section> ----------` and `// <Slide title>` markers, then `view_range`.
- `docs/public/cracktro/assets/avatars/{mona,ducky}.jpg` and `mona-full.png` (full-body Mona): the only assets a test allows. Diagrams are inline SVG in `deck.js` (e.g. the `GA_RUNS` pipeline).
- `docs/public/cracktro/fonts/`: Monaspace Neon plus its OFL licence. Mona Sans comes from `docs/public/fonts/`.
- `docs/tests/talk.spec.ts`: Playwright tests of the deck, runtime, parser and built JSON-LD against `slides.md`.
- `.github/skills/cracktro/slides.md`: ordered slides: Heading, Notes (`illustrative` or empty), Summary, Keywords; band headers included.
- `docs/src/lib/cracktro/`: `slides.js` parses `slides.md` and renders JSON-LD; `integration.js` (Astro) applies it.
- `docs/README.md` "Interactive talk": user-facing description (keys, public-content rules).

## Layout (search for these markers)

| Marker | Contents |
|---|---|
| `---------- styles ----------` | Deck CSS string. Slide classes use prefixes (`atk-`, `mra-`, `eph-`, `bh-` band headers, ...); check a new prefix is unused. |
| `---------- helpers ----------` | From the runtime: `h(tag, attrs, ...kids)` DOM; `letters(text, cls)` per-letter headings (`l-grad`, `l-out`); `resetIndex()`. |
| `---------- slides ----------` | One `{ ... slides.push({...}) }` block per slide after a `// Title: summary` comment. |
| `const OCTI` | Octicon paths for `ico(name,x,y,size,fill)`. |
| `---------- sound:` | WebAudio synth `sfx` (methods in `mechanics.md`); `MUSIC` themes; `slide(dir, music, shift)` transposes `shift` semitones. |
| `---------- band themes ----------` | `THEME` per band (see `mechanics.md`). |
| `---------- start ----------` | `CracktroDeck.create({ width, height, css, slides, themes, bands, solo, prototypes, last, sound, tours })`; `TOURS` above it holds the hard-coded tours (`mechanics.md`); `slides` may be a factory of `{ h, letters, resetIndex, sound, current }`. |
| runtime.js `create`, `navigation`, `search`, `slide map`, `animated background` | Rows, headers, chrome; `go()`, keys, dots, notes, hash; `fz-` search; map; `THEME` canvas grid. |

The stage is a fixed 1920x1080 frame (`W`/`H`) scaled to the window.

## Slide object

```js
slides.push({
  notes: "...",           // public What/Why/When cues; toolbar button or N
  group: "security",      // band id; picks the THEME; crossing groups slides up
  experimental: "lsp",    // optional .xbadge; label only from the feature's `cracktro` entry in pkg/workflow/data/experimental_features.json (test-enforced)
  key: "improvers",      // optional; pattern slide heading a column of examples
  under: "improvers",    // optional; hangs below the same-group slide with that key (mechanics.md)
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
| `mechanics.md` | Changing bands, slide order, `group`, navigation, transitions, backgrounds, audio or SVG flows (`EDGES`/`SEQ`/`LANE`). |
| `content-rules.md` | Adding public text, data, avatars or images; per-slide Exception bullets for measured data. |
| `workflow-prompts.md` | Adding or refreshing a slide's `/agentic-workflows` prompt (`PROMPTS`, phone dialog); prompt mining and PR/task/session references. |
| `data-refresh.md` | Adding, changing or refreshing a data slide; refresh mini-skills and the slide→`refresh/<slug>.md` index. |
| `brand.md` | Any colour, gradient, glow, title or font change; brand/Primer palette, semantic hues, regularization pass. |
| `concept-style.md` | Adding or restyling a concept slide in the style of The factory mindset and Trust the process (light header-less row, problem/answer statements, one live metaphor, horizontal split motion). |
| `asset-generator.md` | Setting up or using the `asset-generator` MCP (og/social cards, wallpapers, charts, `create_slide_deck`); where outputs may live. |

<!-- instruction-prefix-end -->

## People and mascots

- Never use a real GitHub user's avatar, handle or name for a person in slides, notes or comments; never fetch `github.com/<login>.png`.
- People are only the fake users (GitHub mascots) `mona` (maintainer) and `ducky` (external community contributor), from `assets/avatars/`. A `Developer` can be either: mix them across slides; use both for two developers.
- Follow https://brand.github.com/graphic-elements/mascots: mascots sparingly, never as logos or on serious topics (security, enterprise, money); use role icons there.
- Never generate or redraw mascots. Avatars are 160x160 clips of brand.github.com 3D art (`mona.jpg`: dark-mode purple Mona, `mona-2026-versions.png`; `ducky.jpg`: middle Ducky, `mascots-03.png`), background `#0d1117`, never white.

Core rules (`content-rules.md`): everything is public; no real people; no 40-hex SHAs or private repos; the only URLs are `https://github.github.com/gh-aw/introduction/overview/`, the For PMs source link `https://github.com/chrizbo/agentics-beyond-code` and the repo-widget star endpoint `https://api.github.com/repositories/` (https pages only); speaker notes of illustrative slides must match `/illustrative|synthetic|simulated/i`.

Brand (`brand.md`): neutral canvas and text with one accent hue per slide; titles solid `#f0f6fc` or a single-hue gradient, never rainbow; Mona Sans at `"wdth" 100`, Monaspace for code and labels.

Colour accessibility: never use red/green as the only pair for opposing states (pass/fail, allowed/blocked); colour-blind viewers cannot tell them apart. Use blue (`#4493f8`/`#79c0ff`) vs orange (`#db6d28`/`#ffa657`) and add a non-colour cue (label, icon, dash or strike).

## Preview loop

No build step (static page lacks injected JSON-LD). Serve and open in the browser canvas:

```bash
python3 -m http.server 4329 --bind 127.0.0.1 --directory docs/public   # run async + detached
# open http://127.0.0.1:4329/cracktro/#security   (#<id> = Id column of slides.md)
```

After each edit:

1. Run `node --check` on each edited `docs/public/cracktro/*.js`.
2. Run `location.reload()`, wait, screenshot.
3. Jump: click `[aria-label="Slide N"]` or set the hash.
4. Dispatch keys on `window`: `dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowRight'}))`.
5. Transitions: read `getComputedStyle(el).transform` ~200 ms after navigating.

Screenshots are always 16:9. The canvas panel can be any shape and cannot be resized, so never screenshot the whole page. Capture the stage element instead: `screenshot_page` with `selector: ".stage"` and `scroll_into_view_if_needed: false`. This returns only the 1920x1080 frame, scaled, without the navigation chrome. To check the chrome (buttons, dots, minimap), take a separate full-page screenshot.

## Tests

Run from `docs/` after `npm ci`: `npx playwright test tests/talk.spec.ts`. The config builds and previews (`npm run build && npm run preview`, `localhost:4321`, `/gh-aw/cracktro/`, Chromium `/usr/bin/chromium-browser`), covering built JSON-LD; under `npm run dev`, check `curl -s localhost:4321/gh-aw/cracktro/ | grep hasPart`. If `node_modules` is missing, report the tests as not run.

## Pull requests

A PR that adds or changes a slide must show it. Screenshot each changed slide from the preview as a 16:9 `.stage` capture (see Preview loop), at the step that carries its message (often the last), and upload the image into the PR description so it renders inline. Never commit screenshots. If you cannot upload images, say so in the PR description and ask the author to attach one.

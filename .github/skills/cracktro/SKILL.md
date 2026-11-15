---
name: cracktro
description: Edit, preview, and verify the standalone interactive talk deck in docs/public/cracktro (slides, SVG diagrams, transitions, WebAudio sound).
---

# Cracktro talk deck

Use Claude Opus 5.5 (`claude-opus-5.5`) for deck work, including any subagents you delegate to.

The deck is published at `https://github.github.com/gh-aw/cracktro/`. It is a deck laid out as horizontal bands (Prezi-style) with no dependencies, separate from the docs homepage slideshow.

## Files

- `docs/public/cracktro/index.html`: loads `deck.js` and holds the SEO/Open Graph/Twitter meta (absolute `https://github.github.com/gh-aw/cracktro/` URLs).
- `docs/public/cracktro/og-image.png`: 1200x630 social card rendered from slide 1 with the dots/count/sound/progress UI hidden. Keep it outside `assets/` (the asset test only allows avatars there).
- `docs/public/cracktro/deck.js`: about 575 KB holding everything (CSS, slides, navigation, synth, background). **Never read it whole.** Use `grep -n` on the `// ---------- <section> ----------` and `// <Slide title>` comment markers, then `view_range`.
- `docs/public/cracktro/assets/avatars/{mona,ducky}.jpg`: the fake users Mona and Ducky (GitHub's mascots), plus `mona-full.png` (transparent full-body Mona for the Trust the process slide); the only allowed assets, which a test enforces. Diagrams are inline SVG built in `deck.js` (for example slide 2's CI-to-agentic `GA_RUNS` pipeline).
- `docs/public/cracktro/fonts/`: Monaspace Neon plus its OFL licence. Mona Sans comes from `docs/public/fonts/`.
- `docs/tests/talk.spec.ts`: Playwright tests. They read `.github/skills/cracktro/slides.md`.
- `.github/skills/cracktro/slides.md`: the single ordered slide list (headings, band headers included, and which notes must say illustrative). A slide's number is its row; never write slide numbers or counts anywhere else. Edit one row per slide in the same change as `deck.js`.
- `docs/README.md`, "Interactive talk" section: user-facing description (keys, public-content rules).

## deck.js layout (search for these markers)

| Marker | Contents |
|---|---|
| `---------- styles ----------` | A single CSS template string. Slide-specific classes use prefixes such as `fm-`, `atk-`, `rpa-`, `mra-`, `eph-`, `fv-`, `sec-`, `ch-`, `pf-` and `bh-` (band header slides). Check that a new prefix is unused before adding it. |
| `---------- helpers ----------` | `h(tag, attrs, ...kids)` creates DOM; `letters(text, cls)` builds per-letter headings with classes like `l-grad` and `l-out`; `resetIndex()`. |
| `---------- slides ----------` | One `{ ... slides.push({...}) }` block per slide, each preceded by a `// Title: summary` comment. |
| `const OCTI` | Octicon path map (calendar, issue, pr, merge, person, copilot, ...) used by `ico(name,x,y,size,fill)`. |
| `---------- sound:` | WebAudio synth `sfx`: `slide, band, step, cut, techno, pop, key, compile, review, notify, omen, route, mr, toggle, unlock`. `MUSIC` holds one theme per band; `slide(dir, music, shift)` transposes `tone`/`bell`/`pad` by `shift` semitones. |
| `---------- band themes ----------` | `THEME`: per-band background, grid, angle, accent, red wash and music (see `mechanics.md`). |
| `---------- navigation ----------` | `go()`, `show(n, horiz)`, keys, dots, notes, hash. |
| `---------- animated background ----------` | Canvas background and grid driven by the current band's `THEME`. |

The stage is a fixed 1920x1080 frame (`W`/`H`), scaled to fit the window.

## Slide object

```js
slides.push({
  notes: "...",           // speaker notes (N key); public text
  group: "security",      // band id; picks the THEME (colours, grid, music, red wash); crossing groups slides up
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
- Never generate or redraw mascots. Both avatars are 160x160 clips of official brand.github.com art in the modern three-quarter 3D style: `mona.jpg` is the dark-mode purple Mona from `mona-2026-versions.png`, `ducky.jpg` the middle Ducky from `mascots-03.png`. Their backgrounds are recoloured to the deck's `#0d1117` (never white) so they blend into the dark avatar circles.

Core rules (details in `content-rules.md`): everything is public; no real GitHub avatars, handles or people, only the fake users Mona and Ducky (per the brand mascot guidelines); no 40-hex SHAs or private repos; the only URLs are `https://github.github.com/gh-aw/introduction/overview/` and the For PMs source link `https://github.com/chrizbo/agentics-beyond-code`; speaker notes of illustrative slides must match `/illustrative|synthetic|simulated/i`.

## Preview loop

There is no build step. Serve the static folder and open it in the browser canvas:

```bash
python3 -m http.server 4329 --bind 127.0.0.1 --directory docs/public   # run async + detached
# open http://127.0.0.1:4329/cracktro/#11   (#N = row N of slides.md)
```

After each edit:

1. Run `node --check docs/public/cracktro/deck.js`.
2. In the canvas, run `location.reload()`, wait, and take a screenshot.
3. To jump to a slide, click `[aria-label="Slide N"]` or set the hash.
4. Dispatch key events on `window`, for example `dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowRight'}))`.
5. To catch a transition, read `getComputedStyle(el).transform` about 200 ms after navigating.

## Tests

Run from `docs/` after `npm ci`: `npx playwright test tests/talk.spec.ts`. The tests use the Astro dev server on `localhost:4321` at the base path `/gh-aw/cracktro/`. If `node_modules` is missing, say the tests were not run rather than skipping silently.

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
- `docs/public/cracktro/assets/avatars/{pelikhan,lpcox,dsyme,mnkiefer,ievangelist,mona}.jpg`: the only allowed assets, which a test enforces. Diagrams are inline SVG built in `deck.js` (for example slide 2's CI-to-agentic `GA_RUNS` pipeline).
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
| `---------- sound:` | WebAudio synth `sfx`: `slide, band, step, cut, techno, pop, key, compile, review, notify, omen, route, mr, toggle, unlock`. `THEMES` holds the per-slide melodies. |
| `---------- navigation ----------` | `go()`, `show(n, horiz)`, keys, dots, notes, hash. |
| `---------- animated background ----------` | Canvas background tinted by the slide's `palette`. |

The stage is a fixed 1920x1080 frame (`W`/`H`), scaled to fit the window.

## Slide object

```js
slides.push({
  sound: "loop",          // THEMES key played on entry
  palette: [c1, c2, c3, c4],
  notes: "...",           // speaker notes (N key); public text
  group: "security",      // section id; crossing groups slides up (down going back), siblings push left
  red: true,              // optional: red wash covers the old slide, then this one enters
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

Core rules (details in `content-rules.md`): everything is public; no people except the allowed avatars; no 40-hex SHAs or private repos; the only URL is `https://github.github.com/gh-aw/introduction/overview/`; speaker notes of illustrative slides must match `/illustrative|synthetic|simulated/i`.

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

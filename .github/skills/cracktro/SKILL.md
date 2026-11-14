---
name: cracktro
description: Edit, preview, and verify the standalone interactive talk deck in docs/public/cracktro (slides, SVG diagrams, transitions, WebAudio sound).
---

# Cracktro talk deck

The deck is published at `https://github.github.com/gh-aw/cracktro/`. It is a sixteen-slide deck with no dependencies, separate from the docs homepage slideshow.

## Files

- `docs/public/cracktro/index.html`: only loads `deck.js`.
- `docs/public/cracktro/deck.js`: about 200 KB holding everything (CSS, slides, navigation, synth, background). **Never read it whole.** Use `grep -n` on the `// ---------- <section> ----------` and `// <Slide title>` comment markers, then `view_range`.
- `docs/public/cracktro/assets/avatars/{pelikhan,lpcox,dsyme,mnkiefer}.jpg`: the only allowed assets, which a test enforces. Diagrams are inline SVG built in `deck.js` (for example slide 2's CI-to-agentic `GA_RUNS` pipeline).
- `docs/public/cracktro/fonts/`: Monaspace Neon plus its OFL licence. Mona Sans comes from `docs/public/fonts/`.
- `docs/tests/talk.spec.ts`: Playwright tests. They contain the list of sixteen slide headings, so update it when a heading changes.
- `docs/README.md`, "Interactive talk" section: user-facing description (keys, public-content rules).

## deck.js layout (search for these markers)

| Marker | Contents |
|---|---|
| `---------- styles ----------` | A single CSS template string. Slide-specific classes use prefixes such as `fm-`, `rpa-`, `mra-`, `sec-`, `ch-` and `pf-`. |
| `---------- helpers ----------` | `h(tag, attrs, ...kids)` creates DOM; `letters(text, cls)` builds per-letter headings with classes like `l-grad` and `l-out`; `resetIndex()`. |
| `---------- slides ----------` | One `{ ... slides.push({...}) }` block per slide, each preceded by a `// Title: summary` comment. |
| `const OCTI` | Octicon path map (calendar, issue, pr, merge, person, copilot, ...) used by `ico(name,x,y,size,fill)`. |
| `---------- sound:` | WebAudio synth `sfx`: `slide, step, cut, techno, pop, key, compile, review, notify, omen, route, toggle, unlock`. `THEMES` holds the per-slide melodies. |
| `---------- navigation ----------` | `go()`, `show(n, horiz)`, keys, dots, notes, hash. |
| `---------- animated background ----------` | Canvas background tinted by the slide's `palette`. |

The stage is a fixed 1920x1080 frame (`W`/`H`), scaled to fit the window.

## Slide object

```js
slides.push({
  sound: "loop",          // THEMES key played on entry
  palette: [c1, c2, c3, c4],
  notes: "...",           // speaker notes (N key); public text
  trans: "up" | "down",   // optional vertical transition (reversed going back)
  red: true,              // optional: red wash, then slides in from right ("left-in")
  techno: 1,              // optional techno layer level
  el: h("section", { class: "slide" }, ...),
  enter(dir) {},          // reset per-slide state; dir = 1 forward, -1 back
  tick(t) {},             // called every frame, ONLY for the current slide
  onNext() {}, onPrev() {}, onKey(e) {}, // return true to consume (in-slide steps)
});
```

## Mechanics worth knowing

- **Transitions:** the default is a reveal-style horizontal push using `slide-in`/`slide-out` keyframes. `show()` sets `--dx` to `100%` going forward, `-100%` going back, and `0%` on first load. Up/down transitions use the `up-in`/`up-out` and `down-in`/`down-out` classes; red slides use `left-in`. The old slide keeps the `.leaving` class until a timeout clears it.
- **Audio isolation:** `master` is a persistent gain that feeds the compressor and delay. `out` is a per-slide bus. `show()` calls `sfx.cut()`, which fades the current bus and replaces it, so long themes cannot leak into the next slide. Any new sound must connect to `out`, or to `master` if it must persist across slides (as techno does).
- **CSS animations restart** through `void el.offsetWidth` before `.active` is added. Prefer `.active .x { animation: ... }` for entry effects.
- **Animated SVG flow diagrams** (for example "Research · Assign · Refine"):
  - Built with helpers such as `agent()`, `dev()`, `box()`, `codeScan()` and `ico()`.
  - `EDGES` entries are `[id, svgPath, class, labelLines, lx, ly, anchor]`.
  - `SEQ` entries are `[edgeId|null, nodeLitOnArrival, durationMs?]`.
  - `LANE` maps each node to its lane.
  - The loop length is `START + steps * STEP + HOLD`.
  - On cycle reset, `.on`, `.hot` and `.seen` are removed from the whole SVG, so keep any persistent state (such as counters) outside those classes.
- **Repeated ids:** several slides reuse ids such as `nIssue`, `nDev1` and `nDev2`, and constants such as `HOLD`. When editing, make `old_str` unique by including nearby slide-specific context.

## Content rules (enforced by tests)

- Everything is public. Use only synthetic examples and role icons, never people. Do not include private repos, `github/<repo>`-style references, 40-hex SHAs, or `who:` fields.
- Exception: the avatars of `pelikhan`, `lpcox`, `dsyme`, and `mnkiefer` only, as local files `assets/avatars/<login>.jpg` referenced from SVG `<image>` elements. No other avatars or `<image>` sources.
- The only URL allowed in `deck.js` is `https://github.github.com/gh-aw/introduction/overview/`.
- Speaker notes for slides 2, 5, and 7–16 must match `/illustrative|synthetic|simulated/i`.
- No external requests and no page errors.

## Preview loop

There is no build step. Serve the static folder and open it in the browser canvas:

```bash
python3 -m http.server 4329 --bind 127.0.0.1 --directory docs/public   # run async + detached
# open http://127.0.0.1:4329/cracktro/#9   (#N = slide N)
```

After each edit:

1. Run `node --check docs/public/cracktro/deck.js`.
2. In the canvas, run `location.reload()`, wait, and take a screenshot.
3. To jump to a slide, click `[aria-label="Slide N"]` or set the hash.
4. Dispatch key events on `window`, for example `dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowRight'}))`.
5. To catch a transition, read `getComputedStyle(el).transform` about 200 ms after navigating.

## Tests

Run from `docs/` after `npm ci`: `npx playwright test tests/talk.spec.ts`. The tests use the Astro dev server on `localhost:4321` at the base path `/gh-aw/cracktro/`. If `node_modules` is missing, say the tests were not run rather than skipping silently.

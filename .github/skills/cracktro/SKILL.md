---
name: cracktro
description: Edit, preview, and verify the standalone interactive talk deck in docs/public/cracktro (slides, SVG diagrams, transitions, WebAudio sound).
---

# Cracktro talk deck

The deck is published at `https://github.github.com/gh-aw/cracktro/`. It is a forty-five-slide deck with no dependencies, separate from the docs homepage slideshow.

## Files

- `docs/public/cracktro/index.html`: loads `deck.js` and holds the SEO/Open Graph/Twitter meta (absolute `https://github.github.com/gh-aw/cracktro/` URLs).
- `docs/public/cracktro/og-image.png`: 1200x630 social card rendered from slide 1 with the dots/count/sound/progress UI hidden. Keep it outside `assets/` (the asset test only allows avatars there).
- `docs/public/cracktro/deck.js`: about 575 KB holding everything (CSS, slides, navigation, synth, background). **Never read it whole.** Use `grep -n` on the `// ---------- <section> ----------` and `// <Slide title>` comment markers, then `view_range`.
- `docs/public/cracktro/assets/avatars/{pelikhan,lpcox,dsyme,mnkiefer,ievangelist}.jpg`: the only allowed assets, which a test enforces. Diagrams are inline SVG built in `deck.js` (for example slide 2's CI-to-agentic `GA_RUNS` pipeline).
- `docs/public/cracktro/fonts/`: Monaspace Neon plus its OFL licence. Mona Sans comes from `docs/public/fonts/`.
- `docs/tests/talk.spec.ts`: Playwright tests. They contain the list of forty-five slide headings, so update it when a heading changes.
- `docs/README.md`, "Interactive talk" section: user-facing description (keys, public-content rules).

## deck.js layout (search for these markers)

| Marker | Contents |
|---|---|
| `---------- styles ----------` | A single CSS template string. Slide-specific classes use prefixes such as `fm-`, `atk-`, `rpa-`, `mra-`, `eph-`, `fv-`, `sec-`, `ch-` and `pf-`. |
| `---------- helpers ----------` | `h(tag, attrs, ...kids)` creates DOM; `letters(text, cls)` builds per-letter headings with classes like `l-grad` and `l-out`; `resetIndex()`. |
| `---------- slides ----------` | One `{ ... slides.push({...}) }` block per slide, each preceded by a `// Title: summary` comment. |
| `const OCTI` | Octicon path map (calendar, issue, pr, merge, person, copilot, ...) used by `ico(name,x,y,size,fill)`. |
| `---------- sound:` | WebAudio synth `sfx`: `slide, step, cut, techno, pop, key, compile, review, notify, omen, route, mr, toggle, unlock`. `THEMES` holds the per-slide melodies. |
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

## Mechanics worth knowing

- **Transitions:** derived from each slide's `group` (intro, platform, security, factory, loops, cost, enterprise, scale). Siblings in a group use a reveal-style horizontal push (`slide-in`/`slide-out`); `show()` sets `--dx` to `100%` going forward, `-100%` going back, and `0%` on first load. Crossing into another group slides up (`up-in`/`up-out`) going forward and down (`down-in`/`down-out`) going back. All use the same 0.8 s curve. Every slide needs a `group`; never add per-slide transition overrides. The old slide keeps the `.leaving` class until a timeout clears it.
- **Audio isolation:** `master` is a persistent gain that feeds the compressor and delay. `out` is a per-slide bus. `show()` calls `sfx.cut()`, which fades the current bus and replaces it, so long themes cannot leak into the next slide. Any new sound must connect to `out`, or to `master` if it must persist across slides (as techno does).
- **CSS animations restart** through `void el.offsetWidth` before `.active` is added. Prefer `.active .x { animation: ... }` for entry effects.
- **Animated SVG flow diagrams** (for example "Daily Unsloppers"):
  - Built with helpers such as `agent()`, `dev()`, `box()`, `codeScan()` and `ico()`.
  - `EDGES` entries are `[id, svgPath, class, labelLines, lx, ly, anchor]`.
  - `SEQ` entries are `[edgeId|null, nodeLitOnArrival, durationMs?]`.
  - `LANE` maps each node to its lane.
  - The loop length is `START + steps * STEP + HOLD`.
  - On cycle reset, `.on`, `.hot` and `.seen` are removed from the whole SVG, so keep any persistent state (such as counters) outside those classes.
- **Repeated ids:** several slides reuse ids such as `nIssue`, `nDev1` and `nDev2`, and constants such as `HOLD`. When editing, make `old_str` unique by including nearby slide-specific context.

## Content rules (enforced by tests)

- Everything is public. Use only synthetic examples and role icons, never people. Do not include private repos, `github/<repo>`-style references, 40-hex SHAs, or `who:` fields.
- Exception: the avatars of `pelikhan`, `lpcox`, `dsyme`, `mnkiefer`, and `ievangelist` only, as local files `assets/avatars/<login>.jpg` referenced from SVG `<image>` elements. No other avatars or `<image>` sources.
- Exception: the Model routing slide's results panel shows figures measured on gh-aw's own workflows; its speaker notes say so.
- Exception: the Continuous Security slide (9, `ge-` classes) shows measured counts and dates from The Great Escapi (`firewall-escape` workflow) repo memory (426 escape techniques, 15 Feb to 22 Sep 2026; #401 was the first escape) and the AWF firewall SNI-allowlist fix; the attack diagram is illustrative and its notes say so.
- Exception: the Issues as intent slide (13) shows real counts for gh-aw issues labelled `community` and the merged PRs that closed them (November 2026 snapshot); its notes and caption say so.
- Exception: the Query · Ask · Grow slide (18, `otg-` classes) has a bottom timeline of gh-aw's measured `[otel-advisor]`/`[grafana-otel-advisor]` issues (5 Apr to 9 Jun 2026, day offsets in `DAYS`); the loop above it is illustrative and its notes say so.
- Exception: the Mine · Refine · Apply slide (19, `mra-` classes) shows real gh-aw data, 15 May to 11 Nov 2026: a dated feed of merged `[linter-miner]` analyzers (with their ordinal of 65), `[lint-monster]` issues and Copilot cleanup PRs, plus counts (73 analyzer PRs, 65 merged, 376 reviews, 235 LintMonster issues, 92 merged cleanup PRs, 54,128 lines). The loop is illustrative; notes and caption say so. Feed authors are agents only (`linter miner`, `lintmonster`, `copilot`).
- Exception: the Semantic clustering slide (21, `sd-` classes) shows the real functions, files and counts from gh-aw's Semantic Function Refactoring report of 7 May 2026 (issue 30711) and the merged PR 30770 that closed it; the scan, beams and chip animation are illustrative. Its notes and caption say so.
- Exception: the Go Fan slide (23, `gf-` classes) shows real counts of gh-aw `[go-fan]` review issues, the merged PRs that closed them, and shortened PR titles (February to November 2026); its notes and caption say so.
- Exception: the Spec-driven testing slide (24) shows an illustrative excerpt of gh-aw's `pkg/ctxutil` change, README spec and spec tests, and names gh-aw's own spec workflows; its notes say so.
- Exception: the Docs that heal slide (25, `dh-` classes) names gh-aw's own docs workflows; the workflow, page and noob tester report shown are illustrative and its notes say so.
- Exception: the Speak · Map · Grep slide (26, `dg-` classes) shows spoken forms from gh-aw's real `DICTATION.md` and names its dictation and function-namer workflows; grep counts and renames are synthetic, the maintainer is the pelikhan avatar, and its notes say so.
- Exception: the data slides after the finale. Slide 43, "First commit to GA" (`hist-` classes), shows gh-aw's measured public history (git on main plus GitHub search counts, 12 Aug 2025 to GA on 28 Oct 2026); its speaker notes give the sources and say account attribution is not proof of AI use. Slide 44, "How PRs land" (`land-` classes), shows measured time-to-merge and lines-changed distributions of merged PRs, split by the same origins; issues opened under a person's account may still have been drafted in the Copilot app or CLI, which GitHub does not record. Slide 45, "How workflows evolve" (`evo-` classes), shows measured first-parent commits on main that changed `.github/workflows/**/*.md` (monthly by author type, live workflow count, lines-changed histogram and statistical tests: Spearman, Mann–Whitney, Gini), 12 Aug 2025 to GA; its notes give the method.
- The only URL allowed in `deck.js` is `https://github.github.com/gh-aw/introduction/overview/`.
- Speaker notes for slides 2, 3, 5, 7, 8, 9, and 11–42 must match `/illustrative|synthetic|simulated/i`.
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

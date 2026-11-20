# Concept slide style

The house style of the two "concept" slides, The factory mindset (`fmm-`) and Trust the process (`tp-`). Load it to restyle a slide in this look or to add a new concept slide. Also follow `brand.md` (colour, type) and `mechanics.md` (bands, `THEME`, `MUSIC`).

A concept slide states one idea of the talk as a manifesto: a light, quiet canvas, a problem line, a green answer line and one live visual metaphor. Use it sparingly, for the talk's turning points, never for product features, data or flows.

## Ingredients

1. **Own header-less row.** Give the slide its own `group`, add it to `SOLO` (`// ---------- bands ----------`) and to `slides.md` as a normal slide section (no band header section). No `BANDS` entry.
2. **Light, gridless theme.** Add a `THEME` entry cloned from `mindset`/`trust`: `light: true`, `base: [200, 209, 218]` (`#c8d1da`), `pal` all `#000000` (no glows), `grid: [200, 209, 218, 0, 0]` (invisible), `cell: 32`, a small `ang` (-π/12, π/8) so the eased background still moves, `prog: [0]`. `light` sets `body.light`; `go()` stages the light/dark swap for you.
3. **Copy stack.** `.kicker` (runtime style: Monaspace, uppercase, muted) naming the idea, then `h2` statements, never `letters()`, `.l-grad` or `slide-head`:
   - problem line: neutral, 76–80px, `line-height` 1.04–1.1;
   - answer line(s): green, 100–112px, `line-height` ~1.03, the largest text on the slide; short imperatives ("Improve your process.", "Accelerate your factory.").
   - Both: Mona Sans `"wght" 520–560, "wdth" 100`, `letter-spacing: -.02em`, `margin: 0` with explicit `margin-top` per line. This tight tracking is the established exception for these display statements; do not add more.
4. **One live metaphor.** A single visual that animates in `tick(t)` and loops forever, built from the deck's own vocabulary (code glyphs, octicons such as `OCTI.merge`, pills, tiles); reset all state in `enter()`. Examples: the 7×7 tile grid flipping between code glyphs and green swatches (`fmm-grid`); the two-lane race of a slow and a fast pill filling while merge icons pop into a tray (`tp-vs`). Throttle work (`if (t - last < 110) return;`), use `setTimeout` only for short flip halves.
5. **Horizontal split motion.** Elements fly in from the sides and leave the way they came; the slide itself does not take the vertical band push or fade (see CSS skeleton).
6. **Own music.** One short `MUSIC` theme keyed by the `THEME`'s `music` that sonifies the metaphor (factory: metallic machinery ticks; trust: one heavy review `bell` against quick merge blips, then a resolving `pad`). Sync per-event `sfx` calls (`sfx.pop`, `sfx.step`) with visual events, sparingly (every 3rd blip).
7. **Notes.** Read the slide aloud as a short manifesto. If the visual implies numbers (durations, counts), say it is illustrative and mark the `slides.md` entry with Notes `illustrative`.

## Layouts

| Archetype | Reference | Geometry (1920×1080 stage) |
|---|---|---|
| Split | `fmm` | Copy column `left: 110px; top: 150px; width: 860px`. Visual square `left: 1050px; top: 110px; 770×770`, `border-radius: 36px`, `overflow: hidden`. |
| Centred | `tp` | Copy `left/right: 160px; top: 150px; text-align: center`. Problem line, then a comparison row (`display: flex; gap: 70px; margin: 64px 0 60px`) with a mono `VS` separator, then answer lines. |

Keep generous empty canvas: nothing above `top: 110px`, nothing in the bottom ~150px except a mascot.

## Colour

Neutral canvas and text, one green accent, a second hue only to encode a comparison.

| Role | Dark (`.slide` default) | Light (`body.light ...` overrides) |
|---|---|---|
| Problem line | `#f0f6fc` | `#1f2328` |
| Answer line | `#3fb950` | `#116329` |
| Comparison "slow/old" | `#4493f8` | `#0550ae` |
| Comparison "fast/new" | `#3fb950` | `#116329` |
| Muted / `VS` | `#8b949e` | `#424a53` |
| Visual shadow | `0 30px 120px #0008, 0 0 0 1px #ffffff14` | `0 30px 90px #1f232826, 0 0 0 1px #1f23281f` |

- Always write both: base rules for dark, then a `/* light concept bands */` block of `body.light .<prefix>-…` overrides (the theme is light, but the rules must still read on dark if the band changes).
- Comparisons pair blue with green and carry a text label in each pill ("1h review", "30sec skim"); never red/green, never colour alone.
- Tiles and fills may use one green ramp (`#aff5b4 #7ee787 #3fb950 #2ea043 #238636`) plus neutrals (`#f6f8fa #d0d7de #30363d #161b22 #0d1117`); glyphs in `#0d1117`, Monaspace 700.
- Pills: 2px `var(--c)` border, fully rounded, Monaspace 500 uppercase with `.06em` tracking, progress fill `var(--c)` at opacity `.22` (`.14` light).

## Motion

Shared keyframes already exist in the deck CSS; reuse them, do not redefine: `tp-l`/`tp-r` (in from ±1500px), `tp-lo`/`tp-ro` (out), `tp-vs`/`tp-vso` (scale pop), `tp-pop` (small item pop), `tp-bob` (idle float).

- Enter: `.9s cubic-bezier(.2,.9,.25,1) both`; stagger copy by `.12–.15s`, the visual ~`.1s`, comparison row ~`.45s`, separator `1.05s`, answer lines `.8s`/`.95s`. Alternate sides (`tp`: odd children from the left, even from the right) or split by column (`fmm`: copy left, visual right).
- Exit: `.55s cubic-bezier(.6,0,.85,.3) both !important` on `:is(.fadeout, .leaving)`, each element back to the side it came from.
- Overshoot pops for small items: `cubic-bezier(.3,1.6,.4,1)` (tiles also use `.3,1.5,.4,1` with a `calc(var(--i) * 45ms + 300ms)` diagonal stagger).

```css
.<p>-copy { position: absolute; left: 110px; top: 150px; width: 860px; }
.<p>-copy h2 { margin: 0; font-variation-settings: "wght" 560, "wdth" 100; letter-spacing: -.02em; }
.<p>-stop { margin-top: 30px !important; font-size: 80px; line-height: 1.04; color: #f0f6fc; }
.<p>-go { margin-top: 44px !important; font-size: 100px; line-height: 1.02; color: #3fb950; }
body.light .<p>-stop { color: #1f2328; } body.light .<p>-go { color: #116329; }
.slide.<p>.active .<p>-copy > * { animation: tp-l .9s cubic-bezier(.2,.9,.25,1) both; }
.slide.<p>.active .<p>-copy > :nth-child(2) { animation-delay: .12s; }
.slide.<p>.active .<p>-copy > :nth-child(3) { animation-delay: .24s; }
.slide.<p>.active .<p>-vis { animation: tp-r .9s cubic-bezier(.2,.9,.25,1) both .1s; }
/* horizontal only: no vertical band push or fade for the slide itself */
.slide.<p>.up-in, .slide.<p>.down-in, .slide.<p>.leaving { animation: none; }
.slide.<p>.fadeout { opacity: 1; }
.slide.<p>:is(.fadeout, .leaving) .<p>-copy > * { animation: tp-lo .55s cubic-bezier(.6,0,.85,.3) both !important; }
.slide.<p>:is(.fadeout, .leaving) .<p>-vis { animation: tp-ro .55s cubic-bezier(.6,0,.85,.3) both !important; }
```

```js
// <Title>: <summary>
{
  resetIndex();
  const vis = h("div", { class: "<p>-vis" });
  let last = 0;
  slides.push({
    group: "<id>", // own SOLO row + light THEME entry + MUSIC key
    notes: "<Kicker>. <Problem line> <Answer line>.",
    enter() { last = 0; /* reset visual state */ },
    tick(t) { if (t - last < 110) return; last = t; /* advance the metaphor */ },
    el: h("section", { class: "slide <p>" },
      h("div", { class: "<p>-copy" },
        h("div", { class: "kicker" }, "<Kicker>"),
        h("h2", { class: "<p>-stop" }, "<Problem line>"),
        h("h2", { class: "<p>-go" }, "<Answer line>")),
      vis),
  });
}
```

## Mascot

Optional and at most one per concept slide. Trust the process uses the only full-body asset, `assets/avatars/mona-full.png`, as an SVG `<image>` rotated 14° and clipped at the left edge (`tp-mona`), entering with `tp-l` then idling with `tp-bob`; `content-rules.md` restricts that file to that slide, so other concept slides use no mascot or a 160×160 avatar. Never on serious topics.

## Checklist

- New, unused CSS prefix; base dark rules plus `body.light` overrides.
- `SOLO`, `THEME` (light, gridless), `MUSIC` and `slides.md` section added together.
- One accent (green); a second hue only for a labelled comparison.
- Answer line is the largest text; no `letters()`, gradients or glows.
- Enter and exit are horizontal; slide-level `up-in`/`down-in`/`leaving` animations disabled.
- `tick` loops indefinitely, `enter` resets; `node --check docs/public/cracktro/deck.js`.
- Preview the `.stage` mid-loop and after the last entry (SKILL.md Preview loop).

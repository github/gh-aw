# Guide slide style

The secondary house style of the `/agentic-workflows` guide row: /agentic-workflows (`gdr-`), The compiler corrects the agent (`gdl-`) and Personas test the guide. Agent fixes it (`gdp-`). Load it to add or restyle a slide that explains **how to use** a gh-aw tool or process. Also follow `brand.md` (colour, type) and `mechanics.md` (bands, `THEME`, `MUSIC`).

A guide slide teaches one usage pattern: a light, gridless canvas, a title block (kicker, two-part heading, one plain sentence), one white board that loops the process, and a mono caption stating what is illustrative or measured. Compared with `concept-style.md`, guide slides explain a mechanism step by step, may carry measured data, and use blue instead of green.

## Ingredients

1. **Own header-less row.** All guide slides share `group: "guide"`, listed in `SOLO` (`guide: "/agentic-workflows"`); their `slides.md` rows have no band header row. Source position decides the row's place (between the platform and security bands).
2. **Light, gridless theme.** `THEME.guide` clones `mindset`/`trust` (`light: true`, `base: [200, 209, 218]`, `pal` all `#000000`, invisible `grid`, `cell: 32`) with a Primer blue `accent` and `music: "guide"`.
3. **Title block (`gd-head`).** `.kicker` naming the tool and the topic (`/agentic-workflows · authoring loop`), an `h2` whose second sentence is an `<em>` answer in blue, then one `p` (30px) explaining the mechanism with inline `code`. The `h2` text is the `slides.md` Heading.
4. **One board (`gd-board`).** A 1700×600 rounded panel at `left: 110px; top: 340px`, children absolutely positioned in board pixels. Draw wires in one inline `<svg viewBox="0 0 1700 600">` (built via `innerHTML`; `h()` makes HTML only). Shared parts: `gd-lab` (mono uppercase label), `gd-box` (+ `.on`), `gd-wire` (+ `.on`: blue, flowing dash), `gd-ok` (blue `✓`) and `gd-bad` (orange `✗`).
5. **One looping process.** `tick(t)` drives a timed phase table (`PH` or a fixed cycle), throttled to 110 ms, starting ~1 s after entry; `enter()` resets every element. Show real artefacts where they exist (guide file names, real `gh aw compile` messages, measured counts), typed or revealed one step at a time.
6. **Caption (`gd-cap`).** Mono, uppercase, at `top: 972px`, stating what is illustrative and, for data, `MEASURED · snapshot <date> · <window>`. Keep it left of the sound button.
7. **Notes.** What/Why/When cues per `../cracktro-speaker-notes/SKILL.md`; say what is illustrative and mark the `slides.md` row `illustrative`. Measured numbers also need a `content-rules.md` Exception bullet and a refresh step.

## Layouts

| Archetype | Reference | Board geometry |
|---|---|---|
| Router | `gdr` | Request bubble (left) → hub with numbered steps → stacked route rows (right) joined by curved wires; the active route lights up. |
| Ring loop | `gdl` | Three nodes on a ring (`CX 360, CY 290, R 196`) with a dot turning clockwise (`gdl-spin`) and a centre counter; dark editor + terminal panel on the right. |
| Stats | `gdp` | Input column (chips) → numbered loop steps → 2×2 KPI tiles counting up and one labelled proportion bar. |

## Colour

Neutral canvas and text, one blue accent; orange only for an error or a problem, always with a `✗`/`✓` or text label (never red/green).

| Role | Dark | Light (`body.light` overrides) |
|---|---|---|
| Heading / answer `em` | `#f0f6fc` / `#4493f8` | `#1f2328` / `#0969da` |
| Muted | `#9198a1` | `#424a53`, `#59636e` |
| Board | `#161b22` | `#ffffff`, shadow `0 30px 90px #1f232826, 0 0 0 1px #1f23281f` |
| Box / active box | `#0d1117` / blue border, tint | `#f6f8fa` / `#0969da` border, `#ddf4ff` |
| Error | `#ffa657` | `#bc4c00` |

Code panels (editor, terminal) stay dark in light mode, like a real editor.

## Motion

Reuse the concept keyframes (`tp-l`, `tp-r`, `tp-lo`, `tp-ro`): the title block enters from the left (staggered `.12s`), the board from the right (`.1s`), the caption last (`.36s`); exits mirror them. Slide-level `up-in`/`down-in`/`leaving` animations are disabled, as in the concept style. Active state changes use `.25s` colour transitions; never animate layout per frame.

## Checklist

- `group: "guide"`, explicit `id`, unused per-slide prefix (`gd<x>-`) on top of the shared `gd-` parts.
- `h2` text equals the `slides.md` Heading; Summary has no aging counts or dates.
- Blue accent only; orange only for errors, with a label.
- `tick` loops indefinitely and `enter` resets; `node --check docs/public/cracktro/deck.js`.
- Preview the `.stage` mid-loop and at the step carrying the message (SKILL.md Preview loop); nothing overflows the board.

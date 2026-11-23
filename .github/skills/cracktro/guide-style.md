# Guide slide style

The secondary house style of the `/agentic-workflows` guide examples hanging under Turn Intent to Workflows: Ask once. The agent picks the guide (`gdr-`), The compiler corrects the agent (`gdl-`) and Personas test the guide. Agent fixes it (`gdp-`). Load it to add or restyle a slide that explains **how to use** a gh-aw tool or process. Also follow `brand.md` (colour, type) and `mechanics.md` (columns, `under`).

A guide slide teaches one usage pattern: the band's canvas, a title block (kicker, two-part heading, one plain sentence), one board that loops the process, and a mono caption stating what is illustrative or measured. Compared with `concept-style.md`, guide slides explain a mechanism step by step, may carry measured data, and use blue instead of green.

## Ingredients

1. **Column examples.** Guide slides take `group: "platform"` and `under: "intent"`, hanging in a column below Turn Intent to Workflows (`key: "intent"`); kids follow source order. In `slides.md` their sections follow the head's section. A guide slide in another band hangs under that band's matching column head (same `group`).
2. **Theme.** The slide inherits its band's `THEME`; guide styles are dark-only for now; add `body.light` overrides if a guide slide moves to a light band.
3. **Title block (`gd-head`).** `.kicker` naming the tool and the topic (`/agentic-workflows · authoring loop`), an `h2` whose second sentence is an `<em>` answer in blue, then one `p` (30px) explaining the mechanism with inline `code`. The `h2` text is the `slides.md` Heading.
4. **One board (`gd-board`).** A 1700×600 rounded panel at `left: 110px; top: 340px`, children absolutely positioned in board pixels. Draw wires in one inline `<svg viewBox="0 0 1700 600">` (built via `innerHTML`; `h()` makes HTML only). Shared parts: `gd-lab` (mono uppercase label), `gd-box` (+ `.on`), `gd-wire` (+ `.on`: blue, flowing dash), `gd-ok` (blue `✓`) and `gd-bad` (orange `✗`).
5. **One looping process.** `tick(t)` drives a timed phase table (`PH` or a fixed cycle), throttled to 110 ms, starting ~1 s after entry; `enter()` resets every element. Show real artefacts where they exist (guide file names, real `gh aw compile` messages, measured counts), typed or revealed one step at a time.
6. **Caption (`gd-cap`).** Mono, uppercase, at `top: 972px`, stating what is illustrative and, for data, `MEASURED · snapshot <date> · <window>`. Keep it left of the sound button.
7. **Notes.** What/Why/When cues per `../cracktro-speaker-notes/SKILL.md`; say what is illustrative and mark the `slides.md` entry with Notes `illustrative`. Measured numbers also need a `content-rules.md` Exception bullet and a refresh step.

## Layouts

| Archetype | Reference | Board geometry |
|---|---|---|
| Router | `gdr` | Request bubble (left) → hub with numbered steps → stacked route rows (right) joined by curved wires; the active route lights up. |
| Ring loop | `gdl` | Three nodes on a ring (`CX 360, CY 290, R 196`) with a dot turning clockwise (`gdl-spin`) and a centre counter; dark editor + terminal panel on the right. |
| Stats | `gdp` | Input column (chips) → numbered loop steps → 2×2 KPI tiles counting up and one labelled proportion bar. |

## Colour

Neutral canvas and text, one blue accent; orange only for an error or a problem, always with a `✗`/`✓` or text label (never red/green).

| Role | Dark |
|---|---|
| Heading / answer `em` | `#f0f6fc` / `#4493f8` |
| Muted | `#9198a1` |
| Board | `#161b22` |
| Box / active box | `#0d1117` / blue border, tint |
| Error | `#ffa657` |

## Motion

Reuse the concept keyframes (`tp-l`, `tp-r`, `tp-lo`, `tp-ro`): the title block enters from the left (staggered `.12s`), the board from the right (`.1s`), the caption last (`.36s`); exits mirror them. Slide-level `up-in`/`down-in`/`leaving` animations are disabled, as in the concept style. Active state changes use `.25s` colour transitions; never animate layout per frame.

## Checklist

- `group` and `under` of the column head, explicit `id`, unused per-slide prefix (`gd<x>-`) on top of the shared `gd-` parts.
- `h2` text equals the `slides.md` Heading; Summary has no aging counts or dates.
- Blue accent only; orange only for errors, with a label.
- `tick` loops indefinitely and `enter` resets; `node --check docs/public/cracktro/deck.js`.
- Preview the `.stage` mid-loop and at the step carrying the message (SKILL.md Preview loop); nothing overflows the board.

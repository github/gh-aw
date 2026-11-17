# Brand: colour, titles and type

Sources: https://brand.github.com/foundations/color, https://brand.github.com/foundations/typography, Primer Brand primitives (https://primer.style/brand/primitives/color) and Primer product dark tokens (https://primer.style/foundations/color). Load this file before changing any colour, gradient, glow, title or font in the deck.

## Principle: neutral first, one accent

GitHub's palette is mostly neutral with small moments of one hero colour. A slide should read as dark neutral canvas + white/grey text + **one** accent hue. The Wider Spectrum (purple, pink, coral, lime, ...) is for expressive illustration, not for headings or chrome.

- One accent hue per slide (normally the band accent). A second hue only when it encodes meaning (state, comparison); see Semantic hues.
- Never more than three hues on one slide, neutrals excluded.
- No rainbow: no multi-hue gradient (purple→blue→green, pink→purple→blue, ...) on text, borders, progress bars or backgrounds.
- Glows (`box-shadow`, `drop-shadow`, `text-shadow`) are subtle, in the element's own hue, alpha ≤ `55`; never stack glows of different hues.

## Titles

- Default heading: solid `#f0f6fc`, Mona Sans, `"wght" 700, "wdth" 100`.
- Emphasis: colour at most one word or short phrase in the slide's accent; the rest stays `#f0f6fc`.
- Gradient text (`.l-grad`) is single-hue only: shades of one ramp, optionally fading to `#f0f6fc` (for example `#0fbf3e → #5fed83 → #bfffd1`, the title slide). Band headers already do this (`--bh`, `--bh2`, white); per-slide `.l-grad` overrides must follow the same rule.
- Kickers: Monaspace, uppercase, single line, muted `#9198a1` (dark) / `#424a53` (light).

## Palette

Dark product tokens (Primer dark; the deck's UI vocabulary):

| Role | Hex |
|---|---|
| Canvas | `#0d1117` |
| Inset / card | `#151b23`, `#161b22` |
| Border | `#3d444d`, `#30363d` |
| Text | `#f0f6fc` (headings), `#e6edf3` (body), `#c9d1d9` (secondary) |
| Muted | `#9198a1`, `#8b949e` |
| Accent (blue) | `#4493f8`, light `#79c0ff` |
| Success (green) | `#3fb950`, light `#7ee787` |
| Attention (yellow) | `#d29922`, light `#e3b341` |
| Severe (orange) | `#db6d28`, light `#ffa657` |
| Danger (red) | `#f85149`, light `#ff7b72` |
| Done / AI (purple) | `#ab7df8`, light `#d2a8ff` |

Brand hero ramp (marketing moments: title, band headers, finale): Green 1–6 `#bfffd1 #8cf2a6 #5fed83 #0fbf3e #08872b #0a241b`; brand grays `#f2f5f3 #e4ebe6 #b6bfb8 #909692 #232925 #101411`. For other ramps use the Primer Brand scales (`blue.0–9`, `purple.0–9`, ...), staying within one scale.

## Semantic hues

Keep meanings stable across the deck so colour carries information, not decoration:

- Green: GitHub / brand / success / merged.
- Blue: neutral accent, links, information, "allowed" in allowed/blocked pairs.
- Orange: warning, blocked, cost of failure (pair with blue; never red/green alone, see the SKILL.md accessibility rule).
- Red: danger and the security band only.
- Purple: AI / agent / Copilot moments; not a general decoration colour.
- Yellow/gold: attention and the cost band; the research-prototype badge.

## Typography

- Mona Sans for headings and body; Monaspace Neon for code, kickers, labels and data.
- Width stays `"wdth" 100`; other widths are reserved for events such as Universe.
- No ligatures in headlines and body copy; no extra `letter-spacing` on Mona Sans (its optical sizing handles tracking). Tracking is allowed only on single-line mono uppercase labels.
- No multiline uppercase mono.

## Regularization pass

When asked to calm or re-brand the deck:

1. `grep -n 'linear-gradient\|l-grad' docs/public/cracktro/deck.js` and replace each multi-hue gradient with a single-hue ramp of the slide's accent (or solid `#f0f6fc`).
2. Grep for heavy glows (`drop-shadow`, `box-shadow: 0 0`, `text-shadow`) and drop or tone them to the element's hue.
3. Per slide, count accent hues; remap decorative extra hues to neutrals (`#9198a1`, `#3d444d`) or the slide accent, keeping semantic pairs.
4. Leave runtime chrome (`runtime.js`: progress bar, dots) neutral or single-hue too.
5. Preview each changed slide (SKILL.md Preview loop) and check contrast: body ≥ 7:1, muted ≥ 4.5:1 on the slide background.

## Title gradient hook

`runtime.js` renders `.l-grad` as solid `#f0f6fc` by default. For a single-hue shimmer, set `--lg` to the slide accent on the slide or heading (e.g. `.atk-title { --lg: #79c0ff; }`); do not write new multi-stop `.l-grad` gradients.

## Generated brand assets

For social cards, wallpapers, charts or a PowerPoint companion, use the `asset-generator` MCP; see `asset-generator.md`. Never embed its output in slides.

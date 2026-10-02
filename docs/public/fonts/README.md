# Fonts

This directory contains webfont files served by the docs site.

## Mona Sans

Source: https://github.com/github/mona-sans (release v2.0.27)

Files used by the docs (variable fonts covering the full design space):

- `MonaSansVF.woff2` (upright; weight 200–900, width 75–125%, optical size)
- `MonaSansVF-Italic.woff2` (italic; same axes)

License text (required for redistribution under OFL-1.1):

- `MonaSans-LICENSE.txt`

The docs CSS registers these via `@font-face` in `src/styles/tokens.css` and uses `"Mona Sans"` as the first choice in `--aw-font-sans`.

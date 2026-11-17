# Asset generator (GitHub brand assets)

The `asset-generator` MCP server renders on-brand GitHub images and PowerPoint decks from official templates. Use it for material *around* the talk (social cards, wallpapers, charts, a PowerPoint companion), not inside the cracktro slides.

## Setup

The server is internal to GitHub (`github/asset-generator`, needs repo access). Configure it per user, never in this repo's `.github/mcp.json` or `.vscode/mcp.json`: contributors without access would get a broken server.

1. Install (Node.js 24+, `gh auth login` first):

   ```bash
   gh release download --repo github/asset-generator --pattern 'asset-generator-mcp.tgz' --clobber
   npm install --global ./asset-generator-mcp.tgz && asset-generator-mcp --version && rm ./asset-generator-mcp.tgz
   ```

   Upgrade by repeating both commands, then restart the MCP client. If npm downloads fail on a managed machine, use the `npm` skill or follow the package README.
2. Register the server, launching the installed binary directly (not through `npx`):
   - Copilot CLI, `~/.copilot/mcp-config.json`: `"asset-generator": {"type": "local", "command": "asset-generator-mcp", "tools": ["*"]}` under `mcpServers`.
   - VS Code, user `mcp.json`: `"asset-generator": {"type": "stdio", "command": "asset-generator-mcp"}` under `servers`.
   - GitHub Copilot app: add a Local (stdio) server, command `asset-generator-mcp`, no arguments.
3. Auth uses `gh auth token` automatically; `ASSET_GENERATOR_TOKEN` overrides it. On 401/403, re-run `gh auth login` with an account that has access; if no tools appear, the server is unreachable: report it, do not work around it.
4. Check: the tool list shows `asset-generator-*` tools and `list_templates` returns templates.

If the tools are missing in a session, say so and point the user to this section; never fall back to hand-drawn brand art.

## Where outputs may go

- Slides stay dependency-free: inline SVG, CSS and canvas in `deck.js`. Never embed generated images in slides.
- Never write into `docs/public/cracktro/assets/`; `docs/tests/talk.spec.ts` allows only the avatars there.
- `docs/public/cracktro/og-image.png` must stay 1200x630 and outside `assets/`. It is currently a title-slide screenshot; ask before replacing it with a generated card.
- Everything else (promo cards, banners, wallpapers, `.pptx`) goes to the session `files/` folder or the user, not the repo, unless the user asks.

## Workflow

1. Search the deferred tools for `asset-generator` to load schemas; never guess arguments.
2. Call `list_templates`, then `list_themes` for the chosen template, before creating anything.
3. Pick the pillar: `copilot` for agentic/AI content (most of this talk), `security` for the security band, `general` otherwise. Prefer dark variants to match the deck (`#0d1117` canvas).
4. Use short copy from the talk's public text (`slides.md` Summaries); never invent dates, venues, numbers or event facts. Ask for missing facts.
5. Pass images as local file paths. Do not upscale blurry screenshots; capture a fresh 1920x1080 `.stage` screenshot from the Preview loop instead.
6. Show the result to the user (publish it as an image attachment) before committing anything.

## Choosing a tool

| Need | Tool | Notes |
|---|---|---|
| Open Graph / link card | `create_wallpaper` (`format: wide`, 1200x630) or `create_share_card` | Matches `og-image.png` size; `logo: none` or `artwork`. |
| Wallpaper / background art | `create_wallpaper` | Formats: desktop, square, wide, portrait, email 660x284. |
| Hero landscape | `create_landscape` | 2400x1260; layout `offset` or `stacked`. |
| Social posts | `create_social_banner` (2560x1440), `create_social_square` (1080x1080/1350), `create_social_portrait` (1080x1920) | One message per card. |
| Speaker promo | `create_speaker_card`, `create_speaker_card_big_talk`, `create_speaker_quote` | See People rule below. |
| Data charts | `create_chart_bar`, `create_chart_line` | 1-3 datasets; dark variant; theme `copilot` or `general`. Only measured, public data (`content-rules.md`). |
| Product shots | `create_dither_patterns` then `create_full_screen_product_shot` | Layered; `create_product_landscape` is a flattened last resort. |
| PowerPoint companion | `create_slide_deck` | See below. |

## PowerPoint companion (`create_slide_deck`)

- Operations: `templates` (pick an official template) → `preview` → `get` / `inspect` → `patch` (edit slides) → `regenerate` → `finalize`.
- Slide 1 must use the cover layout.
- Mirror band order and slide headings from `slides.md`; put Summaries in notes. Do not copy SVG diagrams; describe them or attach `.stage` screenshots.
- Return the `presentationUrl` to the user; do not commit the `.pptx`.

## People, mascots and brand

- No real people: speaker cards default to GitHub avatars; use placeholders or ask the user for the speaker's consent and image.
- Mascots follow SKILL.md People rules; never generate or redraw them.
- Generated art already follows brand colour; if you sample it for the deck, map colours to the `brand.md` palette instead of adding new hues.

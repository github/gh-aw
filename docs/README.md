# Startlight Docs

## Project Structure

Inside of your Astro + Starlight project, you'll see the following folders and files:

```text
.
├── public/
├── src/
│   ├── assets/
│   ├── content/
│   │   └── docs/
│   └── content.config.ts
├── astro.config.mjs
├── package.json
└── tsconfig.json
```

Starlight looks for `.md` or `.mdx` files in the `src/content/docs/` directory. Each file is exposed as a route based on its file name.

Images can be added to `src/assets/` and embedded in Markdown with a relative link.

Static assets, like favicons, can be placed in the `public/` directory.

## 🧞 Commands

All commands are run from the root of the project, from a terminal:

| Command                   | Action                                           |
| :------------------------ | :----------------------------------------------- |
| `npm install`             | Installs dependencies                            |
| `npm run dev`             | Starts local dev server at `localhost:4321`      |
| `npm run build`           | Build your production site to `./dist/`          |
| `npm run preview`         | Preview your build locally, before deploying     |
| `npm run astro ...`       | Run CLI commands like `astro add`, `astro check` |
| `npm run astro -- --help` | Get help using the Astro CLI                     |

## Homepage slideshow

On desktop, select the presentation icon in the homepage header to present the
landing page as a nine-slide deck. Slides fit the window and retain their
interactive examples. Use the previous/next buttons, arrow keys or Page Up/Page
Down to navigate, Home/End to jump to the first/last slide, and Escape or the
close button to return to the page. Keyboard controls inside demos keep their
normal behavior. The presentation icon is hidden below the desktop menu breakpoint
(50rem), and resizing to mobile exits the slideshow.
Supported browsers animate slide changes with directional CSS View Transitions;
reduced-motion preferences disable these animations.
The floating toolbar is semi-transparent until hovered or focused with the keyboard.
When a demo tab is focused, Left/Right select tabs; Up/Down and Page Up/Page Down
still navigate slides, and Tab/Shift+Tab leave the tablist.

The pencil button opens drawing tools inspired by
[Microsoft Streamer](https://github.com/microsoft/streamer): colored rectangles
(hold Shift for squares), lines, and emoji stamps. Click to stamp an emoji or
drag to resize and rotate it. Select the pointer tool to interact with demos.
Undo (also Ctrl/Cmd+Z) and clear apply to the current slide. Drawings stay with
their slide while presenting and are cleared when exiting. Escape first closes
an open drawing palette, then exits the presentation.

Click a code snippet, prompt, or example output to expand it to a full-window
view with larger type. Enter/Space also expand a focused snippet. The view
zooms and fades smoothly (unless reduced motion is enabled); Escape or its
close button returns focus to the snippet without changing slides.

## ⚠️ Known Dev-Mode Limitations

### Sitemap behavior in dev and production

The robots file references `/gh-aw/sitemap.xml`, which is the stable sitemap entrypoint for the docs site.

During a production build (`npm run build`), Astro generates `/gh-aw/sitemap-index.xml`, and the static `/gh-aw/sitemap.xml` entrypoint points crawlers at that generated sitemap index. The generated sitemap index is not available when running the local development server (`npm run dev`).

If a CI pipeline or automated tool checks the generated sitemap index URL during a local preview, it will receive a 404 response. To verify the production sitemap flow, run `npm run build` followed by `npm run preview`.

### Robots/AI discovery paths on GitHub Pages project sites

This docs site is deployed as a GitHub Pages **project site** under `/gh-aw/` (see `base: '/gh-aw/'` in `astro.config.mjs`).

- `robots.txt` is served at `/gh-aw/robots.txt`
- AI discovery file is served at `/gh-aw/.well-known/ai.txt`
- AI metadata files are served under `/gh-aw/ai/`

Root-level endpoints on `https://github.github.com/` (for example `/robots.txt`) are controlled by the main `github.github.com` site, not this repository.

## Want to learn more?

Check out [Starlight’s docs](https://starlight.astro.build/), read [the Astro documentation](https://docs.astro.build), or jump into the [Astro Discord server](https://astro.build/chat).

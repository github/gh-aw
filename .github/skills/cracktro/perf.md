# Cracktro slide performance guard

Load when a change touches `runtime.js` (frame loop, background, transitions), adds slide animations, timers, `tick`, `requestAnimationFrame` loops or DOM built at runtime, or when the deck feels slow. Goal: no slide leaves work running off screen, grows without bound, or saturates the main thread.

## Run

```bash
npm --prefix docs run perf:cracktro                      # whole deck, ~9 min, exit 0 pass / 1 budget exceeded / 2 tool error
npm --prefix docs run perf:cracktro -- --slides security,dreaming --revisits 1   # changed slides only (ids from slides.md)
npm --prefix docs run test:cracktro-perf                 # guard regression test on a runaway fixture deck
```

- Dependency-free Node (`docs/scripts/cracktro-perf.mjs`): serves `docs/public`, drives the real deck in headless Chrome over the DevTools pipe at 1920x1080, DPR 1.
- Browser: needs Chrome or Chromium; `--chrome PATH` or `CHROME_PATH`, else `/usr/bin/chromium-browser`, `/usr/bin/chromium`, `google-chrome`, macOS Chrome/Chromium. Extra launch flags: repeat `--chrome-arg=...` or set `CRACKTRO_PERF_CHROME_ARGS`.
- No browser, or launch blocked (sandbox denial, crash, exit 2): stop and report the guard as not run with the error line. Never install another browser, weaken its sandbox, or retry by another route.
- Progress: one line per slide (`ok|FAIL i/N #id ...`), one per phase and per completed revisit, and a `… <elapsed>s <phase>` heartbeat every `--heartbeat` ms (5000; 0 disables) naming slide `i/N` and revisit `r/R`. No line for well over 5 s means a hang. Each Chrome command fails after `--cdp-timeout` ms (60000); errors name the phase and elapsed time.
- Numeric flags must be finite and >= 0; `--window` and `--cdp-timeout` > 0, `--revisits` an integer. Invalid values exit 2 before the server or browser starts.
- `--json FILE` writes every per-slide row, tour snapshots and violations; keep reports under `$TMPDIR` or the session folder, never commit them.
- Run it before a fix to capture findings and after the fix to verify; quote both in the PR.

## What it measures

For every slide: navigate with the deck's own `go()`, wait `--settle` (1300 ms, covers transitions and staged light/dark swaps), then sample three times `--window` (800 ms) apart. Then tour the deck `--revisits` (2) more times and compare state back on the first slide after a forced GC.

| Budget flag (default) | Measures | Typical cause |
|---|---|---|
| `--max-busy` (0.75) | Main-thread busy fraction: CDP `Performance.getMetrics` TaskDuration ÷ wall time; includes script, style, layout, paint and, in headless software rendering, canvas raster (a deliberate full-stage canvas animation measures about 0.5, saturation about 1) | Heavy `tick`, full-screen canvas redraws every frame |
| `--min-fps` (30) | Frames delivered per second; max frame gap in the report | Long tasks, jank |
| `--max-offscreen-animations` (0) | Running CSS/Web animations inside slides that are not shown, attributed to their slide | `el.animate()` never cancelled; CSS outside `.slide` |
| `--max-foreign-callbacks` (1/s) | Timer and rAF callbacks per second still firing for a slide that is not shown | `setInterval`/rAF loops started in `enter` or `tick` and never stopped |
| `--max-dwell-growth` (60) | Elements added while idling on one slide, strictly growing across both windows | `tick` appending nodes |
| `--max-tour-growth-elements` (100), `-listeners` (50), `-intervals` (0), `-heap-mb` (4) | Smallest per-tour increase after GC across revisits | Leaks on re-entry |
| `page-errors` (0) | Uncaught page exceptions | Broken slide code |

Attribution: an in-page probe wraps timers, `requestAnimationFrame` and each slide's `enter`/`tick`/`onNext`/`onPrev`/`onKey`, so work is charged to the slide that started it; the runtime's own frame loop is exempt.

Thresholds are runaway bounds, not tuning targets. Change a default only with measured evidence in the same PR; never raise one to pass a regression. Use flags only to make a slow or emulated environment run reliably (e.g. `--settle`), and say so.

## Fix patterns

- Hidden slides stay in the DOM with `visibility: hidden`; the runtime pauses every CSS animation (and `::before`/`::after`) outside `.active`/`.leaving`, so infinite CSS loops need no cleanup. Scope entry effects to `.active` (mechanics.md).
- Web Animations (`el.animate`), `setInterval`, `setTimeout` chains and rAF loops are not paused: drive them from `tick` (runs only for the current slide), or stop them when `enter` runs again and keep them bounded.
- The background canvas renders base fill and glows at quarter resolution and repaints the grid only when its size, angle or colour changes; keep any new full-screen drawing cached or low-resolution.
- After a fix: `node --check` edited deck files, rerun the guard on the affected slides, then the whole deck, and capture `.stage` screenshots of changed slides (SKILL.md Preview loop).

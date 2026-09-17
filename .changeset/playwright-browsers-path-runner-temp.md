---
"gh-aw": patch
---

Fix `PLAYWRIGHT_BROWSERS_PATH` so browser installation and browser launch agree on the same directory. The value is now emitted as `${{ runner.temp }}/gh-aw/playwright-browsers` instead of the shell form `${RUNNER_TEMP}/...`, which GitHub Actions never expands in `env:` maps and which caused browsers to be downloaded into a directory literally named `${RUNNER_TEMP}`.

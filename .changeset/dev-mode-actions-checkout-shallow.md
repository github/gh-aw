---
"gh-aw": patch
---

Use a shallow clone (`fetch-depth: 1`) when checking out the `actions/` folder from `github/gh-aw` in dev and script action modes, reducing the amount of data downloaded during workflow runs.

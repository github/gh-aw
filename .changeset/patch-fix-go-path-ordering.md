---
"gh-aw": patch
---

Ensure the npm PATH helper re‑prepends `$GOROOT/bin` after enumerating hostedtoolcache bins so the Go version set by actions/setup-go stays first, and add regression coverage that verifies the ordering and that the command chain still runs when GOROOT is empty.

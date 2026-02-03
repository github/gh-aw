---
"gh-aw": patch
---

Simplify AWF chroot mode setup so the host PATH is inherited (no more manual PATH prep for engine commands) and only Go's GOROOT is captured; tests now verify `--env-all` is required to deliver the environment.

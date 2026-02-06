---
"gh-aw": patch
---

Log exit codes and stderr when `gh workflow list` and `gh workflow disable` fail, and trust every directory inside the Docker image to avoid dubious ownership errors with mounted volumes.

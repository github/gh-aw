---
"gh-aw": minor
---

Add `gh aw private-clone` to configure a disposable standalone private remediation repository (an independent source snapshot with no upstream history) with operator-supplied workflows and offline code scanning/advisory context. After publishing, the command sets operator-supplied `--secret` values, enables Actions, and dispatches a `workflow_dispatch` starter workflow to begin the investigation; `--no-start` leaves Actions disabled for review.

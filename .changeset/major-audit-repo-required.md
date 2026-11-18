---
"gh-aw": major
---

Require `--repo owner/repo` when `gh aw audit` receives bare numeric run IDs.

**⚠️ Breaking change:** Bare numeric IDs now fail without a repository selector,
including IDs read with `--stdin`. Full run, job, and step URLs continue to
provide their own repository context.

**Migration guide:**
- Add `--repo owner/repo` to positional and stdin invocations that use numeric IDs.
- No change is needed for full run, job, or step URLs.

The explicit repository avoids ambiguous or implicit repository resolution.

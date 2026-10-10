---
name: model-routing-golden
description: Maintain model-routing audit golden fixtures and expected outputs.
---

# Model-Routing Golden Fixtures

Use this skill when a model-routing golden test fails, analysis output changes
intentionally, or a new routed workflow run needs coverage.

## Verify

Run `make verify-model-routing-golden` (or
`python3 scripts/model-routing-golden.py --verify`) before changing fixtures.
Treat a mismatch as a behavior change to investigate, not a reason to regenerate
immediately.

## Refresh expected outputs

When analysis behavior intentionally changes, run
`make update-model-routing-golden`. Review the complete diff for each
`expected*.json` file and confirm that routing status, classifier attribution,
AWF credits, token totals, warnings, and subagent costs match the intended
behavior. Keep separate legacy goldens when missing artifacts cause documented
information loss. Do not regenerate outputs merely to silence an unexplained
failure.

## Add or replace a fixture

Capture a run with:

```bash
python3 scripts/model-routing-golden.py --run <id-or-url> --case <case-name>
```

Use `--replace` only when intentionally updating an existing source run. The
tool downloads through the local `gh aw logs` artifact path, minimizes and
redacts its files, and refuses to write if analysis on the fixture differs from
analysis on the full download. Use `--source-dir` to exercise capture against
the synthetic downloaded-run fixture without GitHub access.

Add the case and its source scenario to `modelRoutingGoldenCases` and the
fixture README. Then run `make update-model-routing-golden`, inspect all
generated files, and run `make verify-model-routing-golden`. Retain the
established file layout and `logs/` ignore exception so raw proxy-log fixtures
remain tracked.

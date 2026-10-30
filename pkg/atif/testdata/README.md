# ATIF compatibility fixtures

The `.atif` golden files are generated from the adjacent unified-session JSONL
with `atif.Convert` and `atif.Encode`. They cover ordinary dialogue/tool
correlation/accounting and partial/subagent/unknown-event evidence.

Tool correlation is scoped by source component, phase, path, and session ID.
Terminal accounting keeps the latest supplied value for each metric within one
such scope, without summing unrelated sessions. Overflowed required cache
contributions make cache-inclusive prompt totals unavailable. Copilot parent-call
aliases identify child evidence; ordinary Pi tool relationships do not.

Validated with Harbor's unmodified `TrajectoryValidator` and Pydantic models at
revision `7d9a0b688898a11cd03cf14a21d51a9ab45d3b94`:

```bash
python -m harbor.utils.trajectory_validator pkg/atif/testdata/session.atif
python -m harbor.utils.trajectory_validator pkg/atif/testdata/partial.atif
```

With that Harbor environment available, validate newly generated output rather
than only the checked-in goldens:

```bash
ATIF_VALIDATION_DIR=/tmp/atif-check go test ./pkg/atif -run TestCompatibilityFixtures -count=1
python -m harbor.utils.trajectory_validator /tmp/atif-check/session.atif
python -m harbor.utils.trajectory_validator /tmp/atif-check/partial.atif
```

Create the destination directory before running the capture command. Harbor is
a development compatibility check, not a dependency of the Go converter.

# Go-native longitudinal value reports

Author the measurement definition and report recipe consumed by `gh aw value`.
The command runs named Go evaluators, never JavaScript or an LLM.
For typed non-Git inputs or window policies, read
[evidence-and-windows.md](evidence-and-windows.md). Reuse a supported query only
after the target runtime exposes the required evidence scope and contract.
Do not turn snapshot observations into synthetic session events.

## Any workflow repository

Work in the workflow owner's full checkout with installed `gh aw`. Keep the
definition, recipe, and semantic fixtures in that repository. No workflow
registration, copy into github/gh-aw, or local Go development environment is
required. Resolve all measurement paths relative to that checkout's Git root.
The schema slides below are an example, not a required workflow or repository.

## Measurement definition

Inspect workflow intent, eligible subjects, constraints, and output semantics.
If independent goal evidence is missing, stay unavailable; never substitute activity.

Save `.github/aw/value/WORKFLOW.json`, auto-detected by the command, or pass
`--value-function PATH`. Supported native evaluators are `json-number-v1` and
`schema-example-coverage-v1`; typed bundles use `evidence-number-v1`.
This example is the executable schema-feature
definition, not a universal metric:

```json
{
  "version": 1,
  "id": "schema-textual-coverage-v1",
  "name": "Schema fields with an example",
  "unit": "fields",
  "direction": "higher_is_better",
  "evaluator": "schema-example-coverage-v1",
  "config": {
    "schema": "pkg/parser/schemas/main_workflow_schema.json",
    "examples": [".github/workflows", "schema-demos"]
  }
}
```

Paths are literal repository-relative paths, not globs; examples are 1–32
directories. The evaluator reads top-level schema `.properties` keys and
recursive Markdown files. A column-zero `<field>:` line counts, including
fenced code blocks; indentation does not count. This proves textual presence,
not compilation, correctness, fewer defects, or saved time.

Observations preserve sorted field identities and missing/covered counts.
An observed Markdown inventory with no matches is zero; an empty schema,
absent inventory, malformed input, missing Git object, or invalid configuration
fails explicitly, never as a successful zero. Merge change counts previously
missing fields covered at merge only when the exact schema population is
unchanged. Weekly stable-cohort diagnostics separate churn from improvement.

Definitions and recipes are strict JSON objects, at most 512 KiB. Unknown
versions, fields, evaluator IDs, and legacy `script` declarations fail. No
implicit JavaScript fallback or newly invented query syntax is permitted.
For another objective with independently measured numeric evidence already
committed in history, select `json-number-v1` with `config: {"path":
"metrics/outcome.json"}`, the objective's native unit, and either direction.
The evidence file is a strict object: `{"value":12}` or `{"value":null}`.
Optionally include `"population":["subject-a","subject-b"]`; subjects must be
unique nonempty strings. Population presence or membership changes make merge
improvement unavailable. Higher-is-better improvement is after minus before;
lower-is-better reverses the sign. Negative improvement remains negative.
Missing or malformed files fail; explicit null is unavailable, never zero.
This evaluator reads evidence, not code. Establish its independent source,
measurement semantics, and provenance; do not commit an agent's self-score.
An objective not observable through these inputs still needs a reviewed native
evaluator/projection. Use lifecycle-only reporting until then.

## Schema slides recipe

Keep the definition separate from
`.github/aw/value/schema-feature-coverage.slides.json`. The recipe fixes:

- the cohort/snapshot range, 22 March–17 November 2026 at 12:00 UTC;
- the original `app/github-actions` search author and verified
  `github-actions[bot]` login;
- same-day early merge batches through 30 June;
- individual witnesses 63949, 72028, and 74061.

Weekly sampling is every seven days plus the final cutoff. All merges remain
visible in `windows`; `inspected_windows` selects the six slide rows. Batch
samples use the first merge's seven pre samples and the last merge's seven post
samples; the before observation is the first parent's, and end is seven days
after the last merge. Closed/retired field sets are deduplicated across members.
Batches remain descriptive and have no attributed value. Witnesses use the
same shared native adoption/isolation/survival gates as every other report.
Missing witnesses fail rather than silently dropping a row.

```bash
printf '%s\n' 36389625388 | gh aw logs --stdin --repo github/gh-aw --adoption --output .github/aw/logs
gh aw value schema-feature-coverage \
  --value-function .github/aw/value/schema-feature-coverage.json \
  --recipe .github/aw/value/schema-feature-coverage.slides.json --json
```

Use a full checkout with a local `origin/main` covering the cutoff. The report
does not fetch or dispatch. `--ref` may select a retained full commit SHA.
Do not combine a recipe with `--start`/`--end`; edit a separately versioned
recipe for another period.

The 17 November slide baseline is 36 weekly observations (32/45 → 75/75),
39 stable fields (30 → 39 covered), 44 producer-linked PRs, 19 merges, and six
inspected rows with 84 daily samples. Early batches: May 4 (2 PRs, 2 gaps),
May 18 (8 PRs, 7 gaps, one retired field), June 15 (4 PRs, 4 gaps). Witnesses:
September 28 (5 gaps), October 26 (1), November 2 (1).
Only the September witness had complete native human-adoption proof, so five
fields qualified. PR search totals are not complete native delivery totals.
Verify evidence; never embed these baseline numbers as computed results.

## Reproducibility and attribution

The report records resolved Git SHA, evaluator ID/version, exact definition and
recipe digests, PR/merge identities, native records, snapshots, and collection
time. Built-in defaults identify the versioned evaluator but have no external
definition digest. Save JSON reports and input lifecycle evidence. Live API
observations can change; dates alone cannot reproduce a frozen historical
report. Offline API-evidence replay is not currently implemented.

The shared pipeline owns native delivery/adoption, exact-parent comparison,
neighbor isolation, and complete recipe-defined file survival. Require matching
explicit-human acceptance and untouched changed files through the full window;
bot resolution, fallback delivery, or a survival snapshot cannot substitute.
Other output types retain lifecycle evidence without fabricated value.

Test through the same Go evaluator with exact inputs and repeat measurements.
In an external workflow repository, use its semantic fixtures and the installed
CLI on full historical inputs. Only contributors changing gh-aw itself run
`go test ./pkg/cli -run '^TestOperationalValue' -count=1` in the gh-aw source
checkout with configured caches.
Compare every baseline layer, not just endpoint counts. A shallow checkout or
missing native evidence blocks historical verification; state that limitation.
Freeze definition, recipe, fixtures, and provenance together. Corrections are
prospective; never rewrite sessions, past reports, or grader artifacts.

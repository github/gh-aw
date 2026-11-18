# Run-scoped operational-value graders

This reference preserves the existing `graders.operational-value.script`
contract for checkouts supporting custom JavaScript session graders. It is not
the default for `gh aw value`, a Go server evaluator, or longitudinal replay.
Prefer existing native graders or supported queries when available; inspect
the target implementation before authoring a declaration.

## Reserved grader contract

Use the reserved `graders.operational-value.script` field:

```yaml
graders:
  operational-value:
    name: Recorded comment application
    description: Whether this run recorded a successful comment on the configured issue
    unit: boolean
    direction: higher_is_better
    threshold: 1
    config:
      repository: OWNER/REPO
      issueNumber: 123
    script: |
      const unavailable = [
        {id: "comment-application-recorded", value: null},
        {id: "recorded-comment-count", value: null},
      ];
      if (typeof config.repository !== "string" || !config.repository.trim() ||
          !Number.isInteger(config.issueNumber) || config.issueNumber < 1) return unavailable;
      const { eventsOfType } = helpers.createSessionLookups(session);
      const comments = eventsOfType("safe_output.result")
        .filter(({ data }) => data?.type === "add_comment");
      if (!comments.length || comments.some(({ data }) =>
          data?.status !== "success" ||
          typeof data.repo !== "string" || !data.repo.trim() ||
          !Number.isInteger(data.number) || data.number < 1 ||
          typeof data.url !== "string" || !data.url)) return unavailable;
      return [
        {id: "comment-application-recorded", value:
          comments.every(({ data }) =>
            data.repo === config.repository && data.number === config.issueNumber) ? 1 : 0},
        {id: "recorded-comment-count", value: comments.length},
      ];
```

The synchronous body receives immutable `session`, `config`, and `helpers`.
Reuse `helpers.createSessionLookups(session)` for statistics and lookups. The
script runs in the downstream `graders` job, never conclusion.

Return a nonempty ordered array of unique `{id,value}` objects. IDs must be
nonempty strings; values must be finite numbers or `null`; extra fields are
rejected. The first metric supplies top-level `operational-value.value`.
Remaining metrics become diagnostics. The full ordered `metrics` array remains
in schema-v2 grader artifacts and events. Keep IDs and order stable.

Scripts are limited to 256 KiB. No file, module, process, network, environment,
credential, clock, random, timer, or input-mutation access exists. Record evidence
before publication; never return a Promise or the string `"unavailable"`.

Other custom grader IDs still use `graders.<distinct-id>.script` and return one
finite number, `null`, or an object with `value`; they do not use metric arrays.

## Design procedure

Before implementation, write a compact table with:

- ultimate workflow goal and applicable run subject;
- outcome ladder from durable effect back to execution activity;
- strongest observable, attributable, independently verifiable run-time effect;
- why stronger downstream effects are not knowable at the grading boundary;
- primary metric, native unit, formula, direction, and threshold;
- success, zero, `null`, noop, malformed-evidence, and inapplicable rules;
- exact required unified-session observations;
- contract adoption commit.

Use the furthest downstream recorded effect that meets all three tests:
observable now, attributable to this run or exact subject, and independently
verifiable. Do not reward activity, output existence, token use, or an agent's
claim of success. A request is not an applied mutation. A handler success is not
human adoption. Silence does not prove correct restraint.

Return `0` for observed non-attainment. Return `null` when applicability or
required evidence is absent, malformed, truncated, conflicting, or incomplete.
Correct restraint may score success only when recorded evidence proves an
eligible opportunity existed and no action was the right result.

For batches, derive one complete eligible denominator from one recorded snapshot.
Apply intentional sampling before fixing the denominator. Do not shrink it for
execution caps or missing work. Bind user-driven runs to the exact repository,
issue, pull request, ref, URL, mode, or other declared target.

Keep one primary metric. Add diagnostics only when they explain distinct,
actionable failure modes. Preserve native scale; do not normalize a count,
duration, amount, or signed quantity merely to fit `[0,1]`.

Freeze intent, script, configuration, and semantic fixtures together. Corrections
are prospective; never rewrite prior results. The workflow revision and script
SHA-256 digest identify the adopted implementation.

## Fixtures and verification

Fixtures use `{name,session,config,expected}` and require `attained`, `missed`,
`unavailable`, and `malformed`; add noop/inapplicable cases when relevant.
`expected` is the exact ordered array. Test exact inline bytes in a temporary
`.js` file; never maintain a second production source:

```bash
bash .github/skills/operational-value-designer/scripts/verify-operational-value-evaluator.sh \
  "$TMPDIR/operational-value.js" fixtures.json
gh aw lint --rules grader-file-io --javascript-module \
  /path/to/gh-aw/pkg/workflow/taint/javascript .github/workflows/WORKFLOW.md
gh aw compile .github/workflows/WORKFLOW.md
cat aw_session.jsonl | gh aw graders run WORKFLOW operational-value
```

The verifier runs the restricted runtime twice and checks identity, digest, and
order. A missing native parser warning means unchecked lint coverage.

`graders.operational-value.run` is removed. Invalid declarations must fail with
migration guidance to `script`; never ignore or translate shell automatically.

## Generated job check

Inspect the DAG: `graders` depends on successful conclusion, downloads
`usage/aw_session.jsonl`, has no write/inference/App/OIDC credentials, and uploads
the separate `graders` artifact.

## Removed Bash migration

Do not wrap shell file/API behavior in JavaScript. Record required evidence in
the session, then rewrite the predicate as pure JavaScript.

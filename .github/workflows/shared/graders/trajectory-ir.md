## Canonical Trajectory IR

Every grader in `shared/graders/` is a projection over one shared,
canonical intermediate representation (IR) of a completed agent run, built
**once** per grader run rather than re-parsed per grader. Do not write a
bespoke parser per grader — build (or reuse) this IR first, then compute the
grader's formula against it.

### Why an IR

`gh-aw` already ships deterministic built-in graders (`tool-success-rate`,
`retries`, `loops`, `trajectory-efficiency`, `execution-step-count`,
`execution-duration`, `working-set-rebuild-factor`, `context-growth`,
`artifact-production` — see
[Graders reference](https://githubnext.github.io/gh-aw/reference/trace-graders/)).
Those cover step count, retries, generic loop counts, duration, tool-success
rate, and trajectory efficiency. The graders in this directory intentionally
do **not** duplicate those signals; they exist to answer different questions
(near-miss safety, exploration vs. exploitation, recurrence structure,
provenance, termination timing, planning conformance, reference-trajectory
fidelity). Build the IR once and project every new grader from it so adding a
26th grader never means writing a 26th trace parser.

### Source data

Build the IR from artifacts already produced by a gh-aw run (no new
telemetry, no network calls):

- `agent_output.json` — safe-output payload emitted by the agent
- MCP gateway / tool-call logs (tool name, arguments, result, success/failure, timestamps)
- `aw_info.json` / `run_summary.json` — run metadata, per-job/step status
- token usage / turn records (per-request input/output tokens)
- the workflow's own frontmatter (`safe-outputs`, declared objectives, `on:` triggers) when a grader needs declared expectations rather than inferred ones

### IR schema

```jsonc
{
  "runId": "string",
  "events": [
    // one entry per atomic occurrence, in execution order
    { "index": 0, "kind": "tool_call|observation|state_change|message|safe_output", "timestamp": "ISO8601", "ref": "id of actions[]/observations[]/... entry" }
  ],
  "states": [
    // canonical, coarse-grained snapshots of "where the agent is"
    // canonicalize so that semantically identical states collapse to the same id
    // (e.g. same file + same tool + same normalized argument shape)
    { "id": "string", "label": "string", "firstEventIndex": 0 }
  ],
  "actions": [
    { "id": "string", "eventIndex": 0, "type": "string", "target": "string|null", "validAtIssueTime": true, "consequential": true }
  ],
  "toolCalls": [
    { "id": "string", "eventIndex": 0, "name": "string", "arguments": {}, "success": true, "durationMs": 0, "outputRef": "observations[].id|null" }
  ],
  "observations": [
    { "id": "string", "eventIndex": 0, "sourceToolCallId": "string|null", "consumedByActionIds": ["string"] }
  ],
  "resources": [
    { "id": "string", "kind": "file|url|issue|pr|artifact", "uri": "string" }
  ],
  "provenanceEdges": [
    // directed edges: evidence/tool/environment -> derived action/output
    { "from": "observations[].id|toolCalls[].id", "to": "actions[].id|safe_output id", "relation": "informed|required-for|derived-from" }
  ],
  "objectives": [
    // declared or inferred completion/evidence conditions
    { "id": "string", "description": "string", "satisfiedAtEventIndex": null, "dependsOn": ["objective-id"] }
  ],
  "reference": null
  // optional: reference trajectory / patch for benchmark-mode graders (#20-#24).
  // Populate only when the workflow importing the grader supplies one; leave
  // null otherwise and let the grader report "not-applicable". Shape:
  // {
  //   "toolCalls": [{ "name": "string", "arguments": {} }], // tool-wise-score
  //   "states": ["states[].id"],                            // trajectory-ndtw
  //   "patch": { "files": [{ "path": "string" }] }          // code-search-recall
  // }
}
```

### Build procedure (once per run)

1. Read the trace sources listed above.
2. Emit `events[]` in strict chronological order; every tool call, tool
   result, safe-output emission, and detected state transition gets exactly
   one event.
3. Canonicalize states: normalize tool name + argument shape (drop volatile
   fields such as timestamps/request IDs) into a stable state id so that
   identical behavioral states collapse together across the whole run, not
   only on adjacent steps.
4. Link `toolCalls[].outputRef` to the `observations[]` entry it produced,
   and populate `observations[].consumedByActionIds` by checking whether
   later actions reference values that only appear in that observation
   (e.g. a file path, an ID, a computed number). An observation with an
   empty `consumedByActionIds` was never used.
5. Set `actions[].consequential` to `true` for actions with an observable
   side effect outside the sandbox (edits, comments, issue/PR mutations,
   safe outputs) and `false` for read-only actions (searches, listings,
   inspections); never infer this from substring matches on `type` at
   grading time — record the flag once, here, from the action's real
   effect. Populate `provenanceEdges[]` for every consequential action:
   trace it back to the tool call/observation that informed it, if any.
   Actions with no incoming edge are provenance gaps.
6. Populate `objectives[]` from declared expectations when available
   (workflow `safe-outputs` config, explicit task/issue checklist, README
   "Definition of Done") and mark `satisfiedAtEventIndex` the first event
   index at which the objective's evidence appears; leave `null` if never
   satisfied. Preserve explicitly declared prerequisite objective IDs in
   `dependsOn`; use an empty array when no dependency is declared, and never
   infer dependencies from execution order.
7. Record one `resources[]` entry of kind `file` for every file the agent
   located (searched, listed, read, or edited), and set
   `actions[].validAtIssueTime` to whether the action matched the
   valid-action schema of the state in which it was issued.
8. Write the IR to `/tmp/gh-aw/agent/graders/trajectory_ir.json` so a
   single run can compute more than one grader without rebuilding the IR.

### Output contract every grader in this directory follows

Each grader script returns either a finite number or an object with a finite
numeric `value`. Return `value: null` when required trace data is unavailable
or the grader is not applicable:

```jsonc
{
  "value": 0.0,
  "details": "short grounded summary citing IR event indices or ids"
}
```

The grader ID, name, unit, and `higher_is_better` or `lower_is_better`
direction come from its frontmatter. An object result may additionally include
`passed`, `severity`, `details`, or `message`; other fields are ignored by the
runtime. Use `{ value: null, passed: null, message: "not applicable: ..." }`
rather than fabricating a score when the trace does not contain the inputs the
grader needs (e.g. no reference trajectory for benchmark-mode graders or no
declared objectives for `objective-coverage`).

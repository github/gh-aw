---
graders:
  # Fraction of precompiled, workflow-supplied behavioral constraints
  # (config.constraints: { id, description, pattern, requireSuccess }) that
  # were both exercised (regex pattern matched at least one toolCalls/actions
  # entry) and passed (all matches succeeded/were valid at issue time, unless
  # requireSuccess: false) during the run. Constraints are stable across runs
  # of the same harness/skill, unlike per-run inferred objectives. Higher is
  # better: more declared requirements exercised and satisfied.
  skill-constraint-coverage:
    name: Skill Constraint Coverage
    unit: ratio
    direction: higher_is_better
    min: 0.0
    max: 1.0
    script: |
      const isRecord = value => value !== null && typeof value === "object" && !Array.isArray(value);
      const constraints = Array.isArray(config.constraints) ? config.constraints : [];

      if (constraints.length === 0) {
        return { value: null, unit: "ratio", passed: null, message: "not applicable: no constraints configured" };
      }

      const candidates = [
        trace,
        trace.trajectoryIR,
        trace.trajectoryIr,
        trace.ir,
        isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIR : null,
        isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIr : null,
        isRecord(trace.agentOutput) ? trace.agentOutput.trajectory : null,
        isRecord(trace.agentOutput) ? trace.agentOutput : null,
      ].filter(isRecord);

      const toolCalls = (candidates.find(value => Array.isArray(value.toolCalls) && value.toolCalls.some(isRecord))?.toolCalls ?? []).filter(isRecord);
      const actions = (candidates.find(value => Array.isArray(value.actions) && value.actions.some(isRecord))?.actions ?? []).filter(isRecord);

      if (toolCalls.length === 0 && actions.length === 0) {
        return { value: null, unit: "ratio", passed: null, message: "not applicable: trace lacks toolCalls/actions" };
      }

      // Project toolCalls/actions into a flat list of { text, ok } entries: text is
      // matched against each constraint's pattern, ok reflects success/validity.
      const entries = [];
      for (const call of toolCalls) {
        const name = typeof call.name === "string" ? call.name : "";
        let argsText = "";
        try {
          argsText = call.arguments === undefined ? "" : JSON.stringify(call.arguments);
        } catch {
          argsText = "";
        }
        entries.push({ text: `${name} ${argsText}`, ok: call.success !== false });
      }
      for (const action of actions) {
        const type = typeof action.type === "string" ? action.type : "";
        const target = typeof action.target === "string" ? action.target : "";
        entries.push({ text: `${type} ${target}`, ok: action.validAtIssueTime !== false });
      }

      // The denominator is every supplied constraint, including ones with an
      // invalid/empty pattern -- malformed constraints count as unmet rather
      // than being silently dropped from the fraction (which would inflate
      // the score). validConstraints tracks how many had a usable pattern.
      let validConstraints = 0;
      let invalidConstraints = 0;
      let exercised = 0;
      let covered = 0;
      const failing = [];
      for (const constraint of constraints) {
        const constraintRecord = isRecord(constraint) ? constraint : null;
        const patternSource = constraintRecord !== null && typeof constraintRecord.pattern === "string" ? constraintRecord.pattern : "";
        let regex = null;
        if (patternSource !== "") {
          try {
            regex = new RegExp(patternSource, "i");
          } catch {
            regex = null;
          }
        }
        if (regex === null) {
          invalidConstraints += 1;
          continue;
        }
        validConstraints += 1;
        const matches = entries.filter(entry => regex.test(entry.text));
        if (matches.length === 0) continue;
        exercised += 1;
        // Passing requires every matched entry to have succeeded/been valid
        // at issue time, unless the constraint opts out via requireSuccess: false.
        const requireSuccess = constraintRecord === null || constraintRecord.requireSuccess !== false;
        const passedConstraint = !requireSuccess || matches.every(entry => entry.ok);
        const label = constraintRecord !== null && typeof constraintRecord.id === "string" && constraintRecord.id !== "" ? constraintRecord.id : patternSource;
        if (passedConstraint) {
          covered += 1;
        } else if (failing.length < 5) {
          failing.push(label);
        }
      }

      if (validConstraints === 0) {
        return { value: null, unit: "ratio", passed: null, message: "not applicable: no constraints with a valid pattern configured" };
      }

      return {
        value: helpers.ratio(covered, constraints.length),
        unit: "ratio",
        details: `constraints=${constraints.length} exercised=${exercised} covered=${covered}${invalidConstraints === 0 ? "" : ` invalidPattern=${invalidConstraints}`}${failing.length === 0 ? "" : `; unmet: ${failing.join(", ")}`}`,
      };
---

<!--
skill-constraint-coverage converts a precompiled, workflow-supplied list of
behavioral constraints (config.constraints: { id, description, pattern,
requireSuccess }) into a harness-improvement signal: the fraction that were
both exercised (regex pattern matched at least one toolCalls/actions entry,
matched case-insensitively against "name arguments" for tool calls and "type
target" for actions) and passed (all matches succeeded/were valid at issue
time, unless requireSuccess: false) during the run. Unlike policy-near-miss
(keyword-matched guard objectives already declared as IR objectives) or the
not-yet-implemented objective-coverage (inferred per-run objectives),
constraints here are stable across runs of the same harness/skill, making the
metric trackable over time. The denominator is every supplied constraint,
including ones with an empty/invalid regex pattern -- malformed constraints
count as unmet rather than being silently dropped from the fraction, which
would otherwise inflate the score. A single constraint pattern may match
multiple entries; passing requires all of them to have succeeded/been valid,
unless requireSuccess: false. Reports not-applicable (passed: null) when no
constraints are configured, none have a valid pattern, or the trace lacks
toolCalls/actions.
-->

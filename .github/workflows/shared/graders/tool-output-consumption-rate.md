---
graders:
  # Measures the fraction of tool-originated observations that were actually
  # referenced by a later action, per observations[].consumedByActionIds in
  # the canonical Trajectory IR. A low rate indicates the agent frequently
  # called tools and then ignored their outputs (wasted or speculative tool
  # calls); a high rate indicates most tool outputs fed into subsequent
  # decisions. Higher is better.
  tool-output-consumption-rate:
    name: Tool Output Consumption Rate
    unit: ratio
    direction: higher_is_better
    min: 0.0
    max: 1.0
    script: |
      const isRecord = value => value !== null && typeof value === "object" && !Array.isArray(value);
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

      const candidate =
        candidates.find(value =>
          Array.isArray(value.observations) &&
          value.observations.some(isRecord) &&
          Array.isArray(value.toolCalls) &&
          value.toolCalls.some(isRecord)
        ) ??
        // Preserve observations-only traces so missing toolCalls is reported
        // as no tool-originated observations rather than no observations.
        candidates.find(value => Array.isArray(value.observations) && value.observations.some(isRecord)) ??
        candidates.find(value => Array.isArray(value.observations)) ??
        null;
      const observations = (candidate && Array.isArray(candidate.observations) ? candidate.observations : []).filter(isRecord);

      if (observations.length === 0) {
        return { value: null, unit: "ratio", passed: null, message: "not applicable: no observations in the trace" };
      }

      const toolCalls = (candidate && Array.isArray(candidate.toolCalls) ? candidate.toolCalls : []).filter(isRecord);
      const toolCallIds = new Set(
        toolCalls
          .map(toolCall => toolCall.id)
          .filter(id => typeof id === "string" && id !== "")
      );
      const toolObservations = observations.filter(
        observation =>
          typeof observation.sourceToolCallId === "string" &&
          observation.sourceToolCallId !== "" &&
          toolCallIds.has(observation.sourceToolCallId)
      );

      if (toolObservations.length === 0) {
        return { value: null, unit: "ratio", passed: null, message: "not applicable: no tool-originated observations in the trace" };
      }

      const isConsumed = observation =>
        Array.isArray(observation.consumedByActionIds) &&
        observation.consumedByActionIds.some(actionId => typeof actionId === "string" && actionId !== "");

      const consumed = toolObservations.filter(isConsumed);
      const unconsumedIds = toolObservations
        .filter(observation => !isConsumed(observation))
        .slice(0, 5)
        .map(observation =>
          typeof observation.id === "string" && observation.id !== "" ? observation.id : observation.sourceToolCallId
        );

      return {
        value: helpers.ratio(consumed.length, toolObservations.length),
        unit: "ratio",
        details: `toolObservations=${toolObservations.length} consumed=${consumed.length}${unconsumedIds.length === 0 ? "" : `; unconsumed: ${unconsumedIds.join(", ")}`}`,
      };
---

<!--
tool-output-consumption-rate computes the fraction of tool-originated
observations whose consumedByActionIds array is non-empty in the canonical
Trajectory IR, i.e. the fraction of tool outputs that a later action
actually referenced. Depends on observations[].sourceToolCallId,
observations[].consumedByActionIds, and toolCalls[].id from the IR. Reports
not-applicable (passed: null) when the trace has no observations, or no
observations that originate from a matching tool call, rather than fabricating
a value. Complements the built-in tool-success-rate grader (which measures
whether tool calls succeeded, not whether their outputs were used).
-->

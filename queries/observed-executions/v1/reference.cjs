"use strict";

// Executable specification for fixtures. This is NOT a KQL interpreter or Azure emulator.
const counter = value => (typeof value === "number" && Number.isFinite(value) && value >= 0 ? value : null);
const string = value => (value === undefined || value === null ? "" : String(value));
const numeric = value => (value === undefined || value === null || value === "" || !Number.isFinite(Number(value)) ? null : Number(value));
const physicalKey = row => JSON.stringify([row.TraceId, row.SpanId]);
const validId = (value, length) => typeof value === "string" && new RegExp(`^[0-9a-f]{${length}}$`).test(value) && !/^0+$/.test(value);
const inWindow = (row, options) => row.TimeGenerated >= options.start && row.TimeGenerated < options.end;
const identityFields = ["RepositoryName", "ExecutionRunId", "Attempt", "WorkflowRef", "Hop", "Job", "TraceId", "SpanId"];
const identity = row => Object.fromEntries(identityFields.map(key => [key, row[key] ?? null]));
const ordered = (rows, fields) =>
  rows.sort((a, b) => {
    for (const field of fields) {
      if (a[field] === b[field]) continue;
      if (a[field] === null || a[field] === undefined) return -1;
      if (b[field] === null || b[field] === undefined) return 1;
      return a[field] < b[field] ? -1 : 1;
    }
    return 0;
  });
const rowOrder = ["RepositoryName", "ExecutionRunId", "Attempt", "WorkflowRef", "Job", "SpanId"];
const signature = row =>
  JSON.stringify([row.Name, row.TimeGenerated, row.EndTime, row.DurationMs, row.ParentSpanId, row.StatusCode, row.StatusMessage, row.Attributes, row.ResourceAttributes, row.ItemCount, row.DroppedAttributesCount, row.DroppedEventsCount]);

function group(rows, key) {
  const groups = new Map();
  for (const row of rows) {
    const value = key(row);
    if (!groups.has(value)) groups.set(value, []);
    groups.get(value).push(row);
  }
  return [...groups.values()];
}

function normalizedSpan(copies) {
  const sorted = copies.toSorted((a, b) => (signature(a) < signature(b) ? -1 : signature(a) > signature(b) ? 1 : 0));
  const row = sorted.at(-1);
  const a = row.Attributes;
  const r = row.ResourceAttributes;
  const result = {
    ...row,
    PhysicalCopies: copies.length,
    ConflictingCopies: new Set(copies.map(signature)).size > 1,
    RepositoryName: string(a["gh-aw.repository"] || r["github.repository"]),
    ExecutionRunId: string(a["gh-aw.run.id"] || r["github.run_id"]),
    Attempt: numeric(a["gh-aw.run.attempt"] ?? r["github.run_attempt"]),
    WorkflowRef: string(r["github.workflow_ref"]),
    Hop: string(a["gh-aw.hop.id"] || a["gh-aw.workflow_call.id"]),
    Job: string(a["gh-aw.job.name"] || r["github.job"]),
    Engine: string(a["gh-aw.engine.id"] || r["gh-aw.engine.id"]),
  };
  result.Correlated = !!(result.RepositoryName && result.ExecutionRunId && result.Attempt > 0 && result.WorkflowRef && result.Job);
  return result;
}

function select(tables, options) {
  const valid = options.start < options.end && ["all", "exact", "latest-observed"].includes(options.mode) && (options.mode !== "exact" || options.attempt > 0);
  const raw = tables.OTelSpans.filter(row => inWindow(row, options) && row.ScopeName === "gh-aw" && row.Name.startsWith("gh-aw."));
  const physical = raw.filter(row => validId(row.TraceId, 32) && validId(row.SpanId, 16));
  const spans = group(physical, physicalKey).map(normalizedSpan);
  const latest = new Map();
  for (const row of spans) {
    if (row.RepositoryName && row.ExecutionRunId && row.Attempt > 0) {
      const key = JSON.stringify([row.RepositoryName, row.ExecutionRunId]);
      latest.set(key, Math.max(latest.get(key) || 0, row.Attempt));
    }
  }
  for (const row of spans) {
    const inScope = (!options.repository || row.RepositoryName === options.repository) && (!options.run || row.ExecutionRunId === options.run);
    row.IsSelected =
      valid &&
      inScope &&
      (options.mode === "all" || (options.mode === "exact" && row.Attempt === options.attempt) || (options.mode === "latest-observed" && row.Attempt === latest.get(JSON.stringify([row.RepositoryName, row.ExecutionRunId]))));
  }
  const selected = spans.filter(row => row.IsSelected);
  const dedicated = selected.filter(row => row.Name === "gh-aw.agent.agent" && row.Job === "agent");
  const parents = new Set(dedicated.map(row => physicalKey({ TraceId: row.TraceId, SpanId: row.ParentSpanId })));
  const owners = [...dedicated, ...selected.filter(row => row.Name === `gh-aw.${row.Job}.conclusion` && (["agent", "detection"].includes(row.Job) || (row.Engine && row.Job === row.Engine)) && !parents.has(physicalKey(row)))].map(row => {
    const usable = row.Correlated && !row.ConflictingCopies && (row.ItemCount ?? 1) === 1 && (row.DroppedAttributesCount ?? 0) === 0;
    const result = { ...row, AccountingUsable: usable };
    for (const [field, attribute] of [
      ["Aic", "gh-aw.aic"],
      ["InputTokens", "gen_ai.usage.input_tokens"],
      ["OutputTokens", "gen_ai.usage.output_tokens"],
      ["CacheReadTokens", "gen_ai.usage.cache_read.input_tokens"],
      ["CacheWriteTokens", "gen_ai.usage.cache_creation.input_tokens"],
    ]) {
      result[field] = usable ? counter(row.Attributes[attribute]) : null;
    }
    result.TotalTokens = result.InputTokens !== null && result.OutputTokens !== null ? result.InputTokens + result.OutputTokens : null;
    return result;
  });
  return { valid, raw, physical, spans, selected, owners };
}

function coverage(context, options) {
  const rows = context.selected;
  return [
    {
      ObservedSpans: rows.length,
      PhysicalCopies: rows.reduce((total, row) => total + row.PhysicalCopies, 0),
      MissingCorrelation: rows.filter(row => !row.Correlated).length,
      ConflictingSpans: rows.filter(row => row.ConflictingCopies).length,
      SampledSpans: rows.filter(row => row.ItemCount > 1).length,
      DroppedAttributes: rows.filter(row => row.DroppedAttributesCount > 0).length,
      DroppedEvents: rows.filter(row => row.DroppedEventsCount > 0).length,
      Dashboard: "observed executions",
      PackVersion: "1",
      SelectionValid: context.valid,
      AttemptMode: options.mode,
      RequestedAttempt: options.attempt,
      WindowStart: options.start,
      WindowEnd: options.end,
      ObservedExecutions: group(
        rows.filter(row => row.Correlated),
        row => JSON.stringify([row.RepositoryName, row.ExecutionRunId, row.Attempt, row.WorkflowRef, row.Hop])
      ).length,
      WindowPhysicalSpans: context.raw.length,
      WindowInvalidIdentity: context.raw.length - context.physical.length,
      Evidence: "observed window only; missing exports, jobs and later attempts unknown",
    },
  ];
}

function failures(context) {
  return ordered(
    context.selected.flatMap(row => {
      const status = string(row.Attributes["gh-aw.run.status"]);
      const failed = row.StatusCode.toLowerCase() === "error" || ["failure", "timeout", "timed_out", "cancelled"].includes(status);
      const exportErrors = counter(row.Attributes["gh-aw.otlp.export_errors"]);
      return failed || exportErrors > 0
        ? [
            {
              ...identity(row),
              Name: row.Name,
              ObservedFailure: failed,
              ObservedStatus: status,
              StatusCode: row.StatusCode,
              StatusMessage: row.StatusMessage,
              FailureCategories: row.Attributes["gh-aw.failure.categories"] ?? null,
              ExportErrors: exportErrors,
              ExportDetails: string(row.Attributes["gh-aw.otlp.export_error_details"]),
              Correlated: row.Correlated,
              ConflictingCopies: row.ConflictingCopies,
            },
          ]
        : [];
    }),
    rowOrder
  );
}

function phases(context) {
  return ordered(
    context.selected
      .filter(row => row.Name.endsWith(".setup") || row.Name.endsWith(".conclusion") || row.Name === "gh-aw.agent.agent")
      .map(row => {
        const measured = !!row.TimeGenerated && !!row.EndTime && row.EndTime >= row.TimeGenerated && counter(row.DurationMs) !== null && !row.ConflictingCopies;
        return {
          ...identity(row),
          Phase: row.Name === "gh-aw.agent.agent" ? "agent-execution" : row.Name.endsWith(".setup") ? "setup" : "job-window",
          StartTime: row.TimeGenerated,
          EndTime: row.EndTime,
          DurationMs: measured ? row.DurationMs : null,
          TimingState: measured ? "measured-span" : "unknown",
          Correlated: row.Correlated,
        };
      }),
    rowOrder
  );
}

function usage(context) {
  const dimensions = ["RepositoryName", "WorkflowRef", "Job", "Engine"];
  return ordered(
    group(context.owners, row => JSON.stringify(dimensions.map(key => row[key]))).map(rows => {
      const output = {
        ...Object.fromEntries(dimensions.map(key => [key, rows[0][key]])),
        Owners: rows.length,
        UnusableOwners: rows.filter(row => !row.AccountingUsable).length,
      };
      for (const [field, known, subtotal] of [
        ["Aic", "AicKnownOwners", "ObservedAic"],
        ["InputTokens", "InputKnownOwners", "ObservedInputTokens"],
        ["OutputTokens", "OutputKnownOwners", "ObservedOutputTokens"],
        ["TotalTokens", "TotalKnownOwners", "ObservedTotalTokens"],
        ["CacheReadTokens", "CacheReadKnownOwners", "ObservedCacheReadTokens"],
        ["CacheWriteTokens", "CacheWriteKnownOwners", "ObservedCacheWriteTokens"],
      ]) {
        const values = rows.map(row => row[field]).filter(value => value !== null);
        output[known] = values.length;
        output[subtotal] = values.length ? values.reduce((a, b) => a + b, 0) : null;
      }
      return output;
    }),
    dimensions
  );
}

const modelAttributes = {
  EffectiveRequestModel: "gen_ai.request.model",
  ResponseModel: "gen_ai.response.model",
  Effort: "gh-aw.model.effort",
  RoutingStatus: "gh-aw.model_routing.status",
  RoutingMode: "gh-aw.model_routing.mode",
  RouterVersion: "gh-aw.model_routing.router_version",
  FailureCode: "gh-aw.model_routing.failure_code",
  Objective: "gh-aw.model_routing.objective",
  TaskType: "gh-aw.model_routing.task_type",
  Scope: "gh-aw.model_routing.scope",
  Complexity: "gh-aw.model_routing.complexity",
};
function models(context) {
  return ordered(
    context.owners.map(row => ({
      ...identity(row),
      Engine: row.Engine,
      RequestedModel: string(row.Attributes["gh-aw.model.requested"] || row.Attributes["gen_ai.request.model"]),
      ...Object.fromEntries(Object.entries(modelAttributes).map(([field, attribute]) => [field, string(row.Attributes[attribute])])),
      Degraded: typeof row.Attributes["gh-aw.model_routing.degraded"] === "boolean" ? row.Attributes["gh-aw.model_routing.degraded"] : null,
      ClassifierAicDiagnosticOnly: counter(row.Attributes["gh-aw.model_routing.classifier_aic"]),
      DeviatedRequests: counter(row.Attributes["gh-aw.model_routing.deviated_requests"]),
      AccountingUsable: row.AccountingUsable,
    })),
    rowOrder
  );
}

function graders(context, tables, options) {
  if (!context.valid) return [];
  const parents = new Map(context.spans.map(row => [physicalKey(row), row]));
  const events = tables.OTelEvents.filter(row => inWindow(row, options) && row.Name === "grader.result");
  const rows = [];
  for (const copies of group(events, row => JSON.stringify([row.TraceId, row.SpanId, row.Attributes["gh-aw.grader.id"], row.Attributes["gh-aw.grader.source"]]))) {
    const row = copies.toSorted((a, b) => (JSON.stringify(a.Attributes) < JSON.stringify(b.Attributes) ? -1 : JSON.stringify(a.Attributes) > JSON.stringify(b.Attributes) ? 1 : 0)).at(-1);
    const parent = parents.get(physicalKey(row));
    if (parent && !parent.IsSelected) continue;
    if (!parent && options.repository && row.ResourceAttributes["github.repository"] !== options.repository) continue;
    const a = row.Attributes;
    const source = string(a["gh-aw.grader.source"]);
    const owned =
      parent &&
      ((parent.Name === "gh-aw.conclusion.conclusion" && parent.Job === "conclusion" && source === "builtin") || (parent.Name === "gh-aw.graders.conclusion" && parent.Job === "graders" && ["inline", "operational-value"].includes(source)));
    if (parent && !owned) continue;
    rows.push({
      ...identity(parent || { TraceId: row.TraceId, SpanId: row.SpanId, RepositoryName: "", ExecutionRunId: "", WorkflowRef: "", Hop: "", Job: "" }),
      GraderId: string(a["gh-aw.grader.id"]),
      Source: source,
      Status: string(a["gh-aw.grader.status"]),
      Value: numeric(a["gh-aw.grader.value"]),
      Unit: string(a["gh-aw.grader.unit"]),
      Passed: typeof a["gh-aw.grader.passed"] === "boolean" ? a["gh-aw.grader.passed"] : null,
      Ownership: owned ? "owned" : "unknown",
      EventTime: null,
      ClockState: "unavailable-native-event-clock",
      ParentEndTime: parent?.EndTime ?? null,
      EventCopies: copies.length,
      PartialEvidence: !owned || !parent.Correlated || parent.ConflictingCopies || new Set(copies.map(copy => JSON.stringify(copy.Attributes))).size > 1 || (row.ItemCount ?? 1) > 1 || (row.DroppedAttributesCount ?? 0) > 0,
    });
  }
  return ordered(rows, ["RepositoryName", "ExecutionRunId", "Attempt", "WorkflowRef", "Job", "GraderId"]);
}

function compact(context, tables, options) {
  if (!context.valid) return [];
  const parents = new Map(context.spans.map(row => [physicalKey(row), row]));
  const groups = group(
    tables.OTelLogs.filter(row => inWindow(row, options) && row.ScopeName === "gh-aw.session"),
    physicalKey
  );
  const seen = new Set(groups.map(rows => physicalKey(rows[0])));
  for (const parent of context.selected.filter(row => row.Name === "gh-aw.conclusion.setup" && !seen.has(physicalKey(row)))) groups.push([{ TraceId: parent.TraceId, SpanId: parent.SpanId, missing: true }]);
  const results = [];
  const quality = ["included_events", "omitted_events", "oversized_lines", "scan_truncated", "truncated"];
  for (const rows of groups) {
    const first = rows[0];
    const parent = parents.get(physicalKey(first));
    if (parent && !parent.IsSelected) continue;
    const missing = first.missing === true;
    const max = field => {
      const values = missing
        ? []
        : rows
            .map(row => row.ResourceAttributes[`gh-aw.session.${field}`])
            .filter(value => value !== undefined && value !== null)
            .map(Number);
      return values.length ? Math.max(...values) : null;
    };
    let untimed = 0;
    let unparseable = 0;
    for (const row of missing ? [] : rows) {
      let body;
      try {
        body = JSON.parse(row.Body);
      } catch (error) {
        if (!(error instanceof SyntaxError)) throw error;
      }
      if (!body || typeof body !== "object" || Array.isArray(body)) unparseable++;
      const timestamp = body?.timestamp;
      if (typeof timestamp !== "string" || !/^\d{4}-\d{2}-\d{2}T/.test(timestamp) || !Number.isFinite(Date.parse(timestamp))) untimed++;
    }
    const unknown = missing ? null : rows.filter(row => quality.some(field => row.ResourceAttributes[`gh-aw.session.${field}`] === undefined || row.ResourceAttributes[`gh-aw.session.${field}`] === null)).length;
    results.push({
      ...identity(parent || { TraceId: first.TraceId, SpanId: first.SpanId, RepositoryName: "", ExecutionRunId: "", WorkflowRef: "", Hop: "", Job: "" }),
      PhysicalLogRecords: missing ? null : rows.length,
      PhysicalUntimedRecords: missing ? null : untimed,
      UnparseableBodies: missing ? null : unparseable,
      UnknownQualityRecords: unknown,
      Included: max("included_events"),
      Omitted: max("omitted_events"),
      Oversized: max("oversized_lines"),
      ScanTruncated: max("scan_truncated"),
      Truncated: max("truncated"),
      ExporterUntimed: max("untimed_events"),
      Evidence: !parent?.Correlated
        ? "unknown-correlation"
        : missing
          ? "not-observed"
          : unknown > 0
            ? "unknown-quality"
            : max("omitted_events") > 0 || max("oversized_lines") > 0 || max("scan_truncated") === 1 || max("truncated") === 1 || untimed > 0 || unparseable > 0 || max("untimed_events") > 0
              ? "partial"
              : "compact-projection",
      ClockBasis: "body ISO timestamp only; TimeGenerated is not the event clock",
    });
  }
  return ordered(results, rowOrder);
}

function queryPack(tables, options) {
  const resolved = { mode: "all", attempt: 1, repository: "", run: "", ...options };
  const context = select(tables, resolved);
  return {
    coverage: coverage(context, resolved),
    failures: failures(context),
    phases: phases(context),
    usage: usage(context),
    "models-routing": models(context),
    graders: graders(context, tables, resolved),
    "compact-quality": compact(context, tables, resolved),
  };
}

module.exports = { queryPack, counter };

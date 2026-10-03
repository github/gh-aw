// Sanitized excerpts downloaded from github/gh-aw's retained agent artifacts.
// These are startup diagnostics plus bootstrap telemetry, NOT native lifecycle evidence.
const runs = {
  "37082362756": { timestamp: "2026-10-03T00:33:09", startupMs: 203, migrationMs: 3, jsonMigrationMs: 4, result: { type: "result", num_turns: 1 } },
  "36797163342": { timestamp: "2026-10-01T00:40:15", startupMs: 205, migrationMs: 4, jsonMigrationMs: 5, result: { type: "result", num_turns: 1, usage: { input_tokens: 0, output_tokens: 0 } } },
  "36503599029": { timestamp: "2026-09-29T00:35:08", startupMs: 198, migrationMs: 3, jsonMigrationMs: 5, result: { type: "result", num_turns: 1, usage: { input_tokens: 0, output_tokens: 0 } } },
  "36282968227": { timestamp: "2026-09-27T00:37:45", startupMs: 205, migrationMs: 2, jsonMigrationMs: 4, result: { type: "result", num_turns: 1, usage: { input_tokens: 0, output_tokens: 0 } } },
};

const logs = Object.fromEntries(
  Object.entries(runs).map(([id, run]) => [
    id,
    [
      "GITHUB_EVENT_PATH /home/runner/work/_temp/_github_workflow/event.json does not exist",
      `INFO  ${run.timestamp} +${run.startupMs}ms service=default version=1.2.14 args=["run","--print-logs","--log-level","DEBUG","[sanitized prompt]"]`,
      "Performing one time database migration, may take a few minutes...",
      `INFO  ${run.timestamp} +0ms service=db path=/tmp/opencode-data/opencode/opencode.db opening database`,
      `INFO  ${run.timestamp} +${run.migrationMs}ms service=db count=3 mode=bundled applying migrations`,
      `INFO  ${run.timestamp} +${run.jsonMigrationMs}ms service=json-migration storage directory does not exist, skipping migration`,
      "sqlite-migration:done",
      "Database migration complete.",
      JSON.stringify(run.result),
      "",
    ].join("\n"),
  ])
);

module.exports = { logs };

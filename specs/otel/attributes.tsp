import "@typespec/json-schema";

using TypeSpec.JsonSchema;

namespace GH.AW.Telemetry;

@jsonSchema
model AttributeNames {
  workflowName: "gh-aw.workflow.name";
  repository: "gh-aw.repository";
  runId: "gh-aw.run.id";
  githubRunId: "github.run_id";
  engineId: "gh-aw.engine.id";
}

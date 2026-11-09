import type { CreatedIssueArtifact, CreateIssueItem, UpdateIssueItem, IssueArtifact, IssueArtifactBodyInput, IssueArtifactInput, IssueArtifactType } from "./safe-outputs";
import type { CreateIssueConfig, UpdateIssueConfig } from "./safe-outputs-config";
import type { HandlerConfig } from "./handler-factory";
import type { CreateIssueParameters, CreateIssueResult } from "../create_issue.cjs";
import { createIssue } from "../create_issue.cjs";
import { createIssue as createIssueFromIndex } from "../index.cjs";
import { createIssueArtifacts, upsertIssueArtifacts, normalizeIssueArtifacts, sanitizeIssueArtifacts } from "../issue_artifacts.cjs";

const inputs: IssueArtifactBodyInput[] = [
  { name: "Notes", body: "Research notes" },
  { type: "generic", name: "Notes", body: "Research notes" },
  { type: "plan", name: "Plan", body: "Implementation plan" },
  { type: "link", name: "Reference", body: "https://github.com/github/gh-aw" },
  { type: "embedded_html", name: "Preview", body: "<h1>Preview</h1>" },
];
const item: CreateIssueItem = { type: "create_issue", title: "Issue", body: "Details", artifacts: inputs };
const updateItem: UpdateIssueItem = { type: "update_issue", artifacts: inputs };
void updateItem;
const parameters: CreateIssueParameters = { title: item.title, body: item.body, artifacts: item.artifacts };
void parameters;
const fileInput: IssueArtifactInput = { type: "plan", name: "Plan", path: "/tmp/gh-aw/agent/plan.md" };
void fileInput;
// @ts-expect-error An MCP artifact has exactly one content source.
const ambiguousInput: IssueArtifactInput = { name: "Plan", body: "Plan", path: "/tmp/plan.md" };
void ambiguousInput;
// @ts-expect-error Recorded output must contain content, never a path to be read by a privileged job.
item.artifacts = [fileInput];
// @ts-expect-error Updates also record content rather than privileged file paths.
updateItem.artifacts = [fileInput];

const normalized = normalizeIssueArtifacts(inputs);
const sanitized = sanitizeIssueArtifacts(normalized, {});
const artifactType: IssueArtifactType = sanitized[0].type;
void artifactType;

// @ts-expect-error Normalized artifacts require an explicit format.
const missingNormalizedType: IssueArtifact = { name: "Notes", body: "Details" };
void missingNormalizedType;
// @ts-expect-error Sanitization requires normalized artifacts, not optional input formats.
sanitizeIssueArtifacts(inputs, {});
// @ts-expect-error Artifact inputs require a body.
const missingBody: IssueArtifactInput = { name: "Notes" };
void missingBody;
// @ts-expect-error Artifact inputs require a name.
const missingName: IssueArtifactInput = { body: "Details" };
void missingName;
// @ts-expect-error Artifact bodies must be strings.
const numericBody: IssueArtifactInput = { name: "Notes", body: 42 };
void numericBody;
// @ts-expect-error Normalization does not widen the format to an arbitrary string.
normalized[0].type = "unknown";

const frontmatter: CreateIssueConfig = { type: "create_issue", "max-issue-artifacts": 3, "max-issue-artifact-size": "1mb" };
const updateConfig: UpdateIssueConfig = { type: "update_issue", "max-issue-artifacts": 3, "max-issue-artifact-size": "1mb" };
void updateConfig;
frontmatter["max-issue-artifact-size"] = 1048576;
const compiled: HandlerConfig = { max_issue_artifacts: 3, max_issue_artifact_size: 1048576 };
void compiled;
// @ts-expect-error Artifact counts must be numeric.
frontmatter["max-issue-artifacts"] = "3";
// @ts-expect-error The misspelled artifact count option is not supported.
const misspelledConfig: CreateIssueConfig = { type: "create_issue", "max-isssue-artifacts": 3 };
void misspelledConfig;
// @ts-expect-error Human-friendly sizes must be compiled to bytes before runtime.
compiled.max_issue_artifact_size = "1mb";

export async function checkIssueArtifactApiTypes(): Promise<void> {
  // @ts-expect-error The shared entry point preserves artifact format checking.
  await createIssueFromIndex({ title: "Issue", body: "Details", artifacts: [{ type: "unknown", name: "Notes", body: "Details" }] });
  // @ts-expect-error Custom jobs cannot specify an unsupported artifact format.
  await createIssue({ title: "Issue", body: "Details", artifacts: [{ type: "unknown", name: "Notes", body: "Details" }] });
  // @ts-expect-error Custom jobs require artifact names.
  await createIssue({ title: "Issue", body: "Details", artifacts: [{ body: "Details" }] });
  // @ts-expect-error Custom jobs require artifact bodies.
  await createIssue({ title: "Issue", body: "Details", artifacts: [{ name: "Notes" }] });
  // @ts-expect-error Path expansion belongs to the MCP backend, not the privileged helper.
  await createIssue({ title: "Issue", body: "Details", artifacts: [fileInput] });

  const created = await createIssueArtifacts(github, "owner", "repo", 1, normalized);
  const upserted: CreatedIssueArtifact[] = await upsertIssueArtifacts(github, "owner", "repo", 1, sanitized);
  void upserted;
  const metadata: CreatedIssueArtifact[] = created;
  void metadata;
  const number: number = created[0].number;
  const url: string | undefined = created[0].url;
  void number;
  void url;
  // @ts-expect-error Returned artifact numbers cannot be strings.
  created[0].number = "1";
  // @ts-expect-error Returned URLs are strings or undefined, not arbitrary API data.
  created[0].url = 42;

  const result = await createIssue({ title: "Issue", body: "Details", artifacts: inputs });
  const namedResult: CreateIssueResult = result;
  void namedResult;
  const sharedResult: CreateIssueResult = await createIssueFromIndex(parameters);
  void sharedResult;
  if (result.staged) {
    const preview: IssueArtifact[] | undefined = result.preview.artifacts;
    void preview;
    // @ts-expect-error A staged issue has no created issue.
    void result.issue;
  } else {
    const artifacts: CreatedIssueArtifact[] | undefined = result.artifacts;
    void artifacts;
    // @ts-expect-error An applied issue has no staged preview.
    void result.preview;
  }
}

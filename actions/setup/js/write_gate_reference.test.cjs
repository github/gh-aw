const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const Ajv = require("ajv/dist/2020");
const { itemDigest, inputDigest } = require("./pending_writes.cjs");
const validateManifest = new Ajv({ strict: false }).compile(require("./pending_writes.schema.json"));

const exampleRoot = path.resolve(__dirname, "../../../docs/examples/write-gate");
const source = fs.readFileSync(path.join(exampleRoot, "shared.md"), "utf8");
const script = source.match(/          script: \|\n([\s\S]+?)\n---/)[1].replace(/^            /gm, "");
const execute = new (Object.getPrototypeOf(async function () {}).constructor)("github", "core", "process", "require", script);
const policy = JSON.parse(fs.readFileSync(path.join(exampleRoot, "policy.json"), "utf8"));
const workflowSHA = "1234567890abcdef1234567890abcdef12345678";

function item(type, output_id, paths) {
  const value = {
    type,
    output_id,
    complete: true,
    reason_category: "ready",
    target: { repository: "target/project" },
    depends_on: [],
    free_text: [],
    settings: { staged: false, sha256: inputDigest({}) },
    input_sha256: inputDigest({ type, output_id }),
  };
  if (paths) {
    value.code = {
      transport: "patch",
      base_commit: workflowSHA,
      complete: true,
      sha256: inputDigest("fixture patch"),
      length: 13,
      patch_rendering: { path: "aw.patch", length: 13, sha256: inputDigest("fixture patch") },
      branch_salt: null,
      additions: paths.length,
      deletions: 0,
      evaluated_paths: paths,
      files: paths.map(name => ({
        path: name,
        previous_path: null,
        status: "modified",
        old_mode: "100644",
        new_mode: "100644",
        mode_changed: false,
        binary: false,
        submodule: false,
        additions: 1,
        deletions: 0,
      })),
    };
  }
  return { ...value, item_sha256: itemDigest(value) };
}

function envelope(manifest) {
  return {
    schema: "gh-aw/pending-writes/v1",
    run: {
      repository: "control/policy",
      workflow_ref: "control/policy/.github/workflows/agent.lock.yml@refs/heads/main",
      workflow_sha: workflowSHA,
      run_id: "123",
      run_attempt: "1",
      event: "workflow_dispatch",
    },
    detection: {
      enabled: true,
      mode: "block",
      conclusion: "success",
      reason_category: "unknown",
      threat_flags: [],
    },
    inputs: [],
    ...manifest,
  };
}

async function run(manifest, options = {}) {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "write-gate-reference-"));
  const pending = path.join(directory, "pending.json");
  const output = path.join(directory, "decisions.json");
  try {
    const fixture = typeof manifest === "string" ? manifest : envelope(manifest);
    fs.writeFileSync(pending, typeof fixture === "string" ? fixture : JSON.stringify(fixture));
    const bytes = options.policyBytes || Buffer.from(JSON.stringify(options.policy || policy));
    const getContent = vi.fn().mockResolvedValue({
      data: { type: "file", encoding: "base64", size: bytes.length, content: bytes.toString("base64") },
    });
    if (options.apiError) getContent.mockRejectedValue(new Error("private policy response must not escape"));
    const setFailed = vi.fn();
    await execute(
      { rest: { repos: { getContent } } },
      { setFailed },
      {
        env: {
          GH_AW_PENDING_WRITES: pending,
          GH_AW_WRITE_DECISIONS: output,
          CONTROL_WORKFLOW_REF: "control/policy/.github/workflows/agent.lock.yml@refs/heads/main",
          CONTROL_WORKFLOW_SHA: workflowSHA,
          CONTROL_RUN_ID: "123",
          CONTROL_RUN_ATTEMPT: "1",
        },
      },
      require
    );
    return { result: JSON.parse(fs.readFileSync(output, "utf8")), getContent, setFailed, fixture };
  } finally {
    fs.rmSync(directory, { recursive: true, force: true });
  }
}

describe("control repository reference write gate", () => {
  it("reads policy at the workflow SHA and emits only decision metadata", async () => {
    const manifest = {
      complete: true,
      items: [
        item("create_issue", "so_123456789012"),
        item("create_pull_request", "so_123456789013", ["docs/guide.md"]),
        item("create_pull_request", "so_123456789014", ["src/main.go"]),
        item("create_pull_request", "so_123456789015", [".github/workflows/agent.yml"]),
        item("create_pull_request", "so_123456789016", ["package.json"]),
      ],
    };
    const { result, getContent, setFailed, fixture } = await run(manifest);
    expect(validateManifest(fixture), JSON.stringify(validateManifest.errors)).toBe(true);
    expect(getContent).toHaveBeenCalledExactlyOnceWith({
      owner: "control",
      repo: "policy",
      path: ".github/write-policy.json",
      ref: workflowSHA,
    });
    expect(setFailed).not.toHaveBeenCalled();
    expect(result).toEqual({
      decision: "partial",
      reason: "policy_evaluated",
      items: manifest.items.map((value, index) => ({
        output_id: value.output_id,
        decision: ["apply", "apply", "review", "hold", "drop"][index],
        reason: `policy_${["apply", "apply", "review", "hold", "drop"][index]}`,
      })),
    });
    expect(JSON.stringify(result)).not.toContain("docs/guide.md");
  });

  it("considers both sides of a rename and never widens a held type", async () => {
    const { result } = await run({
      complete: true,
      items: [item("create_pull_request", "so_123456789012", [".github/old.yml", "docs/new.md"]), item("push_to_pull_request_branch", "so_123456789013", ["docs/guide.md"])],
    });
    expect(result.items.map(value => value.decision)).toEqual(["hold", "hold"]);
  });

  it("drops an unlisted downstream publication without rejecting its ID namespace", async () => {
    const publication = item("repo_memory", "job_1234567890abcdef");
    publication.target.job = "push_repo_memory";
    publication.publication = { id: "default", scope: "supplied_inputs", sha256: inputDigest([]), files: [] };
    publication.item_sha256 = itemDigest(publication);
    const { result, fixture } = await run({
      complete: true,
      items: [item("create_issue", "so_123456789012"), publication],
    });
    expect(validateManifest(fixture), JSON.stringify(validateManifest.errors)).toBe(true);
    expect(result.items.map(value => value.decision)).toEqual(["apply", "drop"]);
  });

  it("keeps staged types staged and composes overlapping path rules restrictively", async () => {
    const { result } = await run(
      {
        complete: true,
        items: [item("create_pull_request", "so_123456789012", ["src/main.go"]), item("push_to_pull_request_branch", "so_123456789013", ["docs/shared/file.md"])],
      },
      {
        policy: {
          ...policy,
          types: { ...policy.types, create_pull_request: "stage" },
          paths: { ...policy.paths, hold: ["docs/"], review: ["docs/shared/", "src/"] },
        },
      }
    );
    expect(result.items.map(value => value.decision)).toEqual(["stage", "review"]);
  });

  it.each([
    ["invalid JSON", "{"],
    ["unknown schema", { schema: "gh-aw/pending-writes/v2", complete: true, items: [] }],
    ["another workflow commit", { complete: true, items: [], run: { ...envelope({}).run, workflow_sha: "f".repeat(40) } }],
    ["another run attempt", { complete: true, items: [], run: { ...envelope({}).run, run_attempt: "2" } }],
    ["incomplete batch", { complete: false, items: [] }],
    ["incomplete item", { complete: true, items: [{ ...item("create_issue", "so_123456789012"), complete: false }] }],
    ["missing code evidence", { complete: true, items: [item("create_pull_request", "so_123456789012")] }],
    ["incomplete code evidence", { complete: true, items: [{ ...item("create_pull_request", "so_123456789012", ["docs/file.md"]), code: { ...item("create_pull_request", "so_123456789012", ["docs/file.md"]).code, complete: false } }] }],
    ["unsafe path", { complete: true, items: [item("create_pull_request", "so_123456789012", ["../docs/file.md"])] }],
    ["duplicate ID", { complete: true, items: [item("create_issue", "so_123456789012"), item("create_issue", "so_123456789012")] }],
  ])("fails closed on %s", async (_name, manifest) => {
    const { result, setFailed } = await run(manifest);
    expect(result).toEqual({ decision: "deny", reason: "policy_error" });
    expect(setFailed).toHaveBeenCalledOnce();
  });

  it.each([{ apiError: true }, { policy: { ...policy, version: 2 } }, { policy: { ...policy, types: { create_issue: "unknown" } } }])("fails closed on policy failures without logging private responses", async options => {
    const { result, setFailed } = await run({ complete: true, items: [] }, options);
    expect(result).toEqual({ decision: "deny", reason: "policy_error" });
    expect(setFailed).toHaveBeenCalledExactlyOnceWith("Reference write gate: policy or manifest validation failed; writes denied.");
  });

  it.each([65536, 65537])("enforces the policy byte limit at %s bytes", async size => {
    const policyBytes = Buffer.from(JSON.stringify(policy).padEnd(size, " "));
    const { result } = await run({ complete: true, items: [] }, { policyBytes });
    expect(result.decision).toBe(size === 65536 ? "partial" : "deny");
  });

  it.each([1048576, 1048577])("enforces the manifest byte limit at %s bytes", async size => {
    const manifest = JSON.stringify(envelope({ complete: true, items: [] })).padEnd(size, " ");
    const { result } = await run(manifest);
    expect(result.decision).toBe(size === 1048576 ? "partial" : "deny");
  });
});

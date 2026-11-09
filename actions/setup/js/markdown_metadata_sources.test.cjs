const fs = require("node:fs");
const path = require("node:path");
const crypto = require("node:crypto");
const os = require("node:os");
const metadata = require("./markdown_metadata.cjs");
const revision = "a".repeat(40);
const source = "description: Private reader\nmodel: small\ntools: [Read]";
const document = `---\n${source}\n---\nRead files.`;
const record = value => ({ sha256: crypto.createHash("sha256").update(source).digest("hex"), kind: "artifact", value });
const catalog = records => ({ version: 2, records });
const headers = records => `# gh-aw-metadata: {}\n# gh-aw-manifest: ${JSON.stringify({ version: 1, markdown_metadata: catalog(records) })}\n# generated\ninvalid: [yaml\n`;
const remoteFile = (file, records) => ({ data: { type: "file", path: file, encoding: "base64", content: Buffer.from(headers(records)).toString("base64") } });

describe("compiler metadata source acquisition", () => {
  let workspace;
  let additionalPaths;
  let originalCore;
  beforeEach(() => {
    originalCore = global.core;
    global.core = { debug: vi.fn(), warning: vi.fn() };
    additionalPaths = [];
    workspace = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-metadata-sources-"));
    fs.mkdirSync(path.join(workspace, ".github/workflows"), { recursive: true });
    vi.stubEnv("GH_AW_TMP_DIR", workspace);
    metadata.installMarkdownMetadata(catalog([]));
  });
  afterEach(() => {
    vi.unstubAllEnvs();
    fs.rmSync(workspace, { recursive: true, force: true });
    for (const directory of additionalPaths) fs.rmSync(directory, { recursive: true, force: true });
    vi.restoreAllMocks();
    global.core = originalCore;
  });

  it("reads the immutable executing revision rather than a replaced checkout lock", async () => {
    const file = ".github/workflows/main.lock.yml";
    fs.writeFileSync(path.join(workspace, file), headers([record({ model: "forged" })]));
    const githubClient = { rest: { repos: { getContent: vi.fn(async () => remoteFile(file, [record({ model: "small", tools: ["Read"] })])) } } };
    await metadata.loadMarkdownMetadata(`owner/repo/${file}@refs/heads/main`, workspace, { repository: "owner/repo", revision, githubClient });
    expect(githubClient.rest.repos.getContent).toHaveBeenCalledWith({ owner: "owner", repo: "repo", path: file, ref: revision });
    expect(metadata.readMarkdownMetadata("agent", document).value).toEqual({ model: "small", tools: ["Read"] });
  });

  it("never falls back to checkout metadata after a source authorization failure", async () => {
    const file = ".github/workflows/main.lock.yml";
    fs.writeFileSync(path.join(workspace, file), headers([record({ model: "forged" })]));
    const failure = Object.assign(new Error("Forbidden"), { status: 403 });
    const githubClient = { rest: { repos: { getContent: vi.fn().mockRejectedValue(failure) } } };
    await expect(metadata.loadMarkdownMetadata(`owner/repo/${file}@main`, workspace, { revision, githubClient })).rejects.toBe(failure);
    expect(githubClient.rest.repos.getContent).toHaveBeenCalledTimes(1);
    expect(() => metadata.readMarkdownMetadata("agent", document)).toThrow(/compiler manifest/);
  });

  it("merges matching caller checkout records for reusable workflows and preserves cache transport", async () => {
    fs.mkdirSync(path.join(workspace, ".github/agents"), { recursive: true });
    fs.writeFileSync(path.join(workspace, ".github/agents/reader.agent.md"), document);
    const value = { description: "Private reader", model: "small", tools: ["Read"] };
    fs.writeFileSync(path.join(workspace, ".github/workflows/caller.lock.yml"), headers([record(value)]));
    const file = ".github/workflows/reusable.lock.yml";
    const githubClient = { rest: { repos: { getContent: vi.fn(async () => remoteFile(file, [])) } } };
    const result = await metadata.loadMarkdownMetadata(`source/library/${file}@v1`, workspace, { repository: "caller/repo", revision, githubClient });
    expect(result.records).toHaveLength(1);
    expect(metadata.readMarkdownMetadata("agent", document).value).toEqual(value);
    fs.writeFileSync(path.join(workspace, "aw_info.json"), JSON.stringify({ run_id: 123 }));
    metadata.persistMarkdownMetadata();
    expect(JSON.parse(fs.readFileSync(path.join(workspace, "aw_info.json"), "utf8"))).toEqual({ run_id: 123, markdown_metadata: result });
  });

  it("merges consistent records but rejects conflicting records atomically", () => {
    metadata.installMarkdownMetadata(catalog([record({ model: "small", tools: ["Read"] })]));
    metadata.installMarkdownMetadata(catalog([record({ tools: ["Read"], model: "small" })]), true);
    expect(() => metadata.installMarkdownMetadata(catalog([record({ model: "forged" })]), true)).toThrow(/Conflicting/);
    expect(metadata.readMarkdownMetadata("agent", document).value.model).toBe("small");
  });

  it("restores native catalog values from both existing headers before checking provenance", async () => {
    const file = ".github/workflows/main.lock.yml";
    const value = { model: "small", tools: ["Read"], description: "Private reader", rate: 0.25 };
    const compiled = { ...record({ description: "Private reader", rate: 0.25 }), references: { "/model": "/metadata/agent_model", "/tools": "/manifest/declared_tools" } };
    const header = `# gh-aw-metadata: ${JSON.stringify({ agent_model: "small" })}\n# gh-aw-manifest: ${JSON.stringify({ version: 1, declared_tools: ["Read"], markdown_metadata: catalog([compiled]) })}\n# generated\n`;
    fs.writeFileSync(path.join(workspace, file), header);
    const result = await metadata.loadMarkdownMetadata(`owner/repo/${file}@main`, workspace);
    expect(result).toEqual(catalog([record(value)]));
    expect(metadata.readMarkdownMetadata("agent", document).value).toEqual(value);
    expect(header.match(/"small"/g)).toHaveLength(1);
    expect(header.match(/"Read"/g)).toHaveLength(1);
    expect(header).not.toContain("value_json");
    expect(() => metadata.readMarkdownMetadata("agent", document.replace("small", "forged"))).toThrow(/compiler manifest/);
    fs.writeFileSync(path.join(workspace, "aw_info.json"), '{"run_id":123}');
    metadata.persistMarkdownMetadata();
    expect(JSON.parse(fs.readFileSync(path.join(workspace, "aw_info.json"), "utf8")).markdown_metadata).toEqual(result);
  });

  it.each([
    { "/model": "/metadata/missing" },
    { "/model": "/manifest/markdown_metadata/version" },
    { "/model": "/metadata/agent_model", "/model/nested": "/metadata/agent_model" },
    { "/model": "/metadata/agent~2model" },
    { "/description": "/metadata/agent_model" },
    { "/model/nested": "/metadata/agent_model" },
  ])("rejects missing, recursive, overlapping, malformed, and conflicting references (%j)", references => {
    const file = path.join(workspace, ".github/workflows/main.lock.yml");
    const compiled = { ...record({ description: "Private reader" }), references };
    fs.writeFileSync(file, `# gh-aw-metadata: {"agent_model":"small"}\n# gh-aw-manifest: ${JSON.stringify({ markdown_metadata: catalog([compiled]) })}\n# generated\n`);
    expect(() => metadata.readLocalMarkdownMetadata(file, workspace)).toThrow(/metadata.*reference/);
  });

  it("resolves escaped object names, array slots, and whole values without adding parser dependencies", () => {
    const file = path.join(workspace, ".github/workflows/main.lock.yml");
    const compiled = [
      { ...record({ "x/y~z": [null, "unique"] }), references: { "/x~1y~0z/0": "/manifest/a~1b~0c/0" } },
      { ...record(null), sha256: "b".repeat(64), references: { "": "/manifest/a~1b~0c" } },
    ];
    fs.writeFileSync(file, `# gh-aw-metadata: {}\n# gh-aw-manifest: ${JSON.stringify({ "a/b~c": ["model"], markdown_metadata: catalog(compiled) })}\n# generated\n`);
    expect(metadata.readLocalMarkdownMetadata(file, workspace).records.map(record => record.value)).toEqual([{ "x/y~z": ["model", "unique"] }, ["model"]]);
    expect(() => metadata.installMarkdownMetadata(catalog(compiled))).toThrow(/metadata record/);
  });

  it("acquires only matching skill records from compiler headers using the supplied source client", async () => {
    const file = ".github/workflows/metadata.lock.yml";
    const value = { description: "Private reader", model: "small", tools: ["Read"] };
    const githubClient = { rest: { repos: { getContent: vi.fn(async request => (request.path === ".github/workflows" ? { data: [{ type: "file", path: file }] } : remoteFile(file, [record(value)]))) } } };
    await metadata.ensureRemoteMarkdownMetadata([document], githubClient, { repository: "private/skills", ref: revision });
    expect(githubClient.rest.repos.getContent).toHaveBeenNthCalledWith(1, { owner: "private", repo: "skills", path: ".github/workflows", ref: revision });
    expect(metadata.readMarkdownMetadata("agent", document).value).toEqual(value);
    await metadata.ensureRemoteMarkdownMetadata([document], githubClient, { repository: "private/skills", ref: revision });
    expect(githubClient.rest.repos.getContent).toHaveBeenCalledTimes(2);
  });

  it("resolves a dynamic source ref once and requests every header at that immutable revision", async () => {
    const file = ".github/workflows/metadata.lock.yml";
    const githubClient = {
      rest: {
        repos: {
          getCommit: vi.fn(async () => ({ data: { sha: revision } })),
          getContent: vi.fn(async request => (request.path === ".github/workflows" ? { data: [{ type: "file", path: file }] } : remoteFile(file, [record({ model: "small" })]))),
        },
      },
    };
    await metadata.ensureRemoteMarkdownMetadata([document], githubClient, { repository: "owner/skills", ref: "release" });
    expect(githubClient.rest.repos.getCommit).toHaveBeenCalledWith({ owner: "owner", repo: "skills", ref: "release" });
    for (const [request] of githubClient.rest.repos.getContent.mock.calls) expect(request.ref).toBe(revision);
  });

  it("fails explicitly when a source provides no matching compiler manifest", async () => {
    const githubClient = { rest: { repos: { getContent: vi.fn(async () => ({ data: [] })) } } };
    await expect(metadata.ensureRemoteMarkdownMetadata([document], githubClient, { repository: "owner/skills", ref: revision })).rejects.toThrow(/compiler manifest/);
    expect(() => metadata.readMarkdownMetadata("agent", document)).toThrow(/compiler manifest/);
  });

  it("does not discover author files through a parent-directory symlink outside the checkout", async () => {
    const outside = `${workspace}-outside`;
    additionalPaths.push(outside);
    fs.mkdirSync(path.join(outside, "agents"), { recursive: true });
    fs.writeFileSync(path.join(outside, "agents/reader.md"), document);
    fs.symlinkSync(outside, path.join(workspace, ".claude"));
    const read = vi.spyOn(fs, "readFileSync");
    const file = ".github/workflows/main.lock.yml";
    const githubClient = { rest: { repos: { getContent: vi.fn(async () => remoteFile(file, [])) } } };
    await metadata.loadMarkdownMetadata(`owner/repo/${file}@main`, workspace, { revision, githubClient });
    expect(read).not.toHaveBeenCalledWith(path.join(workspace, ".claude/agents/reader.md"), "utf8");
    expect(metadata.currentMarkdownMetadata().records).toEqual([]);
  });

  it("rejects malformed caller catalogs and missing cache transport explicitly", () => {
    const file = path.join(workspace, ".github/workflows/main.lock.yml");
    fs.writeFileSync(file, '# gh-aw-metadata: {}\n# gh-aw-manifest: {"markdown_metadata":{"version":1}}\n# generated\n');
    expect(() => metadata.ensureCheckoutMarkdownMetadata([document], workspace)).toThrow(/Invalid compiler Markdown metadata manifest/);
    expect(() => metadata.persistMarkdownMetadata()).toThrow(/cache is unavailable/);
  });

  it("acquires selected caller workflow inline metadata during runtime import and persists it", async () => {
    const value = { description: "Private reader", model: "small", tools: ["Read"] };
    const body = `---\nengine: copilot\n---\n## agent: reader\n${document}\n## end agent: reader\n`;
    fs.writeFileSync(path.join(workspace, ".github/workflows/caller.md"), body);
    fs.writeFileSync(path.join(workspace, ".github/workflows/caller.lock.yml"), headers([record(value)]));
    fs.writeFileSync(path.join(workspace, "aw_info.json"), JSON.stringify({ run_id: 123 }));
    const result = await require("./runtime_import.cjs").processRuntimeImport(".github/workflows/caller.md", false, workspace);
    expect(result).toContain(JSON.stringify(value));
    expect(result).not.toContain("engine: copilot");
    const info = JSON.parse(fs.readFileSync(path.join(workspace, "aw_info.json"), "utf8"));
    expect(info.run_id).toBe(123);
    expect(info.markdown_metadata.records).toHaveLength(2);
  });
});

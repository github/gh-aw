const fs = require("node:fs");
const path = require("node:path");
const crypto = require("node:crypto");
const os = require("node:os");
const { execFileSync } = require("node:child_process");
const metadata = require("./markdown_metadata.cjs");
const revision = "a".repeat(40);
const source = "description: Private reader\nmodel: small\ntools: [Read]";
const document = `---\n${source}\n---\nRead files.`;
const record = value => ({ sha256: crypto.createHash("sha256").update(source).digest("hex"), kind: "artifact", value });
const catalog = records => ({ version: 2, records });
const decodedCatalog = records => catalog(records.map(record => ({ ...record, sha256: record.sha256.slice(0, 20) })));
const fields = records => metadata.metadataManifestFields(catalog(records));
const headers = records => `# gh-aw-metadata: {}\n# gh-aw-manifest: ${JSON.stringify({ version: 1, ...fields(records) })}\n# generated\ninvalid: [yaml\n`;
const remoteFile = (file, records) => ({ data: { type: "file", path: file, encoding: "base64", content: Buffer.from(headers(records)).toString("base64") } });

describe("compiler metadata source acquisition", () => {
  let workspace;
  let additionalPaths;
  let originalCore;
  beforeEach(() => {
    originalCore = global.core;
    global.core = { debug: vi.fn(), info: vi.fn(), warning: vi.fn() };
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

  it("uses flat digest maps with separate references and errors, not record envelopes", () => {
    const file = path.join(workspace, ".github/workflows/main.lock.yml");
    const compiled = { ...record({}), references: { "/model": "/metadata/agent_model" } };
    const manifest = fields([compiled]);
    expect(manifest.agent_metadata).toEqual({});
    expect(manifest.metadata_refs).toEqual({ [`artifact:${compiled.sha256.slice(0, 20)}`]: compiled.references });
    expect(manifest).not.toHaveProperty("markdown_metadata");
    expect(JSON.stringify(manifest)).not.toContain('"kind":');
    fs.writeFileSync(file, `# gh-aw-metadata: {"agent_model":"small"}\n# gh-aw-manifest: ${JSON.stringify(manifest)}\n# generated\n`);
    metadata.installMarkdownMetadata(metadata.readLocalMarkdownMetadata(file, workspace));
    expect(metadata.readMarkdownMetadata("agent", document).value).toEqual({ model: "small" });
    fs.writeFileSync(file, headers([{ ...record(null), error: "invalid authored metadata" }]));
    metadata.installMarkdownMetadata(metadata.readLocalMarkdownMetadata(file, workspace));
    expect(() => metadata.readMarkdownMetadata("agent", document)).toThrow("invalid authored metadata");
  });

  it("rejects retired model-pin projection records", () => {
    const retired = { kind: "model-pins", sha256: "b".repeat(64), value: { model: "small" } };
    expect(() => metadata.installMarkdownMetadata(catalog([retired]))).toThrow(/Invalid or duplicate/);
    expect(() => metadata.metadataManifestFields(catalog([retired]))).toThrow(/Invalid compiler Markdown metadata source hash/);
  });

  it.each(["metadata_refs", "metadata_errors"])("rejects invalid %s source keys", field => {
    const file = path.join(workspace, ".github/workflows/main.lock.yml");
    fs.writeFileSync(file, `# gh-aw-metadata: {}\n# gh-aw-manifest: ${JSON.stringify({ [field]: { "artifact:invalid": field === "metadata_refs" ? {} : "invalid" } })}\n# generated\n`);
    expect(() => metadata.readLocalMarkdownMetadata(file, workspace)).toThrow(/source hash/);
  });

  it("omits empty records and defaults absent source metadata to empty across fresh processes", () => {
    const emptySource = "description: Private reader";
    const emptyDocument = `---\n${emptySource}\n---\nRead files.`;
    const sha256 = crypto.createHash("sha256").update(emptySource).digest("hex");
    const records = [{ kind: "artifact", sha256, value: {} }];
    const manifest = fields(records);
    expect(manifest.agent_metadata).toEqual({});
    expect(manifest).not.toHaveProperty("model_pins");
    expect(manifest).not.toHaveProperty("metadata_empty");
    expect(JSON.stringify(manifest)).not.toContain(sha256);
    const file = path.join(workspace, ".github/workflows/main.lock.yml");
    fs.writeFileSync(file, headers(records));
    metadata.installMarkdownMetadata(metadata.readLocalMarkdownMetadata(file, workspace));
    expect(metadata.readMarkdownMetadata("agent", emptyDocument).value).toEqual({});
    expect(metadata.readMarkdownMetadata("agent", emptyDocument.replace("Private", "Unknown")).value).toEqual({});
    fs.writeFileSync(path.join(workspace, "aw_info.json"), JSON.stringify({ run_id: 123 }));
    metadata.persistMarkdownMetadata();
    const restored = execFileSync(
      process.execPath,
      ["-e", 'const metadata = require(process.argv[1]); process.stdout.write(JSON.stringify(metadata.readMarkdownMetadata("agent", process.argv[2]).value));', require.resolve("./markdown_metadata.cjs"), emptyDocument],
      { env: { ...process.env, GH_AW_TMP_DIR: workspace }, encoding: "utf8" }
    );
    expect(JSON.parse(restored)).toEqual({});
    metadata.installMarkdownMetadata(catalog([record({ model: "small" })]));
    metadata.persistMarkdownMetadata();
    expect(JSON.parse(fs.readFileSync(path.join(workspace, "aw_info.json"), "utf8"))).not.toHaveProperty("metadata_empty");
  });

  it.each(["invalid", "A".repeat(20), "a".repeat(19), "a".repeat(65)])("rejects invalid source hash keys: %s", hash => {
    const file = path.join(workspace, ".github/workflows/main.lock.yml");
    fs.writeFileSync(file, `# gh-aw-metadata: {}\n# gh-aw-manifest: ${JSON.stringify({ agent_metadata: { [hash]: { empty: true } } })}\n# generated\n`);
    expect(() => metadata.readLocalMarkdownMetadata(file, workspace)).toThrow(/source hash/);
  });

  it.each(["reference", "error"])("rejects conflicting empty source approval with %s", scenario => {
    const compiled = record({});
    const hash = compiled.sha256.slice(0, 20);
    const key = `artifact:${hash}`;
    const manifest = { agent_metadata: { [hash]: { empty: true } } };
    if (scenario === "reference") manifest.metadata_refs = { [key]: { "/model": "/metadata/agent_model" } };
    if (scenario === "error") manifest.metadata_errors = { [key]: "invalid source" };
    const file = path.join(workspace, ".github/workflows/main.lock.yml");
    fs.writeFileSync(file, `# gh-aw-metadata: {"agent_model":"small"}\n# gh-aw-manifest: ${JSON.stringify(manifest)}\n# generated\n`);
    expect(() => metadata.readLocalMarkdownMetadata(file, workspace)).toThrow("Conflicting empty compiler metadata");
  });

  it.each(["full", "short"])("matches computed full source digests against %s manifest keys", form => {
    const compiled = record({ model: "small" });
    const sha256 = form === "full" ? compiled.sha256 : compiled.sha256.slice(0, 20);
    metadata.installMarkdownMetadata(catalog([{ ...compiled, sha256 }]));
    expect(metadata.readMarkdownMetadata("agent", document).value).toEqual({ model: "small" });
    expect(metadata.readMarkdownMetadata("agent", document.replace("small", "forged")).value).toEqual({});
  });

  it("extends colliding source prefixes and coalesces consistent full/short aliases", () => {
    const prefix = "a".repeat(20);
    const first = prefix + "b" + "c".repeat(43),
      second = prefix + "c" + "b".repeat(43);
    expect(
      Object.keys(
        fields([
          { kind: "artifact", sha256: first, value: { model: "a" } },
          { kind: "artifact", sha256: second, value: { model: "b" } },
        ]).agent_metadata
      )
    ).toEqual([first.slice(0, 21), second.slice(0, 21)]);
    const compiled = record({ model: "small" });
    metadata.installMarkdownMetadata(catalog([compiled]));
    metadata.installMarkdownMetadata(catalog([{ ...compiled, sha256: compiled.sha256.slice(0, 20) }]), true);
    expect(metadata.currentMarkdownMetadata().records).toEqual([compiled]);
    expect(() => metadata.installMarkdownMetadata(catalog([{ ...compiled, sha256: compiled.sha256.slice(0, 20), value: { model: "forged" } }]), true)).toThrow(/Conflicting/);
    expect(metadata.readMarkdownMetadata("agent", document).value.model).toBe("small");
  });

  it("prunes null value entries while preserving root references and compiler errors", () => {
    const file = path.join(workspace, ".github/workflows/main.lock.yml");
    const compiled = { ...record(null), references: { "": "/metadata/template" } };
    const rejected = { ...record(null), sha256: "b".repeat(64), error: "invalid authored metadata" };
    const manifest = fields([compiled, rejected]);
    expect(manifest.agent_metadata).toEqual({});
    expect(JSON.stringify(manifest)).not.toContain(":null");
    fs.writeFileSync(file, `# gh-aw-metadata: {"template":{"model":"small"}}\n# gh-aw-manifest: ${JSON.stringify(manifest)}\n# generated\n`);
    metadata.installMarkdownMetadata(metadata.readLocalMarkdownMetadata(file, workspace));
    expect(metadata.readMarkdownMetadata("agent", document).value).toEqual({ model: "small" });
    expect(metadata.currentMarkdownMetadata().records).toContainEqual({ ...rejected, sha256: rejected.sha256.slice(0, 20) });
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
    const failure = Object.assign(new Error("Forbidden"), { status: 403, response: { status: 403, headers: {} } });
    const githubClient = { rest: { repos: { getContent: vi.fn().mockRejectedValue(failure) } } };
    await expect(metadata.loadMarkdownMetadata(`owner/repo/${file}@main`, workspace, { revision, githubClient })).rejects.toBe(failure);
    expect(githubClient.rest.repos.getContent).toHaveBeenCalledTimes(1);
    expect(metadata.readMarkdownMetadata("agent", document).value).toEqual({});
  });

  const quotaError = (resetSeconds, extra = {}) =>
    Object.assign(new Error("API rate limit exceeded for installation"), {
      status: 403,
      response: {
        status: 403,
        headers: { "x-ratelimit-remaining": "0", "x-ratelimit-limit": "15000", "x-ratelimit-used": "15000", "x-ratelimit-resource": "core", ...(resetSeconds ? { "x-ratelimit-reset": String(resetSeconds) } : {}), ...extra },
      },
    });
  const fakeClock = start => {
    let current = start;
    vi.spyOn(Date, "now").mockImplementation(() => current);
    return {
      now: () => current,
      sleep: vi.fn(async ms => {
        current += ms;
      }),
    };
  };

  it("resumes remote metadata reads only after the failed response's quota reset", async () => {
    const file = ".github/workflows/main.lock.yml";
    const start = Date.parse("2026-11-09T23:58:53Z");
    const clock = fakeClock(start);
    const reset = Date.parse("2026-11-10T00:00:04Z");
    const requestTimes = [];
    const githubClient = {
      rest: {
        repos: {
          getContent: vi.fn(async () => {
            requestTimes.push(clock.now());
            if (requestTimes.length === 1) throw quotaError(reset / 1000);
            return remoteFile(file, [record({ model: "small" })]);
          }),
        },
      },
    };
    const result = await metadata.loadMarkdownMetadata(`owner/repo/${file}@main`, workspace, { revision, githubClient, quotaWait: clock });
    expect(githubClient.rest.repos.getContent).toHaveBeenCalledTimes(2);
    expect(clock.sleep).toHaveBeenCalledExactlyOnceWith(reset + 1000 - start);
    expect(requestTimes[1]).toBeGreaterThanOrEqual(reset);
    expect(result.records).toEqual(decodedCatalog([record({ model: "small" })]).records);
    expect(metadata.readMarkdownMetadata("agent", document).value).toEqual({ model: "small" });
  });

  it.each([
    ["reset beyond the bounded wait", 3600, "reset_exceeds_budget", 0, 1],
    ["unknown reset", 0, "unknown_reset", 0, 1],
    ["renewed exhaustion", 60, "renewed_exhaustion", 1, 2],
  ])("blocks activation with an explicit quota error on %s without an empty or checkout fallback", async (_case, resetOffset, reason, sleeps, requests) => {
    const file = ".github/workflows/main.lock.yml";
    fs.writeFileSync(path.join(workspace, file), headers([record({ model: "forged" })]));
    const start = Date.parse("2026-11-09T23:58:53Z");
    const clock = fakeClock(start);
    const githubClient = { rest: { repos: { getContent: vi.fn(async () => Promise.reject(quotaError(resetOffset ? Math.floor(clock.now() / 1000) + resetOffset : 0))) } } };
    const failure = metadata.loadMarkdownMetadata(`owner/repo/${file}@main`, workspace, { revision, githubClient, quotaWait: { ...clock, maxWaitMs: 10 * 60 * 1000 } });
    await expect(failure).rejects.toMatchObject({ code: "GH_AW_GITHUB_API_QUOTA_EXHAUSTED", reason, quota: { status: 403, resource: "core", limit: 15000, remaining: 0, used: 15000 } });
    await expect(failure).rejects.toThrow("GitHub API quota exhausted");
    expect(clock.sleep).toHaveBeenCalledTimes(sleeps);
    expect(githubClient.rest.repos.getContent).toHaveBeenCalledTimes(requests);
    expect(metadata.readMarkdownMetadata("agent", document).value).toEqual({});
  });

  it("keeps the bounded retry for rate limits without quota exhaustion metadata", async () => {
    const file = ".github/workflows/main.lock.yml";
    const secondary = Object.assign(new Error("You have exceeded a secondary rate limit. API rate limit exceeded"), { status: 403, response: { status: 403, headers: {} } });
    const githubClient = {
      rest: {
        repos: {
          getContent: vi
            .fn()
            .mockRejectedValueOnce(secondary)
            .mockResolvedValue(remoteFile(file, [record({ model: "small" })])),
        },
      },
    };
    vi.useFakeTimers();
    try {
      const request = metadata.loadMarkdownMetadata(`owner/repo/${file}@main`, workspace, { revision, githubClient });
      await vi.runAllTimersAsync();
      await request;
      expect(githubClient.rest.repos.getContent).toHaveBeenCalledTimes(2);
    } finally {
      vi.useRealTimers();
    }
  });

  it("reads the exact executing revision from the local git object store with zero Contents API requests", async () => {
    const file = ".github/workflows/main.lock.yml";
    const git = (...args) =>
      execFileSync("git", ["-C", workspace, ...args], { stdio: ["ignore", "pipe", "ignore"] })
        .toString()
        .trim();
    git("init", "-q");
    fs.writeFileSync(path.join(workspace, file), headers([record({ model: "small", tools: ["Read"] })]));
    git("add", file);
    git("-c", "user.name=test", "-c", "user.email=test@example.invalid", "commit", "-q", "-m", "lock");
    const executing = git("rev-parse", "HEAD");
    // A replaced worktree file must never be used as the executing lock.
    fs.writeFileSync(path.join(workspace, file), headers([record({ model: "forged" })]));
    const githubClient = { rest: { repos: { getContent: vi.fn().mockRejectedValue(quotaError(0)) } } };
    await metadata.loadMarkdownMetadata(`owner/repo/${file}@refs/heads/main`, workspace, { repository: "owner/repo", revision: executing, githubClient });
    expect(githubClient.rest.repos.getContent).not.toHaveBeenCalled();
    expect(metadata.readMarkdownMetadata("agent", document).value).toEqual({ model: "small", tools: ["Read"] });
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
    expect(JSON.parse(fs.readFileSync(path.join(workspace, "aw_info.json"), "utf8"))).toEqual({ run_id: 123, ...metadata.metadataManifestFields(result) });
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
    const header = `# gh-aw-metadata: ${JSON.stringify({ agent_model: "small" })}\n# gh-aw-manifest: ${JSON.stringify({ version: 1, declared_tools: ["Read"], ...fields([compiled]) })}\n# generated\n`;
    fs.writeFileSync(path.join(workspace, file), header);
    const result = await metadata.loadMarkdownMetadata(`owner/repo/${file}@main`, workspace);
    expect(result).toEqual(decodedCatalog([record(value)]));
    expect(metadata.readMarkdownMetadata("agent", document).value).toEqual(value);
    expect(header.match(/"small"/g)).toHaveLength(1);
    expect(header.match(/"Read"/g)).toHaveLength(1);
    expect(header).not.toContain("value_json");
    expect(metadata.readMarkdownMetadata("agent", document.replace("small", "forged")).value).toEqual({});
    fs.writeFileSync(path.join(workspace, "aw_info.json"), '{"run_id":123}');
    metadata.persistMarkdownMetadata();
    expect(JSON.parse(fs.readFileSync(path.join(workspace, "aw_info.json"), "utf8"))).toEqual({ run_id: 123, ...metadata.metadataManifestFields(result) });
  });

  it.each([
    { "/model": "/metadata/missing" },
    { "/model": "/manifest/agent_metadata/source/model" },
    { "/model": "/metadata/agent_model", "/model/nested": "/metadata/agent_model" },
    { "/model": "/metadata/agent~2model" },
    { "/description": "/metadata/agent_model" },
    { "/model/nested": "/metadata/agent_model" },
  ])("rejects missing, recursive, overlapping, malformed, and conflicting references (%j)", references => {
    const file = path.join(workspace, ".github/workflows/main.lock.yml");
    const compiled = { ...record({ description: "Private reader" }), references };
    fs.writeFileSync(file, `# gh-aw-metadata: {"agent_model":"small"}\n# gh-aw-manifest: ${JSON.stringify(fields([compiled]))}\n# generated\n`);
    expect(() => metadata.readLocalMarkdownMetadata(file, workspace)).toThrow(/metadata.*reference/);
  });

  it("resolves escaped object names, array slots, and whole values without adding parser dependencies", () => {
    const file = path.join(workspace, ".github/workflows/main.lock.yml");
    const compiled = [
      { ...record({ "x/y~z": [null, "unique"] }), references: { "/x~1y~0z/0": "/manifest/a~1b~0c/0" } },
      { ...record(null), sha256: "b".repeat(64), references: { "": "/manifest/a~1b~0c" } },
    ];
    fs.writeFileSync(file, `# gh-aw-metadata: {}\n# gh-aw-manifest: ${JSON.stringify({ "a/b~c": ["model"], ...fields(compiled) })}\n# generated\n`);
    expect(metadata.readLocalMarkdownMetadata(file, workspace).records.map(record => record.value)).toEqual([{ "x/y~z": ["model", "unique"] }, ["model"]]);
    expect(() => metadata.installMarkdownMetadata(catalog(compiled))).toThrow(/metadata record/);
  });

  it("restores references to booleans, integers, null, and empty values through installation and caching", async () => {
    const file = ".github/workflows/main.lock.yml";
    const value = { enabled: true, disabled: false, count: 1, empty: "", object: {}, array: [], null: null };
    const references = Object.fromEntries(Object.keys(value).map(key => [`/${key}`, `/metadata/${key}`]));
    const compiled = { ...record({}), references };
    fs.writeFileSync(path.join(workspace, file), `# gh-aw-metadata: ${JSON.stringify(value)}\n# gh-aw-manifest: ${JSON.stringify({ version: 1, ...fields([compiled]) })}\n# generated\n`);
    const result = await metadata.loadMarkdownMetadata(`owner/repo/${file}@main`, workspace);
    expect(result).toEqual(decodedCatalog([record(value)]));
    expect(metadata.readMarkdownMetadata("agent", document).value).toEqual(value);
    fs.writeFileSync(path.join(workspace, "aw_info.json"), '{"run_id":123}');
    metadata.persistMarkdownMetadata();
    expect(JSON.parse(fs.readFileSync(path.join(workspace, "aw_info.json"), "utf8"))).toEqual({ run_id: 123, ...metadata.metadataManifestFields(result) });
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

  it("leaves unavailable source metadata empty after searching compiler manifests", async () => {
    const githubClient = { rest: { repos: { getContent: vi.fn(async () => ({ data: [] })) } } };
    await expect(metadata.ensureRemoteMarkdownMetadata([document], githubClient, { repository: "owner/skills", ref: revision })).resolves.toBeUndefined();
    expect(metadata.readMarkdownMetadata("agent", document).value).toEqual({});
    expect(metadata.currentMarkdownMetadata().records).toEqual([]);
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
    fs.writeFileSync(file, '# gh-aw-metadata: {}\n# gh-aw-manifest: {"agent_metadata":[]}\n# generated\n');
    expect(() => metadata.ensureCheckoutMarkdownMetadata([document], workspace)).toThrow(/Invalid compiler metadata manifest/);
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
    expect(Object.keys(info.agent_metadata)).toHaveLength(2);
    expect(info).not.toHaveProperty("markdown_metadata");
  });

  it("imports uncompiled inline agents with empty settings without requiring a metadata cache", async () => {
    const body = '---\nengine: copilot\n---\n## agent: reader\n---\n{"model":"uncompiled-model","tools":["Write"]}\n---\nBody\n## end agent: reader\n';
    fs.writeFileSync(path.join(workspace, ".github/workflows/uncompiled.md"), body);
    const result = await require("./runtime_import.cjs").processRuntimeImport(".github/workflows/uncompiled.md", false, workspace);
    expect(result).toContain("## agent: reader\n---\n{}\n---\nBody");
    expect(result).not.toContain("uncompiled-model");
    expect(result).not.toContain("Write");
    expect(fs.existsSync(path.join(workspace, "aw_info.json"))).toBe(false);
  });
});

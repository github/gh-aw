const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const vm = require("node:vm");
const { EventEmitter } = require("node:events");
const { execFileSync } = require("node:child_process");
const { getPatchDiffSizeBytes } = require("../../actions/setup/js/git_patch_utils.cjs");

const workflow = fs.readFileSync(path.join(__dirname, "../workflows/daily-arxiv-researcher.md"), "utf8");
const marker = "      script: |\n";
const start = workflow.indexOf(marker);
assert.notEqual(start, -1, "Workflow must contain the inline JavaScript producer");
const lines = workflow.slice(start + marker.length).split("\n");
const end = lines.findIndex(line => line.trim() && !line.startsWith("        "));
const source = lines
  .slice(0, end)
  .map(line => line.slice(8))
  .join("\n");
const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;
const fetchPapers = new AsyncFunction("require", "core", "Buffer", source);
const jsonText = value => JSON.stringify(value, null, 2) + "\n";

function paper(number, title = "Paper title") {
  const id = `2610.${String(number).padStart(5, "0")}v1`;
  return { id, title, published: "2026-10-27", categories: ["cs.AI", "cs.SE"], url: `https://arxiv.org/abs/${id}` };
}

function fixture(t) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "arxiv-memory-"));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const memory = path.join(root, "repo-memory/default");
  const data = path.join(root, "agent/arxiv");
  const cache = path.join(root, "cache-memory");
  const local = file => {
    assert.ok(file.startsWith("/tmp/gh-aw/"), `Unexpected fixture path: ${file}`);
    return path.join(root, file.slice("/tmp/gh-aw/".length));
  };
  const fixtureFs = {};
  for (const method of ["existsSync", "mkdirSync", "readdirSync", "readFileSync", "writeFileSync"]) {
    fixtureFs[method] = (file, ...args) => fs[method](local(file), ...args);
  }
  const writeJSON = (file, value) => fs.writeFileSync(file, jsonText(value));
  const readJSON = file => JSON.parse(fs.readFileSync(file, "utf8"));
  const snapshot = () =>
    Object.fromEntries(
      fs.existsSync(memory)
        ? fs
            .readdirSync(memory, { withFileTypes: true })
            .filter(entry => entry.isFile())
            .map(entry => [entry.name, fs.readFileSync(path.join(memory, entry.name), "utf8")])
        : []
    );
  const ids = () =>
    new Set(
      Object.entries(snapshot())
        .filter(([name]) => /^paper-index(?:-\d{4,})?\.json$/.test(name))
        .flatMap(([, text]) => JSON.parse(text).papers.map(record => record.id))
    );

  async function prepare(papers, relevant = false) {
    const escape = value => value.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
    const xml = `<feed>${papers
      .map(p => `<entry><id>${p.url}</id><title>${escape(p.title)}</title>` + `<published>${p.published}</published><summary>Abstract</summary>` + p.categories.map(category => `<category term="${category}"/>`).join("") + "</entry>")
      .join("")}</feed>`;
    const https = {
      get(url, options, callback) {
        assert.ok(url.startsWith("https://export.arxiv.org/api/query?"));
        assert.equal(options.timeout, 30000);
        const request = new EventEmitter();
        queueMicrotask(() => {
          const response = new EventEmitter();
          callback(response);
          response.emit("data", Buffer.from(xml));
          response.emit("end");
        });
        return request;
      },
    };
    await fetchPapers(
      name => {
        if (name === "fs") return fixtureFs;
        assert.equal(name, "https");
        return https;
      },
      {
        info() {},
        warning(message) {
          assert.fail(message);
        },
      },
      Buffer
    );
    const newPapers = readJSON(path.join(data, "new-papers.json")).papers;
    writeJSON(path.join(data, "screened-papers.json"), {
      papers: newPapers.map(p => ({ id: p.id, relevant })),
    });
  }

  function update() {
    // Execute the exact updater emitted from the workflow, with no network or ambient filesystem access.
    vm.runInNewContext(
      fs.readFileSync(path.join(data, "update-memory.cjs"), "utf8"),
      {
        require(name) {
          assert.equal(name, "fs");
          return fixtureFs;
        },
        Buffer,
      },
      { timeout: 5000 }
    );
  }

  function rejectsWithoutWrites(error) {
    const before = snapshot();
    const beforeCache = fs.readFileSync(path.join(cache, "seen-paper-ids.json"), "utf8");
    assert.throws(update, error);
    assert.deepEqual(snapshot(), before);
    assert.equal(fs.readFileSync(path.join(cache, "seen-paper-ids.json"), "utf8"), beforeCache);
  }
  return { memory, data, cache, prepare, update, snapshot, ids, readJSON, writeJSON, rejectsWithoutWrites };
}

test("runtime JavaScript stays inline in Markdown and the prompt runs the emitted updater", () => {
  assert.match(workflow, /function updatePaperMemory\(\)/);
  assert.match(workflow, /node \/tmp\/gh-aw\/agent\/arxiv\/update-memory\.cjs/);
  assert.doesNotMatch(workflow, /python3|arxiv_memory\.py|child_process/);
});

test("first run records every irrelevant paper", async t => {
  const f = fixture(t);
  await f.prepare([paper(1), paper(2)]);
  f.update();
  const records = f.readJSON(path.join(f.memory, "paper-index-0001.json")).papers;
  assert.deepEqual(
    records.map(p => p.id),
    [paper(1).id, paper(2).id]
  );
  assert.ok(records.every(p => p.relevant === false));
  const ledger = f.snapshot()["paper-ledger-0001.md"];
  assert.equal(ledger.match(/- \*\*Relevant\*\*: No/g).length, 2);
  assert.doesNotMatch(ledger, /- \*\*Opportunity\*\*:/);
  assert.equal(f.readJSON(path.join(f.cache, "seen-paper-ids.json")).ids.length, 2);
});

test("legacy history remains byte-identical across the original failing size boundary", async t => {
  const f = fixture(t);
  fs.mkdirSync(f.memory, { recursive: true });
  fs.writeFileSync(path.join(f.memory, "paper-ledger.md"), "# Legacy table and block history\n" + "x".repeat(96000));
  f.writeJSON(path.join(f.memory, "paper-index.json"), { papers: [{ id: "old", published: "2026-07-30" }] });
  const legacy = f.snapshot();
  await f.prepare(
    Array.from({ length: 25 }, (_, i) => paper(i, "A realistic title ".repeat(8))),
    true
  );
  const results = f.readJSON(path.join(f.data, "screened-papers.json"));
  Object.assign(results.papers[0], { opportunity: "Improve typed safe outputs", area: "safe-outputs" });
  f.writeJSON(path.join(f.data, "screened-papers.json"), results);
  f.update();
  const after = f.snapshot();
  for (const [name, content] of Object.entries(legacy)) assert.equal(after[name], content);
  assert.equal(f.ids().size, 26);
  assert.match(after["paper-ledger-0001.md"], /Improve typed safe outputs/);
  assert.ok(Buffer.byteLength(legacy["paper-ledger.md"] + after["paper-ledger-0001.md"]) > 102400);
  assert.ok(Buffer.byteLength(after["paper-ledger-0001.md"] + after["paper-index-0001.json"]) < 51200);
});

test("expired or ahead-of-push caches reconcile against every durable index", async t => {
  const f = fixture(t);
  await f.prepare([paper(1)]);
  f.update();
  f.writeJSON(path.join(f.memory, "paper-index.json"), { papers: [{ id: paper(3).id }] });
  f.writeJSON(path.join(f.cache, "seen-paper-ids.json"), { ids: [paper(2).id] });
  await f.prepare([paper(1), paper(2), paper(3)]);
  assert.deepEqual(
    f.readJSON(path.join(f.data, "new-papers.json")).papers.map(p => p.id),
    [paper(2).id]
  );
  fs.unlinkSync(path.join(f.cache, "seen-paper-ids.json"));
  await f.prepare([paper(1), paper(2), paper(3)]);
  assert.equal(f.readJSON(path.join(f.data, "new-papers.json")).new_count, 1);
});

test("repeated updates and no-new-paper runs are idempotent", async t => {
  const f = fixture(t);
  await f.prepare([paper(1)]);
  f.update();
  const before = f.snapshot();
  f.update();
  assert.deepEqual(f.snapshot(), before);
  await f.prepare([paper(1)]);
  assert.equal(f.readJSON(path.join(f.data, "new-papers.json")).new_count, 0);
  f.update();
  assert.deepEqual(f.snapshot(), before);
});

test("continued Unicode growth rotates either shard without losing history or exceeding the real patch budget", async t => {
  const f = fixture(t);
  const sealed = new Map();
  for (let batch = 0; batch < 12; batch++) {
    await f.prepare(Array.from({ length: 25 }, (_, i) => paper(batch * 25 + i, "Unicode \u7814\u7a76 \u{1f52c} ".repeat(25))));
    f.update();
    const snapshot = f.snapshot();
    for (const [name, content] of sealed) assert.equal(snapshot[name], content);
    const indices = Object.keys(snapshot)
      .filter(name => /^paper-index-\d+\.json$/.test(name))
      .sort();
    for (const index of indices.slice(0, -1)) {
      const ledger = index.replace("index", "ledger").replace(".json", ".md");
      sealed.set(index, snapshot[index]);
      sealed.set(ledger, snapshot[ledger]);
    }
    for (const content of Object.values(snapshot)) assert.ok(Buffer.byteLength(content) <= 81920);
  }
  assert.ok(sealed.size >= 4);
  assert.deepEqual(f.ids(), new Set(Array.from({ length: 300 }, (_, i) => paper(i).id)));
  const git = args => execFileSync("git", args, { cwd: f.memory, encoding: "utf8" });
  git(["init", "--quiet"]);
  git(["add", "."]);
  git(["-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "--quiet", "-m", "Fixture"]);
  await f.prepare(Array.from({ length: 25 }, (_, i) => paper(300 + i)));
  f.update();
  for (const [name, content] of Object.entries(f.snapshot()).filter(([name]) => name.endsWith(".json"))) {
    const formatted = jsonText(JSON.parse(content));
    assert.equal(content, formatted);
    assert.ok(Buffer.byteLength(formatted) <= 81920);
  }
  git(["add", "."]);
  assert.ok(getPatchDiffSizeBytes(git(["diff", "--cached", "--no-renames"])) < 51200);
  assert.equal(f.ids().size, 325);
});

test("a full JSON index rotates even when its Markdown ledger still has room", async t => {
  const f = fixture(t);
  await f.prepare([paper(1)]);
  f.update();
  const file = path.join(f.memory, "paper-index-0001.json");
  const history = f.readJSON(file);
  history.papers[0].padding = "x".repeat(81000);
  f.writeJSON(file, history);
  const before = fs.readFileSync(file, "utf8");
  await f.prepare([paper(2, "Long title ".repeat(100))]);
  f.update();
  assert.equal(fs.readFileSync(file, "utf8"), before);
  assert.ok(fs.existsSync(path.join(f.memory, "paper-index-0002.json")));
});

test("invalid screening results fail before any memory or cache writes", async t => {
  const f = fixture(t);
  await f.prepare([paper(1)]);
  for (const [papers, error] of [
    [[], /each new paper/],
    [
      [
        { id: paper(1).id, relevant: false },
        { id: paper(1).id, relevant: false },
      ],
      /each new paper/,
    ],
    [[{ id: "unknown", relevant: false }], /each new paper/],
    [[{ id: paper(1).id, relevant: "false" }], /boolean/],
    [[{ id: paper(1).id, relevant: true, opportunity: [] }], /string/],
  ]) {
    f.writeJSON(path.join(f.data, "screened-papers.json"), { papers });
    f.rejectsWithoutWrites(error);
  }
});

test("missing paper metadata fails rather than serializing an incomplete record", async t => {
  const f = fixture(t);
  await f.prepare([paper(1)]);
  const data = f.readJSON(path.join(f.data, "new-papers.json"));
  delete data.papers[0].title;
  f.writeJSON(path.join(f.data, "new-papers.json"), data);
  f.rejectsWithoutWrites(/metadata requires/);
});

test("an oversized record cannot bypass the UTF-8 shard limit", async t => {
  const f = fixture(t);
  await f.prepare([paper(1, "\u7814".repeat(40000))]);
  f.rejectsWithoutWrites(/80 KiB/);
});

test("the unchanged 50 KiB daily patch budget is enforced before writing", async t => {
  const f = fixture(t);
  await f.prepare(Array.from({ length: 25 }, (_, i) => paper(i, "x".repeat(1500))));
  f.rejectsWithoutWrites(/50 KiB/);
});

test("the unchanged 100-file budget fails explicitly without dropping history", async t => {
  const f = fixture(t);
  fs.mkdirSync(f.memory, { recursive: true });
  for (let i = 0; i < 99; i++) fs.writeFileSync(path.join(f.memory, `archive-${i}.md`), "Preserved history\n");
  await f.prepare([paper(1)]);
  f.rejectsWithoutWrites(/100-file/);
});

test("a malformed durable index is not silently discarded", async t => {
  const f = fixture(t);
  fs.mkdirSync(f.memory, { recursive: true });
  fs.writeFileSync(path.join(f.memory, "paper-index.json"), "broken JSON");
  await assert.rejects(() => f.prepare([paper(1)]), /JSON/);
});

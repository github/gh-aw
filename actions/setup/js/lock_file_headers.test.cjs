const { Readable } = require("node:stream");
const { readLockFileHeaders, parseLockFileHeaders } = require("./lock_file_headers.cjs");

describe("shared JSON lock headers", () => {
  it("preserves the work-queue reader interface and ignores workflow YAML", async () => {
    expect(require("./work_queue_provisioning.cjs").readLockFileHeaders).toBe(readLockFileHeaders);
    const content = '# gh-aw-metadata: {"name":"worker"}\n# gh-aw-manifest: {"version":1}\n# generated\ninvalid: [yaml\n';
    expect(await readLockFileHeaders(Readable.from([content]))).toEqual(parseLockFileHeaders(content.split("\n")));
  });
  it("retains duplicate invalidation, strict JSON checks, and the first-three-lines boundary", () => {
    expect(parseLockFileHeaders(['# gh-aw-manifest: {"version":1}', '# gh-aw-manifest: {"version":1}', "# generated"]).manifest).toBeUndefined();
    expect(parseLockFileHeaders(['# gh-aw-manifest: {"version":1,"\\u0076ersion":2}']).manifest).toBeUndefined();
    expect(parseLockFileHeaders(["# first", "# second", "# third", '# gh-aw-manifest: {"version":1}']).manifest).toBeUndefined();
    expect(parseLockFileHeaders(['# gh-aw-manifest: {"rate":0.25}']).manifest).toBeUndefined();
  });
  it("accepts finite fractional catalog values without weakening inventory or queue numbers", () => {
    const manifest = { version: 1, agent_metadata: { source: { rate: 0.25 } } };
    expect(parseLockFileHeaders([`# gh-aw-manifest: ${JSON.stringify(manifest)}`]).manifest).toEqual(manifest);
    expect(parseLockFileHeaders(['# gh-aw-manifest: {"agent_metadata":{"source":{"rate":1e999}}}']).manifest).toBeUndefined();
    expect(parseLockFileHeaders(['# gh-aw-manifest: {"agent_metadata":{"source":{"rate":0.25,"rate":0.5}}}']).manifest).toBeUndefined();
    expect(() => require("./work_queue_codec.cjs").parseStrictJSON('{"rate":0.25}')).toThrow();
    expect(parseLockFileHeaders(['# gh-aw-manifest: {"version":1e0,"agent_metadata":{"rate":0.25}}']).manifest).toBeUndefined();
    expect(parseLockFileHeaders(['# gh-aw-manifest: {"version":1,"inventory":{"agent_metadata":{"rate":0.25}}}']).manifest).toBeUndefined();
  });
});

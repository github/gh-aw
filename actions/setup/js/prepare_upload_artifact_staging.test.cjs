// @ts-check
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createRequire } from "node:module";
import { createHash } from "node:crypto";

const { prepareTransferDirectory, resolveRequestFiles, resolveRuntimePolicyValues } = createRequire(import.meta.url)("./prepare_upload_artifact_staging.cjs");
const { claimArtifactPath } = createRequire(import.meta.url)("./work_queue_claim_scope.cjs");

const claimAssignment = {
  dispatch_id: "dispatch-1",
  claims: [{ handle: "claim-1", claim_id: "claim-id-1", work_id: "work-1", work: { plan: "task" }, result_refs: [] }],
};

function stagedConnectorFile(root, content, extension = ".pdf") {
  const filename = `${createHash("sha256").update(content).digest("hex")}${extension}`;
  fs.writeFileSync(path.join(root, filename), content);
  return filename;
}

describe("prepare_upload_artifact_staging", () => {
  let root;
  let stagingRoot;
  let transferRoot;

  beforeEach(() => {
    root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-artifact-transfer-"));
    stagingRoot = path.join(root, "agent-staging");
    transferRoot = path.join(root, "transfer");
    fs.mkdirSync(path.join(stagingRoot, "reports"), { recursive: true });
    fs.writeFileSync(path.join(stagingRoot, "reports", "public.txt"), "requested and allowed");
    fs.writeFileSync(path.join(stagingRoot, "reports", "private.txt"), "excluded by policy");
    fs.writeFileSync(path.join(stagingRoot, "unrequested.txt"), "not selected");
  });

  afterEach(() => {
    fs.rmSync(root, { recursive: true, force: true });
  });

  it("exports only requested files that match allowed-paths", () => {
    const result = prepareTransferDirectory({
      items: [{ type: "upload_artifact", path: "reports" }],
      config: { upload_artifact: { "allowed-paths": ["reports/public.txt"] } },
      stagingRoot,
      transferRoot,
    });

    expect(result.files).toEqual(["reports/public.txt"]);
    expect(fs.readFileSync(path.join(transferRoot, "reports", "public.txt"), "utf8")).toBe("requested and allowed");
    expect(fs.existsSync(path.join(transferRoot, "reports", "private.txt"))).toBe(false);
    expect(fs.existsSync(path.join(transferRoot, "unrequested.txt"))).toBe(false);
  });

  it("keeps configured exclusions when the request supplies an empty exclude list", () => {
    const result = prepareTransferDirectory({
      items: [{ type: "upload_artifact", filters: { include: ["reports/*.txt"], exclude: [] } }],
      config: {
        upload_artifact: {
          "filters-exclude": ["reports/private.txt"],
        },
      },
      stagingRoot,
      transferRoot,
    });

    expect(result.files).toEqual(["reports/public.txt"]);
    expect(fs.existsSync(path.join(transferRoot, "reports", "private.txt"))).toBe(false);
  });

  it("intersects request and configured include filters", () => {
    const result = prepareTransferDirectory({
      items: [{ type: "upload_artifact", filters: { include: ["reports/private.txt"] } }],
      config: {
        upload_artifact: {
          "filters-include": ["reports/public.txt"],
        },
      },
      stagingRoot,
      transferRoot,
    });

    expect(result.files).toEqual([]);
  });

  it("preserves declared connector file inputs but not unrelated staging files", () => {
    const result = prepareTransferDirectory({
      items: [{ type: "azure_devops_upload_attachment", file: "report-hash.pdf" }],
      config: {
        azure_devops_upload_attachment: {
          staged_files: { file: { maxBytes: 1024 } },
        },
      },
      stagingRoot,
      transferRoot,
    });

    expect(result.files).toEqual([]);
    const content = Buffer.from("connector attachment");
    const filename = stagedConnectorFile(stagingRoot, content);
    const updated = prepareTransferDirectory({
      items: [{ type: "azure_devops_upload_attachment", file: filename }],
      config: {
        azure_devops_upload_attachment: {
          staged_files: { file: { maxBytes: 1024, extensions: [".pdf"] } },
        },
      },
      stagingRoot,
      transferRoot,
    });

    expect(updated.files).toEqual([filename]);
    expect(fs.readFileSync(path.join(transferRoot, filename))).toEqual(content);
    expect(fs.existsSync(path.join(transferRoot, "unrequested.txt"))).toBe(false);
  });

  it("keeps valid requests when another request is malformed", () => {
    const result = prepareTransferDirectory({
      items: [
        { type: "upload_artifact", path: "reports/public.txt" },
        { type: "upload_artifact", path: 42 },
      ],
      config: { upload_artifact: { "allowed-paths": ["reports/**"] } },
      stagingRoot,
      transferRoot,
    });

    expect(result.files).toEqual(["reports/public.txt"]);
  });

  it("fails closed for traversal even when a valid request precedes it", () => {
    expect(() =>
      prepareTransferDirectory({
        items: [
          { type: "upload_artifact", path: "reports/public.txt" },
          { type: "upload_artifact", path: "../outside.txt" },
        ],
        config: { upload_artifact: { "allowed-paths": ["reports/**"] } },
        stagingRoot,
        transferRoot,
      })
    ).toThrow("escapes its root");
  });

  it("fails closed when a requested path traverses a symbolic link", () => {
    const outsideFile = path.join(root, "outside.txt");
    fs.writeFileSync(outsideFile, "outside staging root");
    fs.symlinkSync(outsideFile, path.join(stagingRoot, "reports", "linked.txt"));

    expect(() =>
      prepareTransferDirectory({
        items: [{ type: "upload_artifact", path: "reports/linked.txt" }],
        config: { upload_artifact: {} },
        stagingRoot,
        transferRoot,
      })
    ).toThrow("symbolic link");
    expect(fs.existsSync(path.join(transferRoot, "reports", "linked.txt"))).toBe(false);
  });

  it("does not follow a transfer-root symlink while clearing previous output", () => {
    const outsideRoot = path.join(root, "outside-transfer");
    fs.mkdirSync(outsideRoot);
    fs.writeFileSync(path.join(outsideRoot, "keep.txt"), "outside file");
    fs.symlinkSync(outsideRoot, transferRoot);

    prepareTransferDirectory({
      items: [{ type: "upload_artifact", path: "reports/public.txt" }],
      config: { upload_artifact: { "allowed-paths": ["reports/**"] } },
      stagingRoot,
      transferRoot,
    });

    expect(fs.readFileSync(path.join(outsideRoot, "keep.txt"), "utf8")).toBe("outside file");
    expect(fs.readFileSync(path.join(transferRoot, "reports", "public.txt"), "utf8")).toBe("requested and allowed");
  });

  it("stages workspace-relative and absolute workspace paths", () => {
    const workspaceRoot = path.join(root, "workspace");
    fs.mkdirSync(workspaceRoot);
    fs.writeFileSync(path.join(workspaceRoot, "workspace.txt"), "from workspace");

    const relative = prepareTransferDirectory({
      items: [{ type: "upload_artifact", path: "workspace.txt" }],
      config: { upload_artifact: { "allowed-paths": ["workspace.txt"] } },
      stagingRoot,
      transferRoot,
      workspaceRoot,
    });
    expect(relative.files).toEqual(["workspace.txt"]);
    expect(fs.readFileSync(path.join(transferRoot, "workspace.txt"), "utf8")).toBe("from workspace");

    const absolute = prepareTransferDirectory({
      items: [{ type: "upload_artifact", path: path.join(workspaceRoot, "workspace.txt") }],
      config: { upload_artifact: { "allowed-paths": ["workspace.txt"] } },
      stagingRoot,
      transferRoot,
      workspaceRoot,
    });
    expect(absolute.files).toEqual(["workspace.txt"]);
    expect(fs.readFileSync(path.join(transferRoot, "workspace.txt"), "utf8")).toBe("from workspace");
  });

  it("enforces max-size-bytes for each request before writing any selected file", () => {
    const result = prepareTransferDirectory({
      items: [{ type: "upload_artifact", filters: { include: ["reports/*.txt"] } }],
      config: { upload_artifact: { "max-size-bytes": 8 } },
      stagingRoot,
      transferRoot,
    });

    expect(result.files).toEqual([]);
    expect(fs.existsSync(transferRoot)).toBe(false);
  });

  it("preserves zero-match path metadata without transferring rejected content", () => {
    const result = prepareTransferDirectory({
      items: [{ type: "upload_artifact", path: "reports/private.txt" }],
      config: { upload_artifact: { "allowed-paths": ["reports/public.txt"] } },
      stagingRoot,
      transferRoot,
    });

    expect(result.files).toEqual(["reports/private.txt"]);
    expect(fs.readFileSync(path.join(transferRoot, "reports", "private.txt"))).toEqual(Buffer.alloc(0));
    expect(resolveRequestFiles({ path: "reports/private.txt" }, transferRoot, { "allowed-paths": ["reports/public.txt"] })).toEqual([]);
  });

  it("preserves empty-directory metadata for if-no-files handling", () => {
    fs.mkdirSync(path.join(stagingRoot, "empty"));
    const result = prepareTransferDirectory({
      items: [{ type: "upload_artifact", path: "empty" }],
      config: { upload_artifact: { "allowed-paths": ["reports/**"] } },
      stagingRoot,
      transferRoot,
    });

    expect(result.files).toEqual(["empty/.gh-aw-empty-selection"]);
    expect(resolveRequestFiles({ path: "empty" }, transferRoot, { "allowed-paths": ["reports/**"] })).toEqual([]);
  });

  it("applies the size limit independently to each request", () => {
    fs.writeFileSync(path.join(stagingRoot, "first.txt"), "first file!");
    fs.writeFileSync(path.join(stagingRoot, "second.txt"), "second");
    const result = prepareTransferDirectory({
      items: [
        { type: "upload_artifact", path: "first.txt" },
        { type: "upload_artifact", path: "second.txt" },
      ],
      config: { upload_artifact: { "allowed-paths": ["*.txt"], "max-size-bytes": 10 } },
      stagingRoot,
      transferRoot,
    });

    expect(result.files).toEqual(["second.txt"]);
  });

  it("omits connector files that do not match their declared extension policy", () => {
    const content = Buffer.from("connector attachment");
    const filename = stagedConnectorFile(stagingRoot, content, ".txt");
    const result = prepareTransferDirectory({
      items: [{ type: "azure_devops_upload_attachment", file: filename }],
      config: {
        azure_devops_upload_attachment: {
          staged_files: { file: { maxBytes: 1024, extensions: [".pdf"] } },
        },
      },
      stagingRoot,
      transferRoot,
    });

    expect(result.files).toEqual([]);
  });

  it("resolves runtime policy values as data after configuration parsing", () => {
    const injectedValue = 'public.txt"],"staged_files":{"other":{"maxBytes":999999}},"allowed-paths":["';
    const config = {
      upload_artifact: {
        "allowed-paths": ["reports/__GH_AW_UPLOAD_ARTIFACT_RUNTIME_POLICY_0__"],
      },
    };

    const resolved = resolveRuntimePolicyValues(config, { GH_AW_UPLOAD_ARTIFACT_RUNTIME_POLICY_0: injectedValue });

    expect(resolved.upload_artifact["allowed-paths"]).toEqual([`reports/${injectedValue}`]);
    expect(resolved.upload_artifact["staged_files"]).toBeUndefined();
  });

  it("fails closed when a runtime policy value is missing", () => {
    expect(() => resolveRuntimePolicyValues({ upload_artifact: { "allowed-paths": ["reports/__GH_AW_UPLOAD_ARTIFACT_RUNTIME_POLICY_0__"] } }, {})).toThrow("Missing runtime safe-output policy value 0");
  });

  it("ignores claim selectors when work queues are disabled", () => {
    const result = prepareTransferDirectory({
      items: [{ type: "upload_artifact", path: "reports/public.txt", claim_handle: "claim-1" }],
      config: { upload_artifact: { "allowed-paths": ["reports/**"] } },
      stagingRoot,
      transferRoot,
      queueEnabled: false,
    });

    expect(result.files).toEqual(["reports/public.txt"]);
  });

  it("uses the assigned Claim namespace for uploads and the shared root for connectors", () => {
    const claimRoot = claimArtifactPath(stagingRoot, "claim-1", claimAssignment);
    fs.mkdirSync(claimRoot, { recursive: true });
    fs.writeFileSync(path.join(claimRoot, "claim.txt"), "claim file");
    const connectorContent = Buffer.from("shared connector");
    const connectorFilename = stagedConnectorFile(stagingRoot, connectorContent);

    const result = prepareTransferDirectory({
      items: [
        { type: "upload_artifact", path: "claim.txt", claim_handle: "claim-1" },
        { type: "azure_devops_upload_attachment", file: connectorFilename, claim_handle: "claim-1" },
      ],
      config: {
        upload_artifact: { "allowed-paths": ["claim.txt"] },
        azure_devops_upload_attachment: {
          staged_files: { file: { maxBytes: 1024, extensions: [".pdf"] } },
        },
      },
      stagingRoot,
      transferRoot,
      assignment: claimAssignment,
      queueEnabled: true,
    });

    expect(result.files).toEqual([connectorFilename, path.relative(stagingRoot, claimRoot).split(path.sep).join("/") + "/claim.txt"]);
    expect(fs.readFileSync(path.join(transferRoot, connectorFilename))).toEqual(connectorContent);
    expect(fs.readFileSync(path.join(transferRoot, path.relative(stagingRoot, claimRoot), "claim.txt"), "utf8")).toBe("claim file");
  });

  it("does not export records for unconfigured safe-output types", () => {
    const result = prepareTransferDirectory({
      items: [{ type: "upload_artifact", path: "reports" }],
      config: {},
      stagingRoot,
      transferRoot,
    });

    expect(result.files).toEqual([]);
    expect(fs.existsSync(transferRoot) ? fs.readdirSync(transferRoot) : []).toEqual([]);
  });
});

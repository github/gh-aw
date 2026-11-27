// @ts-check
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createRequire } from "node:module";

const { prepareTransferDirectory } = createRequire(import.meta.url)("./prepare_upload_artifact_staging.cjs");

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

  it("applies request filters and configured exclusions before transfer", () => {
    const result = prepareTransferDirectory({
      items: [{ type: "upload_artifact", filters: { include: ["reports/*.txt"] } }],
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
    fs.writeFileSync(path.join(stagingRoot, "report-hash.pdf"), "connector attachment");
    const updated = prepareTransferDirectory({
      items: [{ type: "azure_devops_upload_attachment", file: "report-hash.pdf" }],
      config: {
        azure_devops_upload_attachment: {
          staged_files: { file: { maxBytes: 1024 } },
        },
      },
      stagingRoot,
      transferRoot,
    });

    expect(updated.files).toEqual(["report-hash.pdf"]);
    expect(fs.readFileSync(path.join(transferRoot, "report-hash.pdf"), "utf8")).toBe("connector attachment");
    expect(fs.existsSync(path.join(transferRoot, "unrequested.txt"))).toBe(false);
  });

  it("fails closed for path traversal requests", () => {
    expect(() =>
      prepareTransferDirectory({
        items: [{ type: "upload_artifact", path: "../outside.txt" }],
        config: { upload_artifact: {} },
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

  it("does not export records for unconfigured safe-output types", () => {
    const result = prepareTransferDirectory({
      items: [{ type: "upload_artifact", path: "reports" }],
      config: {},
      stagingRoot,
      transferRoot,
    });

    expect(result.files).toEqual([]);
    expect(fs.readdirSync(transferRoot)).toEqual([]);
  });
});

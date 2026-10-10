import { describe, expect, it } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { readPortfolioSettings } from "./work_queue_portfolio_config.cjs";

describe("portfolio shared configuration", () => {
  it("uses repository aw.json and never identity-specific generator defaults", () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "aw-portfolio-config-"));
    try {
      expect(readPortfolioSettings(directory)).toBeUndefined();
      fs.mkdirSync(path.join(directory, ".github/workflows"), { recursive: true });
      const filename = path.join(directory, ".github/workflows/aw.json");
      fs.writeFileSync(filename, JSON.stringify({ work_queue: { concurrency: 2, issues: { label: "cookie" } } }));
      expect(readPortfolioSettings(directory)).toEqual({ concurrency: 2, issues: { label: "cookie" } });
      fs.writeFileSync(filename, '{"work_queue":{"concurrency":0}}');
      expect(() => readPortfolioSettings(directory)).toThrow(/work_queue.concurrency/);
      fs.writeFileSync(filename, '{"work_queue":{"principal":"11"}}');
      expect(() => readPortfolioSettings(directory)).toThrow(/work_queue.principal/);
      fs.writeFileSync(filename, '{"work_queue":null}');
      expect(() => readPortfolioSettings(directory)).toThrow(/work_queue/);
      fs.writeFileSync(filename, "{");
      expect(() => readPortfolioSettings(directory)).toThrow();
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });
});

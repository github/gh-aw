import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import vm from "node:vm";
import { describe, expect, it } from "vitest";

const configSource = readFileSync(resolve(__dirname, "../eslint.config.cjs"), "utf8");
const executableConfigSource = configSource.replace(`const plugin = require("./dist/index.js");`, "const plugin = {};");
const cjsModule = { exports: {} };
vm.runInNewContext(executableConfigSource, { module: cjsModule, exports: cjsModule.exports }, { filename: "eslint.config.cjs" });
const eslintConfigs = Array.isArray(cjsModule.exports) ? cjsModule.exports : [];
const getRuleSeverity = (ruleName: string) => {
  const configWithRule = eslintConfigs.find(config => config && typeof config === "object" && config.rules && typeof config.rules === "object" && ruleName in config.rules);
  return configWithRule?.rules?.[ruleName];
};

describe("eslint.config.cjs", () => {
  it("promotes require-http-response-error-listener to error severity", () => {
    expect(getRuleSeverity("gh-aw-custom/require-http-response-error-listener")).toBe("error");
  });

  it("keeps non-promoted custom rules at warning severity", () => {
    expect(getRuleSeverity("gh-aw-custom/no-throw-plain-object")).toBe("warn");
  });

  it("rejects ad hoc JSONL parsers at error severity", () => {
    expect(getRuleSeverity("gh-aw-custom/no-ad-hoc-jsonl-parser")).toBe("error");
  });
});

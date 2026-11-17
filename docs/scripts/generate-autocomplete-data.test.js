import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import { autocompleteData } from "./generate-autocomplete-data.js";

const publishedData = JSON.parse(fs.readFileSync(new URL("../public/editor/autocomplete-data.json", import.meta.url), "utf8"));

test("merge-pull-request exposes nested policy configuration in generated and published editor data", () => {
  const generatedMerge = autocompleteData.root["safe-outputs"].children["merge-pull-request"];
  const publishedMerge = publishedData.root["safe-outputs"].children["merge-pull-request"];

  assert.ok(generatedMerge.children, "merge-pull-request must expand beyond the shallow safe-outputs limit");
  assert.equal(generatedMerge.children["allowed-base-branches"].type, "array");
  assert.equal(generatedMerge.children["allowed-base-branches"].array, true);
  assert.deepEqual(generatedMerge.children["protected-files"].enum, ["blocked", "allowed"]);
  assert.equal(generatedMerge.children["protected-files"].leaf, true);
  assert.deepEqual(publishedMerge, generatedMerge, "published merge configuration must match the generator");
});

test("graders autocomplete describes JavaScript-only operational value", () => {
  for (const data of [autocompleteData, publishedData]) {
    assert.match(data.root.graders.desc, /reserved operational-value JavaScript/);
    assert.doesNotMatch(data.root.graders.desc, /operational-value\.run|deprecated operational-value/);
  }
});

test("other safe-output entries retain shallow completion data", () => {
  assert.equal(autocompleteData.root["safe-outputs"].children["create-pull-request"].children, undefined);
});

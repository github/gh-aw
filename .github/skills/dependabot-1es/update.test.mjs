import assert from "node:assert/strict";
import { test } from "node:test";
import { changedPackages, requireDependencyOnlyManifest } from "./update.mjs";

const lock = packages => ({ lockfileVersion: 3, packages });

test("allows dependency bumps but rejects changes to scripts", () => {
  const before = { dependencies: { a: "1.0.0" }, scripts: { test: "node --test" } };
  const after = { ...before, dependencies: { a: "1.1.0" } };
  assert.doesNotThrow(() => requireDependencyOnlyManifest(before, after));
  assert.throws(() => requireDependencyOnlyManifest(before, { ...after, scripts: {} }), /non-dependency change/);
});

test("detects exact top-level and nested changes without treating the title as data", () => {
  const before = lock({
    "node_modules/a": { version: "1.0.0" },
    "node_modules/unchanged": { version: "2.0.0", dev: true },
    "node_modules/removed": { version: "3.0.0" },
  });
  const after = lock({
    "node_modules/a": { version: "1.1.0" },
    "node_modules/a/node_modules/@scope/pkg": {
      version: "2.0.0-beta.1",
      resolved: "https://registry.npmjs.org/@scope/pkg/-/pkg-2.0.0-beta.1.tgz",
    },
    "node_modules/unchanged": { version: "2.0.0" },
  });
  assert.deepEqual(changedPackages(before, after), ["@scope/pkg@2.0.0-beta.1", "a@1.1.0"]);
});

test("rejects a changed tarball outside the approved registries even at the same version", () => {
  const before = lock({ "node_modules/a": { version: "1.0.0" } });
  const after = lock({
    "node_modules/a": { version: "1.0.0", resolved: "https://example.org/a-1.0.0.tgz" },
  });
  assert.throws(() => changedPackages(before, after), /unsupported tarball source for a@1\.0\.0/);
});

test("ignores unchanged pinned Git dependencies but rejects changes to them", () => {
  const entry = { version: "2.0.0", resolved: "git+ssh://git@github.com/example/repo.git#old" };
  const before = lock({ "node_modules/a": entry });
  assert.deepEqual(changedPackages(before, before), []);
  assert.throws(() => changedPackages(before, lock({ "node_modules/a": { ...entry, resolved: entry.resolved.replace("old", "new") } })), /unsupported tarball source/);
});

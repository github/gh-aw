// After regenerating either SDK lockfile, run this script from the repository.
// npm uses the repository registry by default; npm_config_registry can override it.
import { execFile as execFileCallback } from "node:child_process";
import { createHash } from "node:crypto";
import { readFile, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { collectPackages } from "./check-npm-1es.mjs";

const execFile = promisify(execFileCallback);
const root = fileURLToPath(new URL("../", import.meta.url));
const variants = ["node", "typescript"];
const projects = await Promise.all(
  variants.map(async variant => {
    const dir = new URL(`../actions/setup/copilot-sdk/${variant}/`, import.meta.url);
    const manifest = await readFile(new URL("package.json", dir));
    const lock = await readFile(new URL("package-lock.json", dir));
    return {
      variant,
      file: new URL("cooldown.json", dir),
      digest: createHash("sha256").update(manifest).update("\0").update(lock).digest("hex"),
      graph: JSON.parse(lock),
      newest: 0,
    };
  })
);

const { packages, external } = collectPackages(projects.map(project => [project.variant, project.graph]));
if (external.length || !packages.size) {
  throw new Error(`Expected only registry packages in the SDK locks. External entries: ${external.join(", ")}`);
}

const times = new Map();
for (const [spec, locations] of packages) {
  const separator = spec.lastIndexOf("@");
  const name = spec.slice(0, separator);
  const version = spec.slice(separator + 1);
  if (!times.has(name)) {
    const { stdout } = await execFile("npm", ["view", name, "time", "--json", "--prefer-online", "--fetch-retries=0", "--fetch-timeout=15000"], { cwd: root });
    times.set(name, JSON.parse(stdout));
  }
  const published = Date.parse(times.get(name)?.[version]);
  if (!Number.isFinite(published)) throw new Error(`Missing registry publication time for ${spec}`);
  for (const project of projects) {
    if (locations.some(location => location.startsWith(`${project.variant}: `))) {
      project.newest = Math.max(project.newest, published);
    }
  }
}

for (const { file, digest, newest } of projects) {
  await writeFile(file, JSON.stringify({ digest, newest: new Date(newest).toISOString() }, null, 2) + "\n");
}

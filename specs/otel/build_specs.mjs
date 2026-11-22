import { copyFileSync, globSync, mkdirSync, mkdtempSync, rmSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { compile, listServices, logDiagnostics, NodeHost } from "@typespec/compiler";

const project = dirname(fileURLToPath(import.meta.url));
const root = join(project, "../..");
const sources = globSync("**/*.tsp", {
  cwd: root,
  exclude: [".git", "**/node_modules", "**/dist"],
}).sort();

if (sources.length === 0) {
  throw new Error("No TypeSpec specifications found.");
}

const staging = mkdtempSync(join(project, "node_modules/.spec-build-"));
try {
  // Keep relative imports intact while resolving libraries from the locked toolchain.
  for (const source of sources) {
    const file = join(staging, source);
    mkdirSync(dirname(file), { recursive: true });
    copyFileSync(join(root, source), file);
  }

  for (const source of sources) {
    console.log(`Building ${source}`);
    const file = join(staging, source);
    const program = await compile(NodeHost, file, { noEmit: true, warningAsError: true });
    if (program.hasError()) {
      logDiagnostics(program.diagnostics, NodeHost.logSink);
      process.exitCode = 1;
      break;
    }

    const emitter = listServices(program).length ? "@typespec/openapi3" : "@typespec/json-schema";
    const emitted = await compile(NodeHost, file, {
      emit: [emitter],
      outputDir: join(root, "dist/specs", source.slice(0, -4)),
      options: {
        "@typespec/openapi3": { "seal-object-schemas": true },
        "@typespec/json-schema": { "file-type": "json", "seal-object-schemas": true },
      },
      warningAsError: true,
    });
    logDiagnostics(emitted.diagnostics, NodeHost.logSink);
    if (emitted.hasError()) {
      process.exitCode = 1;
      break;
    }
  }
} finally {
  rmSync(staging, { recursive: true, force: true });
}

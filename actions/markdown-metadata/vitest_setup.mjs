import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

export default function setup() {
  const executable = process.platform === "win32" ? "markdown-metadata.exe" : "markdown-metadata";
  execFileSync("go", ["build", "-mod=readonly", "-trimpath", "-o", `../setup/js/${executable}`, "."], {
    cwd: fileURLToPath(new URL("./", import.meta.url)),
    env: { ...process.env, GOWORK: "off", GOTOOLCHAIN: "local", CGO_ENABLED: "0" },
    stdio: "inherit",
  });
}

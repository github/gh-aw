// @ts-check

// Sanitized stdout shapes from existing Actions artifacts, not new executions.
// https://github.com/github/gh-aw/actions/runs/37553714211 (agent artifact 11453917896)
const startupFailure = `[entrypoint] Executing command: dsh --profile headless "private fixture prompt"
[deepseek-harness] awf-reflect: models fetch returned 401 for http://api-proxy:10000/v1/models
[deepseek-harness] configured provider=github model=auto
file:///fixture/node_modules/@deepseek-ai/dsh-app-boot/lib/index.js:764
\tif (hmr === void 0) throw new Error(\`\${binName}: user patch-layer watching requires the Cordis HMR service\`);
\t                          ^

Error: dsh: user patch-layer watching requires the Cordis HMR service
    at watchUserPatches (file:///fixture/node_modules/@deepseek-ai/dsh-app-boot/lib/index.js:764:28)
    at runProfile (file:///fixture/node_modules/@deepseek-ai/dsh/lib/profile-boot.js:264:9)
    at async file:///fixture/node_modules/@deepseek-ai/dsh/lib/bin.js:133:3

Node.js v24.21.0
[deepseek-harness] DeepSeek Harness execution failed with exit code 1
[entrypoint] Relaxed /fixture/gh-aw group permissions for host-side post-processing
[INFO] Stopping containers...
 Container awf-agent  Stopping
 Container awf-agent  Stopped
[SUCCESS] Containers stopped successfully
[WARN] Command completed with exit code: 1
Process exiting with code: 1
`;

// https://github.com/github/gh-aw/actions/runs/37866549668 (agent artifact 11589545575)
// The job timed out after 15 minutes. Its stdout ended at configuration; no
// native terminal observation, message, usage, or exit code was present there.
const timedOut = `[entrypoint] Executing command: dsh --profile headless "private fixture prompt"
[deepseek-harness] awf-reflect: models fetch returned 401 for http://api-proxy:10000/v1/models
[deepseek-harness] configured provider=github model=auto
`;

module.exports = { startupFailure, timedOut };

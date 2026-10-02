import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { spawnSync } from "child_process";
import fs from "fs";
import os from "os";
import path from "path";
import { main } from "./create_prompt.cjs";

const core = { info: vi.fn(), setFailed: vi.fn() };

describe.skipIf(process.platform === "win32")("create_prompt renderer integration", () => {
  let tempDir;
  let originalEnv;

  beforeEach(() => {
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-create-prompt-integration-"));
    originalEnv = { ...process.env };
    core.info.mockReset();
    core.setFailed.mockReset();
  });

  afterEach(() => {
    process.env = originalEnv;
    fs.rmSync(tempDir, { recursive: true, force: true });
  });

  it.each(["true", "false"])("matches the legacy shell renderer when conditional content is %s", async includeContext => {
    const promptsDir = path.join(tempDir, "gh-aw", "prompts");
    const legacyPromptPath = path.join(tempDir, "legacy", "prompt.txt");
    const javascriptPromptPath = path.join(tempDir, "gh-aw", "aw-prompts", "prompt.txt");
    const userPrompt = "# User Prompt\nLiteral $PATH and `backticks`\nUnicode: 雪man ☃️ café\n";
    fs.mkdirSync(promptsDir, { recursive: true });
    fs.writeFileSync(path.join(promptsDir, "system.md"), "File context\n", "utf8");
    fs.writeFileSync(path.join(promptsDir, "conditional.md"), "Conditional context\n", "utf8");

    const legacyScript = `
set +o histexpand
mkdir -p "$(dirname "$GH_AW_PROMPT")"
{
cat << 'GH_AW_PROMPT_TEST_EOF'
<system>
Inline context
GH_AW_PROMPT_TEST_EOF
cat "$GH_AW_PROMPTS_DIR/system.md"
if [ "$GH_AW_INCLUDE_CONTEXT" = "true" ]; then
  cat "$GH_AW_PROMPTS_DIR/conditional.md"
fi
cat << 'GH_AW_PROMPT_TEST_EOF'
</system>
# User Prompt
Literal $PATH and \`backticks\`
Unicode: 雪man ☃️ café
GH_AW_PROMPT_TEST_EOF
} > "$GH_AW_PROMPT"
`;
    const legacyResult = spawnSync("bash", ["-c", legacyScript], {
      env: {
        ...process.env,
        GH_AW_PROMPT: legacyPromptPath,
        GH_AW_PROMPTS_DIR: promptsDir,
        GH_AW_INCLUDE_CONTEXT: includeContext,
      },
      encoding: "utf8",
    });
    expect(legacyResult.status, legacyResult.stderr).toBe(0);

    process.env = {
      ...originalEnv,
      RUNNER_TEMP: tempDir,
      GH_AW_PROMPT: javascriptPromptPath,
      GH_AW_PROMPT_CONFIG: JSON.stringify({
        system_item_count: 4,
        items: [{ content_env: "INLINE_CONTEXT" }, { file: "system.md" }, { file: "conditional.md", condition_env: "GH_AW_INCLUDE_CONTEXT" }, { content_env: "SYSTEM_CLOSE" }, { content_env: "USER_PROMPT" }],
      }),
      GH_AW_INCLUDE_CONTEXT: includeContext,
      INLINE_CONTEXT: "<system>\nInline context\n",
      SYSTEM_CLOSE: "</system>\n",
      USER_PROMPT: userPrompt,
    };

    await main(core);

    expect(core.setFailed).not.toHaveBeenCalled();
    expect(fs.readFileSync(javascriptPromptPath)).toEqual(fs.readFileSync(legacyPromptPath));
    const system = fs.readFileSync(path.join(path.dirname(javascriptPromptPath), "system.txt"), "utf8");
    const user = fs.readFileSync(path.join(path.dirname(javascriptPromptPath), "user.txt"), "utf8");
    expect(system).toBe(`<system>\nInline context\nFile context\n${includeContext === "true" ? "Conditional context\n" : ""}</system>\n`);
    expect(user).toBe(userPrompt);
    expect(fs.readFileSync(javascriptPromptPath, "utf8")).toBe(system + user);
  });
});

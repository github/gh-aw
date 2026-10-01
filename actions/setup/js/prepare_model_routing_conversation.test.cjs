import { describe, expect, it } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { prepareModelRoutingConversation } from "./prepare_model_routing_conversation.cjs";

describe("prepareModelRoutingConversation", () => {
  const cases = [
    {
      name: "separates system and task",
      prompt: "<system>\nboilerplate\n</system>\n  Do the task. \n",
      system: "<system>\nboilerplate\n</system>",
      user: "\n  Do the task. \n",
      task: "Do the task.",
      taskOnly: true,
    },
    {
      name: "falls back without leading system",
      prompt: "Do the task.",
      user: "Do the task.",
      task: "Do the task.",
      taskOnly: false,
    },
    {
      name: "falls back without closing system tag",
      prompt: "<system>\nboilerplate\nDo the task.",
      user: "<system>\nboilerplate\nDo the task.",
      task: "<system>\nboilerplate\nDo the task.",
      taskOnly: false,
    },
    {
      name: "falls back when task is empty",
      prompt: "<system>\nboilerplate\n</system> \n",
      user: "<system>\nboilerplate\n</system> \n",
      task: "<system>\nboilerplate\n</system>",
      taskOnly: false,
    },
    {
      name: "keeps neutralized system text in task",
      prompt: "<system>\nboilerplate\n</system>\nHandle (system) text.",
      system: "<system>\nboilerplate\n</system>",
      user: "\nHandle (system) text.",
      task: "Handle (system) text.",
      taskOnly: true,
    },
  ];

  for (const testCase of cases) {
    it(testCase.name, () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-routing-conversation-"));
      try {
        const promptPath = path.join(tempDir, "aw-prompts", "prompt.txt");
        const conversationFile = path.join(tempDir, "routing-conversation.json");
        fs.mkdirSync(path.dirname(promptPath), {
          recursive: true,
          mode: 0o700,
        });
        fs.writeFileSync(promptPath, testCase.prompt, { mode: 0o600 });

        const result = prepareModelRoutingConversation(promptPath, conversationFile);

        expect(result.taskOnly).toBe(testCase.taskOnly);
        expect(fs.readFileSync(path.join(path.dirname(promptPath), "system.txt"), "utf8")).toBe(testCase.system ?? "");
        expect(fs.readFileSync(path.join(path.dirname(promptPath), "user.txt"), "utf8")).toBe(testCase.user);
        expect(fs.readFileSync(promptPath, "utf8")).toBe(testCase.prompt);
        expect(JSON.parse(fs.readFileSync(conversationFile, "utf8"))).toEqual([{ role: "user", parts: [{ text: testCase.task }] }]);

        if (process.platform !== "win32") {
          for (const file of [path.join(path.dirname(promptPath), "system.txt"), path.join(path.dirname(promptPath), "user.txt"), promptPath, conversationFile]) {
            expect(fs.statSync(file).mode & 0o777).toBe(0o600);
          }
        }
      } finally {
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });
  }

  it("rejects an empty rendered prompt", () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-routing-conversation-"));
    try {
      const promptPath = path.join(tempDir, "prompt.txt");
      fs.writeFileSync(promptPath, " \n", { mode: 0o600 });
      expect(() => prepareModelRoutingConversation(promptPath, path.join(tempDir, "conversation.json"))).toThrow("Rendered workflow prompt is empty; cannot route this task");
    } finally {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  });
});

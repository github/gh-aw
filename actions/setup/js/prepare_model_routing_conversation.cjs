// @ts-check

const fs = require("node:fs");
const path = require("node:path");

/**
 * Splits the rendered prompt and writes private router input files.
 * @param {string} promptPath
 * @param {string} conversationFile
 */
function prepareModelRoutingConversation(promptPath, conversationFile) {
  const prompt = fs.readFileSync(promptPath, "utf8");
  if (!prompt.trim()) {
    throw new Error("Rendered workflow prompt is empty; cannot route this task");
  }

  const closeTag = "</system>";
  const closeIndex = prompt.indexOf(closeTag);
  let system = "";
  let user = prompt;
  let taskOnly = false;
  if (prompt.trimStart().startsWith("<system>") && closeIndex >= 0) {
    const task = prompt.slice(closeIndex + closeTag.length);
    if (task.trim()) {
      system = prompt.slice(0, closeIndex + closeTag.length);
      user = task;
      taskOnly = true;
    }
  }

  const promptDir = path.dirname(promptPath);
  fs.mkdirSync(promptDir, { recursive: true, mode: 0o700 });
  const writePrivateFile = (file, content) => {
    const flags = fs.constants.O_WRONLY | fs.constants.O_CREAT | fs.constants.O_TRUNC | (fs.constants.O_NOFOLLOW || 0);
    const fd = fs.openSync(file, flags, 0o600);
    try {
      fs.fchmodSync(fd, 0o600);
      fs.writeFileSync(fd, content, "utf8");
    } finally {
      fs.closeSync(fd);
    }
  };

  for (const [name, content] of [
    ["system.txt", system],
    ["user.txt", user],
  ]) {
    writePrivateFile(path.join(promptDir, name), content);
  }
  writePrivateFile(promptPath, system + user);

  const taskText = user.trim();
  fs.mkdirSync(path.dirname(conversationFile), {
    recursive: true,
    mode: 0o700,
  });
  writePrivateFile(conversationFile, JSON.stringify([{ role: "user", parts: [{ text: taskText }] }]));

  return {
    taskLength: taskText.length,
    promptLength: prompt.length,
    taskOnly,
  };
}

function main() {
  const promptPath = process.env.GH_AW_ROUTING_PROMPT;
  const conversationFile = process.env.GH_AW_ROUTING_CONVERSATION_FILE;
  if (!promptPath || !conversationFile) {
    throw new Error("GH_AW_ROUTING_PROMPT and GH_AW_ROUTING_CONVERSATION_FILE must be set");
  }

  const result = prepareModelRoutingConversation(promptPath, conversationFile);
  console.log(`model-routing conversation: ${result.taskLength} of ${result.promptLength} chars (${result.taskOnly ? "task only" : "full prompt fallback"})`);
}

if (require.main === module) {
  try {
    main();
  } catch (error) {
    console.error(error);
    process.exitCode = 1;
  }
}

module.exports = { prepareModelRoutingConversation };

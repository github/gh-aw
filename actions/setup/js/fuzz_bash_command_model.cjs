// @ts-check
"use strict";

const assert = require("node:assert/strict");
const { splitOnPipelineOperators, extractCommandName, extractCommandNamesFromPipeline } = require("./bash_command_parser.cjs");
const { buildCopilotSDKPermissionHandler } = require("./copilot_sdk_permissions.cjs");

const COMMANDS = ["echo", "cat", "jq", "wc", "git", "python3"];
const OPERATORS = [" && ", " || ", " | ", " |& ", "; ", " & ", "\n", "\r\n"];
const ARGUMENTS = ["plain#hash", "'# && || | ; \" $()'", '"# && || | ; \\" literal"', "\\#escaped\\;word", "'first\n# still quoted'", "${value:-#fallback}", '"${outer:-${inner:-#fallback}}"'];
const ASSIGNMENTS = ["", "A=1 ", "A='# literal' B=two ", 'A="# literal" '];
const REDIRECTIONS = ["", " 2>/dev/null", " >output 2>&1", " &>output", " <input"];
const BLANKS = [" ", "\t", " \\\n  "];
const UNSUPPORTED = ["'unclosed", '"unclosed', "${value:-unclosed", "$(echo unclosed", '"$(echo "#")"', "$'escaped\\' #'; python3", "@(x #); python3", "?(x #); python3", "*(x #); python3", "+(x #); python3", "!(x #); python3"];

/**
 * Build a bounded shell-program model from fuzzer-controlled bytes. Choices are
 * consumed directly (not hashed), so Go can shrink failures to small byte inputs.
 * The model records executable names before rendering; it never parses its own
 * output to compute the oracle. Generated programs are data, never executed.
 *
 * @param {number[]} bytes
 * @param {string} payload Arbitrary text, quoted as an argument and kept in comments.
 */
function generateShellProgram(bytes, payload) {
  let cursor = 0;
  const next = () => (bytes[cursor++] ?? 0) & 255;
  const pick = values => values[next() % values.length];
  // LF alone ends Bash comments; retain CR, quotes, operators and backslashes.
  const comment = `# ${payload.slice(0, 256).replace(/\n/g, " ")} ' " $( ; && | \r python3`;
  const literal = payload.slice(0, 128).replace(/\0/g, "");
  const argumentChoices = [...ARGUMENTS, `'${literal.replace(/'/g, "'\\''")}'`, `"${literal.replace(/[\\"$`]/g, "\\$&")}"`];
  const count = next() % 9;
  const commands = Array.from({ length: count }, () => ({
    name: pick(COMMANDS),
    assignment: pick(ASSIGNMENTS),
    blank: pick(BLANKS),
    args: Array.from({ length: next() % 4 }, () => pick(argumentChoices)),
    redirection: pick(REDIRECTIONS),
    operator: pick(OPERATORS),
    comment: next() % 2 === 0,
  }));
  const render = (command, blank) => `${command.assignment}${command.name}${command.args.length ? blank + command.args.join(" ") : ""}${command.redirection}`;
  const segments = commands.map(command => render(command, command.blank === BLANKS[2] ? " " : command.blank));
  let source = `${comment}\n`;
  for (let i = 0; i < commands.length; i++) {
    source += render(commands[i], commands[i].blank);
    if (i + 1 < commands.length) {
      source += commands[i].operator;
      if (commands[i].comment) source += `${comment}\n`;
    }
  }
  source += ` # ${comment}`;
  return {
    source,
    segments,
    commandNames: commands.map(command => command.name),
    names: [...new Set(commands.map(command => command.name))],
    unsupported: pick(UNSUPPORTED),
  };
}

/**
 * Assert exact extraction for the supported grammar, fail-closed behaviour for
 * unsupported constructs, and robustness for arbitrary malformed shell text.
 * SDK allow/deny expectations come from the model, not from parser output.
 *
 * @param {number[]} bytes
 * @param {string} payload
 */
async function checkShellProgramModel(bytes, payload) {
  const model = generateShellProgram(bytes, payload);
  const context = JSON.stringify(model.source);
  const expectedSegments = model.segments;
  assert.deepEqual(splitOnPipelineOperators(model.source), expectedSegments, context);
  assert.deepEqual(extractCommandNamesFromPipeline(model.source), model.names, context);
  for (let i = 0; i < expectedSegments.length; i++) {
    assert.equal(extractCommandName(expectedSegments[i]), model.commandNames[i], context);
  }
  const allowed = COMMANDS.filter(name => name !== "python3");
  const handler = buildCopilotSDKPermissionHandler({ allowedTools: allowed.map(name => `shell(${name})`) }, () => ({ kind: "approve-once" }));
  /** @type {import("@microsoft/copilot-sdk").PermissionRequest} */
  const request = { kind: "shell", fullCommandText: model.source, commands: [], canOfferSessionApproval: false, hasWriteFileRedirection: false, intention: "Parse only", possiblePaths: [], possibleUrls: [] };
  const permission = await handler(request, { sessionId: "shell-model-fuzz" });
  assert.equal(permission.kind, model.names.length > 0 && !model.names.includes("python3") ? "approve-once" : "reject", context);

  // Every unsupported construct must invalidate the whole parse, even after a
  // valid prefix; never accept only the commands preceding an ambiguous suffix.
  const unsupported = `${model.source}\necho ${model.unsupported}`;
  assert.deepEqual(splitOnPipelineOperators(unsupported), [], JSON.stringify(unsupported));
  assert.deepEqual(extractCommandNamesFromPipeline(unsupported), [], JSON.stringify(unsupported));
  assert.equal((await handler({ ...request, fullCommandText: unsupported }, { sessionId: "shell-model-fuzz" })).kind, "reject", JSON.stringify(unsupported));

  const cut = Math.floor(((bytes.at(-1) ?? 0) * model.source.length) / 256);
  for (const text of [payload.slice(0, 4096), model.source.slice(0, cut)]) {
    assert.ok(splitOnPipelineOperators(text).every(segment => typeof segment === "string"));
    assert.ok(extractCommandNamesFromPipeline(text).every(name => typeof name === "string"));
    const name = extractCommandName(text);
    assert.ok(name === null || typeof name === "string");
  }
  return model;
}

module.exports = { generateShellProgram, checkShellProgramModel };

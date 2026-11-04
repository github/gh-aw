// @ts-check
/**
 * Tests for bash_command_parser.cjs
 *
 * Covers:
 *   - splitOnPipelineOperators: &&, ||, |, ; operators; quoted strings; subshells
 *   - extractCommandName: env-var skipping, redirection, keywords, negation
 *   - extractCommandNamesFromPipeline: end-to-end piping scenarios
 */

import { describe, it, expect } from "vitest";
import { createRequire } from "module";

const require = createRequire(import.meta.url);
const { splitOnPipelineOperators, extractCommandName, extractCommandNamesFromPipeline } = require("./bash_command_parser.cjs");

// ─────────────────────────────────────────────────────────────────────────────
// splitOnPipelineOperators
// ─────────────────────────────────────────────────────────────────────────────

describe("splitOnPipelineOperators", () => {
  it("returns a single segment for a plain command", () => {
    expect(splitOnPipelineOperators("ls /tmp")).toEqual(["ls /tmp"]);
  });

  it("splits on && (AND-then)", () => {
    expect(splitOnPipelineOperators("ls /tmp && echo done")).toEqual(["ls /tmp", "echo done"]);
  });

  it("splits on || (OR-else)", () => {
    expect(splitOnPipelineOperators("cat file.json || echo missing")).toEqual(["cat file.json", "echo missing"]);
  });

  it("splits on | (pipe)", () => {
    expect(splitOnPipelineOperators("ls -la | grep pattern")).toEqual(["ls -la", "grep pattern"]);
  });

  it.each([
    ["echo ok & cat data.json", ["echo ok", "cat data.json"]],
    ["echo ok&cat data.json&", ["echo ok", "cat data.json"]],
    ["echo ok |& cat data.json", ["echo ok", "cat data.json"]],
    ["echo ok 2>&1&cat data.json", ["echo ok 2>&1", "cat data.json"]],
    ["echo ok 2<&0 & cat data.json", ["echo ok 2<&0", "cat data.json"]],
    ["echo ok &>output & cat data.json", ["echo ok &>output", "cat data.json"]],
    ["echo ok &>>output&cat data.json", ["echo ok &>>output", "cat data.json"]],
    [String.raw`echo ok \>&cat data.json`, [String.raw`echo ok \>`, "cat data.json"]],
  ])("separates background and stderr-pipe commands in %s", (script, segments) => {
    expect(splitOnPipelineOperators(script)).toEqual(segments);
    expect(extractCommandNamesFromPipeline(script)).toEqual(["echo", "cat"]);
  });

  it.each(["echo ok 2>&1", "echo ok 1>&2", "echo ok 2<&0", "echo ok &>output", "echo ok &>>output", "echo 'a & b |& c'", 'echo "a & b |& c"', String.raw`echo a\&b`])("preserves redirections and literal ampersands in %s", script => {
    expect(splitOnPipelineOperators(script)).toEqual([script]);
    expect(extractCommandNamesFromPipeline(script)).toEqual(["echo"]);
  });

  it("splits on ; (sequential)", () => {
    expect(splitOnPipelineOperators("echo a; echo b")).toEqual(["echo a", "echo b"]);
  });

  it("handles a complex mixed pipeline", () => {
    const cmd = 'ls /tmp/dir 2>/dev/null && echo "---" && cat file.json 2>/dev/null || echo "not found"';
    const segments = splitOnPipelineOperators(cmd);
    expect(segments).toHaveLength(4);
    expect(segments[0]).toContain("ls");
    expect(segments[1]).toContain("echo");
    expect(segments[2]).toContain("cat");
    expect(segments[3]).toContain("echo");
  });

  it("does not split on && inside single quotes", () => {
    const segments = splitOnPipelineOperators("echo 'foo && bar'");
    expect(segments).toEqual(["echo 'foo && bar'"]);
  });

  it("does not split on || inside double quotes", () => {
    const segments = splitOnPipelineOperators('echo "foo || bar"');
    expect(segments).toEqual(['echo "foo || bar"']);
  });

  it("does not split on | inside double quotes", () => {
    const segments = splitOnPipelineOperators('echo "foo | bar"');
    expect(segments).toEqual(['echo "foo | bar"']);
  });

  it("does not split on ; inside single quotes", () => {
    const segments = splitOnPipelineOperators("echo 'a;b'");
    expect(segments).toEqual(["echo 'a;b'"]);
  });

  it("handles backslash escapes inside double quotes", () => {
    const segments = splitOnPipelineOperators('echo "foo\\"bar" && echo baz');
    expect(segments).toHaveLength(2);
  });

  it("does not split inside $() subshells", () => {
    const segments = splitOnPipelineOperators("echo $(ls && pwd)");
    expect(segments).toEqual(["echo $(ls && pwd)"]);
  });

  it("handles nested $() subshells", () => {
    const segments = splitOnPipelineOperators("echo $(echo $(ls && pwd)) && date");
    expect(segments).toHaveLength(2);
    expect(segments[0]).toContain("echo $(echo $(ls && pwd))");
    expect(segments[1]).toContain("date");
  });

  it("returns empty array for empty string", () => {
    expect(splitOnPipelineOperators("")).toEqual([]);
  });

  it("returns empty array for whitespace-only string", () => {
    expect(splitOnPipelineOperators("   ")).toEqual([]);
  });

  it("filters out blank segments between adjacent operators", () => {
    // '&&;' is odd but shouldn't crash
    const segments = splitOnPipelineOperators("echo a && echo b");
    expect(segments).toEqual(["echo a", "echo b"]);
  });

  it("handles three-stage &&-chain", () => {
    const segments = splitOnPipelineOperators("pwd && ls -la && safeoutputs --help");
    expect(segments).toEqual(["pwd", "ls -la", "safeoutputs --help"]);
  });

  it("splits on newlines as sequential separators", () => {
    const segments = splitOnPipelineOperators("pwd\nls -la\nsafeoutputs --help");
    expect(segments).toEqual(["pwd", "ls -la", "safeoutputs --help"]);
  });

  it.each(["\n", "\r\n"])("ignores standalone comments with %j line endings", newline => {
    const command = `# Check attribution data${newline}  # Ignore "quotes, && operators; and $(subshells)${newline}cat data.json${newline}# Done`;
    expect(splitOnPipelineOperators(command)).toEqual(["cat data.json"]);
  });

  it("ignores inline comments without hiding the next command", () => {
    expect(splitOnPipelineOperators('echo ready # Ignore "quotes && operators; too\ncat data.json')).toEqual(["echo ready", "cat data.json"]);
  });

  it("returns no segments for a comment-only script", () => {
    expect(splitOnPipelineOperators("  # Nothing to execute\n\t# Still a comment")).toEqual([]);
  });

  it.each(["echo '# literal'", 'echo "# literal"', "echo value#literal", String.raw`echo \#literal`, String.raw`echo value\ #literal`, String.raw`echo \"#literal`])("preserves literal hashes in %s", command => {
    expect(splitOnPipelineOperators(`${command} && cat data.json`)).toEqual([command, "cat data.json"]);
  });

  it("recognizes a comment after an escaped backslash and unescaped whitespace", () => {
    expect(splitOnPipelineOperators(String.raw`echo \\ # Ignore this && command` + "\ncat data.json")).toEqual([String.raw`echo \\`, "cat data.json"]);
  });

  it.each(["echo ${value:- # literal}", "echo ${outer:-${inner:- # literal}}", "echo ${value:-'quoted }' # literal}"])("preserves literal hashes inside parameter expansions: %s", command => {
    expect(splitOnPipelineOperators(`${command} && cat data.json`)).toEqual([command, "cat data.json"]);
  });

  it("does not continue a comment ending in a backslash", () => {
    expect(splitOnPipelineOperators("# Comment \\\ncat data.json")).toEqual(["cat data.json"]);
  });

  it("does not split on escaped newline continuations", () => {
    const segments = splitOnPipelineOperators("git log \\\n  --oneline \\\n  --max-count=1");
    expect(segments).toEqual(["git log --oneline --max-count=1"]);
  });

  it("trims leading/trailing whitespace from each segment", () => {
    const segments = splitOnPipelineOperators("  ls /tmp  &&  cat file  ");
    expect(segments[0]).toBe("ls /tmp");
    expect(segments[1]).toBe("cat file");
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// extractCommandName
// ─────────────────────────────────────────────────────────────────────────────

describe("extractCommandName", () => {
  it("extracts a plain command name", () => {
    expect(extractCommandName("ls /tmp")).toBe("ls");
  });

  it("extracts command name with flags", () => {
    expect(extractCommandName("cat -n file.txt")).toBe("cat");
  });

  it("extracts a command with redirection suffix", () => {
    expect(extractCommandName("ls /tmp 2>/dev/null")).toBe("ls");
  });

  it("skips a leading env-var assignment", () => {
    expect(extractCommandName("FOO=bar ls /tmp")).toBe("ls");
  });

  it("skips multiple leading env-var assignments", () => {
    expect(extractCommandName("FOO=bar BAZ=qux echo hi")).toBe("echo");
  });

  it("skips leading env-var assignment with quoted spaces", () => {
    expect(extractCommandName("FILES='a b c' echo hi")).toBe("echo");
  });

  it("skips leading env-var assignment with double-quoted spaces", () => {
    expect(extractCommandName('FILES="a b c" echo hi')).toBe("echo");
  });

  it("skips leading env-var assignment with escaped quote in double-quoted value", () => {
    expect(extractCommandName('FILES="a \\"b\\" c" echo hi')).toBe("echo");
  });

  it("handles negation operator ! and returns next command", () => {
    expect(extractCommandName("! ls /tmp")).toBe("ls");
  });

  it("handles group opening brace { and returns next command", () => {
    expect(extractCommandName("{ echo hi; }")).toBe("echo");
  });

  it("returns null for shell keyword 'then'", () => {
    expect(extractCommandName("then")).toBeNull();
  });

  it("returns null for shell keyword 'else'", () => {
    expect(extractCommandName("else")).toBeNull();
  });

  it("extracts command after shell keyword 'else'", () => {
    expect(extractCommandName("else cat file")).toBe("cat");
  });

  it("returns null for shell keyword 'fi'", () => {
    expect(extractCommandName("fi")).toBeNull();
  });

  it("extracts command after shell keyword 'elif'", () => {
    expect(extractCommandName("elif grep x file")).toBe("grep");
  });

  it("returns null for shell keyword 'if'", () => {
    expect(extractCommandName("if [ -f file ]")).toBeNull();
  });

  it("returns null for shell keyword 'for'", () => {
    expect(extractCommandName("for f in a b c")).toBeNull();
  });

  it("extracts command after shell keyword 'do'", () => {
    expect(extractCommandName("do git status")).toBe("git");
  });

  it("returns null for a bare redirection like >file", () => {
    expect(extractCommandName(">file.txt")).toBeNull();
  });

  it("returns null for a numeric redirection like 2>file", () => {
    expect(extractCommandName("2>/dev/null")).toBeNull();
  });

  it("returns null for empty string", () => {
    expect(extractCommandName("")).toBeNull();
  });

  it("returns null for whitespace-only string", () => {
    expect(extractCommandName("   ")).toBeNull();
  });

  it("extracts safeoutputs (CLI proxy command)", () => {
    expect(extractCommandName("safeoutputs report_missing_data --help 2>/dev/null")).toBe("safeoutputs");
  });

  it("extracts printf (built-in)", () => {
    expect(extractCommandName("printf '%s\\n' hello")).toBe("printf");
  });

  it("extracts pwd", () => {
    expect(extractCommandName("pwd")).toBe("pwd");
  });

  it("extracts jq with complex args", () => {
    expect(extractCommandName("jq '.[] | select(.score > 50)' results.json")).toBe("jq");
  });

  it("extracts command after env assignments without space between = and value", () => {
    expect(extractCommandName("VERBOSE=1 make build")).toBe("make");
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// extractCommandNamesFromPipeline
// ─────────────────────────────────────────────────────────────────────────────

describe("extractCommandNamesFromPipeline", () => {
  it("returns single command for a plain command", () => {
    expect(extractCommandNamesFromPipeline("ls /tmp")).toEqual(["ls"]);
  });

  it("returns both commands for a && pipeline", () => {
    expect(extractCommandNamesFromPipeline("ls /tmp && cat file.json")).toEqual(["ls", "cat"]);
  });

  it("returns all commands in a complex &&/|| chain", () => {
    const cmd = 'ls /tmp/dir 2>/dev/null && echo "---" && cat file.json 2>/dev/null || echo "not found"';
    expect(extractCommandNamesFromPipeline(cmd)).toEqual(["ls", "echo", "cat"]);
  });

  it("deduplicates repeated command names", () => {
    expect(extractCommandNamesFromPipeline("echo a && echo b && echo c")).toEqual(["echo"]);
  });

  it("handles pipe (|) operator", () => {
    expect(extractCommandNamesFromPipeline("ls -la | grep pattern | wc -l")).toEqual(["ls", "grep", "wc"]);
  });

  it("handles semicolon (;) separator", () => {
    expect(extractCommandNamesFromPipeline("echo a; date; pwd")).toEqual(["echo", "date", "pwd"]);
  });

  it("handles the GEO optimizer failed command 1", () => {
    const cmd = 'ls /tmp/gh-aw/agent/geo-optimizer/ 2>/dev/null && echo "---" && cat /tmp/gh-aw/agent/geo-optimizer/metadata.json 2>/dev/null || echo "Directory or files not found"';
    expect(extractCommandNamesFromPipeline(cmd)).toEqual(["ls", "echo", "cat"]);
  });

  it("handles the GEO optimizer failed command 2 (safeoutputs || echo)", () => {
    const cmd = 'safeoutputs report_missing_data --help 2>/dev/null || echo "unavailable"';
    expect(extractCommandNamesFromPipeline(cmd)).toEqual(["safeoutputs", "echo"]);
  });

  it("handles the GEO optimizer failed command 3 (pwd && ls && safeoutputs && printf)", () => {
    const cmd = "pwd && ls -la && safeoutputs --help && printf '%s\\n' done";
    expect(extractCommandNamesFromPipeline(cmd)).toEqual(["pwd", "ls", "safeoutputs", "printf"]);
  });

  it("returns empty array for empty string", () => {
    expect(extractCommandNamesFromPipeline("")).toEqual([]);
  });

  it("returns empty array for whitespace-only string", () => {
    expect(extractCommandNamesFromPipeline("   ")).toEqual([]);
  });

  it("extracts only executable commands from a commented script", () => {
    expect(extractCommandNamesFromPipeline("# Check counts\njq length data.json # Count entries\n# Read the data\ncat data.json")).toEqual(["jq", "cat"]);
  });

  it("handles command with $() subshell — does not split inside subshell", () => {
    const result = extractCommandNamesFromPipeline("cat $(ls /tmp)");
    expect(result).toEqual(["cat"]);
  });

  it("handles command with quoted && — does not split on quoted operator", () => {
    const result = extractCommandNamesFromPipeline('echo "a && b"');
    expect(result).toEqual(["echo"]);
  });

  it("preserves first-occurrence order", () => {
    const result = extractCommandNamesFromPipeline("cat f1 && grep x && cat f2 && echo done");
    expect(result).toEqual(["cat", "grep", "echo"]);
  });

  it("handles env-var assignments before commands in pipeline", () => {
    const result = extractCommandNamesFromPipeline("FOO=bar ls /tmp && BAZ=qux cat file");
    expect(result).toEqual(["ls", "cat"]);
  });

  it("skips shell keywords inside pipeline", () => {
    // fi / else as standalone segment yield null
    const result = extractCommandNamesFromPipeline("ls /tmp && fi");
    expect(result).toEqual(["ls"]);
  });

  it("handles a single command with no piping", () => {
    expect(extractCommandNamesFromPipeline("jq '.' results.json")).toEqual(["jq"]);
  });

  it("handles date with flags", () => {
    expect(extractCommandNamesFromPipeline("date +%Y-%m-%d && echo done")).toEqual(["date", "echo"]);
  });

  it("extracts all command names from multiline script with variables and control flow", () => {
    const cmd = `set -euo pipefail
WORK_DIR='/tmp/work'
mkdir -p "$WORK_DIR"
FILES='file1.txt file2.txt'
git log -1 --format='%H' -- file.txt | sed 's|^|file.txt |'
printf '%s\n' '---FILES---'
if [ -f "$WORK_DIR/result.json" ]; then cat "$WORK_DIR/result.json"; fi
for f in $FILES; do wc -l "$WORK_DIR/$f"; done`;
    expect(extractCommandNamesFromPipeline(cmd)).toEqual(["set", "mkdir", "git", "sed", "printf", "cat", "wc"]);
  });

  it("keeps continued multiline command as one extracted command", () => {
    expect(extractCommandNamesFromPipeline("git log \\\n  --oneline \\\n  --max-count=1")).toEqual(["git"]);
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// Extensive vector coverage (requested in PR feedback)
// ─────────────────────────────────────────────────────────────────────────────

describe("splitOnPipelineOperators – extensive vectors", () => {
  it.each([
    {
      id: "BP-SP-001",
      input: 'echo "a && b" && echo c',
      expected: ['echo "a && b"', "echo c"],
    },
    {
      id: "BP-SP-002",
      input: "echo 'x|y' | cat",
      expected: ["echo 'x|y'", "cat"],
    },
    {
      id: "BP-SP-003",
      input: 'echo $(printf "x;y") ; date',
      expected: ['echo $(printf "x;y")', "date"],
    },
    {
      id: "BP-SP-004",
      input: "FOO=1 BAR=2 env | grep FOO",
      expected: ["FOO=1 BAR=2 env", "grep FOO"],
    },
    {
      id: "BP-SP-005",
      input: "ls&&cat",
      expected: ["ls", "cat"],
    },
    {
      id: "BP-SP-006",
      input: "echo a;;;echo b",
      expected: ["echo a", "echo b"],
    },
    {
      id: "BP-SP-007",
      input: "echo $(echo $(printf '%s' hi)) && pwd",
      expected: ["echo $(echo $(printf '%s' hi))", "pwd"],
    },
    {
      id: "BP-SP-008",
      input: " ! ls /tmp &&  echo done ",
      expected: ["! ls /tmp", "echo done"],
    },
    {
      id: "BP-SP-009",
      input: "{ ls /tmp; } && echo done",
      expected: ["{ ls /tmp", "}", "echo done"],
    },
    {
      id: "BP-SP-010",
      input: "cat file.json||echo missing",
      expected: ["cat file.json", "echo missing"],
    },
  ])("matches vector $id", ({ input, expected }) => {
    expect(splitOnPipelineOperators(input)).toEqual(expected);
  });
});

describe("extractCommandName – extensive vectors", () => {
  it.each([
    { id: "BP-EC-001", segment: "FOO=bar BAR=baz grep foo file.txt", expected: "grep" },
    { id: "BP-EC-002", segment: "! printf '%s' ok", expected: "printf" },
    { id: "BP-EC-003", segment: "{ jq '.a' data.json; }", expected: "jq" },
    { id: "BP-EC-004", segment: "2>&1", expected: null },
    { id: "BP-EC-005", segment: ">out.txt", expected: null },
    { id: "BP-EC-006", segment: "A=1 B=2 safeoutputs report_missing_data", expected: "safeoutputs" },
    { id: "BP-EC-007", segment: "then cat file", expected: "cat" },
    { id: "BP-EC-008", segment: "fi", expected: null },
    { id: "BP-EC-009", segment: "do", expected: null },
    { id: "BP-EC-010", segment: "done", expected: null },
    { id: "BP-EC-011", segment: "esac", expected: null },
    { id: "BP-EC-012", segment: "in", expected: null },
    { id: "BP-EC-013", segment: "function", expected: null },
    { id: "BP-EC-014", segment: "time", expected: null },
    { id: "BP-EC-015", segment: "coproc", expected: null },
  ])("matches vector $id", ({ segment, expected }) => {
    expect(extractCommandName(segment)).toBe(expected);
  });
});

describe("extractCommandNamesFromPipeline – extensive vectors", () => {
  it.each([
    {
      id: "BP-EP-001",
      input: 'echo "a && b" && echo c',
      expected: ["echo"],
    },
    {
      id: "BP-EP-002",
      input: "echo 'x|y' | cat",
      expected: ["echo", "cat"],
    },
    {
      id: "BP-EP-003",
      input: 'echo $(printf "x;y") ; date',
      expected: ["echo", "date"],
    },
    {
      id: "BP-EP-004",
      input: "FOO=1 BAR=2 env | grep FOO",
      expected: ["env", "grep"],
    },
    {
      id: "BP-EP-005",
      input: "{ ls /tmp; } && echo done",
      expected: ["ls", "echo"],
    },
    {
      id: "BP-EP-006",
      input: "! { echo hi; }",
      expected: ["echo"],
    },
    {
      id: "BP-EP-007",
      input: "do && ls /tmp",
      expected: ["ls"],
    },
    {
      id: "BP-EP-008",
      input: "safeoutputs --help || safeoutputs report_missing_data",
      expected: ["safeoutputs"],
    },
    {
      id: "BP-EP-009",
      input: "pwd; ls; pwd; ls; echo done",
      expected: ["pwd", "ls", "echo"],
    },
    {
      id: "BP-EP-010",
      input: "cat file.json||echo missing",
      expected: ["cat", "echo"],
    },
  ])("matches vector $id", ({ input, expected }) => {
    expect(extractCommandNamesFromPipeline(input)).toEqual(expected);
  });
});

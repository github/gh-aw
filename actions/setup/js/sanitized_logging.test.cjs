// @ts-check
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { neutralizeWorkflowCommands, safeInfo, safeDebug, safeWarning, safeError } from "./sanitized_logging.cjs";

// Mock the global core object
global.core = {
  info: vi.fn(),
  debug: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
};

describe("neutralizeWorkflowCommands", () => {
  it("should neutralize double colons in workflow commands", () => {
    const input = "::set-output name=test::value";
    const output = neutralizeWorkflowCommands(input);
    // Should replace :: with : (zero-width space) :
    expect(output).toBe(":\u200B:set-output name=test:\u200B:value");
    expect(output).not.toBe(input);
  });

  it("should neutralize ::warning:: command", () => {
    const input = "::warning file=app.js,line=1::This is a warning";
    const output = neutralizeWorkflowCommands(input);
    expect(output).toBe(":\u200B:warning file=app.js,line=1:\u200B:This is a warning");
  });

  it("should neutralize ::error:: command", () => {
    const input = "::error file=app.js,line=1::This is an error";
    const output = neutralizeWorkflowCommands(input);
    expect(output).toBe(":\u200B:error file=app.js,line=1:\u200B:This is an error");
  });

  it("should neutralize ::debug:: command", () => {
    const input = "::debug::Debug message";
    const output = neutralizeWorkflowCommands(input);
    expect(output).toBe(":\u200B:debug:\u200B:Debug message");
  });

  it("should neutralize ::group:: and ::endgroup:: commands", () => {
    const input = "::group::My Group\nContent\n::endgroup::";
    const output = neutralizeWorkflowCommands(input);
    expect(output).toBe(":\u200B:group:\u200B:My Group\nContent\n:\u200B:endgroup:\u200B:");
  });

  it("should neutralize ::add-mask:: command", () => {
    const input = "::add-mask::secret123";
    const output = neutralizeWorkflowCommands(input);
    expect(output).toBe(":\u200B:add-mask:\u200B:secret123");
  });

  it("should handle text without workflow commands", () => {
    const input = "This is a normal message with no commands";
    const output = neutralizeWorkflowCommands(input);
    expect(output).toBe(input);
  });

  it("should handle text with single colons (not workflow commands)", () => {
    const input = "Time is 12:30 PM, ratio is 3:1";
    const output = neutralizeWorkflowCommands(input);
    expect(output).toBe(input);
  });

  it("should handle IPv6 addresses and other :: patterns", () => {
    const input = "IPv6 address ::1, C++ namespace std::vector";
    const output = neutralizeWorkflowCommands(input);
    // All :: should be neutralized, even if they're not workflow commands
    // This is safer than trying to detect context
    expect(output).toBe("IPv6 address :\u200B:1, C++ namespace std:\u200B:vector");
  });

  it("should handle multiple workflow commands in one string", () => {
    const input = "::warning::First\n::error::Second\n::debug::Third";
    const output = neutralizeWorkflowCommands(input);
    expect(output).toBe(":\u200B:warning:\u200B:First\n:\u200B:error:\u200B:Second\n:\u200B:debug:\u200B:Third");
  });

  it("should handle empty string", () => {
    const input = "";
    const output = neutralizeWorkflowCommands(input);
    expect(output).toBe("");
  });

  it("should handle non-string input by converting to string", () => {
    const input = 12345;
    const output = neutralizeWorkflowCommands(input);
    expect(output).toBe("12345");
  });

  it("should handle null by converting to string", () => {
    const input = null;
    const output = neutralizeWorkflowCommands(input);
    expect(output).toBe("null");
  });

  it("should handle undefined by converting to string", () => {
    const input = undefined;
    const output = neutralizeWorkflowCommands(input);
    expect(output).toBe("undefined");
  });

  it("should preserve readability with zero-width space", () => {
    const input = "User message: ::set-output name=token::abc123";
    const output = neutralizeWorkflowCommands(input);
    // The zero-width space should be invisible but prevent command execution
    expect(output).toContain(":\u200B:");
    expect(output).not.toContain("::");
  });
});

describe("safeInfo", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("should call core.info with neutralized message", () => {
    const message = "::set-output name=test::value";
    safeInfo(message);
    expect(core.info).toHaveBeenCalledWith(":\u200B:set-output name=test:\u200B:value");
  });

  it("should handle normal messages without modification", () => {
    const message = "This is a normal message";
    safeInfo(message);
    expect(core.info).toHaveBeenCalledWith(message);
  });

  it("should handle messages with single colons unchanged", () => {
    const message = "Time: 12:30 PM";
    safeInfo(message);
    expect(core.info).toHaveBeenCalledWith(message);
  });
});

describe("safeDebug", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("should call core.debug with neutralized message", () => {
    const message = "::debug::User input";
    safeDebug(message);
    expect(core.debug).toHaveBeenCalledWith(":\u200B:debug:\u200B:User input");
  });
});

describe("safeWarning", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("should call core.warning with neutralized message", () => {
    const message = "::warning::Malicious warning";
    safeWarning(message);
    expect(core.warning).toHaveBeenCalledWith(":\u200B:warning:\u200B:Malicious warning");
  });
});

describe("safeError", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("should call core.error with neutralized message", () => {
    const message = "::error::Malicious error";
    safeError(message);
    expect(core.error).toHaveBeenCalledWith(":\u200B:error:\u200B:Malicious error");
  });
});

describe("Integration tests", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("should prevent workflow command injection in user-generated noop message", () => {
    const userMessage = "No changes needed ::set-output name=hack::compromised";
    safeInfo(`No-op message: ${userMessage}`);
    const callArg = core.info.mock.calls[0][0];

    // Verify :: is neutralized
    expect(callArg).toContain(":\u200B:");
    expect(callArg).not.toContain("::");
  });

  it("should prevent workflow command injection in issue titles", () => {
    const title = "Bug report ::add-mask::secret123";
    safeInfo(`Created issue: ${title}`);
    const callArg = core.info.mock.calls[0][0];

    expect(callArg).toContain(":\u200B:");
    expect(callArg).not.toContain("::");
  });

  it("should prevent workflow command injection in comment bodies", () => {
    const body = "This is a comment\n::warning file=x.js::injected";
    safeInfo(`Comment body: ${body}`);
    const callArg = core.info.mock.calls[0][0];

    expect(callArg).toContain(":\u200B:");
    expect(callArg).not.toContain("::");
  });
});

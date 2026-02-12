// @ts-check
const { neutralizeWorkflowCommands, safeInfo, safeDebug, safeWarning, safeError } = require("./sanitized_logging.cjs");

// Mock core object
global.core = {
  info: vi.fn(),
  debug: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
};

describe("sanitized_logging", () => {
  beforeEach(() => {
    // Reset mocks before each test
    vi.clearAllMocks();
  });

  describe("neutralizeWorkflowCommands", () => {
    it("should neutralize workflow commands at the start of a line", () => {
      const input = "::set-output name=test::value";
      const expected = ": :set-output name=test::value";
      expect(neutralizeWorkflowCommands(input)).toBe(expected);
    });

    it("should neutralize multiple workflow commands on different lines", () => {
      const input = "::error::Something failed\n::warning::Be careful\n::debug::Details here";
      const expected = ": :error::Something failed\n: :warning::Be careful\n: :debug::Details here";
      expect(neutralizeWorkflowCommands(input)).toBe(expected);
    });

    it("should not neutralize :: in the middle of a line", () => {
      const input = "This has :: in the middle";
      expect(neutralizeWorkflowCommands(input)).toBe(input);
    });

    it("should not neutralize :: that is not at line start", () => {
      const input = "namespace::function";
      expect(neutralizeWorkflowCommands(input)).toBe(input);
    });

    it("should handle :: in various positions correctly", () => {
      const input = "Time 12:30 PM, ratio 3:1, IPv6 ::1, namespace::function";
      expect(neutralizeWorkflowCommands(input)).toBe(input);
    });

    it("should neutralize workflow command after newline", () => {
      const input = "Normal text\n::set-output name=x::y";
      const expected = "Normal text\n: :set-output name=x::y";
      expect(neutralizeWorkflowCommands(input)).toBe(expected);
    });

    it("should handle empty string", () => {
      expect(neutralizeWorkflowCommands("")).toBe("");
    });

    it("should handle string with only ::", () => {
      expect(neutralizeWorkflowCommands("::")).toBe(": :");
    });

    it("should handle multiple :: at line start", () => {
      const input = "::::test";
      const expected = ": :::test";
      expect(neutralizeWorkflowCommands(input)).toBe(expected);
    });

    it("should preserve :: after spaces", () => {
      const input = "  ::command";
      expect(neutralizeWorkflowCommands(input)).toBe(input);
    });

    it("should handle multiline with mixed patterns", () => {
      const input = "First line\n::error::Bad\nmiddle::text\n::warning::Watch out";
      const expected = "First line\n: :error::Bad\nmiddle::text\n: :warning::Watch out";
      expect(neutralizeWorkflowCommands(input)).toBe(expected);
    });

    it("should handle non-string input gracefully", () => {
      // @ts-expect-error - Testing non-string input
      expect(neutralizeWorkflowCommands(null)).toBe(null);
      // @ts-expect-error - Testing non-string input
      expect(neutralizeWorkflowCommands(undefined)).toBe(undefined);
      // @ts-expect-error - Testing non-string input
      expect(neutralizeWorkflowCommands(123)).toBe(123);
    });

    it("should neutralize real workflow command examples", () => {
      const commands = [
        { input: "::add-mask::secret", expected: ": :add-mask::secret" },
        { input: "::stop-commands::token", expected: ": :stop-commands::token" },
        { input: "::group::My Group", expected: ": :group::My Group" },
        { input: "::endgroup::", expected: ": :endgroup::" },
        { input: "::save-state name=foo::bar", expected: ": :save-state name=foo::bar" },
      ];

      for (const { input, expected } of commands) {
        expect(neutralizeWorkflowCommands(input)).toBe(expected);
      }
    });

    it("should handle file content with potential workflow commands", () => {
      const fileContent = `
Some text here
::error::This is in the file
More content
::set-output name=test::value
End of file`;
      const expected = `
Some text here
: :error::This is in the file
More content
: :set-output name=test::value
End of file`;
      expect(neutralizeWorkflowCommands(fileContent)).toBe(expected);
    });
  });

  describe("safeInfo", () => {
    it("should call core.info with neutralized message", () => {
      const message = "::error::test";
      safeInfo(message);
      expect(core.info).toHaveBeenCalledWith(": :error::test");
    });

    it("should handle safe messages without modification", () => {
      const message = "This is a safe message";
      safeInfo(message);
      expect(core.info).toHaveBeenCalledWith(message);
    });

    it("should neutralize multiline messages", () => {
      const message = "Line 1\n::error::Line 2";
      safeInfo(message);
      expect(core.info).toHaveBeenCalledWith("Line 1\n: :error::Line 2");
    });
  });

  describe("safeDebug", () => {
    it("should call core.debug with neutralized message", () => {
      const message = "::debug::test";
      safeDebug(message);
      expect(core.debug).toHaveBeenCalledWith(": :debug::test");
    });

    it("should handle safe messages without modification", () => {
      const message = "Debug info";
      safeDebug(message);
      expect(core.debug).toHaveBeenCalledWith(message);
    });
  });

  describe("safeWarning", () => {
    it("should call core.warning with neutralized message", () => {
      const message = "::warning::test";
      safeWarning(message);
      expect(core.warning).toHaveBeenCalledWith(": :warning::test");
    });

    it("should handle safe messages without modification", () => {
      const message = "Warning message";
      safeWarning(message);
      expect(core.warning).toHaveBeenCalledWith(message);
    });
  });

  describe("safeError", () => {
    it("should call core.error with neutralized message", () => {
      const message = "::error::test";
      safeError(message);
      expect(core.error).toHaveBeenCalledWith(": :error::test");
    });

    it("should handle safe messages without modification", () => {
      const message = "Error message";
      safeError(message);
      expect(core.error).toHaveBeenCalledWith(message);
    });
  });

  describe("integration tests", () => {
    it("should prevent workflow command injection from user input", () => {
      // Simulate user input that tries to inject workflow commands
      const userInput = "User message\n::set-output name=admin::true";
      safeInfo(userInput);
      expect(core.info).toHaveBeenCalledWith("User message\n: :set-output name=admin::true");
    });

    it("should handle command names from comment body", () => {
      const commandName = "::stop-commands::token";
      safeInfo(`Command: ${commandName}`);
      expect(core.info).toHaveBeenCalledWith("Command: ::stop-commands::token");
    });

    it("should protect file content logging", () => {
      const fileLines = ["::add-mask::password123", "normal line", "::error::fake error"];
      fileLines.forEach(line => safeInfo(line));

      expect(core.info).toHaveBeenNthCalledWith(1, ": :add-mask::password123");
      expect(core.info).toHaveBeenNthCalledWith(2, "normal line");
      expect(core.info).toHaveBeenNthCalledWith(3, ": :error::fake error");
    });
  });
});

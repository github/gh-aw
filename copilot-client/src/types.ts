/**
 * Configuration for the Copilot SDK client
 */
export interface CopilotClientConfig {
  /**
   * Path to the copilot CLI executable (default: "copilot" from PATH)
   */
  cliPath?: string;

  /**
   * Extra arguments prepended before SDK-managed flags
   */
  cliArgs?: string[];

  /**
   * URL of existing CLI server to connect to
   */
  cliUrl?: string;

  /**
   * Server port (default: 0 for random)
   */
  port?: number;

  /**
   * Use stdio transport instead of TCP (default: true)
   */
  useStdio?: boolean;

  /**
   * Log level (default: "info")
   */
  logLevel?: "none" | "error" | "warning" | "info" | "debug" | "all";

  /**
   * Auto-start server (default: true)
   */
  autoStart?: boolean;

  /**
   * Auto-restart on crash (default: true)
   */
  autoRestart?: boolean;

  /**
   * GitHub token for authentication
   */
  githubToken?: string;

  /**
   * Whether to use logged-in user for authentication
   */
  useLoggedInUser?: boolean;

  /**
   * Session configuration
   */
  session?: {
    /**
     * Model to use (e.g., "gpt-5", "claude-sonnet-4.5")
     */
    model?: string;

    /**
     * Reasoning effort level
     */
    reasoningEffort?: "low" | "medium" | "high" | "xhigh";

    /**
     * System message configuration
     */
    systemMessage?: string;

    /**
     * MCP server configurations for the session.
     * Keys are server names, values are server configurations.
     * Example:
     * {
     *   "myserver": {
     *     "type": "http",
     *     "url": "https://example.com/mcp",
     *     "tools": ["*"],
     *     "headers": { "Authorization": "Bearer token" }
     *   }
     * }
     */
    mcpServers?: Record<string, any>;
  };

  /**
   * Path to the prompt file to load
   */
  promptFile: string;

  /**
   * Path to the JSONL event log file
   */
  eventLogFile: string;
}

/**
 * Event logged to the JSONL file
 */
export interface LoggedEvent {
  timestamp: string;
  type: string;
  sessionId?: string;
  data: any;
}

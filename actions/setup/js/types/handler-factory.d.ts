// TypeScript definitions for Safe Output Handler Factory Pattern
// This file provides type definitions for the handler factory functions

/**
 * Configuration object passed to handler main() function
 * Contains handler-specific configuration from GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG
 */
interface HandlerConfig {
  /** Maximum number of items this handler should process */
  max?: number;
  /** Strict allowlist of glob patterns for files eligible for push/create. Checked independently of protected-files; both checks must pass. */
  allowed_files?: string[];
  /** List of glob patterns for files to exclude from the patch using git :(exclude) pathspecs. Matching files are stripped by git at generation time and will not appear in the commit or be subject to allowed-files or protected-files checks. */
  excluded_files?: string[];
  /** List of filenames (basenames) whose presence in a patch triggers protected-file handling */
  protected_files?: string[];
  /** Exact repository paths exempt from protected-file handling (not from allowed_files) */
  protected_files_path_exclude?: string[];
  /** List of path prefixes that trigger protected-file handling when any changed file matches */
  protected_path_prefixes?: string[];
  /** When true (default), protect any top-level directory whose name starts with "." */
  protect_top_level_dot_folders?: boolean;
  /** List of top-level dot-folder prefixes (e.g. [".agents/"]) excluded from the dot-folder protection rule */
  protected_dot_folder_excludes?: string[];
  /** Policy for how protected file matches are handled: "blocked" (default), "fallback-to-issue", or "allowed" */
  protected_files_policy?: string;
  /** When true (default), create a fallback pull request if direct push to PR branch fails with non-fast-forward/diverged branch. */
  fallback_as_pull_request?: boolean;
  /** When false, skip GraphQL signed commits and push the local git history directly. */
  signed_commits?: boolean;
  /** Additional handler-specific configuration properties */
  [key: string]: any;
}

/**
 * Map of resolved temporary IDs to their actual issue/PR/discussion references
 */
interface ResolvedTemporaryIds {
  [temporaryId: string]: {
    /** Repository in format "owner/repo" */
    repo: string;
    /** Issue, PR, or discussion number */
    number: number;
  };
}

/**
 * Result object returned by message handler function when successful
 */
interface HandlerSuccessResult {
  /** Indicates the operation was successful */
  success: true;
  /** True when the handler intentionally skipped/no-oped without applying a mutation */
  skipped?: boolean;
  /** Machine-readable summary-safe reason code */
  reasonCode?: string;
  /** Human-readable summary-safe reason */
  reason?: string;
  /** Summary-safe target metadata */
  target?: { repo?: string; number?: number; url?: string };
  /** Additional summary-safe diagnostic fields */
  safeDetails?: Record<string, any>;
  /** Additional result properties (number, url, temporaryId, etc.) */
  [key: string]: any;
}

/**
 * Result object returned by message handler function when failed
 */
interface HandlerErrorResult {
  /** Indicates the operation failed */
  success: false;
  /** Error message describing what went wrong */
  error?: string;
  /** True when the handler intentionally skipped/no-oped without applying a mutation */
  skipped?: boolean;
  /** True when the handler deferred processing for a later retry */
  deferred?: boolean;
  /** True when processing was cancelled by policy or upstream failure */
  cancelled?: boolean;
  /** Machine-readable summary-safe reason code */
  reasonCode?: string;
  /** Human-readable summary-safe reason */
  reason?: string;
  /** Summary-safe target metadata */
  target?: { repo?: string; number?: number; url?: string };
  /** Additional summary-safe diagnostic fields */
  safeDetails?: Record<string, any>;
  /** Additional result properties (skipped, etc.) */
  [key: string]: any;
}

/**
 * Result object returned by message handler function
 */
type HandlerResult = HandlerSuccessResult | HandlerErrorResult;

/**
 * Message handler function returned by the main() factory function
 * Processes a single safe output message
 *
 * @param message - The safe output message to process
 * @param resolvedTemporaryIds - Map of temporary IDs that have been resolved to actual issue/PR/discussion numbers
 * @param temporaryIdMap - Live Map of temporary IDs for in-flight substitutions (may be used by action handlers)
 * @returns Promise resolving to result with success status and details
 */
type MessageHandlerFunction = (message: any, resolvedTemporaryIds: ResolvedTemporaryIds, temporaryIdMap?: Map<string, any>) => Promise<HandlerResult>;

/**
 * Main factory function signature for safe output handlers
 * Creates and returns a message handler function configured with the provided config
 *
 * @param config - Handler configuration object
 * @returns Promise resolving to a message handler function
 */
type HandlerFactoryFunction = (config?: HandlerConfig) => Promise<MessageHandlerFunction>;

export { HandlerConfig, ResolvedTemporaryIds, HandlerSuccessResult, HandlerErrorResult, HandlerResult, MessageHandlerFunction, HandlerFactoryFunction };

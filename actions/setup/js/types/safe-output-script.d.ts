// TypeScript definitions for GitHub Agentic Workflows Safe Output Script Handlers
// This file describes the types available when writing a custom safe-output script
// (defined under safe-outputs.scripts in workflow frontmatter).
//
// Usage — write only the handler body (compiler generates the outer wrapper):
//
//   const targetChannel = item.channel ?? channel ?? "#general";
//   core.info(`[SLACK] → ${targetChannel}: ${item.message}`);
//   return { success: true };
//
// The compiler generates:
//
//   const { sanitizeContent } = require("./sanitize_content.cjs");
//
//   async function main(config = {}) {
//     const { channel } = config;          // from declared inputs
//     return async function handleMyScript(item, resolvedTemporaryIds, temporaryIdMap) {
//       // ← your handler body here
//     };
//   }
//   module.exports = { main };

import type { HandlerResult } from "./handler-factory";
export type { HandlerResult };

// ── Input-definition types ──────────────────────────────────────────────────

/**
 * The definition of a single user-declared input from the YAML `inputs:` section.
 * These definitions are available at runtime through `config.inputs`.
 */
export interface SafeOutputScriptInputDefinition {
  /** The declared type of this input ("string" | "boolean" | "number"). */
  type?: "string" | "boolean" | "number";
  /** Human-readable description shown in MCP tool registration. */
  description?: string;
  /** Whether the caller is required to supply a value for this input. */
  required?: boolean;
  /**
   * The default value to use when the caller omits the input.
   * `null` means no default was specified.
   */
  default?: string | boolean | number | null;
  /** Available options when `type` is "string" (choice constraint). */
  options?: string[];
}

// ── Config type ─────────────────────────────────────────────────────────────

/**
 * The `config` object passed to the `main()` factory function of a
 * custom safe-output script.
 *
 * This contains the **static** YAML configuration for the script — the
 * description and the input-definition metadata. The actual per-call input
 * values sent by the agent are exposed as direct properties on the `item`
 * object inside the handler function (not here).
 *
 * @example
 * ```javascript
 * // config.inputs.channel.required === true
 * // config.inputs.channel.type === "string"
 * const { inputs } = config;
 * return async function handleMyScript(item) {
 *   const ch = item.channel ?? inputs?.channel?.default ?? "#general";
 *   return { success: true };
 * };
 * ```
 */
export interface SafeOutputScriptConfig {
  /**
   * Human-readable description of this script (from `description:` in YAML).
   * Used in MCP tool registration.
   */
  description?: string;
  /**
   * Metadata for each declared input.
   * Keys are input names as declared in the YAML `inputs:` section.
   * **Note**: This is the *schema* for each input (type, description, required, default),
   * not the runtime values. Use `item.<inputName>` inside the handler to access values.
   */
  inputs?: Record<string, SafeOutputScriptInputDefinition>;
}

// ── Per-call message type ───────────────────────────────────────────────────

/**
 * The per-call message object passed to the handler function returned by `main()`.
 *
 * For custom safe-output scripts the agent sends a JSONL line like:
 * ```json
 * { "type": "post_slack_message", "channel": "#general", "message": "Hello" }
 * ```
 * All user-declared input values are properties at the **top level** of this
 * object (not nested under `.data`).
 *
 * @typeParam TInputs - The shape of the user-declared inputs.  When omitted the
 *   properties are typed as `unknown` and can be narrowed at runtime.
 *
 * @example
 * ```typescript
 * // With explicit input types:
 * type SlackInputs = { channel?: string; message?: string };
 * return async function handleSlack(item: SafeOutputScriptItem<SlackInputs>) {
 *   core.info(`channel: ${item.channel ?? "#general"}`);
 * };
 * ```
 */
export type SafeOutputScriptItem<TInputs extends Record<string, unknown> = Record<string, unknown>> = {
  /** The safe-output type identifier (normalized script name, e.g. "post_slack_message"). */
  type: string;
  /** Optional secrecy level of the message content (e.g. "public", "internal", "private"). */
  secrecy?: string;
  /** Optional integrity level of the message source (e.g. "low", "medium", "high"). */
  integrity?: string;
} & TInputs;

// ── Resolved temporary IDs ──────────────────────────────────────────────────

/**
 * A single entry in the resolved temporary IDs map.
 * Represents a GitHub issue, PR, or discussion that was created during this run.
 */
export interface ResolvedTemporaryIdEntry {
  /** Repository in "owner/repo" format. */
  repo: string;
  /** Issue, PR, or discussion number. */
  number: number;
}

/**
 * Plain-object snapshot of the temporary ID map at the time the handler is
 * invoked. Passed as the **second** argument to the handler function.
 * Temporary IDs are string keys like `"#tmp-1"` or `"#issue-123"`.
 *
 * @example
 * ```javascript
 * const resolved = resolvedTemporaryIds["#tmp-1"];
 * if (resolved) {
 *   core.info(`Issue was created: ${resolved.repo}#${resolved.number}`);
 * }
 * ```
 */
export interface ResolvedTemporaryIds {
  [temporaryId: string]: ResolvedTemporaryIdEntry;
}

/**
 * Live Map of temporary IDs to their resolved references.
 * Passed as the **third** argument to the handler function.
 *
 * Unlike `resolvedTemporaryIds` (a plain object snapshot), this is the live
 * `Map<string, ResolvedTemporaryIdEntry>` that the handler loop updates as new
 * issues/PRs are created. Use it when you need up-to-date values.
 *
 * @example
 * ```javascript
 * const entry = temporaryIdMap.get("tmp-1");
 * if (entry) {
 *   core.info(`Resolved: ${entry.repo}#${entry.number}`);
 * }
 * ```
 */
export type TemporaryIdMap = Map<string, ResolvedTemporaryIdEntry>;

// ── sanitizeContent ──────────────────────────────────────────────────────────

/**
 * Options for the {@link sanitizeContent} function.
 */
export interface SanitizeOptions {
  /** Maximum length of content (default: 524288). */
  maxLength?: number;
  /** `@mention` aliases that should NOT be neutralized. */
  allowedAliases?: string[];
  /** Maximum distinct allowed aliases to preserve. */
  maxMentions?: number;
  /** Maximum bot-trigger references before filtering (default: 10). */
  maxBotMentions?: number;
}

/**
 * Sanitizes user-supplied content for safe output.
 *
 * Injected into every auto-generated safe-output script handler module via a
 * module-level `require("./sanitize_content.cjs")` at the top of the generated
 * wrapper.  It is available as a plain `const` in the handler body — no import
 * or require needed.
 *
 * @param content - The text to sanitize.
 * @param maxLengthOrOptions - Optional maximum length or full options object.
 * @returns The sanitized text.
 *
 * @example
 * ```javascript
 * const safeTitle = sanitizeContent(item.title);
 * const safeBody  = sanitizeContent(item.body, { allowedAliases: ["copilot-swe-agent"] });
 * ```
 */
export declare const sanitizeContent: (content: string, maxLengthOrOptions?: number | SanitizeOptions) => string;

// ── Handler and factory function types ─────────────────────────────────────

/**
 * The async message-handler function returned by `main()`.
 * Receives a single safe-output message and should return a `HandlerResult`.
 *
 * The handler receives three arguments:
 * - `item` — the per-call message with runtime input values as top-level properties
 * - `resolvedTemporaryIds` — plain-object snapshot of resolved temporary IDs
 * - `temporaryIdMap` — live `Map` of resolved temporary IDs (updated as the loop runs)
 *
 * @typeParam TInputs - The shape of the user-declared inputs (defaults to
 *   `Record<string, unknown>`).
 */
export type SafeOutputScriptHandler<TInputs extends Record<string, unknown> = Record<string, unknown>> = (
  item: SafeOutputScriptItem<TInputs>,
  resolvedTemporaryIds: ResolvedTemporaryIds,
  temporaryIdMap: TemporaryIdMap
) => Promise<HandlerResult>;

/**
 * The type of the `main()` function generated by the compiler around the user's
 * handler body.
 *
 * The compiler generates the full outer wrapper from the user's declared inputs
 * and script body:
 * ```javascript
 * async function main(config = {}) {
 *   const { channel, message } = config;  // auto-generated from declared inputs
 *   return async function handleX(item, resolvedTemporaryIds, temporaryIdMap) {
 *     // <user handler body here>
 *   };
 * }
 * module.exports = { main };
 * ```
 *
 * The `main` function receives the static YAML configuration (including input
 * defaults) and returns an async handler function that processes individual
 * messages. Users write only the handler body — the outer structure is
 * generated automatically.
 *
 * @typeParam TInputs - The shape of the user-declared inputs.
 */
export type SafeOutputScriptMain<TInputs extends Record<string, unknown> = Record<string, unknown>> = (config: SafeOutputScriptConfig) => Promise<SafeOutputScriptHandler<TInputs>>;

/**
 * The `main` factory function exported by every auto-generated safe-output
 * script module (`module.exports = { main }`).
 *
 * This TypeScript declaration provides IDE type-checking support for the
 * CommonJS export (`module.exports = { main }`) that the compiler generates.
 */
export declare function main(config: SafeOutputScriptConfig): Promise<SafeOutputScriptHandler>;

// ── Globals available in the script body ────────────────────────────────────
// The globals below are injected by the `actions/github-script` environment
// that hosts the handler manager.  They are already declared in
// `github-script.d.ts`; this comment serves as a quick reference.
//
//   github   — authenticated Octokit instance
//   context  — GitHub Actions workflow run context
//   core     — @actions/core (setOutput, info, warning, error, …)
//   exec     — @actions/exec
//   glob     — @actions/glob
//   io       — @actions/io
//   require  — CommonJS require (supports relative paths and npm packages)
//
// The following is injected by the auto-generated wrapper (not github-script):
//
//   sanitizeContent — sanitize user content (see SanitizeOptions for options)

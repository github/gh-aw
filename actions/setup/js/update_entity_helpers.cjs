// @ts-check
/// <reference types="@actions/github-script" />

const { sanitizeTitle } = require("./sanitize_title.cjs");
const { parseBoolTemplatable } = require("./templatable.cjs");

/**
 * @typedef {{ title?: string, body?: string, operation?: string }} EntityUpdateItem
 */

/**
 * @typedef {{ allow_body?: boolean, footer?: boolean | string, body_footer?: string }} EntityUpdateConfig
 */

/**
 * @typedef {{ _includeFooter: boolean, _bodyFooter?: string, title?: string, _operation?: string, _rawBody?: string, body?: string }} EntityUpdateDataBase
 */

/**
 * @typedef {EntityUpdateDataBase & { [key: string]: any }} EntityUpdateData
 */

/**
 * @typedef {{ updateData: EntityUpdateData, hasCommonUpdates: boolean }} EntityUpdateResult
 */

/**
 * Build shared update payload fields for issue/PR update handlers.
 *
 * `options.defaultOperation` is required when `item.body` may be present;
 * used as fallback when `item.operation` and `configDefaultOperation` are both absent.
 *
 * @param {EntityUpdateItem} item
 * @param {EntityUpdateConfig} config
 * @param {{
 *   allowTitle?: boolean,
 *   defaultOperation?: string,
 *   configDefaultOperation?: string,
 *   includeBodyInApiData?: boolean,
 *   onBodyDisallowed?: (() => void),
 * }} [options]
 * @returns {EntityUpdateResult}
 */
function buildCommonEntityUpdateData(item, config, options = {}) {
  const { allowTitle = true, defaultOperation, configDefaultOperation, includeBodyInApiData = false, onBodyDisallowed } = options;

  const updateData = {};
  let hasCommonUpdates = false;

  if (allowTitle && item.title !== undefined) {
    updateData.title = sanitizeTitle(item.title);
    hasCommonUpdates = true;
  }

  const canUpdateBody = config.allow_body !== false;
  if (item.body !== undefined && canUpdateBody) {
    const resolvedOperation = item.operation || configDefaultOperation || defaultOperation;
    if (!resolvedOperation) {
      throw new Error("buildCommonEntityUpdateData: defaultOperation is required when body may be present");
    }
    updateData._operation = resolvedOperation;
    updateData._rawBody = item.body;
    if (includeBodyInApiData) {
      updateData.body = item.body;
    }
    hasCommonUpdates = true;
  } else if (item.body !== undefined && !canUpdateBody && typeof onBodyDisallowed === "function") {
    onBodyDisallowed();
  }

  // Always populate _includeFooter: downstream executeUpdate reads it regardless of
  // whether title/body changed, matching pre-refactor behavior in both callers.
  updateData._includeFooter = parseBoolTemplatable(config.footer, true);
  updateData._bodyFooter = config.body_footer;

  return { updateData, hasCommonUpdates };
}

module.exports = { buildCommonEntityUpdateData };

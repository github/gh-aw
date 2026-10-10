// @ts-check

/** @param {any} user */
function isNonBotActor(user) {
  const login = typeof user?.login === "string" ? user.login.toLowerCase() : "";
  return login !== "" && String(user?.type || "").toLowerCase() !== "bot" && !login.endsWith("[bot]") && login !== "github-actions" && login !== "copilot-swe-agent";
}

/** @param {string} at @param {string} since */
function after(at, since) {
  const a = Date.parse(at);
  const b = Date.parse(since);
  return Number.isFinite(a) && Number.isFinite(b) && a > b;
}

/** @param {string} from @param {string} to */
function secondsBetween(from, to) {
  const a = Date.parse(from);
  const b = Date.parse(to);
  return Number.isFinite(a) && Number.isFinite(b) ? Math.floor((b - a) / 1000) : null;
}

/** @param {any[]} comments @param {string} since */
function nonBotCommentsAfter(comments, since) {
  return comments.filter(comment => isNonBotActor(comment?.user) && after(comment?.created_at, since)).length;
}

/** @param {any} item */
function itemNumber(item) {
  if (Number.isSafeInteger(item.number) && item.number > 0) return item.number;
  const match = String(item.url || "").match(/\/(?:issues|pull|discussions)\/(\d+)/);
  const number = match ? Number(match[1]) : 0;
  return Number.isSafeInteger(number) && number > 0 ? number : 0;
}

/** @param {any} item @param {string} fallback */
function itemRepo(item, fallback) {
  if (item.repo) return item.repo;
  const match = String(item.url || "").match(/github\.com\/([^/]+\/[^/]+)/);
  return match ? match[1] : fallback;
}

module.exports = { isNonBotActor, after, secondsBetween, nonBotCommentsAfter, itemNumber, itemRepo };

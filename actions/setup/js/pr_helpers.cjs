// @ts-check

/**
 * Detect if a pull request is from a fork repository.
 *
 * Uses multiple signals for robust detection:
 * 1. Check if head.repo.fork is explicitly true (GitHub's fork flag)
 * 2. Compare repository full names if both repos exist
 * 3. Handle deleted fork case (head.repo is null)
 *
 * @param {object} pullRequest - The pull request object from GitHub context
 * @returns {{isFork: boolean, reason: string}} Fork detection result with reason
 */
function detectForkPR(pullRequest) {
  let isFork = false;
  let reason = "same repository";

  if (!pullRequest.head?.repo) {
    // Head repo is null - likely a deleted fork
    isFork = true;
    reason = "head repository deleted (was likely a fork)";
  } else if (pullRequest.head.repo.fork === true) {
    // GitHub's explicit fork flag
    isFork = true;
    reason = "head.repo.fork flag is true";
  } else if (pullRequest.head.repo.full_name !== pullRequest.base?.repo?.full_name) {
    // Different repository names
    isFork = true;
    reason = "different repository names";
  }

  return { isFork, reason };
}

module.exports = { detectForkPR };

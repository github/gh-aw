// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");
const path = require("path");
const crypto = require("crypto");
const { loadAgentOutput } = require("./load_agent_output.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { ERR_API, ERR_CONFIG, ERR_SYSTEM, ERR_VALIDATION } = require("./error_codes.cjs");
const { normalizeBranchName } = require("./normalize_branch_name.cjs");
const { readGuardedFile, writeGuardedFile } = require("./symlink_guard.cjs");
const { main: authorizeExecutorBatch, recordExecutorOutcome } = require("./safe_output_authorization_gate.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { parseStrictJSON } = require("./work_queue_codec.cjs");

/**
 * @typedef {{ type: string, path?: string, fileName: string, sha: string, size: number, targetFileName: string, url?: string }} UploadAssetItem
 */

/**
 * @param {string} githubServer
 * @param {string} repo
 * @param {string} branchName
 * @param {string} targetFileName
 * @returns {string}
 */
function buildAssetUrl(githubServer, repo, branchName, targetFileName) {
  try {
    const serverHostname = new URL(githubServer).hostname;
    if (serverHostname === "github.com") {
      return `https://github.com/${repo}/blob/${branchName}/${targetFileName}?raw=true`;
    }
  } catch {
    // Fall through to the GHES-compatible raw URL.
  }
  return `${githubServer}/${repo}/raw/${branchName}/${targetFileName}`;
}

async function main() {
  const descriptor = process.env.GH_AW_SAFE_OUTPUT_AUTHORIZATION_GATE ? parseStrictJSON(process.env.GH_AW_SAFE_OUTPUT_AUTHORIZATION_GATE) : undefined;
  const isStaged = isStagedMode(descriptor);

  if (!isStaged && process.env.GH_AW_ASSET_GIT_AUTH_REQUIRED === "true" && (!process.env.GH_AW_ASSET_GIT_TOKEN || /[\r\n]/.test(process.env.GH_AW_ASSET_GIT_TOKEN))) {
    core.setFailed(`${ERR_CONFIG}: Asset publication credential is required`);
    return;
  }

  // Get the branch name from environment variable (required)
  const branchName = process.env.GH_AW_ASSETS_BRANCH;
  if (!branchName || typeof branchName !== "string") {
    core.setFailed(`${ERR_CONFIG}: GH_AW_ASSETS_BRANCH environment variable is required but not set`);
    return;
  }

  // Normalize the branch name to ensure it's a valid git branch name
  const normalizedBranchName = normalizeBranchName(branchName);
  core.info(`Using assets branch: ${normalizedBranchName}`);

  const result = loadAgentOutput();
  if (!result.success) {
    core.setOutput("upload_count", "0");
    core.setOutput("branch_name", normalizedBranchName);
    return;
  }

  // Find all upload-asset items
  /** @type {UploadAssetItem[]} */
  const uploadItems = result.items.filter(item => item.type === "upload_asset");

  if (uploadItems.length === 0) {
    core.info("No upload-asset items found in agent output");
    core.setOutput("upload_count", "0");
    core.setOutput("branch_name", normalizedBranchName);
    return;
  }

  core.info(`Found ${uploadItems.length} upload-asset item(s)`);

  // Read the staged-assets directory directly. The upload_assets job's
  // download-artifact step writes the safe-outputs assets artifact to this exact
  // directory, and the Go generator passes the same path via GH_AW_ASSETS_DIR, so
  // producer and consumer can never disagree on the location. The literal fallback
  // matches constants.TmpGhAwAssetsDir for robustness if the env var is unset.
  const assetsDir = process.env.GH_AW_ASSETS_DIR || "/tmp/gh-aw/safeoutputs/assets";
  core.info(`Reading staged assets from: ${assetsDir}`);
  let uploadCount = 0;
  let missingAssetCount = 0;
  let hasChanges = false;
  const processedAssets = [];
  const authorizationRecords = [];
  let authorization;
  let operationStatus = "failed";

  try {
    // Check if orphaned branch already exists, if not create it
    try {
      await exec.exec("git", ["rev-parse", "--verify", `origin/${normalizedBranchName}`]);
      await exec.exec("git", ["checkout", "-B", normalizedBranchName, `origin/${normalizedBranchName}`]);
      core.info(`Checked out existing branch from origin: ${normalizedBranchName}`);
    } catch (originError) {
      // Validate that branch starts with "assets/" prefix before creating orphaned branch
      if (!normalizedBranchName.startsWith("assets/")) {
        core.setFailed(
          `Branch '${normalizedBranchName}' does not start with the required 'assets/' prefix. ` +
            `Orphaned branches can only be automatically created under the 'assets/' prefix. ` +
            `Please create the branch manually first, or use a branch name starting with 'assets/'.`
        );
        return;
      }

      // Branch doesn't exist on origin and has valid prefix, create orphaned branch
      core.info(`Creating new orphaned branch: ${normalizedBranchName}`);
      await exec.exec("git", ["checkout", "--orphan", normalizedBranchName]);
      await exec.exec("git", ["rm", "-rf", "."]);
      await exec.exec("git", ["clean", "-fdx"]);
    }

    // Process each asset
    for (const asset of uploadItems) {
      const declaredPath = typeof asset.path === "string" ? asset.path : "";
      const pathFileName = declaredPath ? path.basename(declaredPath) : "";
      const rawFileName = typeof asset.fileName === "string" ? asset.fileName : pathFileName;
      const fileName = path.basename(rawFileName);

      if (!fileName || (asset.fileName && asset.fileName !== fileName)) {
        core.setFailed(`${ERR_VALIDATION}: Invalid asset filename: ${JSON.stringify(asset)}`);
        return;
      }
      if (!pathFileName && (!asset.sha || !asset.targetFileName)) {
        core.setFailed(`${ERR_VALIDATION}: Invalid asset entry missing required fields: ${JSON.stringify(asset)}`);
        return;
      }

      // Check if file exists in the staged-assets directory
      const stagedFileName = pathFileName ? `${crypto.createHash("sha256").update(declaredPath).digest("hex")}${path.extname(fileName).toLowerCase()}` : fileName;
      const assetSourcePath = path.join(assetsDir, stagedFileName);
      let fileContent;
      try {
        const maxSizeKB = process.env.GH_AW_ASSETS_MAX_SIZE_KB ? Number(process.env.GH_AW_ASSETS_MAX_SIZE_KB) : 10240;
        fileContent = readGuardedFile(assetSourcePath, maxSizeKB * 1024);
      } catch (error) {
        if (!error || typeof error !== "object" || !("code" in error) || error.code !== "ENOENT") throw error;
        core.warning(`${ERR_SYSTEM}: Asset file not found: ${assetSourcePath} — skipping`);
        missingAssetCount++;
        continue;
      }

      // Verify SHA matches
      const computedSha = crypto.createHash("sha256").update(fileContent).digest("hex");

      if (asset.sha && computedSha !== asset.sha) {
        core.setFailed(`${ERR_VALIDATION}: SHA mismatch for ${fileName}: expected ${asset.sha}, got ${computedSha}`);
        return;
      }

      const generatedTargetFileName = `${computedSha}${path.extname(fileName).toLowerCase()}`;
      // In path mode, the source path and content are re-derived from trusted staged state,
      // so always compute the target filename server-side and ignore any agent-supplied value.
      const targetFileName = pathFileName ? generatedTargetFileName : asset.targetFileName || generatedTargetFileName;
      if (targetFileName !== path.basename(targetFileName)) {
        core.setFailed(`${ERR_VALIDATION}: Invalid asset target filename: ${targetFileName}`);
        return;
      }

      const size = fileContent.length;
      const githubServer = process.env.GITHUB_SERVER_URL || "https://github.com";
      const repo = process.env.GITHUB_REPOSITORY || "owner/repo";
      const url = buildAssetUrl(githubServer, repo, normalizedBranchName, targetFileName);
      const processedAsset = { fileName, sha: computedSha, size, targetFileName, url };

      // Check if file already exists in the branch
      if (fs.existsSync(targetFileName)) {
        let existingContent;
        try {
          existingContent = readGuardedFile(targetFileName, fileContent.length);
        } catch (error) {
          const errorCode = error && typeof error === "object" && "code" in error && error.code === ERR_VALIDATION ? ERR_VALIDATION : ERR_SYSTEM;
          core.setFailed(`${errorCode}: Failed to verify existing asset ${targetFileName}: ${getErrorMessage(error)}`);
          return;
        }
        if (!existingContent.equals(fileContent)) {
          core.setFailed(`${ERR_VALIDATION}: Existing asset content differs from staged content: ${targetFileName}`);
          return;
        }
        core.info(`Asset ${targetFileName} already exists, skipping`);
        processedAssets.push(processedAsset);
        continue;
      }

      // Copy file to branch with target filename
      try {
        writeGuardedFile(targetFileName, fileContent, { mode: 0o644, directoryMode: 0o755 });

        // Add to git
        await exec.exec("git", ["add", targetFileName]);

        uploadCount++;
        hasChanges = true;
        processedAssets.push(processedAsset);
        authorizationRecords.push({
          index: result.items.indexOf(asset),
          inputs: { repository: repo, branch: normalizedBranchName, targetFileName, sha256: computedSha, size },
          artifacts: [{ path: targetFileName, digest: computedSha, size }],
        });

        core.info(`Added asset: ${targetFileName} (${size} bytes)`);
      } catch (error) {
        core.setFailed(`${ERR_API}: Failed to process asset ${fileName}: ${getErrorMessage(error)}`);
        return;
      }
    }

    if (uploadCount === 0 && missingAssetCount > 0 && missingAssetCount === uploadItems.length) {
      core.setFailed(`All ${missingAssetCount} declared assets were missing; no assets published.`);
      return;
    }

    // Commit and push if there are changes (skip if staged)
    if (hasChanges) {
      if (process.env.GH_AW_SAFE_OUTPUT_AUTHORIZATION_GATE) {
        authorization = await authorizeExecutorBatch({ core, descriptor: { ...descriptor, staged: isStaged }, batch: result.items, records: authorizationRecords });
        if (!isStaged && authorization.state !== "allowed") throw new Error("Safe-output authorization did not release asset publication");
      }
      const commitMessage = `[skip-ci] Add ${uploadCount} asset(s)`;
      await exec.exec(`git`, [`commit`, `-m`, commitMessage]);
      if (!isStaged) {
        const maxPushAttempts = 3;
        for (let attempt = 1; attempt <= maxPushAttempts; attempt++) {
          operationStatus = "indeterminate";
          const pushResult = await exec.getExecOutput("git", ["push", "--porcelain", "origin", normalizedBranchName], { ignoreReturnCode: true });
          if (pushResult.exitCode === 0) {
            break;
          }
          const pushError = [pushResult.stdout, pushResult.stderr].filter(Boolean).join("\n").trim() || `git push exited with code ${pushResult.exitCode}`;
          const isNonFastForward = /non-fast-forward|fetch first/i.test(pushError);
          if (!isNonFastForward || attempt === maxPushAttempts) {
            throw new Error(pushError);
          }
          core.warning(`Asset push attempt ${attempt}/${maxPushAttempts} was rejected because the branch changed; rebasing onto the latest ${normalizedBranchName} branch before retrying`);
          const remoteBranch = `refs/remotes/origin/${normalizedBranchName}`;
          await exec.exec("git", ["fetch", "--no-tags", "origin", `+refs/heads/${normalizedBranchName}:${remoteBranch}`]);
          try {
            await exec.exec("git", ["rebase", remoteBranch]);
          } catch (rebaseError) {
            await exec.exec("git", ["rebase", "--abort"], { ignoreReturnCode: true });
            throw rebaseError;
          }
        }
        operationStatus = "confirmed";
        core.info(`Successfully uploaded ${uploadCount} assets to branch ${normalizedBranchName}`);
      }

      const status = isStaged
        ? `🎭 Staged mode: **${uploadCount}** asset${uploadCount === 1 ? "" : "s"} ready for publication to branch \`${normalizedBranchName}\` (not pushed).`
        : `Successfully uploaded **${uploadCount}** asset${uploadCount === 1 ? "" : "s"} to branch \`${normalizedBranchName}\`.`;
      const assetList = processedAssets.map(asset => `- [\`${asset.fileName}\`](${asset.url}) → \`${asset.targetFileName}\` (${asset.size} bytes)`).join("\n");
      await core.summary.addRaw(`### Assets\n\n${status}\n\n<details>\n<summary>View ${processedAssets.length} asset${processedAssets.length === 1 ? "" : "s"}</summary>\n\n${assetList}\n\n</details>\n`).write();
    } else {
      core.info("No new assets to upload");
    }
  } catch (error) {
    core.setFailed(`${ERR_API}: Failed to upload assets: ${getErrorMessage(error)}`);
    return;
  } finally {
    if (authorization?.state === "allowed") recordExecutorOutcome(authorization, operationStatus, core);
  }

  core.setOutput("upload_count", uploadCount.toString());
  core.setOutput("branch_name", normalizedBranchName);
}

module.exports = { main };

// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Secret Validation Script
 *
 * Tests each known secret in the repository and generates a diagnostic report
 * about their configuration status and availability.
 *
 * This script is part of the agentic maintenance workflow and validates:
 * - GitHub tokens (REST and GraphQL API access)
 * - AI Engine API keys (Anthropic, OpenAI)
 * - Integration tokens (Brave Search, Notion)
 */

const https = require("https");
const { promisify } = require("util");
const { exec } = require("child_process");
const execAsync = promisify(exec);
const { getErrorMessage } = require("./error_helpers.cjs");

/**
 * Test result status
 */
const Status = {
  SUCCESS: "success",
  FAILURE: "failure",
  NOT_SET: "not_set",
  SKIPPED: "skipped",
};

/**
 * Secret documentation URLs
 */
const SECRET_DOCS = {
  GH_AW_GITHUB_TOKEN: "https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens",
  GH_AW_GITHUB_MCP_SERVER_TOKEN: "https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens",
  GH_AW_PROJECT_GITHUB_TOKEN: "https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens",
  GH_AW_COPILOT_TOKEN: "https://docs.github.com/en/copilot",
  ANTHROPIC_API_KEY: "https://docs.anthropic.com/en/api/getting-started",
  OPENAI_API_KEY: "https://platform.openai.com/docs/api-reference/auth",
  BRAVE_API_KEY: "https://brave.com/search/api/",
  NOTION_API_TOKEN: "https://developers.notion.com/docs/authorization",
};

/**
 * Make an HTTPS request
 * @param {string} hostname
 * @param {string} path
 * @param {Object} headers
 * @param {string} method
 * @returns {Promise<{statusCode: number, data: string}>}
 */
function makeRequest(hostname, path, headers, method = "GET") {
  return new Promise((resolve, reject) => {
    const options = {
      hostname,
      path,
      method,
      headers,
    };

    const req = https.request(options, res => {
      let data = "";
      res.on("data", chunk => {
        data += chunk;
      });
      res.on("end", () => {
        resolve({ statusCode: res.statusCode || 0, data });
      });
    });

    req.on("error", err => {
      reject(err);
    });

    req.setTimeout(10000, () => {
      req.destroy();
      reject(new Error("Request timeout"));
    });

    req.end();
  });
}

/**
 * Test GitHub token with REST API
 * @param {string | undefined} token
 * @param {string} owner
 * @param {string} repo
 * @returns {Promise<{status: string, message: string, details?: any}>}
 */
async function testGitHubRESTAPI(token, owner, repo) {
  if (!token) {
    return { status: Status.NOT_SET, message: "Token not set" };
  }

  try {
    const result = await makeRequest("api.github.com", `/repos/${owner}/${repo}`, {
      "User-Agent": "gh-aw-secret-validation",
      Authorization: `Bearer ${token}`,
      Accept: "application/vnd.github+json",
      "X-GitHub-Api-Version": "2022-11-28",
    });

    if (result.statusCode === 200) {
      const data = JSON.parse(result.data);
      return {
        status: Status.SUCCESS,
        message: "REST API access successful",
        details: {
          statusCode: result.statusCode,
          repoName: data.full_name,
          repoPrivate: data.private,
        },
      };
    } else {
      return {
        status: Status.FAILURE,
        message: `REST API returned status ${result.statusCode}`,
        details: { statusCode: result.statusCode },
      };
    }
  } catch (error) {
    const errorMessage = error instanceof Error ? error.message : String(error);
    return {
      status: Status.FAILURE,
      message: `REST API error: ${errorMessage}`,
      details: { error: errorMessage },
    };
  }
}

/**
 * Test GitHub token with GraphQL API
 * @param {string | undefined} token
 * @param {string} owner
 * @param {string} repo
 * @returns {Promise<{status: string, message: string, details?: any}>}
 */
async function testGitHubGraphQLAPI(token, owner, repo) {
  if (!token) {
    return { status: Status.NOT_SET, message: "Token not set" };
  }

  const query = `
    query {
      repository(owner: "${owner}", name: "${repo}") {
        name
        owner {
          login
        }
        isPrivate
      }
    }
  `;

  try {
    const result = await new Promise((resolve, reject) => {
      const postData = JSON.stringify({ query });
      const options = {
        hostname: "api.github.com",
        path: "/graphql",
        method: "POST",
        headers: {
          "User-Agent": "gh-aw-secret-validation",
          Authorization: `Bearer ${token}`,
          "Content-Type": "application/json",
          "Content-Length": Buffer.byteLength(postData),
        },
      };

      const req = https.request(options, res => {
        let data = "";
        res.on("data", chunk => {
          data += chunk;
        });
        res.on("end", () => {
          resolve({ statusCode: res.statusCode || 0, data });
        });
      });

      req.on("error", reject);
      req.setTimeout(10000, () => {
        req.destroy();
        reject(new Error("Request timeout"));
      });
      req.write(postData);
      req.end();
    });

    if (result.statusCode === 200) {
      const data = JSON.parse(result.data);
      if (data.errors) {
        return {
          status: Status.FAILURE,
          message: "GraphQL query returned errors",
          details: { errors: data.errors },
        };
      }
      return {
        status: Status.SUCCESS,
        message: "GraphQL API access successful",
        details: {
          statusCode: result.statusCode,
          repoName: data.data?.repository?.name,
          repoPrivate: data.data?.repository?.isPrivate,
        },
      };
    } else {
      return {
        status: Status.FAILURE,
        message: `GraphQL API returned status ${result.statusCode}`,
        details: { statusCode: result.statusCode },
      };
    }
  } catch (error) {
    const errorMessage = error instanceof Error ? error.message : String(error);
    return {
      status: Status.FAILURE,
      message: `GraphQL API error: ${errorMessage}`,
      details: { error: errorMessage },
    };
  }
}

/**
 * Test Copilot CLI availability
 * @param {string | undefined} token
 * @returns {Promise<{status: string, message: string, details?: any}>}
 */
async function testCopilotCLI(token) {
  if (!token) {
    return { status: Status.NOT_SET, message: "Token not set" };
  }

  try {
    // Check if copilot CLI is installed
    const { stdout, stderr } = await execAsync('which copilot 2>/dev/null || echo ""');
    if (!stdout.trim()) {
      return {
        status: Status.SKIPPED,
        message: "Copilot CLI not installed (skipped)",
        details: { note: "Install @github/copilot to test" },
      };
    }

    return {
      status: Status.SUCCESS,
      message: "Copilot CLI is available",
      details: { cliPath: stdout.trim() },
    };
  } catch (error) {
    return {
      status: Status.SKIPPED,
      message: "Copilot CLI check skipped",
      details: { note: "Install @github/copilot to test" },
    };
  }
}

/**
 * Test Anthropic API
 * @param {string | undefined} apiKey
 * @returns {Promise<{status: string, message: string, details?: any}>}
 */
async function testAnthropicAPI(apiKey) {
  if (!apiKey) {
    return { status: Status.NOT_SET, message: "API key not set" };
  }

  try {
    // Test with a minimal API call to check authentication
    const result = await new Promise((resolve, reject) => {
      const postData = JSON.stringify({
        model: "claude-3-haiku-20240307",
        max_tokens: 1,
        messages: [{ role: "user", content: "test" }],
      });

      const options = {
        hostname: "api.anthropic.com",
        path: "/v1/messages",
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "x-api-key": apiKey,
          "anthropic-version": "2023-06-01",
          "Content-Length": Buffer.byteLength(postData),
        },
      };

      const req = https.request(options, res => {
        let data = "";
        res.on("data", chunk => {
          data += chunk;
        });
        res.on("end", () => {
          resolve({ statusCode: res.statusCode || 0, data });
        });
      });

      req.on("error", reject);
      req.setTimeout(10000, () => {
        req.destroy();
        reject(new Error("Request timeout"));
      });
      req.write(postData);
      req.end();
    });

    if (result.statusCode === 200) {
      return {
        status: Status.SUCCESS,
        message: "Anthropic API access successful",
        details: { statusCode: result.statusCode },
      };
    } else if (result.statusCode === 401) {
      return {
        status: Status.FAILURE,
        message: "Invalid Anthropic API key",
        details: { statusCode: result.statusCode },
      };
    } else {
      return {
        status: Status.FAILURE,
        message: `Anthropic API returned status ${result.statusCode}`,
        details: { statusCode: result.statusCode },
      };
    }
  } catch (error) {
    const errorMessage = error instanceof Error ? error.message : String(error);
    return {
      status: Status.FAILURE,
      message: `Anthropic API error: ${errorMessage}`,
      details: { error: errorMessage },
    };
  }
}

/**
 * Test OpenAI API
 * @param {string | undefined} apiKey
 * @returns {Promise<{status: string, message: string, details?: any}>}
 */
async function testOpenAIAPI(apiKey) {
  if (!apiKey) {
    return { status: Status.NOT_SET, message: "API key not set" };
  }

  try {
    // Test with models endpoint which is lightweight
    const result = await makeRequest("api.openai.com", "/v1/models", {
      Authorization: `Bearer ${apiKey}`,
    });

    if (result.statusCode === 200) {
      return {
        status: Status.SUCCESS,
        message: "OpenAI API access successful",
        details: { statusCode: result.statusCode },
      };
    } else if (result.statusCode === 401) {
      return {
        status: Status.FAILURE,
        message: "Invalid OpenAI API key",
        details: { statusCode: result.statusCode },
      };
    } else {
      return {
        status: Status.FAILURE,
        message: `OpenAI API returned status ${result.statusCode}`,
        details: { statusCode: result.statusCode },
      };
    }
  } catch (error) {
    const errorMessage = error instanceof Error ? error.message : String(error);
    return {
      status: Status.FAILURE,
      message: `OpenAI API error: ${errorMessage}`,
      details: { error: errorMessage },
    };
  }
}

/**
 * Test Brave Search API
 * @param {string | undefined} apiKey
 * @returns {Promise<{status: string, message: string, details?: any}>}
 */
async function testBraveSearchAPI(apiKey) {
  if (!apiKey) {
    return { status: Status.NOT_SET, message: "API key not set" };
  }

  try {
    const result = await makeRequest("api.search.brave.com", "/res/v1/web/search?q=test&count=1", {
      Accept: "application/json",
      "X-Subscription-Token": apiKey,
    });

    if (result.statusCode === 200) {
      return {
        status: Status.SUCCESS,
        message: "Brave Search API access successful",
        details: { statusCode: result.statusCode },
      };
    } else if (result.statusCode === 401 || result.statusCode === 403) {
      return {
        status: Status.FAILURE,
        message: "Invalid Brave Search API key",
        details: { statusCode: result.statusCode },
      };
    } else {
      return {
        status: Status.FAILURE,
        message: `Brave Search API returned status ${result.statusCode}`,
        details: { statusCode: result.statusCode },
      };
    }
  } catch (error) {
    const errorMessage = error instanceof Error ? error.message : String(error);
    return {
      status: Status.FAILURE,
      message: `Brave Search API error: ${errorMessage}`,
      details: { error: errorMessage },
    };
  }
}

/**
 * Test Notion API
 * @param {string | undefined} token
 * @returns {Promise<{status: string, message: string, details?: any}>}
 */
async function testNotionAPI(token) {
  if (!token) {
    return { status: Status.NOT_SET, message: "Token not set" };
  }

  try {
    // Test with users/me endpoint
    const result = await makeRequest("api.notion.com", "/v1/users/me", {
      Authorization: `Bearer ${token}`,
      "Notion-Version": "2022-06-28",
    });

    if (result.statusCode === 200) {
      return {
        status: Status.SUCCESS,
        message: "Notion API access successful",
        details: { statusCode: result.statusCode },
      };
    } else if (result.statusCode === 401) {
      return {
        status: Status.FAILURE,
        message: "Invalid Notion API token",
        details: { statusCode: result.statusCode },
      };
    } else {
      return {
        status: Status.FAILURE,
        message: `Notion API returned status ${result.statusCode}`,
        details: { statusCode: result.statusCode },
      };
    }
  } catch (error) {
    const errorMessage = error instanceof Error ? error.message : String(error);
    return {
      status: Status.FAILURE,
      message: `Notion API error: ${errorMessage}`,
      details: { error: errorMessage },
    };
  }
}

/**
 * Format status emoji
 * @param {string} status
 * @returns {string}
 */
function statusEmoji(status) {
  switch (status) {
    case Status.SUCCESS:
      return "✅";
    case Status.FAILURE:
      return "❌";
    case Status.NOT_SET:
      return "⚪";
    case Status.SKIPPED:
      return "⏭️";
    default:
      return "❓";
  }
}

/**
 * Generate markdown diagnostic report
 * @param {Array} results
 * @returns {string}
 */
function generateMarkdownReport(results) {
  const timestamp = new Date().toISOString();
  const repository = process.env.GITHUB_REPOSITORY || "unknown";

  let report = `## 📊 Summary\n\n`;
  report += `**Generated:** ${timestamp} | **Repository:** ${repository}\n\n`;

  const summary = {
    total: results.length,
    success: results.filter(r => r.status === Status.SUCCESS).length,
    failure: results.filter(r => r.status === Status.FAILURE).length,
    notSet: results.filter(r => r.status === Status.NOT_SET).length,
    skipped: results.filter(r => r.status === Status.SKIPPED).length,
  };

  // Create a summary table
  report += `| Status | Count | Percentage |\n`;
  report += `|--------|-------|------------|\n`;
  report += `| ✅ Successful | ${summary.success} | ${Math.round((summary.success / summary.total) * 100)}% |\n`;
  report += `| ❌ Failed | ${summary.failure} | ${Math.round((summary.failure / summary.total) * 100)}% |\n`;
  report += `| ⚪ Not Set | ${summary.notSet} | ${Math.round((summary.notSet / summary.total) * 100)}% |\n`;
  if (summary.skipped > 0) {
    report += `| ⏭️ Skipped | ${summary.skipped} | ${Math.round((summary.skipped / summary.total) * 100)}% |\n`;
  }
  report += `| **Total** | **${summary.total}** | **100%** |\n\n`;

  // Add recommendations section early with callouts
  const notSetSecrets = [...new Set(results.filter(r => r.status === Status.NOT_SET).map(r => r.secret))];
  const failedSecrets = [...new Set(results.filter(r => r.status === Status.FAILURE).map(r => r.secret))];

  if (notSetSecrets.length === 0 && failedSecrets.length === 0) {
    report += `> [!TIP]\n`;
    report += `> ✅ All configured secrets are working correctly!\n\n`;
  } else {
    if (failedSecrets.length > 0) {
      report += `> [!WARNING]\n`;
      report += `> **Failed Tests:** ${failedSecrets.length} secret(s) failed validation\n`;
      report += `>\n`;
      failedSecrets.forEach(secret => {
        const docsLink = SECRET_DOCS[secret] || "https://docs.github.com";
        report += `> - [\`${secret}\`](${docsLink})\n`;
      });
      report += `>\n`;
      report += `> Review the secret values and ensure they have proper permissions.\n\n`;
    }

    if (notSetSecrets.length > 0) {
      report += `> [!NOTE]\n`;
      report += `> **Not Configured:** ${notSetSecrets.length} secret(s) not set\n`;
      report += `>\n`;
      notSetSecrets.forEach(secret => {
        const docsLink = SECRET_DOCS[secret] || "https://docs.github.com";
        report += `> - [\`${secret}\`](${docsLink})\n`;
      });
      report += `>\n`;
      report += `> Configure these secrets in [repository settings](https://github.com/${repository}/settings/secrets/actions) if needed.\n\n`;
    }
  }

  report += `---\n\n`;
  report += `## 🔍 Detailed Results\n\n`;

  // Group by secret
  const bySecret = {};
  results.forEach(result => {
    if (!bySecret[result.secret]) {
      bySecret[result.secret] = [];
    }
    bySecret[result.secret].push(result);
  });

  Object.entries(bySecret).forEach(([secret, tests]) => {
    // Determine overall status for the secret
    const hasFailure = tests.some(t => t.status === Status.FAILURE);
    const hasNotSet = tests.some(t => t.status === Status.NOT_SET);

    let statusIcon = "✅";
    if (hasFailure) statusIcon = "❌";
    else if (hasNotSet) statusIcon = "⚪";
    else if (tests.some(t => t.status === Status.SKIPPED)) statusIcon = "⏭️";

    // Add documentation link for the secret
    const docsLink = SECRET_DOCS[secret] || "https://docs.github.com";

    // Use collapsible sections for each secret
    report += `<details>\n`;
    report += `<summary><strong>${statusIcon} <a href="${docsLink}">${secret}</a></strong> (${tests.length} test${tests.length > 1 ? "s" : ""})</summary>\n\n`;

    tests.forEach(test => {
      report += `### ${statusEmoji(test.status)} ${test.test}\n\n`;
      report += `**Status:** ${test.status} | **Message:** ${test.message}\n\n`;

      if (test.details) {
        report += `<details>\n`;
        report += `<summary>View details</summary>\n\n`;
        report += `\`\`\`json\n`;
        report += `${JSON.stringify(test.details, null, 2)}\n`;
        report += `\`\`\`\n\n`;
        report += `</details>\n\n`;
      }
    });

    report += `</details>\n\n`;
  });

  return report;
}

/**
 * Main validation function
 */
async function main() {
  try {
    core.info("Starting secret validation...");

    const owner = context.repo.owner;
    const repo = context.repo.repo;

    const results = [];

    // Test GH_AW_GITHUB_TOKEN
    core.info("Testing GH_AW_GITHUB_TOKEN...");
    const ghAwToken = process.env.GH_AW_GITHUB_TOKEN;
    const restResult = await testGitHubRESTAPI(ghAwToken, owner, repo);
    results.push({
      secret: "GH_AW_GITHUB_TOKEN",
      test: "GitHub REST API",
      ...restResult,
    });
    core.info(`  ${statusEmoji(restResult.status)} ${restResult.message}`);

    const graphqlResult = await testGitHubGraphQLAPI(ghAwToken, owner, repo);
    results.push({
      secret: "GH_AW_GITHUB_TOKEN",
      test: "GitHub GraphQL API",
      ...graphqlResult,
    });
    core.info(`  ${statusEmoji(graphqlResult.status)} ${graphqlResult.message}`);

    // Test GH_AW_GITHUB_MCP_SERVER_TOKEN
    core.info("Testing GH_AW_GITHUB_MCP_SERVER_TOKEN...");
    const mcpToken = process.env.GH_AW_GITHUB_MCP_SERVER_TOKEN;
    const mcpRestResult = await testGitHubRESTAPI(mcpToken, owner, repo);
    results.push({
      secret: "GH_AW_GITHUB_MCP_SERVER_TOKEN",
      test: "GitHub REST API",
      ...mcpRestResult,
    });
    core.info(`  ${statusEmoji(mcpRestResult.status)} ${mcpRestResult.message}`);

    // Test GH_AW_PROJECT_GITHUB_TOKEN
    core.info("Testing GH_AW_PROJECT_GITHUB_TOKEN...");
    const projectToken = process.env.GH_AW_PROJECT_GITHUB_TOKEN;
    const projectRestResult = await testGitHubRESTAPI(projectToken, owner, repo);
    results.push({
      secret: "GH_AW_PROJECT_GITHUB_TOKEN",
      test: "GitHub REST API",
      ...projectRestResult,
    });
    core.info(`  ${statusEmoji(projectRestResult.status)} ${projectRestResult.message}`);

    // Test GH_AW_COPILOT_TOKEN
    core.info("Testing GH_AW_COPILOT_TOKEN...");
    const copilotToken = process.env.GH_AW_COPILOT_TOKEN;
    const copilotResult = await testCopilotCLI(copilotToken);
    results.push({
      secret: "GH_AW_COPILOT_TOKEN",
      test: "Copilot CLI Availability",
      ...copilotResult,
    });
    core.info(`  ${statusEmoji(copilotResult.status)} ${copilotResult.message}`);

    // Test ANTHROPIC_API_KEY
    core.info("Testing ANTHROPIC_API_KEY...");
    const anthropicKey = process.env.ANTHROPIC_API_KEY;
    const anthropicResult = await testAnthropicAPI(anthropicKey);
    results.push({
      secret: "ANTHROPIC_API_KEY",
      test: "Anthropic Claude API",
      ...anthropicResult,
    });
    core.info(`  ${statusEmoji(anthropicResult.status)} ${anthropicResult.message}`);

    // Test OPENAI_API_KEY
    core.info("Testing OPENAI_API_KEY...");
    const openaiKey = process.env.OPENAI_API_KEY;
    const openaiResult = await testOpenAIAPI(openaiKey);
    results.push({
      secret: "OPENAI_API_KEY",
      test: "OpenAI API",
      ...openaiResult,
    });
    core.info(`  ${statusEmoji(openaiResult.status)} ${openaiResult.message}`);

    // Test BRAVE_API_KEY
    core.info("Testing BRAVE_API_KEY...");
    const braveKey = process.env.BRAVE_API_KEY;
    const braveResult = await testBraveSearchAPI(braveKey);
    results.push({
      secret: "BRAVE_API_KEY",
      test: "Brave Search API",
      ...braveResult,
    });
    core.info(`  ${statusEmoji(braveResult.status)} ${braveResult.message}`);

    // Test NOTION_API_TOKEN
    core.info("Testing NOTION_API_TOKEN...");
    const notionToken = process.env.NOTION_API_TOKEN;
    const notionResult = await testNotionAPI(notionToken);
    results.push({
      secret: "NOTION_API_TOKEN",
      test: "Notion API",
      ...notionResult,
    });
    core.info(`  ${statusEmoji(notionResult.status)} ${notionResult.message}`);

    // Generate markdown report
    core.info("Generating report...");
    const report = generateMarkdownReport(results);

    // Write to file for artifact upload
    const fs = require("fs");
    fs.writeFileSync("secret-validation-report.md", report);
    core.info("✅ Report written to secret-validation-report.md");

    // Write to step summary
    await core.summary.addHeading("🔐 Secret Validation Report", 2).addRaw(report).write();

    // Log summary
    const failures = results.filter(r => r.status === Status.FAILURE).length;
    const notSet = results.filter(r => r.status === Status.NOT_SET).length;

    if (failures > 0) {
      core.warning(`${failures} secret(s) failed validation. Check the report for details.`);
    }
    if (notSet > 0) {
      core.info(`${notSet} secret(s) not configured. This is expected if they are not needed.`);
    }
    if (failures === 0 && notSet === 0) {
      core.info("✅ All configured secrets validated successfully!");
    }
  } catch (error) {
    core.setFailed(`Secret validation failed: ${getErrorMessage(error)}`);
    throw error;
  }
}

module.exports = {
  main,
  testGitHubRESTAPI,
  testGitHubGraphQLAPI,
  testCopilotCLI,
  testAnthropicAPI,
  testOpenAIAPI,
  testBraveSearchAPI,
  testNotionAPI,
  generateMarkdownReport,
};

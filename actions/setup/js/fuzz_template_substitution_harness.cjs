// @ts-check
/**
 * Fuzz test harness for template substitution and interpolation
 * This file tests the interaction between:
 * 1. Placeholder substitution (substitute_placeholders.cjs)
 * 2. Variable interpolation (interpolate_prompt.cjs)
 * 3. Template rendering with conditionals (renderMarkdownTemplate)
 * 4. Different value states (undefined, null, empty, valid)
 */

const substitutePlaceholders = require("./substitute_placeholders.cjs");
const { isTruthy } = require("./is_truthy.cjs");
const fs = require("fs");
const os = require("os");
const path = require("path");
const { getErrorMessage } = require("./error_helpers.cjs");

/**
 * Simulates the template rendering logic from interpolate_prompt
 * @param {string} markdown - The markdown content to process
 * @returns {string} - The processed markdown content
 */
function renderMarkdownTemplate(markdown) {
  // First pass: Handle blocks where tags are on their own lines
  let result = markdown.replace(/(\n?)([ \t]*{{#if\s+([^}]*)}}[ \t]*\n)([\s\S]*?)([ \t]*{{\/if}}[ \t]*)(\n?)/g, (match, leadNL, openLine, cond, body, closeLine, trailNL) => {
    if (isTruthy(cond)) {
      return leadNL + body;
    } else {
      return "";
    }
  });

  // Second pass: Handle inline conditionals
  result = result.replace(/{{#if\s+([^}]*)}}([\s\S]*?){{\/if}}/g, (_, cond, body) => (isTruthy(cond) ? body : ""));

  // Clean up excessive blank lines
  result = result.replace(/\n{3,}/g, "\n\n");

  return result;
}

/**
 * Simulates variable interpolation from interpolate_prompt
 * @param {string} content - The prompt content with ${VAR} placeholders
 * @param {Record<string, string>} variables - Map of variable names to their values
 * @returns {string} - The interpolated content
 */
function interpolateVariables(content, variables) {
  let result = content;
  for (const [varName, value] of Object.entries(variables)) {
    const escapedVarName = varName.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    const pattern = new RegExp(`\\$\\{${escapedVarName}\\}`, "g");
    result = result.replace(pattern, value);
  }
  return result;
}

/**
 * Test the full pipeline: substitution -> interpolation -> template rendering
 * @param {string} template - Template with placeholders and conditionals
 * @param {Record<string, any>} substitutions - Substitution values (can include undefined/null)
 * @param {Record<string, string>} variables - Variable interpolation values
 * @returns {Promise<{result: string, error: string | null, stages: {afterSubstitution: string, afterInterpolation: string, afterTemplate: string}}>} Test result
 */
async function testTemplateSubstitution(template, substitutions, variables) {
  let tempDir;
  try {
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "fuzz-template-"));
  } catch (error) {
    throw new Error("Failed to create fuzz harness temporary directory", { cause: error });
  }
  const testFile = path.join(tempDir, "test.txt");

  try {
    // Stage 1: Write template to file
    fs.writeFileSync(testFile, template, "utf8");

    // Stage 2: Perform placeholder substitution
    await substitutePlaceholders({ file: testFile, substitutions });
    const afterSubstitution = fs.readFileSync(testFile, "utf8");

    // Stage 3: Interpolate variables
    const afterInterpolation = interpolateVariables(afterSubstitution, variables);

    // Stage 4: Render template conditionals
    const afterTemplate = renderMarkdownTemplate(afterInterpolation);

    // Clean up
    fs.rmSync(tempDir, { recursive: true, force: true });

    return {
      result: afterTemplate,
      error: null,
      stages: {
        afterSubstitution,
        afterInterpolation,
        afterTemplate,
      },
    };
  } catch (err) {
    // Clean up on error
    try {
      fs.rmSync(tempDir, { recursive: true, force: true });
    } catch {
      // Best-effort cleanup after the original error.
    }

    return {
      result: "",
      error: getErrorMessage(err),
      stages: {
        afterSubstitution: "",
        afterInterpolation: "",
        afterTemplate: "",
      },
    };
  }
}

/**
 * Test specific edge cases for value states
 * @param {any} value - The value to test (undefined, null, "", "0", "false", etc.)
 * @returns {Promise<{isTruthyResult: boolean, substitutedValue: string, templateRemoved: boolean, error: string | null}>}
 */
async function testValueState(value) {
  try {
    // Create a simple template with a conditional
    const template = `{{#if __TEST_VALUE__}}\nValue exists: __TEST_VALUE__\n{{/if}}`;

    const result = await testTemplateSubstitution(template, { TEST_VALUE: value }, {});

    if (result.error) {
      return {
        isTruthyResult: false,
        substitutedValue: "",
        templateRemoved: true,
        error: result.error,
      };
    }

    // Determine what the substituted value was
    const substitutedValue = result.stages.afterSubstitution.match(/{{#if ([^}]*)}}/)?.[1] || "";

    // Check if the template block was removed (empty result) or kept
    const templateRemoved = result.result.trim() === "";

    // Check what isTruthy returned for this value
    const isTruthyResult = isTruthy(substitutedValue);

    return {
      isTruthyResult,
      substitutedValue,
      templateRemoved,
      error: null,
    };
  } catch (err) {
    return {
      isTruthyResult: false,
      substitutedValue: "",
      templateRemoved: true,
      error: getErrorMessage(err),
    };
  }
}

// Read input from stdin for fuzzing
if (require.main === module) {
  let input = "";

  process.stdin.on("data", chunk => {
    input += chunk;
  });

  process.stdin.on("end", async () => {
    try {
      // Parse input as JSON with either testType and data
      const parsed = JSON.parse(input);

      let result;
      if (parsed.testType === "valueState") {
        result = await testValueState(parsed.value);
      } else {
        // Full pipeline test
        const { template, substitutions, variables } = parsed;
        result = await testTemplateSubstitution(template || "", substitutions || {}, variables || {});
      }

      process.stdout.write(JSON.stringify(result));
      process.exit(0);
    } catch (err) {
      const errorMsg = getErrorMessage(err);
      process.stdout.write(
        JSON.stringify({
          result: "",
          error: errorMsg,
          stages: {
            afterSubstitution: "",
            afterInterpolation: "",
            afterTemplate: "",
          },
        })
      );
      process.exit(1);
    }
  });
}

module.exports = { testTemplateSubstitution, testValueState };

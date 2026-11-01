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
const { interpolateVariables } = require("./interpolate_prompt.cjs");
const { renderMarkdownTemplate } = require("./render_template.cjs");
const { createLiteralValues, readLiteralState, readAuthorContent, writeLiteralState, literalFileKey, restoreInlineMetadata } = require("./prompt_literal_values.cjs");
const { isTruthy } = require("./is_truthy.cjs");
const fs = require("fs");
const os = require("os");
const path = require("path");
const { getErrorMessage } = require("./error_helpers.cjs");

/**
 * Test the full pipeline: substitution -> interpolation -> template rendering
 * @param {string} template - Template with placeholders and conditionals
 * @param {Record<string, any>} substitutions - Substitution values (can include undefined/null)
 * @param {Record<string, string>} variables - Variable interpolation values
 * @param {{deferredBindings?: Record<string, string>, lateSubstitutions?: Record<string, string>, metadata?: boolean}} [options]
 * @returns {Promise<{result: string, error: string | null, stages: {afterSubstitution: string, afterInterpolation: string, afterTemplate: string}}>} Test result
 */
async function testTemplateSubstitution(template, substitutions, variables, { deferredBindings = {}, lateSubstitutions = {}, metadata = false } = {}) {
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
    await substitutePlaceholders({ file: testFile, substitutions, deferredBindings });
    const afterSubstitution = fs.readFileSync(testFile, "utf8");

    // Follow the production provenance boundary instead of parsing restored values.
    const literals = createLiteralValues(readLiteralState(testFile) ?? undefined);
    const authored = readAuthorContent(testFile, testFile, literals.state).content;
    const protectedVariables = Object.fromEntries(Object.entries(variables).map(([key, value]) => [key, literals.protect(value)]));
    const interpolated = interpolateVariables(authored, protectedVariables);
    const afterInterpolation = literals.restore(interpolated);

    const opaque = renderMarkdownTemplate(interpolated, literals.restore);
    const afterTemplate = metadata ? restoreInlineMetadata(opaque, literals.restore) : literals.restore(opaque);
    fs.writeFileSync(testFile, afterTemplate, "utf8");
    literals.state.files[literalFileKey(testFile, testFile)] = { opaque, rendered: afterTemplate, metadata };
    writeLiteralState(testFile, literals.state);
    await substitutePlaceholders({ file: testFile, substitutions: lateSubstitutions });
    const result = fs.readFileSync(testFile, "utf8");

    // Clean up
    fs.rmSync(tempDir, { recursive: true, force: true });

    return {
      result,
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
        const { template, substitutions, variables, options } = parsed;
        result = await testTemplateSubstitution(template || "", substitutions || {}, variables || {}, options);
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

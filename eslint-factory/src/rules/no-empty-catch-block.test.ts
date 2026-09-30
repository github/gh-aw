import { RuleTester } from "eslint";
import { describe, expect, it } from "vitest";
import { noEmptyCatchBlockRule } from "./no-empty-catch-block";

const ruleTester = new RuleTester({
  languageOptions: {
    ecmaVersion: 2022,
    sourceType: "commonjs",
  },
});

describe("no-empty-catch-block", () => {
  it("uses the correct docs URL", () => {
    expect(noEmptyCatchBlockRule.meta.docs.url).toBe("https://github.com/github/gh-aw/tree/main/eslint-factory#no-empty-catch-block");
  });

  it("accepts catch blocks that log, assign a fallback, or document intent", () => {
    ruleTester.run("no-empty-catch-block", noEmptyCatchBlockRule, {
      valid: [
        `try { risky(); } catch (err) { core.debug(getErrorMessage(err)); }`,
        `try { value = JSON.parse(raw); } catch { value = {}; }`,
        `try { risky(); } catch { /* best-effort cleanup */ }`,
        `try { risky(); } catch { /* best effort cleanup */ }`,
        `try { cleanup(); } catch {\n  // Cleanup failure is non-fatal.\n}`,
        `try { parse(); } catch (_parseError) {\n  // aw_context is not valid JSON – ignore and fall through\n}`,
        `try { close(); } catch { /* safe to ignore: stream may already be closed */ }`,
        `try { optional(); } catch { /* swallowed because feature probing can fail */ }`,
        `try { remove(); } catch { /* no-op when cache file is absent */ }`,
        `try { risky(); } catch (err) { throw err; }`,
        `try { close(); } catch { /* Closing after fsync must not turn a durable append into a reported failure. */ }`,
        `try { close(); } catch { /* Closing after the directory sync must not reject a published shard. */ }`,
        `try { risky(); } catch (err) {\n  // intentional no-op: file may not exist on first run\n}`,
        `try { risky(); } catch { /* intentional ignore: optional file is absent */ }`,
        `run().catch((err) => { core.warning(getErrorMessage(err)); });`,
        `run().catch(function (err) { core.warning(getErrorMessage(err)); });`,
        `run().catch(() => { /* best-effort cleanup */ });`,
        `// Non-fatal: errors are silently swallowed.\nif (require.main === module) {\n  run().catch(() => {});\n}`,
      ],
      invalid: [],
    });
  });

  it("reports catch blocks with no statements and no explanatory comment", () => {
    ruleTester.run("no-empty-catch-block", noEmptyCatchBlockRule, {
      valid: [],
      invalid: [
        {
          code: `try { risky(); } catch {}`,
          errors: [{ messageId: "noEmptyCatch" }],
        },
        {
          code: `try { risky(); } catch (err) {}`,
          errors: [{ messageId: "noEmptyCatch" }],
        },
        {
          code: `try { risky(); } catch (err) {\n\n}`,
          errors: [{ messageId: "noEmptyCatch" }],
        },
        {
          code: `try { risky(); } catch { /* TODO */ }`,
          errors: [{ messageId: "noEmptyCatch" }],
        },
        {
          code: `try { risky(); } catch { /* file processing failed */ }`,
          errors: [{ messageId: "noEmptyCatch" }],
        },
        {
          code: `try { risky(); } catch { /* eslint-ignore */ }`,
          errors: [{ messageId: "noEmptyCatch" }],
        },
        {
          code: `try { risky(); } catch { /* do not ignore this */ }`,
          errors: [{ messageId: "noEmptyCatch" }],
        },
        {
          code: `try { risky(); } catch { /* can't ignore this error */ }`,
          errors: [{ messageId: "noEmptyCatch" }],
        },
        {
          code: `try { risky(); } catch { /* do not fall through */ }`,
          errors: [{ messageId: "noEmptyCatch" }],
        },
        {
          code: `try { risky(); } catch { /* this is not a no-op */ }`,
          errors: [{ messageId: "noEmptyCatch" }],
        },
        {
          code: `try { risky(); } catch { /* don't swallow exceptions */ }`,
          errors: [{ messageId: "noEmptyCatch" }],
        },
        {
          code: `try { risky(); } catch { /* do not silently swallow errors */ }`,
          errors: [{ messageId: "noEmptyCatch" }],
        },
        {
          code: `try { risky(); } catch { /* swallow errors is bad practice */ }`,
          errors: [{ messageId: "noEmptyCatch" }],
        },
        {
          code: `try { risky(); } catch { /* swallow errors as usual */ }`,
          errors: [{ messageId: "noEmptyCatch" }],
        },
        {
          code: `run().catch(() => {});`,
          errors: [{ messageId: "noEmptyCatch" }],
        },
        {
          code: `run().catch(function () {});`,
          errors: [{ messageId: "noEmptyCatch" }],
        },
      ],
    });
  });
});

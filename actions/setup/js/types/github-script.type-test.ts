export function checkGithubScriptGlobalTypes(): void {
  // @ts-expect-error - Context must retain the imported Actions context type.
  context = 42;
  // @ts-expect-error - Core must retain the complete imported Actions namespace.
  core = { info() {} };
}

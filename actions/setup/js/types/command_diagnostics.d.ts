export type DiagnosticLanguage = "go" | "typescript" | "python";

export interface DiagnosticLocation {
  /** Repository-relative path; never a URL or an external absolute path. */
  path: string;
  /** @minimum 1 @maximum 9007199254740991 @multipleOf 1 */
  line: number;
  /** @minimum 1 @maximum 9007199254740991 @multipleOf 1 */
  column?: number;
}

export interface CommandDiagnostic {
  language: DiagnosticLanguage;
  severity: "error" | "warning" | "info";
  /** @maxLength 2000 */
  message: string;
  /** @maxLength 128 */
  code?: string;
  location?: DiagnosticLocation;
  /** @maxLength 256 */
  test?: string;
  /** @maxLength 256 */
  package?: string;
  /** @maxItems 10 */
  related?: {
    /** @maxLength 1000 */
    message: string;
    location?: DiagnosticLocation;
  }[];
}

export interface DiagnosticReport {
  version: 1;
  /** @maxItems 50 */
  diagnostics: CommandDiagnostic[];
  truncated: boolean;
  issues: string[];
}

export interface CommandDiagnosticInput {
  command?: string;
  stdout?: string;
  stderr?: string;
  output?: string;
  cwd?: string;
  root?: string;
}

export interface DiagnosticOptions {
  secrets?: string[];
  maskedValues?: string[];
}

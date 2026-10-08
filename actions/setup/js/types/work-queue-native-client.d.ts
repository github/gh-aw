type GitHubClient = typeof github;

type NativeMethod<Method extends (...args: never[]) => unknown, Data> = (...args: Parameters<Method>) => Promise<{ data: Data }>;

/**
 * Native transports consumed by the persistent Claim adapters.
 * Endpoints stay optional so staged previews do not require write clients.
 */
export interface ClaimNativeClient {
  rest?: {
    repos?: {
      get?: NativeMethod<GitHubClient["rest"]["repos"]["get"], { id: unknown; full_name: string }>;
      getCommit?: NativeMethod<GitHubClient["rest"]["repos"]["getCommit"], { sha?: string }>;
    };
    codeScanning?: {
      uploadSarif?: NativeMethod<GitHubClient["rest"]["codeScanning"]["uploadSarif"], { id?: unknown }>;
    };
    git?: {
      getRef?: NativeMethod<GitHubClient["rest"]["git"]["getRef"], { object: { sha: string } }>;
      getCommit?: NativeMethod<GitHubClient["rest"]["git"]["getCommit"], { tree: { sha: string } }>;
      createBlob?: NativeMethod<GitHubClient["rest"]["git"]["createBlob"], { sha: string }>;
      createTree?: NativeMethod<GitHubClient["rest"]["git"]["createTree"], { sha: string }>;
      createCommit?: NativeMethod<GitHubClient["rest"]["git"]["createCommit"], { sha: string }>;
      createRef?: NativeMethod<GitHubClient["rest"]["git"]["createRef"], unknown>;
      updateRef?: NativeMethod<GitHubClient["rest"]["git"]["updateRef"], unknown>;
    };
  };
  request?: (route: string, parameters?: Record<string, unknown>) => Promise<{ data: unknown }>;
}

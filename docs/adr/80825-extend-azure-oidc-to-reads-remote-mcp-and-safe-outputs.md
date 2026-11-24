# ADR-80825: Extend Azure OIDC to Connector Reads, Remote MCP, and Safe Outputs

**Date**: 2026-11-24
**Status**: Draft
**Deciders**: pelikhan (PR author)
**Related**: [PR #80825](https://github.com/github/gh-aw/pull/80825), [ADR-74077](74077-declarative-connectors-replace-native-provider-integrations.md), [ADR-79595](79595-azure-ai-gateway-connector-inference.md)

## Context

The `azure-oidc` connector credential route was limited to inference. The selected native engine host performed the Entra exchange for inference only (connector specification section 5.4). REST tools, remote MCP servers, and safe-output handlers had no way to obtain Entra access tokens:

- Stored `token` or `secret` routes hold long-lived credentials and perform no exchange or renewal. The Azure Monitor reader used this route with a `reader-secret`.
- The `github-app` and `github-oidc` routes do not produce Entra tokens. `.github/aw/connectors.md` states that "a GitHub App token or GitHub JWT is not an Entra access token."

Forces:

- Credentials must stay outside the agent sandbox. Reads through the MCP adapter run outside the sandbox.
- Tokens should be short-lived and renewed before expiry.
- A failed exchange must not fall back to a stored secret.
- Reader and writer identities should be separate, and staged writes should acquire no token.
- The compiler should grant `id-token: write` only where a selected credential needs it.

Unknown from the PR evidence: the rollout plan beyond Azure Monitor and inference, and the operational cost of provisioning federated credentials for each trust subject. The PR body does not report either.

## Decision

Move the Entra token exchange for these routes into a trusted runner-side component and expose only a bearer token over a private socket to the agent-side runtime:

1. **Exchange** (`actions/setup/js/connector_azure_oidc.cjs`). The component exchanges a GitHub OIDC assertion (audience `api://AzureADTokenExchange` by default) for an Entra access token. The token is requested for an explicit resource scope ending in `/.default`, at the authority for the selected cloud (public, usgovernment, or china). Tenant and client IDs must be GUIDs. Redirects are refused, responses are size-bounded, and failures are reported generically.
2. **Broker** (`actions/setup/js/connector_credential_broker.cjs`). The broker caches the token in memory and renews it before expiry, coalescing concurrent renewals. It serves one bearer per connection over a Unix socket in a per-job `0700` directory under `RUNNER_TEMP`. It exits at its deadline or when its socket is removed.
3. **Runtime** (`actions/setup/js/connector_runtime.cjs`). When a broker route is configured, the runtime reads the bearer from the socket and never falls back to a stored secret. Each retry re-resolves the credential.
4. **Compiler** (`pkg/workflow/connector_azure_broker.go`, `connector_oidc.go`). `id-token: write` is added only to jobs that select an Azure credential needing it. Live writes require a separate writer client ID and a literal `safe-outputs.environment` reserved for the writer job. Staged writes acquire no token.
5. **Azure Monitor**. An opt-in `federated-reader` credential sits alongside the stored `reader` credential. No Azure writer is added. Inference keeps its existing exchange path.

## Alternatives Considered

1. **Keep stored secrets for all Azure routes.** Rejected for the new paths. It keeps long-lived credentials in GitHub configuration and provides no renewal. The stored-token route remains available for the existing Azure Monitor reader.
2. **Use the GitHub OIDC identity token directly (`github-oidc`).** Rejected. Entra issues the access token, and a GitHub JWT is not one.
3. **Run the Entra exchange inside the agent sandbox, or through a per-connector SDK credential chain.** Rejected. This places the client assertion and Entra token within the agent's reach. The broker keeps the exchange in the trusted host.
4. **Use one Entra identity for reads and writes.** Rejected. A shared identity would couple read-path exposure to write permissions. The PR tests separation of reader and writer credentials.

## Consequences

**Positive**

- Connector reads, remote MCP servers, and safe outputs can use short-lived Entra tokens without storing a client secret.
- No fallback path exists. A failed exchange fails the call rather than downgrading to a stored secret.
- Reader and writer identities and environments are separated, and staged writes acquire no token.
- Existing stored-token routes and the inference exchange are unchanged. Federation is opt-in, so current Azure Monitor users are not broken.

**Negative**

- The new trusted broker adds a process lifecycle to maintain: a socket, a deadline, and cleanup logic. It depends on Unix domain sockets, and its tests are skipped on Windows (`skipIf(win32)`).
- Operators must provision federated credentials for each trust subject and a dedicated writer environment, which adds setup cost. The connector specification documents this work.
- Jobs that select these routes gain `id-token: write`, which widens the OIDC token surface of those jobs.
- Rollout beyond Azure Monitor and inference is not described in the PR evidence.

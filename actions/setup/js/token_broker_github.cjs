// @ts-check

const { isTokenBrokerEnabled, getBrokerToken } = require("./token_broker.cjs");

const BROKER_ERROR_MESSAGES = Object.freeze({
  ERR_TOKEN_BROKER_CONFIGURATION: "Token broker configuration is invalid.",
  ERR_TOKEN_BROKER_OIDC: "Token broker identity acquisition failed.",
  ERR_TOKEN_BROKER_UNAVAILABLE: "Token broker is unavailable.",
  ERR_TOKEN_BROKER_UNAUTHORIZED: "Token broker authorization failed.",
  ERR_TOKEN_BROKER_UNSUPPORTED: "Token broker capability is unsupported.",
  ERR_TOKEN_BROKER_MALFORMED_RESPONSE: "Token broker response is invalid.",
});

/**
 * Use a request-time strategy rather than snapshotting an expiring broker token.
 * Sanitize errors here, before shared assignment helpers can log them.
 * @returns {Object}
 */
function createTokenBrokerGitHubClient() {
  const authStrategy = () => {
    const auth = async () => {
      try {
        const token = await getBrokerToken("copilot-task");
        if (typeof token !== "string" || !token) throw new Error();
        return { type: "token", tokenType: "installation", token };
      } catch (error) {
        const errorCode = error && typeof error === "object" && "code" in error ? error.code : undefined;
        const code = typeof errorCode === "string" && Object.hasOwn(BROKER_ERROR_MESSAGES, errorCode) ? errorCode : "ERR_TOKEN_BROKER_AUTHENTICATION";
        const message = BROKER_ERROR_MESSAGES[code] || "Token broker copilot-task authentication failed.";
        throw Object.assign(new Error(message), { code, status: 401 });
      }
    };
    auth.hook = async (request, route, parameters) => {
      const { token } = await auth();
      const options = request.endpoint.merge(route, parameters);
      options.headers.authorization = `token ${token}`;
      try {
        return await request(options);
      } catch (error) {
        const status = Number(error && typeof error === "object" && "status" in error ? error.status : undefined);
        const safeStatus = Number.isInteger(status) && status >= 400 && status <= 599 ? status : undefined;
        throw Object.assign(new Error(`Broker-authenticated GitHub request failed${safeStatus ? ` (HTTP ${safeStatus})` : ""}.`), { status: safeStatus });
      }
    };
    return auth;
  };
  // getOctokit requires an auth value even with a custom strategy. This is not a
  // credential: only the strategy above may supply an Authorization header.
  return global.getOctokit("", { auth: "broker-managed", authStrategy });
}

module.exports = { createTokenBrokerGitHubClient, isTokenBrokerEnabled };

# compilerenv Package

> Resolution of enterprise environment-variable overrides used during workflow compilation.

## Overview

`compilerenv` separates environment lookup from override resolution. A `Manager` can use an injected `EnvGetter` for deterministic callers and tests; package-level `Resolve*` functions delegate to a default manager backed by `os.Getenv`.

Values are read when a resolver is called. The package also builds GitHub Actions expressions for defaults that must be resolved at workflow runtime rather than at compile time.

## Public API

| Symbol | Description |
|--------|-------------|
| `EnvGetter` | Function type matching `os.Getenv`. |
| `Manager`, `New` | Resolver with an injected environment lookup function; a nil getter uses `os.Getenv`. |
| `ResolveRequireSelfHostedRunners` | Resolves the self-hosted-runner policy, returning an error for values other than `true` or `false`. |
| `ResolveDefaultMaxTurns`, `ResolveDefaultTimeoutMinutes` | Resolve enterprise defaults with caller-supplied fallbacks. |
| `ResolveDefaultMaxTurnCacheMisses` | Resolves the maximum cache-miss override. |
| `ResolveDefaultDetectionModel`, `ResolveDefaultEvalsModel` | Resolve model defaults for detection and evaluation jobs. |
| `ResolveDefaultUTC` | Resolves the configured project timezone override. |
| `ResolvePolicyModelsAllowed`, `ResolvePolicyModelsBlocked` | Resolve model allow/block policies and report whether an override was supplied. |
| `BuildTimeoutMinutesExpression` | Builds a timeout expression with a compiled-in fallback. |
| `BuildDefaultMaxDailyAICreditsExpression`, `BuildDefaultMaxAICreditsExpression` | Resolve daily and per-run AIC credit defaults from GitHub variables. |
| `BuildDefaultDetectionMaxAICreditsExpression`, `BuildDefaultEvalsMaxAICreditsExpression` | Resolve detection and evaluation AIC credit defaults from GitHub variables. |
| `BuildDefaultMaxTurnsExpression` | Resolves the max-turns variable, using an empty fallback to preserve an unlimited setting. |
| `BuildDefaultOTLPEndpointExpression`, `BuildDefaultOTLPHeadersExpression` | Resolve OTLP endpoint and headers at workflow runtime from secrets, with endpoint variable compatibility fallback. |
| `BuildModelOverrideExpression`, `BuildModelOverrideExpressionEmptyFallback` | Build phase model expressions with either a built-in fallback or an empty fallback for runtime selection. |

The exported variable-name constants identify the corresponding enterprise overrides and policy variables. Configuration precedence and individual override behavior are documented in the parent [`workflow` package specification](../README.md).

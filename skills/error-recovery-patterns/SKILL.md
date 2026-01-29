# Error Recovery Patterns Skill

This skill provides comprehensive guidance on error handling patterns, recovery strategies, and debugging techniques in GitHub Agentic Workflows (gh-aw).

## Purpose

Guide developers in implementing robust error recovery patterns to:
- Reduce retry loops in agent sessions (target: <10% vs current 23%)
- Implement circuit breakers to prevent infinite retry loops
- Add proactive recovery for installation, dependency, and API failures
- Improve debug logging for recovery attempts

## When to Use This Skill

Invoke this skill when:
- Implementing retry logic for network operations, installations, or API calls
- Debugging retry loop issues in workflows or agent sessions
- Adding error recovery patterns to new or existing code
- Understanding transient vs non-transient error classification
- Implementing circuit breakers or exponential backoff
- Adding debug logging for recovery attempts

## Key Concepts Covered

### 1. Circuit Breaker Pattern
- Maximum retry limits (standard: 3 attempts)
- Exponential backoff strategies
- Fail-fast on non-transient errors
- Implementation in JavaScript, Shell, and Go

### 2. Installation Failure Recovery
- NPM installation with cache clearing and registry fallbacks
- Python pip installation with mirror alternatives
- Docker image pull with retry and rate limit handling
- Copilot CLI installation with network retry

### 3. API Timeout and Rate Limit Handling
- GitHub API rate limit detection and backoff
- Transient error detection patterns
- Custom retry configuration for different APIs
- Rate limit-specific retry strategies

### 4. Debug Logging for Recovery
- Logger package usage for retry attempts
- Category naming conventions (pkg:filename)
- DEBUG environment variable patterns
- Zero-overhead logging when disabled

### 5. Error Categorization
- Transient vs non-transient errors
- Network errors, timeout patterns
- HTTP error codes (502, 503, 504)
- GitHub-specific errors (rate limits, abuse detection)

## Anti-Patterns to Avoid

This skill explicitly covers anti-patterns to avoid:
- ❌ Infinite retry loops without maximum limits
- ❌ Retrying validation errors that won't self-correct
- ❌ No backoff delay between attempts
- ❌ Silent retries without logging
- ❌ Retrying non-transient errors

## Code Examples Provided

The skill includes production-ready examples for:
- JavaScript retry with `withRetry()` function
- Shell script retry loops with exponential backoff
- Go retry patterns with context and timeouts
- NPM/pip/docker installation recovery
- GitHub API rate limit handling
- Debug logging for all recovery attempts

## Related Skills

- **error-messages** - Error message formatting and style guide
- **error-pattern-safety** - Safety guidelines for error pattern regex
- **developer** - General development guidelines and conventions

## Full Documentation

Complete documentation available at: `../../scratchpad/error-recovery-patterns.md`

This skill references the comprehensive error recovery patterns document which includes:
- Console formatting requirements
- Error wrapping patterns
- Common error scenarios with step-by-step resolution
- Error message templates
- Debugging runbook
- Error categorization decision trees
- Metrics and monitoring strategies

# GitHub Agentic Workflows - Design Specifications

This directory contains design specifications and implementation documentation for key features of GitHub Agentic Workflows.

## Architecture Documentation

| Document | Status | Implementation |
|----------|--------|----------------|
| [Code Organization Patterns](./code-organization.md) | ✅ Documented | Code organization guidelines and patterns |
| [Validation Architecture](./validation-architecture.md) | ✅ Documented | `pkg/workflow/validation.go` and domain-specific files |
| [Go Type Patterns and Best Practices](./go-type-patterns.md) | ✅ Documented | `pkg/constants/constants.go`, `pkg/workflow/permissions_validator.go`, `pkg/parser/frontmatter.go` |
| [Styles Guide](./styles-guide.md) | ✅ Documented | `pkg/styles/theme.go` - Adaptive color palette and terminal styling |
| [Campaign Files Architecture](./campaigns-files.md) | ✅ Documented | `pkg/campaign/`, `actions/setup/js/campaign_discovery.cjs` - Campaign discovery, compilation, and execution |

## Specifications

| Document | Status | Implementation |
|----------|--------|----------------|
| [Safe Outputs System Specification](./safe-outputs-specification.md) | ✅ Documented | W3C-style formal specification for safe outputs architecture, security, and operations |
| [Capitalization Guidelines](./capitalization.md) | ✅ Documented | `cmd/gh-aw/capitalization_test.go` |
| [Safe Output Messages Design System](./safe-output-messages.md) | ✅ Implemented | `pkg/workflow/safe_outputs.go` |
| [Safe Output Environment Variables Reference](./safe-output-environment-variables.md) | ✅ Documented | Environment variable requirements for safe output jobs |
| [MCP Logs Guardrail](./MCP_LOGS_GUARDRAIL.md) | ✅ Implemented | `pkg/cli/mcp_logs_guardrail.go` |
| [YAML Version Compatibility](./yaml-version-gotchas.md) | ✅ Documented | `pkg/workflow/compiler.go` |
| [Schema Validation](./SCHEMA_VALIDATION.md) | ✅ Documented | `pkg/parser/schemas/` |
| [GitHub Actions Security Best Practices](./github-actions-security-best-practices.md) | ✅ Documented | Workflow security guidelines and patterns |
| [End-to-End Feature Testing](./end-to-end-feature-testing.md) | ✅ Documented | `.github/workflows/dev.md`, `.github/workflows/dev-hawk.md` |

## Security Reviews

| Document | Date | Status |
|----------|------|--------|
| [Template Injection Security Review](./SECURITY_REVIEW_TEMPLATE_INJECTION.md) | 2025-11-11 | ✅ No vulnerabilities found |

## Comparative Analysis

| Document | Status | Description |
|----------|--------|-------------|
| [mdflow Syntax Comparison](./mdflow-comparison.md) | ✅ Documented | Detailed comparison of mdflow and gh-aw syntax covering 17 aspects: file naming, frontmatter design, templates, imports, security models, execution patterns, and more |
| [Gastown Multi-Agent Orchestration](./gastown.md) | ✅ Documented | Deep analysis of Gastown's multi-agent coordination patterns and mapping to gh-aw concepts: persistent state, workflow composition, crash recovery, agent communication, and implementation recommendations |

## Statistical Analysis & Reports

| Document | Date | Description |
|----------|------|-------------|
| [Serena Tools Usage Analysis](./serena-tools-analysis.md) | 2026-02-01 | ✅ Complete deep-dive statistical analysis of Serena MCP server tool usage in workflow run 21560089409 |
| [Serena Tools Quick Reference](./serena-tools-quick-reference.md) | 2026-02-01 | ✅ At-a-glance summary of Serena tool usage metrics and insights |
| [Serena Tools Raw Data](./serena-tools-data.json) | 2026-02-01 | ✅ JSON dataset with complete statistics for programmatic access |

## Related Documentation

For user-facing documentation, see [docs/](../docs/).

## Contributing

When adding new specifications:

1. Document implementation details with file paths
2. Mark status with standard icons: ✅ Implemented, 🚧 In Progress, or 📋 Planned
3. Provide code samples and usage patterns
4. Link to test files
5. Update this README's table

---

**Last Updated**: 2026-01-20

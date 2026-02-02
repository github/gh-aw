# Security Architecture Specification - Summary

**Document**: `security-architecture-spec.md`  
**Version**: 1.0.0  
**Status**: Candidate Recommendation  
**Date**: January 29, 2026

## Overview

The GitHub Agentic Workflows Security Architecture Specification is a formal W3C-style document that defines the security architecture, guarantees, and implementation requirements for gh-aw. This specification enables organizations to replicate the security model in other CI/CD environments.

## Key Highlights

### Conformance Classes

1. **Basic Conformance** (Level 1): Core security controls
   - Input sanitization
   - Output isolation
   - Permission management
   - Compilation-time checks

2. **Standard Conformance** (Level 2): Production-ready security
   - Basic + Network isolation
   - Basic + Sandbox isolation
   - Basic + Runtime enforcement

3. **Complete Conformance** (Level 3): Maximum security
   - Standard + Threat detection
   - Standard + All recommended enhancements

### Security Architecture Layers

The specification defines a **7-layer defense-in-depth architecture**:

0. **Compilation-Time Validation** - Schema, expressions, permissions
1. **Input Sanitization Layer** - @mentions, bot triggers, XML/HTML, URIs
2. **Output Isolation Layer** - Separate read/write operations
3. **Network Isolation Layer** - Domain allowlisting, ecosystem IDs
4. **Permission Management Layer** - Least privilege, role-based access
5. **Sandbox Isolation Layer** - AWF/SRT containers, MCP isolation
6. **Threat Detection Layer** - Prompt injection, secret leaks, malicious patches

### Core Security Guarantees

The specification defines **7 security guarantees (SG-01 to SG-07)**:

- **SG-01**: Untrusted input not directly interpolated into GitHub Actions expressions without sanitization
- **SG-02**: AI agents have no direct write access
- **SG-03**: Network access restricted to allowlists
- **SG-04**: Least-privilege permissions by default
- **SG-05**: Agent processes in isolated sandboxes
- **SG-06**: All actions produce auditable artifacts
- **SG-07**: Security failures prevent execution (fail-secure)

**Note on SG-01**: This guarantee protects against template injection in GitHub Actions expressions. It does not prevent AI agents from accessing untrusted data at runtime through tools like GitHub MCP (which can return issue titles, PR bodies, etc.). Such data is subject to AI prompt injection risks, which are addressed through threat detection (Layer 6) and safe outputs isolation (Layer 2).

### Formal Requirements

- **130+ formal requirements** using RFC 2119 keywords (MUST, SHALL, SHOULD, MAY)
- **70+ compliance tests** across 8 categories
- **6 comprehensive appendices** with diagrams, examples, and best practices

### Test Categories

1. **Input Sanitization Tests** (T-IS-001 to T-IS-008)
2. **Output Isolation Tests** (T-OI-001 to T-OI-007)
3. **Network Isolation Tests** (T-NI-001 to T-NI-009)
4. **Permission Management Tests** (T-PM-001 to T-PM-007)
5. **Sandbox Isolation Tests** (T-SI-001 to T-SI-007)
6. **Threat Detection Tests** (T-TD-001 to T-TD-007)
7. **Compilation-Time Security Tests** (T-CS-001 to T-CS-006)
8. **Runtime Security Tests** (T-RS-001 to T-RS-008)

## Document Structure

| Section | Content | Requirements |
|---------|---------|--------------|
| 1. Introduction | Purpose, scope, design goals | - |
| 2. Conformance | Classes, notation, compliance levels | 3 classes |
| 3. Architecture | Multi-layer overview, guarantees, threat model | 7 guarantees, 6 principles |
| 4. Input Sanitization | Sanitization procedures, bypass prevention | 11 requirements |
| 5. Output Isolation | Job architecture, validation, token management | 11 requirements |
| 6. Network Isolation | Configuration, allowlists, enforcement | 14 requirements |
| 7. Permission Management | Defaults, strict mode, role-based access | 15 requirements |
| 8. Sandbox Isolation | Agent sandbox, MCP isolation, guarantees | 13 requirements |
| 9. Threat Detection | Categories, methods, output format | 15 requirements |
| 10. Compilation Security | Schema, expressions, permissions, actions | 13 requirements |
| 11. Runtime Security | Timestamp, repository, role, token validation | 15 requirements |
| 12. Compliance Testing | Test suite, categories, procedures | 70+ tests |
| Appendices A-F | Diagrams, examples, best practices | 6 appendices |

## Appendices

### Appendix A: Security Architecture Diagram
Complete visual representation of the security architecture with all layers.

### Appendix B: Sanitization Examples
Real-world examples of @mention, bot trigger, XML/HTML, and URI sanitization.

### Appendix C: Network Configuration Examples
Sample configurations for default, selective, protocol-specific, and blocked domains.

### Appendix D: Safe Output Configuration Examples
Examples of basic, multi-output, and threat-detection-enabled configurations.

### Appendix E: Strict Mode Violations
Common violations and error messages for write permissions, unpinned actions, and wildcards.

### Appendix F: Security Best Practices
Six key best practices with "Don't" and "Do" examples.

## Target Audience

- **Security Engineers**: Audit and verify security controls
- **Platform Engineers**: Implement equivalent systems in other CI/CD platforms
- **Compliance Teams**: Assess conformance to security standards
- **Workflow Authors**: Understand security guarantees and limitations
- **Research Teams**: Build upon or extend the security architecture

## References

### Normative References
- RFC 2119 (Requirement keywords)
- JSON Schema
- YAML 1.2
- GitHub Actions Syntax
- GitHub Actions Security

### Informative References
- MCP Specification
- MCP Security Best Practices
- OWASP Top 10
- CWE (Common Weakness Enumeration)
- actionlint, zizmor (security tools)
- GitHub Agentic Workflows documentation

## Implementation Status

The specification documents the **current implementation** in gh-aw version 1.0.0:

- **Reference Implementation**: GitHub Agentic Workflows (Go-based)
- **Compiled Format**: GitHub Actions YAML (`.lock.yml` files)
- **Runtime**: GitHub Actions with AWF/SRT sandboxes
- **Language**: Go with embedded JavaScript/shell scripts

### Implementation Files

Key implementation files referenced in the specification:

- `pkg/workflow/safe_inputs_parser.go` - Input sanitization
- `pkg/workflow/safe_outputs_config.go` - Output isolation
- `pkg/workflow/engine.go` - Network permissions
- `pkg/workflow/compiler_safe_outputs.go` - Safe output compilation
- `pkg/workflow/safe_jobs.go` - Threat detection
- `pkg/workflow/compiler_types.go` - Core types
- Actions in `actions/setup/js/*.cjs` and `actions/setup/sh/*.sh`

## Next Steps

### For Security Review
1. Read the full specification: `security-architecture-spec.md`
2. Review the security guarantees (Section 3.2)
3. Examine the formal requirements (Sections 4-11)
4. Assess compliance testing requirements (Section 12)

### For Implementation
1. Determine target conformance class (Basic/Standard/Complete)
2. Review implementation requirements for chosen class
3. Study the reference implementation in gh-aw
4. Implement compliance tests (Section 12)
5. Generate conformance report

### For Integration
1. Understand the compilation model (Section 10)
2. Map security layers to target CI/CD platform
3. Implement equivalent sandbox mechanisms
4. Adapt network isolation to platform capabilities
5. Validate against compliance tests

## Versioning

The specification follows **semantic versioning**:

- **Major**: Breaking changes, incompatible modifications
- **Minor**: New features, backward-compatible additions
- **Patch**: Bug fixes, clarifications, editorial changes

Current version: **1.0.0** (Candidate Recommendation)

## Feedback

For questions, feedback, or errata:

- **Repository**: https://github.com/github/gh-aw
- **Issues**: https://github.com/github/gh-aw/issues
- **Discussions**: https://github.com/github/gh-aw/discussions

## License

Copyright © 2026 GitHub, Inc.  
This specification is provided under the MIT License.

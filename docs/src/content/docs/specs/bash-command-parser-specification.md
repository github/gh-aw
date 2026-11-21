---
title: Bash Command Parser Specification
spec: true
description: W3C-style specification for the Copilot SDK bash command parser and conformance test generation
sidebar:
  order: 1370
---

# Bash Command Parser Specification

**Version**: 1.1.0  
**Status**: Draft Specification  
**Latest Version**: [bash-command-parser-specification](/gh-aw/specs/bash-command-parser-specification/)  
**Editors**: GitHub Agentic Workflows Team

---

## Abstract

This specification defines the behavior of the bash command parser used by the Copilot SDK permission integration. It formalizes segment splitting, command-name extraction, deduplicated pipeline extraction, and conformance testing. It also defines a language-agnostic method for generating and verifying parser test suites.

## Status of This Document

This document is a draft extracted from the current production parser behavior and repository conformance tests.

## Table of Contents

1. [Introduction](#1-introduction)
2. [Conformance](#2-conformance)
3. [Formal Grammar](#3-formal-grammar)
4. [Parser Semantics](#4-parser-semantics)
5. [Driver Integration Semantics](#5-driver-integration-semantics)
6. [Conformance Test Suite Construction](#6-conformance-test-suite-construction)
7. [Model-Based Test Generation](#7-model-based-test-generation)
8. [Verification-Based Test Generation](#8-verification-based-test-generation)
9. [Machine-Readable Test Vectors](#9-machine-readable-test-vectors)
10. [Testing Strategies](#10-testing-strategies)
11. [Security Considerations](#11-security-considerations)
12. [References](#12-references)
13. [Sync Notes](#sync-notes)
14. [Change Log](#change-log)

---

## 1. Introduction

### 1.1 Purpose

The parser is a lightweight recognizer for shell command identifiers in chained/piped command text. Its output is used by the Copilot SDK permission handler to decide whether shell requests are allowed.

### 1.2 Scope

This specification defines:

- splitting on `&&`, `||`, `|`, `;`, and top-level line breaks
- quote/subshell shielding during splitting
- executable token extraction from a segment
- deduplicated name extraction from pipeline text

This specification does **not** define a full POSIX shell parser.

---

## 2. Conformance

### 2.1 Requirements Notation

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**, **SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **MAY**, and **OPTIONAL** are to be interpreted as described in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119).

### 2.2 Conformance Classes

- **Class S (Splitter)**: Implements segment splitting in §4.1.
- **Class E (Extractor)**: Implements single-segment extraction in §4.2.
- **Class P (Pipeline Extractor)**: Implements pipeline extraction in §4.3.
- **Class I (Integration Consumer)**: Implements driver behavior in §5.

A conforming implementation MUST satisfy all applicable MUST-level requirements for its class.

A Class I (Integration Consumer) conforming implementation MUST apply default-deny behavior when `extractCommandNamesFromPipeline` (§4.3) returns no commands (`length === 0`). An empty list result MUST NOT be treated as implicit authorization to proceed with command execution.

### 2.3 Language-Neutral Type Contract

Conforming implementations MUST expose semantics equivalent to the following total functions:

```text
splitOnPipelineOperators(commandText: StringLike) -> List<Segment>
extractCommandName(segment: StringLike) -> Option<CommandName>
extractCommandNamesFromPipeline(commandText: StringLike) -> List<CommandName>
```

Where:

- `StringLike` includes any runtime value accepted by the host language.
- `Segment` and `CommandName` are textual values.
- `Option<T>` is either `None` (no command identified) or `Some(T)`.
- `List<T>` preserves element order.

For any `StringLike` input, each function MUST return a value in its codomain and MUST NOT fail with an uncaught exception.

---

## 3. Formal Grammar

The grammar below is recognition-oriented and intentionally limited to parser behavior.

### 3.1 Splitting Grammar (EBNF)

```
command_text   = { unit } ;
unit           = single_quoted | double_quoted | subshell | operator | other ;
operator       = "&&" | "||" | "|" | ";" | newline ;
newline        = "\n" | "\r\n" | "\r" ;

single_quoted  = "'" , { ? any char except "'" ? } , [ "'" ] ;
double_quoted  = '"' , { dq_char | escape } , [ '"' ] ;
escape         = "\" , ? any char ? ;
dq_char        = ? any char except unescaped '"' ? ;

subshell       = "$(" , subshell_body ;
subshell_body  = { subshell_char | nested } ;
nested         = "(" , subshell_body , ")" ;
subshell_char  = ? any char not terminating current depth ? ;

other          = ? any other single char ? ;
```

The optional closing quote in `single_quoted` and `double_quoted` is intentional and models malformed-input tolerance; implementations MUST preserve the non-throwing robustness requirement in §4.1 even when quotes are unbalanced.

### 3.2 Segment Extraction Grammar (EBNF)

```
segment        = ws , { env_assign , ws } , core ;
env_assign     = ident , "=" , env_value ;
env_value      = dq_value | sq_value | nonspace* ;
dq_value       = '"' , { dq_char | escape } , [ '"' ] ;
sq_value       = "'" , { ? any char except "'" ? } , [ "'" ] ;
ident          = ("_" | letter) , { "_" | letter | digit } ;

core           = negation | brace | keyword | redirection | word | empty ;
negation       = "!" , ws , core ;
brace          = ("{" | "}") , ws , core ;
keyword        = clause_keyword | structural_keyword ;
clause_keyword = "then" | "else" | "elif" | "do" ;
structural_keyword
               = "if" | "fi" | "for" | "done" | "while" | "until"
               | "case" | "esac" | "select" | "in"
               | "function" | "time" | "coproc" ;
redirection    = ("<" | ">") , nonspace*
               | digits , ("<" | ">" | "&") , nonspace* ;
word           = nonspace , nonspace* ;
ws             = { " " | "\t" | "\n" | "\r" } ;
nonspace       = ? any non-whitespace character ? ;
digits         = digit , { digit } ;
digit          = "0" | "1" | "2" | "3" | "4" | "5" | "6" | "7" | "8" | "9" ;
letter         = "A" | "B" | "C" | "D" | "E" | "F" | "G" | "H" | "I" | "J"
               | "K" | "L" | "M" | "N" | "O" | "P" | "Q" | "R" | "S" | "T"
               | "U" | "V" | "W" | "X" | "Y" | "Z"
               | "a" | "b" | "c" | "d" | "e" | "f" | "g" | "h" | "i" | "j"
               | "k" | "l" | "m" | "n" | "o" | "p" | "q" | "r" | "s" | "t"
               | "u" | "v" | "w" | "x" | "y" | "z" ;
```

---

## 4. Parser Semantics

### 4.1 `splitOnPipelineOperators(commandText)`

1. Non-string or empty/falsy input MUST return `[]`.
2. The implementation MUST split at top-level operators `&&`, `||`, `|`, `;`, and line breaks (`\n`, `\r\n`, `\r`), except escaped line continuations (`\\` immediately before the line break).
3. Operators inside single quotes, double quotes, or `$(` `)` regions MUST NOT split.
4. Output segments MUST be trimmed.
5. Empty segments MUST be removed.
6. The function SHOULD be non-throwing for malformed input.

### 4.2 `extractCommandName(segment)`

1. Non-string or blank segment MUST return `null`.
2. Leading environment assignments (`IDENTIFIER=<value>`) MUST be stripped repeatedly, where `<value>` MAY be unquoted, single-quoted, or double-quoted (including escaped content in double quotes).
3. If the first token is redirection (`^[<>]` or `^\d+[<>&]`), return `null`.
4. If the first token is `!`, `{`, or `}`, extraction MUST recurse on the remainder.
5. If the first token is a clause keyword (`then`, `else`, `elif`, `do`), extraction MUST continue scanning on the remainder.
6. If the first token is a structural keyword (`if`, `fi`, `for`, `done`, `while`, `until`, `case`, `esac`, `select`, `in`, `function`, `time`, `coproc`), return `null`.
7. Otherwise return the first token.

### 4.3 `extractCommandNamesFromPipeline(commandText)`

1. Non-string or blank input MUST return `[]`.
2. Input MUST be split using §4.1.
3. Each segment MUST be extracted using §4.2.
4. Null extraction results MUST be ignored.
5. Returned command names MUST be deduplicated while preserving first-occurrence order.

---

## 5. Driver Integration Semantics

The parser output is consumed by fallback shell-permission logic:

1. If multiple names are extracted (`length > 1`), **all** names MUST satisfy shell identifier rules.
2. If one name is extracted (`length === 1`), normal single-command matching applies, including exact full-command matching for literal shell rules that contain spaces (for example `ls /tmp`).
3. If no names are extracted (`length === 0`), only exact full-command matching for shell rules that contain spaces is attempted; otherwise deny.
4. This preserves default-deny behavior when parsing cannot confidently identify commands.

---

## 6. Conformance Test Suite Construction

A language-independent test suite MUST contain:

- **Vector tests** for each parser function (split/extract/pipeline).
- **Robustness tests** for malformed/unbalanced inputs.
- **Deduplication/order tests** for repeated command names.
- **Integration tests** for fallback behavior in §5.

Implementations SHOULD consume machine-readable vectors and run identical assertions in each target language.

### 6.1 Minimal Conformance Suite

A minimal suite MUST include all categories below:

- **S-CORE (4 tests)**:
  1. top-level split on each operator `&&`, `||`, `|`, `;`, and newline (excluding escaped line continuations)
  2. no split when operators occur in single quotes
  3. no split when operators occur in double quotes
  4. no split when operators occur inside `$(` `)`
- **E-CORE (5 tests)**:
  1. simple word returns command name
  2. leading environment assignments are stripped
  3. redirection-first segment returns null/none
  4. clause keywords continue extraction while structural keywords return null/none
  5. recursive skip for `!`, `{`, and `}`
- **P-CORE (4 tests)**:
  1. split + extract composition over multi-operator pipeline
  2. null/none extraction results are ignored
  3. deduplication keeps first occurrence only
  4. non-string and blank input returns empty list
- **R-CORE (2 tests)**:
  1. unbalanced quote input is non-throwing
  2. unbalanced subshell input is non-throwing

The minimal suite therefore contains **15 required tests**.

---

## 7. Model-Based Test Generation

Model-based tests MUST be generated from a finite-state splitter model with states:

- `TopLevel`
- `InSingleQuote`
- `InDoubleQuote`
- `InSubshell(depth>=1)`

Generation procedure:

1. Define token alphabet: command words, operators, quotes, `$(`, `)`, escapes, whitespace.
2. Build transition traces across all states and transitions.
3. Emit expected split points only in `TopLevel`.
4. Derive expected command extraction outputs from segment-first-token rules in §4.2.
5. Serialize generated vectors for cross-language execution.

---

## 8. Verification-Based Test Generation

Verification MUST include metamorphic/property-derived vectors:

1. **Whitespace invariance**: surrounding/inter-operator whitespace does not change extracted command names.
2. **Quoted operator shielding**: moving operators into quotes preserves single-segment behavior.
3. **Env-prefix invariance**: prepending env assignments does not change extracted command identifier.
4. **Redirection-suffix invariance**: appending redirection suffixes does not change extracted identifier.
5. **Duplicate-collapse invariance**: repeating same command across stages still yields one unique name.
6. **No-throw robustness**: malformed inputs SHOULD not throw and SHOULD keep return-shape guarantees.

---

## 9. Machine-Readable Test Vectors

Conforming projects SHOULD publish vectors in JSON (or equivalent structured data) with stable IDs and source tags:

- `source = "model-based"` for state-model-derived vectors
- `source = "verification"` for metamorphic/property-derived vectors

Reference vectors MUST be stored at `specs/test-vectors/bash-command-parser/` within the repository root and MUST be addressable via stable, version-qualified filenames following the pattern `<version>-<source>.json` (for example, `v1.1.0-model-based.json` and `v1.1.0-verification.json`). The canonical path for the test vector collection is `specs/test-vectors/bash-command-parser/` relative to the repository root. Each implementation's native test runner MUST consume vectors from this canonical path when this collection is present in the repository.

---

## 10. Testing Strategies

Conforming projects SHOULD apply all of the following:

1. **Deterministic vector conformance**  
   Run stable, versioned vectors in every language binding and compare exact outputs.
2. **Metamorphic verification**  
   Generate follow-up inputs from seed vectors using relations from §8 and assert relation-preserving behavior.
3. **State-space coverage tracking**  
   Track model transition coverage for `TopLevel`, quote states, and subshell depth.
4. **Typed oracle validation**  
   Validate that every observed output satisfies the type contract in §2.3 (including null/none cases).
5. **Negative robustness testing**  
   Exercise malformed inputs (unbalanced quotes, trailing operators, nested partial subshells) and assert non-throwing behavior.
6. **Differential replay across implementations**  
   Replay the same vector corpus against each implementation and require identical semantic outcomes.

---

## 11. Security Considerations

This parser is not a shell sandbox and MUST NOT be treated as proof of command safety. Consumers MUST keep permission checks default-deny when command identification fails. Ambiguous/unparseable input MUST result in deny behavior at the integration layer.

### 11.1 Misuse Examples

The following examples illustrate security misuse patterns that conforming consumers MUST NOT adopt:

**Misuse Example 1 — Using parser output as sandbox proof**: An integration that allows a shell command to execute solely because the parser returned a non-null command name is incorrect. The parser identifies command tokens; it does not validate that the command is safe, permitted, or confined. A consumer MUST NOT treat a non-null extraction result as proof that execution is authorized or sandbox-contained.

**Misuse Example 2 — Allowing execution on empty parse result**: An integration that permits shell execution when `extractCommandNamesFromPipeline` returns an empty list (for example, because the input contained only redirections or structural keywords) violates the default-deny contract. An empty result MUST NOT be treated as implicit permission; the integration MUST apply its default-deny fallback.

**Misuse Example 3 — Bypassing permission checks on single-command pipelines**: An integration that skips scoped allow-list checks for inputs the parser classifies as "simple" (single command, no operators) misuses the parser's output. Parser-identified simplicity is a syntactic property only. All permission-check logic MUST be applied regardless of pipeline complexity as determined by the parser.

### 11.2 Normative Security Requirements

A Class I (Integration Consumer) conforming implementation MUST NOT treat any parser output — including non-empty command-name results and empty lists — as proof that the corresponding shell invocation is safe or permitted. Permission-enforcement logic MUST remain fully operative independent of what the parser returns.

### 11.3 Safeguards

The following MUST-level norms apply to all Class I (Integration Consumer) conforming implementations:

1. **Empty-result default-deny**: When `extractCommandNamesFromPipeline` returns an empty list (`length === 0`), the integration MUST apply default-deny and MUST NOT proceed with command execution. An empty result is never implicit authorization. This norm strengthens the requirement stated in §2.2 and §5, and applies regardless of whether the empty result arises from a blank input, a fully structural-keyword input, or a parse that yields only redirections.

2. **Parse-error denial**: When the parser encounters input that cannot be processed without throwing an exception (or equivalent unrecoverable error condition), the integration MUST NOT silently allow execution. Parse errors MUST be treated as denial conditions. The integration MUST log or surface a diagnostic indicating that the command could not be parsed and that execution was denied.

---

## 12. References

### 12.1 Normative

- RFC 2119: Key words for use in RFCs to Indicate Requirement Levels

### 12.2 Informative

- Copilot SDK Shell Permission Integration (implementation-defined binding)
- Repository Conformance Vectors (implementation-defined location)

---

<a id="sync-notes"></a>
## Sync Notes

The canonical machine-readable vector collection for this specification is `specs/test-vectors/bash-command-parser/`.

For version `1.1.0`, the canonical seed files are:

- `specs/test-vectors/bash-command-parser/v1.1.0-model-based.json`
- `specs/test-vectors/bash-command-parser/v1.1.0-verification.json`

These vectors MUST be revalidated whenever either of the following occurs:

1. This specification is incremented to a new version.
2. The parser behavior changes in `actions/setup/js/bash_command_parser.cjs`, including updates to `splitOnPipelineOperators`, `extractCommandName`, or `extractCommandNamesFromPipeline`.

---

<a id="change-log"></a>
## Change Log

### Version 1.1.0 (2026-07-06 maintenance update)

- Added canonical versioned machine-readable vector files at `specs/test-vectors/bash-command-parser/v1.1.0-model-based.json` and `specs/test-vectors/bash-command-parser/v1.1.0-verification.json`.
- Added Sync Notes identifying the canonical vector path and the required revalidation triggers for parser behavior and specification version changes.

# shellpattern Package

> Lexical detection of shell expansions blocked by the safe-output harness.

## Public API

| Symbol | Description |
|--------|-------------|
| `Expansion` | Byte range and description of one unsupported expansion pattern. |
| `FirstUnsafeExpansion` | Returns the first parameter transformation, indirect expansion, or command substitution found in a script. |

The check is deliberately lexical: matches in quoted text and comments are included, arithmetic expansion is allowed, and command substitutions nested inside arithmetic are still checked. It is not a shell parser or a general execution analysis.

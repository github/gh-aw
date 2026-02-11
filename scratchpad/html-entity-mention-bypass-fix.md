# HTML Entity Encoding Bypass Fix for @mention Sanitization

## Problem

The safe-outputs sanitization system had a vulnerability where HTML entities could bypass @mention detection. If entities were decoded after @mention neutralization, an attacker could use entity-encoded @ symbols to trigger unwanted user notifications.

### Attack Vectors

1. Named entity: `&commat;user` → `@user`
2. Decimal entity: `&#64;user` → `@user`
3. Hexadecimal entity: `&#x40;user` or `&#X40;user` → `@user`
4. Double-encoded: `&amp;commat;user`, `&amp;#64;user`, `&amp;#x40;user` → `@user`
5. Mixed encoding: `&#64;us&#101;r` → `@user`
6. Fully encoded: `&#64;&#117;&#115;&#101;&#114;` → `@user`

## Solution

Added `decodeHtmlEntities()` function in `actions/setup/js/sanitize_content_core.cjs` that:

1. **Decodes named entities**: `&commat;` → `@` (case-insensitive)
2. **Decodes decimal entities**: `&#NNN;` → corresponding Unicode character
3. **Decodes hexadecimal entities**: `&#xHHH;` or `&#XHHH;` → corresponding Unicode character  
4. **Handles double-encoding**: `&amp;commat;`, `&amp;#64;`, `&amp;#x40;`
5. **Validates code points**: Only accepts valid Unicode range (0x0 - 0x10FFFF)

### Integration

The `decodeHtmlEntities()` function is integrated into `hardenUnicodeText()` at **Step 2**, ensuring HTML entities are decoded **before** @mention detection occurs:

```javascript
function hardenUnicodeText(text) {
  // Step 1: Normalize Unicode (NFC)
  result = result.normalize("NFC");
  
  // Step 2: Decode HTML entities (CRITICAL - must be early)
  result = decodeHtmlEntities(result);
  
  // Step 3: Strip zero-width characters
  // Step 4: Remove bidirectional overrides
  // Step 5: Convert full-width ASCII
  
  return result;
}
```

### Sanitization Pipeline

```
Input Text
    ↓
hardenUnicodeText()
  ├─ Unicode normalization (NFC)
  ├─ HTML entity decoding ←  decodeHtmlEntities()
  ├─ Zero-width character removal
  ├─ Bidirectional control removal
  └─ Full-width ASCII conversion
    ↓
ANSI escape sequence removal
    ↓
neutralizeMentions() or neutralizeAllMentions()
    ↓
Other sanitization steps
    ↓
Output (safe text)
```

## Test Coverage

Comprehensive test suite in `actions/setup/js/sanitize_content.test.cjs` covers:

- ✅ Named entity decoding (`&commat;`)
- ✅ Double-encoded named entities (`&amp;commat;`)
- ✅ Decimal entity decoding (`&#64;`)
- ✅ Double-encoded decimal entities (`&amp;#64;`)
- ✅ Hexadecimal entity decoding (lowercase `&#x40;`, uppercase `&#X40;`)
- ✅ Double-encoded hex entities (`&amp;#x40;`, `&amp;#X40;`)
- ✅ Multiple encoded mentions in one string
- ✅ Mixed encoded and normal mentions
- ✅ Org/team mentions with entities
- ✅ General entity decoding (non-@ characters)
- ✅ Invalid code point handling
- ✅ Malformed entity handling
- ✅ Case-insensitive named entities
- ✅ Interaction with other sanitization steps
- ✅ Allowed aliases with encoded mentions

Total: 25+ test cases

## Examples

```javascript
// Named entity
sanitizeContent("&commat;pelikhan")  
// → "`@pelikhan`"

// Decimal entity  
sanitizeContent("&#64;pelikhan")
// → "`@pelikhan`"

// Hexadecimal entity
sanitizeContent("&#x40;pelikhan")
// → "`@pelikhan`"

// Mixed encoding in username
sanitizeContent("&#64;us&#101;r")
// → "`@user`"

// Fully encoded
sanitizeContent("&#64;&#117;&#115;&#101;&#114;")
// → "`@user`"

// Double-encoded
sanitizeContent("&amp;#64;pelikhan")
// → "`@pelikhan`"
```

## Security Impact

- **Risk Level**: MEDIUM → **RESOLVED**
- **Attack Surface**: Entity-encoded @ symbols could bypass mention detection
- **Fix**: All HTML entity encoding variants now decoded before @mention processing
- **Coverage**: Universal - applies to both `sanitizeContent()` and `sanitizeIncomingText()`

## Files Modified

- `actions/setup/js/sanitize_content_core.cjs` - Added `decodeHtmlEntities()` function and integrated into `hardenUnicodeText()`
- `actions/setup/js/sanitize_content.test.cjs` - Added 25+ test cases for HTML entity decoding
- Exported `decodeHtmlEntities` from module for potential standalone use

## Defense in Depth

This fix follows defense-in-depth principles:
1. **Early decoding**: Entities decoded at Step 2 of Unicode hardening
2. **Comprehensive coverage**: Handles all entity types and double-encoding  
3. **Validation**: Rejects invalid Unicode code points
4. **Universal application**: Applies to all content sanitization flows
5. **Test coverage**: Extensive test suite validates all attack vectors

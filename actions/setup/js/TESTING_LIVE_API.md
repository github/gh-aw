# Testing Frontmatter Hash with Live GitHub API

This directory includes tests for the JavaScript frontmatter hash implementation, including tests that use the **real GitHub API** (no mocks) to fetch workflow files.

## Running Tests

### Standard Test Suite (with mocks)
```bash
npm test -- frontmatter_hash_github_api.test.cjs
```

This runs all tests including mocked GitHub API calls.

### Live GitHub API Test (no mocks)

The test suite includes a live API test that fetches real data from the GitHub repository. To run it, you need a GitHub token:

#### Option 1: Run via npm test
```bash
GITHUB_TOKEN=ghp_your_token_here npm test -- frontmatter_hash_github_api.test.cjs
```

#### Option 2: Run standalone script
```bash
GITHUB_TOKEN=ghp_your_token_here node test-live-github-api.cjs
```

The standalone script provides more detailed output about the API interaction.

## Getting a GitHub Token

1. Go to https://github.com/settings/tokens
2. Click "Generate new token (classic)"
3. Give it a descriptive name like "gh-aw testing"
4. Select the `public_repo` scope (sufficient for reading public repositories)
5. Click "Generate token"
6. Copy the token (starts with `ghp_`)

**Note:** Keep your token secure and never commit it to the repository.

## What the Live API Test Does

The live API test:
1. Fetches the `audit-workflows.md` workflow from the `github/gh-aw` repository
2. Resolves and fetches all imported files (like `shared/mcp/gh-aw.md`)
3. Computes the frontmatter hash using the JavaScript implementation
4. Verifies the hash is deterministic by computing it twice
5. Confirms the hash format is a valid SHA-256 hex string

This validates that the JavaScript hash implementation works correctly with real GitHub API responses, not just mocked data.

## Example Output

### Without Token (Skipped)
```
stdout | frontmatter_hash_github_api.test.cjs > live GitHub API integration > should compute hash using real GitHub API (no mocks)
Skipping live API test - no GITHUB_TOKEN or GH_TOKEN available
To run this test, set GITHUB_TOKEN or GH_TOKEN environment variable
Example: GITHUB_TOKEN=ghp_xxx npm test -- frontmatter_hash_github_api.test.cjs

 ✓ frontmatter_hash_github_api.test.cjs (10 tests) 16ms
```

### With Token (Using standalone script)
```bash
$ GITHUB_TOKEN=ghp_xxx node test-live-github-api.cjs
🔍 Testing frontmatter hash with live GitHub API

Repository: github/gh-aw
Branch: main
Workflow: .github/workflows/audit-workflows.md

📡 Connecting to GitHub API...
📥 Fetching workflow from GitHub API...

✅ Success! Hash computed from live GitHub API data:
   db7af18719075a860ef7e08bb6f49573ac35fbd88190db4f21da3499d3604971

🔄 Verifying determinism (fetching again)...
✅ Hashes match - computation is deterministic

📊 Summary:
   - Successfully fetched workflow from live GitHub API
   - Processed workflow with imports (shared/mcp/gh-aw.md, etc.)
   - Computed deterministic SHA-256 hash
   - Verified hash consistency across multiple API calls

✨ All tests passed! The JavaScript implementation works correctly with GitHub API.
```

## Cross-Language Validation

The test suite also includes cross-language validation to ensure the JavaScript hash matches the Go implementation:

```javascript
// JavaScript hash
const jsHash = await computeFrontmatterHash(workflowPath);

// Go hash (from go test -run TestHashWithRealWorkflow ./pkg/parser/)
const goHash = "db7af18719075a860ef7e08bb6f49573ac35fbd88190db4f21da3499d3604971";

// They should match
expect(jsHash).toBe(goHash);
```

## Files

- `frontmatter_hash_github_api.test.cjs` - Test suite with mocked and live API tests
- `test-live-github-api.cjs` - Standalone script for live API testing with detailed output
- `frontmatter_hash_pure.cjs` - Core implementation of hash computation
- `frontmatter_hash.cjs` - API wrapper for hash computation

## Troubleshooting

### Rate Limiting
If you hit GitHub API rate limits:
- Wait for the rate limit to reset (check `X-RateLimit-Reset` header)
- Use a personal access token (provides higher rate limits)
- The test is designed to be minimal and should not hit rate limits under normal use

### Authentication Errors
- Ensure your token has the `public_repo` or `repo` scope
- Check that the token hasn't expired
- Verify the token is correctly set in the environment variable

### File Not Found
- The test uses `github/gh-aw` repository which is public
- If testing against a different repository, update the owner/repo in the test

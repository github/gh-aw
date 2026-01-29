//go:build !integration

package gitutil

import "testing"

func TestIsAuthError(t *testing.T) {
	tests := []struct {
		name   string
		errMsg string
		want   bool
	}{
		// Test gh_token variations
		{"gh_token error", "error: GH_TOKEN is required", true},
		{"gh_token lowercase", "error: gh_token is required", true},
		{"gh_token mixed case", "Error: Gh_Token missing", true},

		// Test GITHUB_TOKEN variations
		{"GITHUB_TOKEN error", "GITHUB_TOKEN not set", true},
		{"github_token lowercase", "github_token not set", true},
		{"github_token mixed", "GitHub_Token is missing", true},

		// Test authentication variations
		{"authentication failed", "authentication failed: invalid credentials", true},
		{"Authentication uppercase", "AUTHENTICATION ERROR: Please log in", true},
		{"authentication in sentence", "The authentication process has failed", true},

		// Test not logged into variations
		{"not logged into GitHub", "You are not logged into any GitHub hosts", true},
		{"Not Logged Into mixed", "Error: Not Logged Into GitHub", true},
		{"not logged into lowercase", "error: not logged into github", true},

		// Test unauthorized variations
		{"unauthorized access", "401 Unauthorized: access denied", true},
		{"UNAUTHORIZED uppercase", "UNAUTHORIZED: Access token is invalid", true},
		{"unauthorized lowercase", "401 unauthorized", true},

		// Test forbidden variations
		{"forbidden access", "403 Forbidden: insufficient permissions", true},
		{"FORBIDDEN uppercase", "FORBIDDEN: You don't have access", true},
		{"forbidden lowercase", "403 forbidden", true},

		// Test permission denied variations
		{"permission denied", "Permission denied to repository", true},
		{"PERMISSION DENIED uppercase", "PERMISSION DENIED: Insufficient privileges", true},
		{"permission denied lowercase", "permission denied", true},

		// Test case insensitivity
		{"case insensitive", "AUTHENTICATION ERROR", true},
		{"mixed case 1", "AuThEnTiCaTiOn FaIlEd", true},
		{"mixed case 2", "PeRmIsSiOn DeNiEd", true},

		// Test errors that should NOT match
		{"not auth error - file not found", "file not found: example.txt", false},
		{"not auth error - network", "network timeout while connecting", false},
		{"not auth error - syntax", "syntax error: unexpected token", false},
		{"not auth error - partial match author", "author not found in repository", false},
		{"not auth error - partial match bidden", "the bidden treasure was found", false},
		{"not auth error - generic", "something went wrong", false},
		{"empty error message", "", false},
		{"not auth - rate limit", "API rate limit exceeded", false},
		{"not auth - not found", "404 Not Found: repository does not exist", false},

		// Test edge cases
		{"only keyword gh_token", "gh_token", true},
		{"only keyword github_token", "github_token", true},
		{"only keyword authentication", "authentication", true},
		{"only keyword unauthorized", "unauthorized", true},
		{"only keyword forbidden", "forbidden", true},
		{"only keyword permission denied", "permission denied", true},

		// Test with additional context
		{"with newlines", "error:\nauthentication failed\nplease login", true},
		{"with tabs", "error:\tauthentication\tfailed", true},
		{"at start", "forbidden: access denied", true},
		{"at end", "access denied: forbidden", true},
		{"in middle", "the authentication has failed completely", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsAuthError(tt.errMsg)
			if got != tt.want {
				t.Errorf("IsAuthError(%q) = %v, want %v", tt.errMsg, got, tt.want)
			}
		})
	}
}

func TestIsHexString(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want bool
	}{
		// Valid hex strings - lowercase
		{"valid lowercase", "abcdef0123456789", true},
		{"valid all a-f", "abcdef", true},
		{"valid all 0-9", "0123456789", true},

		// Valid hex strings - uppercase
		{"valid uppercase", "ABCDEF0123456789", true},
		{"valid all A-F", "ABCDEF", true},

		// Valid hex strings - mixed case
		{"valid mixed", "AbCdEf0123456789", true},
		{"valid alternating", "AaBbCcDdEeFf", true},

		// Valid real-world SHAs
		{"valid full SHA", "d3422bf940923ef1d43db5559652b8e1e71869f3", true},
		{"valid short SHA 1", "abc123", true},
		{"valid short SHA 2", "deadbeef", true},
		{"valid short SHA 3", "cafebabe", true},

		// Valid single characters
		{"valid single digit 0", "0", true},
		{"valid single digit 9", "9", true},
		{"valid single letter a", "a", true},
		{"valid single letter f", "f", true},
		{"valid single letter A", "A", true},
		{"valid single letter F", "F", true},

		// Valid special cases
		{"valid all zeros", "0000000000", true},
		{"valid all ones", "1111111111", true},
		{"valid all f lowercase", "ffffffff", true},
		{"valid all F uppercase", "FFFFFFFF", true},
		{"valid long SHA", "0123456789abcdef0123456789abcdef01234567", true},

		// Invalid - letters beyond f
		{"invalid - contains g", "abcdefg", false},
		{"invalid - contains h", "abc123h", false},
		{"invalid - contains z", "xyz123", false},
		{"invalid - all invalid letters", "ghijklmnopqrstuvwxyz", false},
		{"invalid - mixed valid and invalid", "abc123xyz", false},

		// Invalid - special characters
		{"invalid - contains space", "abc 123", false},
		{"invalid - contains dash", "abc-123", false},
		{"invalid - contains underscore", "abc_123", false},
		{"invalid - contains dot", "abc.123", false},
		{"invalid - contains slash", "abc/123", false},
		{"invalid - contains backslash", "abc\\123", false},
		{"invalid - contains colon", "abc:123", false},
		{"invalid - contains at", "abc@123", false},
		{"invalid - contains hash", "abc#123", false},
		{"invalid - contains dollar", "abc$123", false},
		{"invalid - contains percent", "abc%123", false},
		{"invalid - contains ampersand", "abc&123", false},
		{"invalid - contains star", "abc*123", false},
		{"invalid - contains plus", "abc+123", false},
		{"invalid - contains equals", "abc=123", false},

		// Invalid - whitespace
		{"invalid - leading space", " abc123", false},
		{"invalid - trailing space", "abc123 ", false},
		{"invalid - tab", "abc\t123", false},
		{"invalid - newline", "abc\n123", false},
		{"invalid - carriage return", "abc\r123", false},

		// Invalid - empty and edge cases
		{"empty string", "", false},
		{"single space", " ", false},
		{"only spaces", "   ", false},

		// Invalid - unicode and non-ASCII
		{"invalid - unicode", "abc123中文", false},
		{"invalid - emoji", "abc123😀", false},
		{"invalid - accented", "ábć123", false},

		// Invalid - parentheses and brackets
		{"invalid - parentheses", "abc(123)", false},
		{"invalid - square brackets", "abc[123]", false},
		{"invalid - curly braces", "abc{123}", false},
		{"invalid - angle brackets", "abc<123>", false},

		// Invalid - quotes
		{"invalid - single quote", "abc'123", false},
		{"invalid - double quote", "abc\"123", false},
		{"invalid - backtick", "abc`123", false},

		// Invalid - punctuation
		{"invalid - comma", "abc,123", false},
		{"invalid - semicolon", "abc;123", false},
		{"invalid - exclamation", "abc!123", false},
		{"invalid - question", "abc?123", false},

		// Valid - boundary testing
		{"valid - exactly 40 chars", "1234567890abcdef1234567890abcdef12345678", true},
		{"valid - 41 chars", "1234567890abcdef1234567890abcdef123456789", true},
		{"valid - 7 chars (short SHA)", "abc1234", true},
		{"valid - 6 chars", "abc123", true},
		{"valid - 2 chars", "ab", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsHexString(tt.s)
			if got != tt.want {
				t.Errorf("IsHexString(%q) = %v, want %v", tt.s, got, tt.want)
			}
		})
	}
}

// Benchmark tests to measure performance
func BenchmarkIsAuthError(b *testing.B) {
	testCases := []string{
		"authentication failed: invalid credentials",
		"file not found: example.txt",
		"403 Forbidden: insufficient permissions",
		"network timeout while connecting to server",
		"AUTHENTICATION ERROR: Please log in",
		"something went wrong",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, tc := range testCases {
			IsAuthError(tc)
		}
	}
}

func BenchmarkIsHexString(b *testing.B) {
	testCases := []string{
		"d3422bf940923ef1d43db5559652b8e1e71869f3",
		"abc123",
		"ABCDEF0123456789",
		"invalid-string-with-special-chars",
		"0123456789",
		"ghijklmnop",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, tc := range testCases {
			IsHexString(tc)
		}
	}
}

// Test edge cases and behavior consistency
func TestIsAuthErrorConsistency(t *testing.T) {
	// Test that function is case-insensitive
	variants := []string{
		"authentication",
		"AUTHENTICATION",
		"Authentication",
		"AuThEnTiCaTiOn",
	}

	for _, v := range variants {
		if !IsAuthError(v) {
			t.Errorf("IsAuthError should be case-insensitive, failed for: %q", v)
		}
	}
}

func TestIsHexStringConsistency(t *testing.T) {
	// Test that function handles both upper and lower case
	testPairs := []struct {
		lower string
		upper string
	}{
		{"abc", "ABC"},
		{"def", "DEF"},
		{"0123456789abcdef", "0123456789ABCDEF"},
		{"deadbeef", "DEADBEEF"},
	}

	for _, pair := range testPairs {
		lowerResult := IsHexString(pair.lower)
		upperResult := IsHexString(pair.upper)

		if lowerResult != upperResult {
			t.Errorf("IsHexString should handle case consistently: %q=%v, %q=%v",
				pair.lower, lowerResult, pair.upper, upperResult)
		}

		if !lowerResult || !upperResult {
			t.Errorf("Both %q and %q should be valid hex strings", pair.lower, pair.upper)
		}
	}
}

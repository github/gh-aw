//go:build !integration

package workflow

import (
	"regexp"
	"testing"
)

// Benchmark the old approach (compiling regex in function)
func BenchmarkRegexCompileInFunction(b *testing.B) {
	text := "2024-01-15 12:34:56 [ERROR] Something went wrong with the process"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Old approach: compile regex every time
		re := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}(\.\d+)?\s+`)
		_ = re.ReplaceAllString(text, "")
	}
}

// Benchmark the new approach (pre-compiled regex)
func BenchmarkRegexPrecompiled(b *testing.B) {
	text := "2024-01-15 12:34:56 [ERROR] Something went wrong with the process"
	// Pre-compile regex once
	re := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}(\.\d+)?\s+`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// New approach: use pre-compiled regex
		_ = re.ReplaceAllString(text, "")
	}
}

// Benchmark realistic scenario: processing multiple log lines
func BenchmarkLogProcessingOld(b *testing.B) {
	lines := []string{
		"2024-01-15 12:34:56 [ERROR] Connection timeout",
		"2024-01-15 12:34:57 [WARNING] Retry attempt 1",
		"2024-01-15 12:34:58 [ERROR] Failed to connect",
		"2024-01-15 12:34:59 [INFO] Retrying...",
		"2024-01-15 12:35:00 [ERROR] Max retries exceeded",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, line := range lines {
			// Old approach: compile for each line
			re1 := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}(\.\d+)?\s+`)
			cleaned := re1.ReplaceAllString(line, "")
			re2 := regexp.MustCompile(`(?i)^\[?(ERROR|WARNING|WARN|INFO|DEBUG)\]?\s*[:-]?\s*`)
			_ = re2.ReplaceAllString(cleaned, "")
		}
	}
}

// Benchmark realistic scenario with pre-compiled regexes
func BenchmarkLogProcessingNew(b *testing.B) {
	lines := []string{
		"2024-01-15 12:34:56 [ERROR] Connection timeout",
		"2024-01-15 12:34:57 [WARNING] Retry attempt 1",
		"2024-01-15 12:34:58 [ERROR] Failed to connect",
		"2024-01-15 12:34:59 [INFO] Retrying...",
		"2024-01-15 12:35:00 [ERROR] Max retries exceeded",
	}

	// Pre-compile regexes once
	re1 := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}(\.\d+)?\s+`)
	re2 := regexp.MustCompile(`(?i)^\[?(ERROR|WARNING|WARN|INFO|DEBUG)\]?\s*[:-]?\s*`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, line := range lines {
			// New approach: use pre-compiled regexes
			cleaned := re1.ReplaceAllString(line, "")
			_ = re2.ReplaceAllString(cleaned, "")
		}
	}
}

// Benchmark codex log parsing scenario
func BenchmarkCodexLogParsingOld(b *testing.B) {
	lines := []string{
		"] tool github.search_issues(...)",
		"tool github.issue_read(...)",
		"] exec ls -la in /tmp",
		"exec cat file.txt in /home",
		"] success in 0.5s",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, line := range lines {
			// Old approach: compile for each line
			regexp.MustCompile(`\] tool ([^(]+)\(`).FindStringSubmatch(line)
			regexp.MustCompile(`^tool ([^(]+)\(`).FindStringSubmatch(line)
			regexp.MustCompile(`\] exec (.+?) in`).FindStringSubmatch(line)
			regexp.MustCompile(`^exec (.+?) in`).FindStringSubmatch(line)
			regexp.MustCompile(`in\s+(\d+(?:\.\d+)?)\s*s`).FindStringSubmatch(line)
		}
	}
}

// Benchmark codex log parsing with pre-compiled regexes
func BenchmarkCodexLogParsingNew(b *testing.B) {
	lines := []string{
		"] tool github.search_issues(...)",
		"tool github.issue_read(...)",
		"] exec ls -la in /tmp",
		"exec cat file.txt in /home",
		"] success in 0.5s",
	}

	// Pre-compile regexes once (as in our optimization)
	re1 := regexp.MustCompile(`\] tool ([^(]+)\(`)
	re2 := regexp.MustCompile(`^tool ([^(]+)\(`)
	re3 := regexp.MustCompile(`\] exec (.+?) in`)
	re4 := regexp.MustCompile(`^exec (.+?) in`)
	re5 := regexp.MustCompile(`in\s+(\d+(?:\.\d+)?)\s*s`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, line := range lines {
			// New approach: use pre-compiled regexes
			re1.FindStringSubmatch(line)
			re2.FindStringSubmatch(line)
			re3.FindStringSubmatch(line)
			re4.FindStringSubmatch(line)
			re5.FindStringSubmatch(line)
		}
	}
}

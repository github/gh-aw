package logger

import (
	"fmt"
	"hash/fnv"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/github/gh-aw/pkg/timeutil"
	"github.com/github/gh-aw/pkg/tty"
)

// Logger represents a debug logger for a specific namespace.
type Logger struct {
	namespace string
	enabled   bool
	lastLog   time.Time
	mu        sync.Mutex
	color     string
}

var (
	// DEBUG environment variable value, read once at initialization.
	debugEnv = os.Getenv("DEBUG")

	// DEBUG_COLORS environment variable to control color output.
	debugColors = os.Getenv("DEBUG_COLORS") != "0"

	// Check if stderr is a terminal (for color support).
	isTTY = tty.IsStderrTerminal()

	// Color palette - chosen to be readable on both light and dark backgrounds.
	// Using ANSI 256-color codes for better compatibility.
	colorPalette = []string{
		"\033[38;5;33m",  // Blue
		"\033[38;5;35m",  // Green
		"\033[38;5;166m", // Orange
		"\033[38;5;125m", // Purple
		"\033[38;5;37m",  // Cyan
		"\033[38;5;161m", // Magenta
		"\033[38;5;136m", // Yellow
		"\033[38;5;124m", // Red
		"\033[38;5;28m",  // Dark green
		"\033[38;5;63m",  // Light blue
		"\033[38;5;95m",  // Brown
		"\033[38;5;21m",  // Dark blue
	}

	colorReset = "\033[0m"
)

// New creates a new Logger for the given namespace.
// The enabled state is computed at construction time based on the DEBUG environment variable.
// DEBUG syntax follows https://www.npmjs.com/package/debug patterns:
//
//	DEBUG=*              - enables all loggers
//	DEBUG=namespace:*    - enables all loggers in a namespace
//	DEBUG=ns1,ns2        - enables specific namespaces
//	DEBUG=ns:*,-ns:skip  - enables namespace but excludes specific patterns
//
// Colors are automatically assigned to each namespace if DEBUG_COLORS != "0" and stderr is a TTY.
func New(namespace string) *Logger {
	enabled := computeEnabled(namespace)
	color := selectColor(namespace)
	return &Logger{
		namespace: namespace,
		enabled:   enabled,
		lastLog:   time.Now(),
		color:     color,
	}
}

// selectColor selects a color for the namespace based on its hash.
func selectColor(namespace string) string {
	if !debugColors || !isTTY {
		return ""
	}

	// Use FNV-1a hash for consistent color assignment
	h := fnv.New32a()
	// hash.Hash.Write never returns an error in practice, but check to satisfy gosec G104
	if _, err := h.Write([]byte(namespace)); err != nil {
		// Return empty string (no color) if write somehow fails
		return ""
	}
	hash := h.Sum32()

	// Select color from palette based on hash
	return colorPalette[hash%uint32(len(colorPalette))]
}

// Enabled returns whether this logger is enabled
func (l *Logger) Enabled() bool {
	return l.enabled
}

// Printf prints a formatted message if the logger is enabled.
// A newline is always added at the end.
// Time diff since last log is displayed like the debug npm package.
func (l *Logger) Printf(format string, args ...any) {
	if !l.enabled {
		return
	}
	l.mu.Lock()
	now := time.Now()
	diff := now.Sub(l.lastLog)
	l.lastLog = now
	l.mu.Unlock()

	message := fmt.Sprintf(format, args...)
	if l.color != "" {
		fmt.Fprintf(os.Stderr, "%s%s%s %s +%s\n", l.color, l.namespace, colorReset, message, timeutil.FormatDuration(diff))
	} else {
		fmt.Fprintf(os.Stderr, "%s %s +%s\n", l.namespace, message, timeutil.FormatDuration(diff))
	}
}

// Print prints a message if the logger is enabled.
// A newline is always added at the end.
// Time diff since last log is displayed like the debug npm package.
func (l *Logger) Print(args ...any) {
	if !l.enabled {
		return
	}
	l.mu.Lock()
	now := time.Now()
	diff := now.Sub(l.lastLog)
	l.lastLog = now
	l.mu.Unlock()

	message := fmt.Sprint(args...)
	if l.color != "" {
		fmt.Fprintf(os.Stderr, "%s%s%s %s +%s\n", l.color, l.namespace, colorReset, message, timeutil.FormatDuration(diff))
	} else {
		fmt.Fprintf(os.Stderr, "%s %s +%s\n", l.namespace, message, timeutil.FormatDuration(diff))
	}
}

// computeEnabled computes whether a namespace matches the DEBUG patterns
func computeEnabled(namespace string) bool {
	patterns := strings.Split(debugEnv, ",")

	enabled := false

	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)

		// Handle exclusion patterns (starting with -)
		if strings.HasPrefix(pattern, "-") {
			excludePattern := strings.TrimPrefix(pattern, "-")
			if matchPattern(namespace, excludePattern) {
				return false // Exclusions take precedence
			}
			continue
		}

		// Check if pattern matches
		if matchPattern(namespace, pattern) {
			enabled = true
		}
	}

	return enabled
}

// matchPattern checks if a namespace matches a pattern
// Supports wildcards (*) for pattern matching
func matchPattern(namespace, pattern string) bool {
	// Exact match or wildcard-all
	if pattern == "*" || pattern == namespace {
		return true
	}

	// Pattern with wildcard
	if strings.Contains(pattern, "*") {
		// Replace * with .* for regex-like matching, but keep it simple
		// Convert pattern to prefix/suffix matching
		if strings.HasSuffix(pattern, "*") {
			prefix := strings.TrimSuffix(pattern, "*")
			return strings.HasPrefix(namespace, prefix)
		}
		if strings.HasPrefix(pattern, "*") {
			suffix := strings.TrimPrefix(pattern, "*")
			return strings.HasSuffix(namespace, suffix)
		}
		// Middle wildcard: split and check both parts
		parts := strings.SplitN(pattern, "*", 2)
		if len(parts) == 2 {
			prefix := parts[0]
			suffix := parts[1]
			return strings.HasPrefix(namespace, prefix) && strings.HasSuffix(namespace, suffix)
		}
	}

	return false
}

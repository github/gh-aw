package logger

import (
	"fmt"
	"hash/fnv"
	"image/color"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/github/gh-aw/pkg/styles"
	"github.com/github/gh-aw/pkg/timeutil"
)

// Logger represents a debug logger for a specific namespace.
type Logger struct {
	namespace string
	enabled   bool
	lastLog   time.Time
	mu        sync.Mutex
	label     string
}

var (
	// DEBUG environment variable value, read once at initialization.
	// GitHub Actions debug mode enables all loggers unless DEBUG is set.
	debugEnv = initDebugEnv()

	// DEBUG_COLORS environment variable to control color output.
	debugColors = os.Getenv("DEBUG_COLORS") != "0" //nolint:osgetenvlibrary

	basePaletteColors = []color.Color{
		styles.ColorInfo,
		styles.ColorSuccess,
		styles.ColorWarning,
		styles.ColorPurple,
		styles.ColorYellow,
		styles.ColorError,
		styles.ColorComment,
		styles.ColorForeground,
		styles.ColorBorder,
	}

	// Color palette for namespace coloring, using adaptive styles.
	colorPalette = buildColorPalette()
)

func buildColorPalette() []lipgloss.Style {
	order := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 0, 1, 3}
	palette := make([]lipgloss.Style, 0, len(order))
	for _, idx := range order {
		baseColor := basePaletteColors[idx]
		palette = append(palette, lipgloss.NewStyle().Foreground(baseColor))
	}
	return palette
}

// initDebugEnv resolves the effective debug pattern.
// If DEBUG is set, it takes precedence. Otherwise, if ACTIONS_RUNNER_DEBUG=true or RUNNER_DEBUG=1,
// all loggers are enabled (equivalent to DEBUG=*).
func initDebugEnv() string {
	if d := os.Getenv("DEBUG"); d != "" { //nolint:osgetenvlibrary
		return d
	}
	if os.Getenv("ACTIONS_RUNNER_DEBUG") == "true" || os.Getenv("RUNNER_DEBUG") == "1" { //nolint:osgetenvlibrary
		return "*"
	}
	return ""
}

// New creates a new Logger for the given namespace.
// The enabled state is computed at construction time based on the DEBUG environment variable.
// DEBUG syntax follows https://www.npmjs.com/package/debug patterns:
//
//	DEBUG=*              - enables all loggers
//	DEBUG=namespace:*    - enables all loggers in a namespace
//	DEBUG=ns1,ns2        - enables specific namespaces
//	DEBUG=ns:*,-ns:skip  - enables namespace but excludes specific patterns
//
// Colors are automatically assigned to each namespace if DEBUG_COLORS != "0".
func New(namespace string) *Logger {
	enabled := computeEnabled(namespace)
	label := selectNamespaceLabel(namespace)
	return &Logger{
		namespace: namespace,
		enabled:   enabled,
		lastLog:   time.Now(),
		label:     label,
	}
}

// selectNamespaceLabel renders the namespace label with a hash-selected style.
func selectNamespaceLabel(namespace string) string {
	if !debugColors || len(colorPalette) == 0 {
		return namespace
	}

	// Use FNV-1a hash for consistent color assignment
	h := fnv.New32a()
	// hash.Hash.Write never returns an error in practice, but check to satisfy gosec G104
	if _, err := io.WriteString(h, namespace); err != nil {
		// Return plain namespace (no color) if write somehow fails
		return namespace
	}
	hash := h.Sum32()

	// Select color from palette based on hash and render namespace with it.
	return colorPalette[int(hash%uint32(len(colorPalette)))].Render(namespace)
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
	diff := l.tickTime()

	message := fmt.Sprintf(format, args...)
	lipgloss.Fprintf(stderrWriter(), "%s %s +%s\n", l.label, message, timeutil.FormatDuration(diff))
}

// Print prints a message if the logger is enabled.
// A newline is always added at the end.
// Time diff since last log is displayed like the debug npm package.
func (l *Logger) Print(args ...any) {
	if !l.enabled {
		return
	}
	diff := l.tickTime()

	message := fmt.Sprint(args...)
	lipgloss.Fprintf(stderrWriter(), "%s %s +%s\n", l.label, message, timeutil.FormatDuration(diff))
}

func (l *Logger) tickTime() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	diff := now.Sub(l.lastLog)
	l.lastLog = now
	return diff
}

// computeEnabled computes whether a namespace matches the DEBUG patterns
func computeEnabled(namespace string) bool {
	patterns := strings.Split(debugEnv, ",")

	enabled := false

	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)

		// Handle exclusion patterns (starting with -)
		if after, ok := strings.CutPrefix(pattern, "-"); ok {
			excludePattern := after
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
		if before, ok := strings.CutSuffix(pattern, "*"); ok {
			prefix := before
			return strings.HasPrefix(namespace, prefix)
		}
		if after, ok := strings.CutPrefix(pattern, "*"); ok {
			suffix := after
			return strings.HasSuffix(namespace, suffix)
		}
		// Middle wildcard: split and check both parts
		if prefix, suffix, ok := strings.Cut(pattern, "*"); ok {
			return len(namespace) >= len(prefix)+len(suffix) &&
				strings.HasPrefix(namespace, prefix) &&
				strings.HasSuffix(namespace, suffix)
		}
	}

	return false
}

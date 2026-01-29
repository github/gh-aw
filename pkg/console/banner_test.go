//go:build !integration

package console

import (
	"strings"
	"testing"
)

func TestFormatBanner(t *testing.T) {
	banner := FormatBanner()

	// Check that the banner contains the expected ASCII art patterns
	// The logo spells out "Agentic" and "Workflows" in ASCII art
	if !strings.Contains(banner, "___") {
		t.Errorf("FormatBanner() should contain ASCII art '___', got: %s", banner)
	}

	if !strings.Contains(banner, "/ _ \\") {
		t.Errorf("FormatBanner() should contain ASCII art '/ _ \\', got: %s", banner)
	}

	// Check that banner is multi-line
	lines := strings.Split(banner, "\n")
	if len(lines) < 10 {
		t.Errorf("FormatBanner() should have at least 10 lines, got %d lines", len(lines))
	}
}

func TestBannerLogoEmbedded(t *testing.T) {
	// Check that the embedded logo is not empty
	if bannerLogo == "" {
		t.Error("bannerLogo should not be empty")
	}

	// Check that it contains the expected ASCII art patterns
	if !strings.Contains(bannerLogo, "___") {
		t.Error("bannerLogo should contain ASCII art pattern '___'")
	}

	if !strings.Contains(bannerLogo, "|") {
		t.Error("bannerLogo should contain ASCII art pipe characters")
	}
}

func TestBannerStyleInitialized(t *testing.T) {
	// Ensure BannerStyle is properly initialized with expected attributes
	// Test by rendering a test string and verifying the style is applied
	testString := "test"
	rendered := BannerStyle.Render(testString)

	// When BannerStyle is properly configured with Bold and Foreground color,
	// the rendered string should differ from the input (contain ANSI codes)
	// or at minimum, not be empty
	if rendered == "" {
		t.Error("BannerStyle.Render should produce non-empty output")
	}

	// Verify Bold is enabled
	if !BannerStyle.GetBold() {
		t.Error("BannerStyle should have Bold enabled")
	}
}

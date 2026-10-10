package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var depsReportLog = logger.New("cli:deps_report")

// DependencyReport contains all dependency health information
type DependencyReport struct {
	TotalDeps    int
	DirectDeps   int
	IndirectDeps int
	Outdated     []OutdatedDependency
	Advisories   []SecurityAdvisory
	V0Count      int
	V1PlusCount  int
	V2PlusCount  int
}

// GenerateDependencyReport creates a comprehensive dependency health report
func GenerateDependencyReport(ctx context.Context, verbose bool) (*DependencyReport, error) {
	depsReportLog.Print("Generating dependency report")

	// Find go.mod file
	goModPath, err := findGoMod()
	if err != nil {
		return nil, fmt.Errorf("failed to find go.mod: %w", err)
	}

	// Parse go.mod to get all dependencies
	depsReportLog.Printf("Parsing go.mod file: %s", goModPath)
	allDeps, err := parseGoModFile(goModPath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse go.mod: %w", err)
	}
	depsReportLog.Printf("Parsed go.mod: %d total dependencies", len(allDeps))

	// Count direct vs indirect dependencies
	directCount := 0
	indirectCount := 0
	v0Count := 0
	v1Count := 0
	v2Count := 0

	for _, dep := range allDeps {
		if dep.Indirect {
			indirectCount++
		} else {
			directCount++
		}

		// Count version maturity
		if strings.HasPrefix(dep.Version, "v0.") {
			v0Count++
		} else if strings.HasPrefix(dep.Version, "v1.") {
			v1Count++
		} else if strings.HasPrefix(dep.Version, "v2.") || strings.HasPrefix(dep.Version, "v3.") {
			v2Count++
		}
	}

	// Check for outdated dependencies (only direct)
	outdated, err := CheckOutdatedDependencies(ctx, verbose)
	if err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(fmt.Sprintf("could not check outdated dependencies: %v", err)))
		}
		outdated = []OutdatedDependency{}
	}

	// Check for security advisories
	advisories, err := CheckSecurityAdvisories(ctx, verbose)
	if err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(fmt.Sprintf("could not check security advisories: %v", err)))
		}
		advisories = []SecurityAdvisory{}
	}

	report := &DependencyReport{
		TotalDeps:    len(allDeps),
		DirectDeps:   directCount,
		IndirectDeps: indirectCount,
		Outdated:     outdated,
		Advisories:   advisories,
		V0Count:      v0Count,
		V1PlusCount:  v1Count,
		V2PlusCount:  v2Count,
	}

	depsReportLog.Printf("Report generated: %d total deps, %d outdated, %d advisories", report.TotalDeps, len(report.Outdated), len(report.Advisories))
	return report, nil
}

// DisplayDependencyReport shows the comprehensive dependency report
func DisplayDependencyReport(report *DependencyReport) {
	fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("═══════════════════════════════════════"))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("  Dependency Health Report"))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("═══════════════════════════════════════"))
	fmt.Fprintln(os.Stderr, "")

	// Summary section
	fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("Summary"))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("-------"))
	fmt.Fprintf(os.Stderr, "Total dependencies: %d (%d direct, %d indirect)\n", report.TotalDeps, report.DirectDeps, report.IndirectDeps)

	outdatedPercentage := 0.0
	if report.DirectDeps > 0 {
		outdatedPercentage = float64(len(report.Outdated)) / float64(report.DirectDeps) * 100
	}
	fmt.Fprintf(os.Stderr, "Outdated: %d (%.0f%%)\n", len(report.Outdated), outdatedPercentage)
	fmt.Fprintf(os.Stderr, "Security advisories: %d\n", len(report.Advisories))

	v0Percentage := safePercent(report.V0Count, report.TotalDeps)
	fmt.Fprintf(os.Stderr, "v0.x dependencies: %d (%.0f%%)", report.V0Count, v0Percentage)
	if v0Percentage > 30 {
		fmt.Fprintf(os.Stderr, " ⚠️")
	}
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "")

	// Outdated dependencies section
	if len(report.Outdated) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("Outdated Dependencies"))
		fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("---------------------"))
		DisplayOutdatedDependencies(report.Outdated, report.DirectDeps)
		fmt.Fprintln(os.Stderr, "")
	}

	// Security status section
	fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("Security Status"))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("---------------"))
	if len(report.Advisories) == 0 {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessageStderr("No known vulnerabilities"))
	} else {
		DisplaySecurityAdvisories(report.Advisories)
	}
	fmt.Fprintln(os.Stderr, "")

	// Dependency maturity section
	fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("Dependency Maturity"))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("-------------------"))
	fmt.Fprintf(os.Stderr, "v0.x (unstable): %d (%.0f%%)", report.V0Count, v0Percentage)
	if v0Percentage > 30 {
		fmt.Fprintf(os.Stderr, " ⚠️")
	}
	fmt.Fprintln(os.Stderr, "")

	v1Percentage := safePercent(report.V1PlusCount, report.TotalDeps)
	fmt.Fprintf(os.Stderr, "v1.x (stable): %d (%.0f%%)\n", report.V1PlusCount, v1Percentage)

	v2Percentage := safePercent(report.V2PlusCount, report.TotalDeps)
	fmt.Fprintf(os.Stderr, "v2+ (mature): %d (%.0f%%)\n", report.V2PlusCount, v2Percentage)
	fmt.Fprintln(os.Stderr, "")

	// Recommendations section
	if len(report.Outdated) > 0 || len(report.Advisories) > 0 || v0Percentage > 30 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("Recommendations"))
		fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("---------------"))

		if len(report.Advisories) > 0 {
			fmt.Fprintf(os.Stderr, "🔴 CRITICAL: Address %d security %s immediately\n", len(report.Advisories), pluralize("advisory", len(report.Advisories)))
		}

		if len(report.Outdated) > 0 {
			fmt.Fprintf(os.Stderr, "📦 Update %d outdated %s\n", len(report.Outdated), pluralize("dependency", len(report.Outdated)))
		}

		if v0Percentage > 30 {
			fmt.Fprintf(os.Stderr, "⚠️  Reduce v0.x exposure from %.0f%% to <30%%\n", v0Percentage)
		}

		fmt.Fprintln(os.Stderr, "")
	}
}

// DisplayDependencyReportJSON outputs the dependency report in JSON format
func DisplayDependencyReportJSON(report *DependencyReport) error {
	depsReportLog.Printf("Generating JSON dependency report: %d total, %d outdated, %d advisories", report.TotalDeps, len(report.Outdated), len(report.Advisories))

	// Calculate percentages
	outdatedPercentage := 0.0
	if report.DirectDeps > 0 {
		outdatedPercentage = float64(len(report.Outdated)) / float64(report.DirectDeps) * 100
	}

	v0Percentage := safePercent(report.V0Count, report.TotalDeps)
	v1Percentage := safePercent(report.V1PlusCount, report.TotalDeps)
	v2Percentage := safePercent(report.V2PlusCount, report.TotalDeps)

	// Build JSON-friendly output structure
	output := map[string]any{
		"summary": map[string]any{
			"total_dependencies":    report.TotalDeps,
			"direct_dependencies":   report.DirectDeps,
			"indirect_dependencies": report.IndirectDeps,
			"outdated_count":        len(report.Outdated),
			"outdated_percentage":   outdatedPercentage,
			"security_advisories":   len(report.Advisories),
			"v0_count":              report.V0Count,
			"v0_percentage":         v0Percentage,
			"v1_count":              report.V1PlusCount,
			"v1_percentage":         v1Percentage,
			"v2_count":              report.V2PlusCount,
			"v2_percentage":         v2Percentage,
		},
		"outdated": report.Outdated,
		"security": report.Advisories,
		"maturity": map[string]any{
			"v0_unstable": map[string]any{
				"count":      report.V0Count,
				"percentage": v0Percentage,
			},
			"v1_stable": map[string]any{
				"count":      report.V1PlusCount,
				"percentage": v1Percentage,
			},
			"v2_mature": map[string]any{
				"count":      report.V2PlusCount,
				"percentage": v2Percentage,
			},
		},
	}

	// Add recommendations
	recommendations := []string{}
	if len(report.Advisories) > 0 {
		recommendations = append(recommendations, fmt.Sprintf("Address %d security %s immediately", len(report.Advisories), pluralize("advisory", len(report.Advisories))))
	}
	if len(report.Outdated) > 0 {
		recommendations = append(recommendations, fmt.Sprintf("Update %d outdated %s", len(report.Outdated), pluralize("dependency", len(report.Outdated))))
	}
	if v0Percentage > 30 {
		recommendations = append(recommendations, fmt.Sprintf("Reduce v0.x exposure from %.0f%% to <30%%", v0Percentage))
	}
	output["recommendations"] = recommendations

	// Marshal and output to stdout
	jsonData, err := marshalIndentJSONOrWrap(output, "dependency report")
	if err != nil {
		return err
	}

	fmt.Fprintln(os.Stdout, string(jsonData))
	return nil
}

// DependencyInfoWithIndirect extends DependencyInfo to track indirect dependencies
type DependencyInfoWithIndirect struct {
	DependencyInfo
	Indirect bool
}

// pluralize returns the singular or plural form of a word based on count
func pluralize(word string, count int) string {
	if count == 1 {
		return word
	}
	// Handle words ending in 'y' preceded by a consonant
	if strings.HasSuffix(word, "y") && len(word) > 1 {
		// Check if the character before 'y' is a consonant
		secondLast := word[len(word)-2]
		if secondLast != 'a' && secondLast != 'e' && secondLast != 'i' && secondLast != 'o' && secondLast != 'u' {
			return word[:len(word)-1] + "ies"
		}
	}
	return word + "s"
}

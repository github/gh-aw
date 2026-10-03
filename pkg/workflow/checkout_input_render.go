package workflow

import (
	"fmt"
	"strings"
)

func (cm *CheckoutManager) defaultCheckoutIndex(entry *resolvedCheckout) int {
	if index, found := cm.index[entry.key]; found {
		return index
	}
	return 0
}

func (cm *CheckoutManager) writeDefaultCheckoutInputs(sb *strings.Builder, override *resolvedCheckout, trialMode bool, trialRepository string) bool {
	if trialMode {
		if trialRepository != "" {
			fmt.Fprintf(sb, "          repository: %s\n", trialRepository)
		}
		fmt.Fprintf(sb, "          token: %s\n", resolveGitHubToken(""))
		return true
	}
	if override == nil {
		return false
	}
	if override.key.wiki {
		fmt.Fprintf(sb, "          repository: %s\n", wikiRepository(override.key.repository))
	} else if override.key.repository != "" {
		fmt.Fprintf(sb, "          repository: %s\n", override.key.repository)
	}
	if override.ref != "" && cm.defaultRefOverride == "" {
		fmt.Fprintf(sb, "          ref: %s\n", override.ref)
	}
	if len(override.sparsePatterns) > 0 {
		// Fetch blobs up front rather than requiring authenticated lazy fetches.
		sb.WriteString("          filter: 'blob:limit=1073741824'\n")
	}
	token := resolveCheckoutTokenExpression(override, cm.defaultCheckoutIndex(override), false)
	if token != "" {
		fmt.Fprintf(sb, "          token: %s\n", token)
	}
	writeCheckoutDepthAndSparse(sb, override)
	writeCheckoutSubmodulesAndLFS(sb, override)
	return token != ""
}

func writeCheckoutDepthAndSparse(sb *strings.Builder, entry *resolvedCheckout) {
	if entry.fetchDepth != nil {
		fmt.Fprintf(sb, "          fetch-depth: %d\n", *entry.fetchDepth)
	}
	if len(entry.sparsePatterns) > 0 {
		sb.WriteString("          sparse-checkout: |\n")
		for _, pattern := range entry.sparsePatterns {
			fmt.Fprintf(sb, "            %s\n", strings.TrimSpace(pattern))
		}
	}
}

func writeCheckoutSubmodulesAndLFS(sb *strings.Builder, entry *resolvedCheckout) {
	if entry.submodules != "" {
		fmt.Fprintf(sb, "          submodules: %s\n", entry.submodules)
	}
	if entry.lfs {
		sb.WriteString("          lfs: true\n")
	}
}

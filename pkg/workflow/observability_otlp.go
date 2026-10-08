package workflow

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/typeutil"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
)

var otlpLog = logger.New("workflow:observability_otlp")

var sentryEndpointNeverMatchPattern = regexp.MustCompile(`$^`)

var sentryEndpointExpressionPattern = func() *regexp.Regexp {
	//nolint:regexpcompileinfunction // The pattern is initialized once at package load.
	re, err := regexp.Compile(`(?i)^\$\{\{\s*secrets\.` + regexp.QuoteMeta(constants.OTELSentryEndpointSecretName) + `\s*\}\}$`) //nolint:regexpdynamicpattern // The secret name is quoted and the fixed pattern is valid.
	if err != nil {
		return sentryEndpointNeverMatchPattern
	}
	return re
}()
var otlpResourceAttributeSecretRefPattern = regexp.MustCompile(`\$\{\{\s*(secrets|vars)\.`)

// workflowEnvHasKey reports whether the given top-level key is already defined in a
// workflow's raw `env:` block text. Used to avoid emitting a duplicate YAML mapping key
// (which would produce an invalid workflow) when the compiler injects OTLP env vars that
// a user may have already defined in their own env: block.
func workflowEnvHasKey(env, key string) bool {
	if env == "" {
		return false
	}
	pattern := `(?m)^\s*` + regexp.QuoteMeta(key) + `:`
	matched, err := regexp.MatchString(pattern, env)
	return err == nil && matched
}

func normalizeOTLPHeadersForEndpoint(raw any, endpoint string) string {
	if raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		if v == "" {
			return ""
		}
		return rewriteOTLPHeaderPairsForEndpoint(v, endpoint)
	case map[string]any:
		if len(v) == 0 {
			return ""
		}
		// Sort keys for deterministic output
		keys := sliceutil.SortedKeys(v)
		var parts []string
		for _, k := range keys {
			val, ok := v[k].(string)
			if !ok {
				otlpLog.Printf("OTLP headers map: value for key %q is not a string (got %T), skipping", k, v[k])
				continue
			}
			parts = append(parts, normalizeOTLPHeaderNameForEndpoint(k, endpoint)+"="+val)
		}
		return strings.Join(parts, ",")
	default:
		otlpLog.Printf("Unexpected type for OTLP headers: %T", raw)
		return ""
	}
}

func rewriteOTLPHeaderPairsForEndpoint(raw string, endpoint string) string {
	if !shouldRewriteAuthorizationForSentry(endpoint) || !strings.Contains(raw, "=") {
		return raw
	}
	if strings.Contains(raw, "Authorization=Sentry sentry_version=") && strings.Contains(raw, ", sentry_key=") {
		otlpLog.Printf("Detected Sentry auth value with commas in string form - this may cause parsing errors. Use map form for headers instead: map[string]any{\"Authorization\": \"...\"}")
	}

	pairs := strings.Split(raw, ",")
	for i, pair := range pairs {
		key, value, found := strings.Cut(pair, "=")
		if !found {
			continue
		}
		pairs[i] = normalizeOTLPHeaderNameForEndpoint(strings.TrimSpace(key), endpoint) + "=" + value
	}

	return strings.Join(pairs, ",")
}

func normalizeOTLPHeaderNameForEndpoint(name string, endpoint string) string {
	if shouldRewriteAuthorizationForSentry(endpoint) && strings.EqualFold(strings.TrimSpace(name), "Authorization") {
		return "x-sentry-auth"
	}

	return name
}

func shouldRewriteAuthorizationForSentry(endpoint string) bool {
	trimmed := strings.TrimSpace(endpoint)
	if trimmed == "" {
		return false
	}
	lowerTrimmed := strings.ToLower(trimmed)

	if parsed, err := url.Parse(trimmed); err == nil {
		if host := strings.ToLower(parsed.Hostname()); host != "" { //nolint:tolowerequalfold
			return strings.Contains(host, "sentry")
		}
	}

	if isGitHubActionsExpression(trimmed) {
		return sentryEndpointExpressionPattern.MatchString(trimmed)
	}

	return strings.Contains(lowerTrimmed, "sentry")
}

// isGitHubActionsExpression returns true when the value is wrapped in GitHub
// Actions expression delimiters like `${{ ... }}` after trimming surrounding
// whitespace.
func isGitHubActionsExpression(value string) bool {
	trimmed := strings.TrimSpace(value)
	return isExpression(trimmed)
}

// extractOTLPEndpointDomain parses an OTLP endpoint URL and returns its hostname.
// Returns an empty string when the endpoint is a GitHub Actions expression (which
// cannot be resolved at compile time) or when the URL is otherwise invalid.
func extractOTLPEndpointDomain(endpoint string) string {
	if endpoint == "" {
		return ""
	}

	// GitHub Actions expressions (e.g. ${{ secrets.OTLP_ENDPOINT }}) cannot be
	// resolved at compile time, so skip domain extraction for them.
	if strings.Contains(endpoint, "${{") {
		otlpLog.Printf("OTLP endpoint is a GitHub Actions expression, skipping domain extraction: %s", endpoint)
		return ""
	}

	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" {
		otlpLog.Printf("Failed to extract domain from OTLP endpoint %q: %v", endpoint, err)
		return ""
	}

	// Strip the port from the host so the AWF domain allowlist entry matches all ports
	// (e.g. "traces.example.com:4317" → "traces.example.com").
	host := parsed.Hostname()
	otlpLog.Printf("Extracted OTLP domain: %s", host)
	return host
}

// getOTLPEndpointEnvValue returns the raw string endpoint value suitable for
// injecting as an environment variable in the generated GitHub Actions workflow YAML.
// Only handles the backward-compat string form of the endpoint field; object/array
// forms are handled by collectAllOTLPEndpoints via RawFrontmatter.
// Returns an empty string when no OTLP endpoint is configured or when the endpoint
// is not a plain string.
func getOTLPEndpointEnvValue(config *FrontmatterConfig) string {
	if config == nil || config.Observability == nil || config.Observability.OTLP == nil {
		return ""
	}
	if ep, ok := config.Observability.OTLP.Endpoint.(string); ok {
		return ep
	}
	return ""
}

func getOTLPGitHubApp(config *FrontmatterConfig, frontmatter map[string]any) *OTLPGitHubAppConfig {
	if config != nil && config.Observability != nil && config.Observability.OTLP != nil && config.Observability.OTLP.GitHubApp != nil {
		return config.Observability.OTLP.GitHubApp
	}
	if frontmatter == nil {
		return nil
	}
	obsAny, ok := frontmatter["observability"]
	if !ok {
		return nil
	}
	obsMap, ok := obsAny.(map[string]any)
	if !ok {
		return nil
	}
	otlpAny, ok := obsMap["otlp"]
	if !ok {
		return nil
	}
	otlpMap, ok := otlpAny.(map[string]any)
	if !ok {
		return nil
	}
	authAny, ok := otlpMap["github-app"]
	if !ok {
		return nil
	}
	authMap, ok := authAny.(map[string]any)
	if !ok {
		return nil
	}
	audience, _ := authMap["audience"].(string)
	return &OTLPGitHubAppConfig{
		Audience: audience,
	}
}

func getOTLPWorkloadIdentity(config *FrontmatterConfig, frontmatter map[string]any) *OTLPWorkloadIdentityConfig {
	if config != nil && config.Observability != nil && config.Observability.OTLP != nil && config.Observability.OTLP.WorkloadIdentity != nil {
		return config.Observability.OTLP.WorkloadIdentity
	}
	if frontmatter == nil {
		return nil
	}
	obs, _ := frontmatter["observability"].(map[string]any)
	otlp, _ := obs["otlp"].(map[string]any)
	workloadIdentity, _ := otlp["workload-identity"].(map[string]any)
	if workloadIdentity == nil {
		return nil
	}
	provider, _ := workloadIdentity["provider"].(string)
	audience, _ := workloadIdentity["audience"].(string)
	serviceAccount, _ := workloadIdentity["service-account"].(string)
	return &OTLPWorkloadIdentityConfig{
		Provider:       provider,
		Audience:       audience,
		ServiceAccount: serviceAccount,
	}
}

// googleWIFAudiences derives the canonical GitHub OIDC and Google STS audiences from a
// workload identity provider value. Google's WIF inputs use three related forms:
//
//	provider resource : projects/<n>/locations/global/workloadIdentityPools/<pool>/providers/<p>
//	GitHub audience   : https://iam.googleapis.com/<provider resource>
//	STS audience      : //iam.googleapis.com/<provider resource>
//
// Users may configure any of these forms; all three are normalized to the same pair.
func googleWIFAudiences(provider string) (githubAudience string, stsAudience string) {
	trimmed := strings.TrimSpace(provider)
	if trimmed == "" {
		return "", ""
	}
	resource := trimmed
	switch {
	case strings.HasPrefix(resource, "https://iam.googleapis.com/"):
		resource = strings.TrimPrefix(resource, "https://iam.googleapis.com/")
	case strings.HasPrefix(resource, "//iam.googleapis.com/"):
		resource = strings.TrimPrefix(resource, "//iam.googleapis.com/")
	}
	// Expressions (e.g. ${{ vars.GCP_WIF_PROVIDER }}) are resolved at runtime, so the
	// canonical prefixes are applied to the raw value as-is.
	return "https://iam.googleapis.com/" + resource, "//iam.googleapis.com/" + resource
}

func getOTLPGitHubAppTokenConfig(frontmatter map[string]any) *GitHubAppConfig {
	if frontmatter == nil {
		return nil
	}

	obsAny, ok := frontmatter["observability"]
	if !ok {
		return nil
	}

	obsMap, ok := obsAny.(map[string]any)
	if !ok {
		return nil
	}

	githubAppMap := extractRawOTLPGitHubAppMap(obsMap)
	if githubAppMap == nil {
		return nil
	}

	app := parseAppConfig(githubAppMap)
	if !app.hasRequiredCredentials() {
		return nil
	}

	return app
}

func hasOTLPGitHubOIDCAuth(config *FrontmatterConfig, frontmatter map[string]any) bool {
	if getOTLPWorkloadIdentity(config, frontmatter) != nil {
		return true
	}
	if getOTLPGitHubAppTokenConfig(frontmatter) != nil {
		return false
	}

	return getOTLPGitHubApp(config, frontmatter) != nil
}

// normalizeOTLPIfMissingMode returns a validated if-missing mode.
// Empty string means "unset/default (error)".
func normalizeOTLPIfMissingMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "":
		return ""
	case "error", "warn", "ignore":
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return ""
	}
}

// getOTLPIfMissingMode returns observability.otlp.if-missing mode.
// Returns empty string when unset or invalid.
func getOTLPIfMissingMode(config *FrontmatterConfig, frontmatter map[string]any) string {
	if config != nil && config.Observability != nil && config.Observability.OTLP != nil {
		if mode := normalizeOTLPIfMissingMode(config.Observability.OTLP.IfMissing); mode != "" {
			return mode
		}
	}
	if frontmatter == nil {
		return ""
	}
	obsAny, ok := frontmatter["observability"]
	if !ok {
		return ""
	}
	obsMap, ok := obsAny.(map[string]any)
	if !ok {
		return ""
	}
	otlpAny, ok := obsMap["otlp"]
	if !ok {
		return ""
	}
	otlpMap, ok := otlpAny.(map[string]any)
	if !ok {
		return ""
	}
	if v, ok := otlpMap["if-missing"].(string); ok {
		if mode := normalizeOTLPIfMissingMode(v); mode != "" {
			return mode
		}
		if strings.TrimSpace(v) != "" {
			otlpLog.Printf("Ignoring invalid observability.otlp.if-missing value %q (expected one of: error, warn, ignore)", v)
		}
	}
	return ""
}

// isOTLPHeadersPresent returns true when OTEL_EXPORTER_OTLP_HEADERS or
// GH_AW_OTLP_ALL_HEADERS has been injected into the workflow-level env block.
// This indicates that header masking is needed so that authentication tokens in
// the header value do not leak into GitHub Actions runner logs.
func isOTLPHeadersPresent(data *WorkflowData) bool {
	if data == nil || data.DryRun {
		return false
	}
	return strings.Contains(data.Env, "OTEL_EXPORTER_OTLP_HEADERS") ||
		strings.Contains(data.Env, "GH_AW_OTLP_ALL_HEADERS")
}

// generateOTLPHeadersMaskStep returns a GitHub Actions step that runs
// mask_otlp_headers.sh to issue the ::add-mask:: workflow command for the
// OTEL_EXPORTER_OTLP_HEADERS environment variable. Masking the value causes the
// GitHub Actions runner to replace any subsequent occurrence of it in the job
// logs with "***", preventing authentication tokens from leaking even when runner
// debug logging is enabled.
//
// The script performs three levels of masking:
//  1. The entire OTEL_EXPORTER_OTLP_HEADERS value (comma-separated header pairs).
//  2. Each individual header value extracted from the pairs, so that a token
//     appearing without its header name prefix is also redacted.
//  3. For Authorization-style "Bearer <token>" credentials, the raw token after
//     stripping the "Bearer " scheme prefix, so it is masked even when it appears
//     without the scheme (e.g. in downstream tool logs).
//
// When GH_AW_OTLP_ALL_HEADERS is set (multi-endpoint case), the same masking
// logic is applied to all headers from all endpoints.
func generateOTLPHeadersMaskStep() string {
	var sb strings.Builder
	sb.WriteString("      - name: Mask OTLP telemetry headers\n")
	sb.WriteString("        run: bash \"${RUNNER_TEMP}/gh-aw/actions/mask_otlp_headers.sh\"\n")
	return sb.String()
}

// isOTLPDefaultCredentialsCheckNeeded returns true when the OTLP endpoint comes
// from the enterprise default environment (vars.GH_AW_DEFAULT_OTLP_ENDPOINT /
// secrets.GH_AW_DEFAULT_OTLP_HEADERS) rather than from workflow frontmatter. In
// that case a runtime check validates that credentials accompany a configured
// endpoint.
func isOTLPDefaultCredentialsCheckNeeded(data *WorkflowData) bool {
	return data != nil && data.OTLPUsesEnterpriseDefaults
}

// generateOTLPDefaultCredentialsCheckStep returns a GitHub Actions step that runs
// check_otlp_default_credentials.sh. The script is a no-op when the enterprise
// default endpoint variable is empty and fails the job when the endpoint is set
// without the matching headers secret.
func generateOTLPDefaultCredentialsCheckStep() string {
	var sb strings.Builder
	sb.WriteString("      - name: Check OTLP telemetry configuration\n")
	sb.WriteString("        run: bash \"${RUNNER_TEMP}/gh-aw/actions/check_otlp_default_credentials.sh\"\n")
	return sb.String()
}

// isOTLPAttributesPresent returns true when GH_AW_OTLP_ATTRIBUTES has been
// injected into the workflow-level env block.  This indicates that attribute
// value masking is needed so that user-supplied values do not leak into
// GitHub Actions runner logs.
func isOTLPAttributesPresent(data *WorkflowData) bool {
	if data == nil || data.DryRun {
		return false
	}
	return strings.Contains(data.Env, "GH_AW_OTLP_ATTRIBUTES")
}

func getOTLPResourceAttributes(workflowData *WorkflowData) map[string]string {
	if workflowData == nil {
		return nil
	}
	resourceAttrs := collectOTLPResourceAttributes(workflowData.RawFrontmatter)
	if len(resourceAttrs) == 0 &&
		workflowData.ParsedFrontmatter != nil &&
		workflowData.ParsedFrontmatter.Observability != nil &&
		workflowData.ParsedFrontmatter.Observability.OTLP != nil {
		resourceAttrs = workflowData.ParsedFrontmatter.Observability.OTLP.ResourceAttributes
	}
	return resourceAttrs
}

func validateOTLPResourceAttributes(workflowData *WorkflowData) error {
	for key, value := range getOTLPResourceAttributes(workflowData) {
		if otlpResourceAttributeSecretRefPattern.MatchString(value) {
			return fmt.Errorf(
				"observability.otlp.resource-attributes.%s must not reference secrets.* or vars.*; OTEL resource attributes are exported to tracing backends and are not treated as secret values",
				key,
			)
		}
	}
	return nil
}

// generateOTLPAttributesMaskStep returns a GitHub Actions step that runs
// mask_otlp_attributes.sh to issue the ::add-mask:: workflow command for every
// value in the GH_AW_OTLP_ATTRIBUTES JSON object.  Masking the values prevents
// user-supplied custom span attribute values (e.g. session IDs, user IDs) from
// appearing in plaintext in GitHub Actions runner logs.
func generateOTLPAttributesMaskStep() string {
	var sb strings.Builder
	sb.WriteString("      - name: Mask OTLP custom attribute values\n")
	sb.WriteString("        run: bash \"${RUNNER_TEMP}/gh-aw/actions/mask_otlp_attributes.sh\"\n")
	return sb.String()
}

// otlpEndpointEntry is the wire format used when encoding the GH_AW_OTLP_ENDPOINTS
// environment variable as a JSON array.  Each entry carries the endpoint URL and
// its optional normalized (comma-separated key=value) headers string.
type otlpEndpointEntry struct {
	URL     string `json:"url"`
	Headers string `json:"headers,omitempty"`
}

// collectOTLPCustomAttributes reads the `observability.otlp.attributes` map from
// a raw frontmatter map and returns it as a map[string]string. Only string values
// are accepted; non-string values are silently ignored. Returns nil when the
// field is absent or empty.
func collectOTLPCustomAttributes(frontmatter map[string]any) map[string]string {
	if frontmatter == nil {
		return nil
	}
	obsAny, ok := frontmatter["observability"]
	if !ok {
		return nil
	}
	obsMap, ok := obsAny.(map[string]any)
	if !ok {
		return nil
	}
	return extractOTLPStringMapFromObsMap(obsMap, "attributes")
}

// collectOTLPResourceAttributes reads the
// `observability.otlp.resource-attributes` map from a raw frontmatter map and
// returns it as a map[string]string. Only string values are accepted; non-string
// values are silently ignored. Returns nil when the field is absent or empty.
func collectOTLPResourceAttributes(frontmatter map[string]any) map[string]string {
	if frontmatter == nil {
		return nil
	}
	obsAny, ok := frontmatter["observability"]
	if !ok {
		return nil
	}
	obsMap, ok := obsAny.(map[string]any)
	if !ok {
		return nil
	}
	return extractOTLPStringMapFromObsMap(obsMap, "resource-attributes")
}

// extractOTLPStringMapFromObsMap reads an `otlp.<fieldName>` string map from a
// raw observability section map (i.e. the value of the "observability" key in
// the frontmatter) and returns it as a map[string]string. Only string values are
// accepted; non-string values are silently ignored. Returns nil when the field is
// absent or empty.
func extractOTLPStringMapFromObsMap(obsMap map[string]any, fieldName string) map[string]string {
	if obsMap == nil {
		return nil
	}
	otlpAny, ok := obsMap["otlp"]
	if !ok {
		return nil
	}
	otlpMap, ok := otlpAny.(map[string]any)
	if !ok {
		return nil
	}
	attrsAny, ok := otlpMap[fieldName]
	if !ok {
		return nil
	}
	attrsMap, ok := attrsAny.(map[string]any)
	if !ok {
		return nil
	}
	result := make(map[string]string, len(attrsMap))
	for k, v := range attrsMap {
		if s, ok := v.(string); ok && k != "" {
			result[k] = s
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// extractOTLPCustomAttributesFromObsMap reads the `otlp.attributes` map from a
// raw observability section map and returns it as a map[string]string.
func extractOTLPCustomAttributesFromObsMap(obsMap map[string]any) map[string]string {
	return extractOTLPStringMapFromObsMap(obsMap, "attributes")
}

// extractOTLPResourceAttributesFromObsMap reads the
// `otlp.resource-attributes` map from a raw observability section map and
// returns it as a map[string]string.
func extractOTLPResourceAttributesFromObsMap(obsMap map[string]any) map[string]string {
	return extractOTLPStringMapFromObsMap(obsMap, "resource-attributes")
}

// encodeOTLPCustomAttributes serialises a map[string]string of custom OTLP span
// attributes to a compact JSON string suitable for use as the GH_AW_OTLP_ATTRIBUTES
// environment variable.  Returns an empty string when the map is nil/empty or
// serialisation fails.
func encodeOTLPCustomAttributes(attrs map[string]string) string {
	if len(attrs) == 0 {
		return ""
	}
	b, err := json.Marshal(attrs)
	if err != nil {
		otlpLog.Printf("Failed to encode OTLP custom attributes: %v", err)
		return ""
	}
	return string(b)
}

// mergeOTLPStringMaps merges two string maps; values in base take precedence over
// values in override when the same key is present in both. Returns nil when both
// inputs are empty.
func mergeOTLPStringMaps(base, override map[string]string) map[string]string {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}
	merged := make(map[string]string, typeutil.SafeAllocationCapacity(len(base), len(override)))
	maps.Copy(merged, override)
	// base takes precedence
	maps.Copy(merged, base)
	return merged
}

// collectAllOTLPEndpoints reads the `observability.otlp.endpoint` field from the raw
// frontmatter and returns all configured endpoint entries. The `endpoint` field may be:
//
//   - a string:  backward-compat URL; optional top-level `headers` field applies
//   - an object: {url: "...", headers: {...}} — single endpoint with per-endpoint headers
//   - an array:  [{url: ..., headers: ...}, ...] — multiple endpoints for concurrent fan-out
//
// Returns a non-nil slice when at least one valid endpoint is found.
func collectAllOTLPEndpoints(frontmatter map[string]any) []otlpEndpointEntry {
	var entries []otlpEndpointEntry

	obs, ok := frontmatter["observability"]
	if !ok {
		return entries
	}
	obsMap, ok := obs.(map[string]any)
	if !ok {
		return entries
	}
	otlpRaw, ok := obsMap["otlp"]
	if !ok {
		return entries
	}
	otlpMap, ok := otlpRaw.(map[string]any)
	if !ok {
		return entries
	}

	endpointRaw := otlpMap["endpoint"]
	topHeadersRaw := otlpMap["headers"] // only used with backward-compat string form

	switch ep := endpointRaw.(type) {
	case string:
		// Backward-compat string form: endpoint: "https://..."
		if ep != "" {
			headers := normalizeOTLPHeadersForEndpoint(topHeadersRaw, ep)
			entries = append(entries, otlpEndpointEntry{URL: ep, Headers: headers})
		}
	case map[string]any:
		// Object form: endpoint: {url: "...", headers: {...}}
		if url, _ := ep["url"].(string); url != "" {
			headers := ""
			if h, hasH := ep["headers"]; hasH {
				headers = normalizeOTLPHeadersForEndpoint(h, url)
			}
			entries = append(entries, otlpEndpointEntry{URL: url, Headers: headers})
		}
	case []any:
		// Array form: endpoint: [{url: ..., headers: {...}}, ...]
		for _, item := range ep {
			itemMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			url, _ := itemMap["url"].(string)
			if url == "" {
				continue
			}
			headers := ""
			if h, hasH := itemMap["headers"]; hasH {
				headers = normalizeOTLPHeadersForEndpoint(h, url)
			}
			entries = append(entries, otlpEndpointEntry{URL: url, Headers: headers})
		}
	}

	return entries
}

// encodeOTLPEndpoints serialises a slice of otlpEndpointEntry values to a compact
// JSON string suitable for use as the GH_AW_OTLP_ENDPOINTS environment variable.
// Returns an empty string when the slice is empty or serialisation fails.
func encodeOTLPEndpoints(entries []otlpEndpointEntry) string {
	if len(entries) == 0 {
		return ""
	}
	b, err := json.Marshal(entries)
	if err != nil {
		otlpLog.Printf("Failed to encode OTLP endpoints: %v", err)
		return ""
	}
	return string(b)
}

// extractRawOTLPEndpointMaps returns OTLP endpoint entries as []map[string]any from
// an observability section map. Unlike collectAllOTLPEndpoints, headers are kept in
// their original format (string or map) so that no false deprecation warnings are
// emitted when the merged result is later processed by collectAllOTLPEndpoints.
// Supports string, object, and array forms of the endpoint field.
// Top-level `headers` is only applied to the backward-compat string endpoint form.
func extractRawOTLPEndpointMaps(obs map[string]any) []map[string]any {
	if obs == nil {
		return nil
	}
	otlpAny, ok := obs["otlp"]
	if !ok {
		return nil
	}
	otlpMap, ok := otlpAny.(map[string]any)
	if !ok {
		return nil
	}

	endpointRaw := otlpMap["endpoint"]
	headersRaw := otlpMap["headers"] // only applies to the backward-compat string form

	var result []map[string]any
	switch ep := endpointRaw.(type) {
	case string:
		if ep != "" {
			entry := map[string]any{"url": ep}
			if headersRaw != nil {
				entry["headers"] = headersRaw
			}
			result = append(result, entry)
		}
	case map[string]any:
		if url, _ := ep["url"].(string); url != "" {
			// Shallow copy: top-level keys (url, headers) are copied. The headers
			// value (a map[string]any) is shared by reference, but it is never mutated
			// downstream — it is only read by normalizeOTLPHeaders and collectAllOTLPEndpoints.
			entry := make(map[string]any, len(ep))
			maps.Copy(entry, ep)
			result = append(result, entry)
		}
	case []any:
		for _, item := range ep {
			itemMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if url, _ := itemMap["url"].(string); url != "" {
				// Shallow copy: see note above — headers value is never mutated.
				entry := make(map[string]any, len(itemMap))
				maps.Copy(entry, itemMap)
				result = append(result, entry)
			}
		}
	}
	return result
}

// extractRawOTLPGitHubAppMap returns observability.otlp.github-app as a
// shallow-copied map when present and valid.
func extractRawOTLPGitHubAppMap(obs map[string]any) map[string]any {
	if obs == nil {
		return nil
	}
	otlpAny, ok := obs["otlp"]
	if !ok {
		return nil
	}
	otlpMap, ok := otlpAny.(map[string]any)
	if !ok {
		return nil
	}
	githubAppAny, ok := otlpMap["github-app"]
	if !ok {
		return nil
	}
	githubAppMap, ok := githubAppAny.(map[string]any)
	if !ok {
		return nil
	}
	copied := make(map[string]any, len(githubAppMap))
	maps.Copy(copied, githubAppMap)
	return copied
}

// endpoint entry.  Duplicate pairs are included as-is; the result is used only
// for secret-masking and contains no sensitive data itself after runtime
// expression substitution by GitHub Actions.
// Returns an empty string when no endpoint has headers configured.
func allOTLPHeaders(entries []otlpEndpointEntry) string {
	var parts []string
	for _, e := range entries {
		if e.Headers != "" {
			parts = append(parts, e.Headers)
		}
	}
	return strings.Join(parts, ",")
}

//  1. When endpoints contain static URLs, their hostnames are appended to
//     NetworkPermissions.Allowed so the AWF firewall allows outbound traffic to them.
//
//  2. OTEL_EXPORTER_OTLP_ENDPOINT and OTEL_SERVICE_NAME are appended to the
//     workflow-level env: YAML block (workflowData.Env) so they are available to
//     every step in the generated GitHub Actions workflow.
//
//  3. GH_AW_OTLP_ENDPOINTS is injected as a JSON-encoded array of all endpoint
//     entries so that JavaScript can fan out spans to multiple collectors concurrently.
//
//  4. When any endpoint has headers configured, OTEL_EXPORTER_OTLP_HEADERS is
//     injected for the first endpoint (backward compat) and GH_AW_OTLP_ALL_HEADERS
//     is injected with all headers across every endpoint (for secret masking).
//
//  5. OTEL_RESOURCE_ATTRIBUTES is injected with gh-aw/GitHub run context so child
//     OTel SDKs (Copilot CLI, MCP gateway) inherit correlation attributes.
//
//  6. When observability.otlp.attributes is configured, GH_AW_OTLP_ATTRIBUTES is
//     injected as a JSON-encoded map so that span-emitting scripts can append custom
//     attributes (including Langfuse session/user IDs) to every span.
//
// When no OTLP endpoint is configured the function is a no-op.
// resolveOTLPEndpointEntries returns the OTLP endpoint entries configured for the
// workflow together with a flag indicating whether they came from the enterprise
// default environment rather than from frontmatter.
//
// When no endpoint is configured anywhere in the frontmatter (main workflow or
// imports), the endpoint is read from the GH_AW_DEFAULT_OTLP_ENDPOINT Actions
// variable and its headers from the GH_AW_DEFAULT_OTLP_HEADERS secret. Both
// expressions resolve at run time, so an organization or enterprise can enable
// telemetry for every agentic workflow without editing individual workflows.
// When neither is set, the expressions resolve to empty strings and OTLP export
// is a no-op.
func resolveOTLPEndpointEntries(workflowData *WorkflowData) (entries []otlpEndpointEntry, usesEnterpriseDefaults bool) {
	// Collect all endpoint entries from the endpoint field (string, object, or array).
	entries = collectAllOTLPEndpoints(workflowData.RawFrontmatter)

	// Fall back to ParsedFrontmatter when raw map extraction found nothing.
	if len(entries) == 0 {
		if ep := getOTLPEndpointEnvValue(workflowData.ParsedFrontmatter); ep != "" {
			var h string
			if workflowData.ParsedFrontmatter.Observability != nil &&
				workflowData.ParsedFrontmatter.Observability.OTLP != nil {
				h = normalizeOTLPHeadersForEndpoint(workflowData.ParsedFrontmatter.Observability.OTLP.Headers, ep)
			}
			entries = []otlpEndpointEntry{{URL: ep, Headers: h}}
		}
	}

	if len(entries) == 0 {
		otlpLog.Print("No OTLP endpoint configured in frontmatter; using enterprise default env vars")
		return []otlpEndpointEntry{{
			URL:     compilerenv.BuildDefaultOTLPEndpointExpression(),
			Headers: compilerenv.BuildDefaultOTLPHeadersExpression(),
		}}, true
	}

	return entries, false
}

// allowOTLPEndpointDomains appends the statically resolvable OTLP endpoint
// hostnames (and the fixed Google workload identity hosts when applicable) to the
// workflow firewall allowlist. Expression endpoints cannot be resolved at compile
// time and are skipped.
func allowOTLPEndpointDomains(workflowData *WorkflowData, entries []otlpEndpointEntry) {
	allow := func(domains ...string) {
		if workflowData.NetworkPermissions == nil {
			workflowData.NetworkPermissions = &NetworkPermissions{}
		}
		workflowData.NetworkPermissions.Allowed = append(workflowData.NetworkPermissions.Allowed, domains...)
	}

	if workloadIdentity := getOTLPWorkloadIdentity(workflowData.ParsedFrontmatter, workflowData.RawFrontmatter); workloadIdentity != nil &&
		strings.EqualFold(strings.TrimSpace(workloadIdentity.Provider), "google") {
		allow("sts.googleapis.com", "iamcredentials.googleapis.com")
		otlpLog.Print("Added Google workload identity hosts to network allowlist")
	}
	for _, e := range entries {
		if domain := extractOTLPEndpointDomain(e.URL); domain != "" {
			allow(domain)
			otlpLog.Printf("Added OTLP domain to network allowlist: %s", domain)
		}
	}
}

// buildOTLPHeaderEnvLines returns the workflow env: lines carrying OTLP header values.
// OTEL_EXPORTER_OTLP_HEADERS holds the first endpoint headers (backward compat) and
// GH_AW_OTLP_ALL_HEADERS holds all endpoint headers comma-joined (used for masking).
// existingEnv is the workflow's user-defined env: block (before OTLP injection); keys
// already present there are skipped to avoid emitting a duplicate YAML mapping key.
func buildOTLPHeaderEnvLines(entries []otlpEndpointEntry, existingEnv string) string {
	var lines string
	if firstHeaders := entries[0].Headers; firstHeaders != "" {
		if workflowEnvHasKey(existingEnv, "OTEL_EXPORTER_OTLP_HEADERS") {
			otlpLog.Printf("Skipping OTEL_EXPORTER_OTLP_HEADERS injection: already defined by user")
		} else {
			lines += "\n  OTEL_EXPORTER_OTLP_HEADERS: " + firstHeaders
			otlpLog.Printf("Injected OTEL_EXPORTER_OTLP_HEADERS env var")
		}
	}
	if allHeaders := allOTLPHeaders(entries); allHeaders != "" && len(entries) > 1 {
		if workflowEnvHasKey(existingEnv, "GH_AW_OTLP_ALL_HEADERS") {
			otlpLog.Printf("Skipping GH_AW_OTLP_ALL_HEADERS injection: already defined by user")
		} else {
			lines += "\n  GH_AW_OTLP_ALL_HEADERS: " + allHeaders
			otlpLog.Printf("Injected GH_AW_OTLP_ALL_HEADERS env var for %d endpoints", len(entries))
		}
	}
	return lines
}

// buildOTLPPayloadEnvLines returns the workflow env: lines carrying the encoded OTLP
// endpoint list, the resolved if-missing policy, and the custom span attributes.
// JSON values are single-quoted so YAML parsers do not interpret a leading '[' or '{'
// as a collection node rather than a plain string.
// existingEnv is the workflow's user-defined env: block (before OTLP injection); keys
// already present there are skipped to avoid emitting a duplicate YAML mapping key.
func buildOTLPPayloadEnvLines(workflowData *WorkflowData, entries []otlpEndpointEntry, ifMissingMode string, existingEnv string) string {
	var lines string
	if encoded := encodeOTLPEndpoints(entries); encoded != "" {
		if workflowEnvHasKey(existingEnv, "GH_AW_OTLP_ENDPOINTS") {
			otlpLog.Printf("Skipping GH_AW_OTLP_ENDPOINTS injection: already defined by user")
		} else {
			lines += "\n  GH_AW_OTLP_ENDPOINTS: '" + escapeYAMLSingleQuoted(encoded) + "'"
			otlpLog.Printf("Injected GH_AW_OTLP_ENDPOINTS env var")
		}
	}
	if ifMissingMode == "warn" || ifMissingMode == "ignore" {
		if workflowEnvHasKey(existingEnv, "GH_AW_OTLP_IF_MISSING") {
			otlpLog.Printf("Skipping GH_AW_OTLP_IF_MISSING injection: already defined by user")
		} else {
			lines += "\n  GH_AW_OTLP_IF_MISSING: " + ifMissingMode
			otlpLog.Printf("Injected GH_AW_OTLP_IF_MISSING env var (%s)", ifMissingMode)
		}
	}

	// Attributes from RawFrontmatter take precedence; ParsedFrontmatter is the
	// fallback for workflows that were parsed but whose RawFrontmatter was later
	// modified (e.g. during observability merge in the orchestrator).
	customAttrs := collectOTLPCustomAttributes(workflowData.RawFrontmatter)
	if len(customAttrs) == 0 && workflowData.ParsedFrontmatter != nil &&
		workflowData.ParsedFrontmatter.Observability != nil &&
		workflowData.ParsedFrontmatter.Observability.OTLP != nil {
		customAttrs = workflowData.ParsedFrontmatter.Observability.OTLP.Attributes
	}
	if encoded := encodeOTLPCustomAttributes(customAttrs); encoded != "" {
		if workflowEnvHasKey(existingEnv, "GH_AW_OTLP_ATTRIBUTES") {
			otlpLog.Printf("Skipping GH_AW_OTLP_ATTRIBUTES injection: already defined by user")
		} else {
			lines += "\n  GH_AW_OTLP_ATTRIBUTES: '" + escapeYAMLSingleQuoted(encoded) + "'"
			otlpLog.Printf("Injected GH_AW_OTLP_ATTRIBUTES env var (%d custom attributes)", len(customAttrs))
		}
	}
	return lines
}

func (c *Compiler) injectOTLPConfig(workflowData *WorkflowData) {
	entries, usesEnterpriseDefaults := resolveOTLPEndpointEntries(workflowData)
	workflowData.OTLPUsesEnterpriseDefaults = usesEnterpriseDefaults

	otlpLog.Printf("Injecting OTLP configuration: %d endpoint(s)", len(entries))

	// 1. Add all static OTLP endpoint domains to the firewall allowlist.
	allowOTLPEndpointDomains(workflowData, entries)

	firstEndpoint := entries[0].URL
	firstHeaders := entries[0].Headers
	serviceName := otelServiceName(workflowData)
	ifMissingMode := getOTLPIfMissingMode(workflowData.ParsedFrontmatter, workflowData.RawFrontmatter)
	if usesEnterpriseDefaults && ifMissingMode == "" {
		// The enterprise default endpoint is opt-in through Actions variables, so an
		// unset variable must not fail the workflow: unresolved values are ignored.
		ifMissingMode = "ignore"
	}

	// 2. Inject OTEL env vars into the workflow-level env: block.
	//    OTEL_EXPORTER_OTLP_ENDPOINT is set to the first endpoint for backward
	//    compatibility (MCP gateway, legacy scripts). OTEL_SERVICE_NAME is
	//    workflow-specific when WorkflowID is available.
	//    existingEnv captures the user's env: block as it stood before this function
	//    started appending lines, so every key below is checked against user-authored
	//    content only. If the user has already defined any of these keys in their env
	//    block, we respect their value and skip injection to avoid duplicate key errors.
	existingEnv := workflowData.Env
	var envKeyLines []string
	addEnvLine := func(key, line string) {
		if workflowEnvHasKey(existingEnv, key) {
			otlpLog.Printf("Skipping %s injection: already defined by user", key)
			return
		}
		envKeyLines = append(envKeyLines, line)
	}
	addEnvLine("OTEL_EXPORTER_OTLP_ENDPOINT", "  OTEL_EXPORTER_OTLP_ENDPOINT: "+firstEndpoint)
	addEnvLine("OTEL_SERVICE_NAME", "  OTEL_SERVICE_NAME: "+serviceName)
	addEnvLine("OTEL_RESOURCE_ATTRIBUTES", "  OTEL_RESOURCE_ATTRIBUTES: '"+escapeYAMLSingleQuoted(otelResourceAttributes(workflowData))+"'")
	otlpEnvLines := strings.Join(envKeyLines, "\n")

	// 3. Inject per-endpoint headers env vars.
	otlpEnvLines += buildOTLPHeaderEnvLines(entries, existingEnv)

	// 4. Inject GH_AW_OTLP_ENDPOINTS (JSON array) so JavaScript can fan out spans,
	//    GH_AW_OTLP_IF_MISSING for the resolved missing-configuration policy, and
	//    GH_AW_OTLP_ATTRIBUTES (JSON object) for custom per-span attributes.
	otlpEnvLines += buildOTLPPayloadEnvLines(workflowData, entries, ifMissingMode, existingEnv)
	otlpEnvLines = strings.TrimPrefix(otlpEnvLines, "\n")

	if otlpEnvLines == "" {
		otlpLog.Printf("No OTLP env vars injected: all keys already defined by user")
	} else if workflowData.Env == "" {
		workflowData.Env = "env:\n" + otlpEnvLines
	} else {
		workflowData.Env = workflowData.Env + "\n" + otlpEnvLines
	}
	otlpLog.Printf("Injected OTEL env vars into workflow env block")

	// Store the resolved values so downstream code (mcp_gateway_config,
	// mcp_setup_generator) can use workflowData fields as the single source of truth.
	workflowData.OTLPEndpoint = firstEndpoint
	workflowData.OTLPHeaders = firstHeaders
	workflowData.OTLPEndpoints = encodeOTLPEndpoints(entries)
}

func otelServiceName(workflowData *WorkflowData) string {
	const defaultServiceName = "gh-aw"
	if workflowData == nil {
		return defaultServiceName
	}

	// Prefer the file-based WorkflowID to avoid collisions across workflows that
	// may share display names; fall back to workflow Name when WorkflowID is
	// unavailable (for workflow_call-only contexts).
	workflowIDOrName := strings.TrimSpace(workflowData.WorkflowID)
	if workflowIDOrName == "" {
		workflowIDOrName = workflowData.Name
	}

	// SanitizeWorkflowName lowercases the workflow identifier and converts
	// separators/special characters (spaces, slashes, etc.) to hyphens so the
	// service suffix is stable and backend-friendly.
	sanitizedWorkflowName := SanitizeWorkflowName(workflowIDOrName)
	if sanitizedWorkflowName == "" {
		return defaultServiceName
	}

	return defaultServiceName + "." + sanitizedWorkflowName
}

// encodeOTELResourceAttributeValue applies RFC 3986 percent-encoding to the
// UTF-8 bytes used in OTEL_RESOURCE_ATTRIBUTES keys/values.
func encodeOTELResourceAttributeValue(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

func formatOTELResourceAttribute(key, value string) string {
	trimmedKey := strings.TrimSpace(key)
	trimmedValue := strings.TrimSpace(value)
	if strings.Contains(trimmedValue, "${{") {
		return encodeOTELResourceAttributeValue(trimmedKey) + "=" + trimmedValue
	}
	return encodeOTELResourceAttributeValue(trimmedKey) + "=" + encodeOTELResourceAttributeValue(trimmedValue)
}

func otelResourceAttributes(workflowData *WorkflowData) string {
	workflowNameAttrValue := "unknown"
	if workflowData != nil {
		if workflowName := strings.TrimSpace(workflowData.Name); workflowName != "" {
			workflowNameAttrValue = workflowName
		}
	}

	attrs := []string{
		formatOTELResourceAttribute("gh-aw.workflow.name", workflowNameAttrValue),
		formatOTELResourceAttribute("gh-aw.repository", "${{ github.repository }}"),
		formatOTELResourceAttribute("gh-aw.run.id", "${{ github.run_id }}"),
		formatOTELResourceAttribute("github.run_id", "${{ github.run_id }}"),
	}
	if engineID := ResolveEngineID(workflowData); engineID != "" {
		attrs = append(attrs, formatOTELResourceAttribute("gh-aw.engine.id", engineID))
	}
	resourceAttrs := getOTLPResourceAttributes(workflowData)
	if len(resourceAttrs) > 0 {
		keys := sliceutil.SortedKeys(resourceAttrs)
		for _, key := range keys {
			attrs = append(attrs, formatOTELResourceAttribute(key, resourceAttrs[key]))
		}
	}
	return strings.Join(attrs, ",")
}

package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var engineReleaseVersionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)
var engineReleasePackagePattern = regexp.MustCompile(`^(@[a-z0-9][a-z0-9._-]*/)?[a-z0-9][a-z0-9._-]*$`)
var engineReleaseCooldownLog = logger.New("workflow:engine_release_cooldown")
var engineReleaseNPMPackages = map[string]string{
	"claude": claudeCodeNPMPackage,
	"codex":  codexNPMPackage,
	"pi":     piNPMPackage,
	"gemini": geminiCLINPMPackage,
}

const engineReleaseCacheTTL = 5 * time.Minute

type engineReleaseMetadata struct {
	published time.Time
	fetched   time.Time
	err       error
}

// Cache publication times, not policy decisions: another workflow can require a
// longer cooldown, and a young release can mature while the compiler is running.
// Both successes and failures expire after five minutes; nothing is persisted to disk.
type engineReleaseCache struct {
	sync.Mutex
	entries map[string]engineReleaseMetadata
}

var sharedEngineReleaseCache = &engineReleaseCache{entries: map[string]engineReleaseMetadata{}}
var engineReleaseTimeLookup = (*Compiler).fetchEngineReleaseTime

func literalEngineReleaseVersion(version string) bool {
	return engineReleaseVersionPattern.MatchString(version)
}

func (c *Compiler) validateEngineReleaseCooldowns(data *WorkflowData, markdownPath string) error {
	if data == nil {
		return nil
	}
	if err := c.validateEngineReleaseCooldown(data, markdownPath); err != nil {
		return err
	}
	if IsDetectionJobEnabled(data.SafeOutputs) && !data.SafeOutputs.ThreatDetection.EngineDisabled {
		id := c.getThreatDetectionEngineID(data)
		// Mirror the inline detector installer, which resets task settings (including copilot-sdk).
		config := resolveExternalDetectorEngineConfig(data, id)
		resetDetectionEngineTaskSettings(config)
		detector := &WorkflowData{AI: id, EngineConfig: config}
		if isFeatureEnabled(constants.GHAWDetectionFeatureFlag, data) {
			detector = buildExternalDetectorWorkflowData(data, id)
		}
		if ResolveEngineID(data) == id {
			detector.engineDefinition = data.engineDefinition
		}
		if err := c.validateEngineReleaseCooldown(detector, markdownPath); err != nil {
			return fmt.Errorf("safe-outputs.threat-detection: %w", err)
		}
	}
	return nil
}

type engineReleaseTarget struct {
	id          string
	version     string
	packageName string
	endpoint    string
}

func (c *Compiler) engineReleaseTarget(data *WorkflowData) (engineReleaseTarget, bool) {
	id := ResolveEngineID(data)
	def := data.engineDefinition
	if def == nil && c.engineCatalog != nil {
		def = c.engineCatalog.Get(id)
	}
	runtimeID := id
	version := getVersionForSetup(data, c.engineRegistry)
	// Resolve the runtime the same way EngineCatalog.Resolve does: catalog
	// definitions bind to their runtime ID, other IDs fall back to prefix lookup.
	engine, err := c.engineRegistry.GetEngineByPrefix(id)
	if def != nil && def.RuntimeID != "" {
		engine, err = c.engineRegistry.GetEngine(def.RuntimeID)
	}
	if err == nil {
		runtimeID = engine.GetID()
		version = getInstallationVersion(data, engine, c.engineRegistry)
		if behaviorEngine, ok := engine.(*BehaviorDefinedEngine); ok && (def == nil || def.Behaviors == nil) {
			def = behaviorEngine.definition
		}
	}
	if def != nil && def.RuntimeID != "" {
		runtimeID = def.RuntimeID
	}
	packageName := ""
	if def != nil && def.Behaviors != nil {
		install := def.Behaviors.Installation
		if install == nil || install.PackageManager != "npm" {
			return engineReleaseTarget{}, false
		}
		packageName = install.PackageName
		if version == "" {
			version = install.Version
		}
		if data.EngineConfig != nil && data.EngineConfig.Version != "" {
			version = data.EngineConfig.Version
		}
	} else if runtimeID != "copilot" {
		var supported bool
		if packageName, supported = engineReleaseNPMPackages[runtimeID]; !supported {
			return engineReleaseTarget{}, false
		}
	}
	if !literalEngineReleaseVersion(version) {
		return engineReleaseTarget{}, false
	}
	version = strings.TrimPrefix(version, "v")
	endpoint := "https://api.github.com/repos/github/copilot-cli/releases/tags/v" + url.PathEscape(version)
	if packageName != "" {
		if !engineReleasePackagePattern.MatchString(packageName) {
			return engineReleaseTarget{}, false
		}
		endpoint = "https://registry.npmjs.org/" + url.PathEscape(packageName)
	}
	return engineReleaseTarget{id: id, version: version, packageName: packageName, endpoint: endpoint}, true
}

func (c *Compiler) validateEngineReleaseCooldown(data *WorkflowData, markdownPath string) error {
	if data == nil {
		return nil
	}
	days := resolveEngineCooldownDays(data)
	if days == 0 || (data.EngineConfig != nil && data.EngineConfig.Command != "") {
		return nil
	}
	target, supported := c.engineReleaseTarget(data)
	if !supported {
		return nil
	}
	now := time.Now()
	if c.engineReleaseNow != nil {
		now = c.engineReleaseNow()
	}
	metadata := c.engineReleaseMetadata(target, now)
	if c.ctx != nil && c.ctx.Err() != nil {
		return c.ctx.Err()
	}
	if metadata.err != nil {
		c.warnEngineReleaseCooldown(target, days, markdownPath)
		return nil
	}
	if now.Sub(metadata.published).Hours()/24 < float64(days) {
		return NewValidationError("engine.cooldown", fmt.Sprintf("%s@%s", target.id, target.version),
			fmt.Sprintf("release published at %s is younger than the required %d-day cooldown", metadata.published.Format(time.RFC3339), days),
			"Choose an older engine.version or wait until the release satisfies the cooldown.")
	}
	return nil
}

func (c *Compiler) engineReleaseMetadata(target engineReleaseTarget, now time.Time) engineReleaseMetadata {
	if c.verifyAuthorization || c.contentOverride != "" {
		return engineReleaseMetadata{err: errors.New("metadata lookup is unavailable during offline or editor compilation")}
	}
	cache := c.engineReleaseCache
	if cache == nil {
		cache = sharedEngineReleaseCache
		if c.engineReleaseClient != nil {
			cache = &engineReleaseCache{entries: map[string]engineReleaseMetadata{}}
		}
		c.engineReleaseCache = cache
	}
	key := target.endpoint + "@" + target.version
	cache.Lock()
	metadata, found := cache.entries[key]
	cache.Unlock()
	if found && now.Sub(metadata.fetched) < engineReleaseCacheTTL && !now.Before(metadata.fetched) {
		return metadata
	}
	// Fetch without holding the lock so a slow lookup does not serialize
	// unrelated keys or concurrent compiles; a duplicate fetch is harmless.
	metadata = engineReleaseMetadata{fetched: now}
	metadata.published, metadata.err = engineReleaseTimeLookup(c, target.endpoint, target.packageName, target.version)
	if metadata.err == nil && (metadata.published.IsZero() || metadata.published.After(now)) {
		metadata.err = errors.New("missing or future release publication timestamp")
	}
	if c.ctx == nil || c.ctx.Err() == nil {
		cache.Lock()
		if cache.entries == nil {
			cache.entries = map[string]engineReleaseMetadata{}
		}
		cache.entries[key] = metadata
		cache.Unlock()
	}
	return metadata
}

func (c *Compiler) warnEngineReleaseCooldown(target engineReleaseTarget, days int, markdownPath string) {
	key := target.endpoint + "@" + target.version
	if c.engineReleaseWarnings == nil {
		c.engineReleaseWarnings = map[string]bool{}
	}
	if !c.engineReleaseWarnings[key] {
		c.engineReleaseWarnings[key] = true
		message := fmt.Sprintf("Could not validate engine.cooldown for %s@%s from public release metadata; validate the release age manually against every engine.cooldown that applies to this release, including safe-outputs.threat-detection (this check required %d days), before running this workflow.", target.id, target.version, days)
		fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "warning", message))
		c.IncrementWarningCount()
	}
}

func (c *Compiler) fetchEngineReleaseTime(endpoint, packageName, version string) (time.Time, error) {
	ctx := c.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return time.Time{}, err
	}
	request.Header.Set("Accept", "application/json")
	// Never inherit GH_HOST, registry configuration, or caller/runner credentials.
	client := &http.Client{Timeout: 5 * time.Second}
	if c.engineReleaseClient != nil {
		*client = *c.engineReleaseClient
	}
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return errors.New("release metadata redirects are not allowed")
	}
	response, err := client.Do(request)
	if err != nil {
		return time.Time{}, err
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			engineReleaseCooldownLog.Printf("Closing release metadata response: %v", err)
		}
	}()
	if response.StatusCode != http.StatusOK {
		return time.Time{}, fmt.Errorf("release metadata returned HTTP %d", response.StatusCode)
	}
	// A GitHub release is small; an npm packument lists every version (and may
	// embed READMEs), so it is streamed and only name and time[version] are kept.
	// The npm cap only bounds a runaway response; truncation fails the parse.
	maxMetadataBytes := int64(8 * 1024 * 1024)
	if packageName != "" {
		maxMetadataBytes = 512 * 1024 * 1024
	}
	return parseEngineReleaseTime(io.LimitReader(response.Body, maxMetadataBytes), packageName, version)
}

func parseEngineReleaseTime(body io.Reader, packageName, version string) (time.Time, error) {
	var timestamp string
	if packageName == "" {
		var release struct {
			TagName     string `json:"tag_name"`
			PublishedAt string `json:"published_at"`
			Draft       bool   `json:"draft"`
		}
		if err := json.NewDecoder(body).Decode(&release); err != nil {
			return time.Time{}, err
		}
		if release.TagName != "v"+version || release.Draft {
			return time.Time{}, errors.New("release metadata does not match the requested published tag")
		}
		timestamp = release.PublishedAt
	} else {
		name, published, err := scanNPMPackumentTime(json.NewDecoder(body), version)
		if err != nil {
			return time.Time{}, err
		}
		if name != packageName {
			return time.Time{}, errors.New("release metadata does not match the requested npm package")
		}
		timestamp = published
	}
	return time.Parse(time.RFC3339Nano, timestamp)
}

// scanNPMPackumentTime walks a packument token by token, returning the package
// name and time[version] without materializing the versions or readme data.
func scanNPMPackumentTime(decoder *json.Decoder, version string) (string, string, error) {
	if err := expectJSONDelim(decoder, '{'); err != nil {
		return "", "", err
	}
	var name, timestamp string
	var haveName, haveTime bool
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return "", "", err
		}
		switch key {
		case "name":
			if err := decoder.Decode(&name); err != nil {
				return "", "", err
			}
			haveName = true
		case "time":
			if err := expectJSONDelim(decoder, '{'); err != nil {
				return "", "", err
			}
			for decoder.More() {
				entry, err := decoder.Token()
				if err != nil {
					return "", "", err
				}
				if entry == version {
					if err := decoder.Decode(&timestamp); err != nil {
						return "", "", err
					}
				} else if err := skipJSONValue(decoder); err != nil {
					return "", "", err
				}
			}
			if err := expectJSONDelim(decoder, '}'); err != nil {
				return "", "", err
			}
			haveTime = true
		default:
			if err := skipJSONValue(decoder); err != nil {
				return "", "", err
			}
		}
		if haveName && haveTime {
			return name, timestamp, nil
		}
	}
	if err := expectJSONDelim(decoder, '}'); err != nil {
		return "", "", err
	}
	return name, timestamp, nil
}

func expectJSONDelim(decoder *json.Decoder, delim json.Delim) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token != delim {
		return fmt.Errorf("release metadata: expected %q", delim)
	}
	return nil
}

// skipJSONValue consumes one JSON value without retaining nested content.
func skipJSONValue(decoder *json.Decoder) error {
	depth := 0
	for {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		if delim, ok := token.(json.Delim); ok {
			if delim == '{' || delim == '[' {
				depth++
			} else {
				depth--
			}
		}
		if depth == 0 {
			return nil
		}
	}
}

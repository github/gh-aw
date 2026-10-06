package actionpins

import (
	"cmp"
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/semverutil"
)

var actionPinsLog = logger.New("actionpins:actionpins")

//go:embed data/action_pins.json
var actionPinsJSON []byte

// actionPinsCache bundles the parsed/derived action pin data behind a single
// pointer so the package-level variable holding it is never a bare slice or
// map that gets reassigned in place; the pointer itself is written exactly
// once (guarded by actionPinsOnce) and is treated as read-only thereafter.
type actionPinsCache struct {
	pins       []ActionPin
	byRepo     map[string][]ActionPin
	containers map[string]ContainerPin
}

var (
	cachedPins     *actionPinsCache
	actionPinsOnce sync.Once
)

// getCachedActionPins returns the initialized action pin cache.
// Panics if cache initialization did not complete, which can only follow
// invalid embedded action pin data.
func getCachedActionPins() *actionPinsCache {
	actionPinsOnce.Do(func() {
		actionPinsLog.Print("Unmarshaling action pins from embedded JSON (first call, will be cached)")

		data := loadActionPinsData(actionPinsJSON)

		pins := slices.Collect(maps.Values(data.Entries))

		slices.SortFunc(pins, func(a, b ActionPin) int {
			if a.Version != b.Version {
				return cmp.Compare(b.Version, a.Version) // descending by version
			}
			return cmp.Compare(a.Repo, b.Repo)
		})

		actionPinsLog.Printf("Successfully unmarshaled and sorted %d action pins from JSON", len(pins))

		byRepo := buildByRepoIndex(pins)
		actionPinsLog.Printf("Built per-repo action pin index for %d repos", len(byRepo))

		containers := data.Containers
		if containers == nil {
			containers = make(map[string]ContainerPin)
		}
		actionPinsLog.Printf("Loaded %d container pins from JSON", len(containers))

		cachedPins = &actionPinsCache{
			pins:       pins,
			byRepo:     byRepo,
			containers: containers,
		}
	})

	if cachedPins == nil {
		// Build-time invariant: actionPinsOnce.Do above always assigns cachedPins
		// unless loadActionPinsData panicked, so this is unreachable in practice.
		panic("action pins cache was not initialized")
	}
	return cachedPins
}

// loadActionPinsData unmarshals embedded action pin data.
// Panics if the embedded JSON is invalid or any entry has an empty SHA, because
// those conditions indicate corrupted release data that would produce invalid workflow YAML.
func loadActionPinsData(raw []byte) ActionPinsData {
	var data ActionPinsData
	if err := json.Unmarshal(raw, &data); err != nil {
		actionPinsLog.Printf("Failed to unmarshal action pins JSON: %v", err)
		// Build-time invariant: data/action_pins.json is embedded at compile time and
		// validated by this package's tests; unmarshal can only fail for corrupted
		// release data, never dynamic user input.
		panic(fmt.Sprintf("failed to load action pins: %v", err))
	}

	if n := countPinKeyMismatches(data.Entries); n > 0 {
		actionPinsLog.Printf("Found %d key/version mismatches in action_pins.json", n)
	}

	if emptyKeys := collectEntriesWithEmptySHA(data.Entries); len(emptyKeys) > 0 {
		// Build-time invariant: an empty SHA in the embedded pin data would produce
		// invalid workflow YAML at release time and must be caught before shipping.
		panic(fmt.Sprintf("action_pins.json has %d entries with empty SHA %v — these would produce invalid workflow YAML (e.g. 'owner/repo@ # version'); remove or fix these entries before releasing", len(emptyKeys), emptyKeys))
	}

	return data
}

// countPinKeyMismatches returns the number of entries where the key version does not
// match pin.Version, logging each mismatch for diagnostics.
func countPinKeyMismatches(entries map[string]ActionPin) int {
	count := 0
	for key, pin := range entries {
		if idx := strings.LastIndex(key, "@"); idx != -1 {
			keyVersion := key[idx+1:]
			if keyVersion != pin.Version {
				count++
				shortSHA := pin.SHA[:min(len(pin.SHA), 8)]
				actionPinsLog.Printf("WARNING: Key/version mismatch in action_pins.json: key=%s has version=%s but pin.Version=%s (sha=%s)",
					key, keyVersion, pin.Version, shortSHA)
			}
		}
	}
	return count
}

// collectEntriesWithEmptySHA returns the keys of entries whose SHA field is empty,
// logging each offending entry for diagnostics.
func collectEntriesWithEmptySHA(entries map[string]ActionPin) []string {
	var keys []string
	for key, pin := range entries {
		if pin.SHA == "" {
			keys = append(keys, key)
			actionPinsLog.Printf("ERROR: Empty SHA in action_pins.json: key=%s repo=%s version=%s", key, pin.Repo, pin.Version)
		}
	}
	slices.Sort(keys)
	return keys
}

// buildByRepoIndex groups pins by repository and sorts each group by version descending.
func buildByRepoIndex(pins []ActionPin) map[string][]ActionPin {
	byRepo := make(map[string][]ActionPin, len(pins))
	for _, pin := range pins {
		byRepo[pin.Repo] = append(byRepo[pin.Repo], pin)
	}
	for _, repoPins := range byRepo {
		slices.SortFunc(repoPins, func(a, b ActionPin) int {
			v1 := strings.TrimPrefix(a.Version, "v")
			v2 := strings.TrimPrefix(b.Version, "v")
			return semverutil.Compare(v2, v1) // descending by semver
		})
	}
	return byRepo
}

// GetActionPinsByRepo returns a copy of the sorted (version-descending) list of
// action pins for the given repository. Returns nil if the repo has no pins.
func GetActionPinsByRepo(repo string) []ActionPin {
	pins := getCachedActionPins().byRepo[repo]
	actionPinsLog.Printf("Looked up action pins for repo=%s: %d found", repo, len(pins))
	return slices.Clone(pins)
}

// GetLatestActionPinByRepo returns the latest ActionPin for a given repository, if any.
func GetLatestActionPinByRepo(repo string) (ActionPin, bool) {
	pins := GetActionPinsByRepo(repo)
	if len(pins) == 0 {
		actionPinsLog.Printf("No action pins found for repo=%s", repo)
		return ActionPin{}, false
	}
	for _, pin := range pins {
		return pin, true
	}
	return ActionPin{}, false
}

// GetContainerPin returns a pinned container image by its original image reference.
func GetContainerPin(image string) (ContainerPin, bool) {
	pin, ok := getCachedActionPins().containers[image]
	actionPinsLog.Printf("Looked up container pin for image=%s: found=%t", image, ok)
	return pin, ok
}

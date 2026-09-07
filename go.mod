module github.com/github/gh-aw

go 1.26.7

require (
	charm.land/bubbles/v2 v2.2.1
	charm.land/bubbletea/v2 v2.0.9
	charm.land/huh/v2 v2.0.3
	charm.land/lipgloss/v2 v2.0.6
	github.com/charmbracelet/colorprofile v0.4.3
	github.com/charmbracelet/x/exp/golden v0.0.0-20260816001655-68d539dca504
	github.com/charmbracelet/x/term v0.2.2
	github.com/cli/go-gh/v2 v2.13.0
	github.com/creack/pty v1.1.24
	github.com/fsnotify/fsnotify v1.10.1
	github.com/goccy/go-yaml v1.19.2
	github.com/google/jsonschema-go v0.4.3
	github.com/modelcontextprotocol/go-sdk v1.7.0
	github.com/rivo/uniseg v0.4.7
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3
	github.com/sourcegraph/conc v0.3.0
	github.com/spf13/cobra v1.10.2
	github.com/spf13/pflag v1.0.10
	github.com/stretchr/testify v1.12.1
	go.uber.org/goleak v1.3.0
	// Only golang.org/x/crypto/nacl/box is used. The unmaintained
	// golang.org/x/crypto/openpgp subpackage (advisory GO-2026-5932, no fix
	// available) must never be imported; enforced by the depguard rule in
	// .golangci.yml.
	golang.org/x/crypto v0.55.0
	golang.org/x/mod v0.40.0
	golang.org/x/sync v0.22.0
	golang.org/x/term v0.45.0
	golang.org/x/tools v0.49.0
	gonum.org/v1/gonum v0.17.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	cloud.google.com/go v0.123.0 // indirect
	cloud.google.com/go/auth v0.23.2 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	github.com/anthropics/anthropic-sdk-go v1.66.0 // indirect
	github.com/atotto/clipboard v0.1.4 // indirect
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/aymanbagabas/go-udiff v0.4.1 // indirect
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/bmatcuk/doublestar/v4 v4.10.0 // indirect
	github.com/boumenot/gocover-cobertura v1.5.0 // indirect
	github.com/buger/jsonparser v1.6.1 // indirect
	github.com/catppuccin/go v0.3.0 // indirect
	github.com/ccojocar/zxcvbn-go v1.0.4 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/charmbracelet/harmonica v0.2.0 // indirect
	github.com/charmbracelet/ultraviolet v0.0.0-20260811164956-006e29f97886 // indirect
	github.com/charmbracelet/x/ansi v0.11.8 // indirect
	github.com/charmbracelet/x/exp/ordered v0.1.0 // indirect
	github.com/charmbracelet/x/exp/strings v0.0.0-20251106172358-54469c29c2bc // indirect
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/charmbracelet/x/windows v0.2.2 // indirect
	github.com/cli/safeexec v1.0.1 // indirect
	github.com/cli/shurcooL-graphql v0.0.4 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/dlclark/regexp2 v1.11.5 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/fatih/color v1.19.0 // indirect
	github.com/felixge/httpsnoop v1.1.0 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.20 // indirect
	github.com/googleapis/gax-go/v2 v2.24.0 // indirect
	github.com/gookit/color v1.6.1 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/henvic/httpretty v0.1.4 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/invopop/jsonschema v0.14.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-runewidth v0.0.27 // indirect
	github.com/mattn/go-shellwords v1.0.12 // indirect
	github.com/mitchellh/hashstructure/v2 v2.0.2 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/muesli/termenv v0.16.0 // indirect
	github.com/openai/openai-go/v3 v3.52.0 // indirect
	github.com/pb33f/ordered-map/v2 v2.3.1 // indirect
	github.com/rhysd/actionlint v1.7.12 // indirect
	github.com/robfig/cron/v3 v3.0.1 // indirect
	github.com/securego/gosec/v2 v2.29.0 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/standard-webhooks/standard-webhooks/libraries v0.0.1 // indirect
	github.com/thlib/go-timezone-local v0.0.7 // indirect
	github.com/tidwall/gjson v1.19.0 // indirect
	github.com/tidwall/match v1.2.0 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	github.com/xo/terminfo v1.0.0 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.70.0 // indirect
	go.opentelemetry.io/otel v1.45.0 // indirect
	go.opentelemetry.io/otel/metric v1.45.0 // indirect
	go.opentelemetry.io/otel/trace v1.45.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	go.yaml.in/yaml/v4 v4.0.0-rc.6 // indirect
	golang.org/x/exp v0.0.0-20240909161429-701f63a606c0 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/telemetry v0.0.0-20260811182544-a038080d80e5 // indirect
	golang.org/x/text v0.41.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	golang.org/x/vuln v1.7.0 // indirect
	google.golang.org/api v0.293.0 // indirect
	google.golang.org/genai v1.69.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260819154853-08b0e4226688 // indirect
	google.golang.org/grpc v1.83.1 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)

tool (
	github.com/boumenot/gocover-cobertura
	github.com/rhysd/actionlint/cmd/actionlint
	github.com/securego/gosec/v2/cmd/gosec
	golang.org/x/vuln/cmd/govulncheck
)

// actionlint@v1.7.12 requires go.yaml.in/yaml/v4@v4.0.0-rc.3, which exposes
// yaml.ParserError and related fields that were removed in rc.6. gosec@v2.29.0
// pulls in rc.6 transitively (but does not import yaml/v4 directly), causing
// actionlint to fail to compile. Pin the replacement to rc.3 so all
// consumers in this module use the version actionlint depends on.
replace go.yaml.in/yaml/v4 => go.yaml.in/yaml/v4 v4.0.0-rc.3

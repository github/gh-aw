//go:build !unix

package cli

import "errors"

var defaultMCPLogsCache = mcpLogsReportCache{}

type mcpLogsReportCache struct{}

func (mcpLogsReportCache) store(string) (string, error) {
	return "", errors.New("owner-private logs report storage is not supported on this platform")
}

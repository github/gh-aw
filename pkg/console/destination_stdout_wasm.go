//go:build js || wasm

package console

func FormatErrorStdout(err CompilerError) string        { return FormatError(err) }
func FormatSuccessMessageStdout(message string) string  { return FormatSuccessMessage(message) }
func FormatInfoMessageStdout(message string) string     { return FormatInfoMessage(message) }
func FormatWarningMessageStdout(message string) string  { return FormatWarningMessage(message) }
func FormatCommandMessageStdout(command string) string  { return FormatCommandMessage(command) }
func FormatProgressMessageStdout(message string) string { return FormatProgressMessage(message) }
func FormatPromptMessageStdout(message string) string   { return FormatPromptMessage(message) }
func FormatVerboseMessageStdout(message string) string  { return FormatVerboseMessage(message) }
func FormatListItemStdout(item string) string           { return FormatListItem(item) }
func FormatSectionHeaderStdout(header string) string    { return FormatSectionHeader(header) }
func RenderTableStdout(config TableConfig) string       { return RenderTable(config) }

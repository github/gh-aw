package console

// Explicit stderr entry points remain available for existing callers.
func FormatErrorStderr(err CompilerError) string        { return FormatError(err) }
func FormatSuccessMessageStderr(message string) string  { return FormatSuccessMessage(message) }
func FormatInfoMessageStderr(message string) string     { return FormatInfoMessage(message) }
func FormatWarningMessageStderr(message string) string  { return FormatWarningMessage(message) }
func FormatCommandMessageStderr(command string) string  { return FormatCommandMessage(command) }
func FormatProgressMessageStderr(message string) string { return FormatProgressMessage(message) }
func FormatListItemStderr(item string) string           { return FormatListItem(item) }
func FormatSectionHeaderStderr(header string) string    { return FormatSectionHeader(header) }
func RenderTableStderr(config TableConfig) string       { return RenderTable(config) }

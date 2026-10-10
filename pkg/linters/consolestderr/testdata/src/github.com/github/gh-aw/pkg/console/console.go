package console

func FormatInfoMessage(s string) string       { return s }
func FormatInfoMessageStderr(s string) string { return s }
func FormatErrorMessage(s string) string      { return s }
func FormatFileSize(n int64) string           { return "" }
func RenderStruct(v any) string               { return "" }
func RenderStructStderr(v any) string         { return "" }

type RenderOptions struct {
	Stderr   bool
	MaxWidth int
}

func RenderStructWithOptions(v any, options RenderOptions) string { return "" }

package console

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	lipgloss "charm.land/lipgloss/v2"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
)

var renderLog = logger.New("console:render")

// RenderStruct renders a Go struct to console output using reflection and struct tags.
// It supports:
// - Rendering structs as markdown-style headers with key-value pairs
// - Rendering slices as tables using the console table renderer
// - Rendering maps as markdown headers
//
// Struct tags:
// - `console:"title:My Title"` - Sets the title for a section
// - `console:"header:Column Name"` - Sets the column header name for table columns
// - `console:"omitempty"` - Skips zero values
// - `console:"-"` - Skips the field entirely
func RenderStruct(v any) string {
	return renderStructWithRenderer(v, RenderTable)
}

// RenderStructStderr renders reflected data for stderr, including nested tables.
func RenderStructStderr(v any) string {
	return renderStructWithRenderer(v, RenderTableStderr)
}

// RenderStructWithOptions propagates destination and width to nested tables.
func RenderStructWithOptions(v any, options RenderOptions) string {
	renderTable := func(config TableConfig) string {
		config.MaxWidth = options.MaxWidth
		if options.Stderr {
			return RenderTableStderr(config)
		}
		return RenderTable(config)
	}
	return wrapConsoleText(renderStructWithRenderer(v, renderTable), options.MaxWidth)
}

func renderStructWithRenderer(v any, renderTable func(TableConfig) string) string {
	renderLog.Printf("Rendering struct: type=%T", v)
	var output strings.Builder
	renderValue(reflect.ValueOf(v), "", &output, 0, renderTable)
	renderLog.Printf("Struct rendering complete: output_size=%d bytes", output.Len())
	return output.String()
}

// renderValue recursively renders a reflect.Value to the output builder
func renderValue(val reflect.Value, title string, output *strings.Builder, depth int, renderTable func(TableConfig) string) {
	// Dereference pointers
	for val.Kind() == reflect.Pointer {
		if val.IsNil() {
			return
		}
		val = val.Elem()
	}

	switch val.Kind() {
	case reflect.Struct:
		renderStruct(val, title, output, depth, renderTable)
	case reflect.Slice, reflect.Array:
		renderSliceWithRenderer(val, title, output, depth, renderTable)
	case reflect.Map:
		renderMap(val, title, output, depth)
	}
}

// renderStruct renders a struct as markdown-style headers with key-value pairs
func renderStruct(val reflect.Value, title string, output *strings.Builder, depth int, renderTable func(TableConfig) string) {
	typ := val.Type()
	renderLog.Printf("Rendering struct: type=%s, title=%s, depth=%d, fields=%d", typ.Name(), title, depth, val.NumField())

	// Print title without FormatInfoMessage styling
	if title != "" {
		if depth == 0 {
			fmt.Fprintf(output, "# %s\n\n", title)
		} else {
			fmt.Fprintf(output, "%s %s\n\n", strings.Repeat("#", depth+1), title)
		}
	}

	maxFieldLen := computeMaxFieldLen(val)
	renderInlineEmbeddedFields(val, maxFieldLen, output, depth, renderTable)

	output.WriteString("\n")
}

// renderInlineEmbeddedFields renders the fields of an anonymous embedded struct
// directly into the parent struct output, flattening the hierarchy.
func renderInlineEmbeddedFields(val reflect.Value, maxFieldLen int, output *strings.Builder, depth int, renderTable func(TableConfig) string) {
	walkInlineFields(val, func(field reflect.Value, fieldType reflect.StructField) {
		tag := parseConsoleTag(fieldType.Tag.Get("console"))
		if tag.skip {
			return
		}
		if tag.omitempty && isZeroValue(field) {
			return
		}

		fieldName := fieldType.Name
		if tag.header != "" {
			fieldName = tag.header
		}

		renderStructField(field, fieldName, tag, maxFieldLen, output, depth, renderTable)
	})
}

// computeMaxFieldLen computes the longest visible field name for alignment,
// recursing into anonymous embedded structs to include their fields.
func computeMaxFieldLen(val reflect.Value) int {
	maxFieldLen := 0
	walkInlineFields(val, func(field reflect.Value, fieldType reflect.StructField) {
		tag := parseConsoleTag(fieldType.Tag.Get("console"))

		if tag.skip || (tag.omitempty && isZeroValue(field)) {
			return
		}

		fieldName := fieldType.Name
		if tag.header != "" {
			fieldName = tag.header
		}

		if lipgloss.Width(fieldName) > maxFieldLen {
			maxFieldLen = lipgloss.Width(fieldName)
		}
	})
	return maxFieldLen
}

func walkInlineFields(val reflect.Value, visit func(field reflect.Value, fieldType reflect.StructField)) {
	typ := val.Type()
	for i := range val.NumField() {
		field := val.Field(i)
		fieldType := typ.Field(i)

		if fieldType.Anonymous {
			if parseConsoleTag(fieldType.Tag.Get("console")).skip {
				continue
			}
			if embedded, ok := embeddedStructValue(field); ok {
				walkInlineFields(embedded, visit)
				continue
			}
		}

		visit(field, fieldType)
	}
}

func embeddedStructValue(field reflect.Value) (reflect.Value, bool) {
	for field.Kind() == reflect.Pointer {
		if field.IsNil() {
			return reflect.Value{}, false
		}
		field = field.Elem()
	}

	return field, field.Kind() == reflect.Struct
}

// renderStructField renders a single struct field to output, dispatching on its kind.
func renderStructField(field reflect.Value, fieldName string, tag consoleTag, maxFieldLen int, output *strings.Builder, depth int, renderTable func(TableConfig) string) {
	// Dereference pointer to check underlying type
	fieldToCheck := field
	if field.Kind() == reflect.Pointer && !field.IsNil() {
		fieldToCheck = field.Elem()
	}

	subTitle := tag.title
	if subTitle == "" {
		subTitle = fieldName
	}

	switch {
	case fieldToCheck.Kind() == reflect.Struct && fieldToCheck.Type().String() != "time.Time":
		// Nested struct – render recursively
		renderValue(field, subTitle, output, depth+1, renderTable)
	case fieldToCheck.Kind() == reflect.Slice || fieldToCheck.Kind() == reflect.Array:
		// Slice – render as table
		renderValue(field, subTitle, output, depth+1, renderTable)
	case fieldToCheck.Kind() == reflect.Map:
		// Map – render as headers
		renderValue(field, subTitle, output, depth+1, renderTable)
	default:
		// Simple field – render as key-value pair with alignment
		paddedName := lipgloss.NewStyle().Width(maxFieldLen).Render(fieldName)
		fmt.Fprintf(output, "  %s: %v\n", paddedName, formatFieldValueWithTag(field, tag))
	}
}

// renderSliceWithRenderer renders slices using the selected table destination.
func renderSliceWithRenderer(val reflect.Value, title string, output *strings.Builder, depth int, renderTable func(TableConfig) string) {
	if val.Len() == 0 {
		return
	}

	renderLog.Printf("Rendering slice: title=%s, length=%d, element_type=%s", title, val.Len(), val.Type().Elem().Name())

	// Print title without FormatInfoMessage styling
	if title != "" {
		if depth == 0 {
			fmt.Fprintf(output, "# %s\n\n", title)
		} else {
			fmt.Fprintf(output, "%s %s\n\n", strings.Repeat("#", depth+1), title)
		}
	}

	// Check if slice elements are structs (for table rendering)
	elemType := val.Type().Elem()
	for elemType.Kind() == reflect.Pointer {
		elemType = elemType.Elem()
	}

	if elemType.Kind() == reflect.Struct {
		// Render as table
		config := buildTableConfig(val, title)
		output.WriteString(renderTable(config))
	} else {
		// Render as list
		for i := range val.Len() {
			elem := val.Index(i)
			fmt.Fprintf(output, "  • %v\n", formatFieldValue(elem))
		}
		output.WriteString("\n")
	}
}

// renderMap renders a map as markdown-style headers
func renderMap(val reflect.Value, title string, output *strings.Builder, depth int) {
	if val.Len() == 0 {
		return
	}

	// Print title without FormatInfoMessage styling
	if title != "" {
		if depth == 0 {
			fmt.Fprintf(output, "# %s\n\n", title)
		} else {
			fmt.Fprintf(output, "%s %s\n\n", strings.Repeat("#", depth+1), title)
		}
	}

	// Render map entries
	for _, key := range val.MapKeys() {
		mapValue := val.MapIndex(key)
		fmt.Fprintf(output, "  %-18s %v\n", fmt.Sprintf("%v:", key), formatFieldValue(mapValue))
	}
	output.WriteString("\n")
}

// buildTableConfig builds a TableConfig from a slice of structs
func buildTableConfig(val reflect.Value, title string) TableConfig {
	renderLog.Printf("Building table config: title=%s, elements=%d", title, val.Len())

	config := TableConfig{
		Title: "",
	}

	if val.Len() == 0 {
		return config
	}

	// Get the element type
	elemType := val.Type().Elem()
	for elemType.Kind() == reflect.Pointer {
		elemType = elemType.Elem()
	}

	// Build headers from struct fields
	headers, fieldPaths, fieldTags := buildTableHeaders(elemType)
	config.Headers = headers

	// Build rows
	config.Rows = buildTableRows(val, fieldPaths, fieldTags)

	return config
}

// buildTableHeaders extracts column headers, field index paths, and tags from a struct type,
// flattening anonymous embedded struct fields into the top-level column list.
func buildTableHeaders(elemType reflect.Type) (headers []string, fieldPaths [][]int, fieldTags []consoleTag) {
	fields := collectTableFields(elemType, nil)
	headers = make([]string, 0, len(fields))
	fieldPaths = make([][]int, 0, len(fields))
	fieldTags = make([]consoleTag, 0, len(fields))
	for _, field := range fields {
		headers = append(headers, field.header)
		fieldPaths = append(fieldPaths, field.path)
		fieldTags = append(fieldTags, field.tag)
	}
	return headers, fieldPaths, fieldTags
}

// collectTableFields recursively walks a struct type, inlining the fields of any
// anonymous embedded structs so they appear as top-level table columns.
func collectTableFields(t reflect.Type, prefix []int) []tableField {
	fields := make([]tableField, 0, t.NumField())
	for i := range t.NumField() {
		field := t.Field(i)
		fieldPath := make([]int, len(prefix)+1)
		copy(fieldPath, prefix)
		fieldPath[len(prefix)] = i

		if field.Anonymous {
			if parseConsoleTag(field.Tag.Get("console")).skip {
				continue
			}
			if embeddedType, ok := embeddedStructType(field.Type); ok {
				fields = append(fields, collectTableFields(embeddedType, fieldPath)...)
				continue
			}
		}

		tag := parseConsoleTag(field.Tag.Get("console"))

		if tag.skip {
			continue
		}

		headerName := field.Name
		if tag.header != "" {
			headerName = tag.header
		}

		fields = append(fields, tableField{
			header: headerName,
			path:   fieldPath,
			tag:    tag,
		})
	}
	return fields
}

// buildTableRows builds the row data for a slice of struct elements.
func buildTableRows(val reflect.Value, fieldPaths [][]int, fieldTags []consoleTag) [][]string {
	var rows [][]string
	for i := range val.Len() {
		elem := val.Index(i)
		// Dereference pointer if needed
		for elem.Kind() == reflect.Pointer {
			if elem.IsNil() {
				break
			}
			elem = elem.Elem()
		}

		if elem.Kind() != reflect.Struct {
			continue
		}

		var row []string
		for j, fieldPath := range fieldPaths {
			field, ok := fieldByIndexSafe(elem, fieldPath)
			if !ok {
				row = append(row, "")
				continue
			}
			row = append(row, formatFieldValueWithTag(field, fieldTags[j]))
		}
		rows = append(rows, row)
	}
	return rows
}

func fieldByIndexSafe(val reflect.Value, path []int) (reflect.Value, bool) {
	current := val
	for _, idx := range path {
		for current.Kind() == reflect.Pointer {
			if current.IsNil() {
				return reflect.Value{}, false
			}
			current = current.Elem()
		}
		if current.Kind() != reflect.Struct {
			return reflect.Value{}, false
		}
		current = current.Field(idx)
	}
	return current, true
}

func embeddedStructType(t reflect.Type) (reflect.Type, bool) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t, t.Kind() == reflect.Struct
}

type tableField struct {
	header string
	path   []int
	tag    consoleTag
}

// consoleTag represents parsed console struct tag
type consoleTag struct {
	title      string
	header     string
	format     string
	defaultVal string // Default value for zero/empty values
	maxLen     int    // Maximum length for string truncation
	omitempty  bool
	skip       bool
}

// parseConsoleTag parses the console struct tag
func parseConsoleTag(tag string) consoleTag {
	result := consoleTag{}

	if tag == "-" {
		result.skip = true
		return result
	}

	parts := strings.SplitSeq(tag, ",")
	for part := range parts {
		part = strings.TrimSpace(part)
		if part == "omitempty" {
			result.omitempty = true
		} else if after, ok := strings.CutPrefix(part, "title:"); ok {
			result.title = after
		} else if after, ok := strings.CutPrefix(part, "header:"); ok {
			result.header = after
		} else if after, ok := strings.CutPrefix(part, "format:"); ok {
			result.format = after
		} else if after, ok := strings.CutPrefix(part, "default:"); ok {
			result.defaultVal = after
		} else if after, ok := strings.CutPrefix(part, "maxlen:"); ok {
			maxLenStr := after
			if len, err := strconv.Atoi(maxLenStr); err == nil {
				result.maxLen = len
			}
		}
	}

	return result
}

// isZeroValue checks if a reflect.Value is the zero value for its type
func isZeroValue(val reflect.Value) bool {
	if !val.IsValid() {
		return true
	}

	// Special handling for time.Time
	if val.Type().String() == "time.Time" {
		if val.CanInterface() {
			if t, ok := val.Interface().(time.Time); ok {
				return t.IsZero()
			}
		}
		// For unexported time.Time fields, we can't easily check, so assume not zero
		return false
	}

	switch val.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return val.Len() == 0
	case reflect.Bool:
		return !val.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return val.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return val.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return val.Float() == 0
	case reflect.Interface, reflect.Pointer:
		return val.IsNil()
	}

	return false
}

// formatFieldValue formats a reflect.Value as a string for display
func formatFieldValue(val reflect.Value) string {
	// Dereference pointers
	for val.Kind() == reflect.Pointer {
		if val.IsNil() {
			return "-"
		}
		val = val.Elem()
	}

	if !val.IsValid() {
		return "-"
	}

	// Handle zero values
	if isZeroValue(val) {
		if val.Kind() == reflect.String {
			return "-"
		}
		// For numeric types, return the actual value
		if val.Kind() >= reflect.Int && val.Kind() <= reflect.Float64 {
			if val.CanInterface() {
				return fmt.Sprintf("%v", val.Interface())
			}
			return formatNumericKind(val)
		}
		return "-"
	}

	// Special handling for time.Time to avoid unexported field panic
	if val.Type().String() == "time.Time" {
		return formatTimeValue(val)
	}

	// Only call Interface() if we can
	if !val.CanInterface() {
		return formatUnexportedValue(val)
	}

	return fmt.Sprintf("%v", val.Interface())
}

// formatTimeValue formats a time.Time reflect value as a display string.
func formatTimeValue(val reflect.Value) string {
	if val.CanInterface() {
		if timeVal, ok := val.Interface().(time.Time); ok {
			return formatConfiguredTimeValue(timeVal)
		}
	}
	// For unexported time.Time fields, try to call the String method
	stringMethod := val.MethodByName("String")
	if stringMethod.IsValid() {
		result := stringMethod.Call(nil)
		if len(result) > 0 {
			return result[0].String()
		}
	}
	return val.Type().String()
}

// formatUnexportedValue formats unexported struct fields by kind without using Interface().
func formatUnexportedValue(val reflect.Value) string {
	switch val.Kind() {
	case reflect.Bool:
		return strconv.FormatBool(val.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(val.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(val.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		return fmt.Sprintf("%f", val.Float())
	case reflect.String:
		return val.String()
	default:
		return val.Type().String()
	}
}

// formatNumericKind formats numeric kinds without Interface() as a fallback.
func formatNumericKind(val reflect.Value) string {
	switch val.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(val.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(val.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		return fmt.Sprintf("%f", val.Float())
	default:
		return val.Type().String()
	}
}

// formatFieldValueWithTag formats a reflect.Value as a string for display with format tag support
func formatFieldValueWithTag(val reflect.Value, tag consoleTag) string {
	// Get the base formatted value
	baseValue := formatFieldValue(val)

	// Check if value is zero/empty and apply default if specified
	if tag.defaultVal != "" && isZeroValue(val) {
		baseValue = tag.defaultVal
	}

	// Apply format based on tag
	if tag.format != "" && baseValue != "-" {
		baseValue = applyTagFormat(val, tag.format, baseValue)
	}

	// Apply maxlen truncation if specified
	if tag.maxLen > 0 {
		baseValue = stringutil.Truncate(baseValue, tag.maxLen)
	}

	return baseValue
}

// applyTagFormat applies a named format (number, cost, filesize) to baseValue.
func applyTagFormat(val reflect.Value, format, baseValue string) string {
	switch format {
	case "number":
		return applyNumberFormat(val, baseValue)
	case "cost":
		return applyCostFormat(val, baseValue)
	case "filesize":
		return applyFilesizeFormat(val, baseValue)
	}
	return baseValue
}

// applyIntegerFormat formats integer values via a shared dispatcher used by
// number/filesize tag formatters.
func applyIntegerFormat(val reflect.Value, baseValue string, format func(int64) string) string {
	if val.CanInterface() {
		switch v := val.Interface().(type) {
		case int:
			return format(int64(v))
		case int64:
			return format(v)
		case int32:
			return format(int64(v))
		case uint:
			// #nosec G115 -- Converting uint to int64 for display formatting
			return format(int64(v))
		case uint64:
			// #nosec G115 -- Converting uint64 to int64 for display formatting
			return format(int64(v))
		case uint32:
			return format(int64(v))
		}
	}

	// Fallback: use integer kind directly, keeping signed and unsigned separate
	// to avoid calling Int() on an unsigned kind (which panics).
	switch {
	case val.Kind() >= reflect.Int && val.Kind() <= reflect.Int64:
		return format(val.Int())
	case val.Kind() >= reflect.Uint && val.Kind() <= reflect.Uint64:
		// #nosec G115 -- Converting uint to int64 for display formatting
		return format(int64(val.Uint()))
	}
	return baseValue
}

// applyNumberFormat formats a value as a human-readable number (e.g., "1k", "1.2M").
func applyNumberFormat(val reflect.Value, baseValue string) string {
	return applyIntegerFormat(val, baseValue, func(v int64) string {
		// #nosec G115 -- Converting int64 to int for display formatting
		return FormatNumber(int(v))
	})
}

// applyCostFormat formats a value as currency with $ prefix.
func applyCostFormat(val reflect.Value, baseValue string) string {
	if val.CanInterface() {
		switch v := val.Interface().(type) {
		case float64:
			if v > 0 {
				return fmt.Sprintf("$%.3f", v)
			}
		case float32:
			if v > 0 {
				return fmt.Sprintf("$%.3f", v)
			}
		}
	}
	if val.Kind() == reflect.Float64 || val.Kind() == reflect.Float32 {
		if val.Float() > 0 {
			return fmt.Sprintf("$%.3f", val.Float())
		}
	}
	return baseValue
}

// applyFilesizeFormat formats a value as a human-readable file size (e.g., "1.2 MB").
func applyFilesizeFormat(val reflect.Value, baseValue string) string {
	return applyIntegerFormat(val, baseValue, FormatFileSize)
}

// FormatNumber formats large numbers in a human-readable way (e.g., "1k", "1.2k", "1.12M")
func FormatNumber(n int) string {
	if n == 0 {
		return "0"
	}

	f := float64(n)

	if f < 1000 {
		return strconv.Itoa(n)
	} else if f < 1000000 {
		// Format as thousands (k)
		k := f / 1000
		if k >= 100 {
			return fmt.Sprintf("%.0fk", k)
		} else if k >= 10 {
			return fmt.Sprintf("%.1fk", k)
		} else {
			return fmt.Sprintf("%.2fk", k)
		}
	} else if f < 1000000000 {
		// Format as millions (M)
		m := f / 1000000
		if m >= 100 {
			return fmt.Sprintf("%.0fM", m)
		} else if m >= 10 {
			return fmt.Sprintf("%.1fM", m)
		} else {
			return fmt.Sprintf("%.2fM", m)
		}
	} else {
		// Format as billions (B)
		b := f / 1000000000
		if b >= 100 {
			return fmt.Sprintf("%.0fB", b)
		} else if b >= 10 {
			return fmt.Sprintf("%.1fB", b)
		} else {
			return fmt.Sprintf("%.2fB", b)
		}
	}
}

// FormatTokens formats a token count as a compact human-readable string.
// Zero is rendered as "-"; values below 1000 are rendered as plain integers;
// values in the thousands are rendered with one decimal place and a "K" suffix;
// values in the millions are rendered with one decimal place and an "M" suffix.
//
// Examples:
//
//	FormatTokens(0)        // "-"
//	FormatTokens(500)      // "500"
//	FormatTokens(1500)     // "1.5K"
//	FormatTokens(1200000)  // "1.2M"
func FormatTokens(tokens int) string {
	if tokens == 0 {
		return "-"
	}
	if tokens < 1000 {
		return strconv.Itoa(tokens)
	}
	if tokens < 1_000_000 {
		return fmt.Sprintf("%.1fK", float64(tokens)/1000)
	}
	return fmt.Sprintf("%.1fM", float64(tokens)/1_000_000)
}

// ToRelativePath converts an absolute path to a relative path from the current working directory
// If the relative path contains "..", returns the absolute path instead for clarity
func ToRelativePath(path string) string {
	if !filepath.IsAbs(path) {
		return path
	}

	wd, err := os.Getwd()
	if err != nil {
		return path
	}

	relPath, err := filepath.Rel(wd, path)
	if err != nil {
		return path
	}

	if strings.Contains(relPath, "..") {
		return path
	}

	return relPath
}

// FormatErrorWithSuggestions formats an error message with actionable suggestions
func FormatErrorWithSuggestions(message string, suggestions []string) string {
	var output strings.Builder
	output.WriteString(FormatErrorMessage(message))

	if len(suggestions) > 0 {
		output.WriteString("\n\nSuggestions:\n")
		for _, suggestion := range suggestions {
			output.WriteString("  • " + suggestion + "\n")
		}
	}

	return output.String()
}

// findWordEnd finds the end of a word starting at the given position
func findWordEnd(line string, start int) int {
	if start >= len(line) {
		return len(line)
	}

	end := start
	for end < len(line) {
		char := line[end]
		if char == ' ' || char == '\t' || char == ':' || char == '\n' || char == '\r' {
			break
		}
		end++
	}

	return end
}

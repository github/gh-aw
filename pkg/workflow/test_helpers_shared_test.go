package workflow

// flattenSteps returns all lines from a sequence of generated action steps.
func flattenSteps(steps []GitHubActionStep) []string {
	var lines []string
	for _, step := range steps {
		lines = append(lines, step...)
	}
	return lines
}

// boolPtr returns a pointer to a bool value.
// This is a shared helper used by both unit and integration tests.
func boolPtr(b bool) *bool {
	return new(b)
}

// ptrBool returns a pointer to a bool value.
func ptrBool(b bool) *bool {
	return new(b)
}

// strPtr returns a pointer to a string value.
// This is a shared helper used by tests to create *string values for templatable fields.
func strPtr(s string) *string {
	return new(s)
}

// templatableBoolPtr returns a pointer to a TemplatableBool value.
func templatableBoolPtr(s string) *TemplatableBool {
	value := TemplatableBool(s)
	return &value
}

// mockValidationError helps create validation errors for testing.
// This is a shared helper used by both unit and integration tests.
type mockValidationError struct {
	msg string
}

func (m *mockValidationError) Error() string {
	return m.msg
}

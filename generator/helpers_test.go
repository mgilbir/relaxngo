package generator_test // Import by test files in the same directory

import (
	"strings"
)

// parseElementNameToGoType converts an XML element name to a Go type name.
// It removes non-alphanumeric characters and capitalizes each word.
// Shared helper used by both official_suite_test and integration_test.
func parseElementNameToGoType(name string) string {
	var parts []string
	var current strings.Builder

	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			current.WriteRune(r)
		} else if current.Len() > 0 {
			parts = append(parts, current.String())
			current.Reset()
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	if len(parts) == 0 {
		parts = []string{"X"}
	}

	var result strings.Builder
	for i, part := range parts {
		if part == "" {
			continue
		}
		if i == 0 && part[0] >= '0' && part[0] <= '9' {
			result.WriteString("X")
		}
		result.WriteString(capitalize(part))
	}

	identifier := result.String()
	if identifier == "" {
		identifier = "X"
	}

	return identifier
}

// capitalize capitalizes the first letter of a string.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = []rune(strings.ToUpper(string(r[0])))[0]
	return string(r)
}

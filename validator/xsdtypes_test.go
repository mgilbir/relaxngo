package validator

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mgilbir/relaxngo/rng"
)

func TestValidateXSDType(t *testing.T) {
	cases := []struct {
		typ   string
		value string
		want  bool
	}{
		// boolean
		{"boolean", "true", true}, {"boolean", "0", true}, {"boolean", "yes", false},
		// bounded signed integers
		{"byte", "127", true}, {"byte", "128", false}, {"byte", "-128", true}, {"byte", "-129", false},
		{"short", "32767", true}, {"short", "32768", false},
		{"int", "2147483647", true}, {"int", "2147483648", false},
		{"int", "not-a-number", false},
		// unsigned
		{"unsignedByte", "255", true}, {"unsignedByte", "256", false}, {"unsignedByte", "-1", false},
		{"unsignedInt", "4294967295", true}, {"unsignedInt", "4294967296", false},
		// arbitrary-precision integer family
		{"integer", "123456789012345678901234567890", true},
		{"integer", "1.5", false},
		{"nonNegativeInteger", "0", true}, {"nonNegativeInteger", "-1", false},
		{"positiveInteger", "1", true}, {"positiveInteger", "0", false},
		{"negativeInteger", "-1", true}, {"negativeInteger", "0", false}, {"negativeInteger", "1", false},
		{"nonPositiveInteger", "0", true}, {"nonPositiveInteger", "-5", true}, {"nonPositiveInteger", "5", false},
		// decimal vs double
		{"decimal", "3.14", true}, {"decimal", "-0.5", true}, {"decimal", "1e5", false},
		{"decimal", "Inf", false}, {"decimal", "NaN", false},
		{"double", "1e5", true}, {"double", "INF", true}, {"double", "NaN", true},
		// date/time family
		{"date", "2026-07-12", true}, {"date", "2026-07-12Z", true}, {"date", "not-a-date", false},
		{"date", "2026-13-40", true}, // lexical-only: field ranges not checked
		{"dateTime", "2026-07-12T10:30:00", true}, {"dateTime", "2026-07-12", false},
		{"time", "10:30:00", true}, {"time", "10:30", false},
		{"gYear", "2026", true}, {"gYear", "20", false},
		{"gMonth", "--07", true}, {"gMonth", "07", false},
		{"duration", "P1Y2M3DT4H", true}, {"duration", "P", false}, {"duration", "1Y", false},
		// binary
		{"hexBinary", "deadBEEF", true}, {"hexBinary", "abc", false},
		{"base64Binary", "aGVsbG8=", true}, {"base64Binary", "not base64!", false},
		// tokens/names
		{"language", "en-US", true}, {"language", "e n", false},
		{"NCName", "foo.bar-baz", true}, {"NCName", "1abc", false}, {"NCName", "a:b", false},
		{"NMTOKEN", "a:b.c-d", true}, {"NMTOKEN", "a b", false},
		// permissive / unknown
		{"anyURI", "anything at all", true},
		{"someUnknownType", "whatever", true},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%s/%s", c.typ, c.value), func(t *testing.T) {
			if got := validateXSDType(c.typ, c.value); got != c.want {
				t.Errorf("validateXSDType(%q, %q) = %v, want %v", c.typ, c.value, got, c.want)
			}
		})
	}
}

// TestPatternFacet_Anchored verifies XSD pattern facets match the whole value,
// not a substring.
func TestPatternFacet_Anchored(t *testing.T) {
	grammar := &rng.Grammar{
		Start: rng.Start{Ref: &rng.Ref{Name: "code"}},
		Defines: []rng.Define{{
			Name: "code",
			Element: &rng.Element{
				Name: "code",
				Data: &rng.Data{Type: "string", Params: []rng.Param{{Name: "pattern", Value: "[0-9]{3}"}}},
			},
		}},
	}
	v := NewValidator(grammar, DefaultOptions())
	cases := []struct {
		xml     string
		wantErr bool
	}{
		{`<code>123</code>`, false},
		{`<code>abc123def</code>`, true}, // substring match must be rejected
		{`<code>12</code>`, true},
	}
	for _, c := range cases {
		errs, err := v.Validate(strings.NewReader(c.xml))
		if err != nil {
			t.Fatalf("Validate(%q): %v", c.xml, err)
		}
		if (len(errs) > 0) != c.wantErr {
			t.Errorf("Validate(%q) got %d errors, want error=%v", c.xml, len(errs), c.wantErr)
		}
	}
}

// TestDataTypeInElement_IntegerRange exercises the end-to-end path: a bounded
// integer datatype must reject an out-of-range value.
func TestDataTypeInElement_IntegerRange(t *testing.T) {
	grammar := &rng.Grammar{
		Start: rng.Start{Ref: &rng.Ref{Name: "b"}},
		Defines: []rng.Define{{
			Name:    "b",
			Element: &rng.Element{Name: "b", Data: &rng.Data{Type: "byte"}},
		}},
	}
	v := NewValidator(grammar, DefaultOptions())
	if errs, _ := v.Validate(strings.NewReader(`<b>127</b>`)); len(errs) != 0 {
		t.Errorf("byte 127 should be valid, got %v", errs)
	}
	if errs, _ := v.Validate(strings.NewReader(`<b>300</b>`)); len(errs) == 0 {
		t.Error("byte 300 should be invalid (out of range), got no errors")
	}
}

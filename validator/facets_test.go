package validator

import (
	"strings"
	"testing"

	"github.com/mgilbir/relaxngo/rng"
)

// runValidationTests is a helper to run validation tests with common structure
func runValidationTests(t *testing.T, validator *Validator, tests []struct {
	name    string
	xml     string
	wantErr bool
}) {
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors, err := validator.Validate(strings.NewReader(tt.xml))
			if err != nil {
				t.Fatalf("Validate error: %v", err)
			}
			if (len(errors) > 0) != tt.wantErr {
				t.Errorf("Got %d errors, want error=%v", len(errors), tt.wantErr)
			}
		})
	}
}

// makeDataGrammar creates a grammar with a single element containing a data type
func makeDataGrammar(defineName, elementName, dataType string, params []rng.Param) *rng.Grammar {
	return &rng.Grammar{
		Start: rng.Start{
			Ref: &rng.Ref{Name: defineName},
		},
		Defines: []rng.Define{
			{
				Name: defineName,
				Element: &rng.Element{
					Name: elementName,
					Data: &rng.Data{
						Type:   dataType,
						Params: params,
					},
				},
			},
		},
	}
}

// minLengthGrammar creates a grammar with minLength constraint
func minLengthGrammar(minLen string) *rng.Grammar {
	return makeDataGrammar("password", "password", "string", []rng.Param{
		{Name: "minLength", Value: minLen},
	})
}

// TestDataTypeFacets_MinLength tests minLength constraint
func TestDataTypeFacets_MinLength(t *testing.T) {
	grammar := minLengthGrammar("8")
	validator := NewValidator(grammar, DefaultOptions())

	tests := []struct {
		name    string
		xml     string
		wantErr bool
	}{
		{name: "valid_min_length", xml: `<password>longenough</password>`, wantErr: false},
		{name: "valid_exact_min", xml: `<password>12345678</password>`, wantErr: false},
		{name: "invalid_too_short", xml: `<password>short</password>`, wantErr: true},
	}

	runValidationTests(t, validator, tests)
}

// TestDataTypeFacets_MaxLength tests maxLength constraint
func TestDataTypeFacets_MaxLength(t *testing.T) {
	grammar := &rng.Grammar{
		Start: rng.Start{
			Ref: &rng.Ref{Name: "username"},
		},
		Defines: []rng.Define{
			{
				Name: "username",
				Element: &rng.Element{
					Name: "username",
					Data: &rng.Data{
						Type: "string",
						Params: []rng.Param{
							{Name: "maxLength", Value: "20"},
						},
					},
				},
			},
		},
	}

	validator := NewValidator(grammar, DefaultOptions())

	tests := []struct {
		name     string
		xml      string
		wantErr  bool
		testDesc string
	}{
		{
			name:     "valid_short",
			xml:      `<username>john</username>`,
			wantErr:  false,
			testDesc: "4 chars",
		},
		{
			name:     "valid_exact_max",
			xml:      `<username>12345678901234567890</username>`,
			wantErr:  false,
			testDesc: "exactly 20 chars",
		},
		{
			name:     "invalid_too_long",
			xml:      `<username>thisusernameistoolong</username>`,
			wantErr:  true,
			testDesc: "21 chars",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors, err := validator.Validate(strings.NewReader(tt.xml))
			if err != nil {
				t.Fatalf("Validate error: %v", err)
			}

			if (len(errors) > 0) != tt.wantErr {
				t.Errorf("Got %d errors, want error=%v. Test: %s", len(errors), tt.wantErr, tt.testDesc)
			}
		})
	}
}

// TestDataTypeFacets_MinMaxLength tests both min and max length
func TestDataTypeFacets_MinMaxLength(t *testing.T) {
	grammar := makeDataGrammar("code", "code", "string", []rng.Param{
		{Name: "minLength", Value: "3"},
		{Name: "maxLength", Value: "10"},
	})

	validator := NewValidator(grammar, DefaultOptions())

	tests := []struct {
		name    string
		xml     string
		wantErr bool
	}{
		{name: "valid_middle", xml: `<code>hello</code>`, wantErr: false},
		{name: "valid_min", xml: `<code>abc</code>`, wantErr: false},
		{name: "valid_max", xml: `<code>0123456789</code>`, wantErr: false},
		{name: "invalid_too_short", xml: `<code>ab</code>`, wantErr: true},
		{name: "invalid_too_long", xml: `<code>01234567890</code>`, wantErr: true},
	}

	runValidationTests(t, validator, tests)
}

// TestDataTypeFacets_MinMaxInclusive tests numeric min/max constraints
func TestDataTypeFacets_MinMaxInclusive(t *testing.T) {
	grammar := makeDataGrammar("score", "score", "integer", []rng.Param{
		{Name: "minInclusive", Value: "0"},
		{Name: "maxInclusive", Value: "100"},
	})

	validator := NewValidator(grammar, DefaultOptions())

	tests := []struct {
		name    string
		xml     string
		wantErr bool
	}{
		{name: "valid_min", xml: `<score>0</score>`, wantErr: false},
		{name: "valid_middle", xml: `<score>50</score>`, wantErr: false},
		{name: "valid_max", xml: `<score>100</score>`, wantErr: false},
		{name: "invalid_below_min", xml: `<score>-1</score>`, wantErr: true},
		{name: "invalid_above_max", xml: `<score>101</score>`, wantErr: true},
	}

	runValidationTests(t, validator, tests)
}

// TestDataTypeFacets_MinMaxExclusive tests exclusive min/max
func TestDataTypeFacets_MinMaxExclusive(t *testing.T) {
	grammar := &rng.Grammar{
		Start: rng.Start{
			Ref: &rng.Ref{Name: "temp"},
		},
		Defines: []rng.Define{
			{
				Name: "temp",
				Element: &rng.Element{
					Name: "temp",
					Data: &rng.Data{
						Type: "integer",
						Params: []rng.Param{
							{Name: "minExclusive", Value: "0"},
							{Name: "maxExclusive", Value: "100"},
						},
					},
				},
			},
		},
	}

	validator := NewValidator(grammar, DefaultOptions())

	tests := []struct {
		name    string
		xml     string
		wantErr bool
	}{
		{name: "valid_above_min", xml: `<temp>1</temp>`, wantErr: false},
		{name: "valid_below_max", xml: `<temp>99</temp>`, wantErr: false},
		{name: "invalid_at_min", xml: `<temp>0</temp>`, wantErr: true},
		{name: "invalid_at_max", xml: `<temp>100</temp>`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors, err := validator.Validate(strings.NewReader(tt.xml))
			if err != nil {
				t.Fatalf("Validate error: %v", err)
			}
			if (len(errors) > 0) != tt.wantErr {
				t.Errorf("Got %d errors, want error=%v", len(errors), tt.wantErr)
			}
		})
	}
}

// TestDataTypeFacets_Pattern tests regex pattern validation
func TestDataTypeFacets_Pattern(t *testing.T) {
	grammar := &rng.Grammar{
		Start: rng.Start{
			Ref: &rng.Ref{Name: "email"},
		},
		Defines: []rng.Define{
			{
				Name: "email",
				Element: &rng.Element{
					Name: "email",
					Data: &rng.Data{
						Type: "string",
						// Pattern: .+@.+\..+
						Params: []rng.Param{
							{Name: "pattern", Value: `.+@.+\..+`},
						},
					},
				},
			},
		},
	}

	validator := NewValidator(grammar, DefaultOptions())

	tests := []struct {
		name    string
		xml     string
		wantErr bool
	}{
		{name: "valid_email", xml: `<email>user@example.com</email>`, wantErr: false},
		{name: "valid_complex", xml: `<email>john.doe@sub.example.co.uk</email>`, wantErr: false},
		{name: "invalid_no_at", xml: `<email>userexample.com</email>`, wantErr: true},
		{name: "invalid_no_dot", xml: `<email>user@example</email>`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors, err := validator.Validate(strings.NewReader(tt.xml))
			if err != nil {
				t.Fatalf("Validate error: %v", err)
			}
			if (len(errors) > 0) != tt.wantErr {
				t.Errorf("Got %d errors, want error=%v", len(errors), tt.wantErr)
			}
		})
	}
}

// TestDataTypeFacets_FractionDigits tests decimal precision
func TestDataTypeFacets_FractionDigits(t *testing.T) {
	grammar := &rng.Grammar{
		Start: rng.Start{
			Ref: &rng.Ref{Name: "price"},
		},
		Defines: []rng.Define{
			{
				Name: "price",
				Element: &rng.Element{
					Name: "price",
					Data: &rng.Data{
						Type: "decimal",
						Params: []rng.Param{
							{Name: "fractionDigits", Value: "2"},
						},
					},
				},
			},
		},
	}

	validator := NewValidator(grammar, DefaultOptions())

	tests := []struct {
		name    string
		xml     string
		wantErr bool
	}{
		{name: "valid_two_decimals", xml: `<price>19.99</price>`, wantErr: false},
		{name: "valid_one_decimal", xml: `<price>19.9</price>`, wantErr: false},
		{name: "valid_no_decimals", xml: `<price>20</price>`, wantErr: false},
		{name: "valid_trailing_zeros", xml: `<price>19.10</price>`, wantErr: false},
		{name: "invalid_three_decimals", xml: `<price>19.999</price>`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors, err := validator.Validate(strings.NewReader(tt.xml))
			if err != nil {
				t.Fatalf("Validate error: %v", err)
			}
			if (len(errors) > 0) != tt.wantErr {
				t.Errorf("Got %d errors, want error=%v", len(errors), tt.wantErr)
			}
		})
	}
}

// TestDataTypeFacets_AttributeFacets tests facets on attributes
func TestDataTypeFacets_AttributeFacets(t *testing.T) {
	grammar := &rng.Grammar{
		Start: rng.Start{
			Ref: &rng.Ref{Name: "person"},
		},
		Defines: []rng.Define{
			{
				Name: "person",
				Element: &rng.Element{
					Name: "person",
					Attributes: []rng.Attribute{
						{
							Name: "age",
							Data: &rng.Data{
								Type: "integer",
								Params: []rng.Param{
									{Name: "minInclusive", Value: "0"},
									{Name: "maxInclusive", Value: "150"},
								},
							},
						},
					},
				},
			},
		},
	}

	validator := NewValidator(grammar, DefaultOptions())

	tests := []struct {
		name    string
		xml     string
		wantErr bool
	}{
		{name: "valid_age", xml: `<person age="25"/>`, wantErr: false},
		{name: "valid_min", xml: `<person age="0"/>`, wantErr: false},
		{name: "valid_max", xml: `<person age="150"/>`, wantErr: false},
		{name: "invalid_negative", xml: `<person age="-1"/>`, wantErr: true},
		{name: "invalid_too_old", xml: `<person age="200"/>`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors, err := validator.Validate(strings.NewReader(tt.xml))
			if err != nil {
				t.Fatalf("Validate error: %v", err)
			}
			if (len(errors) > 0) != tt.wantErr {
				t.Errorf("Got %d errors, want error=%v", len(errors), tt.wantErr)
			}
		})
	}
}

// TestDataTypeFacets_MultipleFacets tests combination of facets
func TestDataTypeFacets_MultipleFacets(t *testing.T) {
	grammar := &rng.Grammar{
		Start: rng.Start{
			Ref: &rng.Ref{Name: "zipcode"},
		},
		Defines: []rng.Define{
			{
				Name: "zipcode",
				Element: &rng.Element{
					Name: "zipcode",
					Data: &rng.Data{
						Type: "string",
						Params: []rng.Param{
							{Name: "minLength", Value: "5"},
							{Name: "maxLength", Value: "10"},
							{Name: "pattern", Value: `^[0-9\-]+$`},
						},
					},
				},
			},
		},
	}

	validator := NewValidator(grammar, DefaultOptions())

	tests := []struct {
		name    string
		xml     string
		wantErr bool
	}{
		{name: "valid", xml: `<zipcode>12345</zipcode>`, wantErr: false},
		{name: "valid_with_dash", xml: `<zipcode>12345-6789</zipcode>`, wantErr: false},
		{name: "invalid_too_short", xml: `<zipcode>123</zipcode>`, wantErr: true},
		{name: "invalid_too_long", xml: `<zipcode>12345678901</zipcode>`, wantErr: true},
		{name: "invalid_letters", xml: `<zipcode>1234A</zipcode>`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors, err := validator.Validate(strings.NewReader(tt.xml))
			if err != nil {
				t.Fatalf("Validate error: %v", err)
			}
			if (len(errors) > 0) != tt.wantErr {
				t.Errorf("Got %d errors, want error=%v", len(errors), tt.wantErr)
			}
		})
	}
}

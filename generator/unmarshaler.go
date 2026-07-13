package generator

import (
	"bytes"
	"fmt"
	"go/format"
	"strings"
	"text/template"

	"github.com/mgilbir/relaxngo/rng"
)

// TypeWithMethods wraps a TypeInfo with its generated methods
type TypeWithMethods struct {
	TypeInfo        TypeInfo
	UnmarshalMethod string // Generated UnmarshalXML method
	ValidateMethod  string // Generated Validate method (root types only)
}

// Template data structures
type unmarshalMethodData struct {
	TypeName            string
	ValidatorVar        string
	OnceVar             string
	IsRootType          bool
	SchemaContentVar    string
	SchemaGrammarVar    string
	SchemaGrammarErrVar string
	SchemaOnceVar       string
	FormatErrorsFunc    string
}

var unmarshalMethodTemplate = template.Must(template.New("unmarshal").Parse(`
// UnmarshalXML unmarshals XML with validation against the schema.
func (x *{{.TypeName}}) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	// Initialize schema and validator on first use
	{{.SchemaOnceVar}}.Do(func() {
		{{.SchemaGrammarVar}}, {{.SchemaGrammarErrVar}} = rng.ParseSchema(strings.NewReader({{.SchemaContentVar}}))
	})
	if {{.SchemaGrammarErrVar}} != nil {
		return fmt.Errorf("failed to parse embedded schema: %w", {{.SchemaGrammarErrVar}})
	}

	{{.OnceVar}}.Do(func() {
		if {{.SchemaGrammarVar}} != nil {
			{{.ValidatorVar}} = validator.NewValidator({{.SchemaGrammarVar}}, validator.DefaultOptions())
		}
	})

	// Two-pass approach: capture raw XML for validation
	var rawBuf bytes.Buffer
	rawBuf.WriteString("<?xml version=\"1.0\"?>\n")

	// Reconstruct the XML for this element
	enc := xml.NewEncoder(&rawBuf)
	if err := enc.EncodeToken(start); err != nil {
		return fmt.Errorf("error encoding start element: %w", err)
	}

	// Read and copy all tokens until we find the matching end element
	depth := 1 // We've already written the start element
	for depth > 0 {
		tok, err := d.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("error reading tokens: %w", err)
		}

		// Update depth before encoding to properly track nesting
		switch tok.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
		}

		if err := enc.EncodeToken(tok); err != nil {
			return fmt.Errorf("error encoding token: %w", err)
		}
	}
	if err := enc.Flush(); err != nil {
		return fmt.Errorf("error flushing encoder: %w", err)
	}

	// Validate the captured XML
	if {{.ValidatorVar}} != nil {
		if errs, err := {{.ValidatorVar}}.Validate(bytes.NewReader(rawBuf.Bytes())); err != nil {
			return fmt.Errorf("validation error: %w (validating: %s)", err, rawBuf.String())
		} else if len(errs) > 0 {
			return fmt.Errorf("validation failed on XML: %s\nErrors: %w", rawBuf.String(), {{.FormatErrorsFunc}}(errs))
		}
	}

	// Unmarshal from the validated XML
	type Alias {{.TypeName}}
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(x),
	}

	dec := xml.NewDecoder(bytes.NewReader(rawBuf.Bytes()))
	if err := dec.Decode(aux); err != nil {
		return fmt.Errorf("failed to unmarshal {{.TypeName}}: %w", err)
	}

	return nil
}
`))

var validateMethodTemplate = template.Must(template.New("validate").Parse(`
// Validate checks if the current state of the object is compatible with the grammar.
func (x *{{.TypeName}}) Validate() error {
	// Initialize schema and validator on first use
	{{.SchemaOnceVar}}.Do(func() {
		{{.SchemaGrammarVar}}, {{.SchemaGrammarErrVar}} = rng.ParseSchema(strings.NewReader({{.SchemaContentVar}}))
	})
	if {{.SchemaGrammarErrVar}} != nil {
		return fmt.Errorf("failed to parse embedded schema: %w", {{.SchemaGrammarErrVar}})
	}

	{{.OnceVar}}.Do(func() {
		if {{.SchemaGrammarVar}} != nil {
			{{.ValidatorVar}} = validator.NewValidator({{.SchemaGrammarVar}}, validator.DefaultOptions())
		}
	})

	// Serialize the object to XML
	var buf bytes.Buffer
	buf.WriteString("<?xml version=\"1.0\"?>\n")

	enc := xml.NewEncoder(&buf)
	if err := enc.Encode(x); err != nil {
		return fmt.Errorf("failed to serialize {{.TypeName}}: %w", err)
	}
	if err := enc.Flush(); err != nil {
		return fmt.Errorf("error flushing encoder: %w", err)
	}

	// Validate the serialized XML
	if {{.ValidatorVar}} != nil {
		if errs, err := {{.ValidatorVar}}.Validate(bytes.NewReader(buf.Bytes())); err != nil {
			return fmt.Errorf("validation error: %w (validating: %s)", err, buf.String())
		} else if len(errs) > 0 {
			return fmt.Errorf("validation failed on XML: %s\nErrors: %w", buf.String(), {{.FormatErrorsFunc}}(errs))
		}
	}

	return nil
}
`))

var unmarshalHeaderTemplate = template.Must(template.New("header").Parse(`
// {{.SchemaContentVar}} contains the embedded RELAX NG schema
const {{.SchemaContentVar}} = ` + "`" + `{{.Schema}}` + "`" + `

// {{.SchemaGrammarVar}} is the schema grammar used for validation.
// It is automatically initialized from the embedded schema; if that ever fails
// {{.SchemaGrammarErrVar}} records the error so callers can surface it.
var {{.SchemaGrammarVar}} *rng.Grammar
var {{.SchemaGrammarErrVar}} error
var {{.SchemaOnceVar}} sync.Once

{{range .Validators}}var (
	{{.ValidatorVar}} *validator.Validator
	{{.OnceVar}} sync.Once
)
{{end}}
// {{.FormatErrorsFunc}} formats validation errors into a comprehensive error message
func {{.FormatErrorsFunc}}(errs []validator.ValidationError) error {
	if len(errs) == 0 {
		return nil
	}

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("validation failed with %d error(s):\n", len(errs)))

	for i, err := range errs {
		msg.WriteString(fmt.Sprintf("  [%d] %s\n", i+1, err.Error()))
		if len(err.Expected) > 0 {
			msg.WriteString(fmt.Sprintf("      Expected: %v\n", err.Expected))
		}
		if err.Found != "" {
			msg.WriteString(fmt.Sprintf("      Found: %s\n", err.Found))
		}
	}

	return fmt.Errorf("validation error: %s", msg.String())
}
`))

type unmarshalHeaderData struct {
	Schema              string
	SchemaContentVar    string
	SchemaGrammarVar    string
	SchemaGrammarErrVar string
	SchemaOnceVar       string
	FormatErrorsFunc    string
	Validators          []validatorDecl
}

type validatorDecl struct {
	ValidatorVar string
	OnceVar      string
}

// GenerateCodeWithUnmarshal generates Go code with custom UnmarshalXML methods
// that perform validation against the schema using the rng validator.
// Each type gets its own validator variable with lazy initialization.
// The schema is embedded in the generated code and automatically initialized.
//
//nolint:funlen // Code generation function with multiple output sections
func GenerateCodeWithUnmarshal(types []TypeInfo, packageName string, _ string, grammar *rng.Grammar) (string, error) {
	// Serialize the resolved grammar to ensure includes are embedded
	schemaContent := rng.SerializeGrammar(grammar)

	var imports []string
	imports = append(imports, "\"encoding/xml\"")
	imports = append(imports, "\"bytes\"", "\"fmt\"", "\"io\"", "\"strings\"", "\"sync\"")
	imports = append(imports, "\"github.com/mgilbir/relaxngo/rng\"")
	imports = append(imports, "\"github.com/mgilbir/relaxngo/validator\"")

	// Generate a unique identifier for schema variables based on first type name
	schemaID := ""
	if len(types) > 0 {
		schemaID = types[0].Name
	}

	// Generate all type definitions with methods (only root types need UnmarshalXML and Validate)
	typesWithMethods := make([]TypeWithMethods, 0, len(types))

	for _, t := range types {
		twm := TypeWithMethods{
			TypeInfo: t,
		}
		// Only generate UnmarshalXML and Validate methods for root types (which require validation)
		if t.IsRootType {
			twm.UnmarshalMethod = generateUnmarshalXMLMethod(&t, grammar, schemaID)
			twm.ValidateMethod = generateValidateMethod(&t, schemaID)
		}
		typesWithMethods = append(typesWithMethods, twm)
	}

	// Build output code
	var output strings.Builder
	output.WriteString("package " + packageName + "\n\n")

	// Write imports
	if len(imports) > 0 {
		output.WriteString("import (\n")
		for _, imp := range imports {
			output.WriteString("\t" + imp + "\n")
		}
		output.WriteString(")\n\n")
	}

	// Check if there are any root types that need validation
	hasRootTypes := false
	for _, t := range types {
		if t.IsRootType {
			hasRootTypes = true
			break
		}
	}

	// Only write header and validation infrastructure if there are root types
	if hasRootTypes {
		// Build validator declarations for root types only
		validators := make([]validatorDecl, 0)
		for _, twm := range typesWithMethods {
			if twm.TypeInfo.IsRootType {
				validators = append(validators, validatorDecl{
					ValidatorVar: twm.TypeInfo.Name + "Validator",
					OnceVar:      twm.TypeInfo.Name + "Once",
				})
			}
		}

		// Create unique schema variable names to avoid collisions
		schemaContentVar := "schema" + schemaID + "Content"
		schemaGrammarVar := "schema" + schemaID + "Grammar"
		schemaGrammarErrVar := "schema" + schemaID + "GrammarErr"
		schemaOnceVar := "schema" + schemaID + "Once"
		formatErrorsFunc := "formatValidationErrors" + schemaID

		// Write header with schema, grammar declarations, and validation helper
		if err := unmarshalHeaderTemplate.Execute(&output, unmarshalHeaderData{
			Schema:              schemaContent,
			SchemaContentVar:    schemaContentVar,
			SchemaGrammarVar:    schemaGrammarVar,
			SchemaGrammarErrVar: schemaGrammarErrVar,
			SchemaOnceVar:       schemaOnceVar,
			FormatErrorsFunc:    formatErrorsFunc,
			Validators:          validators,
		}); err != nil {
			return "", fmt.Errorf("failed to execute unmarshal header template: %w", err)
		}
		output.WriteString("\n")
	}

	// Write types and methods
	for _, twm := range typesWithMethods {
		// Write struct definition
		output.WriteString(generateStructDefinition(twm.TypeInfo))

		// Write constructor and methods for root types
		if twm.TypeInfo.IsRootType {
			// Write Validate method (root types only)
			if twm.ValidateMethod != "" {
				output.WriteString("\n" + twm.ValidateMethod)
			}
			// Write UnmarshalXML method (root types only)
			if twm.UnmarshalMethod != "" {
				output.WriteString("\n" + twm.UnmarshalMethod)
			}
		}

		output.WriteString("\n")
	}

	// Format the generated code using go/format
	formatted, err := format.Source([]byte(output.String()))
	if err != nil {
		// If formatting fails, return the unformatted code with error info
		return output.String(), fmt.Errorf("generated code has syntax errors: %w", err)
	}

	return string(formatted), nil
}

// generateStructDefinition generates the struct definition for a type
func generateStructDefinition(typeInfo TypeInfo) string {
	var sb strings.Builder
	sb.WriteString("type " + typeInfo.Name + " struct {\n")
	for _, field := range typeInfo.Fields {
		sb.WriteString("\t" + field.Name + " " + field.Type + " " + field.XMLTag + "\n")
	}
	sb.WriteString("}\n")
	return sb.String()
}

// generateUnmarshalXMLMethod generates an UnmarshalXML method with two-pass validation
func generateUnmarshalXMLMethod(typeInfo *TypeInfo, _ *rng.Grammar, schemaID string) string {
	data := unmarshalMethodData{
		TypeName:            typeInfo.Name,
		ValidatorVar:        typeInfo.Name + "Validator",
		OnceVar:             typeInfo.Name + "Once",
		IsRootType:          typeInfo.IsRootType,
		SchemaContentVar:    "schema" + schemaID + "Content",
		SchemaGrammarVar:    "schema" + schemaID + "Grammar",
		SchemaGrammarErrVar: "schema" + schemaID + "GrammarErr",
		SchemaOnceVar:       "schema" + schemaID + "Once",
		FormatErrorsFunc:    "formatValidationErrors" + schemaID,
	}

	var buf bytes.Buffer
	if err := unmarshalMethodTemplate.Execute(&buf, data); err != nil {
		panic(fmt.Sprintf("failed to execute template: %v", err))
	}
	return buf.String()
}

// generateValidateMethod generates a Validate method that checks object state against the grammar
func generateValidateMethod(typeInfo *TypeInfo, schemaID string) string {
	data := unmarshalMethodData{
		TypeName:            typeInfo.Name,
		ValidatorVar:        typeInfo.Name + "Validator",
		OnceVar:             typeInfo.Name + "Once",
		IsRootType:          typeInfo.IsRootType,
		SchemaContentVar:    "schema" + schemaID + "Content",
		SchemaGrammarVar:    "schema" + schemaID + "Grammar",
		SchemaGrammarErrVar: "schema" + schemaID + "GrammarErr",
		SchemaOnceVar:       "schema" + schemaID + "Once",
		FormatErrorsFunc:    "formatValidationErrors" + schemaID,
	}

	var buf bytes.Buffer
	if err := validateMethodTemplate.Execute(&buf, data); err != nil {
		panic(fmt.Sprintf("failed to execute template: %v", err))
	}
	return buf.String()
}

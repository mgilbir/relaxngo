package XXX

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"github.com/mgilbir/relaxngo/rng"
	"github.com/mgilbir/relaxngo/validator"
	"io"
	"strings"
	"sync"
)

// schemaPersonContent contains the embedded RELAX NG schema
const schemaPersonContent = `<?xml version="1.0"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
	<start>
		<ref name="person"/>
	</start>
	<define name="person">
		<element name="person">
			<attribute name="id">
			</attribute>
			<element name="name">
				<text/>
			</element>
			<optional>
				<attribute name="age">
				</attribute>
			</optional>
		</element>
	</define>
</grammar>
`

// schemaPersonGrammar is the schema grammar used for validation.
// It is automatically initialized from the embedded schema.
var schemaPersonGrammar *rng.Grammar
var schemaPersonOnce sync.Once

var (
	PersonValidator *validator.Validator
	PersonOnce      sync.Once
)

// formatValidationErrors formats validation errors into a comprehensive error message
func formatValidationErrors(errs []validator.ValidationError) error {
	if len(errs) == 0 {
		return nil
	}

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("validation failed with %d error(s):\n", len(errs)))

	for i, err := range errs {
		msg.WriteString(fmt.Sprintf("  [%d] %s\n", i+1, err.Error()))
		if err.Expected != nil && len(err.Expected) > 0 {
			msg.WriteString(fmt.Sprintf("      Expected: %v\n", err.Expected))
		}
		if err.Found != "" {
			msg.WriteString(fmt.Sprintf("      Found: %s\n", err.Found))
		}
	}

	return fmt.Errorf("validation error: %s", msg.String())
}

type Person struct {
	XMLName xml.Name `xml:"person"`
	Id      string   `xml:"id,attr"`
	Name    string   `xml:"name"`
	Age     *string  `xml:"age,attr,omitempty"`
}

// Validate checks if the current state of the object is compatible with the grammar.
func (x *Person) Validate() error {
	// Initialize schema and validator on first use
	schemaPersonOnce.Do(func() {
		var err error
		schemaPersonGrammar, err = rng.ParseSchema(strings.NewReader(schemaPersonContent))
		if err != nil {
			panic("failed to parse embedded schema: " + err.Error())
		}
	})

	PersonOnce.Do(func() {
		if schemaPersonGrammar != nil {
			PersonValidator = validator.NewValidator(schemaPersonGrammar, validator.DefaultOptions())
		}
	})

	// Serialize the object to XML
	var buf bytes.Buffer
	buf.WriteString("<?xml version=\"1.0\"?>\n")

	enc := xml.NewEncoder(&buf)
	if err := enc.Encode(x); err != nil {
		return fmt.Errorf("failed to serialize Person: %w", err)
	}
	enc.Flush()

	// Validate the serialized XML
	if PersonValidator != nil {
		if errs, err := PersonValidator.Validate(bytes.NewReader(buf.Bytes())); err != nil {
			return fmt.Errorf("validation error: %w (validating: %s)", err, buf.String())
		} else if len(errs) > 0 {
			return fmt.Errorf("validation failed on XML: %s\nErrors: %w", buf.String(), formatValidationErrors(errs))
		}
	}

	return nil
}

// UnmarshalXML unmarshals XML with validation against the schema.
func (x *Person) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	// Initialize schema and validator on first use
	schemaPersonOnce.Do(func() {
		var err error
		schemaPersonGrammar, err = rng.ParseSchema(strings.NewReader(schemaPersonContent))
		if err != nil {
			panic("failed to parse embedded schema: " + err.Error())
		}
	})

	PersonOnce.Do(func() {
		if schemaPersonGrammar != nil {
			PersonValidator = validator.NewValidator(schemaPersonGrammar, validator.DefaultOptions())
		}
	})

	// Two-pass approach: capture raw XML for validation
	var rawBuf bytes.Buffer
	rawBuf.WriteString("<?xml version=\"1.0\"?>\n")

	// Reconstruct the XML for this element
	enc := xml.NewEncoder(&rawBuf)
	enc.EncodeToken(start)

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
	enc.Flush()

	// Validate the captured XML
	if PersonValidator != nil {
		// Debug: log what we're validating
		_ = rawBuf.String() // Keep the buffer content accessible for debugging
		if errs, err := PersonValidator.Validate(bytes.NewReader(rawBuf.Bytes())); err != nil {
			return fmt.Errorf("validation error: %w (validating: %s)", err, rawBuf.String())
		} else if len(errs) > 0 {
			return fmt.Errorf("validation failed on XML: %s\nErrors: %w", rawBuf.String(), formatValidationErrors(errs))
		}
	}

	// Unmarshal from the validated XML
	type Alias Person
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(x),
	}

	dec := xml.NewDecoder(bytes.NewReader(rawBuf.Bytes()))
	if err := dec.Decode(aux); err != nil {
		return fmt.Errorf("failed to unmarshal Person: %w", err)
	}

	return nil
}


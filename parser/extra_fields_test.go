// Package parser tests additional parser functionality.
package parser

import (
	"encoding/xml"
	"strings"
	"testing"
)

type SimplePerson struct {
	XMLName xml.Name `xml:"person"`
	ID      string   `xml:"id,attr"`
	Name    string   `xml:"name"`
}

func TestParseXML_ExtraFieldsIgnored(t *testing.T) {
	xmlData := `<person id="123">
		<name>John Doe</name>
		<age>30</age>
		<email>john@example.com</email>
		<address>
			<street>123 Main St</street>
			<city>Boston</city>
		</address>
	</person>`

	var person SimplePerson
	err := ParseXML(strings.NewReader(xmlData), &person)
	if err != nil {
		t.Fatalf("ParseXML failed: %v", err)
	}

	if person.ID != "123" {
		t.Errorf("Expected Id '123', got '%s'", person.ID)
	}

	if person.Name != "John Doe" {
		t.Errorf("Expected Name 'John Doe', got '%s'", person.Name)
	}

	t.Logf("Successfully parsed person with Id=%s, Name=%s", person.ID, person.Name)
	t.Logf("Extra fields (age, email, address) were silently ignored")
}

func TestParseXML_ExtraAttributesIgnored(t *testing.T) {
	xmlData := `<person id="456" role="admin" department="IT" active="true">
		<name>Jane Smith</name>
	</person>`

	var person SimplePerson
	err := ParseXML(strings.NewReader(xmlData), &person)
	if err != nil {
		t.Fatalf("ParseXML failed: %v", err)
	}

	if person.ID != "456" {
		t.Errorf("Expected Id '456', got '%s'", person.ID)
	}

	t.Logf("Extra attributes (role, department, active) were silently ignored")
}

func TestParseXML_MissingFieldsUseZeroValues(t *testing.T) {
	xmlData := `<person id="789"></person>`

	var person SimplePerson
	err := ParseXML(strings.NewReader(xmlData), &person)
	if err != nil {
		t.Fatalf("ParseXML failed: %v", err)
	}

	if person.ID != "789" {
		t.Errorf("Expected Id '789', got '%s'", person.ID)
	}

	if person.Name != "" {
		t.Errorf("Expected Name to be empty string (zero value), got '%s'", person.Name)
	}

	t.Logf("Missing 'name' element resulted in zero value (empty string)")
}

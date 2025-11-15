package parser

import (
	"encoding/xml"
	"strings"
	"testing"
)

type TestPerson struct {
	XMLName xml.Name `xml:"person"`
	ID      string   `xml:"id,attr"`
	Name    string   `xml:"name"`
	Age     *int     `xml:"age,attr,omitempty"`
}

func (p *TestPerson) Validate() error {
	return nil
}

func TestParseXML_Simple(t *testing.T) {
	xmlData := `<person id="123">
		<name>John Doe</name>
	</person>`

	var person TestPerson
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

	if person.Age != nil {
		t.Errorf("Expected Age to be nil, got %v", *person.Age)
	}
}

func TestParseXML_WithOptionalField(t *testing.T) {
	xmlData := `<person id="456" age="30">
		<name>Jane Smith</name>
	</person>`

	var person TestPerson
	err := ParseXML(strings.NewReader(xmlData), &person)
	if err != nil {
		t.Fatalf("ParseXML failed: %v", err)
	}

	if person.ID != "456" {
		t.Errorf("Expected Id '456', got '%s'", person.ID)
	}

	if person.Age == nil {
		t.Fatal("Expected Age to be non-nil")
	}

	if *person.Age != 30 {
		t.Errorf("Expected Age 30, got %d", *person.Age)
	}
}

func TestParseXML_InvalidXML(t *testing.T) {
	xmlData := `<person id="123">`

	var person TestPerson
	err := ParseXML(strings.NewReader(xmlData), &person)
	if err == nil {
		t.Error("Expected error for invalid XML")
	}
}

func TestParseXML_EmptyDocument(t *testing.T) {
	xmlData := ``

	var person TestPerson
	err := ParseXML(strings.NewReader(xmlData), &person)
	if err == nil {
		t.Error("Expected error for empty document")
	}
}

package parser

import (
	"encoding/xml"
	"strings"
	"testing"
)

const testJohnDoe = "John Doe"

type StrictPerson struct {
	XMLName xml.Name `xml:"person"`
	ID      string   `xml:"id,attr"`
	Name    string   `xml:"name"`
	Age     *int     `xml:"age,attr,omitempty"`
}

func TestStrictParseXML_ValidDocument(t *testing.T) {
	xmlData := `<person id="123">
		<name>John Doe</name>
	</person>`

	var person StrictPerson
	err := StrictParseXML(strings.NewReader(xmlData), &person)
	if err != nil {
		t.Fatalf("StrictParseXML failed on valid document: %v", err)
	}

	if person.ID != "123" {
		t.Errorf("Expected Id '123', got '%s'", person.ID)
	}

	if person.Name != testJohnDoe {
		t.Errorf("Expected Name %q, got %q", testJohnDoe, person.Name)
	}
}

func TestStrictParseXML_DetectsExtraElements(t *testing.T) {
	xmlData := `<person id="123">
		<name>John Doe</name>
		<email>john@example.com</email>
		<phone>555-1234</phone>
	</person>`

	var person StrictPerson
	err := StrictParseXML(strings.NewReader(xmlData), &person)

	if err == nil {
		t.Fatal("Expected error for extra elements, got nil")
	}

	unknownErr, ok := err.(*UnknownFieldError)
	if !ok {
		t.Fatalf("Expected UnknownFieldError, got %T: %v", err, err)
	}

	if len(unknownErr.UnknownElements) == 0 {
		t.Error("Expected unknown elements to be detected")
	}

	t.Logf("Detected unknown elements: %v", unknownErr.UnknownElements)

	hasEmail := false
	hasPhone := false
	for _, elem := range unknownErr.UnknownElements {
		if strings.Contains(elem, "email") {
			hasEmail = true
		}
		if strings.Contains(elem, "phone") {
			hasPhone = true
		}
	}

	if !hasEmail {
		t.Error("Expected 'email' to be in unknown elements")
	}
	if !hasPhone {
		t.Error("Expected 'phone' to be in unknown elements")
	}
}

func TestStrictParseXML_DetectsExtraAttributes(t *testing.T) {
	xmlData := `<person id="123" role="admin" department="IT">
		<name>Jane Smith</name>
	</person>`

	var person StrictPerson
	err := StrictParseXML(strings.NewReader(xmlData), &person)

	if err == nil {
		t.Fatal("Expected error for extra attributes, got nil")
	}

	unknownErr, ok := err.(*UnknownFieldError)
	if !ok {
		t.Fatalf("Expected UnknownFieldError, got %T: %v", err, err)
	}

	if len(unknownErr.UnknownAttributes) == 0 {
		t.Error("Expected unknown attributes to be detected")
	}

	t.Logf("Detected unknown attributes: %v", unknownErr.UnknownAttributes)

	hasRole := false
	hasDept := false
	for _, attr := range unknownErr.UnknownAttributes {
		if strings.Contains(attr, "role") {
			hasRole = true
		}
		if strings.Contains(attr, "department") {
			hasDept = true
		}
	}

	if !hasRole {
		t.Error("Expected 'role' to be in unknown attributes")
	}
	if !hasDept {
		t.Error("Expected 'department' to be in unknown attributes")
	}
}

func TestStrictParseXML_AllowsOptionalFields(t *testing.T) {
	xmlData := `<person id="456" age="30">
		<name>Jane Smith</name>
	</person>`

	var person StrictPerson
	err := StrictParseXML(strings.NewReader(xmlData), &person)
	if err != nil {
		t.Fatalf("StrictParseXML failed with optional field present: %v", err)
	}

	if person.Age == nil {
		t.Fatal("Expected Age to be non-nil")
	}

	if *person.Age != 30 {
		t.Errorf("Expected Age 30, got %d", *person.Age)
	}
}

func TestStrictParseXML_CombinedExtraFields(t *testing.T) {
	xmlData := `<person id="789" status="active" manager="boss">
		<name>Bob</name>
		<email>bob@example.com</email>
		<address>
			<street>123 Main</street>
		</address>
	</person>`

	var person StrictPerson
	err := StrictParseXML(strings.NewReader(xmlData), &person)

	if err == nil {
		t.Fatal("Expected error for extra fields, got nil")
	}

	unknownErr, ok := err.(*UnknownFieldError)
	if !ok {
		t.Fatalf("Expected UnknownFieldError, got %T: %v", err, err)
	}

	if len(unknownErr.UnknownElements) == 0 {
		t.Error("Expected unknown elements to be detected")
	}

	if len(unknownErr.UnknownAttributes) == 0 {
		t.Error("Expected unknown attributes to be detected")
	}

	t.Logf("Error message: %s", unknownErr.Error())
}

type NestedStruct struct {
	XMLName xml.Name    `xml:"parent"`
	ID      string      `xml:"id,attr"`
	Child   ChildStruct `xml:"child"`
}

type ChildStruct struct {
	Name  string `xml:"name"`
	Value string `xml:"value,attr"`
}

func TestStrictParseXML_NestedStructs(t *testing.T) {
	xmlData := `<parent id="123">
		<child value="test">
			<name>Test</name>
		</child>
	</parent>`

	var nested NestedStruct
	err := StrictParseXML(strings.NewReader(xmlData), &nested)
	if err != nil {
		t.Fatalf("StrictParseXML failed on valid nested structure: %v", err)
	}

	if nested.Child.Name != "Test" {
		t.Errorf("Expected child name 'Test', got '%s'", nested.Child.Name)
	}
}

func TestStrictParseXML_DetectsExtraInNested(t *testing.T) {
	xmlData := `<parent id="123">
		<child value="test" extra="bad">
			<name>Test</name>
			<unknown>Bad</unknown>
		</child>
	</parent>`

	var nested NestedStruct
	err := StrictParseXML(strings.NewReader(xmlData), &nested)

	if err == nil {
		t.Fatal("Expected error for extra fields in nested structure, got nil")
	}

	unknownErr, ok := err.(*UnknownFieldError)
	if !ok {
		t.Fatalf("Expected UnknownFieldError, got %T: %v", err, err)
	}

	if len(unknownErr.UnknownElements) == 0 {
		t.Error("Expected unknown elements to be detected")
	}

	if len(unknownErr.UnknownAttributes) == 0 {
		t.Error("Expected unknown attributes to be detected")
	}

	t.Logf("Detected unknown fields in nested: %s", unknownErr.Error())
}

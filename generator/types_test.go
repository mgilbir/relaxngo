package generator

import (
	"strings"
	"testing"

	"github.com/mgilbir/relaxngo/rng"
)

func TestGenerateTypes(t *testing.T) {
	schema := `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
	<ref name="greeting"/>
  </start>
  <define name="greeting">
	<element name="greeting">
	  <text/>
	</element>
  </define>
</grammar>`

	grammar, err := rng.ParseSchema(strings.NewReader(schema))
	if err != nil {
		t.Fatalf("Failed to parse schema: %v", err)
	}

	types, err := GenerateTypes(grammar)
	if err != nil {
		t.Fatalf("Failed to generate types: %v", err)
	}

	if len(types) != 1 {
		t.Fatalf("Expected 1 type, got %d", len(types))
	}

	v := types[0]
	if v.Name != "Greeting" {
		t.Errorf("Expected type name 'Greeting', got '%s'", v.Name)
	}

	for _, field := range v.Fields {
		// Add assertions for fields if needed
		t.Logf("Field: %s of type %s", field.Name, field.Type)
	}
}

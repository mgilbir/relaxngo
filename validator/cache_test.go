package validator

import (
	"strings"
	"sync"
	"testing"

	"github.com/mgilbir/relaxngo/rng"
)

const (
	// Test schema constants
	testPersonSchemaXML = `<?xml version="1.0" encoding="UTF-8"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <ref name="person"/>
  </start>
  <define name="person">
    <element name="person">
      <attribute name="id"><text/></attribute>
      <element name="name"><text/></element>
      <element name="age"><text/></element>
    </element>
  </define>
</grammar>`

	testPersonDocXML = `<?xml version="1.0"?>
<person id="123">
  <name>John Doe</name>
  <age>30</age>
</person>`
)

// TestCachedValidatorBasic tests basic cached validator functionality
func TestCachedValidatorBasic(t *testing.T) {
	// Parse grammar once
	grammar, err := rng.ParseSchema(strings.NewReader(testPersonSchemaXML))
	if err != nil {
		t.Fatalf("failed to parse schema: %v", err)
	}

	// Create cached validator
	cv := NewCachedValidatorFromGrammar(grammar, DefaultOptions())

	// Test multiple validations with the same schema
	docs := []string{
		`<?xml version="1.0"?>
<person id="1">
  <name>Alice</name>
  <age>30</age>
</person>`,
		`<?xml version="1.0"?>
<person id="2">
  <name>Bob</name>
  <age>25</age>
</person>`,
		`<?xml version="1.0"?>
<person id="3">
  <name>Charlie</name>
  <age>35</age>
</person>`,
	}

	for i, xml := range docs {
		errors, err := cv.Validate(strings.NewReader(xml))
		if err != nil {
			t.Fatalf("document %d validation failed: %v", i, err)
		}
		if len(errors) > 0 {
			t.Fatalf("document %d has errors: %v", i, errors)
		}
	}
}

// TestCachedValidatorConcurrentUsage tests thread-safe concurrent validation
func TestCachedValidatorConcurrentUsage(t *testing.T) {
	schema := `<?xml version="1.0" encoding="UTF-8"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <ref name="item"/>
  </start>
  <define name="item">
    <element name="item">
      <attribute name="id"><data type="string"/></attribute>
      <element name="value"><text/></element>
    </element>
  </define>
</grammar>`

	grammar, err := rng.ParseSchema(strings.NewReader(schema))
	if err != nil {
		t.Fatalf("failed to parse schema: %v", err)
	}
	cv := NewCachedValidatorFromGrammar(grammar, DefaultOptions())

	// Run concurrent validations
	const numGoroutines = 10
	const docsPerGoroutine = 5

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*docsPerGoroutine)

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for d := 0; d < docsPerGoroutine; d++ {
				id := gid*docsPerGoroutine + d
				xml := `<?xml version="1.0"?>
<item id="` + string(rune('0'+id%10)) + `">
   <value>test</value>
</item>`

				_, err := cv.Validate(strings.NewReader(xml))
				if err != nil {
					errors <- err
				}
			}
		}(g)
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Errorf("concurrent validation failed: %v", err)
	}
}

// TestCachedValidatorGetGrammar tests grammar retrieval
func TestCachedValidatorGetGrammar(t *testing.T) {
	schema := `<?xml version="1.0" encoding="UTF-8"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <ref name="root"/>
  </start>
  <define name="root">
    <element name="root"><text/></element>
  </define>
</grammar>`

	grammar, _ := rng.ParseSchema(strings.NewReader(schema))
	cv := NewCachedValidatorFromGrammar(grammar, DefaultOptions())

	// Get grammar and verify it's the same
	retrievedGrammar := cv.GetGrammar()
	if retrievedGrammar == nil {
		t.Fatal("GetGrammar returned nil")
	}

	if len(retrievedGrammar.Defines) != len(grammar.Defines) {
		t.Errorf("grammar defines count mismatch: got %d, want %d",
			len(retrievedGrammar.Defines), len(grammar.Defines))
	}
}

// TestCachedValidatorOptions tests option management
func TestCachedValidatorOptions(t *testing.T) {
	schema := `<?xml version="1.0" encoding="UTF-8"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <ref name="root"/>
  </start>
  <define name="root">
    <element name="root"><text/></element>
  </define>
</grammar>`

	grammar, _ := rng.ParseSchema(strings.NewReader(schema))

	// Create with default options
	opts := DefaultOptions()
	opts.MaxDepth = 100
	opts.MaxDocumentBytes = 1000

	cv := NewCachedValidatorFromGrammar(grammar, opts)

	// Get options
	retrievedOpts := cv.GetOptions()
	if retrievedOpts.MaxDepth != 100 {
		t.Error("MaxDepth option mismatch")
	}
	if retrievedOpts.MaxDocumentBytes != 1000 {
		t.Error("MaxDocumentBytes option mismatch")
	}

	// Update options
	newOpts := DefaultOptions()
	newOpts.MaxDepth = 10
	newOpts.MaxDocumentBytes = 2000

	cv.UpdateOptions(newOpts)

	// Verify update
	retrievedOpts = cv.GetOptions()
	if retrievedOpts.MaxDepth != 10 {
		t.Error("MaxDepth was not updated")
	}
	if retrievedOpts.MaxDocumentBytes != 2000 {
		t.Error("MaxDocumentBytes was not updated")
	}
}

// BenchmarkCachedValidationReuse benchmarks reusing the same cached schema
func BenchmarkCachedValidationReuse(b *testing.B) {
	grammar, err := rng.ParseSchema(strings.NewReader(testPersonSchemaXML))
	if err != nil {
		b.Fatalf("ParseSchema error: %v", err)
	}
	cv := NewCachedValidatorFromGrammar(grammar, DefaultOptions())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := cv.Validate(strings.NewReader(testPersonDocXML))
		if err != nil {
			b.Fatalf("Validate error: %v", err)
		}
	}
}

// BenchmarkVsNonCachedValidation compares cached vs non-cached validation
func BenchmarkVsNonCachedValidation(b *testing.B) {
	b.Run("Cached", func(b *testing.B) {
		grammar, err := rng.ParseSchema(strings.NewReader(testPersonSchemaXML))
		if err != nil {
			b.Fatalf("ParseSchema error: %v", err)
		}
		cv := NewCachedValidatorFromGrammar(grammar, DefaultOptions())

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := cv.Validate(strings.NewReader(testPersonDocXML))
			if err != nil {
				b.Fatalf("Validate error: %v", err)
			}
		}
	})

	b.Run("NonCached", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			grammar, err := rng.ParseSchema(strings.NewReader(testPersonSchemaXML))
			if err != nil {
				b.Fatalf("ParseSchema error: %v", err)
			}
			v := NewValidator(grammar, DefaultOptions())
			_, err = v.Validate(strings.NewReader(testPersonDocXML))
			if err != nil {
				b.Fatalf("Validate error: %v", err)
			}
		}
	})
}

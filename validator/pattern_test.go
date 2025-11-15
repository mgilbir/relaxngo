package validator

import (
	"encoding/xml"
	"strings"
	"testing"

	"github.com/mgilbir/relaxngo/rng"
)

// runChoicePatternTest validates a choice pattern against XML
func runChoicePatternTest(t *testing.T, pattern Pattern, defines map[string]*rng.Define, xmlStr string, shouldMatch bool) {
	decoder := xml.NewDecoder(strings.NewReader(xmlStr))
	_, _ = decoder.Token() // Get <root>

	buffer, err := NewTokenBuffer(decoder)
	if err != nil {
		t.Fatalf("failed to create buffer: %v", err)
	}

	ctx := &validationContext{
		path:    []string{"root"},
		errors:  &[]ValidationError{},
		options: DefaultOptions(),
		defines: defines,
	}

	result := MatchPattern(pattern, buffer, defines, ctx)
	if shouldMatch && !result.Success {
		t.Errorf("expected match but got error: %v", result.Error)
	}
	if !shouldMatch && result.Success {
		t.Errorf("expected no match but got success")
	}
}

// TestPatternAST_SimpleChoice tests basic choice pattern matching
func TestPatternAST_SimpleChoice(t *testing.T) {
	schema := `<?xml version="1.0" encoding="UTF-8"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <element name="root">
      <choice>
        <element name="a"><text/></element>
        <element name="b"><text/></element>
      </choice>
    </element>
  </start>
</grammar>`

	grammar, err := rng.ParseSchema(strings.NewReader(schema))
	if err != nil {
		t.Fatalf("failed to parse schema: %v", err)
	}

	defines := make(map[string]*rng.Define)
	for i := range grammar.Defines {
		defines[grammar.Defines[i].Name] = &grammar.Defines[i]
	}

	// Build pattern from start element
	pattern, err := BuildPattern(grammar.Start.Element.RawContent, defines)
	if err != nil {
		t.Fatalf("failed to build pattern: %v", err)
	}

	t.Logf("Pattern type: %T, kind: %v", pattern, pattern.Kind())

	t.Run("valid choice alternative a", func(t *testing.T) {
		runChoicePatternTest(t, pattern, defines, `<root><a>hello</a></root>`, true)
	})

	t.Run("valid choice alternative b", func(t *testing.T) {
		runChoicePatternTest(t, pattern, defines, `<root><b>world</b></root>`, true)
	})

	t.Run("invalid choice alternative", func(t *testing.T) {
		runChoicePatternTest(t, pattern, defines, `<root><c>invalid</c></root>`, false)
	})
}

// TestPatternAST_Interleave tests interleave pattern matching
func TestPatternAST_Interleave(t *testing.T) {
	schema := `<?xml version="1.0" encoding="UTF-8"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <element name="metadata">
      <interleave>
        <element name="title"><text/></element>
        <element name="author"><text/></element>
      </interleave>
    </element>
  </start>
</grammar>`

	grammar, err := rng.ParseSchema(strings.NewReader(schema))
	if err != nil {
		t.Fatalf("failed to parse schema: %v", err)
	}

	defines := make(map[string]*rng.Define)

	// Build pattern from start element
	pattern, err := BuildPattern(grammar.Start.Element.RawContent, defines)
	if err != nil {
		t.Fatalf("failed to build pattern: %v", err)
	}

	t.Logf("Pattern type: %T, kind: %v", pattern, pattern.Kind())

	// Test with correct order
	xmlCorrect := `<metadata><title>Book</title><author>John</author></metadata>`
	decoder := xml.NewDecoder(strings.NewReader(xmlCorrect))
	_, _ = decoder.Token()

	buffer, err := NewTokenBuffer(decoder)
	if err != nil {
		t.Fatalf("failed to create buffer: %v", err)
	}

	ctx := &validationContext{
		path:    []string{"metadata"},
		errors:  &[]ValidationError{},
		options: DefaultOptions(),
		defines: defines,
	}

	result := MatchPattern(pattern, buffer, defines, ctx)
	if !result.Success {
		t.Errorf("expected match for correct order, got error: %v", result.Error)
	}

	// Test with reversed order
	xmlReversed := `<metadata><author>John</author><title>Book</title></metadata>`
	decoder = xml.NewDecoder(strings.NewReader(xmlReversed))
	_, _ = decoder.Token()

	buffer, err = NewTokenBuffer(decoder)
	if err != nil {
		t.Fatalf("failed to create buffer: %v", err)
	}

	result = MatchPattern(pattern, buffer, defines, ctx)
	if !result.Success {
		t.Errorf("expected match for reversed order (interleave), got error: %v", result.Error)
	}
}

// TestPatternAST_Group tests sequential group matching
func TestPatternAST_Group(t *testing.T) {
	schema := `<?xml version="1.0" encoding="UTF-8"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <element name="doc">
      <group>
        <element name="first"><text/></element>
        <element name="second"><text/></element>
      </group>
    </element>
  </start>
</grammar>`

	grammar, err := rng.ParseSchema(strings.NewReader(schema))
	if err != nil {
		t.Fatalf("failed to parse schema: %v", err)
	}

	defines := make(map[string]*rng.Define)

	// Build pattern from start element
	pattern, err := BuildPattern(grammar.Start.Element.RawContent, defines)
	if err != nil {
		t.Fatalf("failed to build pattern: %v", err)
	}

	// Test with correct order
	xmlCorrect := `<doc><first>A</first><second>B</second></doc>`
	decoder := xml.NewDecoder(strings.NewReader(xmlCorrect))
	_, _ = decoder.Token()

	buffer, err := NewTokenBuffer(decoder)
	if err != nil {
		t.Fatalf("failed to create buffer: %v", err)
	}

	ctx := &validationContext{
		path:    []string{"doc"},
		errors:  &[]ValidationError{},
		options: DefaultOptions(),
		defines: defines,
	}

	result := MatchPattern(pattern, buffer, defines, ctx)
	if !result.Success {
		t.Errorf("expected match for correct order, got error: %v", result.Error)
	}

	// Test with wrong order (should fail)
	xmlWrong := `<doc><second>B</second><first>A</first></doc>`
	decoder = xml.NewDecoder(strings.NewReader(xmlWrong))
	_, _ = decoder.Token()

	buffer, err = NewTokenBuffer(decoder)
	if err != nil {
		t.Fatalf("failed to create buffer: %v", err)
	}

	result = MatchPattern(pattern, buffer, defines, ctx)
	if result.Success {
		t.Errorf("expected no match for wrong order (group is sequential), but got success")
	}
}

// runBuildPatternTest validates pattern building from RawContent
func runBuildPatternTest(t *testing.T, rawContent []byte, wantKind PatternKind, defines map[string]*rng.Define, description string) {
	pattern, err := BuildPattern(rawContent, defines)
	if err != nil {
		t.Fatalf("BuildPattern failed: %v", err)
	}

	if pattern.Kind() != wantKind {
		t.Errorf("%s: got kind %v, want %v", description, pattern.Kind(), wantKind)
	}
}

// TestBuildPattern tests pattern building from RawContent
func TestBuildPattern(t *testing.T) {
	defines := make(map[string]*rng.Define)

	t.Run("empty content", func(t *testing.T) {
		runBuildPatternTest(t, []byte(""), AnyContentK, defines, "empty bytes should create AnyContentPat")
	})

	t.Run("single element", func(t *testing.T) {
		rawContent := []byte(`<element xmlns="http://relaxng.org/ns/structure/1.0" name="test"><text/></element>`)
		runBuildPatternTest(t, rawContent, ElementK, defines, "single element creates ElementPat")
	})

	t.Run("group", func(t *testing.T) {
		rawContent := []byte(`<group xmlns="http://relaxng.org/ns/structure/1.0">
<element name="a"><text/></element>
<element name="b"><text/></element>
</group>`)
		runBuildPatternTest(t, rawContent, GroupK, defines, "group creates GroupPat")
	})

	t.Run("choice", func(t *testing.T) {
		rawContent := []byte(`<choice xmlns="http://relaxng.org/ns/structure/1.0">
<element name="a"><text/></element>
<element name="b"><text/></element>
</choice>`)
		runBuildPatternTest(t, rawContent, ChoiceK, defines, "choice creates ChoicePat")
	})

	t.Run("interleave", func(t *testing.T) {
		rawContent := []byte(`<interleave xmlns="http://relaxng.org/ns/structure/1.0">
<element name="a"><text/></element>
<element name="b"><text/></element>
</interleave>`)
		runBuildPatternTest(t, rawContent, InterleaveK, defines, "interleave creates InterleavePat")
	})
}

// Package main manages the RelaxNG test suite archive.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"

	"github.com/mgilbir/relaxngo/rng"
	"github.com/mgilbir/relaxngo/validator"
)

// TestCase represents a single conformance test
type TestCase struct {
	Name        string   // Test name
	SchemaXML   string   // Schema definition
	ValidDocs   []string // Documents that should validate
	InvalidDocs []string // Documents that should not validate
}

// runTest executes a single test case
func runTest(test *TestCase) bool {
	grammar, err := rng.ParseSchema(bytes.NewReader([]byte(test.SchemaXML)))
	if err != nil {
		fmt.Printf("  ERROR: Failed to parse schema: %v\n", err)
		return false
	}

	v := validator.NewValidator(grammar, validator.DefaultOptions())

	// Check that valid documents pass
	for _, doc := range test.ValidDocs {
		errs, err := v.Validate(bytes.NewReader([]byte(doc)))
		if err != nil || len(errs) > 0 {
			fmt.Printf("  ERROR: Valid document failed validation: %s\n", doc[:minInt(50, len(doc))])
			if err != nil {
				fmt.Printf("    Error: %v\n", err)
			}
			for _, e := range errs {
				fmt.Printf("    Validation error: %v\n", e)
			}
			return false
		}
	}

	// Check that invalid documents fail
	for _, doc := range test.InvalidDocs {
		errs, err := v.Validate(bytes.NewReader([]byte(doc)))
		if err == nil && len(errs) == 0 {
			fmt.Printf("  ERROR: Invalid document passed validation: %s\n", doc[:minInt(50, len(doc))])
			return false
		}
	}

	return true
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Test suite definitions

var basicPatternTests = []TestCase{
	{
		Name: "Simple element",
		SchemaXML: `<?xml version="1.0"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <ref name="root"/>
  </start>
  <define name="root">
    <element name="root">
      <text/>
    </element>
  </define>
</grammar>`,
		ValidDocs: []string{
			`<root>text content</root>`,
			`<root/>`,
		},
		InvalidDocs: []string{
			`<other>text</other>`,
			`<root><child/></root>`,
		},
	},
	{
		Name: "Element with required attribute",
		SchemaXML: `<?xml version="1.0"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <ref name="root"/>
  </start>
  <define name="root">
    <element name="root">
      <attribute name="id"/>
      <text/>
    </element>
  </define>
</grammar>`,
		ValidDocs: []string{
			`<root id="123">text</root>`,
		},
		InvalidDocs: []string{
			`<root>text</root>`,
			`<root id="123" name="foo">text</root>`,
		},
	},
	{
		Name: "Optional element",
		SchemaXML: `<?xml version="1.0"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <ref name="root"/>
  </start>
  <define name="root">
    <element name="root">
      <optional>
        <element name="child">
          <text/>
        </element>
      </optional>
    </element>
  </define>
</grammar>`,
		ValidDocs: []string{
			`<root/>`,
			`<root><child>text</child></root>`,
		},
		InvalidDocs: []string{
			`<root><child/><child/></root>`,
		},
	},
	{
		Name: "Choice pattern",
		SchemaXML: `<?xml version="1.0"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <ref name="root"/>
  </start>
  <define name="root">
    <element name="root">
      <choice>
        <element name="a"><text/></element>
        <element name="b"><text/></element>
      </choice>
    </element>
  </define>
</grammar>`,
		ValidDocs: []string{
			`<root><a>text</a></root>`,
			`<root><b>text</b></root>`,
		},
		InvalidDocs: []string{
			`<root><c>text</c></root>`,
		},
	},
	{
		Name: "ZeroOrMore pattern",
		SchemaXML: `<?xml version="1.0"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <ref name="root"/>
  </start>
  <define name="root">
    <element name="root">
      <zeroOrMore>
        <element name="item">
          <text/>
        </element>
      </zeroOrMore>
    </element>
  </define>
</grammar>`,
		ValidDocs: []string{
			`<root/>`,
			`<root><item>1</item></root>`,
			`<root><item>1</item><item>2</item></root>`,
		},
		InvalidDocs: []string{
			`<root><item/></root>`,
		},
	},
	{
		Name: "OneOrMore pattern",
		SchemaXML: `<?xml version="1.0"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <ref name="root"/>
  </start>
  <define name="root">
    <element name="root">
      <oneOrMore>
        <element name="item">
          <text/>
        </element>
      </oneOrMore>
    </element>
  </define>
</grammar>`,
		ValidDocs: []string{
			`<root><item>1</item></root>`,
			`<root><item>1</item><item>2</item></root>`,
		},
		InvalidDocs: []string{
			`<root/>`,
			`<root><item/></root>`,
		},
	},
}

var groupPatternTests = []TestCase{
	{
		Name: "Group - sequential elements",
		SchemaXML: `<?xml version="1.0"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <ref name="root"/>
  </start>
  <define name="root">
    <element name="root">
      <group>
        <element name="first"><text/></element>
        <element name="second"><text/></element>
      </group>
    </element>
  </define>
</grammar>`,
		ValidDocs: []string{
			`<root><first>1</first><second>2</second></root>`,
		},
		InvalidDocs: []string{
			`<root><second>2</second><first>1</first></root>`,
			`<root><first>1</first></root>`,
		},
	},
}

var dataTypeTests = []TestCase{
	{
		Name: "String data type",
		SchemaXML: `<?xml version="1.0"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0"
         xmlns:xsd="http://www.w3.org/2001/XMLSchema-datatypes">
  <start>
    <ref name="root"/>
  </start>
  <define name="root">
    <element name="root">
      <data type="xsd:string"/>
    </element>
  </define>
</grammar>`,
		ValidDocs: []string{
			`<root>any text</root>`,
			`<root>123</root>`,
		},
		InvalidDocs: []string{
			`<root><child/></root>`,
		},
	},
	{
		Name: "Integer data type",
		SchemaXML: `<?xml version="1.0"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0"
         xmlns:xsd="http://www.w3.org/2001/XMLSchema-datatypes">
  <start>
    <ref name="root"/>
  </start>
  <define name="root">
    <element name="root">
      <data type="xsd:integer"/>
    </element>
  </define>
</grammar>`,
		ValidDocs: []string{
			`<root>123</root>`,
			`<root>-456</root>`,
			`<root>0</root>`,
		},
		InvalidDocs: []string{
			`<root>123.45</root>`,
			`<root>abc</root>`,
		},
	},
	{
		Name: "Boolean data type",
		SchemaXML: `<?xml version="1.0"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0"
         xmlns:xsd="http://www.w3.org/2001/XMLSchema-datatypes">
  <start>
    <ref name="root"/>
  </start>
  <define name="root">
    <element name="root">
      <data type="xsd:boolean"/>
    </element>
  </define>
</grammar>`,
		ValidDocs: []string{
			`<root>true</root>`,
			`<root>false</root>`,
			`<root>1</root>`,
			`<root>0</root>`,
		},
		InvalidDocs: []string{
			`<root>yes</root>`,
			`<root>maybe</root>`,
		},
	},
}

var facetTests = []TestCase{
	{
		Name: "minLength facet",
		SchemaXML: `<?xml version="1.0"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0"
         xmlns:xsd="http://www.w3.org/2001/XMLSchema-datatypes">
  <start>
    <ref name="root"/>
  </start>
  <define name="root">
    <element name="root">
      <data type="xsd:string">
        <param name="minLength">3</param>
      </data>
    </element>
  </define>
</grammar>`,
		ValidDocs: []string{
			`<root>abc</root>`,
			`<root>abcd</root>`,
		},
		InvalidDocs: []string{
			`<root>ab</root>`,
		},
	},
	{
		Name: "maxLength facet",
		SchemaXML: `<?xml version="1.0"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0"
         xmlns:xsd="http://www.w3.org/2001/XMLSchema-datatypes">
  <start>
    <ref name="root"/>
  </start>
  <define name="root">
    <element name="root">
      <data type="xsd:string">
        <param name="maxLength">5</param>
      </data>
    </element>
  </define>
</grammar>`,
		ValidDocs: []string{
			`<root>abc</root>`,
			`<root>abcde</root>`,
		},
		InvalidDocs: []string{
			`<root>abcdef</root>`,
		},
	},
	{
		Name: "pattern facet",
		SchemaXML: `<?xml version="1.0"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0"
         xmlns:xsd="http://www.w3.org/2001/XMLSchema-datatypes">
  <start>
    <ref name="root"/>
  </start>
  <define name="root">
    <element name="root">
      <data type="xsd:string">
        <param name="pattern">[0-9]{3}-[0-9]{4}</param>
      </data>
    </element>
  </define>
</grammar>`,
		ValidDocs: []string{
			`<root>123-4567</root>`,
		},
		InvalidDocs: []string{
			`<root>12-345</root>`,
			`<root>abc-defg</root>`,
		},
	},
}

var interleaveTests = []TestCase{
	{
		Name: "Interleave - any order elements",
		SchemaXML: `<?xml version="1.0"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <ref name="root"/>
  </start>
  <define name="root">
    <element name="root">
      <interleave>
        <element name="first"><text/></element>
        <element name="second"><text/></element>
      </interleave>
    </element>
  </define>
</grammar>`,
		ValidDocs: []string{
			`<root><first>1</first><second>2</second></root>`,
			`<root><second>2</second><first>1</first></root>`,
		},
		InvalidDocs: []string{
			`<root><first>1</first></root>`,
			`<root><first>1</first><second>2</second><first>1</first></root>`,
		},
	},
}

var mixedContentTests = []TestCase{
	{
		Name: "Mixed element and text",
		SchemaXML: `<?xml version="1.0"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <ref name="root"/>
  </start>
  <define name="root">
    <element name="root">
      <mixed>
        <zeroOrMore>
          <element name="b"><text/></element>
        </zeroOrMore>
      </mixed>
    </element>
  </define>
</grammar>`,
		ValidDocs: []string{
			`<root>text</root>`,
			`<root><b>bold</b></root>`,
			`<root>text <b>bold</b> text</root>`,
			`<root><b>bold1</b> text <b>bold2</b></root>`,
		},
		InvalidDocs: []string{
			`<root><i>italic</i></root>`,
		},
	},
}

var refDefineTests = []TestCase{
	{
		Name: "Simple ref/define",
		SchemaXML: `<?xml version="1.0"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <ref name="root"/>
  </start>
  <define name="root">
    <element name="root">
      <text/>
    </element>
  </define>
</grammar>`,
		ValidDocs: []string{
			`<root>text</root>`,
		},
		InvalidDocs: []string{
			`<root><child/></root>`,
		},
	},
}

var valueTests = []TestCase{
	{
		Name: "Choice with values",
		SchemaXML: `<?xml version="1.0"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <ref name="root"/>
  </start>
  <define name="root">
    <element name="root">
      <choice>
        <value>red</value>
        <value>green</value>
        <value>blue</value>
      </choice>
    </element>
  </define>
</grammar>`,
		ValidDocs: []string{
			`<root>red</root>`,
			`<root>green</root>`,
			`<root>blue</root>`,
		},
		InvalidDocs: []string{
			`<root>yellow</root>`,
			`<root>RED</root>`,
		},
	},
}

// Test suite runner
func main() {
	suiteName := flag.String("suite", "all", "Test suite (all, basic, group, datatype, facet, interleave, mixed, ref, value)")
	flag.Parse()

	suites := map[string][]TestCase{
		"basic":      basicPatternTests,
		"group":      groupPatternTests,
		"datatype":   dataTypeTests,
		"facet":      facetTests,
		"interleave": interleaveTests,
		"mixed":      mixedContentTests,
		"ref":        refDefineTests,
		"value":      valueTests,
	}

	var testsToRun []TestCase
	if *suiteName == "all" {
		for _, tests := range suites {
			testsToRun = append(testsToRun, tests...)
		}
	} else if tests, ok := suites[*suiteName]; ok {
		testsToRun = tests
	} else {
		fmt.Fprintf(os.Stderr, "Unknown suite: %s\n", *suiteName)
		os.Exit(1)
	}

	passed := 0
	failed := 0

	for _, test := range testsToRun {
		if runTest(&test) {
			passed++
			fmt.Printf("✓ %s\n", test.Name)
		} else {
			failed++
			fmt.Printf("✗ %s\n", test.Name)
		}
	}

	total := passed + failed
	percentage := 0
	if total > 0 {
		percentage = (passed * 100) / total
	}

	fmt.Printf("\n=== RELAX NG Conformance Test Results ===\n")
	fmt.Printf("Passed:  %d/%d (%.0f%%)\n", passed, total, float64(percentage))
	fmt.Printf("Failed:  %d/%d\n", failed, total)

	if failed > 0 {
		os.Exit(1)
	}
}

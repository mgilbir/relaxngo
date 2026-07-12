# RelaxNGo

A production-ready Go library for parsing RELAX NG schemas and validating XML documents against them.

## What is RELAX NG?

[RELAX NG](http://www.relaxng.org/) is a simple yet powerful schema language for XML. It provides an alternative to XML Schema with a simpler syntax and more intuitive validation rules.

## Quick Start

### Install

```bash
go get github.com/mgilbir/relaxngo
```

### Validate an XML Document

```go
import (
    "log"
    "os"

    "github.com/mgilbir/relaxngo/rng"
    "github.com/mgilbir/relaxngo/validator"
)

// Load schema (second argument is the base directory for resolving includes)
schema, err := rng.ParseSchemaFile("schema.rng", ".")
if err != nil {
    log.Fatal(err)
}

// Open the document to validate
doc, err := os.Open("document.xml")
if err != nil {
    log.Fatal(err)
}
defer doc.Close()

// Validate document
v := validator.NewValidator(schema, validator.DefaultOptions())
errors, err := v.Validate(doc)
if err != nil {
    log.Fatal(err) // XML was not well-formed
}

if len(errors) > 0 {
    for _, e := range errors {
        log.Printf("Error at %d:%d: %s", e.Line, e.Column, e.Message)
    }
} else {
    log.Println("Document is valid!")
}
```

### Generate Go Types from Schema

```bash
go run cmd/generate/main.go -schema schema.rng -package myapp > myapp/types.go
```

The generated file contains a struct for each element, plus the embedded
schema and a `Validate()`/`UnmarshalXML()` method per root type that check the
value against the schema. The struct portion looks like:

```go
package myapp

type Book struct {
    XMLName xml.Name `xml:"book"`
    Id      string   `xml:"id,attr"`
    Title   string   `xml:"title"`
    Author  string   `xml:"author"`
}
```

Repeated elements become slices and optional elements/attributes become
pointers with `,omitempty`.

## Features

### RELAX NG Support

- **Schema Parsing**: Load and parse RELAX NG files (.rng)
- **All Major Patterns**: Elements, attributes, text, choice, sequence, optional, repetition
- **Advanced Patterns**: Mixed content, interleave, groups, name classes
- **Data Types**: Full XSD type validation (integer, boolean, float64, string, etc.)
- **Includes**: Support for external schema includes with cycle detection
- **Namespaces**: Full namespace awareness and support

### Code Generation

- **Type-Safe Structs**: Generate Go types directly from schemas
- **Proper Types**: Automatic type mapping (int64, bool, float64, string)
- **Nested Elements**: Recursive type generation for nested elements
- **Cardinality Detection**: Automatic []slice for repeated elements
- **Optional Fields**: Automatic omitempty for optional elements

### XML Validation

- **Full Validation**: Comprehensive RELAX NG validation engine
- **Data Type Checking**: Validate against XSD types
- **Pattern Matching**: Validate elements, attributes, and content patterns
- **Error Location**: Precise line and column information for errors
- **Clear Messages**: Descriptive error messages with context

### XML Parsing

- **Lenient Mode** (default): Ignores unknown fields
- **Strict Mode**: Detects and reports unknown fields
- **Type-Safe**: Use generated types for type-safe parsing
- **Standard Library**: Compatible with Go's `encoding/xml`

### Security & Performance

- **Path Traversal Protection**: Schema includes/externalRefs are confined to the base directory
- **Size Limits**: 50MB cap on strict XML parsing (`StrictParseXML`) and on individual schema resources
- **Cycle Detection**: Prevents infinite loops in schema includes
- **High Performance**: hundreds of thousands of documents/sec for simple schemas (see [BENCHMARK_RESULTS.md](BENCHMARK_RESULTS.md))
- **Zero Dependencies**: Uses only Go standard library

## Usage

### Parse and Validate XML

```go
import (
    "log"
    "os"
    "github.com/mgilbir/relaxngo/rng"
    "github.com/mgilbir/relaxngo/validator"
)

// Load schema
schema, err := rng.ParseSchemaFile("schema.rng", ".")
if err != nil {
    log.Fatal(err)
}

// Validate from file
v := validator.NewValidator(schema, validator.DefaultOptions())
file, err := os.Open("document.xml")
if err != nil {
    log.Fatal(err)
}
defer file.Close()

errors, err := v.Validate(file)
if err != nil {
    log.Fatal(err)
}

for _, e := range errors {
    log.Printf("Error at %d:%d: %s", e.Line, e.Column, e.Message)
}
```

### Detect Unknown Fields (Strict Mode)

```go
import (
    "bytes"
    "encoding/xml"
    "errors"
    "log"

    "github.com/mgilbir/relaxngo/parser"
)

type Person struct {
    XMLName xml.Name `xml:"person"`
    Name    string   `xml:"name"`
}

data := []byte(`<person>
    <name>John</name>
    <age>30</age>
    <email>john@example.com</email>
</person>`)

var p Person
err := parser.StrictParseXML(bytes.NewReader(data), &p)

var unknown *parser.UnknownFieldError
if errors.As(err, &unknown) {
    log.Printf("Unknown elements: %v, unknown attributes: %v",
        unknown.UnknownElements, unknown.UnknownAttributes)
}
```

## Architecture

### Core Packages

| Package | Purpose | Main Type |
|---------|---------|-----------|
| `rng` | Parse RELAX NG schemas | `Grammar` |
| `generator` | Generate Go types | `GenerateTypes` / `GenerateCode` |
| `validator` | Validate XML against schema | `Validator` |
| `parser` | Parse XML documents (detect unknown fields) | `StrictParseXML` |

### Workflow

```
Schema File (.rng)
    ↓
[rng.ParseSchemaFile]
    ↓
Grammar Structure
    ↓
   ├─ [Code Generator] → Go Types (.go)
   ├─ [Validator] → Validation API
   └─ [Use Generated Types with xml.Unmarshal]
```

## Specification Compliance

**Coverage: 100%**

The library passes the complete official RELAX NG test suite:
- ✅ 213 SyntaxError tests
- ✅ 272 Valid document tests
- ✅ 257 Invalid document tests

## Performance

See [BENCHMARK_RESULTS.md](BENCHMARK_RESULTS.md) for full methodology and numbers.
Representative figures on the reference machine:

| Operation | Performance |
|-----------|------------|
| Schema Parsing | 1-50 µs/op |
| XML Validation | ~2-20 µs/op (simple → complex schemas) |
| Code Generation | <1 sec typical |

Validation is safe to call concurrently on a shared `Validator`.

## Testing

### Run Tests

```bash
make test-all         # Run all tests (default)
make test              # Run Go test suite
make test-official    # Run official RELAX NG test suite
make test-generator   # Run generator-specific tests
```

### Test Results

- **Unit Tests**: passing (`go test ./...`)
- **Official Suite**: 742/742 tests (100%) ✅

## Contributing

Contributions welcome! Please ensure:

1. All tests pass: `make test-all`
2. Code is formatted: `gofmt`
3. New features include tests
4. Linter passes: `make lint`

## Reporting Issues

Found a bug? Please report on GitHub with:

- Go version (`go version`)
- RelaxNGo version
- Schema and document samples (if possible)
- Expected vs actual behavior
- Error message and stack trace

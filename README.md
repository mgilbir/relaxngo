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
    "github.com/mgilbir/relaxngo/rng"
    "github.com/mgilbir/relaxngo/validator"
)

// Load schema
schema, err := rng.ParseFile("schema.rng")
if err != nil {
    log.Fatal(err)
}

// Validate document
v := validator.NewValidator(schema, validator.DefaultOptions())
errors, err := v.Validate(file)

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

Example output:

```go
package myapp

import "encoding/xml"

type Book struct {
    XMLName xml.Name `xml:"book"`
    Title   string   `xml:"title"`
    Author  string   `xml:"author"`
    Year    int64    `xml:"year"`
}

type Library struct {
    XMLName xml.Name `xml:"library"`
    Books   []Book   `xml:"book"`
}
```

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

- **Path Traversal Protection**: Validates schema include paths
- **DoS Prevention**: 50MB default size limit on documents
- **Cycle Detection**: Prevents infinite loops in schema includes
- **High Performance**: 240k-300k documents/sec validation speed
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
schema, err := rng.ParseFile("schema.rng")
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
    "github.com/mgilbir/relaxngo/parser"
    "log"
)

data := []byte(`<person>
    <name>John</name>
    <age>30</age>
    <email>john@example.com</email>
</person>`)

p := parser.NewStrictParser()
_, unknownFields, err := p.ParseXML(data)

if len(unknownFields) > 0 {
    log.Printf("Unknown fields: %v", unknownFields)
}
```

## Architecture

### Core Packages

| Package | Purpose | Main Type |
|---------|---------|-----------|
| `rng` | Parse RELAX NG schemas | `Grammar` |
| `generator` | Generate Go types | `Generator` |
| `validator` | Validate XML against schema | `Validator` |
| `parser` | Parse XML documents | `StrictParser` |

### Workflow

```
Schema File (.rng)
    ↓
[rng.ParseFile]
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

| Operation | Performance |
|-----------|------------|
| Schema Parsing | 1-50 µs/op |
| XML Validation | 240k-300k docs/sec |
| Code Generation | <1 sec typical |

**Tested Scenarios:**
- ✅ Documents up to 100MB
- ✅ Deep nesting (100+ levels)
- ✅ Many elements (10,000+)
- ✅ Concurrent validation

## Testing

### Run Tests

```bash
make test-all         # Run all tests (default)
make test              # Run Go test suite
make test-official    # Run official RELAX NG test suite
make test-generator   # Run generator-specific tests
```

### Test Results

- **Unit Tests**: 136/136 passing ✅
- **Fuzz Tests**: 4.9M+ executions, 0 crashes ✅
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

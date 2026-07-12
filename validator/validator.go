package validator

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"

	"github.com/mgilbir/relaxngo/rng"
)

// XML Schema data type and facet constants shared by the datatype helpers.
const (
	dataTypeString           = "string"
	dataTypeNormalizedString = "normalizedString"
	dataTypeToken            = "token"

	facetMinInclusive = "minInclusive"
	facetMaxInclusive = "maxInclusive"
	facetMinExclusive = "minExclusive"
	facetMaxExclusive = "maxExclusive"
)

// ValidationError represents a single validation error with context.
type ValidationError struct {
	Path     string   // XPath-like path to the error location
	Element  string   // Element name where error occurred
	Line     int      // Line number (if available)
	Column   int      // Column number (if available)
	Expected []string // What was expected
	Found    string   // What was actually found
	Message  string   // Human-readable error message
}

func (e *ValidationError) Error() string {
	if e.Line > 0 {
		if e.Column > 0 {
			return fmt.Sprintf("%s at line %d column %d: %s", e.Path, e.Line, e.Column, e.Message)
		}
		return fmt.Sprintf("%s at line %d: %s", e.Path, e.Line, e.Message)
	}
	return fmt.Sprintf("%s: %s", e.Path, e.Message)
}

// ValidationOptions configures validation behavior.
//
// The derivative engine reports a single located failure per document, so there
// is no multi-error or fail-fast knob. The options that remain bound resource
// use, guarding against untrusted input.
type ValidationOptions struct {
	// MaxDepth caps element nesting depth; a deeper document is rejected before
	// the tree is built. Guards against stack exhaustion from pathological input
	// like <a><a><a>… repeated millions of times. 0 means unlimited.
	MaxDepth int
	// MaxDocumentBytes caps the document size read into memory. A larger document
	// is rejected without being fully buffered. 0 means unlimited.
	MaxDocumentBytes int64
}

// Default resource limits. Both are generous enough for any realistic document
// while still bounding what untrusted input can consume.
const (
	defaultMaxDepth         = 5000
	defaultMaxDocumentBytes = 50 << 20 // 50 MiB
)

// DefaultOptions returns sensible default validation options.
func DefaultOptions() ValidationOptions {
	return ValidationOptions{
		MaxDepth:         defaultMaxDepth,
		MaxDocumentBytes: defaultMaxDocumentBytes,
	}
}

// Validator validates XML documents against a RELAX NG grammar.
type Validator struct {
	grammar *rng.Grammar
	options ValidationOptions
	deriv   *derivEngine // derivative engine; nil when the grammar uses a construct it cannot translate
}

// NewValidator creates a validator from a parsed RELAX NG grammar.
func NewValidator(grammar *rng.Grammar, options ValidationOptions) *Validator {
	return &Validator{
		grammar: grammar,
		options: options,
		deriv:   buildDerivEngine(grammar),
	}
}

// Validate validates an XML document and returns all validation errors.
//
// Validation uses the derivative algorithm (see deriv.go). If the schema uses a
// construct the builder cannot translate it returns an error rather than a
// (possibly wrong) result.
func (v *Validator) Validate(r io.Reader) ([]ValidationError, error) {
	if limit := v.options.MaxDocumentBytes; limit > 0 {
		// Read one byte past the limit so we can tell "exactly at the limit"
		// (allowed) from "over the limit" (rejected) without buffering the rest.
		r = io.LimitReader(r, limit+1)
		data, err := io.ReadAll(r)
		if err != nil {
			return nil, err
		}
		if int64(len(data)) > limit {
			return nil, fmt.Errorf("validator: document exceeds MaxDocumentBytes limit of %d bytes", limit)
		}
		return v.validateData(data)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return v.validateData(data)
}

func (v *Validator) validateData(data []byte) ([]ValidationError, error) {
	if v.deriv == nil {
		return nil, fmt.Errorf("validator: schema uses a construct that is not supported")
	}
	return v.validateDerivative(data)
}

// validationContext carries the options needed by the datatype/value/facet
// helpers below, which the derivative engine reuses for leaf-level checks.
type validationContext struct {
	options ValidationOptions
}

// valueMatches reports whether text matches a <value> pattern, applying the
// whitespace processing implied by the value's data type.
func (ctx *validationContext) valueMatches(val rng.Value, text string) bool {
	// Value matching depends on the type attribute
	// - type="string": exact match (no whitespace normalization)
	// - type="token" or no type (default): whitespace normalized per XML Schema
	valueType := val.Type
	if valueType == "" {
		valueType = dataTypeToken // default type
	}

	if valueType == dataTypeString {
		// For "string" type, compare exactly without normalization
		return text == val.Value
	}

	// For "token" and other types, normalize whitespace per XML Schema spec
	textNorm := normalizeTokenValue(text)
	valNorm := normalizeTokenValue(val.Value)
	return textNorm == valNorm
}

func (ctx *validationContext) validateDataType(typeName, value string) bool {
	// For string types, don't trim - whitespace is significant
	// For other types, trim for proper validation
	trimmedValue := value
	if typeName != dataTypeString && typeName != dataTypeNormalizedString {
		trimmedValue = strings.TrimSpace(value)
	}

	switch typeName {
	case dataTypeString, dataTypeToken, dataTypeNormalizedString:
		// These accept any content; token/normalizedString are already
		// whitespace-processed upstream.
		return true
	default:
		return validateXSDType(typeName, trimmedValue)
	}
}

// validateDataTypeWithFacets validates a value against a data type and facets.
// Facets include constraints like minLength, maxLength, pattern, minInclusive, maxInclusive.
func (ctx *validationContext) validateDataTypeWithFacets(typeName, value string, params []rng.Param) bool {
	// For string types, don't trim - whitespace is part of the value
	// For other types, trim for proper validation
	trimmedValue := value
	if typeName != "string" && typeName != "normalizedString" {
		trimmedValue = strings.TrimSpace(value)
	}

	// First validate the base type (using trimmed value for non-string types)
	if !ctx.validateDataType(typeName, trimmedValue) {
		return false
	}

	// Then validate facets (use original value for string types to preserve whitespace)
	// For numeric types, use trimmed value
	valueForFacets := value
	if typeName != "string" && typeName != "normalizedString" {
		valueForFacets = trimmedValue
	}

	for _, param := range params {
		switch param.Name {
		case "minLength":
			if minLen := ctx.parseInt(param.Value); minLen >= 0 {
				if len(valueForFacets) < minLen {
					return false
				}
			}
		case "maxLength":
			if maxLen := ctx.parseInt(param.Value); maxLen >= 0 {
				if len(valueForFacets) > maxLen {
					return false
				}
			}
		case "pattern":
			if !ctx.matchPattern(valueForFacets, param.Value) {
				return false
			}
		case facetMinInclusive:
			if !ctx.validateNumericConstraint(typeName, trimmedValue, param.Value, facetMinInclusive) {
				return false
			}
		case facetMaxInclusive:
			if !ctx.validateNumericConstraint(typeName, trimmedValue, param.Value, facetMaxInclusive) {
				return false
			}
		case facetMinExclusive:
			if !ctx.validateNumericConstraint(typeName, trimmedValue, param.Value, facetMinExclusive) {
				return false
			}
		case facetMaxExclusive:
			if !ctx.validateNumericConstraint(typeName, trimmedValue, param.Value, facetMaxExclusive) {
				return false
			}
		case "fractionDigits":
			if !ctx.validateFractionDigits(trimmedValue, param.Value) {
				return false
			}
		}
	}

	return true
}

// parseInt safely parses an integer, returns -1 if invalid
func (ctx *validationContext) parseInt(s string) int {
	var result int64
	_, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &result)
	if err != nil {
		return -1
	}
	return int(result)
}

// matchPattern checks if value matches a regex pattern
func (ctx *validationContext) matchPattern(value, pattern string) bool {
	// Simple regex matching with safeguards against catastrophic backtracking
	// Use a limit on pattern complexity
	if len(pattern) > 1000 {
		// Pattern too complex, reject to prevent DoS
		return false
	}

	// Go's regexp package uses RE2 (linear-time automata), so it is not
	// vulnerable to catastrophic backtracking; the length limit above is extra
	// safety against pathological compile times.
	//
	// XSD pattern facets are anchored to the entire lexical value (an implicit
	// ^(?:...)$), unlike Go's default substring semantics. Anchor here so that
	// e.g. pattern="[0-9]{3}" rejects "abc123def".
	regex := cachedRegex("^(?:" + pattern + ")$")
	if regex == nil {
		return false
	}
	return regex.MatchString(value)
}

// regexCache memoizes compiled facet patterns across validations. It is a
// process-global shared by every Validator, so all access must go through its
// mutex — concurrent map access is otherwise a data race and a runtime panic,
// which would break the documented support for concurrent validation.
var (
	regexCacheMu sync.RWMutex
	regexCache   = make(map[string]*regexp.Regexp)
)

func cachedRegex(pattern string) *regexp.Regexp {
	regexCacheMu.RLock()
	cached, ok := regexCache[pattern]
	regexCacheMu.RUnlock()
	if ok {
		return cached
	}

	// Try to compile regex
	regex, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}

	// Cache only if not too many patterns (prevent unbounded memory growth).
	regexCacheMu.Lock()
	if len(regexCache) < 100 {
		regexCache[pattern] = regex
	}
	regexCacheMu.Unlock()

	return regex
}

// validateNumericConstraint checks minInclusive, maxInclusive, minExclusive, maxExclusive
func (ctx *validationContext) validateNumericConstraint(typeName, value, constraint, constraintType string) bool {
	switch typeName {
	case "integer", "int", "long", "short", "byte":
		var v, c int64
		_, errV := fmt.Sscanf(strings.TrimSpace(value), "%d", &v)
		_, errC := fmt.Sscanf(strings.TrimSpace(constraint), "%d", &c)
		if errV != nil || errC != nil {
			return true // Can't parse, don't fail
		}

		switch constraintType {
		case "minInclusive":
			return v >= c
		case "maxInclusive":
			return v <= c
		case "minExclusive":
			return v > c
		case "maxExclusive":
			return v < c
		}
	case "decimal", "double", "float":
		var v, c float64
		_, errV := fmt.Sscanf(strings.TrimSpace(value), "%f", &v)
		_, errC := fmt.Sscanf(strings.TrimSpace(constraint), "%f", &c)
		if errV != nil || errC != nil {
			return true // Can't parse, don't fail
		}

		switch constraintType {
		case "minInclusive":
			return v >= c
		case "maxInclusive":
			return v <= c
		case "minExclusive":
			return v > c
		case "maxExclusive":
			return v < c
		}
	}

	return true
}

// validateFractionDigits checks that decimal has at most n fractional digits
func (ctx *validationContext) validateFractionDigits(value, maxDigitsStr string) bool {
	maxDigits := ctx.parseInt(maxDigitsStr)
	if maxDigits < 0 {
		return true
	}

	value = strings.TrimSpace(value)
	parts := strings.Split(value, ".")
	if len(parts) > 2 {
		return false // Invalid decimal format
	}

	if len(parts) == 1 {
		return true // Integer part only, no fractional digits
	}

	// Count fractional digits (skip trailing zeros for comparison)
	fracPart := strings.TrimRight(parts[1], "0")
	return len(fracPart) <= maxDigits
}

// normalizeTokenValue applies XML Schema token whitespace processing: tabs,
// newlines and carriage returns become spaces, runs of spaces collapse, and
// leading/trailing spaces are trimmed.
func normalizeTokenValue(s string) string {
	// Replace all whitespace chars (tab, newline, CR) with space
	normalized := strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return ' '
		}
		return r
	}, s)

	// Collapse consecutive spaces
	for strings.Contains(normalized, "  ") {
		normalized = strings.ReplaceAll(normalized, "  ", " ")
	}

	// Trim leading and trailing spaces
	return strings.TrimSpace(normalized)
}

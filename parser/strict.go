package parser

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
)

// UnknownFieldError represents an error when unknown fields are found during parsing.
type UnknownFieldError struct {
	UnknownElements   []string
	UnknownAttributes []string
}

func (e *UnknownFieldError) Error() string {
	var parts []string
	if len(e.UnknownElements) > 0 {
		parts = append(parts, fmt.Sprintf("unknown elements: %s", strings.Join(e.UnknownElements, ", ")))
	}
	if len(e.UnknownAttributes) > 0 {
		parts = append(parts, fmt.Sprintf("unknown attributes: %s", strings.Join(e.UnknownAttributes, ", ")))
	}
	return strings.Join(parts, "; ")
}

const defaultMaxBytes = 50 * 1024 * 1024 // 50MB

// StrictParseXML parses XML with strict validation - it detects unknown fields.
// Returns an UnknownFieldError if extra elements or attributes are found.
// Input is limited to 50MB to prevent memory exhaustion attacks.
func StrictParseXML(r io.Reader, v interface{}) error {
	return StrictParseXMLWithLimit(r, v, defaultMaxBytes)
}

// StrictParseXMLWithLimit parses XML with strict validation and a size limit.
// Returns an error if the input exceeds maxBytes.
func StrictParseXMLWithLimit(r io.Reader, v interface{}, maxBytes int64) error {
	limitedReader := io.LimitReader(r, maxBytes+1)
	data, err := io.ReadAll(limitedReader)
	if err != nil {
		return err
	}

	if int64(len(data)) > maxBytes {
		return fmt.Errorf("XML input exceeds maximum size of %d bytes", maxBytes)
	}

	if err := xml.Unmarshal(data, v); err != nil {
		return err
	}

	unknownFields, err := detectUnknownFields(bytes.NewReader(data), v)
	if err != nil {
		return err
	}

	if len(unknownFields.UnknownElements) > 0 || len(unknownFields.UnknownAttributes) > 0 {
		return unknownFields
	}

	return nil
}

// detectUnknownFields walks the XML and compares against the struct definition.
func detectUnknownFields(r io.Reader, v interface{}) (*UnknownFieldError, error) {
	decoder := xml.NewDecoder(r)
	result := &UnknownFieldError{
		UnknownElements:   make([]string, 0),
		UnknownAttributes: make([]string, 0),
	}
	knownFields := extractKnownFields(reflect.TypeOf(v))

	elementStack := make([]string, 0)
	seenUnknown := make(map[string]bool)

	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		if startElem, ok := tok.(xml.StartElement); ok {
			processStartElement(&startElem, &elementStack, knownFields, result, seenUnknown)
		} else if _, ok := tok.(xml.EndElement); ok {
			if len(elementStack) > 0 {
				elementStack = elementStack[:len(elementStack)-1]
			}
		}
	}

	sort.Strings(result.UnknownElements)
	sort.Strings(result.UnknownAttributes)

	return result, nil
}

func processStartElement(startElem *xml.StartElement, elementStack *[]string, knownFields map[string]fieldSet,
	result *UnknownFieldError, seenUnknown map[string]bool) {
	currentPath := append(*elementStack, startElem.Name.Local)
	pathStr := strings.Join(currentPath, ".")

	checkUnknownElement(currentPath, pathStr, startElem.Name.Local, *elementStack, knownFields, result, seenUnknown)
	checkUnknownAttributes(pathStr, startElem.Attr, knownFields, result, seenUnknown)

	*elementStack = currentPath
}

func checkUnknownElement(_ []string, pathStr, elemName string, elementStack []string,
	knownFields map[string]fieldSet, result *UnknownFieldError, seenUnknown map[string]bool) {
	if len(elementStack) == 0 {
		return
	}

	parentPath := strings.Join(elementStack, ".")
	parentFields, ok := getFieldSet(parentPath, knownFields)
	if ok && !parentFields.elements[elemName] && !seenUnknown[pathStr] {
		result.UnknownElements = append(result.UnknownElements, pathStr)
		seenUnknown[pathStr] = true
	}
}

func checkUnknownAttributes(pathStr string, attrs []xml.Attr, knownFields map[string]fieldSet,
	result *UnknownFieldError, seenUnknown map[string]bool) {
	currentFields, hasCurrentFields := getFieldSet(pathStr, knownFields)
	if !hasCurrentFields {
		return
	}

	for _, attr := range attrs {
		attrKey := attr.Name.Local
		attrPath := pathStr + "@" + attrKey
		if !currentFields.attributes[attrKey] && !seenUnknown[attrPath] {
			result.UnknownAttributes = append(result.UnknownAttributes, attrPath)
			seenUnknown[attrPath] = true
		}
	}
}

// getFieldSet retrieves a field set with fallback to "root" prefix for structs without XMLName.
func getFieldSet(path string, known map[string]fieldSet) (fieldSet, bool) {
	if fs, ok := known[path]; ok {
		return fs, true
	}

	if path == "" {
		if fs, ok := known["root"]; ok {
			return fs, true
		}
		return fieldSet{}, false
	}

	if i := strings.Index(path, "."); i >= 0 {
		rootPath := "root" + path[i:]
		if fs, ok := known[rootPath]; ok {
			return fs, true
		}
	} else {
		if fs, ok := known["root"]; ok {
			return fs, true
		}
	}

	return fieldSet{}, false
}

type fieldSet struct {
	elements   map[string]bool
	attributes map[string]bool
}

// extractKnownFields uses reflection to build a map of known XML elements and attributes.
func extractKnownFields(t reflect.Type) map[string]fieldSet {
	result := make(map[string]fieldSet)
	extractKnownFieldsRecursive(t, "root", result)
	return result
}

func extractKnownFieldsRecursive(t reflect.Type, path string, result map[string]fieldSet) {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return
	}

	if _, exists := result[path]; !exists {
		result[path] = fieldSet{
			elements:   make(map[string]bool),
			attributes: make(map[string]bool),
		}
	}

	fields := result[path]
	var rootElementName string
	var actualPath string

	processStructFields(t, path, &rootElementName, &actualPath, fields, result)

	if rootElementName != "" && path == "root" {
		actualPath = rootElementName
		result[rootElementName] = fields
		delete(result, "root")
	}
}

func processStructFields(t reflect.Type, path string, rootElementName *string, actualPath *string,
	fields fieldSet, result map[string]fieldSet) {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("xml")

		if tag == "" || tag == "-" {
			continue
		}

		if handleXMLNameField(field, tag, rootElementName) {
			continue
		}

		processFieldTag(field, tag, path, actualPath, *rootElementName, fields, result)
	}
}

func handleXMLNameField(field reflect.StructField, tag string, rootElementName *string) bool {
	if field.Name != "XMLName" || field.Type != reflect.TypeOf(xml.Name{}) {
		return false
	}

	parts := strings.Split(tag, ",")
	name := parts[0]
	tokens := strings.Fields(name)
	if len(tokens) > 0 {
		*rootElementName = tokens[len(tokens)-1]
	} else {
		*rootElementName = name
	}
	return true
}

func processFieldTag(field reflect.StructField, tag, path string, actualPath *string, rootElementName string,
	fields fieldSet, result map[string]fieldSet) {
	parts := strings.Split(tag, ",")
	name := parts[0]

	if name == "" {
		name = field.Name
	}

	isAttr, isCharData := parseFieldTagParts(parts[1:])

	if isCharData {
		return
	}

	if isAttr {
		fields.attributes[name] = true
	} else {
		processElementField(field, name, path, *actualPath, rootElementName, fields, result)
	}
}

func parseFieldTagParts(parts []string) (bool, bool) {
	isAttr := false
	isCharData := false
	for _, part := range parts {
		if part == "attr" {
			isAttr = true
		}
		if part == "chardata" || part == "innerxml" {
			isCharData = true
		}
	}
	return isAttr, isCharData
}

// processElementField handles processing of element fields during known field extraction
func processElementField(field reflect.StructField, name, path, actualPath, rootElementName string, fields fieldSet, result map[string]fieldSet) {
	fields.elements[name] = true

	fieldType := field.Type
	if fieldType.Kind() == reflect.Slice {
		fieldType = fieldType.Elem()
	}
	if fieldType.Kind() == reflect.Ptr {
		fieldType = fieldType.Elem()
	}

	if fieldType.Kind() == reflect.Struct && fieldType != reflect.TypeOf(xml.Name{}) {
		var childPath string
		switch {
		case actualPath != "":
			childPath = actualPath + "." + name
		case path == "root" && rootElementName != "":
			childPath = rootElementName + "." + name
		default:
			childPath = path + "." + name
		}
		extractKnownFieldsRecursive(fieldType, childPath, result)
	}
}

// ValidateXML checks if an XML document conforms to the expected structure
// without fully unmarshaling it. This can detect extra/missing elements.
func ValidateXML(r io.Reader, _ interface{}) error {
	decoder := xml.NewDecoder(r)

	depth := 0
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("XML validation error: %w", err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
			if depth < 0 {
				return fmt.Errorf("mismatched closing tag: %s", t.Name.Local)
			}
		}
	}

	if depth != 0 {
		return fmt.Errorf("unclosed elements (depth: %d)", depth)
	}

	return nil
}

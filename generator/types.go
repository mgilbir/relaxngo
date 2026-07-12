// Package generator generates Go code from RelaxNG schemas.
package generator

import (
	"fmt"
	"strings"

	"github.com/mgilbir/relaxngo/rng"
)

const stringType = "string"

// TypeInfo represents metadata about a generated Go type from a RELAX NG definition.
type TypeInfo struct {
	Name       string
	Fields     []FieldInfo
	IsRootType bool
}

// FieldInfo represents metadata about a field in a generated Go struct.
type FieldInfo struct {
	Name     string
	Type     string
	XMLTag   string
	Optional bool
}

// buildTypeFromElement builds a TypeInfo from an element.
func buildTypeFromElement(elem *rng.Element, typeName string, isRootType bool, nestedElements map[string]*rng.Element) TypeInfo {
	typeInfo := TypeInfo{
		Name:       typeName,
		Fields:     make([]FieldInfo, 0),
		IsRootType: isRootType,
	}
	seenFields := make(map[string]bool)

	addElementNameField(&typeInfo, elem, seenFields)
	addTextValueField(&typeInfo, elem, seenFields)
	addDirectAttributes(&typeInfo, elem, seenFields)
	addDirectElements(&typeInfo, elem, seenFields)
	addOptionalFields(&typeInfo, elem, seenFields)
	addRefFields(&typeInfo, elem, seenFields)
	addOneOrMoreFields(&typeInfo, elem, seenFields)
	addGroupFields(&typeInfo, elem, seenFields)
	addZeroOrMoreFields(&typeInfo, elem, seenFields)
	addMixedField(&typeInfo, elem, seenFields)
	addDataValueField(&typeInfo, elem, seenFields)
	addListValueField(&typeInfo, elem, seenFields)

	// Collect nested elements from this element
	collectNestedElements(elem, nestedElements)

	return typeInfo
}

// buildInterleaveType builds a TypeInfo for defines with interleave patterns.
func buildInterleaveType(def *rng.Define, defineTypeName string, isRootType bool, nestedElements map[string]*rng.Element) TypeInfo {
	typeInfo := TypeInfo{
		Name:       defineTypeName,
		Fields:     make([]FieldInfo, 0),
		IsRootType: isRootType,
	}
	seenFields := make(map[string]bool)

	// Collect all elements from all interleaves in the define
	elementCounts := make(map[string]int)
	allElements := make([]*rng.Element, 0)

	for _, interleave := range def.Interleave {
		for j := range interleave.Elements {
			allElements = append(allElements, &interleave.Elements[j])
			elementCounts[interleave.Elements[j].Name]++
		}
	}

	// Add all elements as fields
	for _, e := range allElements {
		fieldName := toGoFieldName(e.Name)
		if !seenFields[fieldName] {
			fieldType := stringType
			if e.Text == nil {
				fieldType = toGoTypeName(e.Name)
			}

			// If multiple with same name, use slice
			if elementCounts[e.Name] > 1 {
				fieldType = "[]" + fieldType
			}

			typeInfo.Fields = append(typeInfo.Fields, FieldInfo{
				Name:   fieldName,
				Type:   fieldType,
				XMLTag: fmt.Sprintf("`xml:\"%s\"`", e.Name),
			})
			seenFields[fieldName] = true
		}

		// Collect the element itself as a nested element (if it doesn't have Text)
		if e.Text == nil && e.Name != "" && nestedElements[e.Name] == nil {
			nestedElements[e.Name] = e
		}

		// Collect nested elements within this element
		collectNestedElements(e, nestedElements)
	}

	return typeInfo
}

// buildMultiElementType builds a TypeInfo for defines with multiple direct element children.
func buildMultiElementType(def *rng.Define, defineTypeName string, isRootType bool, nestedElements map[string]*rng.Element) TypeInfo {
	typeInfo := TypeInfo{
		Name:       defineTypeName,
		Fields:     make([]FieldInfo, 0),
		IsRootType: isRootType,
	}
	seenFields := make(map[string]bool)

	// Count element occurrences
	elementCounts := make(map[string]int)
	for _, e := range def.Elements {
		elementCounts[e.Name]++
	}

	// Add all elements as fields
	for _, e := range def.Elements {
		fieldName := toGoFieldName(e.Name)
		if !seenFields[fieldName] {
			fieldType := stringType
			if e.Text == nil {
				fieldType = toGoTypeName(e.Name)
			}

			// If multiple with same name, use slice
			if elementCounts[e.Name] > 1 {
				fieldType = "[]" + fieldType
			}

			typeInfo.Fields = append(typeInfo.Fields, FieldInfo{
				Name:   fieldName,
				Type:   fieldType,
				XMLTag: fmt.Sprintf("`xml:\"%s\"`", e.Name),
			})
			seenFields[fieldName] = true
		}

		// Collect nested elements from this element
		collectNestedElements(&e, nestedElements)
	}

	return typeInfo
}

// processDefines processes all defines from a grammar and builds types.
func processDefines(grammar *rng.Grammar, rootDefineName string, types *[]TypeInfo, seenTypeNames map[string]bool, nestedElements map[string]*rng.Element) {
	for idx := range grammar.Defines {
		def := &grammar.Defines[idx]
		defineTypeName := toGoTypeName(def.Name)

		// Handle defines with Interleave patterns (from merge of multiple defines with combine="interleave")
		if len(def.Interleave) > 0 && def.FirstElement() == nil {
			if !seenTypeNames[defineTypeName] {
				typeInfo := buildInterleaveType(def, defineTypeName, def.Name == rootDefineName, nestedElements)
				*types = append(*types, typeInfo)
				seenTypeNames[defineTypeName] = true
			}
			continue
		}

		if def.FirstElement() == nil {
			continue
		}

		elem := def.FirstElement()
		elemTypeName := toGoTypeName(elem.Name)

		// If the define has multiple direct element children, create a wrapper type for the define
		if len(def.Elements) > 1 {
			if !seenTypeNames[defineTypeName] {
				typeInfo := buildMultiElementType(def, defineTypeName, def.Name == rootDefineName, nestedElements)
				*types = append(*types, typeInfo)
				seenTypeNames[defineTypeName] = true
			}

			// Still generate a type for the first element if it's not already there
			if !seenTypeNames[elemTypeName] {
				typeInfo := buildTypeFromElement(elem, elemTypeName, false, nestedElements)
				*types = append(*types, typeInfo)
				seenTypeNames[elemTypeName] = true
			}
		} else {
			// Single element or other patterns - use element name for type
			if seenTypeNames[elemTypeName] {
				continue
			}

			typeInfo := buildTypeFromElement(elem, elemTypeName, def.Name == rootDefineName, nestedElements)
			*types = append(*types, typeInfo)
			seenTypeNames[elemTypeName] = true
		}
	}
}

// processNestedElements generates types for all collected nested elements.
func processNestedElements(nestedElements map[string]*rng.Element, types *[]TypeInfo, seenTypeNames map[string]bool) {
	for elemName, elem := range nestedElements {
		typeName := toGoTypeName(elemName)

		// Skip if already generated
		if seenTypeNames[typeName] {
			continue
		}

		typeInfo := buildTypeFromElement(elem, typeName, false, nestedElements)
		*types = append(*types, typeInfo)
		seenTypeNames[typeName] = true
	}
}

// GenerateTypes creates TypeInfo structures from a RELAX NG grammar.
// It processes schema definitions and converts them to Go type metadata.
func GenerateTypes(grammar *rng.Grammar) ([]TypeInfo, error) {
	types := make([]TypeInfo, 0)
	seenTypeNames := make(map[string]bool)
	nestedElements := make(map[string]*rng.Element) // Collect nested elements

	// Determine root define name from start element
	var rootDefineName string
	if grammar.Start.Ref != nil {
		rootDefineName = grammar.Start.Ref.Name
	} else if grammar.Start.Element != nil {
		// Direct element in start - use element name as root
		rootDefineName = grammar.Start.Element.Name
	}

	// If start has a direct inline element, generate a type for it
	if grammar.Start.Element != nil {
		elem := grammar.Start.Element
		typeName := toGoTypeName(elem.Name)

		if !seenTypeNames[typeName] {
			typeInfo := buildTypeFromElement(elem, typeName, true, nestedElements)
			types = append(types, typeInfo)
			seenTypeNames[typeName] = true
		}
	}

	processDefines(grammar, rootDefineName, &types, seenTypeNames, nestedElements)
	processNestedElements(nestedElements, &types, seenTypeNames)

	return types, nil
}

// collectNestedElements recursively collects all nested elements in the schema
func collectNestedElements(elem *rng.Element, collected map[string]*rng.Element) {
	// Collect from direct nested elements. addDirectElements emits a field
	// typed after the child element when it has nested content, so that child
	// type must be generated too — otherwise the output references an undefined
	// type and does not compile.
	for i := range elem.Elements {
		subElem := &elem.Elements[i]
		if subElem.Name != "" && collected[subElem.Name] == nil {
			collected[subElem.Name] = subElem
			collectNestedElements(subElem, collected)
		}
	}

	// Collect from groups
	for _, group := range elem.Group {
		for _, subElem := range group.Elements {
			if subElem.Name != "" && collected[subElem.Name] == nil {
				collected[subElem.Name] = &subElem
				collectNestedElements(&subElem, collected)
			}
		}
	}

	// Collect from oneOrMore
	for _, oneOrMore := range elem.OneOrMore {
		for _, subElem := range oneOrMore.Element {
			if subElem.Name != "" && collected[subElem.Name] == nil {
				collected[subElem.Name] = &subElem
				collectNestedElements(&subElem, collected)
			}
		}
	}

	// Collect from zeroOrMore
	for _, zeroOrMore := range elem.ZeroOrMore {
		for _, subElem := range zeroOrMore.Element {
			if subElem.Name != "" && collected[subElem.Name] == nil {
				collected[subElem.Name] = &subElem
				collectNestedElements(&subElem, collected)
			}
		}
	}

	// Collect from optional
	for _, opt := range elem.Optional {
		for _, subElem := range opt.Elements {
			if subElem.Name != "" && collected[subElem.Name] == nil {
				collected[subElem.Name] = &subElem
				collectNestedElements(&subElem, collected)
			}
		}
	}
}

func addElementNameField(typeInfo *TypeInfo, elem *rng.Element, seenFields map[string]bool) {
	if elem.Name != "" {
		typeInfo.Fields = append(typeInfo.Fields, FieldInfo{
			Name:   "XMLName",
			Type:   "xml.Name",
			XMLTag: fmt.Sprintf("`xml:\"%s\"`", elem.Name),
		})
		seenFields["XMLName"] = true
	}
}

func addTextValueField(typeInfo *TypeInfo, elem *rng.Element, seenFields map[string]bool) {
	if elem.Text != nil {
		fieldName := "Value"
		if !seenFields[fieldName] {
			typeInfo.Fields = append(typeInfo.Fields, FieldInfo{
				Name:   fieldName,
				Type:   stringType,
				XMLTag: "`xml:\",chardata\"`",
			})
			seenFields[fieldName] = true
		}
	}
}

func addDirectAttributes(typeInfo *TypeInfo, elem *rng.Element, seenFields map[string]bool) {
	for _, attr := range elem.Attributes {
		fieldType := getAttributeFieldType(&attr, elem)
		fieldName := toGoFieldName(attr.Name)
		if !seenFields[fieldName] {
			typeInfo.Fields = append(typeInfo.Fields, FieldInfo{
				Name:   fieldName,
				Type:   fieldType,
				XMLTag: fmt.Sprintf("`xml:\"%s,attr\"`", attr.Name),
			})
			seenFields[fieldName] = true
		}
	}
}

func addDirectElements(typeInfo *TypeInfo, elem *rng.Element, seenFields map[string]bool) {
	// Skip if no direct elements
	if len(elem.Elements) == 0 {
		return
	}

	// Count occurrences of each element name
	elementCounts := make(map[string]int)
	for _, subElem := range elem.Elements {
		if subElem.Name != "" {
			elementCounts[subElem.Name]++
		}
	}

	for _, subElem := range elem.Elements {
		// Skip elements with empty names (patterns without explicit names)
		if subElem.Name == "" {
			continue
		}

		fieldName := toGoFieldName(subElem.Name)
		if !seenFields[fieldName] {
			var fieldType string

			// Determine field type based on element content
			switch {
			case subElem.Text != nil:
				// Simple text content
				fieldType = stringType
			case hasNestedContent(&subElem):
				// Complex element with nested content - reference the element type
				fieldType = toGoTypeName(subElem.Name)
			default:
				// Empty element - just use string
				fieldType = stringType
			}

			// If multiple elements with the same name, use a slice
			if elementCounts[subElem.Name] > 1 {
				fieldType = "[]" + fieldType
			}

			typeInfo.Fields = append(typeInfo.Fields, FieldInfo{
				Name:   fieldName,
				Type:   fieldType,
				XMLTag: fmt.Sprintf("`xml:\"%s\"`", subElem.Name),
			})
			seenFields[fieldName] = true
		}
	}
}

// hasNestedContent checks if an element has nested element or ref content (not just text or empty)
func hasNestedContent(elem *rng.Element) bool {
	if len(elem.Elements) > 0 || len(elem.Ref) > 0 || len(elem.Group) > 0 ||
		elem.Choice != nil || len(elem.Interleave) > 0 || len(elem.Optional) > 0 ||
		len(elem.OneOrMore) > 0 || len(elem.ZeroOrMore) > 0 {
		return true
	}
	return false
}

func getAttributeFieldType(attr *rng.Attribute, elem *rng.Element) string {
	if attr.Choice != nil && len(attr.Choice.Values) > 0 {
		return stringType
	}
	if attr.Data != nil {
		return mapDataType(attr.Data.Type)
	}
	if elem.List != nil && elem.List.Data != nil {
		elemType := mapDataType(elem.List.Data.Type)
		return "[]" + elemType
	}
	return stringType
}

func addOptionalFields(typeInfo *TypeInfo, elem *rng.Element, seenFields map[string]bool) {
	for _, opt := range elem.Optional {
		addOptionalAttributes(typeInfo, &opt, seenFields)
		addOptionalElements(typeInfo, &opt, seenFields)
	}
}

func addOptionalAttributes(typeInfo *TypeInfo, opt *rng.Optional, seenFields map[string]bool) {
	for _, attr := range opt.Attributes {
		fieldType := stringType
		if attr.Choice != nil && len(attr.Choice.Values) > 0 {
			fieldType = stringType
		} else if attr.Data != nil {
			fieldType = mapDataType(attr.Data.Type)
		}

		fieldName := toGoFieldName(attr.Name)
		if !seenFields[fieldName] {
			typeInfo.Fields = append(typeInfo.Fields, FieldInfo{
				Name:     fieldName,
				Type:     "*" + fieldType,
				XMLTag:   fmt.Sprintf("`xml:\"%s,attr,omitempty\"`", attr.Name),
				Optional: true,
			})
			seenFields[fieldName] = true
		}
	}
}

func addOptionalElements(typeInfo *TypeInfo, opt *rng.Optional, seenFields map[string]bool) {
	for _, subElem := range opt.Elements {
		fieldName := toGoFieldName(subElem.Name)
		if !seenFields[fieldName] {
			// Determine the field type based on element content
			var fieldType string
			switch {
			case subElem.Text != nil:
				// Simple text element -> *string
				fieldType = "*string"
			case subElem.Data != nil:
				// Data element -> pointer to mapped type
				fieldType = "*" + mapDataType(subElem.Data.Type)
			default:
				// Complex element or ref -> pointer to type
				fieldType = "*" + toGoTypeName(subElem.Name)
			}

			typeInfo.Fields = append(typeInfo.Fields, FieldInfo{
				Name:     fieldName,
				Type:     fieldType,
				XMLTag:   fmt.Sprintf("`xml:\"%s,omitempty\"`", subElem.Name),
				Optional: true,
			})
			seenFields[fieldName] = true
		}
	}
}

func addRefFields(typeInfo *TypeInfo, elem *rng.Element, seenFields map[string]bool) {
	for _, ref := range elem.Ref {
		fieldName := toGoFieldName(ref.Name)
		if !seenFields[fieldName] {
			typeInfo.Fields = append(typeInfo.Fields, FieldInfo{
				Name:   fieldName,
				Type:   toGoTypeName(ref.Name),
				XMLTag: fmt.Sprintf("`xml:\"%s\"`", sanitizeRefName(ref.Name)),
			})
			seenFields[fieldName] = true
		}
	}
}

func addOneOrMoreFields(typeInfo *TypeInfo, elem *rng.Element, seenFields map[string]bool) {
	for _, oneOrMore := range elem.OneOrMore {
		addOneOrMoreRefs(typeInfo, &oneOrMore, seenFields)
		addOneOrMoreElements(typeInfo, &oneOrMore, seenFields)
	}
}

func addOneOrMoreRefs(typeInfo *TypeInfo, oneOrMore *rng.OneOrMore, seenFields map[string]bool) {
	for _, ref := range oneOrMore.Ref {
		fieldName := toGoFieldName(ref.Name)
		if !seenFields[fieldName] {
			typeInfo.Fields = append(typeInfo.Fields, FieldInfo{
				Name:   fieldName,
				Type:   "[]" + toGoTypeName(ref.Name),
				XMLTag: fmt.Sprintf("`xml:\"%s\"`", sanitizeRefName(ref.Name)),
			})
			seenFields[fieldName] = true
		}
	}
}

func addOneOrMoreElements(typeInfo *TypeInfo, oneOrMore *rng.OneOrMore, seenFields map[string]bool) {
	for _, subElem := range oneOrMore.Element {
		fieldName := toGoFieldName(subElem.Name)
		if !seenFields[fieldName] {
			typeInfo.Fields = append(typeInfo.Fields, FieldInfo{
				Name:   fieldName,
				Type:   "[]" + toGoTypeName(subElem.Name),
				XMLTag: fmt.Sprintf("`xml:\"%s\"`", subElem.Name),
			})
			seenFields[fieldName] = true
		}
	}
}

func addGroupFields(typeInfo *TypeInfo, elem *rng.Element, seenFields map[string]bool) {
	for _, group := range elem.Group {
		addGroupElements(typeInfo, &group, seenFields)
		addGroupRefs(typeInfo, &group, seenFields)
		addGroupText(typeInfo, &group, seenFields)
		addGroupList(typeInfo, &group, seenFields)
	}
}

func addGroupElements(typeInfo *TypeInfo, group *rng.Group, seenFields map[string]bool) {
	// Count occurrences of each element name in the group
	elementCounts := make(map[string]int)
	for _, subElem := range group.Elements {
		elementCounts[subElem.Name]++
	}

	for _, subElem := range group.Elements {
		fieldName := toGoFieldName(subElem.Name)
		if !seenFields[fieldName] {
			fieldType := stringType
			if subElem.Text == nil {
				fieldType = toGoTypeName(subElem.Name)
			}

			// If there are multiple elements with the same name, use a slice
			if elementCounts[subElem.Name] > 1 {
				fieldType = "[]" + fieldType
			}

			typeInfo.Fields = append(typeInfo.Fields, FieldInfo{
				Name:   fieldName,
				Type:   fieldType,
				XMLTag: fmt.Sprintf("`xml:\"%s\"`", subElem.Name),
			})
			seenFields[fieldName] = true
		}
	}
}

func addGroupRefs(typeInfo *TypeInfo, group *rng.Group, seenFields map[string]bool) {
	for _, ref := range group.Ref {
		fieldName := toGoFieldName(ref.Name)
		if !seenFields[fieldName] {
			typeInfo.Fields = append(typeInfo.Fields, FieldInfo{
				Name:   fieldName,
				Type:   toGoTypeName(ref.Name),
				XMLTag: fmt.Sprintf("`xml:\"%s\"`", sanitizeRefName(ref.Name)),
			})
			seenFields[fieldName] = true
		}
	}
}

func addGroupText(typeInfo *TypeInfo, group *rng.Group, seenFields map[string]bool) {
	if group.Text != nil {
		fieldName := "Content"
		if !seenFields[fieldName] {
			typeInfo.Fields = append(typeInfo.Fields, FieldInfo{
				Name:   fieldName,
				Type:   stringType,
				XMLTag: "`xml:\",innerxml\"`",
			})
			seenFields[fieldName] = true
		}
	}
}

func addGroupList(typeInfo *TypeInfo, group *rng.Group, seenFields map[string]bool) {
	if group.List != nil {
		fieldName := "Text"
		if !seenFields[fieldName] {
			typeInfo.Fields = append(typeInfo.Fields, FieldInfo{
				Name:   fieldName,
				Type:   stringType,
				XMLTag: "`xml:\",chardata\"`",
			})
			seenFields[fieldName] = true
		}
	}
}

func addZeroOrMoreFields(typeInfo *TypeInfo, elem *rng.Element, seenFields map[string]bool) {
	for _, zeroOrMore := range elem.ZeroOrMore {
		addZeroOrMoreRefs(typeInfo, &zeroOrMore, seenFields)
		addZeroOrMoreElements(typeInfo, &zeroOrMore, seenFields)
	}
}

func addZeroOrMoreRefs(typeInfo *TypeInfo, zeroOrMore *rng.ZeroOrMore, seenFields map[string]bool) {
	for _, ref := range zeroOrMore.Ref {
		fieldName := toGoFieldName(ref.Name)
		if !seenFields[fieldName] {
			typeInfo.Fields = append(typeInfo.Fields, FieldInfo{
				Name:   fieldName,
				Type:   "[]" + toGoTypeName(ref.Name),
				XMLTag: fmt.Sprintf("`xml:\"%s\"`", sanitizeRefName(ref.Name)),
			})
			seenFields[fieldName] = true
		}
	}
}

func addZeroOrMoreElements(typeInfo *TypeInfo, zeroOrMore *rng.ZeroOrMore, seenFields map[string]bool) {
	for _, subElem := range zeroOrMore.Element {
		fieldName := toGoFieldName(subElem.Name)
		if !seenFields[fieldName] {
			typeInfo.Fields = append(typeInfo.Fields, FieldInfo{
				Name:   fieldName,
				Type:   "[]" + toGoTypeName(subElem.Name),
				XMLTag: fmt.Sprintf("`xml:\"%s\"`", subElem.Name),
			})
			seenFields[fieldName] = true
		}
	}
}

func addMixedField(typeInfo *TypeInfo, elem *rng.Element, seenFields map[string]bool) {
	if elem.Mixed != nil {
		fieldName := "Content"
		if !seenFields[fieldName] {
			typeInfo.Fields = append(typeInfo.Fields, FieldInfo{
				Name:   fieldName,
				Type:   stringType,
				XMLTag: "`xml:\",innerxml\"`",
			})
			seenFields[fieldName] = true
		}
	}
}

func addDataValueField(typeInfo *TypeInfo, elem *rng.Element, seenFields map[string]bool) {
	if elem.Data != nil {
		fieldName := "Value"
		fieldType := mapDataType(elem.Data.Type)
		if !seenFields[fieldName] {
			typeInfo.Fields = append(typeInfo.Fields, FieldInfo{
				Name:   fieldName,
				Type:   fieldType,
				XMLTag: "`xml:\",chardata\"`",
			})
			seenFields[fieldName] = true
		}
	}
}

func addListValueField(typeInfo *TypeInfo, elem *rng.Element, seenFields map[string]bool) {
	if elem.List != nil {
		fieldName := "Text"
		// For list elements, generate a string field to hold the whitespace-separated text content.
		// The list pattern validates that the content is space-separated tokens matching the child pattern.
		// We don't parse into a slice because:
		// 1. XML unmarshaler can't directly unmarshal chardata into []string
		// 2. The validation is done by the RELAX NG validator
		if !seenFields[fieldName] {
			typeInfo.Fields = append(typeInfo.Fields, FieldInfo{
				Name:   fieldName,
				Type:   stringType,
				XMLTag: "`xml:\",chardata\"`",
			})
			seenFields[fieldName] = true
		}
	}
}

func mapDataType(xsdType string) string {
	switch xsdType {
	case "string", "token", "normalizedString", "language", "Name", "NCName", "NMTOKEN":
		return stringType
	case "boolean":
		return "bool"
	case "integer", "int", "long", "short", "byte", "nonNegativeInteger", "positiveInteger":
		return "int64"
	case "decimal", "double", "float":
		return "float64"
	case "anyURI":
		return stringType
	case "date", "dateTime", "time":
		return stringType
	default:
		return stringType
	}
}

var goKeywords = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
	"func": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true, "var": true,
}

func toGoTypeName(name string) string {
	return sanitizeIdentifier(name, true)
}

func toGoFieldName(name string) string {
	return sanitizeIdentifier(name, true)
}

func sanitizeIdentifier(name string, exported bool) string {
	var parts []string
	var current strings.Builder

	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			current.WriteRune(r)
		} else if current.Len() > 0 {
			parts = append(parts, current.String())
			current.Reset()
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	if len(parts) == 0 {
		parts = []string{"X"}
	}

	var result strings.Builder
	for i, part := range parts {
		if part == "" {
			continue
		}
		if i == 0 && part[0] >= '0' && part[0] <= '9' {
			result.WriteString("X")
		}
		if exported || i > 0 {
			result.WriteString(capitalize(part))
		} else {
			result.WriteString(strings.ToLower(part))
		}
	}

	identifier := result.String()
	if goKeywords[strings.ToLower(identifier)] {
		identifier = "X" + identifier
	}

	if identifier == "" {
		identifier = "X"
	}

	return identifier
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = []rune(strings.ToUpper(string(r[0])))[0]
	return string(r)
}

func sanitizeRefName(name string) string {
	if i := strings.LastIndexAny(name, ".: "); i >= 0 {
		return name[i+1:]
	}
	return name
}

// GenerateCode generates Go source code from TypeInfo structures,
// including custom UnmarshalXML methods with schema validation using the rng validator.
// Each type gets a custom UnmarshalXML method that validates the unmarshaled XML against the schema.
// The schema is embedded in the generated code and automatically initialized.
// schemaContent should be the raw content of the RELAX NG schema file.
// Returns formatted Go source code as a string.
func GenerateCode(types []TypeInfo, packageName string, schemaContent string, grammar *rng.Grammar) (string, error) {
	return GenerateCodeWithUnmarshal(types, packageName, schemaContent, grammar)
}

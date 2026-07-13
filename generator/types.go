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
func buildTypeFromElement(elem *rng.Element, typeName string, isRootType bool, nestedElements map[string][]*rng.Element, refElem map[string]string) TypeInfo {
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
	addRefFields(&typeInfo, elem, seenFields, refElem)
	addOneOrMoreFields(&typeInfo, elem, seenFields, refElem)
	addGroupFields(&typeInfo, elem, seenFields, refElem)
	addZeroOrMoreFields(&typeInfo, elem, seenFields, refElem)
	addChoiceFields(&typeInfo, elem, seenFields, refElem)
	addInterleaveFields(&typeInfo, elem, seenFields, refElem)
	addMixedField(&typeInfo, elem, seenFields)
	addDataValueField(&typeInfo, elem, seenFields)
	addListValueField(&typeInfo, elem, seenFields)

	// Collect nested elements from this element
	collectNestedElements(elem, nestedElements)

	return typeInfo
}

// buildInterleaveType builds a TypeInfo for defines with interleave patterns.
func buildInterleaveType(def *rng.Define, defineTypeName string, isRootType bool, nestedElements map[string][]*rng.Element) TypeInfo {
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
		if e.Text == nil && e.Name != "" {
			collectVariant(e, nestedElements)
		}

		// Collect nested elements within this element
		collectNestedElements(e, nestedElements)
	}

	return typeInfo
}

// buildMultiElementType builds a TypeInfo for defines with multiple direct element children.
func buildMultiElementType(def *rng.Define, defineTypeName string, isRootType bool, nestedElements map[string][]*rng.Element) TypeInfo {
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
func processDefines(grammar *rng.Grammar, rootDefineName string, types *[]TypeInfo, seenTypeNames map[string]bool, nestedElements map[string][]*rng.Element, refElem map[string]string) {
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
				typeInfo := buildTypeFromElement(elem, elemTypeName, false, nestedElements, refElem)
				*types = append(*types, typeInfo)
				seenTypeNames[elemTypeName] = true
			}
		} else {
			// Single element or other patterns - use element name for type
			if seenTypeNames[elemTypeName] {
				continue
			}

			typeInfo := buildTypeFromElement(elem, elemTypeName, def.Name == rootDefineName, nestedElements, refElem)
			*types = append(*types, typeInfo)
			seenTypeNames[elemTypeName] = true
		}
	}
}

// processNestedElements generates types for all collected nested elements,
// merging same-named variants into one type.
func processNestedElements(nestedElements map[string][]*rng.Element, types *[]TypeInfo, seenTypeNames map[string]bool, refElem map[string]string) {
	for elemName, variants := range nestedElements {
		typeName := toGoTypeName(elemName)

		// Skip if already generated
		if seenTypeNames[typeName] {
			continue
		}

		typeInfo := buildMergedType(variants, typeName, false, nestedElements, refElem)
		*types = append(*types, typeInfo)
		seenTypeNames[typeName] = true
	}
}

// buildMergedType builds one TypeInfo for a set of same-named element variants.
// With a single variant it is just buildTypeFromElement; with several (two
// <element name="item"> with different content) it unions their fields and makes
// them optional, so a document matching either variant unmarshals without loss
// instead of the second variant being dropped.
func buildMergedType(variants []*rng.Element, typeName string, isRootType bool, nestedElements map[string][]*rng.Element, refElem map[string]string) TypeInfo {
	base := buildTypeFromElement(variants[0], typeName, isRootType, nestedElements, refElem)
	if len(variants) == 1 {
		return base
	}

	seen := make(map[string]bool, len(base.Fields))
	for _, f := range base.Fields {
		seen[f.Name] = true
	}
	for _, v := range variants[1:] {
		vt := buildTypeFromElement(v, typeName, isRootType, nestedElements, refElem)
		for _, f := range vt.Fields {
			if f.Name == "XMLName" || seen[f.Name] {
				continue
			}
			base.Fields = append(base.Fields, f)
			seen[f.Name] = true
		}
	}

	// Fields come from mutually-exclusive variants, so none is always present.
	for i := range base.Fields {
		base.Fields[i] = makeFieldOptional(base.Fields[i])
	}
	return base
}

// makeFieldOptional rewrites a field so encoding/xml treats it as optional
// (adds omitempty; element fields also become pointers). XMLName is unchanged.
func makeFieldOptional(f FieldInfo) FieldInfo {
	if f.Name == "XMLName" || f.Optional {
		return f
	}
	if !strings.Contains(f.XMLTag, "omitempty") {
		f.XMLTag = strings.Replace(f.XMLTag, `"`+"`", `,omitempty"`+"`", 1)
	}
	// Element fields (no ",attr"/",chardata"/",innerxml") become pointers so an
	// absent element is distinguishable and omitted.
	if !strings.Contains(f.XMLTag, ",attr") && !strings.Contains(f.XMLTag, ",chardata") &&
		!strings.Contains(f.XMLTag, ",innerxml") && !strings.HasPrefix(f.Type, "*") &&
		!strings.HasPrefix(f.Type, "[]") {
		f.Type = "*" + f.Type
	}
	f.Optional = true
	return f
}

// GenerateTypes creates TypeInfo structures from a RELAX NG grammar.
// It processes schema definitions and converts them to Go type metadata.
func GenerateTypes(grammar *rng.Grammar) ([]TypeInfo, error) {
	types := make([]TypeInfo, 0)
	seenTypeNames := make(map[string]bool)
	nestedElements := make(map[string][]*rng.Element) // Collect nested elements

	// A ref generates a field whose Go type and XML tag come from the element
	// the referenced define wraps — not the define name, which may differ (e.g.
	// after nested-grammar unpacking a define "foo_foo" can wrap <element
	// name="innerFoo">). refElem maps define name -> that element's XML name.
	refElem := make(map[string]string)
	for i := range grammar.Defines {
		d := &grammar.Defines[i]
		if el := d.FirstElement(); el != nil && el.Name != "" {
			refElem[d.Name] = el.Name
		}
	}

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
			typeInfo := buildTypeFromElement(elem, typeName, true, nestedElements, refElem)
			types = append(types, typeInfo)
			seenTypeNames[typeName] = true
		}
	}

	processDefines(grammar, rootDefineName, &types, seenTypeNames, nestedElements, refElem)
	processNestedElements(nestedElements, &types, seenTypeNames, refElem)

	return types, nil
}

// collectNestedElements recursively collects all nested elements in the schema.
// Elements are keyed by name and every distinct variant is kept, so that two
// same-named elements with different content can be merged into one type instead
// of the second being silently dropped.
func collectNestedElements(elem *rng.Element, collected map[string][]*rng.Element) {
	// addDirectElements (and the choice/interleave passes) emit a field typed
	// after a child element with nested content, so those child types must be
	// generated too — otherwise the output references an undefined type.
	for i := range elem.Elements {
		collectVariant(&elem.Elements[i], collected)
	}
	for gi := range elem.Group {
		for i := range elem.Group[gi].Elements {
			collectVariant(&elem.Group[gi].Elements[i], collected)
		}
	}
	for oi := range elem.OneOrMore {
		for i := range elem.OneOrMore[oi].Element {
			collectVariant(&elem.OneOrMore[oi].Element[i], collected)
		}
	}
	for zi := range elem.ZeroOrMore {
		for i := range elem.ZeroOrMore[zi].Element {
			collectVariant(&elem.ZeroOrMore[zi].Element[i], collected)
		}
	}
	for oi := range elem.Optional {
		for i := range elem.Optional[oi].Elements {
			collectVariant(&elem.Optional[oi].Elements[i], collected)
		}
	}
	if elem.Choice != nil {
		for i := range elem.Choice.Elements {
			collectVariant(&elem.Choice.Elements[i], collected)
		}
	}
	for ii := range elem.Interleave {
		for i := range elem.Interleave[ii].Elements {
			collectVariant(&elem.Interleave[ii].Elements[i], collected)
		}
	}
}

// collectVariant records subElem as a variant of its element name and, the first
// time a name is seen, recurses into it. The pointer check avoids re-adding the
// exact same element, and recursing only on first sight bounds recursion for
// self-referential elements.
func collectVariant(subElem *rng.Element, collected map[string][]*rng.Element) {
	if subElem.Name == "" {
		return
	}
	existing := collected[subElem.Name]
	for _, e := range existing {
		if e == subElem {
			return
		}
	}
	collected[subElem.Name] = append(existing, subElem)
	if len(existing) == 0 {
		collectNestedElements(subElem, collected)
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
		fieldType := getAttributeFieldType(&attr)
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

// elementFieldType returns the Go type for a field generated from a sub-element.
// An element that carries attributes or nested content needs its own struct;
// one whose content is only text/data (and no attributes) maps to a string.
func elementFieldType(subElem *rng.Element) string {
	if len(subElem.Attributes) > 0 || hasNestedContent(subElem) {
		return toGoTypeName(subElem.Name)
	}
	return stringType
}

// addChoiceFields generates fields for an element-level <choice>. Go struct tags
// cannot express "exactly one of", so each alternative becomes an optional field
// and unmarshalling populates whichever branch is present — preserving data that
// was previously dropped entirely.
func addChoiceFields(typeInfo *TypeInfo, elem *rng.Element, seenFields map[string]bool, refElem map[string]string) {
	ch := elem.Choice
	if ch == nil {
		return
	}

	// A choice consisting only of <value>s is an enumerated text content.
	if len(ch.Values) > 0 && len(ch.Elements) == 0 && len(ch.Refs) == 0 && len(ch.Attributes) == 0 {
		if !seenFields["Value"] {
			typeInfo.Fields = append(typeInfo.Fields, FieldInfo{
				Name: "Value", Type: stringType, XMLTag: "`xml:\",chardata\"`",
			})
			seenFields["Value"] = true
		}
		return
	}

	for i := range ch.Elements {
		subElem := &ch.Elements[i]
		if subElem.Name == "" {
			continue
		}
		fieldName := toGoFieldName(subElem.Name)
		if seenFields[fieldName] {
			continue
		}
		typeInfo.Fields = append(typeInfo.Fields, FieldInfo{
			Name:     fieldName,
			Type:     "*" + elementFieldType(subElem),
			XMLTag:   fmt.Sprintf("`xml:\"%s,omitempty\"`", subElem.Name),
			Optional: true,
		})
		seenFields[fieldName] = true
	}
	for _, ref := range ch.Refs {
		fieldName := toGoFieldName(ref.Name)
		if seenFields[fieldName] {
			continue
		}
		target := refXMLName(refElem, ref.Name)
		typeInfo.Fields = append(typeInfo.Fields, FieldInfo{
			Name:     fieldName,
			Type:     "*" + toGoTypeName(target),
			XMLTag:   fmt.Sprintf("`xml:\"%s,omitempty\"`", sanitizeRefName(target)),
			Optional: true,
		})
		seenFields[fieldName] = true
	}
	for i := range ch.Attributes {
		addChoiceAttribute(typeInfo, &ch.Attributes[i], elem, seenFields)
	}
}

func addChoiceAttribute(typeInfo *TypeInfo, attr *rng.Attribute, elem *rng.Element, seenFields map[string]bool) {
	fieldName := toGoFieldName(attr.Name)
	if attr.Name == "" || seenFields[fieldName] {
		return
	}
	typeInfo.Fields = append(typeInfo.Fields, FieldInfo{
		Name:   fieldName,
		Type:   getAttributeFieldType(attr),
		XMLTag: fmt.Sprintf("`xml:\"%s,attr,omitempty\"`", attr.Name),
	})
	seenFields[fieldName] = true
}

// addInterleaveFields generates fields for an element-level <interleave>. Order
// is irrelevant to encoding/xml, so each child becomes a plain field.
func addInterleaveFields(typeInfo *TypeInfo, elem *rng.Element, seenFields map[string]bool, refElem map[string]string) {
	for i := range elem.Interleave {
		il := &elem.Interleave[i]
		for j := range il.Elements {
			subElem := &il.Elements[j]
			if subElem.Name == "" || seenFields[toGoFieldName(subElem.Name)] {
				continue
			}
			fieldName := toGoFieldName(subElem.Name)
			typeInfo.Fields = append(typeInfo.Fields, FieldInfo{
				Name:   fieldName,
				Type:   elementFieldType(subElem),
				XMLTag: fmt.Sprintf("`xml:\"%s\"`", subElem.Name),
			})
			seenFields[fieldName] = true
		}
		for _, ref := range il.Ref {
			fieldName := toGoFieldName(ref.Name)
			if seenFields[fieldName] {
				continue
			}
			target := refXMLName(refElem, ref.Name)
			typeInfo.Fields = append(typeInfo.Fields, FieldInfo{
				Name:   fieldName,
				Type:   toGoTypeName(target),
				XMLTag: fmt.Sprintf("`xml:\"%s\"`", sanitizeRefName(target)),
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

func getAttributeFieldType(attr *rng.Attribute) string {
	if attr.Choice != nil && len(attr.Choice.Values) > 0 {
		return stringType
	}
	if attr.Data != nil {
		return mapDataType(attr.Data.Type)
	}
	// A <list> in the attribute's own content maps to a slice (space-separated
	// tokens). This must key off the attribute, not the enclosing element: an
	// element's <list> body says nothing about the attribute's type.
	if attr.List != nil && attr.List.Data != nil {
		return "[]" + mapDataType(attr.List.Data.Type)
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

// refXMLName resolves a ref target to the XML name of the element the referenced
// define wraps, so ref fields get the correct Go type and XML tag even when the
// define name differs from the element name. Falls back to the ref name.
func refXMLName(refElem map[string]string, name string) string {
	if n, ok := refElem[name]; ok && n != "" {
		return n
	}
	return name
}

func addRefFields(typeInfo *TypeInfo, elem *rng.Element, seenFields map[string]bool, refElem map[string]string) {
	for _, ref := range elem.Ref {
		fieldName := toGoFieldName(ref.Name)
		if !seenFields[fieldName] {
			typeInfo.Fields = append(typeInfo.Fields, FieldInfo{
				Name:   fieldName,
				Type:   toGoTypeName(refXMLName(refElem, ref.Name)),
				XMLTag: fmt.Sprintf("`xml:\"%s\"`", sanitizeRefName(refXMLName(refElem, ref.Name))),
			})
			seenFields[fieldName] = true
		}
	}
}

func addOneOrMoreFields(typeInfo *TypeInfo, elem *rng.Element, seenFields map[string]bool, refElem map[string]string) {
	for _, oneOrMore := range elem.OneOrMore {
		addOneOrMoreRefs(typeInfo, &oneOrMore, seenFields, refElem)
		addOneOrMoreElements(typeInfo, &oneOrMore, seenFields)
	}
}

func addOneOrMoreRefs(typeInfo *TypeInfo, oneOrMore *rng.OneOrMore, seenFields map[string]bool, refElem map[string]string) {
	for _, ref := range oneOrMore.Ref {
		fieldName := toGoFieldName(ref.Name)
		if !seenFields[fieldName] {
			typeInfo.Fields = append(typeInfo.Fields, FieldInfo{
				Name:   fieldName,
				Type:   "[]" + toGoTypeName(refXMLName(refElem, ref.Name)),
				XMLTag: fmt.Sprintf("`xml:\"%s\"`", sanitizeRefName(refXMLName(refElem, ref.Name))),
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

func addGroupFields(typeInfo *TypeInfo, elem *rng.Element, seenFields map[string]bool, refElem map[string]string) {
	for _, group := range elem.Group {
		addGroupElements(typeInfo, &group, seenFields)
		addGroupRefs(typeInfo, &group, seenFields, refElem)
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

func addGroupRefs(typeInfo *TypeInfo, group *rng.Group, seenFields map[string]bool, refElem map[string]string) {
	for _, ref := range group.Ref {
		fieldName := toGoFieldName(ref.Name)
		if !seenFields[fieldName] {
			typeInfo.Fields = append(typeInfo.Fields, FieldInfo{
				Name:   fieldName,
				Type:   toGoTypeName(refXMLName(refElem, ref.Name)),
				XMLTag: fmt.Sprintf("`xml:\"%s\"`", sanitizeRefName(refXMLName(refElem, ref.Name))),
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

func addZeroOrMoreFields(typeInfo *TypeInfo, elem *rng.Element, seenFields map[string]bool, refElem map[string]string) {
	for _, zeroOrMore := range elem.ZeroOrMore {
		addZeroOrMoreRefs(typeInfo, &zeroOrMore, seenFields, refElem)
		addZeroOrMoreElements(typeInfo, &zeroOrMore, seenFields)
	}
}

func addZeroOrMoreRefs(typeInfo *TypeInfo, zeroOrMore *rng.ZeroOrMore, seenFields map[string]bool, refElem map[string]string) {
	for _, ref := range zeroOrMore.Ref {
		fieldName := toGoFieldName(ref.Name)
		if !seenFields[fieldName] {
			typeInfo.Fields = append(typeInfo.Fields, FieldInfo{
				Name:   fieldName,
				Type:   "[]" + toGoTypeName(refXMLName(refElem, ref.Name)),
				XMLTag: fmt.Sprintf("`xml:\"%s\"`", sanitizeRefName(refXMLName(refElem, ref.Name))),
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

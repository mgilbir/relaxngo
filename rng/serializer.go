package rng

import (
	"strings"
)

// SerializeGrammar converts a resolved Grammar back to XML.
// This ensures that all includes are expanded and available in the embedded schema.
func SerializeGrammar(g *Grammar) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0"?>` + "\n")
	sb.WriteString(`<grammar xmlns="http://relaxng.org/ns/structure/1.0"`)
	if g.DatatypeLibrary != "" {
		sb.WriteString(` datatypeLibrary="` + escapeXMLAttr(g.DatatypeLibrary) + `"`)
	}
	sb.WriteString(">\n")

	// Check if start choice violates spec restrictions
	needsSyntheticDefine := g.Start.Choice != nil && startChoiceMixesPatterns(g.Start.Choice)

	// Write start element
	if needsSyntheticDefine {
		// If start choice mixes patterns, create a synthetic define and reference it
		sb.WriteString("\t<start>\n")
		sb.WriteString("\t\t<ref name=\"_synthetic_start\"/>\n")
		sb.WriteString("\t</start>\n")
	} else {
		serializeStart(&sb, &g.Start, 1)
	}

	// Write synthetic define if needed (must come before other defines)
	if needsSyntheticDefine {
		sb.WriteString("\t<define name=\"_synthetic_start\">\n")
		serializeChoiceContent(&sb, g.Start.Choice, 2)
		sb.WriteString("\t</define>\n")
	}

	// Write define elements
	for _, def := range g.Defines {
		sb.WriteString("\t<define name=\"" + escapeXMLAttr(def.Name) + "\">\n")
		serializeDefineContent(&sb, &def, 2)
		sb.WriteString("\t</define>\n")
	}

	sb.WriteString("</grammar>\n")
	return sb.String()
}

// startChoiceMixesPatterns checks if a choice mixes element/ref patterns with non-element patterns
func startChoiceMixesPatterns(choice *Choice) bool {
	if choice == nil {
		return false
	}

	// Check what types of patterns are present
	hasElements := len(choice.Elements) > 0
	hasRefs := len(choice.Refs) > 0
	hasNonElementPatterns := len(choice.Data) > 0 ||
		len(choice.Values) > 0 ||
		choice.Text != nil ||
		len(choice.Group) > 0 ||
		len(choice.Interleave) > 0 ||
		choice.Empty != nil ||
		choice.NotAllowed != nil

	// If we have non-element patterns, they cannot be mixed with elements or refs
	return hasNonElementPatterns && (hasElements || hasRefs)
}

func serializeStart(sb *strings.Builder, start *Start, indent int) {
	indentStr := strings.Repeat("\t", indent)
	sb.WriteString(indentStr + "<start>\n")
	serializeStartContent(sb, start, indent+1)
	sb.WriteString(indentStr + "</start>\n")
}

// serializeStartGroupContent handles serialization of group patterns with nested group flattening.
func serializeStartGroupContent(sb *strings.Builder, start *Start, indent int) {
	if len(start.Group) == 1 {
		// Flatten nested groups that contain only a single group
		g := &start.Group[0]
		for len(g.Group) == 1 && len(g.Elements) == 0 && len(g.Attributes) == 0 &&
			len(g.Ref) == 0 && len(g.Choice) == 0 && len(g.Optional) == 0 &&
			len(g.OneOrMore) == 0 && len(g.ZeroOrMore) == 0 && len(g.Interleave) == 0 &&
			g.NotAllowed == nil && g.ExternalRef == nil {
			g = &g.Group[0]
		}
		serializeGroupContent(sb, g, indent)
	} else {
		// Multiple groups - this shouldn't happen in valid simplified syntax
		for _, g := range start.Group {
			serializeGroup(sb, &g, indent)
		}
	}
}

//nolint:nolintlint,dupl // Similar to serializeDefineContent and serializeOneOrMore/serializeZeroOrMore
func serializeStartContent(sb *strings.Builder, start *Start, indent int) {
	switch {
	case start.Ref != nil:
		serializeRef(sb, start.Ref, indent)
	case start.ParentRef != nil:
		indentStr := strings.Repeat("\t", indent)
		sb.WriteString(indentStr + "<parentRef name=\"" + escapeXMLAttr(start.ParentRef.Name) + "\"/>\n")
	case start.Element != nil:
		serializeElement(sb, start.Element, indent)
	case start.Choice != nil:
		serializeChoice(sb, start.Choice, indent)
	case len(start.Group) > 0:
		serializeStartGroupContent(sb, start, indent)
	case len(start.Interleave) > 0:
		for _, i := range start.Interleave {
			serializeInterleave(sb, &i, indent)
		}
	case len(start.Optional) > 0:
		for _, opt := range start.Optional {
			serializeOptional(sb, &opt, indent)
		}
	case len(start.OneOrMore) > 0:
		for _, o := range start.OneOrMore {
			serializeOneOrMore(sb, &o, indent)
		}
	case len(start.ZeroOrMore) > 0:
		for _, z := range start.ZeroOrMore {
			serializeZeroOrMore(sb, &z, indent)
		}
	case start.Text != nil:
		writeSimpleTag(sb, indent, "text")
	case start.Data != nil:
		serializeData(sb, start.Data, indent)
	case start.List != nil:
		serializeList(sb, start.List, indent)
	case start.Empty != nil:
		writeSimpleTag(sb, indent, "empty")
	case start.NotAllowed != nil:
		writeSimpleTag(sb, indent, "notAllowed")
	case start.ExternalRef != nil:
		serializeExternalRef(sb, start.ExternalRef, indent)
	}
}

//nolint:nolintlint,dupl // Similar to serializeStartContent
func serializeDefineContent(sb *strings.Builder, def *Define, indent int) {
	switch {
	case def.Ref != nil:
		serializeRef(sb, def.Ref, indent)
	case def.ParentRef != nil:
		indentStr := strings.Repeat("\t", indent)
		sb.WriteString(indentStr + "<parentRef name=\"" + escapeXMLAttr(def.ParentRef.Name) + "\"/>\n")
	case def.Element != nil:
		serializeElement(sb, def.Element, indent)
	case len(def.Elements) > 0:
		for _, elem := range def.Elements {
			serializeElement(sb, &elem, indent)
		}
	case def.Choice != nil:
		serializeChoice(sb, def.Choice, indent)
	case len(def.Group) > 0:
		for _, g := range def.Group {
			serializeGroup(sb, &g, indent)
		}
	case len(def.Interleave) > 0:
		for _, i := range def.Interleave {
			serializeInterleave(sb, &i, indent)
		}
	case len(def.Optional) > 0:
		for _, opt := range def.Optional {
			serializeOptional(sb, &opt, indent)
		}
	case len(def.OneOrMore) > 0:
		for _, o := range def.OneOrMore {
			serializeOneOrMore(sb, &o, indent)
		}
	case len(def.ZeroOrMore) > 0:
		for _, z := range def.ZeroOrMore {
			serializeZeroOrMore(sb, &z, indent)
		}
	case def.Text != nil:
		writeSimpleTag(sb, indent, "text")
	case def.Data != nil:
		serializeData(sb, def.Data, indent)
	case def.List != nil:
		serializeList(sb, def.List, indent)
	case def.Empty != nil:
		writeSimpleTag(sb, indent, "empty")
	case def.NotAllowed != nil:
		writeSimpleTag(sb, indent, "notAllowed")
	case def.ExternalRef != nil:
		serializeExternalRef(sb, def.ExternalRef, indent)
	}
}

func serializeElement(sb *strings.Builder, elem *Element, indent int) {
	indentStr := strings.Repeat("\t", indent)
	sb.WriteString(indentStr + "<element")
	// A name-class child (<name>/<anyName>/<nsName>/<choice>) supplies the name;
	// emitting an empty name="" attribute alongside it produces an invalid schema.
	if elem.Name != "" {
		sb.WriteString(" name=\"" + escapeXMLAttr(elem.Name) + "\"")
	}
	if elem.Ns != "" {
		sb.WriteString(` ns="` + escapeXMLAttr(elem.Ns) + `"`)
	}
	sb.WriteString(">\n")
	serializeElementContent(sb, elem, indent+1)
	sb.WriteString(indentStr + "</element>\n")
}

//nolint:funlen // Element content serialization requires handling all pattern types
func serializeElementContent(sb *strings.Builder, elem *Element, indent int) {
	// Name class must be serialized first: <element> content is (nameClass?, pattern).
	if elem.NameElement != nil {
		serializeNameElement(sb, elem.NameElement, indent)
	}
	if elem.AnyName != nil {
		serializeAnyName(sb, elem.AnyName, indent)
	}
	if elem.NsName != nil {
		serializeNsName(sb, elem.NsName, indent)
	}

	// Text content
	if elem.Text != nil {
		indentStr := strings.Repeat("\t", indent)
		sb.WriteString(indentStr + "<text/>\n")
	}

	// Attributes
	for _, attr := range elem.Attributes {
		serializeAttribute(sb, &attr, indent)
	}

	// Values
	for _, v := range elem.Values {
		serializeValue(sb, &v, indent)
	}

	// Nested elements
	for _, subElem := range elem.Elements {
		serializeElement(sb, &subElem, indent)
	}

	// Ref elements
	for _, ref := range elem.Ref {
		serializeRef(sb, &ref, indent)
	}

	// ParentRef elements
	for _, ref := range elem.ParentRef {
		indentStr := strings.Repeat("\t", indent)
		sb.WriteString(indentStr + "<parentRef name=\"" + escapeXMLAttr(ref.Name) + "\"/>\n")
	}

	// Choice
	if elem.Choice != nil {
		serializeChoice(sb, elem.Choice, indent)
	}

	// Group
	for _, g := range elem.Group {
		serializeGroup(sb, &g, indent)
	}

	// Interleave
	for _, i := range elem.Interleave {
		serializeInterleave(sb, &i, indent)
	}

	// Optional
	for _, opt := range elem.Optional {
		serializeOptional(sb, &opt, indent)
	}

	// OneOrMore
	for _, o := range elem.OneOrMore {
		serializeOneOrMore(sb, &o, indent)
	}

	// ZeroOrMore
	for _, z := range elem.ZeroOrMore {
		serializeZeroOrMore(sb, &z, indent)
	}

	// Data
	if elem.Data != nil {
		serializeData(sb, elem.Data, indent)
	}

	// List
	if elem.List != nil {
		serializeList(sb, elem.List, indent)
	}

	// Empty
	if elem.Empty != nil {
		indentStr := strings.Repeat("\t", indent)
		sb.WriteString(indentStr + "<empty/>\n")
	}

	// Mixed
	if elem.Mixed != nil {
		serializeMixed(sb, elem.Mixed, indent)
	}

	// NotAllowed
	if elem.NotAllowed != nil {
		indentStr := strings.Repeat("\t", indent)
		sb.WriteString(indentStr + "<notAllowed/>\n")
	}

	// ExternalRef
	if elem.ExternalRef != nil {
		serializeExternalRef(sb, elem.ExternalRef, indent)
	}
}

func serializeAttribute(sb *strings.Builder, attr *Attribute, indent int) {
	indentStr := strings.Repeat("\t", indent)
	sb.WriteString(indentStr + "<attribute")
	// A name-class child supplies the name; an empty name="" alongside it is invalid.
	if attr.Name != "" {
		sb.WriteString(" name=\"" + escapeXMLAttr(attr.Name) + "\"")
	}
	if attr.Ns != "" {
		sb.WriteString(` ns="` + escapeXMLAttr(attr.Ns) + `"`)
	}
	sb.WriteString(">\n")
	serializeAttributeContent(sb, attr, indent+1)
	sb.WriteString(indentStr + "</attribute>\n")
}

func serializeAttributeContent(sb *strings.Builder, attr *Attribute, indent int) {
	// Name class must be serialized first: <attribute> content is (nameClass?, pattern).
	if attr.NameElement != nil {
		serializeNameElement(sb, attr.NameElement, indent)
	}
	if attr.AnyName != nil {
		serializeAnyName(sb, attr.AnyName, indent)
	}
	if attr.NsName != nil {
		serializeNsName(sb, attr.NsName, indent)
	}

	if attr.Choice != nil {
		serializeChoice(sb, attr.Choice, indent)
	}

	for _, v := range attr.Values {
		serializeValue(sb, &v, indent)
	}

	if attr.Data != nil {
		serializeData(sb, attr.Data, indent)
	}

	if attr.Empty != nil {
		indentStr := strings.Repeat("\t", indent)
		sb.WriteString(indentStr + "<empty/>\n")
	}

	if attr.List != nil {
		serializeList(sb, attr.List, indent)
	}

	// Text pattern
	if attr.Text != nil {
		indentStr := strings.Repeat("\t", indent)
		sb.WriteString(indentStr + "<text/>\n")
	}
}

func serializeRef(sb *strings.Builder, ref *Ref, indent int) {
	indentStr := strings.Repeat("\t", indent)
	sb.WriteString(indentStr + "<ref name=\"" + escapeXMLAttr(ref.Name) + "\"/>\n")
}

func serializeExternalRef(sb *strings.Builder, extRef *ExternalRef, indent int) {
	indentStr := strings.Repeat("\t", indent)
	sb.WriteString(indentStr + "<externalRef href=\"" + escapeXMLAttr(extRef.Href) + "\"")
	if extRef.Ns != "" {
		sb.WriteString(` ns="` + escapeXMLAttr(extRef.Ns) + `"`)
	}
	sb.WriteString("/>\n")
}

func serializeAnyName(sb *strings.Builder, anyName *AnyName, indent int) {
	indentStr := strings.Repeat("\t", indent)
	if anyName.Except == nil {
		sb.WriteString(indentStr + "<anyName/>\n")
	} else {
		sb.WriteString(indentStr + "<anyName>\n")
		serializeNameExcept(sb, anyName.Except, indent+1)
		sb.WriteString(indentStr + "</anyName>\n")
	}
}

func serializeNsName(sb *strings.Builder, nsName *NsName, indent int) {
	indentStr := strings.Repeat("\t", indent)
	if nsName.Except == nil {
		sb.WriteString(indentStr + "<nsName ns=\"" + escapeXMLAttr(nsName.Ns) + "\"/>\n")
	} else {
		sb.WriteString(indentStr + "<nsName ns=\"" + escapeXMLAttr(nsName.Ns) + "\">\n")
		serializeNameExcept(sb, nsName.Except, indent+1)
		sb.WriteString(indentStr + "</nsName>\n")
	}
}

func serializeNameExcept(sb *strings.Builder, except *NameExcept, indent int) {
	indentStr := strings.Repeat("\t", indent)
	sb.WriteString(indentStr + "<except>\n")
	for _, nc := range except.Names {
		if nc.Ns != "" {
			sb.WriteString(strings.Repeat("\t", indent+1) + "<name ns=\"" + escapeXMLAttr(nc.Ns) + "\">" + escapeXML(nc.Value) + "</name>\n")
		} else {
			sb.WriteString(strings.Repeat("\t", indent+1) + "<name>" + escapeXML(nc.Value) + "</name>\n")
		}
	}
	if except.NsName != nil {
		serializeNsName(sb, except.NsName, indent+1)
	}
	if except.AnyName != nil {
		serializeAnyName(sb, except.AnyName, indent+1)
	}
	sb.WriteString(indentStr + "</except>\n")
}

func serializeNameElement(sb *strings.Builder, nameElem *NameElement, indent int) {
	indentStr := strings.Repeat("\t", indent)
	if nameElem.Ns != "" {
		sb.WriteString(indentStr + "<name ns=\"" + escapeXMLAttr(nameElem.Ns) + "\">" + escapeXML(nameElem.Value) + "</name>\n")
	} else {
		sb.WriteString(indentStr + "<name>" + escapeXML(nameElem.Value) + "</name>\n")
	}
}

func serializeChoice(sb *strings.Builder, choice *Choice, indent int) {
	indentStr := strings.Repeat("\t", indent)
	sb.WriteString(indentStr + "<choice>\n")
	serializeChoiceContent(sb, choice, indent+1)
	sb.WriteString(indentStr + "</choice>\n")
}

func serializeChoiceContent(sb *strings.Builder, choice *Choice, indent int) {
	for _, elem := range choice.Elements {
		serializeElement(sb, &elem, indent)
	}

	for _, attr := range choice.Attributes {
		serializeAttribute(sb, &attr, indent)
	}

	// Serialize <name> elements (name classes)
	for _, nameElem := range choice.NameElements {
		serializeNameElement(sb, &nameElem, indent)
	}

	for _, ref := range choice.Refs {
		serializeRef(sb, &ref, indent)
	}

	for _, v := range choice.Values {
		serializeValue(sb, &v, indent)
	}

	for _, d := range choice.Data {
		serializeData(sb, &d, indent)
	}

	if choice.Text != nil {
		indentStr := strings.Repeat("\t", indent)
		sb.WriteString(indentStr + "<text/>\n")
	}

	if choice.Empty != nil {
		indentStr := strings.Repeat("\t", indent)
		sb.WriteString(indentStr + "<empty/>\n")
	}

	if choice.NotAllowed != nil {
		indentStr := strings.Repeat("\t", indent)
		sb.WriteString(indentStr + "<notAllowed/>\n")
	}

	for _, c := range choice.Group {
		serializeGroup(sb, &c, indent)
	}

	for _, c := range choice.Interleave {
		serializeInterleave(sb, &c, indent)
	}

	if choice.List != nil {
		serializeList(sb, choice.List, indent)
	}

	if choice.Mixed != nil {
		serializeMixed(sb, choice.Mixed, indent)
	}

	if choice.ExternalRef != nil {
		serializeExternalRef(sb, choice.ExternalRef, indent)
	}
}

func serializeGroup(sb *strings.Builder, group *Group, indent int) {
	indentStr := strings.Repeat("\t", indent)
	sb.WriteString(indentStr + "<group>\n")
	serializeGroupContent(sb, group, indent+1)
	sb.WriteString(indentStr + "</group>\n")
}

//nolint:funlen // Group content serialization requires handling all pattern types
func serializeGroupContent(sb *strings.Builder, group *Group, indent int) {
	// If RawContent is available, use it to preserve original structure and order
	if len(group.RawContent) > 0 {
		indentStr := strings.Repeat("\t", indent)
		lines := strings.Split(string(group.RawContent), "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				sb.WriteString(indentStr + trimmed + "\n")
			}
		}
		return
	}

	// Attributes
	for _, attr := range group.Attributes {
		serializeAttribute(sb, &attr, indent)
	}

	// Value patterns (used in list context)
	for _, val := range group.Value {
		serializeValue(sb, &val, indent)
	}

	// Data patterns (can be multiple in list context)
	for _, d := range group.Data {
		serializeData(sb, &d, indent)
	}

	for _, elem := range group.Elements {
		serializeElement(sb, &elem, indent)
	}

	for _, ref := range group.Ref {
		serializeRef(sb, &ref, indent)
	}

	for _, opt := range group.Optional {
		serializeOptional(sb, &opt, indent)
	}

	for _, c := range group.Choice {
		serializeChoice(sb, &c, indent)
	}

	for _, g := range group.Group {
		serializeGroup(sb, &g, indent)
	}

	for _, o := range group.OneOrMore {
		serializeOneOrMore(sb, &o, indent)
	}

	for _, z := range group.ZeroOrMore {
		serializeZeroOrMore(sb, &z, indent)
	}

	for _, i := range group.Interleave {
		serializeInterleave(sb, &i, indent)
	}

	if group.Text != nil {
		indentStr := strings.Repeat("\t", indent)
		sb.WriteString(indentStr + "<text/>\n")
	}

	if group.NotAllowed != nil {
		indentStr := strings.Repeat("\t", indent)
		sb.WriteString(indentStr + "<notAllowed/>\n")
	}

	if group.ExternalRef != nil {
		serializeExternalRef(sb, group.ExternalRef, indent)
	}
}

func serializeInterleave(sb *strings.Builder, interleave *Interleave, indent int) {
	indentStr := strings.Repeat("\t", indent)
	sb.WriteString(indentStr + "<interleave>\n")
	serializeInterleaveContent(sb, interleave, indent+1)
	sb.WriteString(indentStr + "</interleave>\n")
}

func serializeInterleaveContent(sb *strings.Builder, interleave *Interleave, indent int) {
	// Attributes
	for _, attr := range interleave.Attributes {
		serializeAttribute(sb, &attr, indent)
	}

	// Value patterns (used in list context)
	for _, val := range interleave.Value {
		serializeValue(sb, &val, indent)
	}

	// Data pattern (used in list context)
	if interleave.Data != nil {
		serializeData(sb, interleave.Data, indent)
	}

	for _, elem := range interleave.Elements {
		serializeElement(sb, &elem, indent)
	}

	for _, ref := range interleave.Ref {
		serializeRef(sb, &ref, indent)
	}

	for _, opt := range interleave.Optional {
		serializeOptional(sb, &opt, indent)
	}

	for _, c := range interleave.Choice {
		serializeChoice(sb, &c, indent)
	}

	for _, g := range interleave.Group {
		serializeGroup(sb, &g, indent)
	}

	for _, o := range interleave.OneOrMore {
		serializeOneOrMore(sb, &o, indent)
	}

	for _, z := range interleave.ZeroOrMore {
		serializeZeroOrMore(sb, &z, indent)
	}

	if interleave.Text != nil {
		indentStr := strings.Repeat("\t", indent)
		sb.WriteString(indentStr + "<text/>\n")
	}

	if interleave.List != nil {
		serializeList(sb, interleave.List, indent)
	}

	if interleave.NotAllowed != nil {
		indentStr := strings.Repeat("\t", indent)
		sb.WriteString(indentStr + "<notAllowed/>\n")
	}

	if interleave.ExternalRef != nil {
		serializeExternalRef(sb, interleave.ExternalRef, indent)
	}
}

func serializeOptional(sb *strings.Builder, optional *Optional, indent int) {
	indentStr := strings.Repeat("\t", indent)
	sb.WriteString(indentStr + "<optional>\n")

	// If multiple elements, wrap in a group (implicit sequence)
	if len(optional.Elements) > 1 {
		sb.WriteString(strings.Repeat("\t", indent+1) + "<group>\n")
		for _, elem := range optional.Elements {
			serializeElement(sb, &elem, indent+2)
		}
		sb.WriteString(strings.Repeat("\t", indent+1) + "</group>\n")
	} else {
		for _, elem := range optional.Elements {
			serializeElement(sb, &elem, indent+1)
		}
	}

	for _, attr := range optional.Attributes {
		serializeAttribute(sb, &attr, indent+1)
	}
	for _, ref := range optional.Ref {
		serializeRef(sb, &ref, indent+1)
	}
	for _, ref := range optional.ParentRef {
		indentStr := strings.Repeat("\t", indent+1)
		sb.WriteString(indentStr + "<parentRef name=\"" + escapeXMLAttr(ref.Name) + "\"/>\n")
	}
	if optional.AnyName != nil {
		serializeAnyName(sb, optional.AnyName, indent+1)
	}
	if optional.NsName != nil {
		serializeNsName(sb, optional.NsName, indent+1)
	}
	if optional.Text != nil {
		indentStr := strings.Repeat("\t", indent+1)
		sb.WriteString(indentStr + "<text/>\n")
	}
	if optional.List != nil {
		serializeList(sb, optional.List, indent+1)
	}
	if optional.ExternalRef != nil {
		serializeExternalRef(sb, optional.ExternalRef, indent+1)
	}
	sb.WriteString(indentStr + "</optional>\n")
}

//nolint:dupl // Similar to serializeZeroOrMore
func serializeOneOrMore(sb *strings.Builder, oneOrMore *OneOrMore, indent int) {
	indentStr := strings.Repeat("\t", indent)
	sb.WriteString(indentStr + "<oneOrMore>\n")

	// Attributes
	for _, attr := range oneOrMore.Attribute {
		serializeAttribute(sb, &attr, indent+1)
	}

	// Value patterns (used in list context)
	for _, val := range oneOrMore.Value {
		serializeValue(sb, &val, indent+1)
	}

	// Data patterns (can be multiple in list context)
	for _, d := range oneOrMore.Data {
		serializeData(sb, &d, indent+1)
	}

	// If multiple elements, wrap in a group (implicit sequence)
	if len(oneOrMore.Element) > 1 {
		sb.WriteString(strings.Repeat("\t", indent+1) + "<group>\n")
		for _, elem := range oneOrMore.Element {
			serializeElement(sb, &elem, indent+2)
		}
		sb.WriteString(strings.Repeat("\t", indent+1) + "</group>\n")
	} else {
		for _, elem := range oneOrMore.Element {
			serializeElement(sb, &elem, indent+1)
		}
	}

	for _, ref := range oneOrMore.Ref {
		serializeRef(sb, &ref, indent+1)
	}

	// Handle other patterns in oneOrMore
	for _, g := range oneOrMore.Group {
		serializeGroup(sb, &g, indent+1)
	}
	if oneOrMore.Choice != nil {
		serializeChoice(sb, oneOrMore.Choice, indent+1)
	}
	for _, i := range oneOrMore.Interleave {
		serializeInterleave(sb, &i, indent+1)
	}
	if oneOrMore.AnyName != nil {
		serializeAnyName(sb, oneOrMore.AnyName, indent+1)
	}
	if oneOrMore.NsName != nil {
		serializeNsName(sb, oneOrMore.NsName, indent+1)
	}
	if oneOrMore.Text != nil {
		indentStr := strings.Repeat("\t", indent+1)
		sb.WriteString(indentStr + "<text/>\n")
	}
	if oneOrMore.List != nil {
		serializeList(sb, oneOrMore.List, indent+1)
	}
	if oneOrMore.ExternalRef != nil {
		serializeExternalRef(sb, oneOrMore.ExternalRef, indent+1)
	}

	sb.WriteString(indentStr + "</oneOrMore>\n")
}

//nolint:dupl // Similar to serializeOneOrMore
func serializeZeroOrMore(sb *strings.Builder, zeroOrMore *ZeroOrMore, indent int) {
	indentStr := strings.Repeat("\t", indent)
	sb.WriteString(indentStr + "<zeroOrMore>\n")

	// Attributes
	for _, attr := range zeroOrMore.Attribute {
		serializeAttribute(sb, &attr, indent+1)
	}

	// Value patterns (used in list context)
	for _, val := range zeroOrMore.Value {
		serializeValue(sb, &val, indent+1)
	}

	// Data patterns (can be multiple in list context)
	for _, d := range zeroOrMore.Data {
		serializeData(sb, &d, indent+1)
	}

	// If multiple elements, wrap in a group (implicit sequence)
	if len(zeroOrMore.Element) > 1 {
		sb.WriteString(strings.Repeat("\t", indent+1) + "<group>\n")
		for _, elem := range zeroOrMore.Element {
			serializeElement(sb, &elem, indent+2)
		}
		sb.WriteString(strings.Repeat("\t", indent+1) + "</group>\n")
	} else {
		for _, elem := range zeroOrMore.Element {
			serializeElement(sb, &elem, indent+1)
		}
	}

	for _, ref := range zeroOrMore.Ref {
		serializeRef(sb, &ref, indent+1)
	}

	// Handle other patterns in zeroOrMore
	for _, g := range zeroOrMore.Group {
		serializeGroup(sb, &g, indent+1)
	}
	if zeroOrMore.Choice != nil {
		serializeChoice(sb, zeroOrMore.Choice, indent+1)
	}
	for _, i := range zeroOrMore.Interleave {
		serializeInterleave(sb, &i, indent+1)
	}
	if zeroOrMore.AnyName != nil {
		serializeAnyName(sb, zeroOrMore.AnyName, indent+1)
	}
	if zeroOrMore.NsName != nil {
		serializeNsName(sb, zeroOrMore.NsName, indent+1)
	}
	if zeroOrMore.Text != nil {
		indentStr := strings.Repeat("\t", indent+1)
		sb.WriteString(indentStr + "<text/>\n")
	}
	if zeroOrMore.List != nil {
		serializeList(sb, zeroOrMore.List, indent+1)
	}
	if zeroOrMore.ExternalRef != nil {
		serializeExternalRef(sb, zeroOrMore.ExternalRef, indent+1)
	}

	sb.WriteString(indentStr + "</zeroOrMore>\n")
}

func serializeData(sb *strings.Builder, data *Data, indent int) {
	indentStr := strings.Repeat("\t", indent)
	sb.WriteString(indentStr + "<data type=\"" + escapeXMLAttr(data.Type) + "\"")
	if data.DatatypeLibrary != "" {
		sb.WriteString(` datatypeLibrary="` + escapeXMLAttr(data.DatatypeLibrary) + `"`)
	}

	// Check if there are child elements (params or except)
	if len(data.Params) > 0 || data.Except != nil {
		sb.WriteString(">\n")

		// Serialize param elements
		for _, param := range data.Params {
			indentStr1 := strings.Repeat("\t", indent+1)
			sb.WriteString(indentStr1 + "<param name=\"" + escapeXMLAttr(param.Name) + "\">" + escapeXML(param.Value) + "</param>\n")
		}

		// Serialize except element
		if data.Except != nil {
			serializeDataExcept(sb, data.Except, indent+1)
		}

		sb.WriteString(indentStr + "</data>\n")
	} else {
		sb.WriteString("/>\n")
	}
}

func serializeDataExcept(sb *strings.Builder, except *DataExcept, indent int) {
	indentStr := strings.Repeat("\t", indent)
	sb.WriteString(indentStr + "<except>\n")

	// Serialize value elements
	for _, val := range except.Values {
		serializeValue(sb, &val, indent+1)
	}

	// Serialize data elements
	for _, d := range except.Data {
		serializeData(sb, &d, indent+1)
	}

	// Serialize choice element
	if except.Choice != nil {
		serializeChoice(sb, except.Choice, indent+1)
	}

	sb.WriteString(indentStr + "</except>\n")
}

func serializeValue(sb *strings.Builder, value *Value, indent int) {
	indentStr := strings.Repeat("\t", indent)
	sb.WriteString(indentStr + "<value type=\"" + escapeXMLAttr(value.Type) + "\">" + escapeXML(value.Value) + "</value>\n")
}

func serializeList(sb *strings.Builder, list *List, indent int) {
	indentStr := strings.Repeat("\t", indent)
	sb.WriteString(indentStr + "<list>\n")

	// Serialize all possible patterns inside a list
	if list.Data != nil {
		serializeData(sb, list.Data, indent+1)
	}

	if list.OneOrMore != nil {
		serializeOneOrMore(sb, list.OneOrMore, indent+1)
	}

	if list.Choice != nil {
		serializeChoice(sb, list.Choice, indent+1)
	}

	if list.Group != nil {
		serializeGroup(sb, list.Group, indent+1)
	}

	for _, val := range list.Values {
		serializeValue(sb, &val, indent+1)
	}

	if list.Empty != nil {
		sb.WriteString(strings.Repeat("\t", indent+1) + "<empty/>\n")
	}

	sb.WriteString(indentStr + "</list>\n")
}

func serializeMixed(sb *strings.Builder, mixed *Mixed, indent int) {
	indentStr := strings.Repeat("\t", indent)
	sb.WriteString(indentStr + "<mixed>\n")
	for _, elem := range mixed.Elements {
		serializeElement(sb, &elem, indent+1)
	}
	for _, ref := range mixed.Ref {
		serializeRef(sb, &ref, indent+1)
	}
	for _, opt := range mixed.Optional {
		serializeOptional(sb, &opt, indent+1)
	}
	for _, g := range mixed.Group {
		serializeGroup(sb, &g, indent+1)
	}
	for _, o := range mixed.OneOrMore {
		serializeOneOrMore(sb, &o, indent+1)
	}
	for _, z := range mixed.ZeroOrMore {
		serializeZeroOrMore(sb, &z, indent+1)
	}
	for _, c := range mixed.Choice {
		serializeChoice(sb, &c, indent+1)
	}
	if mixed.NotAllowed != nil {
		sb.WriteString(strings.Repeat("\t", indent+1) + "<notAllowed/>\n")
	}
	if mixed.ExternalRef != nil {
		serializeExternalRef(sb, mixed.ExternalRef, indent+1)
	}
	sb.WriteString(indentStr + "</mixed>\n")
}

func escapeXMLAttr(s string) string {
	s = escapeXML(s)
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// writeSimpleTag writes a simple self-closing XML tag with indentation.
func writeSimpleTag(sb *strings.Builder, indent int, tagName string) {
	indentStr := strings.Repeat("\t", indent)
	sb.WriteString(indentStr + "<" + tagName + "/>\n")
}

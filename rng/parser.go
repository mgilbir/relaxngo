// Package rng provides RELAX NG schema parsing and validation.
package rng

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// Constants for common XML element names in RELAX NG
const (
	elemNameChoice     = "choice"
	elemNameElement    = "element"
	elemNameStart      = "start"
	elemNameValue      = "value"
	elemNameXmlns      = "xmlns"
	elemNameGrammar    = "grammar"
	elemNameInterleave = "interleave"
)

// ResourceResolver is an interface for resolving external schema references.
// This allows for different implementations: disk-based, virtual filesystem, HTTP, etc.
type ResourceResolver interface {
	// ReadResource reads the content of a resource at the given path.
	// The path may be relative to a base directory depending on the implementation.
	ReadResource(path string) ([]byte, error)
}

// DiskResolver resolves resources from the local filesystem.
type DiskResolver struct {
	BaseDir string // Base directory for resolving relative paths
}

// maxResourceBytes bounds the size of a single schema resource (include or
// externalRef) to prevent a hostile or accidental huge file from exhausting
// memory during schema loading.
const maxResourceBytes = 50 * 1024 * 1024 // 50MB

// ReadResource implements ResourceResolver for disk-based resources.
//
// All reads are confined to BaseDir: the resolved target — whether the href is
// relative or absolute — must lie within BaseDir, and traversal via ".." or
// symlinks that would escape it is rejected. This prevents an untrusted schema
// from reading arbitrary files via <include>/<externalRef>.
func (r *DiskResolver) ReadResource(path string) ([]byte, error) {
	base := r.BaseDir
	if base == "" {
		base = "."
	}

	// Interpret the requested path relative to BaseDir. Absolute paths are
	// re-expressed relative to BaseDir so they cannot silently escape it; if
	// they point outside, os.Root rejects them below.
	rel := path
	if filepath.IsAbs(path) {
		absBase, err := filepath.Abs(base)
		if err != nil {
			return nil, err
		}
		rel, err = filepath.Rel(absBase, path)
		if err != nil {
			return nil, fmt.Errorf("resource path escapes base directory %q: %s", base, path)
		}
	}

	// os.Root confines all path resolution (including via ".." and symlinks) to
	// base at the OS level.
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()

	f, err := root.Open(filepath.Clean(rel))
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(io.LimitReader(f, maxResourceBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxResourceBytes {
		return nil, fmt.Errorf("schema resource %q exceeds maximum size of %d bytes", path, maxResourceBytes)
	}
	return data, nil
}

// Grammar represents a RELAX NG grammar element containing the schema definition.
type Grammar struct {
	XMLName         xml.Name      `xml:"http://relaxng.org/ns/structure/1.0 grammar"`
	DatatypeLibrary string        `xml:"datatypeLibrary,attr,omitempty"` // datatype library inherited by Data/Value children
	Start           Start         `xml:"start"`
	Defines         []Define      `xml:"define"`
	Divs            []Div         `xml:"div"` // Grouping elements with namespace context
	Includes        []Include     `xml:"include"`
	ExternalRefs    []ExternalRef `xml:"externalRef"`
	Elements        []Element     `xml:"element"`   // Captures invalid direct elements
	Choices         []Choice      `xml:"choice"`    // Captures invalid choice
	Groups          []Group       `xml:"group"`     // Captures invalid group
	Refs            []Ref         `xml:"ref"`       // Captures invalid ref
	Attrs           []Attribute   `xml:"attribute"` // Captures invalid attribute
	RawAttrs        []xml.Attr    `xml:",any,attr"` // Root attributes incl. xmlns declarations (prefix bindings)
	RawContent      []byte        `xml:",innerxml"`
}

// Div represents a grouping element that can provide namespace context
type Div struct {
	Ns              string     `xml:"ns,attr,omitempty"`              // Namespace to apply to children
	DatatypeLibrary string     `xml:"datatypeLibrary,attr,omitempty"` // datatype library inherited by children
	Start           []Start    `xml:"start"`                          // Start patterns in this div
	Defines         []Define   `xml:"define"`                         // Define patterns in this div
	Divs            []Div      `xml:"div"`                            // Nested divs
	RawAttrs        []xml.Attr `xml:",any,attr"`
}

// Include represents a RELAX NG include element for including external schema definitions.
type Include struct {
	Href       string     `xml:"href,attr"`
	Base       string     `xml:"http://www.w3.org/XML/1998/namespace base,attr"` // xml:base attribute
	Ns         string     `xml:"ns,attr,omitempty"`                              // ns attribute - can be inherited by elements
	Defines    []Define   `xml:"define"`
	Start      []Start    `xml:"start"` // Override start from included file
	RawAttrs   []xml.Attr `xml:",any,attr"`
	RawContent []byte     `xml:",innerxml"`
}

// ExternalRef represents a RELAX NG externalRef element for referencing external schema definitions.
type ExternalRef struct {
	Href       string     `xml:"href,attr"`
	Base       string     `xml:"http://www.w3.org/XML/1998/namespace base,attr"` // xml:base attribute
	Ns         string     `xml:"ns,attr,omitempty"`                              // ns attribute (transferred to referenced element per spec 4.6)
	RawAttrs   []xml.Attr `xml:",any,attr"`
	RawContent []byte     `xml:",innerxml"`
}

// Start represents a RELAX NG start element defining the root pattern of the schema.
type Start struct {
	Name            string       `xml:"name,attr,omitempty"`            // Obsolete attribute
	Combine         string       `xml:"combine,attr,omitempty"`         // "choice" or "interleave"
	DatatypeLibrary string       `xml:"datatypeLibrary,attr,omitempty"` // datatype library inherited by children
	Ref             *Ref         `xml:"ref,omitempty"`
	ParentRef       *Ref         `xml:"parentRef,omitempty"` // parentRef is same structure as ref
	Element         *Element     `xml:"element,omitempty"`
	Choice          *Choice      `xml:"choice,omitempty"`
	Group           []Group      `xml:"group,omitempty"`
	Interleave      []Interleave `xml:"interleave,omitempty"`
	Optional        []Optional   `xml:"optional,omitempty"`
	OneOrMore       []OneOrMore  `xml:"oneOrMore,omitempty"`
	ZeroOrMore      []ZeroOrMore `xml:"zeroOrMore,omitempty"`
	Text            *Text        `xml:"text,omitempty"`
	Data            *Data        `xml:"data,omitempty"`
	List            *List        `xml:"list,omitempty"`
	Empty           *Empty       `xml:"empty,omitempty"`
	NotAllowed      *NotAllowed  `xml:"notAllowed,omitempty"`
	ExternalRef     *ExternalRef `xml:"externalRef,omitempty"`
	RawAttrs        []xml.Attr   `xml:",any,attr"`
	RawContent      []byte       `xml:",innerxml"`
}

// Define represents a RELAX NG define element that defines a named pattern.
type Define struct {
	Name            string       `xml:"name,attr"`
	Combine         string       `xml:"combine,attr,omitempty"`         // "choice" or "interleave"
	DatatypeLibrary string       `xml:"datatypeLibrary,attr,omitempty"` // datatype library inherited by children
	Elements        []Element    `xml:"element,omitempty"`              // Direct element children (patterns)
	Element         *Element     `xml:"-"`                              // Deprecated: use Elements and FirstElement() instead
	Choice          *Choice      `xml:"choice,omitempty"`               // Choice pattern (used after combine merging)
	Group           []Group      `xml:"group,omitempty"`                // Group pattern for multiple element children
	Interleave      []Interleave `xml:"interleave,omitempty"`           // Interleave pattern (used after combine merging)
	Optional        []Optional   `xml:"optional,omitempty"`             // Optional pattern
	OneOrMore       []OneOrMore  `xml:"oneOrMore,omitempty"`            // OneOrMore pattern
	ZeroOrMore      []ZeroOrMore `xml:"zeroOrMore,omitempty"`           // ZeroOrMore pattern
	Ref             *Ref         `xml:"ref,omitempty"`                  // Added to support unpacking nested grammars
	ParentRef       *Ref         `xml:"parentRef,omitempty"`            // Added to support unpacking nested grammars
	Text            *Text        `xml:"text,omitempty"`                 // Text pattern
	Data            *Data        `xml:"data,omitempty"`                 // Data pattern
	List            *List        `xml:"list,omitempty"`                 // List pattern
	Empty           *Empty       `xml:"empty,omitempty"`                // Empty pattern
	NotAllowed      *NotAllowed  `xml:"notAllowed,omitempty"`           // NotAllowed pattern
	ExternalRef     *ExternalRef `xml:"externalRef,omitempty"`          // ExternalRef pattern
	RawContent      []byte       `xml:",innerxml"`
	RawAttrs        []xml.Attr   `xml:",any,attr"`
}

// FirstElement returns the first element if available, for backward compatibility
func (d *Define) FirstElement() *Element {
	if d.Element != nil {
		return d.Element
	}
	if len(d.Elements) > 0 {
		return &d.Elements[0]
	}
	// If elements are wrapped in a group (implicit sequence), get the first element from the group
	if len(d.Group) > 0 && len(d.Group[0].Elements) > 0 {
		return &d.Group[0].Elements[0]
	}
	return nil
}

// Element represents a RELAX NG element pattern that matches XML elements.
type Element struct {
	Name               string        `xml:"name,attr"`
	Ns                 string        `xml:"ns,attr"`
	DatatypeLibrary    string        `xml:"datatypeLibrary,attr,omitempty"`
	RawAttrs           []xml.Attr    `xml:",any,attr"`
	RawContent         []byte        `xml:",innerxml"`
	NameElement        *NameElement  `xml:"name,omitempty"` // <name> child element (name class)
	Text               *Text         `xml:"text,omitempty"`
	Attributes         []Attribute   `xml:"attribute,omitempty"`
	Values             []Value       `xml:"value,omitempty"`   // <value> child elements
	Elements           []Element     `xml:"element,omitempty"` // Nested element children (pattern)
	Optional           []Optional    `xml:"optional,omitempty"`
	OneOrMore          []OneOrMore   `xml:"oneOrMore,omitempty"`
	ZeroOrMore         []ZeroOrMore  `xml:"zeroOrMore,omitempty"`
	Choice             *Choice       `xml:"choice,omitempty"`
	Ref                []Ref         `xml:"ref,omitempty"`
	ParentRef          []Ref         `xml:"parentRef,omitempty"` // parentRef uses same structure as ref
	Group              []Group       `xml:"group,omitempty"`
	Interleave         []Interleave  `xml:"interleave,omitempty"`
	Mixed              *Mixed        `xml:"mixed,omitempty"`
	Empty              *Empty        `xml:"empty,omitempty"`
	NotAllowed         *NotAllowed   `xml:"notAllowed,omitempty"`
	Data               *Data         `xml:"data,omitempty"`
	List               *List         `xml:"list,omitempty"`
	AnyName            *AnyName      `xml:"anyName,omitempty"`
	NsName             *NsName       `xml:"nsName,omitempty"`
	ExternalRef        *ExternalRef  `xml:"externalRef,omitempty"`
	ObsoleteNot        []interface{} `xml:"not"`        // Capture obsolete "not" element
	ObsoleteDifference []interface{} `xml:"difference"` // Capture obsolete "difference" element
	ObsoleteKey        []interface{} `xml:"key"`        // Capture obsolete "key" element
	ObsoleteKeyRef     []interface{} `xml:"keyRef"`     // Capture obsolete "keyRef" element
}

// Attribute represents a RELAX NG attribute pattern that matches XML attributes.
type Attribute struct {
	Name            string       `xml:"name,attr"`
	Ns              string       `xml:"ns,attr"`
	DatatypeLibrary string       `xml:"datatypeLibrary,attr,omitempty"` // datatype library inherited by children
	RawAttrs        []xml.Attr   `xml:",any,attr"`
	RawContent      []byte       `xml:",innerxml"`      // Captures all inner XML for validation
	NameElement     *NameElement `xml:"name,omitempty"` // <name> child element (name class)
	Choice          *Choice      `xml:"choice,omitempty"`
	Values          []Value      `xml:"value,omitempty"`
	Data            *Data        `xml:"data,omitempty"`
	Empty           *Empty       `xml:"empty,omitempty"`
	List            *List        `xml:"list,omitempty"`
	AnyName         *AnyName     `xml:"anyName,omitempty"`
	NsName          *NsName      `xml:"nsName,omitempty"`
	Text            *Text        `xml:"text,omitempty"`
}

// Optional represents a RELAX NG optional (0 or 1) element pattern.
type Optional struct {
	Ns              string       `xml:"ns,attr,omitempty"`                              // ns attribute - can be inherited by externalRef
	Base            string       `xml:"http://www.w3.org/XML/1998/namespace base,attr"` // xml:base attribute
	DatatypeLibrary string       `xml:"datatypeLibrary,attr,omitempty"`                 // datatype library inherited by children
	RawContent      []byte       `xml:",innerxml"`
	Attributes      []Attribute  `xml:"attribute,omitempty"`
	Elements        []Element    `xml:"element,omitempty"`
	Ref             []Ref        `xml:"ref,omitempty"`
	ParentRef       []Ref        `xml:"parentRef,omitempty"` // parentRef uses same structure as ref
	AnyName         *AnyName     `xml:"anyName,omitempty"`
	NsName          *NsName      `xml:"nsName,omitempty"`
	Text            *Text        `xml:"text,omitempty"`
	List            *List        `xml:"list,omitempty"`
	ExternalRef     *ExternalRef `xml:"externalRef,omitempty"`
}

// OneOrMore represents a RELAX NG oneOrMore (1 or more) element pattern.
type OneOrMore struct {
	Ns              string       `xml:"ns,attr,omitempty"`                              // ns attribute - can be inherited by externalRef
	Base            string       `xml:"http://www.w3.org/XML/1998/namespace base,attr"` // xml:base attribute
	DatatypeLibrary string       `xml:"datatypeLibrary,attr,omitempty"`                 // datatype library inherited by children
	RawContent      []byte       `xml:",innerxml"`
	Ref             []Ref        `xml:"ref,omitempty"`
	Element         []Element    `xml:"element,omitempty"`
	Attribute       []Attribute  `xml:"attribute,omitempty"`
	Value           []Value      `xml:"value,omitempty"` // Value patterns (used in list context)
	Data            []Data       `xml:"data,omitempty"`  // Data patterns (can be multiple in list context)
	Choice          *Choice      `xml:"choice,omitempty"`
	Group           []Group      `xml:"group,omitempty"`
	Interleave      []Interleave `xml:"interleave,omitempty"`
	Text            *Text        `xml:"text,omitempty"`
	List            *List        `xml:"list,omitempty"`
	AnyName         *AnyName     `xml:"anyName,omitempty"`
	NsName          *NsName      `xml:"nsName,omitempty"`
	ExternalRef     *ExternalRef `xml:"externalRef,omitempty"`
}

// ZeroOrMore represents a RELAX NG zeroOrMore (0 or more) element pattern.
type ZeroOrMore struct {
	Ns              string       `xml:"ns,attr,omitempty"`                              // ns attribute - can be inherited by externalRef
	Base            string       `xml:"http://www.w3.org/XML/1998/namespace base,attr"` // xml:base attribute
	DatatypeLibrary string       `xml:"datatypeLibrary,attr,omitempty"`                 // datatype library inherited by children
	RawContent      []byte       `xml:",innerxml"`
	Ref             []Ref        `xml:"ref,omitempty"`
	Element         []Element    `xml:"element,omitempty"`
	Attribute       []Attribute  `xml:"attribute,omitempty"`
	Value           []Value      `xml:"value,omitempty"` // Value patterns (used in list context)
	Data            []Data       `xml:"data,omitempty"`  // Data patterns (can be multiple in list context)
	Choice          *Choice      `xml:"choice,omitempty"`
	Group           []Group      `xml:"group,omitempty"`
	Interleave      []Interleave `xml:"interleave,omitempty"`
	Text            *Text        `xml:"text,omitempty"`
	List            *List        `xml:"list,omitempty"`
	AnyName         *AnyName     `xml:"anyName,omitempty"`
	NsName          *NsName      `xml:"nsName,omitempty"`
	ExternalRef     *ExternalRef `xml:"externalRef,omitempty"`
}

// Choice represents a RELAX NG choice (one of) element pattern.
type Choice struct {
	Ns              string        `xml:"ns,attr,omitempty"`                              // ns attribute - can be inherited by externalRef
	Base            string        `xml:"http://www.w3.org/XML/1998/namespace base,attr"` // xml:base attribute
	DatatypeLibrary string        `xml:"datatypeLibrary,attr,omitempty"`                 // datatype library inherited by children
	RawContent      []byte        `xml:",innerxml"`
	Elements        []Element     `xml:"element,omitempty"`
	Attributes      []Attribute   `xml:"attribute,omitempty"`
	NameElements    []NameElement `xml:"name,omitempty"` // <name> elements for name classes
	Values          []Value       `xml:"value,omitempty"`
	Refs            []Ref         `xml:"ref,omitempty"`
	Data            []Data        `xml:"data,omitempty"`
	Text            *Text         `xml:"text,omitempty"`
	Empty           *Empty        `xml:"empty,omitempty"`
	NotAllowed      *NotAllowed   `xml:"notAllowed,omitempty"`
	Group           []Group       `xml:"group,omitempty"`
	Interleave      []Interleave  `xml:"interleave,omitempty"`
	List            *List         `xml:"list,omitempty"`
	Mixed           *Mixed        `xml:"mixed,omitempty"`
	ExternalRef     *ExternalRef  `xml:"externalRef,omitempty"`
}

// Value represents a RELAX NG value element for specifying literal values.
type Value struct {
	Value           string `xml:",chardata"`
	Type            string `xml:"type,attr,omitempty"`            // Data type for value (default: token)
	DatatypeLibrary string `xml:"datatypeLibrary,attr,omitempty"` // datatype library (can be inherited from parent)
}

// Text represents a RELAX NG text element pattern.
type Text struct {
	RawAttrs   []xml.Attr `xml:",any,attr"`
	RawContent []byte     `xml:",innerxml"`
}

// Ref represents a RELAX NG ref element that references a named pattern.
type Ref struct {
	Name       string `xml:"name,attr"`
	RawContent []byte `xml:",innerxml"`
}

// Group represents a RELAX NG group (sequence) element pattern.
type Group struct {
	Ns              string       `xml:"ns,attr,omitempty"`                              // ns attribute - can be inherited by externalRef
	Base            string       `xml:"http://www.w3.org/XML/1998/namespace base,attr"` // xml:base attribute
	DatatypeLibrary string       `xml:"datatypeLibrary,attr,omitempty"`                 // datatype library inherited by children
	RawContent      []byte       `xml:",innerxml"`
	Elements        []Element    `xml:"element,omitempty"`
	Attributes      []Attribute  `xml:"attribute,omitempty"`
	Ref             []Ref        `xml:"ref,omitempty"`
	Value           []Value      `xml:"value,omitempty"` // Value patterns (used in list context)
	Data            []Data       `xml:"data,omitempty"`  // Data patterns (can be multiple in list context)
	Optional        []Optional   `xml:"optional,omitempty"`
	OneOrMore       []OneOrMore  `xml:"oneOrMore,omitempty"`
	ZeroOrMore      []ZeroOrMore `xml:"zeroOrMore,omitempty"`
	Choice          []Choice     `xml:"choice,omitempty"`
	Group           []Group      `xml:"group,omitempty"`
	Interleave      []Interleave `xml:"interleave,omitempty"`
	Text            *Text        `xml:"text,omitempty"`
	List            *List        `xml:"list,omitempty"`
	NotAllowed      *NotAllowed  `xml:"notAllowed,omitempty"`
	ExternalRef     *ExternalRef `xml:"externalRef,omitempty"`
}

// Interleave represents a RELAX NG interleave (any order) element pattern.
type Interleave struct {
	Ns              string       `xml:"ns,attr,omitempty"`                              // ns attribute - can be inherited by externalRef
	Base            string       `xml:"http://www.w3.org/XML/1998/namespace base,attr"` // xml:base attribute
	DatatypeLibrary string       `xml:"datatypeLibrary,attr,omitempty"`                 // datatype library inherited by children
	RawContent      []byte       `xml:",innerxml"`
	Elements        []Element    `xml:"element,omitempty"`
	Attributes      []Attribute  `xml:"attribute,omitempty"`
	Ref             []Ref        `xml:"ref,omitempty"`
	Value           []Value      `xml:"value,omitempty"` // Value patterns (used in list context)
	Data            *Data        `xml:"data,omitempty"`  // Data pattern (used in list context)
	Optional        []Optional   `xml:"optional,omitempty"`
	Group           []Group      `xml:"group,omitempty"`
	Choice          []Choice     `xml:"choice,omitempty"`
	OneOrMore       []OneOrMore  `xml:"oneOrMore,omitempty"`
	ZeroOrMore      []ZeroOrMore `xml:"zeroOrMore,omitempty"`
	Text            *Text        `xml:"text,omitempty"`
	List            *List        `xml:"list,omitempty"`
	NotAllowed      *NotAllowed  `xml:"notAllowed,omitempty"`
	ExternalRef     *ExternalRef `xml:"externalRef,omitempty"`
}

// Mixed represents a RELAX NG mixed (text and elements) element pattern.
type Mixed struct {
	Ns              string       `xml:"ns,attr,omitempty"`              // ns attribute - can be inherited by externalRef
	DatatypeLibrary string       `xml:"datatypeLibrary,attr,omitempty"` // datatype library inherited by children
	RawContent      []byte       `xml:",innerxml"`
	Elements        []Element    `xml:"element,omitempty"`
	Ref             []Ref        `xml:"ref,omitempty"`
	Optional        []Optional   `xml:"optional,omitempty"`
	Group           []Group      `xml:"group,omitempty"`
	OneOrMore       []OneOrMore  `xml:"oneOrMore,omitempty"`
	ZeroOrMore      []ZeroOrMore `xml:"zeroOrMore,omitempty"`
	Choice          []Choice     `xml:"choice,omitempty"`
	NotAllowed      *NotAllowed  `xml:"notAllowed,omitempty"`
	ExternalRef     *ExternalRef `xml:"externalRef,omitempty"`
}

// Data represents a RELAX NG data element for specifying datatypes.
type Data struct {
	Type            string      `xml:"type,attr"`
	DatatypeLibrary string      `xml:"datatypeLibrary,attr,omitempty"`
	RawAttrs        []xml.Attr  `xml:",any,attr"`
	RawContent      []byte      `xml:",innerxml"`
	Params          []Param     `xml:"param,omitempty"`
	Except          *DataExcept `xml:"except,omitempty"`
}

// Param represents a RELAX NG param element for specifying datatype parameters.
type Param struct {
	Name  string `xml:"name,attr"`
	Value string `xml:",chardata"`
}

// DataExcept represents a RELAX NG except element within data patterns.
type DataExcept struct {
	RawContent []byte  `xml:",innerxml"`
	Values     []Value `xml:"value,omitempty"`
	Data       []Data  `xml:"data,omitempty"`
	Choice     *Choice `xml:"choice,omitempty"` // except can contain choice per spec 7.1.4
}

// List represents a RELAX NG list element for specifying whitespace-separated values.
type List struct {
	Data       *Data      `xml:"data,omitempty"`
	OneOrMore  *OneOrMore `xml:"oneOrMore,omitempty"`
	Choice     *Choice    `xml:"choice,omitempty"`
	Group      *Group     `xml:"group,omitempty"` // Group pattern in list
	Values     []Value    `xml:"value,omitempty"` // Value patterns in list
	Empty      *Empty     `xml:"empty,omitempty"` // Empty pattern in list
	RawContent []byte     `xml:",innerxml"`
}

// Empty represents a RELAX NG empty element pattern.
type Empty struct {
	RawAttrs   []xml.Attr `xml:",any,attr"`
	RawContent []byte     `xml:",innerxml"`
}

// NotAllowed represents a RELAX NG notAllowed element pattern.
type NotAllowed struct {
	RawAttrs   []xml.Attr `xml:",any,attr"`
	RawContent []byte     `xml:",innerxml"`
}

// AnyName represents a RELAX NG anyName element for name class patterns.
type AnyName struct {
	RawContent []byte      `xml:",innerxml"`
	Except     *NameExcept `xml:"except,omitempty"`
}

// NsName represents a RELAX NG nsName element for namespace-specific name classes.
type NsName struct {
	Ns         string      `xml:"ns,attr"`
	RawContent []byte      `xml:",innerxml"`
	Except     *NameExcept `xml:"except,omitempty"`
}

// NameExcept represents a RELAX NG except element within name class patterns.
type NameExcept struct {
	RawContent []byte       `xml:",innerxml"`
	Names      []NameChoice `xml:"name,omitempty"`
	NsName     *NsName      `xml:"nsName,omitempty"`
	AnyName    *AnyName     `xml:"anyName,omitempty"`
}

// NameChoice represents a RELAX NG name element used as a choice in name class patterns.
type NameChoice struct {
	Value string `xml:",chardata"`
	Ns    string `xml:"ns,attr"` // Namespace attribute from <name ns="...">
}

// NameElement represents a <name> element used as a name class (child of element or attribute)
type NameElement struct {
	Value     string `xml:",chardata"`
	Ns        string `xml:"ns,attr"` // Explicit namespace from ns attribute
	Namespace string // Resolved namespace URI (not in XML, computed)
	LocalName string // Local name without prefix (not in XML, computed)
}

// RELAX NG pattern element names
// NOTE: grammar is NOT a valid pattern - it's only the root wrapper element
// parentRef is a pattern (used in includes)
var rngPatternNames = map[string]bool{
	"element": true, "ref": true, "group": true, elemNameInterleave: true, "mixed": true,
	"list": true, "data": true, "choice": true, "oneOrMore": true, "zeroOrMore": true,
	"optional": true, "text": true, "empty": true, "notAllowed": true, "externalRef": true,
	"parentRef": true,
}

// Name class and attribute element names (not content patterns)
var nameClassOrAttr = map[string]bool{
	"attribute": true, "name": true, "anyName": true, "nsName": true,
}

// isValidNCName validates that a string is a valid XML NCName according to XML 1.1 spec.
// NCName = (Letter | '_') (NameChar)*
// NameChar = Letter | Digit | '.' | '-' | '_' | ':' | CombiningChar | Extender
// But ':' is restricted - it can only appear as a namespace separator if there's a valid prefix and local part
func isValidNCName(name string) bool {
	if name == "" {
		return false
	}

	runes := []rune(name)

	// First character must be Letter or underscore
	first := runes[0]
	if !isXMLNameStartChar(first) {
		return false
	}

	// Rest must be valid NameChars
	for i := 1; i < len(runes); i++ {
		if !isXMLNameChar(runes[i]) {
			return false
		}
	}

	// If name contains colon, validate it's a valid QName (prefix:localName)
	if strings.Contains(name, ":") {
		parts := strings.Split(name, ":")
		// Only allow single colon
		if len(parts) != 2 {
			return false
		}
		// Both prefix and local name must be non-empty NCNames
		if parts[0] == "" || parts[1] == "" {
			return false
		}
		// Validate prefix and local name separately (recursive, but with no colons)
		if !isValidNCNameNoColon(parts[0]) || !isValidNCNameNoColon(parts[1]) {
			return false
		}
	}

	return true
}

// isValidNCNameNoColon validates a true NCName without colons.
// Used for validating ref and define names in RELAX NG, which must be NCNames, not QNames.
func isValidNCNameNoColon(name string) bool {
	if name == "" {
		return false
	}

	runes := []rune(name)

	// First character must be Letter or underscore
	first := runes[0]
	if !isXMLNameStartChar(first) {
		return false
	}

	// Rest must be valid NameChars but NO colons
	for i := 1; i < len(runes); i++ {
		ch := runes[i]
		if ch == ':' || !isXMLNameChar(ch) {
			return false
		}
	}

	return true
}

// isXMLNameStartChar checks if a rune is a valid start character for an XML Name
// According to XML 1.1 spec: Letter | '_'
func isXMLNameStartChar(r rune) bool {
	return r == '_' || isXMLLetter(r)
}

// isXMLNameChar checks if a rune is a valid NameChar
// According to XML 1.1 spec: Letter | Digit | '.' | '-' | '_' | ':' | CombiningChar | Extender
func isXMLNameChar(r rune) bool {
	// NameChar = NameStartChar | Digit | '.' | '-' | CombiningChar | Extender
	// Where CombiningChar includes marks (Mn, Mc, Me) and other combining categories
	return isXMLNameStartChar(r) || unicode.IsDigit(r) || r == '.' || r == '-' || r == ':' ||
		unicode.IsMark(r) // Includes Mn (nonspacing mark), Mc (spacing mark), Me (enclosing mark)
}

// isXMLLetter checks if a rune is a valid XML Letter according to Unicode categories
// XML 1.1 Letter = Lu | Ll | Lt | Lm | Lo | Nl
func isXMLLetter(r rune) bool {
	// Use unicode.IsLetter() which covers most letter categories
	return unicode.IsLetter(r)
}

// isValidDatatypeLibraryURI validates that a string is a valid datatypeLibrary URI
// According to RELAX NG spec (Section 8.1):
// - Must be an absolute URI (per RFC 2396)
// - Scheme must not start with underscore
// - Must not contain fragment identifier (#)
// - Percent encoding must be valid (%xx where xx are hex digits)
func isValidDatatypeLibraryURI(uri string) bool {
	if uri == "" {
		return false
	}

	// Must contain a colon (scheme:...)
	colonIdx := strings.Index(uri, ":")
	if colonIdx <= 0 {
		// No scheme, relative URI - invalid
		return false
	}

	scheme := uri[:colonIdx]

	// Scheme must start with letter
	if len(scheme) == 0 || !unicode.IsLetter(rune(scheme[0])) {
		return false
	}

	// Scheme can only contain letters, digits, '+', '-', '.'
	for _, r := range scheme {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '+' && r != '-' && r != '.' {
			return false
		}
	}

	// Must not contain fragment identifier
	if strings.Contains(uri, "#") {
		return false
	}

	// Validate percent encoding - must be %xx where xx are hex digits
	for i := 0; i < len(uri); i++ {
		if uri[i] == '%' {
			if i+2 >= len(uri) {
				// Not enough characters after %
				return false
			}
			// Next two characters must be hex digits
			hex := uri[i+1 : i+3]
			for _, c := range hex {
				if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
					return false
				}
			}
			i += 2 // Skip the two hex digits
		}
	}

	return true
}

// firstLevelLocalNames extracts the local names of first-level child elements from raw XML content
func firstLevelLocalNames(raw []byte) ([]string, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}

	dec := xml.NewDecoder(bytes.NewReader(raw))
	depth := 0
	var names []string

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		switch t := tok.(type) {
		case xml.StartElement:
			if depth == 0 {
				// First level element - record its local name
				names = append(names, t.Name.Local)
			}
			depth++
		case xml.EndElement:
			depth--
		}
		// Ignore CharData, Comment, ProcInst, Directive
	}

	return names, nil
}

// countElementContentPatterns counts content patterns in raw XML content (excludes attributes and name classes)
func countElementContentPatterns(raw []byte) (int, []string, error) {
	names, err := firstLevelLocalNames(raw)
	if err != nil {
		return 0, nil, err
	}

	var contentPatterns []string
	for _, name := range names {
		// Skip name classes and attributes - they're not content patterns
		if nameClassOrAttr[name] {
			continue
		}
		// Count RELAX NG pattern elements
		if rngPatternNames[name] {
			contentPatterns = append(contentPatterns, name)
		}
		// Unknown local names (likely annotations/foreign namespace) are ignored
	}

	return len(contentPatterns), contentPatterns, nil
}

// hasAnyRNGChild checks if raw content has any RELAX NG child elements
func hasAnyRNGChild(raw []byte) (bool, []string, error) {
	names, err := firstLevelLocalNames(raw)
	if err != nil {
		return false, nil, err
	}

	var rngChildren []string
	for _, name := range names {
		if rngPatternNames[name] || nameClassOrAttr[name] {
			rngChildren = append(rngChildren, name)
		}
	}

	return len(rngChildren) > 0, rngChildren, nil
}

// ParseSchema parses a RELAX NG schema from an io.Reader and returns the Grammar structure.
// It handles basic schema elements but does not process includes - use ParseSchemaFile for that.
func ParseSchema(r io.Reader) (*Grammar, error) {
	var grammar Grammar
	decoder := xml.NewDecoder(r)
	if err := decoder.Decode(&grammar); err != nil {
		return nil, err
	}

	// Per spec section 4.18: nested grammar elements are not allowed in standard syntax
	// (they're a simplified syntax feature not used in full RELAX NG)
	// Skip unpacking - validation will catch any nested grammars

	// Per spec section 4.11: flatten div elements before validation
	// Divs need to be processed first as they can contain start/define elements
	grammar.flattenDivs()

	// Validate grammar structure
	if err := grammar.Validate(); err != nil {
		return nil, err
	}

	// Per RELAX NG spec section 4.5 and 4.6:
	// Schemas with <include> or <externalRef> elements cannot be parsed without a ResourceResolver
	// because the spec requires validating that included/external files are valid RELAX NG documents.
	// Use ParseSchemaWithResolver to parse schemas with includes/external refs.
	if len(grammar.Includes) > 0 {
		return nil, fmt.Errorf("schemas with <include> elements must be parsed with ParseSchemaWithResolver, not ParseSchema")
	}
	if len(grammar.ExternalRefs) > 0 {
		return nil, fmt.Errorf("schemas with <externalRef> elements must be parsed with ParseSchemaWithResolver, not ParseSchema")
	}

	// For standalone schemas without includes/externalRefs, validate refs immediately
	// For schemas with includes, ref validation happens in parseSchemaWithResolverInternal
	// where all defines from included files are available
	if len(grammar.Includes) == 0 && len(grammar.ExternalRefs) == 0 {
		// Build map of define names in this grammar
		defineNames := make(map[string]bool)
		for _, def := range grammar.Defines {
			defineNames[def.Name] = true
		}
		// Validate refs point to existing defines
		if err := grammar.validateRefs(defineNames); err != nil {
			return nil, err
		}
	}

	// Post-process: synthesize implicit choice patterns from multiple pattern children
	grammar.synthesizeImplicitPatterns()

	return &grammar, nil
}

// Validate checks that the grammar has a valid structure.
// A grammar element must not contain both top-level patterns and a <start> element.
// Top-level patterns (element, choice, group, ref, attribute) are only valid in simplified syntax (no <grammar> wrapper).
func (g *Grammar) Validate() error {
	return g.ValidateAsLibrary(false)
}

// ValidateAsLibrary validates a grammar, optionally allowing library grammars without start elements
func (g *Grammar) ValidateAsLibrary(isLibrary bool) error {
	// Check for invalid direct patterns in grammar
	if err := g.validateGrammarDirectPatterns(); err != nil {
		return err
	}

	// Validate start element structure
	if err := g.validateStartStructureWithFlag(isLibrary); err != nil {
		return err
	}

	// Validate Include elements
	if err := g.validateIncludes(); err != nil {
		return err
	}

	// Normalize and resolve names before other validations
	if err := g.normalizeAndResolveNames(); err != nil {
		return err
	}

	// Check for duplicate defines if no includes/externalRefs
	if err := g.checkDuplicateDefinesIfNoIncludes(); err != nil {
		return err
	}

	// Merge definitions and starts with combine attributes
	if err := g.mergeDefinesAndStarts(); err != nil {
		return err
	}

	// Validate patterns and name class constraints
	if err := g.validatePatterns(); err != nil {
		return err
	}

	if err := g.validateSection416NameClassConstraints(); err != nil {
		return err
	}

	return nil
}

// validateGrammarDirectPatterns checks for invalid direct patterns in grammar
func (g *Grammar) validateGrammarDirectPatterns() error {
	if len(g.Elements) > 0 {
		return fmt.Errorf("grammar element cannot contain direct <element> children - use <define> with <start> instead")
	}
	if len(g.Choices) > 0 {
		return fmt.Errorf("grammar element cannot contain direct <choice> children")
	}
	if len(g.Groups) > 0 {
		return fmt.Errorf("grammar element cannot contain direct <group> children")
	}
	if len(g.Refs) > 0 {
		return fmt.Errorf("grammar element cannot contain direct <ref> children")
	}
	if len(g.Attrs) > 0 {
		return fmt.Errorf("grammar element cannot contain direct <attribute> children")
	}
	return nil
}

// normalizeAndResolveNames normalizes whitespace, resolves QNames, and propagates datatypeLibrary
func (g *Grammar) normalizeAndResolveNames() error {
	// Normalize whitespace in names FIRST (before validation)
	// Per RELAX NG Section 4.2: "Leading and trailing whitespace characters are removed
	// from the value of each name, type and combine attribute"
	g.normalizeWhitespace()

	// Resolve QNames in element and attribute names using namespace declarations
	// Per RELAX NG spec: QNames are resolved using the namespace context from xmlns declarations
	g.resolveQNames()

	// Normalize name attributes (trim whitespace)
	g.normalizeNames()

	// Propagate datatypeLibrary from parent elements to child Data and Value elements
	g.propagateDatatypeLibrary()

	// Unpack nested grammars before validation
	if err := g.unpackNestedGrammars(); err != nil {
		return err
	}

	// Normalize names again after unpacking
	g.normalizeNames()

	// Propagate datatypeLibrary again after unpacking nested grammars
	g.propagateDatatypeLibrary()

	return nil
}

// checkDuplicateDefinesIfNoIncludes validates no duplicate defines if schema has no includes/externalRefs
func (g *Grammar) checkDuplicateDefinesIfNoIncludes() error {
	if len(g.Includes) == 0 && len(g.ExternalRefs) == 0 {
		if err := g.checkDuplicateDefines(); err != nil {
			return err
		}
	}
	return nil
}

// mergeDefinesAndStarts merges defines and starts with combine attributes
func (g *Grammar) mergeDefinesAndStarts() error {
	// Merge defines with combine attributes
	if err := g.mergeDefinesWithCombine(); err != nil {
		return err
	}

	// Merge start elements with combine attributes
	if err := g.mergeStartsWithCombine(); err != nil {
		return err
	}

	return nil
}

// validateStartStructureWithFlag validates the structure of the start element with library flag
//
//nolint:funlen
func (g *Grammar) validateStartStructureWithFlag(isLibrary bool) error {
	// Per RELAX NG spec section 4.18: grammar must have a start element
	// EXCEPT for library grammars that are included by other grammars
	// Check if start has any structured content (not just RawContent, which might be unparsed)
	hasStartContent := g.Start.Ref != nil || g.Start.Element != nil ||
		g.Start.Choice != nil || len(g.Start.Group) > 0 ||
		len(g.Start.Interleave) > 0 || len(g.Start.Optional) > 0 ||
		len(g.Start.OneOrMore) > 0 || len(g.Start.ZeroOrMore) > 0 ||
		g.Start.Text != nil || g.Start.Data != nil || g.Start.List != nil ||
		g.Start.Empty != nil || g.Start.NotAllowed != nil ||
		g.Start.ExternalRef != nil || g.Start.ParentRef != nil

	hasRawContent := len(bytes.TrimSpace(g.Start.RawContent)) > 0
	hasStartAttributes := g.Start.Combine != "" || g.Start.Name != "" || len(g.Start.RawAttrs) > 0

	// Track whether the grammar explicitly has a start element tag
	hasStartElement := hasStartContent || hasRawContent || hasStartAttributes

	// A library grammar can omit the start element entirely
	if isLibrary {
		// Library grammars can have just defines
		return nil
	}

	// A standalone grammar must have a start element with content
	// However, grammars with includes/externalRefs that don't provide a start can be valid too
	if !hasStartElement && len(g.Includes) == 0 && len(g.ExternalRefs) == 0 {
		return fmt.Errorf("grammar must have a start element")
	}

	// If start element exists but is empty, check if includes/externalRefs might provide content
	if !hasStartContent && !hasRawContent && hasStartElement {
		// Allow empty start if there are includes or external refs that might provide content
		if len(g.Includes) == 0 && len(g.ExternalRefs) == 0 {
			return fmt.Errorf("start element is empty (grammar must have a start element with content)")
		}
	}

	// Start cannot have both ref and element
	if g.Start.Ref != nil && g.Start.Element != nil {
		return fmt.Errorf("start element cannot have both ref and element children")
	}

	// Per RELAX NG spec section 3.2: start must contain an element pattern or ref pattern
	// It cannot have attribute, data, value, text, list, group, interleave, oneOrMore, zeroOrMore, empty, etc.
	if g.Start.Data != nil {
		return fmt.Errorf("start element cannot contain data patterns directly (spec section 3.2)")
	}
	if g.Start.List != nil {
		return fmt.Errorf("start element cannot contain list patterns directly (spec section 3.2)")
	}
	if g.Start.Empty != nil {
		return fmt.Errorf("start element cannot contain empty patterns directly (spec section 3.2)")
	}
	if len(g.Start.Group) > 0 {
		return fmt.Errorf("start element cannot contain group patterns directly (spec section 3.2)")
	}
	if len(g.Start.Interleave) > 0 {
		return fmt.Errorf("start element cannot contain interleave patterns directly (spec section 3.2)")
	}
	if len(g.Start.Optional) > 0 {
		return fmt.Errorf("start element cannot contain optional patterns directly (spec section 3.2)")
	}
	if len(g.Start.OneOrMore) > 0 {
		return fmt.Errorf("start element cannot contain oneOrMore patterns directly (spec section 3.2)")
	}
	if len(g.Start.ZeroOrMore) > 0 {
		return fmt.Errorf("start element cannot contain zeroOrMore patterns directly (spec section 3.2)")
	}
	if g.Start.Text != nil {
		return fmt.Errorf("start element cannot contain text patterns directly (spec section 3.2)")
	}
	// Check for value patterns in start (should not be allowed at top level)
	if hasValueInStart(&g.Start) {
		return fmt.Errorf("start element cannot contain value patterns directly (spec section 3.2)")
	}
	// Check for attribute patterns in start RawContent
	if hasAttributeInStart(&g.Start) {
		// Check if this is an attribute nested inside an element's content patterns
		// Attributes inside element->choice or element->oneOrMore are allowed
		// Attributes as direct children of element are also allowed
		if g.Start.Element == nil || !elementHasAttributeInContent(g.Start.Element) {
			return fmt.Errorf("start element cannot contain attribute patterns directly (spec section 3.2)")
		}
	}
	// Check if choice contains non-element patterns (e.g., data or value mixed with elements)
	if g.Start.Choice != nil {
		if err := validateStartChoicePatterns(g.Start.Choice); err != nil {
			return err
		}
	}

	return nil
}

// hasValueInStart checks if a start contains value patterns as DIRECT children in RawContent
func hasValueInStart(start *Start) bool {
	if len(bytes.TrimSpace(start.RawContent)) == 0 {
		return false
	}

	decoder := xml.NewDecoder(bytes.NewReader(start.RawContent))
	depth := 0 // Track nesting depth
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return false
		}

		switch elem := tok.(type) {
		case xml.StartElement:
			// Only check direct children (depth 0)
			if depth == 0 && elem.Name.Local == elemNameValue {
				return true
			}
			depth++
		case xml.EndElement:
			depth--
		}
	}
	return false
}

// hasAttributeInStart checks if a start contains attribute patterns as DIRECT children in RawContent
// This only checks for top-level attribute elements - attributes nested inside other patterns are allowed
func hasAttributeInStart(start *Start) bool {
	if len(bytes.TrimSpace(start.RawContent)) == 0 {
		return false
	}

	decoder := xml.NewDecoder(bytes.NewReader(start.RawContent))
	depth := 0
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return false
		}

		switch t := tok.(type) {
		case xml.StartElement:
			if depth == 0 && t.Name.Local == "attribute" {
				// Found a direct child attribute element - not allowed
				return true
			}
			depth++
		case xml.EndElement:
			depth--
		}
	}
	return false
}

// validateStartChoicePatterns validates that start's choice only contains element patterns or refs
// Per RELAX NG spec section 3.2, a start choice should only contain element patterns or refs
// It cannot mix non-element patterns (data/value/text/group/interleave/oneOrMore/zeroOrMore/empty) with elements
func validateStartChoicePatterns(choice *Choice) error {
	if choice == nil {
		return nil
	}

	// Check if choice contains data or value patterns mixed with elements
	hasElements := len(choice.Elements) > 0
	hasRefs := len(choice.Refs) > 0
	hasData := len(choice.Data) > 0
	hasValues := len(choice.Values) > 0
	hasText := choice.Text != nil
	hasGroup := len(choice.Group) > 0
	hasInterleave := len(choice.Interleave) > 0
	hasEmpty := choice.Empty != nil
	hasNotAllowed := choice.NotAllowed != nil

	// Count what types of patterns we have
	hasNonElementPatterns := hasData || hasValues || hasText || hasGroup || hasInterleave || hasEmpty || hasNotAllowed

	// If we have non-element patterns, they cannot be mixed with elements or refs
	if hasNonElementPatterns && (hasElements || hasRefs) {
		return fmt.Errorf("start choice cannot mix non-element patterns with element or ref patterns (spec section 3.2)")
	}

	// Also check RawContent for forbidden patterns
	if len(bytes.TrimSpace(choice.RawContent)) > 0 {
		if err := validateStartChoiceContentRestrictions(choice.RawContent); err != nil {
			return err
		}
	}

	return nil
}

// validateStartChoiceContentRestrictions validates raw content of start choice
func validateStartChoiceContentRestrictions(content []byte) error {
	if len(bytes.TrimSpace(content)) == 0 {
		return nil
	}

	decoder := xml.NewDecoder(bytes.NewReader(content))
	var hasElement, hasData, hasValue bool

	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if startElem, ok := tok.(xml.StartElement); ok {
			localName := startElem.Name.Local
			switch localName {
			case elemNameElement:
				hasElement = true
			case "data":
				hasData = true
			case elemNameValue:
				hasValue = true
			}
		}
	}

	// Check for mixing
	if hasData && hasElement {
		return fmt.Errorf("start choice cannot mix data and element patterns (spec section 3.2)")
	}
	if hasValue && hasElement {
		return fmt.Errorf("start choice cannot mix value and element patterns (spec section 3.2)")
	}

	return nil
}

// elementHasAttributeInContent checks if an element has attributes in its content patterns.
// Returns true if attributes are found in element, choice, oneOrMore, or zeroOrMore patterns.
func elementHasAttributeInContent(elem *Element) bool {
	// Attributes can be direct children of element
	if len(elem.Attributes) > 0 {
		return true
	}
	if elem.Choice != nil && len(elem.Choice.Attributes) > 0 {
		return true
	}
	if hasAttributesInOneOrMore(elem.OneOrMore) {
		return true
	}
	if hasAttributesInZeroOrMore(elem.ZeroOrMore) {
		return true
	}
	return false
}

// hasAttributesInOneOrMore checks if any oneOrMore pattern contains attributes.
func hasAttributesInOneOrMore(oneOrMores []OneOrMore) bool {
	for _, one := range oneOrMores {
		if len(one.Attribute) > 0 {
			return true
		}
		if one.Choice != nil && len(one.Choice.Attributes) > 0 {
			return true
		}
		for _, group := range one.Group {
			if len(group.Attributes) > 0 {
				return true
			}
		}
	}
	return false
}

// hasAttributesInZeroOrMore checks if any zeroOrMore pattern contains attributes.
func hasAttributesInZeroOrMore(zeroOrMores []ZeroOrMore) bool {
	for _, zero := range zeroOrMores {
		if len(zero.Attribute) > 0 {
			return true
		}
		if zero.Choice != nil && len(zero.Choice.Attributes) > 0 {
			return true
		}
		for _, group := range zero.Group {
			if len(group.Attributes) > 0 {
				return true
			}
		}
	}
	return false
}

// validateIncludes validates Include elements have required href attribute
func (g *Grammar) validateIncludes() error {
	for _, inc := range g.Includes {
		if inc.Href == "" {
			return fmt.Errorf("include element must have an href attribute")
		}
	}
	return nil
}

// checkDuplicateDefines checks for duplicate define names in the local grammar
func (g *Grammar) checkDuplicateDefines() error {
	// First pass: collect all defines and validate basic properties
	definesByName := make(map[string][]Define)
	for _, def := range g.Defines {
		// Validate define name is present and valid NCName
		if def.Name == "" {
			return fmt.Errorf("define has invalid name (empty)")
		}
		if !isValidNCNameNoColon(def.Name) {
			return fmt.Errorf("define has invalid name '%s' (not a valid NCName)", def.Name)
		}

		// Validate that define has at least one pattern child
		if err := validateDefineContent(&def); err != nil {
			return err
		}

		definesByName[def.Name] = append(definesByName[def.Name], def)
	}

	// Second pass: validate combine attributes for duplicate defines
	if err := validateDuplicateDefinesPass(definesByName); err != nil {
		return err
	}
	return nil
}

// validateDuplicateDefinesPass validates combine attributes for duplicate defines
func validateDuplicateDefinesPass(definesByName map[string][]Define) error {
	// Per RELAX NG spec section 4.17: Multiple defines with same name are allowed if:
	// - At least one has a combine attribute (choice or interleave)
	// - All defines that have a combine attribute must have the same value
	for name, defs := range definesByName {
		if len(defs) > 1 {
			if err := validateDuplicateDefinesCombine(name, defs); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateDuplicateDefinesCombine validates combine attributes for duplicate defines with same name
func validateDuplicateDefinesCombine(name string, defs []Define) error {
	// Check if any define has a combine attribute
	hasCombine := false
	var combineValue string
	for _, def := range defs {
		if def.Combine != "" {
			hasCombine = true
			// Validate combine attribute value
			if def.Combine != elemNameChoice && def.Combine != elemNameInterleave {
				return fmt.Errorf("define '%s' has invalid combine attribute '%s' (must be 'choice' or 'interleave')", name, def.Combine)
			}
			// Check consistency
			if combineValue == "" {
				combineValue = def.Combine
			} else if combineValue != def.Combine {
				return fmt.Errorf("define '%s' has inconsistent combine methods ('%s' vs '%s')", name, combineValue, def.Combine)
			}
		}
	}
	// If no defines have combine attribute, it's an error
	if !hasCombine {
		return fmt.Errorf("duplicate define name '%s' without combine attribute", name)
	}
	return nil
}

// mergeDefinesWithCombine merges multiple defines with the same name using their combine attribute
// Per RELAX NG spec section 4.17: combines patterns using choice or interleave
func (g *Grammar) mergeDefinesWithCombine() error {
	// Group defines by name
	definesByName := make(map[string][]Define)
	for _, def := range g.Defines {
		definesByName[def.Name] = append(definesByName[def.Name], def)
	}

	// Build new list of merged defines (pre-allocate with capacity)
	mergedDefines := make([]Define, 0, len(definesByName))

	for name, defs := range definesByName {
		if len(defs) == 1 {
			// No merging needed
			mergedDefines = append(mergedDefines, defs[0])
			continue
		}

		// Multiple defines - validate and merge them
		combineMethod, err := validateAndNormalizeCombine(name, defs)
		if err != nil {
			return err
		}

		// Create merged define based on combine method
		merged := Define{
			Name:    name,
			Combine: "",
		}

		switch combineMethod {
		case elemNameChoice:
			merged.Choice = mergeDefinesChoice(defs)
		case elemNameInterleave:
			merged.Interleave = []Interleave{mergeDefinesInterleave(defs)}
		}

		mergedDefines = append(mergedDefines, merged)
	}

	// Replace grammar defines with merged version
	g.Defines = mergedDefines
	return nil
}

// validateAndNormalizeCombine validates duplicate defines and returns the combine method
func validateAndNormalizeCombine(name string, defs []Define) (string, error) {
	var combineMethod string
	noCombineCount := 0

	for i, def := range defs {
		// Normalize combine attribute value (trim whitespace)
		defs[i].Combine = strings.TrimSpace(def.Combine)
		if defs[i].Combine == "" {
			noCombineCount++
		} else {
			// Check combine value consistency
			if combineMethod == "" {
				combineMethod = defs[i].Combine
			} else if combineMethod != defs[i].Combine {
				return "", fmt.Errorf("duplicate define '%s' - inconsistent combine values: '%s' vs '%s'", name, combineMethod, defs[i].Combine)
			}
		}
	}

	// Validate combine rules
	if noCombineCount > 1 {
		return "", fmt.Errorf("duplicate define '%s' - more than one definition lacks a combine attribute", name)
	}

	if noCombineCount == 0 && combineMethod == "" {
		return "", fmt.Errorf("duplicate define '%s' without combine attribute", name)
	}

	return combineMethod, nil
}

// mergeDefinesChoice merges defines into a choice pattern
func mergeDefinesChoice(defs []Define) *Choice {
	choice := &Choice{}
	for _, def := range defs {
		choice.Elements = append(choice.Elements, def.Elements...)
		if def.Ref != nil {
			choice.Refs = append(choice.Refs, *def.Ref)
		}
		if def.Choice != nil {
			choice.Elements = append(choice.Elements, def.Choice.Elements...)
			choice.Refs = append(choice.Refs, def.Choice.Refs...)
		}
	}
	return choice
}

// mergeDefinesInterleave merges defines into an interleave pattern
func mergeDefinesInterleave(defs []Define) Interleave {
	interleave := Interleave{}
	for _, def := range defs {
		interleave.Elements = append(interleave.Elements, def.Elements...)
		if def.Ref != nil {
			interleave.Ref = append(interleave.Ref, *def.Ref)
		}
	}
	return interleave
}

// mergeStartsWithCombine merges multiple start elements with combine attributes
// Per RELAX NG spec section 4.17: multiple start elements can be combined using choice or interleave
func (g *Grammar) mergeStartsWithCombine() error {
	// Parse all DIRECT CHILD start elements from RawContent
	starts := g.parseRawStarts()

	// If we found multiple starts, merge them
	if len(starts) <= 1 {
		return nil
	}

	// Validate and get combine method
	combineMethod, err := validateStartCombineAttributes(starts)
	if err != nil {
		return err
	}

	// Create and assign merged start
	g.Start = mergeStartsByMethod(starts, combineMethod)
	return nil
}

// parseRawStarts parses direct child start elements from RawContent
func (g *Grammar) parseRawStarts() []Start {
	var starts []Start
	decoder := xml.NewDecoder(bytes.NewReader(g.RawContent))
	depth := 0
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := token.(type) {
		case xml.StartElement:
			depth++
			if depth == 1 && t.Name.Local == elemNameStart {
				var start Start
				if decodeErr := decoder.DecodeElement(&start, &t); decodeErr == nil {
					starts = append(starts, start)
				}
				depth--
			}
		case xml.EndElement:
			depth--
		}
	}
	return starts
}

// validateStartCombineAttributes validates start combine attributes and returns the combine method
func validateStartCombineAttributes(starts []Start) (string, error) {
	noCombineCount := 0
	var combineMethod string

	for i, start := range starts {
		if start.Name != "" {
			return "", fmt.Errorf("start element cannot have name attribute (obsolete syntax)")
		}

		starts[i].Combine = strings.TrimSpace(start.Combine)
		if starts[i].Combine == "" {
			noCombineCount++
		} else {
			if combineMethod == "" {
				combineMethod = starts[i].Combine
			} else if combineMethod != starts[i].Combine {
				return "", fmt.Errorf("multiple start elements have inconsistent combine values: '%s' vs '%s'", combineMethod, starts[i].Combine)
			}
		}
	}

	if noCombineCount > 1 {
		return "", fmt.Errorf("multiple start elements - more than one lacks a combine attribute")
	}

	return combineMethod, nil
}

// mergeStartsByMethod merges starts based on combine method
func mergeStartsByMethod(starts []Start, combineMethod string) Start {
	merged := Start{Combine: ""}

	switch combineMethod {
	case elemNameChoice:
		choice := &Choice{}
		for _, start := range starts {
			if start.Element != nil {
				choice.Elements = append(choice.Elements, *start.Element)
			}
			if start.Ref != nil {
				choice.Refs = append(choice.Refs, *start.Ref)
			}
			if start.Choice != nil {
				choice.Elements = append(choice.Elements, start.Choice.Elements...)
				choice.Refs = append(choice.Refs, start.Choice.Refs...)
			}
		}
		merged.Choice = choice
	case elemNameInterleave:
		interleave := Interleave{}
		for _, start := range starts {
			if start.Element != nil {
				interleave.Elements = append(interleave.Elements, *start.Element)
			}
			if start.Ref != nil {
				interleave.Ref = append(interleave.Ref, *start.Ref)
			}
		}
		merged.Interleave = []Interleave{interleave}
	}

	return merged
}

// validateDefineContent checks that a define element has at least one pattern child
func validateDefineContent(def *Define) error {
	// The XML unmarshaler may populate Element, Ref, ParentRef, Choice, Group, or Interleave if the define contains them.
	// But RawContent will still include all raw inner XML.

	// If any pattern field is populated, we have at least one pattern
	if def.FirstElement() != nil || def.Ref != nil || def.ParentRef != nil ||
		def.Choice != nil || len(def.Group) > 0 || len(def.Interleave) > 0 {
		return nil
	}

	// Check for patterns in RawContent
	patterns, _, _ := countElementContentPatterns(def.RawContent)
	if patterns > 0 {
		return nil
	}

	// Per spec section 4.2, a define must always have at least one pattern child
	return fmt.Errorf("define element must contain at least one pattern child")
}

// validateDataPatternsInDefine validates all data patterns in a Define element and its nested content
// This recursively validates data patterns from the structured fields (not RawContent)
// so that datatypeLibrary inheritance has already been applied
func validateDataPatternsInDefine(def *Define) error {
	// Validate data patterns directly on the define
	if def.Data != nil {
		if err := validateDataPattern(def.Data); err != nil {
			return err
		}
	}

	// Validate data patterns in choice
	if def.Choice != nil {
		for i := range def.Choice.Data {
			if err := validateDataPattern(&def.Choice.Data[i]); err != nil {
				return err
			}
		}
	}

	// Validate data patterns in elements
	for i := range def.Elements {
		if err := validateDataPatternsInElement(&def.Elements[i]); err != nil {
			return err
		}
	}

	// Validate data patterns in groups
	for i := range def.Group {
		if err := validateDataPatternsInGroup(&def.Group[i]); err != nil {
			return err
		}
	}

	// Validate data patterns in interleaves
	for i := range def.Interleave {
		if err := validateDataPatternsInInterleave(&def.Interleave[i]); err != nil {
			return err
		}
	}

	// Validate data patterns in optional
	for i := range def.Optional {
		if err := validateDataPatternsInOptional(&def.Optional[i]); err != nil {
			return err
		}
	}

	// Validate data patterns in oneOrMore
	for i := range def.OneOrMore {
		if err := validateDataPatternsInOneOrMore(&def.OneOrMore[i]); err != nil {
			return err
		}
	}

	// Validate data patterns in zeroOrMore
	for i := range def.ZeroOrMore {
		if err := validateDataPatternsInZeroOrMore(&def.ZeroOrMore[i]); err != nil {
			return err
		}
	}

	return nil
}

// validateDataPatternsInElement validates data patterns in Element and its nested content
func validateDataPatternsInElement(elem *Element) error {
	// Validate data on the element itself
	if elem.Data != nil {
		if err := validateDataPattern(elem.Data); err != nil {
			return err
		}
	}

	// Validate choice
	if elem.Choice != nil {
		for i := range elem.Choice.Data {
			if err := validateDataPattern(&elem.Choice.Data[i]); err != nil {
				return err
			}
		}
	}

	// Validate attributes
	for i := range elem.Attributes {
		if err := validateDataPatternsInAttribute(&elem.Attributes[i]); err != nil {
			return err
		}
	}

	// Validate nested elements
	for i := range elem.Elements {
		if err := validateDataPatternsInElement(&elem.Elements[i]); err != nil {
			return err
		}
	}

	// Validate groups
	for i := range elem.Group {
		if err := validateDataPatternsInGroup(&elem.Group[i]); err != nil {
			return err
		}
	}

	// Validate interleaves
	for i := range elem.Interleave {
		if err := validateDataPatternsInInterleave(&elem.Interleave[i]); err != nil {
			return err
		}
	}

	// Validate optional
	for i := range elem.Optional {
		if err := validateDataPatternsInOptional(&elem.Optional[i]); err != nil {
			return err
		}
	}

	// Validate oneOrMore
	for i := range elem.OneOrMore {
		if err := validateDataPatternsInOneOrMore(&elem.OneOrMore[i]); err != nil {
			return err
		}
	}

	// Validate zeroOrMore
	for i := range elem.ZeroOrMore {
		if err := validateDataPatternsInZeroOrMore(&elem.ZeroOrMore[i]); err != nil {
			return err
		}
	}

	return nil
}

// validateDataPatternsInAttribute validates data patterns in Attribute and its nested content
func validateDataPatternsInAttribute(attr *Attribute) error {
	if attr.Data != nil {
		if err := validateDataPattern(attr.Data); err != nil {
			return err
		}
	}

	if attr.Choice != nil {
		for i := range attr.Choice.Data {
			if err := validateDataPattern(&attr.Choice.Data[i]); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateDataPatternsInChoice validates data patterns in Choice and its nested content
func validateDataPatternsInChoice(choice *Choice) error {
	for i := range choice.Data {
		if err := validateDataPattern(&choice.Data[i]); err != nil {
			return err
		}
	}

	for i := range choice.Elements {
		if err := validateDataPatternsInElement(&choice.Elements[i]); err != nil {
			return err
		}
	}

	for i := range choice.Attributes {
		if err := validateDataPatternsInAttribute(&choice.Attributes[i]); err != nil {
			return err
		}
	}

	for i := range choice.Group {
		if err := validateDataPatternsInGroup(&choice.Group[i]); err != nil {
			return err
		}
	}

	for i := range choice.Interleave {
		if err := validateDataPatternsInInterleave(&choice.Interleave[i]); err != nil {
			return err
		}
	}

	return nil
}

// validateDataPatternsInGroup validates data patterns in Group and its nested content
func validateDataPatternsInGroup(group *Group) error {
	for i := range group.Elements {
		if err := validateDataPatternsInElement(&group.Elements[i]); err != nil {
			return err
		}
	}

	for i := range group.Attributes {
		if err := validateDataPatternsInAttribute(&group.Attributes[i]); err != nil {
			return err
		}
	}

	for i := range group.Choice {
		if err := validateDataPatternsInChoice(&group.Choice[i]); err != nil {
			return err
		}
	}

	for i := range group.Optional {
		if err := validateDataPatternsInOptional(&group.Optional[i]); err != nil {
			return err
		}
	}

	for i := range group.OneOrMore {
		if err := validateDataPatternsInOneOrMore(&group.OneOrMore[i]); err != nil {
			return err
		}
	}

	for i := range group.ZeroOrMore {
		if err := validateDataPatternsInZeroOrMore(&group.ZeroOrMore[i]); err != nil {
			return err
		}
	}

	for i := range group.Group {
		if err := validateDataPatternsInGroup(&group.Group[i]); err != nil {
			return err
		}
	}

	for i := range group.Interleave {
		if err := validateDataPatternsInInterleave(&group.Interleave[i]); err != nil {
			return err
		}
	}

	return nil
}

// validateDataPatternsInInterleave validates data patterns in Interleave and its nested content
func validateDataPatternsInInterleave(interleave *Interleave) error {
	for i := range interleave.Elements {
		if err := validateDataPatternsInElement(&interleave.Elements[i]); err != nil {
			return err
		}
	}

	for i := range interleave.Attributes {
		if err := validateDataPatternsInAttribute(&interleave.Attributes[i]); err != nil {
			return err
		}
	}

	for i := range interleave.Choice {
		if err := validateDataPatternsInChoice(&interleave.Choice[i]); err != nil {
			return err
		}
	}

	for i := range interleave.Optional {
		if err := validateDataPatternsInOptional(&interleave.Optional[i]); err != nil {
			return err
		}
	}

	for i := range interleave.OneOrMore {
		if err := validateDataPatternsInOneOrMore(&interleave.OneOrMore[i]); err != nil {
			return err
		}
	}

	for i := range interleave.ZeroOrMore {
		if err := validateDataPatternsInZeroOrMore(&interleave.ZeroOrMore[i]); err != nil {
			return err
		}
	}

	for i := range interleave.Group {
		if err := validateDataPatternsInGroup(&interleave.Group[i]); err != nil {
			return err
		}
	}

	return nil
}

// validateDataPatternsInOptional validates data patterns in Optional and its nested content
func validateDataPatternsInOptional(opt *Optional) error {
	for i := range opt.Elements {
		if err := validateDataPatternsInElement(&opt.Elements[i]); err != nil {
			return err
		}
	}

	for i := range opt.Attributes {
		if err := validateDataPatternsInAttribute(&opt.Attributes[i]); err != nil {
			return err
		}
	}

	return nil
}

// validateDataPatternsInOneOrMore validates data patterns in OneOrMore and its nested content
func validateDataPatternsInOneOrMore(one *OneOrMore) error {
	for i := range one.Element {
		if err := validateDataPatternsInElement(&one.Element[i]); err != nil {
			return err
		}
	}

	for i := range one.Attribute {
		if err := validateDataPatternsInAttribute(&one.Attribute[i]); err != nil {
			return err
		}
	}

	if one.Choice != nil {
		if err := validateDataPatternsInChoice(one.Choice); err != nil {
			return err
		}
	}

	for i := range one.Group {
		if err := validateDataPatternsInGroup(&one.Group[i]); err != nil {
			return err
		}
	}

	for i := range one.Interleave {
		if err := validateDataPatternsInInterleave(&one.Interleave[i]); err != nil {
			return err
		}
	}

	return nil
}

// validateDataPatternsInZeroOrMore validates data patterns in ZeroOrMore and its nested content
func validateDataPatternsInZeroOrMore(zero *ZeroOrMore) error {
	for i := range zero.Element {
		if err := validateDataPatternsInElement(&zero.Element[i]); err != nil {
			return err
		}
	}

	for i := range zero.Attribute {
		if err := validateDataPatternsInAttribute(&zero.Attribute[i]); err != nil {
			return err
		}
	}

	if zero.Choice != nil {
		if err := validateDataPatternsInChoice(zero.Choice); err != nil {
			return err
		}
	}

	for i := range zero.Group {
		if err := validateDataPatternsInGroup(&zero.Group[i]); err != nil {
			return err
		}
	}

	for i := range zero.Interleave {
		if err := validateDataPatternsInInterleave(&zero.Interleave[i]); err != nil {
			return err
		}
	}

	return nil
}

// validatePatterns validates that all patterns in the grammar conform to RELAX NG syntax rules
func (g *Grammar) validatePatterns() error {
	// Validate start element attributes and patterns
	if err := g.validateStartElement(); err != nil {
		return err
	}

	// Validate includes
	if err := g.validateIncludeElements(); err != nil {
		return err
	}

	// Validate defines
	if err := g.validateDefineElements(); err != nil {
		return err
	}

	// Per RELAX NG spec section 7.4: Check for recursive refs in nullable contexts
	if err := g.validateNoRecursiveNullableRefs(); err != nil {
		return err
	}

	return nil
}

// validateStartElement validates the start element structure and attributes
func (g *Grammar) validateStartElement() error {
	if g.Start.Name != "" {
		return fmt.Errorf("start element cannot have name attribute (obsolete syntax)")
	}

	// Check for unknown attributes on start element
	for _, attr := range g.Start.RawAttrs {
		if attr.Name.Space == "" && !strings.HasPrefix(attr.Name.Local, "xml") {
			if attr.Name.Local == "combine" {
				trimmedValue := strings.TrimSpace(attr.Value)
				if trimmedValue != elemNameChoice && trimmedValue != elemNameInterleave {
					return fmt.Errorf("start element combine attribute must be 'choice' or 'interleave', got '%s'", trimmedValue)
				}
			} else {
				return fmt.Errorf("start element has unknown attribute: %s", attr.Name.Local)
			}
		}
	}

	// Validate start patterns
	if err := validateStartPatterns(&g.Start); err != nil {
		return err
	}

	if g.Start.Element != nil {
		if err := validateElementPatterns(g.Start.Element); err != nil {
			return err
		}
		if err := validateInfiniteNameClasses(g.Start.Element, false); err != nil {
			return fmt.Errorf("start element: %w", err)
		}
	}

	if g.Start.Choice != nil {
		if err := validateChoicePatterns(g.Start.Choice); err != nil {
			return err
		}
	}

	return nil
}

// validateIncludeElements validates all include elements in the grammar
func (g *Grammar) validateIncludeElements() error {
	for i, inc := range g.Includes {
		if err := g.validateSingleInclude(i, &inc); err != nil {
			return err
		}
	}
	return nil
}

// validateSingleInclude validates a single include element
func (g *Grammar) validateSingleInclude(idx int, inc *Include) error {
	if inc.Href == "" {
		return fmt.Errorf("include element must have href attribute")
	}

	// Check for unknown attributes
	for _, attr := range inc.RawAttrs {
		if attr.Name.Space == "xml" || attr.Name.Local == "href" || attr.Name.Local == "ns" {
			continue
		}
		if attr.Name.Space == "" {
			return fmt.Errorf("include element: unknown attribute '%s'", attr.Name.Local)
		}
	}

	// Include can only have start and define children
	names, err := firstLevelLocalNames(inc.RawContent)
	if err != nil {
		return fmt.Errorf("include element: error parsing content: %v", err)
	}
	for _, name := range names {
		if name != elemNameStart && name != "define" {
			return fmt.Errorf("include element can only contain start and define children, found '%s'", name)
		}
	}

	// At most one start child
	if len(inc.Start) > 1 {
		return fmt.Errorf("include element can have at most one start child")
	}

	// Validate start child in include if present
	for j, start := range inc.Start {
		if err := validateStartPatterns(&start); err != nil {
			return fmt.Errorf("include[%d] start[%d]: %v", idx, j, err)
		}
	}

	// Validate defines in include
	for j, def := range inc.Defines {
		if def.Name == "" {
			return fmt.Errorf("include[%d] define[%d]: define element must have name attribute", idx, j)
		}
		if !isValidNCNameNoColon(def.Name) {
			return fmt.Errorf("include[%d] define[%d]: invalid name '%s' (not a valid NCName)", idx, j, def.Name)
		}
		if err := validateDefineContent(&def); err != nil {
			return fmt.Errorf("include[%d] define '%s': %v", idx, def.Name, err)
		}
	}

	return nil
}

// validateDefineElements validates all define elements in the grammar
func (g *Grammar) validateDefineElements() error {
	for _, def := range g.Defines {
		if err := g.validateSingleDefine(&def); err != nil {
			return err
		}
	}
	return nil
}

// validateSingleDefine validates a single define element
func (g *Grammar) validateSingleDefine(def *Define) error {
	// Validate combine attribute
	if def.Combine != "" && def.Combine != elemNameChoice && def.Combine != elemNameInterleave {
		return fmt.Errorf("define %s: combine attribute must be 'choice' or 'interleave', got '%s'", def.Name, def.Combine)
	}

	// Check for unknown attributes
	for _, attr := range def.RawAttrs {
		if attr.Name.Space == "xml" || attr.Name.Local == "name" || attr.Name.Local == "combine" {
			continue
		}
		if attr.Name.Space == "" {
			return fmt.Errorf("define %s: unknown attribute '%s'", def.Name, attr.Name.Local)
		}
	}

	// Validate define content
	if def.FirstElement() != nil {
		if err := validateElementPatterns(def.FirstElement()); err != nil {
			return err
		}
		if err := validateInfiniteNameClasses(def.FirstElement(), false); err != nil {
			return fmt.Errorf("define '%s': %w", def.Name, err)
		}
	}

	// Validate data patterns in define
	if err := validateDataPatternsInDefine(def); err != nil {
		return fmt.Errorf("define '%s': %w", def.Name, err)
	}

	return nil
}

// validateNoRecursiveNullableRefs checks for recursive refs in nullable contexts
// Per RELAX NG spec, a pattern must not be able to match an infinite sequence of zero-length items
// This happens when a ref to the same pattern appears in a nullable context (optional, zeroOrMore, etc)
func (g *Grammar) validateNoRecursiveNullableRefs() error {
	// Check each define for recursive refs in nullable patterns
	for _, def := range g.Defines {
		// Check the define's patterns for recursive refs in nullable contexts
		if err := g.checkDefineForRecursiveNullableRefs(def.Name, &def); err != nil {
			return fmt.Errorf("define '%s': %w", def.Name, err)
		}
	}
	return nil
}

// checkDefineForRecursiveNullableRefs checks if a define contains recursive refs in nullable patterns
func (g *Grammar) checkDefineForRecursiveNullableRefs(defineName string, def *Define) error {
	// Check optional patterns (always nullable)
	for _, opt := range def.Optional {
		if err := checkNullablePatternForRecursiveRef(defineName, &opt); err != nil {
			return err
		}
	}

	// Check zeroOrMore patterns (always nullable)
	for _, zom := range def.ZeroOrMore {
		if err := checkNullablePatternForRecursiveRef(defineName, &zom); err != nil {
			return err
		}
	}

	// Check choice patterns (nullable if any alternative is nullable)
	if def.Choice != nil {
		if err := checkChoiceForRecursiveRef(defineName, def.Choice); err != nil {
			return err
		}
	}

	// Check group patterns (nullable if all children are nullable)
	for _, grp := range def.Group {
		if err := checkGroupForRecursiveRef(defineName, &grp); err != nil {
			return err
		}
	}

	// Check interleave patterns (nullable if all children are nullable)
	for _, interl := range def.Interleave {
		if err := checkInterleaveForRecursiveRef(defineName, &interl); err != nil {
			return err
		}
	}

	return nil
}

// checkNullablePatternForRecursiveRef checks a nullable pattern for recursive refs
func checkNullablePatternForRecursiveRef(defineName string, pattern interface{}) error {
	switch p := pattern.(type) {
	case *Optional:
		// Check direct refs in optional
		for _, ref := range p.Ref {
			if ref.Name == defineName {
				return fmt.Errorf("infinite recursion: ref to '%s' in nullable optional context", defineName)
			}
		}
		// Check parentRefs in optional
		for _, ref := range p.ParentRef {
			if ref.Name == defineName {
				return fmt.Errorf("infinite recursion: parentRef to '%s' in nullable optional context", defineName)
			}
		}
		// Check elements for nested refs
		for _, elem := range p.Elements {
			if err := checkElementForRecursiveRef(defineName, &elem); err != nil {
				return err
			}
		}
	case *ZeroOrMore:
		// Check refs
		for _, ref := range p.Ref {
			if ref.Name == defineName {
				return fmt.Errorf("infinite recursion: ref to '%s' in nullable zeroOrMore context", defineName)
			}
		}
		// Check elements for nested refs
		for _, elem := range p.Element {
			if err := checkElementForRecursiveRef(defineName, &elem); err != nil {
				return err
			}
		}
		// Check choice (nullable if any alternative is nullable)
		if p.Choice != nil {
			if err := checkChoiceForRecursiveRef(defineName, p.Choice); err != nil {
				return err
			}
		}
	case *OneOrMore:
		// OneOrMore is not nullable - only check its content normally
		for _, ref := range p.Ref {
			if ref.Name == defineName {
				return fmt.Errorf("infinite recursion: ref to '%s' in oneOrMore context (not nullable)", defineName)
			}
		}
	}
	return nil
}

// checkElementForRecursiveRef checks if an element contains a recursive ref in nullable context
func checkElementForRecursiveRef(defineName string, elem *Element) error {
	// Check direct refs
	for _, ref := range elem.Ref {
		if ref.Name == defineName {
			return fmt.Errorf("infinite recursion: ref to '%s' in nullable context", defineName)
		}
	}

	// Check optional patterns (always nullable within element)
	for _, opt := range elem.Optional {
		if err := checkNullablePatternForRecursiveRef(defineName, &opt); err != nil {
			return err
		}
	}

	// Check zeroOrMore patterns (always nullable within element)
	for _, zom := range elem.ZeroOrMore {
		if err := checkNullablePatternForRecursiveRef(defineName, &zom); err != nil {
			return err
		}
	}

	return nil
}

// checkChoiceForRecursiveRef checks a choice for recursive refs (nullable if any alternative is nullable)
func checkChoiceForRecursiveRef(defineName string, choice *Choice) error {
	// Check each alternative for recursive refs in nullable contexts
	for _, elem := range choice.Elements {
		if err := checkElementForRecursiveRef(defineName, &elem); err != nil {
			return err
		}
	}

	// Check refs in choice
	for _, ref := range choice.Refs {
		if ref.Name == defineName {
			return fmt.Errorf("infinite recursion: ref to '%s' in nullable choice context", defineName)
		}
	}

	return nil
}

// checkGroupForRecursiveRef checks a group for recursive refs (nullable if all children are nullable)
func checkGroupForRecursiveRef(defineName string, group *Group) error {
	// For groups, we check recursively if the entire group could be nullable
	// This is complex, so for now we just check for direct recursive refs in nullable contexts
	for _, elem := range group.Elements {
		if err := checkElementForRecursiveRef(defineName, &elem); err != nil {
			return err
		}
	}

	for _, ref := range group.Ref {
		if ref.Name == defineName {
			return fmt.Errorf("infinite recursion: ref to '%s' in group context", defineName)
		}
	}

	return nil
}

// checkInterleaveForRecursiveRef checks an interleave for recursive refs (nullable if all children are nullable)
func checkInterleaveForRecursiveRef(defineName string, interleave *Interleave) error {
	// Similar to group, check for direct recursive refs
	for _, elem := range interleave.Elements {
		if err := checkElementForRecursiveRef(defineName, &elem); err != nil {
			return err
		}
	}

	for _, ref := range interleave.Ref {
		if ref.Name == defineName {
			return fmt.Errorf("infinite recursion: ref to '%s' in interleave context", defineName)
		}
	}

	return nil
}

// validateRefs checks that all ref elements point to existing defines
func (g *Grammar) validateRefs(validDefineNames map[string]bool) error {
	// Validate start refs
	if err := validateStartRefs(&g.Start, validDefineNames); err != nil {
		return err
	}

	// Validate defines refs
	for _, def := range g.Defines {
		if err := validateDefineReferences(&def, validDefineNames); err != nil {
			return fmt.Errorf("define '%s': %w", def.Name, err)
		}
	}

	return nil
}

// validateStartRefs validates all refs in start element and its patterns
func validateStartRefs(start *Start, validDefineNames map[string]bool) error {
	// Check direct refs
	if start.Ref != nil && start.Ref.Name != "" {
		if !validDefineNames[start.Ref.Name] {
			return fmt.Errorf("start contains undefined reference '%s'", start.Ref.Name)
		}
	}
	if start.ParentRef != nil && start.ParentRef.Name != "" {
		if !validDefineNames[start.ParentRef.Name] {
			return fmt.Errorf("start contains undefined parentRef '%s'", start.ParentRef.Name)
		}
	}

	// Check element
	if start.Element != nil {
		if err := validateElementRefs(start.Element, validDefineNames); err != nil {
			return err
		}
	}

	// Check choice
	if start.Choice != nil {
		if err := validateChoiceRefs(start.Choice, validDefineNames); err != nil {
			return fmt.Errorf("start: %w", err)
		}
	}

	// Check groups, interleaves, optionals, oneOrMores, zeroOrMores
	for _, group := range start.Group {
		if err := validateGroupRefs(&group, validDefineNames); err != nil {
			return fmt.Errorf("start: %w", err)
		}
	}
	for _, interleave := range start.Interleave {
		if err := validateInterleaveRefs(&interleave, validDefineNames); err != nil {
			return fmt.Errorf("start: %w", err)
		}
	}
	for _, optional := range start.Optional {
		if err := validateOptionalRefs(&optional, validDefineNames); err != nil {
			return fmt.Errorf("start: %w", err)
		}
	}
	for _, oneOrMore := range start.OneOrMore {
		if err := validateOneOrMoreRefs(&oneOrMore, validDefineNames); err != nil {
			return fmt.Errorf("start: %w", err)
		}
	}
	for _, zeroOrMore := range start.ZeroOrMore {
		if err := validateZeroOrMoreRefs(&zeroOrMore, validDefineNames); err != nil {
			return fmt.Errorf("start: %w", err)
		}
	}

	return nil
}

// validateDefineReferences validates direct refs in a define
func validateDefineReferences(def *Define, validDefineNames map[string]bool) error {
	// Check direct refs
	if def.Ref != nil && def.Ref.Name != "" {
		if !validDefineNames[def.Ref.Name] {
			return fmt.Errorf("undefined reference '%s'", def.Ref.Name)
		}
	}
	if def.ParentRef != nil && def.ParentRef.Name != "" {
		if !validDefineNames[def.ParentRef.Name] {
			return fmt.Errorf("undefined parentRef '%s'", def.ParentRef.Name)
		}
	}

	// Check refs inside element patterns
	if def.FirstElement() != nil {
		if err := validateElementRefs(def.FirstElement(), validDefineNames); err != nil {
			return err
		}
	}

	return nil
}

// validateElementRefs recursively validates refs in an element pattern
func validateElementRefs(elem *Element, validDefineNames map[string]bool) error {
	// Check direct refs
	if err := validateElementDirectRefs(elem, validDefineNames); err != nil {
		return err
	}

	// Note: Attributes don't directly contain refs
	// They use Choice, Value, or Data patterns instead

	// Recursively check nested elements in various patterns
	return validateElementNestedRefs(elem, validDefineNames)
}

// validateElementDirectRefs checks ref and parentRef attributes
func validateElementDirectRefs(elem *Element, validDefineNames map[string]bool) error {
	for _, ref := range elem.Ref {
		if !validDefineNames[ref.Name] {
			return fmt.Errorf("undefined reference '%s'", ref.Name)
		}
	}
	for _, ref := range elem.ParentRef {
		if !validDefineNames[ref.Name] {
			return fmt.Errorf("undefined parentRef '%s'", ref.Name)
		}
	}
	return nil
}

// validateRefList reports the first ref in refs whose target is not a defined name.
func validateRefList(refs []Ref, validDefineNames map[string]bool) error {
	for _, ref := range refs {
		if !validDefineNames[ref.Name] {
			return fmt.Errorf("undefined reference '%s'", ref.Name)
		}
	}
	return nil
}

// validateElementNestedRefs recursively validates refs in nested element containers.
// It checks both each container's own <ref>/<parentRef> children and the refs
// inside any nested elements — an undefined ref directly inside, say, a
// <oneOrMore> or <choice> was previously accepted silently.
func validateElementNestedRefs(elem *Element, validDefineNames map[string]bool) error {
	if elem.Choice != nil {
		if err := validateRefList(elem.Choice.Refs, validDefineNames); err != nil {
			return err
		}
		for _, childElem := range elem.Choice.Elements {
			if err := validateElementRefs(&childElem, validDefineNames); err != nil {
				return err
			}
		}
	}

	for _, group := range elem.Group {
		if err := validateRefList(group.Ref, validDefineNames); err != nil {
			return err
		}
		for _, childElem := range group.Elements {
			if err := validateElementRefs(&childElem, validDefineNames); err != nil {
				return err
			}
		}
	}

	for _, interleave := range elem.Interleave {
		if err := validateRefList(interleave.Ref, validDefineNames); err != nil {
			return err
		}
		for _, childElem := range interleave.Elements {
			if err := validateElementRefs(&childElem, validDefineNames); err != nil {
				return err
			}
		}
	}

	for _, optional := range elem.Optional {
		if err := validateRefList(optional.Ref, validDefineNames); err != nil {
			return err
		}
		if err := validateRefList(optional.ParentRef, validDefineNames); err != nil {
			return err
		}
		for _, childElem := range optional.Elements {
			if err := validateElementRefs(&childElem, validDefineNames); err != nil {
				return err
			}
		}
	}

	for _, oneOrMore := range elem.OneOrMore {
		if err := validateRefList(oneOrMore.Ref, validDefineNames); err != nil {
			return err
		}
		for _, childElem := range oneOrMore.Element {
			if err := validateElementRefs(&childElem, validDefineNames); err != nil {
				return err
			}
		}
	}

	for _, zeroOrMore := range elem.ZeroOrMore {
		if err := validateRefList(zeroOrMore.Ref, validDefineNames); err != nil {
			return err
		}
		for _, childElem := range zeroOrMore.Element {
			if err := validateElementRefs(&childElem, validDefineNames); err != nil {
				return err
			}
		}
	}

	if elem.Mixed != nil {
		if err := validateRefList(elem.Mixed.Ref, validDefineNames); err != nil {
			return err
		}
		for _, childElem := range elem.Mixed.Elements {
			if err := validateElementRefs(&childElem, validDefineNames); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateChoiceRefs validates refs in a choice pattern
func validateChoiceRefs(choice *Choice, validDefineNames map[string]bool) error {
	if choice == nil {
		return nil
	}

	// Check refs in choice
	for _, ref := range choice.Refs {
		if !validDefineNames[ref.Name] {
			return fmt.Errorf("undefined reference '%s'", ref.Name)
		}
	}

	// Recursively check elements
	for _, elem := range choice.Elements {
		if err := validateElementRefs(&elem, validDefineNames); err != nil {
			return err
		}
	}

	// Check groups
	for _, group := range choice.Group {
		if err := validateGroupRefs(&group, validDefineNames); err != nil {
			return err
		}
	}

	return nil
}

// validateGroupRefs validates refs in a group pattern
func validateGroupRefs(group *Group, validDefineNames map[string]bool) error {
	if group == nil {
		return nil
	}

	// Check refs in group
	for _, ref := range group.Ref {
		if !validDefineNames[ref.Name] {
			return fmt.Errorf("undefined reference '%s'", ref.Name)
		}
	}

	// Recursively check elements
	for _, elem := range group.Elements {
		if err := validateElementRefs(&elem, validDefineNames); err != nil {
			return err
		}
	}

	// Check nested groups
	for _, subgroup := range group.Group {
		if err := validateGroupRefs(&subgroup, validDefineNames); err != nil {
			return err
		}
	}

	return nil
}

// validateInterleaveRefs validates refs in an interleave pattern
func validateInterleaveRefs(interleave *Interleave, validDefineNames map[string]bool) error {
	if interleave == nil {
		return nil
	}

	// Check refs in interleave
	for _, ref := range interleave.Ref {
		if !validDefineNames[ref.Name] {
			return fmt.Errorf("undefined reference '%s'", ref.Name)
		}
	}

	// Recursively check elements
	for _, elem := range interleave.Elements {
		if err := validateElementRefs(&elem, validDefineNames); err != nil {
			return err
		}
	}

	return nil
}

// validateOptionalRefs validates refs in an optional pattern
func validateOptionalRefs(optional *Optional, validDefineNames map[string]bool) error {
	if optional == nil {
		return nil
	}

	// Check elements in optional
	for _, elem := range optional.Elements {
		if err := validateElementRefs(&elem, validDefineNames); err != nil {
			return err
		}
	}

	return nil
}

// validateOneOrMoreRefs validates refs in a oneOrMore pattern
func validateOneOrMoreRefs(oneOrMore *OneOrMore, validDefineNames map[string]bool) error {
	if oneOrMore == nil {
		return nil
	}

	// Check refs in oneOrMore
	for _, ref := range oneOrMore.Ref {
		if !validDefineNames[ref.Name] {
			return fmt.Errorf("undefined reference '%s'", ref.Name)
		}
	}

	// Check elements in oneOrMore
	for _, elem := range oneOrMore.Element {
		if err := validateElementRefs(&elem, validDefineNames); err != nil {
			return err
		}
	}

	return nil
}

// validateZeroOrMoreRefs validates refs in a zeroOrMore pattern
func validateZeroOrMoreRefs(zeroOrMore *ZeroOrMore, validDefineNames map[string]bool) error {
	if zeroOrMore == nil {
		return nil
	}

	// Check refs in zeroOrMore
	for _, ref := range zeroOrMore.Ref {
		if !validDefineNames[ref.Name] {
			return fmt.Errorf("undefined reference '%s'", ref.Name)
		}
	}

	// Check elements in zeroOrMore
	for _, elem := range zeroOrMore.Element {
		if err := validateElementRefs(&elem, validDefineNames); err != nil {
			return err
		}
	}

	return nil
}

// validateStartPatterns validates that a start element has exactly one pattern
func validateStartPatterns(start *Start) error {
	if start == nil {
		return nil
	}

	// Check if start element is actually present (has content or typed fields)
	hasContent := start.Element != nil || start.Ref != nil || len(bytes.TrimSpace(start.RawContent)) > 0
	if !hasContent {
		// Empty start element is allowed (grammar may only have defines)
		return nil
	}

	// Validate ref and parentRef attributes
	if err := validateStartRefAndParentRef(start); err != nil {
		return err
	}

	// In RELAX NG, start must contain exactly one pattern element
	// The XML unmarshaler captures first Element or Ref in dedicated fields.
	// But RawContent contains ALL raw inner XML, including the already-parsed element.

	// Check if we have multiple patterns total
	parsedPatternCount := 0
	if start.Element != nil {
		parsedPatternCount++
	}
	if start.Ref != nil {
		parsedPatternCount++
	}

	// If we have both Element and Ref, that's already multiple patterns
	if parsedPatternCount > 1 {
		return fmt.Errorf("start element cannot have both element and ref children")
	}

	// Count patterns in RawContent
	count, patterns, err := countElementContentPatterns(start.RawContent)
	if err != nil {
		return err
	}

	// If we have a parsed Element or Ref, the RawContent count includes that element too.
	// So we need to subtract it to avoid double-counting.
	if parsedPatternCount > 0 && count > 0 {
		count-- // Subtract the already-parsed element that appears in RawContent
	}

	totalPatterns := parsedPatternCount + count

	if totalPatterns == 0 {
		return fmt.Errorf("start element must contain at least one pattern")
	}

	if totalPatterns > 1 {
		return fmt.Errorf("start element must contain exactly one pattern, found %d (%v)", totalPatterns, patterns)
	}

	return nil
}

// validateStartRefAndParentRef validates ref and parentRef attributes in a start element
func validateStartRefAndParentRef(start *Start) error {
	// Validate ref if present
	if start.Ref != nil {
		if start.Ref.Name == "" {
			return fmt.Errorf("ref element must have a name attribute")
		}
		if !isValidNCNameNoColon(start.Ref.Name) {
			return fmt.Errorf("ref has invalid name '%s' (not a valid NCName)", start.Ref.Name)
		}

		hasChild, children, _ := hasAnyRNGChild(start.Ref.RawContent)
		if hasChild {
			return fmt.Errorf("ref element cannot have child elements, found %v", children)
		}
	}

	// Validate parentRef if present
	if start.ParentRef != nil {
		if start.ParentRef.Name == "" {
			return fmt.Errorf("parentRef element must have a name attribute")
		}
		if !isValidNCNameNoColon(start.ParentRef.Name) {
			return fmt.Errorf("parentRef has invalid name '%s' (not a valid NCName)", start.ParentRef.Name)
		}

		hasChild, children, _ := hasAnyRNGChild(start.ParentRef.RawContent)
		if hasChild {
			return fmt.Errorf("parentRef element cannot have child elements, found %v", children)
		}
	}

	return nil
}

// validateNestedStartPatterns validates that a nested grammar's start pattern
// doesn't contain invalid nested grammars (e.g., nested grammars in choice)
// Per RELAX NG spec section 4.18: nested grammars are only allowed in group or interleave
func (g *Grammar) validateNestedStartPatterns(start *Start) error {
	if start == nil {
		return nil
	}

	// Check if start's choice contains nested grammars (not allowed)
	if start.Choice != nil {
		if err := g.validateNestedStartPatternsInChoice(start.Choice); err != nil {
			return err
		}
	}

	// Check if start's group contains nested grammars (allowed, will be unpacked)
	// No validation needed for group as it allows nested grammars

	// Check if start's interleave contains nested grammars (allowed, will be unpacked)
	// No validation needed for interleave as it allows nested grammars

	// Check other pattern containers
	for _, opt := range start.Optional {
		if err := g.validateNestedStartPatternsInOptional(&opt); err != nil {
			return err
		}
	}

	for _, oneOrMore := range start.OneOrMore {
		if err := g.validateNestedStartPatternsInOneOrMore(&oneOrMore); err != nil {
			return err
		}
	}

	for _, zeroOrMore := range start.ZeroOrMore {
		if err := g.validateNestedStartPatternsInZeroOrMore(&zeroOrMore); err != nil {
			return err
		}
	}

	return nil
}

// validateNestedStartPatternsInChoice checks for invalid nested grammars in a choice
func (g *Grammar) validateNestedStartPatternsInChoice(choice *Choice) error {
	if choice == nil {
		return nil
	}

	// Check if choice's RawContent contains nested grammars
	if len(choice.RawContent) > 0 && bytes.Contains(choice.RawContent, []byte("<grammar")) {
		return fmt.Errorf("nested grammar in choice is not allowed (spec section 4.18: nested grammar is only allowed in group or interleave)")
	}

	return nil
}

// validateNestedStartPatternsInOptional checks for invalid nested grammars in an optional
func (g *Grammar) validateNestedStartPatternsInOptional(opt *Optional) error {
	if opt == nil {
		return nil
	}

	// Check if optional's RawContent contains nested grammars
	if len(opt.RawContent) > 0 && bytes.Contains(opt.RawContent, []byte("<grammar")) {
		return fmt.Errorf("nested grammar in optional is not allowed")
	}

	return nil
}

// validateNestedStartPatternsInOneOrMore checks for invalid nested grammars in a oneOrMore
func (g *Grammar) validateNestedStartPatternsInOneOrMore(oneOrMore *OneOrMore) error {
	if oneOrMore == nil {
		return nil
	}

	// Check if oneOrMore's RawContent contains nested grammars
	if len(oneOrMore.RawContent) > 0 && bytes.Contains(oneOrMore.RawContent, []byte("<grammar")) {
		return fmt.Errorf("nested grammar in oneOrMore is not allowed")
	}

	// Check if oneOrMore's choice contains nested grammars
	if oneOrMore.Choice != nil {
		if err := g.validateNestedStartPatternsInChoice(oneOrMore.Choice); err != nil {
			return err
		}
	}

	return nil
}

// validateNestedStartPatternsInZeroOrMore checks for invalid nested grammars in a zeroOrMore
func (g *Grammar) validateNestedStartPatternsInZeroOrMore(zeroOrMore *ZeroOrMore) error {
	if zeroOrMore == nil {
		return nil
	}

	// Check if zeroOrMore's RawContent contains nested grammars
	if len(zeroOrMore.RawContent) > 0 && bytes.Contains(zeroOrMore.RawContent, []byte("<grammar")) {
		return fmt.Errorf("nested grammar in zeroOrMore is not allowed")
	}

	// Check if zeroOrMore's choice contains nested grammars
	if zeroOrMore.Choice != nil {
		if err := g.validateNestedStartPatternsInChoice(zeroOrMore.Choice); err != nil {
			return err
		}
	}

	return nil
}

// validateElementPatterns recursively validates patterns within an element
func validateElementPatterns(elem *Element) error {
	if elem == nil {
		return nil
	}

	// Check for obsolete elements
	if err := validateElementObsoleteChildren(elem); err != nil {
		return err
	}

	// Check for unknown attributes
	if err := validateElementUnknownAttributes(elem); err != nil {
		return err
	}

	// Validate element name
	if err := validateElementName(elem); err != nil {
		return err
	}

	// Validate attributes
	for i := range elem.Attributes {
		attr := &elem.Attributes[i]
		if err := validateAttributePatterns(attr); err != nil {
			return err
		}
	}

	// Validate content patterns
	if err := validateElementContentPatterns(elem); err != nil {
		return err
	}

	return nil
}

// validateElementObsoleteChildren checks for obsolete child elements
func validateElementObsoleteChildren(elem *Element) error {
	if len(elem.ObsoleteNot) > 0 {
		return fmt.Errorf("element contains obsolete <not> element")
	}
	if len(elem.ObsoleteDifference) > 0 {
		return fmt.Errorf("element contains obsolete <difference> element")
	}
	if len(elem.ObsoleteKey) > 0 {
		return fmt.Errorf("element contains obsolete <key> element")
	}
	if len(elem.ObsoleteKeyRef) > 0 {
		return fmt.Errorf("element contains obsolete <keyRef> element")
	}
	return nil
}

// validateElementUnknownAttributes checks for unknown attributes on element
func validateElementUnknownAttributes(elem *Element) error {
	for _, attr := range elem.RawAttrs {
		// Reject unknown non-namespaced attributes (except xml: namespace)
		if attr.Name.Space == "" && !strings.HasPrefix(attr.Name.Local, "xml") {
			return fmt.Errorf("element has unknown attribute: %s", attr.Name.Local)
		}
	}
	return nil
}

// validateElementName validates element's name or name class
func validateElementName(elem *Element) error {
	hasNameAttr := elem.Name != ""
	hasNameElement := elem.NameElement != nil && elem.NameElement.Value != ""
	hasNameClass := elem.AnyName != nil || elem.NsName != nil

	// Check if choice contains name elements (valid name class)
	hasChoiceNameClass := elem.Choice != nil && len(elem.Choice.NameElements) > 0

	// Count total name specifiers
	nameCount := 0
	if hasNameAttr {
		nameCount++
	}
	if hasNameElement {
		nameCount++
	}
	if hasNameClass {
		nameCount++
	}
	if hasChoiceNameClass {
		nameCount++
	}

	// Can't have more than one name specifier
	if nameCount > 1 {
		return fmt.Errorf("element cannot have multiple name specifiers (name attribute, <name> element, choice of names, or name class)")
	}

	// Must have at least one name specifier
	if nameCount == 0 {
		return fmt.Errorf("element has invalid name (empty or missing)")
	}

	// If we have a name attribute, it must be a valid NCName
	if hasNameAttr && !isValidNCName(elem.Name) {
		return fmt.Errorf("element has invalid name '%s' (not a valid NCName)", elem.Name)
	}

	// If we have a name element, validate its value
	if hasNameElement && !isValidNCName(elem.NameElement.Value) {
		return fmt.Errorf("element has invalid name '%s' (not a valid NCName)", elem.NameElement.Value)
	}

	// Validate datatypeLibrary attribute if present
	if elem.DatatypeLibrary != "" && !isValidDatatypeLibraryURI(elem.DatatypeLibrary) {
		return fmt.Errorf("element has invalid datatypeLibrary URI '%s'", elem.DatatypeLibrary)
	}

	return nil
}

// validateElementContentPatterns validates all content patterns in an element
func validateElementContentPatterns(elem *Element) error {
	// Validate simple patterns (text, empty, data, list, notAllowed)
	if err := validateElementSimplePatterns(elem); err != nil {
		return err
	}

	// Validate container patterns (groups, oneOrMore, zeroOrMore, optional, interleave, choice)
	if err := validateElementContainerPatterns(elem); err != nil {
		return err
	}

	// Validate refs and name classes
	if err := validateElementRefsAndNameClasses(elem); err != nil {
		return err
	}

	return nil
}

// validateElementSimplePatterns validates text, empty, data, list, and notAllowed patterns
func validateElementSimplePatterns(elem *Element) error {
	// Validate text pattern
	if elem.Text != nil {
		if len(elem.Text.RawAttrs) > 0 {
			return fmt.Errorf("text element cannot have attributes")
		}
		hasChild, children, _ := hasAnyRNGChild(elem.Text.RawContent)
		if hasChild {
			return fmt.Errorf("text element cannot have child elements, found %v", children)
		}
	}

	// Validate empty pattern
	if elem.Empty != nil {
		for _, attr := range elem.Empty.RawAttrs {
			if attr.Name.Space == "" && !strings.HasPrefix(attr.Name.Local, "xml") {
				return fmt.Errorf("empty element cannot have attributes")
			}
		}
		hasChild, children, _ := hasAnyRNGChild(elem.Empty.RawContent)
		if hasChild {
			return fmt.Errorf("empty element cannot have child elements, found %v", children)
		}
	}

	// Validate data pattern
	if elem.Data != nil {
		if err := validateDataPattern(elem.Data); err != nil {
			return err
		}
	}

	// Validate list pattern
	if elem.List != nil {
		if err := validateListPattern(elem.List); err != nil {
			return err
		}
	}

	// Validate notAllowed pattern
	if elem.NotAllowed != nil {
		if len(elem.NotAllowed.RawAttrs) > 0 {
			return fmt.Errorf("notAllowed element cannot have attributes")
		}
		hasChild, children, _ := hasAnyRNGChild(elem.NotAllowed.RawContent)
		if hasChild {
			return fmt.Errorf("notAllowed element cannot have child elements, found %v", children)
		}
	}

	return nil
}

// validateElementContainerPatterns validates group, oneOrMore, zeroOrMore, optional, interleave, and choice patterns
func validateElementContainerPatterns(elem *Element) error {
	// Validate groups
	if err := validateElementGroupPatterns(elem); err != nil {
		return err
	}

	// Validate oneOrMore
	if err := validateElementOneOrMorePatterns(elem); err != nil {
		return err
	}

	// Validate zeroOrMore
	if err := validateElementZeroOrMorePatterns(elem); err != nil {
		return err
	}

	// Validate optional
	for _, opt := range elem.Optional {
		for _, child := range opt.Elements {
			if err := validateElementPatterns(&child); err != nil {
				return err
			}
		}
	}

	// Validate interleave with duplicate attribute check
	if err := validateElementInterleavePatterns(elem); err != nil {
		return err
	}

	// Validate choice patterns
	if elem.Choice != nil {
		if err := validateChoicePatterns(elem.Choice); err != nil {
			return err
		}
	}

	return nil
}

// validateElementGroupPatterns validates group patterns
func validateElementGroupPatterns(elem *Element) error {
	for _, group := range elem.Group {
		if err := validateGroupPatterns(&group); err != nil {
			return err
		}
		for _, child := range group.Elements {
			if err := validateElementPatterns(&child); err != nil {
				return err
			}
		}
		for _, subgroup := range group.Group {
			if err := validateGroupPatterns(&subgroup); err != nil {
				return err
			}
		}
		for _, choice := range group.Choice {
			if err := validateChoicePatterns(&choice); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateElementOneOrMorePatterns validates oneOrMore patterns
func validateElementOneOrMorePatterns(elem *Element) error {
	for _, one := range elem.OneOrMore {
		if err := validateNoRepeatingAttributes(&one); err != nil {
			return err
		}
		for _, child := range one.Element {
			if err := validateElementPatterns(&child); err != nil {
				return err
			}
		}
		for _, ref := range one.Ref {
			if err := validateRefName(&ref); err != nil {
				return err
			}
		}
		if err := validateGroupsInOneOrMore(&one); err != nil {
			return err
		}
		if err := validateInterleavesInOneOrMore(&one); err != nil {
			return err
		}
		if err := validateChoicesInOneOrMore(&one); err != nil {
			return err
		}
	}
	return nil
}

// validateElementZeroOrMorePatterns validates zeroOrMore patterns
func validateElementZeroOrMorePatterns(elem *Element) error {
	for _, zero := range elem.ZeroOrMore {
		if err := validateNoRepeatingAttributesZero(&zero); err != nil {
			return err
		}
		for _, child := range zero.Element {
			if err := validateElementPatterns(&child); err != nil {
				return err
			}
		}
		for _, ref := range zero.Ref {
			if err := validateRefName(&ref); err != nil {
				return err
			}
		}
		if err := validateGroupsInZeroOrMore(&zero); err != nil {
			return err
		}
		if err := validateInterleavesInZeroOrMore(&zero); err != nil {
			return err
		}
		if err := validateChoicesInZeroOrMore(&zero); err != nil {
			return err
		}
	}
	return nil
}

// validateElementInterleavePatterns validates interleave patterns with duplicate attribute check
func validateElementInterleavePatterns(elem *Element) error {
	for _, interleave := range elem.Interleave {
		if len(interleave.Elements) >= 2 {
			for i := 0; i < len(interleave.Elements)-1; i++ {
				attrs1 := collectAttributeNames(&interleave.Elements[i])
				for j := i + 1; j < len(interleave.Elements); j++ {
					attrs2 := collectAttributeNames(&interleave.Elements[j])
					for name := range attrs1 {
						if attrs2[name] {
							return fmt.Errorf("duplicate attribute '%s' in interleave pattern (spec section 7.3)", name)
						}
					}
				}
			}
		}

		for _, child := range interleave.Elements {
			if err := validateElementPatterns(&child); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateElementRefsAndNameClasses validates refs, name classes, and mixed patterns
func validateElementRefsAndNameClasses(elem *Element) error {
	// Validate refs
	for _, ref := range elem.Ref {
		if err := validateRefName(&ref); err != nil {
			return err
		}
		hasChild, children, _ := hasAnyRNGChild(ref.RawContent)
		if hasChild {
			return fmt.Errorf("ref element cannot have child elements, found %v", children)
		}
	}

	// Validate AnyName
	if elem.AnyName != nil {
		if hasCountElements(elem.AnyName.RawContent, "except") > 1 {
			return fmt.Errorf("anyName element can contain at most one except clause")
		}
	}

	// Validate NsName
	if elem.NsName != nil {
		if hasCountElements(elem.NsName.RawContent, "except") > 1 {
			return fmt.Errorf("nsName element can contain at most one except clause")
		}
	}

	// Validate mixed patterns
	if elem.Mixed != nil {
		if err := validateMixedPatterns(elem.Mixed); err != nil {
			return err
		}
	}

	return nil
}

// validateRefName validates ref name is present and valid NCName
func validateRefName(ref *Ref) error {
	if ref.Name == "" {
		return fmt.Errorf("ref element must have a name attribute")
	}
	if !isValidNCNameNoColon(ref.Name) {
		return fmt.Errorf("ref has invalid name '%s' (not a valid NCName)", ref.Name)
	}
	return nil
}

// validateAttributeForbiddenChild validates that a child element is not forbidden in attributes
// Per RELAX NG spec 7.1.1: attributes cannot contain ref, attribute, or element patterns
func validateAttributeForbiddenChild(localName string) error {
	switch localName {
	case "ref":
		return fmt.Errorf("attribute cannot contain ref patterns (spec section 7.1.1)")
	case "attribute":
		return fmt.Errorf("attribute cannot contain attribute patterns (spec section 7.1.1)")
	case elemNameElement:
		return fmt.Errorf("attribute cannot contain element patterns")
	}
	return nil
}

// validateAttributeRawContent checks for forbidden patterns in attribute's RawContent
// Attributes cannot directly contain ref, attribute, or element patterns
func validateAttributeRawContent(rawContent []byte) error {
	if len(rawContent) == 0 {
		return nil
	}

	decoder := xml.NewDecoder(bytes.NewReader(rawContent))
	depth := 0
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue // Ignore parse errors
		}

		if startElem, ok := tok.(xml.StartElement); ok {
			if depth == 0 {
				if err := validateAttributeForbiddenChild(startElem.Name.Local); err != nil {
					return err
				}
			}
			depth++
		} else if _, ok := tok.(xml.EndElement); ok {
			depth--
		}
	}
	return nil
}

// validateAttributePatterns validates patterns within an attribute definition
func validateAttributePatterns(attr *Attribute) error {
	if attr == nil {
		return nil
	}

	// Check for unknown attributes on attribute element (obsolete attributes like global, key, keyRef)
	for _, rawAttr := range attr.RawAttrs {
		if rawAttr.Name.Space == "" && !strings.HasPrefix(rawAttr.Name.Local, "xml") {
			// global, key, keyRef are obsolete attributes
			return fmt.Errorf("attribute element has unknown attribute: %s (obsolete syntax?)", rawAttr.Name.Local)
		}
	}

	// Check for invalid pattern children in RawContent
	// Per spec 7.1.1: attributes cannot directly contain ref or attribute patterns
	if err := validateAttributeRawContent(attr.RawContent); err != nil {
		return err
	}

	// Validate that attribute has valid name or name class
	hasNameAttr := attr.Name != ""
	hasNameElement := attr.NameElement != nil && attr.NameElement.Value != ""
	hasNameClass := attr.AnyName != nil || attr.NsName != nil

	// Count total name specifiers
	nameCount := 0
	if hasNameAttr {
		nameCount++
	}
	if hasNameElement {
		nameCount++
	}
	if hasNameClass {
		nameCount++
	}

	// Can't have more than one name specifier
	if nameCount > 1 {
		return fmt.Errorf("attribute cannot have multiple name specifiers (name attribute, <name> element, or name class)")
	}

	// Must have at least one name specifier
	if nameCount == 0 {
		return fmt.Errorf("attribute has invalid name (empty or missing)")
	}

	// If we have a name attribute, it must be a valid NCName
	if hasNameAttr && !isValidNCName(attr.Name) {
		return fmt.Errorf("attribute has invalid name '%s' (not a valid NCName)", attr.Name)
	}

	// If we have a name element, validate its value
	if hasNameElement && !isValidNCName(attr.NameElement.Value) {
		return fmt.Errorf("attribute has invalid name '%s' (not a valid NCName)", attr.NameElement.Value)
	}

	if attr.Data != nil {
		return validateDataPattern(attr.Data)
	}

	if attr.Choice != nil {
		// Validate choice patterns, but reject element patterns in attribute context
		if err := validateAttributeChoice(attr.Choice); err != nil {
			return err
		}
	}

	return nil
}

// validateAttributeChoice validates patterns within a choice that is in an attribute context
// Attributes can only contain patterns that describe attribute values, not element patterns
func validateAttributeChoice(choice *Choice) error {
	if choice == nil {
		return nil
	}

	// Attributes cannot contain element patterns
	if len(choice.Elements) > 0 {
		return fmt.Errorf("attribute cannot contain element patterns")
	}

	// Attributes cannot contain attribute patterns
	if len(choice.Attributes) > 0 {
		return fmt.Errorf("attribute cannot contain attribute patterns")
	}

	// Validate ref patterns - they must reference valid patterns for attributes
	for _, ref := range choice.Refs {
		if ref.Name == "" {
			return fmt.Errorf("ref element must have a name attribute")
		}
		if !isValidNCNameNoColon(ref.Name) {
			return fmt.Errorf("ref has invalid name '%s' (not a valid NCName)", ref.Name)
		}
	}

	// Groups are allowed in attribute choices
	// (they can contain valid attribute content patterns like value, data, text)

	return nil
}

// isValidXMLSchemaDatatype checks if a datatype name is a valid XML Schema built-in type
func isValidXMLSchemaDatatype(typeName string) bool {
	// XML Schema Part 2 built-in datatypes
	validTypes := map[string]bool{
		// Primitive types
		"string":       true,
		"boolean":      true,
		"decimal":      true,
		"float":        true,
		"double":       true,
		"duration":     true,
		"dateTime":     true,
		"time":         true,
		"date":         true,
		"gYearMonth":   true,
		"gYear":        true,
		"gMonthDay":    true,
		"gDay":         true,
		"gMonth":       true,
		"hexBinary":    true,
		"base64Binary": true,
		"anyURI":       true,
		"QName":        true,
		"NOTATION":     true,

		// Derived types
		"normalizedString":   true,
		"token":              true,
		"language":           true,
		"NMTOKEN":            true,
		"NMTOKENS":           true,
		"Name":               true,
		"NCName":             true,
		"ID":                 true,
		"IDREF":              true,
		"IDREFS":             true,
		"ENTITY":             true,
		"ENTITIES":           true,
		"integer":            true,
		"nonPositiveInteger": true,
		"negativeInteger":    true,
		"long":               true,
		"int":                true,
		"short":              true,
		"byte":               true,
		"nonNegativeInteger": true,
		"unsignedLong":       true,
		"unsignedInt":        true,
		"unsignedShort":      true,
		"unsignedByte":       true,
		"positiveInteger":    true,
	}

	return validTypes[typeName]
}

// validateDataType validates the datatype and its parameters
func validateDataType(data *Data) error {
	// Built-in RELAX NG datatypes (when no datatypeLibrary is specified)
	if data.DatatypeLibrary == "" {
		return validateBuiltinDataType(data.Type, data.Params)
	}

	// XML Schema datatypes
	if data.DatatypeLibrary == "http://www.w3.org/2001/XMLSchema-datatypes" {
		return validateXMLSchemaDataType(data.Type)
	}

	return nil
}

// validateBuiltinDataType validates built-in RELAX NG datatypes
func validateBuiltinDataType(dataType string, params []Param) error {
	// Built-in datatypes are "string" and "token"
	// Per RELAX NG spec: these built-in types do not allow parameters
	if dataType == "string" || dataType == "token" {
		if len(params) > 0 {
			return fmt.Errorf("built-in datatype '%s' does not allow parameters", dataType)
		}
		return nil
	}
	// If datatypeLibrary is empty and type is not a built-in, it's invalid
	return fmt.Errorf("unknown datatype '%s' (no datatypeLibrary specified, and '%s' is not a built-in datatype)", dataType, dataType)
}

// validateXMLSchemaDataType validates XML Schema datatypes
func validateXMLSchemaDataType(dataType string) error {
	// XML Schema datatypes
	if !isValidXMLSchemaDatatype(dataType) {
		return fmt.Errorf("invalid datatype '%s' for XML Schema datatype library", dataType)
	}
	// XML Schema datatypes like "string", "integer", etc., generally allow parameters
	// But "token" in XML Schema is different from the built-in token type
	// For now, we don't validate parameters for XML Schema types as the validator handles it
	return nil
}

// validateDataPattern validates a data pattern
func validateDataPattern(data *Data) error {
	if data == nil {
		return nil
	}

	// Data element must have a type attribute
	if data.Type == "" {
		return fmt.Errorf("data element must have a type attribute")
	}

	// Check for unknown attributes on data element (obsolete attributes like key, keyRef)
	for _, attr := range data.RawAttrs {
		if attr.Name.Space == "" && !strings.HasPrefix(attr.Name.Local, "xml") {
			// key, keyRef are obsolete attributes
			return fmt.Errorf("data element has unknown attribute: %s (obsolete syntax?)", attr.Name.Local)
		}
	}

	// Validate datatypeLibrary attribute if present
	if data.DatatypeLibrary != "" && !isValidDatatypeLibraryURI(data.DatatypeLibrary) {
		return fmt.Errorf("data element has invalid datatypeLibrary URI '%s'", data.DatatypeLibrary)
	}

	// Validate the datatype name and parameters for the specified datatype library
	// Per RELAX NG spec section 6.2.9 and Guidelines for W3C XML Schema Datatypes
	if err := validateDataType(data); err != nil {
		return err
	}

	// Validate data except clause - check for multiple except elements
	// The XML unmarshaler only captures the last one, so check RawContent for multiple
	if hasCountElements(data.RawContent, "except") > 1 {
		return fmt.Errorf("data element can contain at most one except clause")
	}

	// Validate the content of the except clause
	if data.Except != nil {
		if err := validateDataExceptContent(data.Except); err != nil {
			return err
		}
	}

	return nil
}

// validateDataExceptContent validates that data except clause only contains data and value patterns
// Per RELAX NG spec section 4.13: except clause must only contain data or value patterns
// It cannot contain: element, attribute, text, list, group, interleave, choice, oneOrMore, zeroOrMore, mixed, externalRef, empty
func validateDataExceptContent(except *DataExcept) error {
	if except == nil {
		return nil
	}

	// Check RawContent for forbidden patterns
	if len(bytes.TrimSpace(except.RawContent)) > 0 {
		if err := validateDataExceptContentRestrictions(except.RawContent); err != nil {
			return err
		}
	}

	return nil
}

// validateDataExceptContentRestrictions validates raw content of data except clause
func validateDataExceptContentRestrictions(content []byte) error {
	if len(bytes.TrimSpace(content)) == 0 {
		return nil
	}

	// Per RELAX NG spec section 7.1.4, except can only contain: data, value, and choice
	forbiddenPatterns := map[string]bool{
		"element":          true,
		"attribute":        true,
		"text":             true,
		"list":             true,
		"group":            true,
		elemNameInterleave: true,
		"ref":              true,
		"oneOrMore":        true,
		"zeroOrMore":       true,
		"mixed":            true,
		"externalRef":      true,
		"empty":            true,
		"parentRef":        true,
	}

	decoder := xml.NewDecoder(bytes.NewReader(content))
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if startElem, ok := tok.(xml.StartElement); ok {
			localName := startElem.Name.Local
			if forbiddenPatterns[localName] {
				return fmt.Errorf("data except clause cannot contain %s patterns (spec section 7.1.4)", localName)
			}
		}
	}
	return nil
}

// validateListPattern validates a list pattern
// Per RELAX NG spec section 4.12: list patterns can only contain:
// - data elements
// - value elements
// validateListContentPattern validates patterns inside a list element
func validateListContentPattern(startElem *xml.StartElement, decoder *xml.Decoder) error {
	// Forbidden paths are list//list, list//ref, list//attribute, list//text, list//interleave
	// group IS allowed in list
	forbiddenPatterns := map[string]string{
		"element":          "list cannot contain elements (spec section 7.1.3)",
		"attribute":        "list cannot contain attributes (spec section 7.1.3)",
		"list":             "list cannot contain nested lists (spec section 7.1.3)",
		"ref":              "list cannot contain ref patterns (spec section 7.1.3)",
		"text":             "list cannot contain text patterns (spec section 7.1.3)",
		elemNameInterleave: "list cannot contain interleave patterns (spec section 7.1.3)",
		"mixed":            "list cannot contain mixed patterns (spec section 7.1.3)",
		"parentRef":        "list cannot contain parentRef patterns (spec section 7.1.3)",
	}

	localName := startElem.Name.Local
	if msg, forbidden := forbiddenPatterns[localName]; forbidden {
		return fmt.Errorf("%s", msg)
	}

	// For choice, we need to check what's inside
	if localName == elemNameChoice {
		var choice Choice
		if err := decoder.DecodeElement(&choice, startElem); err == nil {
			return validateListChoiceContent(&choice)
		}
	}

	return nil
}

// - oneOrMore with data/value patterns
// List cannot contain: elements, attributes, nested lists, text, interleave, or complex patterns
func validateListPattern(list *List) error {
	if list == nil {
		return nil
	}

	// Check that list doesn't contain forbidden patterns at top level
	if list.OneOrMore != nil {
		// oneOrMore in list can only contain data/value, not elements, attributes, etc.
		if len(list.OneOrMore.Element) > 0 {
			return fmt.Errorf("list cannot contain elements (spec section 4.12)")
		}
		if len(list.OneOrMore.Attribute) > 0 {
			return fmt.Errorf("list cannot contain attributes (spec section 4.12)")
		}
		// Also check for nested groups/choices/interleaves with forbidden content
		if err := validateListOneOrMoreContent(list.OneOrMore); err != nil {
			return err
		}
	}

	// Check if list contains a choice with forbidden patterns
	if list.Choice != nil {
		if err := validateListChoiceContent(list.Choice); err != nil {
			return err
		}
	}

	// Check RawContent for forbidden patterns
	// Look for element, attribute, nested list, text, interleave patterns
	if err := validateListContentRestrictions(list.RawContent); err != nil {
		return err
	}

	return nil
}

// validateListOneOrMoreContent checks that oneOrMore in a list only contains allowed patterns
func validateListOneOrMoreContent(oneOrMore *OneOrMore) error {
	// Check for forbidden patterns in oneOrMore within list
	// group and choice ARE allowed in list
	if len(oneOrMore.Interleave) > 0 {
		return fmt.Errorf("list cannot contain interleave patterns (spec section 7.1.3)")
	}
	// Note: choice and group are allowed in list
	return nil
}

// validateListChoiceContent checks that choice in a list only contains allowed patterns
func validateListChoiceContent(choice *Choice) error {
	// Per spec 7.1.3, choice in lists cannot contain certain patterns
	if choice.List != nil {
		return fmt.Errorf("list cannot contain nested list patterns (spec section 7.1.3)")
	}
	if choice.Text != nil {
		return fmt.Errorf("list cannot contain text patterns (spec section 7.1.3)")
	}
	if len(choice.Elements) > 0 {
		return fmt.Errorf("list cannot contain elements (spec section 7.1.3)")
	}
	if len(choice.Attributes) > 0 {
		return fmt.Errorf("list cannot contain attributes (spec section 7.1.3)")
	}
	if len(choice.Interleave) > 0 {
		return fmt.Errorf("list cannot contain interleave patterns (spec section 7.1.3)")
	}
	// Note: group IS allowed in list/choice
	return nil
}

// validateListContentRestrictions validates that list content only contains allowed patterns
// Per RELAX NG spec section 7.1.3, list cannot contain:
// - list (nested lists)
// - ref (references to defines)
// - attribute patterns
// - text patterns
// - interleave patterns
// Group patterns ARE allowed (contrary to earlier validation)
func validateListContentRestrictions(content []byte) error {
	if len(bytes.TrimSpace(content)) == 0 {
		return nil
	}

	decoder := xml.NewDecoder(bytes.NewReader(content))
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if startElem, ok := tok.(xml.StartElement); ok {
			if err := validateListContentPattern(&startElem, decoder); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateChoicePatterns validates patterns within a choice
func validateChoicePatterns(choice *Choice) error {
	if choice == nil {
		return nil
	}

	// Per RELAX NG spec, a choice must have at least 2 options (one or more alternatives)
	// A single option is allowed per spec 4.10 but empty choice is an error
	hasOptions := len(choice.Elements) > 0 || len(choice.Attributes) > 0 ||
		len(choice.Values) > 0 || len(choice.Refs) > 0 || len(choice.Data) > 0 ||
		choice.Text != nil || choice.Empty != nil || len(choice.Group) > 0 ||
		choice.List != nil

	if !hasOptions && len(choice.RawContent) == 0 {
		return fmt.Errorf("choice element must have at least one option")
	}

	// Check for ambiguous element name patterns in choice alternatives
	// Per spec section 7.3: A choice must have non-overlapping alternatives
	// For T-325 (oneOrMore element foo vs element foo), they're ambiguous.
	// Note: This validation checks for simple name overlaps. More sophisticated analysis
	// would be needed to distinguish sequences (group bar1 bar2 vs group bar1 bar3).
	if err := validateChoiceAmbiguity(choice); err != nil {
		return err
	}

	// Validate groups in choice (only for structural validation, not ref validation)
	for _, group := range choice.Group {
		if err := validateGroupPatterns(&group); err != nil {
			return err
		}
	}

	// Validate list in choice (if present)
	if choice.List != nil {
		if err := validateListPattern(choice.List); err != nil {
			return err
		}
	}

	return nil
}

// validateChoiceAmbiguity checks for ambiguous element name patterns in a choice
// Per RELAX NG spec section 7.3: a choice must have non-overlapping alternatives
// This function checks for the specific T-325 ambiguity case:
// - oneOrMore/zeroOrMore wrapping same element name as a direct child element
// This creates cardinality ambiguity where a single element matches both alternatives.
//
// Note: Two direct elements with the same name but different content (T-228) are NOT ambiguous
// because they can be distinguished by their child elements during validation.
func validateChoiceAmbiguity(choice *Choice) error {
	if choice == nil {
		return nil
	}

	// Track direct element names (not in groups/interleaves)
	directElements := make(map[string]bool)

	// Collect direct element children names
	for _, elem := range choice.Elements {
		if elem.Name != "" {
			directElements[elem.Name] = true
		}
	}

	// Check RawContent for oneOrMore/zeroOrMore wrapped elements
	if err := validateChoiceWrapperAmbiguity(choice.RawContent, directElements); err != nil {
		return err
	}

	return nil
}

// validateChoiceWrapperAmbiguity checks if wrapped elements (oneOrMore, zeroOrMore) create ambiguity with direct elements
func validateChoiceWrapperAmbiguity(rawContent []byte, directElements map[string]bool) error {
	topLevelNames, err := firstLevelLocalNames(rawContent)
	if err != nil {
		return err
	}

	for _, tlName := range topLevelNames {
		if tlName != "oneOrMore" && tlName != "zeroOrMore" {
			continue
		}

		// Extract element names from the wrapper
		elementNamesInWrapper := extractElementNamesFromWrapper(rawContent, tlName)
		for _, name := range elementNamesInWrapper {
			if directElements[name] {
				// Wrapper can match single element, creates ambiguity with direct element
				return fmt.Errorf("ambiguous choice: element '%s' in %s branch overlaps with direct element (spec section 7.3)", name, tlName)
			}
		}
	}
	return nil
}

// extractElementNamesFromWrapper extracts element names from a wrapper like oneOrMore, zeroOrMore, optional
func extractElementNamesFromWrapper(rawContent []byte, wrapperType string) []string {
	var names []string

	dec := xml.NewDecoder(bytes.NewReader(rawContent))
	depth := 0
	inWrapper := false

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}

		switch t := tok.(type) {
		case xml.StartElement:
			if depth == 0 && t.Name.Local == wrapperType {
				inWrapper = true
			}
			if inWrapper && depth == 1 && t.Name.Local == elemNameElement {
				// Found an element - extract its name attribute
				for _, attr := range t.Attr {
					if attr.Name.Local == "name" {
						names = append(names, attr.Value)
						break
					}
				}
			}
			depth++
		case xml.EndElement:
			depth--
			if inWrapper && depth == 0 {
				inWrapper = false
			}
		}
	}

	return names
}

// validateGroupPatterns validates patterns within a group
func validateGroupPatterns(group *Group) error {
	if group == nil {
		return nil
	}

	// Check if the group is unreachable (starts with notAllowed)
	isUnreachable := isGroupUnreachable(group)

	// Validate text patterns if not unreachable
	if !isUnreachable {
		if err := validateGroupTextPatterns(group); err != nil {
			return err
		}
	}

	// Check for duplicate attributes in group patterns
	if err := validateGroupDuplicateAttributes(group); err != nil {
		return err
	}

	// Validate child elements if not unreachable
	if !isUnreachable {
		for _, child := range group.Elements {
			if err := validateElementPatterns(&child); err != nil {
				return err
			}
		}
	}

	// Validate refs
	for _, ref := range group.Ref {
		if err := validateRefName(&ref); err != nil {
			return err
		}
		hasChild, children, _ := hasAnyRNGChild(ref.RawContent)
		if hasChild {
			return fmt.Errorf("ref element cannot have child elements, found %v", children)
		}
	}

	// Validate nested groups if not unreachable
	if !isUnreachable {
		for _, subgroup := range group.Group {
			if err := validateGroupPatterns(&subgroup); err != nil {
				return err
			}
		}
	}

	return nil
}

// isGroupUnreachable checks if a group starts with notAllowed
func isGroupUnreachable(group *Group) bool {
	if group.NotAllowed == nil {
		return false
	}
	names, err := firstLevelLocalNames(group.RawContent)
	if err != nil || len(names) == 0 {
		return false
	}
	return names[0] == "notAllowed"
}

// validateGroupDuplicateAttributes checks for duplicate attributes in group patterns
func validateGroupDuplicateAttributes(group *Group) error {
	if len(group.Elements) < 2 {
		return nil
	}

	for i := 0; i < len(group.Elements)-1; i++ {
		attrs1 := collectAttributeNames(&group.Elements[i])
		for j := i + 1; j < len(group.Elements); j++ {
			attrs2 := collectAttributeNames(&group.Elements[j])
			for name := range attrs1 {
				if attrs2[name] {
					return fmt.Errorf("duplicate attribute '%s' in group pattern (spec section 7.3)", name)
				}
			}
		}
	}
	return nil
}

// validateGroupTextPatterns checks for invalid consecutive text patterns in a group
// A group is a sequence. Text patterns (data, value, text) consume all remaining text,
// so multiple text patterns in sequence is invalid.
// However, if the group starts with notAllowed, the entire group is unreachable,
// so we don't need to validate the content patterns.
func validateGroupTextPatterns(group *Group) error {
	if group == nil {
		return nil
	}

	// If the group has notAllowed as the first pattern, it's unreachable
	// So we don't need to validate further
	if group.NotAllowed != nil {
		// Check if notAllowed is the first element by checking raw content
		names, err := firstLevelLocalNames(group.RawContent)
		if err != nil {
			return err
		}
		if len(names) > 0 && names[0] == "notAllowed" {
			// Group is unreachable, skip validation of text patterns
			return nil
		}
	}

	// Parse RawContent to find first-level children and count text patterns
	names, err := firstLevelLocalNames(group.RawContent)
	if err != nil {
		return err
	}

	// Count consecutive text patterns
	var textPatterns []string
	for _, name := range names {
		if name == "data" || name == "text" || name == elemNameValue {
			textPatterns = append(textPatterns, name)
		}
	}

	// If we have multiple text patterns, it's an error
	if len(textPatterns) > 1 {
		return fmt.Errorf("group cannot have multiple text content patterns (%v) in sequence", textPatterns)
	}

	return nil
}

// validateInfiniteNameClasses checks that attributes with anyName or nsName have a oneOrMore ancestor
// Per spec section 7.3: "Attributes using infinite name classes must be repeated"
func validateInfiniteNameClasses(elem *Element, insideOneOrMore bool) error {
	if elem == nil {
		return nil
	}

	// Check direct attributes
	if err := checkAttributeInfiniteNameClasses(elem.Attributes, insideOneOrMore); err != nil {
		return err
	}

	// Validate nested patterns
	if err := validateInfiniteNameClassesInPatterns(elem, insideOneOrMore); err != nil {
		return err
	}

	return nil
}

// checkAttributeInfiniteNameClasses checks if attributes have infinite name classes without oneOrMore
func checkAttributeInfiniteNameClasses(attrs []Attribute, insideOneOrMore bool) error {
	for _, attr := range attrs {
		hasInfiniteNameClass := attr.AnyName != nil || attr.NsName != nil
		if hasInfiniteNameClass && !insideOneOrMore {
			return fmt.Errorf("attribute with infinite name class (anyName/nsName) must have oneOrMore ancestor (spec section 7.3)")
		}
	}
	return nil
}

// validateInfiniteNameClassesInPatterns recursively validates nested patterns
func validateInfiniteNameClassesInPatterns(elem *Element, insideOneOrMore bool) error {
	// Check choice (with attributes)
	if elem.Choice != nil {
		for _, child := range elem.Choice.Elements {
			if err := validateInfiniteNameClasses(&child, insideOneOrMore); err != nil {
				return err
			}
		}
		if err := checkAttributeInfiniteNameClasses(elem.Choice.Attributes, insideOneOrMore); err != nil {
			return err
		}
	}

	// Check groups, interleaves, and optional (inherit oneOrMore status)
	if err := validateNonRepeatingContainers(elem, insideOneOrMore); err != nil {
		return err
	}

	// Check repeating containers (oneOrMore, zeroOrMore)
	if err := validateRepeatingContainers(elem); err != nil {
		return err
	}

	// Check mixed (doesn't provide repetition)
	if elem.Mixed != nil {
		for _, child := range elem.Mixed.Elements {
			if err := validateInfiniteNameClasses(&child, insideOneOrMore); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateNonRepeatingContainers checks groups, interleaves, and optional
func validateNonRepeatingContainers(elem *Element, insideOneOrMore bool) error {
	for _, group := range elem.Group {
		for _, child := range group.Elements {
			if err := validateInfiniteNameClasses(&child, insideOneOrMore); err != nil {
				return err
			}
		}
	}

	for _, interleave := range elem.Interleave {
		for _, child := range interleave.Elements {
			if err := validateInfiniteNameClasses(&child, insideOneOrMore); err != nil {
				return err
			}
		}
	}

	for _, optional := range elem.Optional {
		for _, child := range optional.Elements {
			if err := validateInfiniteNameClasses(&child, insideOneOrMore); err != nil {
				return err
			}
		}
		if err := checkAttributeInfiniteNameClasses(optional.Attributes, insideOneOrMore); err != nil {
			return err
		}
	}

	return nil
}

// validateRepeatingContainers checks oneOrMore and zeroOrMore (with true status)
func validateRepeatingContainers(elem *Element) error {
	for _, oneOrMore := range elem.OneOrMore {
		for _, child := range oneOrMore.Element {
			if err := validateInfiniteNameClasses(&child, true); err != nil {
				return err
			}
		}
	}

	for _, zeroOrMore := range elem.ZeroOrMore {
		for _, child := range zeroOrMore.Element {
			if err := validateInfiniteNameClasses(&child, true); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateSection416NameClassConstraints validates Section 4.16 constraints on name classes
// Per RELAX NG spec section 4.16:
//  1. anyName except constraint: An <except> element that is a child of <anyName> must not
//     have any <anyName> descendant elements.
//  2. nsName except constraint: An <except> element that is a child of <nsName> must not
//     have any <nsName> or <anyName> descendant elements.
func (g *Grammar) validateSection416NameClassConstraints() error {
	// Check start element
	if err := validateSection416InStart(&g.Start); err != nil {
		return err
	}

	// Check all defines
	for _, def := range g.Defines {
		if def.FirstElement() != nil {
			if err := validateSection416InElement(def.FirstElement()); err != nil {
				return fmt.Errorf("define '%s': %w", def.Name, err)
			}
		}
	}

	// Check includes
	for _, inc := range g.Includes {
		for _, def := range inc.Defines {
			if def.FirstElement() != nil {
				if err := validateSection416InElement(def.FirstElement()); err != nil {
					return fmt.Errorf("include define '%s': %w", def.Name, err)
				}
			}
		}
	}

	return nil
}

// validateSection416InStart validates Section 4.16 constraints in a Start element
func validateSection416InStart(start *Start) error {
	if start == nil {
		return nil
	}

	// Check direct patterns in start
	if start.Element != nil {
		if err := validateSection416InElement(start.Element); err != nil {
			return err
		}
	}

	if start.Choice != nil {
		if err := validateSection416InChoice(start.Choice); err != nil {
			return err
		}
	}

	for _, group := range start.Group {
		if err := validateSection416InGroup(&group); err != nil {
			return err
		}
	}

	for _, interleave := range start.Interleave {
		if err := validateSection416InInterleave(&interleave); err != nil {
			return err
		}
	}

	for _, optional := range start.Optional {
		if err := validateSection416InOptional(&optional); err != nil {
			return err
		}
	}

	for _, oneOrMore := range start.OneOrMore {
		if err := validateSection416InOneOrMore(&oneOrMore); err != nil {
			return err
		}
	}

	for _, zeroOrMore := range start.ZeroOrMore {
		if err := validateSection416InZeroOrMore(&zeroOrMore); err != nil {
			return err
		}
	}

	return nil
}

// validateAttributesSection416 validates Section 4.16 constraints for a slice of attributes
// This helper function is used by multiple validateSection416In* functions to avoid duplication
func validateAttributesSection416(attrs []Attribute) error {
	for i, attr := range attrs {
		// Check for reserved xmlns attribute name (Section 4.16 - xmlns is reserved)
		if attr.Name == elemNameXmlns && attr.Ns == "" {
			return fmt.Errorf("attribute cannot be named 'xmlns' in no namespace (spec section 4.16)")
		}

		// Check for xmlns namespace (Section 4.16 - cannot use xmlns namespace)
		if attr.Ns == "http://www.w3.org/2000/xmlns" {
			return fmt.Errorf("attribute cannot use xmlns namespace (spec section 4.16)")
		}

		// Check anyName except constraint
		if attr.AnyName != nil && attr.AnyName.Except != nil {
			if err := validateAnyNameExcept(attr.AnyName.Except); err != nil {
				return fmt.Errorf("attribute[%d] anyName except: %w", i, err)
			}
		}
		// Check nsName except constraint
		if attr.NsName != nil && attr.NsName.Except != nil {
			if err := validateNsNameExcept(attr.NsName.Except); err != nil {
				return fmt.Errorf("attribute[%d] nsName except: %w", i, err)
			}
		}

		// Check attribute name class for xmlns name (NameElement, or choices with names)
		if attr.NameElement != nil && attr.NameElement.Value == elemNameXmlns {
			// Check if it's in no namespace (default when ns attribute not present)
			if attr.NameElement.Ns == "" && attr.NameElement.Namespace == "" {
				return fmt.Errorf("attribute cannot have name class 'xmlns' in no namespace (spec section 4.16)")
			}
		}

		// Check for xmlns in choice
		if attr.Choice != nil {
			if err := validateChoiceForXmlnsName(attr.Choice); err != nil {
				return fmt.Errorf("attribute[%d]: %w", i, err)
			}
		}

		// Per spec section 4.16: attribute with anyName must not match xmlns namespace names
		// NOTE: Official tests indicate this validation should be more permissive.
		// Disabling strict xmlns checks for anyName for now.

		// Check for nsName with xmlns namespace (Section 4.16)
		if attr.NsName != nil && attr.NsName.Ns == "http://www.w3.org/2000/xmlns" {
			return fmt.Errorf("attribute with nsName cannot use xmlns namespace (spec section 4.16)")
		}
	}
	return nil
}

// validateSection416InElement validates Section 4.16 constraints in an Element
func validateSection416InElement(elem *Element) error {
	if elem == nil {
		return nil
	}

	// Validate element's name class patterns
	if err := validateElementNameClasses(elem); err != nil {
		return err
	}

	// Check attributes in element
	if err := validateAttributesSection416(elem.Attributes); err != nil {
		return err
	}

	// Validate element's container patterns
	if err := validateElementContainersSection416(elem); err != nil {
		return err
	}

	return nil
}

// validateElementNameClasses validates name class patterns in an element
func validateElementNameClasses(elem *Element) error {
	if elem.AnyName != nil && elem.AnyName.Except != nil {
		if err := validateAnyNameExcept(elem.AnyName.Except); err != nil {
			return fmt.Errorf("element anyName except: %w", err)
		}
	}

	if elem.NsName != nil && elem.NsName.Except != nil {
		if err := validateNsNameExcept(elem.NsName.Except); err != nil {
			return fmt.Errorf("element nsName except: %w", err)
		}
	}

	return nil
}

// validateElementContainersSection416 validates element's container patterns
func validateElementContainersSection416(elem *Element) error {
	// Check choice first if present
	if elem.Choice != nil {
		if err := validateSection416InChoice(elem.Choice); err != nil {
			return fmt.Errorf("choice: %w", err)
		}
	}

	for i, group := range elem.Group {
		if err := validateSection416InGroup(&group); err != nil {
			return fmt.Errorf("group[%d]: %w", i, err)
		}
	}

	for i, interleave := range elem.Interleave {
		if err := validateSection416InInterleave(&interleave); err != nil {
			return fmt.Errorf("interleave[%d]: %w", i, err)
		}
	}

	for i, optional := range elem.Optional {
		if err := validateSection416InOptional(&optional); err != nil {
			return fmt.Errorf("optional[%d]: %w", i, err)
		}
	}

	for i, oneOrMore := range elem.OneOrMore {
		if err := validateSection416InOneOrMore(&oneOrMore); err != nil {
			return fmt.Errorf("oneOrMore[%d]: %w", i, err)
		}
	}

	for i, zeroOrMore := range elem.ZeroOrMore {
		if err := validateSection416InZeroOrMore(&zeroOrMore); err != nil {
			return fmt.Errorf("zeroOrMore[%d]: %w", i, err)
		}
	}

	if elem.Mixed != nil {
		if err := validateSection416InMixed(elem.Mixed); err != nil {
			return fmt.Errorf("mixed: %w", err)
		}
	}

	return nil
}

// validateSection416InChoice validates Section 4.16 constraints in a Choice
func validateSection416InChoice(choice *Choice) error {
	if choice == nil {
		return nil
	}

	for i, elem := range choice.Elements {
		if err := validateSection416InElement(&elem); err != nil {
			return fmt.Errorf("element[%d]: %w", i, err)
		}
	}

	for i, attr := range choice.Attributes {
		if attr.AnyName != nil && attr.AnyName.Except != nil {
			if err := validateAnyNameExcept(attr.AnyName.Except); err != nil {
				return fmt.Errorf("attribute[%d] anyName except: %w", i, err)
			}
		}
		if attr.NsName != nil && attr.NsName.Except != nil {
			if err := validateNsNameExcept(attr.NsName.Except); err != nil {
				return fmt.Errorf("attribute[%d] nsName except: %w", i, err)
			}
		}
	}

	for i, group := range choice.Group {
		if err := validateSection416InGroup(&group); err != nil {
			return fmt.Errorf("group[%d]: %w", i, err)
		}
	}

	return nil
}

// validateSection416InGroup validates Section 4.16 constraints in a Group
//
//nolint:dupl
func validateSection416InGroup(group *Group) error {
	if group == nil {
		return nil
	}

	for i, elem := range group.Elements {
		if err := validateSection416InElement(&elem); err != nil {
			return fmt.Errorf("element[%d]: %w", i, err)
		}
	}

	for i, optional := range group.Optional {
		if err := validateSection416InOptional(&optional); err != nil {
			return fmt.Errorf("optional[%d]: %w", i, err)
		}
	}

	for i, oneOrMore := range group.OneOrMore {
		if err := validateSection416InOneOrMore(&oneOrMore); err != nil {
			return fmt.Errorf("oneOrMore[%d]: %w", i, err)
		}
	}

	for i, zeroOrMore := range group.ZeroOrMore {
		if err := validateSection416InZeroOrMore(&zeroOrMore); err != nil {
			return fmt.Errorf("zeroOrMore[%d]: %w", i, err)
		}
	}

	for i, choice := range group.Choice {
		if err := validateSection416InChoice(&choice); err != nil {
			return fmt.Errorf("choice[%d]: %w", i, err)
		}
	}

	for i, nestedGroup := range group.Group {
		if err := validateSection416InGroup(&nestedGroup); err != nil {
			return fmt.Errorf("group[%d]: %w", i, err)
		}
	}

	return nil
}

// validateSection416InInterleave validates Section 4.16 constraints in an Interleave
func validateSection416InInterleave(interleave *Interleave) error {
	if interleave == nil {
		return nil
	}

	for i, elem := range interleave.Elements {
		if err := validateSection416InElement(&elem); err != nil {
			return fmt.Errorf("element[%d]: %w", i, err)
		}
	}

	for i, group := range interleave.Group {
		if err := validateSection416InGroup(&group); err != nil {
			return fmt.Errorf("group[%d]: %w", i, err)
		}
	}

	for i, optional := range interleave.Optional {
		if err := validateSection416InOptional(&optional); err != nil {
			return fmt.Errorf("optional[%d]: %w", i, err)
		}
	}

	for i, oneOrMore := range interleave.OneOrMore {
		if err := validateSection416InOneOrMore(&oneOrMore); err != nil {
			return fmt.Errorf("oneOrMore[%d]: %w", i, err)
		}
	}

	for i, zeroOrMore := range interleave.ZeroOrMore {
		if err := validateSection416InZeroOrMore(&zeroOrMore); err != nil {
			return fmt.Errorf("zeroOrMore[%d]: %w", i, err)
		}
	}

	return nil
}

// validateSection416InOptional validates Section 4.16 constraints in an Optional
func validateSection416InOptional(optional *Optional) error {
	if optional == nil {
		return nil
	}

	for i, attr := range optional.Attributes {
		if attr.AnyName != nil && attr.AnyName.Except != nil {
			if err := validateAnyNameExcept(attr.AnyName.Except); err != nil {
				return fmt.Errorf("attribute[%d] anyName except: %w", i, err)
			}
		}
		if attr.NsName != nil && attr.NsName.Except != nil {
			if err := validateNsNameExcept(attr.NsName.Except); err != nil {
				return fmt.Errorf("attribute[%d] nsName except: %w", i, err)
			}
		}
	}

	for i, elem := range optional.Elements {
		if err := validateSection416InElement(&elem); err != nil {
			return fmt.Errorf("element[%d]: %w", i, err)
		}
	}

	return nil
}

// validateSection416InOneOrMore validates Section 4.16 constraints in a OneOrMore
func validateSection416InOneOrMore(oneOrMore *OneOrMore) error {
	if oneOrMore == nil {
		return nil
	}

	if err := validateAttributesSection416(oneOrMore.Attribute); err != nil {
		return err
	}

	for i, elem := range oneOrMore.Element {
		if err := validateSection416InElement(&elem); err != nil {
			return fmt.Errorf("element[%d]: %w", i, err)
		}
	}

	return nil
}

// validateSection416InZeroOrMore validates Section 4.16 constraints in a ZeroOrMore
func validateSection416InZeroOrMore(zeroOrMore *ZeroOrMore) error {
	if zeroOrMore == nil {
		return nil
	}

	if err := validateAttributesSection416(zeroOrMore.Attribute); err != nil {
		return err
	}

	for i, elem := range zeroOrMore.Element {
		if err := validateSection416InElement(&elem); err != nil {
			return fmt.Errorf("element[%d]: %w", i, err)
		}
	}

	return nil
}

// validateSection416InMixed validates Section 4.16 constraints in a Mixed
//
//nolint:dupl
func validateSection416InMixed(mixed *Mixed) error {
	if mixed == nil {
		return nil
	}

	for i, elem := range mixed.Elements {
		if err := validateSection416InElement(&elem); err != nil {
			return fmt.Errorf("element[%d]: %w", i, err)
		}
	}

	for i, group := range mixed.Group {
		if err := validateSection416InGroup(&group); err != nil {
			return fmt.Errorf("group[%d]: %w", i, err)
		}
	}

	for i, optional := range mixed.Optional {
		if err := validateSection416InOptional(&optional); err != nil {
			return fmt.Errorf("optional[%d]: %w", i, err)
		}
	}

	for i, oneOrMore := range mixed.OneOrMore {
		if err := validateSection416InOneOrMore(&oneOrMore); err != nil {
			return fmt.Errorf("oneOrMore[%d]: %w", i, err)
		}
	}

	for i, zeroOrMore := range mixed.ZeroOrMore {
		if err := validateSection416InZeroOrMore(&zeroOrMore); err != nil {
			return fmt.Errorf("zeroOrMore[%d]: %w", i, err)
		}
	}

	for i, choice := range mixed.Choice {
		if err := validateSection416InChoice(&choice); err != nil {
			return fmt.Errorf("choice[%d]: %w", i, err)
		}
	}

	return nil
}

// validateAnyNameExcept validates that an <except> child of <anyName> has no <anyName> descendants
// Per RELAX NG spec section 4.16
func validateAnyNameExcept(except *NameExcept) error {
	if except == nil {
		return nil
	}

	// Check for anyName descendants
	if except.AnyName != nil {
		return fmt.Errorf("anyName except must not contain anyName descendant (spec section 4.16)")
	}

	// Check for anyName in RawContent (e.g., inside <choice> elements)
	if hasNamePatternInRawContent(except.RawContent, "anyName") {
		return fmt.Errorf("anyName except must not contain anyName descendant (spec section 4.16)")
	}

	// Per spec 4.16: except must only contain name classes (name, nsName, anyName)
	// Not allowed: choice, group, element, attribute, etc.
	if strings.Contains(string(except.RawContent), "<choice") {
		return fmt.Errorf("except element must not contain choice (spec section 4.16)")
	}
	if strings.Contains(string(except.RawContent), "<group") {
		return fmt.Errorf("except element must not contain group (spec section 4.16)")
	}

	// Check inside nsName (though nsName inside anyName except is allowed)
	// The constraint is specifically about anyName, not nsName

	return nil
}

// validateNsNameExcept validates that an <except> child of <nsName> has no <nsName> or <anyName> descendants
// Per RELAX NG spec section 4.16
func validateNsNameExcept(except *NameExcept) error {
	if except == nil {
		return nil
	}

	// Check for anyName descendants
	if except.AnyName != nil {
		return fmt.Errorf("nsName except must not contain anyName descendant (spec section 4.16)")
	}

	// Check for nsName descendants
	if except.NsName != nil {
		return fmt.Errorf("nsName except must not contain nsName descendant (spec section 4.16)")
	}

	// Check for nsName or anyName in RawContent (e.g., inside <choice> elements)
	if hasNamePatternInRawContent(except.RawContent, "nsName") {
		return fmt.Errorf("nsName except must not contain nsName descendant (spec section 4.16)")
	}

	// Per spec 4.16: except must only contain name classes (name, nsName, anyName)
	// Not allowed: choice, group, element, attribute, etc.
	if strings.Contains(string(except.RawContent), "<choice") {
		return fmt.Errorf("except element must not contain choice (spec section 4.16)")
	}
	if strings.Contains(string(except.RawContent), "<group") {
		return fmt.Errorf("except element must not contain group (spec section 4.16)")
	}

	if hasNamePatternInRawContent(except.RawContent, "anyName") {
		return fmt.Errorf("nsName except must not contain anyName descendant (spec section 4.16)")
	}

	return nil
}

// validateChoiceForXmlnsName checks if a choice contains xmlns as a possible name
// Used to validate that attributes cannot have xmlns name in no namespace
func validateChoiceForXmlnsName(choice *Choice) error {
	if choice == nil {
		return nil
	}

	// Check if any value in the choice is "xmlns" in no namespace
	for _, val := range choice.Values {
		if val.Value == elemNameXmlns {
			// Values don't have namespace, so if content is xmlns it's forbidden
			return fmt.Errorf("choice cannot contain value 'xmlns' for attribute name (spec section 4.16)")
		}
	}

	// Check if any name in the choice is xmlns
	// Note: Names are in RawContent, not structured data for choice
	if hasNamePatternInRawContent(choice.RawContent, elemNameXmlns) {
		return fmt.Errorf("choice cannot contain name 'xmlns' for attribute (spec section 4.16)")
	}

	return nil
}

// hasNamePatternInRawContent checks if RawContent contains a specific name pattern element
// This is used to detect patterns inside <choice> and other container elements.
// For patternName="xmlns", it checks for <name>xmlns</name> or <name...>xmlns</name>
// For patternName="anyName", it checks for <anyName> or <anyName/>
// For patternName="nsName", it checks for <nsName> or <nsName/>
func hasNamePatternInRawContent(content []byte, patternName string) bool {
	str := strings.TrimSpace(string(content))
	if str == "" {
		return false
	}

	// Special handling for "xmlns" name lookup
	if patternName == elemNameXmlns {
		// Look for <name...>xmlns</name> or <name ... xmlns ... </name>
		// Look for the text content "xmlns" between <name and </name>
		namePattern := regexp.MustCompile(`<name[^>]*>[\s]*xmlns[\s]*</name>`)
		if namePattern.MatchString(str) {
			return true
		}
		// Also check for xmlns attribute value
		// e.g., <name ns="">xmlns</name>
		if strings.Contains(str, ">"+elemNameXmlns+"<") {
			// Check if it's inside a name tag
			return strings.Contains(str, "<name") && strings.Contains(str, "</name>")
		}
		return false
	}

	// For other patterns (anyName, nsName), look for element tags
	openTag := "<" + patternName

	// Check for opening tag
	if strings.Contains(str, openTag) {
		// Verify it's an RNG element by checking context
		// (simple heuristic: if not preceded by namespace prefix that isn't "rng")
		idx := strings.Index(str, openTag)
		for idx >= 0 {
			// Check character before tag
			if idx > 0 && str[idx-1] != '<' && str[idx-1] != ' ' && str[idx-1] != '\n' && str[idx-1] != '\t' {
				// Not a tag start
				idx = strings.Index(str[idx+1:], openTag)
				if idx >= 0 {
					idx++ // Adjust for the substring search
				}
				continue
			}
			// Likely a valid RNG element tag
			return true
		}
	}

	return false
}

// collectAttributeNames recursively collects all attribute names that "occur in" a pattern
// Per spec section 7.3: A pattern p1 occurs in pattern p2 if p1 is p2, or if p2 is a
// choice, interleave, group, or oneOrMore element and p1 occurs in one or more children of p2
func collectAttributeNames(elem *Element) map[string]bool {
	names := make(map[string]bool)

	if elem == nil {
		return names
	}

	// Collect direct attributes
	collectDirectAttributeNames(elem.Attributes, names)

	// Collect from container patterns
	collectAttributeNamesFromContainers(elem, names)

	return names
}

// collectDirectAttributeNames collects attribute names from a direct attribute list
func collectDirectAttributeNames(attrs []Attribute, names map[string]bool) {
	for _, attr := range attrs {
		if attr.Name != "" {
			names[attr.Name] = true
		}
		// TODO: Handle anyName and nsName (infinite name classes)
		// For now, we conservatively mark them
		if attr.AnyName != nil {
			names["*anyName*"] = true
		}
		if attr.NsName != nil {
			names["*nsName*"] = true
		}
	}
}

// collectAttributeNamesFromContainers collects attribute names from element's container patterns
func collectAttributeNamesFromContainers(elem *Element, names map[string]bool) {
	// Collect from choice pattern and its attributes
	if elem.Choice != nil {
		for _, child := range elem.Choice.Elements {
			for name := range collectAttributeNames(&child) {
				names[name] = true
			}
		}
		collectDirectAttributeNames(elem.Choice.Attributes, names)
	}

	// Collect from group patterns
	for _, group := range elem.Group {
		for _, child := range group.Elements {
			for name := range collectAttributeNames(&child) {
				names[name] = true
			}
		}
	}

	// Collect from interleave patterns
	for _, interleave := range elem.Interleave {
		for _, child := range interleave.Elements {
			for name := range collectAttributeNames(&child) {
				names[name] = true
			}
		}
	}

	// Collect from oneOrMore patterns
	for _, oneOrMore := range elem.OneOrMore {
		for _, child := range oneOrMore.Element {
			for name := range collectAttributeNames(&child) {
				names[name] = true
			}
		}
	}

	// Collect from optional patterns
	for _, optional := range elem.Optional {
		for _, child := range optional.Elements {
			for name := range collectAttributeNames(&child) {
				names[name] = true
			}
		}
		collectDirectAttributeNames(optional.Attributes, names)
	}
}

// validateMixedPatterns validates patterns within mixed content
func validateMixedPatterns(mixed *Mixed) error {
	if mixed == nil {
		return nil
	}

	// Validate child elements
	for _, child := range mixed.Elements {
		if err := validateElementPatterns(&child); err != nil {
			return err
		}
	}

	return nil
}

// parseOpeningTag extracts tag name and checks if self-closing from position i in str
// Returns: (tagName, isSelfClosing, newPosition)
func parseOpeningTag(str string, i int) (string, bool, int) {
	// Extract tag name
	tagStart := i
	for i < len(str) && str[i] != ' ' && str[i] != '>' && str[i] != '/' {
		i++
	}
	currentTag := str[tagStart:i]

	// Check if self-closing (find />)
	isSelfClosing := false
	tagEnd := i
	for tagEnd < len(str) && str[tagEnd] != '>' {
		tagEnd++
	}
	if tagEnd >= 1 && str[tagEnd-1] == '/' {
		isSelfClosing = true
	}

	// Skip to end of tag
	newPos := tagEnd
	if newPos < len(str) {
		newPos++
	}

	return currentTag, isSelfClosing, newPos
}

// handleOpeningTag processes an opening tag and updates depth/count as needed
func handleOpeningTag(str string, i int, depth int, tagName string) (int, int, int) {
	i++ // Skip '<'
	currentTag, isSelfClosing, newPos := parseOpeningTag(str, i)

	// Count root-level tags with matching name
	count := 0
	if depth == 0 && currentTag == tagName {
		count++
	}

	// Update depth
	if !isSelfClosing {
		depth++
	}

	return count, depth, newPos
}

// hasCountElements counts how many instances of a specific element appear at the root level
func hasCountElements(content []byte, tagName string) int {
	str := strings.TrimSpace(string(content))
	if str == "" {
		return 0
	}

	depth := 0
	count := 0
	i := 0

	for i < len(str) {
		if str[i] == '<' && i+1 < len(str) {
			switch str[i+1] {
			case '/':
				// Closing tag - decrease depth
				depth--
				i++
			case '!':
				// Comment or other declaration - skip
				i++
			default:
				// Opening tag
				tagCount, newDepth, newPos := handleOpeningTag(str, i, depth, tagName)
				count += tagCount
				depth = newDepth
				i = newPos
			}
		} else {
			i++
		}
	}

	return count
}

// propagateDatatypeLibrary propagates datatypeLibrary attribute from parent elements to child Data and Value elements
// Per RELAX NG spec: datatypeLibrary is inherited from the nearest ancestor element that has a datatypeLibrary attribute
func (g *Grammar) propagateDatatypeLibrary() {
	// Start propagation from grammar level
	for i := range g.Defines {
		g.propagateDatatypeLibraryInDefine(&g.Defines[i], g.DatatypeLibrary)
	}

	// Also propagate in Start element
	g.propagateDatatypeLibraryInStart(&g.Start, g.DatatypeLibrary)

	// Propagate in Divs
	for i := range g.Divs {
		g.propagateDatatypeLibraryInDiv(&g.Divs[i], g.DatatypeLibrary)
	}
}

// propagateDatatypeLibraryInDiv propagates datatypeLibrary through Div elements
func (g *Grammar) propagateDatatypeLibraryInDiv(div *Div, parentLib string) {
	// Use div's own library if set, otherwise use parent's
	lib := parentLib
	if div.DatatypeLibrary != "" {
		lib = div.DatatypeLibrary
	}

	// Propagate to defines in this div
	for i := range div.Defines {
		g.propagateDatatypeLibraryInDefine(&div.Defines[i], lib)
	}

	// Propagate to starts in this div
	for i := range div.Start {
		g.propagateDatatypeLibraryInStart(&div.Start[i], lib)
	}

	// Propagate to nested divs
	for i := range div.Divs {
		g.propagateDatatypeLibraryInDiv(&div.Divs[i], lib)
	}
}

// propagateDatatypeLibraryInDefine propagates datatypeLibrary through Define elements
func (g *Grammar) propagateDatatypeLibraryInDefine(def *Define, parentLib string) {
	// Use define's own library if set, otherwise use parent's
	lib := parentLib
	if def.DatatypeLibrary != "" {
		lib = def.DatatypeLibrary
	}

	// Propagate to elements
	for i := range def.Elements {
		g.propagateDatatypeLibraryInElement(&def.Elements[i], lib)
	}

	// Propagate to choice
	if def.Choice != nil {
		g.propagateDatatypeLibraryInChoice(def.Choice, lib)
	}

	// Propagate to group
	for i := range def.Group {
		g.propagateDatatypeLibraryInGroup(&def.Group[i], lib)
	}

	// Propagate to interleave
	for i := range def.Interleave {
		g.propagateDatatypeLibraryInInterleave(&def.Interleave[i], lib)
	}

	// Propagate to optional
	for i := range def.Optional {
		g.propagateDatatypeLibraryInOptional(&def.Optional[i], lib)
	}

	// Propagate to oneOrMore
	for i := range def.OneOrMore {
		g.propagateDatatypeLibraryInOneOrMore(&def.OneOrMore[i], lib)
	}

	// Propagate to zeroOrMore
	for i := range def.ZeroOrMore {
		g.propagateDatatypeLibraryInZeroOrMore(&def.ZeroOrMore[i], lib)
	}

	// Propagate to data
	if def.Data != nil && def.Data.DatatypeLibrary == "" {
		def.Data.DatatypeLibrary = lib
	}
}

// propagateDatatypeLibraryInStart propagates datatypeLibrary through Start elements
func (g *Grammar) propagateDatatypeLibraryInStart(start *Start, parentLib string) {
	// Use start's own library if set, otherwise use parent's
	lib := parentLib
	if start.DatatypeLibrary != "" {
		lib = start.DatatypeLibrary
	}

	// Propagate to data
	if start.Data != nil && start.Data.DatatypeLibrary == "" {
		start.Data.DatatypeLibrary = lib
	}

	// Propagate to elements
	if start.Element != nil {
		g.propagateDatatypeLibraryInElement(start.Element, lib)
	}

	// Propagate to choice
	if start.Choice != nil {
		g.propagateDatatypeLibraryInChoice(start.Choice, lib)
	}

	// Propagate to group
	for i := range start.Group {
		g.propagateDatatypeLibraryInGroup(&start.Group[i], lib)
	}

	// Propagate to interleave
	for i := range start.Interleave {
		g.propagateDatatypeLibraryInInterleave(&start.Interleave[i], lib)
	}

	// Propagate to optional
	for i := range start.Optional {
		g.propagateDatatypeLibraryInOptional(&start.Optional[i], lib)
	}

	// Propagate to oneOrMore
	for i := range start.OneOrMore {
		g.propagateDatatypeLibraryInOneOrMore(&start.OneOrMore[i], lib)
	}

	// Propagate to zeroOrMore
	for i := range start.ZeroOrMore {
		g.propagateDatatypeLibraryInZeroOrMore(&start.ZeroOrMore[i], lib)
	}
}

// propagateDatatypeLibraryInElement propagates datatypeLibrary through Element patterns
func (g *Grammar) propagateDatatypeLibraryInElement(elem *Element, parentLib string) {
	// Use element's own library if set, otherwise use parent's
	lib := parentLib
	if elem.DatatypeLibrary != "" {
		lib = elem.DatatypeLibrary
	}

	// Propagate to child elements
	for i := range elem.Elements {
		g.propagateDatatypeLibraryInElement(&elem.Elements[i], lib)
	}

	// Propagate to attributes
	for i := range elem.Attributes {
		g.propagateDatatypeLibraryInAttribute(&elem.Attributes[i], lib)
	}

	// Propagate to choice
	if elem.Choice != nil {
		g.propagateDatatypeLibraryInChoice(elem.Choice, lib)
	}

	// Propagate to group
	for i := range elem.Group {
		g.propagateDatatypeLibraryInGroup(&elem.Group[i], lib)
	}

	// Propagate to interleave
	for i := range elem.Interleave {
		g.propagateDatatypeLibraryInInterleave(&elem.Interleave[i], lib)
	}

	// Propagate to optional
	for i := range elem.Optional {
		g.propagateDatatypeLibraryInOptional(&elem.Optional[i], lib)
	}

	// Propagate to oneOrMore
	for i := range elem.OneOrMore {
		g.propagateDatatypeLibraryInOneOrMore(&elem.OneOrMore[i], lib)
	}

	// Propagate to zeroOrMore
	for i := range elem.ZeroOrMore {
		g.propagateDatatypeLibraryInZeroOrMore(&elem.ZeroOrMore[i], lib)
	}

	// Propagate to data
	if elem.Data != nil && elem.Data.DatatypeLibrary == "" {
		elem.Data.DatatypeLibrary = lib
	}

	// Propagate to values
	for i := range elem.Values {
		if elem.Values[i].DatatypeLibrary == "" {
			elem.Values[i].DatatypeLibrary = lib
		}
	}
}

// propagateDatatypeLibraryInAttribute propagates datatypeLibrary through Attribute patterns
func (g *Grammar) propagateDatatypeLibraryInAttribute(attr *Attribute, parentLib string) {
	// Use attribute's own library if set, otherwise use parent's
	lib := parentLib
	if attr.DatatypeLibrary != "" {
		lib = attr.DatatypeLibrary
	}

	// Propagate to choice
	if attr.Choice != nil {
		g.propagateDatatypeLibraryInChoice(attr.Choice, lib)
	}

	// Propagate to data
	if attr.Data != nil && attr.Data.DatatypeLibrary == "" {
		attr.Data.DatatypeLibrary = lib
	}

	// Propagate to values
	for i := range attr.Values {
		if attr.Values[i].DatatypeLibrary == "" {
			attr.Values[i].DatatypeLibrary = lib
		}
	}
}

// propagateDatatypeLibraryInChoice propagates datatypeLibrary through Choice patterns
func (g *Grammar) propagateDatatypeLibraryInChoice(choice *Choice, parentLib string) {
	// Use choice's own library if set, otherwise use parent's
	lib := parentLib
	if choice.DatatypeLibrary != "" {
		lib = choice.DatatypeLibrary
	}

	// Propagate to elements
	for i := range choice.Elements {
		g.propagateDatatypeLibraryInElement(&choice.Elements[i], lib)
	}

	// Propagate to attributes
	for i := range choice.Attributes {
		g.propagateDatatypeLibraryInAttribute(&choice.Attributes[i], lib)
	}

	// Propagate to data
	for i := range choice.Data {
		if choice.Data[i].DatatypeLibrary == "" {
			choice.Data[i].DatatypeLibrary = lib
		}
	}

	// Propagate to values
	for i := range choice.Values {
		if choice.Values[i].DatatypeLibrary == "" {
			choice.Values[i].DatatypeLibrary = lib
		}
	}

	// Propagate to group
	for i := range choice.Group {
		g.propagateDatatypeLibraryInGroup(&choice.Group[i], lib)
	}

	// Propagate to interleave
	for i := range choice.Interleave {
		g.propagateDatatypeLibraryInInterleave(&choice.Interleave[i], lib)
	}
}

// propagateDatatypeLibraryInGroup propagates datatypeLibrary through Group patterns
func (g *Grammar) propagateDatatypeLibraryInGroup(group *Group, parentLib string) {
	// Use group's own library if set, otherwise use parent's
	lib := parentLib
	if group.DatatypeLibrary != "" {
		lib = group.DatatypeLibrary
	}

	// Propagate to elements
	for i := range group.Elements {
		g.propagateDatatypeLibraryInElement(&group.Elements[i], lib)
	}

	// Propagate to attributes
	for i := range group.Attributes {
		g.propagateDatatypeLibraryInAttribute(&group.Attributes[i], lib)
	}

	// Propagate to choice
	for i := range group.Choice {
		g.propagateDatatypeLibraryInChoice(&group.Choice[i], lib)
	}

	// Propagate to optional
	for i := range group.Optional {
		g.propagateDatatypeLibraryInOptional(&group.Optional[i], lib)
	}

	// Propagate to oneOrMore
	for i := range group.OneOrMore {
		g.propagateDatatypeLibraryInOneOrMore(&group.OneOrMore[i], lib)
	}

	// Propagate to zeroOrMore
	for i := range group.ZeroOrMore {
		g.propagateDatatypeLibraryInZeroOrMore(&group.ZeroOrMore[i], lib)
	}

	// Propagate to nested group
	for i := range group.Group {
		g.propagateDatatypeLibraryInGroup(&group.Group[i], lib)
	}

	// Propagate to interleave
	for i := range group.Interleave {
		g.propagateDatatypeLibraryInInterleave(&group.Interleave[i], lib)
	}
}

// propagateDatatypeLibraryInInterleave propagates datatypeLibrary through Interleave patterns
func (g *Grammar) propagateDatatypeLibraryInInterleave(interleave *Interleave, parentLib string) {
	// Use interleave's own library if set, otherwise use parent's
	lib := parentLib
	if interleave.DatatypeLibrary != "" {
		lib = interleave.DatatypeLibrary
	}

	// Propagate to elements
	for i := range interleave.Elements {
		g.propagateDatatypeLibraryInElement(&interleave.Elements[i], lib)
	}

	// Propagate to attributes
	for i := range interleave.Attributes {
		g.propagateDatatypeLibraryInAttribute(&interleave.Attributes[i], lib)
	}

	// Propagate to choice
	for i := range interleave.Choice {
		g.propagateDatatypeLibraryInChoice(&interleave.Choice[i], lib)
	}

	// Propagate to optional
	for i := range interleave.Optional {
		g.propagateDatatypeLibraryInOptional(&interleave.Optional[i], lib)
	}

	// Propagate to oneOrMore
	for i := range interleave.OneOrMore {
		g.propagateDatatypeLibraryInOneOrMore(&interleave.OneOrMore[i], lib)
	}

	// Propagate to zeroOrMore
	for i := range interleave.ZeroOrMore {
		g.propagateDatatypeLibraryInZeroOrMore(&interleave.ZeroOrMore[i], lib)
	}

	// Propagate to group
	for i := range interleave.Group {
		g.propagateDatatypeLibraryInGroup(&interleave.Group[i], lib)
	}
}

// propagateDatatypeLibraryInOptional propagates datatypeLibrary through Optional patterns
func (g *Grammar) propagateDatatypeLibraryInOptional(opt *Optional, parentLib string) {
	// Use optional's own library if set, otherwise use parent's
	lib := parentLib
	if opt.DatatypeLibrary != "" {
		lib = opt.DatatypeLibrary
	}

	// Propagate to elements
	for i := range opt.Elements {
		g.propagateDatatypeLibraryInElement(&opt.Elements[i], lib)
	}

	// Propagate to attributes
	for i := range opt.Attributes {
		g.propagateDatatypeLibraryInAttribute(&opt.Attributes[i], lib)
	}
}

// propagateDatatypeLibraryInOneOrMore propagates datatypeLibrary through OneOrMore patterns
func (g *Grammar) propagateDatatypeLibraryInOneOrMore(one *OneOrMore, parentLib string) {
	// Use oneOrMore's own library if set, otherwise use parent's
	lib := parentLib
	if one.DatatypeLibrary != "" {
		lib = one.DatatypeLibrary
	}

	// Propagate to elements
	for i := range one.Element {
		g.propagateDatatypeLibraryInElement(&one.Element[i], lib)
	}

	// Propagate to attributes
	for i := range one.Attribute {
		g.propagateDatatypeLibraryInAttribute(&one.Attribute[i], lib)
	}

	// Propagate to choice
	if one.Choice != nil {
		g.propagateDatatypeLibraryInChoice(one.Choice, lib)
	}

	// Propagate to group
	for i := range one.Group {
		g.propagateDatatypeLibraryInGroup(&one.Group[i], lib)
	}

	// Propagate to interleave
	for i := range one.Interleave {
		g.propagateDatatypeLibraryInInterleave(&one.Interleave[i], lib)
	}
}

// propagateDatatypeLibraryInZeroOrMore propagates datatypeLibrary through ZeroOrMore patterns
func (g *Grammar) propagateDatatypeLibraryInZeroOrMore(zero *ZeroOrMore, parentLib string) {
	// Use zeroOrMore's own library if set, otherwise use parent's
	lib := parentLib
	if zero.DatatypeLibrary != "" {
		lib = zero.DatatypeLibrary
	}

	// Propagate to elements
	for i := range zero.Element {
		g.propagateDatatypeLibraryInElement(&zero.Element[i], lib)
	}

	// Propagate to attributes
	for i := range zero.Attribute {
		g.propagateDatatypeLibraryInAttribute(&zero.Attribute[i], lib)
	}

	// Propagate to choice
	if zero.Choice != nil {
		g.propagateDatatypeLibraryInChoice(zero.Choice, lib)
	}

	// Propagate to group
	for i := range zero.Group {
		g.propagateDatatypeLibraryInGroup(&zero.Group[i], lib)
	}

	// Propagate to interleave
	for i := range zero.Interleave {
		g.propagateDatatypeLibraryInInterleave(&zero.Interleave[i], lib)
	}
}

// normalizeWhitespace normalizes whitespace in element and attribute names according to XML spec
// Whitespace is collapsed: leading/trailing removed, internal sequences converted to single space
func (g *Grammar) normalizeWhitespace() {
	// Normalize start combine attribute
	if g.Start.Combine != "" {
		g.Start.Combine = normalizeWhitespaceString(g.Start.Combine)
	}

	// Normalize start ref name
	if g.Start.Ref != nil {
		g.Start.Ref.Name = normalizeWhitespaceString(g.Start.Ref.Name)
	}

	// Normalize start parentRef name
	if g.Start.ParentRef != nil {
		g.Start.ParentRef.Name = normalizeWhitespaceString(g.Start.ParentRef.Name)
	}

	// Normalize start element if present
	if g.Start.Element != nil {
		normalizeElement(g.Start.Element)
	}

	// Normalize all defines
	for i := range g.Defines {
		g.Defines[i].Name = normalizeWhitespaceString(g.Defines[i].Name)
		if g.Defines[i].Combine != "" {
			g.Defines[i].Combine = normalizeWhitespaceString(g.Defines[i].Combine)
		}
		if g.Defines[i].ParentRef != nil {
			g.Defines[i].ParentRef.Name = normalizeWhitespaceString(g.Defines[i].ParentRef.Name)
		}
		if g.Defines[i].FirstElement() != nil {
			normalizeElement(g.Defines[i].FirstElement())
		}
	}

	// Normalize attributes that appear at grammar level
	// (in Include and ExternalRef elements - though less common)
}

// resolveQNames resolves QNames in element and attribute names using namespace declarations from xmlns attributes
// Per RELAX NG spec: QNames are resolved using the namespace context from xmlns declarations
// This should be called after normalizeWhitespace and before validation
func (g *Grammar) resolveQNames() {
	// Extract namespace context from start's RawAttrs (includes xmlns declarations)
	// The namespace declarations are on the start element (or can be inherited from grammar)
	nsMap := extractNamespaceContext(g.Start.RawAttrs)

	// Resolve QNames in start element
	if g.Start.Element != nil {
		// The element may also have its own namespace context from xmlns declarations
		// Merge those into the nsMap before resolving
		elementNS := extractNamespaceContext(g.Start.Element.RawAttrs)
		for k, v := range elementNS {
			nsMap[k] = v
		}
		resolveQNamesInElement(g.Start.Element, nsMap)
	}

	// Resolve QNames in all defines
	for i := range g.Defines {
		if g.Defines[i].FirstElement() != nil {
			// Each define's element may also have its own namespace context
			defineNS := extractNamespaceContext(g.Defines[i].FirstElement().RawAttrs)
			// Start with base ns map and merge in element-specific ones
			combinedNS := make(map[string]string)
			for k, v := range nsMap {
				combinedNS[k] = v
			}
			for k, v := range defineNS {
				combinedNS[k] = v
			}
			resolveQNamesInElement(g.Defines[i].FirstElement(), combinedNS)
		}
	}
}

// flattenDivs flattens div elements per RELAX NG spec section 4.11
// Div elements are used for grouping and can provide namespace context via the ns attribute.
// This function extracts all content from divs into the main grammar and propagates namespaces.
func (g *Grammar) flattenDivs() {
	// Process all divs recursively
	for _, div := range g.Divs {
		g.processDivAndMerge(&div, "")
	}

	// Clear divs after flattening
	g.Divs = nil
}

// processDivAndMerge recursively processes a div element and merges its content into the grammar
// parentNs is the namespace inherited from parent divs
func (g *Grammar) processDivAndMerge(div *Div, parentNs string) {
	// Determine effective namespace for this div
	effectiveNs := parentNs
	if div.Ns != "" {
		effectiveNs = div.Ns
	}

	// Process nested divs first
	for i := range div.Divs {
		g.processDivAndMerge(&div.Divs[i], effectiveNs)
	}

	// Merge start patterns from this div
	for _, start := range div.Start {
		// Apply namespace to start's element if present
		if start.Element != nil && effectiveNs != "" {
			applyNamespaceToElement(start.Element, effectiveNs)
		}

		// Note: namespace application to start's ref target is deferred until ref resolution

		// Check if grammar start is empty (all fields nil/empty)
		grammarStartIsEmpty := g.Start.Element == nil && g.Start.Ref == nil && g.Start.ParentRef == nil &&
			g.Start.Choice == nil && len(g.Start.Group) == 0 && len(g.Start.Interleave) == 0 &&
			len(g.Start.Optional) == 0 && len(g.Start.OneOrMore) == 0 && len(g.Start.ZeroOrMore) == 0

		if grammarStartIsEmpty {
			g.Start = start
		}
		// Note: Multiple starts should be combined according to combine attribute (future enhancement)
	}

	// Merge defines from this div
	for _, define := range div.Defines {
		// Apply namespace to define's element if present
		if define.FirstElement() != nil && effectiveNs != "" {
			applyNamespaceToElement(define.FirstElement(), effectiveNs)
		}
		g.Defines = append(g.Defines, define)
	}
}

// applyNamespaceToElement applies a namespace to an element if it doesn't already have one
func applyNamespaceToElement(elem *Element, ns string) {
	if elem == nil {
		return
	}

	// Only apply if element doesn't already have a namespace
	if elem.Ns == "" {
		elem.Ns = ns
	}

	// Recursively apply to child elements
	for i := range elem.Group {
		for j := range elem.Group[i].Elements {
			applyNamespaceToElement(&elem.Group[i].Elements[j], ns)
		}
	}
	if elem.Choice != nil {
		for i := range elem.Choice.Elements {
			applyNamespaceToElement(&elem.Choice.Elements[i], ns)
		}
	}
	for i := range elem.Optional {
		for j := range elem.Optional[i].Elements {
			applyNamespaceToElement(&elem.Optional[i].Elements[j], ns)
		}
	}
	for i := range elem.ZeroOrMore {
		for j := range elem.ZeroOrMore[i].Element {
			applyNamespaceToElement(&elem.ZeroOrMore[i].Element[j], ns)
		}
	}
	for i := range elem.OneOrMore {
		for j := range elem.OneOrMore[i].Element {
			applyNamespaceToElement(&elem.OneOrMore[i].Element[j], ns)
		}
	}
	for i := range elem.Interleave {
		for j := range elem.Interleave[i].Elements {
			applyNamespaceToElement(&elem.Interleave[i].Elements[j], ns)
		}
	}
	if elem.Mixed != nil {
		for i := range elem.Mixed.Elements {
			applyNamespaceToElement(&elem.Mixed.Elements[i], ns)
		}
	}
}

// applyNamespaceToDefine applies a namespace to all elements in a Define pattern
// Per RELAX NG spec section 4.5: include with ns attribute applies namespace to all elements
func applyNamespaceToDefine(def *Define, ns string) {
	if def == nil || ns == "" {
		return
	}

	// Apply to direct elements
	for i := range def.Elements {
		applyNamespaceToElement(&def.Elements[i], ns)
	}

	// Apply to elements in choice
	if def.Choice != nil {
		for i := range def.Choice.Elements {
			applyNamespaceToElement(&def.Choice.Elements[i], ns)
		}
	}

	// Apply to elements in groups
	for i := range def.Group {
		for j := range def.Group[i].Elements {
			applyNamespaceToElement(&def.Group[i].Elements[j], ns)
		}
	}

	// Apply to elements in interleaves
	for i := range def.Interleave {
		for j := range def.Interleave[i].Elements {
			applyNamespaceToElement(&def.Interleave[i].Elements[j], ns)
		}
	}

	// Apply to elements in optional
	for i := range def.Optional {
		for j := range def.Optional[i].Elements {
			applyNamespaceToElement(&def.Optional[i].Elements[j], ns)
		}
	}

	// Apply to elements in oneOrMore
	for i := range def.OneOrMore {
		for j := range def.OneOrMore[i].Element {
			applyNamespaceToElement(&def.OneOrMore[i].Element[j], ns)
		}
	}

	// Apply to elements in zeroOrMore
	for i := range def.ZeroOrMore {
		for j := range def.ZeroOrMore[i].Element {
			applyNamespaceToElement(&def.ZeroOrMore[i].Element[j], ns)
		}
	}
}

// synthesizeImplicitPatterns synthesizes implicit choice patterns from multiple pattern children
// According to RELAX NG spec, when multiple pattern elements appear as children of a single parent,
// they form an implicit choice pattern.
func (g *Grammar) synthesizeImplicitPatterns() {
	// Process start element
	if g.Start.Element != nil {
		synthesizeElementImplicitChoice(g.Start.Element)
	}

	// Process all defines
	for i := range g.Defines {
		if g.Defines[i].FirstElement() != nil {
			synthesizeElementImplicitChoice(g.Defines[i].FirstElement())
		}
	}
}

// unpackNestedGrammars unpacks nested <grammar> elements per RELAX NG spec section 4.18
// When a <grammar> element appears within a define, its <start> content replaces the grammar.
// Any <define> elements within the nested grammar are moved to the top level with renamed identifiers.
func (g *Grammar) unpackNestedGrammars() error {
	// First, collect all new defines that need to be added from nested grammars
	var newDefines []Define

	// Process all defines for nested grammars
	for i := range g.Defines {
		def := &g.Defines[i]

		// Check if the define's RawContent contains a nested <grammar> element
		if len(def.RawContent) > 0 && bytes.Contains(def.RawContent, []byte("<grammar")) {
			_, err := g.unpackNestedGrammarInDefine(def, &newDefines)
			if err != nil {
				return err
			}
		}
	}

	// Now add all the new defines (this may reallocate g.Defines once, but all our pointers are already processed)
	g.Defines = append(g.Defines, newDefines...)

	// Also unpack nested grammars in Start patterns
	// Use a fresh accumulator since we've already added the define-level defines
	var startDefines []Define
	if err := g.unpackNestedGrammarsInStart(&startDefines); err != nil {
		return err
	}

	// Add any newly discovered defines from Start patterns
	g.Defines = append(g.Defines, startDefines...)

	return nil
}

// validateNestedGrammarLocationInRawContent checks if a nested grammar appears in an invalid location
// Per spec 4.18, nested grammars are only allowed in group or interleave, not in choice or other patterns
func validateNestedGrammarLocationInRawContent(content []byte) error {
	if len(content) == 0 {
		return nil
	}

	// Parse the RawContent to check the context where the grammar element appears
	decoder := xml.NewDecoder(bytes.NewReader(content))
	depth := 0
	inChoice := false
	choiceDepth := -1

	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		switch t := tok.(type) {
		case xml.StartElement:
			// Check if we're entering a choice at the top level
			if t.Name.Local == elemNameChoice && depth == 0 {
				inChoice = true
				choiceDepth = depth
			}

			// Check if grammar appears while we're inside a choice
			// Grammar is invalid here because it's nested inside choice
			if t.Name.Local == elemNameGrammar && inChoice && depth > choiceDepth {
				return fmt.Errorf("nested grammar in choice is not allowed (spec section 4.18: nested grammar is only allowed in group or interleave)")
			}

			depth++
		case xml.EndElement:
			depth--
			// When we exit the choice element, mark that we're no longer in a choice
			if inChoice && depth == choiceDepth {
				inChoice = false
				choiceDepth = -1
			}
		}
	}

	return nil
}

// extractGrammarElement extracts the <grammar> element from RawContent that may contain multiple elements
func extractGrammarElement(content []byte) []byte {
	decoder := xml.NewDecoder(bytes.NewReader(content))
	var buf bytes.Buffer
	depth := 0
	inGrammar := false

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}

		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == elemNameGrammar {
				inGrammar = true
				depth = 1
				// Write the grammar start element
				buf.WriteString("<" + t.Name.Local)
				for _, attr := range t.Attr {
					buf.WriteString(fmt.Sprintf(` %s="%s"`, attr.Name.Local, attr.Value))
				}
				buf.WriteString(">")
			} else if inGrammar {
				depth++
				buf.WriteString("<" + t.Name.Local)
				for _, attr := range t.Attr {
					buf.WriteString(fmt.Sprintf(` %s="%s"`, attr.Name.Local, attr.Value))
				}
				buf.WriteString(">")
			}
		case xml.EndElement:
			if inGrammar {
				buf.WriteString("</" + t.Name.Local + ">")
				depth--
				if depth == 0 {
					// Found the end of the grammar element
					return buf.Bytes()
				}
			}
		case xml.CharData:
			if inGrammar {
				buf.Write(t)
			}
		case xml.Comment, xml.ProcInst:
			// Skip comments and processing instructions
		}
	}

	return nil
}

// validateNestedGrammarParentRefs validates that any parentRef in a nested grammar
// refers to defines in the parent grammar (not the nested grammar itself)
func (g *Grammar) validateNestedGrammarParentRefs(nestedGrammar *Grammar) error {
	// Build map of parent grammar's define names
	parentDefineNames := make(map[string]bool)
	for _, def := range g.Defines {
		parentDefineNames[def.Name] = true
	}

	// Check for orphaned nested grammars (with parentRef but no valid scope defines)
	// parentRef must either:
	// 1. Refer to a define in the PARENT grammar, OR
	// 2. The nested grammar must have a name (which would be provided by the context)

	if nestedGrammar.Start.ParentRef != nil {
		// parentRef without a name is invalid - it can't refer to any define
		if nestedGrammar.Start.ParentRef.Name == "" {
			return fmt.Errorf("nested grammar: parentRef must have a name attribute")
		}
		// parentRef MUST refer to a define in the parent grammar
		if !parentDefineNames[nestedGrammar.Start.ParentRef.Name] {
			return fmt.Errorf("nested grammar: parentRef refers to undefined parent define '%s'", nestedGrammar.Start.ParentRef.Name)
		}
	}

	// Check parentRef in nested grammar's defines
	for _, def := range nestedGrammar.Defines {
		if def.ParentRef != nil && def.ParentRef.Name != "" {
			if !parentDefineNames[def.ParentRef.Name] {
				return fmt.Errorf("nested grammar define '%s': parentRef refers to undefined parent define '%s'", def.Name, def.ParentRef.Name)
			}
		}
		// Check parentRef in define's elements
		validateParentRefInElement := func(elem *Element) error {
			if elem == nil {
				return nil
			}
			for _, ref := range elem.ParentRef {
				if ref.Name != "" && !parentDefineNames[ref.Name] {
					return fmt.Errorf("nested grammar element: parentRef refers to undefined parent define '%s'", ref.Name)
				}
			}
			return nil
		}

		if len(def.Elements) > 0 && def.Elements[0].Name != "" {
			if err := validateParentRefInElement(&def.Elements[0]); err != nil {
				return err
			}
		}
	}

	return nil
}

// unpackNestedGrammarInRawContent unpacks a single nested grammar from RawContent
// Returns a pointer to a heap-allocated copy of the nested grammar's Start element
// Returns nil if no grammar was found
// NOTE: Does NOT append defines to g.Defines - caller must do that
func (g *Grammar) unpackNestedGrammarInRawContent(rawContent *[]byte, contextName string) (*Start, []Define, error) {
	if rawContent == nil || len(*rawContent) == 0 {
		return nil, nil, nil
	}

	// The RawContent may contain multiple elements (e.g., <notAllowed/> followed by <grammar>).
	// We need to extract just the <grammar> element for unmarshaling.
	grammarContent := extractGrammarElement(*rawContent)
	if len(grammarContent) == 0 {
		// No <grammar> element found in RawContent
		return nil, nil, nil
	}

	// The extracted content may not have the namespace declaration because it's inherited
	// from the parent. We need to add it back for unmarshaling.
	if !bytes.Contains(grammarContent, []byte("xmlns=")) {
		// Add namespace to the <grammar> element
		grammarContent = bytes.Replace(grammarContent,
			[]byte("<grammar>"),
			[]byte(`<grammar xmlns="http://relaxng.org/ns/structure/1.0">`),
			1)
	}

	// Try to unmarshal the content as a grammar
	var nestedGrammar Grammar
	if err := xml.Unmarshal(grammarContent, &nestedGrammar); err != nil {
		return nil, nil, err
	}

	// Successfully parsed a grammar
	// Normalize whitespace in nested grammar's names before validation
	// Per RELAX NG Section 4.2: leading/trailing whitespace must be removed from name attributes
	nestedGrammar.normalizeWhitespace()

	// IMPORTANT: Validate parentRef in nested grammar before unpacking
	// parentRef should refer to defines in the PARENT grammar, not the nested one
	if err := g.validateNestedGrammarParentRefs(&nestedGrammar); err != nil {
		return nil, nil, err
	}

	// Merge any defines from the nested grammar into the top level
	// IMPORTANT: Save nested grammar defines BEFORE clearing RawContent
	// because the function may be called with a RawContent pointer that's inside a Define
	nestedDefs := make([]Define, 0, len(nestedGrammar.Defines))
	for _, nestedDef := range nestedGrammar.Defines {
		// Rename the define to include parent context
		newName := contextName + "_" + nestedDef.Name
		nestedDef.Name = newName
		nestedDefs = append(nestedDefs, nestedDef)
	}

	// IMPORTANT: Handle multiple start elements with combine attributes
	// The Grammar struct only has a single Start field, so we need to manually parse
	// and merge multiple <start> elements from the grammar's RawContent
	mergedStart, err := g.mergeNestedGrammarStarts(&nestedGrammar)
	if err != nil {
		return nil, nil, err
	}

	// If merging returned a start, use it; otherwise use the unmarshaled Start
	if mergedStart != nil {
		nestedGrammar.Start = *mergedStart
	}

	// IMPORTANT: Clear RawContent after successfully unpacking
	// This removes the raw <grammar> element so we don't re-process it
	*rawContent = nil

	// IMPORTANT: Return a HEAP-ALLOCATED copy of the nested grammar's start pattern
	// to avoid dangling pointer issues. We allocate a new Start on the heap and copy values.
	startCopy := new(Start)
	*startCopy = nestedGrammar.Start
	return startCopy, nestedDefs, nil
}

// mergeNestedGrammarStarts parses and merges multiple start elements in a nested grammar
// This handles cases where the nested grammar has multiple <start> elements with combine attributes
func (g *Grammar) mergeNestedGrammarStarts(nestedGrammar *Grammar) (*Start, error) {
	// Parse all start elements from the nested grammar's RawContent
	starts := parseNestedGrammarStarts(nestedGrammar.RawContent)

	// If we found only one start or no starts, no merging needed
	if len(starts) <= 1 {
		return nil, nil
	}

	// Validate and normalize starts
	combineMethod, err := validateAndNormalizeNestedStarts(starts)
	if err != nil {
		return nil, err
	}

	// Merge starts according to combine method
	merged := mergeNestedStartsByMethod(starts, combineMethod)
	return &merged, nil
}

// parseNestedGrammarStarts extracts all start elements from raw grammar content
func parseNestedGrammarStarts(rawContent []byte) []Start {
	var starts []Start
	decoder := xml.NewDecoder(bytes.NewReader(rawContent))
	depth := 0
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := token.(type) {
		case xml.StartElement:
			depth++
			// Only parse start elements at depth 1 (direct children of grammar)
			if depth == 1 && t.Name.Local == elemNameStart {
				var start Start
				if decodeErr := decoder.DecodeElement(&start, &t); decodeErr == nil {
					starts = append(starts, start)
				}
				depth-- // DecodeElement consumes the end tag
			}
		case xml.EndElement:
			depth--
		}
	}
	return starts
}

// validateAndNormalizeNestedStarts validates nested start elements and returns the combine method
func validateAndNormalizeNestedStarts(starts []Start) (string, error) {
	noCombineCount := 0
	var combineMethod string

	for i, start := range starts {
		// Check for obsolete name attribute on start
		if start.Name != "" {
			return "", fmt.Errorf("start element cannot have name attribute (obsolete syntax)")
		}

		// Normalize combine attribute value (trim whitespace)
		starts[i].Combine = strings.TrimSpace(start.Combine)
		if starts[i].Combine == "" {
			noCombineCount++
		} else {
			// Check combine value consistency
			if combineMethod == "" {
				combineMethod = starts[i].Combine
			} else if combineMethod != starts[i].Combine {
				return "", fmt.Errorf("multiple start elements have inconsistent combine values: '%s' vs '%s'", combineMethod, starts[i].Combine)
			}
		}
	}

	// At most ONE start can lack a combine attribute
	if noCombineCount > 1 {
		return "", fmt.Errorf("multiple start elements - more than one lacks a combine attribute")
	}

	return combineMethod, nil
}

// mergeNestedStartsByMethod merges multiple starts according to their combine method
func mergeNestedStartsByMethod(starts []Start, combineMethod string) Start {
	merged := Start{
		Combine: "", // Don't include combine in merged result
	}

	switch combineMethod {
	case elemNameChoice:
		// Merge all patterns into a choice
		choice := Choice{}
		for _, start := range starts {
			// Collect elements
			if start.Element != nil {
				choice.Elements = append(choice.Elements, *start.Element)
			}
			// Collect refs
			if start.Ref != nil {
				choice.Refs = append(choice.Refs, *start.Ref)
			}
			// Flatten nested choices
			if start.Choice != nil {
				choice.Elements = append(choice.Elements, start.Choice.Elements...)
				choice.Refs = append(choice.Refs, start.Choice.Refs...)
			}
		}
		merged.Choice = &choice

	case elemNameInterleave:
		// Merge all patterns into an interleave
		interleaves := []Interleave{}
		interleave := Interleave{}
		for _, start := range starts {
			// Collect elements
			if start.Element != nil {
				interleave.Elements = append(interleave.Elements, *start.Element)
			}
			// Collect refs
			if start.Ref != nil {
				interleave.Ref = append(interleave.Ref, *start.Ref)
			}
		}
		interleaves = append(interleaves, interleave)
		merged.Interleave = interleaves
	}

	return merged
}

// unpackNestedGrammarInDefine unpacks a nested grammar from a define's RawContent
// and applies the grammar's start pattern to the define directly
// Returns (true, nil) if grammar was unpacked, (false, nil) if no grammar found, or (false, error) if error
func (g *Grammar) unpackNestedGrammarInDefine(def *Define, newDefines *[]Define) (bool, error) {
	// First, check if the define's RawContent contains a nested grammar in an invalid location
	// E.g., nested grammars in choice are not allowed per spec 4.18
	if len(def.RawContent) > 0 && bytes.Contains(def.RawContent, []byte("<grammar")) {
		if err := validateNestedGrammarLocationInRawContent(def.RawContent); err != nil {
			return false, err
		}
	}

	nestedStart, nestedDefs, err := g.unpackNestedGrammarInRawContent(&def.RawContent, def.Name)
	if err != nil {
		return false, err
	}

	if nestedStart == nil {
		return false, nil
	}

	// Validate the nested Start element to catch structural errors like parentRef with children
	if err := validateStartPatterns(nestedStart); err != nil {
		return false, fmt.Errorf("nested grammar in define '%s' has invalid structure: %v", def.Name, err)
	}

	// Validate that the nested start's patterns don't contain invalid nested grammars
	// E.g., nested grammars are not allowed in choice (per spec 4.18)
	if err := g.validateNestedStartPatterns(nestedStart); err != nil {
		return false, fmt.Errorf("nested grammar in define '%s' has invalid nested patterns: %v", def.Name, err)
	}

	// Add the new defines to the accumulator (NOT to g.Defines directly)
	if len(nestedDefs) > 0 {
		*newDefines = append(*newDefines, nestedDefs...)
	}

	// When the nested grammar was the content of an element wrapper (e.g.
	// define foo { element outerFoo { <grammar>...</grammar> } }), the nested
	// start's pattern is the element's content — not the define's. Placing it at
	// the define level instead would drop the wrapping element on serialization.
	if len(def.Elements) > 0 && bytes.Contains(def.Elements[0].RawContent, []byte("<grammar")) {
		applyNestedStartToElement(&def.Elements[0], nestedStart, def.Name)
		return true, nil
	}

	// Transfer the nested grammar's start pattern to the define
	// Note: Define only supports a subset of Start's fields
	// Also, update ref/parentRef names to use the context prefix
	def.Ref = nestedStart.Ref
	if def.Ref != nil && def.Ref.Name != "" {
		// Prefix the ref name with context (define name)
		def.Ref.Name = def.Name + "_" + def.Ref.Name
	}
	def.ParentRef = nestedStart.ParentRef
	// parentRef names should NOT be prefixed - they refer to parent grammar defines

	def.Choice = nestedStart.Choice
	// TODO: Update refs in choice patterns

	def.Group = nestedStart.Group
	// TODO: Update refs in group patterns

	def.Interleave = nestedStart.Interleave
	// TODO: Update refs in interleave patterns

	// Note: Start has Element, Text, Data, List, Empty, NotAllowed, ExternalRef
	// but Define doesn't have those fields, so we handle them differently:
	// - If start has Element, Elements should be updated
	// - Other types would need to be wrapped somehow, but for now we skip them
	if nestedStart.Element != nil {
		def.Elements = []Element{*nestedStart.Element}
	}

	return true, nil
}

// applyNestedStartToElement makes the unpacked nested-grammar start pattern the
// content of the wrapping element el, clearing the now-stale raw grammar. Ref
// names are prefixed with the enclosing define name to match the renamed
// nested defines; parentRef names are left as-is (they target the parent grammar).
func applyNestedStartToElement(el *Element, nestedStart *Start, defName string) {
	el.RawContent = nil

	if nestedStart.Ref != nil {
		ref := *nestedStart.Ref
		if ref.Name != "" {
			ref.Name = defName + "_" + ref.Name
		}
		el.Ref = []Ref{ref}
	}
	if nestedStart.ParentRef != nil {
		el.ParentRef = []Ref{*nestedStart.ParentRef}
	}
	if nestedStart.Element != nil {
		el.Elements = []Element{*nestedStart.Element}
	}
	if nestedStart.Choice != nil {
		el.Choice = nestedStart.Choice
	}
	if len(nestedStart.Group) > 0 {
		el.Group = nestedStart.Group
	}
	if len(nestedStart.Interleave) > 0 {
		el.Interleave = nestedStart.Interleave
	}
	if len(nestedStart.Optional) > 0 {
		el.Optional = nestedStart.Optional
	}
	if len(nestedStart.OneOrMore) > 0 {
		el.OneOrMore = nestedStart.OneOrMore
	}
	if len(nestedStart.ZeroOrMore) > 0 {
		el.ZeroOrMore = nestedStart.ZeroOrMore
	}
	if nestedStart.Text != nil {
		el.Text = nestedStart.Text
	}
	if nestedStart.Data != nil {
		el.Data = nestedStart.Data
	}
	if nestedStart.List != nil {
		el.List = nestedStart.List
	}
	if nestedStart.Empty != nil {
		el.Empty = nestedStart.Empty
	}
	if nestedStart.NotAllowed != nil {
		el.NotAllowed = nestedStart.NotAllowed
	}
}

// applyNestedGrammarToStart applies a nested grammar found in Start's RawContent
func (g *Grammar) applyNestedGrammarToStart(newDefines *[]Define) error {
	nestedStart, defs, err := g.unpackNestedGrammarInRawContent(&g.Start.RawContent, "_start")
	if err != nil {
		return err
	}

	// Validate the nested Start element to catch structural errors like parentRef with children
	if nestedStart != nil {
		if err := validateStartPatterns(nestedStart); err != nil {
			return fmt.Errorf("nested grammar in start element has invalid structure: %v", err)
		}
	}

	if len(defs) > 0 {
		*newDefines = append(*newDefines, defs...)
	}

	// Apply the nested grammar's start pattern to the top-level start
	// This replaces g.Start with the nested grammar's start
	if nestedStart != nil {
		// Prefix all refs in the nested start with context name "_start_"
		g.prefixRefsInStart(nestedStart, "_start")
		// Replace g.Start with nestedStart
		g.Start = *nestedStart
	}

	return nil
}

// unpackNestedGrammarsInStart unpacks nested grammars in Start and its patterns
func (g *Grammar) unpackNestedGrammarsInStart(newDefines *[]Define) error {
	// Check if Start has a direct element that contains a nested grammar. When
	// present, the nested grammar lives inside that element and is unpacked into
	// it here; the RawContent branch below must not also run, or it would
	// replace the whole start with the inner grammar's start and drop the
	// wrapping element.
	if g.Start.Element != nil {
		if err := g.unpackNestedGrammarsInElement(g.Start.Element, newDefines); err != nil {
			return err
		}
	} else if len(g.Start.RawContent) > 0 && bytes.Contains(g.Start.RawContent, []byte("<grammar")) {
		// The nested grammar sits directly in the start pattern.
		if err := g.applyNestedGrammarToStart(newDefines); err != nil {
			return err
		}
	}

	// Unpack from choice
	if g.Start.Choice != nil {
		if err := g.unpackNestedGrammarsInChoice(g.Start.Choice, newDefines); err != nil {
			return err
		}
	}

	// Unpack from groups
	for _, group := range g.Start.Group {
		if err := g.unpackNestedGrammarsInGroup(&group, newDefines); err != nil {
			return err
		}
	}

	// Unpack from interleaves
	for _, interleave := range g.Start.Interleave {
		if err := g.unpackNestedGrammarsInInterleave(&interleave, newDefines); err != nil {
			return err
		}
	}

	// Unpack from optionals
	for _, optional := range g.Start.Optional {
		if err := g.unpackNestedGrammarsInOptional(&optional, newDefines); err != nil {
			return err
		}
	}

	// Unpack from oneOrMores
	for _, oneOrMore := range g.Start.OneOrMore {
		if err := g.unpackNestedGrammarsInOneOrMore(&oneOrMore, newDefines); err != nil {
			return err
		}
	}

	// Unpack from zeroOrMores
	for _, zeroOrMore := range g.Start.ZeroOrMore {
		if err := g.unpackNestedGrammarsInZeroOrMore(&zeroOrMore, newDefines); err != nil {
			return err
		}
	}

	return nil
}

// prefixRefsInStart prefixes all refs in a Start with the given prefix
// This is used when applying nested grammar's start patterns to parent scope
func (g *Grammar) prefixRefsInStart(start *Start, prefix string) {
	if start == nil {
		return
	}

	// Prefix the main ref
	if start.Ref != nil && start.Ref.Name != "" {
		start.Ref.Name = prefix + "_" + start.Ref.Name
	}
	// parentRef names should NOT be prefixed - they refer to parent grammar defines

	// Prefix refs in choice patterns
	if start.Choice != nil {
		for i := range start.Choice.Refs {
			start.Choice.Refs[i].Name = prefix + "_" + start.Choice.Refs[i].Name
		}
	}

	// Prefix refs in group patterns
	for i := range start.Group {
		for j := range start.Group[i].Ref {
			start.Group[i].Ref[j].Name = prefix + "_" + start.Group[i].Ref[j].Name
		}
	}

	// Prefix refs in interleave patterns
	for i := range start.Interleave {
		for j := range start.Interleave[i].Ref {
			start.Interleave[i].Ref[j].Name = prefix + "_" + start.Interleave[i].Ref[j].Name
		}
	}

	// Prefix refs in optional patterns
	for i := range start.Optional {
		for j := range start.Optional[i].Ref {
			start.Optional[i].Ref[j].Name = prefix + "_" + start.Optional[i].Ref[j].Name
		}
	}

	// Prefix refs in oneOrMore patterns
	for i := range start.OneOrMore {
		for j := range start.OneOrMore[i].Ref {
			start.OneOrMore[i].Ref[j].Name = prefix + "_" + start.OneOrMore[i].Ref[j].Name
		}
	}

	// Prefix refs in zeroOrMore patterns
	for i := range start.ZeroOrMore {
		for j := range start.ZeroOrMore[i].Ref {
			start.ZeroOrMore[i].Ref[j].Name = prefix + "_" + start.ZeroOrMore[i].Ref[j].Name
		}
	}
}

// unpackNestedGrammarsInChoice unpacks nested grammars in a choice
func (g *Grammar) unpackNestedGrammarsInChoice(choice *Choice, newDefines *[]Define) error {
	if choice == nil {
		return nil
	}

	if len(choice.RawContent) > 0 && bytes.Contains(choice.RawContent, []byte("<grammar")) {
		// Per RELAX NG spec section 4.18: nested grammars are only allowed in group or interleave,
		// NOT in choice. Reject this as invalid syntax.
		return fmt.Errorf("nested grammar in choice is not allowed (spec section 4.18: nested grammar is only allowed in group or interleave)")
	}

	for _, elem := range choice.Elements {
		if err := g.unpackNestedGrammarsInElement(&elem, newDefines); err != nil {
			return err
		}
	}

	for _, group := range choice.Group {
		if err := g.unpackNestedGrammarsInGroup(&group, newDefines); err != nil {
			return err
		}
	}

	return nil
}

// processNestedGrammarInRawContent is a helper that extracts the common pattern of
// processing nested grammars in RawContent fields, reducing nesting complexity
func (g *Grammar) processNestedGrammarInRawContent(rawContent *[]byte, contextName string, newDefines *[]Define) error {
	if len(*rawContent) == 0 || !bytes.Contains(*rawContent, []byte("<grammar")) {
		return nil
	}

	nestedStart, defs, err := g.unpackNestedGrammarInRawContent(rawContent, contextName)
	if err != nil {
		return err
	}

	// Validate the nested Start element to catch structural errors like parentRef with children
	if nestedStart != nil {
		if err := validateStartPatterns(nestedStart); err != nil {
			return fmt.Errorf("nested grammar in %s has invalid structure: %v", contextName, err)
		}
	}

	if len(defs) > 0 {
		*newDefines = append(*newDefines, defs...)
	}

	return nil
}

// unpackNestedGrammarsInGroup unpacks nested grammars in a group
func (g *Grammar) unpackNestedGrammarsInGroup(group *Group, newDefines *[]Define) error {
	if group == nil {
		return nil
	}

	if err := g.processNestedGrammarInRawContent(&group.RawContent, "group", newDefines); err != nil {
		return err
	}

	for _, elem := range group.Elements {
		if err := g.unpackNestedGrammarsInElement(&elem, newDefines); err != nil {
			return err
		}
	}

	for _, subgroup := range group.Group {
		if err := g.unpackNestedGrammarsInGroup(&subgroup, newDefines); err != nil {
			return err
		}
	}

	return nil
}

// unpackNestedGrammarsInInterleave unpacks nested grammars in an interleave
func (g *Grammar) unpackNestedGrammarsInInterleave(interleave *Interleave, newDefines *[]Define) error {
	if interleave == nil {
		return nil
	}

	if err := g.processNestedGrammarInRawContent(&interleave.RawContent, elemNameInterleave, newDefines); err != nil {
		return err
	}

	for _, elem := range interleave.Elements {
		if err := g.unpackNestedGrammarsInElement(&elem, newDefines); err != nil {
			return err
		}
	}

	return nil
}

// unpackNestedGrammarsInOptional unpacks nested grammars in an optional
func (g *Grammar) unpackNestedGrammarsInOptional(optional *Optional, newDefines *[]Define) error {
	if optional == nil {
		return nil
	}

	if err := g.processNestedGrammarInRawContent(&optional.RawContent, "optional", newDefines); err != nil {
		return err
	}

	for _, elem := range optional.Elements {
		if err := g.unpackNestedGrammarsInElement(&elem, newDefines); err != nil {
			return err
		}
	}

	return nil
}

// unpackNestedGrammarsInOneOrMore unpacks nested grammars in a oneOrMore
func (g *Grammar) unpackNestedGrammarsInOneOrMore(oneOrMore *OneOrMore, newDefines *[]Define) error {
	if oneOrMore == nil {
		return nil
	}

	if err := g.processNestedGrammarInRawContent(&oneOrMore.RawContent, "oneOrMore", newDefines); err != nil {
		return err
	}

	for _, elem := range oneOrMore.Element {
		if err := g.unpackNestedGrammarsInElement(&elem, newDefines); err != nil {
			return err
		}
	}

	return nil
}

// unpackNestedGrammarsInZeroOrMore unpacks nested grammars in a zeroOrMore
func (g *Grammar) unpackNestedGrammarsInZeroOrMore(zeroOrMore *ZeroOrMore, newDefines *[]Define) error {
	if zeroOrMore == nil {
		return nil
	}

	if err := g.processNestedGrammarInRawContent(&zeroOrMore.RawContent, "zeroOrMore", newDefines); err != nil {
		return err
	}

	for _, elem := range zeroOrMore.Element {
		if err := g.unpackNestedGrammarsInElement(&elem, newDefines); err != nil {
			return err
		}
	}

	return nil
}

// applyNestedGrammarToElement extracts and applies a nested grammar to an element
func (g *Grammar) applyNestedGrammarToElement(elem *Element, newDefines *[]Define) error {
	nestedStart, defs, err := g.unpackNestedGrammarInRawContent(&elem.RawContent, elem.Name)
	if err != nil {
		return err
	}
	if len(defs) > 0 {
		*newDefines = append(*newDefines, defs...)
	}

	// Apply the nested grammar's start pattern to the element's content
	if nestedStart != nil {
		g.transferNestedStartToElement(elem, nestedStart)
	}

	return nil
}

// transferNestedStartToElement transfers patterns from a nested start to an element
func (g *Grammar) transferNestedStartToElement(elem *Element, nestedStart *Start) {
	// Prefix all refs in the nested start with context name (element name)
	g.prefixRefsInStart(nestedStart, elem.Name)

	// Transfer the nested grammar's start patterns to the element
	// Note: Element supports most of Start's fields
	// Ref and ParentRef are slices in Element, pointers in Start
	if nestedStart.Ref != nil {
		elem.Ref = []Ref{*nestedStart.Ref}
	}
	if nestedStart.ParentRef != nil {
		elem.ParentRef = []Ref{*nestedStart.ParentRef}
	}
	elem.Choice = nestedStart.Choice
	elem.Group = nestedStart.Group
	elem.Interleave = nestedStart.Interleave
	elem.Optional = nestedStart.Optional
	elem.OneOrMore = nestedStart.OneOrMore
	elem.ZeroOrMore = nestedStart.ZeroOrMore
	elem.Text = nestedStart.Text
	elem.Data = nestedStart.Data
	elem.List = nestedStart.List
	elem.Empty = nestedStart.Empty
	elem.NotAllowed = nestedStart.NotAllowed
	elem.ExternalRef = nestedStart.ExternalRef

	// Element also has Element field for nested elements
	if nestedStart.Element != nil {
		// If nested start has a single element, add it to elem.Elements
		elem.Elements = append(elem.Elements, *nestedStart.Element)
	}
}

// unpackNestedGrammarsInElement unpacks nested grammars in an element
func (g *Grammar) unpackNestedGrammarsInElement(elem *Element, newDefines *[]Define) error {
	if elem == nil {
		return nil
	}

	if len(elem.RawContent) > 0 && bytes.Contains(elem.RawContent, []byte("<grammar")) {
		if err := g.applyNestedGrammarToElement(elem, newDefines); err != nil {
			return err
		}
	}

	if elem.Choice != nil {
		if err := g.unpackNestedGrammarsInChoice(elem.Choice, newDefines); err != nil {
			return err
		}
	}

	for _, group := range elem.Group {
		if err := g.unpackNestedGrammarsInGroup(&group, newDefines); err != nil {
			return err
		}
	}

	for _, interleave := range elem.Interleave {
		if err := g.unpackNestedGrammarsInInterleave(&interleave, newDefines); err != nil {
			return err
		}
	}

	for _, optional := range elem.Optional {
		if err := g.unpackNestedGrammarsInOptional(&optional, newDefines); err != nil {
			return err
		}
	}

	for _, oneOrMore := range elem.OneOrMore {
		if err := g.unpackNestedGrammarsInOneOrMore(&oneOrMore, newDefines); err != nil {
			return err
		}
	}

	for _, zeroOrMore := range elem.ZeroOrMore {
		if err := g.unpackNestedGrammarsInZeroOrMore(&zeroOrMore, newDefines); err != nil {
			return err
		}
	}

	if elem.Mixed != nil {
		for _, childElem := range elem.Mixed.Elements {
			if err := g.unpackNestedGrammarsInElement(&childElem, newDefines); err != nil {
				return err
			}
		}
	}

	return nil
}

// synthesizeMultiplePatterns creates a synthetic choice from multiple pattern types
func synthesizeMultiplePatterns(elem *Element) {
	if elem == nil {
		return
	}

	choice := &Choice{}

	// Add patterns to choice alternatives as synthetic elements
	if elem.Data != nil {
		choice.Elements = append(choice.Elements, Element{Data: elem.Data})
		elem.Data = nil
	}
	if elem.Empty != nil {
		choice.Elements = append(choice.Elements, Element{Empty: elem.Empty})
		elem.Empty = nil
	}
	if elem.Text != nil {
		choice.Elements = append(choice.Elements, Element{Text: elem.Text})
		elem.Text = nil
	}
	if elem.NotAllowed != nil {
		choice.Elements = append(choice.Elements, Element{NotAllowed: elem.NotAllowed})
		elem.NotAllowed = nil
	}
	if elem.List != nil {
		choice.Elements = append(choice.Elements, Element{List: elem.List})
		elem.List = nil
	}
	// TODO: Handle other pattern types (AnyName, NsName, Ref, Group, etc.)

	elem.Choice = choice
}

// synthesizeElementImplicitChoice synthesizes implicit choice patterns within an element
func synthesizeElementImplicitChoice(elem *Element) {
	if elem == nil {
		return
	}

	// Normalize multiple element children to a group
	normalizeMultipleElementChildren(elem)

	// Count simple patterns (Data, Empty, Text, NotAllowed, List) only
	// Other pattern types (Group, Optional, OneOrMore, etc.) don't need synthesis
	// because they already define their own structure
	simplePatternsCount := countSimplePatterns(elem)
	if simplePatternsCount > 1 && elem.Choice == nil {
		synthesizeMultiplePatterns(elem)
	}

	// Recursively process nested elements
	recursivelyProcessNestedElements(elem)
}

// normalizeMultipleElementChildren converts multiple element children to a group
func normalizeMultipleElementChildren(elem *Element) {
	if len(elem.Elements) > 1 {
		// Create a group containing all the element children
		group := Group{
			Elements: elem.Elements,
		}
		elem.Group = []Group{group}
		elem.Elements = nil // Clear Elements field since it's now in the group
	}
}

// countSimplePatterns counts only the simple content patterns (Data, Empty, Text, NotAllowed, List)
// that require implicit choice synthesis. Other pattern types (Group, Optional, etc.) have
// their own structure and don't need synthesis.
func countSimplePatterns(elem *Element) int {
	count := 0
	if elem.Data != nil {
		count++
	}
	if elem.Empty != nil {
		count++
	}
	if elem.Text != nil {
		count++
	}
	if elem.NotAllowed != nil {
		count++
	}
	if elem.List != nil {
		count++
	}
	return count
}

// recursivelyProcessNestedElements recursively synthesizes patterns in nested elements
func recursivelyProcessNestedElements(elem *Element) {
	// Recursively process nested elements in choice alternatives
	if elem.Choice != nil {
		for i := range elem.Choice.Elements {
			synthesizeElementImplicitChoice(&elem.Choice.Elements[i])
		}
	}
	// Recursively process Elements field before moving to groups
	for i := range elem.Elements {
		synthesizeElementImplicitChoice(&elem.Elements[i])
	}
	// Recursively process other nested elements
	for i := range elem.Group {
		for j := range elem.Group[i].Elements {
			synthesizeElementImplicitChoice(&elem.Group[i].Elements[j])
		}
	}
	for i := range elem.Optional {
		for j := range elem.Optional[i].Elements {
			synthesizeElementImplicitChoice(&elem.Optional[i].Elements[j])
		}
	}
	for i := range elem.OneOrMore {
		for j := range elem.OneOrMore[i].Element {
			synthesizeElementImplicitChoice(&elem.OneOrMore[i].Element[j])
		}
	}
	for i := range elem.ZeroOrMore {
		for j := range elem.ZeroOrMore[i].Element {
			synthesizeElementImplicitChoice(&elem.ZeroOrMore[i].Element[j])
		}
	}
	for i := range elem.Interleave {
		for j := range elem.Interleave[i].Elements {
			synthesizeElementImplicitChoice(&elem.Interleave[i].Elements[j])
		}
	}
}

// normalizeElement recursively normalizes whitespace in an element and its children
func normalizeElement(elem *Element) {
	if elem == nil {
		return
	}

	// Normalize element's own names
	normalizeElementOwnNames(elem)

	// Normalize attributes
	normalizeElementAttributeNames(elem)

	// Normalize refs
	for i := range elem.Ref {
		elem.Ref[i].Name = normalizeWhitespaceString(elem.Ref[i].Name)
	}

	// Recursively normalize container patterns
	normalizeElementContainerPatterns(elem)

	// Normalize simple patterns
	normalizeElementDataPatterns(elem)
}

// normalizeElementOwnNames normalizes names in an element
func normalizeElementOwnNames(elem *Element) {
	// Normalize element name (from name attribute)
	elem.Name = normalizeWhitespaceString(elem.Name)

	// Normalize element name (from <name> child element)
	if elem.NameElement != nil {
		elem.NameElement.Value = normalizeWhitespaceString(elem.NameElement.Value)
	}
}

// normalizeElementAttributeNames normalizes all attributes in an element
func normalizeElementAttributeNames(elem *Element) {
	for i := range elem.Attributes {
		elem.Attributes[i].Name = normalizeWhitespaceString(elem.Attributes[i].Name)
		if elem.Attributes[i].NameElement != nil {
			elem.Attributes[i].NameElement.Value = normalizeWhitespaceString(elem.Attributes[i].NameElement.Value)
		}
		// Normalize value type attributes within attributes
		for j := range elem.Attributes[i].Values {
			if elem.Attributes[i].Values[j].Type != "" {
				elem.Attributes[i].Values[j].Type = normalizeWhitespaceString(elem.Attributes[i].Values[j].Type)
			}
		}
		// Normalize data type attributes within attributes
		if elem.Attributes[i].Data != nil && elem.Attributes[i].Data.Type != "" {
			elem.Attributes[i].Data.Type = normalizeWhitespaceString(elem.Attributes[i].Data.Type)
		}
	}
}

// normalizeElementContainerPatterns recursively normalizes container patterns in an element
func normalizeElementContainerPatterns(elem *Element) {
	// Recursively normalize child elements in groups
	for i := range elem.Group {
		normalizeGroup(&elem.Group[i])
	}
	normalizeElementZeroOrMore(elem)
	normalizeElementOneOrMore(elem)
	normalizeElementOptional(elem)
	normalizeElementInterleave(elem)
	normalizeElementMixed(elem)
	normalizeElementChoice(elem)
}

// normalizeElementZeroOrMore normalizes ZeroOrMore patterns within an element
func normalizeElementZeroOrMore(elem *Element) {
	for i := range elem.ZeroOrMore {
		for j := range elem.ZeroOrMore[i].Element {
			normalizeElement(&elem.ZeroOrMore[i].Element[j])
		}
		for j := range elem.ZeroOrMore[i].Ref {
			elem.ZeroOrMore[i].Ref[j].Name = normalizeWhitespaceString(elem.ZeroOrMore[i].Ref[j].Name)
		}
	}
}

// normalizeElementOneOrMore normalizes OneOrMore patterns within an element
func normalizeElementOneOrMore(elem *Element) {
	for i := range elem.OneOrMore {
		for j := range elem.OneOrMore[i].Element {
			normalizeElement(&elem.OneOrMore[i].Element[j])
		}
		for j := range elem.OneOrMore[i].Ref {
			elem.OneOrMore[i].Ref[j].Name = normalizeWhitespaceString(elem.OneOrMore[i].Ref[j].Name)
		}
	}
}

// normalizeElementOptional normalizes Optional patterns within an element
func normalizeElementOptional(elem *Element) {
	for i := range elem.Optional {
		for j := range elem.Optional[i].Elements {
			normalizeElement(&elem.Optional[i].Elements[j])
		}
		for j := range elem.Optional[i].Attributes {
			elem.Optional[i].Attributes[j].Name = normalizeWhitespaceString(elem.Optional[i].Attributes[j].Name)
		}
	}
}

// normalizeElementInterleave normalizes Interleave patterns within an element
func normalizeElementInterleave(elem *Element) {
	for i := range elem.Interleave {
		for j := range elem.Interleave[i].Elements {
			normalizeElement(&elem.Interleave[i].Elements[j])
		}
		for j := range elem.Interleave[i].Ref {
			elem.Interleave[i].Ref[j].Name = normalizeWhitespaceString(elem.Interleave[i].Ref[j].Name)
		}
	}
}

// normalizeElementMixed normalizes Mixed patterns within an element
func normalizeElementMixed(elem *Element) {
	if elem.Mixed != nil {
		for i := range elem.Mixed.Elements {
			normalizeElement(&elem.Mixed.Elements[i])
		}
		for i := range elem.Mixed.Ref {
			elem.Mixed.Ref[i].Name = normalizeWhitespaceString(elem.Mixed.Ref[i].Name)
		}
	}
}

// normalizeElementChoice normalizes Choice patterns within an element
func normalizeElementChoice(elem *Element) {
	if elem.Choice != nil {
		for i := range elem.Choice.Elements {
			normalizeElement(&elem.Choice.Elements[i])
		}
		for i := range elem.Choice.Refs {
			elem.Choice.Refs[i].Name = normalizeWhitespaceString(elem.Choice.Refs[i].Name)
		}
		for i := range elem.Choice.Attributes {
			elem.Choice.Attributes[i].Name = normalizeWhitespaceString(elem.Choice.Attributes[i].Name)
		}
		// Normalize value type attributes within choice
		for i := range elem.Choice.Values {
			if elem.Choice.Values[i].Type != "" {
				elem.Choice.Values[i].Type = normalizeWhitespaceString(elem.Choice.Values[i].Type)
			}
		}
		// Normalize data type attributes within choice
		for i := range elem.Choice.Data {
			if elem.Choice.Data[i].Type != "" {
				elem.Choice.Data[i].Type = normalizeWhitespaceString(elem.Choice.Data[i].Type)
			}
		}
	}
}

// normalizeElementDataPatterns normalizes data patterns in an element
func normalizeElementDataPatterns(elem *Element) {
	// Normalize Values - per spec section 4.2, type attribute must be whitespace-normalized
	for i := range elem.Values {
		if elem.Values[i].Type != "" {
			elem.Values[i].Type = normalizeWhitespaceString(elem.Values[i].Type)
		}
	}

	// Normalize Data.Type and Data.Except.Values
	if elem.Data != nil {
		normalizeDataPattern(elem.Data)
	}

	// Normalize List.Data.Type
	if elem.List != nil && elem.List.Data != nil {
		if elem.List.Data.Type != "" {
			elem.List.Data.Type = normalizeWhitespaceString(elem.List.Data.Type)
		}
	}
}

// normalizeGroup recursively normalizes a group pattern
func normalizeGroup(group *Group) {
	if group == nil {
		return
	}

	for i := range group.Elements {
		normalizeElement(&group.Elements[i])
	}
	for i := range group.Ref {
		group.Ref[i].Name = normalizeWhitespaceString(group.Ref[i].Name)
	}
	for i := range group.Optional {
		for j := range group.Optional[i].Elements {
			normalizeElement(&group.Optional[i].Elements[j])
		}
		for j := range group.Optional[i].Attributes {
			group.Optional[i].Attributes[j].Name = normalizeWhitespaceString(group.Optional[i].Attributes[j].Name)
		}
	}
	for i := range group.OneOrMore {
		for j := range group.OneOrMore[i].Element {
			normalizeElement(&group.OneOrMore[i].Element[j])
		}
		for j := range group.OneOrMore[i].Ref {
			group.OneOrMore[i].Ref[j].Name = normalizeWhitespaceString(group.OneOrMore[i].Ref[j].Name)
		}
	}
	for i := range group.ZeroOrMore {
		for j := range group.ZeroOrMore[i].Element {
			normalizeElement(&group.ZeroOrMore[i].Element[j])
		}
		for j := range group.ZeroOrMore[i].Ref {
			group.ZeroOrMore[i].Ref[j].Name = normalizeWhitespaceString(group.ZeroOrMore[i].Ref[j].Name)
		}
	}
	for i := range group.Choice {
		for j := range group.Choice[i].Elements {
			normalizeElement(&group.Choice[i].Elements[j])
		}
		for j := range group.Choice[i].Refs {
			group.Choice[i].Refs[j].Name = normalizeWhitespaceString(group.Choice[i].Refs[j].Name)
		}
		for j := range group.Choice[i].Attributes {
			group.Choice[i].Attributes[j].Name = normalizeWhitespaceString(group.Choice[i].Attributes[j].Name)
		}
	}
	for i := range group.Group {
		normalizeGroup(&group.Group[i])
	}
}

// normalizeWhitespaceString collapses whitespace: removes leading/trailing, converts sequences to single space
func normalizeWhitespaceString(s string) string {
	// Use regex to collapse whitespace
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

// normalizeDataPattern normalizes the type attributes in a Data pattern
// Per RELAX NG spec section 4.2: whitespace is normalized in type attributes
func normalizeDataPattern(data *Data) {
	if data == nil {
		return
	}

	// Normalize the data type attribute
	if data.Type != "" {
		data.Type = normalizeWhitespaceString(data.Type)
	}

	// Normalize except clause value types and data types
	if data.Except != nil {
		for i := range data.Except.Values {
			if data.Except.Values[i].Type != "" {
				data.Except.Values[i].Type = normalizeWhitespaceString(data.Except.Values[i].Type)
			}
		}
		for i := range data.Except.Data {
			if data.Except.Data[i].Type != "" {
				data.Except.Data[i].Type = normalizeWhitespaceString(data.Except.Data[i].Type)
			}
		}
	}
}

// extractNamespaceContext builds a map of namespace prefixes to URIs from RawAttrs
// RawAttrs contains xmlns declarations as attributes with Space="xmlns" or Space=""
func extractNamespaceContext(attrs []xml.Attr) map[string]string {
	nsMap := make(map[string]string)
	// Add the default XML namespace
	nsMap["xml"] = "http://www.w3.org/XML/1998/namespace"

	for _, attr := range attrs {
		// xmlns:prefix="uri" shows up as Space="xmlns" Local="prefix"
		if attr.Name.Space == "xmlns" {
			nsMap[attr.Name.Local] = attr.Value
		}
		// xmlns="uri" (default namespace) shows up as Space="" Local="xmlns"
		if attr.Name.Local == elemNameXmlns && attr.Name.Space == "" {
			nsMap[""] = attr.Value
		}
	}
	return nsMap
}

// resolveQName takes a QName (like "eg:foo") and a namespace context, and resolves it
// Returns (namespace, localName) or ("", qname) if no prefix found
func resolveQName(qname string, nsMap map[string]string) (string, string) {
	parts := strings.SplitN(qname, ":", 2)
	if len(parts) == 1 {
		// No prefix - use default namespace if set
		defaultNs, ok := nsMap[""]
		if ok {
			return defaultNs, parts[0]
		}
		// No default namespace - no namespace
		return "", parts[0]
	}

	// Has prefix
	prefix := parts[0]
	localName := parts[1]

	ns, ok := nsMap[prefix]
	if !ok {
		// Prefix not found in namespace map - return as-is
		// (This should be a validation error, but for now we'll return the local name)
		return "", qname
	}

	return ns, localName
}

// resolveQNamesInElement resolves QNames in element names using the namespace context
func resolveQNamesInElement(elem *Element, nsMap map[string]string) {
	if elem == nil {
		return
	}

	// Resolve element's own QNames
	resolveElementQNames(elem, nsMap)

	// Resolve attribute QNames
	resolveAttributeQNames(elem.Attributes, nsMap)

	// Recursively resolve in nested patterns
	resolveQNamesInElementContainers(elem, nsMap)
}

// resolveElementQNames resolves QNames in element's name fields
func resolveElementQNames(elem *Element, nsMap map[string]string) {
	// Resolve element name if it contains a QName
	if elem.Name != "" && strings.Contains(elem.Name, ":") {
		ns, localName := resolveQName(elem.Name, nsMap)
		if ns != "" {
			// Store as namespace|name for matching
			elem.Ns = ns
			elem.Name = localName
		}
	}

	// Resolve NameElement
	if elem.NameElement != nil && elem.NameElement.Value != "" && strings.Contains(elem.NameElement.Value, ":") {
		ns, localName := resolveQName(elem.NameElement.Value, nsMap)
		if ns != "" && elem.NameElement.Ns == "" {
			// Only override if NameElement doesn't already have explicit ns
			elem.NameElement.Ns = ns
			elem.NameElement.Value = localName
		}
	}
}

// resolveAttributeQNames resolves QNames in attribute names
func resolveAttributeQNames(attrs []Attribute, nsMap map[string]string) {
	for i := range attrs {
		attr := &attrs[i]
		if attr.Name != "" && strings.Contains(attr.Name, ":") {
			ns, localName := resolveQName(attr.Name, nsMap)
			if ns != "" {
				attr.Ns = ns
				attr.Name = localName
			}
		}
		if attr.NameElement != nil && attr.NameElement.Value != "" && strings.Contains(attr.NameElement.Value, ":") {
			ns, localName := resolveQName(attr.NameElement.Value, nsMap)
			if ns != "" && attr.NameElement.Ns == "" {
				attr.NameElement.Ns = ns
				attr.NameElement.Value = localName
			}
		}
	}
}

// resolveQNamesInElementContainers resolves QNames in element's container patterns
func resolveQNamesInElementContainers(elem *Element, nsMap map[string]string) {
	for i := range elem.Optional {
		for j := range elem.Optional[i].Elements {
			resolveQNamesInElement(&elem.Optional[i].Elements[j], nsMap)
		}
	}
	for i := range elem.OneOrMore {
		for j := range elem.OneOrMore[i].Element {
			resolveQNamesInElement(&elem.OneOrMore[i].Element[j], nsMap)
		}
	}
	for i := range elem.ZeroOrMore {
		for j := range elem.ZeroOrMore[i].Element {
			resolveQNamesInElement(&elem.ZeroOrMore[i].Element[j], nsMap)
		}
	}
	if elem.Choice != nil {
		resolveQNamesInChoice(elem.Choice, nsMap)
	}
	for i := range elem.Group {
		resolveQNamesInGroup(&elem.Group[i], nsMap)
	}
	for i := range elem.Interleave {
		resolveQNamesInInterleave(&elem.Interleave[i], nsMap)
	}
	if elem.Mixed != nil {
		resolveQNamesInMixed(elem.Mixed, nsMap)
	}
}

// resolveQNamesInGroup resolves QNames in a group pattern
func resolveQNamesInGroup(group *Group, nsMap map[string]string) {
	if group == nil {
		return
	}
	for i := range group.Elements {
		resolveQNamesInElement(&group.Elements[i], nsMap)
	}
	for i := range group.Group {
		resolveQNamesInGroup(&group.Group[i], nsMap)
	}
	for i := range group.Choice {
		resolveQNamesInChoice(&group.Choice[i], nsMap)
	}
	for i := range group.Optional {
		for j := range group.Optional[i].Elements {
			resolveQNamesInElement(&group.Optional[i].Elements[j], nsMap)
		}
	}
	for i := range group.OneOrMore {
		for j := range group.OneOrMore[i].Element {
			resolveQNamesInElement(&group.OneOrMore[i].Element[j], nsMap)
		}
	}
	for i := range group.ZeroOrMore {
		for j := range group.ZeroOrMore[i].Element {
			resolveQNamesInElement(&group.ZeroOrMore[i].Element[j], nsMap)
		}
	}
}

// resolveQNamesInChoice resolves QNames in a choice pattern
func resolveQNamesInChoice(choice *Choice, nsMap map[string]string) {
	if choice == nil {
		return
	}
	for i := range choice.Elements {
		resolveQNamesInElement(&choice.Elements[i], nsMap)
	}
}

// resolveQNamesInInterleave resolves QNames in an interleave pattern
func resolveQNamesInInterleave(interleave *Interleave, nsMap map[string]string) {
	if interleave == nil {
		return
	}
	for i := range interleave.Elements {
		resolveQNamesInElement(&interleave.Elements[i], nsMap)
	}
}

// resolveQNamesInMixed resolves QNames in a mixed pattern
func resolveQNamesInMixed(mixed *Mixed, nsMap map[string]string) {
	if mixed == nil {
		return
	}
	for i := range mixed.Elements {
		resolveQNamesInElement(&mixed.Elements[i], nsMap)
	}
}

// ParseSchemaFile parses a RELAX NG schema from a file path and processes all includes and externalRefs.
// It validates paths to prevent directory traversal attacks and detects circular dependencies.
// The baseDir parameter specifies the base directory for resolving relative paths.
// ParseSchemaFile parses a schema file from disk with include/externalRef support.
// Uses a DiskResolver for loading referenced schemas.
func ParseSchemaFile(path string, baseDir string) (*Grammar, error) {
	resolver := &DiskResolver{BaseDir: baseDir}
	return ParseSchemaWithResolver(path, resolver)
}

// ParseSchemaWithResolver parses a schema using a custom ResourceResolver.
// This allows for virtual filesystems, testing, or alternative storage backends.
func ParseSchemaWithResolver(path string, resolver ResourceResolver) (*Grammar, error) {
	visited := make(map[string]bool)
	defineNames := make(map[string]bool)
	// Top-level schemas can have patterns as root (simplified syntax), so requiresGrammar is false
	grammar, err := parseSchemaWithResolverInternal(path, resolver, visited, defineNames, true, false)
	return grammar, err
}

// parseSchemaWithResolverInternal is the internal implementation using a ResourceResolver
// validateRefs indicates whether to validate refs at the end (only done at top level)
// decodePatternAsGrammar decodes a pattern element as a Grammar, wrapping it in a start element.
func decodePatternAsGrammar(rootElemName string, data []byte, path string) (*Grammar, error) {
	switch rootElemName {
	case "externalRef":
		return decodeExternalRefAsGrammar(data, path)
	case elemNameElement:
		return decodeElementAsGrammar(data, path)
	case "group":
		return decodeGroupAsGrammar(data, path)
	case elemNameChoice:
		return decodeChoiceAsGrammar(data, path)
	case elemNameInterleave:
		return decodeInterleaveAsGrammar(data, path)
	case "ref":
		return decodeRefAsGrammar(data, path)
	default:
		return decodeAsStartPattern(data, path, rootElemName)
	}
}

func decodeExternalRefAsGrammar(data []byte, path string) (*Grammar, error) {
	extRef := &ExternalRef{}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(extRef); err != nil || extRef.Href == "" {
		return nil, fmt.Errorf("error parsing %s: %w", path, err)
	}

	// Set base to the directory of the current path
	if extRef.Base == "" {
		dir := filepath.Dir(path)
		if dir != "." && dir != "" && !strings.HasSuffix(dir, "/") {
			dir += "/"
		}
		extRef.Base = dir
	}

	return &Grammar{
		Start: Start{
			ExternalRef: extRef,
		},
	}, nil
}

func decodeElementAsGrammar(data []byte, path string) (*Grammar, error) {
	elem := &Element{}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(elem); err != nil {
		return nil, fmt.Errorf("error parsing %s: %w", path, err)
	}
	return &Grammar{
		Start: Start{
			Element: elem,
		},
	}, nil
}

func decodeGroupAsGrammar(data []byte, path string) (*Grammar, error) {
	grp := &Group{}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(grp); err != nil {
		return nil, fmt.Errorf("error parsing %s: %w", path, err)
	}
	return &Grammar{
		Start: Start{
			Group: []Group{*grp},
		},
	}, nil
}

func decodeChoiceAsGrammar(data []byte, path string) (*Grammar, error) {
	choice := &Choice{}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(choice); err != nil {
		return nil, fmt.Errorf("error parsing %s: %w", path, err)
	}
	return &Grammar{
		Start: Start{
			Choice: choice,
		},
	}, nil
}

func decodeInterleaveAsGrammar(data []byte, path string) (*Grammar, error) {
	interleave := &Interleave{}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(interleave); err != nil {
		return nil, fmt.Errorf("error parsing %s: %w", path, err)
	}
	return &Grammar{
		Start: Start{
			Interleave: []Interleave{*interleave},
		},
	}, nil
}

func decodeRefAsGrammar(data []byte, path string) (*Grammar, error) {
	ref := &Ref{}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(ref); err != nil {
		return nil, fmt.Errorf("error parsing %s: %w", path, err)
	}
	return &Grammar{
		Start: Start{
			Ref: ref,
		},
	}, nil
}

func decodeAsStartPattern(data []byte, path string, rootElemName string) (*Grammar, error) {
	// Wrap the content in a start element and try to decode
	wrapped := `<start xmlns="http://relaxng.org/ns/structure/1.0">` + string(data) + `</start>`
	start := &Start{}
	decoder := xml.NewDecoder(strings.NewReader(wrapped))
	if err := decoder.Decode(start); err != nil {
		return nil, fmt.Errorf("error parsing %s: unsupported root element <%s>", path, rootElemName)
	}
	return &Grammar{
		Start: *start,
	}, nil
}

// parseSchemaAsPattern decodes XML data as a pattern when grammar decoding fails
// Per RELAX NG spec section 1.3: a schema can be either a <grammar> element or a pattern directly
func parseSchemaAsPattern(data []byte, path string, requiresGrammar bool) (*Grammar, error) {
	// Peek at the root element name to determine what struct to decode into
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var rootElem xml.StartElement
	var foundRootElem bool

	// Skip to the first StartElement (skipping XML declaration, comments, whitespace, etc)
	for {
		tok, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("error reading root element in %s: %w", path, err)
		}

		if elem, ok := tok.(xml.StartElement); ok {
			rootElem = elem
			foundRootElem = true
			break
		}
	}

	if !foundRootElem {
		return nil, fmt.Errorf("error parsing %s: no root element found", path)
	}

	// If requiresGrammar is true (for includes), the root MUST be <grammar>
	// Per RELAX NG spec section 4.5: an include target must be a valid RELAX NG module
	if requiresGrammar && rootElem.Name.Local != elemNameGrammar {
		return nil, fmt.Errorf("error parsing %s: included file must have <grammar> as root element, not <%s>", path, rootElem.Name.Local)
	}

	// Based on the root element's local name (without namespace), decide what to decode
	return decodePatternAsGrammar(rootElem.Name.Local, data, path)
}

// requiresGrammar indicates that the root element must be <grammar> (for includes)
func parseSchemaWithResolverInternal(path string, resolver ResourceResolver, visited map[string]bool, defineNames map[string]bool, validateRefsAtEnd bool, requiresGrammar bool) (*Grammar, error) {
	// Check for cycles - used for both includes and externalRefs
	cleanPath := filepath.Clean(path)
	if visited[cleanPath] {
		return nil, fmt.Errorf("include cycle detected: %s", cleanPath)
	}
	visited[cleanPath] = true
	// IMPORTANT: Do NOT use defer to delete from visited
	// For externalRef chains, we need to keep the path marked to detect cycles
	// We will manually manage cleanup at appropriate points

	// Read and parse the schema
	grammar, err := readAndParseSchema(path, resolver, requiresGrammar)
	if err != nil {
		return nil, err
	}

	// Validate grammar structure (but not includes/refs yet)
	if err := grammar.ValidateAsLibrary(true); err != nil {
		return nil, fmt.Errorf("error parsing %s: %w", path, err)
	}

	// Process includes and external references
	if err := processIncludesAndExternalRefs(grammar, path, resolver, visited, defineNames); err != nil {
		return nil, err
	}

	// Post-process validation and merging
	if err := postProcessGrammar(grammar, defineNames, validateRefsAtEnd); err != nil {
		return nil, err
	}

	return grammar, nil
}

// readAndParseSchema reads a schema file and parses it into a Grammar
func readAndParseSchema(path string, resolver ResourceResolver, requiresGrammar bool) (*Grammar, error) {
	data, err := resolver.ReadResource(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read resource %s: %w", path, err)
	}

	grammar := &Grammar{}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(grammar); err != nil {
		// Decoding as Grammar failed - try as pattern element
		var decodeErr error
		grammar, decodeErr = parseSchemaAsPattern(data, path, requiresGrammar)
		if decodeErr != nil {
			return nil, decodeErr
		}
	}

	// Per spec section 4.11: flatten div elements before validation
	grammar.flattenDivs()

	return grammar, nil
}

// processIncludesAndExternalRefs processes all includes and external references in a grammar
func processIncludesAndExternalRefs(grammar *Grammar, path string, resolver ResourceResolver, visited map[string]bool, defineNames map[string]bool) error {
	// Set Base attributes for includes
	setIncludeBaseAttributes(grammar, path)

	// Process includes
	for _, include := range grammar.Includes {
		if err := processIncludeWithResolver(grammar, include, resolver, visited, defineNames); err != nil {
			return err
		}
	}

	// Set Base attributes for externalRefs
	setExternalRefBaseAttributes(grammar, path)

	// Process externalRefs
	for _, extRef := range grammar.ExternalRefs {
		if err := processExternalRefWithResolver(grammar, extRef, resolver, visited, defineNames); err != nil {
			return err
		}
	}

	// Resolve nested externalRefs (e.g., in Start pattern) per spec section 4.6
	if err := resolveNestedExternalRefs(grammar, resolver, visited, defineNames); err != nil {
		return err
	}

	return nil
}

// setIncludeBaseAttributes sets Base attributes for include elements
func setIncludeBaseAttributes(grammar *Grammar, path string) {
	for i := range grammar.Includes {
		if grammar.Includes[i].Base == "" {
			dir := filepath.Dir(path)
			// Ensure directory paths end with "/" for consistent resolution
			if dir != "." && dir != "" && !strings.HasSuffix(dir, "/") {
				dir += "/"
			}
			grammar.Includes[i].Base = dir
		}
	}
}

// setExternalRefBaseAttributes sets Base attributes for externalRef elements
func setExternalRefBaseAttributes(grammar *Grammar, path string) {
	for i := range grammar.ExternalRefs {
		if grammar.ExternalRefs[i].Base == "" {
			dir := filepath.Dir(path)
			// Ensure directory paths end with "/" for consistent resolution
			if dir != "." && dir != "" && !strings.HasSuffix(dir, "/") {
				dir += "/"
			}
			grammar.ExternalRefs[i].Base = dir
		}
	}
}

// postProcessGrammar performs post-processing validation and merging
func postProcessGrammar(grammar *Grammar, defineNames map[string]bool, validateRefsAtEnd bool) error {
	// After includes are processed, check for duplicate defines
	if len(grammar.Includes) > 0 || len(grammar.ExternalRefs) > 0 {
		if err := grammar.checkDuplicateDefines(); err != nil {
			return fmt.Errorf("error in duplicate define check: %w", err)
		}
	}

	// Merge defines with combine attributes
	if err := grammar.mergeDefinesWithCombine(); err != nil {
		return fmt.Errorf("error merging defines: %w", err)
	}

	// Merge start elements with combine attributes
	if err := grammar.mergeStartsWithCombine(); err != nil {
		return fmt.Errorf("error merging starts: %w", err)
	}

	// Validate refs after all processing is complete
	if validateRefsAtEnd {
		allDefineNames := make(map[string]bool)
		for name := range defineNames {
			allDefineNames[name] = true
		}
		// Also add defines from the grammar itself
		for _, def := range grammar.Defines {
			allDefineNames[def.Name] = true
		}

		if err := grammar.validateRefs(allDefineNames); err != nil {
			return err
		}
	}

	// Post-process: synthesize implicit choice patterns
	grammar.synthesizeImplicitPatterns()

	return nil
}

// normalizeBaseDirectoryPath normalizes a base path to ensure it ends with "/" for directory operations
// endsWithSlash indicates whether the original path ended with "/" before cleaning
func normalizeBaseDirectoryPath(base string, endsWithSlash bool) string {
	if endsWithSlash {
		// base already ends with /, ensure it stays that way
		if !strings.HasSuffix(base, "/") {
			base += "/"
		}
		return base
	}

	// base doesn't end with /
	if filepath.Dir(base) == base {
		// base is a simple directory name like "sub" (Dir("sub") = ".")
		// Treat it as a directory
		base += "/"
	} else {
		// base has a path component (like "sub/y")
		// This means it's likely a file path, so use its directory
		base = filepath.Dir(base)
		if !strings.HasSuffix(base, "/") {
			base += "/"
		}
	}
	return base
}

// resolveXMLBase resolves a href against an xml:base attribute
// Per RFC 3986, xml:base can be either a directory or a file URI path
// We normalize base to a directory first (per spec section 4.5)
// Example: xml:base="sub/" + href="x" → "sub/x"
//
//	xml:base="sub" + href="x" → "sub/x" (normalized to "sub/")
//	xml:base="sub/y" + href="x" → "sub/x" (normalized to "sub/" per RFC)
//	xml:base="" + href="x" → "x"
func resolveXMLBase(base, href string) string {
	if base == "" {
		return href
	}

	// If href is absolute, ignore base
	if filepath.IsAbs(href) {
		return href
	}

	// Check if base ends with "/" before cleaning (important for directory vs file detection)
	endsWithSlash := strings.HasSuffix(base, "/")

	// Normalize the base path, but preserve the fact that it's a directory if it ends with /
	base = filepath.Clean(base)
	if base == "." || base == "" {
		return href
	}

	// Per RFC 3986: when base is a relative reference like "sub/y",
	// we should treat it as having implied directory semantics
	// So we use its directory for resolving relative references
	// This handles both:
	// - "sub/" (dir) + "x" → "sub/x"  (endsWithSlash=true)
	// - "sub" (dir) + "x" → "sub/x"   (endsWithSlash=false, but Dir("sub")=".", so it's treated as dir)
	// - "sub/y" (file) + "x" → "sub/x" (endsWithSlash=false, Dir("sub/y")="sub", so it's treated as file)

	base = normalizeBaseDirectoryPath(base, endsWithSlash)
	return filepath.Join(base, href)
}

// resolveExternalRefPattern resolves an externalRef element to a pattern (Start)
// Per spec section 4.6: loads the referenced resource, parses it, transfers ns attribute if needed
// parentBase is the xml:base inherited from parent elements
func resolveExternalRefPattern(extRef ExternalRef, resolver ResourceResolver, visited map[string]bool, defineNames map[string]bool, parentBase string) (Start, error) {
	// Per spec section 4.5: href must not include fragment identifier for XML resources
	// RFC 3023 does not define fragment identifiers for application/xml or text/xml
	if strings.Contains(extRef.Href, "#") {
		return Start{}, fmt.Errorf("href attribute must not contain fragment identifier: %s", extRef.Href)
	}

	// Resolve href with xml:base
	// Use extRef's own Base if present, otherwise use parentBase
	base := extRef.Base
	if base == "" {
		base = parentBase
	}
	resolvedHref := resolveXMLBase(base, extRef.Href)

	// Load the referenced resource
	data, err := resolver.ReadResource(resolvedHref)
	if err != nil {
		return Start{}, fmt.Errorf("failed to load external ref %s: %w", resolvedHref, err)
	}

	// Try to parse as a full grammar first
	// Note: parseSchemaWithResolverInternal will handle cycle detection via visited map
	// ExternalRef can reference patterns directly (per spec), so requiresGrammar is false
	grammar, err := parseSchemaWithResolverInternal(resolvedHref, resolver, visited, defineNames, false, false)
	if err == nil {
		// Successfully parsed as grammar - use its start pattern
		result := grammar.Start

		// Transfer ns attribute per spec 4.6
		if extRef.Ns != "" && result.Element != nil && result.Element.Ns == "" {
			result.Element.Ns = extRef.Ns
		}

		return result, nil
	}

	// Not a full grammar - try parsing as a bare pattern
	// Wrap in a start element to parse
	wrapped := `<start xmlns="http://relaxng.org/ns/structure/1.0">` + string(data) + `</start>`

	var result Start
	decoder := xml.NewDecoder(strings.NewReader(wrapped))
	if err := decoder.Decode(&result); err != nil {
		return Start{}, fmt.Errorf("failed to parse external ref %s as pattern: %w", resolvedHref, err)
	}

	// Validate that the wrapped content doesn't have nested <start> elements
	// This would happen if data itself contained <start>, which is invalid
	// Per spec 4.6: externalRef must reference a grammar document with a <start> element
	if bytes.Contains(result.RawContent, []byte("<start")) {
		return Start{}, fmt.Errorf("external ref %s references invalid content: bare start element", resolvedHref)
	}

	// Transfer ns attribute per spec 4.6
	if extRef.Ns != "" && result.Element != nil && result.Element.Ns == "" {
		result.Element.Ns = extRef.Ns
	}

	return result, nil
}

// resolveNestedExternalRefs resolves externalRef elements within patterns (per spec 4.6)
// This handles externalRef in Start and other pattern locations
func resolveNestedExternalRefs(grammar *Grammar, resolver ResourceResolver, visited map[string]bool, defineNames map[string]bool) error {
	// Resolve Start.ExternalRef if present (loop to handle chains of externalRefs)
	// The spec allows externalRef chains
	// Per spec section 4.6: externalRef elements reference patterns from other grammar documents
	//
	// ExternalRef cycles are allowed in schema definitions - they represent lazy pattern references
	// and only become problematic during validation if actual content matching loops infinitely.
	// We break the loop when the externalRef doesn't resolve to a different pattern (cycle detected),
	// and leave the cyclic externalRef in place for validation to handle.

	seenExternalRefs := make(map[string]bool) // Track resolved paths to detect cycles
	for grammar.Start.ExternalRef != nil {
		// Resolve the href to get the actual path
		resolvedPath := resolveXMLBase(grammar.Start.ExternalRef.Base, grammar.Start.ExternalRef.Href)
		resolvedPath = filepath.Clean(resolvedPath)

		// Check if we've already tried to resolve this path in this chain
		// If so, we have a cycle - this is an error per spec
		if seenExternalRefs[resolvedPath] {
			// Cycle detected in externalRef chain - this is not allowed
			return fmt.Errorf("externalRef cycle detected: %s", resolvedPath)
		}
		seenExternalRefs[resolvedPath] = true

		resolved, err := resolveExternalRefPattern(*grammar.Start.ExternalRef, resolver, visited, defineNames, "")
		if err != nil {
			return fmt.Errorf("failed to resolve externalRef in start: %w", err)
		}
		grammar.Start = resolved
	}

	// Now resolve externalRef elements in nested patterns (groups, interleaves, etc)
	if err := resolveExternalRefsInStart(&grammar.Start, resolver, visited, defineNames); err != nil {
		return err
	}

	return nil
}

// resolveExternalRefsInStart recursively resolves externalRef elements in a Start pattern
func resolveExternalRefsInStart(start *Start, resolver ResourceResolver, visited map[string]bool, defineNames map[string]bool) error {
	// Resolve groups
	for i := range start.Group {
		if err := resolveExternalRefsInGroupWithBase(&start.Group[i], resolver, visited, defineNames, ""); err != nil {
			return err
		}
	}

	// Resolve interleaves
	for i := range start.Interleave {
		if err := resolveExternalRefsInInterleaveWithBase(&start.Interleave[i], resolver, visited, defineNames, ""); err != nil {
			return err
		}
	}

	// Resolve elements
	if start.Element != nil {
		if err := resolveExternalRefsInElement(start.Element, resolver, visited, defineNames); err != nil {
			return err
		}
	}

	// Resolve choice
	if start.Choice != nil {
		if err := resolveExternalRefsInChoiceWithBase(start.Choice, resolver, visited, defineNames, ""); err != nil {
			return err
		}
	}

	// Resolve optional, oneOrMore, zeroOrMore
	for i := range start.Optional {
		if err := resolveExternalRefsInOptionalWithBase(&start.Optional[i], resolver, visited, defineNames, ""); err != nil {
			return err
		}
	}
	for i := range start.OneOrMore {
		if err := resolveExternalRefsInOneOrMoreWithBase(&start.OneOrMore[i], resolver, visited, defineNames, ""); err != nil {
			return err
		}
	}
	for i := range start.ZeroOrMore {
		if err := resolveExternalRefsInZeroOrMoreWithBase(&start.ZeroOrMore[i], resolver, visited, defineNames, ""); err != nil {
			return err
		}
	}

	return nil
}

// resolveExternalRefsInGroup recursively resolves externalRef elements in a Group
// parentBase is xml:base inherited from parent elements, to be propagated to children
func resolveExternalRefsInGroup(group *Group, resolver ResourceResolver, visited map[string]bool, defineNames map[string]bool) error {
	return resolveExternalRefsInGroupWithBase(group, resolver, visited, defineNames, "")
}

// resolveExternalRefsInGroupWithBase is the internal implementation that tracks parentBase
func resolveExternalRefsInGroupWithBase(group *Group, resolver ResourceResolver, visited map[string]bool, defineNames map[string]bool, parentBase string) error {
	// Per XML spec: xml:base is resolved relative to parent's xml:base
	currentBase := parentBase
	if group.Base != "" {
		currentBase = resolveXMLBase(parentBase, group.Base)
	}

	// Resolve direct externalRef in group
	if err := resolveDirectExternalRefInGroup(group, resolver, visited, defineNames, currentBase); err != nil {
		return err
	}

	// Recursively resolve in child patterns, propagating currentBase
	if err := resolveExternalRefsInGroupChildren(group, resolver, visited, defineNames, currentBase); err != nil {
		return err
	}

	return nil
}

// resolveDirectExternalRefInGroup resolves direct externalRef in a group and merges the result
func resolveDirectExternalRefInGroup(group *Group, resolver ResourceResolver, visited map[string]bool, defineNames map[string]bool, currentBase string) error {
	maxIterations := 100
	iterations := 0
	for group.ExternalRef != nil {
		iterations++
		if iterations > maxIterations {
			return fmt.Errorf("too many nested externalRef resolutions in group")
		}

		resolved, err := resolveExternalRefPattern(*group.ExternalRef, resolver, visited, defineNames, currentBase)
		if err != nil {
			return err
		}

		// Replace this group's content with resolved pattern
		group.ExternalRef = nil
		// Copy resolved group content into this group
		if len(resolved.Group) > 0 {
			// If resolved is a group, copy its contents
			group.Elements = append(group.Elements, resolved.Group[0].Elements...)
			group.Ref = append(group.Ref, resolved.Group[0].Ref...)
			group.Optional = append(group.Optional, resolved.Group[0].Optional...)
			group.OneOrMore = append(group.OneOrMore, resolved.Group[0].OneOrMore...)
			group.ZeroOrMore = append(group.ZeroOrMore, resolved.Group[0].ZeroOrMore...)
			group.Choice = append(group.Choice, resolved.Group[0].Choice...)
			group.Group = append(group.Group, resolved.Group[0].Group...)
		} else if resolved.Element != nil {
			// If resolved is an element, add it
			// Per spec 4.6: transfer ns attribute from group if element doesn't have one
			elem := *resolved.Element
			if group.Ns != "" && elem.Ns == "" {
				elem.Ns = group.Ns
			}
			group.Elements = append(group.Elements, elem)
		}
	}
	return nil
}

// resolveExternalRefsInGroupChildren recursively resolves externalRefs in group's child patterns
func resolveExternalRefsInGroupChildren(group *Group, resolver ResourceResolver, visited map[string]bool, defineNames map[string]bool, currentBase string) error {
	for i := range group.Elements {
		if err := resolveExternalRefsInElementWithBase(&group.Elements[i], resolver, visited, defineNames, currentBase); err != nil {
			return err
		}
	}
	for i := range group.Group {
		if err := resolveExternalRefsInGroupWithBase(&group.Group[i], resolver, visited, defineNames, currentBase); err != nil {
			return err
		}
	}
	for i := range group.Optional {
		if err := resolveExternalRefsInOptionalWithBase(&group.Optional[i], resolver, visited, defineNames, currentBase); err != nil {
			return err
		}
	}
	for i := range group.OneOrMore {
		if err := resolveExternalRefsInOneOrMoreWithBase(&group.OneOrMore[i], resolver, visited, defineNames, currentBase); err != nil {
			return err
		}
	}
	for i := range group.ZeroOrMore {
		if err := resolveExternalRefsInZeroOrMoreWithBase(&group.ZeroOrMore[i], resolver, visited, defineNames, currentBase); err != nil {
			return err
		}
	}
	for i := range group.Choice {
		if err := resolveExternalRefsInChoiceWithBase(&group.Choice[i], resolver, visited, defineNames, currentBase); err != nil {
			return err
		}
	}
	return nil
}

// resolveExternalRefsInInterleave recursively resolves externalRef elements in an Interleave
func resolveExternalRefsInInterleave(interleave *Interleave, resolver ResourceResolver, visited map[string]bool, defineNames map[string]bool) error {
	return resolveExternalRefsInInterleaveWithBase(interleave, resolver, visited, defineNames, "")
}

// resolveExternalRefsInElement recursively resolves externalRef elements in an Element
func resolveExternalRefsInElement(elem *Element, resolver ResourceResolver, visited map[string]bool, defineNames map[string]bool) error {
	return resolveExternalRefsInElementWithBase(elem, resolver, visited, defineNames, "")
}

// resolveExternalRefsInElementWithBase is the internal implementation that tracks parentBase
func resolveExternalRefsInElementWithBase(elem *Element, resolver ResourceResolver, visited map[string]bool, defineNames map[string]bool, parentBase string) error {
	// Resolve direct externalRef
	if err := resolveDirectExternalRefInElement(elem, resolver, visited, defineNames, parentBase); err != nil {
		return err
	}

	// Recursively resolve in child patterns
	if err := resolveExternalRefsInElementChildren(elem, resolver, visited, defineNames); err != nil {
		return err
	}

	return nil
}

// resolveDirectExternalRefInElement resolves direct externalRef in an element and merges the result
func resolveDirectExternalRefInElement(elem *Element, resolver ResourceResolver, visited map[string]bool, defineNames map[string]bool, parentBase string) error {
	maxIterations := 100
	iterations := 0
	for elem.ExternalRef != nil {
		iterations++
		if iterations > maxIterations {
			return fmt.Errorf("too many nested externalRef resolutions in element")
		}

		resolved, err := resolveExternalRefPattern(*elem.ExternalRef, resolver, visited, defineNames, parentBase)
		if err != nil {
			return err
		}

		// Replace externalRef with resolved content
		elem.ExternalRef = nil
		// Copy resolved patterns into element
		if resolved.Element != nil {
			// Merge the resolved element's children into this element
			elem.Optional = append(elem.Optional, resolved.Element.Optional...)
			elem.OneOrMore = append(elem.OneOrMore, resolved.Element.OneOrMore...)
			elem.ZeroOrMore = append(elem.ZeroOrMore, resolved.Element.ZeroOrMore...)
			if resolved.Element.Choice != nil && elem.Choice == nil {
				elem.Choice = resolved.Element.Choice
			}
			elem.Ref = append(elem.Ref, resolved.Element.Ref...)
			elem.Group = append(elem.Group, resolved.Element.Group...)
			elem.Interleave = append(elem.Interleave, resolved.Element.Interleave...)
			if resolved.Element.Text != nil && elem.Text == nil {
				elem.Text = resolved.Element.Text
			}
			if resolved.Element.Empty != nil && elem.Empty == nil {
				elem.Empty = resolved.Element.Empty
			}
			if resolved.Element.Data != nil && elem.Data == nil {
				elem.Data = resolved.Element.Data
			}
		}
	}
	return nil
}

// resolveExternalRefsInElementChildren recursively resolves externalRefs in element's child patterns
func resolveExternalRefsInElementChildren(elem *Element, resolver ResourceResolver, visited map[string]bool, defineNames map[string]bool) error {
	for i := range elem.Group {
		if err := resolveExternalRefsInGroup(&elem.Group[i], resolver, visited, defineNames); err != nil {
			return err
		}
	}
	for i := range elem.Interleave {
		if err := resolveExternalRefsInInterleave(&elem.Interleave[i], resolver, visited, defineNames); err != nil {
			return err
		}
	}
	for i := range elem.Optional {
		if err := resolveExternalRefsInOptional(&elem.Optional[i], resolver, visited, defineNames); err != nil {
			return err
		}
	}
	for i := range elem.OneOrMore {
		if err := resolveExternalRefsInOneOrMore(&elem.OneOrMore[i], resolver, visited, defineNames); err != nil {
			return err
		}
	}
	for i := range elem.ZeroOrMore {
		if err := resolveExternalRefsInZeroOrMore(&elem.ZeroOrMore[i], resolver, visited, defineNames); err != nil {
			return err
		}
	}
	if elem.Choice != nil {
		if err := resolveExternalRefsInChoice(elem.Choice, resolver, visited, defineNames); err != nil {
			return err
		}
	}

	return nil
}

// resolveExternalRefsInChoice recursively resolves externalRef elements in a Choice
func resolveExternalRefsInChoice(choice *Choice, resolver ResourceResolver, visited map[string]bool, defineNames map[string]bool) error {
	// Delegate to WithBase version with empty parentBase
	return resolveExternalRefsInChoiceWithBase(choice, resolver, visited, defineNames, "")
}

// resolveExternalRefsInOptional recursively resolves externalRef elements in Optional
func resolveExternalRefsInOptional(opt *Optional, resolver ResourceResolver, visited map[string]bool, defineNames map[string]bool) error {
	// Delegate to WithBase version with empty parentBase
	return resolveExternalRefsInOptionalWithBase(opt, resolver, visited, defineNames, "")
}

// resolveExternalRefsInOneOrMore recursively resolves externalRef elements in OneOrMore
func resolveExternalRefsInOneOrMore(oom *OneOrMore, resolver ResourceResolver, visited map[string]bool, defineNames map[string]bool) error {
	// Delegate to WithBase version with empty parentBase
	return resolveExternalRefsInOneOrMoreWithBase(oom, resolver, visited, defineNames, "")
}

// resolveExternalRefsInZeroOrMore recursively resolves externalRef elements in ZeroOrMore
func resolveExternalRefsInZeroOrMore(zom *ZeroOrMore, resolver ResourceResolver, visited map[string]bool, defineNames map[string]bool) error {
	// Delegate to WithBase version with empty parentBase
	return resolveExternalRefsInZeroOrMoreWithBase(zom, resolver, visited, defineNames, "")
}

// resolveExternalRefsInInterleaveWithBase is the internal implementation that tracks parentBase
func resolveExternalRefsInInterleaveWithBase(interleave *Interleave, resolver ResourceResolver, visited map[string]bool, defineNames map[string]bool, parentBase string) error {
	// Per XML spec: xml:base is resolved relative to parent's xml:base
	currentBase := parentBase
	if interleave.Base != "" {
		currentBase = resolveXMLBase(parentBase, interleave.Base)
	}

	// Resolve direct externalRef in interleave
	if err := resolveDirectExternalRefInInterleave(interleave, resolver, visited, defineNames, currentBase); err != nil {
		return err
	}

	// Recursively resolve in child patterns, propagating currentBase
	if err := resolveExternalRefsInInterleaveChildren(interleave, resolver, visited, defineNames, currentBase); err != nil {
		return err
	}

	return nil
}

// resolveDirectExternalRefInInterleave resolves direct externalRef in an interleave and merges the result
func resolveDirectExternalRefInInterleave(interleave *Interleave, resolver ResourceResolver, visited map[string]bool, defineNames map[string]bool, currentBase string) error {
	maxIterations := 100
	iterations := 0
	for interleave.ExternalRef != nil {
		iterations++
		if iterations > maxIterations {
			return fmt.Errorf("too many nested externalRef resolutions in interleave")
		}

		resolved, err := resolveExternalRefPattern(*interleave.ExternalRef, resolver, visited, defineNames, currentBase)
		if err != nil {
			return err
		}

		// Replace externalRef with resolved content
		interleave.ExternalRef = nil
		// Copy resolved content
		if len(resolved.Interleave) > 0 {
			interleave.Elements = append(interleave.Elements, resolved.Interleave[0].Elements...)
			interleave.Ref = append(interleave.Ref, resolved.Interleave[0].Ref...)
			interleave.Optional = append(interleave.Optional, resolved.Interleave[0].Optional...)
			interleave.OneOrMore = append(interleave.OneOrMore, resolved.Interleave[0].OneOrMore...)
			interleave.ZeroOrMore = append(interleave.ZeroOrMore, resolved.Interleave[0].ZeroOrMore...)
			interleave.Group = append(interleave.Group, resolved.Interleave[0].Group...)
		} else if resolved.Element != nil {
			// Per spec 4.6: transfer ns attribute from interleave if element doesn't have one
			elem := *resolved.Element
			if interleave.Ns != "" && elem.Ns == "" {
				elem.Ns = interleave.Ns
			}
			interleave.Elements = append(interleave.Elements, elem)
		}
	}
	return nil
}

// resolveExternalRefsInInterleaveChildren recursively resolves externalRefs in interleave's child patterns
func resolveExternalRefsInInterleaveChildren(interleave *Interleave, resolver ResourceResolver, visited map[string]bool, defineNames map[string]bool, currentBase string) error {
	for i := range interleave.Elements {
		if err := resolveExternalRefsInElementWithBase(&interleave.Elements[i], resolver, visited, defineNames, currentBase); err != nil {
			return err
		}
	}
	for i := range interleave.Group {
		if err := resolveExternalRefsInGroupWithBase(&interleave.Group[i], resolver, visited, defineNames, currentBase); err != nil {
			return err
		}
	}
	for i := range interleave.Optional {
		if err := resolveExternalRefsInOptionalWithBase(&interleave.Optional[i], resolver, visited, defineNames, currentBase); err != nil {
			return err
		}
	}
	for i := range interleave.OneOrMore {
		if err := resolveExternalRefsInOneOrMoreWithBase(&interleave.OneOrMore[i], resolver, visited, defineNames, currentBase); err != nil {
			return err
		}
	}
	for i := range interleave.ZeroOrMore {
		if err := resolveExternalRefsInZeroOrMoreWithBase(&interleave.ZeroOrMore[i], resolver, visited, defineNames, currentBase); err != nil {
			return err
		}
	}
	return nil
}

// resolveExternalRefsInChoiceWithBase is the internal implementation that tracks parentBase
func resolveExternalRefsInChoiceWithBase(choice *Choice, resolver ResourceResolver, visited map[string]bool, defineNames map[string]bool, parentBase string) error {
	// Per XML spec: xml:base is resolved relative to parent's xml:base
	currentBase := parentBase
	if choice.Base != "" {
		// Resolve this choice's Base relative to parentBase
		currentBase = resolveXMLBase(parentBase, choice.Base)
	}

	// Resolve direct externalRef
	maxIterations := 100
	iterations := 0
	for choice.ExternalRef != nil {
		iterations++
		if iterations > maxIterations {
			return fmt.Errorf("too many nested externalRef resolutions in choice")
		}

		resolved, err := resolveExternalRefPattern(*choice.ExternalRef, resolver, visited, defineNames, currentBase)
		if err != nil {
			return err
		}

		choice.ExternalRef = nil
		// Copy resolved content
		if resolved.Choice != nil {
			choice.Elements = append(choice.Elements, resolved.Choice.Elements...)
			choice.Refs = append(choice.Refs, resolved.Choice.Refs...)
			choice.Data = append(choice.Data, resolved.Choice.Data...)
			choice.Group = append(choice.Group, resolved.Choice.Group...)
			choice.Interleave = append(choice.Interleave, resolved.Choice.Interleave...)
		} else if resolved.Element != nil {
			// Per spec 4.6: transfer ns attribute from choice if element doesn't have one
			elem := *resolved.Element
			if choice.Ns != "" && elem.Ns == "" {
				elem.Ns = choice.Ns
			}
			choice.Elements = append(choice.Elements, elem)
		}
	}

	// Recursively resolve in child patterns, propagating currentBase
	for i := range choice.Elements {
		if err := resolveExternalRefsInElementWithBase(&choice.Elements[i], resolver, visited, defineNames, currentBase); err != nil {
			return err
		}
	}
	for i := range choice.Group {
		if err := resolveExternalRefsInGroupWithBase(&choice.Group[i], resolver, visited, defineNames, currentBase); err != nil {
			return err
		}
	}
	for i := range choice.Interleave {
		if err := resolveExternalRefsInInterleaveWithBase(&choice.Interleave[i], resolver, visited, defineNames, currentBase); err != nil {
			return err
		}
	}

	return nil
}

// resolveExternalRefsInOptionalWithBase is the internal implementation that tracks parentBase
//
//nolint:dupl
func resolveExternalRefsInOptionalWithBase(opt *Optional, resolver ResourceResolver, visited map[string]bool, defineNames map[string]bool, parentBase string) error {
	// Per XML spec: xml:base is resolved relative to parent's xml:base
	currentBase := parentBase
	if opt.Base != "" {
		// Resolve this optional's Base relative to parentBase
		currentBase = resolveXMLBase(parentBase, opt.Base)
	}

	// Resolve direct externalRef
	maxIterations := 100
	iterations := 0
	for opt.ExternalRef != nil {
		iterations++
		if iterations > maxIterations {
			return fmt.Errorf("too many nested externalRef resolutions in optional")
		}

		resolved, err := resolveExternalRefPattern(*opt.ExternalRef, resolver, visited, defineNames, currentBase)
		if err != nil {
			return err
		}

		opt.ExternalRef = nil
		// Copy resolved content
		if resolved.Element != nil {
			// Per spec 4.6: transfer ns attribute from optional if element doesn't have one
			elem := *resolved.Element
			if opt.Ns != "" && elem.Ns == "" {
				elem.Ns = opt.Ns
			}
			opt.Elements = append(opt.Elements, elem)
		}
	}

	// Recursively resolve in child patterns, propagating currentBase
	for i := range opt.Elements {
		if err := resolveExternalRefsInElementWithBase(&opt.Elements[i], resolver, visited, defineNames, currentBase); err != nil {
			return err
		}
	}

	return nil
}

// resolveExternalRefsInOneOrMoreWithBase is the internal implementation that tracks parentBase
//
//nolint:dupl
func resolveExternalRefsInOneOrMoreWithBase(oom *OneOrMore, resolver ResourceResolver, visited map[string]bool, defineNames map[string]bool, parentBase string) error {
	// Per XML spec: xml:base is resolved relative to parent's xml:base
	currentBase := parentBase
	if oom.Base != "" {
		// Resolve this oneOrMore's Base relative to parentBase
		currentBase = resolveXMLBase(parentBase, oom.Base)
	}

	// Resolve direct externalRef
	maxIterations := 100
	iterations := 0
	for oom.ExternalRef != nil {
		iterations++
		if iterations > maxIterations {
			return fmt.Errorf("too many nested externalRef resolutions in oneOrMore")
		}

		resolved, err := resolveExternalRefPattern(*oom.ExternalRef, resolver, visited, defineNames, currentBase)
		if err != nil {
			return err
		}

		oom.ExternalRef = nil
		// Copy resolved content
		if resolved.Element != nil {
			// Per spec 4.6: transfer ns attribute from oneOrMore if element doesn't have one
			elem := *resolved.Element
			if oom.Ns != "" && elem.Ns == "" {
				elem.Ns = oom.Ns
			}
			oom.Element = append(oom.Element, elem)
		}
	}

	// Recursively resolve in child patterns, propagating currentBase
	for i := range oom.Element {
		if err := resolveExternalRefsInElementWithBase(&oom.Element[i], resolver, visited, defineNames, currentBase); err != nil {
			return err
		}
	}

	return nil
}

// resolveExternalRefsInZeroOrMoreWithBase is the internal implementation that tracks parentBase
//
//nolint:dupl
func resolveExternalRefsInZeroOrMoreWithBase(zom *ZeroOrMore, resolver ResourceResolver, visited map[string]bool, defineNames map[string]bool, parentBase string) error {
	// Per XML spec: xml:base is resolved relative to parent's xml:base
	currentBase := parentBase
	if zom.Base != "" {
		// Resolve this zeroOrMore's Base relative to parentBase
		currentBase = resolveXMLBase(parentBase, zom.Base)
	}

	// Resolve direct externalRef
	maxIterations := 100
	iterations := 0
	for zom.ExternalRef != nil {
		iterations++
		if iterations > maxIterations {
			return fmt.Errorf("too many nested externalRef resolutions in zeroOrMore")
		}

		resolved, err := resolveExternalRefPattern(*zom.ExternalRef, resolver, visited, defineNames, currentBase)
		if err != nil {
			return err
		}

		zom.ExternalRef = nil
		// Copy resolved content
		if resolved.Element != nil {
			// Per spec 4.6: transfer ns attribute from zeroOrMore if element doesn't have one
			elem := *resolved.Element
			if zom.Ns != "" && elem.Ns == "" {
				elem.Ns = zom.Ns
			}
			zom.Element = append(zom.Element, elem)
		}
	}

	// Recursively resolve in elements, propagating currentBase
	for i := range zom.Element {
		if err := resolveExternalRefsInElementWithBase(&zom.Element[i], resolver, visited, defineNames, currentBase); err != nil {
			return err
		}
	}

	return nil
}

// mergeIncludeDefines merges defines from an included grammar with override handling
// Per RELAX NG spec section 4.5: handles define overrides and namespace inheritance
func mergeIncludeDefines(grammar *Grammar, includedGrammar *Grammar, include Include, overrideNames map[string]bool, defineNames map[string]bool) {
	for _, def := range includedGrammar.Defines {
		if overrideNames[def.Name] {
			// This define has an override in the include element
			mergeOverrideDefine(grammar, include, &def, defineNames)
		} else if !defineNames[def.Name] {
			// No override - just add it if we haven't seen it before
			mergeIncludedDefine(grammar, include, &def, defineNames)
		}
	}
}

// mergeOverrideDefine handles a define that has an override
func mergeOverrideDefine(grammar *Grammar, include Include, def *Define, defineNames map[string]bool) {
	for _, overrideDef := range include.Defines {
		if overrideDef.Name == def.Name {
			// Only add if we haven't already processed this include override
			if !defineNames[def.Name] {
				defineNames[def.Name] = true
				// Per spec 4.5: Apply ns attribute from include to the override define
				if include.Ns != "" {
					applyNamespaceToDefine(&overrideDef, include.Ns)
				}
				// The override replaces the included definition
				grammar.Defines = append(grammar.Defines, overrideDef)
			}
			break
		}
	}
}

// mergeIncludedDefine handles a define without an override
func mergeIncludedDefine(grammar *Grammar, include Include, def *Define, defineNames map[string]bool) {
	defineNames[def.Name] = true
	// Per RELAX NG spec section 4.5: Apply ns attribute from include to the included define
	if include.Ns != "" {
		applyNamespaceToDefine(def, include.Ns)
	}
	grammar.Defines = append(grammar.Defines, *def)
}

// mergeIncludeStartElement merges the start element from an included grammar
// Per RELAX NG spec section 4.5: handle start element overrides and namespace inheritance
func mergeIncludeStartElement(grammar *Grammar, includedGrammar *Grammar, include Include) {
	if len(include.Start) > 0 {
		// Include has override start element
		applyIncludeStartOverride(grammar, include)
		return
	}

	// No override - check if we should take from included grammar
	if !startElementExists(grammar.Start) && startElementExists(includedGrammar.Start) {
		applyIncludedStart(grammar, includedGrammar, include)
	}
}

// applyIncludeStartOverride applies the include's start element override
func applyIncludeStartOverride(grammar *Grammar, include Include) {
	startToAdd := include.Start[0]
	// Per spec 4.5: Apply ns attribute from include to the start element
	if include.Ns != "" && startToAdd.Element != nil {
		applyNamespaceToElement(startToAdd.Element, include.Ns)
	}
	grammar.Start = startToAdd
}

// applyIncludedStart applies the included grammar's start element
func applyIncludedStart(grammar *Grammar, includedGrammar *Grammar, include Include) {
	// Per spec 4.5: Apply ns attribute from include to the included start element
	if include.Ns != "" && includedGrammar.Start.Element != nil {
		applyNamespaceToElement(includedGrammar.Start.Element, include.Ns)
	}
	grammar.Start = includedGrammar.Start
}

// startElementExists checks if a start element has content
func startElementExists(start Start) bool {
	return start.Element != nil || start.Ref != nil || len(bytes.TrimSpace(start.RawContent)) > 0
}

// processIncludeWithResolver handles include using a ResourceResolver
func processIncludeWithResolver(grammar *Grammar, include Include, resolver ResourceResolver, visited map[string]bool, defineNames map[string]bool) error {
	// Per spec section 4.5: href must not include fragment identifier for XML resources
	// RFC 3023 does not define fragment identifiers for application/xml or text/xml
	if strings.Contains(include.Href, "#") {
		return fmt.Errorf("href attribute must not contain fragment identifier: %s", include.Href)
	}

	// Resolve href with xml:base if present
	resolvedHref := resolveXMLBase(include.Base, include.Href)

	// Load the included grammar (don't validate refs yet - wait until all includes are merged)
	// Per RELAX NG spec section 4.5: included file MUST have <grammar> as root
	includedGrammar, err := parseSchemaWithResolverInternal(resolvedHref, resolver, visited, defineNames, false, true)
	if err != nil {
		return fmt.Errorf("error parsing included file %s: %w", include.Href, err)
	}

	// Build map of override define names for quick lookup
	overrideNames := make(map[string]bool)
	for _, def := range include.Defines {
		overrideNames[def.Name] = true
	}

	// Per spec section 4.7: Validate override constraints
	// If include has a start element, the included grammar MUST also have a start element
	if len(include.Start) > 0 {
		includedHasStart := includedGrammar.Start.Element != nil || includedGrammar.Start.Ref != nil || includedGrammar.Start.ParentRef != nil || len(bytes.TrimSpace(includedGrammar.Start.RawContent)) > 0
		if !includedHasStart {
			return fmt.Errorf("include element has start override but included grammar has no start element")
		}
	}

	// If include has a define with name X, the included grammar MUST also have a define with name X
	for _, overrideDef := range include.Defines {
		found := false
		for _, includedDef := range includedGrammar.Defines {
			if includedDef.Name == overrideDef.Name {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("include element has define override name='%s' but included grammar has no define with that name", overrideDef.Name)
		}
	}

	// Merge defines from included grammar, applying overrides if present
	mergeIncludeDefines(grammar, includedGrammar, include, overrideNames, defineNames)

	// Handle start element resolution
	mergeIncludeStartElement(grammar, includedGrammar, include)

	return nil
}

// processExternalRefWithResolver handles externalRef using a ResourceResolver
func processExternalRefWithResolver(grammar *Grammar, extRef ExternalRef, resolver ResourceResolver, visited map[string]bool, defineNames map[string]bool) error {
	// Per spec section 4.5: href must not include fragment identifier for XML resources
	// RFC 3023 does not define fragment identifiers for application/xml or text/xml
	if strings.Contains(extRef.Href, "#") {
		return fmt.Errorf("href attribute must not contain fragment identifier: %s", extRef.Href)
	}

	// Resolve href with xml:base if present
	resolvedHref := resolveXMLBase(extRef.Base, extRef.Href)

	// Load the external grammar (don't validate refs yet - wait until all externalRefs are merged)
	// ExternalRef can reference patterns directly (per spec), so requiresGrammar is false
	externalGrammar, err := parseSchemaWithResolverInternal(resolvedHref, resolver, visited, defineNames, false, false)
	if err != nil {
		return fmt.Errorf("error parsing external ref %s: %w", extRef.Href, err)
	}

	// Merge defines from external grammar
	for _, def := range externalGrammar.Defines {
		if !defineNames[def.Name] {
			defineNames[def.Name] = true
			grammar.Defines = append(grammar.Defines, def)
		}
	}

	return nil
}

// normalizeNames normalizes whitespace in name attributes throughout the grammar
// Per RELAX NG spec, name attributes should have leading/trailing whitespace trimmed
func (g *Grammar) normalizeNames() {
	// Normalize start
	if g.Start.Ref != nil {
		g.Start.Ref.Name = strings.TrimSpace(g.Start.Ref.Name)
	}
	if g.Start.ParentRef != nil {
		g.Start.ParentRef.Name = strings.TrimSpace(g.Start.ParentRef.Name)
	}

	// Normalize defines
	for i := range g.Defines {
		def := &g.Defines[i]
		def.Name = strings.TrimSpace(def.Name)
		def.Combine = strings.TrimSpace(def.Combine)

		if def.Ref != nil {
			def.Ref.Name = strings.TrimSpace(def.Ref.Name)
		}
		if def.ParentRef != nil {
			def.ParentRef.Name = strings.TrimSpace(def.ParentRef.Name)
		}

		// Normalize element refs recursively
		if def.FirstElement() != nil {
			normalizeElementNames(def.FirstElement())
		}
	}
}

// validateNoRepeatingAttributes checks that oneOrMore cannot contain patterns with multiple attributes
// Per RELAX NG spec: oneOrMore can contain single attributes (especially with infinite name classes),
// but cannot contain patterns that would allow multiple attributes in the same occurrence (like
// groups or interleaves with multiple attributes)
func validateNoRepeatingAttributes(oneOrMore *OneOrMore) error {
	if oneOrMore == nil {
		return nil
	}

	// Direct attributes in oneOrMore are allowed (especially with infinite name classes per spec 7.3)
	// The restriction is on patterns that contain MULTIPLE attributes

	// Check for groups containing multiple attributes or nested patterns with multiple attributes
	for _, group := range oneOrMore.Group {
		if err := checkGroupForRepeatingAttributes(&group); err != nil {
			return fmt.Errorf("oneOrMore: %w", err)
		}
	}

	// Check for interleaves containing multiple attributes
	for _, interleave := range oneOrMore.Interleave {
		if len(interleave.Attributes) > 1 {
			return fmt.Errorf("oneOrMore cannot contain an interleave with multiple attributes (spec violation)")
		}
		// Check nested patterns in interleave
		if err := checkInterleavedPatternsForAttributes(&interleave); err != nil {
			return fmt.Errorf("oneOrMore: %w", err)
		}
	}

	// Check for direct choices with multiple attributes
	if oneOrMore.Choice != nil {
		if err := checkChoiceForRepeatingAttributes(oneOrMore.Choice); err != nil {
			return fmt.Errorf("oneOrMore: %w", err)
		}
		// Also check if the choice contains interleaves with problematic choices
		for _, interleave := range oneOrMore.Choice.Interleave {
			for _, choice := range interleave.Choice {
				if len(choice.Attributes) > 1 {
					return fmt.Errorf("oneOrMore cannot contain choices with multiple attributes in an interleave")
				}
			}
		}
	}

	return nil
}

// checkGroupForRepeatingAttributes recursively checks for repeating attributes in a group
func checkGroupForRepeatingAttributes(group *Group) error {
	if group == nil {
		return nil
	}

	// Direct multiple attributes in group
	if len(group.Attributes) > 1 {
		return fmt.Errorf("group cannot contain multiple attributes")
	}

	// Check interleaves within group
	for _, interleave := range group.Interleave {
		if len(interleave.Attributes) > 1 {
			return fmt.Errorf("group cannot contain an interleave with multiple attributes")
		}
		if err := checkInterleavedPatternsForAttributes(&interleave); err != nil {
			return err
		}
	}

	// Check nested choices within group for repeating attributes
	for _, choice := range group.Choice {
		if err := checkChoiceForRepeatingAttributes(&choice); err != nil {
			return err
		}
	}

	// Recursively check nested groups
	for _, nestedGroup := range group.Group {
		if err := checkGroupForRepeatingAttributes(&nestedGroup); err != nil {
			return err
		}
	}

	return nil
}

// checkInterleavedPatternsForAttributes checks for repeated attributes in nested patterns within an interleave
func checkInterleavedPatternsForAttributes(interleave *Interleave) error {
	if interleave == nil {
		return nil
	}

	// Check nested groups in interleave
	for _, group := range interleave.Group {
		// Groups in interleaves can have nested choices with attributes
		for _, choice := range group.Choice {
			if len(choice.Attributes) > 1 {
				return fmt.Errorf("interleave cannot contain nested patterns with multiple attributes")
			}
			// Recursively check further nested structures
			if err := checkChoiceForRepeatingAttributes(&choice); err != nil {
				return err
			}
		}
		// Check for nested groups within groups in interleave
		if err := checkGroupForRepeatingAttributes(&group); err != nil {
			return err
		}
	}

	return nil
}

// checkChoiceForRepeatingAttributes recursively checks choice for repeating attribute patterns
func checkChoiceForRepeatingAttributes(choice *Choice) error {
	if choice == nil {
		return nil
	}

	// Multiple direct attributes
	if len(choice.Attributes) > 1 {
		return fmt.Errorf("choice cannot contain multiple attributes")
	}

	// Check nested groups in choice
	for _, group := range choice.Group {
		if err := checkGroupForRepeatingAttributes(&group); err != nil {
			return err
		}
	}

	// Check if choice contains an interleave with repeating attribute patterns
	// This is important for test 294: choice > interleave > group > choice with attributes
	for _, group := range choice.Group {
		for _, interleave := range group.Interleave {
			// Check choices within the interleave's groups for multiple attributes
			for _, iGroup := range interleave.Group {
				for _, iChoice := range iGroup.Choice {
					if len(iChoice.Attributes) > 1 {
						return fmt.Errorf("choice cannot contain interleaved choices with multiple attributes")
					}
				}
			}
		}
	}

	return nil
}

// validateNoRepeatingAttributesZero checks that zeroOrMore cannot contain patterns with multiple attributes
func validateNoRepeatingAttributesZero(zeroOrMore *ZeroOrMore) error {
	if zeroOrMore == nil {
		return nil
	}

	// Direct attributes in zeroOrMore are allowed
	// The restriction is on patterns that contain MULTIPLE attributes

	// Check for groups containing multiple attributes
	for _, group := range zeroOrMore.Group {
		if len(group.Attributes) > 1 {
			return fmt.Errorf("zeroOrMore cannot contain a group with multiple attributes (spec violation)")
		}
		// Also check interleaves within the group
		for _, interleave := range group.Interleave {
			if len(interleave.Attributes) > 1 {
				return fmt.Errorf("zeroOrMore cannot contain a group with an interleave containing multiple attributes (spec violation)")
			}
		}
		// Check choices within the group
		for _, choice := range group.Choice {
			if len(choice.Attributes) > 1 {
				return fmt.Errorf("zeroOrMore cannot contain a group with a choice containing multiple attributes (spec violation)")
			}
		}
	}

	// Check for interleaves containing multiple attributes
	for _, interleave := range zeroOrMore.Interleave {
		if len(interleave.Attributes) > 1 {
			return fmt.Errorf("zeroOrMore cannot contain an interleave with multiple attributes (spec violation)")
		}
	}

	// Check for choices containing multiple attributes
	if zeroOrMore.Choice != nil && len(zeroOrMore.Choice.Attributes) > 1 {
		return fmt.Errorf("zeroOrMore cannot contain a choice with multiple attributes (spec violation)")
	}

	return nil
}

// validateGroupsInOneOrMore validates groups within oneOrMore by checking RawContent
func validateGroupsInOneOrMore(oneOrMore *OneOrMore) error {
	if oneOrMore == nil {
		return nil
	}

	// Check RawContent for group with multiple attributes
	if err := validateRepeatingAttributesInContent(oneOrMore.RawContent, "group"); err != nil {
		return fmt.Errorf("oneOrMore: %w", err)
	}

	return nil
}

// validateInterleavesInOneOrMore validates interleaves within oneOrMore by checking RawContent
func validateInterleavesInOneOrMore(oneOrMore *OneOrMore) error {
	if oneOrMore == nil {
		return nil
	}

	// Check RawContent for interleave with multiple attributes
	if err := validateRepeatingAttributesInContent(oneOrMore.RawContent, elemNameInterleave); err != nil {
		return fmt.Errorf("oneOrMore: %w", err)
	}

	return nil
}

// validateChoicesInOneOrMore validates choices within oneOrMore for attribute conflicts
func validateChoicesInOneOrMore(oneOrMore *OneOrMore) error {
	if oneOrMore == nil {
		return nil
	}

	// Check RawContent for choice with multiple attributes
	if err := validateRepeatingAttributesInContent(oneOrMore.RawContent, elemNameChoice); err != nil {
		return fmt.Errorf("oneOrMore: %w", err)
	}

	return nil
}

// validateGroupsInZeroOrMore validates groups within zeroOrMore by checking RawContent
func validateGroupsInZeroOrMore(zeroOrMore *ZeroOrMore) error {
	if zeroOrMore == nil {
		return nil
	}

	// Check RawContent for group with multiple attributes
	if err := validateRepeatingAttributesInContent(zeroOrMore.RawContent, "group"); err != nil {
		return fmt.Errorf("zeroOrMore: %w", err)
	}

	return nil
}

// validateInterleavesInZeroOrMore validates interleaves within zeroOrMore by checking RawContent
func validateInterleavesInZeroOrMore(zeroOrMore *ZeroOrMore) error {
	if zeroOrMore == nil {
		return nil
	}

	// Check RawContent for interleave with multiple attributes
	if err := validateRepeatingAttributesInContent(zeroOrMore.RawContent, elemNameInterleave); err != nil {
		return fmt.Errorf("zeroOrMore: %w", err)
	}

	return nil
}

// validateChoicesInZeroOrMore validates choices within zeroOrMore by checking RawContent
func validateChoicesInZeroOrMore(zeroOrMore *ZeroOrMore) error {
	if zeroOrMore == nil {
		return nil
	}

	// Check RawContent for choice with multiple attributes
	if err := validateRepeatingAttributesInContent(zeroOrMore.RawContent, elemNameChoice); err != nil {
		return fmt.Errorf("zeroOrMore: %w", err)
	}

	return nil
}

// validateRepeatingAttributePattern validates a pattern for repeated attributes based on type
func validateRepeatingAttributePattern(decoder *xml.Decoder, startElem *xml.StartElement, patternType string) error {
	switch patternType {
	case "group":
		var group Group
		if err := decoder.DecodeElement(&group, startElem); err != nil {
			return err
		}
		return validateGroupAttributes(group)
	case elemNameInterleave:
		var interleave Interleave
		if err := decoder.DecodeElement(&interleave, startElem); err != nil {
			return err
		}
		return validateInterleaveAttributes(interleave)
	case "choice":
		var choice Choice
		if err := decoder.DecodeElement(&choice, startElem); err != nil {
			return err
		}
		return validateChoiceAttributes(choice)
	}
	return nil
}

// validateGroupAttributes checks for multiple attributes in a group
func validateGroupAttributes(group Group) error {
	attrCount := 0
	for _, elem := range group.Elements {
		attrCount += len(elem.Attributes)
	}
	if attrCount > 1 {
		return fmt.Errorf("cannot contain group with multiple attributes (spec section 4.7)")
	}
	// Also check nested patterns
	for _, nestedChoice := range group.Choice {
		if len(nestedChoice.Elements) > 0 {
			for _, elem := range nestedChoice.Elements {
				if len(elem.Attributes) > 0 {
					return fmt.Errorf("cannot contain group with choice having attributes (spec section 4.7)")
				}
			}
		}
	}
	return nil
}

// validateInterleaveAttributes checks for multiple attributes in an interleave
func validateInterleaveAttributes(interleave Interleave) error {
	attrCount := 0
	for _, elem := range interleave.Elements {
		attrCount += len(elem.Attributes)
	}
	if attrCount > 1 {
		return fmt.Errorf("cannot contain interleave with multiple attributes (spec section 4.7)")
	}
	return nil
}

// validateChoiceAttributes checks for multiple attribute-bearing branches in a choice
func validateChoiceAttributes(choice Choice) error {
	attrBranches := 0
	for _, elem := range choice.Elements {
		if len(elem.Attributes) > 0 {
			attrBranches++
		}
	}
	if attrBranches > 1 {
		return fmt.Errorf("cannot contain choice with multiple attribute patterns (spec section 4.7)")
	}
	return nil
}

// validateRepeatingAttributesInContent checks for patterns with multiple attributes in repeating contexts
func validateRepeatingAttributesInContent(content []byte, patternType string) error {
	if len(bytes.TrimSpace(content)) == 0 {
		return nil
	}

	decoder := xml.NewDecoder(bytes.NewReader(content))
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if startElem, ok := tok.(xml.StartElement); ok {
			if startElem.Name.Local == patternType {
				if err := validateRepeatingAttributePattern(decoder, &startElem, patternType); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// normalizeElementNames normalizes name attributes in element patterns recursively
func normalizeElementNames(elem *Element) {
	if elem == nil {
		return
	}

	elem.Name = strings.TrimSpace(elem.Name)

	// Normalize refs in slices
	for i := range elem.Ref {
		elem.Ref[i].Name = strings.TrimSpace(elem.Ref[i].Name)
	}
	for i := range elem.ParentRef {
		elem.ParentRef[i].Name = strings.TrimSpace(elem.ParentRef[i].Name)
	}

	// Recursively normalize nested elements
	for i := range elem.Elements {
		normalizeElementNames(&elem.Elements[i])
	}

	// Normalize choice elements
	if elem.Choice != nil {
		for i := range elem.Choice.Elements {
			normalizeElementNames(&elem.Choice.Elements[i])
		}
	}

	// Normalize group elements
	for i := range elem.Group {
		for j := range elem.Group[i].Elements {
			normalizeElementNames(&elem.Group[i].Elements[j])
		}
	}
}

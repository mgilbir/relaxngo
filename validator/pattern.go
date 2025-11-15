package validator

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"github.com/mgilbir/relaxngo/rng"
)

// Pattern represents an ordered content pattern in RELAX NG
// This AST preserves order and handles all pattern types
type Pattern interface {
	Kind() PatternKind
}

// PatternKind represents the type of a pattern node in RELAX NG.
type PatternKind string

const (
	// ElementK represents an element pattern.
	ElementK PatternKind = "element"
	// GroupK represents a sequence of patterns.
	GroupK PatternKind = "group"
	// ChoiceK represents a choice between patterns.
	ChoiceK PatternKind = "choice"
	// InterleaveK represents patterns that can match in any order.
	InterleaveK PatternKind = "interleave"
	// OptionalK represents an optional pattern (zero or one occurrence).
	OptionalK PatternKind = "optional"
	// OneOrMoreK represents one or more occurrences of a pattern.
	OneOrMoreK PatternKind = "oneOrMore"
	// ZeroOrMoreK represents zero or more occurrences of a pattern.
	ZeroOrMoreK PatternKind = "zeroOrMore"
	// RefK represents a reference to a named pattern definition.
	RefK PatternKind = "ref"
	// TextK represents text content.
	TextK PatternKind = "text"
	// EmptyK represents empty content.
	EmptyK PatternKind = "empty"
	// NotAllowedK represents content that is not allowed.
	NotAllowedK PatternKind = "notAllowed"
	// DataK represents typed data content.
	DataK PatternKind = "data"
	// ListK represents a list pattern.
	ListK PatternKind = "list"
	// MixedK represents mixed content.
	MixedK PatternKind = "mixed"
	// AttributeK represents an attribute pattern.
	AttributeK PatternKind = "attribute"
	// AnyContentK represents content that accepts any value.
	AnyContentK PatternKind = "anyContent"
	// ValueK represents a literal value pattern.
	ValueK PatternKind = "value"
	// XML Schema data type constants
	dataTypeString           = "string"
	dataTypeNormalizedString = "normalizedString"
	dataTypeToken            = "token"
	// XML Schema facet constants
	facetMinInclusive = "minInclusive"
	facetMaxInclusive = "maxInclusive"
	facetMinExclusive = "minExclusive"
	facetMaxExclusive = "maxExclusive"
)

// ElementPat represents an element pattern
type ElementPat struct {
	Name     string
	Ns       string
	AnyName  *rng.AnyName
	NsName   *rng.NsName
	Children []Pattern // Content patterns in order
}

// Kind returns the pattern kind for ElementPat.
func (e *ElementPat) Kind() PatternKind { return ElementK }

// GroupPat represents a sequence (group) of patterns
type GroupPat struct {
	Children []Pattern // Must match in order
}

// Kind returns the pattern kind for GroupPat.
func (g *GroupPat) Kind() PatternKind { return GroupK }

// ChoicePat represents alternatives
type ChoicePat struct {
	Alternatives []Pattern // Try each until one succeeds
}

// Kind returns the pattern kind for ChoicePat.
func (c *ChoicePat) Kind() PatternKind { return ChoiceK }

// InterleavePat represents any-order patterns
type InterleavePat struct {
	Children []Pattern // Can match in any order
}

// Kind returns the pattern kind for InterleavePat.
func (i *InterleavePat) Kind() PatternKind { return InterleaveK }

// OptionalPat represents zero or one occurrence
type OptionalPat struct {
	Child Pattern
}

// Kind returns the pattern kind for OptionalPat.
func (o *OptionalPat) Kind() PatternKind { return OptionalK }

// OneOrMorePat represents one or more occurrences
type OneOrMorePat struct {
	Child Pattern
}

// Kind returns the pattern kind for OneOrMorePat.
func (o *OneOrMorePat) Kind() PatternKind { return OneOrMoreK }

// ZeroOrMorePat represents zero or more occurrences
type ZeroOrMorePat struct {
	Child Pattern
}

// Kind returns the pattern kind for ZeroOrMorePat.
func (z *ZeroOrMorePat) Kind() PatternKind { return ZeroOrMoreK }

// RefPat represents a reference to a define
type RefPat struct {
	Name string
}

// Kind returns the pattern kind for RefPat.
func (r *RefPat) Kind() PatternKind { return RefK }

// TextPat represents text content
type TextPat struct{}

// Kind returns the pattern kind for TextPat.
func (t *TextPat) Kind() PatternKind { return TextK }

// EmptyPat represents empty content
type EmptyPat struct{}

// Kind returns the pattern kind for EmptyPat.
func (e *EmptyPat) Kind() PatternKind { return EmptyK }

// NotAllowedPat represents content that is not allowed
type NotAllowedPat struct{}

// Kind returns the pattern kind for NotAllowedPat.
func (n *NotAllowedPat) Kind() PatternKind { return NotAllowedK }

// DataPat represents typed data
type DataPat struct {
	Type   string
	Params []rng.Param
	Except *rng.DataExcept
}

// Kind returns the pattern kind for DataPat.
func (d *DataPat) Kind() PatternKind { return DataK }

// ValuePat represents a literal value pattern with type handling
type ValuePat struct {
	Values []rng.Value // List of allowed values
}

// Kind returns the pattern kind for ValuePat.
func (v *ValuePat) Kind() PatternKind { return ValueK }

// ListPat represents list pattern
type ListPat struct {
	Child Pattern
}

// Kind returns the pattern kind for ListPat.
func (l *ListPat) Kind() PatternKind { return ListK }

// MixedPat represents mixed content
type MixedPat struct {
	Child Pattern
}

// Kind returns the pattern kind for MixedPat.
func (m *MixedPat) Kind() PatternKind { return MixedK }

// AttributePat represents an attribute pattern
type AttributePat struct {
	Name    string
	Ns      string
	AnyName *rng.AnyName
	NsName  *rng.NsName
	Pattern Pattern // Value pattern (text, data, choice, etc.)
}

// Kind returns the pattern kind for AttributePat.
func (a *AttributePat) Kind() PatternKind { return AttributeK }

// AnyContentPat represents a pattern that accepts any content (no restrictions)
// This is used when an element has no explicit content pattern
type AnyContentPat struct{}

// Kind returns the pattern kind for AnyContentPat.
func (ac *AnyContentPat) Kind() PatternKind { return AnyContentK }

// buildPatternFromChoiceWithNameElements builds a pattern from a choice with name elements
func buildPatternFromChoiceWithNameElements(elem *rng.Element, defines map[string]*rng.Define) (Pattern, error) {
	// Special case: choice contains only NameElements (name class)
	// Build content pattern from element (not from choice)
	contentPattern, err := buildElementContentPattern(elem, defines)
	if err != nil {
		return nil, err
	}

	// Create ElementPats for each allowed name with the same content
	alternatives := make([]Pattern, 0, len(elem.Choice.NameElements))
	for _, nameElem := range elem.Choice.NameElements {
		elemPat := &ElementPat{
			Name: nameElem.Value,
			Ns:   nameElem.Ns,
		}
		if contentPattern != nil {
			elemPat.Children = []Pattern{contentPattern}
		}
		alternatives = append(alternatives, elemPat)
	}

	if len(alternatives) > 0 {
		return &ChoicePat{Alternatives: alternatives}, nil
	}
	return nil, nil
}

// buildElementContentPattern extracts the content pattern from an element
func buildElementContentPattern(elem *rng.Element, defines map[string]*rng.Define) (Pattern, error) {
	// Check if element has any content patterns
	if elem.Empty != nil {
		return &EmptyPat{}, nil
	}
	if elem.Text != nil {
		return &TextPat{}, nil
	}
	if elem.Data != nil {
		return &DataPat{
			Type:   elem.Data.Type,
			Params: elem.Data.Params,
			Except: elem.Data.Except,
		}, nil
	}
	if len(elem.Group) > 0 {
		groupPatterns, err := buildPatternsFromGroups(elem.Group, defines)
		if err != nil {
			return nil, err
		}
		if len(groupPatterns) == 1 {
			return groupPatterns[0], nil
		}
		if len(groupPatterns) > 1 {
			return &GroupPat{Children: groupPatterns}, nil
		}
	}
	if len(elem.OneOrMore) > 0 || len(elem.ZeroOrMore) > 0 || len(elem.Optional) > 0 {
		// Handle other content patterns
		return &AnyContentPat{}, nil
	}
	return &AnyContentPat{}, nil
}

// buildMixedElementPattern builds a pattern from a mixed content element
func buildMixedElementPattern(elem *rng.Element, defines map[string]*rng.Define) (Pattern, error) {
	// Build patterns from structured elements first
	patterns := make([]Pattern, 0, len(elem.Mixed.Elements)+len(elem.Mixed.Ref))
	for i := range elem.Mixed.Elements {
		pattern, err := BuildPatternFromElement(&elem.Mixed.Elements[i], defines)
		if err != nil {
			return nil, err
		}
		patterns = append(patterns, pattern)
	}
	// Build patterns from refs
	for _, ref := range elem.Mixed.Ref {
		patterns = append(patterns, &RefPat{Name: ref.Name})
	}

	// If no structured content, use RawContent
	if len(patterns) == 0 {
		rawPatterns, err := buildPatternsFromRawContent(elem.Mixed.RawContent, defines)
		if err != nil {
			return nil, err
		}
		patterns = rawPatterns
	}

	var mixedChild Pattern
	switch {
	case len(patterns) == 1:
		mixedChild = &MixedPat{Child: patterns[0]}
	case len(patterns) > 1:
		mixedChild = &MixedPat{Child: &GroupPat{Children: patterns}}
	default:
		mixedChild = &MixedPat{Child: &EmptyPat{}}
	}
	if elem.Name != "" {
		return &ElementPat{
			Name:     elem.Name,
			Ns:       elem.Ns,
			Children: []Pattern{mixedChild},
		}, nil
	}
	return mixedChild, nil
}

// wrapInElementIfNamed wraps a pattern in an ElementPat if elementName is non-empty.
// This helper reduces nesting complexity in BuildPatternFromElement.
func wrapInElementIfNamed(pattern Pattern, elementName, elementNs string) Pattern {
	if elementName != "" {
		return &ElementPat{
			Name:     elementName,
			Ns:       elementNs,
			Children: []Pattern{pattern},
		}
	}
	return pattern
}

// buildListChildPattern builds the child pattern for a list element
func buildListChildPattern(list *rng.List, defines map[string]*rng.Define) (Pattern, error) {
	// Try parsing from RawContent first (preserves structure and order)
	if len(list.RawContent) > 0 {
		patterns, err := buildPatternsFromRawContent(list.RawContent, defines)
		if err != nil {
			return nil, err
		}
		return consolidatePatternsToOne(patterns), nil
	}

	// Handle Data type in list
	if list.Data != nil {
		return &DataPat{
			Type:   list.Data.Type,
			Params: list.Data.Params,
			Except: list.Data.Except,
		}, nil
	}

	// Handle OneOrMore in list
	if list.OneOrMore != nil {
		return &OneOrMorePat{Child: &TextPat{}}, nil
	}

	// Handle list with multiple values
	if len(list.Values) > 0 {
		var patterns []Pattern
		for _, val := range list.Values {
			patterns = append(patterns, &ValuePat{Values: []rng.Value{val}})
		}
		return consolidatePatternsToOne(patterns), nil
	}

	// Default to TextPat
	return &TextPat{}, nil
}

// consolidatePatternsToOne combines multiple patterns into a single pattern
// Single patterns are returned as-is, multiple patterns become a GroupPat, empty returns TextPat
func consolidatePatternsToOne(patterns []Pattern) Pattern {
	if len(patterns) == 1 {
		return patterns[0]
	}
	if len(patterns) > 1 {
		return &GroupPat{Children: patterns}
	}
	return &TextPat{}
}

// wrapPatternIfNamed wraps a content pattern in an ElementPat if the element has a name
func wrapPatternIfNamed(pat Pattern, elemName string, elemNs string) Pattern {
	if elemName != "" {
		return &ElementPat{
			Name:     elemName,
			Ns:       elemNs,
			Children: []Pattern{pat},
		}
	}
	return pat
}

// buildSimpleContentPattern handles simple content patterns (empty, notAllowed, text, data, value, list)
func buildSimpleContentPattern(elem *rng.Element, defines map[string]*rng.Define) (Pattern, error) {
	switch {
	case elem.Empty != nil:
		return wrapPatternIfNamed(&EmptyPat{}, elem.Name, elem.Ns), nil
	case elem.NotAllowed != nil:
		return wrapPatternIfNamed(&NotAllowedPat{}, elem.Name, elem.Ns), nil
	case elem.Text != nil:
		return wrapPatternIfNamed(&TextPat{}, elem.Name, elem.Ns), nil
	case elem.Data != nil:
		dataPat := &DataPat{
			Type:   elem.Data.Type,
			Params: elem.Data.Params,
			Except: elem.Data.Except,
		}
		return wrapPatternIfNamed(dataPat, elem.Name, elem.Ns), nil
	case len(elem.Values) > 0:
		valuePat := &ValuePat{Values: elem.Values}
		return wrapPatternIfNamed(valuePat, elem.Name, elem.Ns), nil
	case elem.List != nil:
		childPat, err := buildListChildPattern(elem.List, defines)
		if err != nil {
			return nil, err
		}
		listPat := &ListPat{Child: childPat}
		return wrapPatternIfNamed(listPat, elem.Name, elem.Ns), nil
	}
	return nil, nil
}

// combinePatterns combines multiple patterns into a single pattern
func combinePatterns(patterns []Pattern, emptyDefault Pattern) Pattern {
	switch {
	case len(patterns) == 1:
		return patterns[0]
	case len(patterns) > 1:
		return &GroupPat{Children: patterns}
	default:
		return emptyDefault
	}
}

// buildGroupLikePattern handles group-like content (elements, choice, group, interleave)
//
//nolint:funlen // Pattern building requires comprehensive case analysis for all group-like elements
func buildGroupLikePattern(elem *rng.Element, defines map[string]*rng.Define) (Pattern, error) {
	// Check for direct element children (child elements in sequence)
	// When an element has both Elements and other patterns (Optional, Group, etc),
	// they form an implicit sequence (group)
	//nolint:nestif // Comprehensive pattern type checking requires multiple nested conditions
	if len(elem.Elements) > 0 || len(elem.Group) > 0 {
		var patterns []Pattern

		// Add direct element children
		for i := range elem.Elements {
			pat, err := BuildPatternFromElement(&elem.Elements[i], defines)
			if err != nil {
				return nil, err
			}
			patterns = append(patterns, pat)
		}

		// Add group patterns
		if len(elem.Group) > 0 {
			groupPatterns, err := buildPatternsFromGroups(elem.Group, defines)
			if err != nil {
				return nil, err
			}
			patterns = append(patterns, groupPatterns...)
		}

		// If we only have elements/groups and no other content patterns, check for Optional/OneOrMore/ZeroOrMore
		// that form a sequence with the elements
		if len(elem.Optional) > 0 || len(elem.OneOrMore) > 0 || len(elem.ZeroOrMore) > 0 {
			optPatterns, err := buildGroupOptionalPatterns(elem.Optional, defines)
			if err != nil {
				return nil, err
			}
			patterns = append(patterns, optPatterns...)

			oneOrMorePatterns, err := buildGroupOneOrMorePatterns(elem.OneOrMore, defines)
			if err != nil {
				return nil, err
			}
			patterns = append(patterns, oneOrMorePatterns...)

			zeroOrMorePatterns, err := buildGroupZeroOrMorePatterns(elem.ZeroOrMore, defines)
			if err != nil {
				return nil, err
			}
			patterns = append(patterns, zeroOrMorePatterns...)
		}

		contentPat := combinePatterns(patterns, &AnyContentPat{})
		return wrapInElementIfNamed(contentPat, elem.Name, elem.Ns), nil
	}

	// Check for choice
	if elem.Choice != nil {
		patterns, err := buildPatternsFromChoice(elem.Choice, defines)
		if err != nil {
			return nil, err
		}
		if len(patterns) > 0 {
			choicePat := &ChoicePat{Alternatives: patterns}
			return wrapPatternIfNamed(choicePat, elem.Name, elem.Ns), nil
		}
	}

	// Check for group
	if len(elem.Group) > 0 {
		patterns, err := buildPatternsFromGroups(elem.Group, defines)
		if err != nil {
			return nil, err
		}
		groupPat := combinePatterns(patterns, &EmptyPat{})
		return wrapInElementIfNamed(groupPat, elem.Name, elem.Ns), nil
	}

	// Check for interleave
	if len(elem.Interleave) > 0 {
		patterns, err := buildPatternsFromInterleaves(elem.Interleave, defines)
		if err != nil {
			return nil, err
		}
		if len(patterns) > 0 {
			interleavePat := &InterleavePat{Children: patterns}
			return wrapInElementIfNamed(interleavePat, elem.Name, elem.Ns), nil
		}
		return wrapInElementIfNamed(&AnyContentPat{}, elem.Name, elem.Ns), nil
	}

	return nil, nil
}

// buildRepetitionPattern handles repetition patterns (oneOrMore, zeroOrMore, optional)
func buildRepetitionPattern(elem *rng.Element, defines map[string]*rng.Define) (Pattern, error) {
	// Check for oneOrMore
	if len(elem.OneOrMore) > 0 {
		patterns, err := buildPatternsFromRawContent(elem.OneOrMore[0].RawContent, defines)
		if err != nil {
			return nil, err
		}
		var oneOrMoreChild Pattern
		switch {
		case len(patterns) == 1:
			oneOrMoreChild = &OneOrMorePat{Child: patterns[0]}
		case len(patterns) > 1:
			oneOrMoreChild = &OneOrMorePat{Child: &GroupPat{Children: patterns}}
		default:
			oneOrMoreChild = &OneOrMorePat{Child: &EmptyPat{}}
		}
		return wrapInElementIfNamed(oneOrMoreChild, elem.Name, elem.Ns), nil
	}

	// Check for zeroOrMore
	if len(elem.ZeroOrMore) > 0 {
		patterns, err := buildPatternsFromRawContent(elem.ZeroOrMore[0].RawContent, defines)
		if err != nil {
			return nil, err
		}
		var zeroOrMoreChild Pattern
		switch {
		case len(patterns) == 1:
			zeroOrMoreChild = &ZeroOrMorePat{Child: patterns[0]}
		case len(patterns) > 1:
			zeroOrMoreChild = &ZeroOrMorePat{Child: &GroupPat{Children: patterns}}
		default:
			zeroOrMoreChild = &ZeroOrMorePat{Child: &EmptyPat{}}
		}
		return wrapInElementIfNamed(zeroOrMoreChild, elem.Name, elem.Ns), nil
	}

	// Check for optional
	if len(elem.Optional) > 0 {
		patterns, err := buildPatternsFromRawContent(elem.Optional[0].RawContent, defines)
		if err != nil {
			return nil, err
		}
		var optionalChild Pattern
		switch {
		case len(patterns) == 1:
			optionalChild = &OptionalPat{Child: patterns[0]}
		case len(patterns) > 1:
			optionalChild = &OptionalPat{Child: &GroupPat{Children: patterns}}
		default:
			optionalChild = &OptionalPat{Child: &EmptyPat{}}
		}
		return wrapInElementIfNamed(optionalChild, elem.Name, elem.Ns), nil
	}

	return nil, nil
}

// buildElementRefPattern handles ref patterns within elements
func buildElementRefPattern(elem *rng.Element) Pattern {
	if len(elem.Ref) == 0 {
		return nil
	}
	patterns := make([]Pattern, 0, len(elem.Ref))
	for _, ref := range elem.Ref {
		patterns = append(patterns, &RefPat{Name: ref.Name})
	}
	var refPat Pattern
	if len(patterns) == 1 {
		refPat = patterns[0]
	} else {
		refPat = &GroupPat{Children: patterns}
	}
	if elem.Name != "" {
		return &ElementPat{
			Name:     elem.Name,
			Ns:       elem.Ns,
			Children: []Pattern{refPat},
		}
	}
	return refPat
}

// buildDefaultPattern returns the default pattern when no explicit patterns are found
func buildDefaultPattern(elem *rng.Element) Pattern {
	if elem.Name != "" {
		return &ElementPat{
			Name:     elem.Name,
			Ns:       elem.Ns,
			Children: []Pattern{&AnyContentPat{}},
		}
	}
	return &AnyContentPat{}
}

// BuildPatternFromElement builds a Pattern AST from an rng.Element struct
// This uses the structured fields (Empty, Text, Choice, etc.) not RawContent
func BuildPatternFromElement(elem *rng.Element, defines map[string]*rng.Define) (Pattern, error) {
	// Check for choice with NameElements first (element-level name specification)
	// This must come before checking elem.Empty/Text/etc because the choice defines the element names
	// and empty/text/etc define the content for those elements
	if elem.Choice != nil && len(elem.Choice.NameElements) > 0 && elem.Name == "" {
		return buildPatternFromChoiceWithNameElements(elem, defines)
	}

	// Try simple content patterns
	if pat, err := buildSimpleContentPattern(elem, defines); err != nil {
		return nil, err
	} else if pat != nil {
		return pat, nil
	}

	// Check for mixed content
	if elem.Mixed != nil {
		return buildMixedElementPattern(elem, defines)
	}

	// Try group-like patterns (elements, choice, group, interleave)
	if pat, err := buildGroupLikePattern(elem, defines); err != nil {
		return nil, err
	} else if pat != nil {
		return pat, nil
	}

	// Try repetition patterns (oneOrMore, zeroOrMore, optional)
	if pat, err := buildRepetitionPattern(elem, defines); err != nil {
		return nil, err
	} else if pat != nil {
		return pat, nil
	}

	// Check for refs
	if refPat := buildElementRefPattern(elem); refPat != nil {
		return refPat, nil
	}

	// No explicit patterns found - accept any content (mixed content)
	// When no patterns are specified in an element, RELAX NG allows mixed content
	return buildDefaultPattern(elem), nil
}

// Helper function to build patterns from RawContent
func buildPatternsFromRawContent(rawContent []byte, defines map[string]*rng.Define) ([]Pattern, error) {
	patterns, err := BuildPattern(rawContent, defines)
	if err != nil {
		return nil, err
	}

	// Unpack GroupPat to get individual patterns
	if grp, ok := patterns.(*GroupPat); ok {
		return grp.Children, nil
	}

	if patterns != nil {
		return []Pattern{patterns}, nil
	}

	return []Pattern{}, nil
}

// buildPatternsFromChoiceStructure builds patterns from structured choice elements.
func buildPatternsFromChoiceStructure(choice *rng.Choice, defines map[string]*rng.Define, patterns *[]Pattern) error {
	// Build patterns from structured elements
	for i := range choice.Elements {
		pat, err := BuildPatternFromElement(&choice.Elements[i], defines)
		if err != nil {
			return err
		}
		*patterns = append(*patterns, pat)
	}

	// Build patterns from refs
	for _, ref := range choice.Refs {
		*patterns = append(*patterns, &RefPat{Name: ref.Name})
	}

	// Build patterns from data
	for _, data := range choice.Data {
		*patterns = append(*patterns, &DataPat{
			Type:   data.Type,
			Params: data.Params,
			Except: data.Except,
		})
	}

	// Build pattern from simple patterns
	if choice.Text != nil {
		*patterns = append(*patterns, &TextPat{})
	}
	if choice.Empty != nil {
		*patterns = append(*patterns, &EmptyPat{})
	}
	if choice.NotAllowed != nil {
		*patterns = append(*patterns, &NotAllowedPat{})
	}

	// Build patterns from groups
	for _, grp := range choice.Group {
		if err := addGroupPatterns(&grp, defines, patterns); err != nil {
			return err
		}
	}

	// Build patterns from interleaves
	for _, interleave := range choice.Interleave {
		if err := addInterleavePatterns(&interleave, defines, patterns); err != nil {
			return err
		}
	}

	// Build pattern from list
	if choice.List != nil {
		if err := addListPattern(choice.List, defines, patterns); err != nil {
			return err
		}
	}

	// Build patterns from values
	if len(choice.Values) > 0 {
		*patterns = append(*patterns, &ValuePat{Values: choice.Values})
	}

	// Build pattern from mixed
	if choice.Mixed != nil {
		if err := addMixedPattern(choice.Mixed, defines, patterns); err != nil {
			return err
		}
	}

	return nil
}

func addGroupPatterns(grp *rng.Group, defines map[string]*rng.Define, patterns *[]Pattern) error {
	grpPatterns, err := buildPatternsFromGroups([]rng.Group{*grp}, defines)
	if err != nil {
		return err
	}
	if len(grpPatterns) == 1 {
		*patterns = append(*patterns, grpPatterns[0])
	} else if len(grpPatterns) > 1 {
		*patterns = append(*patterns, &GroupPat{Children: grpPatterns})
	}
	return nil
}

func addInterleavePatterns(interleave *rng.Interleave, defines map[string]*rng.Define, patterns *[]Pattern) error {
	intPatterns, err := buildPatternsFromInterleaves([]rng.Interleave{*interleave}, defines)
	if err != nil {
		return err
	}
	if len(intPatterns) > 0 {
		*patterns = append(*patterns, &InterleavePat{Children: intPatterns})
	}
	return nil
}

func addListPattern(list *rng.List, defines map[string]*rng.Define, patterns *[]Pattern) error {
	childPat, err := buildListChildPatternFromList(list, defines)
	if err != nil {
		return err
	}
	*patterns = append(*patterns, &ListPat{Child: childPat})
	return nil
}

// buildListChildPatternFromList builds the child pattern for a list
func buildListChildPatternFromList(list *rng.List, defines map[string]*rng.Define) (Pattern, error) {
	if len(list.RawContent) > 0 {
		rawPatterns, err := buildPatternsFromRawContent(list.RawContent, defines)
		if err != nil {
			return nil, err
		}
		if len(rawPatterns) == 1 {
			return rawPatterns[0], nil
		} else if len(rawPatterns) > 1 {
			return &GroupPat{Children: rawPatterns}, nil
		}
		return &TextPat{}, nil
	}
	return &TextPat{}, nil
}

func addMixedPattern(mixed *rng.Mixed, defines map[string]*rng.Define, patterns *[]Pattern) error {
	var childPat Pattern
	switch {
	case len(mixed.Elements) > 0:
		var elemPatterns []Pattern
		for i := range mixed.Elements {
			elemPat, err := BuildPatternFromElement(&mixed.Elements[i], defines)
			if err != nil {
				return err
			}
			if elemPat != nil {
				elemPatterns = append(elemPatterns, elemPat)
			}
		}
		switch {
		case len(elemPatterns) == 1:
			childPat = elemPatterns[0]
		case len(elemPatterns) > 1:
			childPat = &GroupPat{Children: elemPatterns}
		default:
			childPat = &EmptyPat{}
		}
	case len(mixed.RawContent) > 0:
		rawPat, err := BuildPattern(mixed.RawContent, defines)
		if err != nil {
			return err
		}
		childPat = rawPat
	default:
		childPat = &EmptyPat{}
	}
	*patterns = append(*patterns, &MixedPat{Child: childPat})
	return nil
}

// Helper function to build patterns from Choice
func buildPatternsFromChoice(choice *rng.Choice, defines map[string]*rng.Define) ([]Pattern, error) {
	var patterns []Pattern

	// First try building from structured fields (used after combine merging)
	hasStructured := len(choice.Elements) > 0 || len(choice.Refs) > 0 ||
		len(choice.Data) > 0 || choice.Text != nil || choice.Empty != nil ||
		choice.NotAllowed != nil || len(choice.Group) > 0 || len(choice.Interleave) > 0 ||
		choice.List != nil || len(choice.Values) > 0 || choice.Mixed != nil

	if hasStructured {
		// Build patterns from structured elements
		err := buildPatternsFromChoiceStructure(choice, defines, &patterns)
		if err != nil {
			return nil, err
		}
		if len(patterns) > 0 {
			return patterns, nil
		}
	}

	// Fall back to RawContent for original parsed choices
	if len(choice.RawContent) > 0 {
		result, err := BuildPattern(choice.RawContent, defines)
		if err != nil {
			return nil, err
		}

		if grp, ok := result.(*GroupPat); ok {
			patterns = append(patterns, grp.Children...)
		} else if result != nil {
			patterns = append(patterns, result)
		}
	}

	return patterns, nil
}

// buildPatternsFromElementsAndRefs is a helper to avoid duplication across Optional/OneOrMore/ZeroOrMore pattern building
func buildPatternsFromElementsAndRefs(elements []rng.Element, refs []rng.Ref, rawContent []byte, defines map[string]*rng.Define) ([]Pattern, error) {
	var patterns []Pattern

	// Build from structured fields
	if len(elements) > 0 || len(refs) > 0 {
		for i := range elements {
			pat, err := BuildPatternFromElement(&elements[i], defines)
			if err != nil {
				return nil, err
			}
			patterns = append(patterns, pat)
		}
		for _, ref := range refs {
			patterns = append(patterns, &RefPat{Name: ref.Name})
		}
		return patterns, nil
	}

	// Fall back to RawContent
	if len(rawContent) > 0 {
		result, err := BuildPattern(rawContent, defines)
		if err != nil {
			return nil, err
		}
		if grp, ok := result.(*GroupPat); ok {
			patterns = append(patterns, grp.Children...)
		} else if result != nil {
			patterns = append(patterns, result)
		}
	}

	return patterns, nil
}

// buildPatternsFromOptional builds patterns from an optional structure
func buildPatternsFromOptional(opt *rng.Optional, defines map[string]*rng.Define) ([]Pattern, error) {
	return buildPatternsFromElementsAndRefs(opt.Elements, opt.Ref, opt.RawContent, defines)
}

// buildPatternsFromOneOrMore builds patterns from a oneOrMore structure
func buildPatternsFromOneOrMore(oneOrMore *rng.OneOrMore, defines map[string]*rng.Define) ([]Pattern, error) {
	return buildPatternsFromElementsAndRefs(oneOrMore.Element, oneOrMore.Ref, oneOrMore.RawContent, defines)
}

// buildPatternsFromZeroOrMore builds patterns from a zeroOrMore structure
func buildPatternsFromZeroOrMore(zeroOrMore *rng.ZeroOrMore, defines map[string]*rng.Define) ([]Pattern, error) {
	return buildPatternsFromElementsAndRefs(zeroOrMore.Element, zeroOrMore.Ref, zeroOrMore.RawContent, defines)
}

// buildGroupStructuredPatterns builds patterns from group structured fields
// buildGroupElementPatterns builds patterns from elements in a group
func buildGroupElementPatterns(elements []rng.Element, defines map[string]*rng.Define) ([]Pattern, error) {
	patterns := make([]Pattern, 0, len(elements))
	for i := range elements {
		pat, err := BuildPatternFromElement(&elements[i], defines)
		if err != nil {
			return nil, err
		}
		patterns = append(patterns, pat)
	}
	return patterns, nil
}

// buildGroupChoicePatterns builds patterns from choices in a group
func buildGroupChoicePatterns(choices []rng.Choice, defines map[string]*rng.Define) ([]Pattern, error) {
	var patterns []Pattern
	for _, choice := range choices {
		choicePatterns, err := buildPatternsFromChoice(&choice, defines)
		if err != nil {
			return nil, err
		}
		if len(choicePatterns) > 0 {
			patterns = append(patterns, &ChoicePat{Alternatives: choicePatterns})
		}
	}
	return patterns, nil
}

// buildGroupOptionalPatterns builds patterns from optional elements in a group
func buildGroupOptionalPatterns(optionals []rng.Optional, defines map[string]*rng.Define) ([]Pattern, error) {
	var patterns []Pattern
	for _, opt := range optionals {
		optPatterns, err := buildPatternsFromOptional(&opt, defines)
		if err != nil {
			return nil, err
		}
		for _, optPat := range optPatterns {
			patterns = append(patterns, &OptionalPat{Child: optPat})
		}
	}
	return patterns, nil
}

// buildGroupGroupPatterns builds patterns from nested groups
func buildGroupGroupPatterns(nestedGroups []rng.Group, defines map[string]*rng.Define) ([]Pattern, error) {
	var patterns []Pattern
	for _, nestedGroup := range nestedGroups {
		groupPatterns, err := buildPatternsFromGroups([]rng.Group{nestedGroup}, defines)
		if err != nil {
			return nil, err
		}
		if len(groupPatterns) == 1 {
			patterns = append(patterns, groupPatterns[0])
		} else if len(groupPatterns) > 1 {
			patterns = append(patterns, &GroupPat{Children: groupPatterns})
		}
	}
	return patterns, nil
}

// buildGroupInterleavePatterns builds patterns from interleaves in a group
func buildGroupInterleavePatterns(interleaves []rng.Interleave, defines map[string]*rng.Define) ([]Pattern, error) {
	var patterns []Pattern
	for _, interleave := range interleaves {
		interleavePatterns, err := buildPatternsFromInterleaves([]rng.Interleave{interleave}, defines)
		if err != nil {
			return nil, err
		}
		if len(interleavePatterns) > 0 {
			patterns = append(patterns, &InterleavePat{Children: interleavePatterns})
		}
	}
	return patterns, nil
}

func buildGroupStructuredPatterns(group *rng.Group, defines map[string]*rng.Define) ([]Pattern, error) {
	capacity := len(group.Elements) + len(group.Ref) + len(group.Choice) + len(group.Optional) + len(group.OneOrMore) + len(group.ZeroOrMore) + len(group.Group) + len(group.Interleave)
	patterns := make([]Pattern, 0, capacity)

	// Build patterns from structured elements
	elemPatterns, err := buildGroupElementPatterns(group.Elements, defines)
	if err != nil {
		return nil, err
	}
	patterns = append(patterns, elemPatterns...)

	// Build patterns from refs
	for _, ref := range group.Ref {
		patterns = append(patterns, &RefPat{Name: ref.Name})
	}

	// Build patterns from choice
	choicePatterns, err := buildGroupChoicePatterns(group.Choice, defines)
	if err != nil {
		return nil, err
	}
	patterns = append(patterns, choicePatterns...)

	// Build patterns from optional
	optPatterns, err := buildGroupOptionalPatterns(group.Optional, defines)
	if err != nil {
		return nil, err
	}
	patterns = append(patterns, optPatterns...)

	// Build patterns from oneOrMore
	oneOrMorePatterns, err := buildGroupOneOrMorePatterns(group.OneOrMore, defines)
	if err != nil {
		return nil, err
	}
	patterns = append(patterns, oneOrMorePatterns...)

	// Build patterns from zeroOrMore
	zeroOrMorePatterns, err := buildGroupZeroOrMorePatterns(group.ZeroOrMore, defines)
	if err != nil {
		return nil, err
	}
	patterns = append(patterns, zeroOrMorePatterns...)

	// Build patterns from nested groups
	groupPatterns, err := buildGroupGroupPatterns(group.Group, defines)
	if err != nil {
		return nil, err
	}
	patterns = append(patterns, groupPatterns...)

	// Build patterns from interleave
	interleavePatterns, err := buildGroupInterleavePatterns(group.Interleave, defines)
	if err != nil {
		return nil, err
	}
	patterns = append(patterns, interleavePatterns...)

	// If group has text content, wrap in MixedPat if we have other patterns
	if group.Text != nil && len(patterns) > 0 {
		// Create a GroupPat from the patterns to represent the sequence
		groupPat := &GroupPat{Children: patterns}
		// Wrap in MixedPat to handle text interspersed with elements
		return []Pattern{&MixedPat{Child: groupPat}}, nil
	}

	return patterns, nil
}

// buildGroupOneOrMorePatterns builds patterns from oneOrMore elements in a group
func buildGroupOneOrMorePatterns(oneOrMores []rng.OneOrMore, defines map[string]*rng.Define) ([]Pattern, error) {
	var patterns []Pattern
	for _, oneOrMore := range oneOrMores {
		oneOrMorePatterns, err := buildPatternsFromOneOrMore(&oneOrMore, defines)
		if err != nil {
			return nil, err
		}
		for _, pat := range oneOrMorePatterns {
			patterns = append(patterns, &OneOrMorePat{Child: pat})
		}
	}
	return patterns, nil
}

// buildGroupZeroOrMorePatterns builds patterns from zeroOrMore elements in a group
func buildGroupZeroOrMorePatterns(zeroOrMores []rng.ZeroOrMore, defines map[string]*rng.Define) ([]Pattern, error) {
	var patterns []Pattern
	for _, zeroOrMore := range zeroOrMores {
		zeroOrMorePatterns, err := buildPatternsFromZeroOrMore(&zeroOrMore, defines)
		if err != nil {
			return nil, err
		}
		for _, pat := range zeroOrMorePatterns {
			patterns = append(patterns, &ZeroOrMorePat{Child: pat})
		}
	}
	return patterns, nil
}

// buildPatternsFromGroups builds patterns from group structures
func buildPatternsFromGroups(groups []rng.Group, defines map[string]*rng.Define) ([]Pattern, error) {
	var patterns []Pattern

	for _, group := range groups {
		// Check if we have RawContent with patterns not captured in structured fields
		if len(bytes.TrimSpace(group.RawContent)) > 0 {
			// Try parsing from RawContent to preserve order and capture all pattern types
			rawPatterns, err := buildPatternsFromRawContent(group.RawContent, defines)
			if err == nil && len(rawPatterns) > 0 {
				// Successfully parsed from RawContent
				patterns = append(patterns, rawPatterns...)
				continue
			}
		}

		// Build patterns from structured fields
		groupPats, err := buildGroupStructuredPatterns(&group, defines)
		if err != nil {
			return nil, err
		}
		patterns = append(patterns, groupPats...)
	}

	return patterns, nil
}

// buildInterleaveElementPatterns builds patterns from elements in an interleave
func buildInterleaveElementPatterns(elements []rng.Element, defines map[string]*rng.Define) ([]Pattern, error) {
	patterns := make([]Pattern, 0, len(elements))
	for i := range elements {
		pat, err := BuildPatternFromElement(&elements[i], defines)
		if err != nil {
			return nil, err
		}
		patterns = append(patterns, pat)
	}
	return patterns, nil
}

// buildInterleaveChoicePatterns builds patterns from choices in an interleave
func buildInterleaveChoicePatterns(choices []rng.Choice, defines map[string]*rng.Define) ([]Pattern, error) {
	var patterns []Pattern
	for _, choice := range choices {
		choicePatterns, err := buildPatternsFromChoice(&choice, defines)
		if err != nil {
			return nil, err
		}
		if len(choicePatterns) > 0 {
			patterns = append(patterns, &ChoicePat{Alternatives: choicePatterns})
		}
	}
	return patterns, nil
}

// buildInterleaveOptionalPatterns builds patterns from optional elements in an interleave
func buildInterleaveOptionalPatterns(optionals []rng.Optional, defines map[string]*rng.Define) ([]Pattern, error) {
	var patterns []Pattern
	for _, opt := range optionals {
		optPatterns, err := buildPatternsFromOptional(&opt, defines)
		if err != nil {
			return nil, err
		}
		for _, optPat := range optPatterns {
			patterns = append(patterns, &OptionalPat{Child: optPat})
		}
	}
	return patterns, nil
}

// buildInterleaveGroupPatterns builds patterns from groups in an interleave
func buildInterleaveGroupPatterns(groups []rng.Group, defines map[string]*rng.Define) ([]Pattern, error) {
	var patterns []Pattern
	for _, group := range groups {
		groupPatterns, err := buildPatternsFromGroups([]rng.Group{group}, defines)
		if err != nil {
			return nil, err
		}
		if len(groupPatterns) == 1 {
			patterns = append(patterns, groupPatterns[0])
		} else if len(groupPatterns) > 1 {
			patterns = append(patterns, &GroupPat{Children: groupPatterns})
		}
	}
	return patterns, nil
}

// buildInterleaveStructuredPatterns builds patterns from interleave structured fields
func buildInterleaveStructuredPatterns(interleave *rng.Interleave, defines map[string]*rng.Define) ([]Pattern, error) {
	capacity := len(interleave.Elements) + len(interleave.Ref) + len(interleave.Choice) + len(interleave.Optional) + len(interleave.OneOrMore) + len(interleave.ZeroOrMore) + len(interleave.Group)
	patterns := make([]Pattern, 0, capacity)

	// Build patterns from structured elements
	elemPatterns, err := buildInterleaveElementPatterns(interleave.Elements, defines)
	if err != nil {
		return nil, err
	}
	patterns = append(patterns, elemPatterns...)

	// Build patterns from refs
	for _, ref := range interleave.Ref {
		patterns = append(patterns, &RefPat{Name: ref.Name})
	}

	// Build patterns from choice
	choicePatterns, err := buildInterleaveChoicePatterns(interleave.Choice, defines)
	if err != nil {
		return nil, err
	}
	patterns = append(patterns, choicePatterns...)

	// Build patterns from optional
	optPatterns, err := buildInterleaveOptionalPatterns(interleave.Optional, defines)
	if err != nil {
		return nil, err
	}
	patterns = append(patterns, optPatterns...)

	// Build patterns from oneOrMore
	oneOrMorePatterns, err := buildInterleaveOneOrMorePatterns(interleave.OneOrMore, defines)
	if err != nil {
		return nil, err
	}
	patterns = append(patterns, oneOrMorePatterns...)

	// Build patterns from zeroOrMore
	zeroOrMorePatterns, err := buildInterleaveZeroOrMorePatterns(interleave.ZeroOrMore, defines)
	if err != nil {
		return nil, err
	}
	patterns = append(patterns, zeroOrMorePatterns...)

	// Build patterns from nested groups
	groupPatterns, err := buildInterleaveGroupPatterns(interleave.Group, defines)
	if err != nil {
		return nil, err
	}
	patterns = append(patterns, groupPatterns...)

	return patterns, nil
}

// buildInterleaveOneOrMorePatterns builds patterns from oneOrMore elements in an interleave
func buildInterleaveOneOrMorePatterns(oneOrMores []rng.OneOrMore, defines map[string]*rng.Define) ([]Pattern, error) {
	var patterns []Pattern
	for _, oneOrMore := range oneOrMores {
		oneOrMorePatterns, err := buildPatternsFromOneOrMore(&oneOrMore, defines)
		if err != nil {
			return nil, err
		}
		for _, pat := range oneOrMorePatterns {
			patterns = append(patterns, &OneOrMorePat{Child: pat})
		}
	}
	return patterns, nil
}

// buildInterleaveZeroOrMorePatterns builds patterns from zeroOrMore elements in an interleave
func buildInterleaveZeroOrMorePatterns(zeroOrMores []rng.ZeroOrMore, defines map[string]*rng.Define) ([]Pattern, error) {
	var patterns []Pattern
	for _, zeroOrMore := range zeroOrMores {
		zeroOrMorePatterns, err := buildPatternsFromZeroOrMore(&zeroOrMore, defines)
		if err != nil {
			return nil, err
		}
		for _, pat := range zeroOrMorePatterns {
			patterns = append(patterns, &ZeroOrMorePat{Child: pat})
		}
	}
	return patterns, nil
}

// buildPatternsFromInterleaves builds patterns from interleave structures
func buildPatternsFromInterleaves(interleaves []rng.Interleave, defines map[string]*rng.Define) ([]Pattern, error) {
	var patterns []Pattern

	for _, interleave := range interleaves {
		// Check if we have RawContent with patterns not captured in structured fields
		if len(bytes.TrimSpace(interleave.RawContent)) > 0 {
			// Try parsing from RawContent to preserve order and capture all pattern types
			rawPatterns, err := buildPatternsFromRawContent(interleave.RawContent, defines)
			if err == nil && len(rawPatterns) > 0 {
				// Successfully parsed from RawContent
				patterns = append(patterns, rawPatterns...)
				continue
			}
		}

		// Build patterns from structured fields
		interleavePats, err := buildInterleaveStructuredPatterns(&interleave, defines)
		if err != nil {
			return nil, err
		}
		patterns = append(patterns, interleavePats...)
	}

	return patterns, nil
}

// BuildPattern constructs an ordered Pattern AST from element RawContent
// This preserves the order of child patterns and handles all pattern types
func BuildPattern(rawContent []byte, defines map[string]*rng.Define) (Pattern, error) {
	if len(bytes.TrimSpace(rawContent)) == 0 {
		// Empty content in RELAX NG means "accept anything"
		// When no patterns are specified, the element implicitly allows any content (mixed)
		return &AnyContentPat{}, nil
	}

	decoder := xml.NewDecoder(bytes.NewReader(rawContent))
	var patterns []Pattern

	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		switch t := tok.(type) {
		case xml.StartElement:
			pat, err := buildPatternFromElement(&t, decoder, defines)
			if err != nil {
				return nil, err
			}
			if pat != nil {
				patterns = append(patterns, pat)
			}
		case xml.CharData:
			// Ignore whitespace-only text between elements
			continue
		case xml.Comment:
			continue
		}
	}

	// If single pattern, return it directly
	if len(patterns) == 1 {
		return patterns[0], nil
	}

	// Multiple patterns form an implicit group
	if len(patterns) > 1 {
		return &GroupPat{Children: patterns}, nil
	}

	// No patterns found (e.g., only attributes) - accept any content
	return &AnyContentPat{}, nil
}

// buildPatternFromElement constructs a Pattern from an XML element
// buildSimplePat handles simple patterns (text, empty, notAllowed)
func buildSimplePat(localName string, decoder *xml.Decoder) Pattern {
	_ = skipToEnd(decoder) // Discard error - simple patterns don't need content validation
	switch localName {
	case "text":
		return &TextPat{}
	case "empty":
		return &EmptyPat{}
	case "notAllowed":
		return &NotAllowedPat{}
	}
	return nil
}

func buildPatternFromElement(start *xml.StartElement, decoder *xml.Decoder, defines map[string]*rng.Define) (Pattern, error) {
	localName := start.Name.Local

	switch localName {
	case "element":
		return buildElementPattern(start, decoder, defines)
	case "group":
		return buildGroupPattern(start, decoder, defines)
	case "choice":
		return buildChoicePattern(start, decoder, defines)
	case "interleave":
		return buildInterleavePattern(start, decoder, defines)
	case "optional":
		return buildOptionalPattern(start, decoder, defines)
	case "oneOrMore":
		return buildOneOrMorePattern(start, decoder, defines)
	case "zeroOrMore":
		return buildZeroOrMorePattern(start, decoder, defines)
	case "ref":
		return buildRefPattern(start, decoder)
	case "text", "empty", "notAllowed":
		return buildSimplePat(localName, decoder), nil
	case "data":
		return buildDataPattern(start, decoder)
	case "list":
		return buildListPattern(start, decoder, defines)
	case "mixed":
		return buildMixedPattern(start, decoder, defines)
	case "value":
		return buildValuePattern(start, decoder)
	case "attribute":
		_ = skipToEnd(decoder) // Discard error - attributes are not supported in patterns
		return nil, nil
	default:
		_ = skipToEnd(decoder) // Discard error - skip unknown elements
		return nil, nil
	}
}

// Helper to skip to the end of current element
func skipToEnd(decoder *xml.Decoder) error {
	depth := 1
	for depth > 0 {
		tok, err := decoder.Token()
		if err != nil {
			return err
		}
		switch tok.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
		}
	}
	return nil
}

// buildElementPattern creates an ElementPat
func buildElementPattern(start *xml.StartElement, decoder *xml.Decoder, defines map[string]*rng.Define) (Pattern, error) {
	elem := &ElementPat{}

	// Extract name attribute
	for _, attr := range start.Attr {
		switch attr.Name.Local {
		case "name":
			elem.Name = attr.Value
		case "ns":
			elem.Ns = attr.Value
		}
	}

	// Read inner content to build children
	innerContent, err := readInnerXML(decoder)
	if err != nil {
		return nil, err
	}

	// Build children patterns
	children, err := BuildPattern(innerContent, defines)
	if err != nil {
		return nil, err
	}

	// Wrap single child in a slice
	if children != nil {
		if group, ok := children.(*GroupPat); ok {
			elem.Children = group.Children
		} else {
			elem.Children = []Pattern{children}
		}
	}

	return elem, nil
}

// buildGroupPattern creates a GroupPat
func buildGroupPattern(_ *xml.StartElement, decoder *xml.Decoder, defines map[string]*rng.Define) (Pattern, error) {
	innerContent, err := readInnerXML(decoder)
	if err != nil {
		return nil, err
	}

	children, err := BuildPattern(innerContent, defines)
	if err != nil {
		return nil, err
	}

	if children == nil {
		return &EmptyPat{}, nil
	}

	// If already a group, return it
	if group, ok := children.(*GroupPat); ok {
		return group, nil
	}

	// Wrap single pattern in group
	return &GroupPat{Children: []Pattern{children}}, nil
}

// buildChoicePattern creates a ChoicePat
func buildChoicePattern(_ *xml.StartElement, decoder *xml.Decoder, defines map[string]*rng.Define) (Pattern, error) {
	innerContent, err := readInnerXML(decoder)
	if err != nil {
		return nil, err
	}

	// For choice, we want each direct child as an alternative
	childDecoder := xml.NewDecoder(bytes.NewReader(innerContent))
	var alternatives []Pattern
	hasAttribute := false

	for {
		tok, err := childDecoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		if elemStart, ok := tok.(xml.StartElement); ok {
			// Check if this is an attribute element
			if elemStart.Name.Local == "attribute" {
				hasAttribute = true
				// Skip to end of attribute element
				_ = skipToEnd(childDecoder) // Discard error - attributes are not supported
				continue
			}

			pat, err := buildPatternFromElement(&elemStart, childDecoder, defines)
			if err != nil {
				return nil, err
			}
			if pat != nil {
				alternatives = append(alternatives, pat)
			}
		}
	}

	// If choice has attributes and no content patterns extracted,
	// return AnyContentPat to indicate the choice is satisfied if attributes match
	if hasAttribute && len(alternatives) == 0 {
		return &AnyContentPat{}, nil
	}

	return &ChoicePat{Alternatives: alternatives}, nil
}

// buildInterleavePattern creates an InterleavePat
func buildInterleavePattern(_ *xml.StartElement, decoder *xml.Decoder, defines map[string]*rng.Define) (Pattern, error) {
	innerContent, err := readInnerXML(decoder)
	if err != nil {
		return nil, err
	}

	children, err := BuildPattern(innerContent, defines)
	if err != nil {
		return nil, err
	}

	if children == nil {
		return &EmptyPat{}, nil
	}

	// If already a group, extract children for interleave
	if group, ok := children.(*GroupPat); ok {
		return &InterleavePat{Children: group.Children}, nil
	}

	return &InterleavePat{Children: []Pattern{children}}, nil
}

// buildOptionalPattern creates an OptionalPat
func buildOptionalPattern(_ *xml.StartElement, decoder *xml.Decoder, defines map[string]*rng.Define) (Pattern, error) {
	innerContent, err := readInnerXML(decoder)
	if err != nil {
		return nil, err
	}

	child, err := BuildPattern(innerContent, defines)
	if err != nil {
		return nil, err
	}

	if child == nil {
		child = &EmptyPat{}
	}

	return &OptionalPat{Child: child}, nil
}

// buildOneOrMorePattern creates a OneOrMorePat
func buildOneOrMorePattern(_ *xml.StartElement, decoder *xml.Decoder, defines map[string]*rng.Define) (Pattern, error) {
	innerContent, err := readInnerXML(decoder)
	if err != nil {
		return nil, err
	}

	child, err := BuildPattern(innerContent, defines)
	if err != nil {
		return nil, err
	}

	if child == nil {
		child = &EmptyPat{}
	}

	return &OneOrMorePat{Child: child}, nil
}

// buildZeroOrMorePattern creates a ZeroOrMorePat
func buildZeroOrMorePattern(_ *xml.StartElement, decoder *xml.Decoder, defines map[string]*rng.Define) (Pattern, error) {
	innerContent, err := readInnerXML(decoder)
	if err != nil {
		return nil, err
	}

	child, err := BuildPattern(innerContent, defines)
	if err != nil {
		return nil, err
	}

	if child == nil {
		child = &EmptyPat{}
	}

	return &ZeroOrMorePat{Child: child}, nil
}

// buildRefPattern creates a RefPat
func buildRefPattern(start *xml.StartElement, decoder *xml.Decoder) (Pattern, error) {
	var name string
	for _, attr := range start.Attr {
		if attr.Name.Local == "name" {
			name = attr.Value
			break
		}
	}

	if err := skipToEnd(decoder); err != nil {
		return nil, err
	}

	return &RefPat{Name: name}, nil
}

// buildDataPattern creates a DataPat
func buildDataPattern(start *xml.StartElement, decoder *xml.Decoder) (Pattern, error) {
	data := &DataPat{}

	for _, attr := range start.Attr {
		if attr.Name.Local == "type" {
			data.Type = attr.Value
		}
	}

	// TODO: Parse params and except from inner content if needed
	if err := skipToEnd(decoder); err != nil {
		return nil, err
	}

	return data, nil
}

// buildListPattern creates a ListPat
func buildListPattern(_ *xml.StartElement, decoder *xml.Decoder, defines map[string]*rng.Define) (Pattern, error) {
	innerContent, err := readInnerXML(decoder)
	if err != nil {
		return nil, err
	}

	child, err := BuildPattern(innerContent, defines)
	if err != nil {
		return nil, err
	}

	if child == nil {
		child = &EmptyPat{}
	}

	return &ListPat{Child: child}, nil
}

// buildMixedPattern creates a MixedPat
func buildMixedPattern(_ *xml.StartElement, decoder *xml.Decoder, defines map[string]*rng.Define) (Pattern, error) {
	innerContent, err := readInnerXML(decoder)
	if err != nil {
		return nil, err
	}

	child, err := BuildPattern(innerContent, defines)
	if err != nil {
		return nil, err
	}

	if child == nil {
		child = &EmptyPat{}
	}

	return &MixedPat{Child: child}, nil
}

// buildValuePattern creates a ValuePat from a <value> element
// <value> elements contain text that represents an allowed literal value
func buildValuePattern(start *xml.StartElement, decoder *xml.Decoder) (Pattern, error) {
	// Extract attributes
	var valueType string
	for _, attr := range start.Attr {
		if attr.Name.Local == "type" {
			valueType = attr.Value
		}
	}

	// Read the text content of the value element
	var text string
	depth := 1
	for depth > 0 {
		tok, err := decoder.Token()
		if err != nil {
			return nil, err
		}

		switch t := tok.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
		case xml.CharData:
			text += string(t)
		}
	}

	// Create a Value with the text content and type
	val := rng.Value{
		Value: strings.TrimSpace(text),
		Type:  valueType,
	}

	return &ValuePat{Values: []rng.Value{val}}, nil
}

// readInnerXML reads the inner XML content of the current element
func readInnerXML(decoder *xml.Decoder) ([]byte, error) {
	var buf bytes.Buffer
	depth := 1

	for depth > 0 {
		tok, err := decoder.Token()
		if err != nil {
			return nil, err
		}

		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			buf.WriteString(fmt.Sprintf("<%s", t.Name.Local))
			for _, attr := range t.Attr {
				buf.WriteString(fmt.Sprintf(" %s=\"%s\"", attr.Name.Local, attr.Value))
			}
			buf.WriteString(">")
		case xml.EndElement:
			depth--
			if depth > 0 {
				buf.WriteString(fmt.Sprintf("</%s>", t.Name.Local))
			}
		case xml.CharData:
			buf.Write(t)
		case xml.Comment:
			// Skip comments
		}
	}

	return buf.Bytes(), nil
}

// TokenBuffer represents a buffered sequence of XML tokens for pattern matching
type TokenBuffer struct {
	tokens []xml.Token
	pos    int
}

// NewTokenBuffer creates a token buffer from a decoder (reads until EndElement)
func NewTokenBuffer(decoder *xml.Decoder) (*TokenBuffer, error) {
	var tokens []xml.Token
	depth := 1

	for depth > 0 {
		tok, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		// Copy the token (decoder reuses memory)
		tok = xml.CopyToken(tok)

		switch tok.(type) {
		case xml.StartElement:
			depth++
			tokens = append(tokens, tok)
		case xml.EndElement:
			depth--
			if depth > 0 {
				tokens = append(tokens, tok)
			}
		case xml.CharData, xml.Comment:
			tokens = append(tokens, tok)
		}
	}

	return &TokenBuffer{tokens: tokens, pos: 0}, nil
}

// Peek returns the next token without consuming it, skipping whitespace-only data
func (tb *TokenBuffer) Peek() (xml.Token, bool) {
	// Skip whitespace-only CharData and comments
	for tb.pos < len(tb.tokens) {
		tok := tb.tokens[tb.pos]
		switch t := tok.(type) {
		case xml.CharData:
			if len(bytes.TrimSpace(t)) == 0 {
				tb.pos++
				continue
			}
		case xml.Comment:
			tb.pos++
			continue
		}
		return tok, true
	}
	return nil, false
}

// PeekRaw returns the next token without consuming it, including whitespace-only CharData
// This is used for data pattern matching where whitespace is significant
func (tb *TokenBuffer) PeekRaw() (xml.Token, bool) {
	for tb.pos < len(tb.tokens) {
		tok := tb.tokens[tb.pos]
		if _, ok := tok.(xml.Comment); ok {
			tb.pos++
			continue
		}
		return tok, true
	}
	return nil, false
}

// NextRaw consumes and returns the next token, including whitespace-only CharData
// This is used for data pattern matching where whitespace is significant
func (tb *TokenBuffer) NextRaw() (xml.Token, bool) {
	tok, ok := tb.PeekRaw()
	if ok {
		tb.pos++
	}
	return tok, ok
}

// Next consumes and returns the next token
func (tb *TokenBuffer) Next() (xml.Token, bool) {
	tok, ok := tb.Peek()
	if ok {
		tb.pos++
	}
	return tok, ok
}

// Mark returns current position
func (tb *TokenBuffer) Mark() int {
	return tb.pos
}

// Reset rewinds to a marked position
func (tb *TokenBuffer) Reset(pos int) {
	tb.pos = pos
}

// IsEmpty returns true if no more significant tokens remain
func (tb *TokenBuffer) IsEmpty() bool {
	_, ok := tb.Peek()
	return !ok
}

// MatchResult represents the result of pattern matching
type MatchResult struct {
	Success bool
	Error   string
	Details []string
}

// MatchPattern attempts to match a pattern against a token buffer
// Returns success status and any validation errors
func MatchPattern(pattern Pattern, buffer *TokenBuffer, defines map[string]*rng.Define, ctx *validationContext) MatchResult {
	if pattern == nil {
		return MatchResult{Success: true}
	}

	switch p := pattern.(type) {
	case *ElementPat:
		return matchElementPat(p, buffer, defines, ctx)
	case *GroupPat:
		return matchGroupPat(p, buffer, defines, ctx)
	case *ChoicePat:
		return matchChoicePat(p, buffer, defines, ctx)
	case *InterleavePat:
		return matchInterleavePat(p, buffer, defines, ctx)
	case *OptionalPat:
		return matchOptionalPat(p, buffer, defines, ctx)
	case *OneOrMorePat:
		return matchOneOrMorePat(p, buffer, defines, ctx)
	case *ZeroOrMorePat:
		return matchZeroOrMorePat(p, buffer, defines, ctx)
	case *RefPat:
		return matchRefPat(p, buffer, defines, ctx)
	case *TextPat:
		return matchTextPat(buffer)
	case *EmptyPat:
		return matchEmptyPat(buffer)
	case *NotAllowedPat:
		return MatchResult{Success: false, Error: "content not allowed"}
	case *DataPat:
		return matchDataPat(p, buffer, ctx)
	case *ValuePat:
		return matchValuePat(p, buffer, ctx)
	case *MixedPat:
		return matchMixedPat(p, buffer, defines, ctx)
	case *ListPat:
		return matchListPat(p, buffer, defines, ctx)
	case *AnyContentPat:
		// Accept any content - consume all tokens and succeed
		return matchAnyContentPat(buffer)
	default:
		// Unknown pattern type - accept for now
		return MatchResult{Success: true}
	}
}

// matchGroupPat matches a sequence of patterns in order
func matchGroupPat(group *GroupPat, buffer *TokenBuffer, defines map[string]*rng.Define, ctx *validationContext) MatchResult {
	for _, child := range group.Children {
		result := MatchPattern(child, buffer, defines, ctx)
		if !result.Success {
			return result
		}
	}
	return MatchResult{Success: true}
}

// matchChoicePat tries each alternative with backtracking
func matchChoicePat(choice *ChoicePat, buffer *TokenBuffer, defines map[string]*rng.Define, ctx *validationContext) MatchResult {
	if len(choice.Alternatives) == 0 {
		return MatchResult{Success: false, Error: "empty choice"}
	}

	errors := make([]string, 0, len(choice.Alternatives))
	for i, alt := range choice.Alternatives {
		mark := buffer.Mark()
		result := MatchPattern(alt, buffer, defines, ctx)
		if result.Success {
			// Check if this alternative consumed all remaining content
			// If there's unconsumed content and we have more alternatives, try them first
			if !buffer.IsEmpty() && i < len(choice.Alternatives)-1 {
				// Reset and try next alternative - this one is incomplete
				buffer.Reset(mark)
				errors = append(errors, "partial match, unconsumed content remains")
				continue
			}
			// Either this alternative consumed everything, or it's the last one
			// (in which case unconsumed content will be reported by validateElementAST)
			return result
		}
		// Backtrack and try next alternative
		buffer.Reset(mark)
		errors = append(errors, result.Error)
	}

	return MatchResult{
		Success: false,
		Error:   "no choice alternative matched",
		Details: errors,
	}
}

// matchOptionalPat tries to match the pattern once, succeeds if not matched
func matchOptionalPat(opt *OptionalPat, buffer *TokenBuffer, defines map[string]*rng.Define, ctx *validationContext) MatchResult {
	mark := buffer.Mark()
	result := MatchPattern(opt.Child, buffer, defines, ctx)
	if !result.Success {
		// Optional didn't match - that's OK, reset and succeed
		buffer.Reset(mark)
	}
	return MatchResult{Success: true}
}

// matchOneOrMorePat matches pattern one or more times
func matchOneOrMorePat(one *OneOrMorePat, buffer *TokenBuffer, defines map[string]*rng.Define, ctx *validationContext) MatchResult {
	matchCount := 0
	for {
		mark := buffer.Mark()
		result := MatchPattern(one.Child, buffer, defines, ctx)
		if !result.Success {
			buffer.Reset(mark)
			break
		}
		// Check if we consumed any tokens - if not, break to avoid infinite loop
		if buffer.Mark() == mark {
			// Pattern matched but didn't consume tokens (e.g., EmptyPat, OptionalPat)
			matchCount++
			break
		}
		matchCount++
	}

	if matchCount == 0 {
		// Check if oneOrMore choice attributes were already matched in validateAttributes
		// If so, that counts as satisfying the oneOrMore requirement
		if ctx != nil && ctx.oneOrMoreChoiceAttributeMatched {
			return MatchResult{Success: true}
		}
		return MatchResult{Success: false, Error: "oneOrMore requires at least one match"}
	}
	return MatchResult{Success: true}
}

// matchZeroOrMorePat matches pattern zero or more times
func matchZeroOrMorePat(zero *ZeroOrMorePat, buffer *TokenBuffer, defines map[string]*rng.Define, ctx *validationContext) MatchResult {
	for {
		mark := buffer.Mark()
		result := MatchPattern(zero.Child, buffer, defines, ctx)
		if !result.Success {
			buffer.Reset(mark)
			break
		}
		// Check if we consumed any tokens - if not, break to avoid infinite loop
		if buffer.Mark() == mark {
			// Pattern matched but didn't consume tokens (e.g., EmptyPat, OptionalPat)
			break
		}
	}
	return MatchResult{Success: true}
}

// matchRefPat resolves and matches a define reference
// matchRefPatChoice handles matching when ref defines a choice pattern
func matchRefPatChoice(define *rng.Define, buffer *TokenBuffer, defines map[string]*rng.Define, ctx *validationContext) (MatchResult, bool) {
	if define.Choice == nil {
		return MatchResult{}, false
	}
	patterns, err := buildPatternsFromChoice(define.Choice, defines)
	if err != nil {
		return MatchResult{Success: false, Error: fmt.Sprintf("error building choice pattern: %v", err)}, true
	}
	choicePat := &ChoicePat{Alternatives: patterns}
	return MatchPattern(choicePat, buffer, defines, ctx), true
}

// matchRefPatInterleave handles matching when ref defines an interleave pattern
func matchRefPatInterleave(define *rng.Define, buffer *TokenBuffer, defines map[string]*rng.Define, ctx *validationContext) (MatchResult, bool) {
	if len(define.Interleave) == 0 {
		return MatchResult{}, false
	}
	patterns, err := buildPatternsFromInterleaves(define.Interleave, defines)
	if err != nil {
		return MatchResult{Success: false, Error: fmt.Sprintf("error building interleave pattern: %v", err)}, true
	}
	interleavePat := &InterleavePat{Children: patterns}
	return MatchPattern(interleavePat, buffer, defines, ctx), true
}

// matchRefPatGroup handles matching when ref defines group patterns
func matchRefPatGroup(define *rng.Define, buffer *TokenBuffer, defines map[string]*rng.Define, ctx *validationContext) (MatchResult, bool) {
	if len(define.Group) == 0 {
		return MatchResult{}, false
	}
	patterns, err := buildPatternsFromGroups(define.Group, defines)
	if err != nil {
		return MatchResult{Success: false, Error: fmt.Sprintf("error building group pattern: %v", err)}, true
	}
	var groupPat Pattern
	switch {
	case len(patterns) == 1:
		groupPat = patterns[0]
	case len(patterns) > 1:
		groupPat = &GroupPat{Children: patterns}
	default:
		groupPat = &EmptyPat{}
	}
	return MatchPattern(groupPat, buffer, defines, ctx), true
}

// matchRefPatMultipleElements handles matching when ref defines multiple elements
func matchRefPatMultipleElements(define *rng.Define, buffer *TokenBuffer, defines map[string]*rng.Define, ctx *validationContext) (MatchResult, bool) {
	if len(define.Elements) <= 1 {
		return MatchResult{}, false
	}
	patterns := make([]Pattern, 0, len(define.Elements))
	for _, elem := range define.Elements {
		pattern, err := BuildPatternFromElement(&elem, defines)
		if err != nil {
			return MatchResult{Success: false, Error: fmt.Sprintf("error building element pattern: %v", err)}, true
		}
		patterns = append(patterns, pattern)
	}
	groupPat := &GroupPat{Children: patterns}
	return MatchPattern(groupPat, buffer, defines, ctx), true
}

// matchRefPatSingleElement handles matching when ref defines a single element
func matchRefPatSingleElement(define *rng.Define, buffer *TokenBuffer, defines map[string]*rng.Define, ctx *validationContext) (MatchResult, bool) {
	if define.FirstElement() == nil {
		return MatchResult{}, false
	}
	pattern, err := BuildPatternFromElement(define.FirstElement(), defines)
	if err != nil {
		return MatchResult{Success: false, Error: fmt.Sprintf("error building element pattern from ref: %v", err)}, true
	}
	return MatchPattern(pattern, buffer, defines, ctx), true
}

func matchRefPat(ref *RefPat, buffer *TokenBuffer, defines map[string]*rng.Define, ctx *validationContext) MatchResult {
	define := defines[ref.Name]
	if define == nil {
		return MatchResult{Success: false, Error: fmt.Sprintf("undefined reference '%s'", ref.Name)}
	}

	// Try each pattern type in order
	if result, matched := matchRefPatChoice(define, buffer, defines, ctx); matched {
		return result
	}
	if result, matched := matchRefPatInterleave(define, buffer, defines, ctx); matched {
		return result
	}
	if result, matched := matchRefPatGroup(define, buffer, defines, ctx); matched {
		return result
	}
	if result, matched := matchRefPatMultipleElements(define, buffer, defines, ctx); matched {
		return result
	}
	if result, matched := matchRefPatSingleElement(define, buffer, defines, ctx); matched {
		return result
	}

	return MatchResult{Success: false, Error: fmt.Sprintf("ref '%s' not matched", ref.Name)}
}

// matchElementPat matches an element with given name and validates its content
// collectElementContent reads all tokens until the matching end element
func collectElementContent(buffer *TokenBuffer) ([]xml.Token, error) {
	var contentTokens []xml.Token
	depth := 1
	for depth > 0 {
		tok, ok := buffer.Next()
		if !ok {
			return nil, fmt.Errorf("unexpected end of input while reading element")
		}

		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			contentTokens = append(contentTokens, t)
		case xml.EndElement:
			depth--
			if depth > 0 {
				contentTokens = append(contentTokens, t)
			}
		default:
			contentTokens = append(contentTokens, t)
		}
	}
	return contentTokens, nil
}

// validateElementChildren validates all child patterns against content
func validateElementChildren(elem *ElementPat, contentBuffer *TokenBuffer, defines map[string]*rng.Define, ctx *validationContext) MatchResult {
	for _, child := range elem.Children {
		result := MatchPattern(child, contentBuffer, defines, ctx)
		if !result.Success {
			return result
		}
	}
	return MatchResult{Success: true}
}

func matchElementPat(elem *ElementPat, buffer *TokenBuffer, defines map[string]*rng.Define, ctx *validationContext) MatchResult {
	tok, ok := buffer.Peek()
	if !ok {
		return MatchResult{Success: false, Error: "expected element"}
	}

	start, ok := tok.(xml.StartElement)
	if !ok {
		return MatchResult{Success: false, Error: "expected StartElement"}
	}

	// Check element name
	if elem.Name != "" && elem.Name != start.Name.Local {
		return MatchResult{
			Success: false,
			Error:   fmt.Sprintf("expected element '%s', got '%s'", elem.Name, start.Name.Local),
		}
	}

	buffer.Next() // Consume the start element

	// Collect content tokens until matching end element
	contentTokens, err := collectElementContent(buffer)
	if err != nil {
		return MatchResult{Success: false, Error: err.Error()}
	}

	// If no children patterns, we're done
	if len(elem.Children) == 0 {
		return MatchResult{Success: true}
	}

	// Create a new buffer with content tokens
	contentBuffer := &TokenBuffer{tokens: contentTokens, pos: 0}

	// Validate children patterns against content
	result := validateElementChildren(elem, contentBuffer, defines, ctx)
	if !result.Success {
		return result
	}

	// Check that all content was consumed
	if !contentBuffer.IsEmpty() {
		tok, _ := contentBuffer.Peek()
		return MatchResult{
			Success: false,
			Error:   fmt.Sprintf("unexpected content in element: %T", tok),
		}
	}

	return MatchResult{Success: true}
}

// matchTextPat matches text content
func matchTextPat(buffer *TokenBuffer) MatchResult {
	// Text pattern accepts any amount of text content (CharData, processing instructions, etc.)
	// Per RELAX NG spec: <text/> matches any string of characters and processing instructions
	// Consume consecutive text tokens only - stops at first non-text element
	// This allows interleave matching to try different orderings
	for {
		tok, ok := buffer.Peek()
		if !ok {
			// No more tokens - match succeeded (may have matched nothing)
			return MatchResult{Success: true}
		}

		// CharData and Comments are consumed
		if _, ok := tok.(xml.CharData); ok {
			buffer.Next() // Consume it
			continue
		}
		if _, ok := tok.(xml.Comment); ok {
			buffer.Next() // Consume it
			continue
		}
		// Processing instructions are also allowed in text content
		// (they're handled as tokens by xml.Decoder)

		// Any other token type (StartElement, EndElement) should not be consumed
		// Return success - TextPat matches zero or more text pieces
		return MatchResult{Success: true}
	}
}

// matchEmptyPat ensures no content
func matchEmptyPat(buffer *TokenBuffer) MatchResult {
	if !buffer.IsEmpty() {
		return MatchResult{Success: false, Error: "expected empty content"}
	}
	return MatchResult{Success: true}
}

// matchDataPat validates data type
func matchDataPat(data *DataPat, buffer *TokenBuffer, ctx *validationContext) MatchResult {
	// Read all text content using PeekRaw/NextRaw to preserve whitespace-only data
	var text bytes.Buffer
	for {
		tok, ok := buffer.PeekRaw()
		if !ok {
			break
		}
		if charData, ok := tok.(xml.CharData); ok {
			text.Write(charData)
			buffer.NextRaw()
		} else {
			break
		}
	}

	rawTextStr := text.String()
	textStr := strings.TrimSpace(rawTextStr)

	// For string types, preserve whitespace when validating
	// For other types, use trimmed text
	valueToValidate := textStr
	if data.Type == dataTypeString || data.Type == dataTypeNormalizedString {
		valueToValidate = rawTextStr
	}

	// Check except clause first - if value matches an excepted value, it's invalid
	if data.Except != nil {
		if result := checkDataExceptValues(textStr, data.Except); !result.Success {
			return result
		}
	}

	// Validate the data type AND facets (minLength, maxLength, pattern, etc.)
	if data.Type != "" && !ctx.validateDataTypeWithFacets(data.Type, valueToValidate, data.Params) {
		return MatchResult{
			Success: false,
			Error:   fmt.Sprintf("invalid data type (expected %s)", data.Type),
		}
	}

	return MatchResult{Success: true}
}

// handleValuePatNoToken extracts text from CharData when no next token is available
func handleValuePatNoToken(hasAnyCharData bool, firstCharData xml.CharData, value *ValuePat) (string, error) {
	if hasAnyCharData {
		// There was whitespace-only CharData - use it for matching
		return string(firstCharData), nil
	}

	// Truly empty content - check if any allowed value is empty string
	for _, val := range value.Values {
		if val.Value == "" {
			return "", nil
		}
	}
	return "", fmt.Errorf("expected text content")
}

// checkDataExceptValues validates that a value is not in the except list
func checkDataExceptValues(textStr string, except *rng.DataExcept) MatchResult {
	// Check direct values
	if len(except.Values) > 0 {
		for _, exceptVal := range except.Values {
			if textStr == strings.TrimSpace(exceptVal.Value) {
				return MatchResult{
					Success: false,
					Error:   fmt.Sprintf("value '%s' is not allowed (in except list)", textStr),
				}
			}
		}
	}

	// Check values within choice
	if except.Choice != nil && len(except.Choice.Values) > 0 {
		for _, exceptVal := range except.Choice.Values {
			if textStr == strings.TrimSpace(exceptVal.Value) {
				return MatchResult{
					Success: false,
					Error:   fmt.Sprintf("value '%s' is not allowed (in except list)", textStr),
				}
			}
		}
	}
	return MatchResult{Success: true}
}

// matchValuePat validates literal value with type-specific whitespace handling
func matchValuePat(value *ValuePat, buffer *TokenBuffer, _ *validationContext) MatchResult {
	textStr, err := extractTextForValuePat(value, buffer)
	if err != nil {
		return MatchResult{Success: false, Error: err.Error()}
	}

	// Check against each allowed value
	for _, val := range value.Values {
		if matchesValuePattern(textStr, val) {
			return MatchResult{Success: true}
		}
	}

	// No match found
	return buildValuePatErrorResult(textStr, value)
}

func extractTextForValuePat(value *ValuePat, buffer *TokenBuffer) (string, error) {
	// Check if there's any CharData content (including whitespace-only)
	hasAnyCharData, firstCharData := scanForCharData(buffer)

	// Read only the next CharData token (not all remaining text)
	tok, ok := buffer.Next()

	if !ok {
		// No non-whitespace token - handle whitespace or empty content
		return handleValuePatNoToken(hasAnyCharData, firstCharData, value)
	}

	// We got a non-whitespace CharData token
	charData, ok := tok.(xml.CharData)
	if !ok {
		return "", fmt.Errorf("expected text but got %T", tok)
	}
	return string(charData), nil
}

func scanForCharData(buffer *TokenBuffer) (bool, xml.CharData) {
	pos := buffer.pos
	for pos < len(buffer.tokens) {
		tok := buffer.tokens[pos]
		if charData, ok := tok.(xml.CharData); ok {
			return true, charData
		} else if _, ok := tok.(xml.Comment); !ok {
			break
		}
		pos++
	}
	return false, nil
}

func matchesValuePattern(textStr string, val rng.Value) bool {
	valueType := val.Type
	if valueType == "" {
		valueType = dataTypeToken
	}

	textToCompare, valueToCompare := normalizeForValueType(textStr, val.Value, valueType)
	return textToCompare == valueToCompare
}

func normalizeForValueType(text, value, valueType string) (string, string) {
	if valueType == dataTypeString {
		return text, value
	}
	return normalizeTokenValue(text), normalizeTokenValue(value)
}

func buildValuePatErrorResult(textStr string, value *ValuePat) MatchResult {
	allowedValues := make([]string, 0, len(value.Values))
	for _, val := range value.Values {
		allowedValues = append(allowedValues, fmt.Sprintf("'%s'", val.Value))
	}
	return MatchResult{
		Success: false,
		Error:   fmt.Sprintf("text '%s' does not match any allowed value: %s", textStr, strings.Join(allowedValues, ", ")),
	}
}

// matchInterleavePat matches patterns in any order
// This uses a backtracking algorithm to try all possible assignments
// Special handling: GroupPat patterns are matched partially - only one child at a time
// across multiple elements, while maintaining sequence order within the group
func matchInterleavePat(interleave *InterleavePat, buffer *TokenBuffer, defines map[string]*rng.Define, ctx *validationContext) MatchResult {
	if len(interleave.Children) == 0 {
		return MatchResult{Success: true}
	}

	// Track which patterns have been matched (for non-group patterns)
	// For group patterns, we track which child of the group has been matched
	matched := make([]int, len(interleave.Children)) // 0 = not matched, 1+ = group index, -1 = fully matched

	// Collect all tokens until we run out
	var tokens []xml.Token
	for {
		tok, ok := buffer.Peek()
		if !ok {
			break
		}
		tokens = append(tokens, tok)
		buffer.Next()
	}

	// Try to match tokens against patterns in any order
	result := matchInterleaveRecursiveWithGroups(interleave.Children, tokens, matched, 0, defines, ctx)

	// If successful, we're done (tokens already consumed)
	// If failed, we need to put tokens back
	if !result.Success {
		// Put all tokens back in reverse order
		for i := len(tokens) - 1; i >= 0; i-- {
			buffer.tokens = append([]xml.Token{tokens[i]}, buffer.tokens...)
			buffer.pos = 0
		}
	}

	return result
}

// tryMatchTextPatInInterleave attempts to match a TextPat against non-whitespace CharData in interleave
func tryMatchTextPatInInterleave(patterns []Pattern, tokens []xml.Token, matched []int, tokenIndex int, defines map[string]*rng.Define, ctx *validationContext) MatchResult {
	for i, pat := range patterns {
		if _, isTextPat := pat.(*TextPat); isTextPat {
			// TextPat can consume this text
			tempBuffer := &TokenBuffer{tokens: tokens[tokenIndex:], pos: 0}
			result := MatchPattern(pat, tempBuffer, defines, ctx)
			if result.Success {
				// TextPat consumed tokens - mark as matched for tracking
				// Then continue with remaining tokens
				// NOTE: We don't require TextPat to be fully matched at the end
				oldMatched := matched[i]
				matched[i] = -1
				result = matchInterleaveRecursiveWithGroups(patterns, tokens, matched, tokenIndex+tempBuffer.pos, defines, ctx)
				if result.Success {
					return result
				}
				// Backtrack
				matched[i] = oldMatched
			}
			break // Only one TextPat per interleave expected
		}
	}
	return MatchResult{Success: false}
}

// checkInterleavePatternCompletion validates that all required patterns are fully matched when tokens are exhausted
func checkInterleavePatternCompletion(patterns []Pattern, matched []int) MatchResult {
	for i, pat := range patterns {
		// For groups: check if all children have been matched and mark as fully matched
		if grp, ok := pat.(*GroupPat); ok && matched[i] == len(grp.Children) {
			matched[i] = -1
		}

		if matched[i] != -1 {
			// Pattern not fully matched
			// TextPat is always optional in interleave (it matches zero or more text sequences)
			if _, isTextPat := pat.(*TextPat); isTextPat {
				continue // TextPat is never required
			}

			// For other patterns: they must be optional
			if !isOptionalPattern(pat) && matched[i] == 0 {
				return MatchResult{
					Success: false,
					Error:   fmt.Sprintf("required pattern not matched in interleave: %v", pat.Kind()),
				}
			}
			// For partially matched groups (shouldn't happen, but check anyway)
			if grp, ok := pat.(*GroupPat); ok && matched[i] > 0 && matched[i] < len(grp.Children) {
				return MatchResult{
					Success: false,
					Error:   fmt.Sprintf("group pattern in interleave not fully matched (%d/%d children matched)", matched[i], len(grp.Children)),
				}
			}
		}
	}
	return MatchResult{Success: true}
}

// matchInterleaveRecursiveWithGroups tries to match remaining tokens against patterns
// Handles GroupPat specially by matching one child at a time in order
// tryMatchNonTextPatterns tries to match non-text patterns in interleave
func tryMatchNonTextPatterns(patterns []Pattern, tokens []xml.Token, matched []int, tokenIndex int, defines map[string]*rng.Define, ctx *validationContext) MatchResult {
	for i, pattern := range patterns {
		// Skip fully matched patterns
		if matched[i] == -1 {
			continue
		}

		// Skip TextPat for now - handle it in second pass
		if _, isTextPat := pattern.(*TextPat); isTextPat {
			continue
		}

		// For GroupPat: try to match the next child in sequence
		if grp, ok := pattern.(*GroupPat); ok {
			nextChildIndex := matched[i] // This is the child index (0-based)
			if nextChildIndex >= len(grp.Children) {
				// All children of this group have been matched
				matched[i] = -1
				continue
			}

			// Try to match the next child of the group
			tempBuffer := &TokenBuffer{tokens: tokens[tokenIndex:], pos: 0}
			result := MatchPattern(grp.Children[nextChildIndex], tempBuffer, defines, ctx)
			if result.Success {
				// Mark that we've matched one more child of this group
				matched[i]++
				consumed := tempBuffer.pos

				// Continue with remaining tokens
				result = matchInterleaveRecursiveWithGroups(patterns, tokens, matched, tokenIndex+consumed, defines, ctx)
				if result.Success {
					return result
				}

				// Backtrack
				matched[i]--
			}
			continue
		}

		// For non-group patterns: normal matching (only once)
		if matched[i] != 0 {
			continue
		}

		// Create a temporary buffer with remaining tokens
		tempBuffer := &TokenBuffer{tokens: tokens[tokenIndex:], pos: 0}

		// Try to match this pattern
		result := MatchPattern(pattern, tempBuffer, defines, ctx)
		if result.Success {
			// Calculate how many tokens were consumed
			consumed := tempBuffer.pos

			// Mark pattern as matched
			matched[i] = -1

			// Continue with remaining tokens
			result = matchInterleaveRecursiveWithGroups(patterns, tokens, matched, tokenIndex+consumed, defines, ctx)

			if result.Success {
				return result
			}

			// Backtrack
			matched[i] = 0
		}
	}
	return MatchResult{Success: false} // No non-text pattern matched
}

// tryMatchTextPatterns tries to match text patterns in interleave
func tryMatchTextPatterns(patterns []Pattern, tokens []xml.Token, matched []int, tokenIndex int, defines map[string]*rng.Define, ctx *validationContext) MatchResult {
	for i, pattern := range patterns {
		// Skip fully matched patterns
		if matched[i] == -1 {
			continue
		}

		// Only process TextPat in this pass
		if _, isTextPat := pattern.(*TextPat); !isTextPat {
			continue
		}

		// For TextPat: don't require matched[i] == 0, allow it to match multiple times
		// But only try once per recursion level
		if matched[i] != 0 {
			continue
		}

		// Create a temporary buffer with remaining tokens
		tempBuffer := &TokenBuffer{tokens: tokens[tokenIndex:], pos: 0}

		// Try to match this TextPat
		result := MatchPattern(pattern, tempBuffer, defines, ctx)
		if result.Success {
			// Calculate how many tokens were consumed
			consumed := tempBuffer.pos

			// For TextPat, mark as "seen" (value 1) but not "fully matched" (-1)
			// This allows it to be tried again at different positions
			matched[i] = 1

			// Continue with remaining tokens
			result = matchInterleaveRecursiveWithGroups(patterns, tokens, matched, tokenIndex+consumed, defines, ctx)

			if result.Success {
				// At end of recursion, mark as fully matched
				matched[i] = -1
				return result
			}

			// Backtrack - reset to 0 so it can be tried again
			matched[i] = 0
		}
	}
	return MatchResult{Success: false} // No text pattern matched
}

func matchInterleaveRecursiveWithGroups(patterns []Pattern, tokens []xml.Token, matched []int, tokenIndex int, defines map[string]*rng.Define, ctx *validationContext) MatchResult {
	// Base case: all tokens consumed
	if tokenIndex >= len(tokens) {
		return checkInterleavePatternCompletion(patterns, matched)
	}

	// Skip whitespace
	if charData, ok := tokens[tokenIndex].(xml.CharData); ok {
		if len(bytes.TrimSpace(charData)) == 0 {
			return matchInterleaveRecursiveWithGroups(patterns, tokens, matched, tokenIndex+1, defines, ctx)
		}
	}

	// SPECIAL CASE: If current token is non-whitespace CharData, check if we have a TextPat
	// If yes, let it consume the text without matching it (TextPat is always optional in interleave)
	if charData, ok := tokens[tokenIndex].(xml.CharData); ok && len(bytes.TrimSpace(charData)) > 0 {
		result := tryMatchTextPatInInterleave(patterns, tokens, matched, tokenIndex, defines, ctx)
		if result.Success {
			return result
		}
	}

	// Try non-text patterns first
	result := tryMatchNonTextPatterns(patterns, tokens, matched, tokenIndex, defines, ctx)
	if result.Success {
		return result
	}

	// Try text patterns second
	result = tryMatchTextPatterns(patterns, tokens, matched, tokenIndex, defines, ctx)
	if result.Success {
		return result
	}

	// No pattern matched this token
	return MatchResult{
		Success: false,
		Error:   fmt.Sprintf("no interleave pattern matched token at position %d", tokenIndex),
	}
}

// isOptionalPattern checks if a pattern is optional
func isOptionalPattern(p Pattern) bool {
	switch p.(type) {
	case *OptionalPat, *ZeroOrMorePat, *EmptyPat, *TextPat, *AnyContentPat:
		return true
	default:
		return false
	}
}

// matchMixedPat matches mixed content (text and elements)
func matchMixedPat(mixed *MixedPat, buffer *TokenBuffer, defines map[string]*rng.Define, ctx *validationContext) MatchResult {
	// Mixed content allows text interspersed with elements
	// Text tokens can appear anywhere in mixed content

	if groupPat, ok := mixed.Child.(*GroupPat); ok {
		return matchMixedGroup(groupPat, buffer, defines, ctx)
	}

	return matchMixedNonGroup(mixed, buffer, defines, ctx)
}

// matchMixedGroup matches a group pattern in mixed content
func matchMixedGroup(groupPat *GroupPat, buffer *TokenBuffer, defines map[string]*rng.Define, ctx *validationContext) MatchResult {
	// Match the group but skip CharData tokens
	for _, child := range groupPat.Children {
		// Skip text tokens
		skipMixedTextTokens(buffer)

		// Now match the child pattern
		result := MatchPattern(child, buffer, defines, ctx)
		if !result.Success {
			return result
		}
	}

	// After matching all children, skip any trailing text
	skipMixedTextTokens(buffer)
	return MatchResult{Success: true}
}

// matchMixedNonGroup matches a non-group pattern in mixed content
func matchMixedNonGroup(mixed *MixedPat, buffer *TokenBuffer, defines map[string]*rng.Define, ctx *validationContext) MatchResult {
	// For non-group children, skip leading text first
	skipMixedTextTokens(buffer)

	// Now match the child pattern
	result := MatchPattern(mixed.Child, buffer, defines, ctx)
	if !result.Success {
		return result
	}

	// After matching child, skip any trailing text
	skipMixedTextTokens(buffer)

	return MatchResult{Success: true}
}

// skipMixedTextTokens skips all text tokens (CharData) in mixed content
func skipMixedTextTokens(buffer *TokenBuffer) {
	for {
		tok, ok := buffer.Peek()
		if !ok {
			break
		}
		if _, ok := tok.(xml.CharData); ok {
			buffer.Next()
			continue
		}
		break
	}
}

// matchAnyContentPat matches any content (used when no content pattern is specified)
func matchAnyContentPat(buffer *TokenBuffer) MatchResult {
	// Consume all remaining tokens
	for !buffer.IsEmpty() {
		buffer.Next()
	}
	return MatchResult{Success: true}
}

// countSimplePatterns counts the number of simple patterns (DataPat, ValuePat) in a group
// This is used to determine the expected number of tokens in a list
func countSimplePatterns(group *GroupPat) int {
	count := 0
	for _, child := range group.Children {
		switch c := child.(type) {
		case *DataPat, *ValuePat:
			count++
		case *GroupPat:
			// Recursively count nested groups
			count += countSimplePatterns(c)
		}
		// Other pattern types (RefPat, ChoicePat, etc.) don't contribute to simple token count
	}
	return count
}

// matchListPat matches list pattern
// matchListEmptyContent handles list with zero tokens
func matchListEmptyContent(list *ListPat) MatchResult {
	switch list.Child.(type) {
	case *EmptyPat:
		return MatchResult{Success: true}
	case *OptionalPat:
		return MatchResult{Success: true}
	case *ZeroOrMorePat:
		return MatchResult{Success: true}
	case *NotAllowedPat:
		return MatchResult{Success: false, Error: "list cannot match notAllowed pattern"}
	case *MixedPat:
		return MatchResult{Success: true}
	default:
		return MatchResult{Success: false, Error: "list must contain at least one token for this pattern"}
	}
}

// matchListSingleTokenPatterns handles single-token patterns like DataPat or ValuePat
func matchListSingleTokenPatterns(list *ListPat, items []string, defines map[string]*rng.Define, ctx *validationContext) (MatchResult, bool) {
	// Check for DataPat or ValuePat first
	if _, isDataPat := list.Child.(*DataPat); isDataPat {
		if len(items) != 1 {
			return MatchResult{
				Success: false,
				Error:   fmt.Sprintf("list with single data pattern expects exactly 1 token, got %d", len(items)),
			}, true
		}
		buffer := &TokenBuffer{
			tokens: []xml.Token{xml.CharData(items[0])},
			pos:    0,
		}
		result := MatchPattern(list.Child, buffer, defines, ctx)
		return result, true
	}

	if _, isValuePat := list.Child.(*ValuePat); isValuePat {
		if len(items) != 1 {
			return MatchResult{
				Success: false,
				Error:   fmt.Sprintf("list with single value pattern expects exactly 1 token, got %d", len(items)),
			}, true
		}
		buffer := &TokenBuffer{
			tokens: []xml.Token{xml.CharData(items[0])},
			pos:    0,
		}
		result := MatchPattern(list.Child, buffer, defines, ctx)
		return result, true
	}

	return MatchResult{}, false
}

// validateListGroupPattern validates group patterns with expected token counts
func validateListGroupPattern(list *ListPat, items []string) MatchResult {
	if groupPat, isGroupPat := list.Child.(*GroupPat); isGroupPat {
		expectedTokens := countSimplePatterns(groupPat)
		if expectedTokens > 0 && len(items) != expectedTokens {
			return MatchResult{
				Success: false,
				Error:   fmt.Sprintf("list with group of %d simple patterns expects exactly %d tokens, got %d", expectedTokens, expectedTokens, len(items)),
			}
		}
		// Validation passed
		return MatchResult{Success: true}
	}
	// Not a group pattern, no validation needed
	return MatchResult{Success: true}
}

// readListContent reads all text tokens from buffer and splits on whitespace
func readListContent(buffer *TokenBuffer) []string {
	var text bytes.Buffer
	for {
		tok, ok := buffer.Peek()
		if !ok {
			break
		}
		if charData, ok := tok.(xml.CharData); ok {
			text.Write(charData)
			buffer.Next()
		} else {
			break
		}
	}
	return strings.Fields(text.String())
}

func matchListPat(list *ListPat, buffer *TokenBuffer, defines map[string]*rng.Define, ctx *validationContext) MatchResult {
	// Read all text content and split on whitespace
	items := readListContent(buffer)

	// If list content is empty, check what patterns accept zero tokens
	if len(items) == 0 {
		return matchListEmptyContent(list)
	}

	// Try single-token patterns first
	if result, matched := matchListSingleTokenPatterns(list, items, defines, ctx); matched {
		return result
	}

	// Create a buffer with all tokens (each token is a separate CharData)
	tokens := make([]xml.Token, 0, len(items))
	for _, item := range items {
		tokens = append(tokens, xml.CharData(item))
	}

	itemBuffer := &TokenBuffer{
		tokens: tokens,
		pos:    0,
	}

	// Validate group patterns if needed
	if groupResult := validateListGroupPattern(list, items); !groupResult.Success {
		return groupResult
	}

	// Match the pattern against the token buffer
	result := MatchPattern(list.Child, itemBuffer, defines, ctx)
	if !result.Success {
		return result
	}

	// Verify all tokens were consumed
	if !itemBuffer.IsEmpty() {
		return MatchResult{Success: false, Error: "list pattern did not consume all tokens"}
	}

	return MatchResult{Success: true}
}

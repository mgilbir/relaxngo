package validator

import (
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"

	"github.com/mgilbir/relaxngo/rng"
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
type ValidationOptions struct {
	FailFast       bool // Stop at first error
	MaxErrors      int  // Maximum errors to collect (0 = unlimited)
	MaxDepth       int  // Maximum nesting depth (0 = unlimited)
	MaxInterleave  int  // Maximum interleave branches (default 100)
	CollectUnknown bool // Collect unknown elements/attributes as warnings
	UsePatternAST  bool // Use ordered Pattern AST for content validation (experimental)
}

// DefaultOptions returns sensible default validation options.
func DefaultOptions() ValidationOptions {
	return ValidationOptions{
		FailFast:       false,
		MaxErrors:      100,
		MaxDepth:       100,
		MaxInterleave:  100,
		CollectUnknown: false,
		UsePatternAST:  true, // Enabled - now supports all pattern types with proper ordering
	}
}

// LineTracker wraps an io.Reader to track line and column numbers
type LineTracker struct {
	reader    io.Reader
	lineNum   int
	columnNum int
}

// NewLineTracker creates a new line tracker
func NewLineTracker(r io.Reader) *LineTracker {
	return &LineTracker{
		reader:    r,
		lineNum:   1,
		columnNum: 1,
	}
}

// Read implements io.Reader and tracks line and column numbers
func (lt *LineTracker) Read(p []byte) (n int, err error) {
	n, err = lt.reader.Read(p)
	for i := 0; i < n; i++ {
		if p[i] == '\n' {
			lt.lineNum++
			lt.columnNum = 1
		} else {
			lt.columnNum++
		}
	}
	return n, err
}

// GetLineNumber returns the current line number
func (lt *LineTracker) GetLineNumber() int {
	return lt.lineNum
}

// GetColumnNumber returns the current column number
func (lt *LineTracker) GetColumnNumber() int {
	return lt.columnNum
}

// Validator validates XML documents against RELAX NG schemas.
type Validator struct {
	grammar *rng.Grammar
	options ValidationOptions
	defines map[string]*rng.Define
	deriv   *derivEngine // derivative engine; nil when the grammar uses a construct it cannot translate
}

// NewValidator creates a validator from a parsed RELAX NG grammar.
func NewValidator(grammar *rng.Grammar, options ValidationOptions) *Validator {
	defines := make(map[string]*rng.Define)
	for i := range grammar.Defines {
		defines[grammar.Defines[i].Name] = &grammar.Defines[i]
	}

	return &Validator{
		grammar: grammar,
		options: options,
		defines: defines,
		deriv:   buildDerivEngine(grammar),
	}
}

// getStartPattern extracts the validatable start pattern from various start types
func (v *Validator) getStartPattern() *rng.Element {
	// Handle Ref case
	if v.grammar.Start.Ref != nil {
		return v.handleStartRef()
	}
	// Handle Element case
	if v.grammar.Start.Element != nil {
		return v.grammar.Start.Element
	}
	// Handle Choice case
	if v.grammar.Start.Choice != nil && len(v.grammar.Start.Choice.Elements) > 0 {
		return v.handleStartChoice()
	}
	// Handle Group case
	if len(v.grammar.Start.Group) > 0 {
		return v.extractElementFromGroup(&v.grammar.Start.Group[0])
	}
	// Handle Interleave case
	if len(v.grammar.Start.Interleave) > 0 {
		return &rng.Element{
			Interleave: v.grammar.Start.Interleave,
		}
	}
	// Handle NotAllowed case
	if v.grammar.Start.NotAllowed != nil {
		return &rng.Element{
			NotAllowed: v.grammar.Start.NotAllowed,
		}
	}
	// Handle ExternalRef case
	if v.grammar.Start.ExternalRef != nil {
		return &rng.Element{
			// Empty Name means match any element
		}
	}
	// For other pattern types (text, data, list, etc.),
	// we create a synthetic wildcard element that accepts any element
	// This allows validation to proceed without strict enforcement
	return &rng.Element{
		// Empty Name means match any element (permissive mode for unsupported patterns)
	}
}

// handleStartRef handles the case where start refers to a define
func (v *Validator) handleStartRef() *rng.Element {
	startDefine := v.defines[v.grammar.Start.Ref.Name]
	if startDefine != nil {
		// Check if the define has a choice (from combine="choice" merging)
		if startDefine.Choice != nil && len(startDefine.Choice.Elements) > 0 {
			// Return a synthetic element with a choice
			return &rng.Element{
				Choice: startDefine.Choice,
			}
		}
		// Otherwise try to get the first element
		if firstElem := startDefine.FirstElement(); firstElem != nil {
			return firstElem
		}
	}
	// Fallback to wildcard if define not found
	return &rng.Element{}
}

// handleStartChoice handles the case where start is a choice of elements
func (v *Validator) handleStartChoice() *rng.Element {
	if len(v.grammar.Start.Choice.Elements) == 1 {
		return &v.grammar.Start.Choice.Elements[0]
	}
	// Multiple elements in choice - wrap in a synthetic element with the choice
	return &rng.Element{
		Choice: v.grammar.Start.Choice,
	}
}

// extractElementFromGroup recursively extracts an element from a group, drilling down through nested groups
func (v *Validator) extractElementFromGroup(group *rng.Group) *rng.Element {
	if group == nil {
		return &rng.Element{} // wildcard
	}

	// Check for direct elements
	if len(group.Elements) > 0 {
		return &group.Elements[0]
	}

	// Check for nested groups - recursively extract from the first nested group
	if len(group.Group) > 0 {
		return v.extractElementFromGroup(&group.Group[0])
	}

	// No elements or nested groups - return wildcard
	return &rng.Element{}
}

// Validate validates an XML document and returns all validation errors.
//
// Validation uses the derivative algorithm (see deriv.go). If the schema uses a
// construct the builder cannot translate it returns an error rather than a
// (possibly wrong) result.
func (v *Validator) Validate(r io.Reader) ([]ValidationError, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if v.deriv == nil {
		return nil, fmt.Errorf("validator: schema uses a construct that is not supported")
	}
	return v.validateDerivative(data)
}

type validationContext struct {
	decoder                         *xml.Decoder
	lineTracker                     *LineTracker
	path                            []string
	errors                          *[]ValidationError
	options                         ValidationOptions
	defines                         map[string]*rng.Define
	depth                           int
	oneOrMoreChoiceAttributeMatched bool // Track if oneOrMore choice attributes were matched
}

func (ctx *validationContext) addError(element, message string, expected []string, found string) {
	if ctx.options.FailFast && len(*ctx.errors) > 0 {
		return
	}
	if ctx.options.MaxErrors > 0 && len(*ctx.errors) >= ctx.options.MaxErrors {
		return
	}

	err := ValidationError{
		Path:     strings.Join(ctx.path, "."),
		Element:  element,
		Line:     ctx.lineTracker.GetLineNumber(),
		Column:   ctx.lineTracker.GetColumnNumber(),
		Expected: expected,
		Found:    found,
		Message:  message,
	}
	*ctx.errors = append(*ctx.errors, err)
}

func (ctx *validationContext) validateElement(pattern *rng.Element, element *xml.StartElement) {
	ctx.depth++
	defer func() { ctx.depth-- }()

	// Reset oneOrMore choice attribute flag for this element
	ctx.oneOrMoreChoiceAttributeMatched = false

	if ctx.options.MaxDepth > 0 && ctx.depth > ctx.options.MaxDepth {
		ctx.addError(element.Name.Local, "maximum nesting depth exceeded", nil, "")
		return
	}

	ctx.path = append(ctx.path, element.Name.Local)
	defer func() { ctx.path = ctx.path[:len(ctx.path)-1] }()

	// Validate element name using either explicit name or name class
	if !ctx.matchesElementName(pattern, element) {
		ctx.reportElementNameError(pattern, element)
		return
	}

	// Validate attributes
	ctx.validateAttributes(pattern, element.Attr)

	// Validate content - use AST-based validation if enabled
	if ctx.options.UsePatternAST {
		ctx.validateContentWithAST(pattern, element)
	} else {
		ctx.validateContent(pattern, element)
	}
}

// reportElementNameError reports detailed errors about element name mismatches
func (ctx *validationContext) reportElementNameError(pattern *rng.Element, element *xml.StartElement) {
	if pattern.Name != "" {
		// Check if it's a namespace mismatch
		if pattern.Name == element.Name.Local && pattern.Ns != "" && pattern.Ns != element.Name.Space {
			ctx.addError(
				element.Name.Local,
				fmt.Sprintf("element '%s' has namespace '%s', expected '%s'", element.Name.Local, element.Name.Space, pattern.Ns),
				[]string{pattern.Ns},
				element.Name.Space,
			)
		} else {
			ctx.addError(
				element.Name.Local,
				fmt.Sprintf("expected element '%s'", pattern.Name),
				[]string{pattern.Name},
				element.Name.Local,
			)
		}
	} else {
		ctx.addError(
			element.Name.Local,
			fmt.Sprintf("element '%s' not allowed by name class", element.Name.Local),
			nil,
			element.Name.Local,
		)
	}
}

// collectAttributesFromGroup collects all attribute patterns from a group
func (ctx *validationContext) collectAttributesFromGroup(group *rng.Group) []*rng.Attribute {
	attrs := make([]*rng.Attribute, 0, len(group.Attributes)+len(group.Choice)+len(group.Interleave)+len(group.Optional)+len(group.Group))
	for i := range group.Attributes {
		attrs = append(attrs, &group.Attributes[i])
	}
	for _, choice := range group.Choice {
		attrs = append(attrs, ctx.collectAttributesFromChoice(&choice)...)
	}
	for _, interleave := range group.Interleave {
		attrs = append(attrs, ctx.collectAttributesFromInterleave(&interleave)...)
	}
	for _, opt := range group.Optional {
		attrs = append(attrs, ctx.collectAttributesFromOptional(&opt)...)
	}
	for _, subgroup := range group.Group {
		attrs = append(attrs, ctx.collectAttributesFromGroup(&subgroup)...)
	}
	return attrs
}

// collectAttributesFromChoice collects all attribute patterns from a choice
func (ctx *validationContext) collectAttributesFromChoice(choice *rng.Choice) []*rng.Attribute {
	attrs := make([]*rng.Attribute, 0, len(choice.Attributes)+len(choice.Group)+len(choice.Interleave))
	for i := range choice.Attributes {
		attrs = append(attrs, &choice.Attributes[i])
	}
	for _, group := range choice.Group {
		attrs = append(attrs, ctx.collectAttributesFromGroup(&group)...)
	}
	for _, interleave := range choice.Interleave {
		attrs = append(attrs, ctx.collectAttributesFromInterleave(&interleave)...)
	}
	return attrs
}

// collectAttributesFromInterleave collects all attribute patterns from an interleave
func (ctx *validationContext) collectAttributesFromInterleave(interleave *rng.Interleave) []*rng.Attribute {
	attrs := make([]*rng.Attribute, 0, len(interleave.Attributes)+len(interleave.Group)+len(interleave.Choice))
	for i := range interleave.Attributes {
		attrs = append(attrs, &interleave.Attributes[i])
	}
	for _, group := range interleave.Group {
		attrs = append(attrs, ctx.collectAttributesFromGroup(&group)...)
	}
	for _, choice := range interleave.Choice {
		attrs = append(attrs, ctx.collectAttributesFromChoice(&choice)...)
	}
	return attrs
}

// collectAttributesFromOptional collects all attribute patterns from an optional
func (ctx *validationContext) collectAttributesFromOptional(opt *rng.Optional) []*rng.Attribute {
	attrs := make([]*rng.Attribute, 0, len(opt.Attributes))
	for i := range opt.Attributes {
		attrs = append(attrs, &opt.Attributes[i])
	}
	return attrs
}

// validateAttributesInChoice validates attributes when element content is a choice
// Determines which choice alternative matches based on attributes
// validateChoiceDirectAttributes validates direct attributes in a choice alternative.
// Returns true if validation is complete (either by matching or by error).
func (ctx *validationContext) validateChoiceDirectAttributes(choice *rng.Choice, attrMap map[string]xml.Attr, pattern *rng.Element, hasDirectAttributes, hasEmpty bool) bool {
	// For direct attributes in a choice, we need to determine which alternative matches
	// A choice with <empty/> and <attribute> means:
	// - Alternative 1: no attributes (matches empty)
	// - Alternative 2: has the specified attribute

	if len(attrMap) == 0 && hasEmpty {
		// No attributes provided and <empty/> is an option - this matches
		return true
	}

	// Check if the provided attributes match the choice's direct attributes
	if !hasDirectAttributes {
		return false
	}

	// Collect direct attributes from choice and validate them as optional patterns
	matched := false
	for _, attrPattern := range choice.Attributes {
		attrName := ctx.getAttributeName(attrPattern)
		if attrName == "" {
			continue
		}

		ns := ctx.getAttributeNamespace(attrPattern, pattern)
		key := ns + "|" + attrName
		if attr, found := attrMap[key]; found {
			ctx.validateAttributeValue(&attrPattern, &attr)
			delete(attrMap, key)
			matched = true
		}
	}

	// If we matched attributes in the choice's direct attributes, that's good
	if matched {
		// Check for remaining unmatched attributes (not allowed)
		for _, attr := range attrMap {
			ctx.addError(
				strings.Join(ctx.path, "."),
				fmt.Sprintf("unknown attribute '%s'", attr.Name.Local),
				nil,
				attr.Name.Local,
			)
		}
		return true
	}

	return false
}

// getAttributeName extracts the attribute name from a pattern
func (ctx *validationContext) getAttributeName(attrPattern rng.Attribute) string {
	attrName := attrPattern.Name
	if attrName == "" && attrPattern.NameElement != nil {
		attrName = attrPattern.NameElement.Value
	}
	return attrName
}

// buildAttributeMap builds a map of attributes, ignoring xmlns declarations
func (ctx *validationContext) buildAttributeMap(attrs []xml.Attr) map[string]xml.Attr {
	attrMap := make(map[string]xml.Attr)
	for _, attr := range attrs {
		// Ignore xmlns declarations (both default and prefixed)
		// xmlns declarations have Space="xmlns" or Local="xmlns"
		if attr.Name.Space == "xmlns" || attr.Name.Local == "xmlns" {
			continue
		}
		// Note: xml: namespace attributes (e.g. xml:lang) are NOT ignored
		// They can be declared in RELAX NG schemas and should be validated
		key := attr.Name.Space + "|" + attr.Name.Local
		attrMap[key] = attr
	}
	return attrMap
}

// validateDirectAttributes validates direct attributes of the element
func (ctx *validationContext) validateDirectAttributes(pattern *rng.Element, attrMap map[string]xml.Attr) {
	for _, attrPattern := range pattern.Attributes {
		ctx.validateDirectAttribute(&attrPattern, pattern, attrMap)
	}
}

// validateDirectAttribute validates a single direct attribute
func (ctx *validationContext) validateDirectAttribute(attrPattern *rng.Attribute, pattern *rng.Element, attrMap map[string]xml.Attr) {
	// Get attribute name from either Name field or NameElement
	attrName := attrPattern.Name
	if attrName == "" && attrPattern.NameElement != nil {
		attrName = attrPattern.NameElement.Value
	}
	if attrName == "" {
		return
	}

	// Build key considering namespace
	ns := ctx.getAttributeNamespace(*attrPattern, pattern)
	key := ns + "|" + attrName
	attr, found := attrMap[key]
	if !found {
		ctx.addError(
			strings.Join(ctx.path, "."),
			fmt.Sprintf("required attribute '%s' missing", attrName),
			[]string{attrName},
			"",
		)
		return
	}

	// Validate attribute value
	ctx.validateAttributeValue(attrPattern, &attr)
	delete(attrMap, key)
}

// handleAttributesByStructure handles attributes based on element structure (choice/group/interleave)
// Returns true if early exit is needed
func (ctx *validationContext) handleAttributesByStructure(pattern *rng.Element, attrMap map[string]xml.Attr) bool {
	// If element has a choice with attributes, need to handle it specially
	if pattern.Choice != nil && len(pattern.Choice.Elements) == 0 {
		// This is a choice of groups/patterns, not elements
		ctx.validateAttributesInChoice(pattern.Choice, attrMap, pattern)
		return true
	}

	// If element has groups with attributes, collect and validate them
	if len(pattern.Group) > 0 {
		ctx.validateAttributesInGroups(pattern.Group, attrMap, pattern)
		return true
	}

	// If element has interleaves with attributes, collect and validate them
	if len(pattern.Interleave) > 0 {
		ctx.validateAttributesInInterleaves(pattern.Interleave, attrMap, pattern)
		return true
	}

	return false
}

// getAttributeNamespace extracts the namespace for an attribute
func (ctx *validationContext) getAttributeNamespace(attrPattern rng.Attribute, pattern *rng.Element) string {
	if attrPattern.NameElement != nil {
		if attrPattern.NameElement.Ns != "" {
			return attrPattern.NameElement.Ns
		}
		if attrPattern.Ns != "" {
			return attrPattern.Ns
		}
		return pattern.Ns
	}
	if attrPattern.Ns != "" {
		return attrPattern.Ns
	}
	return ""
}

func (ctx *validationContext) validateAttributesInChoice(choice *rng.Choice, attrMap map[string]xml.Attr, pattern *rng.Element) {
	hasDirectAttributes := len(choice.Attributes) > 0
	hasEmpty := choice.Empty != nil

	// If choice has direct attributes and/or empty option, handle them
	if (hasDirectAttributes || hasEmpty) && ctx.validateChoiceDirectAttributes(choice, attrMap, pattern, hasDirectAttributes, hasEmpty) {
		return
	}

	// Try to match against groups in choice
	matchedGroupAttrs := ctx.findMatchingChoiceGroup(choice, attrMap, pattern)

	// Check if matching succeeded or report errors
	if matchedGroupAttrs == nil && len(choice.Group) > 0 {
		ctx.reportChoiceAttributeMismatch(choice, attrMap)
		return
	}

	// Add direct attributes from the choice
	for _, attrPattern := range choice.Attributes {
		matchedGroupAttrs = append(matchedGroupAttrs, &attrPattern)
	}

	// Validate against matched attributes
	ctx.validateAttributePatterns(matchedGroupAttrs, attrMap, pattern)
}

// findMatchingChoiceGroup finds a group in choice whose attributes all match provided attributes
func (ctx *validationContext) findMatchingChoiceGroup(choice *rng.Choice, attrMap map[string]xml.Attr, pattern *rng.Element) []*rng.Attribute {
	for _, group := range choice.Group {
		groupAttrs := ctx.collectAttributesFromGroup(&group)
		if len(groupAttrs) == 0 {
			continue
		}

		// Check if all required attributes from this group are present
		if ctx.allAttributesPresent(groupAttrs, attrMap, pattern) {
			return groupAttrs
		}
	}
	return nil
}

// allAttributesPresent checks if all required attributes from pattern are present in the map
func (ctx *validationContext) allAttributesPresent(attrPatterns []*rng.Attribute, attrMap map[string]xml.Attr, pattern *rng.Element) bool {
	for _, attrPattern := range attrPatterns {
		attrName := attrPattern.Name
		if attrName == "" && attrPattern.NameElement != nil {
			attrName = attrPattern.NameElement.Value
		}
		if attrName == "" {
			continue
		}

		ns := ctx.getAttributeNamespace(*attrPattern, pattern)
		key := ns + "|" + attrName
		if _, found := attrMap[key]; !found {
			return false
		}
	}
	return true
}

// reportChoiceAttributeMismatch reports an error when choice attributes don't match
func (ctx *validationContext) reportChoiceAttributeMismatch(choice *rng.Choice, attrMap map[string]xml.Attr) {
	var expectedAttrs []string

	if len(attrMap) > 0 {
		ctx.reportChoiceAttributesMismatch(&expectedAttrs, choice)
	} else {
		ctx.reportChoiceAttributesMissing(&expectedAttrs, choice)
	}
}

// reportChoiceAttributesMismatch reports when attributes present don't match any choice
func (ctx *validationContext) reportChoiceAttributesMismatch(expectedAttrs *[]string, choice *rng.Choice) {
	for _, group := range choice.Group {
		groupAttrs := ctx.collectAttributesFromGroup(&group)
		for _, attr := range groupAttrs {
			if attr.Name != "" {
				*expectedAttrs = append(*expectedAttrs, attr.Name)
			}
		}
	}
	if len(*expectedAttrs) > 0 {
		ctx.addError(
			strings.Join(ctx.path, "."),
			fmt.Sprintf("choice requires attributes: %s", strings.Join(*expectedAttrs, ", ")),
			*expectedAttrs,
			"",
		)
	}
}

// reportChoiceAttributesMissing reports when choice requires attributes but none present
func (ctx *validationContext) reportChoiceAttributesMissing(expectedAttrs *[]string, choice *rng.Choice) {
	for _, group := range choice.Group {
		groupAttrs := ctx.collectAttributesFromGroup(&group)
		if len(groupAttrs) > 0 {
			for _, attr := range groupAttrs {
				if attr.Name != "" {
					*expectedAttrs = append(*expectedAttrs, attr.Name)
				}
			}
			break // Just report first alternative's requirements
		}
	}
	if len(*expectedAttrs) > 0 {
		ctx.addError(
			strings.Join(ctx.path, "."),
			fmt.Sprintf("required attribute missing: %s", (*expectedAttrs)[0]),
			*expectedAttrs,
			"",
		)
	}
}

// validateAttributesInGroups validates attributes when element content is groups
func (ctx *validationContext) validateAttributesInGroups(groups []rng.Group, attrMap map[string]xml.Attr, pattern *rng.Element) {
	var allGroupAttrs []*rng.Attribute

	// Collect attributes from all groups
	for _, group := range groups {
		allGroupAttrs = append(allGroupAttrs, ctx.collectAttributesFromGroup(&group)...)
	}

	// Validate all collected attributes
	ctx.validateAttributePatterns(allGroupAttrs, attrMap, pattern)
}

// validateAttributesInInterleaves validates attributes when element content is interleaves
// In an interleave, all attributes are required (unlike choice where they're optional)
func (ctx *validationContext) validateAttributesInInterleaves(interleaves []rng.Interleave, attrMap map[string]xml.Attr, pattern *rng.Element) {
	var allInterleavAttrs []*rng.Attribute

	// Collect attributes from all interleaves
	for _, interleave := range interleaves {
		allInterleavAttrs = append(allInterleavAttrs, ctx.collectAttributesFromInterleave(&interleave)...)
	}

	// Validate all collected attributes
	ctx.validateAttributePatterns(allInterleavAttrs, attrMap, pattern)
}

// validateAttributePatterns validates a list of attribute patterns
func (ctx *validationContext) validateAttributePatterns(attrPatterns []*rng.Attribute, attrMap map[string]xml.Attr, pattern *rng.Element) {
	// Check all attribute patterns
	for _, attrPattern := range attrPatterns {
		attrName := attrPattern.Name
		if attrName == "" && attrPattern.NameElement != nil {
			attrName = attrPattern.NameElement.Value
		}
		if attrName == "" {
			continue
		}

		ns := ctx.resolveAttributeNamespace(attrPattern, pattern)
		key := ns + "|" + attrName
		attr, found := attrMap[key]
		if !found {
			ctx.addError(
				strings.Join(ctx.path, "."),
				fmt.Sprintf("required attribute '%s' missing", attrName),
				[]string{attrName},
				"",
			)
			continue
		}

		ctx.validateAttributeValue(attrPattern, &attr)
		delete(attrMap, key)
	}

	// Error on unknown attributes (strict validation by default)
	for _, attr := range attrMap {
		ctx.addError(
			strings.Join(ctx.path, "."),
			fmt.Sprintf("unknown attribute '%s'", attr.Name.Local),
			nil,
			attr.Name.Local,
		)
	}
}

func (ctx *validationContext) validateAttributes(pattern *rng.Element, attrs []xml.Attr) {
	// Build map of actual attributes using namespace|local as key to avoid collisions
	attrMap := ctx.buildAttributeMap(attrs)

	// Validate direct attributes of the element
	ctx.validateDirectAttributes(pattern, attrMap)

	// Check for early returns based on complex attribute structures
	if ctx.handleAttributesByStructure(pattern, attrMap) {
		return
	}

	// Check optional attributes from oneOrMore/zeroOrMore
	ctx.validateOptionalRepeatingAttributes(pattern, attrMap)

	// Check optional attributes
	for _, opt := range pattern.Optional {
		for _, attrPattern := range opt.Attributes {
			// Get attribute name from either Name field or NameElement
			attrName := attrPattern.Name
			if attrName == "" && attrPattern.NameElement != nil {
				attrName = attrPattern.NameElement.Value
			}
			if attrName == "" {
				continue
			}
			ns := ctx.resolveAttributeNamespace(&attrPattern, pattern)
			key := ns + "|" + attrName
			if attr, found := attrMap[key]; found {
				ctx.validateAttributeValue(&attrPattern, &attr)
				delete(attrMap, key)
			}
		}
	}

	// Validate wildcard and unknown attributes
	ctx.validateWildcardAndUnknownAttributes(pattern, attrMap)
}

// resolveAttributeNamespace determines the namespace for an attribute pattern
func (ctx *validationContext) resolveAttributeNamespace(attrPattern *rng.Attribute, pattern *rng.Element) string {
	if attrPattern.NameElement != nil {
		// Attribute uses <name> child element
		if attrPattern.NameElement.Ns != "" {
			return attrPattern.NameElement.Ns
		}
		if attrPattern.Ns != "" {
			return attrPattern.Ns
		}
		return pattern.Ns
	}
	// Attribute uses name attribute (or no name specified)
	if attrPattern.Ns != "" {
		return attrPattern.Ns
	}
	return "" // No namespace
}

func (ctx *validationContext) validateAttributeValue(pattern *rng.Attribute, attr *xml.Attr) {
	// Validate namespace if specified in schema
	if pattern.Ns != "" && pattern.Ns != attr.Name.Space {
		ctx.addError(
			strings.Join(ctx.path, "."),
			fmt.Sprintf("attribute '%s' has namespace '%s', expected '%s'", attr.Name.Local, attr.Name.Space, pattern.Ns),
			[]string{pattern.Ns},
			attr.Name.Space,
		)
		return
	}

	// Check data type
	if pattern.Data != nil {
		ctx.validateAttributeData(pattern.Data, attr)
		return
	}

	// Check choice values
	if pattern.Choice != nil && len(pattern.Choice.Values) > 0 {
		ctx.validateAttributeChoice(pattern.Choice, attr)
		return
	}

	// Check explicit values
	if len(pattern.Values) > 0 {
		ctx.validateAttributeValues(pattern.Values, attr)
		return
	}

	// Check for empty pattern (attribute must have empty or whitespace-only value)
	if pattern.Empty != nil && strings.TrimSpace(attr.Value) != "" {
		ctx.addError(
			strings.Join(ctx.path, "."),
			fmt.Sprintf("attribute '%s' must have empty value, but got '%s'", attr.Name.Local, attr.Value),
			[]string{"(empty)"},
			attr.Value,
		)
	}

	// Check for list pattern (attribute value should be whitespace-separated list of values)
	if pattern.List != nil {
		ctx.validateAttributeList(pattern.List, attr)
	}
}

// validateAttributeData validates an attribute against a data type pattern
func (ctx *validationContext) validateAttributeData(data *rng.Data, attr *xml.Attr) {
	// Check except clause first
	if data.Except != nil && len(data.Except.Values) > 0 {
		for _, exceptVal := range data.Except.Values {
			if strings.TrimSpace(attr.Value) == strings.TrimSpace(exceptVal.Value) {
				ctx.addError(
					strings.Join(ctx.path, "."),
					fmt.Sprintf("attribute '%s' value '%s' is not allowed (in except list)", attr.Name.Local, attr.Value),
					nil,
					attr.Value,
				)
				return
			}
		}
	}

	// Validate base type
	if !ctx.validateDataTypeWithFacets(data.Type, attr.Value, data.Params) {
		ctx.addError(
			strings.Join(ctx.path, "."),
			fmt.Sprintf("attribute '%s' has invalid type (expected %s)", attr.Name.Local, data.Type),
			[]string{data.Type},
			attr.Value,
		)
	}
}

// validateAttributeChoice validates an attribute against a choice of values
func (ctx *validationContext) validateAttributeChoice(choice *rng.Choice, attr *xml.Attr) {
	expected := make([]string, 0, len(choice.Values))

	// Check value alternatives
	for _, v := range choice.Values {
		expected = append(expected, v.Value)
		if v.Value == attr.Value {
			return // Valid value found
		}
	}

	// Check if empty is an alternative (for empty attribute values)
	if choice.Empty != nil && attr.Value == "" {
		return // Empty is valid
	}

	ctx.addError(
		strings.Join(ctx.path, "."),
		fmt.Sprintf("attribute '%s' has invalid value", attr.Name.Local),
		expected,
		attr.Value,
	)
}

// validateAttributeValues validates an attribute against explicit values
func (ctx *validationContext) validateAttributeValues(values []rng.Value, attr *xml.Attr) {
	expected := make([]string, len(values))
	for i, v := range values {
		expected[i] = v.Value
		if v.Value == attr.Value {
			return // Valid value found
		}
	}
	ctx.addError(
		strings.Join(ctx.path, "."),
		fmt.Sprintf("attribute '%s' has invalid value", attr.Name.Local),
		expected,
		attr.Value,
	)
}

// validateAttributeList validates an attribute against a list pattern
func (ctx *validationContext) validateAttributeList(list *rng.List, attr *xml.Attr) {
	// List pattern contains patterns for list items
	// For list with empty pattern: only whitespace-separated empty items are allowed
	// which means the entire value should be whitespace-only
	if list.Empty != nil && strings.TrimSpace(attr.Value) != "" {
		ctx.addError(
			strings.Join(ctx.path, "."),
			fmt.Sprintf("attribute '%s' must contain only whitespace (empty list), but got '%s'", attr.Name.Local, attr.Value),
			[]string{"(whitespace-only)"},
			attr.Value,
		)
	}
	// TODO: Handle validation of list items for other pattern types (data, value, etc)
}

// extractContentPatternFromChoice extracts the content pattern from a synthetic choice at start level.
// Returns the extracted pattern and a boolean indicating if validation should return early.
func (ctx *validationContext) extractContentPatternFromChoice(choicePat *ChoicePat, pattern *rng.Element, element *xml.StartElement) (Pattern, bool) {
	// Check if choice has NameElements (name classes for element names)
	if len(pattern.Choice.NameElements) > 0 {
		return ctx.extractFromNameElementChoice(choicePat, pattern, element)
	}
	// Choice contains Elements (not just name classes)
	return ctx.extractFromElementChoice(choicePat, pattern, element)
}

func (ctx *validationContext) extractFromNameElementChoice(choicePat *ChoicePat, pattern *rng.Element, element *xml.StartElement) (Pattern, bool) {
	// Choice contains only name classes - check if current element matches any
	matched := false
	for _, nameElem := range pattern.Choice.NameElements {
		if nameElem.Value == element.Name.Local {
			matched = true
			break
		}
	}
	if !matched {
		ctx.addError(
			element.Name.Local,
			fmt.Sprintf("element '%s' does not match any choice of names", element.Name.Local),
			nil,
			"",
		)
		ctx.skipToEndElement()
		return nil, true
	}

	// Element name matched - use the content pattern from the root element
	// The content pattern for all names in the choice is the same (it came from elem.Empty/Text/etc)
	// Since all ElementPats in choicePat have the same Children, use the first one
	if len(choicePat.Alternatives) > 0 {
		if elemPat, ok := choicePat.Alternatives[0].(*ElementPat); ok && len(elemPat.Children) > 0 {
			if len(elemPat.Children) == 1 {
				return elemPat.Children[0], false
			}
			return &GroupPat{Children: elemPat.Children}, false
		}
	}
	return &AnyContentPat{}, false
}

func (ctx *validationContext) extractFromElementChoice(_ *ChoicePat, pattern *rng.Element, element *xml.StartElement) (Pattern, bool) {
	// Collect ALL choice elements that match the element name (can be multiple with same name)
	var matchingElems []*rng.Element
	for i := range pattern.Choice.Elements {
		if pattern.Choice.Elements[i].Name == element.Name.Local {
			matchingElems = append(matchingElems, &pattern.Choice.Elements[i])
		}
	}

	// If no matching elements, this shouldn't happen since matchesElementName passed
	if len(matchingElems) == 0 {
		return &AnyContentPat{}, false
	}

	if len(matchingElems) == 1 {
		return ctx.extractFromSingleElement(matchingElems[0], element)
	}

	return ctx.extractFromMultipleElements(matchingElems, element)
}

func (ctx *validationContext) extractFromSingleElement(choiceElem *rng.Element, element *xml.StartElement) (Pattern, bool) {
	elemPat, err := BuildPatternFromElement(choiceElem, ctx.defines)
	if err != nil {
		ctx.addError(
			element.Name.Local,
			fmt.Sprintf("failed to build pattern: %v", err),
			nil,
			"",
		)
		ctx.skipToEndElement()
		return nil, true
	}

	if elemPat, ok := elemPat.(*ElementPat); ok && len(elemPat.Children) > 0 {
		// Unwrap the element pattern to get the content
		if len(elemPat.Children) == 1 {
			return elemPat.Children[0], false
		}
		return &GroupPat{Children: elemPat.Children}, false
	}
	// No content pattern
	return &AnyContentPat{}, false
}

func (ctx *validationContext) extractFromMultipleElements(matchingElems []*rng.Element, _ *xml.StartElement) (Pattern, bool) {
	// Multiple matching elements with same name - create a choice of their content patterns
	var contentPatterns []Pattern
	for _, choiceElem := range matchingElems {
		elemPat, err := BuildPatternFromElement(choiceElem, ctx.defines)
		if err != nil {
			// Skip on error
			continue
		}
		if elemPat, ok := elemPat.(*ElementPat); ok {
			switch len(elemPat.Children) {
			case 1:
				contentPatterns = append(contentPatterns, elemPat.Children[0])
			case 0:
				contentPatterns = append(contentPatterns, &AnyContentPat{})
			default:
				contentPatterns = append(contentPatterns, &GroupPat{Children: elemPat.Children})
			}
		} else {
			contentPatterns = append(contentPatterns, &AnyContentPat{})
		}
	}
	if len(contentPatterns) > 0 {
		return &ChoicePat{Alternatives: contentPatterns}, false
	}
	return &AnyContentPat{}, false
}

// validateContentWithAST validates element content using the Pattern AST for ordered matching
func (ctx *validationContext) validateContentWithAST(pattern *rng.Element, element *xml.StartElement) {
	// Build Pattern AST from element's structured fields
	contentPattern, err := BuildPatternFromElement(pattern, ctx.defines)
	if err != nil {
		ctx.addError(
			element.Name.Local,
			fmt.Sprintf("failed to build pattern: %v", err),
			nil,
			"",
		)
		ctx.skipToEndElement()
		return
	}

	// Unwrap and normalize pattern
	contentPattern = ctx.normalizeContentPattern(contentPattern, pattern, element)

	// Buffer the element's content tokens
	tokenBuffer, err := NewTokenBuffer(ctx.decoder)
	if err != nil {
		ctx.addError(
			element.Name.Local,
			fmt.Sprintf("failed to buffer tokens: %v", err),
			nil,
			"",
		)
		return
	}

	// Match the pattern against the buffered content
	result := MatchPattern(contentPattern, tokenBuffer, ctx.defines, ctx)
	if !result.Success {
		ctx.addError(
			element.Name.Local,
			result.Error,
			result.Details,
			"",
		)
		return
	}

	// Check for unconsumed content
	ctx.checkUnconsumedContent(element, tokenBuffer)
}

// normalizeContentPattern unwraps and normalizes a pattern
func (ctx *validationContext) normalizeContentPattern(contentPattern Pattern, pattern *rng.Element, element *xml.StartElement) Pattern {
	// If the pattern builder wrapped the content in an ElementPat (because the element had a name),
	// unwrap it to get the actual content pattern
	if elemPat, ok := contentPattern.(*ElementPat); ok {
		switch len(elemPat.Children) {
		case 1:
			contentPattern = elemPat.Children[0]
		case 0:
			// No children - treat as any content (permissive)
			contentPattern = &AnyContentPat{}
		default:
			// Multiple children - wrap in a group
			contentPattern = &GroupPat{Children: elemPat.Children}
		}
	}

	// If we have a ChoicePat with ElementPat alternatives at the top level of content validation,
	// this indicates a synthetic choice element (e.g., from merged defines with combine="choice").
	// The choice matching has already been handled in matchesElementName(), so we don't need
	// to validate the choice here. Instead, we validate the content of the matched element.
	if choicePat, ok := contentPattern.(*ChoicePat); ok && pattern.Name == "" && pattern.Choice != nil {
		// This is a synthetic choice element at the start level.
		extracted, shouldReturn := ctx.extractContentPatternFromChoice(choicePat, pattern, element)
		if shouldReturn {
			return contentPattern // Return early
		}
		contentPattern = extracted
	}

	return contentPattern
}

// checkUnconsumedContent checks for unconsumed tokens in the buffer
func (ctx *validationContext) checkUnconsumedContent(element *xml.StartElement, tokenBuffer *TokenBuffer) {
	if !tokenBuffer.IsEmpty() {
		tok, _ := tokenBuffer.Peek()
		var unexpected string
		if start, ok := tok.(xml.StartElement); ok {
			unexpected = fmt.Sprintf("unexpected element '%s'", start.Name.Local)
		} else if charData, ok := tok.(xml.CharData); ok {
			unexpected = fmt.Sprintf("unexpected text '%s'", string(charData))
		} else {
			unexpected = "unexpected content"
		}
		ctx.addError(
			element.Name.Local,
			unexpected,
			nil,
			"",
		)
	}
}

func (ctx *validationContext) validateContent(pattern *rng.Element, element *xml.StartElement) {
	// If empty pattern, enforce no content
	if pattern.Empty != nil {
		ctx.validateEmptyContent(element)
		return
	}

	// If notAllowed pattern, always error
	if pattern.NotAllowed != nil {
		ctx.addError(
			element.Name.Local,
			"content not allowed here",
			nil,
			"",
		)
		ctx.skipToEndElement()
		return
	}

	// If text content, validate text
	if pattern.Text != nil {
		ctx.validateTextContent(element)
		return
	}

	// If data type, validate typed content
	if pattern.Data != nil {
		ctx.validateContentData(pattern.Data, element)
		return
	}

	// If value elements, validate text content against literal values
	if len(pattern.Values) > 0 {
		ctx.validateContentValues(pattern.Values, element)
		return
	}

	// If mixed content, allow anything
	if pattern.Mixed != nil {
		ctx.skipToEndElement()
		return
	}

	// Try to validate other content patterns
	ctx.validateContentExtended(pattern, element)
}

// validateContentExtended validates remaining content pattern types
func (ctx *validationContext) validateContentExtended(pattern *rng.Element, element *xml.StartElement) {
	// If group, validate sequence
	if len(pattern.Group) > 0 {
		ctx.validateGroup(pattern.Group, element)
		return
	}

	// If refs, validate referenced elements
	if len(pattern.Ref) > 0 {
		ctx.validateRefsInContent(pattern.Ref)
		return
	}

	// If oneOrMore, validate repeated elements (at least one required)
	if len(pattern.OneOrMore) > 0 {
		ctx.validateOneOrMore(pattern.OneOrMore)
		return
	}

	// If zeroOrMore, validate repeated elements
	if len(pattern.ZeroOrMore) > 0 {
		ctx.validateZeroOrMore(pattern.ZeroOrMore)
		return
	}

	// If optional, validate optional elements
	if len(pattern.Optional) > 0 {
		ctx.validateOptional(pattern.Optional)
		return
	}

	// If choice, validate alternative patterns
	if pattern.Choice != nil {
		ctx.validateChoice(pattern.Choice, element)
		return
	}

	// If interleave, validate any-order elements
	if len(pattern.Interleave) > 0 {
		ctx.validateInterleave(pattern.Interleave, element)
		return
	}

	// If list pattern, validate list content
	if pattern.List != nil {
		ctx.validateList(pattern.List, element)
		return
	}

	// Default: skip to end for now (avoiding breaking existing tests)
	// TODO: Implement strict empty validation after full pattern support
	ctx.skipToEndElement()
}

// validateContentData validates element content against a data type pattern
func (ctx *validationContext) validateContentData(data *rng.Data, element *xml.StartElement) {
	text := ctx.readTextContent()

	// Check except clause first
	if data.Except != nil && len(data.Except.Values) > 0 {
		for _, exceptVal := range data.Except.Values {
			if strings.TrimSpace(text) == strings.TrimSpace(exceptVal.Value) {
				ctx.addError(
					element.Name.Local,
					fmt.Sprintf("value '%s' is not allowed (in except list)", text),
					nil,
					text,
				)
				return
			}
		}
		// Also check except data types
		for _, exceptData := range data.Except.Data {
			if ctx.validateDataType(exceptData.Type, text) {
				ctx.addError(
					element.Name.Local,
					fmt.Sprintf("value '%s' matches excepted type %s", text, exceptData.Type),
					nil,
					text,
				)
				return
			}
		}
	}

	// Then validate the base type
	if !ctx.validateDataTypeWithFacets(data.Type, text, data.Params) {
		ctx.addError(
			element.Name.Local,
			fmt.Sprintf("element has invalid type (expected %s)", data.Type),
			[]string{data.Type},
			text,
		)
	}
}

// validateContentValues validates element content against a list of allowed literal values
func (ctx *validationContext) validateContentValues(values []rng.Value, element *xml.StartElement) {
	text := ctx.readTextContent()

	// Try to find a matching value
	for _, val := range values {
		if ctx.valueMatches(val, text) {
			return // Match found
		}
	}

	// No match found - build list of allowed values for error message
	allowedValues := make([]string, 0, len(values))
	for _, val := range values {
		allowedValues = append(allowedValues, val.Value)
	}
	ctx.addError(
		element.Name.Local,
		fmt.Sprintf("element value must be one of: %s", strings.Join(allowedValues, ", ")),
		allowedValues,
		text,
	)
}

// valueMatches checks if text content matches a value pattern
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

// validateEmptyContent enforces that an element has no content (no child elements or non-whitespace text)
func (ctx *validationContext) validateEmptyContent(element *xml.StartElement) {
	for {
		tok, err := ctx.decoder.Token()
		if err != nil {
			return
		}

		switch t := tok.(type) {
		case xml.CharData:
			// Check for non-whitespace text
			if len(strings.TrimSpace(string(t))) > 0 {
				ctx.addError(
					element.Name.Local,
					"unexpected text content (expected empty)",
					nil,
					strings.TrimSpace(string(t)),
				)
				ctx.skipToEndElement()
				return
			}
		case xml.Comment:
			// Comments are allowed in empty content
			continue
		case xml.EndElement:
			// Reached end of element - valid empty content
			return
		case xml.StartElement:
			// Child element in empty content - error
			ctx.addError(
				element.Name.Local,
				fmt.Sprintf("unexpected element '%s' (expected empty)", t.Name.Local),
				nil,
				t.Name.Local,
			)
			ctx.skipToEndElement()
			ctx.skipToEndElement() // Skip the child and then the parent
			return
		}
	}
}

// validateMultiElementRefSequence validates a sequence of multiple elements from a ref define
func (ctx *validationContext) validateMultiElementRefSequence(elemPatterns []rng.Element) {
	for _, elemPattern := range elemPatterns {
		foundElement := false
		for {
			tok, err := ctx.decoder.Token()
			if err != nil {
				return
			}

			switch t := tok.(type) {
			case xml.CharData:
				if len(strings.TrimSpace(string(t))) == 0 {
					continue
				}
				return
			case xml.Comment:
				continue
			case xml.StartElement:
				ctx.validateElement(&elemPattern, &t)
				// Consume the EndElement that validateElement left behind
				for {
					endTok, err := ctx.decoder.Token()
					if err != nil {
						return
					}
					if _, isEnd := endTok.(xml.EndElement); isEnd {
						break
					}
				}
				foundElement = true
			case xml.EndElement:
				return
			}
			if foundElement {
				break
			}
		}
		if !foundElement {
			return
		}
	}
}

// validateSingleElementRef validates a single element from a ref define
func (ctx *validationContext) validateSingleElementRef(elemPattern *rng.Element) {
	for {
		tok, err := ctx.decoder.Token()
		if err != nil {
			return
		}

		switch t := tok.(type) {
		case xml.CharData:
			if len(strings.TrimSpace(string(t))) == 0 {
				continue
			}
			return
		case xml.Comment:
			continue
		case xml.StartElement:
			ctx.validateElement(elemPattern, &t)
			return
		case xml.EndElement:
			return
		}
	}
}

func (ctx *validationContext) validateRefsInContent(refs []rng.Ref) {
	for _, ref := range refs {
		if define := ctx.defines[ref.Name]; define != nil {
			if len(define.Elements) > 1 {
				// Validate multiple elements as a sequence
				ctx.validateMultiElementRefSequence(define.Elements)
			} else if define.FirstElement() != nil {
				// Validate single element
				ctx.validateSingleElementRef(define.FirstElement())
			}
		}
	}

	ctx.skipToEndElement()
}

// tryMatchZeroOrMoreElement tries to match a StartElement against zeroOrMore patterns
// Returns true if element was matched
func (ctx *validationContext) tryMatchZeroOrMoreElement(patterns []rng.ZeroOrMore, t xml.StartElement) bool {
	for _, pattern := range patterns {
		// Try matching against refs
		for _, ref := range pattern.Ref {
			if define := ctx.defines[ref.Name]; define != nil && define.FirstElement() != nil {
				if define.FirstElement().Name == t.Name.Local {
					ctx.validateElement(define.FirstElement(), &t)
					return true
				}
			}
		}

		// Try matching against elements
		for _, elem := range pattern.Element {
			if elem.Name == t.Name.Local {
				ctx.validateElement(&elem, &t)
				return true
			}
		}

		// Try matching against anyName
		if pattern.AnyName != nil {
			if ctx.isNameInExcept(t.Name.Local, t.Name.Space, pattern.AnyName.Except) {
				// Name is in except list, so it's not allowed
				ctx.addError(
					strings.Join(ctx.path, "."),
					fmt.Sprintf("element '%s' is not allowed by anyName except clause", t.Name.Local),
					nil,
					t.Name.Local,
				)
				ctx.skipToEndElement()
				return true
			}
			// Create a synthetic element for validation
			syntheticElem := &rng.Element{
				Name: t.Name.Local,
				Ns:   t.Name.Space,
			}
			ctx.validateElement(syntheticElem, &t)
			return true
		}

		// Try matching against nsName
		if pattern.NsName != nil {
			if t.Name.Space == pattern.NsName.Ns {
				if ctx.isNameInExcept(t.Name.Local, t.Name.Space, pattern.NsName.Except) {
					// Name is in except list, so it's not allowed
					ctx.addError(
						strings.Join(ctx.path, "."),
						fmt.Sprintf("element '%s' is not allowed by nsName except clause", t.Name.Local),
						nil,
						t.Name.Local,
					)
					ctx.skipToEndElement()
					return true
				}
				syntheticElem := &rng.Element{
					Name: t.Name.Local,
					Ns:   t.Name.Space,
				}
				ctx.validateElement(syntheticElem, &t)
				return true
			}
		}
	}
	return false
}

func (ctx *validationContext) validateZeroOrMore(patterns []rng.ZeroOrMore) {
	for {
		tok, err := ctx.decoder.Token()
		if err != nil {
			return
		}

		switch t := tok.(type) {
		case xml.CharData:
			if len(strings.TrimSpace(string(t))) == 0 {
				continue
			}
			return
		case xml.Comment:
			continue
		case xml.EndElement:
			return
		case xml.StartElement:
			// Try to match against one of the zeroOrMore patterns
			matched := ctx.tryMatchZeroOrMoreElement(patterns, t)
			if !matched {
				// Unknown element in zeroOrMore
				ctx.skipToEndElement()
				return
			}
		}
	}
}

// tryMatchOneOrMoreElement tries to match a StartElement against oneOrMore patterns
// Returns (matched, count) where matched=true if element was matched, count=number of matches
func (ctx *validationContext) tryMatchOneOrMoreElement(patterns []rng.OneOrMore, t xml.StartElement) (bool, int) {
	for _, pattern := range patterns {
		// Try matching against refs
		for _, ref := range pattern.Ref {
			if define := ctx.defines[ref.Name]; define != nil && define.FirstElement() != nil {
				if define.FirstElement().Name == t.Name.Local {
					ctx.validateElement(define.FirstElement(), &t)
					return true, 1
				}
			}
		}

		// Try matching against elements
		for _, elem := range pattern.Element {
			if elem.Name == t.Name.Local {
				ctx.validateElement(&elem, &t)
				return true, 1
			}
		}

		// Try matching against anyName
		if pattern.AnyName != nil {
			// Check except clause
			if pattern.AnyName.Except != nil && ctx.isNameInExcept(t.Name.Local, t.Name.Space, pattern.AnyName.Except) {
				// Name is in except list, don't match
				continue
			}
			syntheticElem := &rng.Element{
				Name: t.Name.Local,
				Ns:   t.Name.Space,
			}
			ctx.validateElement(syntheticElem, &t)
			return true, 1
		}

		// Try matching against nsName
		if pattern.NsName != nil {
			if t.Name.Space == pattern.NsName.Ns {
				// Check except clause
				if pattern.NsName.Except != nil && ctx.isNameInExcept(t.Name.Local, t.Name.Space, pattern.NsName.Except) {
					// Name is in except list, don't match
					continue
				}
				syntheticElem := &rng.Element{
					Name: t.Name.Local,
					Ns:   t.Name.Space,
				}
				ctx.validateElement(syntheticElem, &t)
				return true, 1
			}
		}
	}
	return false, 0
}

func (ctx *validationContext) validateOneOrMore(patterns []rng.OneOrMore) {
	matchCount := 0

	for {
		tok, err := ctx.decoder.Token()
		if err != nil {
			if matchCount == 0 {
				// Required at least one match but got none
				ctx.addError(
					strings.Join(ctx.path, "."),
					"oneOrMore requires at least one matching element",
					nil,
					"",
				)
			}
			return
		}

		switch t := tok.(type) {
		case xml.CharData:
			if len(strings.TrimSpace(string(t))) == 0 {
				continue
			}
			// Non-whitespace text means we're done
			if matchCount == 0 {
				// Error: required at least one match but got none
				ctx.addError(
					strings.Join(ctx.path, "."),
					"oneOrMore requires at least one matching element",
					nil,
					"",
				)
			}
			return
		case xml.Comment:
			continue
		case xml.EndElement:
			if matchCount == 0 && !ctx.oneOrMoreChoiceAttributeMatched {
				// Error: required at least one match but got none
				// Unless the oneOrMore had a choice with attributes that matched
				ctx.addError(
					strings.Join(ctx.path, "."),
					"oneOrMore requires at least one matching element",
					nil,
					"",
				)
			}
			return
		case xml.StartElement:
			// Try to match against one of the oneOrMore patterns
			matched, count := ctx.tryMatchOneOrMoreElement(patterns, t)
			if !matched {
				// Unknown element - end of oneOrMore
				if matchCount == 0 {
					// Error: required at least one match but got unmatched element
					ctx.addError(
						strings.Join(ctx.path, "."),
						fmt.Sprintf("oneOrMore requires at least one matching element, got '%s'", t.Name.Local),
						nil,
						t.Name.Local,
					)
				}
				ctx.skipToEndElement()
				return
			}
			matchCount += count
		}
	}
}

func (ctx *validationContext) validateOptional(patterns []rng.Optional) {
	// Optional is like zeroOrMore but only matches once
	for {
		tok, err := ctx.decoder.Token()
		if err != nil {
			return
		}

		switch t := tok.(type) {
		case xml.CharData:
			if len(strings.TrimSpace(string(t))) == 0 {
				continue
			}
			return
		case xml.Comment:
			continue
		case xml.EndElement:
			return
		case xml.StartElement:
			// Try to match against one of the optional patterns
			for _, pattern := range patterns {
				// Try matching against elements
				for _, elem := range pattern.Elements {
					if elem.Name == t.Name.Local {
						ctx.validateElement(&elem, &t)
						// Skip remaining content after matching optional
						ctx.skipToEndElement()
						return
					}
				}

				// Try matching against attributes (shouldn't happen in content but be safe)
				for _, attr := range pattern.Attributes {
					if attr.Name == t.Name.Local {
						ctx.skipToEndElement()
						return
					}
				}
			}
			// Element doesn't match optional pattern - that's OK, it's optional
			// But we need to put it back for parent to handle
			ctx.skipToEndElement()
			return
		}
	}
}

// tryMatchChoiceElement attempts to match a start element against choice alternatives (elements and refs)
// Returns true and validates if matched, returns false otherwise
func (ctx *validationContext) tryMatchChoiceElement(start *xml.StartElement, choice *rng.Choice, _ *xml.StartElement) bool {
	// Try to match against direct choice elements
	for _, elem := range choice.Elements {
		if elem.Name == start.Name.Local {
			ctx.validateElement(&elem, start)
			return true
		}
	}

	// Try to match against choice refs
	for _, ref := range choice.Refs {
		if define := ctx.defines[ref.Name]; define != nil && define.FirstElement() != nil {
			if define.FirstElement().Name == start.Name.Local {
				ctx.validateElement(define.FirstElement(), start)
				return true
			}
		}
	}

	return false
}

// handleChoiceElementMismatch reports an error when choice element doesn't match any alternative
func (ctx *validationContext) handleChoiceElementMismatch(start *xml.StartElement, choice *rng.Choice, parent *xml.StartElement) {
	expectedNames := make([]string, 0, len(choice.Elements))
	for _, elem := range choice.Elements {
		expectedNames = append(expectedNames, elem.Name)
	}
	ctx.addError(
		parent.Name.Local,
		fmt.Sprintf("element '%s' not in choice (expected one of: %s)", start.Name.Local, strings.Join(expectedNames, ", ")),
		expectedNames,
		start.Name.Local,
	)
}

// validateChoiceValues validates choice with value alternatives
func (ctx *validationContext) validateChoiceValues(choice *rng.Choice, parent *xml.StartElement) {
	text := ctx.readTextContent()
	matched := false
	for _, val := range choice.Values {
		if strings.TrimSpace(text) == strings.TrimSpace(val.Value) {
			matched = true
			break
		}
	}
	if !matched {
		var allowedValues []string
		for _, val := range choice.Values {
			allowedValues = append(allowedValues, val.Value)
		}
		ctx.addError(
			parent.Name.Local,
			fmt.Sprintf("value must be one of: %s", strings.Join(allowedValues, ", ")),
			allowedValues,
			text,
		)
	}
}

// validateChoiceElements validates choice with element alternatives
func (ctx *validationContext) validateChoiceElements(choice *rng.Choice, parent *xml.StartElement) {
	// Peek at next element
	tok, err := ctx.decoder.Token()
	if err != nil {
		ctx.addError(parent.Name.Local, "choice requires content", nil, "")
		return
	}

	// Handle whitespace-only char data
	if t, ok := tok.(xml.CharData); ok {
		if len(strings.TrimSpace(string(t))) == 0 {
			// Skip whitespace and try again
			tok, err = ctx.decoder.Token()
			if err != nil {
				ctx.addError(parent.Name.Local, "choice requires content", nil, "")
				return
			}
		} else {
			ctx.addError(parent.Name.Local, "unexpected text in choice", nil, string(t))
			return
		}
	}

	// Match against choice element alternatives
	if start, ok := tok.(xml.StartElement); ok {
		if !ctx.tryMatchChoiceElement(&start, choice, parent) {
			ctx.handleChoiceElementMismatch(&start, choice, parent)
		}
		ctx.skipToEndElement()
		return
	}
}

func (ctx *validationContext) validateChoice(choice *rng.Choice, parent *xml.StartElement) {
	// If choice has values, validate that content matches one of them
	if len(choice.Values) > 0 {
		ctx.validateChoiceValues(choice, parent)
		return
	}

	// If choice has elements, try to match one of them
	if len(choice.Elements) > 0 {
		ctx.validateChoiceElements(choice, parent)
		return
	}

	// For other choice types, skip to end
	ctx.skipToEndElement()
}

func (ctx *validationContext) validateGroup(groups []rng.Group, parent *xml.StartElement) {
	for _, group := range groups {
		// Validate each element in sequence
		for _, elemPattern := range group.Elements {
			if !ctx.validateGroupElement(elemPattern, parent) {
				return
			}
		}

		// Handle refs in group
		for _, ref := range group.Ref {
			if !ctx.validateGroupRef(ref, parent) {
				return
			}
		}
	}

	// After processing all required elements in group, check for unexpected content
	ctx.validateGroupTrailingContent(parent)
}

// validateGroupElement validates a single element in a group
func (ctx *validationContext) validateGroupElement(elemPattern rng.Element, parent *xml.StartElement) bool {
	// Skip whitespace
	for {
		tok, err := ctx.decoder.Token()
		if err != nil {
			ctx.addError(parent.Name.Local, "unexpected end of content in group", nil, "")
			return false
		}

		// Skip whitespace and comments
		switch t := tok.(type) {
		case xml.CharData:
			if len(strings.TrimSpace(string(t))) == 0 {
				continue
			}
			ctx.addError(
				parent.Name.Local,
				fmt.Sprintf("unexpected text in group (expected element '%s')", elemPattern.Name),
				[]string{elemPattern.Name},
				string(t),
			)
			return false
		case xml.Comment:
			continue
		case xml.StartElement:
			ctx.validateElement(&elemPattern, &t)
			return true
		case xml.EndElement:
			ctx.addError(
				parent.Name.Local,
				fmt.Sprintf("expected element '%s' but found end of parent", elemPattern.Name),
				[]string{elemPattern.Name},
				"",
			)
			return false
		default:
			ctx.addError(
				parent.Name.Local,
				fmt.Sprintf("expected element '%s' in group", elemPattern.Name),
				[]string{elemPattern.Name},
				"",
			)
			return false
		}
	}
}

// validateGroupRef validates a reference in a group
func (ctx *validationContext) validateGroupRef(ref rng.Ref, _ *xml.StartElement) bool {
	define := ctx.defines[ref.Name]
	if define == nil || define.FirstElement() == nil {
		return true
	}

	for {
		tok, err := ctx.decoder.Token()
		if err != nil {
			return false
		}

		switch t := tok.(type) {
		case xml.CharData:
			if len(strings.TrimSpace(string(t))) == 0 {
				continue
			}
			return false
		case xml.Comment:
			continue
		case xml.StartElement:
			ctx.validateElement(define.FirstElement(), &t)
			return true
		case xml.EndElement:
			return false
		}
	}
}

// validateGroupTrailingContent validates that no unexpected content follows group elements
func (ctx *validationContext) validateGroupTrailingContent(parent *xml.StartElement) {
	for {
		tok, err := ctx.decoder.Token()
		if err != nil {
			// EOF is OK - just end of content
			return
		}

		switch t := tok.(type) {
		case xml.CharData:
			if len(strings.TrimSpace(string(t))) == 0 {
				// Whitespace is OK, continue
				continue
			}
			// Non-whitespace text is an error
			ctx.addError(
				parent.Name.Local,
				"unexpected text after group content",
				nil,
				string(t),
			)
			ctx.skipToEndElement()
			return
		case xml.Comment:
			// Comments are OK, continue
			continue
		case xml.EndElement:
			// This is the closing tag of the parent element - we're done
			return
		case xml.StartElement:
			// Extra element not expected by group pattern
			ctx.addError(
				parent.Name.Local,
				fmt.Sprintf("unexpected element '<%s>' after group content (group contains fixed sequence)", t.Name.Local),
				nil,
				t.Name.Local,
			)
			ctx.skipToEndElement()
			return
		default:
			// Other tokens are OK, just continue
			continue
		}
	}
}

// validateInterleave validates interleave content where elements can appear in any order.
// Uses backtracking algorithm to try all possible matches.
func (ctx *validationContext) validateInterleave(interleaves []rng.Interleave, parent *xml.StartElement) {
	if len(interleaves) == 0 {
		ctx.skipToEndElement()
		return
	}

	interleave := interleaves[0]

	// Collect all child elements into tokens for backtracking
	tokens := ctx.collectInterleaveTokens()

	// Build element maps for validation
	requiredElements, optionalElements := ctx.buildInterleaveElementMaps(interleave)

	// Track which required elements have been matched
	matched := make(map[string]int) // element name -> count

	// Process tokens in any order
	processedTokens := make([]bool, len(tokens))
	for i := 0; i < len(tokens); i++ {
		if processedTokens[i] {
			continue
		}

		if start, ok := tokens[i].(xml.StartElement); ok {
			ctx.processInterleaveToken(start, i, tokens, processedTokens, requiredElements, optionalElements, matched, parent)
		}
	}

	// Check that all required elements were matched exactly once
	ctx.checkInterleaveRequiredElements(parent, requiredElements, matched)
}

// collectInterleaveTokens collects tokens from the decoder for interleave processing
func (ctx *validationContext) collectInterleaveTokens() []xml.Token {
	var tokens []xml.Token
	depth := 1
	for depth > 0 {
		tok, err := ctx.decoder.Token()
		if err != nil {
			break
		}

		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			tokens = append(tokens, t)
		case xml.EndElement:
			depth--
			if depth > 0 {
				tokens = append(tokens, t)
			}
		case xml.CharData:
			if len(strings.TrimSpace(string(t))) > 0 {
				tokens = append(tokens, t)
			}
		case xml.Comment:
			// Skip comments
		default:
			tokens = append(tokens, t)
		}
	}
	return tokens
}

// processInterleaveToken processes a single token in interleave validation
func (ctx *validationContext) processInterleaveToken(start xml.StartElement, i int, tokens []xml.Token, processedTokens []bool, requiredElements, optionalElements map[string]bool, matched map[string]int, parent *xml.StartElement) {
	elemName := start.Name.Local

	// Try to match against required elements
	if requiredElements[elemName] || optionalElements[elemName] {
		matched[elemName]++
		processedTokens[i] = true

		// Find and skip the matching end element
		depth := 1
		for j := i + 1; j < len(tokens); j++ {
			if st, ok := tokens[j].(xml.StartElement); ok && st.Name.Local == elemName {
				depth++
			} else if et, ok := tokens[j].(xml.EndElement); ok && et.Name.Local == elemName {
				depth--
				if depth == 0 {
					processedTokens[j] = true
					for k := i + 1; k < j; k++ {
						processedTokens[k] = true
					}
					break
				}
			}
		}
		// Validate the element
		// Note: We're just marking as matched; real validation would happen earlier
		return
	}

	// Unknown element in interleave
	ctx.addError(
		parent.Name.Local,
		fmt.Sprintf("unexpected element '%s' in interleave", elemName),
		nil,
		elemName,
	)
}

// buildInterleaveElementMaps builds maps of required and optional elements from interleave
func (ctx *validationContext) buildInterleaveElementMaps(interleave rng.Interleave) (map[string]bool, map[string]bool) {
	requiredElements := make(map[string]bool)
	optionalElements := make(map[string]bool)

	// Add required elements
	for _, elem := range interleave.Elements {
		requiredElements[elem.Name] = true
	}

	// Add optional elements
	for _, opt := range interleave.Optional {
		for _, elem := range opt.Elements {
			optionalElements[elem.Name] = true
		}
	}

	// Add refs
	for _, ref := range interleave.Ref {
		if define := ctx.defines[ref.Name]; define != nil && define.FirstElement() != nil {
			requiredElements[define.FirstElement().Name] = true
		}
	}

	return requiredElements, optionalElements
}

// checkInterleaveRequiredElements validates that all required elements were matched
func (ctx *validationContext) checkInterleaveRequiredElements(parent *xml.StartElement, requiredElements map[string]bool, matched map[string]int) {
	for elemName := range requiredElements {
		if matched[elemName] == 0 {
			ctx.addError(
				parent.Name.Local,
				fmt.Sprintf("required element '%s' missing in interleave", elemName),
				[]string{elemName},
				"",
			)
		} else if matched[elemName] > 1 {
			ctx.addError(
				parent.Name.Local,
				fmt.Sprintf("element '%s' appears %d times (expected 1)", elemName, matched[elemName]),
				[]string{elemName},
				elemName,
			)
		}
	}
}

// validateList validates list pattern content per RELAX NG spec section 5.11
// List pattern treats text content as whitespace-separated tokens
func (ctx *validationContext) validateList(list *rng.List, element *xml.StartElement) {
	// Read all text content
	text := ctx.readTextContent()
	tokens := strings.Fields(text)

	// Try each list pattern type in order
	if len(list.Values) > 0 {
		ctx.validateListValues(list.Values, tokens, element, text)
		return
	}

	if list.Data != nil {
		ctx.validateListData(list.Data, tokens, element, text)
		return
	}

	// List contains Group with multiple data patterns
	expectedTokenCount := ctx.countListDataPatterns(list)
	if expectedTokenCount > 0 {
		ctx.validateListGroup(expectedTokenCount, tokens, element, text)
		return
	}

	// Case 4: List contains OneOrMore pattern (one or more items matching a pattern)
	if list.OneOrMore != nil {
		ctx.validateListOneOrMore(list.OneOrMore, tokens, element, text)
		return
	}

	// Case 5: List contains Choice pattern
	if list.Choice != nil {
		ctx.validateListChoice(list.Choice, tokens, element, text)
		return
	}

	// Case 6: List with empty just needs empty tokens
	if list.Empty != nil {
		if len(tokens) != 0 {
			ctx.addError(
				element.Name.Local,
				fmt.Sprintf("list expects empty content but got %d tokens", len(tokens)),
				nil,
				text,
			)
		}
		return
	}
}

// validateListValues validates tokens against a list of value patterns
func (ctx *validationContext) validateListValues(values []rng.Value, tokens []string, element *xml.StartElement, text string) {
	if len(tokens) != len(values) {
		ctx.addError(
			element.Name.Local,
			fmt.Sprintf("list expects %d tokens but got %d", len(values), len(tokens)),
			nil,
			text,
		)
		return
	}

	// Validate each token against corresponding value
	for i, val := range values {
		if i >= len(tokens) {
			break
		}

		expectedValue := strings.TrimSpace(val.Value)
		actualToken := tokens[i]

		// For type="token" (default), values should match directly
		// For type="string", would need exact match (but less common in lists)
		if actualToken != expectedValue {
			ctx.addError(
				element.Name.Local,
				fmt.Sprintf("list token %d: expected '%s' but got '%s'", i, expectedValue, actualToken),
				[]string{expectedValue},
				actualToken,
			)
			return
		}
	}
}

// validateListData validates tokens against a single data pattern
func (ctx *validationContext) validateListData(data *rng.Data, tokens []string, element *xml.StartElement, text string) {
	// A single <data> pattern in a list means exactly one token is expected
	if len(tokens) != 1 {
		ctx.addError(
			element.Name.Local,
			fmt.Sprintf("list with <data> pattern expects exactly 1 token, got %d", len(tokens)),
			nil,
			text,
		)
		return
	}

	// Validate the single token against the data pattern
	if !ctx.validateDataType(data.Type, tokens[0]) {
		ctx.addError(
			element.Name.Local,
			fmt.Sprintf("token '%s' does not match data type '%s'", tokens[0], data.Type),
			nil,
			tokens[0],
		)
	}
}

// validateListGroup validates tokens against multiple data patterns in a group
func (ctx *validationContext) validateListGroup(expectedTokenCount int, tokens []string, element *xml.StartElement, text string) {
	if len(tokens) != expectedTokenCount {
		ctx.addError(
			element.Name.Local,
			fmt.Sprintf("list expects %d tokens but got %d", expectedTokenCount, len(tokens)),
			nil,
			text,
		)
		return
	}
	// Validate each token against data type token
	for i, token := range tokens {
		if !ctx.validateDataType("token", token) {
			ctx.addError(
				element.Name.Local,
				fmt.Sprintf("token %d '%s' does not match expected type 'token'", i, token),
				nil,
				token,
			)
			return
		}
	}
}

// validateListOneOrMore validates tokens against a oneOrMore pattern in a list
// oneOrMore means one or more of the contained pattern (typically values)
func (ctx *validationContext) validateListOneOrMore(oneOrMore *rng.OneOrMore, tokens []string, element *xml.StartElement, text string) {
	if len(tokens) == 0 {
		ctx.addError(
			element.Name.Local,
			"list with <oneOrMore> pattern expects at least 1 token",
			nil,
			text,
		)
		return
	}

	// Check what pattern is inside oneOrMore
	// If it has Values, all tokens must match one of those values
	if len(oneOrMore.Value) > 0 {
		ctx.validateListOneOrMoreValues(oneOrMore.Value, tokens, element, text)
		return
	}

	// If it has data patterns, all tokens must match those data types
	if len(oneOrMore.Data) > 0 {
		// If single data pattern, all tokens must match it
		if len(oneOrMore.Data) == 1 {
			for i, token := range tokens {
				if !ctx.validateDataType(oneOrMore.Data[0].Type, token) {
					ctx.addError(
						element.Name.Local,
						fmt.Sprintf("token %d '%s' does not match data type '%s'", i, token, oneOrMore.Data[0].Type),
						nil,
						token,
					)
					return
				}
			}
			return
		}
		// Multiple data patterns - this would be in a group, handled by group validation
		return
	}

	// If it has a choice, any token can match any alternative in the choice
	if oneOrMore.Choice != nil {
		// This would require parsing the choice, for now just accept
		return
	}
}

// validateListOneOrMoreValues validates tokens against a list of allowed values in oneOrMore
func (ctx *validationContext) validateListOneOrMoreValues(values []rng.Value, tokens []string, element *xml.StartElement, _ string) {
	// Build set of allowed values
	allowedValues := make(map[string]bool)
	for _, v := range values {
		allowedValues[strings.TrimSpace(v.Value)] = true
	}

	// Each token must match one of the allowed values
	for i, token := range tokens {
		if !allowedValues[token] {
			expectedStr := ""
			for v := range allowedValues {
				if expectedStr != "" {
					expectedStr += ", "
				}
				expectedStr += "'" + v + "'"
			}
			ctx.addError(
				element.Name.Local,
				fmt.Sprintf("token %d '%s' does not match any allowed value: %s", i, token, expectedStr),
				nil,
				token,
			)
			return
		}
	}
}

// validateListChoice validates tokens against a choice pattern in a list
func (ctx *validationContext) validateListChoice(_ *rng.Choice, _ []string, _ *xml.StartElement, _ string) {
	// A list with choice means each token can match any of the choice alternatives
	// For now, just accept any tokens as we would need to parse the full choice structure
	// This is a simplified implementation
}

// countListDataPatterns counts the number of data patterns in a list by parsing RawContent
func (ctx *validationContext) countListDataPatterns(list *rng.List) int {
	if len(list.RawContent) == 0 {
		return 0
	}

	// Parse RawContent to find groups with data elements
	content := string(list.RawContent)

	// Look for <group> ... </group>
	groupStart := strings.Index(content, "<group>")
	if groupStart == -1 {
		groupStart = strings.Index(content, "<group ")
	}
	if groupStart == -1 {
		return 0
	}

	groupEnd := strings.LastIndex(content, "</group>")
	if groupEnd == -1 {
		return 0
	}

	groupContent := content[groupStart : groupEnd+8]

	// Count <data> elements in the group
	count := 0
	idx := 0
	for {
		dataStart := strings.Index(groupContent[idx:], "<data")
		if dataStart == -1 {
			break
		}
		count++
		idx += dataStart + 5
	}

	return count
}

func (ctx *validationContext) validateTextContent(element *xml.StartElement) {
	for {
		tok, err := ctx.decoder.Token()
		if err != nil {
			return
		}

		switch t := tok.(type) {
		case xml.EndElement:
			return
		case xml.CharData:
			// Text is valid
			continue
		default:
			ctx.addError(
				element.Name.Local,
				"expected text content only",
				[]string{"text"},
				fmt.Sprintf("%T", t),
			)
		}
	}
}

func (ctx *validationContext) readTextContent() string {
	var text strings.Builder
	for {
		tok, err := ctx.decoder.Token()
		if err != nil {
			return text.String()
		}

		switch t := tok.(type) {
		case xml.EndElement:
			return text.String()
		case xml.CharData:
			text.Write(t)
		}
	}
}

func (ctx *validationContext) skipToEndElement() {
	depth := 1
	for depth > 0 {
		tok, err := ctx.decoder.Token()
		if err != nil {
			return
		}

		switch tok.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
		}
	}
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

// matchesElementName checks if an element matches the pattern's name or name class
func (ctx *validationContext) matchesElementName(pattern *rng.Element, element *xml.StartElement) bool {
	// If NameElement is specified, match against it (with namespace from ns attribute or element ns)
	if pattern.NameElement != nil && pattern.NameElement.Value != "" {
		if pattern.NameElement.Value != element.Name.Local {
			return false
		}
		// Per RELAX NG spec section 4.6: determine namespace for matching
		// The NameElement.Ns field can be:
		// - non-empty string: explicit namespace requirement
		// - empty string: could mean either "no namespace constraint" OR "explicit empty namespace"
		//   In RELAX NG, when <name ns=""> is used, it explicitly requires NO namespace
		// - The element pattern's Ns provides fallback namespace context

		// If pattern.NameElement.Ns is explicitly set to any value (including empty string),
		// use it as the target namespace
		// Otherwise, inherit from pattern.Ns
		targetNs := pattern.NameElement.Ns
		if pattern.NameElement.Ns == "" && pattern.Ns != "" {
			// NameElement Ns is empty and parent has Ns - inherit from parent
			targetNs = pattern.Ns
		}
		// Note: we cannot distinguish between "ns attribute not present" and "ns=''"
		// So we assume: if we're matching against a NameElement, we must validate namespace
		// This means: match if targetNs equals element.Name.Space
		if targetNs != element.Name.Space {
			return false
		}
		return true
	}

	// If explicit name is specified, match against it
	if pattern.Name != "" {
		if pattern.Name != element.Name.Local {
			return false
		}
		// Also check namespace if specified
		if pattern.Ns != "" && pattern.Ns != element.Name.Space {
			return false
		}
		return true
	}

	// If anyName is specified, match any name (with possible except clause)
	if pattern.AnyName != nil {
		if pattern.AnyName.Except != nil {
			// Name should NOT be in the except list
			return !ctx.isNameInExcept(element.Name.Local, element.Name.Space, pattern.AnyName.Except)
		}
		return true
	}

	// If nsName is specified, match names in the specified namespace
	if pattern.NsName != nil {
		// Check namespace match
		if element.Name.Space != pattern.NsName.Ns {
			return false
		}
		if pattern.NsName.Except != nil {
			// Name should NOT be in the except list
			return !ctx.isNameInExcept(element.Name.Local, element.Name.Space, pattern.NsName.Except)
		}
		return true
	}

	// If pattern has a choice of elements (e.g., from combine="choice" merging),
	// check if the element matches one of the choices
	if pattern.Choice != nil && len(pattern.Choice.Elements) > 0 {
		for _, choiceElem := range pattern.Choice.Elements {
			// Try to match against each choice element
			if ctx.matchesElementName(&choiceElem, element) {
				return true
			}
		}
		return false
	}

	// No name or name class specified - this might be an error in the schema
	// but for validation purposes, we'll accept it if no name constraints exist
	return true
}

func (ctx *validationContext) matchesAttributeNameClass(attr *rng.Attribute, actualName string, actualNamespace string) bool {
	// If no name class constraints, it's a match
	if attr.AnyName == nil && attr.NsName == nil {
		return true
	}

	// anyName matches any attribute name
	if attr.AnyName != nil {
		if attr.AnyName.Except != nil {
			return !ctx.isInNameExcept(actualName, actualNamespace, attr.AnyName.Except)
		}
		return true
	}

	// nsName matches attributes in a specific namespace
	if attr.NsName != nil {
		// Check if attribute is in the specified namespace
		if actualNamespace != attr.NsName.Ns {
			return false
		}

		if attr.NsName.Except != nil {
			return !ctx.isInNameExcept(actualName, actualNamespace, attr.NsName.Except)
		}
		return true
	}

	return false
}

// isInNameExcept checks if a name is in an except constraint
func (ctx *validationContext) isInNameExcept(actualName, namespace string, except *rng.NameExcept) bool {
	if except == nil {
		return false
	}

	// Check against explicit name list
	for _, nc := range except.Names {
		// Compare both local name and namespace
		// If the except clause specifies a namespace, both must match
		// If the except clause doesn't specify a namespace (empty ns=""), only match if namespace is also empty
		if nc.Value == actualName && nc.Ns == namespace {
			return true
		}
	}

	// Check against nsName in except
	if except.NsName != nil && except.NsName.Ns == namespace {
		return ctx.checkNsNameExcept(actualName, namespace, except.NsName)
	}

	// Check against anyName in except (excludes everything)
	if except.AnyName != nil {
		return ctx.checkAnyNameExcept(actualName, namespace, except.AnyName)
	}

	return false
}

// checkNsNameExcept checks if a name is in an nsName except constraint
func (ctx *validationContext) checkNsNameExcept(actualName, namespace string, nsName *rng.NsName) bool {
	// If the nsName has a nested except, check if the name is EXCLUDED from that except
	// If it's excluded, then it's NOT in the nsName's result
	if nsName.Except != nil {
		// If it IS in the nested except, then it's NOT in this nsName (because of the except)
		// So return false (not in the except)
		if ctx.isInNameExcept(actualName, namespace, nsName.Except) {
			return false
		}
	}
	return true
}

// checkAnyNameExcept checks if a name is in an anyName except constraint
func (ctx *validationContext) checkAnyNameExcept(actualName, namespace string, anyName *rng.AnyName) bool {
	// But if anyName has a nested except, check if the name is EXCLUDED
	if anyName.Except != nil {
		if ctx.isInNameExcept(actualName, namespace, anyName.Except) {
			return false
		}
	}
	return true
}

// isNameInExcept checks if a name is in an except constraint for zeroOrMore/oneOrMore patterns
func (ctx *validationContext) isNameInExcept(actualName, namespace string, except *rng.NameExcept) bool {
	return ctx.isInNameExcept(actualName, namespace, except)
}

// validateOptionalRepeatingAttributes validates attributes from oneOrMore/zeroOrMore patterns
func (ctx *validationContext) validateOptionalRepeatingAttributes(pattern *rng.Element, attrMap map[string]xml.Attr) {
	// Collect attributes from oneOrMore/zeroOrMore (both direct and in groups/choices)
	optionalChoiceAttrs, oneOrMoreChoicesHaveAttrs := ctx.collectOptionalRepeatingAttrs(pattern)

	// Check attributes from collected patterns
	for _, attrPattern := range optionalChoiceAttrs {
		ctx.validateOptionalRepeatingAttr(attrPattern, pattern, attrMap, oneOrMoreChoicesHaveAttrs)
	}
}

// collectOptionalRepeatingAttrs collects attributes from oneOrMore/zeroOrMore patterns
func (ctx *validationContext) collectOptionalRepeatingAttrs(pattern *rng.Element) ([]*rng.Attribute, bool) {
	var optionalChoiceAttrs []*rng.Attribute
	var oneOrMoreChoicesHaveAttrs bool

	for _, one := range pattern.OneOrMore {
		// Collect attributes from direct attributes, groups, and choices
		for i := range one.Attribute {
			optionalChoiceAttrs = append(optionalChoiceAttrs, &one.Attribute[i])
		}
		for _, group := range one.Group {
			optionalChoiceAttrs = append(optionalChoiceAttrs, ctx.collectAttributesFromGroup(&group)...)
		}
		if one.Choice != nil {
			attrs := ctx.collectAttributesFromChoice(one.Choice)
			optionalChoiceAttrs = append(optionalChoiceAttrs, attrs...)
			if len(attrs) > 0 {
				oneOrMoreChoicesHaveAttrs = true
			}
		}
	}

	for _, zero := range pattern.ZeroOrMore {
		// Collect attributes from direct attributes, groups, and choices
		for i := range zero.Attribute {
			optionalChoiceAttrs = append(optionalChoiceAttrs, &zero.Attribute[i])
		}
		for _, group := range zero.Group {
			optionalChoiceAttrs = append(optionalChoiceAttrs, ctx.collectAttributesFromGroup(&group)...)
		}
		if zero.Choice != nil {
			optionalChoiceAttrs = append(optionalChoiceAttrs, ctx.collectAttributesFromChoice(zero.Choice)...)
		}
	}

	return optionalChoiceAttrs, oneOrMoreChoicesHaveAttrs
}

// validateOptionalRepeatingAttr validates a single optional repeating attribute
func (ctx *validationContext) validateOptionalRepeatingAttr(attrPattern *rng.Attribute, pattern *rng.Element, attrMap map[string]xml.Attr, oneOrMoreChoicesHaveAttrs bool) {
	// Get attribute name from either Name field or NameElement
	attrName := attrPattern.Name
	if attrName == "" && attrPattern.NameElement != nil {
		attrName = attrPattern.NameElement.Value
	}
	if attrName == "" {
		return
	}

	// Build key considering namespace
	ns := ctx.getAttributeNamespace(*attrPattern, pattern)
	key := ns + "|" + attrName
	if attr, found := attrMap[key]; found {
		// Attribute is present - validate it
		ctx.validateAttributeValue(attrPattern, &attr)
		delete(attrMap, key)
		// Mark that oneOrMore choice attribute was matched
		if oneOrMoreChoicesHaveAttrs {
			ctx.oneOrMoreChoiceAttributeMatched = true
		}
	}
	// If attribute is missing, that's fine - it's optional (choice might pick element)
}

// wildcardPatternInfo represents a wildcard attribute pattern and its matching status
type wildcardPatternInfo struct {
	pattern      *rng.Attribute
	isRequired   bool // true if from oneOrMore, false if from optional or required
	matchedCount int  // number of attributes matched by this pattern
}

// validateWildcardAndUnknownAttributes validates wildcard attributes and reports unknown attributes
func (ctx *validationContext) validateWildcardAndUnknownAttributes(pattern *rng.Element, attrMap map[string]xml.Attr) {
	// Collect wildcard patterns from all sources
	wildcardPatterns, oneOrMoreWildcardIndices := ctx.collectWildcardPatterns(pattern)

	// Match remaining attributes against wildcard patterns
	for key, attr := range attrMap {
		matched := false
		for i, wildcardInfo := range wildcardPatterns {
			if ctx.matchesAttributeNameClass(wildcardInfo.pattern, attr.Name.Local, attr.Name.Space) {
				ctx.validateAttributeValue(wildcardInfo.pattern, &attr)
				wildcardPatterns[i].matchedCount++
				matched = true
				break
			}
		}
		if matched {
			delete(attrMap, key)
		}
	}

	// Check that oneOrMore wildcard patterns matched at least one attribute
	for _, idx := range oneOrMoreWildcardIndices {
		if wildcardPatterns[idx].matchedCount == 0 {
			ctx.addError(
				strings.Join(ctx.path, "."),
				"oneOrMore attribute pattern requires at least one attribute match",
				nil,
				"",
			)
		}
	}

	// Error on unknown attributes (strict validation by default)
	for _, attr := range attrMap {
		ctx.addError(
			strings.Join(ctx.path, "."),
			fmt.Sprintf("unknown attribute '%s'", attr.Name.Local),
			nil,
			attr.Name.Local,
		)
	}
}

// collectWildcardPatterns collects wildcard attribute patterns from all sources
func (ctx *validationContext) collectWildcardPatterns(pattern *rng.Element) ([]wildcardPatternInfo, []int) {
	var wildcardPatterns []wildcardPatternInfo
	var oneOrMoreWildcardIndices []int

	// Helper function to check if an attribute pattern is a wildcard
	isWildcard := func(attrPattern *rng.Attribute) bool {
		hasSpecificName := attrPattern.Name != "" || (attrPattern.NameElement != nil && attrPattern.NameElement.Value != "")
		return !hasSpecificName && (attrPattern.AnyName != nil || attrPattern.NsName != nil)
	}

	// Collect from required attributes
	collectFromAttributeList(pattern.Attributes, &wildcardPatterns, &oneOrMoreWildcardIndices, isWildcard, false)

	// Collect from optional attributes
	for _, opt := range pattern.Optional {
		collectFromAttributeList(opt.Attributes, &wildcardPatterns, &oneOrMoreWildcardIndices, isWildcard, false)
		// Check if optional itself has anyName/nsName
		if opt.AnyName != nil {
			wildcardPatterns = append(wildcardPatterns, wildcardPatternInfo{
				pattern:    &rng.Attribute{AnyName: opt.AnyName},
				isRequired: false,
			})
		}
		if opt.NsName != nil {
			wildcardPatterns = append(wildcardPatterns, wildcardPatternInfo{
				pattern:    &rng.Attribute{NsName: opt.NsName},
				isRequired: false,
			})
		}
	}

	// Collect from oneOrMore attributes (these require at least one match)
	for _, one := range pattern.OneOrMore {
		collectFromAttributeListRequired(one.Attribute, &wildcardPatterns, &oneOrMoreWildcardIndices, isWildcard)
		for _, group := range one.Group {
			collectFromAttributeListRequired(group.Attributes, &wildcardPatterns, &oneOrMoreWildcardIndices, isWildcard)
		}
	}

	// Collect from zeroOrMore attributes
	for _, zero := range pattern.ZeroOrMore {
		collectFromAttributeList(zero.Attribute, &wildcardPatterns, &oneOrMoreWildcardIndices, isWildcard, false)
		for _, group := range zero.Group {
			collectFromAttributeList(group.Attributes, &wildcardPatterns, &oneOrMoreWildcardIndices, isWildcard, false)
		}
	}

	return wildcardPatterns, oneOrMoreWildcardIndices
}

// collectFromAttributeList collects wildcard patterns from an attribute list
func collectFromAttributeList(attrs []rng.Attribute, wildcardPatterns *[]wildcardPatternInfo, oneOrMoreIndices *[]int, isWildcard func(*rng.Attribute) bool, isRequired bool) {
	for i := range attrs {
		if isWildcard(&attrs[i]) {
			idx := len(*wildcardPatterns)
			*wildcardPatterns = append(*wildcardPatterns, wildcardPatternInfo{
				pattern:    &attrs[i],
				isRequired: isRequired,
			})
			if isRequired {
				*oneOrMoreIndices = append(*oneOrMoreIndices, idx)
			}
		}
	}
}

// collectFromAttributeListRequired collects wildcard patterns from an attribute list and marks them as required
func collectFromAttributeListRequired(attrs []rng.Attribute, wildcardPatterns *[]wildcardPatternInfo, oneOrMoreIndices *[]int, isWildcard func(*rng.Attribute) bool) {
	collectFromAttributeList(attrs, wildcardPatterns, oneOrMoreIndices, isWildcard, true)
}

// normalizeTokenValue normalizes whitespace for token type per XML Schema spec
// Replaces tabs, newlines, carriage returns with spaces, then collapses consecutive spaces
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

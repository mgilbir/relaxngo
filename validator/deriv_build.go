package validator

// Builder: translate a parsed *rng.Grammar into the canonical pattern tree the
// derivative engine consumes.
//
// The rng parser leaves content in two places: normalized structured fields and
// the raw inner XML (RawContent). For faithful order and completeness we parse
// the RawContent of each container recursively, threading the inherited
// namespace and datatypeLibrary. Constructs we cannot translate without the
// original parser context (nested externalRef/include, prefixed QNames, nested
// grammars) return errUnsupported, and the caller falls back to the legacy
// engine — so this never produces a wrong result, only "can't handle, defer".

import (
	"bytes"
	"encoding/xml"
	"errors"
	"strings"

	"github.com/mgilbir/relaxngo/rng"
)

var errUnsupported = errors.New("derivative builder: unsupported construct")

// staleRaw reports whether RawContent is a stale remnant left after the parser
// resolved the real content into structured fields (nested-grammar unpacking or
// externalRef resolution). Such RawContent must not be parsed directly.
func staleRaw(raw []byte) bool {
	return bytes.Contains(raw, []byte("<grammar")) || bytes.Contains(raw, []byte("<externalRef"))
}

// includeUsesNs reports whether any <include> tag in raw carries an ns
// attribute (which the parser applies to structured fields only).
func includeUsesNs(raw string) bool {
	for i := 0; ; {
		j := strings.Index(raw[i:], "<include")
		if j < 0 {
			return false
		}
		start := i + j
		end := strings.Index(raw[start:], ">")
		if end < 0 {
			return false
		}
		if strings.Contains(raw[start:start+end], " ns=") {
			return true
		}
		i = start + end + 1
	}
}

// bctx is the inherited context while walking a pattern's raw XML.
type bctx struct {
	ns    string            // inherited target namespace for element/attribute names
	dtl   string            // inherited datatypeLibrary
	nsMap map[string]string // in-scope prefix -> namespace URI (for QNames in name attributes)
}

const xmlNamespace = "http://www.w3.org/XML/1998/namespace"

// resolveQName resolves a name attribute value (which may be prefixed) to a
// namespace and local part. Unprefixed names take defaultNs. The xml prefix is
// always bound. An unknown prefix returns ok=false so the caller can defer.
func resolveQName(name string, nsMap map[string]string, defaultNs string) (ns, local string, ok bool) {
	name = strings.TrimSpace(name)
	i := strings.IndexByte(name, ':')
	if i < 0 {
		return defaultNs, name, true
	}
	prefix, local := name[:i], name[i+1:]
	if prefix == "xml" {
		return xmlNamespace, local, true
	}
	if uri, found := nsMap[prefix]; found {
		return uri, local, true
	}
	return "", "", false
}

// withNsDecls returns ctx with any xmlns:prefix declarations in attrs merged
// into a fresh nsMap (so children don't mutate the parent's map).
func withNsDecls(ctx bctx, attrs []xml.Attr) bctx {
	var merged map[string]string
	for _, a := range attrs {
		if a.Name.Space == "xmlns" && a.Name.Local != "" {
			if merged == nil {
				merged = make(map[string]string, len(ctx.nsMap)+len(attrs))
				for k, v := range ctx.nsMap {
					merged[k] = v
				}
			}
			merged[a.Name.Local] = a.Value
		}
	}
	if merged != nil {
		ctx.nsMap = merged
	}
	return ctx
}

type builder struct {
	defs         map[string]*rng.Define // define name -> parsed define
	defineCtx    map[string]bctx        // define name -> its base context
	built        map[string]pat         // memoized define patterns
	building     map[string]bool        // cycle guard while building defines
	preferStruct bool                   // build names from structured fields (div/include applied a namespace)
	wrapDecls    string                 // xmlns declarations to re-scope over RawContent (when RELAX NG is prefixed)
}

const nsWrapper = "_relaxngo_ns_"

// rawDecoder returns a decoder over raw. When the grammar prefixes the RELAX NG
// namespace, raw is wrapped in a synthetic element carrying the grammar's xmlns
// declarations so element/attribute prefixes resolve; the returned wrapperLocal
// is the wrapper's local name (empty when no wrapping) and the decoder is
// positioned just after the wrapper's start tag.
func (b *builder) rawDecoder(raw []byte) (*xml.Decoder, string) {
	if b.wrapDecls == "" {
		return xml.NewDecoder(bytes.NewReader(raw)), ""
	}
	wrapped := "<" + nsWrapper + b.wrapDecls + ">" + string(raw) + "</" + nsWrapper + ">"
	dec := xml.NewDecoder(strings.NewReader(wrapped))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		if _, ok := tok.(xml.StartElement); ok {
			break
		}
	}
	return dec, nsWrapper
}

// foreign reports whether se is a foreign (non-RELAX NG) element. When RawContent
// is re-scoped with the grammar's namespaces (wrapDecls), structural elements
// carry the RELAX NG namespace and anything else is foreign; otherwise a
// non-empty namespace other than RELAX NG marks foreign content.
func (b *builder) foreign(se xml.StartElement) bool {
	if b.wrapDecls != "" {
		return se.Name.Space != relaxNGNamespace
	}
	return se.Name.Space != "" && se.Name.Space != relaxNGNamespace
}

// nsDeclsString renders the xmlns declarations in attrs as attribute text and
// reports whether any prefix is bound to the RELAX NG namespace.
func nsDeclsString(attrs []xml.Attr) (decls string, prefixesRNG bool) {
	var sb strings.Builder
	for _, a := range attrs {
		switch {
		case a.Name.Space == "" && a.Name.Local == "xmlns":
			sb.WriteString(` xmlns="` + a.Value + `"`)
		case a.Name.Space == "xmlns":
			sb.WriteString(` xmlns:` + a.Name.Local + `="` + a.Value + `"`)
			if a.Value == relaxNGNamespace {
				prefixesRNG = true
			}
		}
	}
	return sb.String(), prefixesRNG
}

// buildGrammar translates a grammar into (start pattern, define env). It returns
// errUnsupported if any construct cannot be faithfully translated.
func buildGrammar(g *rng.Grammar) (pat, map[string]pat, error) {
	b := &builder{
		defs:      map[string]*rng.Define{},
		defineCtx: map[string]bctx{},
		built:     map[string]pat{},
		building:  map[string]bool{},
	}
	// Namespace inheritance from <div>/<include> and nested-grammar unpacking are
	// applied by the parser into structured fields but not into RawContent, which
	// this builder reads. Defer such grammars to the legacy engine rather than
	// translate them from stale raw content. (combine-merged defines, whose
	// RawContent is likewise empty, are handled below from structured fields.)
	// An <include> without a namespace merges its content into the top-level
	// start/defines with faithful RawContent, so it can be built normally. An
	// <include ns="..."> applies that namespace to the included element names in
	// structured fields only, which this RawContent-based path would miss — so
	// defer those.
	// Nested grammars are unpacked by the parser into structured fields (which
	// are now consistent across parse paths); the define/start/element builders
	// read them from there, deferring per-construct on parentRef.
	raw := string(g.RawContent)
	// <div ns="..."> and <include ns="..."> apply a namespace to element names
	// that lives only in structured fields. Build names/refs from structured
	// fields rather than the (unnamespaced) RawContent for these grammars.
	if strings.Contains(raw, "<div") || includeUsesNs(raw) {
		b.preferStruct = true
	}
	// If the grammar binds the RELAX NG namespace to a prefix (e.g. <rng:element>),
	// re-scope RawContent with the grammar's xmlns declarations so those prefixes
	// resolve when it is re-parsed.
	if decls, prefixesRNG := nsDeclsString(g.RawAttrs); prefixesRNG {
		b.wrapDecls = decls
	}

	grammarDTL := g.DatatypeLibrary

	for i := range g.Defines {
		d := &g.Defines[i]
		if _, dup := b.defs[d.Name]; dup {
			// Two defines with the same name that were not merged: defer.
			return nil, nil, errUnsupported
		}
		b.defs[d.Name] = d
		b.defineCtx[d.Name] = bctx{ns: "", dtl: firstNonEmpty(d.DatatypeLibrary, grammarDTL)}
	}

	// Build every define (resolves refs lazily but we materialize all so cycles
	// and unsupported constructs surface up front).
	for name := range b.defs {
		if _, err := b.define(name); err != nil {
			return nil, nil, err
		}
	}

	startCtx := bctx{ns: "", dtl: firstNonEmpty(g.Start.DatatypeLibrary, grammarDTL)}
	start, err := b.buildStart(&g.Start, startCtx)
	if err != nil {
		return nil, nil, err
	}
	return start, b.built, nil
}

func (b *builder) define(name string) (pat, error) {
	if p, ok := b.built[name]; ok {
		return p, nil
	}
	if b.building[name] {
		// Recursive define: return a ref node; it resolves via the env at
		// derivation time.
		return pRef{name}, nil
	}
	def, ok := b.defs[name]
	if !ok {
		return nil, errUnsupported
	}
	b.building[name] = true
	p, err := b.buildDefine(def, b.defineCtx[name])
	delete(b.building, name)
	if err != nil {
		return nil, err
	}
	b.built[name] = p
	return p, nil
}

// buildDefine builds a define's pattern from its RawContent, or from structured
// fields when RawContent is empty (combine-merged) or a stale nested-grammar
// remnant.
func (b *builder) buildDefine(def *rng.Define, ctx bctx) (pat, error) {
	if !b.preferStruct {
		if raw := def.RawContent; len(bytes.TrimSpace(raw)) > 0 && !staleRaw(raw) {
			return b.parseSeq(raw, ctx)
		}
	}
	return b.defineFromStruct(def, ctx)
}

// buildStart builds the start pattern. A structured start ref/parentRef is the
// parser's resolved (and possibly renamed, after nested-grammar unpacking)
// reference and takes precedence over a stale RawContent ref. Otherwise build
// from RawContent, or from structured fields when RawContent is empty
// (combine-merged) or a stale nested-grammar remnant.
func (b *builder) buildStart(start *rng.Start, ctx bctx) (pat, error) {
	if start.Ref != nil {
		return pRef{start.Ref.Name}, nil
	}
	if start.ParentRef != nil {
		return pRef{start.ParentRef.Name}, nil
	}
	if !b.preferStruct {
		if raw := start.RawContent; len(bytes.TrimSpace(raw)) > 0 && !staleRaw(raw) {
			return b.parseSeq(raw, ctx)
		}
	}
	return b.startFromStruct(start, ctx)
}

// ---- structured-fields builders --------------------------------------------
//
// combine-merged defines and starts have empty RawContent and carry their merged
// alternatives in structured Choice/Interleave fields. These builders translate
// those structured fields; each alternative element's own content is still read
// from that element's RawContent.

func (b *builder) defineFromStruct(def *rng.Define, ctx bctx) (pat, error) {
	switch {
	case def.Choice != nil:
		return b.choiceStruct(def.Choice, ctx)
	case len(def.Interleave) > 0:
		return b.interleaveList(def.Interleave, ctx)
	case len(def.Elements) > 0:
		return b.elementsGroup(def.Elements, ctx)
	case def.Element != nil: // deprecated singular field, still used by hand-built grammars
		return b.elementFromStruct(def.Element, ctx)
	case def.Ref != nil:
		return pRef{def.Ref.Name}, nil
	case def.ParentRef != nil:
		return pRef{def.ParentRef.Name}, nil
	case def.Empty != nil:
		return empty, nil
	case def.NotAllowed != nil:
		return notAllowed, nil
	default:
		return nil, errUnsupported
	}
}

func (b *builder) startFromStruct(start *rng.Start, ctx bctx) (pat, error) {
	switch {
	case start.Ref != nil:
		return pRef{start.Ref.Name}, nil
	case start.ParentRef != nil:
		return pRef{start.ParentRef.Name}, nil
	case start.Choice != nil:
		return b.choiceStruct(start.Choice, ctx)
	case start.Element != nil:
		return b.elementFromStruct(start.Element, ctx)
	case len(start.Group) > 0:
		var parts []pat
		for i := range start.Group {
			p, err := b.groupStruct(&start.Group[i], ctx)
			if err != nil {
				return nil, err
			}
			parts = append(parts, p)
		}
		return groupAll(parts), nil
	case len(start.Interleave) > 0:
		return b.interleaveList(start.Interleave, ctx)
	case start.Empty != nil:
		return empty, nil
	case start.NotAllowed != nil:
		return notAllowed, nil
	default:
		return nil, errUnsupported
	}
}

func (b *builder) elementsGroup(els []rng.Element, ctx bctx) (pat, error) {
	var parts []pat
	for i := range els {
		p, err := b.elementFromStruct(&els[i], ctx)
		if err != nil {
			return nil, err
		}
		parts = append(parts, p)
	}
	return groupAll(parts), nil
}

func (b *builder) interleaveList(ils []rng.Interleave, ctx bctx) (pat, error) {
	var parts []pat
	for i := range ils {
		p, err := b.interleaveStruct(&ils[i], ctx)
		if err != nil {
			return nil, err
		}
		parts = append(parts, p)
	}
	return interleaveAll(parts), nil
}

func (b *builder) choiceStruct(ch *rng.Choice, ctx bctx) (pat, error) {
	cctx := ctx
	if ch.Ns != "" {
		cctx.ns = ch.Ns
	}
	if ch.DatatypeLibrary != "" {
		cctx.dtl = ch.DatatypeLibrary
	}
	// Content kinds this structured path does not translate: defer.
	if len(ch.Attributes) > 0 || len(ch.Interleave) > 0 || ch.List != nil ||
		ch.Mixed != nil || ch.ExternalRef != nil || len(ch.NameElements) > 0 ||
		len(ch.Values) > 0 || len(ch.Data) > 0 {
		return nil, errUnsupported
	}
	var alts []pat
	for i := range ch.Elements {
		p, err := b.elementFromStruct(&ch.Elements[i], cctx)
		if err != nil {
			return nil, err
		}
		alts = append(alts, p)
	}
	for _, ref := range ch.Refs {
		alts = append(alts, pRef{ref.Name})
	}
	for i := range ch.Group {
		p, err := b.groupStruct(&ch.Group[i], cctx)
		if err != nil {
			return nil, err
		}
		alts = append(alts, p)
	}
	if ch.Text != nil {
		alts = append(alts, anyText)
	}
	if ch.Empty != nil {
		alts = append(alts, empty)
	}
	if ch.NotAllowed != nil {
		alts = append(alts, notAllowed)
	}
	if len(alts) == 0 {
		return nil, errUnsupported
	}
	return choiceAll(alts), nil
}

func (b *builder) interleaveStruct(il *rng.Interleave, ctx bctx) (pat, error) {
	ictx := ctx
	if il.Ns != "" {
		ictx.ns = il.Ns
	}
	if il.DatatypeLibrary != "" {
		ictx.dtl = il.DatatypeLibrary
	}
	if len(il.Attributes) > 0 || len(il.Choice) > 0 || len(il.Optional) > 0 ||
		len(il.OneOrMore) > 0 || len(il.ZeroOrMore) > 0 || il.List != nil ||
		il.Data != nil || len(il.Value) > 0 || il.NotAllowed != nil || il.ExternalRef != nil {
		return nil, errUnsupported
	}
	var parts []pat
	for i := range il.Elements {
		p, err := b.elementFromStruct(&il.Elements[i], ictx)
		if err != nil {
			return nil, err
		}
		parts = append(parts, p)
	}
	for _, ref := range il.Ref {
		parts = append(parts, pRef{ref.Name})
	}
	for i := range il.Group {
		p, err := b.groupStruct(&il.Group[i], ictx)
		if err != nil {
			return nil, err
		}
		parts = append(parts, p)
	}
	if il.Text != nil {
		parts = append(parts, anyText)
	}
	if len(parts) == 0 {
		return nil, errUnsupported
	}
	return interleaveAll(parts), nil
}

func (b *builder) groupStruct(g *rng.Group, ctx bctx) (pat, error) {
	gctx := ctx
	if g.Ns != "" {
		gctx.ns = g.Ns
	}
	if g.DatatypeLibrary != "" {
		gctx.dtl = g.DatatypeLibrary
	}
	if len(g.Attributes) > 0 || len(g.Optional) > 0 || len(g.OneOrMore) > 0 ||
		len(g.ZeroOrMore) > 0 || g.List != nil ||
		len(g.Value) > 0 || len(g.Data) > 0 || g.ExternalRef != nil {
		return nil, errUnsupported
	}
	var parts []pat
	for i := range g.Elements {
		p, err := b.elementFromStruct(&g.Elements[i], gctx)
		if err != nil {
			return nil, err
		}
		parts = append(parts, p)
	}
	for _, ref := range g.Ref {
		parts = append(parts, pRef{ref.Name})
	}
	for i := range g.Group {
		p, err := b.groupStruct(&g.Group[i], gctx)
		if err != nil {
			return nil, err
		}
		parts = append(parts, p)
	}
	for i := range g.Choice {
		p, err := b.choiceStruct(&g.Choice[i], gctx)
		if err != nil {
			return nil, err
		}
		parts = append(parts, p)
	}
	for i := range g.Interleave {
		p, err := b.interleaveStruct(&g.Interleave[i], gctx)
		if err != nil {
			return nil, err
		}
		parts = append(parts, p)
	}
	if g.Text != nil {
		parts = append(parts, anyText)
	}
	if g.NotAllowed != nil {
		parts = append(parts, notAllowed)
	}
	if len(parts) == 0 {
		return nil, errUnsupported
	}
	return groupAll(parts), nil
}

// elementFromStruct builds an element pattern from a parsed rng.Element: the name
// class comes from the (normalized) structured fields, its content from the
// element's own RawContent.
func (b *builder) elementFromStruct(el *rng.Element, ctx bctx) (pat, error) {
	// Namespace prefixes declared on this element (its RawAttrs) are in scope
	// for QNames appearing in its content's name attributes.
	childCtx := withNsDecls(ctx, el.RawAttrs)
	if el.Ns != "" {
		childCtx.ns = el.Ns
	}
	if el.DatatypeLibrary != "" {
		childCtx.dtl = el.DatatypeLibrary
	}

	nc, ncErr := ncFromElementStruct(el, ctx)
	if ncErr != nil {
		// No structured name class (e.g. a choice-of-names). Parse the name
		// class and content directly from RawContent.
		if len(bytes.TrimSpace(el.RawContent)) == 0 {
			return nil, errUnsupported
		}
		return b.elementFromRawContent(el.RawContent, childCtx)
	}

	if len(bytes.TrimSpace(el.RawContent)) == 0 || staleRaw(el.RawContent) {
		// Empty raw content, or a stale nested-grammar remnant left in RawContent
		// after the parser unpacked the real content into structured fields:
		// build from the structured fields.
		content, err := b.structuredElementContent(el, childCtx)
		if err != nil {
			return nil, err
		}
		return pElem{nc: nc, p: content}, nil
	}
	// When the element uses a name-class child (<name>/<anyName>/<nsName>/choice
	// of names) rather than a name attribute, that child is the first thing in
	// RawContent and is already captured in nc — skip it when parsing content.
	content, err := b.parseElementContent(el.RawContent, childCtx, el.Name == "")
	if err != nil {
		return nil, err
	}
	return pElem{nc: nc, p: content}, nil
}

// elementFromRawContent builds an element pattern from RawContent that begins
// with a name-class child (the element used a name-class form the structured
// fields did not capture, e.g. a choice of names) followed by content.
func (b *builder) elementFromRawContent(raw []byte, ctx bctx) (pat, error) {
	dec, wrap := b.rawDecoder(raw)
	var nc nameClass
	haveNC := false
	var parts []pat
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		if ee, ok := tok.(xml.EndElement); ok && wrap != "" && ee.Name.Local == wrap {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if b.foreign(se) {
			if err := skipElement(dec, se); err != nil {
				return nil, err
			}
			continue
		}
		if !haveNC {
			if !isNameClassElem(se.Name.Local) {
				return nil, errUnsupported
			}
			nc, err = b.parseNameClass(dec, se, ctx)
			if err != nil {
				return nil, err
			}
			haveNC = true
			continue
		}
		p, err := b.parseElementToken(dec, se, ctx)
		if err != nil {
			return nil, err
		}
		parts = append(parts, p)
	}
	if !haveNC {
		return nil, errUnsupported
	}
	return pElem{nc: nc, p: groupAll(parts)}, nil
}

// parseElementContent parses an element's content from RawContent, optionally
// skipping a leading name-class child that has already been translated.
func (b *builder) parseElementContent(raw []byte, ctx bctx, skipNameClass bool) (pat, error) {
	dec, wrap := b.rawDecoder(raw)
	var parts []pat
	skipped := !skipNameClass
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		if ee, ok := tok.(xml.EndElement); ok && wrap != "" && ee.Name.Local == wrap {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if b.foreign(se) {
			if err := skipElement(dec, se); err != nil {
				return nil, err
			}
			continue
		}
		if !skipped {
			skipped = true
			if isNameClassElem(se.Name.Local) {
				if err := skipElement(dec, se); err != nil {
					return nil, err
				}
				continue
			}
		}
		p, err := b.parseElementToken(dec, se, ctx)
		if err != nil {
			return nil, err
		}
		parts = append(parts, p)
	}
	return groupAll(parts), nil
}

// relaxNGNamespace is the RELAX NG structure namespace.
const relaxNGNamespace = "http://relaxng.org/ns/structure/1.0"

// structuredElementContent builds an element's content pattern from its
// structured fields, used when RawContent is empty (e.g. after nested-grammar
// unpacking). It defers on containers whose structured representation is
// incomplete (optional/oneOrMore/zeroOrMore/mixed/list only partially populate
// structured fields) and on parentRef.
func (b *builder) structuredElementContent(el *rng.Element, ctx bctx) (pat, error) {
	if len(el.Optional) > 0 || len(el.OneOrMore) > 0 || len(el.ZeroOrMore) > 0 ||
		el.Mixed != nil || el.List != nil {
		return nil, errUnsupported
	}
	var parts []pat
	for i := range el.Attributes {
		p, err := b.attributeStruct(&el.Attributes[i], ctx)
		if err != nil {
			return nil, err
		}
		parts = append(parts, p)
	}
	for i := range el.Elements {
		p, err := b.elementFromStruct(&el.Elements[i], ctx)
		if err != nil {
			return nil, err
		}
		parts = append(parts, p)
	}
	for _, ref := range el.Ref {
		parts = append(parts, pRef{ref.Name})
	}
	for _, ref := range el.ParentRef {
		parts = append(parts, pRef{ref.Name})
	}
	if el.Choice != nil {
		p, err := b.choiceStruct(el.Choice, ctx)
		if err != nil {
			return nil, err
		}
		parts = append(parts, p)
	}
	for i := range el.Group {
		p, err := b.groupStruct(&el.Group[i], ctx)
		if err != nil {
			return nil, err
		}
		parts = append(parts, p)
	}
	for i := range el.Interleave {
		p, err := b.interleaveStruct(&el.Interleave[i], ctx)
		if err != nil {
			return nil, err
		}
		parts = append(parts, p)
	}
	if el.Text != nil {
		parts = append(parts, anyText)
	}
	if el.Data != nil {
		parts = append(parts, b.dataStruct(el.Data, ctx))
	}
	for i := range el.Values {
		parts = append(parts, valueStruct(&el.Values[i], ctx))
	}
	if el.Empty != nil {
		parts = append(parts, empty)
	}
	if el.NotAllowed != nil {
		parts = append(parts, notAllowed)
	}
	if len(parts) == 0 {
		return empty, nil
	}
	return groupAll(parts), nil
}

// dataStruct builds a data pattern from a structured rng.Data. Params and a
// simple <except> (values/data alternatives) are translated.
func (b *builder) dataStruct(d *rng.Data, ctx bctx) pat {
	lib := ctx.dtl
	if d.DatatypeLibrary != "" {
		lib = d.DatatypeLibrary
	}
	pd := pData{typ: strings.TrimSpace(d.Type), lib: lib}
	for _, pm := range d.Params {
		pd.params = append(pd.params, dParam{name: pm.Name, value: pm.Value})
	}
	if d.Except != nil {
		var alts []pat
		for i := range d.Except.Values {
			alts = append(alts, valueStruct(&d.Except.Values[i], ctx))
		}
		for i := range d.Except.Data {
			alts = append(alts, b.dataStruct(&d.Except.Data[i], ctx))
		}
		if len(alts) > 0 {
			pd.except = choiceAll(alts)
		}
	}
	return pd
}

// attributeStruct builds an attribute pattern from a structured rng.Attribute.
func (b *builder) attributeStruct(a *rng.Attribute, ctx bctx) (pat, error) {
	var nc nameClass
	switch {
	case a.Name != "":
		if strings.Contains(a.Name, ":") {
			return nil, errUnsupported
		}
		nc = ncName{ns: a.Ns, local: a.Name} // attribute names do not inherit the element namespace
	case a.NameElement != nil:
		local := a.NameElement.LocalName
		if local == "" {
			local = strings.TrimSpace(a.NameElement.Value)
		}
		nc = ncName{ns: a.NameElement.Namespace, local: local}
	case a.AnyName != nil:
		n, err := anyNameClassFromStruct(a.AnyName, ctx)
		if err != nil {
			return nil, err
		}
		nc = n
	case a.NsName != nil:
		n, err := nsNameClassFromStruct(a.NsName, ctx)
		if err != nil {
			return nil, err
		}
		nc = n
	default:
		return nil, errUnsupported
	}

	var valuePat pat
	switch {
	case a.Data != nil:
		valuePat = b.dataStruct(a.Data, ctx)
	case len(a.Values) > 0:
		var alts []pat
		for i := range a.Values {
			alts = append(alts, valueStruct(&a.Values[i], ctx))
		}
		valuePat = choiceAll(alts)
	case a.Choice != nil:
		p, err := b.choiceStruct(a.Choice, ctx)
		if err != nil {
			return nil, err
		}
		valuePat = p
	case a.Empty != nil:
		valuePat = empty
	case a.List != nil:
		return nil, errUnsupported
	default:
		valuePat = anyText // <attribute name="x"/> allows any string
	}
	return pAttr{nc: nc, p: valuePat}, nil
}

func valueStruct(v *rng.Value, ctx bctx) pat {
	lib := ctx.dtl
	if v.DatatypeLibrary != "" {
		lib = v.DatatypeLibrary
	}
	return pValue{typ: strings.TrimSpace(v.Type), lib: lib, value: v.Value}
}

func ncFromElementStruct(el *rng.Element, ctx bctx) (nameClass, error) {
	switch {
	case el.Name != "":
		if strings.Contains(el.Name, ":") {
			return nil, errUnsupported
		}
		return ncName{ns: firstNonEmpty(el.Ns, ctx.ns), local: el.Name}, nil
	case el.NameElement != nil:
		ne := el.NameElement
		local := ne.LocalName
		if local == "" {
			local = strings.TrimSpace(ne.Value)
		}
		if strings.Contains(local, ":") {
			return nil, errUnsupported
		}
		ns := ne.Namespace
		if ns == "" {
			ns = firstNonEmpty(ne.Ns, ctx.ns)
		}
		return ncName{ns: ns, local: local}, nil
	case el.AnyName != nil:
		return anyNameClassFromStruct(el.AnyName, ctx)
	case el.NsName != nil:
		return nsNameClassFromStruct(el.NsName, ctx)
	default:
		return nil, errUnsupported
	}
}

func anyNameClassFromStruct(an *rng.AnyName, ctx bctx) (nameClass, error) {
	ex, err := exceptNameClassFromStruct(an.Except, ctx)
	if err != nil {
		return nil, err
	}
	return ncAny{except: ex}, nil
}

func nsNameClassFromStruct(nn *rng.NsName, ctx bctx) (nameClass, error) {
	ex, err := exceptNameClassFromStruct(nn.Except, ctx)
	if err != nil {
		return nil, err
	}
	return ncNs{ns: nn.Ns, except: ex}, nil
}

// exceptNameClassFromStruct translates a name-class <except> (a set of names,
// nsNames and anyNames) into a nameClass, or nil when there is no exception.
func exceptNameClassFromStruct(ne *rng.NameExcept, ctx bctx) (nameClass, error) {
	if ne == nil {
		return nil, nil
	}
	var alts []nameClass
	for _, n := range ne.Names {
		local := strings.TrimSpace(n.Value)
		if strings.Contains(local, ":") {
			return nil, errUnsupported
		}
		alts = append(alts, ncName{ns: firstNonEmpty(n.Ns, ctx.ns), local: local})
	}
	if ne.NsName != nil {
		sub, err := nsNameClassFromStruct(ne.NsName, ctx)
		if err != nil {
			return nil, err
		}
		alts = append(alts, sub)
	}
	if ne.AnyName != nil {
		sub, err := anyNameClassFromStruct(ne.AnyName, ctx)
		if err != nil {
			return nil, err
		}
		alts = append(alts, sub)
	}
	if len(alts) == 0 {
		return nil, nil
	}
	return foldNameChoice(alts), nil
}

// parseSeq parses a run of sibling patterns (the inner content of a container)
// and returns their group. An empty run is pEmpty.
func (b *builder) parseSeq(raw []byte, ctx bctx) (pat, error) {
	dec, wrap := b.rawDecoder(raw)
	var parts []pat
	for {
		tok, err := dec.Token()
		if err != nil {
			break // io.EOF ends the sibling run
		}
		if ee, ok := tok.(xml.EndElement); ok && wrap != "" && ee.Name.Local == wrap {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue // whitespace/chardata/comments between patterns
		}
		if b.foreign(se) {
			if err := skipElement(dec, se); err != nil {
				return nil, err
			}
			continue
		}
		p, err := b.parseElementToken(dec, se, ctx)
		if err != nil {
			return nil, err
		}
		parts = append(parts, p)
	}
	return groupAll(parts), nil
}

// parseElementToken translates one RELAX NG pattern element (already opened as
// se) and consumes tokens through its matching end tag.
func (b *builder) parseElementToken(dec *xml.Decoder, se xml.StartElement, ctx ctxT) (pat, error) {
	local := se.Name.Local
	// Inline xmlns declarations on this element are in scope for QNames in its
	// content's name attributes.
	childCtx := withNsDecls(ctx, se.Attr)
	if ns, ok := attr(se, "ns"); ok {
		childCtx.ns = ns
	}
	if dtl, ok := attr(se, "datatypeLibrary"); ok {
		childCtx.dtl = dtl
	}

	switch local {
	case "element":
		return b.parseElement(dec, se, childCtx)
	case "attribute":
		return b.parseAttribute(dec, se, childCtx)
	case "group":
		return b.parseContainer(dec, se, childCtx, groupAll)
	case "choice":
		return b.parseContainer(dec, se, childCtx, choiceAll)
	case "interleave":
		return b.parseContainer(dec, se, childCtx, interleaveAll)
	case "optional":
		return b.parseContainer(dec, se, childCtx, func(ps []pat) pat { return choice(groupAll(ps), empty) })
	case "oneOrMore":
		return b.parseContainer(dec, se, childCtx, func(ps []pat) pat { return oneOrMore(groupAll(ps)) })
	case "zeroOrMore":
		return b.parseContainer(dec, se, childCtx, func(ps []pat) pat { return choice(oneOrMore(groupAll(ps)), empty) })
	case "mixed":
		return b.parseContainer(dec, se, childCtx, func(ps []pat) pat { return interleave(groupAll(ps), anyText) })
	case "ref", "parentRef":
		// After nested-grammar unpacking the parent grammar's defines are
		// top-level, so a parentRef resolves like a ref to a top-level define.
		name, _ := attr(se, "name")
		if err := skipElement(dec, se); err != nil {
			return nil, err
		}
		return pRef{strings.TrimSpace(name)}, nil
	case "text":
		return anyText, skipElement(dec, se)
	case "empty":
		return empty, skipElement(dec, se)
	case "notAllowed":
		return notAllowed, skipElement(dec, se)
	case "data":
		return b.parseData(dec, se, childCtx)
	case "value":
		return b.parseValue(dec, se, childCtx)
	case "list":
		return b.parseContainer(dec, se, childCtx, func(ps []pat) pat { return pList{groupAll(ps)} })
	default:
		// externalRef, include, grammar, div, obsolete elements, etc.
		return nil, errUnsupported
	}
}

type ctxT = bctx

// parseContainer parses the inner patterns of se and combines them with combine.
func (b *builder) parseContainer(dec *xml.Decoder, se xml.StartElement, ctx bctx, combine func([]pat) pat) (pat, error) {
	parts, err := b.parseChildren(dec, se, ctx)
	if err != nil {
		return nil, err
	}
	return combine(parts), nil
}

// parseChildren consumes tokens until se's end tag, translating each child
// pattern element.
func (b *builder) parseChildren(dec *xml.Decoder, se xml.StartElement, ctx bctx) ([]pat, error) {
	var parts []pat
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, errUnsupported
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if b.foreign(t) {
				if err := skipElement(dec, t); err != nil {
					return nil, err
				}
				continue
			}
			p, err := b.parseElementToken(dec, t, ctx)
			if err != nil {
				return nil, err
			}
			parts = append(parts, p)
		case xml.EndElement:
			if t.Name.Local == se.Name.Local {
				return parts, nil
			}
		}
	}
}

func (b *builder) parseElement(dec *xml.Decoder, se xml.StartElement, ctx bctx) (pat, error) {
	nc, contentStart, err := b.elementNameClass(dec, se, ctx)
	if err != nil {
		return nil, err
	}
	var parts []pat
	if contentStart != nil {
		p, err := b.parseElementToken(dec, *contentStart, ctx)
		if err != nil {
			return nil, err
		}
		parts = append(parts, p)
	}
	rest, err := b.parseChildren(dec, se, ctx)
	if err != nil {
		return nil, err
	}
	parts = append(parts, rest...)
	return pElem{nc: nc, p: groupAll(parts)}, nil
}

func (b *builder) parseAttribute(dec *xml.Decoder, se xml.StartElement, ctx bctx) (pat, error) {
	// Attribute names default to no namespace (ns is not inherited for attrs).
	actx := ctx
	actx.ns = ""
	if ns, ok := attr(se, "ns"); ok {
		actx.ns = ns
	}
	nc, contentStart, err := b.attrNameClass(dec, se, actx, ctx)
	if err != nil {
		return nil, err
	}
	var parts []pat
	if contentStart != nil {
		p, err := b.parseElementToken(dec, *contentStart, ctx)
		if err != nil {
			return nil, err
		}
		parts = append(parts, p)
	}
	rest, err := b.parseChildren(dec, se, ctx)
	if err != nil {
		return nil, err
	}
	parts = append(parts, rest...)
	if len(parts) == 0 {
		// <attribute name="x"/> with no value pattern allows any string. An
		// explicit <empty/> value, in contrast, must remain empty.
		return pAttr{nc: nc, p: anyText}, nil
	}
	return pAttr{nc: nc, p: groupAll(parts)}, nil
}

// elementNameClass resolves an element's name class from its name attribute or
// its first child name-class element. If the name came from an attribute the
// returned start token is nil; otherwise it is the first content child that
// follows the consumed name-class element(s).
func (b *builder) elementNameClass(dec *xml.Decoder, se xml.StartElement, ctx bctx) (nameClass, *xml.StartElement, error) {
	if name, ok := attr(se, "name"); ok {
		ns, local, resolved := resolveQName(name, ctx.nsMap, ctx.ns)
		if !resolved {
			return nil, nil, errUnsupported // unknown QName prefix
		}
		return ncName{ns: ns, local: local}, nil, nil
	}
	// Name class is the first (non-foreign) child element.
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, nil, errUnsupported
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if b.foreign(t) {
				if err := skipElement(dec, t); err != nil {
					return nil, nil, err
				}
				continue
			}
			if isNameClassElem(t.Name.Local) {
				nc, err := b.parseNameClass(dec, t, ctx)
				if err != nil {
					return nil, nil, err
				}
				return nc, nil, nil
			}
			// First child is content, not a name class: only valid if the
			// element used a name attribute, which it didn't -> unsupported.
			return nil, nil, errUnsupported
		case xml.EndElement:
			return nil, nil, errUnsupported
		}
	}
}

func (b *builder) attrNameClass(dec *xml.Decoder, se xml.StartElement, actx, contentCtx bctx) (nameClass, *xml.StartElement, error) {
	if name, ok := attr(se, "name"); ok {
		// The shorthand name attribute of an attribute defaults to no namespace
		// (it does not inherit the element namespace), but a prefix still binds.
		ns, local, resolved := resolveQName(name, contentCtx.nsMap, actx.ns)
		if !resolved {
			return nil, nil, errUnsupported
		}
		return ncName{ns: ns, local: local}, nil, nil
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, nil, errUnsupported
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if b.foreign(t) {
				if err := skipElement(dec, t); err != nil {
					return nil, nil, err
				}
				continue
			}
			if isNameClassElem(t.Name.Local) {
				// A <name>/<nsName>/<anyName> child of an attribute inherits the
				// namespace from its context, like an element's name class.
				nc, err := b.parseNameClass(dec, t, contentCtx)
				if err != nil {
					return nil, nil, err
				}
				return nc, nil, nil
			}
			return nil, nil, errUnsupported
		case xml.EndElement:
			// Attribute with no name and no name class: treat as anyName.
			return ncAny{}, nil, nil
		}
	}
}

func isNameClassElem(local string) bool {
	switch local {
	case "name", "anyName", "nsName", "choice":
		return true
	}
	return false
}

func (b *builder) parseNameClass(dec *xml.Decoder, se xml.StartElement, ctx bctx) (nameClass, error) {
	switch se.Name.Local {
	case "name":
		ns := ctx.ns
		if v, ok := attr(se, "ns"); ok {
			ns = v
		}
		text, err := elementText(dec, se)
		if err != nil {
			return nil, err
		}
		name := strings.TrimSpace(text)
		if strings.Contains(name, ":") {
			return nil, errUnsupported
		}
		return ncName{ns: ns, local: name}, nil
	case "anyName":
		except, err := b.nameClassExcept(dec, se, ctx)
		if err != nil {
			return nil, err
		}
		return ncAny{except: except}, nil
	case "nsName":
		ns := ctx.ns
		if v, ok := attr(se, "ns"); ok {
			ns = v
		}
		except, err := b.nameClassExcept(dec, se, ctx)
		if err != nil {
			return nil, err
		}
		return ncNs{ns: ns, except: except}, nil
	case "choice":
		var alts []nameClass
		for {
			tok, err := dec.Token()
			if err != nil {
				return nil, errUnsupported
			}
			switch t := tok.(type) {
			case xml.StartElement:
				nc, err := b.parseNameClass(dec, t, ctx)
				if err != nil {
					return nil, err
				}
				alts = append(alts, nc)
			case xml.EndElement:
				if t.Name.Local == "choice" {
					return foldNameChoice(alts), nil
				}
			}
		}
	default:
		return nil, errUnsupported
	}
}

// nameClassExcept parses an optional <except> child of anyName/nsName and skips
// to the end of se.
func (b *builder) nameClassExcept(dec *xml.Decoder, se xml.StartElement, ctx bctx) (nameClass, error) {
	var except nameClass
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, errUnsupported
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "except" {
				var alts []nameClass
				for {
					tk, err := dec.Token()
					if err != nil {
						return nil, errUnsupported
					}
					if s2, ok := tk.(xml.StartElement); ok {
						nc, err := b.parseNameClass(dec, s2, ctx)
						if err != nil {
							return nil, err
						}
						alts = append(alts, nc)
						continue
					}
					if e2, ok := tk.(xml.EndElement); ok && e2.Name.Local == "except" {
						break
					}
				}
				except = foldNameChoice(alts)
			} else {
				return nil, errUnsupported
			}
		case xml.EndElement:
			if t.Name.Local == se.Name.Local {
				return except, nil
			}
		}
	}
}

func (b *builder) parseData(dec *xml.Decoder, se xml.StartElement, ctx bctx) (pat, error) {
	typ, _ := attr(se, "type")
	typ = strings.TrimSpace(typ)
	lib := ctx.dtl
	if v, ok := attr(se, "datatypeLibrary"); ok {
		lib = v
	}
	d := pData{typ: typ, lib: lib}
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, errUnsupported
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if b.foreign(t) {
				if err := skipElement(dec, t); err != nil {
					return nil, err
				}
				continue
			}
			switch t.Name.Local {
			case "param":
				pname, _ := attr(t, "name")
				val, err := elementText(dec, t)
				if err != nil {
					return nil, err
				}
				d.params = append(d.params, dParam{name: pname, value: val})
			case "except":
				parts, err := b.parseChildren(dec, t, ctx)
				if err != nil {
					return nil, err
				}
				d.except = choiceAll(parts)
			default:
				return nil, errUnsupported
			}
		case xml.EndElement:
			if t.Name.Local == "data" {
				return d, nil
			}
		}
	}
}

func (b *builder) parseValue(dec *xml.Decoder, se xml.StartElement, ctx bctx) (pat, error) {
	typ, _ := attr(se, "type")
	typ = strings.TrimSpace(typ)
	lib := ctx.dtl
	if v, ok := attr(se, "datatypeLibrary"); ok {
		lib = v
	}
	text, err := elementText(dec, se)
	if err != nil {
		return nil, err
	}
	return pValue{typ: typ, lib: lib, value: text}, nil
}

// ---- small helpers ---------------------------------------------------------

func attr(se xml.StartElement, local string) (string, bool) {
	for _, a := range se.Attr {
		if a.Name.Local == local && (a.Name.Space == "" || local != "base") {
			return a.Value, true
		}
	}
	return "", false
}

// elementText reads the character data of se up to its end tag.
func elementText(dec *xml.Decoder, se xml.StartElement) (string, error) {
	var sb strings.Builder
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return "", errUnsupported
		}
		switch t := tok.(type) {
		case xml.CharData:
			sb.Write(t)
		case xml.StartElement:
			return "", errUnsupported // unexpected markup inside value/param/name
		case xml.EndElement:
			depth--
		}
	}
	return sb.String(), nil
}

// skipElement consumes tokens through se's matching end tag.
func skipElement(dec *xml.Decoder, se xml.StartElement) error {
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return errUnsupported
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

func groupAll(ps []pat) pat {
	if len(ps) == 0 {
		return empty
	}
	acc := ps[0]
	for _, p := range ps[1:] {
		acc = group(acc, p)
	}
	return acc
}

func choiceAll(ps []pat) pat {
	if len(ps) == 0 {
		return notAllowed
	}
	acc := ps[0]
	for _, p := range ps[1:] {
		acc = choice(acc, p)
	}
	return acc
}

func interleaveAll(ps []pat) pat {
	if len(ps) == 0 {
		return empty
	}
	acc := ps[0]
	for _, p := range ps[1:] {
		acc = interleave(acc, p)
	}
	return acc
}

func foldNameChoice(ncs []nameClass) nameClass {
	if len(ncs) == 0 {
		return ncName{} // matches nothing meaningful
	}
	acc := ncs[0]
	for _, n := range ncs[1:] {
		acc = ncChoice{a: acc, b: n}
	}
	return acc
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

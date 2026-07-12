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

// bctx is the inherited context while walking a pattern's raw XML.
type bctx struct {
	ns  string // inherited target namespace for element/attribute names
	dtl string // inherited datatypeLibrary
}

type builder struct {
	defineRaw map[string][]byte // define name -> its raw content
	defineCtx map[string]bctx   // define name -> its base context
	built     map[string]pat    // memoized define patterns
	building  map[string]bool   // cycle guard while building defines
}

// buildGrammar translates a grammar into (start pattern, define env). It returns
// errUnsupported if any construct cannot be faithfully translated.
func buildGrammar(g *rng.Grammar) (pat, map[string]pat, error) {
	b := &builder{
		defineRaw: map[string][]byte{},
		defineCtx: map[string]bctx{},
		built:     map[string]pat{},
		building:  map[string]bool{},
	}
	// Namespace inheritance from <div>/<include> and combine-merge of defines
	// are applied by the parser into structured fields but not into RawContent,
	// which this builder reads. Defer such grammars to the legacy engine rather
	// than translate them from stale raw content.
	if raw := string(g.RawContent); strings.Contains(raw, "<include") ||
		strings.Contains(raw, "<externalRef") ||
		strings.Contains(raw, "combine=") ||
		strings.Contains(raw, "<div") ||
		strings.Contains(raw, "<grammar") || // nested grammar (unpacked into structured fields)
		strings.Contains(raw, "parentRef") {
		return nil, nil, errUnsupported
	}

	grammarDTL := g.DatatypeLibrary

	for i := range g.Defines {
		d := &g.Defines[i]
		raw := d.RawContent
		if len(bytes.TrimSpace(raw)) == 0 {
			// Combine-merged or otherwise structurally-only defines are not
			// represented in RawContent; defer the whole grammar.
			return nil, nil, errUnsupported
		}
		if _, dup := b.defineRaw[d.Name]; dup {
			// Two defines with the same name that were not merged: defer.
			return nil, nil, errUnsupported
		}
		b.defineRaw[d.Name] = raw
		b.defineCtx[d.Name] = bctx{ns: "", dtl: firstNonEmpty(d.DatatypeLibrary, grammarDTL)}
	}

	// Build every define (resolves refs lazily but we materialize all so cycles
	// and unsupported constructs surface up front).
	for name := range b.defineRaw {
		if _, err := b.define(name); err != nil {
			return nil, nil, err
		}
	}

	startRaw := g.Start.RawContent
	if len(bytes.TrimSpace(startRaw)) == 0 {
		return nil, nil, errUnsupported
	}
	start, err := b.parseSeq(startRaw, bctx{ns: "", dtl: firstNonEmpty(g.Start.DatatypeLibrary, grammarDTL)})
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
	raw, ok := b.defineRaw[name]
	if !ok {
		return nil, errUnsupported
	}
	b.building[name] = true
	p, err := b.parseSeq(raw, b.defineCtx[name])
	delete(b.building, name)
	if err != nil {
		return nil, err
	}
	b.built[name] = p
	return p, nil
}

// parseSeq parses a run of sibling patterns (the inner content of a container)
// and returns their group. An empty run is pEmpty.
func (b *builder) parseSeq(raw []byte, ctx bctx) (pat, error) {
	dec := xml.NewDecoder(bytes.NewReader(raw))
	var parts []pat
	for {
		tok, err := dec.Token()
		if err != nil {
			break // io.EOF ends the sibling run
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue // whitespace/chardata/comments between patterns
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
	childCtx := ctx
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
		name, _ := attr(se, "name")
		if err := skipElement(dec, se); err != nil {
			return nil, err
		}
		if local == "parentRef" {
			return nil, errUnsupported // nested-grammar parentRef
		}
		return pRef{name}, nil
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
		name = strings.TrimSpace(name)
		if strings.Contains(name, ":") {
			return nil, nil, errUnsupported // unresolved QName prefix
		}
		return ncName{ns: ctx.ns, local: name}, nil, nil
	}
	// Name class is the first child element.
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, nil, errUnsupported
		}
		switch t := tok.(type) {
		case xml.StartElement:
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
		name = strings.TrimSpace(name)
		if strings.Contains(name, ":") {
			return nil, nil, errUnsupported
		}
		// The shorthand name attribute of an attribute defaults to no
		// namespace (it does not inherit the element namespace).
		return ncName{ns: actx.ns, local: name}, nil, nil
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, nil, errUnsupported
		}
		switch t := tok.(type) {
		case xml.StartElement:
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

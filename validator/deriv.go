package validator

// This file implements RELAX NG validation using the derivative algorithm
// described by James Clark in "An algorithm for RELAX NG validation"
// (https://www.thaiopensource.com/relaxng/derivative.html).
//
// The document is modelled as a tree of nodes; validation computes the
// derivative of the schema pattern with respect to each node in turn. A pattern
// that reduces to notAllowed rejects the document; a pattern that is nullable
// after consuming all input accepts it. Unlike an ad-hoc matcher, this handles
// element order, interleave, attributes at every level, and name classes
// uniformly and correctly.

import (
	"strings"

	"github.com/mgilbir/relaxngo/rng"
)

// ---- Name classes ----------------------------------------------------------

type nameClass interface {
	contains(ns, local string) bool
}

// ncAny matches any name, minus an optional exception.
type ncAny struct{ except nameClass }

func (n ncAny) contains(ns, local string) bool {
	return n.except == nil || !n.except.contains(ns, local)
}

// ncNs matches any name in a namespace, minus an optional exception.
type ncNs struct {
	ns     string
	except nameClass
}

func (n ncNs) contains(ns, local string) bool {
	return ns == n.ns && (n.except == nil || !n.except.contains(ns, local))
}

// ncName matches one qualified name.
type ncName struct{ ns, local string }

func (n ncName) contains(ns, local string) bool { return ns == n.ns && local == n.local }

// ncChoice matches either alternative.
type ncChoice struct{ a, b nameClass }

func (n ncChoice) contains(ns, local string) bool {
	return n.a.contains(ns, local) || n.b.contains(ns, local)
}

// ---- Patterns --------------------------------------------------------------

type pat interface{ isPat() }

type (
	pEmpty      struct{}
	pNotAllowed struct{}
	pText       struct{}
	pChoice     struct{ a, b pat }
	pGroup      struct{ a, b pat }
	pInterleave struct{ a, b pat }
	pOneOrMore  struct{ p pat }
	pList       struct{ p pat }
	pData       struct {
		typ, lib string
		params   []dParam
		except   pat // nil unless <data> has an <except>
	}
	pValue struct {
		typ, lib string
		value    string
	}
	pAttr struct {
		nc nameClass
		p  pat
	}
	pElem struct {
		nc nameClass
		p  pat
	}
	// pAfter p q: p must still be matched within the current element, then q
	// applies to what follows. It arises only during derivation.
	pAfter struct{ a, b pat }
	// pRef refers to a named define, resolved lazily against the environment.
	pRef struct{ name string }
)

type dParam struct{ name, value string }

func (pEmpty) isPat()      {}
func (pNotAllowed) isPat() {}
func (pText) isPat()       {}
func (pChoice) isPat()     {}
func (pGroup) isPat()      {}
func (pInterleave) isPat() {}
func (pOneOrMore) isPat()  {}
func (pList) isPat()       {}
func (pData) isPat()       {}
func (pValue) isPat()      {}
func (pAttr) isPat()       {}
func (pElem) isPat()       {}
func (pAfter) isPat()      {}
func (pRef) isPat()        {}

var (
	empty      pat = pEmpty{}
	notAllowed pat = pNotAllowed{}
	anyText    pat = pText{}
)

func isNotAllowed(p pat) bool { _, ok := p.(pNotAllowed); return ok }
func isEmpty(p pat) bool      { _, ok := p.(pEmpty); return ok }

// ---- Smart constructors (Clark §"Pattern construction") --------------------

func choice(a, b pat) pat {
	switch {
	case isNotAllowed(a):
		return b
	case isNotAllowed(b):
		return a
	}
	return pChoice{a, b}
}

func group(a, b pat) pat {
	switch {
	case isNotAllowed(a) || isNotAllowed(b):
		return notAllowed
	case isEmpty(a):
		return b
	case isEmpty(b):
		return a
	}
	return pGroup{a, b}
}

func interleave(a, b pat) pat {
	switch {
	case isNotAllowed(a) || isNotAllowed(b):
		return notAllowed
	case isEmpty(a):
		return b
	case isEmpty(b):
		return a
	}
	return pInterleave{a, b}
}

func after(a, b pat) pat {
	if isNotAllowed(a) || isNotAllowed(b) {
		return notAllowed
	}
	return pAfter{a, b}
}

func oneOrMore(p pat) pat {
	if isNotAllowed(p) {
		return notAllowed
	}
	return pOneOrMore{p}
}

// ---- The deriver -----------------------------------------------------------

// deriver holds the define environment and datatype helpers used while
// computing derivatives.
type deriver struct {
	defines map[string]pat
	dtx     *validationContext // used only for pure datatype/value/facet checks
	depth   int                // guards against pathological ref recursion
}

const maxRefDepth = 100000

func (d *deriver) resolve(name string) pat {
	if p, ok := d.defines[name]; ok {
		return p
	}
	return notAllowed
}

// nullable reports whether p can match the empty sequence.
func (d *deriver) nullable(p pat) bool { return d.nullableV(p, nil) }

func (d *deriver) nullableV(p pat, visiting map[string]bool) bool {
	switch t := p.(type) {
	case pEmpty, pText:
		return true
	case pGroup:
		return d.nullableV(t.a, visiting) && d.nullableV(t.b, visiting)
	case pInterleave:
		return d.nullableV(t.a, visiting) && d.nullableV(t.b, visiting)
	case pChoice:
		return d.nullableV(t.a, visiting) || d.nullableV(t.b, visiting)
	case pOneOrMore:
		return d.nullableV(t.p, visiting)
	case pAfter:
		// After is treated as non-nullable except via endTagDeriv.
		return false
	case pRef:
		if visiting[t.name] {
			return false
		}
		if visiting == nil {
			visiting = map[string]bool{}
		}
		visiting[t.name] = true
		r := d.nullableV(d.resolve(t.name), visiting)
		delete(visiting, t.name)
		return r
	default:
		// element, attribute, data, value, list, notAllowed
		return false
	}
}

// applyAfter maps f over the "after" continuations of p.
func (d *deriver) applyAfter(f func(pat) pat, p pat) pat {
	switch t := p.(type) {
	case pAfter:
		return after(t.a, f(t.b))
	case pChoice:
		return choice(d.applyAfter(f, t.a), d.applyAfter(f, t.b))
	case pNotAllowed:
		return notAllowed
	default:
		return notAllowed
	}
}

// startTagOpenDeriv: derivative w.r.t. an opening tag with the given name.
func (d *deriver) startTagOpenDeriv(p pat, ns, local string) pat {
	switch t := p.(type) {
	case pChoice:
		return choice(d.startTagOpenDeriv(t.a, ns, local), d.startTagOpenDeriv(t.b, ns, local))
	case pElem:
		if t.nc.contains(ns, local) {
			return after(t.p, empty)
		}
		return notAllowed
	case pInterleave:
		return choice(
			d.applyAfter(func(x pat) pat { return interleave(x, t.b) }, d.startTagOpenDeriv(t.a, ns, local)),
			d.applyAfter(func(x pat) pat { return interleave(t.a, x) }, d.startTagOpenDeriv(t.b, ns, local)),
		)
	case pOneOrMore:
		return d.applyAfter(
			func(x pat) pat { return group(x, choice(pOneOrMore{t.p}, empty)) },
			d.startTagOpenDeriv(t.p, ns, local),
		)
	case pGroup:
		x := d.applyAfter(func(y pat) pat { return group(y, t.b) }, d.startTagOpenDeriv(t.a, ns, local))
		if d.nullable(t.a) {
			return choice(x, d.startTagOpenDeriv(t.b, ns, local))
		}
		return x
	case pAfter:
		return d.applyAfter(func(x pat) pat { return after(x, t.b) }, d.startTagOpenDeriv(t.a, ns, local))
	case pRef:
		if d.depth++; d.depth > maxRefDepth {
			d.depth--
			return notAllowed
		}
		r := d.startTagOpenDeriv(d.resolve(t.name), ns, local)
		d.depth--
		return r
	default:
		return notAllowed
	}
}

// attsDeriv consumes a list of attributes.
func (d *deriver) attsDeriv(p pat, atts []attNode) pat {
	for i := range atts {
		p = d.attDeriv(p, atts[i].ns, atts[i].local, atts[i].value)
		if isNotAllowed(p) {
			return p
		}
	}
	return p
}

func (d *deriver) attDeriv(p pat, ns, local, value string) pat {
	switch t := p.(type) {
	case pAfter:
		return after(d.attDeriv(t.a, ns, local, value), t.b)
	case pChoice:
		return choice(d.attDeriv(t.a, ns, local, value), d.attDeriv(t.b, ns, local, value))
	case pGroup:
		return choice(
			group(d.attDeriv(t.a, ns, local, value), t.b),
			group(t.a, d.attDeriv(t.b, ns, local, value)),
		)
	case pInterleave:
		return choice(
			interleave(d.attDeriv(t.a, ns, local, value), t.b),
			interleave(t.a, d.attDeriv(t.b, ns, local, value)),
		)
	case pOneOrMore:
		return group(d.attDeriv(t.p, ns, local, value), choice(pOneOrMore{t.p}, empty))
	case pAttr:
		if t.nc.contains(ns, local) && d.valueMatch(t.p, value) {
			return empty
		}
		return notAllowed
	case pRef:
		if d.depth++; d.depth > maxRefDepth {
			d.depth--
			return notAllowed
		}
		r := d.attDeriv(d.resolve(t.name), ns, local, value)
		d.depth--
		return r
	default:
		return notAllowed
	}
}

// valueMatch reports whether the attribute/value pattern p accepts the string s.
func (d *deriver) valueMatch(p pat, s string) bool {
	if d.nullable(p) && isWhitespace(s) {
		return true
	}
	return d.nullable(d.textDeriv(p, s))
}

// startTagCloseDeriv: after all attributes, any remaining required attribute
// pattern makes the pattern notAllowed.
func (d *deriver) startTagCloseDeriv(p pat) pat {
	switch t := p.(type) {
	case pAfter:
		return after(d.startTagCloseDeriv(t.a), t.b)
	case pChoice:
		return choice(d.startTagCloseDeriv(t.a), d.startTagCloseDeriv(t.b))
	case pGroup:
		return group(d.startTagCloseDeriv(t.a), d.startTagCloseDeriv(t.b))
	case pInterleave:
		return interleave(d.startTagCloseDeriv(t.a), d.startTagCloseDeriv(t.b))
	case pOneOrMore:
		return oneOrMore(d.startTagCloseDeriv(t.p))
	case pAttr:
		return notAllowed
	case pRef:
		if d.depth++; d.depth > maxRefDepth {
			d.depth--
			return notAllowed
		}
		r := d.startTagCloseDeriv(d.resolve(t.name))
		d.depth--
		return r
	default:
		return p
	}
}

// textDeriv: derivative w.r.t. a text string.
func (d *deriver) textDeriv(p pat, s string) pat {
	switch t := p.(type) {
	case pChoice:
		return choice(d.textDeriv(t.a, s), d.textDeriv(t.b, s))
	case pInterleave:
		return choice(
			interleave(d.textDeriv(t.a, s), t.b),
			interleave(t.a, d.textDeriv(t.b, s)),
		)
	case pGroup:
		g := group(d.textDeriv(t.a, s), t.b)
		if d.nullable(t.a) {
			return choice(g, d.textDeriv(t.b, s))
		}
		return g
	case pAfter:
		return after(d.textDeriv(t.a, s), t.b)
	case pOneOrMore:
		return group(d.textDeriv(t.p, s), choice(pOneOrMore{t.p}, empty))
	case pText:
		return anyText
	case pValue:
		if d.valueEquals(t, s) {
			return empty
		}
		return notAllowed
	case pData:
		if d.dataAllows(t, s) {
			if t.except != nil && d.nullable(d.textDeriv(t.except, s)) {
				return notAllowed
			}
			return empty
		}
		return notAllowed
	case pList:
		if d.nullable(d.listDeriv(t.p, splitWhitespace(s))) {
			return empty
		}
		return notAllowed
	case pRef:
		if d.depth++; d.depth > maxRefDepth {
			d.depth--
			return notAllowed
		}
		r := d.textDeriv(d.resolve(t.name), s)
		d.depth--
		return r
	default:
		return notAllowed
	}
}

func (d *deriver) listDeriv(p pat, tokens []string) pat {
	for _, tok := range tokens {
		p = d.textDeriv(p, tok)
		if isNotAllowed(p) {
			return p
		}
	}
	return p
}

// endTagDeriv: closing the current element.
func (d *deriver) endTagDeriv(p pat) pat {
	switch t := p.(type) {
	case pChoice:
		return choice(d.endTagDeriv(t.a), d.endTagDeriv(t.b))
	case pAfter:
		if d.nullable(t.a) {
			return t.b
		}
		return notAllowed
	default:
		return notAllowed
	}
}

// ---- Datatype helpers (delegate to the existing, tested implementations) ----

func (d *deriver) dataAllows(p pData, s string) bool {
	params := make([]rng.Param, len(p.params))
	for i, pm := range p.params {
		params[i] = rng.Param{Name: pm.name, Value: pm.value}
	}
	return d.dtx.validateDataTypeWithFacets(p.typ, s, params)
}

func (d *deriver) valueEquals(p pValue, s string) bool {
	return d.dtx.valueMatches(rng.Value{Type: p.typ, Value: p.value, DatatypeLibrary: p.lib}, s)
}

// ---- text/whitespace utilities ---------------------------------------------

// expectedElemNs reports, for an element with the given local name that failed
// to match, a namespace the schema does expect for that local name (if the only
// mismatch is the namespace). ok is false if the local name is not expected at
// all here.
func (d *deriver) expectedElemNs(p pat, local string) (ns string, ok bool) {
	var walk func(pat, map[string]bool) (string, bool)
	walk = func(p pat, visiting map[string]bool) (string, bool) {
		switch t := p.(type) {
		case pElem:
			if n, isName := t.nc.(ncName); isName && n.local == local {
				return n.ns, true
			}
		case pChoice:
			if s, o := walk(t.a, visiting); o {
				return s, o
			}
			return walk(t.b, visiting)
		case pGroup:
			if s, o := walk(t.a, visiting); o {
				return s, o
			}
			return walk(t.b, visiting)
		case pInterleave:
			if s, o := walk(t.a, visiting); o {
				return s, o
			}
			return walk(t.b, visiting)
		case pOneOrMore:
			return walk(t.p, visiting)
		case pAfter:
			return walk(t.a, visiting)
		case pRef:
			if !visiting[t.name] {
				visiting[t.name] = true
				s, o := walk(d.resolve(t.name), visiting)
				delete(visiting, t.name)
				return s, o
			}
		}
		return "", false
	}
	return walk(p, map[string]bool{})
}

// expectedElemNames enumerates the concrete element names that p accepts as its
// next start tag (its "first" set), for populating ValidationError.Expected.
// It is an over-approximation: it does not prune branches made unreachable by
// earlier siblings, which is acceptable for a diagnostic hint.
func (d *deriver) expectedElemNames(p pat) []string {
	seen := map[string]bool{}
	var out []string
	var walk func(pat, map[string]bool)
	walk = func(p pat, visiting map[string]bool) {
		switch t := p.(type) {
		case pElem:
			if n, ok := t.nc.(ncName); ok && !seen[n.local] {
				seen[n.local] = true
				out = append(out, n.local)
			}
		case pChoice:
			walk(t.a, visiting)
			walk(t.b, visiting)
		case pGroup:
			walk(t.a, visiting)
			if d.nullable(t.a) {
				walk(t.b, visiting) // b is reachable as "first" only if a can be empty
			}
		case pInterleave:
			walk(t.a, visiting)
			walk(t.b, visiting)
		case pOneOrMore:
			walk(t.p, visiting)
		case pAfter:
			walk(t.a, visiting)
		case pRef:
			if !visiting[t.name] {
				visiting[t.name] = true
				walk(d.resolve(t.name), visiting)
				delete(visiting, t.name)
			}
		}
	}
	walk(p, map[string]bool{})
	return out
}

// requiredAttrNames collects the concrete attribute names still required by p
// (used to build a helpful "missing required attribute" message).
func (d *deriver) requiredAttrNames(p pat) []string {
	seen := map[string]bool{}
	var out []string
	var walk func(pat, map[string]bool)
	walk = func(p pat, visiting map[string]bool) {
		switch t := p.(type) {
		case pAttr:
			if n, ok := t.nc.(ncName); ok && !seen[n.local] {
				seen[n.local] = true
				out = append(out, n.local)
			}
		case pChoice:
			walk(t.a, visiting)
			walk(t.b, visiting)
		case pGroup:
			walk(t.a, visiting)
			walk(t.b, visiting)
		case pInterleave:
			walk(t.a, visiting)
			walk(t.b, visiting)
		case pOneOrMore:
			walk(t.p, visiting)
		case pAfter:
			walk(t.a, visiting)
		case pRef:
			if !visiting[t.name] {
				visiting[t.name] = true
				walk(d.resolve(t.name), visiting)
				delete(visiting, t.name)
			}
		}
	}
	walk(p, map[string]bool{})
	return out
}

func isWhitespace(s string) bool {
	for _, r := range s {
		if r != ' ' && r != '\t' && r != '\n' && r != '\r' {
			return false
		}
	}
	return true
}

func splitWhitespace(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
}

package validator

// Driver: materialize the XML document as a node tree and validate it by
// computing the derivative of the schema pattern with respect to each node.

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"

	"github.com/mgilbir/relaxngo/rng"
)

// derivEngine holds a translated grammar ready for validation.
type derivEngine struct {
	start   pat
	defines map[string]pat
}

// buildDerivEngine attempts to translate grammar; returns nil if any construct
// is unsupported (the caller then uses the legacy engine).
func buildDerivEngine(grammar *rng.Grammar) *derivEngine {
	start, defs, err := buildGrammar(grammar)
	if err != nil {
		return nil
	}
	return &derivEngine{start: start, defines: defs}
}

// ---- document node tree ----------------------------------------------------

type attNode struct{ ns, local, value string }

type childNode struct {
	isText bool
	text   string
	elem   *elemNode
}

type elemNode struct {
	ns, local string
	atts      []attNode
	children  []childNode
	line, col int
}

// validateDerivative validates data against the translated grammar.
func (v *Validator) validateDerivative(data []byte) ([]ValidationError, error) {
	root, err := buildDocTree(data)
	if err != nil {
		return nil, fmt.Errorf("XML parsing error: %w", err)
	}
	if root == nil {
		return []ValidationError{{Message: "document has no root element"}}, nil
	}

	d := &deriver{defines: v.deriv.defines, dtx: &validationContext{options: v.options}}
	var errs []ValidationError
	res := d.childDerivErr(v.deriv.start, childNode{elem: root}, &errs)
	if !d.nullable(res) && len(errs) == 0 {
		errs = append(errs, ValidationError{
			Line: root.line, Column: root.col, Element: root.local,
			Message: fmt.Sprintf("element '%s' does not match the schema", root.local),
		})
	}
	// Only the first (deepest) located failure is meaningful; the derivative
	// carries no notion of "more errors".
	if len(errs) > 1 {
		errs = errs[:1]
	}
	return errs, nil
}

// ---- error-localizing derivative walk --------------------------------------

func (d *deriver) childDerivErr(p pat, c childNode, errs *[]ValidationError) pat {
	if c.isText {
		return d.textDeriv(p, c.text)
	}
	e := c.elem
	p1 := d.startTagOpenDeriv(p, e.ns, e.local)
	if isNotAllowed(p1) {
		if wantNs, ok := d.expectedElemNs(p, e.local); ok && wantNs != e.ns {
			d.report(errs, e, fmt.Sprintf("element '%s' has namespace %q, expected %q", e.local, e.ns, wantNs))
		} else {
			d.report(errs, e, fmt.Sprintf("unexpected element '%s'", e.local))
		}
		return notAllowed
	}
	p2 := d.attsDerivErr(p1, e, errs)
	if isNotAllowed(p2) {
		return notAllowed
	}
	p3 := d.startTagCloseDeriv(p2)
	if isNotAllowed(p3) {
		if names := d.requiredAttrNames(p2); len(names) > 0 {
			d.report(errs, e, fmt.Sprintf("missing required attribute '%s' on element '%s'", names[0], e.local))
		} else {
			d.report(errs, e, fmt.Sprintf("missing required attribute on element '%s'", e.local))
		}
		return notAllowed
	}
	p4 := d.childrenDerivErr(p3, e, errs)
	if isNotAllowed(p4) {
		return notAllowed
	}
	p5 := d.endTagDeriv(p4)
	if isNotAllowed(p5) {
		d.report(errs, e, fmt.Sprintf("incomplete or invalid content in element '%s'", e.local))
		return notAllowed
	}
	return p5
}

func (d *deriver) attsDerivErr(p pat, e *elemNode, errs *[]ValidationError) pat {
	for i := range e.atts {
		a := e.atts[i]
		np := d.attDeriv(p, a.ns, a.local, a.value)
		if isNotAllowed(np) {
			d.report(errs, e, fmt.Sprintf("unexpected or invalid attribute '%s' on element '%s'", a.local, e.local))
			return notAllowed
		}
		p = np
	}
	return p
}

func (d *deriver) childrenDerivErr(p pat, e *elemNode, errs *[]ValidationError) pat {
	children := e.children
	// An element with no child nodes has an (empty) string value: it may match
	// empty content, or a text/data/value pattern against the empty string.
	if len(children) == 0 {
		return choice(p, d.textDeriv(p, ""))
	}
	if len(children) == 1 && children[0].isText {
		s := children[0].text
		if isWhitespace(s) {
			// A single whitespace-only text child lets the element also match
			// empty content.
			return choice(p, d.textDeriv(p, s))
		}
		np := d.textDeriv(p, s)
		if isNotAllowed(np) {
			d.report(errs, e, fmt.Sprintf("invalid text content in element '%s'", e.local))
		}
		return np
	}
	for i := range children {
		c := children[i]
		if c.isText && isWhitespace(c.text) {
			continue // whitespace between elements is not significant
		}
		p = d.childDerivErr(p, c, errs)
		if isNotAllowed(p) {
			return notAllowed
		}
	}
	return p
}

func (d *deriver) report(errs *[]ValidationError, e *elemNode, msg string) {
	if len(*errs) > 0 {
		return // keep the deepest/first located failure
	}
	*errs = append(*errs, ValidationError{Line: e.line, Column: e.col, Element: e.local, Message: msg})
}

// ---- document tree construction --------------------------------------------

func buildDocTree(data []byte) (*elemNode, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		off := dec.InputOffset()
		tok, err := dec.Token()
		if err == io.EOF {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if se, ok := tok.(xml.StartElement); ok {
			return readElement(dec, data, se, off)
		}
	}
}

func readElement(dec *xml.Decoder, data []byte, se xml.StartElement, startOff int64) (*elemNode, error) {
	line, col := offsetToLineCol(data, int(startOff))
	n := &elemNode{ns: se.Name.Space, local: se.Name.Local, line: line, col: col}
	for _, a := range se.Attr {
		if isNamespaceDecl(a.Name) {
			continue
		}
		n.atts = append(n.atts, attNode{ns: a.Name.Space, local: a.Name.Local, value: a.Value})
	}

	var textBuf []byte // coalesce adjacent character data into one text node
	flush := func() {
		if textBuf != nil {
			n.children = append(n.children, childNode{isText: true, text: string(textBuf)})
			textBuf = nil
		}
	}
	for {
		off := dec.InputOffset()
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			flush()
			child, err := readElement(dec, data, t, off)
			if err != nil {
				return nil, err
			}
			n.children = append(n.children, childNode{elem: child})
		case xml.CharData:
			textBuf = append(textBuf, t...)
		case xml.EndElement:
			flush()
			return n, nil
		}
	}
}

func isNamespaceDecl(name xml.Name) bool {
	return name.Local == "xmlns" ||
		name.Space == "xmlns" ||
		name.Space == "http://www.w3.org/2000/xmlns/"
}

func offsetToLineCol(data []byte, off int) (line, col int) {
	if off > len(data) {
		off = len(data)
	}
	line, col = 1, 1
	for i := 0; i < off; i++ {
		if data[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}

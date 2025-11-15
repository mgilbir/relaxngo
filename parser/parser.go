package parser

import (
	"encoding/xml"
	"io"
)

// Document represents an XML document that can be validated.
type Document interface {
	Validate() error
}

// ParseXML parses an XML document from an io.Reader into the provided interface.
// It uses Go's standard encoding/xml package for parsing.
func ParseXML(r io.Reader, v interface{}) error {
	decoder := xml.NewDecoder(r)
	return decoder.Decode(v)
}

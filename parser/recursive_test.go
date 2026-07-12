package parser

import (
	"encoding/xml"
	"strings"
	"testing"
	"time"
)

type recNode struct {
	XMLName  xml.Name  `xml:"node"`
	Name     string    `xml:"name"`
	Children []recNode `xml:"node"`
}

// TestStrictParseXML_RecursiveType ensures a self-referential struct type does
// not send the reflection walk into an infinite loop. Before the type-cycle
// guard this call never returned.
func TestStrictParseXML_RecursiveType(t *testing.T) {
	doc := `<node><name>a</name><node><name>b</name><node><name>c</name></node></node></node>`

	done := make(chan error, 1)
	go func() {
		var n recNode
		done <- StrictParseXML(strings.NewReader(doc), &n)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("StrictParseXML on recursive type returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("StrictParseXML did not terminate on a recursive struct type")
	}
}

// TestStrictParseXML_RecursiveType_DetectsUnknown confirms strict checking still
// flags unknown fields on the recursive type (at least at the top level).
func TestStrictParseXML_RecursiveType_DetectsUnknown(t *testing.T) {
	doc := `<node><name>a</name><bogus>x</bogus></node>`
	var n recNode
	err := StrictParseXML(strings.NewReader(doc), &n)
	if err == nil {
		t.Fatal("expected unknown-field error for <bogus>, got nil")
	}
	if ufe, ok := err.(*UnknownFieldError); !ok {
		t.Fatalf("expected *UnknownFieldError, got %T: %v", err, err)
	} else if len(ufe.UnknownElements) == 0 {
		t.Errorf("expected unknown element to be reported, got %+v", ufe)
	}
}

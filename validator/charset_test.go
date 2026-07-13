package validator

import (
	"strings"
	"testing"

	"github.com/mgilbir/relaxngo/rng"
)

// A document may declare a non-UTF-8 encoding; the validator must decode it
// rather than failing outright.
func TestCharsetDecoding(t *testing.T) {
	// Schema: <name> holds a string (any text). We feed accented Latin-1 bytes.
	const schema = `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
		<start><element name="name"><text/></element></start>
	</grammar>`
	g, err := rng.ParseSchema(strings.NewReader(schema))
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	v := NewValidator(g, DefaultOptions())

	// "café" with é as the ISO-8859-1 byte 0xE9.
	latin1 := "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><name>caf\xe9</name>"
	if errs, err := v.Validate(strings.NewReader(latin1)); err != nil {
		t.Fatalf("ISO-8859-1 document should decode, got error: %v", err)
	} else if len(errs) != 0 {
		t.Fatalf("ISO-8859-1 document should be valid, got: %v", errs)
	}

	// UTF-8 still works.
	utf8 := `<?xml version="1.0" encoding="UTF-8"?><name>café</name>`
	if errs, err := v.Validate(strings.NewReader(utf8)); err != nil || len(errs) != 0 {
		t.Fatalf("UTF-8 document should be valid: errs=%v err=%v", errs, err)
	}

	// An unsupported charset yields a clear error rather than a silent mis-decode.
	other := `<?xml version="1.0" encoding="Shift_JIS"?><name>x</name>`
	if _, err := v.Validate(strings.NewReader(other)); err == nil {
		t.Fatal("unsupported charset should return an error")
	} else if !strings.Contains(err.Error(), "encoding") {
		t.Fatalf("expected an encoding error, got: %v", err)
	}
}

package validator

import (
	"strings"
	"sync"
	"testing"

	"github.com/mgilbir/relaxngo/rng"
)

// TestConcurrentValidate_PatternFacet exercises the shared regex cache from many
// goroutines. Run with -race; before the cache was guarded this panicked with a
// concurrent map read/write.
func TestConcurrentValidate_PatternFacet(t *testing.T) {
	schema := `<grammar xmlns="http://relaxng.org/ns/structure/1.0" datatypeLibrary="http://www.w3.org/2001/XMLSchema-datatypes">
<start><element name="r"><data type="string"><param name="pattern">[0-9]+</param></data></element></start></grammar>`
	g, err := rng.ParseSchema(strings.NewReader(schema))
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	v := NewValidator(g, DefaultOptions())
	doc := `<r>12345</r>`

	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := v.Validate(strings.NewReader(doc)); err != nil {
				t.Errorf("validate: %v", err)
			}
		}()
	}
	wg.Wait()
}

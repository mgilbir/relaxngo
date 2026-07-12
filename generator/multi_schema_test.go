package generator_test

import (
	"strings"
	"testing"

	"github.com/mgilbir/relaxngo/generator"
	"github.com/mgilbir/relaxngo/rng"
)

func genCode(t *testing.T, schema, pkg string) string {
	t.Helper()
	g, err := rng.ParseSchema(strings.NewReader(schema))
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	types, err := generator.GenerateTypes(g)
	if err != nil {
		t.Fatalf("generate types: %v", err)
	}
	code, err := generator.GenerateCode(types, pkg, schema, g)
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}
	return code
}

// Two schemas generated into the same package must not collide on shared helper
// or variable names. Previously both emitted a package-level
// formatValidationErrors, so the second file failed to compile.
func TestTwoSchemasSharePackageWithoutCollision(t *testing.T) {
	alpha := genCode(t, `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
		<start><element name="alpha"><attribute name="a"><text/></attribute></element></start>
	</grammar>`, "p")
	beta := genCode(t, `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
		<start><element name="beta"><attribute name="b"><text/></attribute></element></start>
	</grammar>`, "p")

	// Neither file may declare the bare, collision-prone helper name.
	for name, code := range map[string]string{"alpha": alpha, "beta": beta} {
		if strings.Contains(code, "func formatValidationErrors(") {
			t.Errorf("%s: emits collision-prone bare formatValidationErrors; want a schema-unique name", name)
		}
	}
	// Each file's helper name must be unique to its schema.
	if !strings.Contains(alpha, "func formatValidationErrorsAlpha(") {
		t.Error("alpha: expected schema-unique helper formatValidationErrorsAlpha")
	}
	if !strings.Contains(beta, "func formatValidationErrorsBeta(") {
		t.Error("beta: expected schema-unique helper formatValidationErrorsBeta")
	}
}

// The generated schema initializer must not panic on a parse failure. It should
// record the error and return it, so validation is not silently disabled.
func TestGeneratedInitDoesNotPanic(t *testing.T) {
	code := genCode(t, `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
		<start><element name="root"><text/></element></start>
	</grammar>`, "p")
	if strings.Contains(code, "panic(\"failed to parse embedded schema") {
		t.Error("generated init still panics on schema parse failure; want an error return")
	}
	if !strings.Contains(code, "failed to parse embedded schema: %w") {
		t.Error("generated init should return a wrapped parse error")
	}
}

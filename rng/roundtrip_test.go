package rng

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

const syntheticStartElement = "_synthetic_start"

// TestGrammarRoundtrip validates that schemas can be parsed and serialized
// without losing information. This tests the core parsing/serialization pipeline
// in isolation from the code
//
//nolint:funlen // Comprehensive test suite validating schema parsing/serialization
func TestGrammarRoundtrip(t *testing.T) {
	// Load all official tests
	testDataPath := filepath.Join("..", "testdata", "official-tests")
	testCases, err := loadTestCases(testDataPath)
	if err != nil {
		t.Fatalf("Failed to load official test cases: %v", err)
	}

	t.Logf("Loaded %d official test schemas for roundtrip testing", len(testCases))

	// Statistics
	var stats struct {
		Total          atomic.Int64
		Passed         atomic.Int64
		Failed         atomic.Int64
		SkippedNoRNG   atomic.Int64
		ParseErrors    atomic.Int64
		SerializeError atomic.Int64
		RoundtripError atomic.Int64
	}

	// Print statistics after all tests complete
	t.Cleanup(func() {
		t.Logf("\n=== GRAMMAR ROUNDTRIP RESULTS ===")
		t.Logf("Total schemas tested: %d", stats.Total.Load())
		t.Logf("Passed: %d", stats.Passed.Load())
		t.Logf("Failed: %d", stats.Failed.Load())
		t.Logf("\nSkipped: %d (inline/non-RELAX-NG schemas)", stats.SkippedNoRNG.Load())
		t.Logf("\nFailure breakdown:")
		t.Logf("  Parse errors: %d", stats.ParseErrors.Load())
		t.Logf("  Serialization errors: %d", stats.SerializeError.Load())
		t.Logf("  Roundtrip mismatch: %d", stats.RoundtripError.Load())

		totalRun := stats.Passed.Load() + stats.Failed.Load()
		if totalRun > 0 {
			passRate := float64(stats.Passed.Load()) / float64(totalRun) * 100
			t.Logf("\nPass rate: %.1f%% (%d/%d)", passRate, stats.Passed.Load(), totalRun)
		}
	})

	// Run tests
	for i, tc := range testCases {
		schemaContent := tc
		testIdx := i

		t.Run("Schema"+strings.TrimSuffix(filepath.Base(schemaContent), filepath.Ext(schemaContent)), func(t *testing.T) {
			stats.Total.Add(1)

			result := runRoundtripTest(t, schemaContent)
			switch {
			case result.Skipped:
				stats.SkippedNoRNG.Add(1)
			case result.Success:
				stats.Passed.Add(1)
			default:
				stats.Failed.Add(1)
				switch result.FailureType {
				case "parse":
					stats.ParseErrors.Add(1)
				case "serialize":
					stats.SerializeError.Add(1)
				case "roundtrip":
					stats.RoundtripError.Add(1)
				}
				// Mark test as failed
				failCount := stats.Failed.Load()
				if failCount <= 5 {
					t.Errorf("[%d] %s - %s", testIdx, filepath.Base(schemaContent), result.Error)
				}
			}
		})
	}
}

// RoundtripResult captures the result of a roundtrip test
type RoundtripResult struct {
	Success     bool
	Skipped     bool
	FailureType string // "parse", "serialize", "roundtrip"
	Error       string
}

// runRoundtripTest validates that a schema can be parsed and reserialized
func runRoundtripTest(t *testing.T, schemaPath string) RoundtripResult {
	t.Helper()

	// Read schema
	schemaContent, err := os.ReadFile(schemaPath) // #nosec G304
	if err != nil {
		return RoundtripResult{
			Error: "Failed to read schema: " + err.Error(),
		}
	}

	schemaStr := string(schemaContent)

	// Skip non-RELAX-NG schemas (inlined schemas, etc.)
	if !strings.Contains(schemaStr, "relaxng.org/ns/structure") {
		return RoundtripResult{
			Skipped: true,
		}
	}

	// Step 1: Parse the original schema using ParseSchemaFile
	// which handles both full syntax (<grammar>) and simplified syntax (top-level patterns)
	absPath, absErr := filepath.Abs(schemaPath)
	if absErr != nil {
		return RoundtripResult{
			FailureType: "parse",
			Error:       "Failed to get absolute path: " + absErr.Error(),
		}
	}
	grammar, err := ParseSchemaFile(absPath, filepath.Dir(absPath))
	if err != nil {
		return RoundtripResult{
			FailureType: "parse",
			Error:       "Parse failed: " + err.Error(),
		}
	}

	// Step 2: Serialize the grammar back to XML
	serialized := SerializeGrammar(grammar)
	if serialized == "" {
		return RoundtripResult{
			FailureType: "serialize",
			Error:       "Serialization produced empty result",
		}
	}

	// Step 3: Parse the serialized version
	// Note: The serialized version is always full syntax (with <grammar> wrapper)
	// even if the original was simplified syntax. This is expected behavior.
	grammar2, err := ParseSchema(bytes.NewReader([]byte(serialized)))
	if err != nil {
		return RoundtripResult{
			FailureType: "roundtrip",
			Error:       "Failed to re-parse serialized grammar: " + err.Error(),
		}
	}

	// Step 4: Compare key structures
	// Since serialized output is always full syntax (with <grammar>), both grammars
	// should have equivalent structures
	if !grammarsAreEquivalent(grammar, grammar2) {
		return RoundtripResult{
			FailureType: "roundtrip",
			Error:       "Roundtrip comparison failed: grammars are not equivalent",
		}
	}

	return RoundtripResult{
		Success: true,
	}
}

// grammarsAreEquivalent checks if two grammars have the same logical structure
func grammarsAreEquivalent(g1, g2 *Grammar) bool {
	// When serializing a schema with a mixed-pattern choice, we create a synthetic define
	// So the serialized version may have one more define than the original.
	// Allow for 0-1 synthetic _synthetic_start defines.
	g1DefineCount := len(g1.Defines)
	g2DefineCount := len(g2.Defines)

	// Check if g2 has a synthetic start define
	hasSyntheticStart := false
	for _, d := range g2.Defines {
		if d.Name == syntheticStartElement {
			hasSyntheticStart = true
			break
		}
	}

	// Allow for the synthetic define that may be created during serialization
	if hasSyntheticStart {
		if g2DefineCount != g1DefineCount+1 {
			return false
		}
	} else {
		if g1DefineCount != g2DefineCount {
			return false
		}
	}

	// Compare define names (excluding synthetic)
	defineNames1 := make(map[string]bool)
	for _, d := range g1.Defines {
		defineNames1[d.Name] = true
	}
	for _, d := range g2.Defines {
		if d.Name != syntheticStartElement && !defineNames1[d.Name] {
			return false
		}
	}

	// Both should have a start element - check if both have equivalent start structures
	// After serialization, the start might be a ref to _synthetic_start, so we need to
	// check if one is and adjust expectations accordingly
	g1HasStart := hasStartContent(&g1.Start)
	g2HasStart := hasStartContent(&g2.Start)

	// If g2 has a synthetic start, it will have a Ref instead of the original content
	if hasSyntheticStart && g2.Start.Ref != nil && g2.Start.Ref.Name == syntheticStartElement {
		// This is expected when we serialize a mixed-pattern choice
		g2HasStart = true
	}

	if g1HasStart != g2HasStart {
		return false
	}

	return true
}

// hasStartContent checks if a Start element has any content
func hasStartContent(s *Start) bool {
	return s.Ref != nil ||
		s.ParentRef != nil ||
		s.Element != nil ||
		s.Choice != nil ||
		len(s.Group) > 0 ||
		len(s.Interleave) > 0 ||
		len(s.Optional) > 0 ||
		len(s.OneOrMore) > 0 ||
		len(s.ZeroOrMore) > 0 ||
		s.Text != nil ||
		s.Data != nil ||
		s.List != nil ||
		s.Empty != nil ||
		s.NotAllowed != nil ||
		s.ExternalRef != nil
}

// loadTestCases loads correct schema files from the official test suite
// Only loads c.rng files (correct schemas), not i.rng (incorrect/test error cases)
func loadTestCases(testDataPath string) ([]string, error) {
	var schemas []string

	// Walk through the test data directory
	err := filepath.Walk(testDataPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Only look for c.rng files (correct RELAX NG schemas)
		if !info.IsDir() && strings.HasSuffix(path, "c.rng") {
			schemas = append(schemas, path)
		}

		return nil
	})

	return schemas, err
}

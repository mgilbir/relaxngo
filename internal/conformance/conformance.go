// Package conformance runs the RELAX NG official test suite and folder-based
// test cases against the validator. It lives under internal/ so the harness
// (which pulls in os, html, and 1200 lines of test plumbing) is never compiled
// into external importers of the validator package.
package conformance

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mgilbir/relaxngo/validator"

	"github.com/mgilbir/relaxngo/rng"
)

const (
	// Error message constants
	errSchemaParseFailureExpected = "Expected schema parse to fail but it succeeded"
)

// VirtualFilesystem implements rng.ResourceResolver for in-memory resources
type VirtualFilesystem struct {
	resources map[string]string // path -> content
}

// NewVirtualFilesystem creates a new virtual filesystem
func NewVirtualFilesystem() *VirtualFilesystem {
	return &VirtualFilesystem{
		resources: make(map[string]string),
	}
}

// AddResource adds a resource to the virtual filesystem
func (vfs *VirtualFilesystem) AddResource(path, content string) {
	vfs.resources[path] = content
}

// ReadResource implements rng.ResourceResolver
func (vfs *VirtualFilesystem) ReadResource(path string) ([]byte, error) {
	content, ok := vfs.resources[path]
	if !ok {
		return nil, fmt.Errorf("resource not found: %s", path)
	}
	return []byte(content), nil
}

// buildVirtualFS creates a VirtualFilesystem from a test case's resources and dirs
func buildVirtualFS(tc *OfficialTestCase) *VirtualFilesystem {
	vfs := NewVirtualFilesystem()

	// Add top-level resources
	for _, res := range tc.Resources {
		vfs.AddResource(res.Name, string(res.Content))
	}

	// Recursively add resources from directories
	for _, dir := range tc.Dirs {
		addDirToVFS(vfs, dir.Name, &dir)
	}

	return vfs
}

// addDirToVFS recursively adds a directory and its contents to the VFS
func addDirToVFS(vfs *VirtualFilesystem, basePath string, dir *Dir) {
	// Add resources in this directory
	for _, res := range dir.Resources {
		path := basePath + "/" + res.Name
		vfs.AddResource(path, string(res.Content))
	}

	// Recursively add subdirectories
	for _, subDir := range dir.SubDirs {
		subPath := basePath + "/" + subDir.Name
		addDirToVFS(vfs, subPath, &subDir)
	}
}

// RawElement wraps raw XML element content
type RawElement struct {
	Content []byte `xml:",innerxml"`
}

// Resource represents a <resource> element defining a virtual file
type Resource struct {
	Name    string `xml:"name,attr"`
	Content []byte `xml:",innerxml"`
}

// Dir represents a <dir> element defining a virtual directory
type Dir struct {
	Name      string     `xml:"name,attr"`
	Resources []Resource `xml:"resource"`
	SubDirs   []Dir      `xml:"dir"`
}

// OfficialTestCase represents a single test case from the official test suite
type OfficialTestCase struct {
	XMLName   xml.Name
	Section   string     `xml:"section"`
	Title     string     `xml:"title"`
	Docs      string     `xml:"documentation"`
	RawXML    []byte     `xml:",innerxml"`
	Resources []Resource `xml:"resource"` // Top-level resources
	Dirs      []Dir      `xml:"dir"`      // Top-level directories
	Incorrect string
	Correct   string
	Valid     []string
	Invalid   []string
}

// OfficialTestSuite represents the root structure
type OfficialTestSuite struct {
	XMLName   xml.Name
	Author    string             `xml:"author"`
	Email     string             `xml:"email"`
	Docs      string             `xml:"documentation"`
	TestSuite []TestSuiteSection `xml:"testSuite"`
	TestCases []OfficialTestCase `xml:"testCase"`
}

// TestSuiteSection represents nested test suite sections
type TestSuiteSection struct {
	XMLName   xml.Name
	Section   string             `xml:"section"`
	Docs      string             `xml:"documentation"`
	TestSuite []TestSuiteSection `xml:"testSuite"`
	TestCases []OfficialTestCase `xml:"testCase"`
}

// TestResult represents the result of a single test
type TestResult struct {
	Section       string
	Category      string // "SyntaxError", "Valid", or "Invalid"
	Index         int
	TestID        string // Unique global test identifier
	Passed        bool
	Error         string
	Documentation string
}

// RunOfficialTestSuite runs tests from the official RELAX NG test suite
func RunOfficialTestSuite(testDataPath string) (results []TestResult, err error) {
	// Get the directory containing the test data file and use it as root for security
	rootDir := filepath.Dir(testDataPath)
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return nil, fmt.Errorf("failed to open root directory %s: %w", rootDir, err)
	}
	defer func() { _ = root.Close() }()

	// Read the test file relative to the root
	relPath := filepath.Base(testDataPath)
	data, err := root.ReadFile(relPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read test file: %w", err)
	}

	// Remove DOCTYPE to avoid entity resolution issues
	data = removeDoctype(data)

	var suite OfficialTestSuite
	if err := xml.Unmarshal(data, &suite); err != nil {
		return nil, fmt.Errorf("failed to parse test suite: %w", err)
	}

	// Post-process: extract content from RawXML
	parseTestCaseContent(&suite)

	// Flatten nested test suites and process all test cases
	// Use a global counter to ensure unique test IDs
	counter := &testIDCounter{testCaseID: 1, resultID: 1}
	results = processTestSuite(&suite, "", counter)
	return results, nil
}

// testIDCounter provides a global test case ID counter
type testIDCounter struct {
	testCaseID int // Incremented for each testCase in XML
	resultID   int // Incremented for each result generated
}

// nextTestCaseID returns the next unique test case ID
func (c *testIDCounter) nextTestCaseID() string {
	id := fmt.Sprintf("T-%03d", c.testCaseID)
	c.testCaseID++
	return id
}

// parseTestCaseContent extracts the actual content from RawXML fields
func parseTestCaseContent(suite *OfficialTestSuite) {
	for i := range suite.TestCases {
		extractTestCaseElements(&suite.TestCases[i])
	}
	for i := range suite.TestSuite {
		parseTestSectionContent(&suite.TestSuite[i])
	}
}

// parseTestSectionContent recursively parses nested test sections
func parseTestSectionContent(section *TestSuiteSection) {
	for i := range section.TestCases {
		extractTestCaseElements(&section.TestCases[i])
	}
	for i := range section.TestSuite {
		parseTestSectionContent(&section.TestSuite[i])
	}
}

// extractTestCaseElements extracts test case content from RawXML
func extractTestCaseElements(tc *OfficialTestCase) {
	content := string(tc.RawXML)

	// Extract single-occurrence elements
	tc.Incorrect = extractSingleElement(content, "<incorrect>", "</incorrect>")
	tc.Correct = extractSingleElement(content, "<correct>", "</correct>")

	// Extract multiple-occurrence elements
	tc.Valid = extractMultipleElements(content, "<valid>", "</valid>")
	tc.Invalid = extractMultipleElements(content, "<invalid>", "</invalid>")
}

// extractSingleElement extracts a single element from XML content
func extractSingleElement(content, openTag, closeTag string) string {
	start := strings.Index(content, openTag)
	if start == -1 {
		return ""
	}
	end := strings.Index(content[start:], closeTag)
	if end == -1 {
		return ""
	}
	begin := start + len(openTag)
	finish := start + end
	return extractInnerXML(content[begin:finish])
}

// extractMultipleElements extracts multiple occurrences of an element from XML content
func extractMultipleElements(content, openTag, closeTag string) []string {
	var result []string
	idx := 0
	for {
		elemStart := strings.Index(content[idx:], openTag)
		if elemStart == -1 {
			break
		}
		elemStart += idx
		elemEnd := strings.Index(content[elemStart:], closeTag)
		if elemEnd == -1 {
			break
		}
		start := elemStart + len(openTag)
		end := elemStart + elemEnd
		result = append(result, extractInnerXML(content[start:end]))
		idx = elemStart + elemEnd + len(closeTag)
	}
	return result
}

// extractInnerXML extracts and trims inner XML content
func extractInnerXML(content string) string {
	// Remove surrounding whitespace and empty lines
	lines := strings.Split(content, "\n")
	var cleaned []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			cleaned = append(cleaned, line)
		}
	}
	result := strings.TrimSpace(strings.Join(cleaned, "\n"))

	// Fix namespace prefixes: if the content uses ns0: prefix without namespace declaration,
	// add the RELAX NG namespace declaration to the root element
	if strings.Contains(result, "ns0:") && !strings.Contains(result, "xmlns") {
		result = normalizeNamespacePrefixes(result)
	}

	return result
}

// normalizeNamespacePrefixes converts ns0: prefixes to proper namespace declarations
func normalizeNamespacePrefixes(content string) string {
	// Replace ns0: with empty prefix and add xmlns declaration to root element
	// Find the first element tag
	idx := strings.Index(content, "<ns0:")
	if idx == -1 {
		return content
	}

	// Find the end of the opening tag
	endIdx := strings.Index(content[idx:], ">")
	if endIdx == -1 {
		return content
	}

	endIdx += idx

	// Check if xmlns is already present in the opening tag
	openTag := content[idx : endIdx+1]
	if strings.Contains(openTag, "xmlns") {
		return content
	}

	// Replace ns0: with regular prefix and add xmlns
	result := strings.ReplaceAll(content, "ns0:", "")

	// Add xmlns declaration to the opening tag
	// Find where to insert it (before the closing >)
	firstTagEnd := strings.Index(result, ">")
	if firstTagEnd > 0 {
		// Check if element already has xmlns
		if !strings.Contains(result[:firstTagEnd], "xmlns") {
			result = result[:firstTagEnd] + ` xmlns="http://relaxng.org/ns/structure/1.0"` + result[firstTagEnd:]
		}
	}

	return result
}

// processTestSuite recursively processes test suite sections and cases
func processTestSuite(suite *OfficialTestSuite, prefix string, counter *testIDCounter) []TestResult {
	var results []TestResult

	// Process direct test cases in this section
	for i, tc := range suite.TestCases {
		results = append(results, processTestCase(&tc, prefix, i, counter)...)
	}

	// Process nested test suites
	for _, section := range suite.TestSuite {
		results = append(results, processTestSuiteSection(&section, prefix, counter)...)
	}

	return results
}

// processTestSuiteSection processes a nested test suite section
func processTestSuiteSection(section *TestSuiteSection, prefix string, counter *testIDCounter) []TestResult {
	var results []TestResult

	sectionPrefix := prefix
	if section.Section != "" {
		sectionPrefix = section.Section
	}

	// Process test cases in this section
	for i, tc := range section.TestCases {
		results = append(results, processTestCase(&tc, sectionPrefix, i, counter)...)
	}

	// Process nested sections
	for _, subsection := range section.TestSuite {
		results = append(results, processTestSuiteSection(&subsection, sectionPrefix, counter)...)
	}

	return results
}

// processOfficialIncorrectSchema processes an incorrect schema test case from the official suite
func processOfficialIncorrectSchema(tc *OfficialTestCase, testID, section string, index int) TestResult {
	result := TestResult{
		Section:       section,
		Category:      "SyntaxError",
		Index:         index,
		TestID:        testID,
		Documentation: tc.Docs,
	}

	// Check if test has resources
	hasResources := len(tc.Resources) > 0 || len(tc.Dirs) > 0

	if hasResources {
		// Use virtual filesystem for tests with resources
		vfs := buildVirtualFS(tc)
		schemaStr := wrapSchemaIfNeeded(tc.Incorrect)
		vfs.AddResource("__main__.rng", schemaStr)
		_, err := rng.ParseSchemaWithResolver("__main__.rng", vfs)
		result = checkIncorrectSchemaParseResult(err, result)
	} else {
		// No resources - parse directly
		_, err := rng.ParseSchema(strings.NewReader(tc.Incorrect))
		result = checkIncorrectSchemaParseResult(err, result)
	}

	return result
}

// processOfficialCorrectSchema processes a correct schema test case from the official suite
func processOfficialCorrectSchema(tc *OfficialTestCase, testID, section string, index int) []TestResult {
	var results []TestResult

	schemaStr := wrapSchemaIfNeeded(tc.Correct)

	// Build virtual filesystem from resources and dirs
	vfs := buildVirtualFS(tc)

	// Add the main schema to the VFS (use a special name)
	vfs.AddResource("__main__.rng", schemaStr)

	// Parse using the virtual filesystem
	grammar, err := rng.ParseSchemaWithResolver("__main__.rng", vfs)
	if err != nil {
		// If the schema itself fails to parse, mark all related tests as failed
		result := TestResult{
			Section:       section,
			Category:      "SyntaxError",
			Index:         index,
			TestID:        testID,
			Passed:        false,
			Error:         fmt.Sprintf("Failed to parse correct schema: %v", err),
			Documentation: tc.Docs,
		}
		return []TestResult{result}
	}

	val := validator.NewValidator(grammar, validator.DefaultOptions())

	// Validate valid documents
	validResults := validateOfficialValidDocuments(tc, testID, section, index, val)
	results = append(results, validResults...)

	// Validate invalid documents
	invalidResults := validateOfficialInvalidDocuments(tc, testID, section, index, val)
	results = append(results, invalidResults...)

	return results
}

// validateOfficialValidDocuments validates documents that should pass from the official suite
func validateOfficialValidDocuments(tc *OfficialTestCase, testID, section string, index int, val *validator.Validator) []TestResult {
	results := make([]TestResult, 0, len(tc.Valid))

	for i, validDoc := range tc.Valid {
		result := TestResult{
			Section:       section,
			Category:      "Valid",
			Index:         index*1000 + i,
			TestID:        testID,
			Documentation: tc.Docs,
		}

		errs, err := val.Validate(strings.NewReader(validDoc))
		if err == nil && len(errs) == 0 {
			result.Passed = true
		} else {
			result.Passed = false
			if err != nil {
				result.Error = fmt.Sprintf("Validation error: %v", err)
			} else if len(errs) > 0 {
				result.Error = fmt.Sprintf("Validation failed: %v", errs[0])
			}
		}

		results = append(results, result)
	}

	return results
}

// validateOfficialInvalidDocuments validates documents that should fail from the official suite
func validateOfficialInvalidDocuments(tc *OfficialTestCase, testID, section string, index int, val *validator.Validator) []TestResult {
	results := make([]TestResult, 0, len(tc.Invalid))

	for i, invalidDoc := range tc.Invalid {
		result := TestResult{
			Section:       section,
			Category:      "Invalid",
			Index:         index*1000 + 10000 + i,
			TestID:        testID,
			Documentation: tc.Docs,
		}

		errs, err := val.Validate(strings.NewReader(invalidDoc))
		if err != nil || len(errs) > 0 {
			result.Passed = true
		} else {
			result.Passed = false
			result.Error = "Expected validation to fail but it succeeded"
		}

		results = append(results, result)
	}

	return results
}

// processTestCase processes a single test case
func processTestCase(tc *OfficialTestCase, section string, index int, counter *testIDCounter) []TestResult {
	var results []TestResult

	// Get the test case ID (based on XML position)
	testID := counter.nextTestCaseID()

	// Case 1: Incorrect schema (should fail to parse)
	if len(tc.Incorrect) > 0 {
		result := processOfficialIncorrectSchema(tc, testID, section, index)
		results = append(results, result)
	}

	// Case 2: Correct schema with validation tests
	if len(tc.Correct) > 0 {
		correctResults := processOfficialCorrectSchema(tc, testID, section, index)
		if correctResults == nil {
			return results
		}
		results = append(results, correctResults...)
	}

	return results
}

// wrapSchemaIfNeeded wraps XML content in a grammar element if it's not already a complete schema
func wrapSchemaIfNeeded(content string) string {
	trimmed := strings.TrimSpace(content)

	// Extract XML declaration if present
	var xmlDecl string
	var schemaContent string

	if strings.HasPrefix(trimmed, "<?xml") {
		endXMLDecl := strings.Index(trimmed, "?>")
		if endXMLDecl != -1 {
			xmlDecl = trimmed[:endXMLDecl+2]
			schemaContent = strings.TrimSpace(trimmed[endXMLDecl+2:])
		} else {
			schemaContent = trimmed
		}
	} else {
		schemaContent = trimmed
	}

	// Check if it starts with a grammar element
	if strings.HasPrefix(schemaContent, "<grammar") ||
		strings.HasPrefix(schemaContent, "<rng:grammar") {
		// Check if first tag contains :grammar prefix
		firstTagEnd := strings.Index(schemaContent, ">")
		if firstTagEnd != -1 {
			firstTag := schemaContent[:firstTagEnd]
			if strings.Contains(firstTag, ":grammar") {
				return trimmed // Already a complete schema
			}
		}
	}

	// Check if it's a RELAX NG element that can be used directly as a start pattern
	relaxNGElements := []string{
		"<element", "<attribute", "<group", "<choice", "<interleave",
		"<optional", "<zeroOrMore", "<oneOrMore", "<mixed",
		"<text", "<empty", "<notAllowed", "<ref", "<externalRef",
	}

	for _, elem := range relaxNGElements {
		if strings.HasPrefix(schemaContent, elem) {
			// This is a valid start pattern, wrap it
			if xmlDecl != "" {
				return xmlDecl + `
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    ` + schemaContent + `
  </start>
</grammar>`
			}
			return `<?xml version="1.0"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    ` + schemaContent + `
  </start>
</grammar>`
		}
	}

	// If we get here, it's already a complete schema or something unexpected
	return trimmed
}

// CountResults returns counts of passed and failed tests
func CountResults(results []TestResult) (passed, failed int, byCategory map[string][2]int) {
	byCategory = make(map[string][2]int)

	for _, r := range results {
		counts := byCategory[r.Category]
		if r.Passed {
			counts[0]++
		} else {
			counts[1]++
		}
		byCategory[r.Category] = counts

		if r.Passed {
			passed++
		} else {
			failed++
		}
	}

	return passed, failed, byCategory
}

// FilterResults filters test results by category
func FilterResults(results []TestResult, category string) []TestResult {
	var filtered []TestResult
	for _, r := range results {
		if r.Category == category {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

// removeDoctype removes DOCTYPE declaration and replaces entities
func removeDoctype(data []byte) []byte {
	str := string(data)

	// Extract entity definitions
	entities := make(map[string]string)
	doctypeStart := strings.Index(str, "<!DOCTYPE")
	if doctypeStart == -1 {
		return data
	}

	doctypeEnd := strings.Index(str[doctypeStart:], ">")
	if doctypeEnd == -1 {
		return data
	}

	doctype := str[doctypeStart : doctypeStart+doctypeEnd+1]
	// Extract entities like <!ENTITY dii "<&#xE14;&#xE35;/>">
	lines := strings.Split(doctype, "\n")
	for _, line := range lines {
		if strings.Contains(line, "<!ENTITY") {
			parseEntityDeclaration(line, entities)
		}
	}

	// Replace entity references in content
	for name, value := range entities {
		str = strings.ReplaceAll(str, "&"+name+";", value)
	}

	// Remove DOCTYPE
	str = str[:doctypeStart] + str[doctypeStart+doctypeEnd+1:]

	return []byte(str)
}

// parseEntityDeclaration parses a single entity declaration line
func parseEntityDeclaration(line string, entities map[string]string) {
	// Parse: <!ENTITY name "value">
	parts := strings.SplitN(line, "\"", 2)
	if len(parts) == 2 {
		nameStart := strings.Index(line, "ENTITY") + 6
		name := strings.TrimSpace(line[nameStart : strings.Index(line[nameStart:], "\"")+nameStart])
		value := parts[1]
		value = strings.TrimSuffix(value, "\">")
		// Decode HTML/XML entities
		value = html.UnescapeString(value)
		entities[name] = value
	}
}

// FormatResults formats test results for display
func FormatResults(results []TestResult, verbose bool) string {
	var buf bytes.Buffer

	passed, failed, byCategory := CountResults(results)
	total := passed + failed

	if total == 0 {
		return "No tests found\n"
	}

	// Summary
	fmt.Fprintf(&buf, "\n=== Official RELAX NG Test Suite Results ===\n")
	fmt.Fprintf(&buf, "Total:   %d tests\n", total)
	fmt.Fprintf(&buf, "Passed:  %d (%.1f%%)\n", passed, float64(passed)*100/float64(total))
	fmt.Fprintf(&buf, "Failed:  %d (%.1f%%)\n\n", failed, float64(failed)*100/float64(total))

	// By category
	fmt.Fprintf(&buf, "Results by Category:\n")
	categories := []string{"SyntaxError", "Valid", "Invalid"}
	for _, cat := range categories {
		counts := byCategory[cat]
		if counts[0]+counts[1] > 0 {
			fmt.Fprintf(&buf, "  %-12s: %d passed, %d failed\n", cat, counts[0], counts[1])
		}
	}

	// Verbose details
	if verbose {
		fmt.Fprintf(&buf, "\n=== Failed Tests ===\n")
		formatFailedTests(&buf, results)
	}

	return buf.String()
}

// formatFailedTests formats and outputs failed test details
func formatFailedTests(buf *bytes.Buffer, results []TestResult) {
	failCount := 0
	for _, r := range results {
		if !r.Passed {
			failCount++
			if failCount <= 20 { // Limit to first 20 failures
				fmt.Fprintf(buf, "[%s] %s - Section %s\n", r.Category, r.TestID, r.Section)
				if r.Documentation != "" {
					fmt.Fprintf(buf, "  Doc: %s\n", strings.TrimSpace(r.Documentation))
				}
				fmt.Fprintf(buf, "  Error: %s\n", r.Error)
			}
		}
	}
	if failCount > 20 {
		fmt.Fprintf(buf, "... and %d more failed tests\n", failCount-20)
	}
}

// FolderTestCase represents a test case loaded from a numbered folder
type FolderTestCase struct {
	Number    int               // Test number (001-373)
	Incorrect string            // Path to incorrect schema (i.rng) if exists
	Correct   string            // Path to correct schema (c.rng) if exists
	Valid     []string          // Paths to valid document files (*.v.xml)
	Invalid   []string          // Paths to invalid document files (*.i.xml)
	Resources map[string]string // Extra files (directories, etc.)
}

// LoadTestsFromFolder loads all tests from the numbered folder structure
func LoadTestsFromFolder(testDataPath string) ([]FolderTestCase, error) {
	entries, err := os.ReadDir(testDataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read test directory: %w", err)
	}

	var testDirs []string
	for _, entry := range entries {
		if entry.IsDir() {
			// Check if it's a numbered folder (001-373)
			if len(entry.Name()) == 3 && isNumeric(entry.Name()) {
				testDirs = append(testDirs, entry.Name())
			}
		}
	}

	// Sort numerically
	sort.Slice(testDirs, func(i, j int) bool {
		a, _ := strconv.Atoi(testDirs[i])
		b, _ := strconv.Atoi(testDirs[j])
		return a < b
	})

	var tests []FolderTestCase
	for _, dir := range testDirs {
		testNum, _ := strconv.Atoi(dir)
		testPath := filepath.Join(testDataPath, dir)
		tc, err := loadFolderTest(testPath, testNum)
		if err != nil {
			return nil, fmt.Errorf("failed to load test %s: %w", dir, err)
		}
		if tc != nil {
			tests = append(tests, *tc)
		}
	}

	return tests, nil
}

// isNumeric checks if a string contains only digits
func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// loadFolderTest loads a single test from a numbered folder
func loadFolderTest(testPath string, testNum int) (*FolderTestCase, error) {
	entries, err := os.ReadDir(testPath)
	if err != nil {
		return nil, err
	}

	tc := &FolderTestCase{
		Number:    testNum,
		Resources: make(map[string]string),
	}

	// Collect both files and directories as resources for includes/externalRefs
	// Files are stored with their filename as path, directories with their full path

	for _, entry := range entries {
		name := entry.Name()
		fullPath := filepath.Join(testPath, name)

		if entry.IsDir() {
			// Collect directory resources
			tc.Resources[name] = fullPath
		} else {
			// Process files
			switch {
			case name == "i.rng":
				tc.Incorrect = fullPath
			case name == "c.rng":
				tc.Correct = fullPath
			case strings.HasSuffix(name, ".v.xml"):
				tc.Valid = append(tc.Valid, fullPath)
			case strings.HasSuffix(name, ".i.xml"):
				tc.Invalid = append(tc.Invalid, fullPath)
			default:
				// Any other file (like "x") is a resource that may be included
				tc.Resources[name] = fullPath
			}
		}
	}

	// Sort valid and invalid files for consistent ordering
	sort.Strings(tc.Valid)
	sort.Strings(tc.Invalid)

	// Skip if no schema
	if tc.Incorrect == "" && tc.Correct == "" {
		return nil, nil
	}

	return tc, nil
}

// processIncorrectSchemaTest processes a test case with an incorrect schema (should fail to parse)
func processIncorrectSchemaTest(tc *FolderTestCase, testID, section string) (TestResult, error) {
	schemaData, err := os.ReadFile(tc.Incorrect)
	if err != nil {
		return TestResult{}, fmt.Errorf("failed to read schema %s: %w", tc.Incorrect, err)
	}

	result := TestResult{
		Section:       section,
		Category:      "SyntaxError",
		Index:         tc.Number,
		TestID:        testID,
		Documentation: fmt.Sprintf("Test case %d - Incorrect schema", tc.Number),
	}

	// If there are resources (subdirectories), we need a virtual filesystem
	hasResources := len(tc.Resources) > 0
	if hasResources {
		result, _ = validateIncorrectSchemaWithResources(tc, schemaData, result)
	} else {
		result = validateIncorrectSchemaWithoutResources(schemaData, result)
	}

	return result, nil
}

// validateIncorrectSchemaWithResources validates an incorrect schema using a virtual filesystem
func validateIncorrectSchemaWithResources(tc *FolderTestCase, schemaData []byte, result TestResult) (TestResult, error) {
	vfs := NewVirtualFilesystem()
	for _, resourcePath := range tc.Resources {
		if err := addResourcesRecursive(vfs, resourcePath, ""); err != nil {
			return TestResult{}, fmt.Errorf("failed to add resources for test %d: %w", tc.Number, err)
		}
	}

	schemaStr := wrapSchemaIfNeeded(string(schemaData))
	vfs.AddResource("__main__.rng", schemaStr)

	_, err := rng.ParseSchemaWithResolver("__main__.rng", vfs)
	if err == nil {
		result.Passed = false
		result.Error = errSchemaParseFailureExpected
	} else {
		result.Passed = true
	}
	return result, nil
}

// validateIncorrectSchemaWithoutResources validates an incorrect schema directly
func validateIncorrectSchemaWithoutResources(schemaData []byte, result TestResult) TestResult {
	_, err := rng.ParseSchema(strings.NewReader(string(schemaData)))
	if err == nil {
		result.Passed = false
		result.Error = errSchemaParseFailureExpected
	} else {
		result.Passed = true
	}
	return result
}

// processCorrectSchemaTest processes a test case with a correct schema and validates documents
func processCorrectSchemaTest(tc *FolderTestCase, testID, section string, resultID *int) ([]TestResult, error) {
	var testResults []TestResult

	schemaData, err := os.ReadFile(tc.Correct)
	if err != nil {
		return nil, fmt.Errorf("failed to read schema %s: %w", tc.Correct, err)
	}

	schemaStr := wrapSchemaIfNeeded(string(schemaData))

	// Build virtual filesystem from resources
	vfs := NewVirtualFilesystem()
	for _, resourcePath := range tc.Resources {
		if err := addResourcesRecursive(vfs, resourcePath, ""); err != nil {
			return nil, fmt.Errorf("failed to add resources for test %d: %w", tc.Number, err)
		}
	}

	vfs.AddResource("__main__.rng", schemaStr)

	// Parse using the virtual filesystem
	grammar, err := rng.ParseSchemaWithResolver("__main__.rng", vfs)
	if err != nil {
		// If the schema itself fails to parse, mark all related tests as failed
		result := TestResult{
			Section:       section,
			Category:      "SyntaxError",
			Index:         tc.Number,
			TestID:        testID,
			Passed:        false,
			Error:         fmt.Sprintf("Failed to parse correct schema: %v", err),
			Documentation: fmt.Sprintf("Test case %d - Correct schema", tc.Number),
		}
		return []TestResult{result}, nil
	}

	val := validator.NewValidator(grammar, validator.DefaultOptions())

	// Validate valid documents
	validResults, err := validateCorrectDocuments(tc, testID, section, val, resultID)
	if err != nil {
		return nil, err
	}
	testResults = append(testResults, validResults...)

	// Validate invalid documents
	invalidResults, err := validateIncorrectDocuments(tc, testID, section, val, resultID)
	if err != nil {
		return nil, err
	}
	testResults = append(testResults, invalidResults...)

	return testResults, nil
}

// validateCorrectDocuments validates documents that should pass validation
func validateCorrectDocuments(tc *FolderTestCase, testID, section string, val *validator.Validator, resultID *int) ([]TestResult, error) {
	results := make([]TestResult, 0, len(tc.Valid))

	if len(tc.Valid) == 0 {
		return results, nil
	}

	// Get common root directory for all test files
	rootDir := filepath.Dir(tc.Valid[0])
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return nil, fmt.Errorf("failed to open root directory %s: %w", rootDir, err)
	}
	defer func() { _ = root.Close() }()

	for i, validPath := range tc.Valid {
		relPath, err := filepath.Rel(rootDir, validPath)
		if err != nil {
			return nil, fmt.Errorf("failed to compute relative path for %s: %w", validPath, err)
		}

		validData, err := root.ReadFile(relPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read valid test %s: %w", validPath, err)
		}

		result := TestResult{
			Section:       section,
			Category:      "Valid",
			Index:         tc.Number*1000 + i,
			TestID:        testID,
			Documentation: fmt.Sprintf("Test case %d - Valid document %d", tc.Number, i+1),
		}

		errs, err := val.Validate(strings.NewReader(string(validData)))
		if err == nil && len(errs) == 0 {
			result.Passed = true
		} else {
			result.Passed = false
			if err != nil {
				result.Error = fmt.Sprintf("Validation error: %v", err)
			} else if len(errs) > 0 {
				result.Error = fmt.Sprintf("Validation failed: %v", errs[0])
			}
		}

		results = append(results, result)
		*resultID++
	}

	return results, nil
}

// validateIncorrectDocuments validates documents that should fail validation
func validateIncorrectDocuments(tc *FolderTestCase, testID, section string, val *validator.Validator, resultID *int) ([]TestResult, error) {
	results := make([]TestResult, 0, len(tc.Invalid))

	if len(tc.Invalid) == 0 {
		return results, nil
	}

	// Get common root directory for all test files
	rootDir := filepath.Dir(tc.Invalid[0])
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return nil, fmt.Errorf("failed to open root directory %s: %w", rootDir, err)
	}
	defer func() { _ = root.Close() }()

	for i, invalidPath := range tc.Invalid {
		relPath, err := filepath.Rel(rootDir, invalidPath)
		if err != nil {
			return nil, fmt.Errorf("failed to compute relative path for %s: %w", invalidPath, err)
		}

		invalidData, err := root.ReadFile(relPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read invalid test %s: %w", invalidPath, err)
		}

		result := TestResult{
			Section:       section,
			Category:      "Invalid",
			Index:         tc.Number*1000 + 10000 + i,
			TestID:        testID,
			Documentation: fmt.Sprintf("Test case %d - Invalid document %d", tc.Number, i+1),
		}

		errs, err := val.Validate(strings.NewReader(string(invalidData)))
		if err != nil || len(errs) > 0 {
			result.Passed = true
		} else {
			result.Passed = false
			result.Error = "Expected validation to fail but it succeeded"
		}

		results = append(results, result)
		*resultID++
	}

	return results, nil
}

// ConvertFolderTestsToResults converts folder-based tests to TestResult slice
func ConvertFolderTestsToResults(tests []FolderTestCase) ([]TestResult, error) {
	var results []TestResult
	resultID := 1

	for _, tc := range tests {
		testID := fmt.Sprintf("T-%03d", tc.Number)
		section := fmt.Sprintf("Test %03d", tc.Number)

		// Case 1: Incorrect schema (should fail to parse)
		if tc.Incorrect != "" {
			result, err := processIncorrectSchemaTest(&tc, testID, section)
			if err != nil {
				return nil, err
			}
			results = append(results, result)
		}

		// Case 2: Correct schema with validation tests
		if tc.Correct != "" {
			testResults, err := processCorrectSchemaTest(&tc, testID, section, &resultID)
			if err != nil {
				return nil, err
			}
			results = append(results, testResults...)
		}
	}

	return results, nil
}

// addResourcesRecursive recursively adds files from a directory to the VFS
func addResourcesRecursive(vfs *VirtualFilesystem, dirPath string, basePath string) error {
	// If basePath is empty and dirPath is a directory, use the directory name as the base path
	if basePath == "" {
		stat, err := os.Stat(dirPath)
		if err == nil && stat.IsDir() {
			basePath = filepath.Base(dirPath)
		}
	}
	return addResourcesRecursiveWithRoot(vfs, dirPath, basePath, nil)
}

// addResourcesRecursiveWithRoot is the internal recursive function that uses os.Root for security
func addResourcesRecursiveWithRoot(vfs *VirtualFilesystem, dirPath string, basePath string, root *os.Root) error {
	// On first call, establish root at the top-level directory
	if root == nil {
		stat, err := os.Stat(dirPath)
		if err != nil {
			return err
		}

		topDir := dirPath
		if !stat.IsDir() {
			topDir = filepath.Dir(dirPath)
		}

		root, err = os.OpenRoot(topDir)
		if err != nil {
			return fmt.Errorf("failed to open root directory %s: %w", topDir, err)
		}
		defer func() { _ = root.Close() }()

		// Compute relative path from root
		relPath, err := filepath.Rel(topDir, dirPath)
		if err != nil {
			return err
		}

		return addResourcesRecursiveWithRoot(vfs, relPath, basePath, root)
	}

	// Check if this is a file or directory
	stat, err := root.Stat(dirPath)
	if err != nil {
		return err
	}

	// If it's a file (not a directory), add it directly
	if !stat.IsDir() {
		return addFileToVFS(vfs, root, dirPath, basePath)
	}

	// It's a directory, process its entries
	return processDirEntries(vfs, root, dirPath, basePath)
}

// addFileToVFS adds a single file to the VFS
func addFileToVFS(vfs *VirtualFilesystem, root *os.Root, filePath string, basePath string) error {
	data, err := root.ReadFile(filePath)
	if err != nil {
		return err
	}
	resourcePath := filepath.Base(filePath)
	if basePath != "" {
		resourcePath = filepath.Join(basePath, resourcePath)
	}
	vfs.AddResource(resourcePath, string(data))
	return nil
}

// processDirEntries processes all entries in a directory recursively
func processDirEntries(vfs *VirtualFilesystem, root *os.Root, dirPath string, basePath string) error {
	// Open and read directory entries
	dir, err := root.Open(dirPath)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()

	entries, err := dir.ReadDir(-1)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		name := entry.Name()
		fullPath := filepath.Join(dirPath, name)

		resourcePath := name
		if basePath != "" {
			resourcePath = filepath.Join(basePath, name)
		}

		if entry.IsDir() {
			// Recurse into subdirectories
			if err := addResourcesRecursiveWithRoot(vfs, fullPath, resourcePath, root); err != nil {
				return err
			}
		} else {
			// Add file to VFS
			data, err := root.ReadFile(fullPath)
			if err != nil {
				return err
			}
			vfs.AddResource(resourcePath, string(data))
		}
	}

	return nil
}

// checkIncorrectSchemaParseResult checks if an incorrect schema parse result is correct
// (it should fail to parse)
func checkIncorrectSchemaParseResult(err error, result TestResult) TestResult {
	if err == nil {
		result.Passed = false
		result.Error = errSchemaParseFailureExpected
	} else {
		result.Passed = true
	}
	return result
}

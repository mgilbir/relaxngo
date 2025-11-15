// Package main runs the official RelaxNG conformance test suite.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mgilbir/relaxngo/validator"
)

func main() {
	verbose := flag.Bool("v", false, "Verbose output (show failed tests)")
	category := flag.String("category", "", "Filter by category (SyntaxError, Valid, Invalid)")
	flag.Parse()

	// Find the test data directory
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting working directory: %v\n", err)
		os.Exit(1)
	}

	// Use numbered folder structure (required)
	testFolderPath := filepath.Join(cwd, "testdata", "official-tests")

	if _, err := os.Stat(testFolderPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: Official test folders not found at %s\n", testFolderPath)
		fmt.Fprintf(os.Stderr, "Run: go run cmd/official-tests/main.go from the project root\n")
		os.Exit(1)
	}

	fmt.Printf("Using folder-based tests from: %s\n", testFolderPath)

	// Load tests from numbered folders
	tests, err := validator.LoadTestsFromFolder(testFolderPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading tests from folders: %v\n", err)
		os.Exit(1)
	}

	// Convert to results
	results, err := validator.ConvertFolderTestsToResults(tests)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error converting tests to results: %v\n", err)
		os.Exit(1)
	}

	// Filter if requested
	if *category != "" {
		results = validator.FilterResults(results, *category)
	}

	// Display results
	fmt.Print(validator.FormatResults(results, *verbose))

	// Exit with error if tests failed
	_, failed, _ := validator.CountResults(results)
	if failed > 0 {
		os.Exit(1)
	}
}

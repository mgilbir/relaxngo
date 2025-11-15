// Package main extracts and processes test definitions from XML files.
package main

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strconv"
)

type TestCase struct {
	Section   string   `xml:"section"`
	Doc       string   `xml:"documentation"`
	Correct   []byte   `xml:"correct"`
	Incorrect []byte   `xml:"incorrect"`
	Valid     [][]byte `xml:"valid"`
	Invalid   [][]byte `xml:"invalid"`
}

func printTestDocumentation(tc *TestCase) {
	if tc.Doc != "" {
		fmt.Printf("Documentation: %s\n\n", tc.Doc)
	}
}

func printTestSchemas(tc *TestCase) {
	if len(tc.Correct) > 0 {
		fmt.Printf("Schema (correct):\n%s\n\n", string(tc.Correct))
	}
	if len(tc.Incorrect) > 0 {
		fmt.Printf("Schema (incorrect):\n%s\n\n", string(tc.Incorrect))
	}
}

func printTestDocuments(tc *TestCase) {
	if len(tc.Valid) > 0 {
		for i, v := range tc.Valid {
			fmt.Printf("Valid document #%d:\n%s\n\n", i+1, string(v))
		}
	}

	if len(tc.Invalid) > 0 {
		for i, inv := range tc.Invalid {
			fmt.Printf("Invalid document #%d:\n%s\n\n", i+1, string(inv))
		}
	}
}

func findAndPrintTest(targetNum int) bool {
	file, err := os.Open("../../testdata/official/spectest.xml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening test file: %v\n", err)
		return false
	}
	defer func() { _ = file.Close() }()

	decoder := xml.NewDecoder(file)
	testNum := 0

	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error decoding: %v\n", err)
			return false
		}

		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "testCase" {
			testNum++
			if testNum == targetNum {
				var tc TestCase
				if err := decoder.DecodeElement(&tc, &se); err != nil {
					fmt.Fprintf(os.Stderr, "Error decoding test case: %v\n", err)
					return false
				}

				fmt.Printf("=== Test T-%03d (Section %s) ===\n\n", testNum, tc.Section)
				printTestDocumentation(&tc)
				printTestSchemas(&tc)
				printTestDocuments(&tc)
				return true
			}
		}
	}

	return false
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <test-number>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Example: %s 96\n", os.Args[0])
		os.Exit(1)
	}

	targetNum, err := strconv.Atoi(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid test number: %v\n", err)
		os.Exit(1)
	}

	if !findAndPrintTest(targetNum) {
		fmt.Fprintf(os.Stderr, "Test T-%03d not found\n", targetNum)
		os.Exit(1)
	}
}

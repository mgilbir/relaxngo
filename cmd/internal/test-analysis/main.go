// Package main analyzes and summarizes validator test results.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/mgilbir/relaxngo/internal/conformance"
)

func main() {
	flag.Parse()

	cwd, _ := os.Getwd()
	testFolderPath := filepath.Join(cwd, "testdata", "official-tests")

	results, err := loadAndConvertTests(testFolderPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	passed, failed := organizeResults(results)
	printSummary(passed, failed)
	printFailedDetails(failed)
}

func loadAndConvertTests(testFolderPath string) ([]conformance.TestResult, error) {
	tests, err := conformance.LoadTestsFromFolder(testFolderPath)
	if err != nil {
		return nil, err
	}

	return conformance.ConvertFolderTestsToResults(tests)
}

func organizeResults(results []conformance.TestResult) (map[int][]conformance.TestResult, map[int][]conformance.TestResult) {
	passed := make(map[int][]conformance.TestResult)
	failed := make(map[int][]conformance.TestResult)

	for _, r := range results {
		var testNum int
		_, _ = fmt.Sscanf(r.TestID, "T-%d", &testNum)

		if r.Passed {
			passed[testNum] = append(passed[testNum], r)
		} else {
			failed[testNum] = append(failed[testNum], r)
		}
	}

	return passed, failed
}

func printSummary(passed, failed map[int][]conformance.TestResult) {
	fmt.Println("\n=== Test Summary by Number ===")
	fmt.Println("(T-XXX: P=Passed, F=Failed, Total documents)")
	fmt.Println()

	allNums := getSortedTestNumbers(passed, failed)
	passCount, failCount := printTestResults(allNums, passed, failed)

	fmt.Printf("\nTotal: %d passed, %d failed out of %d tests\n", passCount, failCount, passCount+failCount)
}

func getSortedTestNumbers(passed, failed map[int][]conformance.TestResult) []int {
	var allNums []int
	seen := make(map[int]bool)
	for num := range passed {
		if !seen[num] {
			allNums = append(allNums, num)
			seen[num] = true
		}
	}
	for num := range failed {
		if !seen[num] {
			allNums = append(allNums, num)
			seen[num] = true
		}
	}
	sort.Ints(allNums)
	return allNums
}

func printTestResults(allNums []int, passed, failed map[int][]conformance.TestResult) (int, int) {
	passCount := 0
	failCount := 0

	for _, num := range allNums {
		p := len(passed[num])
		f := len(failed[num])
		passCount += p
		failCount += f

		status := "PASS"
		if f > 0 {
			status = "FAIL"
		}

		fmt.Printf("T-%03d: [%s] %d passed, %d failed\n", num, status, p, f)
	}

	return passCount, failCount
}

func printFailedDetails(failed map[int][]conformance.TestResult) {
	fmt.Println("\n=== Failed Tests Details ===")
	failCount := 0

	allNums := make([]int, 0, len(failed))
	for num := range failed {
		allNums = append(allNums, num)
	}
	sort.Ints(allNums)

	for _, num := range allNums {
		if len(failed[num]) > 0 {
			for _, r := range failed[num] {
				failCount++
				if failCount <= 50 {
					fmt.Printf("\n[%s] %s\n", r.Category, r.TestID)
					fmt.Printf("  Error: %s\n", r.Error)
				}
			}
		}
	}
	if failCount > 50 {
		fmt.Printf("\n... and %d more failed tests\n", failCount-50)
	}
}

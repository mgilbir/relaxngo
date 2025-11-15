// Package main implements a RelaxNG schema-to-Go code generator.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mgilbir/relaxngo/generator"
	"github.com/mgilbir/relaxngo/rng"
)

func main() {
	schemaPath := flag.String("schema", "", "Path to RELAX NG schema file")
	outputPackage := flag.String("package", "generated", "Package name for generated code")
	flag.Parse()

	if *schemaPath == "" {
		fmt.Fprintln(os.Stderr, "Error: -schema flag is required")
		flag.Usage()
		os.Exit(1)
	}

	absPath := *schemaPath
	if !filepath.IsAbs(absPath) {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting working directory: %v\n", err)
			os.Exit(1)
		}
		absPath = filepath.Join(cwd, absPath)
	}

	// Read schema file content for embedding
	// #nosec G304 - Reading user-provided schema path is the intended purpose of this tool
	schemaContent, err := os.ReadFile(absPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading schema file: %v\n", err)
		os.Exit(1)
	}

	grammar, err := rng.ParseSchemaFile(absPath, filepath.Dir(absPath))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing schema: %v\n", err)
		os.Exit(1)
	}

	types, err := generator.GenerateTypes(grammar)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating types: %v\n", err)
		os.Exit(1)
	}

	code, err := generator.GenerateCode(types, *outputPackage, string(schemaContent), grammar)

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating code: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(code)
}

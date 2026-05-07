// Command okapi-gen-typed generates a statically typed Go client from an
// OpenAPI 3.x spec. Output is two files in the target directory:
//
//	types.gen.go    — typed structs for components.schemas
//	client.gen.go   — typed methods (one per operationId) on a Client struct
//
// Usage:
//
//	okapi-gen-typed --source <url|path> --package <name> --out <dir>
//
// The output is self-contained: it depends only on the standard library.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jathanism/okapi/gen/typed"
)

func main() {
	source := flag.String("source", "", "OpenAPI spec source: file://, http(s)://, or a path")
	pkg := flag.String("package", "", "Go package name for generated files (required)")
	out := flag.String("out", ".", "Output directory")
	clientName := flag.String("client", "Client", "Name of the generated client struct")
	flag.Parse()

	if *source == "" || *pkg == "" {
		fmt.Fprintln(os.Stderr, "usage: okapi-gen-typed --source <url|path> --package <name> [--out <dir>] [--client <name>]")
		os.Exit(2)
	}

	files, err := typed.Generate(typed.Options{
		Source:      *source,
		PackageName: *pkg,
		ClientName:  *clientName,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate: %v\n", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(*out, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir %s: %v\n", *out, err)
		os.Exit(1)
	}

	for name, data := range files {
		path := filepath.Join(*out, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "write %s: %v\n", path, err)
			os.Exit(1)
		}
		fmt.Printf("wrote %s (%d bytes)\n", path, len(data))
	}
}

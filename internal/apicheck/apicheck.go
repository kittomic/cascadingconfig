// Package apicheck checks the design records' Go listings against the packages' real APIs (DL-256), and renders each
// released package's exported API for the guard files under api/ (DL-257). It is a test-only module and is never
// released.
package apicheck

import (
	"fmt"
	"go/types"
)

// Load type-checks the packages that patterns name from their export data, running go list in dir, and returns them
// by import path.
func Load(dir string, patterns ...string) (map[string]*types.Package, error) {
	return nil, fmt.Errorf("apicheck: Load(%q, %q) not implemented", dir, patterns)
}

// Finding is one difference between a listing and the real API.
type Finding struct {
	Doc     string // the doc's name, as passed to Listings
	Line    int    // the doc's line holding the declaration, or the block's opening fence for a whole block
	Package string // the import path the declaration was checked against; empty for a whole block
	Name    string // the declaration, such as "WithScopes" or "Loader.Load"; empty for a whole block
	Message string // starts with "missing", "differs", "kind", "unparsable" or "unknown package"
}

func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: %s %s: %s", f.Doc, f.Line, f.Package, f.Name, f.Message)
}

// Listings checks the Go listings in doc, the markdown file called name, against pkgs. module is the import path that
// package markers' paths are relative to, and "." names module itself.
func Listings(module string, pkgs map[string]*types.Package, name string, doc []byte) []Finding {
	_, _, _, _ = module, pkgs, name, doc // not implemented yet
	return nil
}

// API renders pkg's exported API, one declaration per line, sorted (DL-257).
func API(pkg *types.Package) []string {
	_ = pkg // not implemented yet
	return nil
}

// Diff compares an API file's lines with the rendered ones: missing are rendered lines the file lacks, and extra are
// lines in the file that no longer render.
func Diff(file, rendered []string) (missing, extra []string) {
	_, _ = file, rendered // not implemented yet
	return nil, nil
}

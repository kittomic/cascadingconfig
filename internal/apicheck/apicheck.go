// Package apicheck checks the design records' Go listings against the packages' real APIs (DL-256), and renders each
// released package's exported API for the guard files under api/ (DL-257). It is a test-only module and is never
// released.
package apicheck

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/importer"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"slices"
)

// Load type-checks the packages that patterns name from their export data, running go list in dir, and returns them
// by import path.
//
// go test's cache doesn't see what Load reads, so a package whose tests use it reads CC_TEST_INPUTS, which
// scripts/each-module.sh sets to a hash of the repository (DL-288).
func Load(dir string, patterns ...string) (map[string]*types.Package, error) {
	args := append([]string{"list", "-export", "-json", "-deps", "--"}, patterns...)
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("apicheck: go list: %w: %s", err, stderr.String())
	}
	exports := map[string]string{}
	var wanted []string
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p struct {
			ImportPath, Export string
			DepOnly            bool
			Error              *struct{ Err string }
		}
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("apicheck: reading go list's output: %w", err)
		}
		if p.Error != nil {
			return nil, fmt.Errorf("apicheck: %s: %s", p.ImportPath, p.Error.Err)
		}
		exports[p.ImportPath] = p.Export
		if !p.DepOnly {
			wanted = append(wanted, p.ImportPath)
		}
	}
	imp := importer.ForCompiler(token.NewFileSet(), "gc", func(path string) (io.ReadCloser, error) {
		file, ok := exports[path]
		if !ok || file == "" {
			return nil, fmt.Errorf("no export data for %s", path)
		}
		return os.Open(file)
	})
	pkgs := map[string]*types.Package{}
	for _, path := range wanted {
		pkg, err := imp.Import(path)
		if err != nil {
			return nil, fmt.Errorf("apicheck: importing %s: %w", path, err)
		}
		pkgs[path] = pkg
	}
	return pkgs, nil
}

// Finding is one difference between a listing and the real API.
type Finding struct {
	Doc     string // the doc's name, as passed to Listings
	Line    int    // the doc's line holding the declaration, or the block's opening fence for a whole block
	Package string // the import path the declaration was checked against; empty for a whole block
	Name    string // the declaration, such as "WithScopes" or "Loader.Load"; empty for a whole block
	Message string // starts with "missing", "differs", "kind", "incomplete", "unparsable" or "unknown package"
}

func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: %s %s: %s", f.Doc, f.Line, f.Package, f.Name, f.Message)
}

// Diff compares an API file's lines with the rendered ones: missing are rendered lines the file lacks, and extra are
// lines in the file that no longer render. Blank lines are ignored.
func Diff(file, rendered []string) (missing, extra []string) {
	for _, l := range rendered {
		if l != "" && !slices.Contains(file, l) {
			missing = append(missing, l)
		}
	}
	for _, l := range file {
		if l != "" && !slices.Contains(rendered, l) {
			extra = append(extra, l)
		}
	}
	return missing, extra
}

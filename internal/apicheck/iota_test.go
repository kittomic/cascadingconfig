package apicheck_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"slices"
	"testing"

	"github.com/kittomic/cascadingconfig/internal/apicheck"
)

// In a const group, iota counts the specs, and a spec with no values repeats the last one's type and values, so each
// constant's value is checked as Go would compute it.
func TestListingsIota(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "flags.go", "package m\ntype Flag int\nconst (\n\tA Flag = 1 << iota\n\tB\n\tC\n)\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := (&types.Config{}).Check(fixtureModule, fset, []*ast.File{f}, nil)
	if err != nil {
		t.Fatal(err)
	}
	pkgs := map[string]*types.Package{fixtureModule: pkg}
	for _, c := range []struct {
		doc  string
		want []string
	}{
		{md("'''go\nconst (\n\tA Flag = 1 << iota\n\tB\n\tC\n)\n'''\n"), nil},
		{md("'''go\nconst (\n\tA Flag = 1 << iota\n\tC\n\tB\n)\n'''\n"), []string{"B", "C"}},
	} {
		var got []string
		for _, f := range apicheck.Listings(fixtureModule, pkgs, "doc.md", []byte(c.doc)) {
			got = append(got, f.Name)
		}
		slices.Sort(got)
		if !slices.Equal(got, c.want) {
			t.Errorf("findings for\n%s\nname %q, want %q", c.doc, got, c.want)
		}
	}
}

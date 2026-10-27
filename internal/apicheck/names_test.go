package apicheck_test

import (
	"fmt"
	"testing"

	"github.com/kittomic/cascadingconfig/internal/apicheck"
)

// DL-267: backticked identifiers in a user doc must name something in the packages, qualified by a package's name or
// not: a package-level name, a method or a field, with a call's arguments or type arguments ignored. All-capitals
// names, other libraries' names, allowed names, lowercase words, other inline code and code blocks are skipped.
func TestNames(t *testing.T) {
	t.Parallel()
	doc := md(`# Fixture

Build a ` + "`Loader`" + ` with ` + "`New`" + ` or ` + "`m.New`" + `, then call ` + "`Loader.Load`" + ` or ` + "`Load`" + `.
` + "`Layer.Path`" + ` is a field, ` + "`Decoder.Format`" + ` a method, ` + "`WithRule[P]`" + ` generic, ` + "`WithMap(f)`" + ` a call,
and ` + "`sub.Preset`" + ` another package's variable.

These name nothing: ` + "`Nope`" + `, ` + "`m.Nope`" + ` and ` + "`Layer.Nope`" + `.

` + "`MYAPP_ENV`" + ` and ` + "`HOME`" + ` are variables, ` + "`slog.LevelWarn`" + ` and ` + "`time.Duration`" + ` another library's,
` + "`Embedded`" + ` is allowed, ` + "`name`" + ` is a word and ` + "`go run .`" + ` a command.

'''go
Nope2()
'''
`)
	var got []string
	for _, f := range apicheck.Names(fixture(t), "doc.md", []byte(doc), []string{"Embedded"}) {
		if f.Doc != "doc.md" || len(f.Message) < 7 || f.Message[:7] != "missing" {
			t.Errorf("finding %v: want doc.md and a message starting \"missing\"", f)
		}
		got = append(got, fmt.Sprintf("%d %s", f.Line, f.Name))
	}
	want := []string{
		fmt.Sprintf("%d Nope", at(t, doc, "`Nope`")),
		fmt.Sprintf("%d m.Nope", at(t, doc, "`m.Nope`")),
		fmt.Sprintf("%d Layer.Nope", at(t, doc, "`Layer.Nope`")),
	}
	assertFindings(t, got, want)
}

package cascadingconfig_test

import (
	"context"
	"fmt"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
)

// FuzzCustomSources (DL-238): a custom scope returning random sources never panics. A load with only valid sources of
// files that exist succeeds, with the last source's value on top; one with any broken source is a 500; otherwise one
// with a missing file is a 404.
func FuzzCustomSources(f *testing.F) {
	f.Add([]byte{0, 1, 2})
	f.Add([]byte{3, 0})
	f.Add([]byte{0, 4})
	f.Add([]byte{5, 6, 7, 8})
	f.Add([]byte{9, 1})
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 12 {
			b = b[:12]
		}
		files := map[string]string{}
		var srcs []cc.Source
		broken, missing := false, false
		for i, c := range b {
			mark := fmt.Sprintf(`{"s": %d}`, i)
			p := fmt.Sprintf("/srv/f%d.json", i)
			switch c % 10 {
			case 0: // a file
				files[p] = mark
				srcs = append(srcs, cc.Source{Path: p})
			case 1: // a tree
				srcs = append(srcs, cc.Source{Tree: cc.Tree{"s": int64(i)}, Variant: cc.Variant(c / 10 % 4)})
			case 2: // a file with an explicit format and env
				q := fmt.Sprintf("/srv/f%d.conf", i)
				files[q] = mark
				srcs = append(srcs, cc.Source{Path: q, Format: "json", Variant: cc.VariantEnv, Env: "dev"})
			case 3: // missing
				srcs = append(srcs, cc.Source{Path: p})
				missing = true
			case 4: // relative
				srcs = append(srcs, cc.Source{Path: fmt.Sprintf("srv/f%d.json", i)})
				broken = true
			case 5: // both
				files[p] = mark
				srcs = append(srcs, cc.Source{Path: p, Tree: cc.Tree{"s": int64(i)}})
				broken = true
			case 6: // neither
				srcs = append(srcs, cc.Source{Env: "x"})
				broken = true
			case 7: // an unregistered format
				files[p] = mark
				srcs = append(srcs, cc.Source{Path: p, Format: "toml"})
				broken = true
			case 8: // a non-canonical tree
				srcs = append(srcs, cc.Source{Tree: cc.Tree{"s": i}})
				broken = true
			case 9: // a path with dot segments
				files[p] = mark
				srcs = append(srcs, cc.Source{Path: fmt.Sprintf("/srv/x/../f%d.json", i)})
			}
		}
		s := &scopeFunc{discover: func(context.Context, cc.Discovery) ([]cc.Source, error) { return srcs, nil }}
		res, err := custLoad(t, cctest.NewFS(files), s)
		switch {
		case broken:
			if cc.CodeOf(err) != cc.CodeInternal {
				t.Fatalf("broken sources: %v (code %d), want a 500", err, cc.CodeOf(err))
			}
		case missing:
			if cc.CodeOf(err) != cc.CodeNotFound {
				t.Fatalf("a missing file: %v (code %d), want a 404", err, cc.CodeOf(err))
			}
		case err != nil:
			t.Fatalf("valid sources: %v", err)
		case len(srcs) > 0:
			if got, want := res.Tree()["s"], int64(len(srcs)-1); got != want {
				t.Fatalf("s = %v, want %d from the last source", got, want)
			}
		}
	})
}

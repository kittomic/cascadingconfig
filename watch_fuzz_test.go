package cascadingconfig_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
)

// treeGen builds a tree from fuzz bytes: keys k0 to k3, at most 3 levels deep, with int64s, marked strings, arrays and
// non-empty tables as values.
type treeGen struct {
	b []byte
	i int
}

func (g *treeGen) next() byte {
	if g.i >= len(g.b) {
		return 0
	}
	g.i++
	return g.b[g.i-1]
}

func (g *treeGen) table(depth int) map[string]any {
	t := map[string]any{}
	n := 1 + int(g.next()%4)
	for range n {
		t[fmt.Sprintf("k%d", g.next()%4)] = g.value(depth)
	}
	return t
}

func (g *treeGen) value(depth int) any {
	switch c := g.next() % 5; {
	case c == 0:
		return int64(g.next() % 3)
	case c == 1:
		return fmt.Sprintf("VAL%d", g.next()%3)
	case c == 2:
		return []any{int64(g.next() % 2), int64(g.next() % 2)}
	case depth < 3:
		return g.table(depth + 1)
	default:
		return int64(7)
	}
}

// wantDiff is the diff DL-221 describes, from a to b: leaves only, an array as one leaf, added and removed tables as
// their leaves, and a table that becomes a leaf, or the reverse, as one change.
func wantDiff(a, b any, at cc.Path, out *[]cc.KeyDiff) {
	ta, aTable := a.(map[string]any)
	tb, bTable := b.(map[string]any)
	switch {
	case aTable && bTable:
		keys := map[string]bool{}
		for k := range ta {
			keys[k] = true
		}
		for k := range tb {
			keys[k] = true
		}
		for _, k := range slices.Sorted(maps.Keys(keys)) {
			p := append(slices.Clone(at), k)
			va, inA := ta[k]
			vb, inB := tb[k]
			switch {
			case inA && inB:
				wantDiff(va, vb, p, out)
			case inA:
				leaves(va, p, func(p cc.Path, v any) { *out = append(*out, cc.KeyDiff{Path: p, Kind: cc.DiffRemoved, Old: v}) })
			default:
				leaves(vb, p, func(p cc.Path, v any) { *out = append(*out, cc.KeyDiff{Path: p, Kind: cc.DiffAdded, New: v}) })
			}
		}
	case !reflect.DeepEqual(a, b):
		*out = append(*out, cc.KeyDiff{Path: at, Kind: cc.DiffChanged, Old: a, New: b})
	}
}

func leaves(v any, at cc.Path, f func(cc.Path, any)) {
	t, ok := v.(map[string]any)
	if !ok {
		f(at, v)
		return
	}
	for _, k := range slices.Sorted(maps.Keys(t)) {
		leaves(t[k], append(slices.Clone(at), k), f)
	}
}

// FuzzDiff (DL-221, DL-233): for random trees a and b, a reload from a to b calls the handler exactly when they differ,
// with the diff DL-221 describes; every entry's origins name the file; and no String() shows a value.
func FuzzDiff(f *testing.F) {
	f.Add([]byte{1, 0, 1, 0, 1}, []byte{1, 0, 1, 1, 2})
	f.Add([]byte{2, 3, 3, 1, 1, 0}, []byte{2, 3, 3, 2, 0, 1})
	f.Add([]byte{4, 0, 3, 1, 0, 2}, []byte{1, 0, 0, 1})
	f.Add([]byte{1, 2, 2, 1, 0}, []byte{1, 2, 2, 1, 0})
	f.Fuzz(func(t *testing.T, ab, bb []byte) {
		a, b := (&treeGen{b: ab}).table(1), (&treeGen{b: bb}).table(1)
		ja, err := json.Marshal(a)
		if err != nil {
			t.Fatal(err)
		}
		jb, err := json.Marshal(b)
		if err != nil {
			t.Fatal(err)
		}
		e := newWatchEnv(t, map[string]string{rootCfg: string(ja)})
		e.watch([]cc.WatchOption{cc.WithDebounce(0, 0)})
		e.save(rootCfg, string(jb))
		var want []cc.KeyDiff
		wantDiff(normalise(t, ja), normalise(t, jb), nil, &want)
		got := e.changes.all()
		if len(want) == 0 {
			if len(got) != 0 {
				t.Fatalf("equal trees called the handler: %v", got[0].Diff)
			}
			return
		}
		if len(got) != 1 {
			t.Fatalf("different trees: %d handler calls, want 1\na = %s\nb = %s", len(got), ja, jb)
		}
		diff := got[0].Diff
		if len(diff) != len(want) {
			t.Fatalf("Diff has %d entries, want %d\ngot  %v\nwant %v", len(diff), len(want), diff, want)
		}
		for i, d := range diff {
			w := want[i]
			if d.Kind != w.Kind || !slices.Equal(d.Path, w.Path) || !reflect.DeepEqual(d.Old, w.Old) || !reflect.DeepEqual(d.New, w.New) {
				t.Errorf("Diff[%d] = %v %v %#v → %#v, want %v %v %#v → %#v", i, d.Kind, d.Path, d.Old, d.New, w.Kind, w.Path, w.Old, w.New)
			}
			if (d.Kind != cc.DiffAdded && d.OldOrigin.Layer.Path != rootCfg) || (d.Kind != cc.DiffRemoved && d.NewOrigin.Layer.Path != rootCfg) {
				t.Errorf("Diff[%d] origins = %q → %q, want %s", i, d.OldOrigin.Layer.Path, d.NewOrigin.Layer.Path, rootCfg)
			}
			if s := d.String(); strings.Contains(s, "VAL") {
				t.Errorf("Diff[%d].String() = %q shows a value", i, s)
			}
		}
	})
}

// normalise decodes JSON the way the loader does, so integers are int64.
func normalise(t *testing.T, b []byte) map[string]any {
	t.Helper()
	doc, err := jsonDecoder().Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	return doc.Tree
}

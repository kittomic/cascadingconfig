// These tests use Unix paths in cctest's world, which aren't absolute on Windows, so the file doesn't build there
// (DL-282).

//go:build !windows

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

// treeGen builds a tree from fuzz bytes: keys k0 to k3, at most 3 levels deep, with int64s, marked strings, arrays,
// empty tables and non-empty tables as values.
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
	n := int(g.next() % 4)
	for range n {
		t[fmt.Sprintf("k%d", g.next()%4)] = g.value(depth)
	}
	return t
}

func (g *treeGen) value(depth int) any {
	switch c := g.next() % 6; {
	case c == 0:
		return int64(g.next() % 3)
	case c == 1:
		return fmt.Sprintf("VAL%d", g.next()%3)
	case c == 2:
		return []any{int64(g.next() % 2), int64(g.next() % 2)}
	case c == 3:
		return map[string]any{}
	case depth < 3:
		return g.table(depth + 1)
	default:
		return int64(7)
	}
}

// treeLeaves returns the leaves of t by path, where a leaf is a scalar, an array or an empty table (DL-234).
func treeLeaves(t map[string]any) map[string]cc.KeyDiff {
	out := map[string]cc.KeyDiff{}
	var walk func(v any, at cc.Path)
	walk = func(v any, at cc.Path) {
		if m, ok := v.(map[string]any); ok && (len(m) > 0 || len(at) == 0) {
			for k, x := range m {
				walk(x, append(slices.Clone(at), k))
			}
			return
		}
		out[strings.Join(at, "\x00")] = cc.KeyDiff{Path: at, New: v}
	}
	walk(t, nil)
	return out
}

// wantDiff is DL-234's diff from a to b: the leaves that differ, by path, sorted segment by segment.
func wantDiff(a, b map[string]any) []cc.KeyDiff {
	la, lb := treeLeaves(a), treeLeaves(b)
	keys := maps.Clone(la)
	maps.Copy(keys, lb)
	var out []cc.KeyDiff
	for k := range keys {
		x, inA := la[k]
		y, inB := lb[k]
		switch {
		case inA && inB && !reflect.DeepEqual(x.New, y.New):
			out = append(out, cc.KeyDiff{Path: x.Path, Kind: cc.DiffChanged, Old: x.New, New: y.New})
		case inA && !inB:
			out = append(out, cc.KeyDiff{Path: x.Path, Kind: cc.DiffRemoved, Old: x.New})
		case inB && !inA:
			out = append(out, cc.KeyDiff{Path: y.Path, Kind: cc.DiffAdded, New: y.New})
		}
	}
	slices.SortFunc(out, func(x, y cc.KeyDiff) int { return slices.Compare(x.Path, y.Path) })
	return out
}

// replay applies d to a copy of a: removals first, each also removing the tables it leaves empty, then additions and
// changes, creating tables as needed (decision record 0003).
func replay(a map[string]any, d []cc.KeyDiff) map[string]any {
	var copyOf func(v any) any
	copyOf = func(v any) any {
		if m, ok := v.(map[string]any); ok {
			out := map[string]any{}
			for k, x := range m {
				out[k] = copyOf(x)
			}
			return out
		}
		return v
	}
	out := copyOf(a).(map[string]any)
	for _, removals := range []bool{true, false} {
		for _, e := range d {
			if (e.Kind == cc.DiffRemoved) != removals {
				continue
			}
			chain := []map[string]any{out}
			for _, k := range e.Path[:len(e.Path)-1] {
				next, ok := chain[len(chain)-1][k].(map[string]any)
				if !ok {
					next = map[string]any{}
					chain[len(chain)-1][k] = next
				}
				chain = append(chain, next)
			}
			last := e.Path[len(e.Path)-1]
			if !removals {
				chain[len(chain)-1][last] = copyOf(e.New)
				continue
			}
			delete(chain[len(chain)-1], last)
			for i := len(chain) - 1; i > 0 && len(chain[i]) == 0; i-- {
				delete(chain[i-1], e.Path[i-1])
			}
		}
	}
	return out
}

// FuzzDiff (DL-221, DL-233, DL-234): for random trees a and b, with empty tables, a reload from a to b calls the
// handler exactly when they differ, with DL-234's diff; replaying that Diff on a gives b; the reload back gives the
// reverse Diff; every entry's origins name the file; and no String() shows a value.
func FuzzDiff(f *testing.F) {
	f.Add([]byte{1, 0, 1, 0, 1}, []byte{1, 0, 1, 1, 2})
	f.Add([]byte{2, 3, 4, 1, 1, 0}, []byte{2, 3, 4, 2, 0, 1})
	f.Add([]byte{1, 0, 4, 1, 0, 0, 1}, []byte{1, 0, 3})
	f.Add([]byte{1, 0, 4, 1, 0, 0, 1}, []byte{1, 0, 0, 1})
	f.Add([]byte{1, 2, 3}, []byte{})
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
		na, nb := normalise(t, ja), normalise(t, jb)
		want := wantDiff(na, nb)
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
			t.Fatalf("Diff has %d entries, want %d\na = %s\nb = %s\ngot  %v\nwant %v", len(diff), len(want), ja, jb, diff, want)
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
		if r := replay(na, diff); !reflect.DeepEqual(r, nb) {
			t.Errorf("replaying Diff on a gives %v, want b = %v", r, nb)
		}

		e.save(rootCfg, string(ja))
		got = e.changes.all()
		if len(got) != 2 {
			t.Fatalf("the reload back: %d handler calls in all, want 2", len(got))
		}
		back := got[1].Diff
		if len(back) != len(diff) {
			t.Fatalf("the reload back has %d entries, want %d", len(back), len(diff))
		}
		for i, d := range diff {
			r := back[i]
			wantKind := map[cc.DiffKind]cc.DiffKind{cc.DiffAdded: cc.DiffRemoved, cc.DiffRemoved: cc.DiffAdded, cc.DiffChanged: cc.DiffChanged}[d.Kind]
			if r.Kind != wantKind || !slices.Equal(r.Path, d.Path) || !reflect.DeepEqual(r.Old, d.New) || !reflect.DeepEqual(r.New, d.Old) {
				t.Errorf("back[%d] = %v %v, want the reverse of %v %v", i, r.Kind, r.Path, d.Kind, d.Path)
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
	if doc.Tree == nil {
		return map[string]any{}
	}
	return doc.Tree
}

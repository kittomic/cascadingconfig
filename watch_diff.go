package cascadingconfig

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/kittomic/cascadingconfig/internal/tree"
)

// equalValues compares two canonical values exactly (DL-233): strings by their text, and NaN equal to NaN.
func equalValues(a, b any) bool {
	switch a := a.(type) {
	case map[string]any:
		b, ok := b.(map[string]any)
		if !ok || len(a) != len(b) {
			return false
		}
		for k, v := range a {
			w, ok := b[k]
			if !ok || !equalValues(v, w) {
				return false
			}
		}
		return true
	case []any:
		b, ok := b.([]any)
		return ok && slices.EqualFunc(a, b, equalValues)
	case float64:
		b, ok := b.(float64)
		return ok && (a == b || math.IsNaN(a) && math.IsNaN(b))
	case nil:
		return b == nil
	}
	switch b.(type) {
	case map[string]any, []any:
		return false
	}
	return a == b
}

// leaf is one leaf of a tree: a scalar, an array or an empty table (DL-234).
type leaf struct {
	path  Path
	value any
}

// leavesOf returns t's leaves, keyed by path with the segments joined by NUL, which no key contains.
func leavesOf(t Tree) map[string]leaf {
	out := map[string]leaf{}
	var walk func(v any, at Path)
	walk = func(v any, at Path) {
		if m, ok := v.(map[string]any); ok && (len(m) > 0 || len(at) == 0) {
			for k, x := range m {
				walk(x, append(slices.Clip(at), k))
			}
			return
		}
		out[strings.Join(at, "\x00")] = leaf{at, v}
	}
	walk(t, nil)
	return out
}

// diffResults is DL-234's diff from old to new: the leaves that differ, sorted by path segment by segment, with deep
// copies of the values and each side's origin.
func diffResults(old, cur *Result) []KeyDiff {
	lo, ln := leavesOf(old.tree), leavesOf(cur.tree)
	var out []KeyDiff
	origin := func(r *Result, p Path) Origin {
		o, _ := OriginOf(r, p)
		return o
	}
	for k, a := range lo {
		b, inNew := ln[k]
		switch {
		case !inNew:
			out = append(out, KeyDiff{Path: a.path, Kind: DiffRemoved, Old: tree.Copy(a.value), OldOrigin: origin(old, a.path)})
		case !equalValues(a.value, b.value):
			out = append(out, KeyDiff{
				Path: a.path, Kind: DiffChanged, Old: tree.Copy(a.value), New: tree.Copy(b.value),
				OldOrigin: origin(old, a.path), NewOrigin: origin(cur, b.path),
			})
		}
	}
	for k, b := range ln {
		if _, inOld := lo[k]; !inOld {
			out = append(out, KeyDiff{Path: b.path, Kind: DiffAdded, New: tree.Copy(b.value), NewOrigin: origin(cur, b.path)})
		}
	}
	slices.SortFunc(out, func(x, y KeyDiff) int { return slices.Compare(x.Path, y.Path) })
	return out
}

func (k DiffKind) String() string {
	switch k {
	case DiffAdded:
		return "added"
	case DiffRemoved:
		return "removed"
	case DiffChanged:
		return "changed"
	}
	return fmt.Sprintf("DiffKind(%d)", int(k))
}

// String describes the change by its kind, path and the files its old and new values came from. It never shows
// values, because config often holds secrets.
func (d KeyDiff) String() string {
	from, to := describeOrigin(d.OldOrigin), describeOrigin(d.NewOrigin)
	switch d.Kind {
	case DiffAdded:
		return fmt.Sprintf("added %s (%s)", d.Path, to)
	case DiffRemoved:
		return fmt.Sprintf("removed %s (%s)", d.Path, from)
	}
	return fmt.Sprintf("%s %s (%s → %s)", d.Kind, d.Path, from, to)
}

// describeOrigin names the layer an origin points at: its file, or else its scope.
func describeOrigin(o Origin) string {
	switch {
	case o.Layer.Path != "":
		return o.Layer.Path
	case o.Layer.Scope != "":
		return "the " + string(o.Layer.Scope) + " scope"
	}
	return "unknown"
}

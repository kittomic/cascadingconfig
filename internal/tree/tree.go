// Package tree has helpers for canonical config trees: nested map[string]any values holding only the types documented
// on cascadingconfig.Tree.
package tree

import (
	"maps"
	"slices"
)

// Copy returns a deep copy of a canonical value. Maps and slices are copied; every other canonical value is immutable.
func Copy(v any) any {
	switch v := v.(type) {
	case map[string]any:
		return CopyMap(v)
	case []any:
		c := make([]any, len(v))
		for i, e := range v {
			c[i] = Copy(e)
		}
		return c
	default:
		return v
	}
}

// CopyMap returns a deep copy of m. A nil map gives nil.
func CopyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	c := make(map[string]any, len(m))
	for k, e := range m {
		c[k] = Copy(e)
	}
	return c
}

// SortedKeys returns m's keys in order, so that work over a map, and the errors it reports, are deterministic.
func SortedKeys(m map[string]any) []string {
	return slices.Sorted(maps.Keys(m))
}

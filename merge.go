package cascadingconfig

import (
	"errors"
	"fmt"
	"slices"

	"github.com/kittomic/cascadingconfig/internal/tree"
)

// MergeStrategy says how two layers' values at one path combine.
type MergeStrategy int

// Merge strategies.
const (
	// DeepMerge merges tables key by key. It is the default for tables.
	DeepMerge MergeStrategy = iota + 1
	// Replace takes the higher value whole, inheriting nothing below it. It is the default for every other value.
	Replace
	// Append gives the lower array followed by the higher one.
	Append
	// Prepend gives the higher array followed by the lower one.
	Prepend
)

// MergeFunc is a custom merge, set with WithMergeFunc. It gets copies of both values, and runs only when both layers
// set the path. An error it returns becomes a *MergeError.
type MergeFunc func(path Path, lower, higher any) (any, error)

// FormatConflict says what happens when one slot matches more than one file.
type FormatConflict int

// Format conflict policies.
const (
	// ConflictMerge loads every file in the slot, lowest-ranked first, so the highest-ranked wins. It is the default.
	ConflictMerge FormatConflict = iota
	// ConflictFirst loads only the highest-ranked file. The others are ignored, with a WarnFormatConflict warning.
	ConflictFirst
	// ConflictFail fails with a *ConflictError.
	ConflictFail
)

// mergeConfig sets up the merge engine. M3 builds it from options.
type mergeConfig struct {
	rules    *ruleSet      // nil means no rules
	arrays   MergeStrategy // for arrays with no rule: Replace, Append or Prepend
	unsetKey string        // the reserved unset key; empty turns unsetting off
}

// defaultMergeConfig returns the engine defaults: no rules, arrays replaced, and the _unset key.
func defaultMergeConfig() mergeConfig {
	return mergeConfig{arrays: Replace, unsetKey: "_unset"}
}

// mergeLayer is one layer to merge: its metadata and its decoded tree.
type mergeLayer struct {
	layer Layer
	tree  Tree
	vars  map[string]string // an EnvVars layer's variable for each key path; the merge ignores it
}

// merge merges layers, lowest precedence first, into a Result. Each layer's unset paths are removed from the result of
// the layers below it, then its own values merge on top. Every problem found is returned, joined; the layers' own trees
// are never changed.
func merge(cfg mergeConfig, layers []mergeLayer) (*Result, error) {
	m := &merger{cfg: cfg, tree: Tree{}, origins: &originNode{}}
	for i, l := range layers {
		m.layer, m.index = l.layer, i
		m.layers = append(m.layers, l.layer)
		own, unsets, ok := m.prepare(l.tree)
		if !ok {
			continue
		}
		for _, p := range unsets {
			m.unset(p)
		}
		m.mergeMap(m.tree, m.origins, own, Path{})
	}
	if len(m.errs) > 0 {
		return nil, errors.Join(m.errs...)
	}
	return &Result{tree: m.tree, layers: m.layers, origins: m.origins}, nil
}

// merger holds the state of one merge.
type merger struct {
	cfg     mergeConfig
	tree    Tree
	origins *originNode
	layers  []Layer
	errs    []error

	layer Layer // the layer being merged
	index int   // its index in layers
}

// prepare copies a layer's tree without its unset keys, and returns the paths they list, made absolute. A table that
// held only unset keys, or only tables like it, is left out, so it doesn't set its key. It returns false if the
// layer's unset keys were malformed.
func (m *merger) prepare(t Tree) (Tree, []Path, bool) {
	if m.cfg.unsetKey == "" {
		return tree.CopyMap(t), nil, true
	}
	errs := len(m.errs)
	var unsets []Path
	own, _ := m.strip(t, Path{}, &unsets)
	return own, unsets, len(m.errs) == errs
}

// strip copies table, which is at path at, without the unset key. It returns false when the table held only unset keys
// and tables left out for the same reason; an empty table that was written as empty is kept.
func (m *merger) strip(table map[string]any, at Path, unsets *[]Path) (map[string]any, bool) {
	out := make(map[string]any, len(table))
	dropped := false
	for _, k := range tree.SortedKeys(table) {
		v, key := table[k], appendPath(at, k)
		if k == m.cfg.unsetKey {
			m.unsetPaths(v, at, unsets)
			dropped = true
			continue
		}
		switch v := v.(type) {
		case map[string]any:
			c, keep := m.strip(v, key, unsets)
			if !keep {
				dropped = true
				continue
			}
			out[k] = c
		case []any:
			m.checkArray(v, key)
			out[k] = tree.Copy(v)
		default:
			out[k] = v
		}
	}
	return out, len(out) > 0 || !dropped
}

// unsetPaths reads the value of an unset key in the table at path at.
func (m *merger) unsetPaths(v any, at Path, unsets *[]Path) {
	key := appendPath(at, m.cfg.unsetKey)
	list, ok := v.([]any)
	if !ok {
		m.parseError(key, fmt.Errorf("the value must be a list of paths, not %s", describeType(v)))
		return
	}
	for i, e := range list {
		p, err := unsetPath(e)
		if err != nil {
			m.parseError(key, fmt.Errorf("entry %d: %w", i+1, err))
			continue
		}
		*unsets = append(*unsets, append(slices.Clone(at), p...))
	}
}

// unsetPath reads one entry of an unset list: a dotted path string, or a list of key strings.
func unsetPath(e any) (Path, error) {
	switch e := e.(type) {
	case string:
		if e == "" {
			return nil, errors.New("the path is empty")
		}
		return parsePath(e)
	case []any:
		if len(e) == 0 {
			return nil, errors.New("the path is empty")
		}
		p := make(Path, len(e))
		for i, s := range e {
			str, ok := s.(string)
			if !ok {
				return nil, fmt.Errorf("segment %d is %s, but segments must be strings", i+1, describeType(s))
			}
			p[i] = str
		}
		return p, nil
	default:
		return nil, fmt.Errorf("it is %s, but it must be a path string or a list of key strings", describeType(e))
	}
}

// checkArray reports an unset key in any table inside the array a, at path at. Arrays are never merged, so it could
// never remove anything.
func (m *merger) checkArray(a []any, at Path) {
	for _, e := range a {
		m.checkInArray(e, at)
	}
}

func (m *merger) checkInArray(v any, at Path) {
	switch v := v.(type) {
	case []any:
		m.checkArray(v, at)
	case map[string]any:
		if _, ok := v[m.cfg.unsetKey]; ok {
			m.parseError(at, fmt.Errorf("%s can't be used in a table inside an array, because arrays aren't merged",
				m.cfg.unsetKey))
		}
		for _, k := range tree.SortedKeys(v) {
			m.checkInArray(v[k], at)
		}
	}
}

func (m *merger) parseError(key Path, err error) {
	m.errs = append(m.errs, &ParseError{
		BaseError: BaseError{Op: "merge", Path: m.layer.Path, Scope: string(m.layer.Scope), Err: fmt.Errorf("%s: %w", key, err)},
		Format:    m.layer.Format,
	})
}

// unset removes the value at p from the result built so far. A missing key, or a path through a value that isn't a
// table, is a no-op.
func (m *merger) unset(p Path) {
	table, node := m.tree, m.origins
	for i, k := range p {
		v, ok := table[k]
		if !ok {
			return
		}
		node = node.child(k)
		if i == len(p)-1 {
			delete(table, k)
			node.record(m.index, OriginUnset, nil)
			node.children = nil
			return
		}
		if table, ok = v.(map[string]any); !ok {
			return
		}
	}
}

// mergeMap merges the layer's table src into dst, the result's table at path at, whose origins are node.
func (m *merger) mergeMap(dst map[string]any, node *originNode, src map[string]any, at Path) {
	for _, k := range tree.SortedKeys(src) {
		hv, c := src[k], node.child(k)
		lv, ok := dst[k]
		if !ok {
			dst[k] = m.replace(c, hv, OriginSet)
			continue
		}
		dst[k] = m.combine(appendPath(at, k), c, lv, hv)
	}
}

// combine merges the layer's value hv over lv, the value at key so far, and returns the result.
func (m *merger) combine(key Path, node *originNode, lv, hv any) any {
	r, explicit := m.cfg.rules.match(key)
	if explicit && r.fn != nil {
		return m.callFunc(key, node, r.fn, lv, hv) // a function decides for itself, even about null
	}
	if lv == nil || hv == nil {
		return m.replace(node, hv, OriginSet) // null is never a mismatch
	}

	lm, lowerTable := lv.(map[string]any)
	hm, higherTable := hv.(map[string]any)
	la, lowerArray := lv.([]any)
	ha, higherArray := hv.([]any)
	strategy := r.strategy
	if !explicit {
		switch {
		case lowerTable && higherTable:
			strategy = DeepMerge
		case lowerArray && higherArray:
			strategy = m.cfg.arrays
		default:
			strategy = Replace
		}
	}

	switch strategy {
	case DeepMerge:
		if !lowerTable || !higherTable {
			m.mismatch(key, node, strategy, lv, hv)
			return lv
		}
		node.record(m.index, OriginMerged, nil)
		m.mergeMap(lm, node, hm, key)
		return lm
	case Append, Prepend:
		if !lowerArray || !higherArray {
			m.mismatch(key, node, strategy, lv, hv)
			return lv
		}
		lower, higher := node.elementLayers(len(la)), repeat(m.index, len(ha))
		out, elements, kind := slices.Concat(la, ha), slices.Concat(lower, higher), OriginAppended
		if strategy == Prepend {
			out, elements, kind = slices.Concat(ha, la), slices.Concat(higher, lower), OriginPrepended
		}
		node.record(m.index, kind, elements)
		node.children = nil
		return out
	default:
		return m.replace(node, hv, OriginSet)
	}
}

// replace makes hv the value at node, with nothing below it inherited, and returns it.
func (m *merger) replace(node *originNode, hv any, kind OriginKind) any {
	node.record(m.index, kindOf(hv, kind), nil)
	node.reset(hv, m.index, kind)
	return hv
}

// callFunc runs a MergeFunc on copies of both values.
func (m *merger) callFunc(key Path, node *originNode, fn MergeFunc, lv, hv any) any {
	lower := m.layers[node.layer].Path
	v, err := fn(slices.Clone(key), tree.Copy(lv), tree.Copy(hv))
	if err != nil {
		m.errs = append(m.errs, &MergeError{
			BaseError: BaseError{
				Op: "merge", Path: m.layer.Path, Scope: string(m.layer.Scope),
				Err: fmt.Errorf("%s: the merge function failed, merging over %s: %w", key, lower, err),
			},
			Key:   slices.Clone(key),
			Lower: lower,
		})
		return lv
	}
	if err := checkCanonical(v, key); err != nil {
		m.errs = append(m.errs, NewError(CodeInternal, "merge", m.layer.Path,
			fmt.Errorf("%s: the merge function returned %w", key, err)))
		return lv
	}
	return m.replace(node, tree.Copy(v), OriginFunc)
}

func (m *merger) mismatch(key Path, node *originNode, strategy MergeStrategy, lv, hv any) {
	lower := m.layers[node.layer].Path
	need := "arrays"
	if strategy == DeepMerge {
		need = "tables"
	}
	m.errs = append(m.errs, &MergeError{
		BaseError: BaseError{
			Op: "merge", Path: m.layer.Path, Scope: string(m.layer.Scope),
			Err: fmt.Errorf("%s: the %s rule needs two %s, but %s has %s and %s has %s",
				key, strategyName(strategy), need, lower, describeType(lv), m.layer.Path, describeType(hv)),
		},
		Key:   slices.Clone(key),
		Lower: lower,
	})
}

func strategyName(s MergeStrategy) string {
	switch s {
	case DeepMerge:
		return "DeepMerge"
	case Replace:
		return "Replace"
	case Append:
		return "Append"
	case Prepend:
		return "Prepend"
	default:
		return fmt.Sprintf("MergeStrategy(%d)", int(s))
	}
}

// describeType names a canonical value's type for messages, never its value, which may be a secret.
func describeType(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "a boolean"
	case int64:
		return "an integer"
	case float64:
		return "a float"
	case string:
		return "a string"
	case []any:
		return "an array"
	case map[string]any:
		return "a table"
	default:
		return fmt.Sprintf("a %T", v)
	}
}

// kindOf is kind, or OriginNull for a null value.
func kindOf(v any, kind OriginKind) OriginKind {
	if v == nil {
		return OriginNull
	}
	return kind
}

// appendPath returns at with k added, never sharing at's backing array.
func appendPath(at Path, k string) Path {
	return append(at[:len(at):len(at)], k)
}

func repeat(layer, n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = layer
	}
	return out
}

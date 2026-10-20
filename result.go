package cascadingconfig

import (
	"reflect"
	"slices"

	"github.com/kittomic/cascadingconfig/internal/tree"
)

// Result is the outcome of a load: the merged tree and where each value came from.
type Result struct {
	tree    Tree
	layers  []Layer     // the merged layers, lowest precedence first; origins refer to them by index
	origins *originNode // the root; its children follow the tree's tables

	found     []Layer   // every layer the load considered, lowest precedence first, with its status
	warnings  []Warning // warnings that belong to no layer, such as WarnNoHome
	bootstrap []Layer   // the bootstrap files the load read, lowest precedence first
}

// Tree returns a copy of the merged tree, so changing it can't affect the result or another caller.
func (r *Result) Tree() Tree {
	return tree.CopyMap(r.tree)
}

// Origin says where a value in the result came from.
type Origin struct {
	Layer    Layer        // the latest layer to set, merge into, null or unset the value
	Kind     OriginKind   // what that layer did
	Elements []Layer      // OriginAppended and OriginPrepended only: each element's layer, in order
	History  []OriginStep // the earlier steps, oldest first
}

// OriginStep is one earlier step in a value's history.
type OriginStep struct {
	Layer Layer
	Kind  OriginKind
}

// OriginKind says what a layer did to a value.
type OriginKind int

// Origin kinds.
const (
	OriginSet       OriginKind = iota + 1 // set the value, replacing anything below
	OriginMerged                          // a table more than one layer contributed to
	OriginAppended                        // appended to the lower array
	OriginPrepended                       // prepended to the lower array
	OriginNull                            // set the value to null
	OriginFunc                            // the result of a MergeFunc
	OriginUnset                           // removed the value with the unset key
)

// OriginOf reports where the value at path came from: the latest step and its history.
//
// It also reports a key whose latest event was an unset, with kind OriginUnset. It returns false for keys never set,
// for the root, and for path strings that don't parse. History restarts when an enclosing table is replaced, set to null
// or unset; that event is in the table's own history.
func OriginOf[P PathSpec](r *Result, path P) (Origin, bool) {
	p, err := toPath(path)
	if err != nil || len(p) == 0 || r == nil || r.origins == nil {
		return Origin{}, false
	}
	n := r.origins
	for _, k := range p {
		if n = n.children[k]; n == nil {
			return Origin{}, false
		}
	}
	if !n.set {
		return Origin{}, false
	}

	o := Origin{Layer: r.layers[n.layer], Kind: n.kind}
	for _, i := range n.elements {
		o.Elements = append(o.Elements, r.layers[i])
	}
	for _, s := range n.history {
		o.History = append(o.History, OriginStep{Layer: r.layers[s.layer], Kind: s.kind})
	}
	return o, true
}

// toPath converts either path form.
func toPath[P PathSpec](p P) (Path, error) {
	switch v := any(p).(type) {
	case string:
		return ParsePath(v)
	case Path:
		return v, nil
	case []string:
		return Path(v), nil
	}
	rv := reflect.ValueOf(p) // a named type: its underlying type is string or []string
	if rv.Kind() == reflect.String {
		return ParsePath(rv.String())
	}
	return slices.Clone(rv.Convert(reflect.TypeFor[Path]()).Interface().(Path)), nil
}

// originNode records the provenance of one key, and its children follow the keys of the table at that key. Layers are
// referred to by index, so recording a step is cheap.
type originNode struct {
	set      bool
	layer    int
	kind     OriginKind
	elements []int
	history  []originStep
	children map[string]*originNode
}

type originStep struct {
	layer int
	kind  OriginKind
}

// child returns the node for key k, creating it if needed.
func (n *originNode) child(k string) *originNode {
	if n.children == nil {
		n.children = map[string]*originNode{}
	}
	c := n.children[k]
	if c == nil {
		c = &originNode{}
		n.children[k] = c
	}
	return c
}

// record makes a layer's step the node's latest, moving any earlier one into the history.
func (n *originNode) record(layer int, kind OriginKind, elements []int) {
	if n.set {
		n.history = append(n.history, originStep{n.layer, n.kind})
	}
	n.set, n.layer, n.kind, n.elements = true, layer, kind, elements
}

// reset gives every key below v, a value that replaced whatever was at n, a fresh origin.
func (n *originNode) reset(v any, layer int, kind OriginKind) {
	n.children = nil
	table, ok := v.(map[string]any)
	if !ok {
		return
	}
	for k, e := range table {
		c := n.child(k)
		c.record(layer, kindOf(e, kind), nil)
		c.reset(e, layer, kind)
	}
}

// elementLayers returns the layer of each of the n elements of the array at n.
func (n *originNode) elementLayers(count int) []int {
	if (n.kind == OriginAppended || n.kind == OriginPrepended) && len(n.elements) == count {
		return slices.Clone(n.elements)
	}
	return repeat(n.layer, count)
}

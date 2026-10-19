package cascadingconfig

// Result is the outcome of a load: the merged tree and where each value came from.
type Result struct {
	tree Tree
}

// Tree returns a copy of the merged tree, so changing it can't affect the result or another caller.
func (r *Result) Tree() Tree {
	return r.tree
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
	_, _ = r, path // stub
	return Origin{}, false
}

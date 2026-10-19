package cascadingconfig

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

// mergeRule is one per-key rule: a strategy, or a function.
type mergeRule struct {
	path     Path
	strategy MergeStrategy // zero when fn is set
	fn       MergeFunc
}

// ruleSet is a validated set of merge rules.
type ruleSet struct{}

// newRuleSet validates rules. A later rule for the same path replaces an earlier one. Ambiguous rules and rules on the
// root are *OptionErrors, all reported together.
func newRuleSet(rules []mergeRule) (*ruleSet, error) {
	_ = rules // stub
	return &ruleSet{}, nil
}

// mergeLayer is one layer to merge: its metadata and its decoded tree.
type mergeLayer struct {
	layer Layer
	tree  Tree
}

// merge merges layers, lowest precedence first, into a Result.
func merge(cfg mergeConfig, layers []mergeLayer) (*Result, error) {
	_, _ = cfg, layers // stub
	return &Result{}, nil
}

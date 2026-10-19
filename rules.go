package cascadingconfig

import (
	"errors"
	"fmt"
	"slices"
)

// mergeRule is one per-key rule: a strategy, or a function.
type mergeRule struct {
	path     Path
	strategy MergeStrategy // zero when fn is set
	fn       MergeFunc
}

// ruleSet is a validated set of merge rules. No two of its rules are ambiguous, so at most one rule is the most
// specific match for any key.
type ruleSet struct {
	rules []mergeRule
}

// newRuleSet validates rules. A later rule for the same path replaces an earlier one. Ambiguous rules and rules on the
// root are *OptionErrors, all reported together.
func newRuleSet(rules []mergeRule) (*ruleSet, error) {
	var errs []error
	var kept []mergeRule
	index := map[string]int{} // a path's String() is unique, because every Path round-trips through it
	for _, r := range rules {
		if len(r.path) == 0 {
			errs = append(errs, ruleError("a merge rule can't be set on the root"))
			continue
		}
		if r.fn == nil && (r.strategy < DeepMerge || r.strategy > Prepend) {
			errs = append(errs, ruleError(fmt.Sprintf("the merge rule for %q has an unknown strategy %d", r.path, r.strategy)))
			continue
		}
		r.path = slices.Clone(r.path)
		key := r.path.String()
		if i, ok := index[key]; ok {
			kept[i] = r
			continue
		}
		index[key] = len(kept)
		kept = append(kept, r)
	}

	for i := range kept {
		for j := i + 1; j < len(kept); j++ {
			if both, ok := overlap(kept[i].path, kept[j].path); ok {
				errs = append(errs, ruleError(fmt.Sprintf("the merge rules %q and %q are ambiguous: both match %q",
					kept[i].path, kept[j].path, both)))
			}
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return &ruleSet{rules: kept}, nil
}

func ruleError(msg string) error {
	return &OptionError{BaseError: BaseError{Op: "merge rule", Err: errors.New(msg)}}
}

// overlap reports whether two distinct rules are ambiguous: the same length, the same number of wildcards, and some key
// that matches both. It returns the pattern of the keys both match.
func overlap(a, b Path) (Path, bool) {
	if len(a) != len(b) || wildcards(a) != wildcards(b) {
		return nil, false
	}
	both := make(Path, len(a))
	for i := range a {
		switch {
		case a[i] == b[i] || b[i] == "*":
			both[i] = a[i]
		case a[i] == "*":
			both[i] = b[i]
		default:
			return nil, false
		}
	}
	return both, true
}

func wildcards(p Path) int {
	n := 0
	for _, s := range p {
		if s == "*" {
			n++
		}
	}
	return n
}

// match returns the most specific rule for key: the matching rule with the fewest wildcards. Every rule that matches
// has the key's length, because * matches exactly one segment.
func (rs *ruleSet) match(key Path) (mergeRule, bool) {
	if rs == nil {
		return mergeRule{}, false
	}
	best, bestWildcards := -1, 0
	for i, r := range rs.rules {
		if !matches(r.path, key) {
			continue
		}
		if n := wildcards(r.path); best < 0 || n < bestWildcards {
			best, bestWildcards = i, n
		}
	}
	if best < 0 {
		return mergeRule{}, false
	}
	return rs.rules[best], true
}

func matches(rule, key Path) bool {
	if len(rule) != len(key) {
		return false
	}
	for i := range rule {
		if rule[i] != "*" && rule[i] != key[i] {
			return false
		}
	}
	return true
}

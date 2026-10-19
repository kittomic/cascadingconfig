package cascadingconfig

import (
	"errors"
	"slices"

	"github.com/kittomic/cascadingconfig/internal/tree"
)

// WithScopes replaces the selected scopes with exactly names. Their precedence stays as defined, whatever order they're
// listed in. An undefined scope, a scope listed twice, or no scopes at all is an *OptionError.
func WithScopes(names ...ScopeName) Option {
	names = append([]ScopeName(nil), names...)
	return optionFunc(func(c *config) {
		if len(names) == 0 {
			c.fail("WithScopes", "no scopes are listed; use WithoutScopes to remove some")
			return
		}
		selected := map[ScopeName]bool{}
		for _, n := range names {
			switch {
			case c.scope(n) == nil:
				c.fail("WithScopes", "the scope %q isn't defined", n)
			case selected[n]:
				c.fail("WithScopes", "the scope %q is listed twice", n)
			default:
				selected[n] = true
			}
		}
		c.selected = selected
	})
}

// WithoutScopes removes names from the selected scopes. Removing a defined scope that isn't selected does nothing; an
// undefined scope is an *OptionError.
func WithoutScopes(names ...ScopeName) Option {
	names = append([]ScopeName(nil), names...)
	return optionFunc(func(c *config) {
		for _, n := range names {
			if c.scope(n) == nil {
				c.fail("WithoutScopes", "the scope %q isn't defined", n)
				continue
			}
			delete(c.selected, n)
		}
	})
}

// WithScope defines a scope. Defining it doesn't select it. Without Before or After, it goes above every scope defined
// so far. A name already in use is an *OptionError.
func WithScope(name ScopeName, kind ScopeKind, opts ...ScopeOption) Option {
	opts = append([]ScopeOption(nil), opts...)
	return optionFunc(func(c *config) {
		if name == "" {
			c.fail("WithScope", "a scope needs a name")
			return
		}
		if kind == nil {
			c.fail("WithScope", "the scope %q has no kind", name)
			return
		}
		if c.scope(name) != nil {
			c.fail("WithScope", "the scope %q is already defined", name)
			return
		}
		if v, ok := kind.(valuesKind); ok {
			if err := checkCanonical(v.tree, nil); err != nil {
				c.fail("Values", "the tree for scope %q has %w", name, err)
			}
		}

		spec := scopeSpec{def: &scopeDef{name: name, kind: kind, trust: kind.defaultTrust()}}
		for _, o := range opts {
			if o == nil {
				c.fail("WithScope", "the scope %q has a nil option", name)
				continue
			}
			o.applyScopeOption(&spec)
		}
		at, err := c.placement(spec)
		if err != nil {
			c.fail("WithScope", "the scope %q: %w", name, err)
			return
		}
		c.scopes = slices.Insert(c.scopes, at, spec.def)
	})
}

// scopeSpec is a scope being defined: its definition, and where it goes.
type scopeSpec struct {
	def           *scopeDef
	before, after []ScopeName
}

// placement returns the index in c.scopes where spec's scope goes.
func (c *config) placement(spec scopeSpec) (int, error) {
	anchors := len(spec.before) + len(spec.after)
	switch {
	case anchors == 0:
		return len(c.scopes), nil
	case anchors > 1:
		return 0, errors.New("a scope can be placed only once, with Before or After")
	}
	anchor, offset := ScopeName(""), 0
	if len(spec.after) == 1 {
		anchor, offset = spec.after[0], 1
	} else {
		anchor = spec.before[0]
	}
	for i, s := range c.scopes {
		if s.name == anchor {
			return i + offset, nil
		}
	}
	return 0, errors.New("it is placed next to " + quote(anchor) + ", which isn't defined yet")
}

func quote(s ScopeName) string { return `"` + string(s) + `"` }

// Before places a scope directly below scope, which must already be defined.
func Before(scope ScopeName) ScopeOption {
	return scopeOptionFunc(func(s *scopeSpec) { s.before = append(s.before, scope) })
}

// After places a scope directly above scope, which must already be defined.
func After(scope ScopeName) ScopeOption {
	return scopeOptionFunc(func(s *scopeSpec) { s.after = append(s.after, scope) })
}

// Trust sets a scope's trust level, overriding its kind's default.
func Trust(level TrustLevel) ScopeOption {
	return scopeOptionFunc(func(s *scopeSpec) { s.def.trust = level })
}

type scopeOptionFunc func(s *scopeSpec)

func (f scopeOptionFunc) applyScopeOption(s *scopeSpec) { f(s) }

// Values is a scope kind for a fixed tree, such as the app's parsed flags. The tree is copied when Values is called,
// and must hold only canonical values; anything else is an *OptionError at New naming the key. Its default trust is
// TrustUser.
func Values(t Tree) ScopeKind {
	c := tree.CopyMap(t)
	if c == nil {
		c = Tree{}
	}
	return valuesKind{tree: c}
}

// valuesKind is the Values scope kind.
type valuesKind struct {
	tree Tree
}

func (valuesKind) defaultTrust() TrustLevel { return TrustUser }

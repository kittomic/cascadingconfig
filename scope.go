package cascadingconfig

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kittomic/cascadingconfig/internal/tree"
)

// WithScopes replaces the selected scopes with exactly names. Their precedence stays as defined, whatever order they're
// listed in. The defaults scope stays as it was, so it needn't be listed. An undefined scope, a scope listed twice, or
// no scopes at all is an *OptionError.
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
		if s := c.scope(ScopeDefaults); s != nil && c.selected[ScopeDefaults] {
			if _, ok := s.kind.(defaultsKind); ok {
				selected[ScopeDefaults] = true
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

// WithScope defines a scope and selects it (DL-261); a later WithScopes or WithoutScopes can deselect it. Without
// Before or After, it goes above every scope defined so far. A name already in use is an *OptionError.
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
		before := len(c.errs)
		kind.check(c)
		invalid := len(c.errs) > before

		spec := scopeSpec{c: c, def: &scopeDef{name: name, kind: kind, trust: kind.defaultTrust(), invalid: invalid}}
		for _, o := range opts {
			if o == nil {
				c.fail("WithScope", "the scope %q has a nil option", name)
				continue
			}
			o.applyScopeOption(&spec)
		}
		if spec.def.explicitFiles {
			for _, other := range c.scopes {
				if other.explicitFiles {
					c.fail("ReplacedByConfigFiles", "both %q and %q are marked; only one scope can be", other.name, name)
				}
			}
		}
		at, err := c.placement(spec)
		if err != nil {
			c.fail("WithScope", "the scope %q: %w", name, err)
			return
		}
		c.scopes = slices.Insert(c.scopes, at, spec.def)
		c.selected[name] = true // defining a scope selects it (DL-261)
	})
}

// scopeSpec is a scope being defined: its definition, and where it goes.
type scopeSpec struct {
	c             *config
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

// ConfigRoot sets the config root of the scope's RootSearchDir patterns, such as ~. It may start with ~.
func ConfigRoot(dir string) ScopeOption {
	return scopeOptionFunc(func(s *scopeSpec) {
		if dir == "" || !isHomePath(dir) && !filepath.IsAbs(dir) {
			s.c.fail("ConfigRoot", "the config root %q must be absolute or start with ~", dir)
			return
		}
		s.def.configRoot = dir
	})
}

// ReplacedByFile makes the scope load exactly the file that binding b names, when it is set, as an explicit file.
func ReplacedByFile(b Binding) ScopeOption {
	return scopeOptionFunc(func(s *scopeSpec) {
		if b == "" {
			s.c.fail("ReplacedByFile", "an empty binding key")
			return
		}
		s.def.replacedByFile = b
	})
}

// DisabledBy skips the scope when binding b is set.
func DisabledBy(b Binding) ScopeOption {
	return scopeOptionFunc(func(s *scopeSpec) {
		if b == "" {
			s.c.fail("DisabledBy", "an empty binding key")
			return
		}
		s.def.disabledBy = b
	})
}

// ReplacedByConfigFiles marks the scope that explicit config files replace, from WithConfigFiles or
// {NAME}_CONFIG_FILE. They load at its position, even when it isn't selected.
func ReplacedByConfigFiles() ScopeOption {
	return scopeOptionFunc(func(s *scopeSpec) { s.def.explicitFiles = true })
}

// isHomePath reports whether p starts with ~, as ~ or ~/….
func isHomePath(p string) bool {
	return p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`)
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
func (valuesKind) readsFiles() bool         { return false }

func (k valuesKind) check(c *config) {
	if err := checkCanonical(k.tree, nil); err != nil {
		c.fail("Values", "the tree has %w", err)
	}
}

func (k valuesKind) discover(ld *loading, s *scopeDef) {
	ld.addValues(s, tree.CopyMap(k.tree))
}

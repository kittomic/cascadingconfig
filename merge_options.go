package cascadingconfig

// WithMergeRule sets the strategy for a path, in either path form; * matches one key. A rule for a path that already
// has one replaces it. Ambiguous rules are an *OptionError.
func WithMergeRule[P PathSpec](path P, strategy MergeStrategy) Option {
	p, err := toPath(path)
	return optionFunc(func(c *config) {
		switch {
		case err != nil:
			c.fail("WithMergeRule", "%w", err)
		case len(p) == 0:
			c.fail("WithMergeRule", "a merge rule can't be set on the root")
		case strategy < DeepMerge || strategy > Prepend:
			c.fail("WithMergeRule", "the rule for %q has an unknown strategy %d", p, int(strategy))
		default:
			c.rules = append(c.rules, mergeRule{path: p, strategy: strategy})
		}
	})
}

// WithMergeFunc sets a custom merge for a path, in either path form.
func WithMergeFunc[P PathSpec](path P, f MergeFunc) Option {
	p, err := toPath(path)
	return optionFunc(func(c *config) {
		switch {
		case err != nil:
			c.fail("WithMergeFunc", "%w", err)
		case len(p) == 0:
			c.fail("WithMergeFunc", "a merge function can't be set on the root")
		case f == nil:
			c.fail("WithMergeFunc", "the function for %q is nil", p)
		default:
			c.rules = append(c.rules, mergeRule{path: p, fn: f})
		}
	})
}

// WithArrayMerge sets how arrays combine where no rule applies: Replace (the default), Append or Prepend.
func WithArrayMerge(strategy MergeStrategy) Option {
	return optionFunc(func(c *config) {
		if strategy != Replace && strategy != Append && strategy != Prepend {
			c.fail("WithArrayMerge", "arrays can be merged with Replace, Append or Prepend, not %s", strategyName(strategy))
			return
		}
		c.merge.arrays = strategy
	})
}

// WithUnsetKey renames the reserved unset key, which is _unset by default. It shares one setting with WithoutUnset.
func WithUnsetKey(key string) Option {
	return optionFunc(func(c *config) {
		if key == "" {
			c.fail("WithUnsetKey", "the unset key can't be empty; use WithoutUnset to turn unsetting off")
			return
		}
		c.merge.unsetKey = key
	})
}

// WithoutUnsetKey turns unsetting off, so the unset key is plain data.
func WithoutUnsetKey() Option {
	return optionFunc(func(c *config) { c.merge.unsetKey = "" })
}

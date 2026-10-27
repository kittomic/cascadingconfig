package cascadingconfig

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// WithEnvs selects environments, later ones winning. Empty entries are dropped, and with none left, the option means
// not set, so {NAME}_ENV stays in charge. Each name must be safe in a filename.
func WithEnvs(names ...string) Option {
	names = dropEmpty(names)
	return optionFunc(func(c *config) {
		for _, n := range names {
			if err := checkName(n); err != nil {
				c.fail("WithEnvs", "invalid environment name %q: %w", n, err)
			}
		}
		c.envs = slices.Clone(names)
	})
}

// WithConfigFiles names explicit config files, applied in order. Empty entries are dropped, and with none left, the
// option means not set, so {NAME}_CONFIG_FILE stays in charge.
func WithConfigFiles(paths ...string) Option {
	paths = dropEmpty(paths)
	return optionFunc(func(c *config) { c.configFiles = slices.Clone(paths) })
}

// dropEmpty returns s without empty strings, or nil if none are left.
func dropEmpty(s []string) []string {
	var out []string
	for _, e := range s {
		if e != "" {
			out = append(out, e)
		}
	}
	return out
}

// checkName checks that an app or environment name is safe to put in a filename: not empty, . or .., at most 255
// bytes, valid UTF-8, and without path separators, control characters or the characters Windows forbids in filenames.
func checkName(name string) error {
	switch {
	case name == "":
		return errors.New("the name is empty")
	case name == "." || name == "..":
		return fmt.Errorf("the name can't be %q", name)
	case len(name) > 255:
		return fmt.Errorf("the name is %d bytes long; the limit is 255", len(name))
	case !utf8.ValidString(name):
		return errors.New("the name isn't valid UTF-8")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`/\<>:"|?*`, r) {
			return fmt.Errorf("the name contains %q", r)
		}
	}
	return nil
}

// envName returns {NAME} for an app name: upper case, with every character outside [A-Z0-9] replaced by _.
func envName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 'a' + 'A')
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// Binding is the key of an env-var binding: the suffix of its default variable name, {NAME}_<suffix>.
type Binding string

// The built-in bindings.
const (
	BindConfigFile              Binding = "CONFIG_FILE"
	BindConfigDir               Binding = "CONFIG_DIR"
	BindGlobalConfigFile        Binding = "GLOBAL_CONFIG_FILE"
	BindSystemConfigDir         Binding = "SYSTEM_CONFIG_DIR"
	BindSystemConfigFile        Binding = "SYSTEM_CONFIG_FILE"
	BindEnv                     Binding = "ENV"
	BindCeilingPaths            Binding = "CEILING_PATHS"
	BindIgnoredConfigPaths      Binding = "IGNORED_CONFIG_PATHS"
	BindOverrideConfigFilenames Binding = "OVERRIDE_CONFIG_FILENAMES"
	BindConfigFormats           Binding = "CONFIG_FORMATS"
)

var builtinBindings = []Binding{
	BindConfigFile, BindConfigDir, BindGlobalConfigFile, BindSystemConfigDir, BindSystemConfigFile, BindEnv,
	BindCeilingPaths, BindIgnoredConfigPaths, BindOverrideConfigFilenames, BindConfigFormats,
}

// bindingDef is one declared binding. An empty name means the default, {NAME}_<key>.
type bindingDef struct {
	key  Binding
	name string
}

// WithBinding declares a binding or renames it. An empty name means the default, {NAME}_<suffix>. The name must
// match [A-Za-z_][A-Za-z0-9_]*, and no two bindings may share one.
func WithBinding(b Binding, name string) Option {
	return optionFunc(func(c *config) {
		if b == "" || strings.TrimLeft(string(b), "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_") != "" {
			c.fail("WithBinding", "the binding key %q must be letters, digits and _", b)
			return
		}
		if name != "" && !isVarName(name) {
			c.fail("WithBinding", "the variable name %q for %q must match [A-Za-z_][A-Za-z0-9_]*", name, b)
			return
		}
		if i := slices.IndexFunc(c.bindings, func(d bindingDef) bool { return d.key == b }); i >= 0 {
			c.bindings[i].name = name
			return
		}
		c.bindings = append(c.bindings, bindingDef{key: b, name: name})
	})
}

// WithoutBinding turns a binding off. A built-in binding that isn't declared is left alone; an unknown key is an
// *OptionError.
func WithoutBinding(b Binding) Option {
	return optionFunc(func(c *config) {
		if i := slices.IndexFunc(c.bindings, func(d bindingDef) bool { return d.key == b }); i >= 0 {
			c.bindings = slices.Delete(c.bindings, i, i+1)
			return
		}
		if !slices.Contains(builtinBindings, b) {
			c.fail("WithoutBinding", "the binding %q isn't declared", b)
		}
	})
}

func isVarName(s string) bool {
	for i, r := range s {
		letter := r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r == '_'
		if !letter && (i == 0 || r < '0' || r > '9') {
			return false
		}
	}
	return s != ""
}

// finishBindings works out each binding's variable, now that every option has run, and checks that no two share one.
func (c *config) finishBindings() {
	c.vars = map[Binding]string{}
	owner := map[string]Binding{}
	for _, d := range c.bindings {
		v := d.name
		if v == "" {
			v = c.envName + "_" + string(d.key)
		}
		if other, ok := owner[v]; ok {
			c.fail("WithBinding", "the variable %q is used by both the %q and %q bindings", v, other, d.key)
			continue
		}
		owner[v] = d.key
		c.vars[d.key] = v
	}
}

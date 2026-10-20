package cascadingconfig

import (
	"context"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// WithDefaultPolicy sets the policy for every scope without one of its own (DL-188). The last call wins.
func WithDefaultPolicy(opts ...PolicyOption) Option {
	opts = slices.Clone(opts)
	return optionFunc(func(c *config) {
		c.defaultPolicy = newPolicyDef(c, "WithDefaultPolicy", opts)
	})
}

// WithScopePolicy sets scope's policy, which replaces the default as a whole; InheritDefault starts from the default
// instead (DL-188). The last call for a scope wins. A scope that is never declared is an *OptionError (DL-189).
func WithScopePolicy(scope ScopeName, opts ...PolicyOption) Option {
	opts = slices.Clone(opts)
	return optionFunc(func(c *config) {
		if scope == "" {
			c.fail("WithScopePolicy", "a policy needs a scope name")
			return
		}
		if c.scopePolicies == nil {
			c.scopePolicies = map[ScopeName]*policyDef{}
		}
		c.scopePolicies[scope] = newPolicyDef(c, "WithScopePolicy", opts)
	})
}

// policyDef is the options of one WithDefaultPolicy or WithScopePolicy, checked; finish builds the policy from them.
type policyDef struct {
	opts []policyOpt
}

// policyOpt is one policy option: it either starts the policy from the default, or replaces one setting.
type policyOpt struct {
	op      string
	inherit bool
	check   func(c *config) bool // reports the option's own mistakes
	set     func(p *policy)
	schemes []string // the schemes it names, which must have a registered resolver
}

func (policyOpt) policyOption() {}

func newPolicyDef(c *config, op string, opts []PolicyOption) *policyDef {
	d := &policyDef{}
	for _, o := range opts {
		po, ok := o.(policyOpt)
		if !ok {
			c.fail(op, "a nil policy option")
			continue
		}
		if po.check != nil && !po.check(c) {
			continue
		}
		d.opts = append(d.opts, po)
	}
	return d
}

// build returns the policy the options describe, starting from base when one of them is InheritDefault.
func (d *policyDef) build(base *policy) *policy {
	p := &policy{}
	if slices.ContainsFunc(d.opts, func(o policyOpt) bool { return o.inherit }) {
		p = base.clone()
		p.inherited = true
	}
	for _, o := range d.opts {
		if !o.inherit {
			o.set(p)
		}
	}
	return p
}

// policy is one effective policy: what a scope's files may include, and the checks they must pass.
type policy struct {
	inherited    bool
	allResolvers bool     // every registered resolver is usable, as in the engine default
	resolvers    []string // otherwise, the usable schemes
	allow        []Matcher
	allowFor     map[string][]Matcher
	confine      []string
	trust        TrustStore
	perms        func(FileCheck) (Permission, string)
}

func (p *policy) clone() *policy {
	out := *p
	out.resolvers = slices.Clone(p.resolvers)
	out.allow = slices.Clone(p.allow)
	out.allowFor = maps.Clone(p.allowFor)
	out.confine = slices.Clone(p.confine)
	return &out
}

// usable reports whether the policy lets its scope use scheme's resolver.
func (p *policy) usable(c *config, scheme string) bool {
	return c.hasResolver(scheme) && (p.allResolvers || slices.Contains(p.resolvers, scheme))
}

// engineDefault is the policy without WithDefaultPolicy: every registered resolver, and no checks (DL-189).
func engineDefault() *policy { return &policy{allResolvers: true} }

// finishPolicies builds the default policy and each scope's own, and checks the scopes and schemes they name.
func (c *config) finishPolicies() {
	c.policyDefault = engineDefault()
	if c.defaultPolicy != nil {
		c.policyDefault = c.defaultPolicy.build(engineDefault())
		c.policyDefault.inherited = false
		c.checkSchemes(c.defaultPolicy)
	}
	c.policies = map[ScopeName]*policy{}
	for _, name := range slices.Sorted(maps.Keys(c.scopePolicies)) {
		d := c.scopePolicies[name]
		if c.scope(name) == nil {
			c.fail("WithScopePolicy", "the scope %q isn't defined", name)
			continue
		}
		c.policies[name] = d.build(c.policyDefault)
		c.checkSchemes(d)
	}
}

func (c *config) checkSchemes(d *policyDef) {
	for _, o := range d.opts {
		for _, s := range o.schemes {
			if s != "" && !c.hasResolver(s) {
				c.fail(o.op, "no resolver is registered for the scheme %q", s)
			}
		}
	}
}

// policyFor returns a scope's effective policy: its own, or the default.
func (c *config) policyFor(scope ScopeName) *policy {
	if p, ok := c.policies[scope]; ok {
		return p
	}
	if c.policyDefault == nil {
		return engineDefault()
	}
	return c.policyDefault
}

// policyName names the policy that judges scope's files, for messages.
func policyName(scope ScopeName) string {
	if scope == "" {
		return "default"
	}
	return string(scope)
}

// InheritDefault starts the policy from the default policy, wherever it appears in the list.
func InheritDefault() PolicyOption {
	return policyOpt{op: "InheritDefault", inherit: true}
}

// AllowResolvers sets the schemes whose resolvers the policy can use. With no schemes, every include is forbidden. A
// scheme with no registered resolver is an *OptionError (DL-189).
func AllowResolvers(schemes ...string) PolicyOption {
	schemes = slices.Clone(schemes)
	return policyOpt{
		op: "AllowResolvers",
		check: func(c *config) bool {
			if slices.Contains(schemes, "") {
				c.fail("AllowResolvers", "an empty scheme")
				return false
			}
			return true
		},
		set:     func(p *policy) { p.allResolvers, p.resolvers = false, slices.Clone(schemes) },
		schemes: schemes,
	}
}

// Allow sets the allow-list for every scheme: a reference must match at least one of the matchers.
func Allow(m ...Matcher) PolicyOption {
	m = slices.Clone(m)
	return policyOpt{
		op:    "Allow",
		check: func(c *config) bool { return checkMatchers(c, "Allow", m) },
		set:   func(p *policy) { p.allow = slices.Clone(m) },
	}
}

// AllowFor sets scheme's allow-list. A reference must match it as well as the list Allow sets.
func AllowFor(scheme string, m ...Matcher) PolicyOption {
	m = slices.Clone(m)
	return policyOpt{
		op: "AllowFor",
		check: func(c *config) bool {
			if scheme == "" {
				c.fail("AllowFor", "an empty scheme")
				return false
			}
			return checkMatchers(c, "AllowFor", m)
		},
		set: func(p *policy) {
			p.allowFor = maps.Clone(p.allowFor)
			if p.allowFor == nil {
				p.allowFor = map[string][]Matcher{}
			}
			p.allowFor[scheme] = slices.Clone(m)
		},
		schemes: []string{scheme},
	}
}

func checkMatchers(c *config, op string, ms []Matcher) bool {
	ok := true
	for _, m := range ms {
		if m == nil {
			c.fail(op, "a nil matcher")
			ok = false
			continue
		}
		ok = m.check(c) && ok
	}
	return ok
}

// confinePlaceholders are the placeholders a Confine root can start with, besides ~.
var confinePlaceholders = []string{"{config_root}", "{user}", "{system}", "{cwd}"}

// Confine requires a local include to resolve, after symlinks, to one of roots or below it (DL-180). Roots are
// absolute paths, or start with ~, {user}, {system}, {cwd} or {config_root}, the including file's config root.
func Confine(roots ...string) PolicyOption {
	roots = slices.Clone(roots)
	return policyOpt{
		op: "Confine",
		check: func(c *config) bool {
			ok := true
			for _, r := range roots {
				if err := checkRoot(r); err != nil {
					c.fail("Confine", "%w", err)
					ok = false
				}
			}
			return ok
		},
		set: func(p *policy) { p.confine = slices.Clone(roots) },
	}
}

func checkRoot(r string) error {
	switch {
	case r == "":
		return fmt.Errorf("an empty root")
	case filepath.IsAbs(r) || isHomePath(r):
		return nil
	}
	for _, ph := range confinePlaceholders {
		if r == ph || strings.HasPrefix(r, ph+"/") {
			return nil
		}
	}
	if strings.HasPrefix(r, "{") {
		return fmt.Errorf("the root %q starts with an unknown placeholder; use {config_root}, {user}, {system} or {cwd}", r)
	}
	return fmt.Errorf("the root %q must be absolute, or start with ~, {config_root}, {user}, {system} or {cwd}", r)
}

// RequireTrust skips files the store hasn't approved, with a warning (DL-192).
func RequireTrust(store TrustStore) PolicyOption {
	return policyOpt{
		op: "RequireTrust",
		check: func(c *config) bool {
			if store == nil {
				c.fail("RequireTrust", "a nil trust store")
				return false
			}
			return true
		},
		set: func(p *policy) { p.trust = store },
	}
}

// FilePermissions sets the check that every file read, and its containing directory, must pass (DL-191).
func FilePermissions(f func(FileCheck) (Permission, string)) PolicyOption {
	return policyOpt{
		op: "FilePermissions",
		check: func(c *config) bool {
			if f == nil {
				c.fail("FilePermissions", "a nil function")
				return false
			}
			return true
		},
		set: func(p *policy) { p.perms = f },
	}
}

// FileCheck is what a FilePermissions check gets.
type FileCheck struct {
	Path  string
	Info  fs.FileInfo // implements OwnerInfo where the platform has owners
	Dir   bool        // the file's containing directory, not the file
	Scope ScopeName
	UID   int    // the current user
	GOOS  string // the host's, so a check can tell Windows apart
}

// Permission is a FilePermissions check's verdict.
type Permission int

const (
	// PermissionAllow reads the file.
	PermissionAllow Permission = iota
	// PermissionIgnore skips a searched file with a warning; a file named directly fails with a *PolicyError.
	PermissionIgnore
	// PermissionError fails the load with a *PolicyError.
	PermissionError
)

// StrictModes is SSH-style checking: it ignores a file or directory writable by group or other, except a directory with
// the sticky bit, or owned by neither the current user nor root. An unknown owner, and everything on Windows, is
// allowed.
func StrictModes(c FileCheck) (Permission, string) {
	if c.GOOS == "windows" || c.Info == nil {
		return PermissionAllow, ""
	}
	mode := c.Info.Mode()
	if !c.Dir || mode&fs.ModeSticky == 0 {
		switch {
		case mode.Perm()&0o020 != 0:
			return PermissionIgnore, "writable by group"
		case mode.Perm()&0o002 != 0:
			return PermissionIgnore, "writable by other"
		}
	}
	if oi, ok := c.Info.(OwnerInfo); ok {
		if uid, ok := oi.Owner(); ok && uid != 0 && uid != c.UID {
			return PermissionIgnore, fmt.Sprintf("owned by uid %d, neither the current user nor root", uid)
		}
	}
	return PermissionAllow, ""
}

// TrustStore says whether a file has been approved.
type TrustStore interface {
	// Trusted reports whether path (symlinks resolved) has been approved. digest is the lowercase hex SHA-256 of the
	// file's raw bytes, so a store can require re-approval after the file changes.
	Trusted(ctx context.Context, path, digest string) (bool, error)
}

// TrustPaths trusts the files that globs match, as Glob matches, after resolving symlinks and expanding ~. An entry
// without wildcards trusts that path and everything beneath it, as mise's trusted_config_paths does. It has no host to
// ask, so ~ is the process's home directory.
func TrustPaths(globs ...string) TrustStore {
	var ts trustPaths
	for _, g := range globs {
		if g == "" {
			continue
		}
		if isHomePath(g) {
			home, err := os.UserHomeDir()
			if err != nil {
				continue
			}
			g = filepath.ToSlash(home) + filepath.ToSlash(g[1:])
		}
		g = filepath.ToSlash(g)
		if strings.ContainsAny(g, "*?") {
			ts = append(ts, trustEntry{glob: Glob(g).(globMatcher)})
			continue
		}
		ts = append(ts, trustEntry{tree: path.Clean(g)})
	}
	return ts
}

type trustEntry struct {
	tree string // a path whose whole tree is trusted
	glob globMatcher
}

type trustPaths []trustEntry

func (ts trustPaths) Trusted(_ context.Context, p, _ string) (bool, error) {
	p = filepath.ToSlash(p)
	for _, e := range ts {
		switch {
		case e.tree == "":
			if e.glob.matches(p, "") {
				return true, nil
			}
		case e.tree == "/" || p == e.tree || strings.HasPrefix(p, e.tree+"/"):
			return true, nil
		}
	}
	return false, nil
}
